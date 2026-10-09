package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type finalizeFailureStore struct {
	*runStore
	beats atomic.Int64
}

func (store *finalizeFailureStore) Finalize(RunSummary) error {
	return errors.New("injected result persistence failure")
}

func (store *finalizeFailureStore) Heartbeat() error {
	store.beats.Add(1)
	return store.runStore.Heartbeat()
}

func TestFinalizeFailureStopsHeartbeatWatcher(t *testing.T) {
	root := t.TempDir()
	plan := Plan{Version: 1, RunID: "run-finalize-failure", Jobs: []Job{fakeJob("job-001", "success", 0)}}
	store := &finalizeFailureStore{runStore: newRunStore(root, plan.RunID)}
	if _, err := Execute(context.Background(), plan, ExecuteOptions{ReceiptDir: root, Store: store, Adapters: []Adapter{fakeAdapter{}}}); err == nil {
		t.Fatal("expected finalization failure")
	}
	before := store.beats.Load()
	time.Sleep(heartbeatInterval + 200*time.Millisecond)
	if after := store.beats.Load(); after != before {
		status, _ := LoadStatus(root, plan.RunID)
		t.Fatalf("heartbeat survived Execute return: before=%d after=%d status=%s", before, after, status.State)
	}
}

func TestRunDeadlineTimesOutQueuedJobsInsteadOfCancelingThem(t *testing.T) {
	plan := Plan{Version: 1, RunID: "run-deadline-queued", Jobs: []Job{fakeJob("job-001", "sleep", 5000), fakeJob("job-002", "success", 0)}}
	summary, _ := Execute(context.Background(), plan, ExecuteOptions{Workers: 1, Timeout: 10 * time.Second, RunDeadline: 100 * time.Millisecond, ReceiptDir: t.TempDir(), Adapters: []Adapter{fakeAdapter{}}})
	if len(summary.Receipts) != 2 || summary.TimedOut != 2 || summary.Canceled != 0 {
		t.Fatalf("run deadline reported as user cancellation: %#v", summary)
	}
	if queued := summary.Receipts[1]; queued.Executed || queued.Status != StatusTimedOut {
		t.Fatalf("queued job was not timed out: %#v", queued)
	}
}

func TestCanceledDependencyPlanReportsDescendantsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	second := fakeJob("second", "success", 0)
	second.DependsOn = []string{"first"}
	plan := Plan{Version: 1, RunID: "run-dependency-cancel", Jobs: []Job{fakeJob("first", "sleep", 5000), second}}
	summary, _ := Execute(ctx, plan, ExecuteOptions{ReceiptDir: t.TempDir(), Adapters: []Adapter{fakeAdapter{}}})
	if len(summary.Receipts) != 2 || summary.Canceled != 2 || summary.Errors != 0 {
		t.Fatalf("descendant of a canceled job was not reported canceled: %#v", summary)
	}
}

// collidingStore makes one receipt file unwritable for the first attempt.
type collidingStore struct {
	*runStore
	name string
}

func (store collidingStore) Initialize(plan Plan) error {
	if err := store.runStore.Initialize(plan); err != nil {
		return err
	}
	return os.MkdirAll(filepath.Join(store.root, plan.RunID, "job-001", "job-001-attempt-001", store.name, "occupied"), 0o700)
}

func TestReceiptFilesNeverDisagreeWhenPersistenceFails(t *testing.T) {
	for _, name := range []string{"execution-receipt-1.0.json", "receipt.json"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			plan := Plan{Version: 1, RunID: "run-persist-" + strings.TrimSuffix(strings.ReplaceAll(name, ".", "-"), "-json"), Jobs: []Job{fakeJob("job-001", "success", 0)}}
			store := collidingStore{runStore: newRunStore(root, plan.RunID), name: name}
			summary, _ := Execute(context.Background(), plan, ExecuteOptions{MaxAttempts: 1, ReceiptDir: root, Store: store, Adapters: []Adapter{fakeAdapter{}}})
			if summary.Receipts[0].Status != StatusError || !strings.Contains(summary.Receipts[0].Error, "could not persist") {
				t.Fatalf("persistence failure was not reported: %#v", summary.Receipts[0])
			}
			attempt := filepath.Join(root, plan.RunID, "job-001", "job-001-attempt-001")
			for _, file := range []string{"execution-receipt-1.0.json", "receipt.json"} {
				if file == name {
					continue
				}
				if _, err := os.Stat(filepath.Join(attempt, file)); err == nil {
					t.Fatalf("%s claims a result although the attempt failed to persist", file)
				}
			}
		})
	}
}

// stubbornExecutor ignores cancellation for a while, like a process group
// being escalated from SIGTERM to SIGKILL.
type stubbornExecutor struct{ hold time.Duration }

func (executor stubbornExecutor) Run(ctx context.Context, job Job, _ int) ProcessOutput {
	<-ctx.Done()
	time.Sleep(executor.hold)
	return ProcessOutput{Executed: true, ExitCode: -1, Err: ctx.Err()}
}

func TestHeartbeatContinuesWhileTerminatingAfterDeadline(t *testing.T) {
	root := t.TempDir()
	plan := Plan{Version: 1, RunID: "run-heartbeat-cleanup", Jobs: []Job{fakeJob("job-001", "sleep", 0)}}
	deadline := 50 * time.Millisecond
	started := time.Now()
	done := make(chan struct{})
	go func() {
		_, _ = Execute(context.Background(), plan, ExecuteOptions{RunDeadline: deadline, Timeout: time.Minute, ReceiptDir: root, Executor: stubbornExecutor{hold: 2500 * time.Millisecond}, Adapters: []Adapter{fakeAdapter{}}})
		close(done)
	}()
	time.Sleep(1600 * time.Millisecond)
	info, err := os.Stat(filepath.Join(root, plan.RunID, "heartbeat"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().After(started.Add(deadline + heartbeatInterval/2)) {
		t.Fatalf("heartbeat stopped when the run deadline expired: last beat %v after start", info.ModTime().Sub(started))
	}
	if status, err := LoadStatus(root, plan.RunID); err != nil || status.State != "running" {
		t.Fatalf("live run reported %q: %v", status.State, err)
	}
	<-done
	events, err := readEvents(filepath.Join(root, plan.RunID, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Type == "run_heartbeat" {
			t.Fatal("heartbeats must not grow the event log")
		}
	}
}

func TestStatusReportsInterruptedOnlyAfterStaleHeartbeat(t *testing.T) {
	root := t.TempDir()
	store := newRunStore(root, "run-stale")
	if err := store.Initialize(Plan{Version: 1, RunID: "run-stale"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Heartbeat(); err != nil {
		t.Fatal(err)
	}
	if status, _ := LoadStatus(root, "run-stale"); status.State != "running" {
		t.Fatalf("fresh heartbeat reported %q", status.State)
	}
	stale := time.Now().Add(-2 * heartbeatStaleAfter)
	for _, name := range []string{"heartbeat", "plan.json"} {
		if err := os.Chtimes(filepath.Join(root, "run-stale", name), stale, stale); err != nil {
			t.Fatal(err)
		}
	}
	if status, _ := LoadStatus(root, "run-stale"); status.State != "interrupted" {
		t.Fatalf("stale heartbeat reported %q", status.State)
	}
}

func TestControlledEnvironmentKeepsRuntimeAndForwardsNamedVariables(t *testing.T) {
	t.Setenv("QUA2042_BASE_URL", "https://staging.example.test")
	base := []string{"PATH=/bin", "PLAYWRIGHT_BROWSERS_PATH=/opt/pw", "DISPLAY=:0", "https_proxy=http://proxy:3128", "XDG_CACHE_HOME=/cache", "LC_CTYPE=UTF-8", "DATABASE_URL=postgres://private", "path=/lowercase"}
	environment := "\n" + strings.Join(controlledEnvironment(base, passedEnvironment([]string{"QUA2042_BASE_URL", "QUA2042_UNSET"})), "\n") + "\n"
	for _, want := range []string{"PLAYWRIGHT_BROWSERS_PATH=/opt/pw", "DISPLAY=:0", "https_proxy=http://proxy:3128", "XDG_CACHE_HOME=/cache", "LC_CTYPE=UTF-8", "QUA2042_BASE_URL=https://staging.example.test"} {
		if !strings.Contains(environment, "\n"+want+"\n") {
			t.Fatalf("missing %s in %q", want, environment)
		}
	}
	unwantedKeys := []string{"DATABASE_URL", "QUA2042_UNSET"}
	if runtime.GOOS != "windows" {
		unwantedKeys = append(unwantedKeys, "path=")
	}
	for _, unwanted := range unwantedKeys {
		if strings.Contains(environment, "\n"+unwanted) {
			t.Fatalf("%s leaked into %q", unwanted, environment)
		}
	}
}

type recordingExecutor struct{ env chan map[string]string }

func (executor recordingExecutor) Run(_ context.Context, job Job, _ int) ProcessOutput {
	executor.env <- job.Env
	return ProcessOutput{Executed: true, ExitCode: 0, Stdout: []byte(`{"result":"passed"}`)}
}

func TestExecuteForwardsOnlyNamedCallerVariables(t *testing.T) {
	t.Setenv("QUA2042_FORWARDED", "yes")
	t.Setenv("QUA2042_PRIVATE", "no")
	executor := recordingExecutor{env: make(chan map[string]string, 1)}
	plan := Plan{Version: 1, RunID: "run-pass-env", Jobs: []Job{fakeJob("job-001", "success", 0)}}
	_, _ = Execute(context.Background(), plan, ExecuteOptions{PassEnv: []string{"QUA2042_FORWARDED"}, ReceiptDir: t.TempDir(), Executor: executor, Adapters: []Adapter{fakeAdapter{}}})
	env := <-executor.env
	if env["QUA2042_FORWARDED"] != "yes" || env["QUA2042_PRIVATE"] != "" || env["NINELIVES_RUN_ID"] != plan.RunID {
		t.Fatalf("unexpected job environment: %v", env)
	}
}
