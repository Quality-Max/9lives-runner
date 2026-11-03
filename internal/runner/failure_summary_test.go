package runner

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

var digest = strings.Repeat("ab", 32)

func TestEngineFailureClassUsesStructuredFactsOnly(t *testing.T) {
	for _, test := range []struct {
		status, step, reason, setup, want string
	}{
		{"failed", "assertion", "", "", FailureAssertion},
		{"failed", "assertion", "assertion_failed", "", FailureAssertion},
		{"failed", "action", "deadline", "", FailureActionTimeout},
		{"failed", "assertion", "deadline", "", FailureActionTimeout},          // no completed check before the bound
		{"failed", "action", "operation_timeout", "", FailureActionTimeout},    // the call's own bound, not the attempt's
		{"failed", "assertion", "operation_timeout", "", FailureActionTimeout}, // no completed check before the call's bound
		{"failed", "assertion", "operation_failed", "", FailureOther},          // failed reads are not a mismatch
		{"failed", "action", "setup_blocked", "", FailureSetup},
		{"failed", "assertion", "assertion_failed", SetupBlocked, FailureSetup}, // a recheck reclassified it
		{"timedOut", "assertion", "", "", FailureTimeout},
		{"timedOut", "action", "", SetupObserver, FailureSetup},
		{"failed", "browser", "", "", FailureOther},
		{"failed", "", "", "", FailureOther},
		{"passed", "assertion", "assertion_failed", "", FailureUnexpectedPass},
	} {
		if got := EngineFailureClass(test.status, test.step, test.reason, test.setup); got != test.want {
			t.Errorf("%+v: got %s", test, got)
		}
	}
}

func TestFailureStopNamesWhyTheAttemptEnded(t *testing.T) {
	mismatched := &ProvenanceAssessment{Status: "mismatched"}
	for _, test := range []struct {
		name    string
		receipt Receipt
		want    string
	}{
		{"test failure", Receipt{Status: StatusFailed, Executed: true, Validated: true, FailureCount: 1}, StopTestFailed},
		{"goal only", Receipt{Status: StatusFailed, Executed: true, Validated: true, FailureCount: 1, GoalFailed: true}, StopGoalFailed},
		{"goal beside a failure", Receipt{Status: StatusFailed, Executed: true, Validated: true, FailureCount: 2, GoalFailed: true, NonGoalFailureCount: 1}, StopTestFailed},
		{"canceled", Receipt{Status: StatusCanceled, Executed: true}, StopCanceled},
		{"canceled before start", Receipt{Status: StatusCanceled}, StopCanceled},
		{"timed out", Receipt{Status: StatusTimedOut, Executed: true}, StopDeadline},
		{"invalid evidence", Receipt{Status: StatusError, Executed: true, Validation: invalidReport}, StopInvalidEvidence},
		{"truncated evidence", Receipt{Status: StatusError, Executed: true, Validation: invalidReport, Evidence: Evidence{StdoutTruncated: true}}, StopEvidenceTruncated},
		{"exit without failure", Receipt{Status: StatusError, Executed: true, Validated: true, ExitCode: 1}, StopExitWithoutFailure},
		{"provenance changed", Receipt{Status: StatusError, Executed: true, AgentProvenance: mismatched}, StopProvenanceChanged},
		{"provenance refused", Receipt{Status: StatusError, AgentProvenance: mismatched}, StopNotStarted},
		{"not started", Receipt{Status: StatusError, Error: "not started: --wait-for target not ready"}, StopNotStarted},
		{"killed", Receipt{Status: StatusError, Executed: true, ExitCode: -1}, StopInfrastructure},
	} {
		summary := summarizeFailure(test.receipt, nil)
		if summary == nil || summary.Stop != test.want {
			t.Errorf("%s: %+v, want %s", test.name, summary, test.want)
			continue
		}
		if err := summary.Validate(); err != nil {
			t.Errorf("%s: %v", test.name, err)
		}
	}
	if summary := summarizeFailure(Receipt{Status: StatusPassed, Validated: true}, nil); summary != nil {
		t.Fatalf("a passed receipt has no failure summary: %+v", summary)
	}
}

// A plain report's failures carry titles, messages and attachment paths; the
// summary keeps only their class and status, whatever they contain.
func TestFailureSummaryOfAReportKeepsNoFreeText(t *testing.T) {
	secret := "sk-live-0123456789abcdef"
	failures := []TestFailure{
		{Title: "checkout › pays with " + secret, Location: "tests/pay.spec.ts:12", Class: FailureAssertion, Status: "failed",
			Message:     "Error: expect(locator).toHaveText(expected)\nExpected: \"Order confirmed\"\nReceived: \"https://shop.invalid/pay?token=" + secret + "\"",
			Attachments: []TestAttachment{{Name: "error-context", Path: "/tmp/private/error-context.md", Bytes: 10}}},
		{Title: "a pinned failure", Class: FailureUnexpectedPass, Status: "passed", ExpectedStatus: "failed", Message: "Expected to fail, but passed."},
		{Title: "timed out", Class: FailureTimeout, Status: "timedOut", Message: "Test timeout of 30000ms exceeded."},
		{Title: "forged", Class: "Error: something " + secret, Status: "interrupted " + secret},
	}
	receipt := Receipt{Status: StatusFailed, Executed: true, Validated: true, FailureCount: len(failures), Failures: failures}
	summary := summarizeFailure(receipt, nil)
	want := []FailedTestSummary{
		{Class: FailureAssertion, Status: "failed"},
		{Class: FailureUnexpectedPass, Status: "passed"},
		{Class: FailureTimeout, Status: "timedOut"},
		{Class: FailureOther},
	}
	if summary == nil || summary.Stop != StopTestFailed || summary.FailedTests != 4 || summary.TestsTruncated || fmt.Sprint(summary.Tests) != fmt.Sprint(want) {
		t.Fatalf("summary %+v", summary)
	}
	raw, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{secret, "Order confirmed", "https://", "pay.spec.ts", "checkout", "error-context", "Expected", "/tmp"} {
		if strings.Contains(string(raw), leaked) {
			t.Fatalf("summary leaked %q: %s", leaked, raw)
		}
	}
}

func TestFailureSummaryIsBoundedAndCountsWhatItOmits(t *testing.T) {
	var tests []FailedTestSummary
	for i := range MaxFailureSummaryTests + 5 {
		tests = append(tests, FailedTestSummary{TestID: fmt.Sprintf("%064x", i), Class: FailureAssertion, Status: "failed", Step: "assertion", FailedSteps: 1, Attempts: 1})
	}
	receipt := Receipt{Status: StatusFailed, Executed: true, Validated: true, FailureCount: 40}
	summary := summarizeFailure(receipt, tests)
	if summary.FailedTests != 40 || len(summary.Tests) != MaxFailureSummaryTests || !summary.TestsTruncated || summary.Validate() != nil {
		t.Fatalf("bounds: %d listed of %d, truncated %v", len(summary.Tests), summary.FailedTests, summary.TestsTruncated)
	}
	// The summary owns its entries.
	tests[0].Class = FailureOther
	if summary.Tests[0].Class != FailureAssertion {
		t.Fatal("summary aliases the evidence's entries")
	}
	// Never more entries than failed tests, and none from evidence that did
	// not validate.
	if summary := summarizeFailure(Receipt{Status: StatusFailed, Executed: true, Validated: true, FailureCount: 1}, tests); len(summary.Tests) != 1 || summary.TestsTruncated {
		t.Fatalf("more entries than failures: %+v", summary)
	}
	if summary := summarizeFailure(Receipt{Status: StatusError, Executed: true, Validation: invalidReport, FailureCount: 3}, tests); summary.FailedTests != 0 || summary.Tests != nil || summary.TestsTruncated {
		t.Fatalf("unvalidated evidence described tests: %+v", summary)
	}
}

func TestFailureSummaryDropsMalformedEntriesButKeepsTheCount(t *testing.T) {
	tests := []FailedTestSummary{
		{TestID: digest, Class: FailureAssertion, Step: "assertion", Operation: "assert-text", Reason: "assertion_failed"},
		{TestID: digest, Class: FailureAssertion, Step: "assertion", Operation: "assert-text", Reason: "Close picker still displayed"},
	}
	summary := summarizeFailure(Receipt{Status: StatusFailed, Executed: true, Validated: true, FailureCount: 2}, tests)
	if summary.Stop != StopTestFailed || summary.FailedTests != 2 || summary.Tests != nil || !summary.TestsTruncated || summary.Validate() != nil {
		t.Fatalf("malformed entry persisted: %+v", summary)
	}
}

func TestFailureSummaryValidationIsClosed(t *testing.T) {
	valid := FailedTestSummary{TestID: digest, Class: FailureSetup, Status: "failed", Step: "assertion", Operation: "assert-text", Control: "receipt", Reason: "setup_blocked", Setup: SetupBlocked, FailedSteps: 1, Attempts: 1}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*FailedTestSummary){
		"raw test id":           func(t *FailedTestSummary) { t.TestID = "checkout › pays" },
		"short test id":         func(t *FailedTestSummary) { t.TestID = digest[:63] },
		"unknown class":         func(t *FailedTestSummary) { t.Class = "flaky" },
		"empty class":           func(t *FailedTestSummary) { t.Class = "" },
		"unknown status":        func(t *FailedTestSummary) { t.Status = "interrupted" },
		"passed not unexpected": func(t *FailedTestSummary) { t.Status = "passed" },
		"unknown step":          func(t *FailedTestSummary) { t.Step = "expect" },
		"operation on browser":  func(t *FailedTestSummary) { t.Step = "browser" },
		"operation with text":   func(t *FailedTestSummary) { t.Operation = "tap #send" },
		"reason with text":      func(t *FailedTestSummary) { t.Reason = "element 'Send' not visible" },
		"reason too long":       func(t *FailedTestSummary) { t.Reason = strings.Repeat("a", MaxFailureTokenBytes+1) },
		"reason alone":          func(t *FailedTestSummary) { t.Operation = "" },
		"control with text":     func(t *FailedTestSummary) { t.Control = "Pay 4242 now" },
		"control as an email":   func(t *FailedTestSummary) { t.Control = "ada@example.test" },
		"control too long":      func(t *FailedTestSummary) { t.Control = strings.Repeat("a", 65) },
		"control alone":         func(t *FailedTestSummary) { t.Operation, t.Reason = "", "" },
		"unknown setup":         func(t *FailedTestSummary) { t.Setup = "device-locked" },
		"negative steps":        func(t *FailedTestSummary) { t.FailedSteps = -1 },
		"huge attempts":         func(t *FailedTestSummary) { t.Attempts = MaxFailureSummaryCount + 1 },
	} {
		test := valid
		mutate(&test)
		if test.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	entry := FailedTestSummary{Class: FailureAssertion}
	for name, summary := range map[string]FailureSummary{
		"unknown stop":         {Stop: "assertion failed", FailedTests: 0},
		"negative count":       {Stop: StopTestFailed, FailedTests: -1, TestsTruncated: true},
		"more entries":         {Stop: StopTestFailed, FailedTests: 0, Tests: []FailedTestSummary{entry}},
		"unflagged truncation": {Stop: StopTestFailed, FailedTests: 2, Tests: []FailedTestSummary{entry}},
		"false truncation":     {Stop: StopTestFailed, FailedTests: 1, Tests: []FailedTestSummary{entry}, TestsTruncated: true},
		"too many entries":     {Stop: StopTestFailed, FailedTests: 30, Tests: make([]FailedTestSummary, MaxFailureSummaryTests+1), TestsTruncated: true},
		"malformed entry":      {Stop: StopTestFailed, FailedTests: 1, Tests: []FailedTestSummary{{Class: "?"}}},
	} {
		if summary.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestValidationRejectsMalformedFailedTests(t *testing.T) {
	base := Validation{ExecutedTests: 2, FailureCount: 1, AssertionCoverage: "unknown"}
	ok := base
	ok.FailedTests = []FailedTestSummary{{TestID: digest, Class: FailureAssertion, Status: "failed"}}
	if err := validateValidation(ok); err != nil {
		t.Fatal(err)
	}
	extra := ok
	extra.FailedTests = append(extra.FailedTests, extra.FailedTests[0])
	malformed := base
	malformed.FailedTests = []FailedTestSummary{{TestID: digest, Class: FailureAssertion, Reason: "free text"}}
	for name, validation := range map[string]Validation{"more than failed": extra, "malformed": malformed} {
		if validateValidation(validation) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
