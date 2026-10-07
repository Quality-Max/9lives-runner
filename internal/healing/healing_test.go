package healing

import (
	"strings"
	"testing"
)

func TestClassifierPrecedenceAndSelectorExtraction(t *testing.T) {
	if got := Classify("AssertionError: Expected to find element: `#save`", ""); got != "assertion_failed" {
		t.Fatalf("assertion safety precedence = %q", got)
	}
	if got := Classify("AssertionError: expected 2 received 3", ""); got != "assertion_failed" {
		t.Fatalf("assertion = %q", got)
	}
	cases := []struct{ message, stack, want string }{
		{"waiting for locator('text=\\'Sign In\\'')", "", "text='Sign In'"},
		{"Expected to find element: `#login-btn`", "", "#login-btn"},
		{"Unable to locate element: {\"method\":\"css selector\",\"selector\":\"#submit\"}", "", "#submit"},
		{"", `cy.get(".submit-button").click()`, ".submit-button"},
		{"", `driver.find_element(By.CSS_SELECTOR, "#old-btn")`, "#old-btn"},
	}
	for _, c := range cases {
		if got := ExtractSelector(c.message, c.stack); got != c.want {
			t.Errorf("ExtractSelector(%q)=%q want %q", c.message, got, c.want)
		}
	}
}

func TestTier1P1BoundariesRefuseAssertionsCommentsAndUnsafeTiming(t *testing.T) {
	assertion := Heal(Request{Version: Version, Framework: "playwright", ErrorMessage: "Error: expect(locator).toHaveText(expected) failed\nwaiting for locator('#old')", TestCode: "await expect(page.locator('#old')).toHaveText('Paid');", PageSnapshot: `<button id="old-new">Paid</button>`})
	if assertion.Decision != "refuse" || assertion.FailureType != "assertion_failed" || assertion.SelectedTier != "tier3_human" {
		t.Fatalf("locator call log bypassed assertion guard: %+v", assertion)
	}
	for _, code := range []string{
		"// await page.locator('#old').click();\nawait page.locator('#other').click();",
		"/* page.locator('#old').click(); */\nawait page.locator('#other').click();",
		`const documentation = "page.locator('#old').click()";`,
		"const template = `page.locator('#old').click()`;",
		`const matcher = /page\.locator\('#old'\)/;`,
	} {
		r := Heal(Request{Version: Version, Framework: "playwright", FailureType: "locator_not_found", FailedSelector: "#old", TestCode: code, PageSnapshot: `<button id="old-new">x</button>`})
		if r.Decision != "refuse" {
			t.Fatalf("non-executable locator was changed: %+v", r)
		}
	}
	synchronous := Heal(Request{Version: Version, Framework: "playwright", FailureType: "locator_timeout", ErrorMessage: "timeout exceeded", FailedSelector: "button", TestCode: "function getButton() {\n  return page.locator('button');\n}", PageSnapshot: `<button>ok</button>`})
	if synchronous.Decision != "refuse" {
		t.Fatalf("wait was inserted in synchronous code: %+v", synchronous)
	}
	awaited := Heal(Request{Version: Version, Framework: "playwright", FailureType: "locator_timeout", ErrorMessage: "timeout exceeded", FailedSelector: "button", TestCode: "await page.locator('button').click();"})
	if awaited.Decision != "propose" || !strings.Contains(awaited.ProposedCode, ".waitFor({ state: 'visible'") {
		t.Fatalf("standalone await did not receive a wait: %+v", awaited)
	}
}

func TestTier1RefusesForgedAndNonExecutableLocatorContexts(t *testing.T) {
	base := Request{Version: Version, Framework: "playwright", FailureType: "locator_not_found", ErrorMessage: "locator not found", FailedSelector: "#old", PageSnapshot: `<button id="old-new">x</button>`}
	for _, code := range []string{
		"await unrelatedlocator('#old');",
		"await homepage.locator('#old').click();",
		"const regex = /[/]page.locator('#old')/;",
		"const template = `literal ${`page.locator('#old')`}`;",
		`page.locator('#old\b').click()`,
		"await page.locator('#old' + ' > button').click();",
		"await expect(page.locator('#old')).toBeVisible();",
	} {
		base.TestCode = code
		if got := Heal(base); got.Decision != "refuse" {
			t.Fatalf("unsafe source context changed for %q: %+v", code, got)
		}
	}
	python := Request{Version: Version, Framework: "selenium", FailureType: "locator_not_found", FailedSelector: "#old", PageSnapshot: base.PageSnapshot, TestCode: "# driver.find_element(By.CSS_SELECTOR, '#old').click()\nprint('untouched')"}
	if got := Heal(python); got.Decision != "refuse" {
		t.Fatalf("Python comment changed: %+v", got)
	}
	python.TestCode = `"""documentation " driver.find_element(By.CSS_SELECTOR, '#old') """`
	if got := Heal(python); got.Decision != "refuse" {
		t.Fatalf("Python docstring changed: %+v", got)
	}
	assertion := base
	assertion.ErrorMessage = "Error: expect(locator).toContainText(expected) failed\nwaiting for locator('#old')"
	assertion.TestCode = "await expect(page.locator('#old')).toContainText('Paid');"
	assertion.FailureType = ""
	if got := Heal(assertion); got.Decision != "refuse" || got.FailureType != "assertion_failed" {
		t.Fatalf("assertion call log was changed: %+v", got)
	}
}

func TestTier1RefusesFakeOrUnsafeSnapshotAnchors(t *testing.T) {
	for _, snapshot := range []string{
		"<!-- <button id='old-new'>x</button> -->",
		`<div title=" id='old-new' ">x</div>`,
		`<button id="123old">x</button>`,
	} {
		got := Heal(Request{Version: Version, Framework: "playwright", FailureType: "locator_not_found", FailedSelector: "#old", TestCode: "await page.locator('#old').click();", PageSnapshot: snapshot})
		if got.Decision != "refuse" {
			t.Fatalf("unsafe snapshot anchor changed for %q: %+v", snapshot, got)
		}
	}
	compound := Heal(Request{Version: Version, Framework: "playwright", FailureType: "locator_not_found", FailedSelector: "#old > button", TestCode: "await page.locator('#old > button').click();", PageSnapshot: `<div id="old-new"><button>x</button></div>`})
	if compound.Decision != "refuse" {
		t.Fatalf("compound selector was collapsed: %+v", compound)
	}
	unsafeTransform := Heal(Request{Version: Version, Framework: "playwright", FailureType: "locator_not_found", FailedSelector: "[data-testid=\"old\n\"]", TestCode: "page.locator(\"[data-testid=\\\"old\\n\\\"]\").click()"})
	if unsafeTransform.Decision != "refuse" {
		t.Fatalf("unsafe attribute transform changed: %+v", unsafeTransform)
	}
}

func TestTier1ClassCandidateRequiresUniqueElement(t *testing.T) {
	ambiguous := Heal(Request{Version: Version, Framework: "playwright", FailureType: "locator_not_found", FailedSelector: ".save", TestCode: `await page.locator('.save').click();`, PageSnapshot: `<div><button class="Save">One</button><button class="Save">Two</button></div>`})
	if ambiguous.Decision != "refuse" {
		t.Fatalf("ambiguous class candidate was proposed: %+v", ambiguous)
	}
	unique := Heal(Request{Version: Version, Framework: "playwright", FailureType: "locator_not_found", FailedSelector: ".save", TestCode: `await page.locator('.save').click();`, PageSnapshot: `<div><button class="Save primary">One</button><button class="discard">Two</button></div>`})
	if unique.Decision != "propose" || unique.Metadata["newSelector"] != ".Save" {
		t.Fatalf("unique class candidate was refused: %+v", unique)
	}
	repeated := Heal(Request{Version: Version, Framework: "playwright", FailureType: "locator_not_found", FailedSelector: ".save", TestCode: `await page.locator('.save').click();`, PageSnapshot: `<button class="Save save">One</button>`})
	if repeated.Decision != "propose" || repeated.Metadata["newSelector"] != ".Save" {
		t.Fatalf("duplicate tokens on one element were double counted: %+v", repeated)
	}
}

func TestTier1EmitsSyntaxSafeSourceOrRefuses(t *testing.T) {
	newline := Heal(Request{Version: Version, Framework: "playwright", FailureType: "locator_not_found", FailedSelector: "#old", TestCode: "await page.locator('#old').click();", PageSnapshot: "<button id=\"old\nnew\">x</button>"})
	if newline.Decision != "refuse" {
		t.Fatalf("unsafe CSS identifier was proposed: %+v", newline)
	}
	r := Heal(Request{Version: Version, Framework: "playwright", FailureType: "locator_not_found", FailedSelector: "#old", TestCode: "await page.locator('#old').click();", PageSnapshot: `<button id="old-new">x</button>`})
	if r.Decision != "propose" {
		t.Fatalf("safe control proposal refused: %+v", r)
	}
	checkJavaScript(t, r.ProposedCode)
}

func TestTier1FrameworkBoundaries(t *testing.T) {
	for _, code := range []string{`cy.contains('#old').click()`, `cy.contains('#container', '#old').click()`} {
		cyContains := Heal(Request{Version: Version, Framework: "cypress", FailureType: "locator_not_found", FailedSelector: "#old", TestCode: code, PageSnapshot: `<button id="old-new">x</button>`})
		if cyContains.Decision != "refuse" {
			t.Fatalf("Cypress contains overload was changed: %+v", cyContains)
		}
	}
	selenium := Heal(Request{Version: Version, Framework: "selenium", FailureType: "locator_not_found", FailedSelector: "#old", TestCode: `driver.find_element(By.CSS_SELECTOR, "#old").click()`, PageSnapshot: `<button id="old-new">x</button>`})
	if selenium.Decision != "propose" || !strings.Contains(selenium.ProposedCode, `"#old-new"`) {
		t.Fatalf("supported Selenium selector was not changed: %+v", selenium)
	}
	checkPythonCode(t, selenium.ProposedCode)
	for _, code := range []string{`cy.get('#old').click()`, `cy.find('#old').click()`} {
		got := Heal(Request{Version: Version, Framework: "cypress", FailureType: "locator_not_found", FailedSelector: "#old", TestCode: code, PageSnapshot: `<button id="old-new">x</button>`})
		if got.Decision != "propose" || !strings.Contains(got.ProposedCode, "#old-new") {
			t.Fatalf("supported Cypress locator was refused: %+v", got)
		}
	}
}

func TestTier1KeepsSeparateExecutableActionsEligible(t *testing.T) {
	code := "// preceding note\nawait page.locator('#old').click();\nawait expect(page.locator('#other')).toHaveText('Paid');"
	got := Heal(Request{Version: Version, Framework: "playwright", ErrorMessage: "waiting for locator('#old')", FailureType: "locator_not_found", FailedSelector: "#old", TestCode: code, PageSnapshot: `<button id="old-new">x</button>`})
	if got.Decision != "propose" || !strings.Contains(got.ProposedCode, "#old-new") {
		t.Fatalf("separate executable action was refused: %+v", got)
	}
	async := Heal(Request{Version: Version, Framework: "playwright", FailureType: "locator_timeout", ErrorMessage: "timeout", FailedSelector: "button", TestCode: "async function check() {\n  await page.locator('button').click();\n}"})
	if async.Decision != "propose" || !strings.Contains(async.ProposedCode, "waitFor") {
		t.Fatalf("indented async wait was refused: %+v", async)
	}
}

func TestTier1AttributeNameBoundary(t *testing.T) {
	value, anchor := alternative("#old", `<button data-testid="old-new" id="other">x</button>`)
	if value != "" || anchor != "" {
		t.Fatalf("data-testid was mistaken for id: %q, %q", value, anchor)
	}
}

func TestTier1ProposalUsesStableAnchorAndEscapesSourceLiteral(t *testing.T) {
	r := Heal(Request{Version: Version, Framework: "playwright", ErrorMessage: "waiting for locator('#login')", TestCode: `await page.locator('#login').click();`, PageSnapshot: `<button id="login-new">Sign in</button>`})
	if r.Decision != "propose" || r.Apply || !r.RequiresApproval || r.Confidence != .85 {
		t.Fatalf("unsafe proposal: %+v", r)
	}
	if !strings.Contains(r.ProposedCode, "#login-new") || r.Metadata["anchor"] != "id" {
		t.Fatalf("unexpected proposal: %+v", r)
	}
	if r.SelectedTier != "tier2_ai_suggest" || r.AttemptedTier != "tier1_auto" {
		t.Fatalf("selection and direct attempt were conflated: %+v", r)
	}
	quoted, ok := replaceLiteral(`await page.locator("text='old'").click();`, `text='old'`, `text='old "quoted"'`)
	if !ok || !strings.Contains(quoted, `text='old \"quoted\"'`) {
		t.Fatalf("selector was not escaped: %q", quoted)
	}
	if _, ok := replaceLiteral(`page.locator('#old\").click()`, "#old", "#new"); ok {
		t.Fatal("mismatched source quotes were accepted")
	}
}

func TestTier1AnchorOrderIncludesClass(t *testing.T) {
	class, anchor := alternative(".save-button", `<button class="save-button primary">Save</button>`)
	if class != ".save-button" || anchor != "class" {
		t.Fatalf("class anchor = %q, %q", class, anchor)
	}
	stable := Heal(Request{Version: Version, Framework: "playwright", FailureType: "locator_not_found", FailedSelector: "#save", TestCode: `page.locator("#save").click()`, PageSnapshot: `<button data-testid="save-new" id="save-new" class="save-button">Save</button>`})
	if stable.Decision != "propose" || stable.Metadata["anchor"] != "id" || !strings.Contains(stable.ProposedCode, "save-new") {
		t.Fatalf("stable anchor: %+v", stable)
	}
}

func TestTier1RefusesAssertionsNoopsAmbiguityAndRole(t *testing.T) {
	assertion := Heal(Request{Version: Version, Framework: "playwright", FailureType: "assertion_failed", AllowAssertionChange: true, TestCode: `expect(1).toBe(2)`})
	if assertion.Decision != "refuse" || assertion.SelectedTier != "tier3_human" {
		t.Fatalf("assertion changed: %+v", assertion)
	}
	noLiteral := Heal(Request{Version: Version, Framework: "playwright", FailureType: "locator_not_found", FailedSelector: "#old", TestCode: `// #old\nawait page.locator('#different').click()`, PageSnapshot: `<button id="old-new">x</button>`})
	if noLiteral.Decision != "refuse" {
		t.Fatalf("comment/global edit accepted: %+v", noLiteral)
	}
	ambiguous := Heal(Request{Version: Version, Framework: "playwright", FailureType: "locator_not_found", FailedSelector: "#old", TestCode: `page.locator('#old'); page.locator('#old');`, PageSnapshot: `<button id="old-new">x</button>`})
	if ambiguous.Decision != "refuse" {
		t.Fatalf("ambiguous edit accepted: %+v", ambiguous)
	}
	role := Heal(Request{Version: Version, Framework: "playwright", ErrorMessage: `locator.getByRole('button', {name:'Save'})`, TestCode: `page.getByRole('button', {name:'Save'})`})
	if role.Decision != "refuse" || role.FailedSelector != "" {
		t.Fatalf("role was treated as CSS: %+v", role)
	}
}

func TestTier1TransformationsAreCompleteOrRefused(t *testing.T) {
	r := Heal(Request{Version: Version, Framework: "playwright", FailureType: "locator_not_found", FailedSelector: ".foo-bar", TestCode: `page.locator('.foo-bar').click()`})
	if r.Decision != "propose" || !strings.Contains(r.ProposedCode, `[class*="foo"]`) || r.Confidence != .7 {
		t.Fatalf("class transformation: %+v", r)
	}
	partial := Heal(Request{Version: Version, Framework: "playwright", FailureType: "locator_not_found", FailedSelector: ".foo-bar-baz", TestCode: `page.locator('.foo-bar-baz').click()`})
	if partial.Decision != "refuse" {
		t.Fatalf("partial class accepted: %+v", partial)
	}
}
