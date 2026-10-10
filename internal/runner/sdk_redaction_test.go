package runner_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/adapters/playwrightsdk"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// prove and confirm re-read SDK evidence from the persisted, redacted file.
// The largest stream the validator accepts must therefore persist unchanged:
// if redaction rewrote or grew it, a validated run would read back as invalid
// evidence. Every frame value is a closed enum, a digest or a runner-issued ID,
// so redaction has nothing to match, even with secret-like words in a run ID.
func TestLargestValidSDKStreamPersistsUnchanged(t *testing.T) {
	const maxEvents, maxProtocolBytes = 10000, 4 << 20
	// validRunID admits these words; the redaction patterns need a separator
	// such as '=' or ':' after them, which a run ID cannot contain.
	owner := runner.AttemptIdentity{RunID: "run-" + strings.Repeat("token.secret_password-api_key.", 4), JobID: "job-001", AttemptID: "job-001-attempt-001"}
	test := strings.Repeat("a", 64)
	artifacts := make([]any, 32)
	for i := range artifacts {
		artifacts[i] = map[string]any{"id": fmt.Sprintf("artifact-%d", i+1), "kind": "json", "retained": false}
	}
	frames := []map[string]any{
		{"type": "hello", "capabilities": []string{"steps", "artifact-metadata", "terminal-outcomes"}, "totalTests": 1},
		{"type": "test_begin", "testId": test, "retry": 0},
	}
	for step := 1; len(frames) < maxEvents-3; step++ {
		frames = append(frames, map[string]any{"type": "step_end", "testId": test, "retry": 0, "stepId": fmt.Sprintf("step-%d", step), "category": "assertion", "status": "passed"})
	}
	frames = append(frames,
		map[string]any{"type": "test_end", "testId": test, "retry": 0, "status": "passed", "expectedStatus": "passed", "artifacts": artifacts},
		map[string]any{"type": "test_result", "testId": test, "outcome": "expected"},
		map[string]any{"type": "end", "status": "passed"},
	)
	var raw bytes.Buffer
	for i, frame := range frames {
		frame["version"], frame["runId"], frame["jobId"], frame["attemptId"], frame["seq"] = playwrightsdk.Version, owner.RunID, owner.JobID, owner.AttemptID, i+1
		if err := json.NewEncoder(&raw).Encode(frame); err != nil {
			t.Fatal(err)
		}
	}
	stream := raw.Bytes()
	if len(frames) != maxEvents || len(stream) < maxProtocolBytes*3/4 {
		t.Fatalf("stream is not near the limits: %d frames, %d bytes", len(frames), len(stream))
	}
	if _, err := playwrightsdk.New().ValidateAttempt(stream, owner); err != nil {
		t.Fatalf("the largest stream must validate: %v", err)
	}
	persisted := runner.Redact(stream)
	if !bytes.Equal(persisted, stream) {
		t.Fatal("redaction changed validated SDK evidence")
	}
	if facts, err := playwrightsdk.Assertions(persisted); err != nil || facts.Tests[test].Outcome != "expected" {
		t.Fatalf("persisted evidence no longer reads back: %#v %v", facts, err)
	}
}
