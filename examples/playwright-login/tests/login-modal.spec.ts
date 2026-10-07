import {test, expect} from '@9lives/playwright';

test('playwright-login opens its login form', async ({page, nineLives}) => {
  await page.goto('/app');
  await expect(page.getByRole('button', {name: 'Login', exact: true})).toBeVisible();
  await nineLives.goal('Click the Login button to open the login form. Stop when Welcome back is visible. Do not fill or submit the form.');
  await expect(page.getByRole('heading', {name: 'Welcome back', exact: true})).toBeVisible();
  await expect(page.getByLabel('Email', {exact: true})).toBeVisible();
  await expect(page.getByLabel('Password', {exact: true})).toBeVisible();
});
