import {test, expect} from '@playwright/test';

test("R06-target-removed", async ({page}) => {
  await page.setContent(`<h1>Your cart</h1><p>Notebook × 1</p><button>Back</button><button>Help</button>`);
  await page.getByRole('button', { name: 'Checkout' }).click();
  await expect(page.getByRole('heading', { name: 'Payment form' })).toBeVisible();
});
