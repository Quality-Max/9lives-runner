import {createTargetTest} from '@9l/playwright';
import {iosTargetDriver} from '@9l/playwright/ios';
import {readFileSync, writeFileSync, existsSync} from 'node:fs';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {setTimeout as delay} from 'node:timers/promises';
import {resolve, join} from 'node:path';

const mode = process.env.NINELIVES_IOS_CASE ?? 'healthy';
const biometric = ['biometric', 'biometric-nomatch', 'biometric-unenrolled'].includes(mode);
const login = ['login', 'login-covered', 'login-hide'].includes(mode);
const variant = ['defect', 'missing', 'ambiguous', 'label-only'].includes(mode) ? mode : biometric ? 'biometric' : login ? 'login' : 'healthy';
const fixture = JSON.parse(readFileSync(resolve(__dirname, `../../../.context/ios-fixture/${variant}/build.json`), 'utf8'));
const wda = JSON.parse(readFileSync(resolve(__dirname, '../../../.context/ios-wda/build.json'), 'utf8'));
// The smoke names one installed runtime explicitly; nothing selects "latest".
const os = process.env.NINELIVES_IOS_OS ?? '18.6';
if (!/^[0-9]{1,3}(\.[0-9]{1,5}){1,2}$/.test(os)) throw new Error('invalid NINELIVES_IOS_OS');
const stoppedWorker = ['runner-cancel', 'attempt-timeout', 'worker-kill'].includes(mode);
const facts = {driverReason: '', clickDispatches: 0, started: false, deviceStopped: false, stopReason: '', setupPhase: '', deviceReady: false, workerPid: 0,
  commands: [] as {operation: string; elapsedMs: number; outcome: 'pending' | 'passed' | 'failed'}[]};
const persistProbe = () => {
  if (process.env.NINELIVES_IOS_PROBE) writeFileSync(process.env.NINELIVES_IOS_PROBE, JSON.stringify(facts));
};
const test = createTargetTest({native: target => {
  const driver = iosTargetDriver({
    appId: target.appId, app: fixture.app, bundleId: fixture.bundleId,
    appSHA256: mode === 'digest' ? '0'.repeat(64) : fixture.appSHA256,
    runtime: mode === 'unavailable' ? 'com.apple.CoreSimulator.SimRuntime.iOS-0-0' : `com.apple.CoreSimulator.SimRuntime.iOS-${os.replaceAll('.', '-')}`,
    deviceType: 'com.apple.CoreSimulator.SimDeviceType.iPhone-16',
    toolsRoot: resolve(__dirname, '../../../tools/ios'), wdaProducts: wda.products,
    ...(biometric ? {biometrics: {enrolled: mode !== 'biometric-unenrolled'}} : {}),
  });
  const open = driver.open.bind(driver);
  driver.open = async context => {
    try { await open(context); } catch (error) {
      // Observe the driver's eventual rejection even if TargetApp's timeout
      // wins first. Persist before teardown, which can itself fail.
      const message = (error as {cause?: {message?: unknown}}).cause?.message;
      if (typeof message === 'string') facts.setupPhase = /^9lives iOS: open_(tools|artifact|wda|runtime|create|boot|bootstatus|architecture|install|server|session|metadata)$/.exec(message)?.[1] ?? '';
      persistProbe();
      throw error;
    }
  };
  return driver;
}}).extend({
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
  const childProcess = require('node:child_process') as {execFile: typeof execFile};
  const originalExecFile = childProcess.execFile;
  // Record fixed command names, duration and outcome only. Never retain
  // arguments, output or errors; those can contain app/provider data.
  childProcess.execFile = ((file: string, args: string[], options: any, callback: (...result: any[]) => void) => {
    const operation = file === '/usr/bin/xcrun' && args[0] === 'simctl' &&
      ['list', 'create', 'boot', 'bootstatus', 'getenv', 'install', 'get_app_container', 'shutdown', 'delete'].includes(args[1]) ? args[1] : '';
    if (!operation || facts.commands.length >= 64) return originalExecFile(file, args, options, callback);
    const started = Date.now();
    const entry = {operation, elapsedMs: 0, outcome: 'pending' as 'pending' | 'passed' | 'failed'};
    facts.commands.push(entry);persistProbe();
    return originalExecFile(file, args, options, (...result: any[]) => {
      entry.elapsedMs = Math.min(300000, Math.max(0, Date.now() - started));
      entry.outcome = result[0] ? 'failed' : 'passed';
      persistProbe();callback(...result);
    });
  }) as typeof execFile;
  let udid = '';
  let probe: Promise<void> | undefined;
  globalThis.fetch = async (input, init) => {
    const url = String(input);
    if (url.endsWith('/session') && init?.method === 'POST') udid = JSON.parse(String(init.body)).capabilities.alwaysMatch['appium:udid'];
    const pending = originalFetch(input, init);
    if (/\/element\/[^/]+\/click$/.test(url) && init?.method === 'POST') {
      facts.clickDispatches++;
      if (mode === 'cancel' || mode === 'blocked') {
        probe = (async () => {
          const {stdout} = await command('/usr/bin/xcrun', ['simctl', 'get_app_container', udid, fixture.bundleId, 'data'], {timeout: 3000, maxBuffer: 16384});
          for (let i = 0; i < 100; i++) {
            if (existsSync(join(stdout.trim(), 'Documents/started'))) {
              facts.started = true;
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
    await app.open({timeoutMs: 300000});
    if (stoppedWorker) {
      // This worker is stopped from outside while it holds a booted device,
      // so the teardown below never runs. Only the engine can remove it.
      facts.deviceReady = true; facts.workerPid = process.pid; persistProbe();
      await delay(420000);
      throw new Error('worker was not stopped');
    }
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
    if (typeof reason === 'string' && /^[a-z_]{1,64}$/.test(reason)) facts.stopReason = reason;
    persistProbe();
    throw error;
  } finally {
    globalThis.fetch = originalFetch;
    try {
      await app.close();
      await probe;
      if (udid) {
        const {stdout} = await command('/usr/bin/xcrun', ['simctl', 'list', 'devices', '-j'], {timeout: 30000, maxBuffer: 256 * 1024});
        const devices = JSON.parse(stdout).devices;
        facts.deviceStopped = !Object.values(devices).some(list => (list as {udid: string}[]).some(d => d.udid === udid));
      }
    } finally { childProcess.execFile = originalExecFile;persistProbe(); }
  }
});

// A sign-in behind the system Face ID prompt on an owned simulator: the
// driver enrolls Face ID and allows it for the app before launch, the test
// presents the enrolled face and the app reports the sign-in.
// biometric-nomatch presents a face that is not enrolled and
// biometric-unenrolled runs without enrollment; both must fail the assertion.
test('iOS biometrics: Face ID at the system prompt', async ({app}) => {
  try {
    await app.open({timeoutMs: 300000});
    await app.getByTestId('sign-in').tap();
    if (mode !== 'biometric-unenrolled') await app.getByTestId('auth.status').toHaveText('Prompting', 10000);
    await app.biometric(mode === 'biometric-nomatch' ? 'nomatch' : 'match', {timeoutMs: 30000});
    // @9l-outcome biometric-sign-in
    await app.getByTestId('auth.status').toHaveText('Authenticated', mode === 'biometric' ? 15000 : 5000);
  } catch (error) {
    const reason = (error as {reason?: unknown}).reason;
    if (typeof reason === 'string' && /^[a-z_]{1,64}$/.test(reason)) facts.stopReason = reason;
    throw error;
  } finally {
    try { await app.close(); } finally { persistProbe(); }
  }
});

// A password login and the screen after it: Return submits the password,
// the keyboard is read, and a system permission alert is answered with a
// named button. login-covered leaves the keyboard over a button at the
// bottom of the screen: the tap must be refused before any click, not land
// on the keyboard. login-hide asks to hide an iPhone keyboard, which has no
// key that only closes it: that must fail, not tap a control of the app.
test('iOS login: submit, keyboard and a system alert', async ({app}) => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (input, init) => {
    if (/\/element\/[^/]+\/click$/.test(String(input)) && init?.method === 'POST') facts.clickDispatches++;
    return originalFetch(input, init);
  };
  try {
    await app.open({timeoutMs: 300000});
    await app.getByTestId('username').fill('ada@example.com');
    await app.toHaveKeyboard(true);
    await app.getByTestId('password').fill('correct-horse', {submit: true});
    await app.getByTestId('home.title').toBeVisible(15000);
    await app.toHaveKeyboard(false);
    if (mode === 'login') {
      await app.getByTestId('notify').tap();
      await app.answerAlert('Allow', {timeoutMs: 15000});
      // @9l-outcome notifications-allowed
      await app.getByTestId('home.status').toHaveText('Notifications on', 10000);
      return;
    }
    // Return keeps this field's keyboard up, as many real forms do.
    await app.getByTestId('note').fill('x', {submit: true});
    await app.toHaveKeyboard(true);
    // Nothing before this point clicks (fill sets the value), so any click
    // the smoke counts came from the step that must refuse.
    if (mode === 'login-hide') await app.hideKeyboard();
    else await app.getByTestId('continue').tap();
  } catch (error) {
    const e = error as {reason?: unknown; cause?: {message?: unknown}};
    if (typeof e.reason === 'string' && /^[a-z_]{1,64}$/.test(e.reason)) facts.stopReason = e.reason;
    const cause = e.cause?.message;
    if (typeof cause === 'string' && /^9lives iOS: [a-z_]{1,80}$/.test(cause)) facts.driverReason = cause.slice(12);
    throw error;
  } finally {
    globalThis.fetch = originalFetch;
    try { await app.close(); } finally { persistProbe(); }
  }
});
