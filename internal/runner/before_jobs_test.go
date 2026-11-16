package runner

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// countingExecutor fails the test's expectations if any job process starts.
type countingExecutor struct{ runs *atomic.Int32 }

func (executor countingExecutor) Run(ctx context.Context, job Job, attempt int) ProcessOutput {
	executor.runs.Add(1)
	return localProcessExecutor{}.Run(ctx, job, attempt)
}

func beforeJobsPlan(runID string) Plan {
	return Plan{Version: 1, RunID: runID, Jobs: []Job{fakeJob("job-001", "success", 0), fakeJob("job-002", "success", 0)}}
}

// A pre-start failure while the run is live starts no process, leaves an
// error receipt per planned job and is stored, so `9l result` reads it back.
func TestBeforeJobsFailureStoresNotStartedReceipts(t *testing.T) {
	root := t.TempDir()
	var runs atomic.Int32
	plan := beforeJobsPlan("run-before-jobs-failed")
	summary, err := Execute(context.Background(), plan, ExecuteOptions{ReceiptDir: root, Executor: countingExecutor{&runs}, Adapters: []Adapter{fakeAdapter{}}, MaxAttempts: 3,
		BeforeJobs: func(context.Context) error { return errors.New("target not ready") }})
	if err != nil || runs.Load() != 0 || summary.Outcome != OutcomeIncomplete || summary.Errors != 2 || len(summary.Receipts) != 2 {
		t.Fatalf("err=%v runs=%d summary=%+v", err, runs.Load(), summary)
	}
	for _, receipt := range summary.Receipts {
		if receipt.Status != StatusError || receipt.Validated || receipt.Attempt != 1 || receipt.Error != "not started: target not ready" || receipt.ReceiptPath == "" {
			t.Fatalf("receipt=%+v", receipt)
		}
	}
	stored, err := LoadResult(root, plan.RunID)
	if err != nil || stored.Outcome != OutcomeIncomplete || len(stored.Receipts) != 2 || stored.Receipts[0].Error != "not started: target not ready" {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}

// Cancellation during the pre-start phase reports the run canceled, like any
// canceled run, not as a pre-start failure.
func TestBeforeJobsObservesCancellationRequests(t *testing.T) {
	root := t.TempDir()
	var runs atomic.Int32
	plan := beforeJobsPlan("run-before-jobs-canceled")
	waiting := make(chan struct{})
	go func() {
		<-waiting
		if err := RequestCancel(root, plan.RunID); err != nil {
			t.Error(err)
		}
	}()
	summary, err := Execute(context.Background(), plan, ExecuteOptions{ReceiptDir: root, Executor: countingExecutor{&runs}, Adapters: []Adapter{fakeAdapter{}},
		BeforeJobs: func(ctx context.Context) error {
			close(waiting)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(30 * time.Second):
				return errors.New("cancellation was not observed")
			}
		}})
	if !errors.Is(err, context.Canceled) || runs.Load() != 0 || summary.Outcome != OutcomeIncomplete || summary.Canceled != 2 {
		t.Fatalf("err=%v runs=%d summary=%+v", err, runs.Load(), summary)
	}
	for _, receipt := range summary.Receipts {
		if strings.Contains(receipt.Error, "not started") {
			t.Fatalf("a canceled run was reported as a pre-start failure: %+v", receipt)
		}
	}
}

// The run deadline covers the pre-start phase.
func TestRunDeadlineCoversBeforeJobs(t *testing.T) {
	var runs atomic.Int32
	started := time.Now()
	summary, err := Execute(context.Background(), beforeJobsPlan("run-before-jobs-deadline"), ExecuteOptions{ReceiptDir: t.TempDir(), RunDeadline: 300 * time.Millisecond, Executor: countingExecutor{&runs}, Adapters: []Adapter{fakeAdapter{}},
		BeforeJobs: func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(30 * time.Second):
				return errors.New("deadline was not applied")
			}
		}})
	if !errors.Is(err, context.DeadlineExceeded) || runs.Load() != 0 || summary.TimedOut != 2 || time.Since(started) > 10*time.Second {
		t.Fatalf("err=%v runs=%d summary=%+v", err, runs.Load(), summary)
	}
}
