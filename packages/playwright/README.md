# @9l/playwright

Local Playwright fixtures for the [9lives runner](https://github.com/Quality-Max/9lives-runner).
The `n9l` fixture provides `step` and bounded natural-language `goal` methods.
Keep ordinary Playwright assertions to establish the expected behavior. A goal
completing does not verify that behavior.

## Install

Published on [npm](https://www.npmjs.com/package/@9l/playwright):

```sh
npm install --save-dev @9l/playwright@0.5.5 @playwright/test@1.64.1
npm exec playwright install chromium
```

Use Node 24 for the qualified runtime (Node 22 minimum), Playwright 1.61.1
through 1.64.1, and Chromium. macOS, Linux and Windows on amd64/arm64 are the
qualified runner targets (Windows from CLI 0.2.0). The peer range is `@playwright/test` `>=1.61.1 <2`; CI qualifies
every published release from 1.61.1 through 1.64.1 (1.61.1, 1.62.0, 1.62.1,
1.63.0, 1.64.0 and 1.64.1). Later versions, and other browser engines, install
but are unqualified.

The Go `9l` executable is also required. Use the
[CLI installation guide](https://github.com/Quality-Max/9lives-runner/blob/main/docs/install.md)
for release availability or install from source with Go 1.27.3 or newer.
This npm package does not install Go or
bundle the runner, browsers or a hosted service. No platform account is needed.

## Use

```ts
import {test, expect} from '@9l/playwright';

test('checkout confirms one order', async ({page, n9l}) => {
  await page.goto('http://localhost:3000/checkout');
  await n9l.step('place order', async () => {
    await page.getByRole('button', {name: 'Place order'}).click();
    await expect(page.getByRole('status')).toHaveText('Order confirmed');
  });
});
```

Start the example application, then run the selected spec through the Go engine:

```sh
9l run tests/checkout.spec.ts --sdk --format json
```

Use `--pass-env NAME` for explicitly approved application settings. Browser
handles and goal parameter values stay in the worker; provider credentials stay
in Go. The `n9l` fixture requires the engine identity supplied by `--sdk`.
Existing Playwright specs and configuration remain usable independently.

Natural-language goals use finite observed controls and typed actions, with
budgets and a conservative stop policy. See the
[goal contract](https://github.com/Quality-Max/9lives-runner/blob/main/docs/goals.md)
and [SDK bridge](https://github.com/Quality-Max/9lives-runner/blob/main/docs/sdk-bridge.md).
Live provider accuracy, independent behavioral verification, verified replay,
iOS and Android devices beyond those listed below are not qualified features.

## Android (experimental, 0.6.0-beta releases)

The Android target drives an owned emulator or an attached phone through
Appium 3.8.0 and UiAutomator2 8.7.0, with the same `app` fixture a web
target uses. It is qualified on API 35 emulators and one Android 17 phone;
see [Android setup and qualification limits](https://github.com/Quality-Max/9lives-runner/blob/main/docs/android-driver.md).

1. Install the matching beta CLI and `@9l/playwright` together (see
   [beta releases](https://github.com/Quality-Max/9lives-runner/blob/main/docs/releases.md#beta-releases)),
   and the pinned automation tooling once, outside your project:

   ```sh
   npm install --prefix ~/.9l/android-tools --save-exact --ignore-scripts \
     appium@3.8.0 appium-uiautomator2-driver@8.7.0
   ```

   For another location, set `NINELIVES_ANDROID_TOOLS`.

2. Check the host. Doctor reports Java, the SDK, build-tools, AVDs, devices
   and the pinned tooling, each with a fix:

   ```sh
   9l doctor --android
   ```

3. Construct the driver in the shared target fixture and name the Playwright
   project `android:<alias>`:

   ```ts
   import {createTargetTest} from '@9l/playwright';
   import {androidTargetDriver} from '@9l/playwright/android';

   export const test = createTargetTest({
     native: target => androidTargetDriver({
       appId: target.appId, avd: 'Pixel_API_35',
       apk: 'build/app.apk', apkSHA256: '<digest>',
       appPackage: 'com.example.app', activity: '.MainActivity',
     }),
   });
   ```

   ```ts
   // playwright.config.ts
   import {defineConfig} from '@playwright/test';

   export default defineConfig({
     // A cold emulator boot fits in open()'s 180-second bound, which needs
     // a test timeout above Playwright's 30-second default.
     timeout: 240_000,
     // 9l owns attempts: a Playwright retry is refused.
     retries: 0,
     projects: [{name: 'android:app'}],
   });
   ```

4. Run through the engine, forwarding the variables the driver relies on.
   `9l run` passes only a fixed environment to tests:

   ```sh
   9l run tests/login.spec.ts --sdk --target android:app \
     --pass-env ANDROID_HOME,JAVA_HOME,NINELIVES_ANDROID_TOOLS
   ```

   Forward `ANDROID_SDK_ROOT`, `ANDROID_AVD_HOME` or `HOMEBREW_PREFIX` too if
   your SDK or AVDs are found through them. `9l doctor --android` says which.

The driver holds the device's only UI-automation connection while a session
is open, so a spec-side `adb shell uiautomator dump` returns nothing. Use the
fixture's operations instead, and `app.deviceSerial()` for other adb commands.

## License

Apache-2.0. The package includes `LICENSE` and `NOTICE`.
