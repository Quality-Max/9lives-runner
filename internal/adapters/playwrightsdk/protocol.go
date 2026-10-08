package playwrightsdk

import (
	"bytes"
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
}

type testState struct {
	retry    int
	active   bool
	statuses []string
	expected string
	final    bool
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
	ended, summarizing := false, false
	validation := runner.Validation{AssertionCoverage: "unknown", Description: "9l.engine/1; attempt-bound step evidence; assertion count unavailable; artifact metadata only"}
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
		if index == 0 {
			if frame.Type != "hello" || frame.TotalTests < 1 || frame.TotalTests > maxEvents || !equalCapabilities(frame.Capabilities) {
				return invalid("missing handshake, unsupported capabilities or zero tests")
			}
			total = frame.TotalTests
			continue
		}
		state := tests[frame.TestID]
		switch frame.Type {
		case "test_begin":
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
			state.retry, state.active = frame.Retry, true
		case "step_end":
			if summarizing || state == nil || !state.active || frame.Retry != state.retry || !stepID.MatchString(frame.StepID) || steps[frame.StepID] {
				return invalid("foreign, duplicate or late step")
			}
			if !oneOf(frame.Category, "action", "assertion", "browser", "fixture", "other", "goal") || !oneOf(frame.Status, "passed", "failed") {
				return invalid("unsupported step outcome")
			}
			steps[frame.StepID] = true
			if frame.Category == "goal" && frame.Status == "failed" {
				validation.GoalFailed = true
			}
		case "test_end":
			if summarizing || state == nil || !state.active || frame.Retry != state.retry {
				return invalid("missing or duplicate test end")
			}
			if !oneOf(frame.Status, "passed", "failed", "timedOut", "skipped") || !oneOf(frame.ExpectedStatus, "passed", "failed", "skipped") {
				return invalid("unfinished or unsupported test status")
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
				if frame.Outcome == "unexpected" {
					validation.FailureCount++
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

func equalCapabilities(values []string) bool {
	return len(values) == 3 && values[0] == "steps" && values[1] == "artifact-metadata" && values[2] == "terminal-outcomes"
}
func oneOf(value string, allowed ...string) bool { return slices.Contains(allowed, value) }

// Reject unknown/missing/duplicate fields rather than accepting a best-effort
// JSON object. Errors never echo worker-controlled values into a receipt.
func decodeEvent(raw []byte) (event, error) {
	var frame event
	if len(raw) == 0 || len(raw)+1 > maxFrameBytes {
		return frame, fmt.Errorf("invalid frame size")
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
	switch frame.Type {
	case "hello":
		fields = append(fields, "capabilities", "totalTests")
	case "test_begin":
		fields = append(fields, "testId", "retry")
	case "step_end":
		fields = append(fields, "testId", "retry", "stepId", "category", "status")
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
