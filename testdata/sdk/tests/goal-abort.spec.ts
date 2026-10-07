import {test, expect} from '@9lives/playwright';
test('abort cancels an in-flight browser action', async ({page, context, nineLives}) => {
  let clicks = 0;
  await page.exposeFunction('recordClick', () => { clicks++; });
  await page.setContent('<button onclick="recordClick()">Continue</button><div style="position:fixed;inset:0;z-index:10" id="overlay"></div>');
  const signal = new AbortController();
  // The click decision arrives but the overlay keeps Playwright waiting.
  setTimeout(() => signal.abort(), 500);
  setTimeout(() => { void page.evaluate(() => document.getElementById('overlay')?.remove()).catch(() => {}); }, 800);
  await expect(nineLives.goal('Click Continue', {signal: signal.signal})).rejects.toThrow();
  const probe = await context.newPage();
  await probe.waitForTimeout(1000);
  expect(page.isClosed()).toBe(true);
  expect(clicks).toBe(0);
});
