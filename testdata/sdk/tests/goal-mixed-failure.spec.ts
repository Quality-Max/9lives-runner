import {test, expect} from '@9l/playwright';

test('unconfigured goal cannot establish behavior', async ({n9l}) => {
  if (process.env.NINELIVES_SMOKE_CATCH_GOAL === '1') {
    await expect(n9l.goal('Continue')).rejects.toThrow();
  } else {
    await n9l.goal('Continue');
  }
});

test('independent business assertion still fails', async ({page}) => {
  await page.setContent('<output role="status">0 orders</output>');
  await expect(page.getByRole('status')).toHaveText('1 order', {timeout: 100});
});
