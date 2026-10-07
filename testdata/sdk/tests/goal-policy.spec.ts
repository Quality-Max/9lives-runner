import {test, expect} from '@9lives/playwright';
test('a caught policy rejection still fails the engine receipt', async ({page, nineLives}) => {
  await page.setContent('<button onclick="document.body.dataset.deleted=1">Delete project</button>');
  await expect(nineLives.goal('Click Delete project')).rejects.toThrow('policy_blocked');
  expect(await page.locator('body').getAttribute('data-deleted')).toBeNull();
});
