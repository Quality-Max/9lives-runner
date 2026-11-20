import {test, expect} from '@playwright/test';

test("D02-renamed-link", async ({page}) => {
  await page.setContent(`<nav><a href="#home">Home</a> <a href="#orders">Orders</a> <a href="#account">My account</a></nav><h1>Welcome back</h1>`);
  await page.getByRole('link', { name: 'Profile' }).click();
  await expect(page).toHaveURL(/#account$/);
});
