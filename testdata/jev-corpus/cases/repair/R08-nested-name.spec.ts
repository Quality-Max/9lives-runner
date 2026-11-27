import {test, expect} from '@playwright/test';

test("R08-nested-name", async ({page}) => {
  await page.setContent(`<h1>Your cart</h1><p>Notebook × 1</p><button>Back</button><button onclick="document.body.insertAdjacentHTML('beforeend','<h2>Payment form</h2>')"><span aria-hidden="true">→</span> <span>Proceed to payment</span></button><button>Help</button>`);
  await page.getByRole('button', { name: 'Place order' }).click();
  await expect(page.getByRole('heading', { name: 'Payment form' })).toBeVisible();
});
