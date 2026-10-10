package playwright

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

func TestFailuresNameEachFailedTestWithLocationErrorAndAttachments(t *testing.T) {
	project := t.TempDir()
	context := filepath.Join(project, "test-results", "login", "error-context.md")
	write(t, context, "# Page snapshot\n- button \"Anmelden\"\n", 0o600)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	write(t, outside, "not part of the project", 0o600)
	spec := filepath.Join(project, "tests", "login.spec.ts")
	report := map[string]any{"suites": []any{map[string]any{
		"title": "tests/login.spec.ts",
		"suites": []any{map[string]any{"title": "login", "specs": []any{map[string]any{
			"title": "renamed button", "file": "tests/login.spec.ts", "line": 4,
			"tests": []any{map[string]any{"status": "unexpected", "projectName": "chromium", "results": []any{
				map[string]any{"status": "failed", "errors": []any{map[string]any{"message": "first attempt"}}},
				map[string]any{"status": "timedOut", "errors": []any{map[string]any{
					"message":  "\x1b[31mTimeoutError\x1b[39m: locator.click: Timeout\nCall log:\n  - waiting for getByRole('button', { name: 'Login' })\n  token=abc123",
					"location": map[string]any{"file": spec, "line": 9},
				}}, "attachments": []any{
					map[string]any{"name": "error-context", "contentType": "text/markdown", "path": context},
					map[string]any{"name": "screenshot", "contentType": "image/png", "path": filepath.Join(project, "test-results", "login", "missing.png")},
					map[string]any{"name": "leak", "contentType": "text/plain", "path": outside},
					map[string]any{"name": "inline", "contentType": "text/plain", "body": "aGk="},
				}},
			}}},
		}}}},
		"specs": []any{map[string]any{"title": "passes", "file": "tests/login.spec.ts", "line": 20,
			"tests": []any{map[string]any{"status": "expected", "results": []any{map[string]any{"status": "passed"}}}}}},
	}}}
	raw, _ := json.Marshal(report)
	failures := New().Failures(raw, project)
	if len(failures) != 1 {
		t.Fatalf("failures=%+v", failures)
	}
	got := failures[0]
	if got.Title != "[chromium] login › renamed button" || got.Location != "tests/login.spec.ts:9" {
		t.Fatalf("title/location=%q %q", got.Title, got.Location)
	}
	if strings.Contains(got.Message, "\x1b") || strings.Contains(got.Message, "abc123") || !strings.HasPrefix(got.Message, "TimeoutError: locator.click") || strings.Contains(got.Message, "first attempt") {
		t.Fatalf("message=%q", got.Message)
	}
	if len(got.Attachments) != 2 || got.Attachments[0].Name != "error-context" || got.Attachments[0].Bytes == 0 || got.Attachments[0].Retained || got.Attachments[1].Name != "screenshot" || got.Attachments[1].Bytes != 0 {
		t.Fatalf("attachments=%+v", got.Attachments)
	}
}

func TestFailuresAreBounded(t *testing.T) {
	var specs []any
	for i := 0; i < 25; i++ {
		specs = append(specs, map[string]any{"title": fmt.Sprintf("t%d", i), "tests": []any{map[string]any{"status": "unexpected", "results": []any{
			map[string]any{"status": "failed", "errors": []any{map[string]any{"message": strings.Repeat("é", 2000)}}},
		}}}})
	}
	raw, _ := json.Marshal(map[string]any{"suites": []any{map[string]any{"title": "f.spec.ts", "specs": specs}}})
	failures := New().Failures(raw, t.TempDir())
	if len(failures) != 20 {
		t.Fatalf("len=%d", len(failures))
	}
	if message := failures[0].Message; len(message) > 1024+len("…") || !strings.HasSuffix(message, "…") {
		t.Fatalf("message not bounded: %d bytes", len(message))
	}
}

func TestFailuresDoNotFollowSymlinkedDirectoriesOutOfTheProject(t *testing.T) {
	project, elsewhere := t.TempDir(), t.TempDir()
	write(t, filepath.Join(elsewhere, "id_rsa"), "key", 0o600)
	if err := os.Symlink(elsewhere, filepath.Join(project, "test-results")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	raw, _ := json.Marshal(map[string]any{"suites": []any{map[string]any{"title": "f.spec.ts", "specs": []any{map[string]any{"title": "t", "tests": []any{map[string]any{"status": "unexpected", "results": []any{
		map[string]any{"status": "failed", "attachments": []any{map[string]any{"name": "x", "path": filepath.Join(project, "test-results", "id_rsa")}}},
	}}}}}}}})
	if failures := New().Failures(raw, project); len(failures) != 1 || len(failures[0].Attachments) != 0 {
		t.Fatalf("followed a symlink out of the project: %+v", failures)
	}
}

func TestPageSnapshotReadsTheAriaBlockOfErrorContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "error-context.md")
	write(t, path, "# Error details\n\n```\nTimeoutError\n```\n\n# Page snapshot\n\n```yaml\n- button \"Einloggen\" [ref=e2]\n- status\n```\n\n# Test source\n", 0o600)
	failures := []runner.TestFailure{{Attachments: []runner.TestAttachment{{Name: "screenshot", Path: "x.png"}, {Name: "error-context", Path: path, Bytes: 10}}}}
	if got := PageSnapshot(failures); got != "- button \"Einloggen\" [ref=e2]\n- status" {
		t.Fatalf("snapshot=%q", got)
	}
	if got := PageSnapshot(nil); got != "" {
		t.Fatalf("no failures: %q", got)
	}
}

func TestFailureContextKeepsOneCleanCopyOfEachError(t *testing.T) {
	message := "TimeoutError: locator.fill: Timeout 1500ms exceeded.\nCall log:\n\x1b[2m  - waiting for locator('#emailAddress')\x1b[22m"
	raw, _ := json.Marshal(map[string]any{"suites": []any{map[string]any{"specs": []any{map[string]any{"tests": []any{map[string]any{"results": []any{
		map[string]any{"status": "failed", "error": map[string]any{"message": message}, "errors": []any{map[string]any{"message": message}}},
	}}}}}}}})
	want := "TimeoutError: locator.fill: Timeout 1500ms exceeded.\nCall log:\n  - waiting for locator('#emailAddress')"
	for i := 0; i < 5; i++ {
		if got := FailureContext(raw); got != want {
			t.Fatalf("context=%q", got)
		}
	}
}

func TestPageSnapshotDropsTypedValuesAndRedacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "error-context.md")
	write(t, path, "# Page snapshot\n\n```yaml\n- textbox \"API token\" [ref=e2]: sk-live-123\n- textbox [ref=e3]: typed\n- combobox \"Land\": Deutschland\n- combobox \"Country\" [ref=e5]:\n  - option \"DE\" [selected]\n- textbox \"Notes\" [active] [ref=e6]: \"private note: x\"\n- text: password=hunter2\n- button \"Save\" [ref=e4]\n```\n", 0o600)
	got := PageSnapshot([]runner.TestFailure{{Attachments: []runner.TestAttachment{{Name: "error-context", Path: path, Bytes: 10}}}})
	want := "- textbox \"API token\" [ref=e2]\n- textbox [ref=e3]\n- combobox \"Land\"\n- combobox \"Country\" [ref=e5]:\n  - option \"DE\" [selected]\n- textbox \"Notes\" [active] [ref=e6]\n- text: password=[REDACTED]\n- button \"Save\" [ref=e4]"
	if got != want {
		t.Fatalf("snapshot=%q", got)
	}
}

func TestReadAttachmentRefusesNonRegularAndOversizedFiles(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.md")
	write(t, big, strings.Repeat("x", 2048), 0o600)
	if _, err := ReadAttachment(big, 1024); err == nil {
		t.Fatal("oversized attachment read")
	}
	if _, err := ReadAttachment(dir, 1024); err == nil {
		t.Fatal("directory read")
	}
	if raw, err := ReadAttachment(big, 4096); err != nil || len(raw) != 2048 {
		t.Fatalf("regular file: %d %v", len(raw), err)
	}
}

func TestFailureContextIsRedactedBeforeClipping(t *testing.T) {
	// The context leaves the machine in provider prompts, MCP results and
	// session JSON: secrets in error text must be redacted, and the 3,000-byte
	// clip must not cut a pair in half and dodge the patterns.
	message := "Error: request failed\nAuthorization: Bearer sk-live-abcdef123456\napi_key=AKIA1234ABCD in body\n" + strings.Repeat("é", 3000)
	raw, _ := json.Marshal(map[string]any{"suites": []any{map[string]any{"specs": []any{map[string]any{"tests": []any{map[string]any{"results": []any{
		map[string]any{"status": "failed", "error": map[string]any{"message": message}},
	}}}}}}}})
	got := FailureContext(raw)
	if strings.Contains(got, "sk-live-abcdef123456") || strings.Contains(got, "AKIA1234ABCD") {
		t.Fatalf("context leaked a secret: %q", got[:min(200, len(got))])
	}
	if !strings.Contains(got, "Bearer [REDACTED]") || !strings.Contains(got, "api_key=[REDACTED]") {
		t.Fatalf("context lost the redaction markers: %q", got[:min(200, len(got))])
	}
	if !utf8.ValidString(got) {
		t.Fatal("context is not valid UTF-8 after the clip")
	}
}

func TestFailuresBoundTheSpecControlledTitle(t *testing.T) {
	title := strings.Repeat("ü", 1<<10) + strings.Repeat("t", 1<<20)
	raw, _ := json.Marshal(map[string]any{"suites": []any{map[string]any{"title": "f.spec.ts", "specs": []any{map[string]any{
		"title": title, "tests": []any{map[string]any{"status": "unexpected", "results": []any{
			map[string]any{"status": "failed", "errors": []any{map[string]any{"message": "boom"}}},
		}}},
	}}}}})
	failures := New().Failures(raw, t.TempDir())
	if len(failures) != 1 {
		t.Fatalf("len=%d", len(failures))
	}
	if len(failures[0].Title) > 512 || !utf8.ValidString(failures[0].Title) {
		t.Fatalf("title not bounded on a rune boundary: %d bytes", len(failures[0].Title))
	}
}

func TestSanitizeAttachmentTextDropsTypedValuesInWholeFiles(t *testing.T) {
	// firstFailureContext sanitizes the whole error-context.md, not only a
	// extracted snapshot block: snapshot-shaped lines anywhere in the file
	// must lose their typed values.
	file := "# Error details\n\n# Page snapshot\n\n```yaml\n- spinbutton \"Kartennummer\" [ref=e3]: 4111111111111111\n- textbox \"PIN\" [ref=e4]: 9876\n```\n"
	got := SanitizeAttachmentText(file)
	if strings.Contains(got, "4111111111111111") || strings.Contains(got, "9876") {
		t.Fatalf("typed values survived: %q", got)
	}
	if !strings.Contains(got, `- spinbutton "Kartennummer" [ref=e3]`) || !strings.Contains(got, `- textbox "PIN" [ref=e4]`) {
		t.Fatalf("labels lost: %q", got)
	}
}
