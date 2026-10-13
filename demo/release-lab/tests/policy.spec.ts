import {test, expect} from '@9l/playwright';
import {useShop} from './fixtures';
const shop = useShop(test);

test('a goal stops before a purchase action', async ({page, n9l}) => {
  await page.goto(shop());
  let orderRequests = 0;
  page.on('request', request => { if (new URL(request.url()).pathname === '/api/orders') orderRequests++; });
  await expect(n9l.goal('Place the order')).rejects.toThrow('policy_blocked');
  expect(orderRequests).toBe(0);
  await expect(page.getByRole('status', {name: 'Order'})).toHaveText('Your kit is ready.');
  // These assertions pass; the blocked goal still makes the 9l run incomplete.
});
