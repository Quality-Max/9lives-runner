package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qualitymax/9lives-runner/internal/assessment"
	"github.com/qualitymax/9lives-runner/internal/runner"
)

func TestAssessWarnsWhenRequestedProvenanceCannotBeCaptured(t *testing.T) {
	// Exercise the real source analyzer when local npm dependencies are installed.
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	check := exec.Command("node", "-e", "require.resolve('typescript')")
	check.Env = []string{"PATH=" + os.Getenv("PATH")}
	if check.Run() != nil {
		t.Skip("requires Node and local TypeScript; run npm ci")
	}
	dir := t.TempDir()
	spec := filepath.Join(dir, "fixture.spec.ts")
	contract := filepath.Join(dir, "requirements.json")
	snapshot := filepath.Join(dir, "creation.json")
	write := func(path string, raw []byte) {
		t.Helper()
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(spec, []byte("import {test,expect} from '@playwright/test'; test('fixture', () => { expect(1).toBe(1); });"))
	write(contract, []byte(`{"version":1,"requirements":[{"id":"fixture","reference":"fixture","revision":"1","expectedOutcomes":[{"id":"fixture","description":"Fixture"}]}]}`))
	creation := runner.AgentProvenance{Version: 1, Agent: "codex", Branch: "fixture", Commit: strings.Repeat("b", 40), Repository: strings.Repeat("a", 64), Source: strings.Repeat("c", 64), SourcePath: strings.Repeat("d", 64)}
	raw, err := json.Marshal(creation)
	if err != nil {
		t.Fatal(err)
	}
	write(snapshot, raw)
	// Force Git capture to fail independently of the temporary directory's
	// ancestors. The analyzer never imports or executes this fixture.
	write(filepath.Join(dir, ".git"), []byte("invalid git file\n"))
	for _, attached := range []bool{false, true} {
		var stdout, stderr bytes.Buffer
		args := []string{spec, "--requirements", contract, "--format", "json"}
		if attached {
			args = append(args, "--agent-provenance", snapshot)
		}
		if code := assessCommand(args, &stdout, &stderr); code != 0 {
			t.Fatalf("advisory assessment failed: code=%d", code)
		}
		var report assessment.Report
		if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
			t.Fatal("warning corrupted JSON output")
		}
		if report.AgentProvenance.Status != "unknown" || report.Execution != "not_run" || report.Completeness != "partial" {
			t.Fatal("unavailable provenance promoted to success")
		}
		want := ""
		if attached {
			want = "9l: warning: requested agent provenance could not be captured; provenance remains unknown\n"
		}
		if stderr.String() != want {
			t.Fatalf("unexpected diagnostic: %q", stderr.String())
		}
	}
}
