import {test, expect} from '@playwright/test';

test("D15-blank-page", async ({page}) => {
  await page.setContent(``);
  await page.getByRole('button', { name: 'Checkout' }).click();
  await expect(page.getByRole('heading', { name: 'Payment form' })).toBeVisible();
});
