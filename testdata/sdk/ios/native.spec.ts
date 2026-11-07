import {createTargetTest} from '@9l/playwright';
import {iosTargetDriver} from '@9l/playwright/ios';
import {readFileSync, writeFileSync, existsSync} from 'node:fs';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {setTimeout as delay} from 'node:timers/promises';
import {resolve, join} from 'node:path';

const mode = process.env.NINELIVES_IOS_CASE ?? 'healthy';
const variant = ['defect', 'missing', 'ambiguous', 'label-only'].includes(mode) ? mode : 'healthy';
const fixture = JSON.parse(readFileSync(resolve(__dirname, `../../../.context/ios-fixture/${variant}/build.json`), 'utf8'));
const test = createTargetTest({native: target => iosTargetDriver({
  appId: target.appId, app: fixture.app, bundleId: fixture.bundleId,
  appSHA256: mode === 'digest' ? '0'.repeat(64) : fixture.appSHA256,
  runtime: mode === 'unavailable' ? 'com.apple.CoreSimulator.SimRuntime.iOS-0-0' : 'com.apple.CoreSimulator.SimRuntime.iOS-18-5',
  deviceType: 'com.apple.CoreSimulator.SimDeviceType.iPhone-16',
  toolsRoot: resolve(__dirname, '../../../tools/ios'),
})}).extend({
  page: async () => { throw new Error('iOS requested a browser page'); },
  context: async () => { throw new Error('iOS requested a browser context'); },
  browser: async () => { throw new Error('iOS requested a browser'); },
});

// QUA-2108 requires matching platform/build and assertions; QUA-2167 supplies
// the owned local device slice. These annotations are coverage claims only.
// @9l-requirement QUA-2167
// @9l-requirement QUA-2108
test('iOS transfer checks the exact native receipt', async ({app}) => {
  const originalFetch = globalThis.fetch;
  const command = promisify(execFile);
  let udid = '', clickDispatches = 0, started = false, stopReason = '';
  let probe: Promise<void> | undefined;
  globalThis.fetch = async (input, init) => {
    const url = String(input);
    if (url.endsWith('/session') && init?.method === 'POST') udid = JSON.parse(String(init.body)).capabilities.alwaysMatch['appium:udid'];
    const pending = originalFetch(input, init);
    if (/\/element\/[^/]+\/click$/.test(url) && init?.method === 'POST') {
      clickDispatches++;
      if (mode === 'cancel' || mode === 'blocked') {
        probe = (async () => {
          const {stdout} = await command('/usr/bin/xcrun', ['simctl', 'get_app_container', udid, fixture.bundleId, 'data'], {timeout: 3000, maxBuffer: 16384});
          for (let i = 0; i < 100; i++) {
            if (existsSync(join(stdout.trim(), 'Documents/started'))) {
              started = true;
              if (mode === 'cancel') app.cancel();
              return;
            }
            await delay(50);
          }
        })();
        void probe.catch(() => {}); // Rejection is checked in finally.
        // Fault injection: the real app mutation runs, but its HTTP response
        // is held until abort. Never retry the mutation or retain its payload.
        void pending.catch(() => {});
        await new Promise<never>((_, reject) => {
          const signal = init?.signal;
          const aborted = () => reject(new Error('held native response aborted'));
          if (signal?.aborted) aborted(); else signal?.addEventListener('abort', aborted, {once: true});
        });
      }
    }
    return pending;
  };
  try {
    await app.open({timeoutMs: 180000});
    await app.reset();
    await app.getByTestId('amount').fill('5', {timeoutMs: 20000});
    // Missing IDs, duplicates and a label-only decoy must fail before a tap.
    if (['missing', 'ambiguous', 'label-only'].includes(mode)) await app.getByTestId('send').tap();
    await app.getByTestId('send').toHaveState('enabled', true);
    await app.getByTestId('send').tap({timeoutMs: mode === 'blocked' ? 10000 : 15000});
    // @9l-outcome visible-native-receipt
    await app.getByTestId('receipt').toBeVisible();
    // @9l-outcome exact-native-transfer
    await app.getByTestId('receipt').toHaveText('Sent $5');
  } catch (error) {
    const reason = (error as {reason?: unknown}).reason;
    if (typeof reason === 'string' && /^[a-z_]{1,64}$/.test(reason)) stopReason = reason;
    throw error;
  } finally {
    globalThis.fetch = originalFetch;
    await app.close();
    await probe;
    let deviceStopped = false;
    if (udid) {
      const {stdout} = await command('/usr/bin/xcrun', ['simctl', 'list', 'devices', '-j'], {timeout: 5000, maxBuffer: 256 * 1024});
      const devices = JSON.parse(stdout).devices;
      deviceStopped = !Object.values(devices).some(list => (list as {udid: string}[]).some(d => d.udid === udid));
    }
    if (process.env.NINELIVES_IOS_PROBE) writeFileSync(process.env.NINELIVES_IOS_PROBE,
      JSON.stringify({clickDispatches, started, deviceStopped, stopReason}));
  }
});
