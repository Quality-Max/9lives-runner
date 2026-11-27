import {test, expect} from '@playwright/test';

test("R07-wrong-business-outcome", async ({page}) => {
  await page.setContent(`<h1>Your cart</h1><p>Total: <strong data-testid="total">$24.00</strong></p><button>Back</button><button onclick="document.body.insertAdjacentHTML('beforeend','<h2>Payment form</h2>')">Continue to payment</button><button>Help</button>`);
  await page.getByRole('button', { name: 'Checkout' }).click();
  await expect(page.getByRole('heading', { name: 'Payment form' })).toBeVisible();
  await expect(page.getByTestId('total')).toHaveText('$42.00');
});
