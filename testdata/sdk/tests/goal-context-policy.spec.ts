import {test, expect} from '@9l/playwright';
test('a risky form name blocks a generic control inside it', async ({page, n9l}) => {
  await page.setContent('<form aria-label="Delete account"><section><button type="button" onclick="document.body.dataset.deleted=1">Continue</button></section></form>');
  await expect(n9l.goal('Click Continue')).rejects.toThrow('policy_blocked');
  expect(await page.locator('body').getAttribute('data-deleted')).toBeNull();
});
