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
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/qualitymax/9lives-runner/internal/adapters/playwright"
	"github.com/qualitymax/9lives-runner/internal/runner"
)

const version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		usage(out)
		return 0
	}
	if args[0] == "--version" || args[0] == "version" {
		fmt.Fprintf(out, "9l %s (Go runner)\n", version)
		return 0
	}

	switch args[0] {
	case "plan", "run":
		return runCommand(args[0], args[1:], out, errOut)
	case "status", "result", "cancel":
		return stateCommand(args[0], args[1:], out, errOut)
	case "heal":
		return bridgePython(args[1:], out, errOut)
	default:
		fmt.Fprintf(errOut, "9l: unknown command %q\n", args[0])
		usage(errOut)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `9l — 9lives Go runner

Usage:
  9l plan <spec-or-glob>... [--format text|json] [--max-jobs N]
  9l run  <spec-or-glob>... [--workers N] [--timeout D] [--deadline D] [--pass-env NAME]...
  9l status <run-id> [--receipt-dir DIR]
  9l result <run-id> [--format text|json] [--receipt-dir DIR]
  9l cancel <run-id> [--receipt-dir DIR]
  9l heal <args...>  # delegates to the installed Python healing library

The Go runner currently executes Playwright specs from existing projects.
It never reports an incomplete run green.
`)
}

func runCommand(command string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(errOut)
	format := fs.String("format", "text", "text or json")
	maxJobs := fs.Int("max-jobs", 0, "maximum jobs to reserve for this run (0 is unlimited)")
	workers := fs.Int("workers", min(runtime.GOMAXPROCS(0), 8), "maximum concurrent jobs")
	timeout := fs.Duration("timeout", 5*time.Minute, "per-job timeout")
	deadline := fs.Duration("deadline", 0, "shared run deadline (0 is unlimited)")
	maxAttempts := fs.Int("attempts", 1, "maximum attempts per job")
	maxOutputBytes := fs.Int("max-output-bytes", 4<<20, "captured bytes per output stream")
	dryRun := fs.Bool("dry-run", false, "print the plan without executing it")
	receiptDir := fs.String("receipt-dir", ".9lives/receipts", "directory for evidence receipts")
	var passEnv envNames
	fs.Var(&passEnv, "pass-env", "forward this environment variable to test processes (repeatable or comma-separated)")
	normalized := flagsFirst(args, map[string]bool{
		"-pass-env": true, "--pass-env": true,
		"-format": true, "--format": true,
		"-max-jobs": true, "--max-jobs": true,
		"-workers": true, "--workers": true,
		"-timeout": true, "--timeout": true,
		"-deadline": true, "--deadline": true,
		"-attempts": true, "--attempts": true,
		"-max-output-bytes": true, "--max-output-bytes": true,
		"-receipt-dir": true, "--receipt-dir": true,
	})
	if err := fs.Parse(normalized); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintln(errOut, "9l: --format must be text or json")
		return 2
	}
	if *workers < 1 || *maxJobs < 0 || *timeout <= 0 || *deadline < 0 || *maxAttempts < 1 || *maxOutputBytes < 1024 {
		fmt.Fprintln(errOut, "9l: invalid limits; workers, timeout, attempts, and max-output-bytes must be positive")
		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(errOut, "9l: at least one spec, glob, or directory is required")
		return 2
	}

	availableAdapters := []runner.Adapter{playwright.New()}
	plan, err := runner.BuildPlan(fs.Args(), runner.PlanOptions{MaxJobs: *maxJobs, MaxParallel: *workers, MaxAttempts: *maxAttempts, MaxOutputBytes: *maxOutputBytes, Deadline: *deadline, Adapters: availableAdapters})
	if err != nil {
		fmt.Fprintf(errOut, "9l: plan: %v\n", err)
		return 2
	}
	if command == "plan" || *dryRun {
		return printPlan(out, plan, *format)
	}
	if *format == "json" {
		fmt.Fprintf(errOut, "run %s started; use `9l cancel %s --receipt-dir %s` to cancel\n", plan.RunID, plan.RunID, *receiptDir)
	} else {
		fmt.Fprintf(out, "run %s started; use `9l cancel %s --receipt-dir %s` to cancel\n", plan.RunID, plan.RunID, *receiptDir)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := runner.Execute(ctx, plan, runner.ExecuteOptions{
		Workers: *workers, Timeout: *timeout, RunDeadline: *deadline, MaxAttempts: *maxAttempts, MaxOutputBytes: *maxOutputBytes, PassEnv: passEnv, ReceiptDir: *receiptDir, Adapters: availableAdapters,
	})
	executionFailed := err != nil
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		fmt.Fprintf(errOut, "9l: execution: %v\n", err)
	}
	if *format == "json" {
		_ = json.NewEncoder(out).Encode(result)
	} else {
		printResult(out, result)
	}
	if !executionFailed && result.Complete && result.Failed == 0 && result.Canceled == 0 && result.Errors == 0 {
		return 0
	}
	return 1
}

func stateCommand(command string, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(errOut)
	format := fs.String("format", "json", "text or json")
	receiptDir := fs.String("receipt-dir", ".9lives/receipts", "directory containing run state")
	normalized := flagsFirst(args, map[string]bool{"-format": true, "--format": true, "-receipt-dir": true, "--receipt-dir": true})
	if err := fs.Parse(normalized); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(errOut, "9l: %s requires exactly one run ID\n", command)
		return 2
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintln(errOut, "9l: --format must be text or json")
		return 2
	}
	runID := fs.Arg(0)
	if command == "cancel" {
		if err := runner.RequestCancel(*receiptDir, runID); err != nil {
			fmt.Fprintf(errOut, "9l: cancel: %v\n", err)
			return 2
		}
		fmt.Fprintf(out, "cancellation requested for %s\n", runID)
		return 0
	}
	if command == "status" {
		status, err := runner.LoadStatus(*receiptDir, runID)
		if err != nil {
			fmt.Fprintf(errOut, "9l: status: %v\n", err)
			return 2
		}
		if *format == "json" {
			_ = json.NewEncoder(out).Encode(status)
		} else {
			fmt.Fprintf(out, "%s: %s\n", runID, status.State)
		}
		return 0
	}
	result, err := runner.LoadResult(*receiptDir, runID)
	if err != nil {
		fmt.Fprintf(errOut, "9l: result: %v\n", err)
		return 2
	}
	if *format == "json" {
		_ = json.NewEncoder(out).Encode(result)
	} else {
		printResult(out, result)
	}
	return 0
}

func printPlan(w io.Writer, plan runner.Plan, format string) int {
	if format == "json" {
		if err := json.NewEncoder(w).Encode(plan); err != nil {
			return 1
		}
		return 0
	}
	fmt.Fprintf(w, "run %s: %d job(s), %d skipped\n", plan.RunID, len(plan.Jobs), len(plan.Skipped))
	for _, job := range plan.Jobs {
		fmt.Fprintf(w, "  RUN  %s [%s]\n", job.Spec, job.Adapter)
	}
	for _, skip := range plan.Skipped {
		fmt.Fprintf(w, "  SKIP %s — %s\n", skip.Input, skip.Reason)
	}
	return 0
}

func printResult(w io.Writer, result runner.RunSummary) {
	fmt.Fprintf(w, "run %s: %d passed, %d failed, %d canceled, %d timed out, %d errors (%s)\n", result.RunID, result.Passed, result.Failed, result.Canceled, result.TimedOut, result.Errors, time.Duration(result.DurationMS)*time.Millisecond)
	for _, receipt := range result.Receipts {
		fmt.Fprintf(w, "  %-8s %s", strings.ToUpper(string(receipt.Status)), receipt.Spec)
		if receipt.ReceiptPath != "" {
			fmt.Fprintf(w, "  %s", filepath.Clean(receipt.ReceiptPath))
		}
		fmt.Fprintln(w)
	}
	if !result.Complete {
		fmt.Fprintln(w, "  INCOMPLETE: one or more planned jobs did not finish successfully")
	}
}

func bridgePython(args []string, out, errOut io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	code, err := runner.RunPythonHealer(ctx, args, out, errOut)
	if err != nil {
		fmt.Fprintf(errOut, "9l: Python healing bridge: %v\n", err)
		return 2
	}
	return code
}

// envNames collects --pass-env values. Only names are accepted; values are
// read from the caller's environment so they never appear in argv or receipts.
type envNames []string

func (names *envNames) String() string { return strings.Join(*names, ",") }

func (names *envNames) Set(value string) error {
	for _, name := range strings.Split(value, ",") {
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsAny(name, "= \t\x00") {
			return fmt.Errorf("invalid environment variable name %q", name)
		}
		*names = append(*names, name)
	}
	return nil
}

// The standard flag package stops at the first positional argument. Moving
// recognized flags to the front keeps the familiar `9l run spec --workers 2`
// form without adding a CLI dependency.
func flagsFirst(args []string, valueFlags map[string]bool) []string {
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if argument == "--" {
			positionals = append(positionals, args[index+1:]...)
			break
		}
		if !strings.HasPrefix(argument, "-") {
			positionals = append(positionals, argument)
			continue
		}
		flags = append(flags, argument)
		name := strings.SplitN(argument, "=", 2)[0]
		if valueFlags[name] && !strings.Contains(argument, "=") && index+1 < len(args) {
			index++
			flags = append(flags, args[index])
		}
	}
	return append(flags, positionals...)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
