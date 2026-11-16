import {test, expect} from '@9l/playwright';

// Regression fixture for microsoft/playwright#43676, found while qualifying
// Playwright's modifier handling for 9l. On 1.43.0 through 1.64.1 a test.skip()
// in an afterEach hook reports a test whose body ran and passed as skipped. In
// 9l the skip still needs a pin, stays counted as skipped, and the receipt says
// the body had already reported a passed assertion step before the skip.
test.describe('relabeled', () => {
  test.afterEach(() => {
    test.skip();
  });

  test('a passing test an after hook reports as skipped', async ({page}) => {
    await page.setContent('<h1>Ready</h1>');
    await expect(page.getByRole('heading')).toHaveText('Ready');
  });
});

test('an ordinary test in the same file still executes', async ({page}) => {
  await page.setContent('<h1>Executed</h1>');
  await expect(page.getByRole('heading')).toHaveText('Executed');
});
