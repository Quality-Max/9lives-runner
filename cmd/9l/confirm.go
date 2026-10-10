package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/adapters/playwrightsdk"
	"github.com/Quality-Max/9lives-runner/internal/confirm"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// confirmExecutor lets tests replace process execution; nil runs real processes.
var confirmExecutor runner.ProcessExecutor

var findingIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._#:/-]{0,63}$`)

const maxFindingBytes = 64 << 10

type confirmOptions struct {
	spec      string
	unfixed   string
	fixed     string
	finding   string
	findingID string
	// The rest bound each of the two runs.
	workers        int
	timeout        time.Duration
	maxOutputBytes int
	passEnv        []string
	receiptDir     string
}

func confirmCommand(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("confirm", flag.ContinueOnError)
	fs.SetOutput(errOut)
	options := confirmOptions{}
	format := fs.String("format", "text", "text or json")
	fs.StringVar(&options.unfixed, "unfixed", "", "revision the finding was reported against (required)")
	fs.StringVar(&options.fixed, "fixed", "", "revision that claims to fix it (default: the working tree)")
	fs.StringVar(&options.finding, "finding", "", "finding text; only its SHA-256 is recorded")
	fs.StringVar(&options.findingID, "finding-id", "", "short finding label recorded as given, such as an issue key")
	fs.IntVar(&options.workers, "workers", 1, "maximum concurrent jobs per run")
	fs.DurationVar(&options.timeout, "timeout", 2*time.Minute, "per-run timeout")
	deadline := fs.Duration("deadline", 0, "shared deadline for both runs (0 is unlimited)")
	fs.IntVar(&options.maxOutputBytes, "max-output-bytes", 4<<20, "captured bytes per output stream")
	fs.StringVar(&options.receiptDir, "receipt-dir", ".9lives/receipts", "directory for run receipts and the confirmation report")
	var passEnv envNames
	fs.Var(&passEnv, "pass-env", "forward this environment variable to test processes (repeatable or comma-separated)")
	normalized := flagsFirst(args, map[string]bool{
		"-format": true, "--format": true, "-unfixed": true, "--unfixed": true, "-fixed": true, "--fixed": true,
		"-finding": true, "--finding": true, "-finding-id": true, "--finding-id": true, "-workers": true, "--workers": true,
		"-timeout": true, "--timeout": true, "-deadline": true, "--deadline": true,
		"-max-output-bytes": true, "--max-output-bytes": true, "-receipt-dir": true, "--receipt-dir": true,
		"-pass-env": true, "--pass-env": true,
	})
	if err := fs.Parse(normalized); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return exitUsage
	}
	if fs.NArg() != 1 || (*format != "text" && *format != "json") {
		fmt.Fprintln(errOut, "9l: confirm requires exactly one reproduction spec and text or json format")
		return exitUsage
	}
	if options.unfixed == "" {
		fmt.Fprintln(errOut, "9l: confirm requires --unfixed <revision>")
		return exitUsage
	}
	if options.workers < 1 || options.timeout <= 0 || *deadline < 0 || options.maxOutputBytes < 1024 {
		fmt.Fprintln(errOut, "9l: invalid limits; workers, timeout and max-output-bytes must be positive")
		return exitUsage
	}
	options.spec, options.passEnv = fs.Arg(0), passEnv

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
	report, saved, err := confirmFinding(ctx, options, progress)
	var setup confirm.SetupError
	if errors.As(err, &setup) {
		fmt.Fprintln(errOut, "9l: confirm:", setup.Message)
		return exitUsage
	}
	if err != nil {
		fmt.Fprintln(errOut, "9l: confirm:", err)
		return exitIncomplete
	}
	if *format == "json" {
		_ = json.NewEncoder(out).Encode(report)
	} else {
		printConfirmation(out, report, saved)
	}
	if saved == "" {
		return exitIncomplete
	}
	switch report.Verdict {
	case confirm.Confirmed:
		return 0
	case confirm.Inconclusive:
		return exitIncomplete
	default:
		return 1
	}
}

// confirmFinding runs the reproduction spec on the unfixed revision and then
// the fixed one, classifies the pair and saves the report. A
// confirm.SetupError is something the caller can fix; any other error is
// operational. A report whose save failed is returned with an empty path.
func confirmFinding(ctx context.Context, options confirmOptions, progress io.Writer) (confirm.Report, string, error) {
	var report confirm.Report
	if options.findingID != "" && !findingIDPattern.MatchString(options.findingID) {
		return report, "", confirm.SetupError{Message: "--finding-id must be 1-64 letters, digits or ._#:/- characters"}
	}
	if len(options.finding) > maxFindingBytes {
		return report, "", confirm.SetupError{Message: "--finding text exceeds 64 KiB"}
	}
	repo, err := confirm.Open(ctx, options.spec)
	if err != nil {
		return report, "", err
	}
	unfixedCommit, err := repo.Resolve(ctx, options.unfixed)
	if err != nil {
		return report, "", err
	}
	fixed := confirm.Revision{Ref: "working-tree"}
	if options.fixed != "" {
		if fixed.Commit, err = repo.Resolve(ctx, options.fixed); err != nil {
			return report, "", err
		}
		fixed.Ref = options.fixed
		if fixed.Commit == unfixedCommit {
			return report, "", confirm.SetupError{Message: "--unfixed and --fixed name the same commit"}
		}
	} else {
		if fixed.Commit, err = repo.Head(ctx); err != nil {
			return report, "", err
		}
		if fixed.Dirty, err = repo.Dirty(ctx); err != nil {
			return report, "", err
		}
		if !fixed.Dirty && fixed.Commit == unfixedCommit {
			return report, "", confirm.SetupError{Message: "the working tree has no changes beyond the reproduction spec since " + options.unfixed + "; make the fix or pass --fixed"}
		}
	}
	receiptDir, err := filepath.Abs(options.receiptDir)
	if err != nil {
		return report, "", confirm.SetupError{Message: "invalid --receipt-dir"}
	}
	options.receiptDir = receiptDir
	id, err := confirmationID()
	if err != nil {
		return report, "", errors.New("confirmation id could not be generated")
	}
	report = confirm.Report{
		Version: confirm.ReportVersion, Policy: confirm.Policy, ID: id, Spec: repo.SpecPath, SpecSHA256: repo.SpecSHA256(),
		FindingID: options.findingID, Unfixed: confirm.Revision{Ref: options.unfixed, Commit: unfixedCommit}, Fixed: fixed,
		Tests: []confirm.TestResult{}, Limits: confirm.Limits,
	}
	if options.finding != "" {
		sum := sha256.Sum256([]byte(options.finding))
		report.FindingSHA256 = hex.EncodeToString(sum[:])
	}
	report.Unfixed.Spec = repo.SpecAt(ctx, unfixedCommit)
	report.Fixed.Spec = repo.SpecAt(ctx, fixed.Commit)
	if options.fixed == "" {
		// The working tree runs the spec itself.
		report.Fixed.Spec = confirm.SpecSame
	}
	report.DependenciesDiffer = repo.DependenciesDiffer(ctx, unfixedCommit) || (options.fixed != "" && repo.DependenciesDiffer(ctx, fixed.Commit))

	fmt.Fprintf(progress, "unfixed %s (%s)\n", shortCommit(unfixedCommit), options.unfixed)
	unfixedFacts, err := runRevision(ctx, repo, unfixedCommit, options, &report.Unfixed)
	if err != nil {
		return report, "", err
	}
	fixedFacts := confirm.RunFacts{}
	if ctx.Err() == nil {
		fmt.Fprintf(progress, "fixed %s (%s)\n", shortCommit(fixed.Commit), fixed.Ref)
		commit := fixed.Commit
		if options.fixed == "" {
			commit = ""
		}
		if fixedFacts, err = runRevision(ctx, repo, commit, options, &report.Fixed); err != nil {
			return report, "", err
		}
	}
	report.Verdict, report.InconclusiveReason, report.Tests = confirm.Classify(unfixedFacts, fixedFacts)
	if report.Tests == nil {
		report.Tests = []confirm.TestResult{}
	}
	saved, saveErr := saveReport(filepath.Join(receiptDir, "confirmations"), report.ID, report)
	if saveErr != nil {
		fmt.Fprintln(progress, "report could not be saved:", saveErr)
		return report, "", nil
	}
	return report, saved, nil
}

// runRevision runs the reproduction spec once on a commit, or in the working
// tree when commit is empty, as an ordinary receipted SDK run without
// Playwright retries.
func runRevision(ctx context.Context, repo confirm.Repository, commit string, options confirmOptions, revision *confirm.Revision) (confirm.RunFacts, error) {
	facts := confirm.RunFacts{Tests: map[string]confirm.TestFacts{}}
	spec := filepath.Join(repo.Root, filepath.FromSlash(repo.SpecPath))
	if commit != "" {
		checkout, err := repo.Checkout(ctx, commit)
		if err != nil {
			return facts, err
		}
		defer checkout.Close()
		spec = checkout.Spec
	}
	adapters := []runner.Adapter{playwrightsdk.New()}
	plan, err := runner.BuildPlan([]string{spec}, runner.PlanOptions{MaxJobs: 1, MaxParallel: options.workers, MaxAttempts: 1, MaxOutputBytes: options.maxOutputBytes, Adapters: adapters})
	if err != nil {
		return facts, confirm.SetupError{Message: "plan: " + err.Error()}
	}
	if len(plan.Jobs) != 1 || len(plan.Skipped) != 0 {
		return facts, confirm.SetupError{Message: "the reproduction spec is not a Playwright spec this runner can execute"}
	}
	// A retry would hide whether the first attempt failed.
	plan.Jobs[0].Command = append(plan.Jobs[0].Command, "--retries=0")
	summary, execErr := runner.Execute(ctx, plan, runner.ExecuteOptions{
		Workers: options.workers, Timeout: options.timeout, MaxAttempts: 1, MaxOutputBytes: options.maxOutputBytes,
		PassEnv: options.passEnv, ReceiptDir: options.receiptDir, Adapters: adapters, Executor: confirmExecutor,
	})
	var setup runner.SetupError
	if errors.As(execErr, &setup) {
		return facts, confirm.SetupError{Message: setup.Error()}
	}
	revision.RunID = summary.RunID
	if len(summary.Receipts) != 1 {
		if execErr != nil && ctx.Err() == nil {
			return facts, fmt.Errorf("execution: %w", execErr)
		}
		return facts, nil
	}
	receipt := summary.Receipts[0]
	revision.Status, revision.ExecutedTests = string(receipt.Status), receipt.ExecutedTests
	// A goal that failed or stopped leaves the run incomplete even when
	// Playwright reports the test as expected; neither side may count it.
	facts.Validated = execErr == nil && receipt.Validated && !receipt.GoalFailed && (receipt.Status == runner.StatusPassed || receipt.Status == runner.StatusFailed)
	if !facts.Validated {
		return facts, nil
	}
	raw, err := os.ReadFile(receipt.Evidence.StdoutPath)
	evidence, factsErr := playwrightsdk.Assertions(raw)
	if err != nil || factsErr != nil {
		facts.Validated = false
		return facts, nil
	}
	for id, test := range evidence.Tests {
		facts.Tests[id] = confirm.TestFacts{Outcome: test.Outcome, Attempts: test.Attempts, AssertionFailed: test.AssertionFailed}
	}
	return facts, nil
}

func confirmationID() (string, error) {
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "confirm-" + time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(random), nil
}

func shortCommit(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}

func printConfirmation(w io.Writer, report confirm.Report, saved string) {
	fmt.Fprintf(w, "confirm %s\n", report.Spec)
	for _, side := range []struct {
		label    string
		revision confirm.Revision
	}{{"unfixed", report.Unfixed}, {"fixed", report.Fixed}} {
		ref := side.revision.Ref
		if side.revision.Dirty {
			ref += ", uncommitted changes"
		}
		status := side.revision.Status
		if status == "" {
			status = "not run"
		}
		fmt.Fprintf(w, "  %-8s %s (%s)  run %s: %s\n", side.label, shortCommit(side.revision.Commit), ref, side.revision.RunID, status)
	}
	for _, test := range report.Tests {
		reason := ""
		if test.Reason != "" {
			reason = " (" + test.Reason + ")"
		}
		fmt.Fprintf(w, "  %-15s test %s  unfixed %s, fixed %s%s\n", strings.ToUpper(test.Result), test.TestID[:min(12, len(test.TestID))], test.Unfixed, test.Fixed, reason)
	}
	verdict := report.Verdict
	if report.InconclusiveReason != "" {
		verdict += " (" + report.InconclusiveReason + ")"
	}
	fmt.Fprintf(w, "Verdict: %s\n", verdict)
	if saved != "" {
		fmt.Fprintf(w, "Report: %s\n", saved)
	}
	if report.DependenciesDiffer {
		fmt.Fprintln(w, "  DEPENDENCIES DIFFER: a revision's package manifest or lockfile differs from the installed dependencies both runs used")
	}
	switch report.Verdict {
	case confirm.Confirmed:
		fmt.Fprintln(w, "The spec's assertions failed on the unfixed revision and passed on the fixed one. That shows the spec tells them apart, not that it tests the reported finding.")
	case confirm.NotReproduced, confirm.FixIneffective:
		fmt.Fprintln(w, "Both revisions gave the same result. If the application runs separately from the checkout, both runs exercised the same code; start it from the spec, its fixtures or the Playwright webServer configuration.")
	}
}
