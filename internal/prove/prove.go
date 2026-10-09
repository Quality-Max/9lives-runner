package prove

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// Policy names the fault set and classification rules. Change it whenever
// either changes, so reports from different rules are never compared as equal.
const Policy = "prove-network-v2"

const (
	KindAbort     = "abort"
	KindHTTP500   = "http-500"
	KindEmptyJSON = "empty-json"
)

// Results. Only caught and survived are conclusive about the fault.
const (
	Caught                 = "caught"
	Survived               = "survived"
	FailedWithoutAssertion = "failed-without-assertion"
	NotExercised           = "not-exercised"
	NotApplicable          = "not-applicable"
	// Retried marks a test Playwright ran more than once in the fault run,
	// for example under test.describe.configure({retries}); a later attempt
	// can hide what the first one detected, so nothing is concluded.
	Retried    = "retried"
	Incomplete = "incomplete"
	NotRun     = "not-run"
)

// Fault is what Go sends to the SDK in NINELIVES_PROVE_FAULT. Its JSON form
// must match readFault in packages/playwright/src/prove.ts.
type Fault struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Target
	Request string `json:"-"`
}

func (fault Fault) Env() string {
	raw, _ := json.Marshal(struct {
		ID     string `json:"id"`
		Kind   string `json:"kind"`
		Method string `json:"method"`
		Origin string `json:"origin"`
		Path   string `json:"path"`
	}{fault.ID, fault.Kind, fault.Method, fault.Origin, fault.Path})
	return string(raw)
}

// RequestReport identifies a request without its URL unless the caller asks
// for paths: the persisted report never contains them.
type RequestReport struct {
	ID           string `json:"id"`
	Method       string `json:"method"`
	ResourceType string `json:"resourceType"`
	Digest       string `json:"digest"`
	URL          string `json:"url,omitempty"`
	Observed     int    `json:"observed"`
	JSON         bool   `json:"json"`
	target       Target
}

// Plan orders requests by method, origin and path and gives each the faults
// that apply to it. Faults beyond max are returned in notRun, never dropped.
func Plan(observations Observations, max int) (requests []RequestReport, faults, notRun []Fault) {
	keys := make([]string, 0, len(observations.Requests))
	for key := range observations.Requests {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for index, key := range keys {
		request := observations.Requests[key]
		digest := sha256.Sum256([]byte(key))
		report := RequestReport{
			ID: fmt.Sprintf("req-%d", index+1), Method: request.Method, ResourceType: request.ResourceType,
			Digest: hex.EncodeToString(digest[:]), Observed: request.Observed, JSON: request.JSON2xx, target: request.Target,
		}
		requests = append(requests, report)
		kinds := []string{KindAbort, KindHTTP500}
		if request.JSON2xx {
			kinds = append(kinds, KindEmptyJSON)
		}
		for _, kind := range kinds {
			fault := Fault{ID: fmt.Sprintf("fault-%d", len(faults)+len(notRun)+1), Kind: kind, Target: request.Target, Request: report.ID}
			if len(faults) < max {
				faults = append(faults, fault)
			} else {
				notRun = append(notRun, fault)
			}
		}
	}
	return requests, faults, notRun
}

// WithURLs returns requests that carry their origin and path, for local output.
func WithURLs(requests []RequestReport) []RequestReport {
	out := make([]RequestReport, len(requests))
	for index, request := range requests {
		request.URL = request.target.Origin + request.target.Path
		out[index] = request
	}
	return out
}

// TestFacts is what one fault run established about one test: its engine
// outcome and attempts, and how the fault met its requests.
type TestFacts struct {
	Outcome         string
	Attempts        int
	AssertionFailed bool
	Applied         int
	NotApplicable   map[string]int
}

// RunFacts is what one fault run established. Validated is false when the
// receipt is incomplete, canceled, timed out or failed validation.
type RunFacts struct {
	Validated bool
	Overflow  bool
	Tests     map[string]TestFacts
}

// TestResult is one test's result under one fault.
type TestResult struct {
	TestID  string `json:"testId"`
	Result  string `json:"result"`
	Applied int    `json:"applied"`
	// Reason says why an empty-json fault was not applicable: the response
	// was not a JSON object or array, or the upstream request failed.
	Reason string `json:"reason,omitempty"`
}

func notApplicableReason(counts map[string]int) string {
	if counts[ReasonUnreachable] > 0 && counts[ReasonNotJSON] == 0 {
		return ReasonUnreachable
	}
	return ReasonNotJSON
}

func classifyTest(facts TestFacts) TestResult {
	result := TestResult{Applied: facts.Applied}
	notApplicable := 0
	for _, count := range facts.NotApplicable {
		notApplicable += count
	}
	switch {
	case facts.Applied == 0 && notApplicable > 0:
		result.Result, result.Reason = NotApplicable, notApplicableReason(facts.NotApplicable)
	case facts.Applied == 0 || facts.Outcome == "skipped":
		// Whatever happened, the fault cannot be credited with it.
		result.Result = NotExercised
	case facts.Attempts > 1:
		result.Result = Retried
	case facts.Outcome == "expected":
		result.Result = Survived
	case facts.Outcome == "unexpected" && facts.AssertionFailed:
		result.Result = Caught
	default:
		result.Result = FailedWithoutAssertion
	}
	return result
}

// Classify gives the fault's result and each test's. A test counts only when
// the fault was applied to one of its requests. A fault survives when any such
// test passed, and is caught only when none did and an assertion failed.
func Classify(facts RunFacts) (string, []TestResult) {
	if !facts.Validated || facts.Overflow {
		return Incomplete, nil
	}
	ids := make([]string, 0, len(facts.Tests))
	for id := range facts.Tests {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	tests := make([]TestResult, 0, len(ids))
	counts := map[string]int{}
	for _, id := range ids {
		result := classifyTest(facts.Tests[id])
		result.TestID = id
		tests = append(tests, result)
		counts[result.Result]++
	}
	switch {
	case counts[Retried] > 0:
		return Retried, tests
	case counts[Survived] > 0:
		return Survived, tests
	case counts[Caught] > 0:
		return Caught, tests
	case counts[FailedWithoutAssertion] > 0:
		return FailedWithoutAssertion, tests
	case counts[NotApplicable] > 0:
		return NotApplicable, tests
	default:
		return NotExercised, tests
	}
}

type FaultReport struct {
	ID      string `json:"id"`
	Request string `json:"request"`
	Kind    string `json:"kind"`
	Result  string `json:"result"`
	RunID   string `json:"runId,omitempty"`
	// Applied sums the fault's applications over every test in the run.
	Applied           int          `json:"applied"`
	FailedTests       int          `json:"failedTests"`
	AssertionFailures int          `json:"assertionFailedTests"`
	Tests             []TestResult `json:"tests,omitempty"`
	DurationMS        int64        `json:"durationMs"`
}

type Baseline struct {
	RunID         string `json:"runId"`
	Status        string `json:"status"`
	ExecutedTests int    `json:"executedTests"`
	Attempts      int    `json:"instrumentedAttempts"`
}

type Summary struct {
	Faults       int `json:"faults"`
	Caught       int `json:"caught"`
	Survived     int `json:"survived"`
	Inconclusive int `json:"inconclusive"`
	NotRun       int `json:"notRun"`
	// Exercised counts faults that were applied to at least one test, with
	// or without a conclusive result.
	Exercised int `json:"exercised"`
}

// Why a proof is incomplete, in the order checked.
const (
	IncompleteInterrupted      = "interrupted"
	IncompleteInvalidEvidence  = "invalid-evidence"
	IncompleteFaultsNotRun     = "faults-not-run"
	IncompleteNothingExercised = "nothing-exercised"
)

type Report struct {
	Version  int    `json:"version"`
	Policy   string `json:"policy"`
	Spec     string `json:"spec"`
	Complete bool   `json:"complete"`
	// IncompleteReason names the first reason the proof is not complete.
	IncompleteReason string          `json:"incompleteReason,omitempty"`
	Baseline         Baseline        `json:"baseline"`
	Requests         []RequestReport `json:"requests"`
	Faults           []FaultReport   `json:"faults"`
	Summary          Summary         `json:"summary"`
	Limits           []string        `json:"limits"`
	Interrupted      bool            `json:"interrupted,omitempty"`
	// InvalidEvidence marks a fault run whose prove records failed
	// validation; such a proof is never complete.
	InvalidEvidence bool `json:"invalidEvidence,omitempty"`
}

var Limits = []string{
	"A caught fault means an assertion failed while the fault was injected; it does not show that the assertion checks the intended behavior.",
	"Only fetch and XHR requests in browser contexts from the @9l/playwright context fixture are observed and faulted; documents, assets, APIRequestContext and manually created contexts are not.",
	"A request the test handles with its own route, by fulfilling, continuing or aborting it, is never faulted; a test route must call route.fallback() for the fault to apply.",
	"A test that Playwright retries inside a fault run, for example under test.describe.configure({retries}), gives no result for that fault.",
	"A fault applies to every request with the same method, origin and path in its run; query strings are ignored, and an origin that changes between runs is never matched.",
}

func Summarize(faults []FaultReport) Summary {
	summary := Summary{Faults: len(faults)}
	for _, fault := range faults {
		switch fault.Result {
		case Caught:
			summary.Caught++
		case Survived:
			summary.Survived++
		case NotRun:
			summary.NotRun++
		default:
			summary.Inconclusive++
		}
		if fault.Result != NotRun && fault.Result != NotExercised && fault.Result != NotApplicable && fault.Result != Incomplete {
			summary.Exercised++
		}
	}
	return summary
}

// Completeness decides whether the proof established what it set out to:
// every planned fault ran with valid evidence, and at least one was applied.
func Completeness(report Report) (bool, string) {
	switch {
	case report.Interrupted:
		return false, IncompleteInterrupted
	case report.InvalidEvidence:
		return false, IncompleteInvalidEvidence
	case report.Summary.NotRun > 0:
		return false, IncompleteFaultsNotRun
	case report.Summary.Exercised == 0:
		return false, IncompleteNothingExercised
	}
	return true, ""
}
