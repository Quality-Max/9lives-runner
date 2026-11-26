package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
// are every file Playwright left in the attempt's private scratch, named as
// a Playwright report would name them; only FailuresWithin may report them.
// Its diagnostics report stands for --failure-details.
type scratchReportingAdapter struct {
	channelAdapter
	details bool
}

func (adapter scratchReportingAdapter) DiagnosticsEnv() string {
	if !adapter.details {
		return ""
	}
	return "NINELIVES_FAKE_DIAGNOSTICS"
}

func (scratchReportingAdapter) Failures([]byte, string) []TestFailure {
	return []TestFailure{{Title: "native › failure capture"}}
}

func (scratchReportingAdapter) FailuresWithin(_ []byte, _ string, roots []string) []TestFailure {
	failure := TestFailure{Title: "native › failure capture"}
	names := map[string][2]string{
		"9l-failure-screenshot-": {"9l.failure-screenshot", "image/png"},
		"9l-failure-view-tree-":  {"9l.failure-view-tree", "text/xml"},
		"trace":                  {"trace", "application/zip"},
		"error-context":          {"error-context", "text/markdown"},
		"linked":                 {"9l.failure-view-tree", "text/xml"},
		"planted":                {"9l.failure-screenshot", "image/png"},
	}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return nil
			}
			for prefix, kind := range names {
				if strings.HasPrefix(entry.Name(), prefix) {
					failure.Attachments = append(failure.Attachments, TestAttachment{Name: kind[0], ContentType: kind[1], Path: path})
				}
			}
			return nil
		})
	}
	return []TestFailure{failure}
}

// captureExecutor stands in for a native worker whose failed test attached an
// opted-in failure capture beside the files a user's config also produces: a
// trace that embeds the view tree unredacted, an error context, a symlink to
// the view tree and a file planted under the screenshot's reserved name.
type captureExecutor struct {
	t    *testing.T
	kept *string
}

var syntheticPNG = []byte("\x89PNG\r\n\x1a\nsynthetic failure image")

const syntheticTree = `<hierarchy><node text="token=synthetic-secret-value" password="false"/></hierarchy>`

func (e captureExecutor) Run(_ context.Context, job Job, _ int) ProcessOutput {
	*e.kept = job.Env[FailureCaptureKeptEnv]
	output := filepath.Join(job.Env["NINELIVES_TARGET_SCRATCH_DIR"], "playwright-output", "transfer-android-finance")
	directory := filepath.Join(output, "attachments")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		e.t.Fatal(err)
	}
	tree := filepath.Join(directory, "9l-failure-view-tree-"+strings.Repeat("b", 40)+".xml")
	for path, body := range map[string]string{
		filepath.Join(directory, "9l-failure-screenshot-"+strings.Repeat("a", 40)+".png"): string(syntheticPNG),
		tree:                               syntheticTree,
		filepath.Join(output, "trace.zip"): "PK synthetic trace " + syntheticTree,
		filepath.Join(output, "error-context.md"): "# Error details\ntoken=synthetic-secret-value",
		filepath.Join(output, "planted.png"):      string(syntheticPNG),
	} {
		if os.WriteFile(path, []byte(body), 0o600) != nil {
			e.t.Fatal("fixture")
		}
	}
	if err := os.Symlink(tree, filepath.Join(directory, "linked-"+strings.Repeat("c", 40)+".xml")); err != nil {
		e.t.Log("symlink unavailable:", err)
	}
	if err := os.WriteFile(job.Env["NINELIVES_FAKE_EVENTS"], []byte(`{"outcome":"failed","assertions":0,"artifact":true}`), 0o600); err != nil {
		e.t.Fatal(err)
	}
	if path := job.Env["NINELIVES_FAKE_DIAGNOSTICS"]; path != "" {
		if err := os.WriteFile(path, []byte(`{"suites":[]}`), 0o600); err != nil {
			e.t.Fatal(err)
		}
	}
	return ProcessOutput{Executed: true, ExitCode: 1}
}

// A native target's Playwright output is private scratch removed with the
// attempt. The worker is told whether a failure capture will be kept, and
// only the capture's two files leave that output, only when retained: the
// receipt holds references, digests and counts, never the image or the tree.
func TestNativeAttachmentsAreRetainedOnlyOnRequest(t *testing.T) {
	t.Setenv(FailureCaptureKeptEnv, "1") // the caller's environment never decides it
	for _, test := range []struct {
		target        string
		details, keep bool
		signal        string
		retained      int
	}{
		{"android:finance", false, false, "0", 0},
		{"android:finance", true, false, "0", 0},
		{"android:finance", false, true, "0", 0},
		{"ios:finance", true, true, "0", 0},
		{"android:finance", true, true, "1", 2},
	} {
		root, kept := t.TempDir(), ""
		job := fakeJob("job-001", "test-failure", 0)
		job.Target = test.target
		plan := Plan{Version: 1, RunID: "run-native-capture", Jobs: []Job{job}}
		summary, err := Execute(context.Background(), plan, ExecuteOptions{Timeout: 5 * time.Second, ReceiptDir: root, KeepAttachments: test.keep, PassEnv: []string{FailureCaptureKeptEnv},
			Executor: captureExecutor{t: t, kept: &kept}, Adapters: []Adapter{scratchReportingAdapter{details: test.details}}})
		label := fmt.Sprintf("%s details=%v keep=%v", test.target, test.details, test.keep)
		if err != nil || len(summary.Receipts) != 1 || summary.Receipts[0].Status != StatusFailed {
			t.Fatalf("%s: summary=%+v err=%v", label, summary, err)
		}
		if kept != test.signal {
			t.Fatalf("%s: the worker was told %q, want %q", label, kept, test.signal)
		}
		receipt := summary.Receipts[0]
		var attachments []TestAttachment
		if len(receipt.Failures) > 0 {
			attachments = receipt.Failures[0].Attachments
		}
		if len(attachments) != test.retained || receipt.FailureAttachments != test.retained || receipt.RetainedAttachments != test.retained {
			t.Fatalf("%s: referenced %+v (produced=%d retained=%d)", label, attachments, receipt.FailureAttachments, receipt.RetainedAttachments)
		}
		if test.retained == 0 {
			continue
		}
		byName := map[string]TestAttachment{}
		for _, attachment := range attachments {
			if !attachment.Retained || !strings.HasPrefix(attachment.Path, root) || attachment.SHA256 == "" {
				t.Fatalf("%s: not retained into the receipt directory: %+v", label, attachment)
			}
			byName[attachment.Name] = attachment
		}
		image, _ := os.ReadFile(byName["9l.failure-screenshot"].Path)
		sum := sha256.Sum256(image)
		if string(image) != string(syntheticPNG) || byName["9l.failure-screenshot"].SHA256 != hex.EncodeToString(sum[:]) {
			t.Fatalf("%s: screenshot not copied byte for byte with its digest", label)
		}
		tree, _ := os.ReadFile(byName["9l.failure-view-tree"].Path)
		if strings.Contains(string(tree), "synthetic-secret-value") || !strings.Contains(string(tree), "<hierarchy>") {
			t.Fatalf("%s: view tree not retained redacted: %q", label, tree)
		}
		retainedFiles, _ := os.ReadDir(filepath.Dir(byName["9l.failure-view-tree"].Path))
		if len(retainedFiles) != 2 {
			t.Fatalf("%s: retained more than the capture: %v", label, retainedFiles)
		}
		persisted, _ := os.ReadFile(receipt.ReceiptPath)
		for _, leaked := range []string{"synthetic failure image", "hierarchy", "synthetic-secret-value", "trace.zip", "error-context", "planted"} {
			if strings.Contains(string(persisted), leaked) {
				t.Fatalf("%s: receipt.json carries %q", label, leaked)
			}
		}
	}
}
