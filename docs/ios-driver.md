# iOS Simulator (experimental)

A deterministic iOS driver, first released in 0.7.0-beta.1 and included since
0.7.0; 0.6.x does not include it. It is qualified from a source checkout on one host
only; unit tests use a synthetic transport and do not establish iOS support by
themselves. The package ships the driver, but setup needs this repository's
pinned tools (`tools/ios`) and `scripts/build-ios-wda.cjs`.

| Qualified | Not qualified |
| --- | --- |
| iOS 18.6 iPhone 16 Simulator on an Apple silicon Mac (M5 Pro, macOS 26.6.2, Xcode 27.2), with Appium 3.8.0 and XCUITest driver 10.43.1 | GitHub-hosted macOS runners, Intel Macs, other Xcode or iOS versions, other device types, real iPhones, goals, healing and verified replay |

On that host all twelve smoke cases qualified: the healthy transfer passed with
three target assertions in 39 seconds, a new simulator was usable 15 to 30
seconds after boot, and every owned device was removed, by its worker or,
when the worker was stopped first, by the engine. The smoke records the
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

The smoke records `9l provenance`, which needs a branch. A release tag checkout
is a detached HEAD: run `git switch -c <name>` at the same commit first.

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

The smoke then builds an isolated UIKit fixture and runs twelve cases. Nine end
inside a worker that finishes its own teardown: a healthy transfer, a
deliberately wrong receipt, missing and duplicate controls, a label-only decoy,
a changed build digest, an unavailable runtime, cancellation, and a held
response that reaches its operation timeout. Cancellation and timeout must
start the actual native mutation, dispatch it once and remove the owned device.

Three stop the worker from outside while it holds a booted device, so its
teardown never runs: the engine is interrupted (`runner-cancel`), the attempt
reaches its timeout (`attempt-timeout`), and the worker process is killed
(`worker-kill`). Each requires the receipt to say the engine removed the
device, the device and every process of that attempt to be gone, and the run
not to pass. Every case checks that an unrelated simulator survives. A healthy run
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
- A failed open records which phase failed in the receipt's `targetSetup`
  (`phase`, e.g. `install` or `session`). A rejected app reads as `mismatch`
  with a closed reason: `build-digest` (the copy differs from `appSHA256`),
  `build-installed` (the installed tree's names or bytes differ from that
  copy), `build-tree` (unsupported entries or over the 4,096-entry/256 MiB
  limits), `build-platform` (wrong bundle identifier or not a Simulator build)
  or `runtime-metadata` (version, build or runtime outside the recorded shape).
  These come only from the phases that handle the app (`artifact`, `install`,
  `metadata`); the same tree limits on prebuilt WebDriverAgent products are a
  `wda` failure, not a rejected app. Anything else in open is `unknown` /
  `device-state-unknown` with its phase.
- A Simulator is registered with CoreSimulator, outside the worker's process
  tree, so stopping the worker does not remove it. Before `simctl create` the
  driver writes a small ownership record (the unguessable device name, runtime
  and device type) into the attempt's private scratch, and deletes it once the
  device is confirmed gone. When the worker's process tree has stopped, after
  a normal finish, a cancellation, an attempt timeout or a killed worker, the
  engine reads any record that is left and removes exactly that device: one
  device with the recorded name, in the recorded runtime, of the recorded
  type, never a device chosen by its state. A fresh inventory decides the
  result, which the receipt lists as `targetCleanup` with `removed`, `absent`
  or `unconfirmed`. An unconfirmed device may still exist; that attempt cannot
  pass, and a record the engine cannot trust is never acted on. Removal uses
  the same per-command bounds within a two-minute budget for all records, and
  the engine stops waiting for a `simctl` that has not exited two seconds after
  its bound (its device is then `unconfirmed`), so a stopped attempt can take up
  to about two minutes longer to return when `simctl` hangs. Its status is decided when its
  worker stops, so that time never turns a finished attempt into a timeout. A
  scratch directory that cannot be read counts as unconfirmed.
- The engine itself must survive to do this. If the `9l` process is killed
  outright (`SIGKILL`, a crash or a host shutdown), the device stays
  registered; remove it with `xcrun simctl delete <udid>`. Owned devices are
  named `9l-ios-<uuid>`.

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
