package assessment

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	report, err := Assess(context.Background(), source, []byte(contractJSON))
	if err != nil {
		t.Fatal(err)
	}
	if report.Version != 2 || report.Policy != Policy || report.Compiler != parserVersion || len(report.Tests) != 1 || report.Execution != "not_run" || report.Completeness != "partial" {
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
			_, err := Assess(context.Background(), []byte(test.source), []byte(contractJSON))
			assertDiagnostic(t, err, test.code)
		})
	}
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := Assess(ctx, []byte("const a = 1;"), []byte(contractJSON))
		assertDiagnostic(t, err, "cancelled")
	})
	t.Run("missing node", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := Assess(context.Background(), []byte("const a = 1;"), []byte(contractJSON))
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

func TestHelperFailureModes(t *testing.T) {
	node := requireNode(t)
	parser, cleanup, err := materializeParser()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	c, _ := ParseContract([]byte(contractJSON))
	t.Run("missing parser", func(t *testing.T) {
		_, err := assessWithHelper(context.Background(), []byte("const a=1;"), []byte(contractJSON), c, filepath.Join(t.TempDir(), "missing.cjs"), time.Second)
		assertDiagnostic(t, err, "parser-unavailable")
	})
	for _, test := range []struct {
		name, program, code string
		timeout             time.Duration
	}{
		{"timeout", "setInterval(() => {}, 1000)", "timeout", 200 * time.Millisecond},
		{"overflow", "process.stdout.write('x'.repeat(2*1024*1024))", "output-limit", time.Second},
		{"malformed", "process.stdout.write('{}')", "invalid-evidence", time.Second},
		{"failed", "process.stderr.write('private fixture diagnostic'); process.exit(2)", "helper-failed", time.Second},
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
			_, err := assessWithHelper(context.Background(), []byte("const a=1;"), []byte(contractJSON), c, parser, test.timeout)
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
	f := Facts{Version: 2, Compiler: "5.9.3", Tests: []Fact{{Location: Location{9, 1}, Requirements: []string{}, Assertions: []Assertion{}, Sleeps: []Location{}, Disabled: new(bool), Limits: []AnalysisLimit{}}}}
	if validFacts(f, []byte("x")) {
		t.Fatal("foreign location accepted")
	}
	f.Tests[0].Location = Location{1, 1}
	if !validFacts(f, []byte("x")) {
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
		if validFacts(f, []byte("x")) {
			t.Fatal("invalid limit evidence accepted")
		}
	}
	f.Tests[0].Limits = []AnalysisLimit{}
	f.Tests[0].Unsupported = true
	if validFacts(f, []byte("x")) {
		t.Fatal("unsupported flag without reason accepted")
	}
	f.Tests[0].Unsupported = false
	f.Tests[0].Disabled = nil
	if validFacts(f, []byte("x")) {
		t.Fatal("missing disabled state accepted")
	}
	f.Tests[0].Disabled = new(bool)
	f.Tests = append(f.Tests, f.Tests[0])
	if validFacts(f, []byte("x")) {
		t.Fatal("duplicate test accepted")
	}
	if validFacts(Facts{Version: 2, Compiler: "5.9.3"}, []byte("x")) {
		t.Fatal("missing inventory accepted")
	}
}
