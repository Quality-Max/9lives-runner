package tier2

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/contracttest"
)

// TestPinnedPythonTier2PromptAndParserContract runs the pinned 0.2.1 source
// offline. Native deliberately adds stricter candidate boundaries and limits
// CHANGES to 1–5 entries; this fixture proves compatibility for its shared
// labels, fence aliases, complete-file parsing, and newline convention.
func TestPinnedPythonTier2PromptAndParserContract(t *testing.T) {
	python := contracttest.Python(t, "pydantic")
	source := contracttest.UpstreamSource(t, "ninelives-0.2.1")
	program := `
import json, sys
sys.path.insert(0, sys.argv[2])
import python_oracle
python_oracle.prepare(sys.argv[1], "0.2.1")
from ninelives.healing.tier2 import Tier2AISuggest
from ninelives.healing.strategy import TestFailure, FailureType
engine = object.__new__(Tier2AISuggest)
original = "test('one', async ({ page }) => { await page.locator('#old').click(); });\n" + "test('two', async ({ page }) => { await expect(page.locator('#keep')).toBeVisible(); });\n"
candidate = original.replace("#old", "#new", 1)
out = {"candidate": candidate, "prompts": {}, "parsed": {}}
for framework in ("playwright", "cypress", "selenium"):
    failure = TestFailure(FailureType.LOCATOR_TIMEOUT, "waiting for locator('#old')", failed_selector="#old", test_code=original, framework=framework, page_html="p" * 3001, console_logs=[str(i) for i in range(11)])
    out["prompts"][framework] = engine._build_prompt(failure)
fence = chr(96) * 3
for alias in ("javascript", "typescript", "python", "js", "ts", "py"):
    out["parsed"][alias] = engine._parse_suggestion("REASONING: x\nCHANGES:\n- selector\nCODE:\n" + fence + alias + "\n" + candidate + fence, original)
out["provenance"] = python_oracle.verify_loaded_identity(sys.argv[1], "0.2.1")
print(json.dumps(out))
`
	helper := filepath.Join(contracttest.RunnerRoot(), "internal", "contracttest")
	cmd := exec.Command(python, "-I", "-c", program, filepath.Clean(source), helper)
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("pinned Python Tier 2 fixture: %v", err)
	}
	var oracle struct {
		Candidate  string            `json:"candidate"`
		Prompts    map[string]string `json:"prompts"`
		Parsed     map[string]string `json:"parsed"`
		Provenance struct {
			SourceOnly bool `json:"sourceOnly"`
		} `json:"provenance"`
	}
	if err := json.Unmarshal(raw, &oracle); err != nil {
		t.Fatal(err)
	}
	if !oracle.Provenance.SourceOnly {
		t.Fatal("pinned Tier 2 oracle did not prove source-only provenance")
	}
	for framework, want := range map[string]string{"playwright": "A Playwright test", "cypress": "A Cypress test", "selenium": "A Selenium (Python + pytest) test"} {
		if !strings.Contains(oracle.Prompts[framework], want) {
			t.Fatalf("pinned prompt missing %q", want)
		}
		if got := Prompt(framework, oracle.Candidate, "waiting for locator('#old')"); !strings.Contains(got, want) || !strings.Contains(got, oracle.Candidate) {
			t.Fatalf("native prompt incompatible for %s", framework)
		}
	}
	for alias, want := range oracle.Parsed {
		got, err := ParseCandidate("REASONING: x\nCHANGES:\n- selector\nCODE:\n```"+alias+"\n"+oracle.Candidate+"```", oracle.Candidate+"// old\n")
		if err != nil || got != want {
			t.Fatalf("alias %s got=%q want=%q err=%v", alias, got, want, err)
		}
	}
}
