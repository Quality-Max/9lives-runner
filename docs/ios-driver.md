# iOS Simulator (experimental, unreleased)

This source checkout adds a deterministic iOS driver. Runtime qualification is
pending the macOS workflow: unit tests use a synthetic transport and do not
establish iOS support. No published CLI or SDK version is changed by this work.

The initial qualification target is macOS 15 on Intel (`macos-15-intel`), Xcode 16.4, iOS 18.5,
and an iPhone 16 Simulator, with Appium 3.8.0 and XCUITest driver 10.43.1.
ARM hosts, other configurations, real iPhones, goals, healing and verified replay are
unqualified. This is the owned Simulator foundation of
[QUA-2167](https://linear.app/quality-max/issue/QUA-2167), using build identity,
strict assertions and cleanup requirements from
[QUA-2108](https://linear.app/quality-max/issue/QUA-2108). Neither umbrella issue
is completed by this slice.

## Run from source

Install full Xcode with the iOS 18.5 Simulator runtime. Command Line Tools alone
are insufficient. The driver never downloads a runtime or selects a latest
runtime automatically.

```sh
npm ci
npm ci --prefix tools/ios --ignore-scripts
npm run smoke:ios
```

The smoke builds an isolated UIKit fixture and runs nine cases: a healthy
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
  runtime: 'com.apple.CoreSimulator.SimRuntime.iOS-18-5',
  deviceType: 'com.apple.CoreSimulator.SimDeviceType.iPhone-16',
  toolsRoot: '/absolute/path/9lives-runner/tools/ios',
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
  shuts down/deletes the owned Simulator. Cleanup uses a separate 20-second
  bound. A cleanup failure fails the test; it cannot produce a passing receipt.
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
