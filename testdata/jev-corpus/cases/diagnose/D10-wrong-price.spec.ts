import {test, expect} from '@playwright/test';

test("D10-wrong-price", async ({page}) => {
  await page.setContent(`<h1>Your cart</h1><p>Notebook × 2 at $21.00</p><p>Total: <strong data-testid="total">$24.00</strong></p><button>Checkout</button>`);
  await expect(page.getByTestId('total')).toHaveText('$42.00');
});
