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
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/adapters/playwright"
	"github.com/Quality-Max/9lives-runner/internal/healing"
	"github.com/Quality-Max/9lives-runner/internal/healing/tier2"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// healOptions are the inputs of one native healing session, shared by
// `9l heal` and the MCP heal_test tool.
type healOptions struct {
	Spec string
	// Provider proposes Tier 2 candidates; nil heals with offline Tier 1 only.
	Provider     tier2.Provider
	Model        string
	MaxProposals int
	RunTimeout   time.Duration
	ReceiptDir   string
	PassEnv      []string
	Apply        bool
	Interactive  func(context.Context) bool
	Preview      func(original, candidate string)
}

var receiptLabel = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// healSpec runs the original spec, then verifies each candidate in its own
// owned copy before saving or applying it.
func healSpec(ctx context.Context, o healOptions) (tier2.Session, error) {
	runCount := 0
	runSpec := func(ctx context.Context, spec, label string) tier2.RunResult {
		runCount++
		// tier2.Heal labels runs "original", "tier1" and "tier2-<n>"; anything
		// else must not become a path component of the receipt directory.
		if !receiptLabel.MatchString(label) {
			label = "run"
		}
		plan, err := runner.BuildPlan([]string{spec}, runner.PlanOptions{MaxJobs: 1, MaxParallel: 1, MaxAttempts: 1, MaxOutputBytes: 4 << 20, Adapters: []runner.Adapter{playwright.New()}})
		if err != nil || len(plan.Jobs) != 1 {
			return tier2.RunResult{Failure: "spec is not an installed Playwright project"}
		}
		dir := filepath.Join(o.ReceiptDir, fmt.Sprintf("%02d-%s", runCount, label))
		summary, execErr := runner.Execute(ctx, plan, runner.ExecuteOptions{Workers: 1, Timeout: o.RunTimeout, MaxAttempts: 1, MaxOutputBytes: 4 << 20, PassEnv: o.PassEnv, ReceiptDir: dir, Adapters: []runner.Adapter{playwright.New()}})
		if execErr != nil || len(summary.Receipts) != 1 {
			return tier2.RunResult{Failure: "verification did not complete"}
		}
		receipt := summary.Receipts[0]
		failure := string(receipt.Status)
		if receipt.Evidence.StdoutPath != "" && receipt.Status != runner.StatusPassed {
			if raw, readErr := os.ReadFile(receipt.Evidence.StdoutPath); readErr == nil {
				if diagnostic := playwright.FailureContext(raw); diagnostic != "" {
					failure = diagnostic
				}
			}
		}
		// Read the ARIA snapshot now: the next verification run clears
		// Playwright's output directory.
		return tier2.RunResult{Passed: receipt.Status == runner.StatusPassed && receipt.Validated && receipt.ExecutedTests > 0, ExecutedTests: receipt.ExecutedTests, Failure: failure, Receipt: receipt.ReceiptPath, Snapshot: playwright.PageSnapshot(receipt.Failures)}
	}
	return tier2.Heal(ctx, tier2.SessionOptions{Spec: o.Spec, Framework: "playwright", Model: o.Model, MaxProposals: o.MaxProposals, Apply: o.Apply, Interactive: o.Interactive, Preview: o.Preview, Provider: o.Provider, Run: runSpec}, func(source, failure, snapshot string) (string, bool) {
		proposal := healing.Heal(healing.Request{Version: healing.Version, Framework: "playwright", ErrorMessage: failure, FailedSelector: healing.FailedLocator(failure), TestCode: source, AriaSnapshot: snapshot})
		if old, _ := proposal.Metadata["oldSelector"].(string); healing.EquivalentSelectors(old, fmt.Sprint(proposal.Metadata["newSelector"])) {
			return "", false // selects the same missing element; skip its verification run
		}
		return proposal.ProposedCode, proposal.Decision == "propose"
	})
}

// resolveHealProvider returns the Tier 2 provider. A provider named with
// --provider or NINELIVES_PROVIDER must resolve. Otherwise an installed agent
// CLI or configured API key is used when present, and nil means healing is
// offline Tier 1 only.
func resolveHealProvider(name, model, baseURL string) (tier2.Provider, error) {
	// "none" keeps healing offline even when an agent CLI or API key is
	// present, without stripping PATH or the environment.
	explicit := strings.ToLower(strings.TrimSpace(name))
	if explicit == "none" || explicit == "" && tier2.EnvironmentProvider() == "none" {
		return nil, nil
	}
	provider, err := tier2.Resolve(tier2.Options{Name: name, Model: model, BaseURL: baseURL})
	if err != nil {
		if name != "" || os.Getenv("NINELIVES_PROVIDER") != "" {
			return nil, err
		}
		return nil, nil
	}
	return provider, nil
}

// runTimeoutFlag accepts a Go duration such as 5m, or whole seconds as the
// Python CLI's --run-timeout did.
type runTimeoutFlag struct{ value *time.Duration }

func (f runTimeoutFlag) String() string {
	if f.value == nil {
		return ""
	}
	return f.value.String()
}

func (f runTimeoutFlag) Set(raw string) error {
	if seconds, err := strconv.Atoi(raw); err == nil {
		*f.value = time.Duration(seconds) * time.Second
		return nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return errors.New("use a duration such as 5m or whole seconds")
	}
	*f.value = parsed
	return nil
}

func healCommand(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("heal", flag.ContinueOnError)
	fs.SetOutput(errOut)
	providerName := fs.String("provider", "", "Tier 2 provider (claude, codex, opencode, anthropic, openai, or none for offline Tier 1 only); default: an installed agent CLI or configured API key")
	model := fs.String("model", os.Getenv("NINELIVES_MODEL"), "provider model")
	providerURL := fs.String("provider-url", "", "local/provider HTTP URL")
	yes := fs.Bool("yes", false, "apply verified candidate without interactive approval")
	maxProposals := fs.Int("max-proposals", 1, "maximum Tier 2 proposals")
	runTimeout := nativeRunTimeout()
	fs.Var(runTimeoutFlag{&runTimeout}, "run-timeout", "per verification run timeout, as a duration or whole seconds")
	deadline := fs.Duration("deadline", 0, "session deadline")
	receipts := fs.String("receipt-dir", ".9lives/healing-receipts", "isolated verification receipts")
	var passEnv envNames
	fs.Var(&passEnv, "pass-env", "forward this environment variable only to test processes (repeatable or comma-separated)")
	normalized := flagsFirst(args, map[string]bool{"--provider": true, "--model": true, "--provider-url": true, "--max-proposals": true, "--run-timeout": true, "--deadline": true, "--receipt-dir": true, "--pass-env": true})
	if err := fs.Parse(normalized); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(errOut, "9l: unknown heal option; Python-only options such as --framework need the Python CLI: `9lives heal`")
		return 2
	}
	if fs.NArg() != 1 || *maxProposals < 1 || runTimeout <= 0 || *deadline < 0 {
		fmt.Fprintln(errOut, "9l: heal requires one spec and valid limits")
		return 2
	}
	provider, err := resolveHealProvider(*providerName, *model, *providerURL)
	if err != nil {
		fmt.Fprintln(errOut, "9l: heal provider unavailable")
		return 2
	}
	switch {
	case provider != nil:
		// A provider call sends the spec source and failure text off the
		// machine and may bill the account; say which before any call.
		fmt.Fprintf(errOut, "9l: healing provider: %s; --provider none heals offline\n", tier2.Describe(provider))
	case strings.EqualFold(strings.TrimSpace(*providerName), "none") || *providerName == "" && tier2.EnvironmentProvider() == "none":
		fmt.Fprintln(errOut, "9l: provider none: healing with offline Tier 1 only")
	default:
		fmt.Fprintln(errOut, "9l: no Tier 2 provider found; healing with offline Tier 1 only")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *deadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *deadline)
		defer cancel()
	}
	result, err := healSpec(ctx, healOptions{Spec: fs.Arg(0), Provider: provider, Model: *model, MaxProposals: *maxProposals, RunTimeout: runTimeout, ReceiptDir: *receipts, PassEnv: passEnv, Apply: *yes, Interactive: terminalApproval(os.Stdin, errOut), Preview: previewDiff(errOut)})
	if encodeErr := json.NewEncoder(out).Encode(result); encodeErr != nil {
		return 1
	}
	if err != nil && ctx.Err() == nil {
		fmt.Fprintln(errOut, "9l: heal:", healFailure(result, *yes))
	}
	if err != nil || (!result.Applied && result.SavedPath == "" && result.State != "passed") {
		return 1
	}
	return 0
}

// healFailure describes a session that ended with an error, without the
// raw error, which can carry local paths.
func healFailure(session tier2.Session, apply bool) string {
	switch {
	case session.Reason != "":
		return session.Reason
	case session.State == "concurrent_edit":
		return "source changed during healing; the verified candidate was not applied"
	case session.State == "verified" && apply:
		return "the verified candidate could not be applied"
	case session.State == "verified":
		return "the verified candidate could not be saved"
	default:
		return "healing did not complete"
	}
}

// unifiedLines is the line-pair diff previewDiff prints, as a string.
func unifiedLines(original, candidate string) string {
	var diff strings.Builder
	previewDiff(&diff)(original, candidate)
	return diff.String()
}
