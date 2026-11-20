import {test, expect} from '@playwright/test';

test("D04-duplicate-controls", async ({page}) => {
  await page.setContent(`<h1>Projects</h1><ul><li>Alpha <button>Archive</button></li><li>Beta <button>Archive</button></li></ul>`);
  await page.getByRole('button', { name: 'Archive' }).click();
  await expect(page.getByText('Alpha archived')).toBeVisible();
});
