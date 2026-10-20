import {test, expect} from '@9l/playwright';
import {useShop} from './fixtures';
const shop = useShop(test);

// @9l-requirement studio-checkout
test('a green checkout can still miss a broken recommendation', async ({page, n9l}) => {
  await page.goto(shop());
  // @9l-outcome cart-items
  await expect(page.getByRole('list', {name: 'Cart'}).getByRole('listitem')).toHaveCount(2);
  await n9l.step('place the synthetic order', async () => {
    await page.getByRole('button', {name: 'Place order', exact: true}).click();
  });
  // @9l-outcome order-count
  await expect(page.getByRole('status', {name: 'Order'})).toHaveText('Order confirmed: 2 items');
  // Deliberately no recommendation assertion: prove must expose this gap.
});
