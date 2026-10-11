import { test, expect } from "@playwright/test";

// @9l-requirement empty-sign-in
test("empty sign-in keeps visitors outside Demo Bank", async ({ page }) => {
  test.setTimeout(20000);
  await page.goto("/", { waitUntil: "domcontentloaded" });
  // Remove only the public credential hint before any screenshot or failure snapshot.
  await page.getByText("Demo credentials:", { exact: false }).evaluate(element => element.remove());
  // @9l-outcome sign-in-visible
  await expect(page.getByText("Demo Bank", { exact: true })).toBeVisible();
  await page.locator("form").screenshot({ path: ".context/demo-bank-login.png" });
  // Seeded fixed wait for advisory assessment; removed before execution.
  await page.waitForTimeout(400);
  // Seeded stale locator for the healing demonstration.
  await page.getByRole("button", { name: "Log In", exact: true }).click({ timeout: 2500 });
  // @9l-outcome empty-username-invalid
  expect(await page.getByLabel("Username", { exact: true }).evaluate((field: HTMLInputElement) => field.validity.valueMissing)).toBe(true);
  // @9l-outcome invalid-field-focused
  await expect(page.getByLabel("Username", { exact: true })).toBeFocused();
  // @9l-outcome remains-signed-out
  await expect(page.getByText("Sign in to your account", { exact: true })).toBeVisible();
  // @9l-outcome empty-username-preserved
  await expect(page.getByLabel("Username", { exact: true })).toHaveValue("");
  await page.locator("form").screenshot({ path: ".context/demo-bank-validation.png" });
});
