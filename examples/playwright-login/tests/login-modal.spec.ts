import {test, expect} from '@9l/playwright';

test('playwright-login opens its login form', async ({page, n9l}) => {
  await page.goto('/app');
  await expect(page.getByRole('button', {name: 'Login', exact: true})).toBeVisible();
  await n9l.goal('Click the Login button to open the login form. Stop when Welcome back is visible. Do not fill or submit the form.');
  await expect(page.getByRole('heading', {name: 'Welcome back', exact: true})).toBeVisible();
  await expect(page.getByLabel('Email', {exact: true})).toBeVisible();
  await expect(page.getByLabel('Password', {exact: true})).toBeVisible();
});
