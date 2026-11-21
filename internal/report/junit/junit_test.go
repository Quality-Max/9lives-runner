package junit

import (
	"encoding/xml"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// parsed is the document as a JUnit consumer reads it, decoded strictly.
type parsed struct {
	Tests    int `xml:"tests,attr"`
	Failures int `xml:"failures,attr"`
	Errors   int `xml:"errors,attr"`
	Skipped  int `xml:"skipped,attr"`
	Suites   []struct {
		Name       string           `xml:"name,attr"`
		Tests      int              `xml:"tests,attr"`
		Failures   int              `xml:"failures,attr"`
		Errors     int              `xml:"errors,attr"`
		Skipped    int              `xml:"skipped,attr"`
		Properties []parsedProperty `xml:"properties>property"`
		Cases      []parsedCase     `xml:"testcase"`
	} `xml:"testsuite"`
}

type parsedProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type parsedCase struct {
	Name       string           `xml:"name,attr"`
	Time       *string          `xml:"time,attr"`
	Properties []parsedProperty `xml:"properties>property"`
	Failure    *parsedProblem   `xml:"failure"`
	Error      *parsedProblem   `xml:"error"`
	Skipped    *struct{}        `xml:"skipped"`
}

type parsedProblem struct {
	Type    string `xml:"type,attr"`
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

func (c parsedCase) passed() bool { return c.Failure == nil && c.Error == nil && c.Skipped == nil }

func (c parsedCase) property(name string) string {
	for _, property := range c.Properties {
		if property.Name == name {
			return property.Value
		}
	}
	return ""
}

func render(t *testing.T, summary runner.RunSummary) (parsed, string) {
	t.Helper()
	raw, err := Render(summary)
	if err != nil {
		t.Fatal(err)
	}
	var doc parsed
	decoder := xml.NewDecoder(strings.NewReader(string(raw)))
	decoder.Strict = true
	if err := decoder.Decode(&doc); err != nil {
		t.Fatalf("document does not parse: %v\n%s", err, raw)
	}
	// The totals a dashboard reads must be the testcases it would count.
	total, failures, errors, skipped := 0, 0, 0, 0
	for _, suite := range doc.Suites {
		own := [4]int{}
		for _, each := range suite.Cases {
			own[0]++
			switch {
			case each.Error != nil:
				own[2]++
			case each.Failure != nil:
				own[1]++
			case each.Skipped != nil:
				own[3]++
			}
		}
		if own != [4]int{suite.Tests, suite.Failures, suite.Errors, suite.Skipped} {
			t.Fatalf("suite %q attributes disagree with its testcases: %v\n%s", suite.Name, own, raw)
		}
		total, failures, errors, skipped = total+own[0], failures+own[1], errors+own[2], skipped+own[3]
	}
	if [4]int{total, failures, errors, skipped} != [4]int{doc.Tests, doc.Failures, doc.Errors, doc.Skipped} {
		t.Fatalf("document totals disagree with its testcases:\n%s", raw)
	}
	return doc, string(raw)
}

func digest(char string) string { return strings.Repeat(char, 64) }

// validated is a validated receipt whose counts agree with its test list.
func validated(tests ...runner.TestOutcome) runner.Receipt {
	receipt := runner.Receipt{Version: runner.ReceiptVersion, RunID: "run-1", JobID: "job-001", AttemptID: "job-001-attempt-001", Attempt: 1,
		Spec: "tests/checkout.spec.ts", Adapter: "playwright", Status: runner.StatusPassed, Executed: true, Validated: true,
		StartedAt: time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC), DurationMS: 4200, Tests: tests}
	for _, test := range tests {
		switch test.Status {
		case runner.TestSkipped:
			receipt.SkippedTests++
		case runner.TestFailed:
			receipt.ExecutedTests++
			receipt.FailureCount++
		case runner.TestFlaky:
			receipt.ExecutedTests++
			receipt.FlakyTests++
		default:
			receipt.ExecutedTests++
		}
	}
	if receipt.FailureCount > 0 {
		receipt.Status, receipt.ExitCode = runner.StatusFailed, 1
		receipt.FailureSummary = &runner.FailureSummary{Stop: runner.StopTestFailed, FailedTests: receipt.FailureCount, TestsTruncated: true}
	}
	return receipt
}

func run(outcome runner.RunOutcome, receipts ...runner.Receipt) runner.RunSummary {
	return runner.RunSummary{Version: runner.RunSummaryVersion, Outcome: outcome, RunID: "run-1", Complete: outcome == runner.OutcomePassed,
		PlannedJobs: len(receipts), DurationMS: 5000, Receipts: receipts}
}

func TestEachTestOutcomeMapsToItsOwnElement(t *testing.T) {
	receipt := validated(
		runner.TestOutcome{Title: "[chromium] cart › adds an item", Status: runner.TestPassed, DurationMS: 1500},
		runner.TestOutcome{Title: "[chromium] cart › retries", Status: runner.TestFlaky, DurationMS: 20},
		runner.TestOutcome{Title: "[chromium] cart › pending", Status: runner.TestSkipped},
		runner.TestOutcome{Title: "[chromium] cart › total", Status: runner.TestFailed, Class: runner.FailureAssertion, DurationMS: 310},
	)
	receipt.Failures = []runner.TestFailure{{Title: "[chromium] cart › total", Location: "tests/checkout.spec.ts:12", Message: "Error: expect(received).toBe(expected)\nExpected: 3", Class: runner.FailureAssertion, Status: "failed"}}
	doc, raw := render(t, run(runner.OutcomeFailed, receipt))
	if len(doc.Suites) != 1 || doc.Suites[0].Name != "tests/checkout.spec.ts" || doc.Tests != 4 || doc.Failures != 1 || doc.Errors != 0 || doc.Skipped != 1 {
		t.Fatalf("unexpected totals:\n%s", raw)
	}
	cases := doc.Suites[0].Cases
	if !cases[0].passed() || cases[0].Name != "[chromium] cart › adds an item" || cases[0].Time == nil || *cases[0].Time != "1.500" || cases[0].property("flaky") != "" {
		t.Fatalf("passed test: %+v", cases[0])
	}
	// A flaky test passed; it is marked, never reported as a failure.
	if !cases[1].passed() || cases[1].property("flaky") != "true" || strings.Contains(raw, "flakyFailure") {
		t.Fatalf("flaky test: %+v", cases[1])
	}
	if cases[2].Skipped == nil || cases[2].Failure != nil || cases[2].Error != nil || cases[2].Time != nil {
		t.Fatalf("skipped test: %+v", cases[2])
	}
	failure := cases[3].Failure
	if failure == nil || cases[3].Error != nil || failure.Type != runner.FailureAssertion || failure.Message != "Error: expect(received).toBe(expected)" ||
		!strings.Contains(failure.Body, "tests/checkout.spec.ts:12") || !strings.Contains(failure.Body, "Expected: 3") {
		t.Fatalf("failed test: %+v", cases[3])
	}
}

func TestFailureClassesSeparateDefectsFromUnprovenResults(t *testing.T) {
	for class, element := range map[string]string{
		runner.FailureAssertion: "failure", runner.FailureTimeout: "failure", runner.FailureActionTimeout: "failure",
		runner.FailureUnexpectedPass: "failure", runner.FailureOther: "failure",
		runner.FailureSetup: "error", runner.FailureEnvironment: "error",
	} {
		doc, raw := render(t, run(runner.OutcomeFailed, validated(runner.TestOutcome{TestID: digest("a"), Status: runner.TestFailed, Class: class})))
		only := doc.Suites[0].Cases[0]
		got := only.Failure
		if element == "error" {
			got = only.Error
		}
		if len(doc.Suites[0].Cases) != 1 || got == nil || got.Type != class || (only.Failure != nil) == (only.Error != nil) {
			t.Fatalf("class %s should be one <%s type=%q>:\n%s", class, element, class, raw)
		}
	}
}

func TestEngineRunNamesTestsByDigestAndInventsNoDuration(t *testing.T) {
	receipt := validated(
		runner.TestOutcome{TestID: digest("a"), Status: runner.TestPassed},
		runner.TestOutcome{TestID: digest("b"), Status: runner.TestFailed, Class: runner.FailureAssertion},
	)
	receipt.Adapter, receipt.Target = "playwright-sdk", "android:finance"
	receipt.FailureSummary = &runner.FailureSummary{Stop: runner.StopTestFailed, FailedTests: 1, Tests: []runner.FailedTestSummary{{
		TestID: digest("b"), Class: runner.FailureAssertion, Status: "failed", Step: "assertion", Operation: "assert-text", Control: "balance", Reason: "assertion_failed"}}}
	doc, raw := render(t, run(runner.OutcomeFailed, receipt))
	cases := doc.Suites[0].Cases
	if doc.Suites[0].Name != "tests/checkout.spec.ts [android:finance]" || cases[0].Name != "test aaaaaaaaaaaa" || cases[0].property("testId") != digest("a") {
		t.Fatalf("digest naming:\n%s", raw)
	}
	for _, each := range cases {
		if each.Time != nil {
			t.Fatalf("a duration the engine never measured was reported:\n%s", raw)
		}
	}
	// The failure summary's closed tokens say what failed, without prose.
	if failure := cases[1].Failure; failure == nil || failure.Message != "assertion: assert-text on balance (assertion_failed)" {
		t.Fatalf("failure summary detail lost:\n%s", raw)
	}
}

func TestTruncatedListStillAccountsForEveryFailure(t *testing.T) {
	receipt := validated(
		runner.TestOutcome{Title: "one", Status: runner.TestFailed, Class: runner.FailureAssertion},
		runner.TestOutcome{Title: "two", Status: runner.TestPassed},
	)
	// 3 failed, 4 passed and 2 skipped tests; the list holds one of each of the first two.
	receipt.FailureCount, receipt.ExecutedTests, receipt.SkippedTests, receipt.TestsTruncated = 3, 7, 2, true
	doc, raw := render(t, run(runner.OutcomeFailed, receipt))
	var extraFailed, extraPassed, extraSkipped *parsedCase
	for index, each := range doc.Suites[0].Cases {
		switch {
		case strings.HasPrefix(each.Name, "2 more failed test(s)"):
			extraFailed = &doc.Suites[0].Cases[index]
		case strings.HasPrefix(each.Name, "3 passed"):
			extraPassed = &doc.Suites[0].Cases[index]
		case strings.HasPrefix(each.Name, "2 skipped"):
			extraSkipped = &doc.Suites[0].Cases[index]
		}
	}
	if extraFailed == nil || extraFailed.Failure == nil || extraFailed.property("tests") != "2" || doc.Failures != 2 {
		t.Fatalf("unlisted failures are not reported as a failure:\n%s", raw)
	}
	if extraPassed == nil || !extraPassed.passed() || extraSkipped == nil || extraSkipped.Skipped == nil {
		t.Fatalf("unlisted passed and skipped tests are not accounted for:\n%s", raw)
	}
	if !strings.Contains(raw, `name="testsTruncated" value="true"`) || !strings.Contains(raw, `name="failedTests" value="3"`) {
		t.Fatalf("suite does not state the real counts:\n%s", raw)
	}
}

func TestUnprovenAttemptsReportNoPassingTestcase(t *testing.T) {
	passedList := []runner.TestOutcome{{Title: "claims to pass", Status: runner.TestPassed}}
	for name, change := range map[string]func(*runner.Receipt){
		"not validated": func(r *runner.Receipt) {
			r.Validated, r.Status, r.Error = false, runner.StatusError, "missing or invalid structured report"
			r.FailureSummary = &runner.FailureSummary{Stop: runner.StopInvalidEvidence}
		},
		"canceled": func(r *runner.Receipt) {
			r.Validated, r.Status = false, runner.StatusCanceled
			r.FailureSummary = &runner.FailureSummary{Stop: runner.StopCanceled}
		},
		"timed out": func(r *runner.Receipt) {
			r.Validated, r.Status = false, runner.StatusTimedOut
			r.FailureSummary = &runner.FailureSummary{Stop: runner.StopDeadline}
		},
		"validated but exited nonzero": func(r *runner.Receipt) {
			r.Status, r.ExitCode = runner.StatusError, 1
			r.FailureSummary = &runner.FailureSummary{Stop: runner.StopExitWithoutFailure}
		},
		"provenance changed after validation": func(r *runner.Receipt) {
			r.Validated, r.Status = false, runner.StatusError
			r.FailureSummary = &runner.FailureSummary{Stop: runner.StopProvenanceChanged}
		},
		"never started": func(r *runner.Receipt) {
			r.Validated, r.Executed, r.Status, r.ExecutedTests, r.Tests = false, false, runner.StatusError, 0, nil
			r.FailureSummary = &runner.FailureSummary{Stop: runner.StopNotStarted}
		},
		"passed status without validation": func(r *runner.Receipt) { r.Validated = false },
		"passed status with a failure":     func(r *runner.Receipt) { r.FailureCount = 1 },
	} {
		receipt := validated(passedList...)
		change(&receipt)
		doc, raw := render(t, run(runner.OutcomeIncomplete, receipt))
		cases := doc.Suites[0].Cases
		if len(cases) != 1 || cases[0].Error == nil || doc.Errors != 1 || doc.Tests != 1 {
			t.Fatalf("%s: want exactly one <error> and no other testcase:\n%s", name, raw)
		}
		if receipt.FailureSummary != nil && cases[0].Error.Type != receipt.FailureSummary.Stop {
			t.Fatalf("%s: error does not carry the stop reason %s:\n%s", name, receipt.FailureSummary.Stop, raw)
		}
		if strings.Contains(raw, "claims to pass") {
			t.Fatalf("%s: a test of an unproven attempt was reported:\n%s", name, raw)
		}
	}
}

func TestSkippedInputsAreErrorsNotSkippedTests(t *testing.T) {
	summary := run(runner.OutcomeIncomplete, validated(runner.TestOutcome{Title: "ok", Status: runner.TestPassed}))
	summary.SkippedInputs, summary.Skipped = 1, []runner.Skipped{{Input: "tests/missing.spec.ts", Reason: "no adapter supports this input"}}
	doc, raw := render(t, summary)
	if doc.Skipped != 0 || doc.Errors != 1 || len(doc.Suites) != 2 {
		t.Fatalf("a skipped input must be an error:\n%s", raw)
	}
	dropped := doc.Suites[1].Cases[0]
	if dropped.Name != "tests/missing.spec.ts" || dropped.Error == nil || dropped.Error.Type != "skipped_input" || !strings.Contains(dropped.Error.Message, "no adapter supports this input") {
		t.Fatalf("skipped input: %+v", dropped)
	}
}

// Whatever made a run incomplete, the document must not read as green: a
// consumer that counts only failures and errors has to find an error.
func TestIncompleteRunAlwaysContainsAnError(t *testing.T) {
	passing := validated(runner.TestOutcome{Title: "ok", Status: runner.TestPassed})
	failing := validated(runner.TestOutcome{Title: "broken", Status: runner.TestFailed, Class: runner.FailureAssertion})
	failing.JobID = "job-002"
	canceled := validated()
	canceled.JobID = "job-003"
	canceled.Validated, canceled.Executed, canceled.Status = false, false, runner.StatusCanceled
	goalWithPasses := validated(runner.TestOutcome{TestID: digest("a"), Status: runner.TestPassed})
	goalWithPasses.Status, goalWithPasses.GoalFailed, goalWithPasses.Error = runner.StatusFailed, true, "goal did not complete; see bounded goal receipt"
	goalWithPasses.FailureSummary = &runner.FailureSummary{Stop: runner.StopGoalFailed}
	goalFailedTest := validated(runner.TestOutcome{TestID: digest("a"), Status: runner.TestFailed, Class: runner.FailureOther})
	goalFailedTest.GoalFailed = true
	goalFailedTest.FailureSummary = &runner.FailureSummary{Stop: runner.StopGoalFailed, FailedTests: 1, TestsTruncated: true}
	legacy := validated(runner.TestOutcome{Title: "ok", Status: runner.TestPassed})
	legacy.Tests = nil

	cases := map[string]runner.RunSummary{
		"interrupted after every job passed":  run(runner.OutcomeIncomplete, passing),
		"interrupted beside a failure":        run(runner.OutcomeIncomplete, passing, failing),
		"no planned jobs":                     run(runner.OutcomeIncomplete),
		"a planned job has no receipt":        {Outcome: runner.OutcomeIncomplete, PlannedJobs: 2, Receipts: []runner.Receipt{passing}},
		"canceled before starting":            run(runner.OutcomeIncomplete, passing, canceled),
		"goal did not complete, tests passed": run(runner.OutcomeIncomplete, goalWithPasses),
		"goal did not complete, test failed":  run(runner.OutcomeIncomplete, goalFailedTest),
		"skipped input count without a list":  {Outcome: runner.OutcomeIncomplete, PlannedJobs: 1, SkippedInputs: 2, Receipts: []runner.Receipt{passing}},
		"result without per-test lists":       run(runner.OutcomeIncomplete, legacy),
		"unknown outcome":                     run("unheard-of", passing),
		"empty outcome":                       run("", passing),
		"passed outcome that is not complete": {Outcome: runner.OutcomePassed, Complete: false, PlannedJobs: 1, Receipts: []runner.Receipt{passing}},
	}
	for name, summary := range cases {
		doc, raw := render(t, summary)
		if !Incomplete(summary) || doc.Errors == 0 {
			t.Errorf("%s: an incomplete run rendered without an <error>:\n%s", name, raw)
		}
	}
	// A goal's failed test proves no defect: it is an error, not a failure.
	doc, raw := render(t, cases["goal did not complete, test failed"])
	if doc.Failures != 0 || doc.Suites[0].Cases[0].Error == nil || doc.Suites[0].Cases[0].Error.Type != runner.StopGoalFailed {
		t.Fatalf("goal failure reported as a defect:\n%s", raw)
	}
	// And the complete outcomes add nothing: a pass has no error or failure.
	doc, raw = render(t, run(runner.OutcomePassed, passing))
	if doc.Errors != 0 || doc.Failures != 0 || doc.Tests != 1 {
		t.Fatalf("a passed run gained a problem:\n%s", raw)
	}
	if doc, raw = render(t, run(runner.OutcomeFailed, passing, failing)); doc.Errors != 0 || doc.Failures != 1 {
		t.Fatalf("a failed run must show its failure and no error:\n%s", raw)
	}
}

func TestHostileTextCannotBreakOrLeakIntoTheDocument(t *testing.T) {
	hostile := "\x1b[31mred\x1b[0m \x1b]0;window title\x07 </testcase><testcase name=\"forged\"> ]]> & \" ' \x00\x01\x0b\ufffe bad\xff\xfeutf8 password=hunter2secret https://user:pa55word@example.test/x"
	receipt := validated(
		runner.TestOutcome{Title: runner.TestTitle(hostile), Status: runner.TestFailed, Class: runner.FailureAssertion},
		runner.TestOutcome{Title: "plain", Status: runner.TestPassed},
	)
	receipt.Spec = "tests/" + hostile + ".spec.ts"
	receipt.Failures = []runner.TestFailure{{Title: hostile, Message: hostile + "\nAuthorization: Bearer abcdefgh12345678", Location: hostile, Class: runner.FailureAssertion, Status: "failed"}}
	broken := validated()
	broken.JobID, broken.Validated, broken.Status, broken.Error = "job-002", false, runner.StatusError, hostile
	summary := run(runner.OutcomeIncomplete, receipt, broken)
	summary.RunID = "run-" + hostile
	summary.SkippedInputs, summary.Skipped = 1, []runner.Skipped{{Input: hostile, Reason: hostile}}
	doc, raw := render(t, summary)
	for _, leaked := range []string{"hunter2secret", "pa55word", "abcdefgh12345678", "\x1b", "\x00", "\x01", "\x0b", "\x07", "window title"} {
		if strings.Contains(raw, leaked) {
			t.Fatalf("document contains %q:\n%q", leaked, raw)
		}
	}
	// The forged element stayed text: nothing but the real testcases exist.
	if len(doc.Suites) != 3 || len(doc.Suites[0].Cases) != 2 || doc.Tests != 4 {
		t.Fatalf("hostile text changed the document structure:\n%s", raw)
	}
	failed := doc.Suites[0].Cases[0]
	if failed.Failure == nil || !strings.Contains(failed.Name, `</testcase><testcase name="forged">`) || !strings.Contains(failed.Failure.Body, "]]>") {
		t.Fatalf("hostile title was not preserved as text: %+v", failed)
	}
}

func TestLongTextIsBounded(t *testing.T) {
	long := strings.Repeat("é", 5000)
	broken := validated()
	broken.Validated, broken.Status, broken.Error, broken.Spec = false, runner.StatusError, long, long
	_, raw := render(t, run(runner.OutcomeIncomplete, broken))
	if len(raw) > 8<<10 {
		t.Fatalf("unbounded text reached the document: %d bytes", len(raw))
	}
}

func TestOnlyTheFinalAttemptOfAJobIsReported(t *testing.T) {
	first := validated()
	first.Validated, first.Status, first.Error = false, runner.StatusError, "first attempt lost its report"
	second := validated(runner.TestOutcome{Title: "ok", Status: runner.TestPassed})
	second.Attempt, second.AttemptID = 2, "job-001-attempt-002"
	for _, receipts := range [][]runner.Receipt{{first, second}, {second, first}} {
		doc, raw := render(t, run(runner.OutcomePassed, receipts...))
		if len(doc.Suites) != 1 || doc.Tests != 1 || doc.Errors != 0 || !strings.Contains(raw, `name="attempt" value="2"`) {
			t.Fatalf("an earlier attempt was reported:\n%s", raw)
		}
	}
}

// A saved result is untrusted: a list that disagrees with the receipt's own
// counts is not used, so it cannot turn a failure into passing testcases.
func TestAListThatDisagreesWithTheCountsIsNotTrusted(t *testing.T) {
	receipt := validated(runner.TestOutcome{Title: "broken", Status: runner.TestFailed, Class: runner.FailureAssertion})
	receipt.Tests = []runner.TestOutcome{{Title: "edited to pass", Status: runner.TestPassed}}
	doc, raw := render(t, run(runner.OutcomeFailed, receipt))
	if doc.Failures != 1 || strings.Contains(raw, "edited to pass") || !strings.Contains(raw, `name="testsListed" value="false"`) {
		t.Fatalf("a malformed list was rendered:\n%s", raw)
	}
}

func TestResultWithoutListsReportsDescribedFailuresAndCounts(t *testing.T) {
	receipt := validated()
	receipt.Status, receipt.ExecutedTests, receipt.FailureCount, receipt.SkippedTests = runner.StatusFailed, 30, 25, 1
	for index := range runner.MaxReportedFailures {
		receipt.Failures = append(receipt.Failures, runner.TestFailure{Title: fmt.Sprintf("failure %d", index), Class: runner.FailureTimeout, Status: "timedOut"})
	}
	doc, raw := render(t, run(runner.OutcomeFailed, receipt))
	// 20 described failures, one for the 5 undescribed, one for 5 passed, one for the skip.
	if doc.Tests != 23 || doc.Failures != 21 || doc.Skipped != 1 || doc.Errors != 0 || !strings.Contains(raw, "5 more failed test(s), not listed individually") {
		t.Fatalf("legacy receipt:\n%s", raw)
	}
}

func check(id string, result runner.VerificationResult, reason string, status int) runner.Verification {
	return runner.Verification{ID: id, OutcomeID: "order-stored", Method: "GET", Result: result, Reason: reason, HTTPStatus: status, ObservedBy: runner.VerificationObserver}
}

func caseNamed(t *testing.T, doc parsed, name string) parsedCase {
	t.Helper()
	for _, suite := range doc.Suites {
		for _, each := range suite.Cases {
			if each.Name == name {
				return each
			}
		}
	}
	t.Fatalf("no testcase %q", name)
	return parsedCase{}
}

func TestIndependentChecksAreTheirOwnTestcases(t *testing.T) {
	passedTest := runner.TestOutcome{Title: "adds an item", Status: runner.TestPassed}

	verified := validated(passedTest)
	verified.Verifications = []runner.Verification{check("order-paid", runner.VerificationVerified, "", 200)}
	doc, raw := render(t, run(runner.OutcomePassed, verified))
	if doc.Tests != 2 || doc.Failures != 0 || doc.Errors != 0 {
		t.Fatalf("a verified check is a passing testcase:\n%s", raw)
	}
	if each := caseNamed(t, doc, "verify order-paid"); !each.passed() || each.property("verification") != "verified" || each.property("observedBy") != "engine" || each.property("outcomeId") != "order-stored" || each.property("httpStatus") != "200" {
		t.Fatalf("verified case: %+v", each)
	}

	// The worker reported its test passed; the engine's check is what fails.
	contradicted := validated(passedTest)
	contradicted.Status, contradicted.Error = runner.StatusFailed, "independent verification contradicted the result: order-paid"
	contradicted.FailureSummary = &runner.FailureSummary{Stop: runner.StopVerificationContradicted}
	contradicted.Verifications = []runner.Verification{check("order-paid", runner.VerificationContradicted, runner.VerificationReasonJSONValue, 200), check("stock-kept", runner.VerificationVerified, "", 200)}
	doc, raw = render(t, run(runner.OutcomeFailed, contradicted))
	if doc.Tests != 3 || doc.Failures != 1 || doc.Errors != 0 {
		t.Fatalf("a contradicted check is exactly one failure:\n%s", raw)
	}
	each := caseNamed(t, doc, "verify order-paid")
	if each.Failure == nil || each.Failure.Type != "verification_contradicted" || !strings.Contains(each.Failure.Message, "order-paid") || !strings.Contains(each.Failure.Message, "json_value") || !strings.Contains(each.Failure.Message, "HTTP 200") {
		t.Fatalf("contradicted case: %+v", each)
	}
	if !caseNamed(t, doc, "adds an item").passed() || !caseNamed(t, doc, "verify stock-kept").passed() {
		t.Fatalf("the test and the verified check keep their own outcome:\n%s", raw)
	}

	// The engine got no answer: the attempt is an error, and so is the check.
	unreachable := validated(passedTest)
	unreachable.Status, unreachable.Error = runner.StatusError, "independent verification could not be completed: order-paid"
	unreachable.FailureSummary = &runner.FailureSummary{Stop: runner.StopVerificationIncomplete}
	unreachable.Verifications = []runner.Verification{check("order-paid", runner.VerificationUnreachable, runner.VerificationReasonTimeout, 0), check("stock-kept", runner.VerificationNotRun, runner.VerificationReasonBudget, 0)}
	doc, raw = render(t, run(runner.OutcomeIncomplete, unreachable))
	if doc.Failures != 0 || doc.Errors != 3 || doc.Tests != 3 {
		t.Fatalf("an unreachable check proves nothing and shows no passing testcase:\n%s", raw)
	}
	if each := caseNamed(t, doc, "verify order-paid"); each.Error == nil || each.Error.Type != "verification_unreachable" || !strings.Contains(each.Error.Message, "timeout") {
		t.Fatalf("unreachable case: %+v", each)
	}
	if each := caseNamed(t, doc, "verify stock-kept"); each.Error == nil || each.Error.Type != "verification_not_run" {
		t.Fatalf("not-run case: %+v", each)
	}
	if !strings.Contains(raw, `value="verification_incomplete"`) {
		t.Fatalf("the suite names the stop reason:\n%s", raw)
	}
}

// A stored result is untrusted: records that are not the closed form are one
// error, and a receipt edited to "passed" beside an unverified check is not
// rendered as a clean pass.
func TestVerificationRecordsAreNotTrustedFromAStoredResult(t *testing.T) {
	passedTest := runner.TestOutcome{Title: "adds an item", Status: runner.TestPassed}
	malformed := validated(passedTest)
	malformed.Verifications = []runner.Verification{{ID: "x\"/><failure", Method: "GET", Result: runner.VerificationVerified, HTTPStatus: 200, ObservedBy: "worker"}}
	doc, raw := render(t, run(runner.OutcomeIncomplete, malformed))
	if doc.Errors != 1 || strings.Contains(raw, "failure") && !strings.Contains(raw, `failures="0"`) {
		t.Fatalf("malformed records:\n%s", raw)
	}
	if each := caseNamed(t, doc, "verify"); each.Error == nil || each.Error.Type != "verification_malformed" {
		t.Fatalf("malformed case: %+v", each)
	}

	edited := validated(passedTest)
	edited.Verifications = []runner.Verification{check("order-paid", runner.VerificationContradicted, runner.VerificationReasonStatus, 404)}
	doc, raw = render(t, run(runner.OutcomePassed, edited))
	if doc.Failures != 1 {
		t.Fatalf("a contradicted check under an edited status must still fail:\n%s", raw)
	}
}
