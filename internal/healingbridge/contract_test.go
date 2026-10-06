package healingbridge

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPythonCompatibilityFixtures(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(source), "testdata", "healing-v1.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []Fixture
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) < 3 {
		t.Fatal("expected representative compatibility fixtures")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			if err := Validate(fixture.Request, fixture.Expected); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPinnedNineLivesOfflineTierOneCompatibility(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	runnerRoot := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	python := os.Getenv("NINELIVES_CONTRACT_PYTHON")
	if python == "" {
		python = "python3"
	}
	program := `
import asyncio
import json
import sys
from pathlib import Path
source_root = Path(sys.argv[1]).resolve()
sys.path.insert(0, str(source_root))
import ninelives
assert Path(ninelives.__file__).resolve().is_relative_to(source_root)
from ninelives.healing.strategy import FailureType, HealingStrategySelector, HealingTier, TestFailure
from ninelives.healing.tier1 import Tier1LocatorHealer
selector = HealingStrategySelector()
assert selector.classify_failure("locator not found: #save") == FailureType.LOCATOR_NOT_FOUND
assert selector.classify_failure("AssertionError: expected 2 received 3") == FailureType.ASSERTION_FAILED
assert selector.select_strategy(TestFailure(failure_type=FailureType.ASSERTION_FAILED, error_message="AssertionError")) == HealingTier.TIER3_HUMAN
failure = TestFailure(failure_type=FailureType.LOCATOR_NOT_FOUND, error_message="locator not found", failed_selector="#save", test_code="await page.locator('#save').click()", page_html='<button id="save-new">Save</button>')
proposal = asyncio.run(Tier1LocatorHealer().heal(failure))
assert proposal.success and proposal.healed_code and "#save-new" in proposal.healed_code
assert not proposal.requires_approval
print(json.dumps({"failure_type": "locator_not_found", "proposal": proposal.healed_code, "assertion_tier": selector.select_strategy(TestFailure(failure_type=FailureType.ASSERTION_FAILED, error_message="AssertionError")).value}))
`
	for _, fixture := range []struct{ name, checkout, localRelative string }{
		{"ninelives-0.1.3", "ninelives-0.1.3", "9lives-python-0.1.3/src"},
		{"ninelives-0.2.1", "ninelives-0.2.1", "9lives-python/src"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			sourceRoot := filepath.Join(runnerRoot, "testdata", "upstream", fixture.checkout, "src")
			if _, err := os.Stat(sourceRoot); err != nil {
				// Developer-only fallback; CI checks out the exact public revisions above.
				platformRoot := filepath.Clean(filepath.Join(runnerRoot, "../.."))
				sourceRoot = filepath.Join(platformRoot, ".context", fixture.localRelative)
				if _, localErr := os.Stat(sourceRoot); localErr != nil {
					t.Fatalf("pinned offline fixture source is required: checkout=%v local=%v", err, localErr)
				}
			}
			command := exec.Command(python, "-c", program, sourceRoot)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("actual %s offline healing fixture failed: %v: %s", fixture.name, err, strings.TrimSpace(string(output)))
			}
			var actual struct {
				FailureType   string `json:"failure_type"`
				Proposal      string `json:"proposal"`
				AssertionTier string `json:"assertion_tier"`
			}
			if err := json.Unmarshal(output, &actual); err != nil {
				t.Fatalf("actual %s fixture did not produce a bridge payload: %v: %s", fixture.name, err, output)
			}
			proposal := Response{Version: Version, FailureType: actual.FailureType, Tier: "tier1_auto", Decision: "propose", ProposedCode: actual.Proposal, Changes: []string{"offline locator proposal"}, RequiresApproval: true, Apply: false}
			if err := Validate(Request{Version: Version}, proposal); err != nil {
				t.Fatalf("%s proposal was not safely mapped to the bridge: %v", fixture.name, err)
			}
			refusal := Response{Version: Version, FailureType: "assertion_failed", Tier: actual.AssertionTier, Decision: "refuse", RequiresApproval: true, Apply: false}
			if err := Validate(Request{Version: Version, AllowAssertionChange: false}, refusal); err != nil {
				t.Fatalf("%s assertion refusal was not preserved: %v", fixture.name, err)
			}
			if actual.AssertionTier != "tier3_human" {
				t.Fatalf("%s assertion tier changed: %s", fixture.name, actual.AssertionTier)
			}
		})
	}
}

func TestAssertionRefusalCannotRegress(t *testing.T) {
	request := Request{Version: Version, AllowAssertionChange: false}
	response := Response{Version: Version, FailureType: "assertion_failed", Decision: "propose", ProposedCode: "expect(4).toBe(4)", RequiresApproval: true}
	if err := Validate(request, response); err == nil {
		t.Fatal("expected assertion-refusal validation failure")
	}
}
