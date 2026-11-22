package runner

import (
	"errors"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

var testStatuses = []string{TestPassed, TestFailed, TestSkipped, TestFlaky}

// TestTitle prepares a test's title for a receipt: without terminal escapes
// or control characters, on one line, redacted and at most MaxTestTitleBytes,
// cut on a rune boundary. A spec controls its own titles, so they are
// evidence to bound, never text to trust.
func TestTitle(title string) string {
	title = strings.ToValidUTF8(StripTerminalEscapes(title), "")
	title = strings.Map(func(char rune) rune {
		if unicode.IsControl(char) {
			return ' '
		}
		return char
	}, title)
	title = RedactText(strings.Join(strings.Fields(title), " "))
	if len(title) <= MaxTestTitleBytes {
		return title
	}
	const ellipsis = "…"
	cut := MaxTestTitleBytes - len(ellipsis)
	for cut > 0 && !utf8.RuneStart(title[cut]) {
		cut--
	}
	return title[:cut] + ellipsis
}

// Validate checks one test outcome against its closed record: known tokens,
// a class on failed tests only, a bounded duration and a one-line title.
func (test TestOutcome) Validate() error {
	valid := (test.TestID == "" || failureTestID.MatchString(test.TestID)) &&
		slices.Contains(testStatuses, test.Status) &&
		((test.Status == TestFailed && slices.Contains(failureClasses, test.Class)) || (test.Status != TestFailed && test.Class == "")) &&
		test.DurationMS >= 0 && test.DurationMS <= MaxTestDurationMS &&
		len(test.Title) <= MaxTestTitleBytes && utf8.ValidString(test.Title) && !strings.ContainsFunc(test.Title, unicode.IsControl)
	if !valid {
		return errors.New("test list has a malformed entry")
	}
	return nil
}

// boundTestOutcomes keeps at most MaxReportedTests outcomes and says whether
// any were left out. Failed tests are kept first, in order, so a long list
// never drops the tests a reader most needs.
func boundTestOutcomes(tests []TestOutcome) ([]TestOutcome, bool) {
	if len(tests) <= MaxReportedTests {
		return slices.Clone(tests), false
	}
	kept := make([]TestOutcome, 0, MaxReportedTests)
	for _, failed := range []bool{true, false} {
		for _, test := range tests {
			if (test.Status == TestFailed) == failed && len(kept) < MaxReportedTests {
				kept = append(kept, test)
			}
		}
	}
	return kept, true
}

// validateTestList checks a test list against the counts it must agree with.
// A complete list accounts for every executed and skipped test; a truncated
// one may only list fewer of each outcome than was counted.
func validateTestList(tests []TestOutcome, truncated bool, executed, skipped, failed, flaky int) error {
	if executed < 0 || skipped < 0 || len(tests) > MaxReportedTests || len(tests) > executed+skipped || truncated != (len(tests) < executed+skipped) {
		return errors.New("test list disagrees with the test counts")
	}
	counts := map[string]int{}
	for _, test := range tests {
		if err := test.Validate(); err != nil {
			return err
		}
		counts[test.Status]++
	}
	listed := counts[TestPassed] + counts[TestFlaky] + counts[TestFailed]
	within := counts[TestFailed] <= failed && counts[TestFlaky] <= flaky && counts[TestSkipped] <= skipped && listed <= executed
	exact := counts[TestFailed] == failed && counts[TestFlaky] == flaky && counts[TestSkipped] == skipped && listed == executed
	if !within || (!truncated && !exact) {
		return errors.New("test list disagrees with the test counts")
	}
	return nil
}

// ValidTests reports whether the receipt's test list is the closed record
// the runner writes for a validated attempt. A receipt without a list, such
// as one written before lists existed, is valid.
func (receipt Receipt) ValidTests() bool {
	if len(receipt.Tests) == 0 {
		return !receipt.TestsTruncated
	}
	return receipt.Validated && validateTestList(receipt.Tests, receipt.TestsTruncated, receipt.ExecutedTests, receipt.SkippedTests, receipt.FailureCount, receipt.FlakyTests) == nil
}

// dropMalformedTestLists removes a persisted test list that is not the
// closed record the runner writes, as dropMalformedFailureSummaries does for
// summaries: withheld, never repaired, so no report is built from it.
func dropMalformedTestLists(result *RunSummary) {
	for index := range result.Receipts {
		if !result.Receipts[index].ValidTests() {
			result.Receipts[index].Tests, result.Receipts[index].TestsTruncated = nil, false
		}
	}
}
