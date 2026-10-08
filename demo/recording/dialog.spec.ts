import { test, expect } from '@playwright/test';

test('join does not open the dialog', async ({ page }) => {
  await page.setContent('<button>Join</button><dialog>Pick a room</dialog>');
  await page.getByRole('button', { name: 'Join' }).click();
  await page.waitForTimeout(1000);
  await expect(page.getByRole('dialog')).not.toBeVisible();
});
