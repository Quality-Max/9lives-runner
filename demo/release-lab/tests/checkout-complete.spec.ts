import {test, expect} from '@9l/playwright';
import {useShop} from './fixtures';
const shop = useShop(test);

// @9l-requirement studio-checkout
test('checkout protects the complete demo contract', async ({page, n9l}) => {
  await page.goto(shop());
  // @9l-outcome cart-items
  await expect(page.getByRole('list', {name: 'Cart'}).getByRole('listitem')).toHaveCount(2);
  await n9l.step('place the synthetic order', async () => {
    await page.getByRole('button', {name: 'Place order', exact: true}).click();
  });
  // @9l-outcome order-count
  await expect(page.getByRole('status', {name: 'Order'})).toHaveText('Order confirmed: 2 items');
  // @9l-outcome recommendation
  await expect(page.getByRole('complementary', {name: 'Recommended'})).toContainText('Pencil set');
});
