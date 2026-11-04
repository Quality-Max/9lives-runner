import {createTargetTest, type TargetDriver} from '@9l/playwright';
import {androidDefaults, androidTargetDriver} from '@9l/playwright/android';
import {readFileSync, writeFileSync} from 'node:fs';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {setTimeout as delay} from 'node:timers/promises';
import {resolve} from 'node:path';

const mode = process.env.NINELIVES_ANDROID_CASE ?? 'healthy';
// Attached-device mode (scripts/android-phone-smoke.cjs): the fixture app was
// installed with installInPlace; the serial stays in the environment.
const attachedSerial = process.env.NINELIVES_ANDROID_SERIAL;
const biometric = mode.startsWith('biometric');
const fixtureMode = attachedSerial ? 'phone-a' : mode === 'cancel' ? 'blocked' : biometric ? 'biometric' : ['defect', 'state-defect', 'missing', 'ambiguous', 'blocked', 'install-fail', 'semantics', 'semantics-defect'].includes(mode) ? mode : 'healthy';
const fixture = JSON.parse(readFileSync(resolve(__dirname, `../../../.context/android-fixture/${fixtureMode}/build.json`), 'utf8'));
let native: TargetDriver;
const test = createTargetTest({native: target => native = androidTargetDriver({
  appId: target.appId,
  // sdkRoot is omitted: the driver discovers it as `9l doctor --android` does.
  toolsRoot: resolve(__dirname, mode === 'version' ? '../../../.context/android-bad-version' : '../../../tools/android'),
  ...(attachedSerial ? {deviceSerial: attachedSerial, apkSHA256: fixture.apkSHA256} : {
    avd: mode === 'unavailable' ? '9l_missing_avd' : biometric ? process.env.NINELIVES_ANDROID_BIOMETRIC_AVD! : process.env.NINELIVES_ANDROID_AVD ?? 'QualityMax_API_35',
    // A dedicated AVD prepared by scripts/prepare-biometric-avd.cjs.
    ...(biometric ? {biometrics: {snapshot: process.env.NINELIVES_ANDROID_BIOMETRIC_SNAPSHOT ?? '9l-biometric'}} : {}),
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
  // The accessibility shapes a Flutter app reports: a hint-only field and
  // parts of a merged multi-line label (one with regex metacharacters).
  ...(mode.startsWith('semantics') ? {controls: {
    email: {hint: 'Email address', className: 'android.widget.EditText'},
    unread: {accessibilityIdContains: '3 unread'},
    inboxTab: {accessibilityIdSuffix: 'Tab 2 of 4 (new)'},
    otherTab: {accessibilityIdSuffix: 'Tab 3 of 4 (new)'},
  }} : {}),
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
  const sdk = androidDefaults().sdkRoot ?? '';
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
    // Android lifecycle operations default to a minute.
    if (!attachedSerial) await app.reset();
    const viewport = await app.viewport();
    const open = await app.getByTestId('open').bounds();
    if (!(viewport.width > 0 && viewport.height > 0 && open.width > 0 && open.height > 0)) throw new Error('unexpected geometry');
    await app.tapAt({x: open.x + Math.floor(open.width / 2), y: open.y + Math.floor(open.height / 2)});
    // A hosted emulator can take seconds to draw the dialog after the touch.
    await app.getByTestId('dialog').toBeVisible(15000);
    if (mode === 'diagnostics' || mode === 'phone-diagnostics') await app.getByTestId('dialog').toBeHidden(3000);
    await app.back();
    await app.getByTestId('dialog').toBeHidden();
    // A query for a control that is gone answers false and the test goes on.
    if (await app.getByTestId('dialog').isVisible()) throw new Error('dismissed dialog still visible');
    if (await app.foreground() !== 'app') throw new Error('app not in front');
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
    if (!attachedSerial) {
      // The spec's own adb, on the serial the driver booted, puts a system
      // window (Settings) in front; the driver reads it without acting on it.
      const device = app.deviceSerial();
      if (device !== serial) throw new Error('deviceSerial differs from the session device');
      await promisify(execFile)(resolve(androidDefaults().sdkRoot ?? '', 'platform-tools/adb'), ['-s', device, 'shell', 'am', 'start', '-W', '-a', 'android.settings.SETTINGS'], {timeout: 30000, maxBuffer: 64 * 1024});
      if (await app.foreground() !== 'foreign') throw new Error('Settings not in front');
      await app.activate();
      if (!await app.getByTestId('send').isVisible({timeoutMs: 30000})) throw new Error('app not back');
      if (await app.foreground() !== 'app') throw new Error('app not in front after activate');
    }
    await app.terminate();
    await app.getByTestId('send').toBeHidden();
    await app.activate();
    // activateApp returns before a cold relaunch has drawn the app; a slow
    // CI emulator can take longer than the default assertion bound. Android
    // assertions accept up to 180 seconds.
    await app.getByTestId('send').toBeVisible(90000);
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
      try { await promisify(execFile)(resolve(androidDefaults().sdkRoot ?? '', 'platform-tools/adb'), ['-s', serial, 'get-state'], {timeout: 1000, maxBuffer: 1024}); }
      catch { deviceStopped = true; }
    }
    if (process.env.NINELIVES_ANDROID_PROBE) writeFileSync(process.env.NINELIVES_ANDROID_PROBE,
      JSON.stringify({started: false, clickDispatches: 0, deviceStopped, stopReason, driverReason, readTiming: {}}));
  }
});

// Flutter-shaped semantics on the owned emulator: bare identifiers (with
// `.` and `-`), a hint-only field, parts of a merged multi-line label and a
// field error reported as content-invalid. semantics-defect never reports
// the error, so the invalid-state assertion must reject it.
test('native semantics: bare ids, hints, merged labels and validation state', async ({app}) => {
  let stopReason = '', driverReason = '', serial = '';
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (input, init) => {
    if (String(input).endsWith('/session') && init?.method === 'POST') serial = JSON.parse(String(init.body)).capabilities.alwaysMatch['appium:udid'];
    return originalFetch(input, init);
  };
  try {
    await app.open({timeoutMs: 180000});
    await app.reset({timeoutMs: 30000});
    await app.getByTestId('unread').toBeVisible();
    await app.getByTestId('inboxTab').toBeVisible();
    await app.getByTestId('otherTab').toBeHidden();
    await app.getByTestId('email').toHaveState('invalid', false);
    await app.getByTestId('email').fill('ada', {timeoutMs: 20000});
    await app.getByTestId('validate').tap();
    await app.getByTestId('login.status').toHaveText('Invalid');
    // @9l-outcome invalid-field-reported
    await app.getByTestId('email').toHaveState('invalid', true);
    await app.getByTestId('email').fill('ada@example.test', {timeoutMs: 20000});
    await app.getByTestId('validate').tap();
    await app.getByTestId('email').toHaveState('invalid', false);
    await app.getByTestId('login-button').tap();
    // @9l-outcome bare-id-sign-in
    await app.getByTestId('login.status').toHaveText('Signed in as ada@example.test');
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
      try { await promisify(execFile)(resolve(androidDefaults().sdkRoot ?? '', 'platform-tools/adb'), ['-s', serial, 'get-state'], {timeout: 1000, maxBuffer: 1024}); }
      catch { deviceStopped = true; }
    }
    if (process.env.NINELIVES_ANDROID_PROBE) writeFileSync(process.env.NINELIVES_ANDROID_PROBE,
      JSON.stringify({started: false, clickDispatches: 0, deviceStopped, stopReason, driverReason, readTiming: {}}));
  }
});

// A sign-in behind the system BiometricPrompt on a prepared emulator: the
// prompt is a window of another package in front of the app, the enrolled
// finger authenticates and the app is back in front. biometric-unenrolled
// touches finger 2, which is not enrolled, and must fail the assertion.
test('native biometrics: a fingerprint at the system prompt', async ({app}) => {
  let stopReason = '', driverReason = '', serial = '';
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (input, init) => {
    if (String(input).endsWith('/session') && init?.method === 'POST') serial = JSON.parse(String(init.body)).capabilities.alwaysMatch['appium:udid'];
    return originalFetch(input, init);
  };
  try {
    await app.open({timeoutMs: 180000});
    await app.reset();
    await app.getByTestId('sign-in').tap();
    let front = await app.foreground();
    for (let i = 0; front !== 'foreign' && i < 20; i++) { await delay(500); front = await app.foreground(); }
    if (front !== 'foreign') throw new Error('the biometric prompt is not in front');
    await app.fingerprint(mode === 'biometric-unenrolled' ? 2 : 1);
    // @9l-outcome biometric-sign-in
    await app.getByTestId('auth.status').toHaveText('Authenticated', mode === 'biometric-unenrolled' ? 3000 : 15000);
    if (await app.foreground() !== 'app') throw new Error('the app is not back in front');
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
      try { await promisify(execFile)(resolve(androidDefaults().sdkRoot ?? '', 'platform-tools/adb'), ['-s', serial, 'get-state'], {timeout: 1000, maxBuffer: 1024}); }
      catch { deviceStopped = true; }
    }
    if (process.env.NINELIVES_ANDROID_PROBE) writeFileSync(process.env.NINELIVES_ANDROID_PROBE,
      JSON.stringify({started: false, clickDispatches: 0, deviceStopped, stopReason, driverReason, readTiming: {}}));
  }
});
