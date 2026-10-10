// Package confirm decides whether a reproduction spec distinguishes two
// revisions: whether its assertions fail on the revision a finding was
// reported against and pass on the revision that claims to fix it. A
// confirmed verdict shows that the spec tells the revisions apart; it never
// shows that the spec tests the reported finding.
package confirm

import "sort"

// Policy names the classification rules. Change it whenever they change, so
// reports from different rules are never compared as equal.
const Policy = "confirm-v1"

// ReportVersion is the version of the confirmation report.
const ReportVersion = 1

// What one test did on one revision.
const (
	Passed                 = "passed"
	AssertionFailed        = "assertion-failed"
	FailedWithoutAssertion = "failed-without-assertion"
	Retried                = "retried"
	Skipped                = "skipped"
	// Missing marks a test the other revision reported and this one did not.
	Missing = "missing"
)

// Verdicts, for each test and for the whole confirmation. Only confirmed,
// not-reproduced, fix-ineffective and regressed are conclusive.
const (
	Confirmed      = "confirmed"
	NotReproduced  = "not-reproduced"
	FixIneffective = "fix-ineffective"
	Regressed      = "regressed"
	Inconclusive   = "inconclusive"
)

// Why a verdict is inconclusive.
const (
	ReasonRunIncomplete          = "run-incomplete"
	ReasonNoTests                = "no-tests"
	ReasonTestsDiffer            = "tests-differ"
	ReasonRetried                = "retried"
	ReasonSkipped                = "skipped"
	ReasonFailedWithoutAssertion = "failed-without-assertion"
)

// TestFacts is what the engine evidence established about one test.
type TestFacts struct {
	Outcome         string
	Attempts        int
	AssertionFailed bool
}

// RunFacts is what one revision's run established. Validated is false when
// the receipt is incomplete, canceled, timed out or failed validation.
type RunFacts struct {
	Validated bool
	Tests     map[string]TestFacts
}

// TestResult compares one test across the two revisions.
type TestResult struct {
	TestID  string `json:"testId"`
	Unfixed string `json:"unfixed"`
	Fixed   string `json:"fixed"`
	Result  string `json:"result"`
	Reason  string `json:"reason,omitempty"`
}

func side(facts TestFacts, ok bool) string {
	switch {
	case !ok:
		return Missing
	case facts.Attempts > 1:
		// A later attempt can hide what the first one did.
		return Retried
	case facts.Outcome == "skipped":
		return Skipped
	case facts.Outcome == "expected":
		return Passed
	case facts.AssertionFailed:
		return AssertionFailed
	default:
		return FailedWithoutAssertion
	}
}

func compare(unfixed, fixed string) (string, string) {
	for _, result := range []string{unfixed, fixed} {
		switch result {
		case Missing:
			return Inconclusive, ReasonTestsDiffer
		case Retried:
			return Inconclusive, ReasonRetried
		case Skipped:
			return Inconclusive, ReasonSkipped
		case FailedWithoutAssertion:
			// A timeout or setup error is not the defect being reproduced.
			return Inconclusive, ReasonFailedWithoutAssertion
		}
	}
	switch {
	case unfixed == AssertionFailed && fixed == Passed:
		return Confirmed, ""
	case unfixed == Passed && fixed == Passed:
		return NotReproduced, ""
	case unfixed == AssertionFailed && fixed == AssertionFailed:
		return FixIneffective, ""
	default:
		return Regressed, ""
	}
}

// Classify compares every test the two runs reported. The verdict is the
// first inconclusive test's reason if any; otherwise regressed if a test
// passed on the unfixed revision and failed on the fixed one; otherwise
// fix-ineffective if a test failed on both; otherwise confirmed if a test
// failed on the unfixed revision and passed on the fixed one; otherwise
// not-reproduced.
func Classify(unfixed, fixed RunFacts) (verdict, reason string, tests []TestResult) {
	if !unfixed.Validated || !fixed.Validated {
		return Inconclusive, ReasonRunIncomplete, nil
	}
	ids := map[string]bool{}
	for id := range unfixed.Tests {
		ids[id] = true
	}
	for id := range fixed.Tests {
		ids[id] = true
	}
	if len(ids) == 0 {
		return Inconclusive, ReasonNoTests, nil
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	counts := map[string]int{}
	for _, id := range ordered {
		before, beforeOK := unfixed.Tests[id]
		after, afterOK := fixed.Tests[id]
		result := TestResult{TestID: id, Unfixed: side(before, beforeOK), Fixed: side(after, afterOK)}
		result.Result, result.Reason = compare(result.Unfixed, result.Fixed)
		if result.Result == Inconclusive && reason == "" {
			reason = result.Reason
		}
		counts[result.Result]++
		tests = append(tests, result)
	}
	for _, verdict := range []string{Inconclusive, Regressed, FixIneffective, Confirmed} {
		if counts[verdict] > 0 {
			return verdict, reason, tests
		}
	}
	return NotReproduced, "", tests
}

// Conclusive reports whether a verdict says something about the revisions.
func Conclusive(verdict string) bool { return verdict != Inconclusive }

// Revision describes one side of the comparison.
type Revision struct {
	// Ref is the reference as given, or "working-tree".
	Ref    string `json:"ref"`
	Commit string `json:"commit"`
	// Dirty marks a working tree with changes beyond the reproduction spec.
	Dirty bool `json:"dirty,omitempty"`
	// Spec says how the reproduction spec relates to this revision's file at
	// the same path: absent, same or replaced.
	Spec          string `json:"spec"`
	RunID         string `json:"runId,omitempty"`
	Status        string `json:"status,omitempty"`
	ExecutedTests int    `json:"executedTests"`
}

// How the reproduction spec relates to a revision's file at its path.
const (
	SpecAbsent   = "absent"
	SpecSame     = "same"
	SpecReplaced = "replaced"
)

type Report struct {
	Version int    `json:"version"`
	Policy  string `json:"policy"`
	ID      string `json:"id"`
	// Spec is the reproduction spec's path relative to the repository root.
	Spec       string `json:"spec"`
	SpecSHA256 string `json:"specSHA256"`
	FindingID  string `json:"findingId,omitempty"`
	// FindingSHA256 identifies the finding text without storing it.
	FindingSHA256 string   `json:"findingSHA256,omitempty"`
	Unfixed       Revision `json:"unfixed"`
	Fixed         Revision `json:"fixed"`
	// DependenciesDiffer marks a revision whose package manifest or lockfile
	// differs from the installed one that both runs used.
	DependenciesDiffer bool         `json:"dependenciesDiffer,omitempty"`
	Verdict            string       `json:"verdict"`
	InconclusiveReason string       `json:"inconclusiveReason,omitempty"`
	Tests              []TestResult `json:"tests"`
	Limits             []string     `json:"limits"`
}

var Limits = []string{
	"A confirmed verdict shows that the reproduction spec's assertions failed on the unfixed revision and passed on the fixed one; it does not show that the spec tests the reported finding.",
	"Code under test differs between the runs only when the spec, its fixtures or the Playwright configuration start or import it from the checkout; an application running separately is the same for both runs.",
	"Both revisions run with the node_modules installed in the working tree; dependency changes between revisions are not installed.",
	"Revision checkouts contain committed files only: untracked and ignored files such as .env, Git LFS content and submodules are absent.",
	"Each revision runs once with Playwright retries disabled; a nondeterministic application can give another verdict on another run.",
}
