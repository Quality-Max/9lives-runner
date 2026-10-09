package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/prove"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// fakeProveWorker stands in for Playwright and the SDK: it writes the engine
// event stream and the prove channel the way the real fixture does.
type fakeProveWorker struct {
	// asserted paths fail an assertion under any fault; others survive.
	asserted   map[string]bool
	baselineOK bool
	instrument bool
	// corrupt writes a record outside the closed schema; corruptFaults does so
	// only in fault runs.
	corrupt       bool
	corruptFaults bool
	commands      [][]string
}

const fakeOrigin = "http://127.0.0.1:4100"

func (worker *fakeProveWorker) Run(_ context.Context, job runner.Job, _ int) runner.ProcessOutput {
	worker.commands = append(worker.commands, job.Command)
	var fault *prove.Fault
	if raw, ok := job.Env["NINELIVES_PROVE_FAULT"]; ok {
		fault = &prove.Fault{}
		if err := json.Unmarshal([]byte(raw), fault); err != nil {
			return runner.ProcessOutput{Executed: true, ExitCode: 1}
		}
	}
	mode := "observe"
	if fault != nil {
		mode = "fault"
	}
	channel := []string{fmt.Sprintf(`{"type":"hello","protocol":"%s","mode":"%s"}`, prove.Protocol, mode)}
	passed := worker.baselineOK
	if fault == nil {
		for _, path := range []string{"/api/cart", "/api/recommendations"} {
			channel = append(channel,
				fmt.Sprintf(`{"type":"request","method":"GET","origin":"%s","path":"%s","resourceType":"fetch"}`, fakeOrigin, path),
				fmt.Sprintf(`{"type":"response","method":"GET","origin":"%s","path":"%s","status":200,"json":true}`, fakeOrigin, path))
		}
	} else {
		channel = append(channel, fmt.Sprintf(`{"type":"applied","fault":"%s"}`, fault.ID))
		passed = !worker.asserted[fault.Path]
	}
	if worker.corrupt || (worker.corruptFaults && fault != nil) {
		channel = append(channel, `{"type":"request","url":"http://127.0.0.1:4100/api/cart?token=1"}`)
	}
	if worker.instrument {
		_ = os.WriteFile(filepath.Join(job.Env["NINELIVES_PROVE_DIR"], strings.Repeat("c", 32)+".ndjson"), []byte(strings.Join(channel, "\n")+"\n"), 0o600)
	}
	status, outcome, exit := "passed", "expected", 0
	if !passed {
		status, outcome, exit = "failed", "unexpected", 1
	}
	identity := fmt.Sprintf(`"version":"9l.engine/1","runId":"%s","jobId":"%s","attemptId":"%s"`, job.Env["NINELIVES_RUN_ID"], job.Env["NINELIVES_JOB_ID"], job.Env["NINELIVES_ATTEMPT_ID"])
	test := strings.Repeat("a", 64)
	events := []string{
		`"seq":1,"type":"hello","capabilities":["steps","artifact-metadata","terminal-outcomes"],"totalTests":1`,
		`"seq":2,"type":"test_begin","testId":"` + test + `","retry":0`,
		`"seq":3,"type":"step_end","testId":"` + test + `","retry":0,"stepId":"step-1","category":"assertion","status":"` + status + `"`,
		`"seq":4,"type":"test_end","testId":"` + test + `","retry":0,"status":"` + status + `","expectedStatus":"passed","artifacts":[]`,
		`"seq":5,"type":"test_result","testId":"` + test + `","outcome":"` + outcome + `"`,
		`"seq":6,"type":"end","status":"` + status + `"`,
	}
	var stream bytes.Buffer
	for _, event := range events {
		stream.WriteString("{" + identity + "," + event + "}\n")
	}
	_ = os.WriteFile(job.Env["NINELIVES_ENGINE_EVENTS"], stream.Bytes(), 0o600)
	return runner.ProcessOutput{Executed: true, ExitCode: exit}
}

func proveProject(t *testing.T) string {
	t.Helper()
	project := t.TempDir()
	for path, content := range map[string]string{
		"package.json":                                 `{"devDependencies":{"@playwright/test":"1.61.1"}}`,
		"node_modules/.bin/playwright":                 "",
		"node_modules/@9l/playwright/dist/reporter.js": "",
		"tests/shop.spec.ts":                           "",
	} {
		full := filepath.Join(project, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return project
}

func runProve(t *testing.T, worker *fakeProveWorker, args ...string) (int, string, string) {
	t.Helper()
	proveExecutor = worker
	t.Cleanup(func() { proveExecutor = nil })
	var stdout, stderr bytes.Buffer
	code := run(append([]string{"prove"}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestProveClassifiesEachFaultAndPersistsAReportWithoutURLs(t *testing.T) {
	project := proveProject(t)
	receipts := filepath.Join(t.TempDir(), "receipts")
	worker := &fakeProveWorker{asserted: map[string]bool{"/api/cart": true}, baselineOK: true, instrument: true}
	code, stdout, stderr := runProve(t, worker, filepath.Join(project, "tests/shop.spec.ts"), "--format", "json", "--receipt-dir", receipts, "--paths")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	var report prove.Report
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	results := []string{}
	for _, fault := range report.Faults {
		results = append(results, fault.Request+":"+fault.Kind+":"+fault.Result)
	}
	want := "req-1:abort:caught req-1:http-500:caught req-1:empty-json:caught req-2:abort:survived req-2:http-500:survived req-2:empty-json:survived"
	if strings.Join(results, " ") != want || !report.Complete || report.Summary.Caught != 3 || report.Summary.Survived != 3 {
		t.Fatalf("results %q\nreport %+v", strings.Join(results, " "), report)
	}
	if report.Requests[0].URL != fakeOrigin+"/api/cart" {
		t.Fatalf("--paths output lacks the request URL: %+v", report.Requests[0])
	}
	// Seven ordinary runs: one baseline and one per fault, each without retries.
	if len(worker.commands) != 7 {
		t.Fatalf("runs=%d", len(worker.commands))
	}
	for _, command := range worker.commands {
		if !slices.Contains(command, "--retries=0") {
			t.Fatalf("prove run allows Playwright retries: %q", command)
		}
	}
	saved, err := os.ReadFile(filepath.Join(receipts, "proofs", report.Baseline.RunID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(saved, []byte("/api/")) || bytes.Contains(saved, []byte("127.0.0.1")) {
		t.Fatal("persisted proof report contains request URLs")
	}
}

func TestProveStopsAtTheFaultBudget(t *testing.T) {
	project := proveProject(t)
	worker := &fakeProveWorker{asserted: map[string]bool{}, baselineOK: true, instrument: true}
	code, stdout, stderr := runProve(t, worker, filepath.Join(project, "tests/shop.spec.ts"), "--max-faults", "2", "--receipt-dir", t.TempDir())
	if code != 0 || !strings.Contains(stdout, "2 survived, 0 inconclusive, 4 not run") || len(worker.commands) != 3 {
		t.Fatalf("code=%d runs=%d stdout=%s stderr=%s", code, len(worker.commands), stdout, stderr)
	}
	if strings.Contains(stdout, "/api/") {
		t.Fatal("text output shows URLs without --paths")
	}
}

func TestProveRefusesWithoutAGreenInstrumentedBaseline(t *testing.T) {
	project := proveProject(t)
	spec := filepath.Join(project, "tests/shop.spec.ts")
	worker := &fakeProveWorker{baselineOK: false, instrument: true}
	if code, _, stderr := runProve(t, worker, spec, "--receipt-dir", t.TempDir()); code != 1 || !strings.Contains(stderr, "passing, complete baseline") || len(worker.commands) != 1 {
		t.Fatalf("failing baseline: code=%d stderr=%s", code, stderr)
	}
	worker = &fakeProveWorker{baselineOK: true}
	if code, _, stderr := runProve(t, worker, spec, "--receipt-dir", t.TempDir()); code != 1 || !strings.Contains(stderr, "no browser context was instrumented") || len(worker.commands) != 1 {
		t.Fatalf("uninstrumented baseline: code=%d stderr=%s", code, stderr)
	}
	worker = &fakeProveWorker{baselineOK: true, instrument: true, corrupt: true}
	if code, _, stderr := runProve(t, worker, spec, "--receipt-dir", t.TempDir()); code != 1 || !strings.Contains(stderr, "prove records failed validation") || len(worker.commands) != 1 {
		t.Fatalf("invalid prove records: code=%d stderr=%s", code, stderr)
	}
}

func TestProveRejectsInvalidUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"a.spec.ts", "b.spec.ts"}, {"a.spec.ts", "--format", "xml"}, {"a.spec.ts", "--max-faults", "0"}, {"a.spec.ts", "--max-faults", "257"}, {"a.spec.ts", "--pin-skip", "no separator"}} {
		if code, _, _ := runProve(t, &fakeProveWorker{}, args...); code != 2 {
			t.Fatalf("args %q: code=%d", args, code)
		}
	}
}

func TestProveInvalidFaultEvidenceMakesTheProofUnsuccessful(t *testing.T) {
	project := proveProject(t)
	worker := &fakeProveWorker{asserted: map[string]bool{"/api/cart": true}, baselineOK: true, instrument: true, corruptFaults: true}
	code, stdout, stderr := runProve(t, worker, filepath.Join(project, "tests/shop.spec.ts"), "--format", "json", "--receipt-dir", t.TempDir())
	var report prove.Report
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("%v: %s", err, stderr)
	}
	if code != 1 || report.Complete || !report.InvalidEvidence || report.Summary.Inconclusive != len(report.Faults) {
		t.Fatalf("code=%d complete=%v invalid=%v summary=%+v", code, report.Complete, report.InvalidEvidence, report.Summary)
	}
}

func TestProveSeparatesUsageFromOperationalFailures(t *testing.T) {
	project := proveProject(t)
	worker := &fakeProveWorker{baselineOK: true, instrument: true}
	if code, _, stderr := runProve(t, worker, filepath.Join(project, "tests/missing.spec.ts"), "--receipt-dir", t.TempDir()); code != 2 {
		t.Fatalf("missing spec: code=%d stderr=%s", code, stderr)
	}
	// A receipt directory that is a file fails execution, not the command line.
	blocked := filepath.Join(t.TempDir(), "receipts")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runProve(t, worker, filepath.Join(project, "tests/shop.spec.ts"), "--receipt-dir", blocked); code != 1 {
		t.Fatalf("operational failure: code=%d stderr=%s", code, stderr)
	}
}
