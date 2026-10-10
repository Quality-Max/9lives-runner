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
