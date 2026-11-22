// Package junit renders a run result as JUnit XML for CI dashboards.
//
// The document is a pure function of the run result and keeps three things
// apart: a test that failed (<failure>), a test that was skipped (<skipped>)
// and anything that left the result unproven (<error>). An attempt that did
// not validate, was canceled, timed out or errored contributes no passing
// testcase, and a run whose outcome is incomplete always contains at least
// one <error>, so no consumer can read it as green.
package junit

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// Bounds of the text copied into a document.
const (
	maxNameBytes    = 512
	maxMessageBytes = 512
	maxBodyBytes    = 2 << 10
)

type document struct {
	XMLName  xml.Name `xml:"testsuites"`
	Name     string   `xml:"name,attr"`
	Tests    int      `xml:"tests,attr"`
	Failures int      `xml:"failures,attr"`
	Errors   int      `xml:"errors,attr"`
	Skipped  int      `xml:"skipped,attr"`
	Time     string   `xml:"time,attr"`
	Suites   []suite  `xml:"testsuite"`
}

type suite struct {
	Name       string      `xml:"name,attr"`
	Tests      int         `xml:"tests,attr"`
	Failures   int         `xml:"failures,attr"`
	Errors     int         `xml:"errors,attr"`
	Skipped    int         `xml:"skipped,attr"`
	Time       string      `xml:"time,attr"`
	Timestamp  string      `xml:"timestamp,attr,omitempty"`
	Properties *properties `xml:"properties"`
	Cases      []testcase  `xml:"testcase"`
}

type properties struct {
	Items []property `xml:"property"`
}

type property struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type testcase struct {
	Name       string      `xml:"name,attr"`
	Classname  string      `xml:"classname,attr"`
	Time       string      `xml:"time,attr,omitempty"`
	Properties *properties `xml:"properties"`
	Failure    *problem    `xml:"failure"`
	Error      *problem    `xml:"error"`
	Skipped    *skipped    `xml:"skipped"`
}

type problem struct {
	Type    string `xml:"type,attr"`
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

type skipped struct {
	Message string `xml:"message,attr,omitempty"`
}

// Incomplete reports whether a run result proves neither a pass nor a
// failure, as the run's exit code does.
func Incomplete(summary runner.RunSummary) bool {
	return summary.Outcome != runner.OutcomeFailed && !(summary.Outcome == runner.OutcomePassed && summary.Complete)
}

// Render builds the JUnit XML document of a run result: one testsuite per
// job, from the final attempt's receipt, one for the inputs the plan
// skipped, and one for a run that is incomplete for a reason no receipt
// shows.
func Render(summary runner.RunSummary) ([]byte, error) {
	doc := document{Name: "9l " + oneLine(summary.RunID, maxNameBytes), Time: seconds(summary.DurationMS)}
	for _, receipt := range finalAttempts(summary.Receipts) {
		doc.Suites = append(doc.Suites, receiptSuite(receipt))
	}
	if len(summary.Skipped) > 0 || summary.SkippedInputs > 0 {
		doc.Suites = append(doc.Suites, skippedInputs(summary))
	}
	for index := range doc.Suites {
		count(&doc.Suites[index])
		doc.Errors += doc.Suites[index].Errors
	}
	if Incomplete(summary) && doc.Errors == 0 {
		// Interrupted after its last job, missing a planned job's receipt,
		// or classified incomplete for a reason this renderer does not
		// know: the document still must not read as a pass.
		unproven := suite{Name: "9l run", Time: seconds(0), Cases: []testcase{{
			Name: "run outcome", Classname: "9l run",
			Error: &problem{Type: "incomplete", Message: "the run is incomplete: it proves neither a pass nor a failure",
				Body: fmt.Sprintf("outcome %s; %d planned job(s), %d receipt(s), %d skipped input(s)", oneLine(string(summary.Outcome), 64), summary.PlannedJobs, len(summary.Receipts), summary.SkippedInputs)},
		}}}
		count(&unproven)
		doc.Suites = append(doc.Suites, unproven)
	}
	doc.Errors = 0
	for _, each := range doc.Suites {
		doc.Tests, doc.Failures, doc.Errors, doc.Skipped = doc.Tests+each.Tests, doc.Failures+each.Failures, doc.Errors+each.Errors, doc.Skipped+each.Skipped
	}
	var out bytes.Buffer
	out.WriteString(xml.Header)
	encoder := xml.NewEncoder(&out)
	encoder.Indent("", "  ")
	if err := encoder.Encode(doc); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// finalAttempts keeps one receipt per job, the one with the highest attempt
// number, in the order the jobs first appear. A run result already holds
// only final attempts; a hand-assembled one may not.
func finalAttempts(receipts []runner.Receipt) []runner.Receipt {
	final := make([]runner.Receipt, 0, len(receipts))
	position := map[string]int{}
	for _, receipt := range receipts {
		index, seen := position[receipt.JobID]
		switch {
		case !seen || receipt.JobID == "":
			position[receipt.JobID] = len(final)
			final = append(final, receipt)
		case receipt.Attempt > final[index].Attempt:
			final[index] = receipt
		}
	}
	return final
}

func count(target *suite) {
	target.Tests, target.Failures, target.Errors, target.Skipped = len(target.Cases), 0, 0, 0
	for _, each := range target.Cases {
		switch {
		case each.Error != nil:
			target.Errors++
		case each.Failure != nil:
			target.Failures++
		case each.Skipped != nil:
			target.Skipped++
		}
	}
}

// reportable says whether a receipt's tests have outcomes to report: only a
// validated attempt that ended passed or failed. Everything else is one
// <error>, whatever counts the receipt carries.
func reportable(receipt runner.Receipt) bool {
	switch receipt.Status {
	case runner.StatusPassed:
		return receipt.Validated && receipt.Executed && receipt.FailureCount == 0 && receipt.ExecutedTests > 0
	case runner.StatusFailed:
		return receipt.Validated && receipt.ExecutedTests > 0
	}
	return false
}

func receiptSuite(receipt runner.Receipt) suite {
	spec := oneLine(receipt.Spec, maxNameBytes)
	if spec == "" {
		spec = oneLine(receipt.JobID, maxNameBytes)
	}
	name := spec
	if receipt.Target != "" {
		name += " [" + oneLine(receipt.Target, 128) + "]"
	}
	stop := stopReason(receipt)
	result := suite{Name: name, Time: seconds(receipt.DurationMS)}
	if !receipt.StartedAt.IsZero() {
		result.Timestamp = receipt.StartedAt.UTC().Format(time.RFC3339)
	}
	items := []property{
		{"runId", receipt.RunID}, {"jobId", receipt.JobID}, {"attemptId", receipt.AttemptID}, {"attempt", strconv.Itoa(receipt.Attempt)},
		{"adapter", receipt.Adapter}, {"status", string(receipt.Status)}, {"validated", strconv.FormatBool(receipt.Validated)},
	}
	if receipt.Target != "" {
		items = append(items, property{"target", receipt.Target})
	}
	if stop != "" {
		items = append(items, property{"stop", stop})
	}
	if !reportable(receipt) {
		result.Properties = cleanProperties(items)
		body := text(receipt.Error, maxBodyBytes)
		result.Cases = []testcase{{
			Name: spec, Classname: spec,
			Error: &problem{Type: stop, Message: "no test outcome is reported: the attempt ended " + oneLine(string(receipt.Status), 32) + " (" + stop + ")", Body: body},
		}}
		return result
	}
	// The receipt's counts are the authority; the list may hold fewer.
	failed := max(receipt.FailureCount, 0)
	passed := max(receipt.ExecutedTests-failed, 0)
	skippedTests := max(receipt.SkippedTests, 0)
	items = append(items,
		property{"executedTests", strconv.Itoa(receipt.ExecutedTests)}, property{"failedTests", strconv.Itoa(failed)},
		property{"skippedTests", strconv.Itoa(skippedTests)}, property{"flakyTests", strconv.Itoa(max(receipt.FlakyTests, 0))})
	// Every failed test of an attempt whose only failures belong to a goal
	// that did not complete is unproven, not a defect, as in the run outcome.
	goalOnly := receipt.Status == runner.StatusFailed && receipt.GoalFailed && receipt.NonGoalFailureCount == 0
	details := newFailureDetails(receipt)
	if receipt.ValidTests() && len(receipt.Tests) > 0 {
		for index, test := range receipt.Tests {
			each := testcase{Classname: spec, Name: oneLine(test.Title, maxNameBytes)}
			if each.Name == "" && test.TestID != "" {
				each.Name = "test " + test.TestID[:12]
			} else if each.Name == "" {
				each.Name = "test " + strconv.Itoa(index+1)
			}
			if test.DurationMS > 0 {
				each.Time = seconds(test.DurationMS)
			}
			var own []property
			if test.TestID != "" {
				own = append(own, property{"testId", test.TestID})
			}
			switch test.Status {
			case runner.TestFlaky:
				own = append(own, property{"flaky", "true"})
				passed--
			case runner.TestPassed:
				passed--
			case runner.TestSkipped:
				each.Skipped = &skipped{}
				skippedTests--
			case runner.TestFailed:
				failed--
				summary, failure := details.take(test)
				markFailed(&each, test.Class, goalOnly, summary, failure)
			}
			each.Properties = cleanProperties(own)
			result.Cases = append(result.Cases, each)
		}
		if receipt.TestsTruncated {
			items = append(items, property{"testsTruncated", "true"})
		}
	} else {
		// No per-test list, as in a result written before lists existed:
		// the failed tests the receipt describes, then the counts.
		for index, failure := range receipt.Failures {
			if failed == 0 {
				break
			}
			failed--
			each := testcase{Classname: spec, Name: oneLine(failure.Title, maxNameBytes)}
			if each.Name == "" {
				each.Name = "failed test " + strconv.Itoa(index+1)
			}
			class := failure.Class
			if failure.UnexpectedPass() {
				class = runner.FailureUnexpectedPass
			}
			failure := failure
			markFailed(&each, class, goalOnly, nil, &failure)
			result.Cases = append(result.Cases, each)
		}
		if len(receipt.Failures) == 0 {
			for _, test := range details.summaries {
				if failed == 0 {
					break
				}
				failed--
				each := testcase{Classname: spec, Name: "failed test " + strconv.Itoa(len(result.Cases)+1)}
				if test.TestID != "" {
					each.Name = "test " + test.TestID[:12]
					each.Properties = cleanProperties([]property{{"testId", test.TestID}})
				}
				test := test
				markFailed(&each, test.Class, goalOnly, &test, nil)
				result.Cases = append(result.Cases, each)
			}
		}
		items = append(items, property{"testsListed", "false"})
	}
	// What the list left out is reported once per outcome, so a truncated
	// list cannot hide a failure and the suite still accounts for the rest.
	if failed > 0 {
		each := testcase{Classname: spec, Name: fmt.Sprintf("%d more failed test(s), not listed individually", failed)}
		markFailed(&each, runner.FailureOther, goalOnly, nil, nil)
		each.Properties = cleanProperties([]property{{"tests", strconv.Itoa(failed)}})
		result.Cases = append(result.Cases, each)
	}
	if passed > 0 {
		result.Cases = append(result.Cases, testcase{Classname: spec, Name: fmt.Sprintf("%d passed test(s), not listed individually", passed),
			Properties: cleanProperties([]property{{"tests", strconv.Itoa(passed)}})})
	}
	if skippedTests > 0 {
		result.Cases = append(result.Cases, testcase{Classname: spec, Name: fmt.Sprintf("%d skipped test(s), not listed individually", skippedTests),
			Skipped: &skipped{}, Properties: cleanProperties([]property{{"tests", strconv.Itoa(skippedTests)}})})
	}
	// A failed attempt without a failed test to show, such as one whose
	// tests passed and whose goal did not complete, still did not pass.
	unexplained := receipt.Status == runner.StatusFailed
	for _, each := range result.Cases {
		unexplained = unexplained && each.Failure == nil && each.Error == nil
	}
	if unexplained {
		result.Cases = append(result.Cases, testcase{Name: spec, Classname: spec,
			Error: &problem{Type: stop, Message: "the attempt did not pass (" + stop + ")", Body: text(receipt.Error, maxBodyBytes)}})
	}
	result.Properties = cleanProperties(items)
	return result
}

// stopReason is the receipt's failure summary stop token when it is a known
// one, else a token derived from the status. A passed receipt has none.
func stopReason(receipt runner.Receipt) string {
	if receipt.Status == runner.StatusPassed && receipt.Validated {
		return ""
	}
	if receipt.FailureSummary != nil && receipt.FailureSummary.Validate() == nil {
		return receipt.FailureSummary.Stop
	}
	switch receipt.Status {
	case runner.StatusCanceled:
		return runner.StopCanceled
	case runner.StatusTimedOut:
		return runner.StopDeadline
	case runner.StatusFailed:
		return runner.StopTestFailed
	}
	if !receipt.Validated && receipt.Executed {
		return runner.StopInvalidEvidence
	}
	return runner.StopInfrastructure
}

// markFailed marks a testcase failed. Setup and environment classes, and the
// failures of a goal that did not complete, are errors: the test could not
// establish anything about the application. Every other class is a failure.
func markFailed(target *testcase, class string, goalOnly bool, summary *runner.FailedTestSummary, failure *runner.TestFailure) {
	switch class {
	case runner.FailureAssertion, runner.FailureTimeout, runner.FailureActionTimeout, runner.FailureEnvironment, runner.FailureUnexpectedPass, runner.FailureSetup:
	default:
		class = runner.FailureOther
	}
	// The structured account first: closed tokens from the failure summary.
	described := class
	if summary != nil {
		if summary.Operation != "" {
			described += ": " + summary.Operation
			if summary.Control != "" {
				described += " on " + summary.Control
			}
		} else if summary.Step != "" {
			described += ": " + summary.Step + " step"
		}
		if summary.Reason != "" {
			described += " (" + summary.Reason + ")"
		}
		if summary.Setup != "" {
			described += "; setup " + summary.Setup
		}
	}
	message, body := described, ""
	if failure != nil {
		if first, _, _ := strings.Cut(strings.TrimSpace(failure.Message), "\n"); first != "" {
			message = first
		}
		body = strings.TrimSpace(strings.Join([]string{described, failure.Location, failure.Message}, "\n"))
	}
	detail := &problem{Type: class, Message: oneLine(message, maxMessageBytes), Body: text(body, maxBodyBytes)}
	switch {
	case goalOnly:
		detail.Type = runner.StopGoalFailed
		detail.Message = oneLine("goal did not complete; "+message, maxMessageBytes)
		target.Error = detail
	case class == runner.FailureSetup || class == runner.FailureEnvironment:
		target.Error = detail
	default:
		target.Failure = detail
	}
}

// failureDetails matches a listed failed test with what else the receipt
// says about it: the failure summary's entry by test digest and the
// reported failure by title. Unmatched tests keep their class alone.
type failureDetails struct {
	summaries []runner.FailedTestSummary
	byID      map[string]runner.FailedTestSummary
	byTitle   map[string][]runner.TestFailure
}

func newFailureDetails(receipt runner.Receipt) *failureDetails {
	details := &failureDetails{byID: map[string]runner.FailedTestSummary{}, byTitle: map[string][]runner.TestFailure{}}
	if summary := receipt.FailureSummary; summary != nil && summary.Validate() == nil {
		details.summaries = summary.Tests
		for _, test := range summary.Tests {
			if test.TestID != "" {
				details.byID[test.TestID] = test
			}
		}
	}
	for _, failure := range receipt.Failures {
		key := titleKey(failure.Title)
		details.byTitle[key] = append(details.byTitle[key], failure)
	}
	return details
}

func (details *failureDetails) take(test runner.TestOutcome) (*runner.FailedTestSummary, *runner.TestFailure) {
	var summary *runner.FailedTestSummary
	if found, ok := details.byID[test.TestID]; ok && test.TestID != "" {
		summary = &found
	}
	var failure *runner.TestFailure
	if key := titleKey(test.Title); test.Title != "" && len(details.byTitle[key]) > 0 {
		failure, details.byTitle[key] = &details.byTitle[key][0], details.byTitle[key][1:]
	}
	return summary, failure
}

// titleKey compares a listed title with a reported failure's title, which
// are bounded separately: the start of the normalized title identifies both.
func titleKey(title string) string {
	return strings.TrimSuffix(bound(runner.TestTitle(title), runner.MaxTestTitleBytes-16), "…")
}

// skippedInputs reports each input the plan dropped as an error: a spec that
// did not run is missing evidence, not a test that chose to skip.
func skippedInputs(summary runner.RunSummary) suite {
	result := suite{Name: "9l skipped inputs", Time: seconds(0)}
	for _, input := range summary.Skipped {
		name := oneLine(input.Input, maxNameBytes)
		if name == "" {
			name = "skipped input"
		}
		result.Cases = append(result.Cases, testcase{Name: name, Classname: "9l skipped inputs",
			Error: &problem{Type: "skipped_input", Message: "input was not run: " + oneLine(input.Reason, maxMessageBytes-32), Body: text(input.Reason, maxBodyBytes)}})
	}
	if missing := summary.SkippedInputs - len(summary.Skipped); missing > 0 || len(result.Cases) == 0 {
		result.Cases = append(result.Cases, testcase{Name: fmt.Sprintf("%d skipped input(s), not listed individually", max(missing, 1)), Classname: "9l skipped inputs",
			Error: &problem{Type: "skipped_input", Message: "inputs were not run"}})
	}
	return result
}

func cleanProperties(items []property) *properties {
	if len(items) == 0 {
		return nil
	}
	cleaned := make([]property, 0, len(items))
	for _, item := range items {
		cleaned = append(cleaned, property{Name: item.Name, Value: oneLine(item.Value, maxNameBytes)})
	}
	return &properties{Items: cleaned}
}

func seconds(milliseconds int64) string {
	return strconv.FormatFloat(float64(max(milliseconds, 0))/1000, 'f', 3, 64)
}

// text prepares free text for the document: without terminal escapes or
// characters XML 1.0 forbids, redacted and bounded on a rune boundary.
func text(value string, limit int) string {
	value = strings.ToValidUTF8(runner.StripTerminalEscapes(value), "")
	value = strings.Map(func(char rune) rune {
		switch {
		case char == '\t' || char == '\n':
			return char
		case char == '\r':
			return -1
		case char < 0x20 || char == 0x7f || (char >= 0x80 && char <= 0x9f) || char == 0xfffe || char == 0xffff || char == utf8.RuneError:
			return -1
		}
		return char
	}, value)
	return bound(runner.RedactText(value), limit)
}

// oneLine is text on a single line, for names and attribute values.
func oneLine(value string, limit int) string {
	return bound(strings.Join(strings.Fields(text(value, math.MaxInt)), " "), limit)
}

func bound(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	const ellipsis = "…"
	cut := max(limit-len(ellipsis), 0)
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut] + ellipsis
}
