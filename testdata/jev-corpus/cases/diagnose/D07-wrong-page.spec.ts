import {test, expect} from '@playwright/test';

test("D07-wrong-page", async ({page}) => {
  await page.setContent(`<h1>Sign in</h1><p>Your session has expired. Please sign in again.</p><label>Email <input></label><label>Password <input type="password"></label><button>Sign in</button>`);
  await page.getByRole('button', { name: 'Checkout' }).click();
  await expect(page.getByRole('heading', { name: 'Payment form' })).toBeVisible();
});
