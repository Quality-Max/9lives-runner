package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/jevcost"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// `9l run --repeat N` runs the same job N times, one complete run after
// another, to measure how often it passes. Each member is an ordinary run with
// its own run ID and receipts, verifiable on its own; the batch adds only an
// aggregate. Repeats measure, they never retry: a failed member stays failed
// and the batch never reports passed unless every member ran and passed.

// RepeatSummaryVersion is the version of the aggregate `9l run --repeat` writes.
const RepeatSummaryVersion = 1

// MaxRepeat bounds --repeat.
const MaxRepeat = 1000

type RepeatMember struct {
	RunID      string `json:"runId"`
	Outcome    string `json:"outcome"`
	Complete   bool   `json:"complete"`
	Category   string `json:"category"`
	DurationMS int64  `json:"durationMs"`
}

type RepeatCategory struct {
	Category string `json:"category"`
	Runs     int    `json:"runs"`
}

type RepeatSummary struct {
	Version int    `json:"version"`
	BatchID string `json:"batchId"`
	Label   string `json:"label,omitempty"`
	// Requested is --repeat; Executed the members that ran. A batch stopped
	// by its deadline or a cancellation has fewer.
	Requested int    `json:"requested"`
	Executed  int    `json:"executed"`
	StoppedBy string `json:"stoppedBy,omitempty"`
	// Outcome is passed only when every requested member ran, completed and
	// passed; failed when any member failed; otherwise incomplete.
	Outcome    string `json:"outcome"`
	Passed     int    `json:"passed"`
	Failed     int    `json:"failed"`
	Incomplete int    `json:"incomplete"`
	// PassRate is Passed / Executed, rounded to three decimals.
	PassRate float64 `json:"passRate"`
	// PlaywrightRetries is the Playwright retry count each member ran with:
	// 0 unless --retries named another, so a retry cannot hide a failure.
	PlaywrightRetries *int `json:"playwrightRetries,omitempty"`
	// Categories counts members by why they ended, most frequent first:
	// "passed", or the failure summary's metadata tokens (stop, class, step,
	// operation, reason, @control), never messages, titles or app content.
	Categories []RepeatCategory `json:"categories"`
	Runs       []RepeatMember   `json:"runs"`
	StartedAt  time.Time        `json:"startedAt"`
	FinishedAt time.Time        `json:"finishedAt"`
	JevUsage   *jevcost.Summary `json:"jevUsage,omitempty"`
}

func newBatchID(now time.Time) string {
	var suffix [6]byte
	_, _ = rand.Read(suffix[:])
	return "repeat-" + now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(suffix[:])
}

// memberOutcome maps a member run to passed, failed or incomplete with the
// same rule as `9l run`'s exit code.
func memberOutcome(result runner.RunSummary, executionFailed bool) string {
	switch runExitCode(result, executionFailed) {
	case exitPassed:
		return "passed"
	case exitFailed:
		return "failed"
	default:
		return "incomplete"
	}
}

// memberCategory names why a member ended from metadata only: its first
// failure summary's tokens, or its outcome when it carries none.
func memberCategory(result runner.RunSummary, outcome string) string {
	if outcome == "passed" {
		return "passed"
	}
	for _, receipt := range result.Receipts {
		summary := receipt.FailureSummary
		if summary == nil || summary.Stop == "" {
			continue
		}
		parts := []string{summary.Stop}
		if len(summary.Tests) > 0 {
			test := summary.Tests[0]
			for _, token := range []string{test.Class, test.Step, test.Operation, test.Reason} {
				if token != "" {
					parts = append(parts, token)
				}
			}
			if test.Control != "" {
				parts = append(parts, "@"+test.Control)
			}
		}
		return strings.Join(parts, ":")
	}
	return outcome
}

func (s *RepeatSummary) add(result runner.RunSummary, executionFailed bool) RepeatMember {
	outcome := memberOutcome(result, executionFailed)
	member := RepeatMember{RunID: result.RunID, Outcome: outcome, Complete: result.Complete && !executionFailed, Category: memberCategory(result, outcome), DurationMS: result.DurationMS}
	s.Runs = append(s.Runs, member)
	s.Executed++
	if result.JevUsage != nil {
		totals := result.JevUsage.Totals
		if s.JevUsage != nil {
			totals.Add(s.JevUsage.Totals)
		}
		s.JevUsage = totals.Summary()
	}
	switch outcome {
	case "passed":
		s.Passed++
	case "failed":
		s.Failed++
	default:
		s.Incomplete++
	}
	return member
}

// finish computes the outcome, pass rate and histogram.
func (s *RepeatSummary) finish(now time.Time) {
	s.FinishedAt = now
	switch {
	case s.Failed > 0:
		s.Outcome = "failed"
	case s.Executed == s.Requested && s.Incomplete == 0 && s.Passed == s.Requested:
		s.Outcome = "passed"
	default:
		s.Outcome = "incomplete"
	}
	if s.Executed > 0 {
		s.PassRate = math.Round(float64(s.Passed)*1000/float64(s.Executed)) / 1000
	}
	counts := map[string]int{}
	for _, member := range s.Runs {
		counts[member.Category]++
	}
	s.Categories = []RepeatCategory{}
	for category, runs := range counts {
		s.Categories = append(s.Categories, RepeatCategory{category, runs})
	}
	sort.Slice(s.Categories, func(a, b int) bool {
		if s.Categories[a].Runs != s.Categories[b].Runs {
			return s.Categories[a].Runs > s.Categories[b].Runs
		}
		return s.Categories[a].Category < s.Categories[b].Category
	})
}

func (s *RepeatSummary) exitCode() int {
	switch s.Outcome {
	case "passed":
		return exitPassed
	case "failed":
		return exitFailed
	default:
		return exitIncomplete
	}
}

// save writes the aggregate beside the runs' receipts.
func (s *RepeatSummary) save(receiptDir string) (string, error) {
	dir := filepath.Join(receiptDir, "repeats")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, s.BatchID+".json")
	temp := path + ".tmp"
	if err := os.WriteFile(temp, append(data, '\n'), 0o644); err != nil {
		return "", err
	}
	return path, os.Rename(temp, path)
}

func printRepeatMember(out io.Writer, index, total int, member RepeatMember) {
	fmt.Fprintf(out, "repeat %d/%d: run %s %s", index, total, member.RunID, member.Outcome)
	if member.Category != member.Outcome {
		fmt.Fprintf(out, " (%s)", member.Category)
	}
	fmt.Fprintln(out)
}

func printRepeatSummary(out io.Writer, s RepeatSummary, path string) {
	label := ""
	if s.Label != "" {
		label = fmt.Sprintf(" %q", s.Label)
	}
	fmt.Fprintf(out, "Repeat%s: %d of %d runs passed (pass rate %.1f%%), %d failed, %d incomplete", label, s.Passed, s.Executed, s.PassRate*100, s.Failed, s.Incomplete)
	if s.Executed < s.Requested {
		fmt.Fprintf(out, "; stopped by %s after %d of %d", s.StoppedBy, s.Executed, s.Requested)
	}
	fmt.Fprintf(out, "; outcome %s\n", s.Outcome)
	for _, c := range s.Categories {
		fmt.Fprintf(out, "  %d× %s\n", c.Runs, c.Category)
	}
	printJevUsage(out, s.JevUsage)
	if path != "" {
		fmt.Fprintln(out, "Aggregate:", path)
	}
}

// runRepeat runs the batch. The first member uses the plan already built
// and checked; each later one is planned again (a new run ID) with what is
// left of the batch deadline. A cancellation or the deadline ends the batch
// after the member it stopped; that member still counts, as incomplete
// unless it finished.
func runRepeat(ctx context.Context, repeat int, label string, deadline time.Duration, retries *int, first runner.Plan,
	build func(time.Duration) (runner.Plan, error), execute func(runner.Plan, time.Duration) (runner.RunSummary, error),
	format, receiptDir string, out, errOut io.Writer) int {
	started := time.Now()
	summary := RepeatSummary{Version: RepeatSummaryVersion, BatchID: newBatchID(started), Label: label, Requested: repeat, PlaywrightRetries: retries, StartedAt: started}
	progress := out
	if format == "json" {
		progress = errOut
	}
	end := started.Add(deadline)
	for index := 1; index <= repeat; index++ {
		if ctx.Err() != nil {
			summary.StoppedBy = "canceled"
			break
		}
		remaining := time.Until(end)
		if remaining < time.Second {
			summary.StoppedBy = "deadline"
			break
		}
		plan := first
		if index > 1 {
			var err error
			if plan, err = build(remaining); err != nil {
				fmt.Fprintf(errOut, "9l: plan: %v\n", err)
				summary.StoppedBy = "plan_failed"
				break
			}
		}
		result, err := execute(plan, remaining)
		var setup runner.SetupError
		if errors.As(err, &setup) {
			// Nothing started: no member to count.
			fmt.Fprintf(errOut, "9l: %v\n", err)
			summary.StoppedBy = "setup_failed"
			break
		}
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			fmt.Fprintf(errOut, "9l: execution: %v\n", err)
		}
		member := summary.add(result, err != nil)
		printRepeatMember(progress, index, repeat, member)
		if ctx.Err() != nil {
			summary.StoppedBy = "canceled"
			break
		}
		if errors.Is(err, context.DeadlineExceeded) || time.Until(end) < time.Second {
			if index < repeat {
				summary.StoppedBy = "deadline"
			}
			break
		}
	}
	summary.finish(time.Now())
	path, err := summary.save(receiptDir)
	if err != nil {
		fmt.Fprintln(errOut, "9l: the repeat aggregate could not be written:", err)
		path = ""
	}
	if format == "json" {
		_ = json.NewEncoder(out).Encode(summary)
		if path != "" {
			fmt.Fprintln(errOut, "Aggregate:", path)
		}
	} else {
		printRepeatSummary(out, summary, path)
	}
	code := summary.exitCode()
	if path == "" && code == exitPassed {
		// An aggregate that could not be kept does not prove the batch.
		code = exitIncomplete
	}
	return code
}
