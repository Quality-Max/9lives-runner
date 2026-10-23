package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/healing/tier2"
)

// healPassingProject is a project whose Playwright stand-in reports one
// passing test, so a heal session ends at the original run.
func healPassingProject(t *testing.T) string {
	t.Helper()
	passed := `{"stats":{"duration":1},"suites":[{"specs":[{"tests":[{"status":"expected","results":[{"status":"passed"}]}]}]}]}`
	root := writeProject(t, "printf '%s' '"+passed+"' > \"$PLAYWRIGHT_JSON_OUTPUT_FILE\"\nexit 0\n",
		map[string]string{"login.spec.ts": "import { test } from '@playwright/test';\ntest('ok', async ({ page }) => {\n  await page.locator('#ok').click();\n});\n"})
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	return root
}

// A session that ended at the original run says Tier 1 and verification
// were not attempted instead of reporting them as failed zero-test runs.
func TestHealJSONReportsUnattemptedStagesAsNull(t *testing.T) {
	healPassingProject(t)
	var out, errOut bytes.Buffer
	code := run([]string{"heal", "login.spec.ts", "--provider", "none", "--format", "json", "--receipt-dir", ".9lives/healing-receipts"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var session map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &session); err != nil {
		t.Fatalf("stdout is not one JSON session: %v\n%s", err, out.String())
	}
	if string(session["state"]) != `"passed"` {
		t.Fatalf("state = %s", session["state"])
	}
	for _, stage := range []string{"tier1", "verified"} {
		if raw, ok := session[stage]; !ok || string(raw) != "null" {
			t.Errorf("%s = %s, want null", stage, raw)
		}
	}
	var original tier2.RunResult
	if err := json.Unmarshal(session["original"], &original); err != nil || !original.Passed || original.ExecutedTests != 1 {
		t.Fatalf("original = %s (%v)", session["original"], err)
	}
}

// --format text reports the state and the runs as lines, without JSON, and
// is the same session as --format json.
func TestHealTextFormatPrintsStateWithoutJSON(t *testing.T) {
	healPassingProject(t)
	var out, errOut bytes.Buffer
	code := run([]string{"heal", "login.spec.ts", "--provider", "none", "--format", "text"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	text := out.String()
	for _, want := range []string{"state: passed\n", "original: passed (1 tests)\n", "tier1: not attempted\n", "verified: not attempted\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("text output lacks %q:\n%s", want, text)
		}
	}
	if strings.ContainsAny(text, "{}") {
		t.Fatalf("text output contains JSON:\n%s", text)
	}
}

// The text report shows where the verified candidate went and its diff
// against the healed source.
func TestHealTextFormatShowsTheCandidateDiff(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "login.spec.ts")
	original := "import { test } from '@playwright/test';\ntest('save', async ({ page }) => {\n  await page.locator('#old').click();\n});\n"
	candidate := strings.Replace(original, "#old", "#new", 1)
	if err := os.WriteFile(spec+".healed", []byte(candidate), 0600); err != nil {
		t.Fatal(err)
	}
	failed := tier2.RunResult{ExecutedTests: 1, Failure: "TimeoutError: locator.click: Timeout 1000ms exceeded.\nCall log:\n  - waiting for locator('#old')"}
	session := tier2.Session{State: "verified", Original: failed, Tier1: &failed, Verified: &tier2.RunResult{Passed: true, ExecutedTests: 1}, SavedPath: spec + ".healed", Provider: "scripted", ProviderCalls: 1}
	var out bytes.Buffer
	if err := printHealText(&out, spec, []byte(original), session); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"state: verified\n", "provider: scripted\n", "provider calls: 1\n",
		"tier1: failed (1 tests): TimeoutError: locator.click: Timeout 1000ms exceeded.\n", "verified: passed (1 tests)\n",
		"saved: " + spec + ".healed\n",
		"-  await page.locator('#old').click();\n+  await page.locator('#new').click();\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text output lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Call log") || strings.ContainsAny(text, "{}") {
		t.Fatalf("text output carries the whole failure or JSON:\n%s", text)
	}
}

// Unknown options name themselves; only a known Python-only option points
// at the Python CLI, and a bad --format is its own error.
func TestHealOptionErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		args          []string
		want, without string
	}{
		"unknown option": {[]string{"heal", "login.spec.ts", "--frobnicate"}, "unknown heal option --frobnicate", "Python"},
		"python option":  {[]string{"heal", "login.spec.ts", "--framework", "cypress"}, "--framework is a Python-only heal option", "unknown heal option"},
		"bad format":     {[]string{"heal", "login.spec.ts", "--format", "xml"}, "--format must be text or json", "unknown heal option"},
	} {
		var out, errOut bytes.Buffer
		code := run(tc.args, &out, &errOut)
		if code != 2 || out.Len() != 0 {
			t.Fatalf("%s: exit %d stdout %q stderr %q", name, code, out.String(), errOut.String())
		}
		if !strings.Contains(errOut.String(), tc.want) || strings.Contains(errOut.String(), tc.without) {
			t.Errorf("%s: stderr %q, want %q and not %q", name, errOut.String(), tc.want, tc.without)
		}
	}
}

// failingWriter rejects every write, as a full disk behind a redirected
// stdout would.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("no space left on device") }

// A text report that cannot be delivered is an error, as an unwritable JSON
// report already was; a session must not exit 0 with nothing shown.
func TestHealTextReportReportsAnUndeliveredWrite(t *testing.T) {
	if err := printHealText(failingWriter{}, "tests/a.spec.ts", nil, tier2.Session{State: "needs_human", Reason: "no candidate"}); err == nil {
		t.Fatal("undelivered text report was not reported")
	}
}
