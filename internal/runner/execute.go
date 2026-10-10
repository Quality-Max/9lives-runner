package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type ExecuteOptions struct {
	// OnStarted runs after durable initialization, before any jobs start.
	OnStarted       func()
	AgentProvenance *AgentProvenance
	Workers         int
	Timeout         time.Duration
	RunDeadline     time.Duration
	MaxAttempts     int
	MaxOutputBytes  int
	// PassEnv names caller variables forwarded to test processes in addition
	// to the inherited runtime environment.
	PassEnv    []string
	ReceiptDir string
	Adapters   []Adapter
	Store      Store
	Executor   ProcessExecutor
	Services   AttemptServiceFactory
}

// SetupError is a plan or option problem found before any process starts.
// The caller can fix it by changing the invocation, so the CLI reports it as a
// usage error and writes no run result.
type SetupError struct{ Err error }

func (err SetupError) Error() string { return err.Err.Error() }
func (err SetupError) Unwrap() error { return err.Err }

func Execute(ctx context.Context, plan Plan, opts ExecuteOptions) (RunSummary, error) {
	started := time.Now().UTC()
	summary := RunSummary{Version: RunSummaryVersion, Outcome: OutcomeIncomplete, RunID: plan.RunID, StartedAt: started, Complete: len(plan.Jobs) > 0 && len(plan.Skipped) == 0, PlannedJobs: len(plan.Jobs), SkippedInputs: len(plan.Skipped), Receipts: []Receipt{}}
	if err := validateRunID(plan.RunID); err != nil {
		summary.Complete = false
		return summary, SetupError{err}
	}
	if opts.AgentProvenance != nil {
		raw, _ := json.Marshal(opts.AgentProvenance)
		if _, err := ParseAgentProvenance(raw); err != nil || len(plan.Jobs) != 1 || len(plan.Skipped) != 0 {
			summary.Complete = false
			return summary, SetupError{errors.New("agent provenance requires one exact spec and a valid creation record")}
		}
	}
	if plan.Limits.MaxJobs > 0 && len(plan.Jobs) > plan.Limits.MaxJobs {
		summary.Complete = false
		return summary, SetupError{fmt.Errorf("planned jobs (%d) exceed run-wide job budget (%d)", len(plan.Jobs), plan.Limits.MaxJobs)}
	}
	orderedJobs, hasDependencies, err := orderJobs(plan.Jobs)
	if err != nil {
		return summary, SetupError{err}
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
	if err := validateCanonicalPlanIdentifiers(plan, opts.MaxAttempts); err != nil {
		summary.Complete = false
		return summary, SetupError{err}
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
	if opts.OnStarted != nil {
		opts.OnStarted()
	}
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	if opts.RunDeadline > 0 {
		var cancelDeadline context.CancelFunc
		runCtx, cancelDeadline = context.WithTimeout(runCtx, opts.RunDeadline)
		defer cancelDeadline()
	}
	// The heartbeat outlives runCtx: after a deadline or cancellation the
	// runner is still terminating process groups and persisting receipts, and
	// must not look interrupted to `9l status` while it does.
	beat := func() {}
	if heartbeats, ok := store.(heartbeatStore); ok {
		beat = func() { _ = heartbeats.Heartbeat() }
	}
	beat()
	watchDone, stopWatch := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(watchDone)
		ticker := time.NewTicker(50 * time.Millisecond)
		heartbeat := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		defer heartbeat.Stop()
		for {
			select {
			case <-stopWatch:
				return
			case <-ticker.C:
				if runCtx.Err() == nil && store.CancellationRequested() {
					_ = store.AppendEvent(ProgressEvent{Type: "cancellation_observed"})
					cancelRun()
				}
			case <-heartbeat.C:
				beat()
			}
		}
	}()
	defer func() {
		cancelRun()
		close(stopWatch)
		<-watchDone
	}()

	// Dependency plans are deliberately serial. A failed prerequisite produces
	// a durable blocked receipt for descendants rather than running them.
	if hasDependencies {
		completed := map[string]Receipt{}
		for _, job := range orderedJobs {
			if runCtx.Err() != nil {
				// A canceled or expired run reports descendants as canceled or
				// timed out, not as blocked by the prerequisite it interrupted.
				receipt := executeJob(runCtx, plan.RunID, job, opts, store, executor)
				completed[job.ID] = receipt
				summary.Receipts = append(summary.Receipts, receipt)
				continue
			}
			blocked := ""
			for _, dependency := range job.DependsOn {
				if completed[dependency].Status != StatusPassed {
					blocked = dependency
					break
				}
			}
			if blocked != "" {
				receipt := Receipt{Version: ReceiptVersion, RunID: plan.RunID, JobID: job.ID, AttemptID: fmt.Sprintf("%s-attempt-%03d", job.ID, 1), Attempt: 1, Spec: job.Spec, Adapter: job.Adapter, Status: StatusError, Error: "blocked by unsuccessful dependency " + blocked, StartedAt: time.Now().UTC(), ExitCode: -1, Evidence: Evidence{Artifacts: []ArtifactReference{}}}
				receipt = persistAttempt(receipt, store, nil, nil)
				completed[job.ID] = receipt
				summary.Receipts = append(summary.Receipts, receipt)
				continue
			}
			receipt := executeJob(runCtx, plan.RunID, job, opts, store, executor)
			completed[job.ID] = receipt
			summary.Receipts = append(summary.Receipts, receipt)
		}
	} else {
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
		case StatusTimedOut:
			summary.TimedOut++
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
	summary.Outcome = classifyRun(summary, runCtx.Err() != nil)
	if err := store.Finalize(summary); err != nil {
		summary.Outcome = OutcomeIncomplete
		return summary, err
	}
	_ = store.AppendEvent(ProgressEvent{Type: "run_finished"})
	finalErr := runCtx.Err()
	return summary, finalErr
}

func validateCanonicalPlanIdentifiers(plan Plan, maxAttempts int) error {
	for _, job := range plan.Jobs {
		if len(job.ID) > canonicalIdentifierLimit {
			return fmt.Errorf("job ID exceeds canonical identifier limit (%d): %q", canonicalIdentifierLimit, job.ID)
		}
		attemptID := fmt.Sprintf("%s-attempt-%03d", job.ID, maxAttempts)
		if len(attemptID) > canonicalIdentifierLimit {
			return fmt.Errorf("attempt ID exceeds canonical identifier limit (%d) for job %q", canonicalIdentifierLimit, job.ID)
		}
		if len(job.Adapter) > canonicalIdentifierLimit {
			return fmt.Errorf("adapter revision exceeds canonical identifier limit (%d): %q", canonicalIdentifierLimit, job.Adapter)
		}
	}
	return nil
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
		// An interrupted goal may already have changed the application. Never
		// replay the entire worker automatically after an uncertain effect.
		if receipt.Status != StatusError || parent.Err() != nil || len(receipt.Goals) > 0 || receipt.GoalFailed || (receipt.AgentProvenance != nil && receipt.AgentProvenance.Status != "matched") {
			break
		}
	}
	return receipt
}

func executeAttempt(parent context.Context, runID string, job Job, attempt int, opts ExecuteOptions, store Store, executor ProcessExecutor) Receipt {
	started := time.Now().UTC()
	receipt := Receipt{
		Version: ReceiptVersion, RunID: runID, JobID: job.ID, Spec: job.Spec,
		Attempt: attempt, AttemptID: fmt.Sprintf("%s-attempt-%03d", job.ID, attempt),
		Adapter: job.Adapter, StartedAt: started, ExitCode: -1,
		Evidence: Evidence{Command: redactArguments(job.Command), Artifacts: []ArtifactReference{}},
	}
	if runID == "" || job.ID == "" || len(job.Command) == 0 {
		receipt.Status, receipt.Error = StatusError, "invalid planned job"
		return persistAttempt(receipt, store, nil, nil)
	}
	if parent.Err() != nil {
		receipt.Status, receipt.Error = statusFor(parent.Err()), parent.Err().Error()
		receipt.Termination = terminationFor(parent.Err())
		return persistAttempt(receipt, store, nil, nil)
	}
	_ = store.AppendEvent(ProgressEvent{Type: "attempt_started", JobID: job.ID, AttemptID: receipt.AttemptID})
	// Infrastructure that fails before the worker starts is an error, never a
	// test verdict, and still closes the attempt in the event log.
	infrastructureError := func(reason string) Receipt {
		receipt.Status, receipt.Error = StatusError, reason
		receipt = persistAttempt(receipt, store, nil, nil)
		_ = store.AppendEvent(ProgressEvent{Type: "attempt_finished", JobID: job.ID, AttemptID: receipt.AttemptID, Detail: string(receipt.Status)})
		return receipt
	}

	ctx, cancel := context.WithTimeout(parent, opts.Timeout)
	defer cancel()
	if opts.AgentProvenance != nil {
		unknown := UnknownAgentProvenance()
		receipt.AgentProvenance = &unknown
		actual, err := CaptureAgentProvenance(ctx, job.Spec, opts.AgentProvenance.Agent)
		if err != nil {
			return infrastructureError("agent branch/source provenance unavailable; execution refused")
		}
		checked := CompareAgentProvenance(*opts.AgentProvenance, actual)
		receipt.AgentProvenance = &checked
		if checked.Status != "matched" {
			return infrastructureError("agent creation branch, commit, repository or source differs; execution refused")
		}
	}
	job.Env = mergeMaps(passedEnvironment(opts.PassEnv), job.Env, map[string]string{"CI": "1", "NINELIVES_RUN_ID": runID, "NINELIVES_JOB_ID": job.ID, "NINELIVES_ATTEMPT_ID": receipt.AttemptID})
	var service AttemptService
	if opts.Services != nil {
		var err error
		service, err = opts.Services.Start(ctx, AttemptIdentity{runID, job.ID, receipt.AttemptID})
		if err != nil {
			return infrastructureError("goal engine could not start")
		}
		job.Env = mergeMaps(job.Env, service.Environment())
		withholdCredentials(job.Env, opts.Services.CredentialNames())
	}
	evidencePath := ""
	if channel, ok := adapterNamed(opts.Adapters, job.Adapter).(EvidenceChannel); ok {
		directory, err := os.MkdirTemp("", "9lives-evidence-")
		if err != nil {
			if service != nil {
				receipt.Goals = service.Close()
			}
			return infrastructureError("engine evidence channel could not be created")
		}
		defer os.RemoveAll(directory)
		evidencePath = filepath.Join(directory, "events.ndjson")
		job.Env[channel.EvidenceEnv()] = evidencePath
	}
	output := executor.Run(ctx, job, opts.MaxOutputBytes)
	if service != nil {
		receipt.Goals = service.Close()
	}
	if evidencePath != "" {
		// Worker stdout is ordinary process output here; only the private
		// channel is validated and persisted as the evidence stream.
		output.Stdout, output.StdoutTruncated = readEvidence(evidencePath, opts.MaxOutputBytes)
	}
	receipt.Executed, receipt.ExitCode = output.Executed, output.ExitCode
	// Keep actual cleanup attempts as local evidence even when the leader
	// exited normally before its owned descendants were reaped.
	receipt.Termination = output.Termination
	if ctx.Err() != nil {
		receipt.Status, receipt.Error = statusFor(ctx.Err()), ctx.Err().Error()
		if receipt.Termination == nil {
			receipt.Termination = terminationFor(ctx.Err())
		}
	} else if output.Err != nil && output.ExitCode < 0 {
		receipt.Status, receipt.Error = StatusError, output.Err.Error()
	}

	if receipt.Status == "" {
		adapter := adapterNamed(opts.Adapters, job.Adapter)
		if adapter == nil {
			receipt.Status, receipt.Error = StatusError, "planned adapter is not registered"
		} else {
			var validation Validation
			var validationErr error
			if bound, ok := adapter.(AttemptValidator); ok {
				validation, validationErr = bound.ValidateAttempt(output.Stdout, AttemptIdentity{runID, job.ID, receipt.AttemptID})
				if output.StdoutTruncated {
					validationErr = fmt.Errorf("engine protocol output exceeded capture limit")
				}
			} else {
				validation, validationErr = adapter.Validate(output.Stdout)
				// A truncated report never validates, even when the prefix
				// happens to parse; name the limit rather than a parse error.
				if output.StdoutTruncated {
					validationErr = fmt.Errorf("structured report exceeded capture limit")
				}
			}
			if validationErr != nil {
				receipt.Status, receipt.Error, receipt.Validation = StatusError, validationErr.Error(), "missing or invalid structured report"
			} else if err := validateValidation(validation); err != nil {
				receipt.Status, receipt.Error, receipt.Validation = StatusError, err.Error(), "missing or invalid structured report"
			} else {
				receipt.Validated = true
				receipt.FailureCount = validation.FailureCount
				receipt.GoalFailed = validation.GoalFailed
				receipt.NonGoalFailureCount = validation.NonGoalFailureCount
				receipt.ExecutedTests = validation.ExecutedTests
				receipt.SkippedTests = validation.SkippedTests
				receipt.VerifiedAssertions = validation.VerifiedAssertions
				receipt.AssertionCoverage = validation.AssertionCoverage
				receipt.Validation = validation.Description
				if receipt.Executed && receipt.ExitCode == 0 && validation.FailureCount == 0 {
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
	if receipt.Status == StatusPassed {
		for _, goal := range receipt.Goals {
			if goal.Status != "completed" {
				receipt.Status, receipt.Error = StatusFailed, "goal did not complete; see bounded goal receipt"
				receipt.GoalFailed = true
				break
			}
		}
	}
	if receipt.GoalFailed && receipt.Status == StatusPassed {
		receipt.Status, receipt.Error = StatusFailed, "goal invocation failed; see bounded goal evidence"
	}
	receipt.Evidence.StderrTruncated = output.StderrTruncated
	if opts.AgentProvenance != nil {
		actual, err := CaptureAgentProvenance(parent, job.Spec, opts.AgentProvenance.Agent)
		if err != nil {
			unknown := UnknownAgentProvenance()
			unknown.Expected, unknown.Before = receipt.AgentProvenance.Expected, receipt.AgentProvenance.Before
			receipt.AgentProvenance = &unknown
		} else {
			checked := CompareAgentProvenance(*opts.AgentProvenance, actual)
			receipt.AgentProvenance.After = &actual
			receipt.AgentProvenance.Status = checked.Status
			receipt.AgentProvenance.Branch, receipt.AgentProvenance.Commit = checked.Branch, checked.Commit
			receipt.AgentProvenance.Source, receipt.AgentProvenance.Repository = checked.Source, checked.Repository
		}
		if receipt.AgentProvenance.Status != "matched" {
			receipt.Validated = false
			if receipt.Status != StatusCanceled && receipt.Status != StatusTimedOut {
				receipt.Status = StatusError
			}
			receipt.Error = "agent branch/source provenance changed or became unavailable during execution"
		}
	}
	if sanitizer, ok := adapterNamed(opts.Adapters, job.Adapter).(EvidenceSanitizer); ok {
		output.Stdout, output.Stderr = sanitizer.SanitizeEvidence(output.Stdout, output.Stderr, receipt.Validated)
	}
	receipt = persistAttempt(receipt, store, output.Stdout, output.Stderr)
	_ = store.AppendEvent(ProgressEvent{Type: "attempt_finished", JobID: job.ID, AttemptID: receipt.AttemptID, Detail: string(receipt.Status)})
	return receipt
}

// classifyRun separates a run that proved a failure from one that proved
// nothing. Only validated receipts count, and every planned job must have one.
// A failure must be a reported test failure in a test whose goals completed:
// a goal that did not complete (policy, budget, provider) shows that test
// could not exercise the behavior, not that the behavior is broken, so its
// failure is attributed to the goal. Other tests in the same file still count,
// so one failed goal does not hide a proven defect beside it. A goal failure
// the evidence cannot attribute to a test keeps the receipt incomplete.
func classifyRun(summary RunSummary, interrupted bool) RunOutcome {
	if interrupted || summary.PlannedJobs == 0 || summary.SkippedInputs > 0 || len(summary.Receipts) != summary.PlannedJobs {
		return OutcomeIncomplete
	}
	outcome := OutcomePassed
	for _, receipt := range summary.Receipts {
		failures := receipt.FailureCount
		if receipt.GoalFailed {
			failures = receipt.NonGoalFailureCount
		}
		switch {
		case !receipt.Validated:
			return OutcomeIncomplete
		case receipt.Status == StatusFailed && failures > 0:
			outcome = OutcomeFailed
		case receipt.Status != StatusPassed:
			return OutcomeIncomplete
		}
	}
	return outcome
}

func validateValidation(validation Validation) error {
	if validation.ExecutedTests <= 0 {
		return errors.New("structured report contains no executed tests")
	}
	if validation.FailureCount < 0 || validation.SkippedTests < 0 || validation.FailureCount > validation.ExecutedTests || validation.NonGoalFailureCount < 0 || validation.NonGoalFailureCount > validation.FailureCount {
		return errors.New("structured report has inconsistent test counts")
	}
	if validation.ExecutedTests > int(^uint(0)>>1)-validation.SkippedTests {
		return errors.New("structured report test counts overflow")
	}
	if validation.VerifiedAssertions < 0 {
		return errors.New("structured report has negative verified assertions")
	}
	return nil
}

// statusFor distinguishes a per-job or run-wide deadline from a user
// cancellation, including for queued jobs that never started.
func statusFor(err error) ReceiptStatus {
	if errors.Is(err, context.DeadlineExceeded) {
		return StatusTimedOut
	}
	return StatusCanceled
}

func terminationFor(err error) *Termination {
	if errors.Is(err, context.DeadlineExceeded) {
		return &Termination{Kind: "timeout", Detail: err.Error()}
	}
	return &Termination{Kind: "canceled", Detail: err.Error()}
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
	if persisted.RunID != receipt.RunID || persisted.JobID != receipt.JobID || persisted.AttemptID != receipt.AttemptID || persisted.Attempt != receipt.Attempt {
		receipt.Status = StatusError
		receipt.Error = joinError(receipt.Error, "store returned a receipt with an unreserved identity")
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
	err, termination := startAndWait(ctx, command)
	result.Executed = command.Process != nil
	result.Stdout, result.Stderr = stdout.Bytes(), stderr.Bytes()
	result.StdoutTruncated, result.StderrTruncated = stdout.truncated, stderr.truncated
	result.Err = err
	result.Termination = termination
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

// withholdCredentials removes a service's own credentials from the worker
// environment. Names compare case-insensitively, as Windows treats them.
func withholdCredentials(env map[string]string, names []string) {
	for key := range env {
		for _, name := range names {
			if strings.EqualFold(key, name) {
				delete(env, key)
			}
		}
	}
}

// readEvidence returns at most limit bytes of a regular evidence file. A
// missing, non-regular or oversized channel yields output that cannot validate.
func readEvidence(path string, limit int) ([]byte, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, false
	}
	if len(raw) > limit {
		return raw[:limit], true
	}
	return raw, false
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

// inheritedEnvironment is what a browser test runner needs to start, find its
// browsers, render, and reach the network. Application settings and
// credentials (BASE_URL, TEST_PASSWORD, ...) are not inherited implicitly;
// callers name them with ExecuteOptions.PassEnv (`9l run --pass-env NAME`).
var inheritedEnvironment = map[string]bool{
	"HOME": true, "PATH": true, "TMPDIR": true, "TEMP": true, "TMP": true,
	"USERPROFILE": true, "LOCALAPPDATA": true, "APPDATA": true,
	"SYSTEMROOT": true, "WINDIR": true, "COMSPEC": true, "PATHEXT": true,
	"USER": true, "LOGNAME": true, "SHELL": true, "TERM": true, "TZ": true,
	"LANG": true, "LANGUAGE": true,
	"DISPLAY": true, "WAYLAND_DISPLAY": true, "XAUTHORITY": true,
	"PLAYWRIGHT_BROWSERS_PATH": true,
	"HTTP_PROXY":               true, "HTTPS_PROXY": true, "NO_PROXY": true, "ALL_PROXY": true,
	"NODE_EXTRA_CA_CERTS": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true,
}

var inheritedEnvironmentPrefixes = []string{"LC_", "XDG_"}

func inherited(key string) bool {
	// Proxy variables are conventionally lower-case on Unix; Windows variable
	// names are case-insensitive (Path, SystemRoot).
	upper := strings.ToUpper(key)
	if upper != key && runtime.GOOS != "windows" && !strings.HasSuffix(upper, "_PROXY") {
		return false
	}
	if inheritedEnvironment[upper] {
		return true
	}
	for _, prefix := range inheritedEnvironmentPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

// passedEnvironment reads the variables a caller explicitly forwarded. Values
// stay in memory; Job.Env is never persisted in the plan or receipts.
func passedEnvironment(names []string) map[string]string {
	values := map[string]string{}
	for _, name := range names {
		if value, ok := os.LookupEnv(name); ok {
			values[name] = value
		}
	}
	return values
}

func controlledEnvironment(base []string, overlays ...map[string]string) []string {
	values := map[string]string{}
	for _, item := range base {
		if key, value, ok := strings.Cut(item, "="); ok && key != "" && inherited(key) {
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
