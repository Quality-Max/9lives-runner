package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/assessment"
	"github.com/Quality-Max/9lives-runner/internal/runner"
)

func TestAssessWarnsWhenRequestedProvenanceCannotBeCaptured(t *testing.T) {
	// Exercise the bundled source analyzer without consumer npm dependencies.
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	check := exec.Command("node", "--version")
	check.Env = []string{"PATH=" + os.Getenv("PATH")}
	if check.Run() != nil {
		if os.Getenv("NINELIVES_REQUIRE_ASSESSMENT") == "1" {
			t.Fatal("CI requires Node for assessment qualification")
		}
		t.Skip("requires Node")
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

func TestAssessTextWithoutRequirements(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		if os.Getenv("NINELIVES_REQUIRE_ASSESSMENT") == "1" {
			t.Fatal("CI requires Node for assessment qualification")
		}
		t.Skip("requires Node")
	}
	spec := filepath.Join(t.TempDir(), "fixture.spec.ts")
	source := "import {test,expect} from '@playwright/test';\n" +
		"test.skip(isWeekend(), 'business days only');\n" +
		"test('waits \\x1b[31m', async ({page}) => {\n" +
		"  await page.waitForTimeout(500);\n" +
		"  await page.waitForTimeout(500);\n" +
		"});\n" +
		"test('checks', () => { expect(1).toBe(1); });\n"
	if err := os.WriteFile(spec, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	for _, titles := range []bool{false, true} {
		args := []string{spec}
		if titles {
			args = append(args, "--titles")
		}
		var stdout, stderr bytes.Buffer
		if code := assessCommand(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
			t.Fatalf("assessment without requirements failed: code=%d stderr=%q", code, stderr.String())
		}
		text := stdout.String()
		for _, want := range []string{
			"  fixed-wait [demonstrated] at 4:9: ",
			"  conditional-skip [informational] at 2:1: ",
			"Limit: No requirement contract was supplied",
			// The file-level guard applies to both tests and counts once.
			"Summary: 2 tests, 4 findings\n  fixed-wait: 2\n  conditional-skip: 1\n  no-direct-assertion: 1\n",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("text report lacks %q:\n%s", want, text)
			}
		}
		if strings.Contains(text, "requirement=") || strings.Contains(text, "outcome=") || strings.Contains(text, "\x1b") {
			t.Fatalf("empty fields or raw control characters printed:\n%s", text)
		}
		if titled := strings.Contains(text, `test at 3:1 "waits \x1b[31m": `); titled != titles {
			t.Fatalf("title shown=%v with --titles=%v:\n%s", titled, titles, text)
		}
	}
}

func TestAssessRejectsAnEmptyRequirementsPath(t *testing.T) {
	spec := filepath.Join(t.TempDir(), "fixture.spec.ts")
	if err := os.WriteFile(spec, []byte("import {test} from '@playwright/test';\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{spec, "--requirements", ""}, {spec, "--requirements="}} {
		var stdout, stderr bytes.Buffer
		if code := assessCommand(args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "--requirements needs a contract path") {
			t.Fatalf("empty requirements path accepted for %q: code=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
}
