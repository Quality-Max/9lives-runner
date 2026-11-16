import {test, expect} from '@playwright/test';
import {renameSync, writeFileSync} from 'node:fs';

test('owned webServer can be interrupted and restarted', async ({page}) => {
  await page.goto(`http://127.0.0.1:${process.env.NINELIVES_SMOKE_PORT}`);
  await expect(page.getByRole('status')).toHaveText('Ready');
  const marker = process.env.NINELIVES_SMOKE_MARKER!;
  writeFileSync(`${marker}.tmp`, JSON.stringify({workerPID: process.pid}));
  renameSync(`${marker}.tmp`, marker);
  if (process.env.NINELIVES_SMOKE_WAIT === '1') await new Promise(() => {});
});
