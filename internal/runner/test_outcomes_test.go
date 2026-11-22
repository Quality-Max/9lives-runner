package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"
)

// listingAdapter validates like fakeAdapter and lists a test per outcome it
// is told to: failed tests last, so bounding has to reach for them.
type listingAdapter struct {
	fakeAdapter
	passed, failed int
	titles         map[string]string
}

func (adapter listingAdapter) Validate(stdout []byte) (Validation, error) {
	validation, err := adapter.fakeAdapter.Validate(stdout)
	if err != nil {
		return validation, err
	}
	validation.ExecutedTests, validation.FailureCount = adapter.passed+adapter.failed, adapter.failed
	for index := range adapter.passed {
		validation.Tests = append(validation.Tests, TestOutcome{TestID: fmt.Sprintf("%064x", index), Status: TestPassed})
	}
	for index := range adapter.failed {
		validation.Tests = append(validation.Tests, TestOutcome{TestID: fmt.Sprintf("%064x", adapter.passed+index), Status: TestFailed, Class: FailureAssertion})
	}
	return validation, nil
}

func (adapter listingAdapter) TestTitles([]byte) map[string]string { return adapter.titles }

// withoutDetails is an engine adapter run without --failure-details: it has
// a diagnostics channel and did not open it, so no report names its tests.
type withoutDetails struct{ listingAdapter }

func (withoutDetails) DiagnosticsEnv() string { return "" }

func executeOne(t *testing.T, mode string, adapter Adapter) Receipt {
	t.Helper()
	plan := Plan{Version: 1, RunID: "run-tests-" + mode, Jobs: []Job{fakeJob("job-001", mode, 0)}}
	summary, _ := Execute(context.Background(), plan, ExecuteOptions{Workers: 1, Timeout: 10 * time.Second, MaxAttempts: 1, ReceiptDir: t.TempDir(), Adapters: []Adapter{adapter}})
	if len(summary.Receipts) != 1 {
		t.Fatalf("unexpected result: %+v", summary)
	}
	// What a report reads later is the persisted receipt.
	var persisted Receipt
	if raw, err := os.ReadFile(summary.Receipts[0].ReceiptPath); err != nil || json.Unmarshal(raw, &persisted) != nil {
		t.Fatalf("receipt unreadable: %v", err)
	}
	return persisted
}

func TestReceiptListsTestsOnlyForValidatedEvidence(t *testing.T) {
	passed := executeOne(t, "success", listingAdapter{passed: 3})
	if passed.Status != StatusPassed || len(passed.Tests) != 3 || passed.TestsTruncated || !passed.ValidTests() {
		t.Fatalf("validated pass lists its tests: %+v", passed)
	}
	failed := executeOne(t, "test-failure", listingAdapter{passed: 2, failed: 1})
	if failed.Status != StatusFailed || len(failed.Tests) != 3 || failed.Tests[2].Status != TestFailed || failed.Tests[2].Class != FailureAssertion || !failed.ValidTests() {
		t.Fatalf("validated failure lists its tests: %+v", failed)
	}
	// The worker died before reporting: nothing validated, nothing listed.
	if invalid := executeOne(t, "infrastructure", listingAdapter{passed: 3}); invalid.Validated || len(invalid.Tests) != 0 || invalid.TestsTruncated {
		t.Fatalf("an attempt that did not validate lists tests: %+v", invalid)
	}
	// A list that disagrees with the adapter's own counts refuses the attempt.
	if lying := executeOne(t, "success", countingAdapter{}); lying.Validated || lying.Status != StatusError || len(lying.Tests) != 0 {
		t.Fatalf("an inconsistent list validated: %+v", lying)
	}
}

// countingAdapter lists fewer tests than it counts.
type countingAdapter struct{ fakeAdapter }

func (countingAdapter) Validate([]byte) (Validation, error) {
	return Validation{ExecutedTests: 2, AssertionCoverage: "unknown", Tests: []TestOutcome{{Status: TestPassed}}}, nil
}

func TestReceiptTestListIsBoundedAndKeepsFailures(t *testing.T) {
	receipt := executeOne(t, "test-failure", listingAdapter{passed: MaxReportedTests + 500, failed: 7})
	failed := 0
	for _, test := range receipt.Tests {
		if test.Status == TestFailed {
			failed++
		}
	}
	if len(receipt.Tests) != MaxReportedTests || !receipt.TestsTruncated || failed != 7 || receipt.ExecutedTests != MaxReportedTests+507 || !receipt.ValidTests() {
		t.Fatalf("list of %d (truncated %v) with %d of 7 failures", len(receipt.Tests), receipt.TestsTruncated, failed)
	}
}

func TestEngineTitlesComeOnlyFromTheReportThatIdentifiesTheTest(t *testing.T) {
	titles := map[string]string{fmt.Sprintf("%064x", 0): "cart \x1b[31m› adds\nan item password=hunter2", strings.Repeat("f", 64): "not in this run"}
	receipt := executeOne(t, "success", listingAdapter{passed: 2, titles: titles})
	if receipt.Tests[0].Title != "cart › adds an item password=[REDACTED]" || receipt.Tests[1].Title != "" || !receipt.ValidTests() {
		t.Fatalf("titles: %+v", receipt.Tests)
	}
	// Without failure details no title is kept, whatever the adapter could name.
	for _, test := range executeOne(t, "success", withoutDetails{listingAdapter{passed: 2, titles: titles}}).Tests {
		if test.Title != "" || test.TestID == "" {
			t.Fatalf("a title was kept without failure details: %+v", test)
		}
	}
}

func TestTestOutcomeValidationIsClosed(t *testing.T) {
	valid := []TestOutcome{
		{Status: TestPassed}, {Status: TestSkipped}, {Status: TestFlaky, DurationMS: 12},
		{TestID: digest, Status: TestFailed, Class: FailureSetup, Title: "[chromium] cart › total"},
	}
	for _, test := range valid {
		if err := test.Validate(); err != nil {
			t.Errorf("%+v rejected: %v", test, err)
		}
	}
	for name, test := range map[string]TestOutcome{
		"unknown status":         {Status: "interrupted"},
		"empty status":           {},
		"failed without a class": {Status: TestFailed},
		"unknown class":          {Status: TestFailed, Class: "Error: card 4242 declined"},
		"class on a pass":        {Status: TestPassed, Class: FailureAssertion},
		"short test id":          {TestID: "abc", Status: TestPassed},
		"negative duration":      {Status: TestPassed, DurationMS: -1},
		"absurd duration":        {Status: TestPassed, DurationMS: MaxTestDurationMS + 1},
		"multi-line title":       {Status: TestPassed, Title: "one\ntwo"},
		"terminal escape":        {Status: TestPassed, Title: "\x1b[31mred"},
		"oversized title":        {Status: TestPassed, Title: strings.Repeat("a", MaxTestTitleBytes+1)},
		"invalid UTF-8 title":    {Status: TestPassed, Title: "bad\xff"},
	} {
		if test.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestTestTitleIsAlwaysAValidBoundedTitle(t *testing.T) {
	for _, raw := range []string{
		"", "plain", "a\r\n\tb", "\x1b[1mbold\x1b[0m \x1b]8;;http://x\x07link", "bad\xff\xfeutf8", "\x00\x01\x7f\u0085",
		strings.Repeat("é", 2000), "token=abcdef123456 " + strings.Repeat("x", 600),
	} {
		title := TestTitle(raw)
		if err := (TestOutcome{Status: TestPassed, Title: title}).Validate(); err != nil || strings.ContainsFunc(title, unicode.IsControl) || strings.Contains(title, "abcdef123456") {
			t.Errorf("TestTitle(%q) = %q: %v", raw, title, err)
		}
	}
}

func TestReceiptTestListMustAgreeWithItsCounts(t *testing.T) {
	base := Receipt{Validated: true, Status: StatusFailed, ExecutedTests: 3, FailureCount: 1, SkippedTests: 1, FlakyTests: 1, Tests: []TestOutcome{
		{Status: TestPassed}, {Status: TestFlaky}, {Status: TestFailed, Class: FailureTimeout}, {Status: TestSkipped}}}
	if !base.ValidTests() || !(Receipt{}).ValidTests() {
		t.Fatal("a consistent list, or none, must be valid")
	}
	for name, change := range map[string]func(*Receipt){
		"not validated":           func(r *Receipt) { r.Validated = false },
		"a failure listed passed": func(r *Receipt) { r.Tests[2] = TestOutcome{Status: TestPassed} },
		"an extra pass":           func(r *Receipt) { r.Tests = append(r.Tests, TestOutcome{Status: TestPassed}) },
		"a missing entry":         func(r *Receipt) { r.Tests = r.Tests[:3] },
		"truncated but complete":  func(r *Receipt) { r.TestsTruncated = true },
		"malformed entry":         func(r *Receipt) { r.Tests[0].Title = "a\nb" },
		"more failures listed than counted": func(r *Receipt) {
			r.TestsTruncated, r.ExecutedTests = true, 9
			r.Tests[0] = TestOutcome{Status: TestFailed, Class: FailureOther}
		},
		"truncated flag without a list": func(r *Receipt) { r.Tests, r.TestsTruncated = nil, true },
	} {
		receipt := base
		receipt.Tests = append([]TestOutcome{}, base.Tests...)
		change(&receipt)
		if receipt.ValidTests() {
			t.Errorf("%s: accepted", name)
		}
	}
	// A truncated list may hold fewer of each outcome, never more.
	truncated := base
	truncated.Tests, truncated.TestsTruncated = base.Tests[2:3], true
	if !truncated.ValidTests() {
		t.Fatal("a truncated list within the counts must be valid")
	}
}

// A saved result is untrusted input: a hand-edited list is withheld on load,
// and the rest of the receipt still reads.
func TestLoadWithholdsAMalformedSavedTestList(t *testing.T) {
	root, runID := t.TempDir(), "run-edited-tests"
	directory := filepath.Join(root, runID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	good := Receipt{Version: ReceiptVersion, RunID: runID, JobID: "job-001", Status: StatusPassed, Executed: true, Validated: true, ExecutedTests: 1, Tests: []TestOutcome{{Status: TestPassed, Title: "kept"}}}
	edited := good
	edited.JobID, edited.Status, edited.FailureCount = "job-002", StatusFailed, 1
	edited.Tests = []TestOutcome{{Status: TestPassed, Title: "edited to pass"}}
	hostile := good
	hostile.JobID, hostile.Tests = "job-003", []TestOutcome{{Status: TestPassed, Title: "\x1b]0;title\x07"}}
	summary := RunSummary{Version: RunSummaryVersion, Outcome: OutcomeFailed, RunID: runID, PlannedJobs: 3, Receipts: []Receipt{good, edited, hostile}}
	if err := writeJSONAtomic(directory, "result.json", summary); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(directory, "plan.json", Plan{Version: PlanVersion, RunID: runID, Jobs: make([]Job, 3)}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadResult(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	status, err := LoadStatus(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range []RunSummary{loaded, *status.Result} {
		if len(result.Receipts[0].Tests) != 1 || result.Receipts[1].Tests != nil || result.Receipts[2].Tests != nil || result.Receipts[1].FailureCount != 1 {
			t.Fatalf("malformed lists were not withheld: %+v", result.Receipts)
		}
	}
}
