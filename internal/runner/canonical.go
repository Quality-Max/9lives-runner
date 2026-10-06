package runner

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// writeCanonicalReceipt exports the platform-owned execution-receipt/1.0
// envelope beside the legacy local receipt. The legacy file remains the local
// CLI contract; this is an additive, portable evidence export.
func writeCanonicalReceipt(root string, receipt Receipt) error {
	availability := "available"
	verdict := "unknown"
	failure := any(nil)
	if receipt.Status == StatusPassed {
		verdict = "passed"
	}
	if receipt.Status == StatusFailed || receipt.Status == StatusError {
		if receipt.FailureCount > 0 {
			verdict = "failed"
			failure = map[string]any{"category": "test", "code": "test.failed", "confidence": 1.0, "evidence_ids": []string{"terminal", "counts"}}
		} else if receipt.ExitCode != 0 {
			verdict = "failed"
			failure = map[string]any{"category": "infrastructure", "code": "execution.process_failed", "confidence": 1.0, "evidence_ids": []string{"terminal"}}
		}
	}
	if receipt.Status == StatusTimedOut {
		verdict = "timed_out"
		failure = map[string]any{"category": "infrastructure", "code": "execution.timeout", "confidence": 1.0, "evidence_ids": []string{"terminal"}}
	}
	if receipt.Status == StatusCanceled {
		verdict = "cancelled"
		failure = map[string]any{"category": "policy", "code": "execution.cancelled", "confidence": 1.0, "evidence_ids": []string{"terminal"}}
	}
	if !receipt.Executed && receipt.Status != StatusCanceled && receipt.Status != StatusTimedOut {
		availability = "unavailable"
		verdict = "unknown"
		failure = nil
	}
	framework := receipt.Adapter
	if framework != "playwright" {
		framework = "unknown"
	}
	artifacts := []map[string]any{}
	for _, artifact := range receipt.Evidence.Artifacts {
		kind := "report"
		if artifact.Kind == "terminal-stderr" {
			kind = "stderr"
		}
		if artifact.Kind == "structured-output" {
			kind = "stdout"
		}
		truncated := (artifact.Kind == "structured-output" && receipt.Evidence.StdoutTruncated) || (artifact.Kind == "terminal-stderr" && receipt.Evidence.StderrTruncated)
		item := map[string]any{"id": artifact.Kind, "kind": kind, "availability": "missing", "reference": nil, "checksum_sha256": nil, "provenance": "runner", "access": "opaque", "reason": "artifact was not produced"}
		if artifact.Present && artifact.SHA256 != "" {
			if truncated {
				item["availability"] = "truncated"
				item["reason"] = "captured output exceeded the configured byte limit"
			} else {
				item["availability"] = "available"
				item["reason"] = nil
			}
			item["reference"] = "receipt-artifact:" + artifact.SHA256
			item["checksum_sha256"] = artifact.SHA256
		}
		artifacts = append(artifacts, item)
	}
	exitCode := any(receipt.ExitCode)
	if receiptObservedSignal(receipt.Termination) != nil {
		exitCode = nil
	}
	payload := map[string]any{
		"schema_version": "1.0", "receipt_id": boundedRevision(receipt.RunID+"-"+receipt.AttemptID, 160), "framework": framework,
		"correlation": map[string]any{"workflow_id": receipt.RunID, "job_id": receipt.JobID, "run_id": receipt.RunID, "attempt_id": receipt.AttemptID, "attempt_number": receipt.Attempt, "parent_receipt_id": nil, "project_id": nil, "script_id": nil, "availability": "available"},
		"revisions":   map[string]any{"command": boundedRevision(strings.Join(receipt.Evidence.Command, " "), 512), "target": boundedRevision(receipt.Spec, 160), "manifest": nil, "adapter": receipt.Adapter, "template": nil, "availability": "available"},
		"state":       "terminal", "stage": "finished", "verdict": verdict,
		"terminal":   map[string]any{"exit_code": exitCode, "signal": receiptObservedSignal(receipt.Termination), "timed_out": receipt.Status == StatusTimedOut, "cancelled": receipt.Status == StatusCanceled, "started_at": receipt.StartedAt.Format(time.RFC3339Nano), "completed_at": receipt.FinishedAt.Format(time.RFC3339Nano), "duration_seconds": float64(receipt.DurationMS) / 1000, "source": "runner", "availability": availability},
		"counts":     map[string]any{"total": receipt.ExecutedTests + receipt.SkippedTests, "passed": receipt.ExecutedTests - receipt.FailureCount, "failed": receipt.FailureCount, "skipped": receipt.SkippedTests, "errors": 0, "unit": "tests", "availability": map[bool]string{true: "available", false: "unavailable"}[receipt.Validated]},
		"assertions": map[string]any{"total": nil, "passed": nil, "failed": 0, "availability": "unavailable"}, "failure": failure, "artifacts": artifacts,
		"resources": map[string]any{"availability": "unavailable"}, "decision": map[string]any{"confidence": 1.0, "evidence_ids": []string{"terminal"}, "reason": "local runner evidence"},
		"limitations": []string{"Local receipts are durable evidence only; interrupted runs are not restartable."},
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	_, _, err = writeAtomic(filepath.Join(root, receipt.RunID, receipt.JobID, receipt.AttemptID), "execution-receipt-1.0.json", append(raw, '\n'))
	return err
}

func boundedRevision(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	prefix := filepath.Base(value)
	if len(prefix) > limit-17 {
		prefix = prefix[:limit-17]
	}
	return fmt.Sprintf("%s#%x", prefix, sum[:8])
}

func receiptObservedSignal(termination *Termination) any {
	if termination == nil || termination.ObservedSignal == 0 {
		return nil
	}
	return termination.ObservedSignal
}
