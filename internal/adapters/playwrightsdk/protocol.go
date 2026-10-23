package playwrightsdk

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"unicode/utf8"

	"github.com/Quality-Max/9lives-runner/internal/runner"
	"github.com/Quality-Max/9lives-runner/internal/strictjson"
)

const Version = "9l.engine/1"
const TargetVersion = "9l.target/1"
const maxFrameBytes = 16 * 1024
const maxEvents = 10000
const maxProtocolBytes = 4 << 20

var testID = regexp.MustCompile(`^[a-f0-9]{64}$`)
var stepID = regexp.MustCompile(`^step-[1-9][0-9]{0,8}$`)

type event struct {
	Version        string   `json:"version"`
	RunID          string   `json:"runId"`
	JobID          string   `json:"jobId"`
	AttemptID      string   `json:"attemptId"`
	Seq            int      `json:"seq"`
	Type           string   `json:"type"`
	Capabilities   []string `json:"capabilities"`
	TotalTests     int      `json:"totalTests"`
	TestID         string   `json:"testId"`
	Retry          int      `json:"retry"`
	StepID         string   `json:"stepId"`
	Category       string   `json:"category"`
	Status         string   `json:"status"`
	ExpectedStatus string   `json:"expectedStatus"`
	Outcome        string   `json:"outcome"`
	SkipKey        string   `json:"skipKey"`
	Artifacts      []struct {
		ID       string `json:"id"`
		Kind     string `json:"kind"`
		Retained bool   `json:"retained"`
	} `json:"artifacts"`
	TargetID      string `json:"targetId"`
	TargetVersion string `json:"targetVersion"`
	Operation     string `json:"operation"`
}

type testState struct {
	retry        int
	active       bool
	statuses     []string
	expected     string
	final        bool
	goalFailed   bool
	targetFailed bool
	assertions   int
	// reportedAttempt counts the assertion steps the worker reported as passed
	// in the attempt in progress; reportedFinal and finalStatus keep the last
	// completed attempt's count and status.
	reportedAttempt int
	reportedFinal   int
	finalStatus     string
}

func TargetID(target string) string {
	sum := sha256.Sum256([]byte(target))
	return hex.EncodeToString(sum[:])
}

func invalid(reason string) (runner.Validation, error) {
	return runner.Validation{}, fmt.Errorf("invalid SDK engine evidence: %s", reason)
}

// skipPins holds SkipPinKey digests the caller declared before the run. A
// skipped test is complete evidence only when its key is pinned.
func validate(raw []byte, identity runner.AttemptIdentity, skipPins map[string]bool) (runner.Validation, error) {
	if identity.RunID == "" || identity.JobID == "" || identity.AttemptID == "" {
		return invalid("missing owning attempt identity")
	}
	if len(raw) == 0 || len(raw) > maxProtocolBytes || !bytes.HasSuffix(raw, []byte("\n")) || !utf8.Valid(raw) {
		return invalid("missing, oversized or unfinished stream")
	}
	if bytes.Count(raw, []byte("\n")) > maxEvents {
		return invalid("event limit exceeded")
	}
	lines := bytes.Split(raw[:len(raw)-1], []byte("\n"))
	tests := make(map[string]*testState)
	steps := make(map[string]bool)
	total, unpinned := 0, 0
	targeted := identity.Target != ""
	if targeted && !ValidTarget(identity.Target) {
		return invalid("unsupported owning target")
	}
	manifestClosed := false
	ended, summarizing := false, false
	validation := runner.Validation{AssertionCoverage: "unknown", Description: "9l.engine/1; attempt-bound step evidence; verified assertion count unavailable; artifact metadata only"}
	for index, line := range lines {
		frame, err := decodeEvent(line)
		if err != nil {
			return invalid("malformed frame")
		}
		if ended {
			return invalid("event after terminal outcome")
		}
		if frame.Version != Version {
			return invalid("unsupported protocol version")
		}
		if frame.RunID != identity.RunID || frame.JobID != identity.JobID || frame.AttemptID != identity.AttemptID {
			return invalid("foreign attempt identity")
		}
		if frame.Seq != index+1 {
			return invalid("duplicate or out-of-order sequence")
		}
		if (targeted && frame.TargetID != TargetID(identity.Target)) || (!targeted && frame.TargetID != "") {
			return invalid("missing or foreign target identity")
		}
		if index == 0 {
			caps := frame.Capabilities
			if targeted {
				if frame.TargetVersion != TargetVersion || len(caps) != 4 || caps[3] != TargetVersion {
					return invalid("unsupported target contract")
				}
				caps = caps[:3]
			} else if frame.TargetVersion != "" {
				return invalid("unexpected target contract")
			}
			if frame.Type != "hello" || frame.TotalTests < 1 || frame.TotalTests > maxEvents || !equalCapabilities(caps) {
				return invalid("missing handshake, unsupported capabilities or zero tests")
			}
			total = frame.TotalTests
			continue
		}
		state := tests[frame.TestID]
		switch frame.Type {
		case "required_test":
			if !targeted || manifestClosed || !testID.MatchString(frame.TestID) || state != nil || len(tests) >= total {
				return invalid("invalid required target test")
			}
			tests[frame.TestID] = &testState{retry: -1}
		case "test_begin":
			if targeted {
				manifestClosed = true
				if len(tests) != total || state == nil || frame.Retry != 0 {
					return invalid("incomplete target manifest or repeated target test")
				}
			}
			if summarizing || !testID.MatchString(frame.TestID) || frame.Retry < 0 || frame.Retry > maxEvents {
				return invalid("invalid test attempt")
			}
			if state == nil {
				if frame.Retry != 0 || len(tests) >= total {
					return invalid("unexpected test attempt")
				}
				state = &testState{retry: -1}
				tests[frame.TestID] = state
			}
			// Playwright retries every test in a serial group, including prior
			// expected and skipped attempts. Validate ordering, not retry policy.
			if state.active || state.final || frame.Retry != state.retry+1 {
				return invalid("duplicate or late test attempt")
			}
			state.retry, state.active, state.reportedAttempt = frame.Retry, true, 0
		case "step_end":
			if summarizing || state == nil || !state.active || frame.Retry != state.retry || !stepID.MatchString(frame.StepID) || steps[frame.StepID] {
				return invalid("foreign, duplicate or late step")
			}
			if !oneOf(frame.Category, "action", "assertion", "browser", "fixture", "other", "goal") || !oneOf(frame.Status, "passed", "failed") {
				return invalid("unsupported step outcome")
			}
			steps[frame.StepID] = true
			if frame.Category == "assertion" && frame.Status == "passed" {
				state.reportedAttempt++
			}
			if frame.Operation != "" {
				assertion := oneOf(frame.Operation, "assert-visible", "assert-text", "assert-state")
				if !targeted || !oneOf(frame.Operation, "open", "reset", "tap", "fill", "assert-visible", "assert-text", "assert-state") || (assertion && frame.Category != "assertion") || (!assertion && frame.Category != "action") {
					return invalid("unsupported target operation")
				}
				if frame.Status == "failed" {
					state.targetFailed = true
				} else if assertion {
					state.assertions++
				}
			}
			if frame.Category == "goal" && frame.Status == "failed" {
				validation.GoalFailed, state.goalFailed = true, true
			}
		case "test_end":
			if summarizing || state == nil || !state.active || frame.Retry != state.retry {
				return invalid("missing or duplicate test end")
			}
			if !oneOf(frame.Status, "passed", "failed", "timedOut", "skipped") || !oneOf(frame.ExpectedStatus, "passed", "failed", "skipped") {
				return invalid("unfinished or unsupported test status")
			}
			if targeted {
				// Skips stay subject to --pin-skip. An expected failure would
				// let a failed target operation report success.
				switch {
				case frame.ExpectedStatus == "failed":
					return invalid("target tests cannot expect failure")
				case frame.Status != "failed" && frame.Status != "timedOut" && state.targetFailed:
					return invalid("target test hid a failed target operation")
				case frame.Status == "passed" && state.assertions == 0:
					return invalid("target test passed without a successful target assertion")
				}
			}
			if state.expected != "" && state.expected != frame.ExpectedStatus {
				return invalid("changed expected outcome")
			}
			if len(frame.Artifacts) > 32 {
				return invalid("artifact limit exceeded")
			}
			for i, artifact := range frame.Artifacts {
				if artifact.ID != fmt.Sprintf("artifact-%d", i+1) || artifact.Retained || !oneOf(artifact.Kind, "image", "json", "other") {
					return invalid("unsupported artifact metadata")
				}
			}
			state.active, state.expected = false, frame.ExpectedStatus
			state.statuses = append(state.statuses, frame.Status)
			if frame.Status != "skipped" {
				state.reportedFinal, state.finalStatus = state.reportedAttempt, frame.Status
			}
		case "test_result":
			summarizing = true
			if state == nil || state.active || state.final || len(state.statuses) == 0 {
				return invalid("missing, duplicate or unfinished test result")
			}
			if frame.Outcome != outcome(state) {
				return invalid("inconsistent test outcome")
			}
			state.final = true
			if frame.Outcome == "skipped" {
				if !testID.MatchString(frame.SkipKey) {
					return invalid("invalid skip key")
				}
				validation.SkippedTests++
				if !skipPins[frame.SkipKey] {
					unpinned++
				}
			} else {
				validation.ExecutedTests++
				validation.TargetAssertions += state.assertions
				// Worker-reported, from the last completed attempt: a signal
				// that a passed test asserted nothing, never verified coverage.
				validation.ReportedAssertions += state.reportedFinal
				if state.finalStatus == "passed" && state.reportedFinal == 0 {
					validation.PassedWithoutAssertions++
				}
				if frame.Outcome == "unexpected" {
					validation.FailureCount++
					// Preserve failures in other tests even if a goal failed
					// (including a caught goal error) elsewhere in this file.
					if !state.goalFailed {
						validation.NonGoalFailureCount++
					}
				}
			}
		case "end":
			if !oneOf(frame.Status, "passed", "failed") || len(tests) != total {
				return invalid("incomplete terminal outcome")
			}
			for _, test := range tests {
				if !test.final || test.active {
					return invalid("missing final test evidence")
				}
			}
			if validation.ExecutedTests == 0 {
				return invalid("no completed tests")
			}
			if unpinned != 0 {
				return invalid("selected tests were skipped without a pin")
			}
			if validation.SkippedTests != 0 {
				validation.Description += fmt.Sprintf("; %d pinned skip(s)", validation.SkippedTests)
			}
			validation.Description += fmt.Sprintf("; %d worker-reported assertion step(s)", validation.ReportedAssertions)
			if validation.PassedWithoutAssertions != 0 {
				validation.Description += fmt.Sprintf("; %d passed test(s) reported none", validation.PassedWithoutAssertions)
			}
			if (frame.Status == "passed") != (validation.FailureCount == 0) {
				return invalid("inconsistent terminal outcome")
			}
			ended = true
		default:
			return invalid("unexpected event or worker error")
		}
	}
	if !ended {
		return invalid("missing terminal outcome")
	}
	if targeted {
		validation.Description += "; 9l.target/1; required target tests; worker-reported target assertion count only"
	}
	return validation, nil
}

// Match Playwright test.outcome(): an expected attempt after an unexpected
// attempt is flaky; skipped attempts do not prove execution.
func outcome(state *testState) string {
	expected, unexpected := false, false
	for _, status := range state.statuses {
		if status == "skipped" {
			continue
		}
		if status == state.expected {
			expected = true
		} else {
			unexpected = true
		}
	}
	if !expected && !unexpected {
		return "skipped"
	}
	if expected && unexpected {
		return "flaky"
	}
	if unexpected {
		return "unexpected"
	}
	return "expected"
}

// Capabilities is the exact capability list a hello frame must carry. The
// SDK declares the same list in its package metadata so doctor can compare
// an installed SDK with this engine without running it.
var Capabilities = []string{"steps", "artifact-metadata", "terminal-outcomes"}

func equalCapabilities(values []string) bool     { return slices.Equal(values, Capabilities) }
func oneOf(value string, allowed ...string) bool { return slices.Contains(allowed, value) }

// Reject unknown/missing/duplicate fields rather than accepting a best-effort
// JSON object. Errors never echo worker-controlled values into a receipt.
func decodeEvent(raw []byte) (event, error) {
	var frame event
	if len(raw) == 0 || len(raw)+1 > maxFrameBytes {
		return frame, fmt.Errorf("invalid frame size")
	}
	// A frame is exactly one object: no surrounding whitespace, so a CRLF
	// stream or padding after a valid value is rejected rather than trimmed.
	if raw[0] != '{' || raw[len(raw)-1] != '}' {
		return frame, fmt.Errorf("unframed object")
	}
	if err := strictjson.Value(json.NewDecoder(bytes.NewReader(raw))); err != nil {
		return frame, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return frame, fmt.Errorf("expected object")
	}
	values := make(map[string]json.RawMessage)
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok || values[name] != nil {
			return frame, fmt.Errorf("invalid member")
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(value, []byte("null")) {
			return frame, fmt.Errorf("invalid value")
		}
		values[name] = value
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
		return frame, fmt.Errorf("unterminated object")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return frame, fmt.Errorf("trailing data")
	}
	if json.Unmarshal(raw, &frame) != nil {
		return frame, fmt.Errorf("invalid fields")
	}
	fields := []string{"version", "runId", "jobId", "attemptId", "seq", "type"}
	if frame.TargetID != "" {
		fields = append(fields, "targetId")
	}
	switch frame.Type {
	case "hello":
		fields = append(fields, "capabilities", "totalTests")
		if frame.TargetID != "" {
			fields = append(fields, "targetVersion")
		}
	case "required_test":
		fields = append(fields, "testId")
	case "test_begin":
		fields = append(fields, "testId", "retry")
	case "step_end":
		fields = append(fields, "testId", "retry", "stepId", "category", "status")
		if frame.Operation != "" {
			fields = append(fields, "operation")
		}
	case "test_end":
		fields = append(fields, "testId", "retry", "status", "expectedStatus", "artifacts")
	case "test_result":
		fields = append(fields, "testId", "outcome")
		if frame.Outcome == "skipped" {
			fields = append(fields, "skipKey")
		}
	case "end":
		fields = append(fields, "status")
	case "engine_error":
	default:
		return frame, fmt.Errorf("unsupported event")
	}
	if len(values) != len(fields) {
		return frame, fmt.Errorf("unexpected fields")
	}
	for _, key := range fields {
		if values[key] == nil {
			return frame, fmt.Errorf("missing field")
		}
	}
	// Artifact objects are a closed metadata schema too.
	if value := values["artifacts"]; value != nil {
		var artifacts []map[string]json.RawMessage
		if json.Unmarshal(value, &artifacts) != nil {
			return frame, fmt.Errorf("invalid artifacts")
		}
		for _, artifact := range artifacts {
			if len(artifact) != 3 || artifact["id"] == nil || artifact["kind"] == nil || artifact["retained"] == nil {
				return frame, fmt.Errorf("invalid artifact")
			}
		}
	}
	return frame, nil
}

// TestAssertionFacts is one test's outcome in an attempt's evidence.
type TestAssertionFacts struct {
	// Outcome is Playwright's final outcome: expected, unexpected, flaky or
	// skipped.
	Outcome string
	// Attempts counts Playwright attempts of the test, so 2 or more means it
	// was retried inside this run.
	Attempts int
	// AssertionFailed reports that an assertion step failed in any attempt.
	AssertionFailed bool
}

// AssertionFacts counts failed tests and, among them, those in which an
// assertion step failed, and keeps the same facts per test. Call it only on
// evidence ValidateAttempt accepted; it re-reads frames but does not re-check
// identity or ordering.
type AssertionFacts struct {
	Unexpected              int
	UnexpectedWithAssertion int
	Tests                   map[string]TestAssertionFacts
}

func Assertions(raw []byte) (AssertionFacts, error) {
	facts := AssertionFacts{Tests: map[string]TestAssertionFacts{}}
	if len(raw) == 0 || len(raw) > maxProtocolBytes || !bytes.HasSuffix(raw, []byte("\n")) {
		return facts, fmt.Errorf("invalid SDK engine evidence")
	}
	for _, line := range bytes.Split(raw[:len(raw)-1], []byte("\n")) {
		frame, err := decodeEvent(line)
		if err != nil {
			return facts, fmt.Errorf("invalid SDK engine evidence")
		}
		test := facts.Tests[frame.TestID]
		switch frame.Type {
		case "test_begin":
			test.Attempts++
		case "step_end":
			if frame.Category == "assertion" && frame.Status == "failed" {
				test.AssertionFailed = true
			}
		case "test_result":
			test.Outcome = frame.Outcome
			if frame.Outcome == "unexpected" {
				facts.Unexpected++
				if test.AssertionFailed {
					facts.UnexpectedWithAssertion++
				}
			}
		default:
			continue
		}
		facts.Tests[frame.TestID] = test
	}
	return facts, nil
}
