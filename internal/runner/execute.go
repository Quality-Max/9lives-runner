package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

type ExecuteOptions struct {
	Workers        int
	Timeout        time.Duration
	RunDeadline    time.Duration
	MaxAttempts    int
	MaxOutputBytes int
	ReceiptDir     string
	Adapters       []Adapter
	Store          Store
	Executor       ProcessExecutor
}

func Execute(ctx context.Context, plan Plan, opts ExecuteOptions) (RunSummary, error) {
	started := time.Now().UTC()
	summary := RunSummary{RunID: plan.RunID, StartedAt: started, Complete: len(plan.Jobs) > 0 && len(plan.Skipped) == 0, Receipts: []Receipt{}}
	if err := validateRunID(plan.RunID); err != nil {
		return summary, err
	}
	orderedJobs, hasDependencies, err := orderJobs(plan.Jobs)
	if err != nil {
		return summary, err
	}
	if opts.Workers < 1 {
		opts.Workers = max(1, plan.Limits.MaxParallel)
	}
	if plan.Limits.MaxParallel > 0 && opts.Workers > plan.Limits.MaxParallel {
		opts.Workers = plan.Limits.MaxParallel
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Minute
	}
	if opts.MaxAttempts < 1 {
		opts.MaxAttempts = max(1, plan.Limits.MaxAttempts)
	}
	if plan.Limits.MaxAttempts > 0 && opts.MaxAttempts > plan.Limits.MaxAttempts {
		opts.MaxAttempts = plan.Limits.MaxAttempts
	}
	if opts.MaxOutputBytes < 1 {
		opts.MaxOutputBytes = max(1024, plan.Limits.MaxOutputBytes)
	}
	if plan.Limits.MaxOutputBytes > 0 && opts.MaxOutputBytes > plan.Limits.MaxOutputBytes {
		opts.MaxOutputBytes = plan.Limits.MaxOutputBytes
	}
	plannedDeadline := time.Duration(plan.Limits.DeadlineMS) * time.Millisecond
	if opts.RunDeadline <= 0 || (plannedDeadline > 0 && opts.RunDeadline > plannedDeadline) {
		opts.RunDeadline = plannedDeadline
	}
	if opts.ReceiptDir == "" {
		opts.ReceiptDir = ".9lives/receipts"
	}
	// The initial scheduler takes the conservative path for dependency graphs:
	// topologically ordered jobs execute serially. Independent plans retain
	// bounded parallelism. QUA-1925 owns the later durable DAG scheduler.
	if hasDependencies {
		opts.Workers = 1
	}
	store := opts.Store
	if store == nil {
		store = newRunStore(opts.ReceiptDir, plan.RunID)
	}
	executor := opts.Executor
	if executor == nil {
		executor = localProcessExecutor{}
	}
	if err := store.Initialize(plan); err != nil {
		return summary, err
	}
	if err := store.AppendEvent(ProgressEvent{Type: "run_started"}); err != nil {
		return summary, err
	}
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	if opts.RunDeadline > 0 {
		var cancelDeadline context.CancelFunc
		runCtx, cancelDeadline = context.WithTimeout(runCtx, opts.RunDeadline)
		defer cancelDeadline()
	}
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if store.CancellationRequested() {
					_ = store.AppendEvent(ProgressEvent{Type: "cancellation_observed"})
					cancelRun()
					return
				}
			}
		}
	}()

	// Queue every reserved job. Cancellation therefore produces terminal
	// canceled receipts for jobs that had not started yet instead of dropping
	// them from the accounting.
	jobs := make(chan Job, len(orderedJobs))
	for _, job := range orderedJobs {
		jobs <- job
	}
	close(jobs)
	receipts := make(chan Receipt, len(plan.Jobs))

	var workers sync.WaitGroup
	for range min(opts.Workers, max(1, len(plan.Jobs))) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobs {
				receipts <- executeJob(runCtx, plan.RunID, job, opts, store, executor)
			}
		}()
	}
	go func() { workers.Wait(); close(receipts) }()
	for receipt := range receipts {
		summary.Receipts = append(summary.Receipts, receipt)
	}

	sort.Slice(summary.Receipts, func(i, j int) bool { return summary.Receipts[i].JobID < summary.Receipts[j].JobID })
	for _, receipt := range summary.Receipts {
		switch receipt.Status {
		case StatusPassed:
			summary.Passed++
		case StatusFailed:
			summary.Failed++
		case StatusCanceled:
			summary.Canceled++
		case StatusError:
			summary.Errors++
		}
		if receipt.Status != StatusPassed {
			summary.Complete = false
		}
	}
	if len(summary.Receipts) != len(plan.Jobs) {
		summary.Complete = false
	}
	summary.FinishedAt = time.Now().UTC()
	summary.DurationMS = summary.FinishedAt.Sub(started).Milliseconds()
	if err := store.Finalize(summary); err != nil {
		return summary, err
	}
	_ = store.AppendEvent(ProgressEvent{Type: "run_finished"})
	finalErr := runCtx.Err()
	cancelRun()
	<-watchDone
	return summary, finalErr
}

func orderJobs(jobs []Job) ([]Job, bool, error) {
	byID := make(map[string]Job, len(jobs))
	hasDependencies := false
	for _, job := range jobs {
		if !validComponent.MatchString(job.ID) {
			return nil, false, fmt.Errorf("planned job has invalid ID %q", job.ID)
		}
		if _, exists := byID[job.ID]; exists {
			return nil, false, fmt.Errorf("duplicate planned job ID %q", job.ID)
		}
		byID[job.ID] = job
		hasDependencies = hasDependencies || len(job.DependsOn) > 0
	}
	for _, job := range jobs {
		for _, dependency := range job.DependsOn {
			if _, ok := byID[dependency]; !ok {
				return nil, false, fmt.Errorf("job %q depends on unknown job %q", job.ID, dependency)
			}
		}
	}
	ordered := make([]Job, 0, len(jobs))
	done := map[string]bool{}
	for len(ordered) < len(jobs) {
		progress := false
		for _, job := range jobs {
			if done[job.ID] {
				continue
			}
			ready := true
			for _, dependency := range job.DependsOn {
				if !done[dependency] {
					ready = false
					break
				}
			}
			if ready {
				ordered = append(ordered, job)
				done[job.ID], progress = true, true
			}
		}
		if !progress {
			return nil, false, fmt.Errorf("planned job dependencies contain a cycle")
		}
	}
	return ordered, hasDependencies, nil
}

func executeJob(parent context.Context, runID string, job Job, opts ExecuteOptions, store Store, executor ProcessExecutor) Receipt {
	var receipt Receipt
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		receipt = executeAttempt(parent, runID, job, attempt, opts, store, executor)
		if receipt.Status != StatusError || parent.Err() != nil {
			break
		}
	}
	return receipt
}

func executeAttempt(parent context.Context, runID string, job Job, attempt int, opts ExecuteOptions, store Store, executor ProcessExecutor) Receipt {
	started := time.Now().UTC()
	receipt := Receipt{
		Version: 1, RunID: runID, JobID: job.ID, Spec: job.Spec,
		Attempt: attempt, AttemptID: fmt.Sprintf("%s-attempt-%03d", job.ID, attempt),
		Adapter: job.Adapter, StartedAt: started, ExitCode: -1,
		Evidence: Evidence{Command: redactArguments(job.Command), Artifacts: []ArtifactReference{}},
	}
	if runID == "" || job.ID == "" || len(job.Command) == 0 {
		receipt.Status, receipt.Error = StatusError, "invalid planned job"
		return persistAttempt(receipt, store, nil, nil)
	}
	if parent.Err() != nil {
		receipt.Status, receipt.Error = StatusCanceled, parent.Err().Error()
		receipt.Termination = terminationFor(parent.Err())
		return persistAttempt(receipt, store, nil, nil)
	}
	_ = store.AppendEvent(ProgressEvent{Type: "attempt_started", JobID: job.ID, AttemptID: receipt.AttemptID})

	ctx, cancel := context.WithTimeout(parent, opts.Timeout)
	defer cancel()
	job.Env = mergeMaps(job.Env, map[string]string{"CI": "1", "NINELIVES_RUN_ID": runID, "NINELIVES_ATTEMPT_ID": receipt.AttemptID})
	output := executor.Run(ctx, job, opts.MaxOutputBytes)
	receipt.Executed, receipt.ExitCode = output.Executed, output.ExitCode
	if ctx.Err() != nil {
		receipt.Status, receipt.Error = StatusCanceled, ctx.Err().Error()
		receipt.Termination = terminationFor(ctx.Err())
	} else if output.Err != nil && output.ExitCode < 0 {
		receipt.Status, receipt.Error = StatusError, output.Err.Error()
	}

	if receipt.Status == "" {
		adapter := adapterNamed(opts.Adapters, job.Adapter)
		if adapter == nil {
			receipt.Status, receipt.Error = StatusError, "planned adapter is not registered"
		} else {
			validation, validationErr := adapter.Validate(output.Stdout)
			if validationErr != nil {
				receipt.Status, receipt.Error, receipt.Validation = StatusError, validationErr.Error(), "missing or invalid structured report"
			} else {
				receipt.Validated = true
				receipt.FailureCount = validation.FailureCount
				receipt.ExecutedTests = validation.ExecutedTests
				receipt.VerifiedAssertions = validation.VerifiedAssertions
				receipt.AssertionCoverage = validation.AssertionCoverage
				receipt.Validation = validation.Description
				if receipt.ExitCode == 0 && validation.FailureCount == 0 {
					receipt.Status = StatusPassed
				} else if receipt.ExitCode != 0 && validation.FailureCount == 0 {
					receipt.Status = StatusError
					receipt.Error = fmt.Sprintf("test process exited %d without a reported test failure", receipt.ExitCode)
				} else {
					receipt.Status = StatusFailed
				}
			}
		}
	}
	receipt.Evidence.StdoutTruncated = output.StdoutTruncated
	receipt.Evidence.StderrTruncated = output.StderrTruncated
	receipt = persistAttempt(receipt, store, output.Stdout, output.Stderr)
	_ = store.AppendEvent(ProgressEvent{Type: "attempt_finished", JobID: job.ID, AttemptID: receipt.AttemptID, Detail: string(receipt.Status)})
	return receipt
}

func terminationFor(err error) *Termination {
	if errors.Is(err, context.DeadlineExceeded) {
		return &Termination{Kind: "timeout", Detail: err.Error(), Signal: "SIGTERM", EscalationSignal: "SIGKILL", EscalationAfterMS: 2000}
	}
	return &Termination{Kind: "canceled", Detail: err.Error(), Signal: "SIGTERM", EscalationSignal: "SIGKILL", EscalationAfterMS: 2000}
}

func persistAttempt(receipt Receipt, store Store, stdout, stderr []byte) Receipt {
	receipt.FinishedAt = time.Now().UTC()
	receipt.DurationMS = receipt.FinishedAt.Sub(receipt.StartedAt).Milliseconds()
	persisted, err := store.PersistAttempt(receipt, stdout, stderr)
	if err != nil {
		receipt.Error = joinError(receipt.Error, "could not persist attempt: "+err.Error())
		receipt.Status = StatusError
		return receipt
	}
	return persisted
}

func joinError(left, right string) string {
	if left == "" {
		return right
	}
	return left + "; " + right
}

type localProcessExecutor struct{}

func (localProcessExecutor) Run(ctx context.Context, job Job, outputLimit int) ProcessOutput {
	result := ProcessOutput{ExitCode: -1}
	command := exec.Command(job.Command[0], job.Command[1:]...)
	command.Dir = job.WorkDir
	command.Env = controlledEnvironment(os.Environ(), job.Env)
	stdout, stderr := newLimitedBuffer(outputLimit), newLimitedBuffer(outputLimit)
	command.Stdout, command.Stderr = stdout, stderr
	err := startAndWait(ctx, command)
	result.Executed = command.Process != nil
	result.Stdout, result.Stderr = stdout.Bytes(), stderr.Bytes()
	result.StdoutTruncated, result.StderrTruncated = stdout.truncated, stderr.truncated
	result.Err = err
	if err == nil {
		result.ExitCode = 0
	} else {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			result.ExitCode = exitError.ExitCode()
		}
	}
	return result
}

func mergeMaps(inputs ...map[string]string) map[string]string {
	result := map[string]string{}
	for _, input := range inputs {
		for key, value := range input {
			result[key] = value
		}
	}
	return result
}

type limitedBuffer struct {
	data      []byte
	truncated bool
	limit     int
}

func newLimitedBuffer(limit int) *limitedBuffer { return &limitedBuffer{limit: limit} }

func (buffer *limitedBuffer) Write(payload []byte) (int, error) {
	originalLength := len(payload)
	remaining := buffer.limit - len(buffer.data)
	if remaining > 0 {
		if len(payload) > remaining {
			payload, buffer.truncated = payload[:remaining], true
		}
		buffer.data = append(buffer.data, payload...)
	} else if len(payload) > 0 {
		buffer.truncated = true
	}
	return originalLength, nil
}

func (buffer *limitedBuffer) Bytes() []byte { return buffer.data }

func controlledEnvironment(base []string, overlays ...map[string]string) []string {
	values := map[string]string{}
	for _, item := range base {
		if key, value, ok := strings.Cut(item, "="); ok {
			values[key] = value
		}
	}
	for _, overlay := range overlays {
		for key, value := range overlay {
			values[key] = value
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}
