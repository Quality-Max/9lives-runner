package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// junitTotals is what a CI dashboard reads first from a JUnit document.
type junitTotals struct {
	Tests    int `xml:"tests,attr"`
	Failures int `xml:"failures,attr"`
	Errors   int `xml:"errors,attr"`
	Skipped  int `xml:"skipped,attr"`
}

func readJUnit(t *testing.T, path string) (junitTotals, string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no JUnit report: %v", err)
	}
	var totals junitTotals
	if err := xml.Unmarshal(raw, &totals); err != nil {
		t.Fatalf("JUnit report does not parse: %v\n%s", err, raw)
	}
	return totals, string(raw)
}

const junitPassingReport = `{"stats":{"duration":5},"suites":[{"title":"a.spec.ts","specs":[
	{"title":"adds an item","tests":[{"status":"expected","expectedStatus":"passed","results":[{"status":"passed","duration":40}]}]},
	{"title":"pending","tests":[{"status":"skipped","expectedStatus":"skipped","results":[{"status":"skipped","duration":0}]}]}]}]}`

const junitFailingReport = `{"stats":{"duration":5},"suites":[{"title":"a.spec.ts","specs":[
	{"title":"adds an item","tests":[{"status":"expected","expectedStatus":"passed","results":[{"status":"passed","duration":40}]}]},
	{"title":"sums the cart","tests":[{"status":"unexpected","expectedStatus":"passed","results":[{"status":"failed","duration":90,"errors":[{"message":"Error: expect(received).toBe(expected)\n\nExpected: 3\nReceived: 2"}]}]}]}]}]}`

// stubProject is a Playwright project whose local binary is a script that
// writes the given JSON report where 9l asked for it, runs `during`, and
// exits with the given code: the whole CLI path runs, without a browser.
func stubProject(t *testing.T, report string, exit int, during string) (spec string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub test runner is a shell script")
	}
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "package.json"), `{"devDependencies":{"@playwright/test":"1.61.1"}}`)
	writeTestFile(t, filepath.Join(root, "a.spec.ts"), "// stub\n")
	script := "#!/bin/sh\n"
	if report != "" {
		writeTestFile(t, filepath.Join(root, "report.json"), report)
		script += fmt.Sprintf("cat '%s' > \"$PLAYWRIGHT_JSON_OUTPUT_FILE\"\n", filepath.Join(root, "report.json"))
	}
	script += during + "\n" + fmt.Sprintf("exit %d\n", exit)
	binary := filepath.Join(root, "node_modules", ".bin", "playwright")
	writeTestFile(t, binary, script)
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "a.spec.ts")
}

func TestRunWritesJUnitForEachOutcome(t *testing.T) {
	for _, test := range []struct {
		name, report string
		exit, code   int
		want         junitTotals
	}{
		{"passed", junitPassingReport, 0, exitPassed, junitTotals{Tests: 2, Skipped: 1}},
		{"failed", junitFailingReport, 1, exitFailed, junitTotals{Tests: 2, Failures: 1}},
		// The worker exited cleanly and left no report: nothing is proven,
		// and nothing may be shown as passed.
		{"incomplete", "", 0, exitIncomplete, junitTotals{Tests: 1, Errors: 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			spec := stubProject(t, test.report, test.exit, "")
			receipts := t.TempDir()
			// A directory that does not exist yet, and the flag after the spec.
			report := filepath.Join(t.TempDir(), "reports", "junit.xml")
			var stdout, stderr bytes.Buffer
			code := run([]string{"run", spec, "--junit", report, "--receipt-dir", receipts, "--format", "json", "--timeout", "30s"}, &stdout, &stderr)
			if code != test.code {
				t.Fatalf("exit %d, want %d: %s", code, test.code, stderr.String())
			}
			totals, raw := readJUnit(t, report)
			if totals != test.want {
				t.Fatalf("totals %+v, want %+v:\n%s", totals, test.want, raw)
			}
			if info, err := os.Stat(report); err != nil || info.Mode().Perm() != 0o644 {
				t.Fatalf("report must be readable by an uploader: %v %v", info, err)
			}
			if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(report), ".junit-*")); len(leftovers) != 0 {
				t.Fatalf("temporary files left behind: %v", leftovers)
			}
			if test.name == "failed" && (!strings.Contains(raw, `<failure type="assertion"`) || !strings.Contains(raw, "Expected: 3") || !strings.Contains(raw, `name="sums the cart"`)) {
				t.Fatalf("failure detail missing:\n%s", raw)
			}
			// `9l result --junit` rebuilds the same document from the stored run.
			var result runner.RunSummary
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			again := filepath.Join(t.TempDir(), "again.xml")
			stdout.Reset()
			if code := run([]string{"result", result.RunID, "--receipt-dir", receipts, "--junit", again}, &stdout, &stderr); code != 0 {
				t.Fatalf("result exit %d: %s", code, stderr.String())
			}
			if _, rebuilt := readJUnit(t, again); rebuilt != raw {
				t.Fatalf("result --junit differs from run --junit:\n%s\n---\n%s", rebuilt, raw)
			}
		})
	}
}

func TestBadJUnitPathIsAUsageErrorBeforeAnythingRuns(t *testing.T) {
	spec := stubProject(t, junitPassingReport, 0, "touch '"+filepath.Join(t.TempDir(), "never")+"'")
	directory := t.TempDir()
	file := filepath.Join(directory, "file")
	writeTestFile(t, file, "x")
	for name, path := range map[string]string{
		"a directory":         directory,
		"a trailing slash":    filepath.Join(directory, "new") + "/",
		"below a file":        filepath.Join(file, "junit.xml"),
		"deep below a file":   filepath.Join(file, "a", "b", "junit.xml"),
		"empty":               "",
		"blank":               "  ",
		"multiple lines":      "a\nb.xml",
		"an unwritable place": "/dev/null/junit.xml",
	} {
		for _, command := range []string{"run", "plan"} {
			receipts := filepath.Join(t.TempDir(), "receipts")
			var stdout, stderr bytes.Buffer
			code := run([]string{command, spec, "--receipt-dir", receipts, "--junit", path}, &stdout, &stderr)
			if code != exitUsage || stdout.Len() != 0 || !strings.Contains(stderr.String(), "--junit") {
				t.Errorf("%s %s: exit %d, stdout %q, stderr %q", command, name, code, stdout.String(), stderr.String())
			}
			if _, err := os.Stat(receipts); !os.IsNotExist(err) {
				t.Errorf("%s %s: a run was started", command, name)
			}
		}
	}
	// A missing value is a flag error, not a spec named "--junit".
	var stdout, stderr bytes.Buffer
	if code := run([]string{"run", spec, "--junit"}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("missing value: exit %d", code)
	}
}

func TestUnwritableJUnitDirectoryIsRefusedBeforeExecution(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	ran := filepath.Join(t.TempDir(), "ran")
	spec := stubProject(t, junitPassingReport, 0, "touch '"+ran+"'")
	locked := t.TempDir()
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	receipts := filepath.Join(t.TempDir(), "receipts")
	var stdout, stderr bytes.Buffer
	code := run([]string{"run", spec, "--receipt-dir", receipts, "--junit", filepath.Join(locked, "junit.xml")}, &stdout, &stderr)
	if code != exitUsage || !strings.Contains(stderr.String(), "not writable") {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if _, err := os.Stat(ran); !os.IsNotExist(err) {
		t.Fatal("the test process ran although its report could not be written")
	}
}

func TestSetupErrorAndDryRunWriteNoJUnit(t *testing.T) {
	dir := t.TempDir()
	specs := []string{filepath.Join(dir, "a.spec.ts"), filepath.Join(dir, "b.spec.ts")}
	for _, spec := range specs {
		writeTestFile(t, spec, "// setup-only fixture\n")
	}
	// As in TestRunSetupErrorsWriteNoResultOrCancelHint: a creation record
	// with two specs is refused by Execute before any process starts.
	record := runner.AgentProvenance{Version: 1, Agent: "codex", Branch: "fixture", Commit: strings.Repeat("a", 40), Repository: strings.Repeat("b", 64), Source: strings.Repeat("c", 64), SourcePath: strings.Repeat("d", 64)}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(dir, "creation.json")
	writeTestFile(t, snapshot, string(raw))
	report := filepath.Join(t.TempDir(), "junit.xml")
	var stdout, stderr bytes.Buffer
	args := append([]string{"run"}, specs...)
	if code := run(append(args, "--agent-provenance", snapshot, "--receipt-dir", filepath.Join(dir, "receipts"), "--junit", report), &stdout, &stderr); code != exitUsage || !strings.Contains(stderr.String(), "requires one exact spec") {
		t.Fatalf("setup error: exit %d: %s", code, stderr.String())
	}
	if _, err := os.Stat(report); !os.IsNotExist(err) {
		t.Fatal("a setup error wrote a JUnit report")
	}

	spec := stubProject(t, junitPassingReport, 0, "")
	for _, args := range [][]string{{"run", spec, "--dry-run", "--junit", report}, {"plan", spec, "--junit", report}} {
		stdout.Reset()
		if code := run(args, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "RUN") {
			t.Fatalf("%v: exit %d: %s", args, code, stderr.String())
		}
		if _, err := os.Stat(report); !os.IsNotExist(err) {
			t.Fatalf("%v wrote a JUnit report without a run", args)
		}
	}
}

// The report is extra output: failing to write it must be visible, and must
// never make a failed or incomplete run look better than it was.
func TestJUnitWriteFailureNeverLowersTheExitCode(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	for _, test := range []struct {
		name, report string
		exit, want   int
	}{
		{"passed run becomes incomplete", junitPassingReport, 0, exitIncomplete},
		{"failed run stays failed", junitFailingReport, 1, exitFailed},
		{"incomplete run stays incomplete", "", 0, exitIncomplete},
	} {
		t.Run(test.name, func(t *testing.T) {
			reports := t.TempDir()
			// The directory is writable when the run starts and not when it ends.
			spec := stubProject(t, test.report, test.exit, "chmod 500 '"+reports+"'")
			t.Cleanup(func() { _ = os.Chmod(reports, 0o700) })
			var stdout, stderr bytes.Buffer
			code := run([]string{"run", spec, "--receipt-dir", t.TempDir(), "--junit", filepath.Join(reports, "junit.xml"), "--timeout", "30s"}, &stdout, &stderr)
			if code != test.want || !strings.Contains(stderr.String(), "--junit: the report was not written") {
				t.Fatalf("exit %d, want %d: %s", code, test.want, stderr.String())
			}
			// The run's own result is still printed.
			if !strings.Contains(stdout.String(), "a.spec.ts") {
				t.Fatalf("result not printed: %s", stdout.String())
			}
		})
	}
	// And directly, for every code a run can end with.
	unwritable := &junitReport{path: filepath.Join(t.TempDir(), "missing", "junit.xml")}
	for code, want := range map[int]int{exitPassed: exitIncomplete, exitFailed: exitFailed, exitUsage: exitUsage, exitIncomplete: exitIncomplete} {
		var stderr bytes.Buffer
		if got := unwritable.finish(runner.RunSummary{}, code, &stderr); got != want || stderr.Len() == 0 {
			t.Errorf("exit %d became %d, want %d", code, got, want)
		}
	}
	var none *junitReport
	if none.finish(runner.RunSummary{}, exitPassed, &bytes.Buffer{}) != exitPassed {
		t.Fatal("a run without --junit changed its exit code")
	}
}

func TestResultJUnitRefusesBadInputWithoutWriting(t *testing.T) {
	receipts := t.TempDir()
	report := filepath.Join(t.TempDir(), "junit.xml")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"result", "run-unknown", "--receipt-dir", receipts, "--junit", report}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("unknown run: exit %d", code)
	}
	if _, err := os.Stat(report); !os.IsNotExist(err) {
		t.Fatal("a report was written for a run that does not exist")
	}
	if code := run([]string{"result", "run-unknown", "--receipt-dir", receipts, "--junit", t.TempDir()}, &stdout, &stderr); code != exitUsage || !strings.Contains(stderr.String(), "--junit") {
		t.Fatalf("directory path: exit %d: %s", code, stderr.String())
	}
	// Only result has a report to write.
	for _, command := range []string{"status", "cancel"} {
		if code := run([]string{command, "run-unknown", "--receipt-dir", receipts, "--junit", report}, &stdout, &stderr); code != exitUsage {
			t.Fatalf("%s accepted --junit: exit %d", command, code)
		}
	}
}

// A hand-edited saved result cannot make `9l result --junit` show a failed
// test as passed: the malformed list is withheld and the counts decide.
func TestResultJUnitDoesNotTrustAnEditedTestList(t *testing.T) {
	spec := stubProject(t, junitFailingReport, 1, "")
	receipts := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := run([]string{"run", spec, "--receipt-dir", receipts, "--format", "json", "--timeout", "30s"}, &stdout, &stderr); code != exitFailed {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	var result runner.RunSummary
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	saved := filepath.Join(receipts, result.RunID, "result.json")
	raw, err := os.ReadFile(saved)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), `"status": "failed",
          "class": "assertion"`, `"status": "passed"`, 1)
	if edited == string(raw) {
		t.Fatalf("fixture did not match the saved result:\n%s", raw)
	}
	if err := os.WriteFile(saved, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(t.TempDir(), "junit.xml")
	if code := run([]string{"result", result.RunID, "--receipt-dir", receipts, "--junit", report}, &stdout, &stderr); code != 0 {
		t.Fatalf("result exit %d: %s", code, stderr.String())
	}
	if totals, document := readJUnit(t, report); totals.Failures != 1 {
		t.Fatalf("an edited list hid the failure:\n%s", document)
	}
}

// A dashboard keys a test's history on these names, and a report is often
// published: the spec is named from the working directory, not from the
// machine's root.
func TestJUnitNamesSpecsRelativeToTheWorkingDirectory(t *testing.T) {
	project := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(project); err == nil {
		project = resolved
	}
	t.Chdir(project)
	elsewhere := filepath.Join(t.TempDir(), "other", "b.spec.ts")
	summary := runner.RunSummary{Outcome: runner.OutcomePassed, Complete: true, PlannedJobs: 2, Receipts: []runner.Receipt{
		{JobID: "job-001", Attempt: 1, Spec: filepath.Join(project, "tests", "a.spec.ts"), Status: runner.StatusPassed, Executed: true, Validated: true, ExecutedTests: 1},
		{JobID: "job-002", Attempt: 1, Spec: elsewhere, Status: runner.StatusPassed, Executed: true, Validated: true, ExecutedTests: 1},
	}}
	path := filepath.Join(project, "junit.xml")
	if err := (&junitReport{path: path}).write(summary); err != nil {
		t.Fatal(err)
	}
	_, raw := readJUnit(t, path)
	if !strings.Contains(raw, `<testsuite name="tests/a.spec.ts"`) || !strings.Contains(raw, `classname="tests/a.spec.ts"`) || strings.Contains(raw, filepath.ToSlash(project)) {
		t.Fatalf("a spec below the working directory must be named relative to it:\n%s", raw)
	}
	// A spec outside it keeps its full path, and the caller's result is untouched.
	if !strings.Contains(raw, `<testsuite name="`+filepath.ToSlash(elsewhere)+`"`) || summary.Receipts[0].Spec != filepath.Join(project, "tests", "a.spec.ts") {
		t.Fatalf("outside spec or the caller's result changed:\n%s", raw)
	}
}
