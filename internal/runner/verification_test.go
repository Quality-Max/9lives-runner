package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeVerifier answers each planned check with a scripted result and counts
// how often the engine asked it to probe.
type fakeVerifier struct {
	results   []VerificationResult
	changed   bool
	probes    *atomic.Int32
	override  []Verification
	duringRun func()
}

func checkReason(result VerificationResult) (string, int) {
	switch result {
	case VerificationVerified:
		return "", 200
	case VerificationContradicted:
		return VerificationReasonJSONValue, 200
	case VerificationUnreachable:
		return VerificationReasonConnection, 0
	}
	return VerificationReasonBudget, 0
}

func (fake fakeVerifier) Planned() []Verification {
	planned := []Verification{}
	for index := range fake.results {
		planned = append(planned, Verification{ID: "check-" + string(rune('a'+index)), OutcomeID: "order-stored", Method: "GET", Result: VerificationNotRun, Reason: VerificationReasonNotValidated, ObservedBy: VerificationObserver})
	}
	return planned
}
func (fake fakeVerifier) SourceSHA256() string { return strings.Repeat("a", 64) }
func (fake fakeVerifier) Unchanged() bool      { return !fake.changed }
func (fake fakeVerifier) Verify(context.Context) []Verification {
	if fake.probes != nil {
		fake.probes.Add(1)
	}
	if fake.override != nil {
		return fake.override
	}
	records := fake.Planned()
	for index, result := range fake.results {
		records[index].Result = result
		records[index].Reason, records[index].HTTPStatus = checkReason(result)
	}
	return records
}

// workerRecorder keeps what each worker was started with.
type workerRecorder struct {
	runs *atomic.Int32
	jobs *[]Job
}

func (executor workerRecorder) Run(ctx context.Context, job Job, limit int) ProcessOutput {
	executor.runs.Add(1)
	if executor.jobs != nil {
		*executor.jobs = append(*executor.jobs, job)
	}
	return localProcessExecutor{}.Run(ctx, job, limit)
}

func verifiedRun(t *testing.T, runID, mode string, verifier PostconditionVerifier, attempts int) (RunSummary, int32) {
	t.Helper()
	var runs atomic.Int32
	plan := Plan{Version: 1, RunID: runID, Jobs: []Job{fakeJob("job-001", mode, 0)}}
	root := t.TempDir()
	summary, err := Execute(context.Background(), plan, ExecuteOptions{Workers: 1, Timeout: 10 * time.Second, MaxAttempts: attempts, ReceiptDir: root,
		Adapters: []Adapter{fakeAdapter{}}, Executor: workerRecorder{runs: &runs}, Verifier: verifier})
	if err != nil || len(summary.Receipts) != 1 {
		t.Fatalf("err=%v summary=%+v", err, summary)
	}
	// What a host reads back is the stored result, not the in-memory one.
	stored, err := LoadResult(root, runID)
	if err != nil || stored.Outcome != summary.Outcome || len(stored.Receipts) != 1 || !slices.Equal(stored.Receipts[0].Verifications, summary.Receipts[0].Verifications) {
		t.Fatalf("stored result differs: %+v err=%v", stored, err)
	}
	var persisted Receipt
	if raw, err := os.ReadFile(summary.Receipts[0].ReceiptPath); err != nil || json.Unmarshal(raw, &persisted) != nil || !slices.Equal(persisted.Verifications, summary.Receipts[0].Verifications) {
		t.Fatalf("receipt on disk differs: %v", err)
	}
	return summary, runs.Load()
}

func TestIndependentChecksDecideAnAttemptTheWorkerReportedPassed(t *testing.T) {
	for _, test := range []struct {
		name     string
		results  []VerificationResult
		status   ReceiptStatus
		outcome  RunOutcome
		stop     string
		verified int
	}{
		{"all verified", []VerificationResult{VerificationVerified, VerificationVerified}, StatusPassed, OutcomePassed, "", 2},
		{"one contradicted", []VerificationResult{VerificationVerified, VerificationContradicted}, StatusFailed, OutcomeFailed, StopVerificationContradicted, 1},
		{"one unreachable", []VerificationResult{VerificationVerified, VerificationUnreachable}, StatusError, OutcomeIncomplete, StopVerificationIncomplete, 1},
		{"out of time budget", []VerificationResult{VerificationVerified, VerificationNotRun}, StatusError, OutcomeIncomplete, StopVerificationIncomplete, 1},
		// A demonstrated mismatch is not hidden by a check that says nothing.
		{"contradicted beside unreachable", []VerificationResult{VerificationUnreachable, VerificationContradicted}, StatusFailed, OutcomeFailed, StopVerificationContradicted, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			var probes atomic.Int32
			// Three attempts are allowed; a probed attempt is never retried.
			summary, runs := verifiedRun(t, "run-verify-"+strings.ReplaceAll(test.name, " ", "-"), "success", fakeVerifier{results: test.results, probes: &probes}, 3)
			receipt := summary.Receipts[0]
			if receipt.Status != test.status || summary.Outcome != test.outcome || !receipt.Validated || runs != 1 || probes.Load() != 1 {
				t.Fatalf("status=%s outcome=%s validated=%v runs=%d probes=%d error=%q", receipt.Status, summary.Outcome, receipt.Validated, runs, probes.Load(), receipt.Error)
			}
			// The fake adapter's own evidence carries 3; the engine adds what it verified.
			if receipt.VerifiedAssertions != 3+test.verified || receipt.VerificationSHA256 != strings.Repeat("a", 64) || len(receipt.Verifications) != len(test.results) {
				t.Fatalf("verifiedAssertions=%d receipt=%+v", receipt.VerifiedAssertions, receipt)
			}
			for index, record := range receipt.Verifications {
				if record.Result != test.results[index] || record.ObservedBy != VerificationObserver || record.Validate() != nil {
					t.Fatalf("record %d = %+v", index, record)
				}
			}
			if (receipt.FailureSummary == nil) != (test.stop == "") || (receipt.FailureSummary != nil && (receipt.FailureSummary.Stop != test.stop || receipt.FailureSummary.Validate() != nil || receipt.FailureSummary.FailedTests != 0)) {
				t.Fatalf("failure summary %+v, want stop %q", receipt.FailureSummary, test.stop)
			}
			if summary.Complete != (test.outcome == OutcomePassed) || (test.status != StatusPassed && receipt.Error == "") {
				t.Fatalf("complete=%v error=%q", summary.Complete, receipt.Error)
			}
		})
	}
}

func TestAFailingTestStaysFailedWhateverTheChecksSay(t *testing.T) {
	for _, results := range [][]VerificationResult{{VerificationVerified}, {VerificationContradicted}, {VerificationUnreachable}} {
		summary, _ := verifiedRun(t, "run-verify-failing-"+string(results[0]), "test-failure", fakeVerifier{results: results}, 1)
		receipt := summary.Receipts[0]
		if receipt.Status != StatusFailed || summary.Outcome != OutcomeFailed || receipt.FailureSummary == nil || receipt.FailureSummary.Stop != StopTestFailed || receipt.FailureCount != 1 {
			t.Fatalf("%s: status=%s outcome=%s summary=%+v", results[0], receipt.Status, summary.Outcome, receipt.FailureSummary)
		}
		if len(receipt.Verifications) != 1 || receipt.Verifications[0].Result != results[0] {
			t.Fatalf("checks of a validated attempt still run and are recorded: %+v", receipt.Verifications)
		}
	}
}

func TestChecksDoNotRunForAnAttemptThatDidNotValidate(t *testing.T) {
	for _, mode := range []string{"malformed", "missing-artifact", "infrastructure"} {
		var probes atomic.Int32
		summary, _ := verifiedRun(t, "run-verify-unvalidated-"+mode, mode, fakeVerifier{results: []VerificationResult{VerificationVerified}, probes: &probes}, 1)
		receipt := summary.Receipts[0]
		if probes.Load() != 0 || receipt.Validated || receipt.Status != StatusError || summary.Outcome != OutcomeIncomplete || receipt.VerifiedAssertions != 0 {
			t.Fatalf("%s: probes=%d receipt=%+v", mode, probes.Load(), receipt)
		}
		if len(receipt.Verifications) != 1 || receipt.Verifications[0].Result != VerificationNotRun || receipt.Verifications[0].Reason != VerificationReasonNotValidated || receipt.Verifications[0].HTTPStatus != 0 {
			t.Fatalf("%s: %+v", mode, receipt.Verifications)
		}
		// The attempt's own reason stays the stop reason.
		if receipt.FailureSummary == nil || receipt.FailureSummary.Stop != StopInvalidEvidence {
			t.Fatalf("%s: stop %+v", mode, receipt.FailureSummary)
		}
	}
}

func TestAChangedChecksFileCannotPassAndIsNotProbed(t *testing.T) {
	var probes atomic.Int32
	summary, runs := verifiedRun(t, "run-verify-changed", "success", fakeVerifier{results: []VerificationResult{VerificationVerified}, changed: true, probes: &probes}, 3)
	receipt := summary.Receipts[0]
	if probes.Load() != 0 || runs != 1 || receipt.Status != StatusError || summary.Outcome != OutcomeIncomplete || receipt.VerifiedAssertions != 3 ||
		receipt.Verifications[0].Result != VerificationNotRun || receipt.Verifications[0].Reason != VerificationReasonFileChanged ||
		receipt.FailureSummary == nil || receipt.FailureSummary.Stop != StopVerificationChanged || !strings.Contains(receipt.Error, "checks file changed") {
		t.Fatalf("probes=%d runs=%d receipt=%+v summary=%+v", probes.Load(), runs, receipt, receipt.FailureSummary)
	}
}

// A verifier cannot rename, drop or add checks after the fact: anything but
// a well-formed answer for exactly the planned checks proves nothing.
func TestAnAnswerForOtherChecksIsDiscarded(t *testing.T) {
	verified := func(id string) Verification {
		return Verification{ID: id, OutcomeID: "order-stored", Method: "GET", Result: VerificationVerified, HTTPStatus: 200, ObservedBy: VerificationObserver}
	}
	for name, override := range map[string][]Verification{
		"renamed":       {verified("other"), verified("check-b")},
		"dropped":       {verified("check-a")},
		"added":         {verified("check-a"), verified("check-b"), verified("check-c")},
		"not by engine": {verified("check-a"), {ID: "check-b", OutcomeID: "order-stored", Method: "GET", Result: VerificationVerified, HTTPStatus: 200, ObservedBy: "worker"}},
		"no answer":     {verified("check-a"), {ID: "check-b", OutcomeID: "order-stored", Method: "GET", Result: VerificationVerified, ObservedBy: VerificationObserver}},
		"empty":         {},
	} {
		summary, _ := verifiedRun(t, "run-verify-discard-"+strings.ReplaceAll(name, " ", "-"), "success", fakeVerifier{results: []VerificationResult{VerificationVerified, VerificationVerified}, override: override}, 1)
		receipt := summary.Receipts[0]
		if receipt.Status != StatusError || summary.Outcome != OutcomeIncomplete || receipt.VerifiedAssertions != 3 || len(receipt.Verifications) != 2 {
			t.Fatalf("%s: receipt=%+v", name, receipt)
		}
		for _, record := range receipt.Verifications {
			if record.Result != VerificationNotRun {
				t.Fatalf("%s: %+v", name, record)
			}
		}
	}
}

func TestVerificationRequiresExactlyOnePlannedJob(t *testing.T) {
	verifier := fakeVerifier{results: []VerificationResult{VerificationVerified}}
	for name, plan := range map[string]Plan{
		"two jobs":      {Version: 1, RunID: "run-verify-two", Jobs: []Job{fakeJob("job-001", "success", 0), fakeJob("job-002", "success", 0)}},
		"skipped input": {Version: 1, RunID: "run-verify-skip", Jobs: []Job{fakeJob("job-001", "success", 0)}, Skipped: []Skipped{{Input: "x", Reason: "unsupported"}}},
		"no jobs":       {Version: 1, RunID: "run-verify-none"},
	} {
		var runs atomic.Int32
		root := t.TempDir()
		summary, err := Execute(context.Background(), plan, ExecuteOptions{ReceiptDir: root, Adapters: []Adapter{fakeAdapter{}}, Executor: workerRecorder{runs: &runs}, Verifier: verifier})
		var setup SetupError
		if !errors.As(err, &setup) || runs.Load() != 0 || summary.Complete || len(summary.Receipts) != 0 {
			t.Fatalf("%s: err=%v runs=%d summary=%+v", name, err, runs.Load(), summary)
		}
		if entries, _ := os.ReadDir(root); len(entries) != 0 {
			t.Fatalf("%s: a refused run created storage", name)
		}
	}
	// A verifier without a closed, bounded plan of checks is refused too.
	for name, results := range map[string][]VerificationResult{"no checks": {}, "too many": slices.Repeat([]VerificationResult{VerificationVerified}, MaxVerifications+1)} {
		_, err := Execute(context.Background(), Plan{Version: 1, RunID: "run-verify-plan", Jobs: []Job{fakeJob("job-001", "success", 0)}}, ExecuteOptions{ReceiptDir: t.TempDir(), Adapters: []Adapter{fakeAdapter{}}, Verifier: fakeVerifier{results: results}})
		var setup SetupError
		if !errors.As(err, &setup) {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
}

// The worker is started with exactly what it would get without a verifier.
func TestNothingAboutTheChecksReachesTheWorker(t *testing.T) {
	started := func(verifier PostconditionVerifier) Job {
		var runs atomic.Int32
		var jobs []Job
		plan := Plan{Version: 1, RunID: "run-verify-env", Jobs: []Job{fakeJob("job-001", "success", 0)}}
		if _, err := Execute(context.Background(), plan, ExecuteOptions{Workers: 1, Timeout: 10 * time.Second, ReceiptDir: t.TempDir(), Adapters: []Adapter{fakeAdapter{}}, Executor: workerRecorder{runs: &runs, jobs: &jobs}, Verifier: verifier}); err != nil || len(jobs) != 1 {
			t.Fatalf("err=%v jobs=%d", err, len(jobs))
		}
		return jobs[0]
	}
	plain, checked := started(nil), started(fakeVerifier{results: []VerificationResult{VerificationVerified}})
	if !slices.Equal(plain.Command, checked.Command) || len(plain.Env) != len(checked.Env) {
		t.Fatalf("the worker was started differently: %v / %v", plain.Command, checked.Command)
	}
	for key, value := range checked.Env {
		if plain.Env[key] != value || strings.Contains(strings.ToLower(key), "verif") {
			t.Fatalf("worker environment differs at %s", key)
		}
	}
}

// A receipt is untrusted once stored: classification never reads a passed
// receipt with an unverified or malformed check as a pass, and counts a
// contradiction as a failure only where the attempt's own evidence passed.
func TestClassifyRunNeverLetsAPassOverrideACheck(t *testing.T) {
	record := func(result VerificationResult) Verification {
		reason, status := checkReason(result)
		return Verification{ID: "check-a", Method: "GET", Result: result, Reason: reason, HTTPStatus: status, ObservedBy: VerificationObserver}
	}
	base := Receipt{Status: StatusPassed, Validated: true, Executed: true, ExecutedTests: 1}
	with := func(status ReceiptStatus, records ...Verification) Receipt {
		receipt := base
		receipt.Status, receipt.Verifications = status, records
		return receipt
	}
	goalFailed := with(StatusFailed, record(VerificationContradicted))
	goalFailed.GoalFailed = true
	exited := with(StatusFailed, record(VerificationContradicted))
	exited.ExitCode = 1
	malformed := record(VerificationVerified)
	malformed.ObservedBy = "worker"
	for name, test := range map[string]struct {
		receipt Receipt
		want    RunOutcome
	}{
		"verified":                        {with(StatusPassed, record(VerificationVerified)), OutcomePassed},
		"passed but contradicted":         {with(StatusPassed, record(VerificationContradicted)), OutcomeIncomplete},
		"passed but unreachable":          {with(StatusPassed, record(VerificationUnreachable)), OutcomeIncomplete},
		"passed but not run":              {with(StatusPassed, record(VerificationNotRun)), OutcomeIncomplete},
		"passed with a malformed record":  {with(StatusPassed, malformed), OutcomeIncomplete},
		"failed by contradiction":         {with(StatusFailed, record(VerificationContradicted)), OutcomeFailed},
		"failed without a contradiction":  {with(StatusFailed, record(VerificationVerified)), OutcomeIncomplete},
		"goal failed and contradicted":    {goalFailed, OutcomeIncomplete},
		"process failed and contradicted": {exited, OutcomeIncomplete},
		"error and unreachable":           {with(StatusError, record(VerificationUnreachable)), OutcomeIncomplete},
	} {
		if got := classifyRun(RunSummary{PlannedJobs: 1, Receipts: []Receipt{test.receipt}}, false); got != test.want {
			t.Errorf("%s: outcome %s, want %s", name, got, test.want)
		}
	}
}

func TestVerificationRecordIsClosed(t *testing.T) {
	valid := Verification{ID: "order.created-1", OutcomeID: "order-stored", Method: "GET", Result: VerificationContradicted, Reason: VerificationReasonStatus, HTTPStatus: 404, ObservedBy: VerificationObserver}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Verification){
		"url as id":            func(v *Verification) { v.ID = "http://host/orders?token=x" },
		"free text reason":     func(v *Verification) { v.Reason = "expected paid, got pending" },
		"reason of another":    func(v *Verification) { v.Reason = VerificationReasonTimeout },
		"unknown result":       func(v *Verification) { v.Result = "passed" },
		"other method":         func(v *Verification) { v.Method = "POST" },
		"worker reported":      func(v *Verification) { v.ObservedBy = "" },
		"impossible status":    func(v *Verification) { v.HTTPStatus = 7000 },
		"compared no answer":   func(v *Verification) { v.HTTPStatus = 0 },
		"outcome with a space": func(v *Verification) { v.OutcomeID = "order stored" },
	} {
		changed := valid
		mutate(&changed)
		if changed.Validate() == nil {
			t.Errorf("%s: accepted %+v", name, changed)
		}
	}
	if (Receipt{Verifications: []Verification{valid, valid}}).ValidVerifications() {
		t.Error("duplicate identifiers accepted")
	}
}
