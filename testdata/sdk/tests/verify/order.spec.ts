import {test, expect} from '@9l/playwright';

// The assertion is deliberately weak: it checks what the page says, not what
// the shop stored. `9l run --verify` asks the shop itself.
test('placing an order shows a confirmation', async ({page, n9l}) => {
  await page.goto(process.env.NINELIVES_SMOKE_SHOP_URL!);
  await n9l.step('place the order', async () => {
    await page.getByRole('button', {name: 'Place order'}).click();
  });
  await n9l.step('see a confirmation', async () => {
    await expect(page.getByRole('status')).toContainText('Order confirmed');
  });
});
