package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// The stub worker prints like a spec's console.log: one line with a
// secret-looking value and one on stderr.
const chattyWorker = `echo "spec console: password=fixture-live-secret"; echo "worker warning" >&2`

const ciNotice = "--show-output streams test output into the CI log"

func TestRunShowsWorkerOutputOnlyWhenAsked(t *testing.T) {
	t.Setenv("CI", "")
	spec := stubProject(t, junitPassingReport, 0, chattyWorker)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"run", spec, "--receipt-dir", t.TempDir(), "--timeout", "30s"}, &stdout, &stderr); code != exitPassed {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if all := stdout.String() + stderr.String(); strings.Contains(all, "spec console") || strings.Contains(all, "worker warning") || strings.Contains(all, ciNotice) {
		t.Fatalf("worker output shown without --show-output:\n%s", all)
	}
}

func TestRunShowOutputStreamsToStderrAndKeepsJSONStdout(t *testing.T) {
	t.Setenv("CI", "")
	spec := stubProject(t, junitPassingReport, 0, chattyWorker)
	receipts := t.TempDir()
	var stdout, stderr bytes.Buffer
	// A bool flag after the spec, as for every other run flag.
	code := run([]string{"run", spec, "--format", "json", "--receipt-dir", receipts, "--timeout", "30s", "--show-output"}, &stdout, &stderr)
	if code != exitPassed {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	var result runner.RunSummary
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Outcome != runner.OutcomePassed {
		t.Fatalf("stdout is not the JSON result: %v\n%s", err, stdout.String())
	}
	shown := stderr.String()
	if !strings.Contains(shown, "] spec console: password=[REDACTED]\n") || !strings.Contains(shown, " stderr] worker warning\n") {
		t.Fatalf("worker output missing from stderr:\n%s", shown)
	}
	if strings.Contains(stdout.String()+shown, "fixture-live-secret") || strings.Contains(stdout.String(), "spec console") {
		t.Fatalf("secret or worker output leaked:\nstdout: %s\nstderr: %s", stdout.String(), shown)
	}
	if strings.Contains(shown, ciNotice) {
		t.Fatal("CI notice printed outside CI")
	}
	stored, err := runner.LoadResult(receipts, result.RunID)
	if err != nil || len(stored.Receipts) != 1 {
		t.Fatalf("stored result: %v", err)
	}
	if raw, _ := json.Marshal(stored); strings.Contains(string(raw), "spec console") {
		t.Fatal("worker output reached the stored result")
	}
}

func TestRunShowOutputNotesTheCILog(t *testing.T) {
	t.Setenv("CI", "true")
	spec := stubProject(t, junitPassingReport, 0, chattyWorker)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"run", "--show-output", spec, "--receipt-dir", t.TempDir(), "--timeout", "30s"}, &stdout, &stderr); code != exitPassed {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if strings.Count(stderr.String(), ciNotice) != 1 || !strings.Contains(stderr.String(), "spec console: password=[REDACTED]") {
		t.Fatalf("stderr:\n%s", stderr.String())
	}
	if strings.Contains(stdout.String(), "spec console") {
		t.Fatalf("worker output on stdout:\n%s", stdout.String())
	}
}

func TestShowOutputIsARunFlagOnly(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"plan", "x.spec.ts", "--show-output"}, &stdout, &stderr); code != exitUsage || !strings.Contains(stderr.String(), "show-output") {
		t.Fatalf("plan accepted --show-output: %d %s", code, stderr.String())
	}
}

func TestPrintingReporterNamesPlaywrightTerminalReporters(t *testing.T) {
	for names, want := range map[string]bool{"": false, "html": false, "html,list": true, "line": true, "dot": true, "./list.ts": false} {
		got := printingReporter(strings.Split(names, ","))
		if got != want {
			t.Errorf("%q: %v, want %v", names, got, want)
		}
	}
}
