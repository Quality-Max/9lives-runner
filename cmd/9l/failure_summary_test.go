package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// A saved result is untrusted input: a hand-edited failure summary with a
// short or malformed test ID must neither panic `9l result` / `9l status` nor
// be shown; the receipt reads as one written before summaries existed.
func TestResultAndStatusWithholdAMalformedSavedFailureSummary(t *testing.T) {
	receipts := t.TempDir()
	runID := "run-hand-edited"
	directory := filepath.Join(receipts, runID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	plan := `{"version":1,"runId":"run-hand-edited","createdAt":"2026-11-02T00:00:00Z","jobs":[],"skipped":[],"limits":{"maxJobs":1,"maxParallel":1,"maxAttempts":1,"maxOutputBytes":1,"deadlineMs":1}}`
	valid := `{"stop":"test_failed","failedTests":1,"tests":[{"testId":"` + strings.Repeat("c2", 32) + `","class":"assertion","status":"failed","step":"assertion"}]}`
	for _, summary := range []string{
		`{"stop":"test_failed","failedTests":1,"tests":[{"testId":"abc","class":"assertion"}]}`,
		`{"stop":"test_failed","failedTests":1,"tests":[{"testId":"` + strings.Repeat("z", 64) + `","class":"assertion"}]}`,
		`{"stop":"Error: card 4242 declined","failedTests":0}`,
		`{"stop":"test_failed","failedTests":1,"tests":[{"class":"assertion","reason":"element Send not visible"}]}`,
		valid,
	} {
		result := `{"version":1,"outcome":"failed","runId":"run-hand-edited","startedAt":"2026-11-02T00:00:00Z","finishedAt":"2026-11-02T00:00:01Z","durationMs":1000,"complete":false,"plannedJobs":1,"skippedInputs":0,"passed":0,"failed":1,"canceled":0,"timedOut":0,"errors":0,` +
			`"receipts":[{"version":1,"runId":"run-hand-edited","jobId":"job-001","attemptId":"job-001-attempt-001","attempt":1,"spec":"a.spec.ts","adapter":"playwright-sdk","status":"failed","startedAt":"2026-11-02T00:00:00Z","finishedAt":"2026-11-02T00:00:01Z","durationMs":1000,"exitCode":1,"executed":true,"validated":true,"failureCount":1,"executedTests":1,"skippedTests":0,"verifiedAssertions":0,"assertionCoverage":"unknown","failureSummary":` + summary + `,"evidence":{"command":[],"artifacts":[]}}]}`
		for name, content := range map[string]string{"plan.json": plan, "result.json": result} {
			if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		for _, args := range [][]string{{"result", runID}, {"result", runID, "--format", "json"}, {"status", runID, "--format", "json"}} {
			var stdout, stderr bytes.Buffer
			if code := run(append(args, "--receipt-dir", receipts), &stdout, &stderr); code != 0 {
				t.Fatalf("%v with %s: code %d: %s", args, summary, code, stderr.String())
			}
			shown := strings.Contains(stdout.String(), "failureSummary") || strings.Contains(stdout.String(), "✗ test")
			if shown != (summary == valid) {
				t.Fatalf("%v with %s: shown=%v:\n%s", args, summary, shown, stdout.String())
			}
			for _, leaked := range []string{"4242", "Send not visible", "zzzz"} {
				if strings.Contains(stdout.String(), leaked) {
					t.Fatalf("%v: malformed summary text %q shown:\n%s", args, leaked, stdout.String())
				}
			}
		}
	}
	// The formatter is safe on its own too, for callers that did not load.
	var out bytes.Buffer
	printFailureSummary(&out, runner.FailureSummary{Stop: runner.StopTestFailed, FailedTests: 1, Tests: []runner.FailedTestSummary{{TestID: "abc", Class: runner.FailureAssertion}}})
	if out.Len() != 0 {
		t.Fatalf("malformed summary formatted: %s", out.String())
	}
}
