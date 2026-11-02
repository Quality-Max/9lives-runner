import {createTargetTest, type TargetDriver} from '@9l/playwright';
import {androidTargetDriver} from '@9l/playwright/android';
import {readFileSync, writeFileSync} from 'node:fs';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {setTimeout as delay} from 'node:timers/promises';
import {resolve} from 'node:path';

const mode = process.env.NINELIVES_ANDROID_CASE ?? 'healthy';
// Attached-device mode (scripts/android-phone-smoke.cjs): the fixture app was
// installed with installInPlace; the serial stays in the environment.
const attachedSerial = process.env.NINELIVES_ANDROID_SERIAL;
const fixtureMode = attachedSerial ? 'phone-a' : mode === 'cancel' ? 'blocked' : ['defect', 'state-defect', 'missing', 'ambiguous', 'blocked', 'install-fail'].includes(mode) ? mode : 'healthy';
const fixture = JSON.parse(readFileSync(resolve(__dirname, `../../../.context/android-fixture/${fixtureMode}/build.json`), 'utf8'));
let native: TargetDriver;
const test = createTargetTest({native: target => native = androidTargetDriver({
  appId: target.appId,
  sdkRoot: process.env.ANDROID_HOME ?? '/usr/local/share/android-commandlinetools',
  toolsRoot: resolve(__dirname, mode === 'version' ? '../../../.context/android-bad-version' : '../../../tools/android'),
  ...(attachedSerial ? {deviceSerial: attachedSerial, apkSHA256: fixture.apkSHA256} : {
    avd: mode === 'unavailable' ? '9l_missing_avd' : process.env.NINELIVES_ANDROID_AVD ?? 'QualityMax_API_35',
    apk: fixture.apk, apkSHA256: mode === 'digest' ? '0'.repeat(64) : fixture.apkSHA256,
  }),
  appPackage: fixture.appPackage, activity: fixture.activity,
  // Opt-in diagnostics and declared requirements for their qualification cases.
  ...(mode === 'diagnostics' || mode === 'phone-diagnostics' ? {diagnostics: {gestures: true}} : {}),
  ...(mode === 'phone-healthy' ? {expected: {versionCode: '7', versionName: '1.4.0', signerSHA256: process.env.NINELIVES_ANDROID_SIGNER}} : {}),
  ...(mode === 'phone-dirty-recovered' ? {recovery: {back: 2}} : {}),
  ...(mode === 'phone-posture' ? {requires: {posture: 'opened'}} : {}),
  ...(mode === 'phone-width' ? {requires: {minWidthDp: 600}} : {}),
  ...(mode === 'width-mismatch' ? {requires: {minWidthDp: 10000}} : {}),
  ...(mode === 'version-mismatch' ? {expected: {versionCode: '999'}} : {}),
})}).extend({
  page: async () => { throw new Error('native requested a browser page'); },
  context: async () => { throw new Error('native requested a browser context'); },
  browser: async () => { throw new Error('native requested a browser'); },
});

// @9l-requirement QUA-2204
test('native transfer has fresh exact text and state', async ({app}) => {
  // Qualification-only transport probe: no server bodies, hierarchy or device logs.
  // Fault injection holds the real click response until abort. Appium can
  // acknowledge dispatch before the app finishes its onClick handler.
  // Cancellation waits for the app's marker, never a timer before dispatch.
  const originalFetch = globalThis.fetch;
  let stopReason = '', driverReason = '';
  let serial = '', started = false, clickDispatches = 0;
  const readTiming: Record<string, {count: number; errors: number; maxMs: number}> = {};
  const sdk = process.env.ANDROID_HOME ?? '/usr/local/share/android-commandlinetools';
  const command = promisify(execFile);
  let probe: Promise<void> | undefined;
  globalThis.fetch = async (input, init) => {
    const url = String(input);
    if (url.endsWith('/session') && init?.method === 'POST') {
      serial = JSON.parse(String(init.body)).capabilities.alwaysMatch['appium:udid'];
    }
    const route = url.endsWith('/elements') ? 'find' : url.endsWith('/text') ? 'text' : url.endsWith('/displayed') ? 'visible' : /\/attribute\/(enabled|checked)$/.test(url) ? 'state' : '';
    const requestStart = Date.now();
    const pending = originalFetch(input, init);
    if (route) {
      const timing = readTiming[route] ??= {count: 0, errors: 0, maxMs: 0};
      timing.count++;
      const finished = (failed: boolean) => {
        if (failed) timing.errors++;
        timing.maxMs = Math.max(timing.maxMs, Date.now() - requestStart);
      };
      void pending.then(() => finished(false), () => finished(true));
    }
    if (/\/element\/[^/]+\/click$/.test(url) && init?.method === 'POST') {
      clickDispatches++;
      if (mode === 'cancel' || mode === 'blocked') probe = (async () => {
        for (let i = 0; i < 80; i++) {
          try {
            await command(resolve(sdk, 'platform-tools/adb'), ['-s', serial, 'shell', 'run-as', fixture.appPackage, 'test', '-f', 'files/started'], {timeout: 1000, maxBuffer: 1024});
            started = true;
            if (mode === 'cancel') app.cancel();
            return;
          } catch { await delay(50); }
        }
      })();
      if (mode === 'cancel' || mode === 'blocked') {
        // Hold both acknowledgements and errors: this control specifically
        // injects an unresponsive transport after a real native dispatch.
        void pending.catch(() => {});
        const signal = init?.signal;
        await new Promise<never>((_, reject) => {
          const aborted = () => reject(new Error('synthetic stalled transport aborted'));
          if (signal?.aborted) aborted();
          else signal?.addEventListener('abort', aborted, {once: true});
        });
      }
    }
    return pending;
  };
  try {
  await app.open({timeoutMs: 180000});
  if (mode === 'disconnect') await native.close();
  // An attached phone keeps its app data; the fields are refilled below.
  if (!attachedSerial) await app.reset({timeoutMs: 30000});
  await app.getByTestId('recipient').fill('Ada', {timeoutMs: 20000});
  await app.getByTestId('amount').fill('5', {timeoutMs: 20000});
  // Strict mutations must reject missing/ambiguous controls before dispatch.
  if (mode === 'missing' || mode === 'ambiguous') await app.getByTestId('send').tap();
  await app.getByTestId('send').toHaveState('enabled', true);
  await app.getByTestId('confirmed').toHaveState('checked', false);
  if (mode === 'cancel') {
    await app.getByTestId('send').tap({timeoutMs: 15000});
  } else {
    await app.getByTestId('send').tap(mode === 'blocked' ? {timeoutMs: 5000} : {});
  }
  // @9l-outcome visible-native-receipt
  await app.getByTestId('receipt').toBeVisible();
  // @9l-outcome exact-native-transfer
  await app.getByTestId('receipt').toHaveText('Sent $5 to Ada');
  // @9l-outcome completed-native-transfer
  await app.getByTestId('confirmed').toHaveState('checked', true);
  } catch (error) {
    const e = error as {reason?: unknown; cause?: {message?: unknown}};
    if (typeof e.reason === 'string' && /^[a-z_]{1,64}$/.test(e.reason)) stopReason = e.reason;
    const cause = e.cause?.message;
    if (typeof cause === 'string' && /^9lives Android: [a-z_]{1,80}$/.test(cause)) driverReason = cause.slice(16);
    throw error;
  } finally {
    globalThis.fetch = originalFetch;
    await app.close();
    await probe;
    let deviceStopped = false;
    if (serial) {
      try { await command(resolve(sdk, 'platform-tools/adb'), ['-s', serial, 'get-state'], {timeout: 1000, maxBuffer: 1024}); }
      catch { deviceStopped = true; }
    }
    // Only bounded synthetic probe facts; no serial/handles or device output.
    if (process.env.NINELIVES_ANDROID_PROBE) writeFileSync(process.env.NINELIVES_ANDROID_PROBE,
      JSON.stringify({started, clickDispatches, deviceStopped, stopReason, driverReason, readTiming}));
  }
});

// Native operations on the owned emulator: geometry, a touch at a point that
// opens a dialog window, Back, a hidden assertion, scrolling, the keyboard
// read and the app lifecycle. With diagnostics, a hidden assertion against
// the open dialog fails and records the real window counts.
test('native operations: touch, back, scroll, keyboard and lifecycle', async ({app}) => {
  const originalFetch = globalThis.fetch;
  let stopReason = '', driverReason = '', serial = '';
  globalThis.fetch = async (input, init) => {
    if (String(input).endsWith('/session') && init?.method === 'POST') serial = JSON.parse(String(init.body)).capabilities.alwaysMatch['appium:udid'];
    return originalFetch(input, init);
  };
  try {
    await app.open({timeoutMs: 180000});
    if (!attachedSerial) await app.reset({timeoutMs: 30000});
    const viewport = await app.viewport();
    const open = await app.getByTestId('open').bounds();
    if (!(viewport.width > 0 && viewport.height > 0 && open.width > 0 && open.height > 0)) throw new Error('unexpected geometry');
    await app.tapAt({x: open.x + Math.floor(open.width / 2), y: open.y + Math.floor(open.height / 2)});
    await app.getByTestId('dialog').toBeVisible();
    if (mode === 'diagnostics' || mode === 'phone-diagnostics') await app.getByTestId('dialog').toBeHidden(3000);
    await app.back();
    await app.getByTestId('dialog').toBeHidden();
    await app.getByTestId('list').scroll('down', {percent: 0.5});
    await app.getByTestId('status').toHaveText('scrolled');
    if (attachedSerial) {
      // A phone keeps its own input method: touching the field shows it
      // (fill sets the text without opening it), hiding closes it.
      await app.getByTestId('amount').fill('7');
      await app.getByTestId('amount').tap();
      await app.toHaveKeyboard(true);
      await app.hideKeyboard();
      await app.toHaveKeyboard(false);
    } else {
      await app.toHaveKeyboard(false);
      await app.hideKeyboard();
    }
    await app.terminate();
    await app.getByTestId('send').toBeHidden();
    await app.activate();
    // activateApp returns before a cold relaunch has drawn the app; a slow
    // CI emulator can take longer than the default assertion bound.
    await app.getByTestId('send').toBeVisible(30000);
  } catch (error) {
    const e = error as {reason?: unknown; cause?: {message?: unknown}};
    if (typeof e.reason === 'string' && /^[a-z_]{1,64}$/.test(e.reason)) stopReason = e.reason;
    const cause = e.cause?.message;
    if (typeof cause === 'string' && /^9lives Android: [a-z_]{1,80}$/.test(cause)) driverReason = cause.slice(16);
    throw error;
  } finally {
    globalThis.fetch = originalFetch;
    await app.close();
    let deviceStopped = false;
    if (serial) {
      try { await promisify(execFile)(resolve(process.env.ANDROID_HOME ?? '/usr/local/share/android-commandlinetools', 'platform-tools/adb'), ['-s', serial, 'get-state'], {timeout: 1000, maxBuffer: 1024}); }
      catch { deviceStopped = true; }
    }
    if (process.env.NINELIVES_ANDROID_PROBE) writeFileSync(process.env.NINELIVES_ANDROID_PROBE,
      JSON.stringify({started: false, clickDispatches: 0, deviceStopped, stopReason, driverReason, readTiming: {}}));
  }
});
