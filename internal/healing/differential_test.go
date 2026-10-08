package healing

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/contracttest"
)

type pythonResult struct {
	Classified, Extracted, SelectedTier, Code string
	Success                                   bool
	Confidence                                float64
	Metadata                                  map[string]any
}

func actualPython(t *testing.T, version string, requests []Request) []pythonResult {
	t.Helper()
	input, _ := json.Marshal(requests)
	cmd := exec.Command(contracttest.Python(t), "-I", filepath.Join(contracttest.RunnerRoot(), "internal/contracttest/python_oracle.py"), contracttest.UpstreamSource(t, "ninelives-"+version), version)
	cmd.Stdin = bytes.NewReader(input)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("actual source-only Python %s: %v: %s", version, err, output)
	}
	var result struct {
		Results    []pythonResult
		Provenance struct {
			Revision, Version string
			SourceOnly        bool
			Modules           map[string]struct{ Path, SHA256 string }
		}
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("oracle JSON: %v", err)
	}
	pins := map[string]string{"0.1.3": "8a40d8d5c83f27f84384f060aeed74a3ded7ab77", "0.2.1": "568c7a6882441c13cdb9bfe8c0190ca0bf7d8240"}
	if len(result.Results) != len(requests) || !result.Provenance.SourceOnly || result.Provenance.Version != version || result.Provenance.Revision != pins[version] || len(result.Provenance.Modules) < 4 {
		t.Fatalf("incomplete oracle provenance: %+v", result.Provenance)
	}
	for name, m := range result.Provenance.Modules {
		if !strings.HasPrefix(m.Path, "ninelives/") || !strings.HasSuffix(m.Path, ".py") || len(m.SHA256) != 64 {
			t.Fatalf("invalid protected module %s: %+v", name, m)
		}
	}
	return result.Results
}

// Every dimension is compared to the real implementation. An exception must
// name the historical/native pair and its version or safety rationale.
func TestPinnedPythonClassifierAndStrategyCorpus(t *testing.T) {
	type row struct {
		name        string
		r           Request
		differences map[string][2]string
	}
	rows := []row{
		{name: "flow-before-navigation", r: Request{ErrorMessage: "unexpected page; navigation failed"}},
		{name: "navigation-before-assertion", r: Request{ErrorMessage: "navigation failed; assertion failed"}},
		{name: "assertion-before-wait", r: Request{ErrorMessage: "AssertionError; waiting for locator('#old')"}},
		{name: "visibility-before-locator", r: Request{ErrorMessage: "element not visible; locator not found"}},
		{name: "locator-before-timeout", r: Request{ErrorMessage: "TimeoutError waiting for locator('#old')"}},
		{name: "generic-timeout", r: Request{ErrorMessage: "Timeout exceeded"}},
		{name: "syntax", r: Request{ErrorMessage: "SyntaxError: invalid token"}},
		{name: "historical-network-unknown", r: Request{ErrorMessage: "connection refused"}},
		{name: "fallback", r: Request{ErrorMessage: "locator not found", FailedSelector: "button", TestCode: "// FALLBACK:"}},
		{name: "stable-testid", r: Request{ErrorMessage: "locator not found", FailedSelector: `[data-testid="old"]`}},
		{name: "strategy-id-attribute", r: Request{ErrorMessage: "locator not found", FailedSelector: `[id="old"]`, PageSnapshot: `<button id="old-new">x</button>`}},
		{name: "strategy-bare-id", r: Request{ErrorMessage: "locator not found", FailedSelector: "#old", PageSnapshot: `<button id="old-new">x</button>`}},
		{name: "visibility-scroll", r: Request{FailureType: "element_not_visible", ErrorMessage: "element not visible; scroll"}},
		{name: "visibility-no-scroll", r: Request{FailureType: "element_not_visible", ErrorMessage: "element not visible"}},
		{name: "known-network-human", r: Request{FailureType: "network_error"}},
		{name: "waiting-selector-precedence", r: Request{ErrorMessage: "waiting for locator('#first'); Expected to find element: `#second`", StackTrace: `cy.get('#third')`}},
		{name: "escaped-playwright", r: Request{ErrorMessage: `waiting for locator('text=\'Sign In\'')`}},
		{name: "cypress-contains-parser", r: Request{StackTrace: `cy.contains('Save')`}, differences: map[string][2]string{"0.1.3:extracted": {"", "Save"}}},
		{name: "getter", r: Request{StackTrace: `page.getByTestId('save')`}},
		{name: "cypress-parser", r: Request{ErrorMessage: "Expected to find element: `#login`"}, differences: map[string][2]string{"0.1.3:classified": {"unknown", "locator_not_found"}, "0.1.3:extracted": {"", "#login"}}},
		{name: "cypress-find-parser", r: Request{StackTrace: `cy.find('.save')`}, differences: map[string][2]string{"0.1.3:extracted": {"", ".save"}}},
		{name: "selenium-json", r: Request{ErrorMessage: `{"method":"css selector","selector":"#submit"}`}, differences: map[string][2]string{"0.1.3:extracted": {"", "#submit"}}},
		{name: "selenium-by", r: Request{StackTrace: `driver.find_element(By.CSS_SELECTOR, '#submit')`}, differences: map[string][2]string{"0.1.3:extracted": {"", "#submit"}}},
		{name: "stale-version-difference", r: Request{ErrorMessage: "StaleElementReferenceException"}, differences: map[string][2]string{"0.2.1:classified": {"locator_not_found", "unknown"}}},
		{name: "no-such-version-difference", r: Request{ErrorMessage: "NoSuchElementException"}, differences: map[string][2]string{"0.1.3:classified": {"unknown", "locator_not_found"}}},
		{name: "intentional-safety-deviation-assertion-override", r: Request{ErrorMessage: "AssertionError: Expected to find content: `Save`, but never found it."}, differences: map[string][2]string{"0.1.3:extracted": {"", "Save"}, "0.2.1:classified": {"locator_not_found", "assertion_failed"}, "0.2.1:selected": {"tier2_ai_suggest", "tier3_human"}}},
		{name: "intentional-safety-deviation-role", r: Request{StackTrace: `page.getByRole('button', {name:'Save'})`}, differences: map[string][2]string{"0.1.3:extracted": {"button", ""}, "0.2.1:extracted": {"button", ""}}},
		{name: "intentional-safety-deviation-outside-viewport", r: Request{ErrorMessage: "element outside viewport"}, differences: map[string][2]string{"0.1.3:classified": {"unknown", "element_not_visible"}, "0.2.1:classified": {"unknown", "element_not_visible"}, "0.1.3:selected": {"tier2_ai_suggest", "tier1_auto"}, "0.2.1:selected": {"tier2_ai_suggest", "tier1_auto"}}},
	}
	requests := make([]Request, len(rows))
	for i, row := range rows {
		requests[i] = row.r
		requests[i].Framework = "playwright"
	}
	for _, v := range []string{"0.1.3", "0.2.1"} {
		t.Run(v, func(t *testing.T) {
			actual := actualPython(t, v, requests)
			for i, row := range rows {
				t.Run(row.name, func(t *testing.T) {
					nativeClass := Classify(row.r.ErrorMessage, row.r.StackTrace)
					ft := row.r.FailureType
					if ft == "" {
						ft = nativeClass
					}
					pairs := map[string][2]string{"classified": {actual[i].Classified, nativeClass}, "extracted": {actual[i].Extracted, ExtractSelector(row.r.ErrorMessage, row.r.StackTrace)}, "selected": {actual[i].SelectedTier, selectedTier(ft, row.r)}}
					for dimension, pair := range pairs {
						want, exception := row.differences[v+":"+dimension]
						if exception {
							if pair != want {
								t.Errorf("labelled %s difference %s=%q want %q", v, dimension, pair, want)
							}
						} else if pair[0] != pair[1] {
							t.Errorf("common-parity %s %s: Python=%q native=%q", v, dimension, pair[0], pair[1])
						}
					}
				})
			}
		})
	}
}

func TestPinnedPythonDifferential(t *testing.T) {
	type row struct {
		name            string
		r               Request
		deviation       bool
		nativeProposes  bool
		upstreamSuccess map[string]bool
		upstreamCode    string
		anchor          string
	}
	mk := func(sel, code, snapshot string) Request {
		return Request{Version: 1, Framework: "playwright", FailureType: "locator_not_found", ErrorMessage: "locator not found", FailedSelector: sel, TestCode: code, PageSnapshot: snapshot}
	}
	rows := []row{
		{name: "common-parity-moved-id", r: mk("#save", `await page.locator('#save').click();`, `<button id='save-new'>x</button>`), anchor: "id"},
		{name: "common-parity-testid", r: mk(`[data-testid='save']`, `await page.locator("[data-testid='save']").click();`, `<button data-testid='save-new'>x</button>`), anchor: "testid"},
		{name: "intentional-safety-deviation-accessibility-text-casing", r: mk("text='Sign In'", `await page.locator("text='Sign In'").click();`, `- button "Sign in" [ref=e3]`), deviation: true, upstreamSuccess: map[string]bool{"0.1.3": true, "0.2.1": true}, upstreamCode: `await page.locator("text='Sign in'").click();`, anchor: "text"},
		{name: "common-parity-html-text-casing", r: mk("text='Sign In'", `await page.locator("text='Sign In'").click();`, `<button>Sign in</button>`), anchor: "text"},
		{name: "intentional-safety-deviation-text-substring", r: mk("text='save'", `await page.locator("text='save'").click();`, `<button>Save now</button>`), deviation: true, upstreamSuccess: map[string]bool{"0.1.3": true, "0.2.1": true}, upstreamCode: `await page.locator("text='Save'").click();`, anchor: "text"},
		{name: "common-parity-class-casing", r: mk(".save", `await page.locator('.save').click();`, `<button class='Save primary'>x</button>`), anchor: "class"},
		{name: "intentional-safety-deviation-ambiguous-class", r: mk(".save", `await page.locator('.save').click();`, `<div><button class='Save'>One</button><button class='Save'>Two</button></div>`), deviation: true, upstreamSuccess: map[string]bool{"0.1.3": true, "0.2.1": true}, anchor: "class"},
		{name: "common-parity-id-transform", r: mk("#save", `await page.locator('#save').click();`, "")},
		{name: "common-parity-class-transform", r: mk(".save-button", `await page.locator('.save-button').click();`, "")},
		{name: "common-parity-cypress-get", r: mk("#save", `cy.get('#save').click();`, `<button id='save-new'>x</button>`), anchor: "id"},
		{name: "common-parity-cypress-find", r: mk("#save", `cy.find('#save').click();`, `<button id='save-new'>x</button>`), anchor: "id"},
		{name: "common-parity-selenium", r: mk("#save", `driver.find_element(By.CSS_SELECTOR, '#save').click()`, `<button id='save-new'>x</button>`), anchor: "id"},
		{name: "intentional-safety-deviation-aria-live-anchor", r: mk(`[aria-label='Save']`, `page.locator("[aria-label='Save']").click();`, `<button aria-label='Save now'>x</button>`), deviation: true, nativeProposes: true, upstreamSuccess: map[string]bool{"0.1.3": false, "0.2.1": false}, anchor: "aria-label"},
		{name: "intentional-safety-deviation-testid-transform", r: mk(`[data-testid="save"]`, `page.locator('[data-testid="save"]').click();`, ""), deviation: true, nativeProposes: true, upstreamSuccess: map[string]bool{"0.1.3": true, "0.2.1": true}, anchor: "transformation"},
		{name: "intentional-safety-deviation-compound-priority", r: mk(`#save[data-testid="stable"][aria-label="Save"]`, `page.locator('#save[data-testid="stable"][aria-label="Save"]').click();`, `<button id='save-new' data-testid='stable-new' aria-label='Save'>x</button>`), deviation: true, upstreamSuccess: map[string]bool{"0.1.3": true, "0.2.1": true}, anchor: "testid"},
		{name: "intentional-safety-deviation-priority-id-over-aria", r: mk(`#save[aria-label='Save'] >> text='Save' .save`, `page.locator("#save[aria-label='Save'] >> text='Save' .save").click();`, `<button id='save-new' aria-label='Save' class='save'>Save</button>`), deviation: true, upstreamSuccess: map[string]bool{"0.1.3": true, "0.2.1": true}, anchor: "id"},
		{name: "intentional-safety-deviation-priority-aria-over-text", r: mk(`[aria-label='Save'] >> text='Save' .save`, `page.locator("[aria-label='Save'] >> text='Save' .save").click();`, `<button aria-label='Save' class='save'>Save</button>`), deviation: true, upstreamSuccess: map[string]bool{"0.1.3": true, "0.2.1": true}, anchor: "aria-label"},
		{name: "intentional-safety-deviation-priority-text-over-class", r: mk(`text='Save' .save`, `page.locator("text='Save' .save").click();`, `<button class='save'>Save</button>`), deviation: true, upstreamSuccess: map[string]bool{"0.1.3": true, "0.2.1": true}, anchor: "text"},
		{name: "intentional-safety-deviation-transform-priority", r: mk(`.save-button #save`, `page.locator('.save-button #save').click();`, ""), deviation: true, upstreamSuccess: map[string]bool{"0.1.3": true, "0.2.1": true}},
		{name: "intentional-safety-deviation-assertion-syntax", r: mk("#save", `await expect(
 page.locator('#save')
).toBeVisible();`, `<button id='save-new'>x</button>`), deviation: true, upstreamSuccess: map[string]bool{"0.1.3": true, "0.2.1": true}},
		{name: "intentional-safety-deviation-comment", r: mk("#save", "/* docs\npage.locator('#save')\n*/", `<button id='save-new'>x</button>`), deviation: true, upstreamSuccess: map[string]bool{"0.1.3": true, "0.2.1": true}},
		{name: "intentional-safety-deviation-fake-html", r: mk("#save", `page.locator('#save').click();`, `<script>"<button id='save-new'>x</button>"</script>`), deviation: true, upstreamSuccess: map[string]bool{"0.1.3": true, "0.2.1": true}},
		{name: "intentional-safety-deviation-transformation-with-snapshot", r: mk("#save", `page.locator('#save').click();`, `<button id='different'>x</button>`), deviation: true, upstreamSuccess: map[string]bool{"0.1.3": true, "0.2.1": true}},
		{name: "intentional-safety-deviation-missing-literal", r: mk("#save", `page.locator('#other').click();`, `<button id='save-new'>x</button>`), deviation: true, upstreamSuccess: map[string]bool{"0.1.3": true, "0.2.1": true}},
		{name: "intentional-safety-deviation-unchanged", r: mk("button", `page.locator('button').click();`, ""), deviation: true, upstreamSuccess: map[string]bool{"0.1.3": false, "0.2.1": false}},
		{name: "common-parity-timing", r: mk("button", "await page.locator('button').click();", "")},
		{name: "version-difference-cypress-timing", r: mk("button", "cy.get('button').click();", ""), deviation: true, upstreamSuccess: map[string]bool{"0.1.3": true, "0.2.1": false}},
		{name: "intentional-safety-deviation-error-assertion", r: mk("#save", `page.locator('#save').click(); await expect(page.locator('#other')).toBeVisible();`, `<button id='save-new'>x</button>`), deviation: true, upstreamSuccess: map[string]bool{"0.1.3": true, "0.2.1": true}},
	}
	requests := make([]Request, len(rows))
	for i := range rows {
		r := &rows[i]
		if strings.Contains(r.name, "cypress") {
			r.r.Framework = "cypress"
		}
		if strings.Contains(r.name, "selenium") {
			r.r.Framework = "selenium"
		}
		if strings.Contains(r.name, "timing") {
			r.r.ErrorMessage = "timeout exceeded"
			r.r.FailureType = "locator_timeout"
		}
		if strings.Contains(r.name, "error-assertion") {
			r.r.ErrorMessage = "AssertionError: waiting for locator('#save')"
		}
		requests[i] = r.r
	}
	for _, v := range []string{"0.1.3", "0.2.1"} {
		t.Run(v, func(t *testing.T) {
			actual := actualPython(t, v, requests)
			for i, row := range rows {
				t.Run(row.name, func(t *testing.T) {
					original := row.r.TestCode
					native := Heal(row.r)
					upstream := actual[i]
					if row.deviation {
						if upstream.Success != row.upstreamSuccess[v] || (native.Decision == "propose") != row.nativeProposes {
							t.Fatalf("labelled deviation: Python=%+v native=%+v", upstream, native)
						}
						if row.upstreamCode != "" && upstream.Code != row.upstreamCode {
							t.Fatalf("historical proposal bytes changed: got=%q want=%q", upstream.Code, row.upstreamCode)
						}
						if row.anchor != "" && !row.nativeProposes && upstream.Metadata["anchor"] != row.anchor {
							t.Fatalf("historical stability priority lost: %+v", upstream)
						}
					} else {
						if !upstream.Success || native.Decision != "propose" || upstream.Code != native.ProposedCode || upstream.Confidence != native.Confidence {
							t.Fatalf("common-parity: Python=%+v native=%+v", upstream, native)
						}
					}
					if native.Decision == "propose" {
						if !native.RequiresApproval || native.Apply || native.Metadata["provenance"] != "unverified" || native.ProposedCode == original {
							t.Fatalf("proposal governance lost: %+v", native)
						}
						if row.anchor != "" && native.Metadata["anchor"] != row.anchor {
							t.Fatalf("native anchor = %v", native.Metadata["anchor"])
						}
						if old := upstream.Metadata["old_selector"]; old != nil && !row.deviation && old != native.Metadata["oldSelector"] {
							t.Fatalf("old-selector provenance mismatch")
						}
						if next := upstream.Metadata["new_selector"]; next != nil && !row.deviation && next != native.Metadata["newSelector"] {
							t.Fatalf("new-selector provenance mismatch")
						}
					}
					if row.r.TestCode != original {
						t.Fatal("input changed")
					}
				})
			}
		})
	}
}
