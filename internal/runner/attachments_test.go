package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		receipt := summary.Receipts[0]
		if receipt.KeepAttachments != keep || receipt.FailureAttachments != len(adapter.attachments) || receipt.RetainedAttachments != map[bool]int{false: 0, true: 2}[keep] {
			t.Fatalf("keep=%v receipt does not account for its attachments: keep=%v produced=%d retained=%d", keep, receipt.KeepAttachments, receipt.FailureAttachments, receipt.RetainedAttachments)
		}
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

// A retention request that found nothing to copy is recorded as such.
func TestKeepAttachmentsWithoutAttachmentsIsRecorded(t *testing.T) {
	plan := Plan{Version: 1, RunID: "run-no-attachments", Jobs: []Job{fakeJob("job-001", "test-failure", 0)}}
	summary, err := Execute(context.Background(), plan, ExecuteOptions{ReceiptDir: t.TempDir(), KeepAttachments: true, Adapters: []Adapter{reportingAdapter{}}})
	if err != nil || len(summary.Receipts) != 1 || len(summary.Receipts[0].Failures) != 1 {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
	if receipt := summary.Receipts[0]; !receipt.KeepAttachments || receipt.FailureAttachments != 0 || receipt.RetainedAttachments != 0 {
		t.Fatalf("receipt: keep=%v produced=%d retained=%d", receipt.KeepAttachments, receipt.FailureAttachments, receipt.RetainedAttachments)
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

// annotatingAdapter reports one annotation from any report it is given.
type annotatingAdapter struct{ fakeAdapter }

func (annotatingAdapter) Annotations([]byte) []TestAnnotation {
	return []TestAnnotation{{Test: "t", Type: "icon-font", Description: "0 visible icon(s)"}}
}

func TestPassedAttemptCarriesTestAnnotations(t *testing.T) {
	plan := Plan{Version: 1, RunID: "run-annotations", Jobs: []Job{fakeJob("job-001", "success", 0)}}
	summary, err := Execute(context.Background(), plan, ExecuteOptions{ReceiptDir: t.TempDir(), Adapters: []Adapter{annotatingAdapter{}}})
	if err != nil || len(summary.Receipts) != 1 || summary.Receipts[0].Status != StatusPassed || len(summary.Receipts[0].Annotations) != 1 {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

// scratchReportingAdapter reports a native worker's failure. Its attachments
// are what Playwright wrote into the attempt's private scratch, which only
// FailuresWithin may name.
type scratchReportingAdapter struct{ channelAdapter }

func (scratchReportingAdapter) Failures([]byte, string) []TestFailure {
	return []TestFailure{{Title: "native › failure capture"}}
}

func (scratchReportingAdapter) FailuresWithin(_ []byte, _ string, roots []string) []TestFailure {
	failure := TestFailure{Title: "native › failure capture"}
	for _, root := range roots {
		directory := filepath.Join(root, "attachments")
		entries, _ := os.ReadDir(directory)
		for _, entry := range entries {
			contentType := map[string]string{".png": "image/png", ".xml": "text/xml"}[filepath.Ext(entry.Name())]
			failure.Attachments = append(failure.Attachments, TestAttachment{Name: entry.Name(), ContentType: contentType, Path: filepath.Join(directory, entry.Name())})
		}
	}
	return []TestFailure{failure}
}

// captureExecutor stands in for a native worker whose failed test attached an
// opted-in failure capture to Playwright's output in the private scratch.
type captureExecutor struct{ t *testing.T }

var syntheticPNG = []byte("\x89PNG\r\n\x1a\nsynthetic failure image")

func (e captureExecutor) Run(_ context.Context, job Job, _ int) ProcessOutput {
	directory := filepath.Join(job.Env["NINELIVES_TARGET_SCRATCH_DIR"], "playwright-output", "attachments")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		e.t.Fatal(err)
	}
	tree := filepath.Join(directory, "9l.failure-view-tree-1.xml")
	if os.WriteFile(filepath.Join(directory, "9l.failure-screenshot-1.png"), syntheticPNG, 0o600) != nil ||
		os.WriteFile(tree, []byte(`<hierarchy><node text="token=synthetic-secret-value" password="false"/></hierarchy>`), 0o600) != nil {
		e.t.Fatal("fixture")
	}
	// A file that cannot be retained (not a regular file) is not referenced either.
	if err := os.Symlink(tree, filepath.Join(directory, "linked.xml")); err != nil {
		e.t.Log("symlink unavailable:", err)
	}
	if err := os.WriteFile(job.Env["NINELIVES_FAKE_EVENTS"], []byte(`{"outcome":"failed","assertions":0,"artifact":true}`), 0o600); err != nil {
		e.t.Fatal(err)
	}
	return ProcessOutput{Executed: true, ExitCode: 1}
}

// A native target's Playwright output is private scratch removed with the
// attempt. Its attachments are referenced only when --keep-attachments
// copies them into the receipt directory; the receipt holds references,
// digests and counts, never the image or the view tree.
func TestNativeAttachmentsAreRetainedOnlyOnRequest(t *testing.T) {
	for _, keep := range []bool{false, true} {
		root := t.TempDir()
		job := fakeJob("job-001", "test-failure", 0)
		job.Target = "android:finance"
		plan := Plan{Version: 1, RunID: "run-native-capture", Jobs: []Job{job}}
		summary, err := Execute(context.Background(), plan, ExecuteOptions{Timeout: 5 * time.Second, ReceiptDir: root, KeepAttachments: keep, Executor: captureExecutor{t: t}, Adapters: []Adapter{scratchReportingAdapter{}}})
		if err != nil || len(summary.Receipts) != 1 {
			t.Fatalf("keep=%v summary=%+v err=%v", keep, summary, err)
		}
		receipt := summary.Receipts[0]
		if receipt.Status != StatusFailed || len(receipt.Failures) != 1 {
			t.Fatalf("keep=%v: a failed native test stays failed and listed: %+v", keep, receipt)
		}
		attachments := receipt.Failures[0].Attachments
		if !keep {
			if len(attachments) != 0 || receipt.FailureAttachments != 0 || receipt.RetainedAttachments != 0 {
				t.Fatalf("without retention a scratch file was referenced: %+v", attachments)
			}
			continue
		}
		if len(attachments) != 2 || receipt.FailureAttachments != 2 || receipt.RetainedAttachments != 2 {
			t.Fatalf("retained %+v (produced=%d retained=%d)", attachments, receipt.FailureAttachments, receipt.RetainedAttachments)
		}
		for _, attachment := range attachments {
			if !attachment.Retained || !strings.HasPrefix(attachment.Path, root) || attachment.SHA256 == "" {
				t.Fatalf("not retained into the receipt directory: %+v", attachment)
			}
		}
		image, _ := os.ReadFile(attachments[0].Path)
		sum := sha256.Sum256(image)
		if attachments[0].ContentType != "image/png" || string(image) != string(syntheticPNG) || attachments[0].SHA256 != hex.EncodeToString(sum[:]) {
			t.Fatalf("screenshot not copied byte for byte with its digest: %+v", attachments[0])
		}
		tree, _ := os.ReadFile(attachments[1].Path)
		if attachments[1].ContentType != "text/xml" || strings.Contains(string(tree), "synthetic-secret-value") || !strings.Contains(string(tree), "<hierarchy>") {
			t.Fatalf("view tree not retained redacted: %q", tree)
		}
		persisted, _ := os.ReadFile(receipt.ReceiptPath)
		for _, leaked := range []string{"synthetic failure image", "hierarchy", "synthetic-secret-value"} {
			if strings.Contains(string(persisted), leaked) {
				t.Fatalf("receipt.json carries capture contents %q", leaked)
			}
		}
	}
}
