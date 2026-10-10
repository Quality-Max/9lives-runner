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
	"slices"
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
	// passed reports a complete run whose receipt passed.
	passed bool
	// channelInvalid reports that the SDK's prove records failed validation,
	// as distinct from the tests themselves failing.
	channelInvalid          bool
	unexpected              int
	unexpectedWithAssertion int
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
	faultList := fs.String("faults", strings.Join(prove.DefaultKinds, ","), "comma-separated fault kinds: "+strings.Join(prove.Kinds, ", "))
	var passEnv envNames
	fs.Var(&passEnv, "pass-env", "forward this environment variable to test processes (repeatable or comma-separated)")
	var skipPins skipPinList
	fs.Var(&skipPins, "pin-skip", "accept this skipped test: \"<file> › <title>\" (repeatable)")
	normalized := flagsFirst(args, map[string]bool{
		"-format": true, "--format": true, "-workers": true, "--workers": true,
		"-timeout": true, "--timeout": true, "-deadline": true, "--deadline": true,
		"-max-faults": true, "--max-faults": true, "-max-output-bytes": true, "--max-output-bytes": true,
		"-receipt-dir": true, "--receipt-dir": true, "-pass-env": true, "--pass-env": true,
		"-pin-skip": true, "--pin-skip": true, "-faults": true, "--faults": true,
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
	kinds, err := parseFaultKinds(*faultList)
	if err != nil {
		fmt.Fprintf(errOut, "9l: --faults: %v\n", err)
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
	// An interrupted baseline is an operational stop, not a failing test.
	if ctx.Err() != nil || baseline.receipt.Status == runner.StatusCanceled || baseline.receipt.Status == runner.StatusTimedOut {
		fmt.Fprintln(errOut, "9l: prove: the baseline was canceled or timed out before it finished; nothing was proved")
		return 1
	}
	if !baseline.facts.Validated || !baseline.passed || !baseline.summary.Complete {
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

	// Only an SDK that reported a kind can apply it: an older one rejects an
	// unknown fault and every test would fail without an assertion.
	if unsupported := unsupportedKinds(kinds, baseline.observations.FaultKinds); len(unsupported) > 0 {
		fmt.Fprintf(errOut, "9l: prove: --faults %s needs a newer @9l/playwright; the installed SDK applies only %s\n", strings.Join(unsupported, ","), strings.Join(supportedKinds(baseline.observations.FaultKinds), ", "))
		return 2
	}

	requests, faults, notRun := prove.Plan(baseline.observations, *maxFaults, kinds)
	report := prove.Report{
		Version: 1, Policy: prove.Policy, Spec: settings.spec, FaultKinds: kinds, Requests: requests, Faults: []prove.FaultReport{}, Limits: prove.Limits,
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
		entry.RunID, entry.Applied = result.summary.RunID, result.observations.Applied()
		entry.FailedTests, entry.AssertionFailures = result.unexpected, result.unexpectedWithAssertion
		entry.Result, entry.Tests = prove.Classify(result.facts)
		if ctx.Err() != nil && entry.Result == prove.Incomplete {
			report.Interrupted = true
		}
		report.Faults = append(report.Faults, entry)
	}
	for _, fault := range notRun {
		report.Faults = append(report.Faults, prove.FaultReport{ID: fault.ID, Request: fault.Request, Kind: fault.Kind, Result: prove.NotRun})
	}
	report.Summary = prove.Summarize(report.Faults)
	report.Complete, report.IncompleteReason = prove.Completeness(report)
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
	// An incomplete proof established less than it set out to, so it is not
	// a success, whatever the faults it did run found.
	if !report.Complete {
		fmt.Fprintf(errOut, "9l: prove: the proof is incomplete (%s)\n", report.IncompleteReason)
		return 1
	}
	if saveErr != nil {
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
	} else {
		// Ask the SDK which fault kinds it can apply; one that predates this
		// ignores the variable and writes no capability record.
		job.Env["NINELIVES_PROVE_CAPABILITIES"] = "1"
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
	result.passed = receipt.Status == runner.StatusPassed && summary.Complete
	result.facts.Tests = map[string]prove.TestFacts{}
	if result.facts.Validated {
		raw, err := os.ReadFile(receipt.Evidence.StdoutPath)
		facts, factsErr := playwrightsdk.Assertions(raw)
		if err != nil || factsErr != nil {
			result.facts.Validated = false
		}
		result.unexpected, result.unexpectedWithAssertion = facts.Unexpected, facts.UnexpectedWithAssertion
		for id, test := range facts.Tests {
			result.facts.Tests[id] = prove.TestFacts{Outcome: test.Outcome, Attempts: test.Attempts, AssertionFailed: test.AssertionFailed}
		}
	}
	faultID := ""
	if fault != nil {
		faultID = fault.ID
	}
	observations, err := prove.ReadChannel(dir, mode, faultID)
	if err != nil {
		// Worker-written records are untrusted; an invalid stream proves nothing.
		result.facts.Validated, result.channelInvalid = false, true
		return result, nil
	}
	result.observations = observations
	result.facts.Overflow = observations.Overflow
	// Join each instrumented test's fault facts to its engine outcome; a
	// prove file for a test the engine never reported is invalid evidence.
	for id, attempt := range observations.Tests {
		test, ok := result.facts.Tests[id]
		if !ok && result.facts.Validated {
			result.facts.Validated, result.channelInvalid = false, true
			return result, nil
		}
		test.Applied, test.NotApplicable = attempt.Applied, attempt.NotApplicable
		result.facts.Tests[id] = test
	}
	return result, nil
}

// parseFaultKinds returns the selected kinds in prove.Kinds order.
func parseFaultKinds(list string) ([]string, error) {
	selected := map[string]bool{}
	for _, name := range strings.Split(list, ",") {
		name = strings.TrimSpace(name)
		if !slices.Contains(prove.Kinds, name) {
			return nil, fmt.Errorf("unknown fault kind %q; choose from %s", name, strings.Join(prove.Kinds, ", "))
		}
		if selected[name] {
			return nil, fmt.Errorf("fault kind %q is listed twice", name)
		}
		selected[name] = true
	}
	kinds := []string{}
	for _, kind := range prove.Kinds {
		if selected[kind] {
			kinds = append(kinds, kind)
		}
	}
	return kinds, nil
}

func unsupportedKinds(kinds []string, supported map[string]bool) []string {
	missing := []string{}
	for _, kind := range kinds {
		if !supported[kind] {
			missing = append(missing, kind)
		}
	}
	return missing
}

func supportedKinds(supported map[string]bool) []string {
	kinds := []string{}
	for _, kind := range prove.Kinds {
		if supported[kind] {
			kinds = append(kinds, kind)
		}
	}
	return kinds
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
	retried := false
	for _, fault := range report.Faults {
		request := requests[fault.Request]
		label := request.ID + " " + request.Method
		if paths {
			label += " " + request.URL
		}
		fmt.Fprintf(w, "  %-26s %-14s %s\n", strings.ToUpper(fault.Result), fault.Kind, label)
		retried = retried || fault.Result == prove.Retried
		// With several tests, say which one each result comes from.
		if len(fault.Tests) > 1 {
			for _, test := range fault.Tests {
				reason := ""
				if test.Reason != "" {
					reason = " (" + test.Reason + ")"
				}
				fmt.Fprintf(w, "    %-24s test %s%s\n", test.Result, test.TestID[:12], reason)
			}
		}
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
	if summary.Exercised == 0 && len(report.Faults) > 0 {
		fmt.Fprintln(w, "No fault was exercised: a fault targets the baseline's method, origin and path, so an origin that changes between runs (such as a random port) never matches; a test route that fulfills, continues or aborts the request instead of calling route.fallback() also hides it.")
	}
	if retried {
		fmt.Fprintln(w, "A retried fault means Playwright ran a test more than once in that run, for example under test.describe.configure({retries}); a later attempt can hide what the first detected, so disable retries in the spec.")
	}
	if report.InvalidEvidence {
		fmt.Fprintln(w, "  INVALID EVIDENCE: a fault run's prove records failed validation; the proof is incomplete")
	}
	if report.Interrupted {
		fmt.Fprintln(w, "  INTERRUPTED: remaining faults were not run")
	}
	if !report.Complete {
		fmt.Fprintf(w, "  INCOMPLETE (%s): this proof is not a success\n", report.IncompleteReason)
	}
}
