import {test, expect} from '@9l/playwright';
import {useShop} from './fixtures';
const shop = useShop(test);

test('goal completion still needs a business assertion', async ({page, n9l}) => {
  await page.goto(shop());
  const result = await n9l.goal('Enter the customer name and continue to review', {params: {name: 'Ada'}, maxActions: 3, maxDecisions: 5});
  expect(result.verified).toBe(false);
  await expect(page.getByRole('status', {name: 'Profile'})).toHaveText('Ready for Ada');
});
