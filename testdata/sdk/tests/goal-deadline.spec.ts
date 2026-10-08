import {test, expect} from '@9l/playwright';
test('the goal deadline also stops browser observation', async ({page, n9l}) => {
  await page.setContent('<button>Continue</button>');
  let observations = 0;
  const original = page.locator.bind(page);
  page.locator = (...args) => {
    const locator = original(...args);
    locator.count = async () => { observations++; await page.waitForTimeout(5000); return 1; };
    return locator;
  };
  const started = Date.now();
  // Leave time for Go to admit the goal before the deliberately slow observation.
  await expect(n9l.goal('Click Continue', {timeoutMs: 1000})).rejects.toThrow('budget_exhausted');
  expect(observations).toBe(1);
  expect(Date.now() - started).toBeLessThan(2000);
  expect(page.isClosed()).toBe(true);
});
