package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/confirm"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// fakeConfirmWorker stands in for Playwright and the SDK. It decides the one
// test's result from app/shop.ts in the checkout it is run in, so a test
// that passes proves each revision ran its own code.
type fakeConfirmWorker struct {
	// outcome maps app/shop.ts content to "passed", "assertion" (an
	// assertion step failed) or "action" (only an action failed).
	outcome  func(app string) string
	workDirs []string
	commands [][]string
}

const confirmTestID = "a000000000000000000000000000000000000000000000000000000000000000"

func shopOutcome(app string) string {
	if strings.Contains(app, "items.length - 1") {
		return "assertion"
	}
	return "passed"
}

func (worker *fakeConfirmWorker) Run(_ context.Context, job runner.Job, _ int) runner.ProcessOutput {
	worker.workDirs = append(worker.workDirs, job.WorkDir)
	worker.commands = append(worker.commands, job.Command)
	app, _ := os.ReadFile(filepath.Join(job.WorkDir, "app", "shop.ts"))
	result := worker.outcome(string(app))
	identity := fmt.Sprintf(`"version":"9l.engine/1","runId":"%s","jobId":"%s","attemptId":"%s"`, job.Env["NINELIVES_RUN_ID"], job.Env["NINELIVES_JOB_ID"], job.Env["NINELIVES_ATTEMPT_ID"])
	category, step, status, outcome, end := "assertion", "passed", "passed", "expected", "passed"
	if result != "passed" {
		step, status, outcome, end = "failed", "failed", "unexpected", "failed"
		if result == "action" {
			category = "action"
		}
	}
	events := []string{
		`"seq":1,"type":"hello","capabilities":["steps","artifact-metadata","terminal-outcomes"],"totalTests":1`,
		fmt.Sprintf(`"seq":2,"type":"test_begin","testId":"%s","retry":0`, confirmTestID),
		fmt.Sprintf(`"seq":3,"type":"step_end","testId":"%s","retry":0,"stepId":"step-1","category":"%s","status":"%s"`, confirmTestID, category, step),
		fmt.Sprintf(`"seq":4,"type":"test_end","testId":"%s","retry":0,"status":"%s","expectedStatus":"passed","artifacts":[]`, confirmTestID, status),
		fmt.Sprintf(`"seq":5,"type":"test_result","testId":"%s","outcome":"%s"`, confirmTestID, outcome),
		`"seq":6,"type":"end","status":"` + end + `"`,
	}
	var stream bytes.Buffer
	for _, event := range events {
		stream.WriteString("{" + identity + "," + event + "}\n")
	}
	_ = os.WriteFile(job.Env["NINELIVES_ENGINE_EVENTS"], stream.Bytes(), 0o600)
	exit := 0
	if result != "passed" {
		exit = 1
	}
	return runner.ProcessOutput{Executed: true, ExitCode: exit}
}

// confirmRepository is a Playwright project in Git with a buggy commit and a
// fixing commit; the reproduction spec is untracked, as a new one would be.
func confirmRepository(t *testing.T) (root, spec string, git func(...string) string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root = t.TempDir()
	git = func(args ...string) string {
		t.Helper()
		output, err := exec.Command("git", append([]string{"-C", root, "-c", "user.name=9lives", "-c", "user.email=9lives@example.invalid", "-c", "commit.gpgsign=false"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	files := map[string]string{
		".gitignore":   "node_modules/\n.9lives/\n",
		"package.json": `{"devDependencies":{"@playwright/test":"1.61.1"}}`,
		"app/shop.ts":  "export const count = (items: unknown[]) => items.length - 1;\n",
	}
	for path, content := range files {
		writeTestFile(t, filepath.Join(root, path), content)
	}
	git("init", "--quiet")
	git("add", ".")
	git("commit", "--quiet", "-m", "buggy")
	writeTestFile(t, filepath.Join(root, "app/shop.ts"), "export const count = (items: unknown[]) => items.length;\n")
	git("commit", "--quiet", "-am", "fix")
	installed := []string{"node_modules/.bin/playwright", "node_modules/@9l/playwright/dist/reporter.js"}
	if runtime.GOOS == "windows" {
		installed = append(installed, "node_modules/.bin/playwright.cmd", "node_modules/@playwright/test/cli.js")
	}
	for _, path := range installed {
		writeTestFile(t, filepath.Join(root, path), "placeholder")
	}
	spec = filepath.Join(root, "tests", "repro.spec.ts")
	writeTestFile(t, spec, "// asserts count([a, b]) === 2\n")
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return resolved, filepath.Join(resolved, "tests", "repro.spec.ts"), git
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runConfirm(t *testing.T, worker *fakeConfirmWorker, args ...string) (int, string, string) {
	t.Helper()
	confirmExecutor = worker
	t.Cleanup(func() { confirmExecutor = nil })
	var stdout, stderr bytes.Buffer
	code := run(append([]string{"confirm"}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func confirmReport(t *testing.T, stdout, stderr string) confirm.Report {
	t.Helper()
	var report confirm.Report
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("%v: stdout=%s stderr=%s", err, stdout, stderr)
	}
	return report
}

func skipWithoutSymlinks(t *testing.T, code int, stderr string) {
	t.Helper()
	if runtime.GOOS == "windows" && code == exitUsage && strings.Contains(stderr, "could not be linked") {
		t.Skip("this Windows account cannot create symbolic links")
	}
}

func TestConfirmRunsEachRevisionInItsOwnCheckout(t *testing.T) {
	root, spec, git := confirmRepository(t)
	receipts := filepath.Join(t.TempDir(), "receipts")
	worker := &fakeConfirmWorker{outcome: shopOutcome}
	code, stdout, stderr := runConfirm(t, worker, spec, "--unfixed", "HEAD~1", "--fixed", "HEAD", "--finding-id", "QUA-14",
		"--finding", "count drops the last item", "--format", "json", "--receipt-dir", receipts)
	skipWithoutSymlinks(t, code, stderr)
	if code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	report := confirmReport(t, stdout, stderr)
	if report.Verdict != confirm.Confirmed || report.Policy != confirm.Policy || report.Spec != "tests/repro.spec.ts" || report.FindingID != "QUA-14" ||
		len(report.FindingSHA256) != 64 || len(report.Tests) != 1 || report.Tests[0].Unfixed != confirm.AssertionFailed || report.Tests[0].Fixed != confirm.Passed {
		t.Fatalf("report %+v", report)
	}
	if report.Unfixed.Commit != git("rev-parse", "HEAD~1") || report.Fixed.Commit != git("rev-parse", "HEAD") || report.Unfixed.Spec != confirm.SpecAbsent || report.Fixed.Spec != confirm.SpecAbsent || report.Unfixed.Status != "failed" || report.Fixed.Status != "passed" {
		t.Fatalf("revisions %+v %+v", report.Unfixed, report.Fixed)
	}
	// Two runs, each in a checkout of its own, neither in the working tree.
	if len(worker.workDirs) != 2 || worker.workDirs[0] == worker.workDirs[1] || worker.workDirs[0] == root || worker.workDirs[1] == root {
		t.Fatalf("work dirs %v (root %s)", worker.workDirs, root)
	}
	for _, command := range worker.commands {
		if command[len(command)-1] != "--retries=0" {
			t.Fatalf("confirm run allows Playwright retries: %q", command)
		}
	}
	for _, dir := range worker.workDirs {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("checkout %s left behind", dir)
		}
	}
	if list := git("worktree", "list", "--porcelain"); strings.Count(list, "worktree ") != 1 {
		t.Fatalf("worktree registration left behind:\n%s", list)
	}
	saved, err := os.ReadFile(filepath.Join(receipts, "confirmations", report.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(saved, []byte("drops the last item")) {
		t.Fatal("the saved report contains the finding text")
	}
	for _, runID := range []string{report.Unfixed.RunID, report.Fixed.RunID} {
		if _, err := os.Stat(filepath.Join(receipts, runID)); err != nil {
			t.Fatalf("receipt for run %s: %v", runID, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "node_modules", ".bin", "playwright")); err != nil {
		t.Fatalf("cleanup removed the installed dependencies: %v", err)
	}
}

func TestConfirmUsesTheWorkingTreeAsTheFixedRevision(t *testing.T) {
	root, spec, _ := confirmRepository(t)
	worker := &fakeConfirmWorker{outcome: shopOutcome}
	code, stdout, stderr := runConfirm(t, worker, spec, "--unfixed", "HEAD~1", "--format", "json", "--receipt-dir", t.TempDir())
	skipWithoutSymlinks(t, code, stderr)
	report := confirmReport(t, stdout, stderr)
	if code != 0 || report.Verdict != confirm.Confirmed || report.Fixed.Ref != "working-tree" || report.Fixed.Dirty || report.Fixed.Spec != confirm.SpecSame || len(worker.workDirs) != 2 || worker.workDirs[1] != root {
		t.Fatalf("code=%d report=%+v dirs=%v stderr=%s", code, report, worker.workDirs, stderr)
	}
	// An uncommitted change that does not fix anything: both revisions fail.
	writeTestFile(t, filepath.Join(root, "app", "shop.ts"), "export const count = (items: unknown[]) => items.length - 1; // tidy\n")
	worker = &fakeConfirmWorker{outcome: shopOutcome}
	code, stdout, stderr = runConfirm(t, worker, spec, "--unfixed", "HEAD~1", "--format", "json", "--receipt-dir", t.TempDir())
	report = confirmReport(t, stdout, stderr)
	if code != 1 || report.Verdict != confirm.FixIneffective || !report.Fixed.Dirty {
		t.Fatalf("ineffective fix: code=%d report=%+v", code, report)
	}
	// Both revisions pass: the spec does not reproduce anything.
	writeTestFile(t, filepath.Join(root, "app", "shop.ts"), "export const count = (items: unknown[]) => items.length; // tidy\n")
	worker = &fakeConfirmWorker{outcome: shopOutcome}
	code, stdout, stderr = runConfirm(t, worker, spec, "--unfixed", "HEAD", "--receipt-dir", t.TempDir())
	if code != 1 || !strings.Contains(stdout, "Verdict: not-reproduced") || !strings.Contains(stdout, "application runs separately") {
		t.Fatalf("not reproduced: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
}

func TestConfirmIsInconclusiveWhenAFailureHasNoAssertion(t *testing.T) {
	_, spec, _ := confirmRepository(t)
	// The unfixed revision times out on an action instead of failing an assertion.
	worker := &fakeConfirmWorker{outcome: func(app string) string {
		if strings.Contains(app, "- 1") {
			return "action"
		}
		return "passed"
	}}
	code, stdout, stderr := runConfirm(t, worker, spec, "--unfixed", "HEAD~1", "--fixed", "HEAD", "--receipt-dir", t.TempDir())
	skipWithoutSymlinks(t, code, stderr)
	if code != exitIncomplete || !strings.Contains(stdout, "Verdict: inconclusive (failed-without-assertion)") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
}

func TestConfirmRejectsInvalidUsage(t *testing.T) {
	root, spec, _ := confirmRepository(t)
	outside := filepath.Join(t.TempDir(), "outside.spec.ts")
	writeTestFile(t, outside, "")
	cases := map[string][]string{
		"no spec":                {"--unfixed", "HEAD~1"},
		"two specs":              {spec, spec, "--unfixed", "HEAD~1"},
		"no unfixed revision":    {spec},
		"unknown revision":       {spec, "--unfixed", "no-such-branch"},
		"option-like revision":   {spec, "--unfixed=--output=x"},
		"same commit":            {spec, "--unfixed", "HEAD", "--fixed", "HEAD"},
		"no change since HEAD":   {spec, "--unfixed", "HEAD"},
		"spec outside the repo":  {outside, "--unfixed", "HEAD~1"},
		"missing spec":           {filepath.Join(root, "tests", "missing.spec.ts"), "--unfixed", "HEAD~1"},
		"invalid finding id":     {spec, "--unfixed", "HEAD~1", "--finding-id", "has spaces"},
		"oversized finding text": {spec, "--unfixed", "HEAD~1", "--finding", strings.Repeat("x", maxFindingBytes+1)},
		"bad format":             {spec, "--unfixed", "HEAD~1", "--format", "xml"},
		"bad limits":             {spec, "--unfixed", "HEAD~1", "--workers", "0"},
	}
	for name, args := range cases {
		worker := &fakeConfirmWorker{outcome: shopOutcome}
		if code, _, stderr := runConfirm(t, worker, append(args, "--receipt-dir", t.TempDir())...); code != exitUsage || len(worker.workDirs) != 0 {
			t.Errorf("%s: code=%d runs=%d stderr=%s", name, code, len(worker.workDirs), stderr)
		}
	}
}
