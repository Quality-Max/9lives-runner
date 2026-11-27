import {createTargetTest} from '@9l/playwright';
import {androidDefaults, androidTargetDriver} from '@9l/playwright/android';
import {existsSync, readFileSync, writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {frontWindow, readAppLog} from './device-facts.cjs';

// #244: a failed test must not donate its emulator or private app session to
// the next test. Deliberately omit logout and explicit close in both bodies:
// the real test-scoped fixture owns teardown, including the failure path.
const defect = process.env.NINELIVES_ANDROID_CASE === 'isolation-defect';
const fixture = JSON.parse(readFileSync(resolve(__dirname, '../../../.context/android-fixture/isolation/build.json'), 'utf8'));
const test = createTargetTest({native: target => {
  const driver = androidTargetDriver({
    appId: target.appId,
    toolsRoot: resolve(__dirname, '../../../tools/android'),
    avd: process.env.NINELIVES_ANDROID_AVD ?? 'QualityMax_API_35',
    apk: fixture.apk, apkSHA256: fixture.apkSHA256,
    appPackage: fixture.appPackage, activity: fixture.activity,
  });
  // Negative control: suppress data clearing. The second test must reject the
  // persisted login after terminate/activate proves it survived an app restart.
  if (defect) driver.reset = async () => {};
  return driver;
}}).extend({
  page: async () => { throw new Error('native requested a browser page'); },
  context: async () => { throw new Error('native requested a browser context'); },
  browser: async () => { throw new Error('native requested a browser'); },
});

// Qualification-only probe (#244, #323), outside the test bodies so the
// isolation sequence stays as written: after each test, while its emulator
// is still up (afterEach runs before fixture teardown), read the app's own
// log of what it drew (signed in or out) and which clicks it handled, and on
// a failure which window had input focus. Fixed words and counts only; the
// smoke prints them per test. It never fails a test.
let serial = '';
const originalFetch = globalThis.fetch;
test.beforeEach(() => {
  serial = '';
  globalThis.fetch = async (input, init) => {
    if (String(input).endsWith('/session') && init?.method === 'POST') serial = JSON.parse(String(init.body)).capabilities.alwaysMatch['appium:udid'];
    return originalFetch(input, init);
  };
});
test.afterEach(async ({}, testInfo) => {
  globalThis.fetch = originalFetch;
  const probe = process.env.NINELIVES_ANDROID_PROBE;
  if (!probe || !serial) return;
  const adb = resolve(androidDefaults().sdkRoot ?? '', 'platform-tools/adb');
  const facts: Record<string, unknown> = {app: await readAppLog(adb, serial, fixture.appPackage)};
  if (testInfo.status !== testInfo.expectedStatus) facts.front = await frontWindow(adb, serial, fixture.appPackage);
  try {
    const recorded = existsSync(probe) ? JSON.parse(readFileSync(probe, 'utf8')) : {};
    const tests = {...recorded.tests, [testInfo.title.includes('reset after') ? 'second' : 'first']: facts};
    writeFileSync(probe, JSON.stringify({...recorded, tests}));
  } catch { /* an unwritable probe leaves the facts absent */ }
});

test('native isolation: fail while still signed in', async ({app}) => {
  await app.open({timeoutMs: 180000});
  await app.reset();
  await app.getByTestId('session.status').toHaveText('Signed out', 90000);
  await app.getByTestId('sign-in').tap();
  await app.getByTestId('session.status').toHaveText('Signed in');
  // Intentional unexpected failure: Playwright must tear down this worker.
  // The ordinary bound, not 1 s: a bound that ends before any read completed
  // is an operation timeout, and one lookup alone took up to 1.8 s on the
  // hosted emulator, which ended this step `operation_timeout` (#323).
  await app.getByTestId('session.status').toHaveText('Signed out');
});

test('native isolation: reset after the previous failure clears persisted login', async ({app}) => {
  await app.open({timeoutMs: 180000});
  await app.reset();
  await app.getByTestId('session.status').toHaveText('Signed out', 90000);
  await app.getByTestId('sign-in').tap();
  await app.getByTestId('session.status').toHaveText('Signed in');
  await app.terminate();
  await app.activate();
  await app.getByTestId('session.status').toHaveText('Signed in', 90000);
  await app.reset();
  // A real reset relaunches the app, so this read gets the cold-relaunch
  // allowance. The negative control's reset does nothing: the app stays on
  // screen, signed in, and this assertion can only fail. Polling it for the
  // 90 s allowance ran into the 240 s test budget on a slow hosted emulator,
  // where the steps before it had taken more than 150 s, so the test timed
  // out instead of failing here (#278). It keeps the ordinary bound instead.
  await app.getByTestId('session.status').toHaveText('Signed out', defect ? undefined : 90000);
});
