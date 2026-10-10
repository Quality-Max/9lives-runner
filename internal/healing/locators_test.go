package healing

import (
	"strings"
	"testing"
)

func TestFailedLocatorReadsSingleGetByCallsFromTheCallLog(t *testing.T) {
	for message, want := range map[string]string{
		"TimeoutError: locator.click: Timeout\nCall log:\n\x1b[2m  - waiting for getByRole('button', { name: 'Anmelden' })\x1b[22m": "getByRole('button', { name: 'Anmelden' })",
		"Call log:\n  - waiting for getByRole('button', { name: 'Don\\'t go', exact: true })":                                       "getByRole('button', { name: 'Don\\'t go', exact: true })",
		"Call log:\n  - waiting for getByLabel('E-mail', { exact: true })":                                                          "getByLabel('E-mail', { exact: true })",
		"Call log:\n  - waiting for getByTestId('submit')":                                                                          "getByTestId('submit')",
		"Call log:\n  - waiting for getByRole('form').getByRole('button', { name: 'Go' })":                                          "",
		"Call log:\n  - waiting for getByRole('button', { name: /sign in/i })":                                                      "",
		"Call log:\n  - waiting for locator('#emailAddress')":                                                                       "#emailAddress",
	} {
		if got := FailedLocator(message); got != want {
			t.Errorf("FailedLocator(%q) = %q, want %q", message, got, want)
		}
	}
}

func TestGetByLocatorNameIsTheOnlyEditableSpan(t *testing.T) {
	source := "test('login', async ({ page }) => {\n  await page.getByRole('button', { name: \"Anmelden\" }).click();\n  await expect(page.getByRole('status')).toHaveText('ok');\n});\n"
	failed := "getByRole('button', { name: 'Anmelden' })"
	if ok, why := EditableLocatorAction(source, failed, "playwright"); !ok {
		t.Fatalf("not editable: %s", why)
	}
	renamed := strings.Replace(source, `"Anmelden"`, `"Login"`, 1)
	if !ExactLocatorSelectorReplacement(source, renamed, failed, "playwright") {
		t.Fatal("name change refused")
	}
	for name, candidate := range map[string]string{
		"role changed":      strings.Replace(source, "'button', { name", "'link', { name", 1),
		"option added":      strings.Replace(source, `name: "Anmelden" }`, `name: "Login", exact: true }`, 1),
		"assertion changed": strings.Replace(renamed, "'ok'", "'fine'", 1),
		"quote injected":    strings.Replace(source, `"Anmelden"`, `"Log\"in"`, 1),
	} {
		if ExactLocatorSelectorReplacement(source, candidate, failed, "playwright") {
			t.Errorf("%s: candidate admitted", name)
		}
	}
}

func TestGetByLocatorBoundaries(t *testing.T) {
	for name, tc := range map[string]struct{ source, failed, reason string }{
		"inside an assertion": {"test('x', async ({ page }) => {\n  await expect(page.getByText('Saved')).toBeVisible();\n});\n", "getByText('Saved')", "inside an assertion"},
		"different role":      {"test('x', async ({ page }) => {\n  await page.getByRole('link', { name: 'Go' }).click();\n});\n", "getByRole('button', { name: 'Go' })", "no page.getByRole('button', { name: 'Go' }) call"},
		"exact differs":       {"test('x', async ({ page }) => {\n  await page.getByLabel('E-mail').fill('a');\n});\n", "getByLabel('E-mail', { exact: true })", "no page.getByLabel('E-mail', { exact: true }) call"},
		"chained":             {"test('x', async ({ page }) => {\n  await page.getByRole('form').getByRole('button', { name: 'Go' }).click();\n});\n", "getByRole('button', { name: 'Go' })", "no page.getByRole('button', { name: 'Go' }) call"},
		"used twice":          {"test('x', async ({ page }) => {\n  await page.getByText('Go').click();\n  await page.getByText('Go').hover();\n});\n", "getByText('Go')", "more than once"},
	} {
		ok, why := EditableLocatorAction(tc.source, tc.failed, "playwright")
		if ok || !strings.Contains(why, tc.reason) {
			t.Errorf("%s: ok=%v reason=%q", name, ok, why)
		}
	}
}

func TestTier1RefusesGetByAssertionAndDoesNotAddCSSWait(t *testing.T) {
	source := "test('x', async ({ page }) => {\n  await page.getByRole('button', { name: 'Go' }).click();\n});\n"
	response := Heal(Request{Version: Version, Framework: "playwright", ErrorMessage: "TimeoutError: locator.click: Timeout 500ms exceeded.\nCall log:\n  - waiting for getByRole('button', { name: 'Go' })", FailedSelector: "getByRole('button', { name: 'Go' })", TestCode: source})
	if response.Decision != "refuse" || strings.Contains(response.ProposedCode, "page.locator(") {
		t.Fatalf("response=%+v", response)
	}
	owned := "test('x', async ({ page }) => {\n  await expect(page.getByText('Saved')).toBeVisible();\n});\n"
	if response := Heal(Request{Version: Version, Framework: "playwright", ErrorMessage: "TimeoutError: waiting for getByText('Saved')", FailedSelector: "getByText('Saved')", TestCode: owned}); response.FailureType != "assertion_failed" {
		t.Fatalf("assertion-owned getBy locator: %+v", response)
	}
}

func TestAriaAlternativeReFindsOnlyASingleRenamedCandidate(t *testing.T) {
	snapshot := "- generic [ref=e2]:\n  - text: E-mail\n  - textbox \"E-mail Adresse\" [ref=e3]\n  - button \"Einloggen\" [ref=e4]\n  - link \"Passwort vergessen\" [ref=e5]\n"
	role := editableLocator{method: "getByRole", role: "button", value: "Anmelden"}
	if got := ariaAlternative(role, snapshot); got != "Einloggen" {
		t.Fatalf("unique role: %q", got)
	}
	if got := ariaAlternative(editableLocator{method: "getByLabel", value: "Email"}, snapshot); got != "E-mail Adresse" {
		t.Fatalf("labelled control: %q", got)
	}
	two := snapshot + "  - button \"Registrieren\" [ref=e6]\n"
	if got := ariaAlternative(role, two); got != "" {
		t.Fatalf("two unrelated buttons must not pick one: %q", got)
	}
	if got := ariaAlternative(editableLocator{method: "getByRole", role: "button", value: "Jetzt einloggen"}, two); got != "Einloggen" {
		t.Fatalf("shared word: %q", got)
	}
	if got := ariaAlternative(editableLocator{method: "getByRole", role: "button", value: "einloggen"}, snapshot); got != "" {
		t.Fatalf("a still-matching name is not a rename: %q", got)
	}
	if got := ariaAlternative(editableLocator{method: "getByText", value: "Hi"}, snapshot); got != "" {
		t.Fatalf("getByText is not re-found from roles: %q", got)
	}
}

func TestTier1RepairsRenamedButtonFromAriaSnapshot(t *testing.T) {
	source := "test('x', async ({ page }) => {\n  await page.getByRole('button', { name: 'Anmelden' }).click();\n});\n"
	request := Request{Version: Version, Framework: "playwright", ErrorMessage: "TimeoutError: locator.click: Timeout 500ms exceeded.\nCall log:\n  - waiting for getByRole('button', { name: 'Anmelden' })", FailedSelector: "getByRole('button', { name: 'Anmelden' })", TestCode: source}
	if response := Heal(request); response.Decision != "refuse" || !strings.Contains(response.Reason, "ARIA snapshot") {
		t.Fatalf("without snapshot: %+v", response)
	}
	request.AriaSnapshot = "- button \"Einloggen\" [ref=e2]\n"
	response := Heal(request)
	if response.Decision != "propose" || response.ProposedCode != strings.Replace(source, "Anmelden", "Einloggen", 1) || !ExactLocatorSelectorReplacement(source, response.ProposedCode, request.FailedSelector, "playwright") {
		t.Fatalf("with snapshot: %+v", response)
	}
}

func TestPageActionShorthandSelectorIsEditable(t *testing.T) {
	source := "test('x', async ({ page }) => {\n  await page.fill('#emailAddress', 'qa@example.test');\n  await page.click(\"#submit\");\n});\n"
	for _, selector := range []string{"#emailAddress", "#submit"} {
		if ok, why := EditableLocatorAction(source, selector, "playwright"); !ok {
			t.Fatalf("%s not editable: %s", selector, why)
		}
	}
	renamed := strings.Replace(source, "'#emailAddress'", "'#email'", 1)
	if !ExactLocatorSelectorReplacement(source, renamed, "#emailAddress", "playwright") {
		t.Fatal("selector change refused")
	}
	if ExactLocatorSelectorReplacement(source, strings.Replace(renamed, "qa@example.test", "x", 1), "#emailAddress", "playwright") {
		t.Fatal("value change admitted")
	}
	if code, ok := addWait(source, "#submit"); !ok || !strings.Contains(code, "  await page.locator(\"#submit\").waitFor({ state: 'visible', timeout: 10000 });\n  await page.click(\"#submit\");") {
		t.Fatalf("wait not added before shorthand: %v\n%s", ok, code)
	}
	if ok, _ := EditableLocatorAction("test('x', async ({ page }) => {\n  await page.waitForSelector('#x');\n});\n", "#x", "playwright"); ok {
		t.Fatal("non-action page method treated as editable")
	}
}
