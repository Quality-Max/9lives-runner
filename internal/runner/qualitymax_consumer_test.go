package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// QualityMax-hosted coding sessions read receipt.json back from the sandbox to
// build their per-turn provenance ledger (docs/qualitymax-coding-sessions.md).
// That reader accepts a receipt only
// at <receipt-dir>/<runId>/<jobId>/<attemptId>/receipt.json with matching
// identity fields, and rejects anything it cannot type-check, so a rename here
// would silently turn every hosted pass into a rejected receipt. This test pins
// the layout and the fields that consumer reads; change both sides together.
func TestReceiptLayoutAndFieldsReadByQualityMaxCodingSessions(t *testing.T) {
	root := t.TempDir()
	started := time.Date(2026, 11, 2, 7, 24, 0, 0, time.UTC)
	receipt := Receipt{
		Version:            1,
		RunID:              "run-20261102T072400Z-699ac8de49c2",
		JobID:              "job-001",
		AttemptID:          "job-001-attempt-001",
		Attempt:            1,
		Spec:               "/home/user/repo/tests/pass.spec.js",
		Adapter:            "playwright",
		Status:             StatusPassed,
		StartedAt:          started,
		FinishedAt:         started.Add(2 * time.Second),
		DurationMS:         2000,
		Executed:           true,
		Validated:          true,
		ExecutedTests:      1,
		SkippedTests:       0,
		FlakyTests:         1,
		ReportedAssertions: 2,
		AssertionCoverage:  "unknown",
		AgentProvenance:    &ProvenanceAssessment{Status: "matched", Scope: "spec"},
	}
	path, err := writeReceipt(root, receipt)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, receipt.RunID, receipt.JobID, receipt.AttemptID, "receipt.json")
	if path != want {
		t.Fatalf("receipt written to %q, QualityMax lists %q", path, want)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	textFields := map[string]string{
		"runId":             receipt.RunID,
		"jobId":             receipt.JobID,
		"attemptId":         receipt.AttemptID,
		"spec":              receipt.Spec,
		"adapter":           "playwright",
		"status":            "passed",
		"assertionCoverage": "unknown",
	}
	for key, value := range textFields {
		if decoded[key] != value {
			t.Errorf("%s = %#v, want %q", key, decoded[key], value)
		}
	}
	numbers := map[string]float64{
		"version":            1,
		"failureCount":       0,
		"executedTests":      1,
		"skippedTests":       0,
		"flakyTests":         1,
		"verifiedAssertions": 0,
		"reportedAssertions": 2,
		"durationMs":         2000,
	}
	for key, value := range numbers {
		if decoded[key] != value {
			t.Errorf("%s = %#v, want %v", key, decoded[key], value)
		}
	}
	for _, key := range []string{"executed", "validated"} {
		if decoded[key] != true {
			t.Errorf("%s = %#v, want true", key, decoded[key])
		}
	}
	for _, key := range []string{"startedAt", "finishedAt"} {
		if _, ok := decoded[key].(string); !ok {
			t.Errorf("%s is not a timestamp string: %#v", key, decoded[key])
		}
	}
	provenance, ok := decoded["agentProvenance"].(map[string]any)
	if !ok || provenance["status"] != "matched" {
		t.Errorf("agentProvenance.status = %#v, want matched", decoded["agentProvenance"])
	}
	if len(raw) > 64*1024 {
		t.Errorf("receipt is %d bytes; QualityMax reads at most 64 KiB", len(raw))
	}
}
