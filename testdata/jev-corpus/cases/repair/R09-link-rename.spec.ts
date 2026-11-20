import {test, expect} from '@playwright/test';

test("R09-link-rename", async ({page}) => {
  await page.setContent(`<nav><a href="#docs">Docs</a> <a href="#plans">Plans and billing</a> <a href="#blog">Blog</a></nav><h1>Product</h1>`);
  await page.getByRole('link', { name: 'Pricing' }).click();
  await expect(page).toHaveURL(/#plans$/);
});
