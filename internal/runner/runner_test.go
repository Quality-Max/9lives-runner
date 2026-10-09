package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustWrite(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}

func TestBuildPlanIsDeterministicAndExplainsSkippedInputs(t *testing.T) {
	directory := t.TempDir()
	mustWrite(t, filepath.Join(directory, "b.fake"), "", 0o600)
	mustWrite(t, filepath.Join(directory, "a.fake"), "", 0o600)
	mustWrite(t, filepath.Join(directory, "notes.txt"), "", 0o600)
	options := PlanOptions{Adapters: []Adapter{fakeAdapter{}}}
	if _, err := BuildPlan([]string{filepath.Join(directory, "*.fake[")}, options); err == nil {
		t.Fatal("malformed glob should fail")
	}
	plan, err := BuildPlan([]string{filepath.Join(directory, "*.fake"), filepath.Join(directory, "notes.txt"), filepath.Join(directory, "missing*.fake")}, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Jobs) != 2 || !strings.HasSuffix(plan.Jobs[0].Spec, "job-001.fake") {
		t.Fatalf("unexpected jobs: %#v", plan.Jobs)
	}
	if len(plan.Skipped) != 2 {
		t.Fatalf("unexpected skips: %#v", plan.Skipped)
	}
}

func TestBuildPlanReservesRunWideBudget(t *testing.T) {
	directory := t.TempDir()
	mustWrite(t, filepath.Join(directory, "a.fake"), "", 0o600)
	mustWrite(t, filepath.Join(directory, "b.fake"), "", 0o600)
	plan, err := BuildPlan([]string{directory}, PlanOptions{MaxJobs: 1, Adapters: []Adapter{fakeAdapter{}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Jobs) != 1 || len(plan.Skipped) != 1 || plan.Skipped[0].Reason != "run-wide job budget exhausted" {
		t.Fatalf("unexpected budget plan: %#v", plan)
	}
}

func TestExecuteRejectsIdentifiersThatCannotBeExportedCanonically(t *testing.T) {
	tooLongRun := "run-" + strings.Repeat("a", 157)
	plan := Plan{Version: 1, RunID: tooLongRun, Jobs: []Job{fakeJob("job-001", "success", 0)}}
	if summary, err := Execute(context.Background(), plan, ExecuteOptions{ReceiptDir: t.TempDir(), Adapters: []Adapter{fakeAdapter{}}}); err == nil || summary.Complete || !strings.Contains(err.Error(), "canonical identifier limit") {
		t.Fatalf("oversized run ID was accepted: summary=%#v err=%v", summary, err)
	}
	job := fakeJob(strings.Repeat("j", 149), "success", 0)
	plan = Plan{Version: 1, RunID: "run-long-job", Jobs: []Job{job}}
	if _, err := Execute(context.Background(), plan, ExecuteOptions{ReceiptDir: t.TempDir(), Adapters: []Adapter{fakeAdapter{}}}); err == nil || !strings.Contains(err.Error(), "attempt ID exceeds") {
		t.Fatalf("oversized derived attempt ID was accepted: %v", err)
	}
	job = fakeJob("job-001", "success", 0)
	job.Adapter = strings.Repeat("a", 161)
	plan = Plan{Version: 1, RunID: "run-long-adapter", Jobs: []Job{job}}
	if _, err := Execute(context.Background(), plan, ExecuteOptions{ReceiptDir: t.TempDir(), Adapters: []Adapter{fakeAdapter{}}}); err == nil || !strings.Contains(err.Error(), "adapter revision exceeds") {
		t.Fatalf("oversized adapter revision was accepted: %v", err)
	}
}

func TestExecuteRejectsOverBudgetPlanAndBlocksFailedDependencies(t *testing.T) {
	tooMany := Plan{Version: 1, RunID: "run-over-budget", Limits: Limits{MaxJobs: 1}, Jobs: []Job{fakeJob("job-001", "success", 0), fakeJob("job-002", "success", 0)}}
	if summary, err := Execute(context.Background(), tooMany, ExecuteOptions{ReceiptDir: t.TempDir(), Adapters: []Adapter{fakeAdapter{}}}); err == nil || summary.Complete {
		t.Fatal("manually supplied plan exceeded MaxJobs")
	}
	root := t.TempDir()
	plan := Plan{Version: 1, RunID: "run-blocked-dependency", Jobs: []Job{fakeJob("first", "test-failure", 0), {ID: "second", Spec: "second.fake", Adapter: "fake", Command: []string{"not-run"}, DependsOn: []string{"first"}}}}
	summary, _ := Execute(context.Background(), plan, ExecuteOptions{ReceiptDir: root, Adapters: []Adapter{fakeAdapter{}}})
	if len(summary.Receipts) != 2 || summary.Receipts[1].Status != StatusError || !strings.Contains(summary.Receipts[1].Error, "first") {
		t.Fatalf("failed dependency was not durably blocked: %#v", summary)
	}
	// The prerequisite proved a failure, but its descendant never ran. The
	// full plan remains incomplete and the failing receipt remains visible.
	if summary.Outcome != OutcomeIncomplete || summary.Complete || summary.Failed != 1 || !summary.Receipts[0].Validated || summary.Receipts[1].Executed || summary.Receipts[1].Validated {
		t.Fatalf("blocked jobs must not claim executed evidence: %#v", summary)
	}
}

type forgedIdentityStore struct{ Store }

func (store forgedIdentityStore) PersistAttempt(receipt Receipt, stdout, stderr []byte) (Receipt, error) {
	persisted, err := store.Store.PersistAttempt(receipt, stdout, stderr)
	persisted.RunID, persisted.JobID, persisted.AttemptID, persisted.Attempt = "run-other", "unreserved", "unreserved-attempt", 99
	return persisted, err
}

type skippedOnlyAdapter struct{ fakeAdapter }

func (skippedOnlyAdapter) Validate([]byte) (Validation, error) {
	return Validation{ExecutedTests: 0, SkippedTests: 1, Description: "forged skipped-only report"}, nil
}

func TestExecuteRejectsSkippedOnlyGenericAdapterReport(t *testing.T) {
	plan := Plan{Version: 1, RunID: "run-skipped-generic", Jobs: []Job{fakeJob("job-001", "success", 0)}}
	summary, err := Execute(context.Background(), plan, ExecuteOptions{ReceiptDir: t.TempDir(), Adapters: []Adapter{skippedOnlyAdapter{}}})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Complete || summary.Passed != 0 || summary.Errors != 1 || !strings.Contains(summary.Receipts[0].Error, "no executed tests") {
		t.Fatalf("skipped-only adapter report was accepted: %#v", summary)
	}
}

func TestExecuteRejectsMalformedGenericAdapterCounts(t *testing.T) {
	cases := []struct {
		name, want string
		validation Validation
	}{
		{name: "overflow", want: "overflow", validation: Validation{ExecutedTests: int(^uint(0) >> 1), SkippedTests: 1}},
		{name: "negative assertions", want: "negative verified assertions", validation: Validation{ExecutedTests: 1, VerifiedAssertions: -1, AssertionCoverage: "known"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			plan := Plan{Version: 1, RunID: "run-malformed-" + strings.ReplaceAll(test.name, " ", "-"), Jobs: []Job{fakeJob("job-001", "success", 0)}}
			summary, err := Execute(context.Background(), plan, ExecuteOptions{ReceiptDir: t.TempDir(), Adapters: []Adapter{validationAdapter{Validation: test.validation}}})
			if err != nil {
				t.Fatal(err)
			}
			if summary.Complete || summary.Passed != 0 || summary.Errors != 1 || len(summary.Receipts) != 1 || summary.Receipts[0].Status != StatusError || !strings.Contains(summary.Receipts[0].Error, test.want) {
				t.Fatalf("malformed adapter report was accepted: %#v", summary)
			}
		})
	}
}

type validationAdapter struct {
	fakeAdapter
	Validation
}

func (adapter validationAdapter) Validate([]byte) (Validation, error) { return adapter.Validation, nil }

func TestExecuteRejectsForgedPersistedReceiptIdentity(t *testing.T) {
	root := t.TempDir()
	plan := Plan{Version: 1, RunID: "run-identity", Jobs: []Job{fakeJob("job-001", "success", 0)}}
	summary, err := Execute(context.Background(), plan, ExecuteOptions{ReceiptDir: root, Adapters: []Adapter{fakeAdapter{}}, Store: forgedIdentityStore{newRunStore(root, plan.RunID)}})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Complete || summary.Passed != 0 || summary.Errors != 1 || len(summary.Receipts) != 1 || !strings.Contains(summary.Receipts[0].Error, "unreserved identity") {
		t.Fatalf("forged persisted identity was accepted: %#v", summary)
	}
}

func TestControlledEnvironmentAndURLCredentialRedaction(t *testing.T) {
	environment := strings.Join(controlledEnvironment([]string{"PATH=/bin", "QUA2042_SENTINEL=private"}, map[string]string{"NINELIVES_RUN_ID": "run-safe"}), "\n")
	if strings.Contains(environment, "SENTINEL") || !strings.Contains(environment, "NINELIVES_RUN_ID=run-safe") {
		t.Fatalf("controlled environment leaked or omitted values: %q", environment)
	}
	if redacted := string(redact([]byte("DATABASE_URL=postgres://user:private-value@example.test/db"))); strings.Contains(redacted, "private-value") {
		t.Fatalf("URL credential was not redacted: %q", redacted)
	}
}

func TestExecuteCancellationAccountsForEveryReservedJob(t *testing.T) {
	plan := Plan{Version: 1, RunID: "run-canceled", Jobs: []Job{
		{ID: "job-001", Spec: "a.fake", Adapter: "fake", Command: []string{"not-started"}, DependsOn: []string{}},
		{ID: "job-002", Spec: "b.fake", Adapter: "fake", Command: []string{"not-started"}, DependsOn: []string{}},
		{ID: "job-003", Spec: "c.fake", Adapter: "fake", Command: []string{"not-started"}, DependsOn: []string{}},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	summary, err := Execute(ctx, plan, ExecuteOptions{Workers: 1, ReceiptDir: t.TempDir(), Adapters: []Adapter{fakeAdapter{}}})
	if err == nil {
		t.Fatal("expected canceled context")
	}
	if len(summary.Receipts) != len(plan.Jobs) || summary.Canceled != len(plan.Jobs) || summary.Complete {
		t.Fatalf("canceled jobs were lost: %#v", summary)
	}
}

func TestRunIDsAndPlanEnvironmentAreSafeToPersist(t *testing.T) {
	if err := RequestCancel(t.TempDir(), "../../outside"); err == nil {
		t.Fatal("path-traversing run ID was accepted")
	}
	plan := Plan{Version: 1, RunID: "run-safe", Jobs: []Job{{ID: "job-001", Env: map[string]string{"API_KEY": "top-secret"}}}}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "top-secret") {
		t.Fatalf("environment secret leaked into plan: %s", raw)
	}
	redacted := string(redact([]byte(`{"token":"top-secret","password":"also-secret"}`)))
	if strings.Contains(redacted, "top-secret") || strings.Contains(redacted, "also-secret") {
		t.Fatalf("JSON secret was not redacted: %s", redacted)
	}
}

func TestRunAndJobIdentitiesCannotBeReusedOrTraverse(t *testing.T) {
	root := t.TempDir()
	store := newRunStore(root, "run-once")
	if err := store.Initialize(Plan{Version: 1, RunID: "run-once"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize(Plan{Version: 1, RunID: "run-once"}); err == nil {
		t.Fatal("run identity was reused")
	}
	if _, _, err := orderJobs([]Job{{ID: "../../outside"}}); err == nil {
		t.Fatal("path-traversing job ID was accepted")
	}
}
