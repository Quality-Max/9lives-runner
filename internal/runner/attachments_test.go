package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reportingAdapter attributes a fake failure to one test whose attachments
// are files prepared by the test.
type reportingAdapter struct {
	fakeAdapter
	attachments []TestAttachment
}

func (adapter reportingAdapter) Failures([]byte, string) []TestFailure {
	return []TestFailure{{Title: "login › renamed button", Attachments: append([]TestAttachment{}, adapter.attachments...)}}
}

func TestFailedAttemptReferencesOrRetainsAttachments(t *testing.T) {
	source := t.TempDir()
	errorContext := filepath.Join(source, "error-context.md")
	screenshot := filepath.Join(source, "test-failed-1.png")
	if os.WriteFile(errorContext, []byte("- textbox \"E-mail\"\npassword=hunter2\n"), 0o600) != nil || os.WriteFile(screenshot, []byte{0x89, 'P', 'N', 'G', 0}, 0o600) != nil {
		t.Fatal("fixture")
	}
	link := filepath.Join(source, "link.md")
	if err := os.Symlink(errorContext, link); err != nil {
		link = ""
	}
	adapter := reportingAdapter{attachments: []TestAttachment{
		{Name: "error-context", ContentType: "text/markdown", Path: errorContext},
		{Name: "screenshot", ContentType: "image/png", Path: screenshot},
		{Name: "missing", ContentType: "text/plain", Path: filepath.Join(source, "gone.txt")},
	}}
	if link != "" {
		adapter.attachments = append(adapter.attachments, TestAttachment{Name: "symlink", ContentType: "text/markdown", Path: link})
	}
	for _, keep := range []bool{false, true} {
		root := t.TempDir()
		plan := Plan{Version: 1, RunID: "run-attachments", Jobs: []Job{fakeJob("job-001", "test-failure", 0)}}
		summary, err := Execute(context.Background(), plan, ExecuteOptions{ReceiptDir: root, KeepAttachments: keep, Adapters: []Adapter{adapter}})
		if err != nil || len(summary.Receipts) != 1 || len(summary.Receipts[0].Failures) != 1 {
			t.Fatalf("keep=%v summary=%+v err=%v", keep, summary, err)
		}
		attachments := summary.Receipts[0].Failures[0].Attachments
		if !keep {
			for _, attachment := range attachments {
				if attachment.Retained || attachment.SHA256 != "" || !strings.HasPrefix(attachment.Path, source) {
					t.Fatalf("reference mode copied or rewrote %+v", attachment)
				}
			}
			continue
		}
		text, binary := attachments[0], attachments[1]
		copied, _ := os.ReadFile(text.Path)
		if !text.Retained || !strings.HasPrefix(text.Path, root) || strings.Contains(string(copied), "hunter2") || !strings.Contains(string(copied), "textbox") {
			t.Fatalf("text attachment not retained redacted: %+v %q", text, copied)
		}
		raw, _ := os.ReadFile(binary.Path)
		sum := sha256.Sum256(raw)
		if !binary.Retained || binary.SHA256 != hex.EncodeToString(sum[:]) || binary.Bytes != 5 {
			t.Fatalf("binary attachment not retained with digest: %+v", binary)
		}
		for _, attachment := range attachments[2:] {
			if attachment.Retained {
				t.Fatalf("retained a missing or symlinked file: %+v", attachment)
			}
		}
		// The receipt on disk carries the same references.
		persisted, _ := os.ReadFile(summary.Receipts[0].ReceiptPath)
		if !strings.Contains(string(persisted), `"retained": true`) && !strings.Contains(string(persisted), `"retained":true`) {
			t.Fatalf("receipt lacks retained attachments: %s", persisted)
		}
	}
}

func TestPassedAttemptReportsNoFailures(t *testing.T) {
	plan := Plan{Version: 1, RunID: "run-passed-failures", Jobs: []Job{fakeJob("job-001", "success", 0)}}
	summary, err := Execute(context.Background(), plan, ExecuteOptions{ReceiptDir: t.TempDir(), KeepAttachments: true, Adapters: []Adapter{reportingAdapter{}}})
	if err != nil || len(summary.Receipts) != 1 || summary.Receipts[0].Failures != nil {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

func TestRunnerErrorLineNamesWhyNoReportWasWritten(t *testing.T) {
	stderr := []byte("\x1b[31mError: Project(s) \"nope\" not found. Available projects: \"chromium\"\x1b[39m\n    at filterProjects (index.js:1)\n")
	if got := runnerErrorLine(stderr); got != `Error: Project(s) "nope" not found. Available projects: "chromium"` {
		t.Fatalf("got %q", got)
	}
	if got := runnerErrorLine([]byte("Error: token=abc123\n")); strings.Contains(got, "abc123") {
		t.Fatalf("not redacted: %q", got)
	}
	if got := runnerErrorLine([]byte("warning only\n")); got != "" {
		t.Fatalf("got %q", got)
	}
}
