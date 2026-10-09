package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunOutcomeSeparatesProvenFailureFromIncompleteRun(t *testing.T) {
	for _, test := range []struct {
		name    string
		modes   []string
		skipped bool
		cancel  bool
		want    RunOutcome
	}{
		{name: "all passed", modes: []string{"success", "success"}, want: OutcomePassed},
		{name: "test failure", modes: []string{"success", "test-failure"}, want: OutcomeFailed},
		{name: "failure and infrastructure error", modes: []string{"test-failure", "infrastructure"}, want: OutcomeIncomplete},
		{name: "nonzero exit without a reported failure", modes: []string{"infra-json"}, want: OutcomeIncomplete},
		{name: "missing evidence", modes: []string{"missing-artifact"}, want: OutcomeIncomplete},
		{name: "skipped input", modes: []string{"success"}, skipped: true, want: OutcomeIncomplete},
		{name: "canceled", modes: []string{"sleep"}, cancel: true, want: OutcomeIncomplete},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := Plan{Version: PlanVersion, RunID: "run-outcome", Skipped: []Skipped{}}
			for index, mode := range test.modes {
				plan.Jobs = append(plan.Jobs, fakeJob(filepath.Base(t.Name())+"-"+string(rune('a'+index)), mode, 5000))
			}
			if test.skipped {
				plan.Skipped = append(plan.Skipped, Skipped{Input: "missing.spec.ts", Reason: "no adapter"})
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.cancel {
				go func() { time.Sleep(40 * time.Millisecond); cancel() }()
			}
			root := t.TempDir()
			summary, _ := Execute(ctx, plan, ExecuteOptions{Workers: 2, Timeout: 5 * time.Second, MaxAttempts: 1, ReceiptDir: root, Adapters: []Adapter{fakeAdapter{}}})
			if summary.Version != RunSummaryVersion || summary.Outcome != test.want || summary.PlannedJobs != len(test.modes) || (summary.SkippedInputs == 1) != test.skipped {
				t.Fatalf("outcome %q version %d planned %d skipped %d, want %q", summary.Outcome, summary.Version, summary.PlannedJobs, summary.SkippedInputs, test.want)
			}
			if summary.Complete != (test.want == OutcomePassed) {
				t.Fatalf("complete=%v disagrees with outcome %q", summary.Complete, summary.Outcome)
			}
			persisted, err := LoadResult(root, plan.RunID)
			if err != nil || persisted.Outcome != summary.Outcome || persisted.Version != RunSummaryVersion {
				t.Fatalf("persisted result %q/%d: %v", persisted.Outcome, persisted.Version, err)
			}
			status, err := LoadStatus(root, plan.RunID)
			if err != nil || status.Version != RunStatusVersion || status.Result == nil || status.Result.Outcome != summary.Outcome {
				t.Fatalf("status %+v: %v", status, err)
			}
		})
	}
}

func TestRunOutcomeRequiresValidatedReceiptForEveryPlannedJob(t *testing.T) {
	failed := Receipt{Status: StatusFailed, Validated: true, FailureCount: 1}
	for _, test := range []struct {
		name    string
		summary RunSummary
	}{
		{"no planned jobs", RunSummary{}},
		{"missing receipt", RunSummary{PlannedJobs: 2, Receipts: []Receipt{failed}}},
		{"unvalidated failure", RunSummary{PlannedJobs: 1, Receipts: []Receipt{{Status: StatusFailed}}}},
		{"unvalidated pass", RunSummary{PlannedJobs: 1, Receipts: []Receipt{{Status: StatusPassed}}}},
		{"timed out", RunSummary{PlannedJobs: 2, Receipts: []Receipt{failed, {Status: StatusTimedOut}}}},
		{"goal did not complete", RunSummary{PlannedJobs: 1, Receipts: []Receipt{{Status: StatusFailed, Validated: true, GoalFailed: true}}}},
		{"assertion failed after an incomplete goal", RunSummary{PlannedJobs: 1, Receipts: []Receipt{{Status: StatusFailed, Validated: true, FailureCount: 1, GoalFailed: true}}}},
	} {
		if got := classifyRun(test.summary, false); got != OutcomeIncomplete {
			t.Errorf("%s: %q, want incomplete", test.name, got)
		}
	}
	if got := classifyRun(RunSummary{PlannedJobs: 1, Receipts: []Receipt{failed}}, true); got != OutcomeIncomplete {
		t.Errorf("interrupted run classified %q", got)
	}
	// One failed goal in a file does not hide an assertion failure in another
	// test of that file: the second failure was proved without the goal.
	beside := Receipt{Status: StatusFailed, Validated: true, FailureCount: 2, GoalFailed: true, NonGoalFailureCount: 1}
	if got := classifyRun(RunSummary{PlannedJobs: 1, Receipts: []Receipt{beside}}, false); got != OutcomeFailed {
		t.Errorf("assertion failure beside a failed goal classified %q, want failed", got)
	}
	beside.FailureCount = 1 // The test that caught the goal error passed.
	if got := classifyRun(RunSummary{PlannedJobs: 1, Receipts: []Receipt{beside}}, false); got != OutcomeFailed {
		t.Errorf("assertion failure beside a caught goal error classified %q, want failed", got)
	}
}

func TestInvalidGoalAttributionCountsCannotValidate(t *testing.T) {
	for _, count := range []int{-1, 2} {
		if err := validateValidation(Validation{ExecutedTests: 2, FailureCount: 1, NonGoalFailureCount: count}); err == nil {
			t.Fatalf("accepted non-goal failure count %d outside failure count", count)
		}
	}
}

func TestExecuteReportsSetupProblemsAsSetupErrors(t *testing.T) {
	plan := Plan{Version: PlanVersion, RunID: "run-setup", Jobs: []Job{fakeJob("a", "success", 100), fakeJob("b", "success", 100)}, Skipped: []Skipped{}, Limits: Limits{MaxJobs: 1}}
	_, err := Execute(context.Background(), plan, ExecuteOptions{Workers: 1, Timeout: time.Second, MaxAttempts: 1, ReceiptDir: t.TempDir(), Adapters: []Adapter{fakeAdapter{}}})
	var setup SetupError
	if !errors.As(err, &setup) {
		t.Fatalf("job budget overflow is not a setup error: %v", err)
	}
	record := AgentProvenance{}
	_, err = Execute(context.Background(), plan, ExecuteOptions{AgentProvenance: &record, Workers: 1, Timeout: time.Second, MaxAttempts: 1, ReceiptDir: t.TempDir(), Adapters: []Adapter{fakeAdapter{}}})
	if !errors.As(err, &setup) {
		t.Fatalf("invalid provenance is not a setup error: %v", err)
	}
	// An unwritable receipt directory is operational, not a usage error.
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Execute(context.Background(), Plan{Version: PlanVersion, RunID: "run-blocked", Jobs: []Job{fakeJob("a", "success", 100)}, Skipped: []Skipped{}}, ExecuteOptions{Workers: 1, Timeout: time.Second, MaxAttempts: 1, ReceiptDir: blocked, Adapters: []Adapter{fakeAdapter{}}})
	if err == nil || errors.As(err, &setup) {
		t.Fatalf("unwritable receipt directory reported as %v", err)
	}
}

// A result.json written by CLI 0.1.1 has no version, outcome or counts.
func TestLoadResultUpgradesUnversionedResult(t *testing.T) {
	root := t.TempDir()
	runID := "run-legacy"
	directory := filepath.Join(root, runID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	plan := Plan{Version: 1, RunID: runID, Jobs: []Job{{ID: "job-001"}, {ID: "job-002"}}, Skipped: []Skipped{}}
	legacy := map[string]any{"runId": runID, "complete": false, "passed": 1, "failed": 1, "receipts": []Receipt{
		{JobID: "job-001", Status: StatusPassed, Validated: true}, {JobID: "job-002", Status: StatusFailed, Validated: true, FailureCount: 1},
	}}
	for name, value := range map[string]any{"plan.json": plan, "result.json": legacy} {
		raw, _ := json.Marshal(value)
		if err := os.WriteFile(filepath.Join(directory, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := LoadResult(root, runID)
	if err != nil || result.Version != RunSummaryVersion || result.Outcome != OutcomeFailed || result.PlannedJobs != 2 {
		t.Fatalf("upgraded result %+v: %v", result, err)
	}
	status, err := LoadStatus(root, runID)
	if err != nil || status.Result == nil || status.Result.Outcome != OutcomeFailed {
		t.Fatalf("upgraded status %+v: %v", status, err)
	}
}
