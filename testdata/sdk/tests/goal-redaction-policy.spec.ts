import {test, expect} from '@9l/playwright';
test('redaction cannot hide a blocked control', async ({page, n9l}) => {
  await page.setContent('<button onclick="document.body.dataset.deleted=1">Delete project</button>');
  await expect(n9l.goal('Choose the project control', {params: {name: 'Delete'}})).rejects.toThrow('policy_blocked');
  expect(await page.locator('body').getAttribute('data-deleted')).toBeNull();
});
