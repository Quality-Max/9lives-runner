import {test} from '@9l/playwright';
import {renameSync, writeFileSync} from 'node:fs';
test('owned browser work can be interrupted', async ({page, n9l}) => {
  await page.setContent('<p role="status">Waiting</p>');
  // Account-free smoke records only a PID, after real browser work starts.
  if (process.env.NINELIVES_SMOKE_MARKER) {
    const marker = process.env.NINELIVES_SMOKE_MARKER;
    writeFileSync(`${marker}.tmp`, JSON.stringify({workerPID: process.pid}));
    renameSync(`${marker}.tmp`, marker);
  }
  await n9l.step('wait for a result', async () => {
    await page.waitForTimeout(60000);
  });
});
