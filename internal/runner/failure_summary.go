package runner

import (
	"errors"
	"regexp"
	"slices"
	"strings"
)

// invalidReport is the receipt validation text of an attempt whose
// structured evidence was missing or did not validate.
const invalidReport = "missing or invalid structured report"

var (
	// failureReason is a stop reason token, such as assertion_failed.
	failureReason = regexp.MustCompile(`^[a-z_]{1,64}$`)
	// failureOperation is a typed target operation name, such as tap-at.
	failureOperation = regexp.MustCompile(`^[a-z][a-z-]{0,63}$`)
	failureTestID    = regexp.MustCompile(`^[a-f0-9]{64}$`)

	failureStops = []string{StopTestFailed, StopGoalFailed, StopDeadline, StopCanceled, StopInvalidEvidence, StopEvidenceTruncated,
		StopExitWithoutFailure, StopProvenanceChanged, StopNotStarted, StopInfrastructure}
	failureClasses = []string{FailureAssertion, FailureTimeout, FailureActionTimeout, FailureEnvironment, FailureUnexpectedPass, FailureSetup, FailureOther}
	failureSteps   = []string{"assertion", "action", "browser", "fixture", "goal", "other"}
	// The final completed statuses of a failed test: an unexpected pass is
	// a test that was expected to fail.
	failureStatuses = []string{"failed", "timedOut", "passed"}
	failureSetups   = []string{SetupBlocked, SetupDirty, SetupMismatch, SetupObserver, SetupUnknown}
)

// EngineFailureClass classifies a failed test from structured engine facts
// alone: the status of its last completed attempt, the category and target
// stop reason of the step its failure is attributed to, and a setup
// classification that was not recovered. It never reads message prose.
func EngineFailureClass(status, step, reason, setup string) string {
	switch {
	case status == "passed":
		return FailureUnexpectedPass
	case setup != "" || strings.HasPrefix(reason, "setup_"):
		return FailureSetup
	case status == "timedOut":
		return FailureTimeout
	case reason == "assertion_failed":
		return FailureAssertion
	case reason == "deadline":
		// The operation's own bound, or the attempt's, ran out before it
		// established anything.
		return FailureActionTimeout
	case reason != "":
		// Another stop reason, such as operation_failed or invalid_point:
		// the operation could not run, not a completed comparison.
		return FailureOther
	case step == "assertion":
		return FailureAssertion
	}
	return FailureOther
}

// Validate checks a failure summary against its closed record: known tokens,
// bounded counts and lists, and test entries that carry no free text.
func (summary FailureSummary) Validate() error {
	if !slices.Contains(failureStops, summary.Stop) {
		return errors.New("failure summary has an unknown stop token")
	}
	if summary.FailedTests < 0 || summary.FailedTests > MaxFailureSummaryCount ||
		len(summary.Tests) > MaxFailureSummaryTests || len(summary.Tests) > summary.FailedTests ||
		summary.TestsTruncated != (summary.FailedTests > len(summary.Tests)) {
		return errors.New("failure summary has inconsistent counts")
	}
	for _, test := range summary.Tests {
		if err := test.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks one failed test entry against its closed record.
func (test FailedTestSummary) Validate() error {
	valid := (test.TestID == "" || failureTestID.MatchString(test.TestID)) &&
		slices.Contains(failureClasses, test.Class) &&
		(test.Status == "" || slices.Contains(failureStatuses, test.Status)) &&
		(test.Status != "passed" || test.Class == FailureUnexpectedPass) &&
		(test.Step == "" || slices.Contains(failureSteps, test.Step)) &&
		(test.Operation == "" || (failureOperation.MatchString(test.Operation) && (test.Step == "assertion" || test.Step == "action"))) &&
		(test.Reason == "" || (failureReason.MatchString(test.Reason) && test.Operation != "")) &&
		(test.Setup == "" || slices.Contains(failureSetups, test.Setup)) &&
		test.FailedSteps >= 0 && test.FailedSteps <= MaxFailureSummaryCount &&
		test.Attempts >= 0 && test.Attempts <= MaxFailureSummaryCount
	if !valid {
		return errors.New("failure summary has a malformed test entry")
	}
	return nil
}

// summarizeFailure builds the failure summary of a receipt that did not pass.
// Failed tests come only from validated evidence: the engine's own entries
// when the adapter produced them, otherwise the classes and statuses of the
// report's failures. An entry outside the closed record is dropped, never
// repaired, and the count keeps saying how many failed.
func summarizeFailure(receipt Receipt, evidence []FailedTestSummary) *FailureSummary {
	if receipt.Status == StatusPassed {
		return nil
	}
	summary := &FailureSummary{Stop: failureStop(receipt)}
	if receipt.Validated {
		summary.FailedTests = min(max(receipt.FailureCount, 0), MaxFailureSummaryCount)
		tests := evidence
		if tests == nil {
			tests = reportFailedTests(receipt.Failures)
		}
		tests = tests[:min(len(tests), summary.FailedTests, MaxFailureSummaryTests)]
		if len(tests) > 0 {
			summary.Tests = slices.Clone(tests)
		}
	}
	summary.TestsTruncated = summary.FailedTests > len(summary.Tests)
	if summary.Validate() != nil {
		summary.Tests, summary.TestsTruncated = nil, summary.FailedTests > 0
	}
	return summary
}

// failureStop names why a receipt that did not pass ended, from its own
// status and structured fields, never from error text.
func failureStop(receipt Receipt) string {
	switch receipt.Status {
	case StatusCanceled:
		return StopCanceled
	case StatusTimedOut:
		return StopDeadline
	case StatusFailed:
		// As in classifyRun: a goal that did not complete explains the
		// failures of its own test, not a failure in a sibling test.
		if receipt.GoalFailed && receipt.NonGoalFailureCount == 0 {
			return StopGoalFailed
		}
		return StopTestFailed
	}
	switch {
	case receipt.AgentProvenance != nil && receipt.AgentProvenance.Status != "matched" && receipt.Executed:
		return StopProvenanceChanged
	case !receipt.Executed:
		return StopNotStarted
	case receipt.Validation == invalidReport && receipt.Evidence.StdoutTruncated:
		return StopEvidenceTruncated
	case receipt.Validation == invalidReport:
		return StopInvalidEvidence
	case receipt.Validated && receipt.ExitCode != 0 && receipt.FailureCount == 0:
		return StopExitWithoutFailure
	}
	return StopInfrastructure
}

// reportFailedTests keeps only the class and final status of each report
// failure; titles, locations, messages and attachments stay in Failures.
func reportFailedTests(failures []TestFailure) []FailedTestSummary {
	var tests []FailedTestSummary
	for _, failure := range failures {
		test := FailedTestSummary{Class: FailureOther}
		if slices.Contains(failureClasses, failure.Class) {
			test.Class = failure.Class
		}
		if slices.Contains(failureStatuses, failure.Status) {
			test.Status = failure.Status
		}
		if test.Status == "passed" {
			test.Class = FailureUnexpectedPass
		}
		tests = append(tests, test)
	}
	return tests
}

// withStop replaces the stop token of a receipt whose status changed after
// it was summarized, keeping what the evidence said about its tests.
func withStop(summary *FailureSummary, stop string) *FailureSummary {
	if summary == nil {
		return &FailureSummary{Stop: stop}
	}
	changed := *summary
	changed.Stop = stop
	return &changed
}
