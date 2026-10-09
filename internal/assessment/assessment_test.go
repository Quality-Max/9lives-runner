package assessment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const contractJSON = `{"version":1,"requirements":[{"id":"checkout-order","reference":"requirements/checkout","revision":"1","expectedOutcomes":[{"id":"order-count","description":"Exactly one order"},{"id":"order-items","description":"Selected items"}]},{"id":"confirmation","reference":"requirements/confirmation","revision":"1","expectedOutcomes":[{"id":"confirmation","description":"Confirmation is displayed"}]}]}`

func TestRequirementChangesBannerAssessment(t *testing.T) {
	c, err := ParseContract([]byte(contractJSON))
	if err != nil {
		t.Fatal(err)
	}
	fact := Fact{Location: Location{1, 1}, Requirements: []string{"checkout-order"}, Assertions: []Assertion{{Location: Location{2, 1}, Outcomes: []string{"confirmation"}}}}
	report := Build([]byte("source"), []byte(contractJSON), c, Facts{Compiler: "5.9.3", Tests: []Fact{fact}})
	if report.Tests[0].Dimensions["intentAlignment"] != "concern" || len(report.Tests[0].Findings) != 2 {
		t.Fatalf("missing requirement gaps: %+v", report.Tests[0])
	}
	for _, f := range report.Tests[0].Findings {
		if f.Classification != "suspected" {
			t.Fatal("static gap claimed as proof")
		}
	}
	fact.Requirements = []string{"confirmation"}
	report = Build([]byte("source"), []byte(contractJSON), c, Facts{Compiler: "5.9.3", Tests: []Fact{fact}})
	if len(report.Tests[0].Findings) != 0 || report.Tests[0].Dimensions["purpose"] != "supported" || report.Tests[0].Dimensions["assertionAdequacy"] != "unknown" || report.Tests[0].Dimensions["intentAlignment"] != "unknown" {
		t.Fatal("declared mapping promoted to proof")
	}
	if report.Execution != "not_run" || report.Tests[0].Dimensions["runtimeEvidence"] != "unknown" {
		t.Fatal("invented execution")
	}
}

func TestOutcomesMappedBySiblingTestsCoverTheRequirement(t *testing.T) {
	c, err := ParseContract([]byte(contractJSON))
	if err != nil {
		t.Fatal(err)
	}
	// Two tests share the requirement; each maps one of its outcomes.
	count := Fact{Location: Location{1, 1}, Requirements: []string{"checkout-order"}, Assertions: []Assertion{{Location: Location{2, 1}, Outcomes: []string{"order-count"}}}}
	items := Fact{Location: Location{4, 1}, Requirements: []string{"checkout-order"}, Assertions: []Assertion{{Location: Location{5, 1}, Outcomes: []string{"order-items"}}}}
	report := Build([]byte("source"), []byte(contractJSON), c, Facts{Compiler: "5.9.3", Tests: []Fact{count, items}})
	for _, test := range report.Tests {
		if len(test.Findings) != 0 || test.Dimensions["intentAlignment"] != "unknown" {
			t.Fatalf("sibling mapping not credited: %+v", test)
		}
	}
	// The second test now maps none of the requirement's outcomes, so it is
	// reported for every outcome; the first is reported only for the outcome
	// mapped nowhere in the file. Each requirement/outcome pair counts once.
	items.Assertions[0].Outcomes = []string{"confirmation"}
	report = Build([]byte("source"), []byte(contractJSON), c, Facts{Compiler: "5.9.3", Tests: []Fact{count, items}})
	var got []string
	for _, test := range report.Tests {
		if test.Dimensions["intentAlignment"] != "concern" {
			t.Fatalf("unmapped outcome not reported: %+v", test)
		}
		for _, f := range test.Findings {
			got = append(got, f.Rule+":"+f.Outcome)
		}
	}
	if strings.Join(got, " ") != "unmapped-outcome:order-items unmapped-outcome:order-count unmapped-outcome:order-items" {
		t.Fatalf("unexpected mapping findings %v", got)
	}
	if counts, total := CountFindings(report); total != 2 || counts["unmapped-outcome"] != 2 {
		t.Fatalf("two unmapped outcomes counted %d times", total)
	}
}

func requireNode(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("NINELIVES_REQUIRE_ASSESSMENT") == "1" {
			t.Fatal("CI requires Node for assessment qualification")
		}
		t.Skip("requires Node")
	}
	return node
}

func TestBundledParserIgnoresConsumerDependenciesAndHooks(t *testing.T) {
	requireNode(t)
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("TMPDIR", dir)
	// These are controlled fixtures, not real credentials or user configuration.
	poison := `require('node:fs').writeFileSync('executed', 'unexpected'); throw Error('must not load consumer code');`
	module := filepath.Join(dir, "node_modules", "typescript")
	if err := os.MkdirAll(module, 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		filepath.Join(module, "package.json"): `{"version":"7.0.2","main":"index.js"}`,
		filepath.Join(module, "index.js"):     poison,
		filepath.Join(dir, "hook.cjs"):        poison,
	} {
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("NODE_OPTIONS", "--require="+filepath.Join(dir, "hook.cjs"))
	t.Setenv("NODE_PATH", filepath.Join(dir, "node_modules"))
	source := []byte("import {test,expect} from '@playwright/test'; import './missing'; throw Error('do not execute');\n" +
		"for (const name of ['one','two']) { test(`case ${name}`, () => { expect(1).toBe(1); }); }")
	report, err := Assess(context.Background(), source, []byte(contractJSON), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Version != 3 || report.Policy != Policy || report.Compiler != parserVersion || len(report.Tests) != 1 || report.Execution != "not_run" || report.Completeness != "partial" {
		t.Fatal("bundled parser qualification failed")
	}
	if len(report.Tests[0].Findings) != 1 || report.Tests[0].Findings[0].Code != "declaration-generation" || report.Tests[0].Dimensions["assertionAdequacy"] != "unknown" {
		t.Fatal("parameterized callback lost its assertion or invented coverage")
	}
	if _, err := os.Stat(filepath.Join(dir, "executed")); !os.IsNotExist(err) {
		t.Fatal("consumer code executed")
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, "9lives-assessment-parser-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatal("private parser directory leaked")
	}
}

func TestUnsupportedAnalysisDoesNotInventMissingAssertions(t *testing.T) {
	c, _ := ParseContract([]byte(contractJSON))
	for code := range analysisLimits {
		fact := Fact{Location: Location{1, 1}, Unsupported: true, Limits: []AnalysisLimit{{Location: Location{1, 1}, Code: code}}}
		report := Build([]byte("source"), []byte(contractJSON), c, Facts{Tests: []Fact{fact}})
		if len(report.Tests[0].Findings) != 1 || report.Tests[0].Findings[0].Code != code || report.Tests[0].Dimensions["assertionAdequacy"] != "unknown" {
			t.Fatal("unsupported syntax was labeled assertion-free")
		}
	}
}

func TestAssessmentSafeDiagnostics(t *testing.T) {
	requireNode(t)
	for _, test := range []struct{ name, source, code string }{
		{"syntax", "import {test} from '@playwright/test'; test(", "syntax"},
		{"annotation", "import {test} from '@playwright/test';\n// @9l-requirement invalid/identifier\ntest('x', () => {});", "annotation"},
		{"limit", "import {test} from '@playwright/test';" + strings.Repeat("test('x', () => {});", 257), "limit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Assess(context.Background(), []byte(test.source), []byte(contractJSON), Options{})
			assertDiagnostic(t, err, test.code)
		})
	}
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := Assess(ctx, []byte("const a = 1;"), []byte(contractJSON), Options{})
		assertDiagnostic(t, err, "cancelled")
	})
	t.Run("missing node", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := Assess(context.Background(), []byte("const a = 1;"), []byte(contractJSON), Options{})
		assertDiagnostic(t, err, "node-unavailable")
	})
}

func assertDiagnostic(t *testing.T, err error, code string) {
	t.Helper()
	var diagnostic *Error
	if !errors.As(err, &diagnostic) || diagnostic.Code != code || err.Error() != "assessment ["+code+"]: "+diagnostics[code] {
		t.Fatalf("unexpected sanitized diagnostic; wanted %s", code)
	}
}

// generous is the budget for helper runs whose outcome does not depend on
// time: Node startup under the race detector on a loaded runner has exceeded
// one second, which turned an expected diagnostic into a timeout.
const generous = 10 * time.Second

func TestHelperFailureModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture; Windows process ownership is tested separately")
	}
	node := requireNode(t)
	parser, cleanup, err := materializeParser()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	c, _ := ParseContract([]byte(contractJSON))
	t.Run("missing parser", func(t *testing.T) {
		_, err := assessWithHelper(context.Background(), []byte("const a=1;"), []byte(contractJSON), c, Options{}, filepath.Join(t.TempDir(), "missing.cjs"), generous)
		assertDiagnostic(t, err, "parser-unavailable")
	})
	for _, test := range []struct {
		name, program, code string
		timeout             time.Duration
	}{
		{"timeout", "setInterval(() => {}, 1000)", "timeout", 200 * time.Millisecond},
		{"overflow", "process.stdout.write('x'.repeat(2*1024*1024))", "output-limit", generous},
		{"malformed", "process.stdout.write('{}')", "invalid-evidence", generous},
		{"failed", "process.stderr.write('private fixture diagnostic'); process.exit(2)", "helper-failed", generous},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			program := filepath.Join(dir, "fake.cjs")
			if err := os.WriteFile(program, []byte(test.program), 0600); err != nil {
				t.Fatal(err)
			}
			// Trusted test paths, quoted for /bin/sh; no environment values or
			// user-provided text enter this wrapper.
			quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
			wrapper := "#!/bin/sh\nexec " + quote(node) + " " + quote(program) + "\n"
			if err := os.WriteFile(filepath.Join(dir, "node"), []byte(wrapper), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			_, err := assessWithHelper(context.Background(), []byte("const a=1;"), []byte(contractJSON), c, Options{}, parser, test.timeout)
			assertDiagnostic(t, err, test.code)
		})
	}
}

func TestAnalyzerOutputLimitCannotBeBypassedByCopy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := &boundedOutput{cancel: cancel}
	if _, err := io.Copy(output, strings.NewReader(strings.Repeat("x", MaxSource+1))); err == nil || !output.overflow || ctx.Err() == nil || len(output.Bytes()) > MaxSource {
		t.Fatal("helper output escaped its bound or cancellation")
	}
}

func TestMissingIntentAndSourceProvenance(t *testing.T) {
	c, _ := ParseContract([]byte(contractJSON))
	a := Build([]byte("a"), []byte(contractJSON), c, Facts{Tests: []Fact{{Location: Location{1, 1}}}})
	b := Build([]byte("b"), []byte(contractJSON), c, Facts{})
	if a.SourceSHA256 == b.SourceSHA256 || a.Tests[0].Dimensions["purpose"] != "unknown" || a.Tests[0].Findings[0].Classification != "suspected" {
		t.Fatal("missing intent or provenance lost")
	}
}

func TestInvalidContracts(t *testing.T) {
	for _, raw := range []string{
		strings.Replace(contractJSON, `"version":1`, `"version":1,"version":1`, 1),
		contractJSON + ` {}`, strings.Replace(contractJSON, `"version":1`, `"version":2`, 1),
		strings.Replace(contractJSON, `"revision":"1"`, `"revision":null`, 1),
		strings.Replace(contractJSON, `"order-items"`, `"order-count"`, 1),
		strings.Replace(contractJSON, `"id":"confirmation","description"`, `"id":"order-count","description"`, 1),
		strings.Replace(contractJSON, `"reference":`, `"extra":`, 1),
		`{"version":1,"requirements":[]}`,
	} {
		if _, err := ParseContract([]byte(raw)); err == nil {
			t.Fatal("accepted malformed contract")
		}
	}
}

func TestDisabledTestIsAnEngineeringConcernWithoutRuntimeProof(t *testing.T) {
	c, _ := ParseContract([]byte(contractJSON))
	disabled := true
	fact := Fact{Location: Location{1, 1}, Requirements: []string{"confirmation"}, Disabled: &disabled, Assertions: []Assertion{{Location: Location{2, 1}, Outcomes: []string{"confirmation"}}}}
	report := Build([]byte("source"), []byte(contractJSON), c, Facts{Tests: []Fact{fact}})
	test := report.Tests[0]
	if len(test.Findings) != 1 || test.Findings[0].Rule != "disabled-test" || test.Findings[0].Classification != "demonstrated" || test.Dimensions["engineeringQuality"] != "concern" {
		t.Fatalf("disabled test lost: %+v", test)
	}
	if report.Execution != "not_run" || test.Dimensions["runtimeEvidence"] != "unknown" || test.Dimensions["intentAlignment"] != "unknown" {
		t.Fatal("disabled syntax invented behavioral proof")
	}
}

func TestRejectForeignAndIncompleteFacts(t *testing.T) {
	f := Facts{Version: 6, Compiler: "5.9.3", Tests: []Fact{{Location: Location{9, 1}, Requirements: []string{}, Assertions: []Assertion{}, Sleeps: []Sleep{}, ConditionalSkips: []ConditionalSkip{}, Disabled: new(bool), Limits: []AnalysisLimit{}}}}
	if validFacts(f, []byte("x"), false) {
		t.Fatal("foreign location accepted")
	}
	f.Tests[0].Location = Location{1, 1}
	if !validFacts(f, []byte("x"), false) {
		t.Fatal("valid facts rejected")
	}
	for _, limits := range [][]AnalysisLimit{
		nil,
		{{Location: Location{1, 1}, Code: "unknown"}},
		{{Location: Location{9, 1}, Code: "dynamic-callback"}},
		{{Location: Location{1, 1}, Code: "dynamic-callback"}, {Location: Location{1, 1}, Code: "dynamic-callback"}},
	} {
		f.Tests[0].Limits = limits
		f.Tests[0].Unsupported = len(limits) > 0
		if validFacts(f, []byte("x"), false) {
			t.Fatal("invalid limit evidence accepted")
		}
	}
	f.Tests[0].Limits = []AnalysisLimit{}
	f.Tests[0].Unsupported = true
	if validFacts(f, []byte("x"), false) {
		t.Fatal("unsupported flag without reason accepted")
	}
	f.Tests[0].Unsupported = false
	f.Tests[0].Disabled = nil
	if validFacts(f, []byte("x"), false) {
		t.Fatal("missing disabled state accepted")
	}
	f.Tests[0].Disabled = new(bool)
	f.Tests = append(f.Tests, f.Tests[0])
	if validFacts(f, []byte("x"), false) {
		t.Fatal("duplicate test accepted")
	}
	if validFacts(Facts{Version: 6, Compiler: "5.9.3"}, []byte("x"), false) {
		t.Fatal("missing inventory accepted")
	}
}

func TestWithoutContractRunsCodeChecksOnly(t *testing.T) {
	fact := Fact{Location: Location{1, 1}, Requirements: []string{"checkout-order"}, Sleeps: []Sleep{{Location: Location{2, 1}}}}
	report := Build([]byte("source"), nil, Contract{}, Facts{Tests: []Fact{fact}})
	test := report.Tests[0]
	var rules []string
	for _, f := range test.Findings {
		rules = append(rules, f.Rule)
	}
	if strings.Join(rules, ",") != "no-direct-assertion,fixed-wait" {
		t.Fatalf("unexpected findings without a contract: %v", rules)
	}
	if test.Dimensions["purpose"] != "unknown" || report.RequirementsSHA256 != "" || !strings.Contains(strings.Join(report.Limits, "\n"), "No requirement contract was supplied") {
		t.Fatalf("unchecked requirements reported as checked: %+v", report)
	}
}

func TestEveryFindingCarriesACode(t *testing.T) {
	c, _ := ParseContract([]byte(contractJSON))
	disabled := true
	fact := Fact{Location: Location{1, 1}, Requirements: []string{"checkout-order", "missing"}, Sleeps: []Sleep{{Location: Location{2, 1}}}, ConditionalSkips: []ConditionalSkip{{Location: Location{3, 1}}, {Location: Location{3, 5}, Environment: true}}, Disabled: &disabled, Exclusive: true,
		Assertions: []Assertion{{Location: Location{4, 1}, Unawaited: true, Absence: true, AfterWait: true}}, Unsupported: true, Limits: []AnalysisLimit{{Location: Location{5, 1}, Code: "unresolved-helper"}}}
	report := Build([]byte("source"), []byte(contractJSON), c, Facts{Tests: []Fact{fact}})
	seen := map[string]bool{}
	for _, f := range report.Tests[0].Findings {
		want := f.Rule
		if f.Rule == "analysis-limit" {
			want = "unresolved-helper"
		}
		if f.Code != want {
			t.Fatalf("finding %s has code %q", f.Rule, f.Code)
		}
		seen[f.Rule] = true
	}
	for _, rule := range []string{"analysis-limit", "unknown-requirement", "unmapped-outcome", "unawaited-assertion", "fixed-wait", "exclusive-test", "disabled-test", "conditional-skip", "environment-skip", "absence-after-wait"} {
		if !seen[rule] {
			t.Fatalf("fixture did not exercise %s", rule)
		}
	}
}

func TestConditionalSkipIsInformationalAndNotDisabled(t *testing.T) {
	fact := Fact{Location: Location{1, 1}, Disabled: new(bool), Assertions: []Assertion{{Location: Location{2, 1}}}, ConditionalSkips: []ConditionalSkip{{Location: Location{3, 1}}}}
	test := Build([]byte("source"), nil, Contract{}, Facts{Tests: []Fact{fact}}).Tests[0]
	if len(test.Findings) != 1 || test.Findings[0].Rule != "conditional-skip" || test.Findings[0].Classification != "informational" || test.Findings[0].Location != (Location{3, 1}) {
		t.Fatalf("conditional skip misreported: %+v", test.Findings)
	}
	if test.Dimensions["engineeringQuality"] != "unknown" {
		t.Fatal("informational finding raised a concern")
	}
}

func TestTitlesAndConditionalSkipEvidenceAreValidated(t *testing.T) {
	f := Facts{Version: 6, Compiler: "5.9.3", Tests: []Fact{{Location: Location{1, 1}, Title: "checkout", Requirements: []string{}, Assertions: []Assertion{}, Sleeps: []Sleep{}, ConditionalSkips: []ConditionalSkip{}, Disabled: new(bool), Limits: []AnalysisLimit{}}}}
	if validFacts(f, []byte("x"), false) || !validFacts(f, []byte("x"), true) {
		t.Fatal("title accepted without a request or rejected with one")
	}
	f.Tests[0].Title = strings.Repeat("x", 1025)
	if validFacts(f, []byte("x"), true) {
		t.Fatal("oversized title accepted")
	}
	f.Tests[0].Title = ""
	f.Tests[0].ConditionalSkips = []ConditionalSkip{{Location: Location{9, 1}}}
	if validFacts(f, []byte("x"), false) {
		t.Fatal("foreign conditional skip accepted")
	}
	f.Tests[0].ConditionalSkips = []ConditionalSkip{{Location: Location{1, 1}}, {Location: Location{1, 1}, Environment: true}}
	if validFacts(f, []byte("x"), false) {
		t.Fatal("duplicate conditional skip accepted")
	}
	f.Tests[0].ConditionalSkips = nil
	if validFacts(f, []byte("x"), false) {
		t.Fatal("missing conditional skips accepted")
	}
}

func TestSharedConditionalSkipsCountOnceTowardTheLimit(t *testing.T) {
	source := []byte(strings.Repeat("xxxxxxxx\n", 256))
	guards := []ConditionalSkip{{Location: Location{1, 1}}, {Location: Location{2, 1}}, {Location: Location{3, 1}}, {Location: Location{4, 1}}, {Location: Location{5, 1}}}
	f := Facts{Version: 6, Compiler: "5.9.3", Tests: []Fact{}}
	for line := 10; line < 210; line++ {
		assertions := make([]Assertion, 6)
		for i := range assertions {
			assertions[i] = Assertion{Location: Location{line, i + 2}, Outcomes: []string{}}
		}
		f.Tests = append(f.Tests, Fact{Location: Location{line, 1}, Requirements: []string{}, Assertions: assertions, Sleeps: []Sleep{}, ConditionalSkips: guards, Disabled: new(bool), Limits: []AnalysisLimit{}})
	}
	if !validFacts(f, source, false) {
		t.Fatal("shared modifiers counted once per test")
	}
	many := make([]ConditionalSkip, 17)
	for i := range many {
		many[i] = ConditionalSkip{Location: Location{i + 1, 1}}
	}
	f.Tests = f.Tests[:1]
	f.Tests[0].ConditionalSkips = many
	if validFacts(f, source, false) {
		t.Fatal("more than 16 modifiers on one test accepted")
	}
}

func TestAssessSuiteGuardWithoutContract(t *testing.T) {
	requireNode(t)
	source := []byte("import {test,expect} from '@playwright/test';\n" +
		"test.describe('weekday flows', () => {\n" +
		"  test.skip(isWeekend(), 'Only meaningful on business days');\n" +
		"  test('shows today', async ({page}) => { await expect(page.getByRole('heading')).toBeVisible(); });\n" +
		"});\n")
	for _, titles := range []bool{false, true} {
		report, err := Assess(context.Background(), source, nil, Options{Titles: titles})
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Tests) != 1 || report.RequirementsSHA256 != "" {
			t.Fatalf("guard became a test or a contract was invented: %+v", report)
		}
		test := report.Tests[0]
		if len(test.Findings) != 1 || test.Findings[0].Rule != "conditional-skip" || test.Findings[0].Line != 3 {
			t.Fatalf("suite guard misreported: %+v", test.Findings)
		}
		if want := map[bool]string{false: "", true: "shows today"}[titles]; test.Title != want {
			t.Fatalf("title %q with titles=%v", test.Title, titles)
		}
	}
}

func TestEnvironmentSkipIsASuspectedConcern(t *testing.T) {
	fact := Fact{Location: Location{1, 1}, Disabled: new(bool), Assertions: []Assertion{{Location: Location{2, 1}}}, ConditionalSkips: []ConditionalSkip{{Location: Location{3, 1}, Environment: true}}}
	test := Build([]byte("source"), nil, Contract{}, Facts{Tests: []Fact{fact}}).Tests[0]
	if len(test.Findings) != 1 || test.Findings[0].Rule != "environment-skip" || test.Findings[0].Code != "environment-skip" || test.Findings[0].Classification != "suspected" || test.Findings[0].Location != (Location{3, 1}) {
		t.Fatalf("environment skip misreported: %+v", test.Findings)
	}
	if test.Dimensions["engineeringQuality"] != "concern" {
		t.Fatal("environment-dependent skip raised no concern")
	}
}

func TestAbsenceAfterWaitIsASuspectedAdequacyConcern(t *testing.T) {
	fact := Fact{Location: Location{1, 1}, Disabled: new(bool),
		Sleeps: []Sleep{{Location: Location{2, 3}}, {Location: Location{5, 3}}, {Location: Location{8, 3}}, {Location: Location{9, 3}}},
		// The analyzer marks the first assertion after a wait in execution order.
		Assertions: []Assertion{
			{Location: Location{3, 3}, Absence: true, AfterWait: true},
			{Location: Location{6, 3}, AfterWait: true}, // positive completion signal
			{Location: Location{7, 3}, Absence: true},   // not first after a wait
			{Location: Location{10, 3}, Absence: true, AfterWait: true},
		}}
	test := Build([]byte("source"), nil, Contract{}, Facts{Tests: []Fact{fact}}).Tests[0]
	var flagged []Location
	for _, f := range test.Findings {
		if f.Rule == "absence-after-wait" {
			if f.Classification != "suspected" || f.Dimension != "assertionAdequacy" || f.Code != f.Rule {
				t.Fatalf("absence after wait misclassified: %+v", f)
			}
			flagged = append(flagged, f.Location)
		}
	}
	if fmt.Sprint(flagged) != "[{3 3} {10 3}]" || test.Dimensions["assertionAdequacy"] != "concern" {
		t.Fatalf("absence after wait flagged at %v; dimensions %v", flagged, test.Dimensions)
	}
}

func TestAssessFlagsWaitThenAbsence(t *testing.T) {
	requireNode(t)
	source := []byte("import {test,expect} from '@playwright/test';\n" +
		"test('toast disappears', async ({page}) => {\n" +
		"  await page.getByRole('button').click();\n" +
		"  await page.waitForTimeout(2000);\n" +
		"  await expect(page.getByRole('alert')).toBeHidden();\n" +
		"});\n" +
		"test('saved first', async ({page}) => {\n" +
		"  await page.waitForTimeout(2000);\n" +
		"  await expect(page.getByText('Saved')).toBeVisible();\n" +
		"  await expect(page.getByRole('alert')).toBeHidden();\n" +
		"});\n")
	report, err := Assess(context.Background(), source, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var rules [][]string
	for _, test := range report.Tests {
		var r []string
		for _, f := range test.Findings {
			r = append(r, f.Rule)
		}
		rules = append(rules, r)
	}
	if fmt.Sprint(rules) != "[[fixed-wait absence-after-wait] [fixed-wait]]" || report.Policy != "assessment-source-v8" {
		t.Fatalf("unexpected findings %v under %s", rules, report.Policy)
	}
}

func TestAssessResolvesSameFileAndMarkedHelpers(t *testing.T) {
	requireNode(t)
	source := []byte("import {test,expect} from '@playwright/test';\n" +
		"// @9l-assertion-helper\n" +
		"import { expectOrderSaved } from './checks';\n" +
		"import { seed } from './data';\n" +
		"async function fillCheckout(page) { await page.getByRole('button').click(); }\n" +
		"async function closesToast(page) { await page.waitForTimeout(2000); await expect(page.getByRole('alert')).toBeHidden(); }\n" +
		"test('confirmed', async ({page}) => { await fillCheckout(page); await expectOrderSaved(page); });\n" +
		"test('action only', async ({page}) => { await fillCheckout(page); });\n" +
		"test('toast', async ({page}) => { await closesToast(page); });\n" +
		"test('unresolved', async ({page}) => { await seed(page); });\n")
	report, err := Assess(context.Background(), source, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, test := range report.Tests {
		var rules []string
		for _, f := range test.Findings {
			site := ""
			if f.Site != nil {
				site = "via" + fmt.Sprint(f.Site.Line)
			}
			rules = append(rules, FindingRule(f)+"@"+fmt.Sprint(f.Line)+site)
		}
		got = append(got, strings.Join(rules, ","))
	}
	// Helper facts keep the helper's own line and carry the test's call site.
	want := []string{"", "no-direct-assertion@8", "fixed-wait@6via9,absence-after-wait@6via9", "analysis-limit/unresolved-helper@10"}
	if strings.Join(got, " | ") != strings.Join(want, " | ") || report.Policy != "assessment-source-v8" {
		t.Fatalf("helper findings %q under %s", got, report.Policy)
	}
}
