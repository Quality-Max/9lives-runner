package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

func TestFlagsFirstAllowsFlagsAfterSpecs(t *testing.T) {
	got := flagsFirst([]string{"a.spec.ts", "--workers", "3", "b.spec.ts", "--format=json"}, map[string]bool{"--workers": true, "--format": true})
	want := []string{"--workers", "3", "--format=json", "a.spec.ts", "b.spec.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestRunRejectsMissingSpecs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"plan"}, &stdout, &stderr); code != 2 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

func TestPreviewDiffDoesNotUseJSONWriter(t *testing.T) {
	var preview, result bytes.Buffer
	previewDiff(&preview)("line\n", "changed\n")
	if preview.Len() == 0 || result.Len() != 0 || !bytes.Contains(preview.Bytes(), []byte("+++ verified candidate")) {
		t.Fatalf("preview=%q result=%q", preview.String(), result.String())
	}
}

func TestNativeRunTimeoutReadsPositiveEnvironmentOverride(t *testing.T) {
	t.Setenv("NINELIVES_RUN_TIMEOUT", "17")
	if got := nativeRunTimeout(); got.String() != "17s" {
		t.Fatalf("timeout=%s", got)
	}
	t.Setenv("NINELIVES_RUN_TIMEOUT", "not-a-duration")
	if got := nativeRunTimeout(); got.String() != "5m0s" {
		t.Fatalf("fallback=%s", got)
	}
}

type nonInterruptibleReader struct{ release <-chan struct{} }

func (r nonInterruptibleReader) Read([]byte) (int, error) {
	<-r.release
	return 0, io.EOF
}

func TestReadTerminalApprovalSelectsCancellationWhenReaderCannotBeClosed(t *testing.T) {
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan bool, 1)
	go func() { result <- readTerminalApproval(ctx, nonInterruptibleReader{release: release}, io.Discard) }()
	cancel()
	select {
	case approved := <-result:
		if approved {
			t.Fatal("canceled approval accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled approval waited for non-interruptible reader")
	}
	close(release)
}

func TestReadTerminalApprovalCancelsBlockedRead(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan bool, 1)
	go func() { result <- readTerminalApproval(ctx, reader, io.Discard) }()
	cancel()
	select {
	case approved := <-result:
		if approved {
			t.Fatal("canceled approval accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled approval remained blocked")
	}
}

func TestReadTerminalApprovalAcceptsOnlyYOrYes(t *testing.T) {
	for input, want := range map[string]bool{"y\n": true, "yes\n": true, "n\n": false} {
		t.Run(input, func(t *testing.T) {
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(writer, input); err != nil {
				t.Fatal(err)
			}
			_ = writer.Close()
			if got := readTerminalApproval(context.Background(), reader, io.Discard); got != want {
				t.Fatalf("got=%v want=%v", got, want)
			}
		})
	}
}

func TestRejectInvalidGoalConfigurationBeforeExecution(t *testing.T) {
	for _, args := range [][]string{
		{"run", "fixture.spec.ts", "--goal-provider", "openai"},
		{"run", "fixture.spec.ts", "--sdk", "--goal-provider", "other"},
		{"run", "fixture.spec.ts", "--sdk", "--goal-provider", "openai", "--goal-script", "fixture.json"},
		{"run", "fixture.spec.ts", "--sdk", "--goal-provider", "openai", "--goal-max-actions", "0"},
		{"run", "fixture.spec.ts", "--sdk", "--goal-provider", "openai", "--goal-max-cost-micros", "1"},
		{"run", "fixture.spec.ts", "--sdk", "--goal-model", "fixture"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 || stdout.Len() != 0 {
			t.Fatalf("invalid goal config executed: code %d", code)
		}
	}
}

func TestRejectInvalidSkipPinsBeforeExecution(t *testing.T) {
	for _, args := range [][]string{
		{"run", "fixture.spec.ts", "--pin-skip", "tests/a.spec.ts › skipped"},
		{"run", "fixture.spec.ts", "--sdk", "--pin-skip", "no separator"},
		{"run", "fixture.spec.ts", "--sdk", "--pin-skip", "tests/a.spec.ts › two\nlines"},
		{"run", "fixture.spec.ts", "--sdk", "--pin-skip", " tests/a.spec.ts › padded"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "pin-skip") {
			t.Fatalf("invalid skip pin executed: code %d %q", code, stderr.String())
		}
	}
}

func TestPrintResultShowsWhyAJobDidNotPass(t *testing.T) {
	var out bytes.Buffer
	printResult(&out, runner.RunSummary{Outcome: runner.OutcomeIncomplete, Errors: 2, Receipts: []runner.Receipt{
		{Status: runner.StatusPassed, Spec: "tests/ok.spec.ts"},
		{Status: runner.StatusError, Spec: ".context/a.spec.ts", Error: "Playwright found no tests in this spec\n\x1b[31mtestDir"},
		{Status: runner.StatusError, Spec: "tests/long.spec.ts", Error: strings.Repeat("x", 400)},
	}})
	text := out.String()
	if !strings.Contains(text, "  ERROR    .context/a.spec.ts\n           Playwright found no tests in this spec  [31mtestDir\n") ||
		!strings.Contains(text, "           "+strings.Repeat("x", 299)+"…\n") || strings.Count(text, "\n") != 7 {
		t.Fatalf("text summary:\n%s", text)
	}
}

func TestPrintResultNamesEachFailedTestAndItsContext(t *testing.T) {
	errorContext := filepath.Join(t.TempDir(), "test-results", "login", "error-context.md")
	var out bytes.Buffer
	printResult(&out, runner.RunSummary{Outcome: runner.OutcomeFailed, Failed: 1, Complete: true, Receipts: []runner.Receipt{{
		Status: runner.StatusFailed, Spec: "tests/login.spec.ts", FailureCount: 1,
		Failures: []runner.TestFailure{{
			Title: "login › renamed button", Location: "tests/login.spec.ts:9",
			Message:     "TimeoutError: locator.click: Timeout 1500ms exceeded.\nCall log:\n  - waiting for getByRole('button', { name: 'Login' })\n  - second\n  - third",
			Attachments: []runner.TestAttachment{{Name: "error-context", Path: errorContext}},
		}},
	}}})
	want := "  FAILED   tests/login.spec.ts\n" +
		"           ✗ login › renamed button  tests/login.spec.ts:9\n" +
		"             TimeoutError: locator.click: Timeout 1500ms exceeded.\n" +
		"             - waiting for getByRole('button', { name: 'Login' })\n" +
		"             - second\n" +
		"             context: " + displayPath(errorContext) + "\n" +
		"  Attachments are Playwright's own files; the project's next run may delete them. --keep-attachments copies them into the receipt.\n"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("text summary:\n%s", out.String())
	}
}

func TestPrintResultExplainsSkippedInputs(t *testing.T) {
	var out bytes.Buffer
	printResult(&out, runner.RunSummary{Outcome: runner.OutcomeIncomplete, PlannedJobs: 1, Passed: 1, SkippedInputs: 1,
		Skipped:  []runner.Skipped{{Input: "tests/missing.spec.ts", Reason: "no matching files"}},
		Receipts: []runner.Receipt{{Status: runner.StatusPassed, Spec: "tests/ok.spec.ts"}}})
	if !strings.Contains(out.String(), "  SKIP     tests/missing.spec.ts — no matching files\n  INCOMPLETE: 1 input(s) were skipped and not run; a skipped input is never a pass\n") {
		t.Fatalf("text summary:\n%s", out.String())
	}
}

func TestProviderNoneKeepsHealingOfflineWithAnAgentCLIInstalled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell stand-in for an agent CLI")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NINELIVES_PROVIDER", "")
	if provider, err := resolveHealProvider("", "", ""); err != nil || provider == nil || provider.Name() != "claude" {
		t.Fatalf("auto-detection: %v %v", provider, err)
	}
	if provider, err := resolveHealProvider("none", "", ""); err != nil || provider != nil {
		t.Fatalf("--provider none: %v %v", provider, err)
	}
	t.Setenv("NINELIVES_PROVIDER", "none")
	if provider, err := resolveHealProvider("", "", ""); err != nil || provider != nil {
		t.Fatalf("NINELIVES_PROVIDER=none: %v %v", provider, err)
	}
	if provider, err := resolveHealProvider("claude", "", ""); err != nil || provider == nil {
		t.Fatalf("an explicit flag overrides the environment: %v %v", provider, err)
	}
}

func TestPrintPlanShowsTheTestSelection(t *testing.T) {
	var out bytes.Buffer
	printPlan(&out, runner.Plan{RunID: "run-x", Jobs: []runner.Job{{Spec: "tests/a.spec.ts", Adapter: "playwright", Selection: `line 12, --grep "login"`}}}, "text")
	if !strings.Contains(out.String(), "  RUN  tests/a.spec.ts [playwright] (line 12, --grep \"login\")\n") {
		t.Fatalf("plan:\n%s", out.String())
	}
}

func TestReporterListKeepsJSONForEvidence(t *testing.T) {
	if got, err := reporterList("html, list, ./reporters/slack.ts, @acme/reporter"); err != nil || strings.Join(got, ",") != "html,list,./reporters/slack.ts,@acme/reporter" {
		t.Fatalf("got=%v err=%v", got, err)
	}
	for _, bad := range []string{"json", "html,json", "html --grep x", "../reporter.js", ""} {
		got, err := reporterList(bad)
		if bad == "" {
			if err != nil || got != nil {
				t.Fatalf("empty: %v %v", got, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%q accepted as %v", bad, got)
		}
	}
}

func TestUsageNamesEveryMCPTool(t *testing.T) {
	var out bytes.Buffer
	usage(&out)
	for _, tool := range mcpTools {
		if name := tool["name"].(string); !strings.Contains(out.String(), name) {
			t.Errorf("usage omits MCP tool %s", name)
		}
	}
}

func TestAutoDetectSettingKeepsHealingOfflineUnlessAProviderIsNamed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell stand-in for an agent CLI")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NINELIVES_PROVIDER", "")
	t.Setenv("NINELIVES_AUTODETECT_PROVIDER", "off")
	if provider, err := resolveHealProvider("", "", ""); err != nil || provider != nil {
		t.Fatalf("auto-detected with the setting off: %v %v", provider, err)
	}
	if notice := offlineNotice(""); !strings.Contains(notice, "auto-detection is off") {
		t.Fatalf("notice=%q", notice)
	}
	if provider, err := resolveHealProvider("claude", "", ""); err != nil || provider == nil {
		t.Fatalf("named provider: %v %v", provider, err)
	}
	t.Setenv("NINELIVES_AUTODETECT_PROVIDER", "sometimes")
	if _, err := resolveHealProvider("", "", ""); err == nil {
		t.Fatal("invalid setting accepted")
	}
}

func TestProjectNamesAreBounded(t *testing.T) {
	var names projectNames
	for _, bad := range []string{"", "  ", "a\nb", strings.Repeat("x", 129)} {
		if names.Set(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	for i := 0; i < 16; i++ {
		if err := names.Set(fmt.Sprintf("p%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if names.Set("one too many") == nil {
		t.Fatal("17th project accepted")
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"run", "x.spec.ts", "--config", "missing.config.ts"}, &stdout, &stderr); code != exitUsage || !strings.Contains(stderr.String(), "--config must name an existing") {
		t.Fatalf("missing config: %d %q", code, stderr.String())
	}
}
