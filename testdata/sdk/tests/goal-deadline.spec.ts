import {test, expect} from '@9l/playwright';
test('the goal deadline also stops browser observation', async ({page, n9l}) => {
  await page.setContent('<button>Continue</button>');
  const original = page.locator.bind(page);
  page.locator = (...args) => {
    const locator = original(...args);
    locator.count = async () => { await page.waitForTimeout(5000); return 1; };
    return locator;
  };
  const started = Date.now();
  await expect(n9l.goal('Click Continue', {timeoutMs: 100})).rejects.toThrow('budget_exhausted');
  expect(Date.now() - started).toBeLessThan(2000);
  expect(page.isClosed()).toBe(true);
});
