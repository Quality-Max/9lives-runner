import {test, expect} from '@playwright/test';

test("R04-label-rename", async ({page}) => {
  await page.setContent(`<h1>Create account</h1><label>Full name <input id="name"></label><label>Phone <input id="phone"></label><label>Email address <input id="email"></label>`);
  await page.getByLabel('E-mail').fill('person@example.test');
  await expect(page.locator('#email')).toHaveValue('person@example.test');
});
