import {test, expect} from '@playwright/test';

test("D14-hidden-promo", async ({page}) => {
  await page.setContent(`<h1>Your cart</h1><p style="visibility:hidden">Free shipping applied</p><p>Shipping: $4.99</p>`);
  await expect(page.getByText('Free shipping applied')).toBeVisible();
});
