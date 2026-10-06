package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qualitymax/9lives-runner/internal/contracttest"
)

type fakeAdapter struct{}

func (fakeAdapter) Name() string              { return "fake" }
func (fakeAdapter) Supports(path string) bool { return filepath.Ext(path) == ".fake" }
func (fakeAdapter) Plan(path, input string, index int) (Job, error) {
	return fakeJob(fmt.Sprintf("job-%03d", index), "success", 0), nil
}
func (fakeAdapter) Validate(stdout []byte) (Validation, error) {
	var result struct {
		Outcome    string `json:"outcome"`
		Assertions int    `json:"assertions"`
		Artifact   bool   `json:"artifact"`
	}
	if err := json.Unmarshal(stdout, &result); err != nil {
		return Validation{}, fmt.Errorf("malformed fake output: %w", err)
	}
	if !result.Artifact {
		return Validation{}, errors.New("required fake artifact is missing")
	}
	if result.Outcome == "failed" {
		return Validation{FailureCount: 1, ExecutedTests: 1, VerifiedAssertions: result.Assertions, AssertionCoverage: "known", Description: "fake report"}, nil
	}
	if result.Outcome != "passed" {
		return Validation{}, errors.New("unknown fake outcome")
	}
	return Validation{ExecutedTests: 1, VerifiedAssertions: result.Assertions, AssertionCoverage: "known", Description: "fake report"}, nil
}

func TestFakeAdapterContractOutcomes(t *testing.T) {
	cases := []struct {
		name, mode string
		want       ReceiptStatus
		validated  bool
	}{
		{name: "success", mode: "success", want: StatusPassed, validated: true},
		{name: "test failure", mode: "test-failure", want: StatusFailed, validated: true},
		{name: "infrastructure failure", mode: "infrastructure", want: StatusError},
		{name: "nonzero without finding", mode: "infra-json", want: StatusError, validated: true},
		{name: "malformed output", mode: "malformed", want: StatusError},
		{name: "missing artifact", mode: "missing-artifact", want: StatusError},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			job := fakeJob("job-001", test.mode, 0)
			plan := Plan{Version: 1, RunID: "run-" + test.mode, Jobs: []Job{job}}
			summary, _ := Execute(context.Background(), plan, ExecuteOptions{Workers: 1, Timeout: 5 * time.Second, MaxAttempts: 1, ReceiptDir: t.TempDir(), Adapters: []Adapter{fakeAdapter{}}})
			if len(summary.Receipts) != 1 || summary.Receipts[0].Status != test.want || summary.Receipts[0].Validated != test.validated {
				t.Fatalf("unexpected result: %#v", summary)
			}
			if test.mode == "success" {
				canonical := filepath.Join(filepath.Dir(summary.Receipts[0].ReceiptPath), "execution-receipt-1.0.json")
				if _, err := os.Stat(canonical); err != nil {
					t.Fatalf("canonical receipt missing: %v", err)
				}
			}
		})
	}
}

func TestFakeAdapterTimeoutAndCancellation(t *testing.T) {
	for _, test := range []struct {
		name    string
		timeout time.Duration
		cancel  bool
		kind    string
	}{
		{name: "timeout", timeout: 50 * time.Millisecond, kind: "timeout"},
		{name: "cancellation", timeout: time.Second, cancel: true, kind: "canceled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if test.cancel {
				go func() { time.Sleep(40 * time.Millisecond); cancel() }()
			} else {
				defer cancel()
			}
			started := time.Now()
			plan := Plan{Version: 1, RunID: "run-" + test.name, Jobs: []Job{fakeJob("job-001", "sleep", 5000)}}
			summary, _ := Execute(ctx, plan, ExecuteOptions{Workers: 1, Timeout: test.timeout, ReceiptDir: t.TempDir(), Adapters: []Adapter{fakeAdapter{}}})
			if time.Since(started) > time.Second {
				t.Fatal("cancellation did not terminate child promptly")
			}
			want := StatusCanceled
			if test.kind == "timeout" {
				want = StatusTimedOut
			}
			if len(summary.Receipts) != 1 || summary.Receipts[0].Status != want || summary.Receipts[0].Termination == nil || summary.Receipts[0].Termination.Kind != test.kind {
				t.Fatalf("unexpected termination: %#v", summary)
			}
		})
	}
}

func TestRunDeadlineIsNotReportedAsUserCancellation(t *testing.T) {
	plan := Plan{Version: 1, RunID: "run-deadline-not-cancel", Jobs: []Job{fakeJob("job-001", "sleep", 5000)}}
	summary, _ := Execute(context.Background(), plan, ExecuteOptions{
		Workers: 1, Timeout: 10 * time.Second, RunDeadline: 50 * time.Millisecond,
		ReceiptDir: t.TempDir(), Adapters: []Adapter{fakeAdapter{}},
	})
	if len(summary.Receipts) != 1 || summary.Receipts[0].Status == StatusCanceled {
		t.Fatalf("run deadline must not be reported as user cancellation: %#v", summary)
	}
}

func TestCanonicalReceiptMatchesPlatformModelOffline(t *testing.T) {
	cases := []struct {
		name, mode string
		timeout    time.Duration
		cancel     bool
	}{
		{name: "success", mode: "success", timeout: time.Second},
		{name: "test-failure", mode: "test-failure", timeout: time.Second},
		{name: "infrastructure", mode: "infrastructure", timeout: time.Second},
		{name: "malformed", mode: "malformed", timeout: time.Second},
		{name: "missing-artifact", mode: "missing-artifact", timeout: time.Second},
		{name: "timeout", mode: "sleep", timeout: 25 * time.Millisecond},
		{name: "cancelled", mode: "sleep", timeout: time.Second, cancel: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.cancel {
				go func() { time.Sleep(20 * time.Millisecond); cancel() }()
			}
			root := t.TempDir()
			plan := Plan{Version: 1, RunID: "run-canonical-" + test.name, Jobs: []Job{fakeJob("job-001", test.mode, 5000)}}
			summary, _ := Execute(ctx, plan, ExecuteOptions{Workers: 1, Timeout: test.timeout, ReceiptDir: root, Adapters: []Adapter{fakeAdapter{}}})
			if len(summary.Receipts) != 1 {
				t.Fatalf("expected one terminal receipt: %#v", summary)
			}
			validateCanonicalReceiptSnapshot(t, filepath.Join(filepath.Dir(summary.Receipts[0].ReceiptPath), "execution-receipt-1.0.json"))
		})
	}
}

func validateCanonicalReceiptSnapshot(t *testing.T, receipt string) {
	t.Helper()
	contract := filepath.Join(contracttest.RunnerRoot(), "testdata", "contracts", "execution-receipt")
	if _, err := os.Stat(filepath.Join(contract, "execution_receipt.py")); err != nil {
		t.Fatalf("contract snapshot is required: %v", err)
	}
	python := contracttest.Python(t, "pydantic", "jsonschema")
	program := `import importlib.util,json,sys,jsonschema
p=sys.argv[1]
s=importlib.util.spec_from_file_location("execution_receipt",p+"/execution_receipt.py")
m=importlib.util.module_from_spec(s)
sys.modules[s.name]=m
s.loader.exec_module(m)
payload=json.load(open(sys.argv[2]))
schema=json.load(open(p+"/1.0.schema.json"))
m.ExecutionReceipt.model_validate(payload)
jsonschema.validate(payload,schema)
bad=dict(payload)
bad["schema_version"]="9.9"
try:
    jsonschema.validate(bad,schema)
except jsonschema.ValidationError:
    pass
else:
    raise AssertionError("schema accepted invalid version")
bad=dict(payload)
bad["terminal"]=dict(payload["terminal"])
bad["terminal"]["duration_seconds"]=-1
try:
    m.ExecutionReceipt.model_validate(bad)
except Exception:
    pass
else:
    raise AssertionError("model accepted negative duration")`

	command := exec.Command(python, "-c", program, contract, receipt)
	command.Dir = contracttest.RunnerRoot()
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("canonical receipt rejected by pinned platform model/schema: %v: %s", err, output)
	}
}

func TestBoundedConcurrencyAndRunIdentity(t *testing.T) {
	jobs := make([]Job, 4)
	for index := range jobs {
		jobs[index] = fakeJob(fmt.Sprintf("job-%03d", index+1), "sleep", 120)
	}
	root := t.TempDir()
	plan := Plan{Version: 1, RunID: "run-concurrency", Jobs: jobs}
	summary, err := Execute(context.Background(), plan, ExecuteOptions{Workers: 2, Timeout: 5 * time.Second, ReceiptDir: root, Adapters: []Adapter{fakeAdapter{}}})
	if err != nil {
		t.Fatal(err)
	}
	events, err := readEvents(filepath.Join(root, plan.RunID, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	active, maximum := 0, 0
	for _, event := range events {
		if event.Type == "attempt_started" {
			active++
			if active > maximum {
				maximum = active
			}
		}
		if event.Type == "attempt_finished" {
			active--
		}
	}
	if maximum != 2 {
		t.Fatalf("configured concurrency was not observed: max=%d events=%#v", maximum, events)
	}
	for _, receipt := range summary.Receipts {
		if receipt.RunID != plan.RunID || filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(receipt.ReceiptPath)))) != plan.RunID {
			t.Fatalf("receipt escaped originating run: %#v", receipt)
		}
	}
}

func TestDependencyOrderingAndCycleRejection(t *testing.T) {
	jobs := []Job{{ID: "second", DependsOn: []string{"first"}}, {ID: "first", DependsOn: []string{}}}
	ordered, dependent, err := orderJobs(jobs)
	if err != nil || !dependent || ordered[0].ID != "first" {
		t.Fatalf("bad dependency order: %#v %v", ordered, err)
	}
	_, _, err = orderJobs([]Job{{ID: "a", DependsOn: []string{"b"}}, {ID: "b", DependsOn: []string{"a"}}})
	if err == nil {
		t.Fatal("dependency cycle should be rejected")
	}
}

func TestAttemptAndOutputBudgetsAreEnforced(t *testing.T) {
	root := t.TempDir()
	plan := Plan{Version: 1, RunID: "run-attempt-budget", Jobs: []Job{fakeJob("job-001", "infrastructure", 0)}}
	summary, _ := Execute(context.Background(), plan, ExecuteOptions{Workers: 1, Timeout: time.Second, MaxAttempts: 2, MaxOutputBytes: 1024, ReceiptDir: root, Adapters: []Adapter{fakeAdapter{}}})
	if len(summary.Receipts) != 1 || summary.Receipts[0].Attempt != 2 {
		t.Fatalf("attempt limit was not enforced: %#v", summary)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		path := filepath.Join(root, plan.RunID, "job-001", fmt.Sprintf("job-001-attempt-%03d", attempt), "receipt.json")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("attempt %d receipt missing: %v", attempt, err)
		}
	}

	largePlan := Plan{Version: 1, RunID: "run-output-budget", Jobs: []Job{fakeJob("job-001", "large", 0)}}
	large, _ := Execute(context.Background(), largePlan, ExecuteOptions{Workers: 1, Timeout: 5 * time.Second, MaxAttempts: 1, MaxOutputBytes: 1024, ReceiptDir: t.TempDir(), Adapters: []Adapter{fakeAdapter{}}})
	if len(large.Receipts) != 1 || !large.Receipts[0].Evidence.StdoutTruncated || large.Receipts[0].Status != StatusError {
		t.Fatalf("output budget was not enforced: %#v", large)
	}
}

func TestSharedRunDeadlineIsClassified(t *testing.T) {
	plan := Plan{Version: 1, RunID: "run-deadline", Jobs: []Job{fakeJob("job-001", "sleep", 5000)}}
	summary, _ := Execute(context.Background(), plan, ExecuteOptions{Workers: 1, Timeout: 10 * time.Second, RunDeadline: 50 * time.Millisecond, ReceiptDir: t.TempDir(), Adapters: []Adapter{fakeAdapter{}}})
	if len(summary.Receipts) != 1 || summary.Receipts[0].Termination == nil || summary.Receipts[0].Termination.Kind != "timeout" {
		t.Fatalf("run deadline was not classified: %#v", summary)
	}
}

func TestPersistedStatusResultAndExternalCancellation(t *testing.T) {
	root := t.TempDir()
	plan := Plan{Version: 1, RunID: "run-external-cancel", Jobs: []Job{fakeJob("job-001", "sleep", 5000)}}
	done := make(chan RunSummary, 1)
	go func() {
		summary, _ := Execute(context.Background(), plan, ExecuteOptions{Workers: 1, Timeout: 10 * time.Second, ReceiptDir: root, Adapters: []Adapter{fakeAdapter{}}})
		done <- summary
	}()
	deadline := time.Now().Add(time.Second)
	for {
		status, err := LoadStatus(root, plan.RunID)
		if err == nil && status.State == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run never became visible: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := RequestCancel(root, plan.RunID); err != nil {
		t.Fatal(err)
	}
	select {
	case summary := <-done:
		if summary.Canceled != 1 || summary.Complete {
			t.Fatalf("unexpected canceled result: %#v", summary)
		}
	case <-time.After(time.Second):
		t.Fatal("external cancellation was not observed promptly")
	}
	status, err := LoadStatus(root, plan.RunID)
	if err != nil || status.Result == nil || status.State != "finished" {
		t.Fatalf("bad final status: %#v %v", status, err)
	}
	result, err := LoadResult(root, plan.RunID)
	if err != nil || result.RunID != plan.RunID {
		t.Fatalf("bad persisted result: %#v %v", result, err)
	}
}

func fakeJob(id, mode string, milliseconds int) Job {
	return Job{
		ID: id, Spec: id + ".fake", WorkDir: os.TempDir(), Adapter: "fake", DependsOn: []string{},
		Command: []string{os.Args[0], "-test.run=TestRunnerHelperProcess", "--", mode, fmt.Sprint(milliseconds)},
		Env:     map[string]string{"NINELIVES_FAKE_HELPER": "1"},
	}
}

func TestRunnerHelperProcess(t *testing.T) {
	if os.Getenv("NINELIVES_FAKE_HELPER") != "1" {
		return
	}
	separator := 0
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	mode := os.Args[separator+1]
	switch mode {
	case "success":
		fmt.Print(`{"outcome":"passed","assertions":3,"artifact":true}`)
		os.Exit(0)
	case "test-failure":
		fmt.Print(`{"outcome":"failed","assertions":2,"artifact":true}`)
		os.Exit(1)
	case "infrastructure":
		fmt.Fprint(os.Stderr, "executor unavailable")
		os.Exit(2)
	case "infra-json":
		fmt.Print(`{"outcome":"passed","assertions":0,"artifact":true}`)
		os.Exit(2)
	case "malformed":
		fmt.Print("not json")
		os.Exit(0)
	case "missing-artifact":
		fmt.Print(`{"outcome":"passed","assertions":3,"artifact":false}`)
		os.Exit(0)
	case "sleep":
		var milliseconds int
		_, _ = fmt.Sscan(os.Args[separator+2], &milliseconds)
		time.Sleep(time.Duration(milliseconds) * time.Millisecond)
		fmt.Print(`{"outcome":"passed","assertions":1,"artifact":true}`)
		os.Exit(0)
	case "large":
		fmt.Print(`{"outcome":"passed","assertions":1,"artifact":true,"padding":"` + strings.Repeat("x", 4096) + `"}`)
		os.Exit(0)
	}
	os.Exit(3)
}
