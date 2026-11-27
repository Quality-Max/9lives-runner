import {test, expect} from '@playwright/test';

test("D03-renamed-label", async ({page}) => {
  await page.setContent(`<h1>Create account</h1><label>Full name <input id="name"></label><label>Email address <input id="email"></label><button>Create account</button>`);
  await page.getByLabel('E-mail').fill('person@example.test');
  await expect(page.locator('#email')).toHaveValue('person@example.test');
});
