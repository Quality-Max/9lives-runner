import {test, expect} from '@playwright/test';
import {start} from '../app/server.cjs';

test('a renamed profile button can be healed without changing intent', async ({page}) => {
  const app = await start();
  try {
    await page.goto(app.url);
    await page.getByRole('textbox', {name: 'Your name'}).fill('Ada');
    await page.getByRole('button', {name: "Continue now", exact: true}).click();
    await expect(page.getByRole('status', {name: 'Profile'})).toHaveText('Ready for Ada');
  } finally { await app.close(); }
});
