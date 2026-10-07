package healing

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/qualitymax/9lives-runner/internal/contracttest"
)

func TestFrozenReviewSemanticRegressions(t *testing.T) {
	base := Request{Version: 1, Framework: "playwright", FailureType: "locator_not_found", FailedSelector: "#old", TestCode: "await page.locator('#old').click();", PageSnapshot: "<button id='old-new'>x</button>"}
	for name, code := range map[string]string{
		"multiline-comment":   "/* docs\nawait page.locator('#old').click();\n*/",
		"multiline-template":  "const doc = `docs\nawait page.locator('#old').click();\n`;",
		"multiline-assertion": "await expect(\n page.locator('#old')\n).toBeVisible();",
	} {
		t.Run(name, func(t *testing.T) {
			r := base
			r.TestCode = code
			if got := Heal(r); got.Decision != "refuse" {
				t.Fatalf("frozen unsafe context proposed: %+v", got)
			}
		})
	}
	t.Run("python-docstring", func(t *testing.T) {
		r := base
		r.Framework = "selenium"
		r.TestCode = "\"\"\"docs\ndriver.find_element(By.CSS_SELECTOR, '#old').click()\n\"\"\""
		if got := Heal(r); got.Decision != "refuse" {
			t.Fatalf("docstring proposed: %+v", got)
		}
	})
	for name, code := range map[string]string{
		"unbraced-if":      "if (shouldClick)\n  await page.locator('button').click();\nnext();",
		"unbraced-if-else": "if (shouldClick)\n  await page.locator('button').click();\nelse\n next();",
		"arrow-expression": "async function test() {\n const fn = async () =>\n  await page.locator('button').click();\n return fn();\n}",
	} {
		t.Run(name, func(t *testing.T) {
			r := base
			r.FailureType = "locator_timeout"
			r.ErrorMessage = "timeout exceeded"
			r.FailedSelector = "button"
			r.PageSnapshot = ""
			r.TestCode = code
			if got := Heal(r); got.Decision != "refuse" {
				t.Fatalf("unsafe wait proposed: %+v", got)
			}
		})
	}
	for name, snapshot := range map[string]string{
		"script":       "<script>const template = \"<button id='old-new'>x</button>\";</script>",
		"duplicate-id": "<button id='other' id='old-new'>x</button>",
	} {
		t.Run(name, func(t *testing.T) {
			r := base
			r.PageSnapshot = snapshot
			if got := Heal(r); got.Decision != "refuse" {
				t.Fatalf("phantom HTML anchor proposed: %+v", got)
			}
		})
	}
	t.Run("testid-positive", func(t *testing.T) {
		r := base
		r.FailedSelector = `[data-testid="old"]`
		r.TestCode = `page.locator('[data-testid="old"]').click()`
		r.PageSnapshot = `<button data-testid="old-new">x</button>`
		if got := Heal(r); got.Decision != "propose" || got.Metadata["anchor"] != "testid" {
			t.Fatalf("required attribute path absent: %+v", got)
		}
	})
}

func TestSupportedSourceContextsAndIndependentSyntax(t *testing.T) {
	cases := []struct{ name, framework, selector, code, snapshot string }{
		{"playwright-async-separate-assertion", "playwright", "#old", "test('save', async ({ page }) => {\n  // #old belongs to this action\n  await page.locator('#old').click();\n  await expect(page.locator('#status')).toHaveText('Saved');\n});", "<button id='old-new'>Save</button>"},
		{"playwright-escaped-runtime-selector", "playwright", "text='Sign In'", "await page.locator('text=\\'Sign In\\'').click();", "<button>Sign in</button>"},
		{"cypress-static-template", "cypress", "#old", "cy.get(`#old`).click();", "<button id='old-new'>x</button>"},
		{"cypress-find", "cypress", "#old", "cy.find('#old').click();", "<button id='old-new'>x</button>"},
		{"selenium-real-code-with-docstring", "selenium", "#old", "def test_save(driver):\n    \"\"\"Docs\n    driver.find_element(By.CSS_SELECTOR, '#old').click()\n    \"\"\"\n    # driver.find_element(By.CSS_SELECTOR, '#old')\n    driver.find_element(By.CSS_SELECTOR, '#old').click()\n    assert driver.title == 'Saved'\n", "<button id='old-new'>x</button>"},
		{"canonical-aria-with-space", "playwright", `[aria-label="Save"]`, `page.locator('[aria-label="Save"]').click();`, "<button aria-label='Save now'>x</button>"},
		{"regex-character-class", "playwright", "#old", "const pattern = /[a/][\\]]/;\nawait page.locator('#old').click();", "<button id='old-new'>x</button>"},
		{"ordinary-comparison", "playwright", "#old", "const limit = 3; if (count < limit) {\n await page.locator('#old').click();\n}", "<button id='old-new'>x</button>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Request{Version: 1, Framework: c.framework, FailureType: "locator_not_found", ErrorMessage: "locator not found", FailedSelector: c.selector, TestCode: c.code, PageSnapshot: c.snapshot}
			got := Heal(r)
			if got.Decision != "propose" {
				t.Fatalf("supported executable path refused: %+v", got)
			}
			if c.framework == "selenium" {
				cmd := exec.Command(contracttest.Python(t), "-I", "-c", "import sys; compile(sys.stdin.read(), '<proposal>', 'exec')")
				cmd.Stdin = strings.NewReader(got.ProposedCode)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("complete Python proposal invalid: %v: %s", err, out)
				}
			} else {
				checkJavaScript(t, got.ProposedCode)
			}
			if c.name == "playwright-async-separate-assertion" && !strings.Contains(got.ProposedCode, "await expect(page.locator('#status')).toHaveText('Saved');") {
				t.Fatal("separate assertion changed")
			}
			if c.name == "selenium-real-code-with-docstring" && !strings.Contains(got.ProposedCode, "    driver.find_element(By.CSS_SELECTOR, '#old').click()\n    \"\"\"") {
				t.Fatal("docstring changed")
			}
		})
	}
}

func checkJavaScript(t *testing.T, code string) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		contracttest.Unavailable(t, "Node syntax/runtime validator unavailable")
	}
	cmd := exec.Command("node", "--input-type=module", "--check")
	cmd.Stdin = strings.NewReader(code)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("complete JavaScript proposal invalid: %v: %s", err, out)
	}
}

func checkPythonCode(t *testing.T, code string) {
	t.Helper()
	cmd := exec.Command(contracttest.Python(t), "-I", "-c", "import sys; compile(sys.stdin.read(), '<proposal>', 'exec')")
	cmd.Stdin = strings.NewReader(code)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("complete Python source invalid: %v: %s", err, out)
	}
}

func TestTimingPreservesControlledBodyAndCallbackScope(t *testing.T) {
	code := "async function run(shouldClick) {\n  const callback = async () => {\n    if (shouldClick) {\n      await page.locator('button').click();\n    } else {\n      trace.push('else');\n    }\n    trace.push('callback');\n  };\n  trace.push('before');\n  await callback();\n  trace.push('after');\n}\n"
	r := Request{Version: 1, Framework: "playwright", FailureType: "locator_timeout", ErrorMessage: "timeout exceeded", FailedSelector: "button", TestCode: code}
	got := Heal(r)
	if got.Decision != "propose" || got.Metadata["anchor"] != "timing" {
		t.Fatalf("supported async braced control refused: %+v", got)
	}
	checkJavaScript(t, got.ProposedCode)
	prelude := "let trace = []; const page = { locator: () => ({ waitFor: async () => trace.push('wait'), click: async () => trace.push('click') }) };\n"
	suffix := "await run(false); const noClick = trace; trace = []; await run(true); console.log(JSON.stringify([noClick, trace]));"
	cmd := exec.Command("node", "--input-type=module")
	cmd.Stdin = strings.NewReader(prelude + got.ProposedCode + suffix)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("runtime validation: %v: %s", err, out)
	}
	if strings.TrimSpace(string(out)) != `[["before","else","callback","after"],["before","wait","click","callback","after"]]` {
		t.Fatalf("wait changed control flow or callback scope: %s", out)
	}
}

func TestAdditionalOpaqueAndAssertionContextsRefuse(t *testing.T) {
	for name, code := range map[string]string{
		"nested-template":   "const docs = `outer ${`inner ${page.locator('#old')}`}`;\npage.locator('#old').click();",
		"regex-doc":         "const docs = /[a/](page.locator('#old'))/;",
		"expect-soft":       "await expect.soft(\n page.locator('#old')\n).toBeVisible();",
		"same-target-error": "await page.locator('#old').click();\nawait expect(page.locator('#other')).toBeVisible();",
	} {
		t.Run(name, func(t *testing.T) {
			r := Request{Version: 1, Framework: "playwright", FailureType: "locator_not_found", ErrorMessage: "locator not found", FailedSelector: "#old", TestCode: code, PageSnapshot: "<button id='old-new'>x</button>"}
			if name == "same-target-error" {
				r.ErrorMessage = "AssertionError: waiting for locator('#old')"
			}
			if got := Heal(r); got.Decision != "refuse" {
				t.Fatalf("unsafe context proposed: %+v", got)
			}
		})
	}
	for _, code := range []string{"assert \\\n driver.find_element(By.CSS_SELECTOR, '#old')", "self.assertEqual(\n driver.find_element(By.CSS_SELECTOR, '#old'), expected)"} {
		r := Request{Version: 1, Framework: "selenium", FailureType: "locator_not_found", FailedSelector: "#old", TestCode: code, PageSnapshot: "<button id='old-new'>x</button>"}
		if got := Heal(r); got.Decision != "refuse" {
			t.Fatalf("Python assertion proposed: %+v", got)
		}
	}
}

func TestFinalUnsupportedLexicalContextsRefuse(t *testing.T) {
	for name, code := range map[string]string{
		"assert-method":                `assert.equal(page.locator('#old'), expected);`,
		"assert-nested-method":         `assert.strict.equal(page.locator('#old'), expected);`,
		"expect-computed-method":       `expect['soft'](page.locator('#old')).toBeVisible();`,
		"escaped-assertion-identifier": `\u0065xpect(page.locator('#old')).toBeVisible();`,
		"jsx-text":                     `const doc = <div>page.locator('#old')<span /></div>;`,
	} {
		t.Run(name, func(t *testing.T) {
			r := Request{Version: 1, Framework: "playwright", FailureType: "locator_not_found", ErrorMessage: "locator not found", FailedSelector: "#old", TestCode: code, PageSnapshot: "<button id='old-new'>x</button>"}
			if got := Heal(r); got.Decision != "refuse" {
				t.Fatalf("unsupported context proposed: %+v", got)
			}
		})
	}
}

func TestSemanticFinalReviewV2Regressions(t *testing.T) {
	cases := []struct{ name, framework, code, snapshot, decision string }{
		{"parenthesized-expect", "playwright", "await (expect)(page.locator('#old')).toBeVisible();", "<button id='old-new'>x</button>", "refuse"},
		{"optional-expect", "playwright", "await expect?.(page.locator('#old')).toBeVisible();", "<button id='old-new'>x</button>", "refuse"},
		{"optional-expect-member", "playwright", "await expect?.soft(page.locator('#old')).toBeVisible();", "<button id='old-new'>x</button>", "refuse"},
		{"python-comparison-assert", "selenium", "assert expected == (\n    driver.find_element(By.CSS_SELECTOR, '#old')\n)", "<button id='old-new'>x</button>", "refuse"},
		{"python-logical-assert", "selenium", "assert ready and (\n    driver.find_element(By.CSS_SELECTOR, '#old')\n)", "<button id='old-new'>x</button>", "refuse"},
		{"unicode-malformed-rawtext", "playwright", "await page.locator('#old').click();", "<p>İİ</p><script>x", "refuse"},
		{"unicode-live-anchor", "playwright", "await page.locator('#old').click();", "<script>İ</script><button id=\"old-new\">x</button>", "propose"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("bounded snapshot panicked: %v", p)
				}
			}()
			if c.framework == "selenium" {
				checkPythonCode(t, c.code)
			} else {
				checkJavaScript(t, c.code)
			}
			r := Request{Version: 1, Framework: c.framework, FailureType: "locator_not_found", ErrorMessage: "locator not found", FailedSelector: "#old", TestCode: c.code, PageSnapshot: c.snapshot}
			got := Heal(r)
			if got.Decision != c.decision {
				t.Fatalf("decision=%s, want %s; code=%q reason=%q", got.Decision, c.decision, got.ProposedCode, got.Reason)
			}
			if c.decision == "propose" {
				checkJavaScript(t, got.ProposedCode)
				if got.Metadata["newSelector"] != "#old-new" {
					t.Fatalf("live anchor lost: %+v", got)
				}
			} else if got.ProposedCode != "" {
				t.Fatal("refusal contains source mutation")
			}
		})
	}
}

func TestAssertionExpressionOwnershipBoundaries(t *testing.T) {
	cases := []struct{ name, framework, code string }{
		{"nested-group-optional-computed", "playwright", "await ((expect))?.['soft']?.(page.locator('#old')).toBeVisible();"},
		{"grouped-optional-member", "playwright", "await (expect?.soft)?.(page.locator('#old')).toBeVisible();"},
		{"grouped-computed-assert", "playwright", "(assert.strict?.['equal'])?.(page.locator('#old'), expected);"},
		{"computed-assertion-member", "playwright", "await tools?.['expect']?.(page.locator('#old')).toBeVisible();"},
		{"grouped-computed-assertion-member", "playwright", "await (tools?.['expect'])?.(page.locator('#old')).toBeVisible();"},
		{"conditional-callee", "playwright", "await (ready ? expect : fallback)(page.locator('#old')).toBeVisible();"},
		{"comma-callee", "playwright", "await (fallback, expect)(page.locator('#old')).toBeVisible();"},
		{"bound-callee", "playwright", "await expect.bind(null)(page.locator('#old')).toBeVisible();"},
		{"assertion-alias", "playwright", "const check = expect; await check(page.locator('#old')).toBeVisible();"},
		{"unknown-optional-computed-callee", "playwright", "await unknown?.[method]?.(page.locator('#old'));"},
		{"unknown-object-argument", "playwright", "unknown({value: page.locator('#old')});"},
		{"python-message-operand", "selenium", "assert ready, (\n    driver.find_element(By.CSS_SELECTOR, '#old')\n)"},
		{"python-comment-logical-operand", "selenium", "assert ready and ( # preceding condition\n    # locator belongs to this assertion\n    driver.find_element(By.CSS_SELECTOR, '#old')\n), 'message'"},
		{"python-nested-operand", "selenium", "assert any([\n    (ready or driver.find_element(By.CSS_SELECTOR, '#old'))\n]), 'message'"},
		{"python-semicolon-assert", "selenium", "ready = True; assert expected == (\n    driver.find_element(By.CSS_SELECTOR, '#old')\n)"},
		{"python-inline-suite-assert", "selenium", "if ready: assert expected == (\n    driver.find_element(By.CSS_SELECTOR, '#old')\n)"},
		{"python-alias-argument", "selenium", "check = self.assertEqual\ncheck(driver.find_element(By.CSS_SELECTOR, '#old'), expected)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.framework == "selenium" {
				checkPythonCode(t, c.code)
			} else {
				checkJavaScript(t, c.code)
			}
			r := Request{Version: 1, Framework: c.framework, FailureType: "locator_not_found", ErrorMessage: "locator not found", FailedSelector: "#old", TestCode: c.code, PageSnapshot: "<button id='old-new'>x</button>"}
			if got := Heal(r); got.Decision != "refuse" || got.ProposedCode != "" {
				t.Fatalf("unproved/assertion argument context changed: %+v", got)
			}
		})
	}
}

func TestSeparateStatementOwnershipAndUnicodeBoundaries(t *testing.T) {
	cases := []struct{ name, framework, code, snapshot, decision string }{
		{"python-semicolon-action-after-assert", "selenium", "assert ready, 'message'; driver.find_element(By.CSS_SELECTOR, '#old').click()", "<button id='old-new'>x</button>", "propose"},
		{"python-action-before-semicolon-assert", "selenium", "driver.find_element(By.CSS_SELECTOR, '#old').click(); assert ready and visible, 'message'", "<button id='old-new'>x</button>", "propose"},
		{"python-action-after-logical-assert", "selenium", "assert (\n    ready and visible # comment ; is not a boundary\n), 'message'\n# separate statement\ndriver.find_element(By.CSS_SELECTOR, '#old').click()", "<button id='old-new'>x</button>", "propose"},
		{"python-action-before-unrelated-logical-assert", "selenium", "driver.find_element(By.CSS_SELECTOR, '#old').click()\nassert ready and (\n    visible\n), 'message'", "<button id='old-new'>x</button>", "propose"},
		{"js-separate-grouped-optional-assert", "playwright", "test('save', async ({page}) => {\n await (expect)?.(page.locator('#status')).toBeVisible();\n await page.locator('#old').click();\n});", "<button id='old-new'>x</button>", "propose"},
		{"js-ordinary-function-callback", "playwright", "test('save', async function testSave() {\n await page.locator('#old').click();\n await expect(page.locator('#status')).toBeVisible();\n});", "<button id='old-new'>x</button>", "propose"},
		{"unicode-mixed-case-markup", "playwright", "await page.locator('#old').click();", "<p>İİ Straße Σ</p><ScRiPt>İ <button id='old-fake'>x</button></sCrIpT><BUTTON ID='old-new'>x</BUTTON>", "propose"},
		{"unicode-unclosed-rawtext-after-anchor", "playwright", "await page.locator('#old').click();", "<button id='old-new'>x</button><script>İ>", "refuse"},
		{"rawtext-start-at-eof", "playwright", "await page.locator('#old').click();", "<button id='old-new'>x</button><script>", "refuse"},
		{"rawtext-unclosed-endtag-quote", "playwright", "await page.locator('#old').click();", "<button id='old-new'>x</button><script>İ</script ignored='>", "refuse"},
		{"rawtext-endtag-name-prefix", "playwright", "await page.locator('#old').click();", "<script>İ</scripture><button id='old-fake'>x</button></SCRIPT><button id='old-new'>x</button>", "propose"},
		{"rawtext-unclosed-endtag-name-prefix", "playwright", "await page.locator('#old').click();", "<button id='old-new'>x</button><script>İ</scripture>", "refuse"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Request{Version: 1, Framework: c.framework, FailureType: "locator_not_found", ErrorMessage: "locator not found", FailedSelector: "#old", TestCode: c.code, PageSnapshot: c.snapshot}
			got := Heal(r)
			if got.Decision != c.decision {
				t.Fatalf("decision=%s want %s: %+v", got.Decision, c.decision, got)
			}
			if got.Decision == "propose" {
				if got.ProposedCode != strings.Replace(c.code, "'#old'", "'#old-new'", 1) || !got.RequiresApproval || got.Apply || got.Metadata["provenance"] != "unverified" {
					t.Fatalf("proposal changed surrounding source/governance: %+v", got)
				}
				if c.framework == "selenium" {
					checkPythonCode(t, got.ProposedCode)
				} else {
					checkJavaScript(t, got.ProposedCode)
				}
			}
		})
	}
}

func TestScriptDoubleEscapedDOMRegressions(t *testing.T) {
	for _, snapshot := range []string{
		`<script><!--<script></script><button id="old-new">x</button></script>`,
		`<script><!--<script></script><button id="old-new">x</button>--></script>`,
	} {
		t.Run(snapshot, func(t *testing.T) {
			in := Request{Version: 1, Framework: "playwright", FailureType: "locator_not_found", ErrorMessage: "locator not found", FailedSelector: "#old", TestCode: "await page.locator('#old').click();"}
			in.PageSnapshot = snapshot
			result := Heal(in)
			if result.Decision != "refuse" || result.ProposedCode != "" {
				t.Fatalf("phantom DOM anchor proposed: %+v", result)
			}
			if text := htmlVisibleText(snapshot); strings.Contains(text, "x") {
				t.Fatalf("script text leaked into live text: %q", text)
			}
			in.FailedSelector = "text='x'"
			in.TestCode = `await page.locator("text='x'").click();`
			if got := Heal(in); got.Decision != "refuse" || got.ProposedCode != "" {
				t.Fatalf("phantom script text proposed: %+v", got)
			}
		})
	}
}

func TestDOMSnapshotTreeAndTextControls(t *testing.T) {
	// These synthetic fixtures were independently checked in Chrome 154 with
	// script execution disabled. The Go suite needs no installed browser.
	cases := []struct {
		name, snapshot, decision, text string
	}{
		{"ordinary-unicode", `<script>İ🙂</script><button id="old-new">x</button>`, "propose", "x"},
		{"escaped-script-live-after", `<script><!--<script></script>--></script><button id="old-new">x</button>`, "propose", "x"},
		{"double-escaped-live-after", `<script><!--<script></script><button id="old-fake">fake</button></script><button id="old-new">x</button>`, "propose", "x"},
		{"double-escaped-comment-exit-live-after", `<script><!--<script></script><button id="old-fake">fake</button>--></script><button id="old-new">x</button>`, "propose", "x"},
		{"template-followed-by-live", `<template><button id="old-fake">fake</button></template><button id="old-new">x</button>`, "propose", "x"},
		{"noscript-followed-by-live", `<noscript><button id="old-fake">fake</button></noscript><button id="old-new">x</button>`, "propose", "x"},
		{"textarea-followed-by-live", `<textarea><button id="old-fake">fake</button></textarea><button id="old-new">x</button>`, "propose", "x"},
		{"style-followed-by-live", `<style><button id="old-fake">fake</button></style><button id="old-new">x</button>`, "propose", "x"},
		{"select-live-option", `<select><option id="old-new">x</option></select>`, "propose", "x"},
		{"select-modern-button", `<select><button id="old-new">x</button></select>`, "propose", "x"},
		{"select-modern-table", `<select><table id="old-new">x</table></select>`, "propose", "x"},
		{"table-foster-parented-button", `<table><button id="old-new">x</button></table>`, "propose", "x"},
		{"table-live-cell", `<table><tr><td id="old-new">x</td></tr></table>`, "propose", "x"},
		{"frameset-discards-button", `<frameset><button id="old-new">x</button></frameset>`, "refuse", ""},
		{"duplicate-attributes-first", `<button ID="old-new" id="old-fake">x</button>`, "propose", "x"},
		{"foreign-snapshot", `<svg><text>fake</text></svg><button id="old-new">x</button>`, "refuse", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := Request{Version: 1, Framework: "playwright", FailureType: "locator_not_found", ErrorMessage: "locator not found", FailedSelector: "#old", TestCode: "await page.locator('#old').click();", PageSnapshot: c.snapshot}
			got := Heal(in)
			if got.Decision != c.decision {
				t.Fatalf("decision=%s want=%s; code=%q reason=%q", got.Decision, c.decision, got.ProposedCode, got.Reason)
			}
			if c.decision == "propose" {
				if got.Metadata["newSelector"] != "#old-new" {
					t.Fatalf("live DOM anchor lost: %+v", got)
				}
				checkJavaScript(t, got.ProposedCode)
			}
			if text := htmlVisibleText(c.snapshot); text != c.text {
				t.Fatalf("eligible DOM text=%q want=%q", text, c.text)
			}
			in.FailedSelector = "text='fake'"
			in.TestCode = `await page.locator("text='fake'").click();`
			if got := Heal(in); got.Decision != "refuse" || got.ProposedCode != "" {
				t.Fatalf("inert/unsupported DOM text supplied an anchor: %+v", got)
			}
		})
	}
	unicode := Request{Version: 1, Framework: "playwright", FailureType: "locator_not_found", ErrorMessage: "locator not found", FailedSelector: `[data-testid="old"]`, TestCode: `await page.locator('[data-testid="old"]').click();`, PageSnapshot: `<script>İ🙂</script><button data-testid="old-İ&#128578;">x</button>`}
	got := Heal(unicode)
	if got.Decision != "propose" || got.Metadata["newSelector"] != "[data-testid='old-İ🙂']" {
		t.Fatalf("decoded Unicode attribute identity lost: %+v", got)
	}
	checkJavaScript(t, got.ProposedCode)
}

func TestDOMSnapshotMalformedAndDepthRefusals(t *testing.T) {
	anchor := `<button id="old-new">x</button>`
	for name, snapshot := range map[string]string{
		"double-escaped-unclosed-after-anchor": anchor + `<script><!--<script></script>`,
		"ordinary-rawtext-unclosed":            anchor + `<style>fake`,
		"ordinary-tag-unclosed":                anchor + `<div`,
		"attribute-quote-unclosed":             anchor + `<div title="fake>`,
		"comment-unclosed":                     anchor + `<!-- fake`,
		"template-unclosed":                    anchor + `<template>fake`,
		"bogus-declaration":                    anchor + `<!fake <button>>`,
		"invalid-unquoted-attribute":           anchor + "<div title=fake`>",
		"nul-byte":                             anchor + "\x00",
		"parser-depth-limit":                   strings.Repeat("<div>", 513) + anchor + strings.Repeat("</div>", 513),
		"snapshot-size-limit":                  strings.Repeat("x", 1<<20) + anchor,
	} {
		t.Run(name, func(t *testing.T) {
			in := Request{Version: 1, Framework: "playwright", FailureType: "locator_not_found", ErrorMessage: "locator not found", FailedSelector: "#old", TestCode: "await page.locator('#old').click();", PageSnapshot: snapshot}
			if got := Heal(in); got.Decision != "refuse" || got.ProposedCode != "" {
				t.Fatalf("malformed/unsupported snapshot retained an earlier anchor: %+v", got)
			}
			if attrs, text := snapshotTokens(snapshot); len(attrs) != 0 || text != "" {
				t.Fatalf("malformed/unsupported snapshot exposed attributes/text: %v, %q", attrs, text)
			}
		})
	}
}

func TestExactTextReviewRegressions(t *testing.T) {
	for name, snapshot := range map[string]string{
		"substring-is-not-exact":                  "<button>Save now</button>",
		"adjacent-elements-are-not-one-text-node": "<div>Sa</div><div>Ve</div>",
	} {
		t.Run(name, func(t *testing.T) {
			in := Request{Version: 1, Framework: "playwright", FailureType: "locator_not_found", ErrorMessage: "locator not found", FailedSelector: "text='save'", TestCode: `await page.locator("text='save'").click();`, PageSnapshot: snapshot}
			if got := Heal(in); got.Decision != "refuse" || got.ProposedCode != "" {
				t.Fatalf("nonexistent exact text anchor proposed: %+v", got)
			}
		})
	}
}

func TestExactTextElementProofControls(t *testing.T) {
	// Browser evidence checks every proposed selector has exactly one match.
	// Refusals include both nonexistent/ambiguous anchors and a deliberately
	// unsupported subset (nonleaf direct text, input values and shadow trees).
	cases := []struct{ name, snapshot, old, next string }{
		{"whole-button", `<button>Save</button>`, "save", "Save"},
		{"unrelated-siblings", `<div>Sa</div><div>Ve</div><button>Save</button>`, "save", "Save"},
		{"whitespace", "<button>  Sign\n\t in  </button>", "Sign In", "Sign in"},
		{"nbsp-entity", `<button>Sign&nbsp;in</button>`, "Sign In", "Sign in"},
		{"comments-do-not-split", `<button>Sa<!-- note -->ve</button>`, "save", "Save"},
		{"removed-format-characters", `<button>Sa&#8203;&#173;ve</button>`, "save", "Save"},
		{"nested-leaf", `<button><span>Save</span></button>`, "save", "Save"},
		{"unicode-entity", `<button>&#201;cole</button>`, "école", "École"},
		{"script-skipped", `<script>Save</script><button>Save</button>`, "save", "Save"},
		{"double-escaped-script-skipped", `<script><!--<script></script><button>Save</button>--></script><button>Save</button>`, "save", "Save"},
		{"style-skipped", `<style>Save</style><button>Save</button>`, "save", "Save"},
		{"template-skipped", `<template><button>Save</button></template><button>Save</button>`, "save", "Save"},
		{"head-skipped", `<head><title>Save</title></head><body><button>Save</button></body>`, "save", "Save"},
		{"substring", `<button>Save now</button>`, "save", ""},
		{"sibling-concatenation", `<div>Sa</div><div>Ve</div>`, "save", ""},
		{"duplicate-text", `<button>Save</button><div>Save</div>`, "save", ""},
		{"case-variant-competitor", `<button>Save</button><div>SAVE</div>`, "save", ""},
		{"nonleaf-direct-run", `<button>Save<span> now</span></button>`, "save", ""},
		{"split-child-text", `<button><span>Sa</span><span>Ve</span></button>`, "save", ""},
		{"parent-child-competitors", `<div>Save<span>Save</span></div>`, "save", ""},
		{"input-button-only", `<input type='button' value='Save'>`, "save", ""},
		{"input-submit-competitor", `<input type='submit' value='Save'><button>Save</button>`, "save", ""},
		{"duplicate-input-attribute-first", `<input TYPE='submit' type='text' value='Save'><button>Save</button>`, "save", ""},
		{"textarea-only", `<textarea>Save</textarea>`, "save", ""},
		{"textarea-competitor", `<textarea>Save</textarea><button>Save</button>`, "save", ""},
		{"head-only", `<head><title>Save</title></head>`, "save", ""},
		{"noscript-setting-unsupported", `<noscript><button>Save</button></noscript><button>Save</button>`, "save", ""},
		{"accessibility-snapshot", `- button "Save" [ref=e3]`, "save", ""},
		{"nel-is-not-js-whitespace", "<button>Sign\u0085in</button>", "Sign In", ""},
		{"shadow-tree-unsupported", `<div><template shadowrootmode='open'><button>Save</button></template></div><button>Save</button>`, "save", ""},
		{"escaped-text-unsupported", `<button>Save</button>`, `sa\ve`, ""},
		{"malformed-after-text", `<button>Save</button><script>unclosed`, "save", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			selector := "text='" + c.old + "'"
			code := `await page.locator("` + selector + `").click();`
			in := Request{Version: 1, Framework: "playwright", FailureType: "locator_not_found", ErrorMessage: "locator not found", FailedSelector: selector, TestCode: code, PageSnapshot: c.snapshot}
			got := Heal(in)
			if c.next == "" {
				if got.Decision != "refuse" || got.ProposedCode != "" {
					t.Fatalf("unsupported or ambiguous exact text proposed: %+v", got)
				}
			} else {
				next := "text='" + c.next + "'"
				if got.Decision != "propose" || got.Metadata["newSelector"] != next || got.Metadata["anchor"] != "text" || got.ProposedCode != strings.Replace(code, selector, next, 1) || !got.RequiresApproval || got.Apply || got.Metadata["provenance"] != "unverified" {
					t.Fatalf("exact text proof or proposal governance lost: %+v", got)
				}
				checkJavaScript(t, got.ProposedCode)
			}
			if in.TestCode != code || in.PageSnapshot != c.snapshot {
				t.Fatal("input changed")
			}
		})
	}
	for _, framework := range []string{"cypress", "selenium"} {
		t.Run(framework+"-text-selector-unsupported", func(t *testing.T) {
			in := Request{Version: 1, Framework: framework, FailureType: "locator_not_found", ErrorMessage: "locator not found", FailedSelector: "text='save'", TestCode: `cy.get("text='save'").click();`, PageSnapshot: `<button>Save</button>`}
			if framework == "selenium" {
				in.TestCode = `driver.find_element(By.CSS_SELECTOR, "text='save'").click()`
			}
			if got := Heal(in); got.Decision != "refuse" || got.ProposedCode != "" {
				t.Fatalf("Playwright text selector sent to CSS framework: %+v", got)
			}
		})
	}
}

func TestExactTextTimingCannotBypassProof(t *testing.T) {
	for name, snapshot := range map[string]string{
		"substring":        `<button>Save now</button>`,
		"ambiguous":        `<button>Save</button><div>Save</div>`,
		"missing-snapshot": "",
	} {
		t.Run(name, func(t *testing.T) {
			in := Request{Version: 1, Framework: "playwright", FailureType: "locator_timeout", ErrorMessage: "waiting for locator timeout exceeded", FailedSelector: "text='save'", TestCode: `await page.locator("text='save'").click();`, PageSnapshot: snapshot}
			if got := Heal(in); got.Decision != "refuse" || got.ProposedCode != "" {
				t.Fatalf("timing bypassed exact text proof: %+v", got)
			}
		})
	}
	t.Run("supported-exact-match", func(t *testing.T) {
		code := `await page.locator("text='save'").click();`
		in := Request{Version: 1, Framework: "playwright", FailureType: "locator_timeout", ErrorMessage: "waiting for locator timeout exceeded", FailedSelector: "text='save'", TestCode: code, PageSnapshot: `<button>save</button>`}
		got := Heal(in)
		expected := `await page.locator("text='save'").waitFor({ state: 'visible', timeout: 10000 });` + "\n" + code
		if got.Decision != "propose" || got.ProposedCode != expected || got.Metadata["anchor"] != "timing" || !got.RequiresApproval || got.Apply || got.Metadata["provenance"] != "unverified" {
			t.Fatalf("supported exact text timing lost: %+v", got)
		}
		checkJavaScript(t, got.ProposedCode)
		if in.TestCode != code {
			t.Fatal("input changed")
		}
	})
	for _, selector := range []string{`text='sa\ve'`, `text='save' >> button`, `text=save`} {
		t.Run("unsupported-"+selector, func(t *testing.T) {
			in := Request{Version: 1, Framework: "playwright", FailureType: "locator_timeout", ErrorMessage: "waiting for locator timeout exceeded", FailedSelector: selector, TestCode: `await page.locator("` + selector + `").click();`, PageSnapshot: `<button>save</button>`}
			if got := Heal(in); got.Decision != "refuse" || got.ProposedCode != "" {
				t.Fatalf("unsupported text syntax bypassed exact proof: %+v", got)
			}
		})
	}
}
