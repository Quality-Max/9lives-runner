import {test, expect} from '@9lives/playwright';
import {appendFileSync} from 'node:fs';
test.describe.configure({retries: 1});
test('Playwright retries cannot repeat goal mutations', async ({page, nineLives}, testInfo) => {
  await page.exposeFunction('recordClick', () => appendFileSync(process.env.NINELIVES_SMOKE_COUNTER!, 'click\n'));
  await page.setContent('<button onclick="recordClick()">Continue</button>');
  await nineLives.goal('Click Continue');
  await expect.poll(() => testInfo.retry).toBe(-1); // Required business outcome deliberately fails.
});
