import {test, expect} from '@9l/playwright';

test.describe('serial retry compatibility', () => {
  test.describe.configure({mode: 'serial', retries: 1});
  test('previously passed test runs again', async ({page}, testInfo) => {
    await page.setContent('<h1>Ready</h1>');
    await expect(page.getByRole('heading')).toHaveText('Ready');
    expect(testInfo.retry).toBeLessThanOrEqual(1);
  });
  test('one failure triggers the group retry', async ({page}, testInfo) => {
    await page.setContent('<h1>Ready</h1>');
    expect(testInfo.retry).toBe(1);
  });
  test('previously skipped test executes on retry', async ({page}, testInfo) => {
    expect(testInfo.retry).toBe(1);
    await page.setContent('<h1>Recovered</h1>');
    await expect(page.getByRole('heading')).toHaveText('Recovered');
  });
});
