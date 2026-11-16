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
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/adapters/playwright"
	"github.com/Quality-Max/9lives-runner/internal/jev"
	"github.com/Quality-Max/9lives-runner/internal/jevassist"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// newDiagnosisDecider builds the attempt-bound Jev client. Tests replace it
// with an offline fixture; production always uses the fixed J1 endpoint.
var newDiagnosisDecider = func(ctx context.Context, config jev.Config, identity runner.AttemptIdentity, revision, source string) (jevassist.Decider, func(), error) {
	limits := jev.DefaultLimits()
	limits.MaxRequests, limits.MaxQuestions, limits.Timeout, limits.MaxEvidenceAge = 1, 5, 45*time.Second, time.Minute
	client, err := jev.New(ctx, config, identity, revision, source, limits)
	if err != nil {
		return nil, func() {}, err
	}
	return client, client.Close, nil
}

type diagnoseReport struct {
	Spec      string               `json:"spec"`
	Outcome   string               `json:"outcome"`
	Receipt   string               `json:"receipt,omitempty"`
	Diagnosis *jevassist.Diagnosis `json:"diagnosis,omitempty"`
}

func diagnoseCommand(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	fs.SetOutput(errOut)
	useJev := fs.Bool("jev", false, "ask Jev for a bounded diagnosis of a failure (one paid request; requires TYPESAFE_API_KEY)")
	model := fs.String("jev-model", jev.DefaultModel, "explicit Jev model")
	images := fs.String("jev-images", "off", "off, or synthetic to upload the failure screenshot of test-only data")
	runTimeout := nativeRunTimeout()
	fs.Var(runTimeoutFlag{&runTimeout}, "run-timeout", "run timeout, as a duration or whole seconds")
	receipts := fs.String("receipt-dir", ".9lives/diagnoses", "run receipts and diagnosis records")
	var passEnv envNames
	fs.Var(&passEnv, "pass-env", "forward this environment variable only to test processes (repeatable or comma-separated)")
	format := fs.String("format", "text", "text or json")
	normalized := flagsFirst(args, map[string]bool{"-jev-model": true, "--jev-model": true, "-jev-images": true, "--jev-images": true, "-run-timeout": true, "--run-timeout": true, "-receipt-dir": true, "--receipt-dir": true, "-pass-env": true, "--pass-env": true, "-format": true, "--format": true})
	if err := fs.Parse(normalized); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	jevOptions := false
	fs.Visit(func(f *flag.Flag) { jevOptions = jevOptions || f.Name == "jev-model" || f.Name == "jev-images" })
	if fs.NArg() != 1 || runTimeout <= 0 || (*format != "text" && *format != "json") || (jevOptions && !*useJev) || (*images != "off" && *images != "synthetic") {
		fmt.Fprintln(errOut, "9l: diagnose takes one spec, --jev with --jev-model and --jev-images off|synthetic, --run-timeout, --receipt-dir, --pass-env and --format text|json")
		return 2
	}
	config := jev.Config{Enabled: *useJev, Model: *model, Images: *images}
	if *useJev {
		preflight, err := jev.CheckConfig(config)
		if err != nil || preflight.Status != "configured" {
			fmt.Fprintln(errOut, "9l: diagnose --jev needs a valid model and "+jev.CredentialName+" in this process; no test was run")
			return 2
		}
		uploads := "the error message and a redacted accessible snapshot"
		if *images == "synthetic" {
			uploads += " and the failure screenshot (synthetic: you assert it shows test-only data)"
		}
		fmt.Fprintf(errOut, "9l: a failure will send %s to Jev (%s); the result is never changed\n", uploads, config.Model)
	}
	spec := fs.Arg(0)
	source, err := readSource(spec)
	if err != nil {
		fmt.Fprintln(errOut, "9l: diagnose: cannot read the spec")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	report, code := diagnoseSpec(ctx, spec, source, config, runTimeout, *receipts, passEnv)
	if code == 2 {
		fmt.Fprintln(errOut, "9l: diagnose: the run did not complete; check the spec and its Playwright project")
		return 2
	}
	if *format == "json" {
		if json.NewEncoder(out).Encode(report) != nil {
			return 2
		}
	} else if printDiagnosis(out, report) != nil {
		return 2
	}
	return code
}

// diagnoseSpec runs the spec once and diagnoses a failure immediately, while
// its evidence is still fresh. It returns 0 when the test passed, 1 when it
// failed (diagnosed or not) and 2 when the run did not complete.
func diagnoseSpec(ctx context.Context, spec string, source []byte, config jev.Config, timeout time.Duration, receipts string, passEnv []string) (diagnoseReport, int) {
	report := diagnoseReport{Spec: spec}
	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return report, 2
	}
	dir := filepath.Join(receipts, time.Now().UTC().Format("20060102T150405Z")+"-"+hex.EncodeToString(nonce[:]))
	plan, err := runner.BuildPlan([]string{spec}, runner.PlanOptions{MaxJobs: 1, MaxParallel: 1, MaxAttempts: 1, MaxOutputBytes: 4 << 20, Adapters: []runner.Adapter{playwright.New()}})
	if err != nil || len(plan.Jobs) != 1 {
		return report, 2
	}
	summary, execErr := runner.Execute(ctx, plan, runner.ExecuteOptions{Workers: 1, Timeout: timeout, MaxAttempts: 1, MaxOutputBytes: 4 << 20, PassEnv: passEnv, ReceiptDir: dir, Adapters: []runner.Adapter{playwright.New()}})
	if execErr != nil || len(summary.Receipts) != 1 {
		return report, 2
	}
	receipt := summary.Receipts[0]
	report.Outcome, report.Receipt = string(receipt.Status), receipt.ReceiptPath
	if receipt.Status == runner.StatusPassed && receipt.Validated && receipt.ExecutedTests > 0 {
		return report, 0
	}
	sourceSHA := hex.EncodeToString(sha256Of(source))
	// Evidence is bound to the exact source that ran.
	if current, err := readSource(spec); err != nil || hex.EncodeToString(sha256Of(current)) != sourceSHA {
		d := jevassist.Diagnosis{Version: jevassist.DiagnosisVersion, Status: "error", Disposition: jevassist.Unresolved, Reason: "source_changed", Facts: jevassist.Facts{Outcome: "failed"}, Limitations: []string{"the spec changed during the run"}}
		report.Diagnosis = &d
		return report, 1
	}
	if len(receipt.Failures) == 0 {
		return report, 1 // timed out or canceled before a test failed: nothing to diagnose
	}
	revision, limitations := sourceRevision(spec, sourceSHA)
	evidence, more, err := jevassist.FromReceipt(receipt, revision, sourceSHA)
	if err != nil {
		return report, 1
	}
	var decider jevassist.Decider
	if config.Enabled {
		client, closeClient, err := newDiagnosisDecider(ctx, config, evidence.Identity, revision, sourceSHA)
		defer closeClient()
		if err == nil {
			decider = client
		} else {
			limitations = append(limitations, "Jev could not be prepared for this attempt")
		}
	}
	d := jevassist.Diagnosis{}
	if config.Enabled && decider == nil {
		d = jevassist.Diagnose(ctx, nil, evidence, config.Images, append(limitations, more...))
		d.Status, d.Reason = "error", "unavailable"
	} else {
		d = jevassist.Diagnose(ctx, decider, evidence, config.Images, append(limitations, more...))
	}
	d.Facts.ReceiptPath = receipt.ReceiptPath
	report.Diagnosis = &d
	if raw, err := json.MarshalIndent(d, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "diagnosis.json"), append(raw, '\n'), 0o600)
	}
	return report, 1
}

// sourceRevision is the checkout's commit, or the source hash outside git.
func sourceRevision(spec, sourceSHA string) (string, []string) {
	cmd := exec.Command("git", "-C", filepath.Dir(spec), "rev-parse", "HEAD")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if raw, err := cmd.Output(); err == nil {
		if revision := strings.TrimSpace(string(raw)); len(revision) == 40 || len(revision) == 64 {
			return revision, nil
		}
	}
	return sourceSHA, []string{"the spec is not in a git checkout; its source hash stands in for the revision"}
}

func sha256Of(raw []byte) []byte { sum := sha256.Sum256(raw); return sum[:] }

func printDiagnosis(w io.Writer, report diagnoseReport) error {
	var b strings.Builder
	if report.Diagnosis == nil {
		fmt.Fprintf(&b, "9l diagnose: %s %s; nothing to diagnose.\n", report.Spec, report.Outcome)
		_, err := io.WriteString(w, b.String())
		return err
	}
	d := report.Diagnosis
	fmt.Fprintf(&b, "9l diagnose: %s failed", firstNonEmptyString(d.Facts.Test, report.Spec))
	if d.Facts.FailureClass != "" {
		fmt.Fprintf(&b, " (%s)", d.Facts.FailureClass)
	}
	fmt.Fprintf(&b, "\nDisposition: %s · %s: %s\n", d.Disposition, d.Status, d.Reason)
	fmt.Fprintf(&b, "Observed facts: deterministic class %s", d.Facts.DeterministicClass)
	if d.Facts.FailedLocator != "" {
		fmt.Fprintf(&b, " · failed locator %s", d.Facts.FailedLocator)
	}
	b.WriteString("\n")
	if in := d.Inference; in != nil {
		fmt.Fprintf(&b, "Jev inference: %s (probability %.2f, confidence %.2f)\n", in.Family, in.FamilyProbability, in.Confidence)
		fmt.Fprintf(&b, "Jev observations: overlay %.2f · loading %.2f · visible error %.2f", in.Observations["overlay"], in.Observations["loading"], in.Observations["visibleError"])
		if v, ok := in.Observations["targetVisible"]; ok {
			fmt.Fprintf(&b, " · target present %.2f", v)
		}
		b.WriteString("\n")
	}
	for _, limitation := range d.Limitations {
		b.WriteString("Limit: " + limitation + "\n")
	}
	fmt.Fprintf(&b, "Result unchanged: failed. Receipt: %s\n", report.Receipt)
	_, err := io.WriteString(w, b.String())
	return err
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
