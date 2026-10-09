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
	"time"

	"github.com/Quality-Max/9lives-runner/internal/prove"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// fakeProveWorker stands in for Playwright and the SDK: it writes the engine
// event stream and the prove channel the way the real fixture does. Each
// fake test requests the paths in `requests`; one that asserts fails an
// assertion under any fault on an asserted path.
type fakeTest struct {
	id       string
	requests []string
	asserted map[string]bool
	// retries makes the test fail its first attempt and pass a retry, as
	// test.describe.configure({retries: 1}) would, under any applied fault.
	retries bool
	// unreachable makes an empty-json fault report the upstream as down.
	unreachable bool
}

type fakeProveWorker struct {
	tests      []fakeTest
	baselineOK bool
	instrument bool
	// corrupt writes a record outside the closed schema; corruptFaults does so
	// only in fault runs.
	corrupt       bool
	corruptFaults bool
	// delay makes every run outlast a short --deadline.
	delay    time.Duration
	commands [][]string
}

const fakeOrigin = "http://127.0.0.1:4100"

type executorFunc func(context.Context, runner.Job, int) runner.ProcessOutput

func (run executorFunc) Run(ctx context.Context, job runner.Job, attempt int) runner.ProcessOutput {
	return run(ctx, job, attempt)
}

// shopWorker is the one-test shop: the cart is asserted, recommendations are not.
func shopWorker() *fakeProveWorker {
	return &fakeProveWorker{baselineOK: true, instrument: true, tests: []fakeTest{{id: strings.Repeat("a", 64), requests: []string{"/api/cart", "/api/recommendations"}, asserted: map[string]bool{"/api/cart": true}}}}
}

func (worker *fakeProveWorker) Run(ctx context.Context, job runner.Job, _ int) runner.ProcessOutput {
	worker.commands = append(worker.commands, job.Command)
	if worker.delay > 0 {
		select {
		case <-ctx.Done():
			return runner.ProcessOutput{Executed: true, ExitCode: 1}
		case <-time.After(worker.delay):
		}
	}
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
	identity := fmt.Sprintf(`"version":"9l.engine/1","runId":"%s","jobId":"%s","attemptId":"%s"`, job.Env["NINELIVES_RUN_ID"], job.Env["NINELIVES_JOB_ID"], job.Env["NINELIVES_ATTEMPT_ID"])
	events := []string{fmt.Sprintf(`"seq":1,"type":"hello","capabilities":["steps","artifact-metadata","terminal-outcomes"],"totalTests":%d`, len(worker.tests))}
	seq, step := 1, 0
	emit := func(event string) { seq++; events = append(events, fmt.Sprintf(`"seq":%d,`, seq)+event) }
	results := []string{}
	runStatus := "passed"
	for index, test := range worker.tests {
		channel := []string{fmt.Sprintf(`{"type":"hello","protocol":"%s","mode":"%s","testId":"%s","retry":0}`, prove.Protocol, mode, test.id)}
		passed := worker.baselineOK
		if fault == nil {
			for _, path := range test.requests {
				channel = append(channel,
					fmt.Sprintf(`{"type":"request","method":"GET","origin":"%s","path":"%s","resourceType":"fetch"}`, fakeOrigin, path),
					fmt.Sprintf(`{"type":"response","method":"GET","origin":"%s","path":"%s","status":200,"json":true}`, fakeOrigin, path))
			}
		} else if slices.Contains(test.requests, fault.Path) && fault.Origin == fakeOrigin {
			if fault.Kind == prove.KindEmptyJSON && test.unreachable {
				channel = append(channel, fmt.Sprintf(`{"type":"not-applicable","fault":"%s","reason":"unreachable"}`, fault.ID))
			} else {
				channel = append(channel, fmt.Sprintf(`{"type":"applied","fault":"%s"}`, fault.ID))
				passed = !test.asserted[fault.Path]
			}
		}
		if worker.corrupt || (worker.corruptFaults && fault != nil) {
			channel = append(channel, `{"type":"request","url":"http://127.0.0.1:4100/api/cart?token=1"}`)
		}
		if worker.instrument {
			_ = os.WriteFile(filepath.Join(job.Env["NINELIVES_PROVE_DIR"], fmt.Sprintf("%032x", index)+".ndjson"), []byte(strings.Join(channel, "\n")+"\n"), 0o600)
		}
		attempts := []string{"failed"}
		if passed {
			attempts = []string{"passed"}
		} else if test.retries && fault != nil {
			// A retried test: first attempt fails, the retry passes.
			attempts = []string{"failed", "passed"}
			if worker.instrument {
				retry := fmt.Sprintf(`{"type":"hello","protocol":"%s","mode":"%s","testId":"%s","retry":1}`, prove.Protocol, mode, test.id) + "\n" + fmt.Sprintf(`{"type":"applied","fault":"%s"}`, fault.ID) + "\n"
				_ = os.WriteFile(filepath.Join(job.Env["NINELIVES_PROVE_DIR"], fmt.Sprintf("%031x", index)+"f.ndjson"), []byte(retry), 0o600)
			}
		}
		for retry, status := range attempts {
			step++
			emit(fmt.Sprintf(`"type":"test_begin","testId":"%s","retry":%d`, test.id, retry))
			emit(fmt.Sprintf(`"type":"step_end","testId":"%s","retry":%d,"stepId":"step-%d","category":"assertion","status":"%s"`, test.id, retry, step, status))
			emit(fmt.Sprintf(`"type":"test_end","testId":"%s","retry":%d,"status":"%s","expectedStatus":"passed","artifacts":[]`, test.id, retry, status))
		}
		outcome := "expected"
		if len(attempts) > 1 {
			outcome = "flaky"
		} else if !passed {
			outcome, runStatus = "unexpected", "failed"
		}
		results = append(results, fmt.Sprintf(`"type":"test_result","testId":"%s","outcome":"%s"`, test.id, outcome))
	}
	for _, result := range results {
		emit(result)
	}
	emit(`"type":"end","status":"` + runStatus + `"`)
	var stream bytes.Buffer
	for _, event := range events {
		stream.WriteString("{" + identity + "," + event + "}\n")
	}
	_ = os.WriteFile(job.Env["NINELIVES_ENGINE_EVENTS"], stream.Bytes(), 0o600)
	exit := 0
	if runStatus == "failed" {
		exit = 1
	}
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

func proveReport(t *testing.T, stdout, stderr string) prove.Report {
	t.Helper()
	var report prove.Report
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("%v: stdout=%s stderr=%s", err, stdout, stderr)
	}
	return report
}

func faultResults(report prove.Report) string {
	results := []string{}
	for _, fault := range report.Faults {
		results = append(results, fault.Request+":"+fault.Kind+":"+fault.Result)
	}
	return strings.Join(results, " ")
}

func TestProveClassifiesEachFaultAndPersistsAReportWithoutURLs(t *testing.T) {
	project := proveProject(t)
	receipts := filepath.Join(t.TempDir(), "receipts")
	worker := shopWorker()
	code, stdout, stderr := runProve(t, worker, filepath.Join(project, "tests/shop.spec.ts"), "--format", "json", "--receipt-dir", receipts, "--paths")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	report := proveReport(t, stdout, stderr)
	want := "req-1:abort:caught req-1:http-500:caught req-1:empty-json:caught req-2:abort:survived req-2:http-500:survived req-2:empty-json:survived"
	if faultResults(report) != want || !report.Complete || report.IncompleteReason != "" || report.Policy != "prove-network-v2" || report.Summary != (prove.Summary{Faults: 6, Caught: 3, Survived: 3, Exercised: 6}) {
		t.Fatalf("results %q\nreport %+v", faultResults(report), report)
	}
	if tests := report.Faults[0].Tests; len(tests) != 1 || tests[0].TestID != strings.Repeat("a", 64) || tests[0].Result != prove.Caught || tests[0].Applied != 1 {
		t.Fatalf("per-test results %+v", tests)
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

func TestProveStopsAtTheFaultBudgetAndStaysIncomplete(t *testing.T) {
	project := proveProject(t)
	worker := shopWorker()
	code, stdout, stderr := runProve(t, worker, filepath.Join(project, "tests/shop.spec.ts"), "--max-faults", "2", "--receipt-dir", t.TempDir())
	// The faults that ran are reported, but a truncated proof is not a success.
	if code != 1 || !strings.Contains(stdout, "2 caught, 0 survived, 0 inconclusive, 4 not run") || !strings.Contains(stdout, "INCOMPLETE (faults-not-run)") || !strings.Contains(stderr, "incomplete (faults-not-run)") || len(worker.commands) != 3 {
		t.Fatalf("code=%d runs=%d stdout=%s stderr=%s", code, len(worker.commands), stdout, stderr)
	}
	if strings.Contains(stdout, "/api/") {
		t.Fatal("text output shows URLs without --paths")
	}
}

func TestProveThatExercisesNothingIsNotASuccess(t *testing.T) {
	project := proveProject(t)
	// The baseline observed requests on one origin; the fault runs see another,
	// as a server on a random port would, so no fault is ever applied.
	worker := shopWorker()
	worker.tests[0].requests = []string{"/api/cart"}
	runs := 0
	proveExecutor = executorFunc(func(ctx context.Context, job runner.Job, attempt int) runner.ProcessOutput {
		runs++
		if runs > 1 {
			worker.tests[0].requests = nil
		}
		return worker.Run(ctx, job, attempt)
	})
	t.Cleanup(func() { proveExecutor = nil })
	var stdout, stderr bytes.Buffer
	code := run([]string{"prove", filepath.Join(project, "tests/shop.spec.ts"), "--format", "json", "--receipt-dir", t.TempDir()}, &stdout, &stderr)
	report := proveReport(t, stdout.String(), stderr.String())
	if code != 1 || report.Complete || report.IncompleteReason != prove.IncompleteNothingExercised || report.Summary.Exercised != 0 || report.Summary.Inconclusive != 3 {
		t.Fatalf("code=%d report=%+v stderr=%s", code, report, stderr.String())
	}
}

func TestProveClassifiesEachTestAndAFaultSurvivesWhenAnyTestPasses(t *testing.T) {
	project := proveProject(t)
	// Test a asserts the cart; test b fetches it and asserts nothing about it;
	// test c never touches it but fails under every fault on an unrelated path.
	a, b, c := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	worker := &fakeProveWorker{baselineOK: true, instrument: true, tests: []fakeTest{
		{id: a, requests: []string{"/api/cart"}, asserted: map[string]bool{"/api/cart": true}},
		{id: b, requests: []string{"/api/cart"}},
		{id: c, requests: []string{"/api/other"}, asserted: map[string]bool{"/api/other": true}},
	}}
	code, stdout, stderr := runProve(t, worker, filepath.Join(project, "tests/shop.spec.ts"), "--format", "json", "--receipt-dir", t.TempDir())
	report := proveReport(t, stdout, stderr)
	if code != 0 || faultResults(report) != "req-1:abort:survived req-1:http-500:survived req-1:empty-json:survived req-2:abort:caught req-2:http-500:caught req-2:empty-json:caught" {
		t.Fatalf("code=%d results %q stderr=%s", code, faultResults(report), stderr)
	}
	// The cart fault: a caught it, b survived it, c was never exercised by it.
	got := []string{}
	for _, test := range report.Faults[0].Tests {
		got = append(got, test.TestID[:1]+"="+test.Result)
	}
	if strings.Join(got, " ") != "a=caught b=survived c=not-exercised" {
		t.Fatalf("per-test results %v", got)
	}
	_, text, _ := runProve(t, worker, filepath.Join(project, "tests/shop.spec.ts"), "--receipt-dir", t.TempDir())
	if !strings.Contains(text, "survived                 test "+b[:12]) || !strings.Contains(text, "not-exercised            test "+c[:12]) {
		t.Fatalf("text output lacks per-test lines:\n%s", text)
	}
}

func TestProveReportsRetriedTestsInsteadOfSurvived(t *testing.T) {
	project := proveProject(t)
	worker := shopWorker()
	worker.tests[0].retries = true
	code, stdout, stderr := runProve(t, worker, filepath.Join(project, "tests/shop.spec.ts"), "--format", "json", "--receipt-dir", t.TempDir())
	report := proveReport(t, stdout, stderr)
	// The cart faults failed the first attempt and passed the retry: not survived.
	if code != 0 || faultResults(report) != "req-1:abort:retried req-1:http-500:retried req-1:empty-json:retried req-2:abort:survived req-2:http-500:survived req-2:empty-json:survived" || report.Summary.Inconclusive != 3 || !report.Complete {
		t.Fatalf("code=%d results %q summary %+v stderr=%s", code, faultResults(report), report.Summary, stderr)
	}
	_, text, _ := runProve(t, worker, filepath.Join(project, "tests/shop.spec.ts"), "--receipt-dir", t.TempDir())
	if !strings.Contains(text, "RETRIED") || !strings.Contains(text, "disable retries in the spec") {
		t.Fatalf("text output lacks the retry explanation:\n%s", text)
	}
}

func TestProveRecordsWhyEmptyJSONWasNotApplicable(t *testing.T) {
	project := proveProject(t)
	worker := shopWorker()
	worker.tests[0].unreachable = true
	_, stdout, stderr := runProve(t, worker, filepath.Join(project, "tests/shop.spec.ts"), "--format", "json", "--receipt-dir", t.TempDir())
	report := proveReport(t, stdout, stderr)
	if fault := report.Faults[2]; fault.Kind != prove.KindEmptyJSON || fault.Result != prove.NotApplicable || fault.Tests[0].Reason != prove.ReasonUnreachable {
		t.Fatalf("empty-json fault %+v", fault)
	}
}

func TestProveReportsAnInterruptedBaselineAsSuch(t *testing.T) {
	project := proveProject(t)
	worker := shopWorker()
	worker.delay = 2 * time.Second
	code, _, stderr := runProve(t, worker, filepath.Join(project, "tests/shop.spec.ts"), "--deadline", "100ms", "--receipt-dir", t.TempDir())
	if code != 1 || !strings.Contains(stderr, "canceled or timed out") || strings.Contains(stderr, "failing test") || len(worker.commands) != 1 {
		t.Fatalf("code=%d runs=%d stderr=%s", code, len(worker.commands), stderr)
	}
}

func TestProveMarksAnInterruptedFaultRunIncomplete(t *testing.T) {
	project := proveProject(t)
	worker := shopWorker()
	runs := 0
	proveExecutor = executorFunc(func(ctx context.Context, job runner.Job, attempt int) runner.ProcessOutput {
		runs++
		if runs == 3 {
			worker.delay = 2 * time.Second
		}
		return worker.Run(ctx, job, attempt)
	})
	t.Cleanup(func() { proveExecutor = nil })
	var stdout, stderr bytes.Buffer
	code := run([]string{"prove", filepath.Join(project, "tests/shop.spec.ts"), "--format", "json", "--deadline", "1500ms", "--receipt-dir", t.TempDir()}, &stdout, &stderr)
	report := proveReport(t, stdout.String(), stderr.String())
	if code != 1 || !report.Interrupted || report.Complete || report.IncompleteReason != prove.IncompleteInterrupted || report.Faults[0].Result != prove.Caught || report.Faults[1].Result != prove.Incomplete || report.Summary.NotRun != 4 {
		t.Fatalf("code=%d report=%+v stderr=%s", code, report, stderr.String())
	}
}

func TestProveRefusesWithoutAGreenInstrumentedBaseline(t *testing.T) {
	project := proveProject(t)
	spec := filepath.Join(project, "tests/shop.spec.ts")
	worker := shopWorker()
	worker.baselineOK = false
	if code, _, stderr := runProve(t, worker, spec, "--receipt-dir", t.TempDir()); code != 1 || !strings.Contains(stderr, "passing, complete baseline") || len(worker.commands) != 1 {
		t.Fatalf("failing baseline: code=%d stderr=%s", code, stderr)
	}
	worker = shopWorker()
	worker.instrument = false
	if code, _, stderr := runProve(t, worker, spec, "--receipt-dir", t.TempDir()); code != 1 || !strings.Contains(stderr, "no browser context was instrumented") || len(worker.commands) != 1 {
		t.Fatalf("uninstrumented baseline: code=%d stderr=%s", code, stderr)
	}
	worker = shopWorker()
	worker.corrupt = true
	if code, _, stderr := runProve(t, worker, spec, "--receipt-dir", t.TempDir()); code != 1 || !strings.Contains(stderr, "prove records failed validation") || len(worker.commands) != 1 {
		t.Fatalf("invalid prove records: code=%d stderr=%s", code, stderr)
	}
}

func TestProveRejectsInvalidUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"a.spec.ts", "b.spec.ts"}, {"a.spec.ts", "--format", "xml"}, {"a.spec.ts", "--max-faults", "0"}, {"a.spec.ts", "--max-faults", "257"}, {"a.spec.ts", "--pin-skip", "no separator"}} {
		if code, _, _ := runProve(t, shopWorker(), args...); code != 2 {
			t.Fatalf("args %q: code=%d", args, code)
		}
	}
}

func TestProveInvalidFaultEvidenceMakesTheProofUnsuccessful(t *testing.T) {
	project := proveProject(t)
	worker := shopWorker()
	worker.corruptFaults = true
	code, stdout, stderr := runProve(t, worker, filepath.Join(project, "tests/shop.spec.ts"), "--format", "json", "--receipt-dir", t.TempDir())
	report := proveReport(t, stdout, stderr)
	if code != 1 || report.Complete || report.IncompleteReason != prove.IncompleteInvalidEvidence || !report.InvalidEvidence || report.Summary.Inconclusive != len(report.Faults) {
		t.Fatalf("code=%d complete=%v invalid=%v summary=%+v", code, report.Complete, report.InvalidEvidence, report.Summary)
	}
}

func TestProveSeparatesUsageFromOperationalFailures(t *testing.T) {
	project := proveProject(t)
	worker := shopWorker()
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
