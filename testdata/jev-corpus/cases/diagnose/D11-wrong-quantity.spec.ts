import {test, expect} from '@playwright/test';

test("D11-wrong-quantity", async ({page}) => {
  await page.setContent(`<h1>Your cart</h1><p>Notebook</p><label>Quantity <input data-testid="qty" value="1"></label><p>You added 2 notebooks.</p>`);
  await expect(page.getByTestId('qty')).toHaveValue('2');
});
