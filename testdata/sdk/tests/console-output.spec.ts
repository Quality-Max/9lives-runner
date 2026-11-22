import {test, expect} from '@9l/playwright';

// `9l run --sdk --show-output` qualification: the console line reaches the
// CLI's stderr with the secret-looking value redacted, and never the evidence.
test('console output is shown, not kept', async ({page, n9l}) => {
  await page.setContent('<p role="status">ready</p>');
  console.log('show-output check: token=fixture-smoke-secret');
  await n9l.step('verify the page is ready', async () => {
    await expect(page.getByRole('status')).toHaveText('ready');
  });
});
