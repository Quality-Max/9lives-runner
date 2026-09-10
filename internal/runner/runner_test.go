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
