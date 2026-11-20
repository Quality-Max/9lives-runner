import {test, expect} from '@playwright/test';

test("D12-wrong-status", async ({page}) => {
  await page.setContent(`<h1>Thank you!</h1><p role="status">Payment pending</p><p>We emailed your receipt.</p>`);
  await expect(page.getByRole('status')).toHaveText('Order confirmed');
});
