import {test, expect} from '@playwright/test';
import {writeFileSync} from 'node:fs';

test('owned webServer can be interrupted and restarted', async ({page}) => {
  await page.goto(`http://127.0.0.1:${process.env.NINELIVES_SMOKE_PORT}`);
  await expect(page.getByRole('status')).toHaveText('Ready');
  writeFileSync(process.env.NINELIVES_SMOKE_MARKER!, JSON.stringify({workerPID: process.pid}));
  if (process.env.NINELIVES_SMOKE_WAIT === '1') await new Promise(() => {});
});
