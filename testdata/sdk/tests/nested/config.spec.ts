import {test, expect} from '@playwright/test';

test('nested ordinary spec uses its enclosing project configuration', async ({page}) => {
  await page.setContent('<main>Local nested application</main>');
  expect(page.viewportSize()?.width).toBe(777);
  await expect(page.getByRole('main')).toHaveText('Local nested application');
});
