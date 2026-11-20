import {test, expect} from '@playwright/test';

test("D08-disabled-target", async ({page}) => {
  await page.setContent(`<h1>Your cart</h1><p>Notebook × 1</p><button disabled>Checkout</button>`);
  await page.getByRole('button', { name: 'Checkout' }).click();
  await expect(page.getByRole('heading', { name: 'Payment form' })).toBeVisible();
});
