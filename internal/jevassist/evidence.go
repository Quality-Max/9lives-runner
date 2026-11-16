// Package jevassist composes optional Jev judgments about one failed attempt:
// a bounded diagnosis and a grounded locator choice. Go owns every policy
// decision; Jev only answers typed questions about attempt-bound evidence.
// Nothing here changes a receipt, retries, or acts on the application.
package jevassist

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/adapters/playwright"
	"github.com/Quality-Max/9lives-runner/internal/healing"
	"github.com/Quality-Max/9lives-runner/internal/jev"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

const (
	maxMessageBytes    = 2 << 10
	maxScreenshotBytes = 16 << 20
	// The whole observation, including labels, must fit Jev's 16 KiB snapshot.
	maxObservationBytes = 15 << 10
)

// Evidence is what one failed attempt left behind, bound to that attempt.
// Facts come from the runner and the framework; none is model inference.
type Evidence struct {
	Identity               runner.AttemptIdentity
	Revision, SourceSHA256 string
	Test, Location         string
	// FailureClass is the runner's structured class; DeterministicClass is the
	// native healing classification of the same message.
	FailureClass, DeterministicClass string
	// Message is the runner's bounded, redacted first error. It may quote page
	// text, so it is sent only as observation data.
	Message       string
	FailedLocator string
	// Snapshot is the sanitized ARIA snapshot at failure, "" when absent.
	Snapshot string
	// Screenshot is the framework's failure screenshot, nil when absent.
	Screenshot []byte
	ObservedAt time.Time
}

// FromReceipt reads the first failed test of a failed receipt. Missing
// attachments are recorded as limitations, never invented.
func FromReceipt(receipt runner.Receipt, revision, sourceSHA256 string) (Evidence, []string, error) {
	if receipt.Status == runner.StatusPassed || len(receipt.Failures) == 0 {
		return Evidence{}, nil, errors.New("receipt has no failed test")
	}
	failure := receipt.Failures[0]
	var limitations []string
	if len(receipt.Failures) > 1 {
		limitations = append(limitations, "only the first failed test is diagnosed")
	}
	message := failure.Message
	if len(message) > maxMessageBytes {
		message = truncateUTF8(message, maxMessageBytes)
		limitations = append(limitations, "the error message was truncated")
	}
	e := Evidence{
		Identity: runner.AttemptIdentity{RunID: receipt.RunID, JobID: receipt.JobID, AttemptID: receipt.AttemptID, Target: "web"},
		Revision: revision, SourceSHA256: sourceSHA256,
		Test: failure.Title, Location: failure.Location, FailureClass: failure.Class,
		DeterministicClass: healing.Classify(failure.Message, ""), Message: message,
		FailedLocator: healing.FailedLocator(failure.Message),
		Snapshot:      playwright.PageSnapshot(receipt.Failures[:1]),
		ObservedAt:    receipt.FinishedAt,
	}
	if e.Snapshot == "" {
		limitations = append(limitations, "no accessible snapshot was attached to the failure")
	}
	for _, attachment := range failure.Attachments {
		if attachment.Name != "screenshot" || (attachment.ContentType != "image/png" && attachment.ContentType != "image/jpeg") || attachment.Bytes > maxScreenshotBytes {
			continue
		}
		if raw, err := playwright.ReadAttachment(attachment.Path, maxScreenshotBytes); err == nil {
			e.Screenshot = raw
			break
		}
	}
	if e.Screenshot == nil {
		limitations = append(limitations, "no failure screenshot was attached; enable screenshot: 'only-on-failure' to include one")
	}
	return e, limitations, nil
}

// binding names this attempt's failed step. Test titles are hashed so page or
// project text never becomes an identifier.
func (e Evidence) binding() jev.Binding {
	sum := sha256.Sum256([]byte(e.Test + "\x00" + e.Location))
	return jev.Binding{RunID: e.Identity.RunID, JobID: e.Identity.JobID, AttemptID: e.Identity.AttemptID, Target: e.Identity.Target, Revision: e.Revision, SourceSHA256: e.SourceSHA256, TestID: "test-" + hex.EncodeToString(sum[:8]), StepID: "failed-step", ObservationID: "failure-evidence"}
}

// observationText is the single data field Jev receives. Labels are fixed;
// everything after them is untrusted observed content.
func (e Evidence) observationText(extra string) (string, bool) {
	var b strings.Builder
	b.WriteString("Failed step location: " + oneLine(e.Location) + "\n")
	b.WriteString("Runner failure class: " + oneLine(e.FailureClass) + "\n")
	b.WriteString("Failed target locator: " + oneLine(firstNonEmpty(e.FailedLocator, "none named")) + "\n")
	b.WriteString("Error message (untrusted, may quote page text):\n" + e.Message + "\n")
	if extra != "" {
		b.WriteString(extra)
	}
	truncated := false
	if e.Snapshot != "" {
		b.WriteString("Accessible snapshot at failure (untrusted page content):\n")
		room := maxObservationBytes - b.Len()
		snapshot := e.Snapshot
		if len(snapshot) > room {
			snapshot, truncated = truncateUTF8(snapshot, max(0, room)), true
		}
		b.WriteString(snapshot)
	}
	return b.String(), truncated
}

func (e Evidence) failureClass() string {
	switch e.FailureClass {
	case runner.FailureAssertion, runner.FailureTimeout, runner.FailureActionTimeout, runner.FailureEnvironment, runner.FailureUnexpectedPass, runner.FailureSetup, runner.FailureOther:
		return e.FailureClass
	}
	return ""
}

// AssertionFailure reports whether either classification says an expectation
// failed. Such a failure is never eligible for locator repair.
func (e Evidence) AssertionFailure() bool {
	return e.FailureClass == runner.FailureAssertion || e.DeterministicClass == "assertion_failed"
}

func digest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func oneLine(s string) string {
	return truncateUTF8(strings.Join(strings.Fields(s), " "), 300)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func truncateUTF8(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && cut < len(s) && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut]
}
