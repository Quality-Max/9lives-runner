package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/jevcost"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// seededProject is a stub Playwright project whose runs fail on a seeded
// schedule: the run numbers in failOn (1-based, counted in a file) write the
// failing report and exit 1, the others pass. Each run waits sleep seconds
// and records its arguments.
func seededProject(t *testing.T, failOn []int, sleep string) (spec, root string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub test runner is a shell script")
	}
	root = t.TempDir()
	writeTestFile(t, filepath.Join(root, "package.json"), `{"devDependencies":{"@playwright/test":"1.61.1"}}`)
	writeTestFile(t, filepath.Join(root, "a.spec.ts"), "// stub\n")
	writeTestFile(t, filepath.Join(root, "pass.json"), junitPassingReport)
	writeTestFile(t, filepath.Join(root, "fail.json"), junitFailingReport)
	failing := make([]string, len(failOn))
	for i, n := range failOn {
		failing[i] = fmt.Sprint(n)
	}
	script := fmt.Sprintf(`#!/bin/sh
root='%s'
n=$(cat "$root/count" 2>/dev/null || echo 0); n=$((n+1)); echo "$n" > "$root/count"
echo "$@" >> "$root/args"
sleep %s
for f in %s; do
  if [ "$n" -eq "$f" ]; then cat "$root/fail.json" > "$PLAYWRIGHT_JSON_OUTPUT_FILE"; exit 1; fi
done
cat "$root/pass.json" > "$PLAYWRIGHT_JSON_OUTPUT_FILE"
exit 0
`, root, sleep, strings.Join(failing, " "))
	binary := filepath.Join(root, "node_modules", ".bin", "playwright")
	writeTestFile(t, binary, script)
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "a.spec.ts"), root
}

func TestRepeatMeasuresASeededThirtyPercentFailure(t *testing.T) {
	spec, root := seededProject(t, []int{3, 6, 9}, "0")
	receipts := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"run", spec, "--repeat", "10", "--deadline", "5m", "--label", "no wait", "--receipt-dir", receipts, "--format", "json", "--timeout", "30s"}, &stdout, &stderr)
	if code != exitFailed {
		t.Fatalf("exit %d, want %d (a failed member fails the batch): %s", code, exitFailed, stderr.String())
	}
	var summary RepeatSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("aggregate: %v\n%s", err, stdout.String())
	}
	if summary.Requested != 10 || summary.Executed != 10 || summary.Passed != 7 || summary.Failed != 3 || summary.Incomplete != 0 ||
		summary.PassRate != 0.7 || summary.Outcome != "failed" || summary.Label != "no wait" || summary.StoppedBy != "" {
		t.Fatalf("summary %+v", summary)
	}
	if len(summary.Categories) != 2 || summary.Categories[0] != (RepeatCategory{"passed", 7}) || summary.Categories[1].Runs != 3 || !strings.HasPrefix(summary.Categories[1].Category, "test_failed") {
		t.Fatalf("histogram %+v", summary.Categories)
	}
	for i, member := range summary.Runs {
		want := "passed"
		if i == 2 || i == 5 || i == 8 {
			want = "failed"
		}
		if member.Outcome != want || member.RunID == "" {
			t.Fatalf("member %d: %+v", i+1, member)
		}
		// Each member is an ordinary run, readable and verifiable on its own.
		var result bytes.Buffer
		if c := run([]string{"result", member.RunID, "--receipt-dir", receipts}, &result, &stderr); c != 0 || !strings.Contains(result.String(), member.RunID) {
			t.Fatalf("member %s result: exit %d", member.RunID, c)
		}
	}
	if summary.Runs[0].RunID == summary.Runs[1].RunID {
		t.Fatal("members share a run ID")
	}
	// Playwright retries are off, so a retry cannot hide a failure.
	args, _ := os.ReadFile(filepath.Join(root, "args"))
	if summary.PlaywrightRetries == nil || *summary.PlaywrightRetries != 0 || strings.Count(string(args), "--retries=0") != 10 {
		t.Fatalf("retries %v, args:\n%s", summary.PlaywrightRetries, args)
	}
	// The aggregate is kept beside the receipts.
	saved, err := os.ReadFile(filepath.Join(receipts, "repeats", summary.BatchID+".json"))
	if err != nil || !bytes.Contains(saved, []byte(`"passRate": 0.7`)) {
		t.Fatalf("saved aggregate: %v\n%s", err, saved)
	}
}

func TestRepeatPassesOnlyWhenEveryMemberPassed(t *testing.T) {
	spec, _ := seededProject(t, nil, "0")
	var stdout, stderr bytes.Buffer
	code := run([]string{"run", spec, "--repeat", "3", "--deadline", "2m", "--receipt-dir", t.TempDir(), "--timeout", "30s"}, &stdout, &stderr)
	if code != exitPassed || !strings.Contains(stdout.String(), "Repeat: 3 of 3 runs passed (pass rate 100.0%), 0 failed, 0 incomplete; outcome passed") ||
		!strings.Contains(stdout.String(), "3× passed") || strings.Count(stdout.String(), "repeat ") != 3 {
		t.Fatalf("exit %d:\n%s\n%s", code, stdout.String(), stderr.String())
	}
}

func TestRepeatJevUsageIncludesIncompleteMembers(t *testing.T) {
	summary := RepeatSummary{}
	complete := jevcost.Totals{Requests: 1, RequestsWithUsage: 1, InputTokens: 122, OutputTokens: 8}
	incomplete := jevcost.Totals{Requests: 2, RequestsWithUsage: 1, InputTokens: 244, OutputTokens: 16}
	summary.add(runner.RunSummary{Complete: true, JevUsage: complete.Summary()}, false)
	summary.add(runner.RunSummary{JevUsage: incomplete.Summary()}, true)
	want := jevcost.Totals{Requests: 3, RequestsWithUsage: 2, InputTokens: 366, OutputTokens: 24}
	if got := summary.JevUsage; got == nil || got.Totals != want || got.InputRateEstimate == nil || got.InputRateEstimate.EstimatedCostNanoUSD != 15372 {
		t.Fatalf("repeat usage = %+v, want %+v and 15372 nano-USD", got, want)
	}
}

func TestRepeatStopsAtItsDeadlineWithAPartialResult(t *testing.T) {
	spec, _ := seededProject(t, nil, "2")
	var stdout, stderr bytes.Buffer
	code := run([]string{"run", spec, "--repeat", "5", "--deadline", "3s", "--receipt-dir", t.TempDir(), "--format", "json", "--timeout", "30s"}, &stdout, &stderr)
	var summary RepeatSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("aggregate: %v\n%s", err, stdout.String())
	}
	// Never passed: not every requested member ran.
	if code != exitIncomplete || summary.Outcome != "incomplete" || summary.Executed >= 5 || summary.Executed < 1 || summary.StoppedBy != "deadline" {
		t.Fatalf("exit %d summary %+v\n%s", code, summary, stderr.String())
	}
}

func TestRepeatRefusesWhatWouldHideOrMisreportFailures(t *testing.T) {
	spec, _ := seededProject(t, nil, "0")
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--repeat", "3"}, "--repeat needs --deadline"},
		{[]string{"--repeat", "3", "--deadline", "1m", "--attempts", "2"}, "--repeat measures single attempts"},
		{[]string{"--repeat", "3", "--deadline", "1m", "--junit", "j.xml"}, "cannot be combined with --repeat"},
		{[]string{"--label", "x"}, "--label applies only with --repeat"},
		{[]string{"--repeat", "0", "--deadline", "1m"}, "--repeat must be 1 to 1000"},
		{[]string{"--repeat", "1001", "--deadline", "1m"}, "--repeat must be 1 to 1000"},
		{[]string{"--repeat", "2", "--deadline", "1m", "--label", "a\nb"}, "--label is one line"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(append([]string{"run", spec}, c.args...), &stdout, &stderr); code != exitUsage || !strings.Contains(stderr.String(), c.want) {
			t.Fatalf("%v: exit %d %q", c.args, code, stderr.String())
		}
	}
}

func TestRepeatStopsOnCancellationAndCountsTheStoppedMember(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	execute := func(plan runner.Plan, _ time.Duration) (runner.RunSummary, error) {
		calls++
		if calls == 2 {
			cancel()
			return runner.RunSummary{RunID: plan.RunID, Outcome: runner.OutcomeIncomplete, Canceled: 1}, context.Canceled
		}
		return runner.RunSummary{RunID: plan.RunID, Outcome: runner.OutcomePassed, Complete: true, Passed: 1}, nil
	}
	build := func(time.Duration) (runner.Plan, error) {
		return runner.Plan{RunID: fmt.Sprintf("run-%d", calls+1)}, nil
	}
	var stdout, stderr bytes.Buffer
	receipts := t.TempDir()
	code := runRepeat(ctx, 5, "", time.Minute, nil, runner.Plan{RunID: "run-1"}, build, execute, "json", receipts, &stdout, &stderr)
	var summary RepeatSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if code != exitIncomplete || calls != 2 || summary.Executed != 2 || summary.Passed != 1 || summary.Incomplete != 1 || summary.StoppedBy != "canceled" || summary.Outcome != "incomplete" {
		t.Fatalf("exit %d calls %d summary %+v", code, calls, summary)
	}
}
