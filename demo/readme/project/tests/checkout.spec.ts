import {test, expect} from '@9l/playwright';

test.afterEach(() => {
  test.fail(); // left in a shared hook
});

test('order is confirmed', async ({page}) => {
  await page.setContent('<p role="status">Order rejected</p>');
  await expect(page.getByRole('status')).toHaveText('Order confirmed', {timeout: 100});
});
