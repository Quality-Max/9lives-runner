import {test, expect} from '@playwright/test';

test("D13-error-banner-plausible-page", async ({page}) => {
  await page.setContent(`<h1>Order summary</h1><div role="alert" style="background:#fdd;color:#900;padding:12px">Card declined. Try another card.</div><p>Notebook × 1 — $21.00</p><p role="status">Processing</p>`);
  await expect(page.getByRole('status')).toHaveText('Order confirmed');
});
