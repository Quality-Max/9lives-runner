import {test, expect} from '@playwright/test';

// Visual-mode qualification for the ordinary adapter: the page reports how
// the browser was launched. The smoke sets NINELIVES_SMOKE_HEADED=1 when the
// browser must be headed. Ordinary runner benchmarks use pass.spec.ts, which
// stays browser-free.
test('visual mode is observable from the page', async ({page}) => {
  await page.setContent(`<button type="button" onclick="this.textContent = 'Clicked'">Click</button>`);
  await page.getByRole('button').click();
  await expect(page.getByRole('button')).toHaveText('Clicked');
  const userAgent = await page.evaluate(() => navigator.userAgent);
  expect(userAgent.includes('HeadlessChrome')).toBe(process.env.NINELIVES_SMOKE_HEADED !== '1');
});
