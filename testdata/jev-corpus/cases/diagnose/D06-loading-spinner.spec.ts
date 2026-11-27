import {test, expect} from '@playwright/test';

test("D06-loading-spinner", async ({page}) => {
  await page.setContent(`<h1>Your cart</h1><div role="progressbar" aria-label="Loading cart">Loading your cart…</div><script>setTimeout(()=>{document.querySelector("[role=progressbar]").remove();document.body.insertAdjacentHTML("beforeend","<button>Checkout</button>")},8000)</script>`);
  await page.getByRole('button', { name: 'Checkout' }).click();
  await expect(page.getByRole('heading', { name: 'Payment form' })).toBeVisible();
});
