import {createTargetTest} from '@9l/playwright';
import {androidTargetDriver} from '@9l/playwright/android';
import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';

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

test('native isolation: fail while still signed in', async ({app}) => {
  await app.open({timeoutMs: 180000});
  await app.reset();
  await app.getByTestId('session.status').toHaveText('Signed out', 90000);
  await app.getByTestId('sign-in').tap();
  await app.getByTestId('session.status').toHaveText('Signed in');
  // Intentional unexpected failure: Playwright must tear down this worker.
  await app.getByTestId('session.status').toHaveText('Signed out', 1000);
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
