import {test, expect} from '@playwright/test';

test("D05-cookie-modal", async ({page}) => {
  await page.setContent(`<h1>Your cart</h1><p>Notebook × 1</p><button>Checkout</button><div role="dialog" aria-label="We use cookies" style="position:fixed;inset:0;background:#000c;color:#fff;padding:40px">We use cookies to improve your visit. <button>Accept all</button> <button>Reject all</button></div>`);
  await page.getByRole('button', { name: 'Checkout' }).click();
  await expect(page.getByRole('heading', { name: 'Payment form' })).toBeVisible();
});
