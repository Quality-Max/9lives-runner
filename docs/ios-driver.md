# iOS Simulator (experimental, unreleased)

This source checkout adds a deterministic iOS driver. It is qualified from
source on one host only; unit tests use a synthetic transport and do not
establish iOS support by themselves. No published CLI or SDK version is changed
by this work.

| Qualified | Not qualified |
| --- | --- |
| iOS 18.6 iPhone 16 Simulator on an Apple silicon Mac (M5 Pro, macOS 26.6.2, Xcode 27.2), with Appium 3.8.0 and XCUITest driver 10.43.1 | GitHub-hosted macOS runners, Intel Macs, other Xcode or iOS versions, other device types, real iPhones, goals, healing and verified replay |

On that host all nine smoke cases qualified: the healthy transfer passed with
three target assertions in 39 seconds, a new simulator was usable 15 to 19
seconds after boot, and every owned device was removed. The smoke records the
host's Xcode version and requires the receipt to match it. This is the owned
Simulator foundation of
[QUA-2167](https://linear.app/quality-max/issue/QUA-2167), using build identity,
strict assertions and cleanup requirements from
[QUA-2108](https://linear.app/quality-max/issue/QUA-2108). Neither umbrella issue
is completed by this slice.

## Run from source

Install full Xcode with the iOS 18.6 Simulator runtime. Command Line Tools alone
are insufficient. The driver never downloads a runtime or selects a latest
runtime automatically.

```sh
npm ci
npm ci --prefix tools/ios --ignore-scripts
npm run smoke:ios
```

Before any case the smoke compiles the pinned WebDriverAgent for the Simulator
once (`scripts/build-ios-wda.cjs`, reused while the tools and Xcode are
unchanged), outside every open bound. That took 20 seconds on the qualified
host and 53 to 260 seconds on hosted runners. The step's duration and outcome
are kept in `host-preparation.json`. `NINELIVES_IOS_OS` names another installed
runtime for a comparison run; nothing selects a latest runtime.

### GitHub-hosted macOS runners are unqualified

No case has passed on a hosted runner. Every new simulator pays its own first
boot inside `open`, and there it took most of the 300-second bound:

| Runner | Boot until usable | Install | Open stopped in |
| --- | --- | --- | --- |
| `macos-15-intel`, Xcode 16.4 | 238 s | cut off after 57 s | install |
| `macos-15` (ARM), Xcode 16.4 | 180 s | 31 s | session creation |
| `macos-15` (ARM), Xcode 16.4, headless sessions | 263 s | cut off after 34 s | install |

Booting another simulator of the same runtime beforehand did not shorten the
next one's first boot (254 s, then 205 s). Headless sessions removed a second
boot that the pinned driver was adding, but the first boot alone still leaves
too little of the bound, and it varied from 180 to 263 seconds on the same
runner type. The `iOS Simulator` workflow is therefore manual and qualifies
nothing. Expect `open` to time out on these runners.

The smoke then builds an isolated UIKit fixture and runs nine cases: a healthy
transfer, a deliberately wrong receipt, missing and duplicate controls, a
label-only decoy, a changed build digest, an unavailable runtime, cancellation,
and a held response that reaches its operation timeout. Cancellation and timeout
must start the actual native mutation, dispatch it once and remove the owned
device. Every case checks that an unrelated simulator survives. A healthy run
must have three successful target assertions; the defect must fail the exact
receipt assertion. Sanitized receipts and probe facts live under
`.context/ios-qualification/`. A setup failure does not qualify the scenario.

For your own source-checkout spec:

```ts
import {createTargetTest} from '@9l/playwright';
import {iosTargetDriver, iosAppDigest} from '@9l/playwright/ios';

const app = '/absolute/path/Finance.app';
const test = createTargetTest({native: target => iosTargetDriver({
  appId: target.appId,
  app,
  appSHA256: iosAppDigest(app),
  bundleId: 'dev.example.finance',
  runtime: 'com.apple.CoreSimulator.SimRuntime.iOS-18-6',
  deviceType: 'com.apple.CoreSimulator.SimDeviceType.iPhone-16',
  toolsRoot: '/absolute/path/9lives-runner/tools/ios',
  // The "products" path in .context/ios-wda/build.json, written by
  // scripts/build-ios-wda.cjs.
  wdaProducts: '/absolute/path/ios-wda/derived/Build/Products',
})});

test('transfer receipt', async ({app}) => {
  await app.open();
  await app.reset();
  await app.getByTestId('amount').fill('5');
  await app.getByTestId('send').tap();
  await app.getByTestId('receipt').toHaveText('Sent $5');
});
```

Name the Playwright project `ios:finance`, then use
`9l run path/to/spec.ts --sdk --config path/to/config.ts --target ios:finance`.
Go supplies the private scratch directory and attempt identity. The driver
requires that engine context. For repeatable build selection, record the digest
when producing the app artifact and supply that recorded value to the test.
Computing it at test startup binds that run to the current local artifact.

`wdaProducts` is optional. With it, the driver checks the runner's bundle
identifier and platform before creating a device, copies the products into its
private scratch and has xcodebuild run them without compiling. Build them with
`node scripts/build-ios-wda.cjs` after installing the pinned tools, and rebuild
when those tools or Xcode change. Without it, WebDriverAgent is compiled inside
`open`, which needs a warm build machine to finish within the bound; that path
is not qualified.

Sessions are headless. The simulator is booted without a window and the pinned
driver is told to keep it that way; by default it would shut the device down
and boot it again through the Simulator app. In headless mode that driver
closes a Simulator app window that is already open on the Mac, so expect an
open Simulator window to close when a test starts. WebDriverAgent listens on
`127.0.0.1` only.

## Supported boundary

- An unpacked, signed Simulator `.app` with `DTPlatformName=iphonesimulator`.
  The bounded tree digest includes names, executable bits and bytes. Symlinks,
  special files, more than 4,096 entries or more than 256 MiB are refused. IPA
  files and physical-device builds are unsupported. The copied artifact and
  installed tree must match the declared digest, including after reset.
- One newly created Simulator, Appium process group and private automation home
  per driver. No attachment to an existing device, automatic permission grants,
  onboarding recovery or reuse of another session. Reset uninstalls and installs
  the frozen build again; it does not preserve app data.
- Tap, fill, visibility, text equality and `enabled` state through the shared
  target API. Each action re-observes its control and requires one match.
  Mutations have no driver retries. Back, gestures, keyboard control, lifecycle
  commands outside reset, screenshots, bounds and coordinate actions are not
  implemented; optional operations stop as unsupported.
- Controls use exact accessibility identifiers restricted to the shared test-ID
  alphabet. WDA exposes `name = identifier || label`, without a raw identifier
  attribute. The predicate also excludes matching labels, so a label-only decoy
  cannot match. **An identifier equal to its label is unsupported**, even if a
  real identifier is present. Give controls distinct identifiers and labels.
- Native lifecycle operations have finite bounds (open defaults to 300 seconds;
  reset defaults to 60 seconds). In-worker abort stops the owned server process group and
  shuts down/deletes the owned Simulator. Cleanup bounds each command on its
  own: 15 seconds for shutdown, 30 for deletion and 30 for the inventory that
  must confirm the device is gone. The device is removed by the identifier
  `create` returned, so a slow inventory cannot cost deletion its turn; an
  inventory locates the device only when that identifier was lost. A cleanup
  failure fails the test; it cannot produce a passing receipt.
  Runner cancellation, attempt timeout and abrupt worker termination still need
  surviving Simulator cleanup and qualification. Do not rely on device cleanup
  when the worker cannot finish teardown.

The receipt's closed `iosRuntimes` metadata records bundle/build version, app
digest, runtime and device type, OS, preferred architecture advertised by the
Simulator (ARM first when both slices are available), Xcode, pinned automation
versions, viewport and a hashed worker identity, attributed to the test and
attempt. Missing, stale, duplicate, malformed or cross-platform metadata cannot
pass. These are worker-reported facts, not independent behavior verification.
No app content, device UUID, Appium log, hierarchy or attachment body is retained
by this driver. Provider credentials are not passed to the native server.

## Reuse assessment

The platform reference was assessed at commit
`1eccc34477ee3df9cf2554f57a1165b65295e6b0` of `qa-rag-app`:
`services/mobile_flow_compiler.py`,
`services/ai_crawl/mobile_discovery_crawler.py` and
`services/flutter_appium_runtime.py`. The useful contracts are finite grounded
controls, exact unique matches, matching build/platform, and an owned loopback
Appium session with cancellation and cleanup. This implementation applies those
contracts to the existing `9l.target/3` fixtures and receipt validator. It does
not duplicate the hosted mobile DSL, discovery/planning or provider engine, and
does not require a platform account.
