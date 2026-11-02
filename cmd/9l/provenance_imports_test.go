package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/assessment"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

// `9l provenance` records the spec's imported helper, `9l assess` names the
// helper after it is edited, and an import that cannot be resolved keeps
// provenance unknown with the reason on stderr.
func TestProvenanceCommandCoversImportedHelpers(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root) // the bundled source analyzer runs from the repository
	check := exec.Command("node", "--version")
	check.Env = []string{"PATH=" + os.Getenv("PATH")}
	if check.Run() != nil {
		if os.Getenv("NINELIVES_REQUIRE_ASSESSMENT") == "1" {
			t.Fatal("CI requires Node for assessment qualification")
		}
		t.Skip("requires Node")
	}
	repo := t.TempDir()
	emptyConfig := filepath.Join(t.TempDir(), "gitconfig")
	write := func(name, contents string) {
		t.Helper()
		path := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(emptyConfig, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "fixture"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "Fixture"}} {
		git := exec.Command("git", append([]string{"-C", repo}, args...)...)
		git.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + emptyConfig}
		if output, err := git.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v %s", args[0], err, output)
		}
	}
	write("e2e/wide.spec.mjs", "import { test, expect } from '@playwright/test';\nimport { visible } from './support.mjs';\ntest('wide', async () => { expect(await visible()).toBe(true); });\n")
	write("e2e/support.mjs", "export const visible = async () => true;\n")
	spec := filepath.Join(repo, "e2e/wide.spec.mjs")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"provenance", spec, "--agent", "fixture-agent"}, &stdout, &stderr); code != 0 {
		t.Fatalf("provenance: code=%d stderr=%s", code, stderr.String())
	}
	var record runner.AgentProvenance
	if err := json.Unmarshal(stdout.Bytes(), &record); err != nil || record.Version != 2 || len(record.Dependencies) != 1 || record.Dependencies[0].Path != "e2e/support.mjs" {
		t.Fatalf("record does not cover the helper: %s", stdout.String())
	}
	snapshot := filepath.Join(t.TempDir(), "creation.json")
	if err := os.WriteFile(snapshot, stdout.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	assess := func() (assessment.Report, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := run([]string{"assess", spec, "--agent-provenance", snapshot, "--format", "json"}, &stdout, &stderr); code != 0 {
			t.Fatalf("assess: code=%d stderr=%s", code, stderr.String())
		}
		var report assessment.Report
		if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		return report, stderr.String()
	}
	if report, _ := assess(); report.AgentProvenance.Status != "matched" || report.AgentProvenance.Scope != runner.ProvenanceScopeImports {
		t.Fatalf("unchanged helper: %+v", report.AgentProvenance)
	}
	write("e2e/support.mjs", "export const visible = async () => false;\n")
	report, _ := assess()
	if p := report.AgentProvenance; p.Status != "mismatch" || p.Source != "matched" || len(p.DependencyChanges) != 1 || p.DependencyChanges[0] != (runner.ProvenanceChange{Path: "e2e/support.mjs", Kind: "import", Change: "modified"}) {
		t.Fatalf("edited helper not named: %+v", p)
	}
	write("e2e/support.mjs", "export const visible = async () => (await import(process.env.DRIVER)).visible();\n")
	report, warning := assess()
	if report.AgentProvenance.Status != "unknown" || warning != "9l: warning: requested agent provenance could not be captured (local import graph: import or require with a non-literal specifier at line 1 in e2e/support.mjs); provenance remains unknown\n" {
		t.Fatalf("unresolvable helper: %+v %q", report.AgentProvenance, warning)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"provenance", spec, "--agent", "fixture-agent"}, &stdout, &stderr); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "non-literal specifier") {
		t.Fatalf("unresolvable capture: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	// A version 1 snapshot of the same checkout is compared spec-only, so the
	// import that version 2 cannot follow does not make it unknown.
	v1, err := runner.CaptureAgentProvenanceFor(context.Background(), spec, runner.AgentProvenance{Version: 1, Agent: "fixture-agent"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(v1)
	if err := os.WriteFile(snapshot, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	report, warning = assess()
	if p := report.AgentProvenance; p.Status != "matched" || p.Scope != runner.ProvenanceScopeSpec || p.Dependencies != "" || warning != "" {
		t.Fatalf("version 1 snapshot: %+v %q", p, warning)
	}
}
