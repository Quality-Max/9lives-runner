package playwright

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qualitymax/9lives-runner/internal/runner"
)

func TestPlanUsesInstalledProjectWithoutNpxDownload(t *testing.T) {
	project := t.TempDir()
	write(t, filepath.Join(project, "package.json"), `{"devDependencies":{"@playwright/test":"1.61.1"}}`, 0o600)
	binary := filepath.Join(project, "node_modules", ".bin", "playwright")
	write(t, binary, "#!/bin/sh\n", 0o700)
	spec := filepath.Join(project, "tests", "a.spec.ts")
	write(t, spec, "", 0o600)
	plan, err := runner.BuildPlan([]string{spec}, runner.PlanOptions{Adapters: []runner.Adapter{New()}})
	if err != nil || len(plan.Jobs) != 1 {
		t.Fatalf("unexpected plan: %#v %v", plan, err)
	}
	if plan.Jobs[0].Command[0] != binary || plan.Jobs[0].Command[0] == "npx" {
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
	if err != nil || validation.FailureCount != 2 || validation.ExecutedTests != 3 || validation.AssertionCoverage != "unknown" {
		t.Fatalf("validation=%#v err=%v", validation, err)
	}
	if _, err := New().Validate([]byte(`{}`)); err == nil {
		t.Fatal("schema-less JSON must not validate")
	}
}

func TestValidateRejectsReportsWithoutCompletedTests(t *testing.T) {
	_, err := New().Validate([]byte(`{"stats":{"duration":10.5},"suites":[{"specs":[{"tests":[{"results":[{"status":"skipped"}]}]}]}]}`))
	if err == nil {
		t.Fatal("report containing only skipped results must not validate as an executed test run")
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
