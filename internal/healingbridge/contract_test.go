package healingbridge

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/contracttest"
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

// TestPinnedNineLivesOfflineTierOneCompatibility drives the real Python healer
// offline and maps its actual output through FromUpstream, the production seam.
func TestPinnedNineLivesOfflineTierOneCompatibility(t *testing.T) {
	python := contracttest.Python(t)
	program := `
import asyncio
import json
import sys
from pathlib import Path
source_root = Path(sys.argv[1]).resolve()
sys.path.insert(0, str(source_root))
import ninelives
assert Path(ninelives.__file__).resolve().is_relative_to(source_root)
from ninelives.healing.strategy import FailureType, HealingStrategySelector, TestFailure
from ninelives.healing.tier1 import Tier1LocatorHealer
selector = HealingStrategySelector()
locator = selector.classify_failure("locator not found: #save")
assertion = selector.classify_failure("AssertionError: expected 2 received 3")
failure = TestFailure(failure_type=locator, error_message="locator not found", failed_selector="#save", test_code="await page.locator('#save').click()", page_html='<button id="save-new">Save</button>')
result = asyncio.run(Tier1LocatorHealer().heal(failure))
print(json.dumps({
    "locator_failure_type": locator.value,
    "locator_result": {**result.to_dict(), "healed_code": result.healed_code},
    "assertion_failure_type": assertion.value,
    "assertion_tier": selector.select_strategy(TestFailure(failure_type=assertion, error_message="AssertionError")).value,
}))
`
	for _, checkout := range []string{"ninelives-0.1.3", "ninelives-0.2.1"} {
		t.Run(checkout, func(t *testing.T) {
			source := contracttest.UpstreamSource(t, checkout)
			output, err := exec.Command(python, "-c", program, source).Output()
			if err != nil {
				t.Fatalf("actual %s offline healing fixture failed: %v", checkout, err)
			}
			var actual struct {
				LocatorFailureType   string         `json:"locator_failure_type"`
				LocatorResult        UpstreamResult `json:"locator_result"`
				AssertionFailureType string         `json:"assertion_failure_type"`
				AssertionTier        string         `json:"assertion_tier"`
			}
			if err := json.Unmarshal(output, &actual); err != nil {
				t.Fatalf("%s did not produce a healing result: %v: %s", checkout, err, output)
			}
			if actual.LocatorFailureType != "locator_not_found" || actual.AssertionFailureType != "assertion_failed" || actual.AssertionTier != "tier3_human" {
				t.Fatalf("%s classification changed: %+v", checkout, actual)
			}
			// Upstream Tier 1 would auto-apply; the bridge must still require approval.
			if !actual.LocatorResult.Success || actual.LocatorResult.RequiresApproval || actual.LocatorResult.HealedCode == nil || !strings.Contains(*actual.LocatorResult.HealedCode, "#save-new") {
				t.Fatalf("%s Tier 1 result changed shape; revisit FromUpstream: %+v", checkout, actual.LocatorResult)
			}
			request := Request{Version: Version, Framework: "playwright", AllowAssertionChange: false}
			proposal, err := FromUpstream(request, actual.LocatorFailureType, actual.LocatorResult)
			if err != nil {
				t.Fatalf("%s locator result was not mapped safely: %v", checkout, err)
			}
			if proposal.Decision != "propose" || !proposal.RequiresApproval || proposal.Apply || proposal.ProposedCode != *actual.LocatorResult.HealedCode {
				t.Fatalf("%s proposal lost review semantics: %+v", checkout, proposal)
			}
			// Even a successful upstream repair of an assertion must be refused.
			refusal, err := FromUpstream(request, actual.AssertionFailureType, UpstreamResult{Tier: actual.AssertionTier, Success: true, HealedCode: actual.LocatorResult.HealedCode})
			if err != nil || refusal.Decision != "refuse" || refusal.ProposedCode != "" || refusal.Apply {
				t.Fatalf("%s assertion refusal was not preserved: %+v err=%v", checkout, refusal, err)
			}
		})
	}
}

func TestFromUpstreamRefusesEmptyRepair(t *testing.T) {
	response, err := FromUpstream(Request{Version: Version}, "locator_not_found", UpstreamResult{Tier: "tier1_auto", Success: false})
	if err != nil || response.Decision != "refuse" || response.RequiresApproval {
		t.Fatalf("failed upstream heal must be a refusal: %+v err=%v", response, err)
	}
}

func TestAssertionRefusalCannotRegress(t *testing.T) {
	request := Request{Version: Version, AllowAssertionChange: false}
	response := Response{Version: Version, FailureType: "assertion_failed", Decision: "propose", ProposedCode: "expect(4).toBe(4)", RequiresApproval: true}
	if err := Validate(request, response); err == nil {
		t.Fatal("expected assertion-refusal validation failure")
	}
}
