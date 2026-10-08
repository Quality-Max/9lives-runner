import {test, expect} from '@9l/playwright';
test('business failure remains a failure', async ({page, n9l}) => {
  await page.setContent('<p role="status">Order rejected</p>');
  await n9l.step('verify the required outcome', async () => {
    await expect(page.getByRole('status')).toHaveText('Order confirmed', {timeout: 100});
  });
});
