import {test, expect} from '@9l/playwright';

// Regression fixture for microsoft/playwright#43738, found while qualifying
// Playwright's modifier handling for 9l. On 1.61.1 through 1.64.1 a test.fail()
// in an after hook sets the completed test's expected status to failed, so a
// genuinely failing test ends "expected", the summary says "1 passed" and
// Playwright exits 0. 9l keeps Playwright's verdict and reports in the receipt
// that the expectation came from a runtime test.fail(), not a declaration.
test.afterEach(() => {
  test.fail();
});

test('a failing assertion an after hook relabels as expected', async ({page}) => {
  await page.setContent('<p role="status">Order rejected</p>');
  await expect(page.getByRole('status')).toHaveText('Order confirmed', {timeout: 100});
});
