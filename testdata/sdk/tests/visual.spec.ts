import {test, expect} from '@9l/playwright';

// Visual-mode qualification: the page reports how the browser was launched.
// The smoke sets NINELIVES_SMOKE_HEADED=1 when the browser must be headed.
test('visual mode is observable from the page', async ({page}) => {
  await page.setContent(`<button type="button" onclick="this.textContent = 'Clicked'">Click</button>`);
  await page.getByRole('button').click();
  await expect(page.getByRole('button')).toHaveText('Clicked');
  const userAgent = await page.evaluate(() => navigator.userAgent);
  expect(userAgent.includes('HeadlessChrome')).toBe(process.env.NINELIVES_SMOKE_HEADED !== '1');
});
