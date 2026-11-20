import {test, expect} from '@playwright/test';

test("R05-duplicates-only", async ({page}) => {
  await page.setContent(`<h1>Projects</h1><ul><li>Alpha <button>Remove</button></li><li>Beta <button>Remove</button></li></ul>`);
  await page.getByRole('button', { name: 'Delete' }).click();
  await expect(page.getByText('Alpha removed')).toBeVisible();
});
