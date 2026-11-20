import {test, expect} from '@playwright/test';

test("D09-visible-error", async ({page}) => {
  await page.setContent(`<h1>Your cart</h1><div role="alert" style="background:#fdd;color:#900;padding:12px">Something went wrong (500). We could not load your cart.</div>`);
  await page.getByRole('button', { name: 'Checkout' }).click();
  await expect(page.getByRole('heading', { name: 'Payment form' })).toBeVisible();
});
