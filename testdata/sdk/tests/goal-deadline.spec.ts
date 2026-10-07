import {test, expect} from '@9lives/playwright';
test('the goal deadline also stops browser observation', async ({page, nineLives}) => {
  await page.setContent('<button>Continue</button>');
  const original = page.locator.bind(page);
  page.locator = (...args) => {
    const locator = original(...args);
    locator.count = async () => { await page.waitForTimeout(5000); return 1; };
    return locator;
  };
  const started = Date.now();
  await expect(nineLives.goal('Click Continue', {timeoutMs: 100})).rejects.toThrow('budget_exhausted');
  expect(Date.now() - started).toBeLessThan(2000);
  expect(page.isClosed()).toBe(true);
});
