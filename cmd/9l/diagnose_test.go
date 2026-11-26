package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/jev"
	"github.com/Quality-Max/9lives-runner/internal/jevassist"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

type scriptedDiagnosis struct {
	family string
	calls  int
	got    jev.Observation
}

func (s *scriptedDiagnosis) Decide(_ context.Context, b jev.Binding, o jev.Observation, c jev.Contract) (jev.Result, error) {
	s.calls++
	s.got = o
	one := func(v float64) *float64 { return &v }
	probabilities := map[string]float64{}
	for id := range c.Questions["family"].Criteria {
		probabilities[id] = .02
	}
	probabilities[s.family] = 1 - .02*float64(len(probabilities)-1)
	answers := map[string]jev.Answer{"family": {Type: "choice", Choice: s.family, Confidence: one(.85), Probabilities: probabilities}}
	for id := range c.Questions {
		if id != "family" {
			answers[id] = jev.Answer{Type: "noul", Noul: one(.05)}
		}
	}
	if _, ok := c.Questions["targetVisible"]; ok {
		answers["targetVisible"] = jev.Answer{Type: "noul", Noul: one(.95)}
	}
	return jev.Result{Record: jev.Record{Version: jev.Version, Binding: b, Status: "answered", RequestCount: 1}, Answers: answers}, nil
}

func useScriptedDiagnosis(t *testing.T, family string) *scriptedDiagnosis {
	t.Helper()
	fake := &scriptedDiagnosis{family: family}
	previous := newDiagnosisDecider
	newDiagnosisDecider = func(context.Context, jev.Config, runner.AttemptIdentity, string, string) (jevassist.Decider, func(), error) {
		return fake, func() {}, nil
	}
	t.Cleanup(func() { newDiagnosisDecider = previous })
	return fake
}

func TestDiagnoseRefusesUnsafeOptionsBeforeRunning(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "a.spec.ts")
	writeTestFile(t, spec, "// never runs\n")
	t.Setenv(jev.CredentialName, "")
	for _, args := range [][]string{
		{spec, "--jev-model", "jev-1.14.0"},
		{spec, "--jev", "--jev-images", "masked"},
		{spec, "--jev"}, // no credential: refuse before running the test
		{spec, "extra.spec.ts"},
		{spec, "--format", "yaml"},
	} {
		receipts := filepath.Join(dir, "receipts")
		var out, errOut bytes.Buffer
		if code := run(append(append([]string{"diagnose"}, args...), "--receipt-dir", receipts), &out, &errOut); code != 2 || out.Len() != 0 {
			t.Fatalf("%v: exit %d", args, code)
		}
		if _, err := os.Stat(receipts); !os.IsNotExist(err) {
			t.Fatalf("%v ran the test", args)
		}
	}
}

func TestDiagnoseKeepsTheJevCredentialOutOfTestProcesses(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "a.spec.ts")
	writeTestFile(t, spec, "// never runs\n")
	t.Setenv(jev.CredentialName, "fixture-presence-only")
	for _, names := range []string{jev.CredentialName, "BASE_URL," + strings.ToLower(jev.CredentialName)} {
		receipts := filepath.Join(dir, "receipts")
		var out, errOut bytes.Buffer
		code := run([]string{"diagnose", spec, "--jev", "--pass-env", names, "--receipt-dir", receipts}, &out, &errOut)
		if code != 2 || out.Len() != 0 || !strings.Contains(errOut.String(), "--pass-env") {
			t.Fatalf("--pass-env %s: exit %d, stderr %q", names, code, errOut.String())
		}
		if _, err := os.Stat(receipts); !os.IsNotExist(err) {
			t.Fatalf("--pass-env %s ran the test", names)
		}
	}
	if env := withoutEnv([]string{"PATH=/bin", strings.ToLower(jev.CredentialName) + "=x", jev.CredentialName + "=y", "HOME=/h"}, jev.CredentialName); strings.Join(env, " ") != "PATH=/bin HOME=/h" {
		t.Fatalf("credential left in a child environment: %v", env)
	}
}

const renamedButtonSpec = `import {test} from '@playwright/test';
test('checkout', async ({page}) => {
  await page.setContent('<h1>Your cart</h1><button>Continue to payment</button>');
  await page.getByRole('button', { name: 'Checkout' }).click();
});
`

const wrongPriceSpec = `import {test, expect} from '@playwright/test';
test('total', async ({page}) => {
  await page.setContent('<h1>Your cart</h1><p data-testid="total">$24.00</p><button>Checkout</button>');
  await expect(page.getByTestId('total')).toHaveText('$42.00');
});
`

func diagnosisProject(t *testing.T) string {
	root := browserProject(t)
	writeTestFile(t, filepath.Join(root, "package.json"), `{"devDependencies":{"@playwright/test":"1.61.1"}}`)
	writeTestFile(t, filepath.Join(root, "playwright.config.cjs"), "module.exports = { retries: 0, workers: 1, timeout: 15000, expect: { timeout: 1000 }, use: { browserName: 'chromium', viewport: { width: 1000, height: 700 }, actionTimeout: 1000, screenshot: 'only-on-failure' } };")
	writeTestFile(t, filepath.Join(root, "renamed.spec.ts"), renamedButtonSpec)
	writeTestFile(t, filepath.Join(root, "price.spec.ts"), wrongPriceSpec)
	writeTestFile(t, filepath.Join(root, "pass.spec.ts"), "import {test, expect} from '@playwright/test';\ntest('ok', async ({page}) => { await page.setContent('<p>ok</p>'); await expect(page.getByText('ok')).toBeVisible(); });\n")
	writeTestFile(t, filepath.Join(root, "hang.spec.ts"), "import {test} from '@playwright/test';\ntest('waits', async ({page}) => { await page.setContent('<p>wait</p>'); await new Promise(() => {}); });\n")
	t.Chdir(root)
	return root
}

func diagnoseJSON(t *testing.T, args ...string) (diagnoseReport, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(append(append([]string{"diagnose"}, args...), "--format", "json", "--run-timeout", "40s"), &out, &errOut)
	var report diagnoseReport
	if code != 2 && json.Unmarshal(out.Bytes(), &report) != nil {
		t.Fatalf("invalid report: %s %s", out.String(), errOut.String())
	}
	return report, code
}

func TestDiagnoseRealFailuresKeepTheirResult(t *testing.T) {
	root := diagnosisProject(t)
	t.Setenv(jev.CredentialName, "fixture-presence-only")
	t.Run("renamed button is eligible for repair", func(t *testing.T) {
		fake := useScriptedDiagnosis(t, "locator_drift")
		report, code := diagnoseJSON(t, "renamed.spec.ts", "--jev")
		d := report.Diagnosis
		if code != 1 || report.Outcome != "failed" || d == nil || d.Status != "diagnosed" || d.Disposition != jevassist.EligibleForRepair || fake.calls != 1 {
			t.Fatalf("exit %d, report %+v, diagnosis %+v", code, report, report.Diagnosis)
		}
		if d.Facts.FailedLocator != "getByRole('button', { name: 'Checkout' })" || d.Facts.ScreenshotSHA256 == "" || d.Facts.SnapshotSHA256 == "" {
			t.Fatalf("facts not observed: %+v", d.Facts)
		}
		if !strings.Contains(fake.got.Snapshot, `button "Continue to payment"`) || fake.got.Image != nil {
			t.Fatal("observation missing the snapshot, or sent a screenshot without opt-in")
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(d.Facts.ReceiptPath)))), "diagnosis.json")); err != nil {
			t.Fatal("diagnosis record not saved beside the run")
		}
	})
	t.Run("wrong price stays an application question", func(t *testing.T) {
		useScriptedDiagnosis(t, "locator_drift")
		report, code := diagnoseJSON(t, "price.spec.ts", "--jev", "--jev-images", "synthetic")
		d := report.Diagnosis
		if code != 1 || report.Outcome != "failed" || d == nil || d.Disposition != jevassist.InvestigateApp || d.Reason != "assertion_failed" || d.Facts.FailureClass != runner.FailureAssertion {
			t.Fatalf("exit %d, report %+v, diagnosis %+v", code, report, report.Diagnosis)
		}
	})
	t.Run("without Jev the facts remain", func(t *testing.T) {
		fake := useScriptedDiagnosis(t, "locator_drift")
		report, code := diagnoseJSON(t, "renamed.spec.ts")
		if code != 1 || report.Diagnosis == nil || report.Diagnosis.Status != "disabled" || report.Diagnosis.Facts.DeterministicClass == "" || fake.calls != 0 {
			t.Fatalf("exit %d, report %+v, diagnosis %+v", code, report, report.Diagnosis)
		}
	})
	t.Run("text report separates facts from inference", func(t *testing.T) {
		useScriptedDiagnosis(t, "locator_drift")
		var out, errOut bytes.Buffer
		if code := run([]string{"diagnose", "renamed.spec.ts", "--jev", "--run-timeout", "40s"}, &out, &errOut); code != 1 {
			t.Fatalf("exit %d", code)
		}
		for _, want := range []string{"Disposition: eligible-for-repair", "Observed facts: deterministic class locator_not_found", "Jev inference: locator_drift", "Result unchanged: failed"} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("missing %q in %s", want, out.String())
			}
		}
		if !strings.Contains(errOut.String(), "never changed") {
			t.Fatal("no upload notice before the run")
		}
	})
	t.Run("a run that times out is incomplete, not a failure", func(t *testing.T) {
		fake := useScriptedDiagnosis(t, "locator_drift")
		var out, errOut bytes.Buffer
		code := run([]string{"diagnose", "hang.spec.ts", "--jev", "--run-timeout", "3s"}, &out, &errOut)
		if code != exitIncomplete || fake.calls != 0 || !strings.Contains(out.String(), "did not complete (timed_out); nothing to diagnose") {
			t.Fatalf("exit %d, %d Jev calls, stdout %q", code, fake.calls, out.String())
		}
	})
	t.Run("a passing test is not diagnosed", func(t *testing.T) {
		fake := useScriptedDiagnosis(t, "locator_drift")
		report, code := diagnoseJSON(t, "pass.spec.ts", "--jev")
		if code != 0 || report.Outcome != "passed" || report.Diagnosis != nil || fake.calls != 0 {
			t.Fatalf("exit %d, report %+v, diagnosis %+v", code, report, report.Diagnosis)
		}
	})
	for _, spec := range []string{"renamed.spec.ts", "price.spec.ts"} {
		if raw, _ := os.ReadFile(filepath.Join(root, spec)); !strings.Contains(string(raw), "test(") || (spec == "renamed.spec.ts" && string(raw) != renamedButtonSpec) || (spec == "price.spec.ts" && string(raw) != wrongPriceSpec) {
			t.Fatalf("diagnosis changed %s", spec)
		}
	}
}
