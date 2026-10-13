import {test, expect} from '@playwright/test';
import {start} from '../app/server.cjs';

test('an ordinary Playwright test keeps working', async ({page}) => {
  const app = await start();
  try {
    await page.goto(app.url);
    await expect(page.getByRole('list', {name: 'Cart'}).getByRole('listitem')).toHaveCount(2);
    await page.getByRole('button', {name: 'Place order', exact: true}).click();
    await expect(page.getByRole('status', {name: 'Order'})).toHaveText('Order confirmed: 2 items');
  } finally { await app.close(); }
});
