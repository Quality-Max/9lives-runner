package playwright

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/runner"
)

func TestPlanUsesInstalledProjectWithoutNpxDownload(t *testing.T) {
	project := t.TempDir()
	write(t, filepath.Join(project, "package.json"), `{"devDependencies":{"@playwright/test":"1.61.1"}}`, 0o600)
	binary := filepath.Join(project, "node_modules", ".bin", "playwright")
	if runtime.GOOS == "windows" {
		binary += ".cmd"
		write(t, filepath.Join(project, "node_modules", "@playwright", "test", "cli.js"), "placeholder", 0o600)
	}
	write(t, binary, "#!/bin/sh\n", 0o700)
	spec := filepath.Join(project, "tests", "a.spec.ts")
	write(t, spec, "", 0o600)
	plan, err := runner.BuildPlan([]string{spec}, runner.PlanOptions{Adapters: []runner.Adapter{New()}})
	if err != nil || len(plan.Jobs) != 1 {
		t.Fatalf("unexpected plan: %#v %v", plan, err)
	}
	command, commandErr := InstalledCommand(binary, "test", TestFileFilter(project, filepath.Join("tests", "a.spec.ts")), "--reporter=json")
	if commandErr != nil || !slices.Equal(plan.Jobs[0].Command, command) {
		t.Fatalf("unexpected command: %#v", plan.Jobs[0].Command)
	}
}

func TestPlanRefusesToDownloadMissingPlaywright(t *testing.T) {
	project := t.TempDir()
	write(t, filepath.Join(project, "package.json"), `{"devDependencies":{"@playwright/test":"1.61.1"}}`, 0o600)
	spec := filepath.Join(project, "a.spec.ts")
	write(t, spec, "", 0o600)
	plan, err := runner.BuildPlan([]string{spec}, runner.PlanOptions{Adapters: []runner.Adapter{New()}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Jobs) != 0 || len(plan.Skipped) != 1 || !strings.Contains(plan.Skipped[0].Reason, "local binary") {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

func TestValidateReportAndSchema(t *testing.T) {
	raw := []byte(`{"stats":{"duration":10.5},"suites":[{"specs":[{"tests":[{"results":[{"status":"passed"},{"status":"failed"}]}]}],"suites":[{"specs":[{"tests":[{"results":[{"status":"timedOut"}]}]}]}]}]}`)
	validation, err := New().Validate(raw)
	// Legacy reports without a per-test status take Playwright's precedence
	// over every attempt: a pass with a failure is flaky, a lone timeout fails.
	if err != nil || validation.FailureCount != 1 || validation.ExecutedTests != 2 || validation.AssertionCoverage != "unknown" {
		t.Fatalf("validation=%#v err=%v", validation, err)
	}
	if _, err := New().Validate([]byte(`{}`)); err == nil {
		t.Fatal("schema-less JSON must not validate")
	}
}

func TestFailureContextIsStructuredAndBounded(t *testing.T) {
	if got := FailureContext([]byte(`{"suites":[{"specs":[{"tests":[{"results":[{"errors":[{"message":"waiting for locator('#old')\n  12 | await expect(page.locator('#result')).toBeVisible();"}]}]}]}]}]}`)); got != "waiting for locator('#old')" {
		t.Fatalf("context=%q", got)
	}
	if got := FailureContext([]byte("not-json")); got != "" {
		t.Fatalf("invalid report context=%q", got)
	}
}

func TestValidateRejectsReportsWithoutCompletedTests(t *testing.T) {
	_, err := New().Validate([]byte(`{"stats":{"duration":10.5},"suites":[{"specs":[{"tests":[{"results":[{"status":"skipped"}]}]}]}]}`))
	if err == nil || strings.Contains(err.Error(), "config") {
		t.Fatalf("report containing only skipped results must not validate as an executed test run: %v", err)
	}
	// Playwright 1.61.1's report for a spec its config does not select.
	_, err = New().Validate([]byte(`{"config":{},"suites":[],"errors":[{"message":"Error: No tests found."}],"stats":{"duration":5.8,"expected":0,"skipped":0,"unexpected":0,"flaky":0}}`))
	if err == nil || !strings.Contains(err.Error(), "found no tests in this spec") || !strings.Contains(err.Error(), "testDir") {
		t.Fatalf("a spec the config does not select must name the config as the likely cause: %v", err)
	}
	// A spec that throws while loading is not blamed on the config.
	_, err = New().Validate([]byte(`{"config":{},"suites":[],"errors":[{"message":"Error: import boom"},{"message":"Error: No tests found."}],"stats":{"duration":5.8}}`))
	if err == nil || !strings.Contains(err.Error(), "fails to load") || strings.Contains(err.Error(), "testDir") || strings.Contains(err.Error(), "boom") {
		t.Fatalf("a load error must be reported without its message and without the config hint: %v", err)
	}
}

func TestValidateRejectsExecutedOutcomesWithoutCompletedAttempts(t *testing.T) {
	for _, outcome := range []string{"expected", "flaky", "unexpected"} {
		for _, results := range []string{"", `"results":[]`, `"results":[{"status":"skipped"}]`} {
			t.Run(outcome+"/"+results, func(t *testing.T) {
				separator := ""
				if results != "" {
					separator = ","
				}
				raw := []byte(`{"stats":{"duration":1},"suites":[{"specs":[{"tests":[{"status":"` + outcome + `"` + separator + results + `}]}]}]}`)
				if validation, err := New().Validate(raw); err == nil {
					t.Fatalf("accepted %s without a completed attempt: %#v", outcome, validation)
				}
			})
		}
	}
}

func TestValidateCountsTestsNotRetryAttempts(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "retries-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	validation, err := New().Validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	// A flaky test that passed on retry and a test.fail() test that failed as
	// expected are both green outcomes; the run exited 0.
	if validation.ExecutedTests != 2 || validation.FailureCount != 0 || validation.SkippedTests != 1 || !strings.Contains(validation.Description, "1 flaky") {
		t.Fatalf("retry attempts were counted as tests: %#v", validation)
	}
}

// Mixed statuses across retries keep Playwright's outcome precedence, with or
// without the per-test status field: a skipped or interrupted retry never
// hides an earlier failure, and an unknown status is never guessed.
func TestValidateMixedRetryStatusesKeepPlaywrightPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, status, results string
		executed, failures    int
		flaky, invalid        bool
	}{
		{name: "failed then passed", results: `"failed","passed"`, executed: 1, flaky: true},
		{name: "failed then skipped", results: `"failed","skipped"`, executed: 1, failures: 1},
		{name: "timed out then interrupted", results: `"timedOut","interrupted"`, executed: 1, failures: 1},
		{name: "passed then interrupted", results: `"passed","interrupted"`, executed: 1},
		{name: "interrupted only", results: `"interrupted"`, invalid: true},
		{name: "unknown status", results: `"failed","mystery"`, invalid: true},
		{name: "reported flaky", status: "flaky", results: `"failed","passed"`, executed: 1, flaky: true},
		{name: "reported unexpected after interruption", status: "unexpected", results: `"timedOut","interrupted"`, executed: 1, failures: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			results := []string{}
			for _, status := range strings.Split(tc.results, ",") {
				results = append(results, `{"status":`+status+`}`)
			}
			test := `"results":[` + strings.Join(results, ",") + `]`
			if tc.status != "" {
				test = `"status":"` + tc.status + `",` + test
			}
			// A second, passing test keeps the report executable, so a
			// misclassified test shows up in the counts rather than as an error.
			raw := []byte(`{"stats":{"duration":1},"suites":[{"specs":[{"tests":[{` + test + `}]},{"tests":[{"status":"expected","results":[{"status":"passed"}]}]}]}]}`)
			validation, err := New().Validate(raw)
			if tc.invalid {
				if err == nil && validation.SkippedTests == 0 {
					t.Fatalf("accepted an unclassifiable test: %#v", validation)
				}
				return
			}
			if err != nil || validation.ExecutedTests != tc.executed+1 || validation.FailureCount != tc.failures || strings.Contains(validation.Description, "flaky") != tc.flaky {
				t.Fatalf("validation=%#v err=%v", validation, err)
			}
		})
	}
}

func TestValidateCountsUnexpectedOutcomeAsFailure(t *testing.T) {
	raw := []byte(`{"stats":{"duration":1},"suites":[{"specs":[{"tests":[{"status":"unexpected","results":[{"status":"failed"},{"status":"failed"}]},{"status":"expected","results":[{"status":"passed"}]}]}]}]}`)
	validation, err := New().Validate(raw)
	if err != nil || validation.ExecutedTests != 2 || validation.FailureCount != 1 {
		t.Fatalf("validation=%#v err=%v", validation, err)
	}
	if _, err := New().Validate([]byte(`{"stats":{"duration":1},"suites":[{"specs":[{"tests":[{"status":"expected","results":[{"status":"passed"}]},{"status":"mystery","results":[]}]}]}]}`)); err == nil {
		t.Fatal("unknown test outcome must not validate")
	}
}

func write(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}

// A config or dependency that prints to stdout (dotenv 17 does by default)
// must not invalidate the run: the JSON report goes to the evidence file.
func TestConfigStdoutDoesNotCorruptTheReport(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Playwright binary is a POSIX shell script")
	}
	project := t.TempDir()
	write(t, filepath.Join(project, "package.json"), `{"devDependencies":{"@playwright/test":"1.61.1"}}`, 0o600)
	report := `{"stats":{"duration":1},"suites":[{"specs":[{"tests":[{"results":[{"status":"passed"}]}]}]}]}`
	write(t, filepath.Join(project, "node_modules", ".bin", "playwright"),
		"#!/bin/sh\necho '[dotenv@17.2.3] injecting env (0) from .env'\nprintf '%s' '"+report+"' > \"$PLAYWRIGHT_JSON_OUTPUT_FILE\"\n", 0o700)
	spec := filepath.Join(project, "tests", "a.spec.ts")
	write(t, spec, "", 0o600)
	plan, err := runner.BuildPlan([]string{spec}, runner.PlanOptions{Adapters: []runner.Adapter{New()}})
	if err != nil {
		t.Fatal(err)
	}
	summary, _ := runner.Execute(context.Background(), plan, runner.ExecuteOptions{ReceiptDir: t.TempDir(), Adapters: []runner.Adapter{New()}})
	if receipt := summary.Receipts[0]; receipt.Status != runner.StatusPassed || !receipt.Validated {
		t.Fatalf("config stdout invalidated the run: %+v", receipt)
	}
}

func TestPlanSelectsALineAndPassesGrepThrough(t *testing.T) {
	project := t.TempDir()
	write(t, filepath.Join(project, "package.json"), `{"devDependencies":{"@playwright/test":"1.61.1"}}`, 0o600)
	binary := filepath.Join(project, "node_modules", ".bin", "playwright")
	if runtime.GOOS == "windows" {
		binary += ".cmd"
		write(t, filepath.Join(project, "node_modules", "@playwright", "test", "cli.js"), "placeholder", 0o600)
	}
	write(t, binary, "#!/bin/sh\n", 0o700)
	spec := filepath.Join(project, "tests", "a.spec.ts")
	write(t, spec, "", 0o600)
	plan, err := runner.BuildPlan([]string{spec + ":12", spec + ":30", spec + ":12", filepath.Join(project, "tests", "missing.spec.ts") + ":3"}, runner.PlanOptions{Adapters: []runner.Adapter{New()}, ExtraArgs: []string{"--grep", "login"}, Selection: `--grep "login"`})
	if err != nil || len(plan.Jobs) != 2 || len(plan.Skipped) != 1 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	filter := TestFileFilter(project, filepath.Join("tests", "a.spec.ts"))
	job := plan.Jobs[0]
	if !slices.Contains(job.Command, filter+":12") || slices.Contains(job.Command, filter) || !slices.Equal(job.Command[len(job.Command)-2:], []string{"--grep", "login"}) || job.Selection != `line 12, --grep "login"` {
		t.Fatalf("job=%+v", job)
	}
	if !slices.Contains(plan.Jobs[1].Command, filter+":30") {
		t.Fatalf("second line job=%+v", plan.Jobs[1])
	}
	plan, err = runner.BuildPlan([]string{spec}, runner.PlanOptions{Adapters: []runner.Adapter{New()}, Reporters: []string{"html"}})
	if err != nil || len(plan.Jobs) != 1 || !slices.Contains(plan.Jobs[0].Command, "--reporter=json,html") {
		t.Fatalf("reporters not added: %+v %v", plan, err)
	}
}

func TestPlanFindsWorkspaceHoistedPlaywright(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "package.json"), `{"private":true,"workspaces":["packages/*"]}`, 0o600)
	binary := filepath.Join(root, "node_modules", ".bin", "playwright")
	if runtime.GOOS == "windows" {
		binary += ".cmd"
		write(t, filepath.Join(root, "node_modules", "@playwright", "test", "cli.js"), "placeholder", 0o600)
	}
	write(t, binary, "#!/bin/sh\n", 0o700)
	app := filepath.Join(root, "packages", "app")
	write(t, filepath.Join(app, "package.json"), `{"devDependencies":{"@playwright/test":"1.61.1"}}`, 0o600)
	spec := filepath.Join(app, "tests", "a.spec.ts")
	write(t, spec, "", 0o600)
	plan, err := runner.BuildPlan([]string{spec}, runner.PlanOptions{Adapters: []runner.Adapter{New()}})
	if err != nil || len(plan.Jobs) != 1 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	if job := plan.Jobs[0]; job.WorkDir != app || (runtime.GOOS != "windows" && job.Command[0] != binary) {
		t.Fatalf("job=%+v", job)
	}
}
