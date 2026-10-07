import {test, expect} from '@9lives/playwright';
test('business failure remains a failure', async ({page, nineLives}) => {
  await page.setContent('<p role="status">Order rejected</p>');
  await nineLives.step('verify the required outcome', async () => {
    await expect(page.getByRole('status')).toHaveText('Order confirmed', {timeout: 100});
  });
});
