package tier2

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeProvider struct {
	responses []string
	calls     int
	err       error
}

func (p *fakeProvider) Name() string { return "fake" }
func (p *fakeProvider) Complete(_ context.Context, _, _ string) (string, error) {
	p.calls++
	if p.err != nil {
		return "", p.err
	}
	if len(p.responses) == 0 {
		return "", errors.New("no response")
	}
	response := p.responses[0]
	p.responses = p.responses[1:]
	return response, nil
}

func TestHealRunsActualCandidateBytesAndSavesOnlyVerifiedCandidate(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "login.spec.ts")
	original := "import { test, expect } from '@playwright/test';\ntest('save', async ({ page }) => {\n  await page.locator('#old').click();\n  await expect(page.locator('#other')).toBeVisible();\n});\n"
	if err := os.WriteFile(spec, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	candidate := strings.Replace(original, "#old", "#new", 1)
	provider := &fakeProvider{responses: []string{"```typescript\n" + candidate + "```"}}
	var executed []string
	run := func(_ context.Context, path, label string) RunResult {
		data, _ := os.ReadFile(path)
		executed = append(executed, string(data))
		if label == "original" {
			return RunResult{ExecutedTests: 1, Failure: "waiting for locator('#old')"}
		}
		if string(data) == candidate {
			return RunResult{Passed: true, ExecutedTests: 1, Receipt: label}
		}
		return RunResult{ExecutedTests: 1, Failure: "waiting for locator('#old')"}
	}
	result, err := Heal(context.Background(), SessionOptions{Spec: spec, Framework: "playwright", Provider: provider, MaxProposals: 1, Run: run}, func(string, string) (string, bool) { return "", false })
	if err != nil || result.State != "verified" || result.SavedPath != spec+".healed" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	// A declined/non-interactive apply saves a verified candidate, never edits source.
	result, err = Heal(context.Background(), SessionOptions{Spec: spec, Framework: "playwright", Provider: &fakeProvider{responses: []string{"```typescript\n" + candidate + "```"}}, MaxProposals: 1, Run: run, Interactive: func(context.Context) bool { return false }}, func(string, string) (string, bool) { return "", false })
	if err != nil || result.SavedPath != spec+".healed" {
		t.Fatalf("declined proposal was not saved: %+v %v", result, err)
	}
	stored, _ := os.ReadFile(result.SavedPath)
	source, _ := os.ReadFile(spec)
	if string(stored) != candidate || string(source) != original || len(executed) < 4 || executed[0] != original || executed[len(executed)-1] != candidate {
		t.Fatalf("wrong executed or persisted bytes")
	}
	if copies, _ := filepath.Glob(filepath.Join(dir, "login.9lives-heal-*.spec.ts")); len(copies) != 0 {
		t.Fatalf("owned copy leaked: %v", copies)
	}
}

func TestHealRejectsBoundariesAndNeverSavesUnverifiedOutput(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "login.spec.ts")
	original := "import { test } from '@playwright/test';\ntest('save', async ({ page }) => {\n  await page.locator('#old').click();\n});\n"
	_ = os.WriteFile(spec, []byte(original), 0600)
	for _, candidate := range []string{
		strings.Replace(original, "import { test }", "import { test, expect }", 1),
		strings.Replace(original, "#old", "#new", 1) + "// unrelated\n",
	} {
		provider := &fakeProvider{responses: []string{"```typescript\n" + candidate + "```"}}
		result, err := Heal(context.Background(), SessionOptions{Spec: spec, Provider: provider, MaxProposals: 1, Run: func(_ context.Context, _ string, label string) RunResult {
			return RunResult{ExecutedTests: 1, Failure: "waiting for locator('#old')"}
		}}, func(string, string) (string, bool) { return "", false })
		if err != nil || result.State != "unverified" || result.SavedPath != "" {
			t.Fatalf("unsafe candidate result=%+v err=%v", result, err)
		}
	}
}

func TestHealConcurrentEditBlocksApplyAndFailedCandidateCannotApply(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "login.spec.ts")
	original := "test('x', async ({ page }) => {\n  await page.locator('#old').click();\n});\n"
	candidate := strings.Replace(original, "#old", "#new", 1)
	_ = os.WriteFile(spec, []byte(original), 0600)
	provider := &fakeProvider{responses: []string{"```typescript\n" + candidate + "```"}}
	result, err := Heal(context.Background(), SessionOptions{Spec: spec, Provider: provider, Apply: true, MaxProposals: 1, Run: func(_ context.Context, _ string, label string) RunResult {
		if label != "original" {
			_ = os.WriteFile(spec, []byte("edited elsewhere\n"), 0600)
			return RunResult{Passed: true, ExecutedTests: 1}
		}
		return RunResult{ExecutedTests: 1, Failure: "waiting for locator('#old')"}
	}}, func(string, string) (string, bool) { return "", false })
	if err == nil || result.State != "concurrent_edit" || result.Applied {
		t.Fatalf("concurrent edit applied: %+v %v", result, err)
	}
	// A zero-test / failed verification is never persisted or applied.
	_ = os.WriteFile(spec, []byte(original), 0600)
	provider = &fakeProvider{responses: []string{"```typescript\n" + candidate + "```"}}
	result, err = Heal(context.Background(), SessionOptions{Spec: spec, Provider: provider, Apply: true, MaxProposals: 1, Run: func(_ context.Context, _ string, label string) RunResult {
		if label == "original" {
			return RunResult{ExecutedTests: 1, Failure: "waiting for locator('#old')"}
		}
		return RunResult{Passed: true, ExecutedTests: 0}
	}}, func(string, string) (string, bool) { return "", false })
	if err != nil || result.Applied || result.SavedPath != "" {
		t.Fatalf("zero-test result persisted: %+v %v", result, err)
	}
}

func TestSafeCandidatePreservesNewlinesAndMultipleTests(t *testing.T) {
	original := "test('one', async ({ page }) => {\r\n  await page.locator('#old').click();\r\n});\r\ntest('two', async ({ page }) => {\r\n  await page.locator('#keep').click();\r\n});\r\n"
	candidate := strings.Replace(original, "#old", "#new", 1)
	parsed, err := ParseCandidate("```typescript\n"+strings.ReplaceAll(candidate, "\r\n", "\n")+"```", original)
	if err != nil {
		t.Fatal(err)
	}
	parsed = preserveNewlines(original, parsed)
	if err := SafeCandidate(original, parsed, "#old", "playwright"); err != nil || !strings.Contains(parsed, "test('two'") || !strings.Contains(parsed, "\r\n") {
		t.Fatalf("candidate lost preservation: %v %q", err, parsed)
	}
}

func TestSafeCandidateRejectsInjectedControlFlow(t *testing.T) {
	original := "test('save', async ({ page }) => {\n  await page.locator('#old').click();\n  await expect(page.locator('#result')).toHaveAttribute('data-clicked', 'no');\n});\n"
	malicious := strings.Replace(original, "await page.locator('#old').click();", "await page.locator('#new').click(); return;", 1)
	if err := SafeCandidate(original, malicious, "#old", "playwright"); err == nil {
		t.Fatal("accepted control-flow injection")
	}
}

func TestSafeCandidateRejectsSelectorInConditionalOrAssertionBinding(t *testing.T) {
	for _, source := range []string{
		"test('x', async ({ page }) => {\n  if (await page.locator('#old').textContent()) { await expect(page.locator('#status')).toHaveText('impossible'); }\n});\n",
		"test('x', async ({ page }) => {\n  const control = page.locator('#old');\n  await expect(control).toBeVisible();\n});\n",
	} {
		candidate := strings.Replace(source, "#old", "#new", 1)
		if err := SafeCandidate(source, candidate, "#old", "playwright"); err == nil {
			t.Fatalf("accepted non-action locator: %q", source)
		}
	}
}

func TestHealDoesNotEscalateZeroTestsOrAssertions(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "login.spec.ts")
	original := "test('x', async ({ page }) => { await page.locator('#old').click(); });\n"
	if err := os.WriteFile(spec, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	for _, originalResult := range []RunResult{
		{Failure: "waiting for locator('#old')"},
		{ExecutedTests: 1, Failure: "AssertionError: expected result"},
	} {
		provider := &fakeProvider{responses: []string{"unexpected"}}
		result, err := Heal(context.Background(), SessionOptions{Spec: spec, Provider: provider, Run: func(context.Context, string, string) RunResult { return originalResult }}, func(string, string) (string, bool) { return "", false })
		if err != nil || provider.calls != 0 || result.SavedPath != "" || result.Applied {
			t.Fatalf("escalated unsafe original result=%+v calls=%d err=%v", result, provider.calls, err)
		}
	}
}

func TestSafeCandidateRejectsConditionalReturnAndASIContinuationActions(t *testing.T) {
	for _, source := range []string{
		"test('x', async ({ page }) => {\n  if (await page.locator('#old').click()\n  ) { await expect(page.locator('#status')).toHaveText('impossible'); }\n});\n",
		"test('x', async ({ page }) => {\n  return await page.locator('#old').click();\n});\n",
		"test('x', async ({ page }) => {\n  await page.locator('#old').click()\n  && await expect(page.locator('#status')).toHaveText('impossible');\n});\n",
		"test('x', async ({ page }) => {\n  for (\n    ;\n    await page.locator('#old').click();\n  ) { await expect(page.locator('#status')).toHaveText('impossible'); }\n});\n",
		"test('x', async ({ page }) => {\n  foo(await page.locator('#old').click());\n});\n",
		"test('x', async ({ page }) => {\n  (await page.locator('#old').click());\n});\n",
	} {
		if err := SafeCandidate(source, strings.Replace(source, "#old", "#new", 1), "#old", "playwright"); err == nil {
			t.Fatalf("accepted non-standalone action: %q", source)
		}
	}
}

func TestHealRefusesUnsafeTier1BeforeVerification(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "login.spec.ts")
	original := "test('x', async ({ page }) => {\n  if (await page.locator('[data-testid=\"old\"]').textContent()) { await expect(page.locator('#status')).toHaveText('impossible'); }\n});\n"
	if err := os.WriteFile(spec, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	unsafe := strings.Replace(original, "[data-testid=\"old\"]", "[data-testid*=\"old\"]", 1)
	var labels []string
	provider := &fakeProvider{responses: []string{"unexpected"}}
	result, err := Heal(context.Background(), SessionOptions{Spec: spec, Provider: provider, Run: func(_ context.Context, _ string, label string) RunResult {
		labels = append(labels, label)
		return RunResult{ExecutedTests: 1, Failure: "waiting for locator('[data-testid=\\\"old\\\"]')"}
	}}, func(string, string) (string, bool) { return unsafe, true })
	if err != nil || len(labels) != 1 || labels[0] != "original" || result.Tier1.ExecutedTests != 0 || result.Applied || result.SavedPath != "" {
		t.Fatalf("unsafe Tier1 executed or persisted: result=%+v labels=%v err=%v", result, labels, err)
	}
}

func TestHealDoesNotEscalateNonEditableOrAssertionCandidateFailures(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "login.spec.ts")
	original := "test('x', async ({ page }) => {\n  await page.locator('#old').click();\n});\n"
	if err := os.WriteFile(spec, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	for _, failure := range []string{"network request failed waiting for locator('#old')", "syntax error waiting for locator('#old')"} {
		provider := &fakeProvider{responses: []string{"unexpected"}}
		result, err := Heal(context.Background(), SessionOptions{Spec: spec, Provider: provider, Run: func(context.Context, string, string) RunResult { return RunResult{ExecutedTests: 1, Failure: failure} }}, func(string, string) (string, bool) { return "", false })
		if err != nil || provider.calls != 0 || result.State != "unverified" {
			t.Fatalf("failure=%q result=%+v calls=%d err=%v", failure, result, provider.calls, err)
		}
	}
	candidate := strings.Replace(original, "#old", "#new", 1)
	provider := &fakeProvider{responses: []string{"```typescript\n" + candidate + "```", "unexpected"}}
	result, err := Heal(context.Background(), SessionOptions{Spec: spec, Provider: provider, MaxProposals: 2, Run: func(_ context.Context, _ string, label string) RunResult {
		if label == "original" {
			return RunResult{ExecutedTests: 1, Failure: "waiting for locator('#old')"}
		}
		return RunResult{ExecutedTests: 1, Failure: "AssertionError: expected result"}
	}}, func(string, string) (string, bool) { return "", false })
	if err != nil || provider.calls != 1 || result.State != "needs_human" {
		t.Fatalf("result=%+v calls=%d err=%v", result, provider.calls, err)
	}
}

func TestPreserveNewlinesMatchesSourceWithoutTrailingNewline(t *testing.T) {
	original := "test('x', async ({ page }) => {\n  await page.locator('#old').click();\n});"
	candidate := strings.Replace(original, "#old", "#new", 1)
	parsed, err := ParseCandidate("```typescript\n"+candidate+"\n```", original)
	if err != nil {
		t.Fatal(err)
	}
	parsed = preserveNewlines(original, parsed)
	if err := SafeCandidate(original, parsed, "#old", "playwright"); strings.HasSuffix(parsed, "\n") || err != nil {
		t.Fatalf("newline convention unsafe: %q err=%v", parsed, err)
	}
}

func TestParseCandidateTreatsCODEInBareSourceAsSource(t *testing.T) {
	original := "test('x', async ({ page }) => { await page.locator('#old').click(); });\n"
	candidate := strings.Replace(original, "#old", "#new", 1) + "// CODE: is source text\n"
	got, err := ParseCandidate("```typescript\n"+candidate+"```", original)
	if err != nil || got != candidate {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestHealMarksProviderCancellationCanceled(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "login.spec.ts")
	original := "test('x', async ({ page }) => {\n  await page.locator('#old').click();\n});\n"
	if err := os.WriteFile(spec, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	provider := &fakeProvider{err: context.Canceled}
	result, err := Heal(context.Background(), SessionOptions{Spec: spec, Provider: provider, Run: func(context.Context, string, string) RunResult {
		return RunResult{ExecutedTests: 1, Failure: "waiting for locator('#old')"}
	}}, func(string, string) (string, bool) { return "", false })
	if !errors.Is(err, context.Canceled) || result.State != "canceled" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestCleanupStaleOwnedCopiesPreservesLiveForeignAndSymlinkFiles(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "login.spec.ts")
	dead := ownedCopyPath(spec, "000000000001")
	live := ownedCopyPath(spec, "000000000002")
	foreign := filepath.Join(dir, "foreign.9lives-heal-000000000003.spec.ts")
	wrongVersion := ownedCopyPath(spec, "000000000004")
	badNonce := filepath.Join(dir, "login.9lives-heal-not-a-validx.spec.ts")
	symlink := ownedCopyPath(spec, "000000000005")
	for _, path := range []string{dead, live, foreign, wrongVersion, badNonce} {
		if err := os.WriteFile(path, []byte("copy"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(foreign, symlink); err != nil {
		t.Fatal(err)
	}
	metadata := func(pid int, nonce string) []byte {
		b, _ := json.Marshal(ownedCopyMetadata{Version: ownedCopyMetadataVersion, Spec: spec, PID: pid, Nonce: nonce})
		return b
	}
	if err := os.WriteFile(dead+".owner", metadata(999999, "000000000001"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live+".owner", metadata(os.Getpid(), "000000000002"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreign+".owner", metadata(999999, "000000000003"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wrongVersion+".owner", []byte(`{"Version":99,"Spec":"`+spec+`","PID":999999,"Nonce":"000000000004"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badNonce+".owner", metadata(999999, "not-a-validx"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(symlink+".owner", metadata(999999, "000000000005"), 0600); err != nil {
		t.Fatal(err)
	}
	cleanupStaleOwnedCopies(spec)
	for _, path := range []string{dead, dead + ".owner"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("dead copy remained %s: %v", path, err)
		}
	}
	for _, path := range []string{live, live + ".owner", foreign, foreign + ".owner", wrongVersion, wrongVersion + ".owner", badNonce, badNonce + ".owner", symlink, symlink + ".owner"} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("recognized/living copy removed %s: %v", path, err)
		}
	}
}

func TestHealFeedsLatestCandidateAndFailureToNextProposal(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "login.spec.ts")
	original := "test('x', async ({ page }) => {\n  await page.locator('#old').click();\n});\n"
	first := strings.Replace(original, "#old", "#first", 1)
	second := strings.Replace(first, "#first", "#new", 1)
	if err := os.WriteFile(spec, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	provider := &recordingProvider{responses: []string{"```typescript\n" + first + "```", "```typescript\n" + second + "```"}}
	result, err := Heal(context.Background(), SessionOptions{Spec: spec, Framework: "playwright", Provider: provider, MaxProposals: 2, Run: func(_ context.Context, path, label string) RunResult {
		if label == "original" {
			return RunResult{ExecutedTests: 1, Failure: "waiting for locator('#old')"}
		}
		bytes, _ := os.ReadFile(path)
		if string(bytes) == second {
			return RunResult{Passed: true, ExecutedTests: 1}
		}
		return RunResult{ExecutedTests: 1, Failure: "waiting for locator('#first')"}
	}}, func(string, string) (string, bool) { return "", false })
	if err != nil || result.State != "verified" || len(provider.prompts) != 2 {
		t.Fatalf("result=%+v prompts=%d err=%v", result, len(provider.prompts), err)
	}
	if !strings.Contains(provider.prompts[1], "locator('#first')") || !strings.Contains(provider.prompts[1], first) {
		t.Fatalf("second prompt did not carry latest candidate and failure: %q", provider.prompts[1])
	}
}

func TestPromptPreservesCompleteSourceWithinExplicitLimit(t *testing.T) {
	source := "import x from 'x';\n" + strings.Repeat("test('x', () => {});\n", 200)
	prompt := Prompt("playwright", source, "failed")
	if !strings.Contains(prompt, source) {
		t.Fatal("prompt truncated complete source")
	}
}

type recordingProvider struct {
	responses []string
	prompts   []string
}

func (p *recordingProvider) Name() string { return "recording" }
func (p *recordingProvider) Complete(_ context.Context, prompt, _ string) (string, error) {
	p.prompts = append(p.prompts, prompt)
	response := p.responses[0]
	p.responses = p.responses[1:]
	return response, nil
}

func TestParseCandidateAcceptsNativeEnvelopeAndRefusesExtraProse(t *testing.T) {
	original := "test('x', async ({ page }) => { await page.locator('#old').click(); });\n"
	candidate := strings.Replace(original, "#old", "#new", 1)
	parsed, err := ParseCandidate("CODE:\n```typescript\n"+candidate+"```\nCHANGES:\n- replaced locator\n", original)
	if err != nil || parsed != candidate {
		t.Fatalf("native envelope parsed=%q err=%v", parsed, err)
	}
	if _, err := ParseCandidate("CODE:\n```typescript\n"+candidate+"```\nCHANGES:\n- one\n- two\n- three\n- four\n- five\n- six\n", original); err == nil {
		t.Fatal("accepted more than five changes")
	}
}

func TestPromptUsesFrameworkSpecificFenceLanguage(t *testing.T) {
	for framework, language := range map[string]string{"playwright": "javascript", "cypress": "javascript", "selenium": "python"} {
		if prompt := Prompt(framework, "source", "failure"); !strings.Contains(prompt, "```"+language+"\nsource") {
			t.Fatalf("%s prompt=%q", framework, prompt)
		}
	}
}

func TestParseCandidateAcceptsPinnedPythonResponseAndAliases(t *testing.T) {
	original := "test('x', async ({ page }) => { await page.locator('#old').click(); });\n"
	candidate := strings.Replace(original, "#old", "#new", 1)
	response := "REASONING: selector changed\nCHANGES:\n- updated selector\nCODE:\n```JS\n" + candidate + "```\n"
	parsed, err := ParseCandidate(response, original)
	if err != nil || parsed != candidate {
		t.Fatalf("parsed=%q err=%v", parsed, err)
	}
}

func TestPromptBoundsOptionalPageAndConsoleContext(t *testing.T) {
	prompt := PromptWithContext("selenium", "source", "failure", strings.Repeat("p", 3001), []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"})
	if !strings.Contains(prompt, "A Selenium (Python + pytest) test") || !strings.Contains(prompt, "```python\nsource") || strings.Contains(prompt, "11\n") {
		t.Fatalf("prompt contract not bounded: %q", prompt)
	}
	if !strings.Contains(prompt, strings.Repeat("p", 3000)) || strings.Contains(prompt, strings.Repeat("p", 3001)) {
		t.Fatal("page snapshot bound missing")
	}
}

func TestHealRejectsMismatchedTestCountAndCanceledSession(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "login.spec.ts")
	original := "test('x', async ({ page }) => {\n  await page.locator('#old').click();\n});\n"
	candidate := strings.Replace(original, "#old", "#new", 1)
	if err := os.WriteFile(spec, []byte(original), 0750); err != nil {
		t.Fatal(err)
	}
	provider := &fakeProvider{responses: []string{"```javascript\n" + candidate + "```"}}
	result, err := Heal(context.Background(), SessionOptions{Spec: spec, Framework: "playwright", Provider: provider, Apply: true, Run: func(_ context.Context, _ string, label string) RunResult {
		if label == "original" {
			return RunResult{ExecutedTests: 1, Failure: "waiting for locator('#old')"}
		}
		return RunResult{Passed: true, ExecutedTests: 2}
	}}, func(string, string) (string, bool) { return "", false })
	if err != nil || result.Applied || result.SavedPath != "" || result.State != "unverified" {
		t.Fatalf("mismatched count accepted: %+v %v", result, err)
	}
	stored, _ := os.ReadFile(spec)
	if string(stored) != original {
		t.Fatal("mismatched candidate changed source")
	}

	for _, receipt := range []RunResult{
		{Passed: true, ExecutedTests: 1},
		{ExecutedTests: 0, Failure: "waiting for locator('#old')"},
		{ExecutedTests: 1, Failure: "waiting for locator('#old')"},
	} {
		ctx, cancel := context.WithCancel(context.Background())
		deferred := &fakeProvider{responses: []string{"unexpected"}}
		result, err = Heal(ctx, SessionOptions{Spec: spec, Provider: deferred, Run: func(_ context.Context, _ string, label string) RunResult {
			if label == "original" {
				cancel()
				return receipt
			}
			return RunResult{}
		}}, func(string, string) (string, bool) { return "", false })
		if !errors.Is(err, context.Canceled) || result.State != "canceled" || deferred.calls != 0 || result.SavedPath != "" {
			t.Fatalf("cancel gate failed: receipt=%+v result=%+v calls=%d err=%v", receipt, result, deferred.calls, err)
		}
	}
}

func TestHealApplyPreservesModeAndUsesVerifiedBytes(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "login.spec.ts")
	original := "test('x', async ({ page }) => {\n  await page.locator('#old').click();\n});\n"
	candidate := strings.Replace(original, "#old", "#new", 1)
	if err := os.WriteFile(spec, []byte(original), 0750); err != nil {
		t.Fatal(err)
	}
	originalInfo, err := os.Stat(spec)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Heal(context.Background(), SessionOptions{Spec: spec, Framework: "playwright", Provider: &fakeProvider{responses: []string{"```javascript\n" + candidate + "```"}}, Apply: true, Run: func(_ context.Context, path, label string) RunResult {
		if label == "original" {
			return RunResult{ExecutedTests: 1, Failure: "waiting for locator('#old')"}
		}
		got, _ := os.ReadFile(path)
		if string(got) != candidate {
			return RunResult{ExecutedTests: 1, Failure: "wrong executed bytes"}
		}
		return RunResult{Passed: true, ExecutedTests: 1}
	}}, func(string, string) (string, bool) { return "", false })
	if err != nil || !result.Applied {
		t.Fatalf("apply=%+v err=%v", result, err)
	}
	got, _ := os.ReadFile(spec)
	info, _ := os.Stat(spec)
	if string(got) != candidate || info.Mode().Perm() != originalInfo.Mode().Perm() {
		t.Fatalf("apply bytes/mode wrong: %q %o", got, info.Mode().Perm())
	}
}

func paddedLocatorSpec(size int) string {
	base := "test('x', async ({ page }) => {\n  await page.locator('#old').click();\n});\n"
	if len(base) > size {
		panic("test fixture too small")
	}
	return base + "//" + strings.Repeat("x", size-len(base)-3) + "\n"
}

func TestTier2AdmissionLimitPreservesOriginalAndTier1(t *testing.T) {
	for _, tc := range []struct {
		name          string
		passed, tier1 bool
	}{{"passing original", true, false}, {"tier1 remains local", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			spec := filepath.Join(dir, "large.spec.ts")
			original := paddedLocatorSpec(maxTier2SourceBytes + 1)
			if err := os.WriteFile(spec, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			provider := &fakeProvider{}
			result, err := Heal(context.Background(), SessionOptions{Spec: spec, Framework: "playwright", Provider: provider, Run: func(_ context.Context, path, label string) RunResult {
				if label == "original" && tc.passed {
					return RunResult{Passed: true, ExecutedTests: 1}
				}
				bytes, _ := os.ReadFile(path)
				if tc.tier1 && string(bytes) == strings.Replace(original, "#old", "#new", 1) {
					return RunResult{Passed: true, ExecutedTests: 1}
				}
				return RunResult{ExecutedTests: 1, Failure: "waiting for locator('#old')"}
			}}, func(source, _ string) (string, bool) { return strings.Replace(source, "#old", "#new", 1), tc.tier1 })
			if err != nil || provider.calls != 0 || (tc.passed && result.State != "passed") || (tc.tier1 && result.State != "verified") {
				t.Fatalf("result=%+v calls=%d err=%v", result, provider.calls, err)
			}
		})
	}
}

func TestTier2RejectsOverEightKiBWithoutProviderOrMutation(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "large.spec.ts")
	original := paddedLocatorSpec(maxTier2SourceBytes + 1)
	if err := os.WriteFile(spec, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	provider := &fakeProvider{}
	result, err := Heal(context.Background(), SessionOptions{Spec: spec, Framework: "playwright", Provider: provider, Run: func(context.Context, string, string) RunResult {
		return RunResult{ExecutedTests: 1, Failure: "waiting for locator('#old')"}
	}}, func(string, string) (string, bool) { return "", false })
	stored, _ := os.ReadFile(spec)
	if err != nil || result.State != "unverified" || provider.calls != 0 || string(stored) != original {
		t.Fatalf("result=%+v calls=%d err=%v", result, provider.calls, err)
	}
}

func TestPromptBoundsTotalContextAndSelectsOuterFence(t *testing.T) {
	source := "const fence = ` ``` `;\n```\n" + strings.Repeat("x", maxTier2SourceBytes-30)
	prompt := PromptWithContext("playwright", source, strings.Repeat("e", maxFailureBytes+1), strings.Repeat("p", maxPageHTMLBytes+1), []string{strings.Repeat("c", maxConsoleEntryBytes+1), strings.Repeat("d", maxConsoleEntryBytes+1), strings.Repeat("z", maxConsoleEntryBytes+1), strings.Repeat("q", maxConsoleEntryBytes+1), "overflow"})
	if len(prompt) > maxTier2PromptBytes || !strings.Contains(prompt, "````javascript\n"+source+"\n````") || strings.Contains(prompt, "overflow") {
		t.Fatalf("prompt bytes=%d contract broken", len(prompt))
	}
}

func TestParseCandidateUsesStructuralMatchingFences(t *testing.T) {
	original := "const text = 'CODE:';\n// inline ``` remains text\n```\nold\n```\n"
	candidate := strings.Replace(original, "old", "new", 1)
	response := "````typescript\r\n" + strings.ReplaceAll(candidate, "\n", "\r\n") + "````\r\n"
	parsed, err := ParseCandidate(response, original)
	if err != nil || parsed != strings.ReplaceAll(candidate, "\n", "\r\n") {
		t.Fatalf("parsed=%q err=%v", parsed, err)
	}
	if _, err := ParseCandidate("```js\nnew\n```\n```js\nother\n```", original); err == nil {
		t.Fatal("accepted multiple files")
	}
	if _, err := ParseCandidate("```js\nnew\n````", original); err == nil {
		t.Fatal("accepted mismatched fence")
	}
}

func TestTier2AcceptsExactEightKiBCompleteCandidate(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "exact.spec.ts")
	original := paddedLocatorSpec(maxTier2SourceBytes)
	candidate := strings.Replace(original, "#old", "#new", 1)
	if err := os.WriteFile(spec, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	provider := &fakeProvider{responses: []string{"```typescript\n" + candidate + "```"}}
	result, err := Heal(context.Background(), SessionOptions{Spec: spec, Framework: "playwright", Provider: provider, Run: func(_ context.Context, path, label string) RunResult {
		if label == "original" {
			return RunResult{ExecutedTests: 1, Failure: "waiting for locator('#old')"}
		}
		bytes, _ := os.ReadFile(path)
		if string(bytes) == candidate {
			return RunResult{Passed: true, ExecutedTests: 1}
		}
		return RunResult{ExecutedTests: 1, Failure: "waiting for locator('#old')"}
	}}, func(string, string) (string, bool) { return "", false })
	if err != nil || result.State != "verified" || provider.calls != 1 {
		t.Fatalf("result=%+v calls=%d err=%v", result, provider.calls, err)
	}
}

func TestHealWithoutProviderIsOfflineTier1Only(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "login.spec.ts")
	original := "import { test, expect } from '@playwright/test';\ntest('save', async ({ page }) => {\n  await page.locator('#old').click();\n  await expect(page.locator('#other')).toBeVisible();\n});\n"
	if err := os.WriteFile(spec, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	candidate := strings.Replace(original, "#old", "#new", 1)
	run := func(_ context.Context, path, label string) RunResult {
		data, _ := os.ReadFile(path)
		if string(data) == candidate {
			return RunResult{Passed: true, ExecutedTests: 1}
		}
		return RunResult{ExecutedTests: 1, Failure: "waiting for locator('#old')"}
	}
	// A verified Tier 1 candidate needs no provider.
	result, err := Heal(context.Background(), SessionOptions{Spec: spec, Framework: "playwright", Run: run}, func(string, string) (string, bool) { return candidate, true })
	if err != nil || result.State != "verified" || result.SavedPath != spec+".healed" {
		t.Fatalf("offline Tier 1: result=%+v err=%v", result, err)
	}
	// Without a Tier 1 candidate it stops, unverified, instead of calling Tier 2.
	result, err = Heal(context.Background(), SessionOptions{Spec: spec, Framework: "playwright", Run: run}, func(string, string) (string, bool) { return "", false })
	if err != nil || result.State != "unverified" || !strings.Contains(result.Reason, "no Tier 2 provider") || result.Applied {
		t.Fatalf("no provider: result=%+v err=%v", result, err)
	}
	if source, _ := os.ReadFile(spec); string(source) != original {
		t.Fatal("source changed")
	}
}

func TestHealRecordsWhyTheProviderCallFailed(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "login.spec.ts")
	if err := os.WriteFile(spec, []byte("test('x', async ({ page }) => {\n  await page.locator('#old').click();\n});\n"), 0600); err != nil {
		t.Fatal(err)
	}
	provider := &fakeProvider{err: &CallError{Provider: "claude", ExitCode: 1, Diagnostic: "Not logged in · Please run /login"}}
	result, err := Heal(context.Background(), SessionOptions{Spec: spec, Provider: provider, Run: func(context.Context, string, string) RunResult {
		return RunResult{ExecutedTests: 1, Failure: "waiting for locator('#old')"}
	}}, func(string, string) (string, bool) { return "", false })
	if err == nil || result.State != "provider_error" || result.ProviderDiagnostic != "claude provider failed (exit 1): Not logged in · Please run /login" || !strings.Contains(result.Reason, "Not logged in") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestHealDoesNotAskProviderForAnUneditableSourceShape(t *testing.T) {
	for name, tc := range map[string]struct{ source, reason string }{
		"page action shorthand": {"test('x', async ({ page }) => {\n  await page.fill('#old', 'a');\n});\n", "no page.locator('#old') call"},
		"same line as closing":  {"test('x', async ({ page }) => { await page.locator('#old').click(); });\n", "not a direct `await page.locator('#old')"},
		"two locator calls":     {"test('x', async ({ page }) => {\n  await page.locator('#old').click();\n  await page.locator('#old').fill('a');\n});\n", "more than once"},
	} {
		t.Run(name, func(t *testing.T) {
			spec := filepath.Join(t.TempDir(), "login.spec.ts")
			if err := os.WriteFile(spec, []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			provider := &fakeProvider{responses: []string{"```typescript\n" + strings.ReplaceAll(tc.source, "#old", "#new") + "```"}}
			result, err := Heal(context.Background(), SessionOptions{Spec: spec, Framework: "playwright", Provider: provider, Run: func(context.Context, string, string) RunResult {
				return RunResult{ExecutedTests: 1, Failure: "TimeoutError: waiting for locator('#old')"}
			}}, func(string, string) (string, bool) { return "", false })
			if err != nil || provider.calls != 0 || result.State != "unverified" || !strings.Contains(result.Reason, "Tier 2 was not asked") || !strings.Contains(result.Reason, tc.reason) {
				t.Fatalf("calls=%d result=%+v err=%v", provider.calls, result, err)
			}
		})
	}
}

func TestHealRepairsARenamedGetByRoleName(t *testing.T) {
	spec := filepath.Join(t.TempDir(), "login.spec.ts")
	original := "test('login', async ({ page }) => {\n  await page.getByRole('button', { name: 'Anmelden' }).click();\n  await expect(page.getByRole('status')).toHaveText('Willkommen');\n});\n"
	if err := os.WriteFile(spec, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	candidate := strings.Replace(original, "'Anmelden'", "'Einloggen'", 1)
	provider := &fakeProvider{responses: []string{"```typescript\n" + candidate + "```"}}
	run := func(_ context.Context, path, _ string) RunResult {
		data, _ := os.ReadFile(path)
		if string(data) == candidate {
			return RunResult{Passed: true, ExecutedTests: 1}
		}
		return RunResult{ExecutedTests: 1, Failure: "TimeoutError: locator.click: Timeout 1500ms exceeded.\nCall log:\n\x1b[2m  - waiting for getByRole('button', { name: 'Anmelden' })\x1b[22m"}
	}
	result, err := Heal(context.Background(), SessionOptions{Spec: spec, Framework: "playwright", Provider: provider, Run: run}, func(string, string) (string, bool) { return "", false })
	saved, _ := os.ReadFile(spec + ".healed")
	if err != nil || result.State != "verified" || provider.calls != 1 || string(saved) != candidate {
		t.Fatalf("result=%+v calls=%d err=%v", result, provider.calls, err)
	}
}
