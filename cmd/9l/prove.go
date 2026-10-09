package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/adapters/playwrightsdk"
	"github.com/Quality-Max/9lives-runner/internal/prove"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// proveExecutor lets tests replace process execution; nil runs real processes.
var proveExecutor runner.ProcessExecutor

const maxProveFaults = 256

type proveSettings struct {
	spec           string
	adapter        playwrightsdk.Adapter
	workers        int
	timeout        time.Duration
	maxOutputBytes int
	passEnv        []string
	receiptDir     string
}

// proveUsageError is a planning failure the caller can fix by changing the
// command (exit 2). Every other run error is operational (exit 1).
type proveUsageError struct{ error }

func proveExit(err error) int {
	var usage proveUsageError
	if errors.As(err, &usage) {
		return 2
	}
	return 1
}

type proveRun struct {
	summary      runner.RunSummary
	receipt      runner.Receipt
	observations prove.Observations
	facts        prove.RunFacts
	// channelInvalid reports that the SDK's prove records failed validation,
	// as distinct from the tests themselves failing.
	channelInvalid bool
}

func proveCommand(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("prove", flag.ContinueOnError)
	fs.SetOutput(errOut)
	format := fs.String("format", "text", "text or json")
	workers := fs.Int("workers", 1, "maximum concurrent jobs per run")
	timeout := fs.Duration("timeout", 2*time.Minute, "per-run timeout")
	deadline := fs.Duration("deadline", 0, "shared deadline for the baseline and every fault run (0 is unlimited)")
	maxFaults := fs.Int("max-faults", 24, "maximum fault runs; the rest are reported as not run")
	maxOutputBytes := fs.Int("max-output-bytes", 4<<20, "captured bytes per output stream")
	receiptDir := fs.String("receipt-dir", ".9lives/receipts", "directory for run receipts and the proof report")
	paths := fs.Bool("paths", false, "print request URLs (origin and path) for local use; never persisted")
	var passEnv envNames
	fs.Var(&passEnv, "pass-env", "forward this environment variable to test processes (repeatable or comma-separated)")
	var skipPins skipPinList
	fs.Var(&skipPins, "pin-skip", "accept this skipped test: \"<file> › <title>\" (repeatable)")
	normalized := flagsFirst(args, map[string]bool{
		"-format": true, "--format": true, "-workers": true, "--workers": true,
		"-timeout": true, "--timeout": true, "-deadline": true, "--deadline": true,
		"-max-faults": true, "--max-faults": true, "-max-output-bytes": true, "--max-output-bytes": true,
		"-receipt-dir": true, "--receipt-dir": true, "-pass-env": true, "--pass-env": true,
		"-pin-skip": true, "--pin-skip": true,
	})
	if err := fs.Parse(normalized); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 || (*format != "text" && *format != "json") {
		fmt.Fprintln(errOut, "9l: prove requires exactly one spec and text or json format")
		return 2
	}
	if *workers < 1 || *timeout <= 0 || *deadline < 0 || *maxOutputBytes < 1024 || *maxFaults < 1 || *maxFaults > maxProveFaults {
		fmt.Fprintf(errOut, "9l: invalid limits; workers, timeout and max-output-bytes must be positive and max-faults 1..%d\n", maxProveFaults)
		return 2
	}
	adapter, err := playwrightsdk.New().WithSkipPins(skipPins)
	if err != nil {
		fmt.Fprintf(errOut, "9l: --pin-skip: %v\n", err)
		return 2
	}
	settings := proveSettings{spec: fs.Arg(0), adapter: adapter, workers: *workers, timeout: *timeout, maxOutputBytes: *maxOutputBytes, passEnv: passEnv, receiptDir: *receiptDir}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *deadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *deadline)
		defer cancel()
	}

	progress := errOut
	if *format == "text" {
		progress = out
	}
	baseline, err := settings.run(ctx, nil)
	if err != nil {
		fmt.Fprintln(errOut, "9l: prove:", err)
		return proveExit(err)
	}
	fmt.Fprintf(progress, "baseline %s: %s\n", baseline.summary.RunID, baseline.receipt.Status)
	if baseline.channelInvalid {
		fmt.Fprintln(errOut, "9l: prove: the baseline's prove records failed validation; nothing can be planned from them")
		return 1
	}
	if !baseline.facts.Validated || !baseline.facts.Passed || !baseline.summary.Complete {
		fmt.Fprintln(errOut, "9l: prove needs a passing, complete baseline; a failing test proves nothing about faults")
		return 1
	}
	if baseline.observations.Attempts == 0 {
		fmt.Fprintln(errOut, "9l: prove: no browser context was instrumented; import test from an @9l/playwright build with prove support and use page or context")
		return 1
	}
	if baseline.observations.Overflow {
		fmt.Fprintln(errOut, "9l: prove: the baseline exceeded the observation limit; prove a smaller spec")
		return 1
	}

	requests, faults, notRun := prove.Plan(baseline.observations, *maxFaults)
	report := prove.Report{
		Version: 1, Policy: prove.Policy, Spec: settings.spec, Requests: requests, Faults: []prove.FaultReport{}, Limits: prove.Limits,
		Baseline: prove.Baseline{RunID: baseline.summary.RunID, Status: string(baseline.receipt.Status), ExecutedTests: baseline.receipt.ExecutedTests, Attempts: baseline.observations.Attempts},
	}
	for index, fault := range faults {
		entry := prove.FaultReport{ID: fault.ID, Request: fault.Request, Kind: fault.Kind, Result: prove.NotRun}
		if ctx.Err() != nil {
			report.Interrupted = true
			report.Faults = append(report.Faults, entry)
			continue
		}
		fmt.Fprintf(progress, "fault %d/%d: %s %s\n", index+1, len(faults), fault.Kind, fault.Request)
		started := time.Now()
		result, err := settings.run(ctx, &fault)
		entry.DurationMS = time.Since(started).Milliseconds()
		if err != nil {
			fmt.Fprintln(errOut, "9l: prove:", err)
			return proveExit(err)
		}
		// Invalid worker records make this fault incomplete and the whole
		// proof unsuccessful, not merely one inconclusive row.
		report.InvalidEvidence = report.InvalidEvidence || result.channelInvalid
		entry.RunID, entry.Applied = result.summary.RunID, result.facts.Applied
		entry.FailedTests, entry.AssertionFailures = result.facts.Unexpected, result.facts.UnexpectedWithAssertion
		entry.Result = prove.Classify(result.facts)
		if ctx.Err() != nil && entry.Result == prove.Incomplete {
			report.Interrupted = true
		}
		report.Faults = append(report.Faults, entry)
	}
	for _, fault := range notRun {
		report.Faults = append(report.Faults, prove.FaultReport{ID: fault.ID, Request: fault.Request, Kind: fault.Kind, Result: prove.NotRun})
	}
	report.Summary = prove.Summarize(report.Faults)
	report.Complete = !report.Interrupted && !report.InvalidEvidence && report.Summary.NotRun == 0
	saved, saveErr := saveProof(settings.receiptDir, report)
	if saveErr != nil {
		fmt.Fprintln(errOut, "9l: prove: report could not be saved:", saveErr)
	}
	if *paths {
		report.Requests = prove.WithURLs(report.Requests)
	}
	if *format == "json" {
		_ = json.NewEncoder(out).Encode(report)
	} else {
		printProof(out, report, saved, *paths)
	}
	if report.Interrupted || report.InvalidEvidence || saveErr != nil {
		return 1
	}
	return 0
}

// run executes the spec once: observing when fault is nil, otherwise with the
// one fault injected. Each run is an ordinary receipted 9l run.
func (settings proveSettings) run(ctx context.Context, fault *prove.Fault) (proveRun, error) {
	var result proveRun
	adapters := []runner.Adapter{settings.adapter}
	plan, err := runner.BuildPlan([]string{settings.spec}, runner.PlanOptions{MaxJobs: 1, MaxParallel: settings.workers, MaxAttempts: 1, MaxOutputBytes: settings.maxOutputBytes, Adapters: adapters})
	if err != nil {
		return result, proveUsageError{fmt.Errorf("plan: %w", err)}
	}
	if len(plan.Jobs) != 1 || len(plan.Skipped) != 0 {
		return result, proveUsageError{errors.New("prove requires exactly one spec file")}
	}
	dir, err := os.MkdirTemp("", "9lives-prove-")
	if err != nil {
		return result, errors.New("prove evidence directory could not be created")
	}
	defer os.RemoveAll(dir)
	job := &plan.Jobs[0]
	job.Env["NINELIVES_PROVE"] = prove.Protocol
	job.Env["NINELIVES_PROVE_DIR"] = dir
	mode := "observe"
	if fault != nil {
		job.Env["NINELIVES_PROVE_FAULT"] = fault.Env()
		mode = "fault"
	}
	// A Playwright retry would hide whether the first attempt detected the
	// fault, and the project's configured retries otherwise apply.
	job.Command = append(job.Command, "--retries=0")
	summary, execErr := runner.Execute(ctx, plan, runner.ExecuteOptions{
		Workers: settings.workers, Timeout: settings.timeout, MaxAttempts: 1, MaxOutputBytes: settings.maxOutputBytes,
		PassEnv: settings.passEnv, ReceiptDir: settings.receiptDir, Adapters: adapters, Executor: proveExecutor,
	})
	result.summary = summary
	if len(summary.Receipts) != 1 {
		if execErr != nil && ctx.Err() == nil {
			return result, fmt.Errorf("execution: %w", execErr)
		}
		return result, nil
	}
	result.receipt = summary.Receipts[0]
	receipt := result.receipt
	result.facts.Validated = execErr == nil && receipt.Validated && (receipt.Status == runner.StatusPassed || receipt.Status == runner.StatusFailed)
	result.facts.Passed = receipt.Status == runner.StatusPassed && summary.Complete
	if result.facts.Validated {
		raw, err := os.ReadFile(receipt.Evidence.StdoutPath)
		facts, factsErr := playwrightsdk.Assertions(raw)
		if err != nil || factsErr != nil {
			result.facts.Validated = false
		}
		result.facts.Unexpected, result.facts.UnexpectedWithAssertion = facts.Unexpected, facts.UnexpectedWithAssertion
	}
	observations, err := prove.ReadChannel(dir, mode)
	if err != nil {
		// Worker-written records are untrusted; an invalid stream proves nothing.
		result.facts.Validated, result.channelInvalid = false, true
		return result, nil
	}
	result.observations = observations
	result.facts.Overflow = observations.Overflow
	if fault != nil {
		result.facts.Applied, result.facts.NotApplicable = observations.Applied[fault.ID], observations.NotApplicable[fault.ID]
	}
	return result, nil
}

// saveProof persists the report without request URLs beside the run receipts.
func saveProof(receiptDir string, report prove.Report) (string, error) {
	dir := filepath.Join(receiptDir, "proofs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, report.Baseline.RunID+".json")
	temporary, err := os.CreateTemp(dir, ".proof-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(append(raw, '\n')); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	return path, os.Rename(temporary.Name(), path)
}

func printProof(w io.Writer, report prove.Report, saved string, paths bool) {
	requests := map[string]prove.RequestReport{}
	for _, request := range report.Requests {
		requests[request.ID] = request
	}
	fmt.Fprintf(w, "prove %s: %d request(s) observed in %d instrumented test attempt(s)\n", report.Spec, len(report.Requests), report.Baseline.Attempts)
	for _, fault := range report.Faults {
		request := requests[fault.Request]
		label := request.ID + " " + request.Method
		if paths {
			label += " " + request.URL
		}
		fmt.Fprintf(w, "  %-26s %-10s %s\n", strings.ToUpper(fault.Result), fault.Kind, label)
	}
	summary := report.Summary
	fmt.Fprintf(w, "Summary: %d fault(s): %d caught, %d survived, %d inconclusive, %d not run\n", summary.Faults, summary.Caught, summary.Survived, summary.Inconclusive, summary.NotRun)
	if saved != "" {
		fmt.Fprintf(w, "Report: %s\n", saved)
	}
	fmt.Fprintln(w, "A caught fault means an assertion failed while it was injected, not that the assertion checks the intended behavior.")
	if !paths && len(report.Requests) > 0 {
		fmt.Fprintln(w, "Use --paths to show request URLs locally.")
	}
	exercised := false
	for _, fault := range report.Faults {
		exercised = exercised || (fault.Result != prove.NotExercised && fault.Result != prove.NotRun)
	}
	if !exercised && len(report.Faults) > 0 {
		fmt.Fprintln(w, "No fault was exercised: a fault targets the baseline's method, origin and path, so an origin that changes between runs (such as a random port) never matches.")
	}
	if report.InvalidEvidence {
		fmt.Fprintln(w, "  INVALID EVIDENCE: a fault run's prove records failed validation; the proof is incomplete")
	}
	if report.Interrupted {
		fmt.Fprintln(w, "  INTERRUPTED: remaining faults were not run")
	}
}
