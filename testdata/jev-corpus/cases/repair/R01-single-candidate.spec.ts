import {test, expect} from '@playwright/test';

test("R01-single-candidate", async ({page}) => {
  await page.setContent(`<h1>Your cart</h1><p>Notebook × 1</p><button onclick="document.body.insertAdjacentHTML('beforeend','<h2>Payment form</h2>')">Continue to payment</button>`);
  await page.getByRole('button', { name: 'Checkout' }).click();
  await expect(page.getByRole('heading', { name: 'Payment form' })).toBeVisible();
});
