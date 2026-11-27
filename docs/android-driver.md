# Android target (experimental)

The experimental Android target ships since 0.6.0. This page starts with a
quickstart, then records the driver decision, host setup,
the qualification evidence and the attached-device mode.

## Quickstart

The target drives an owned emulator or an attached phone through Appium
3.8.0 and UiAutomator2 8.7.0, with the same `app` fixture a web target
uses. It is qualified on API 35 emulators and one Android 17 phone;
[Set up a host](#set-up-a-host) has the details and
[Run the opt-in qualification](#run-the-opt-in-qualification) the limits.

1. Install the matching 0.7.3 CLI and `@9l/playwright` together (see
   [installation](install.md)), and the pinned automation
   tooling once, outside your project:

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

## Driver decision (QUA-2204)

The M2 adapter wraps Appium 3.8.0 and UiAutomator2 8.7.0 (both Apache-2.0).
Installation is separate from the ordinary Playwright SDK. Deterministic native
execution needs no platform account, model or provider credential.

Before adding a dependency, the candidate Callstack `agent-device` 0.21.31
(MIT, Node >=22.12) was inspected from its published package and typed Node API.
It supports embedded ownership, cancellation, fresh accessibility snapshots and
strict mutations. Its snapshot result also permits automatic fallback
screenshots, and its session machinery records actions. The adapter would need
additional qualification to establish the required default of no retained screen
content. It is deferred, rather than claiming that `noRecord` disables every
artifact path.

The existing F5 compiler (`services/mobile_flow_compiler.py` in qa-rag-app)
already uses resource IDs and deterministic Appium Android operations under
`qtml-mobile/1`, with runtime data kept outside persisted flows. M2 reuses that
boundary: stable resource IDs, explicit app/build identity and worker-local
values. It does not import the platform's account, Limrun provisioning,
discovery, recording or orchestration machinery.

Appium provides the native execution engine. The local wrapper owns its server
and an ephemeral, read-only emulator session. It requests exact resource IDs,
rejects ambiguity, reads fresh native properties and never retries mutations.
Server output, page source, screenshots, video and device logs are not retained.
Only bounded version/build/runtime metadata may enter execution evidence. The
one exception is opt-in: a spec that sets
[`diagnostics: {failureCapture: true}`](#failure-screenshot-and-view-tree-opt-in)
gets a screenshot and the page source of a failed test as Playwright
attachments, taken only in a run that keeps them.
UiAutomator2's supported `hideKeyboard` capability installs its empty input
method on this owned emulator. This keeps the fixture deterministic without
keyboard-state dumps or back navigation. Software-keyboard behavior and layout
are outside this qualification.

References: [Callstack commands](https://oss.callstack.com/agent-device/docs/commands),
[Callstack snapshots](https://oss.callstack.com/agent-device/docs/snapshots),
[UiAutomator2 capabilities](https://appium.github.io/appium-uiautomator2-driver/latest/reference/capabilities/).

## Set up a host

The adapter needs Node 22.12+, Java 17+, the Android SDK's platform-tools and
build-tools 35.0.0, and, for owned-emulator runs, the emulator and an existing
ARM64 or x86-64 API 35 AVD. Appium 3.8.0 and UiAutomator2 8.7.0 are installed
separately from `@9l/playwright`, which never depends on them. Neither the
adapter nor doctor downloads an image, installs a package or changes an AVD.

Install the pinned tooling once, into a directory of your choice:

```sh
npm install --prefix ~/.9l/android-tools --save-exact --ignore-scripts \
  appium@3.8.0 appium-uiautomator2-driver@8.7.0
```

`~/.9l/android-tools` is the default location; for another one, set
`NINELIVES_ANDROID_TOOLS` to it or pass `toolsRoot`. The driver rejects any
other Appium or UiAutomator2 version at open. To use this repository's
lockfile instead, run `npm ci --prefix tools/android --ignore-scripts` in a
checkout and point `NINELIVES_ANDROID_TOOLS` at `tools/android`.

Then check the host from the project directory:

```sh
9l doctor --android
```

Doctor reports, each with a fix command, Java (major release from `JAVA_HOME`,
else `java` on `PATH`), the SDK root and how it was found, `adb` and its
version, the emulator, `aapt2` and `apksigner` in `build-tools/35.0.0`, the
AVD names `emulator -list-avds` prints, how many emulators and physical
devices are attached and ready (and how many are unauthorized or offline),
and the Appium and UiAutomator2 versions in the tooling directory. Java, the
SDK, `adb`, build-tools and the pinned tooling are required: one missing
makes the report not ready and doctor exits 2. A missing emulator or AVD is
a warning, since attached devices need neither. Doctor runs only
`java -version`, `adb version` and `emulator -list-avds`. It counts devices
by asking an adb server that is already listening on loopback for its list,
never starts one, and keeps serials in memory; they are never printed or
stored. Paths appear only with `--paths`. It does not boot an emulator,
start Appium or open a session; those stay unverified.

When `sdkRoot` or `toolsRoot` is omitted, the driver and `installInPlace`
use `androidDefaults()` from `@9l/playwright/android`, the discovery doctor
reports: `ANDROID_HOME`, then `ANDROID_SDK_ROOT`, then the first existing
directory among the Homebrew command-line tools
(`$HOMEBREW_PREFIX/share/android-commandlinetools`, then under `/opt/homebrew`,
`/usr/local` and `/home/linuxbrew/.linuxbrew`; `brew` itself is not run) and
Android Studio's default (`~/Library/Android/sdk` on macOS, `~/Android/Sdk`
on Linux, `%LOCALAPPDATA%\Android\Sdk` on Windows). A variable that is set is
used as given, so a wrong value is reported instead of silently replaced.
Tooling comes from `NINELIVES_ANDROID_TOOLS`, else `~/.9l/android-tools`.
Explicit options win. `9l run` passes only a fixed environment to tests, so
forward the variables you rely on, such as
`--pass-env ANDROID_HOME,JAVA_HOME,NINELIVES_ANDROID_TOOLS`; an SDK found
through `ANDROID_SDK_ROOT` or a custom `HOMEBREW_PREFIX` needs that variable
forwarded too. Locations under the home directory and the fixed prefixes need
no variable. `android/sdk` in `9l doctor --android` says whether a test
without `sdkRoot` would find the same root, and otherwise which variable to
forward.

## Run the opt-in qualification

The qualification additionally needs SDK platform 35 to build its synthetic
APK, and `JAVA_HOME` set to a JDK for that build. Set `NINELIVES_ANDROID_AVD`
to the AVD name if it is not `QualityMax_API_35`. The scripts find the SDK
with the same discovery as the driver.
AVDs outside `~/.android/avd` are found through `ANDROID_AVD_HOME` (or
`ANDROID_USER_HOME`/`ANDROID_EMULATOR_HOME`), which the driver forwards to the
emulator; pass it to tests with `--pass-env ANDROID_AVD_HOME`.

```sh
npm ci
npm ci --prefix tools/android --ignore-scripts
npm run smoke:android
```

The smoke builds a synthetic native APK with local SDK tools and an ephemeral
signing key, then exercises healthy behavior and isolated failure controls.
It uses no finance account or real transaction. Each case runs as
`android:finance` with browser fixtures disabled, zero Playwright retries and
one Go attempt even when `--attempts 3` is requested. Each deliberate test
revision has a creation provenance record in `.context/android-qualification/`.
The default suite can take several minutes because each case owns a cold
emulator; selected cases can be run with
`node scripts/android-smoke.cjs healthy defect` after `npm run build`.

In CI, pull requests that touch the Android paths and pushes to `main` run a
fast subset: `healthy`, `ambiguous`, `cancel`, `misfill`, `width-mismatch`,
`digest` and `install-fail`. Every case runs nightly and on demand, three
times by default, and the run's pass-rate job lists how often each case
qualified and why it did not. The cases that have been intermittent on the
hosted x86_64 emulator run only there, so they are measured rather than
re-run until green. Dispatch the full matrix with
`gh workflow run android.yml -f scope=full -f repeat=3`.

`semantics-delayed` draws the synthetic semantics content eight seconds after
launch. Its first visibility assertion uses the same 90-second cold-relaunch
bound as the operations case; later assertions keep their ordinary bounds.
`isolation` runs two required tests in one job: the first deliberately fails
while signed in without logging out, and the second checks a fresh signed-out
session, persistence across terminate/activate, and data clearing by reset.
The harness requires separate runtime workers, the complete ordered target
operation sequence per test, exact failed/passed terminal outcomes and passing
fixture teardown. `isolation-defect` disables reset in the fixture driver;
the second test must fail its final session assertion after successful
persistence checks and the final reset. Earlier assertion failures or failed
fixture/hook steps cannot qualify either case. The deliberate failures
make the inner jobs fail; only the harness's exact outcome checks qualify them.
With reset disabled the app is never relaunched, so that final assertion keeps
the ordinary five-second bound instead of the 90-second cold-relaunch
allowance: it can only fail, and waiting out the allowance took the test past
its 240-second budget on a slow hosted emulator. The first test's deliberate
failure keeps the ordinary bound too: a bound that ends before any read has
completed is an operation timeout rather than an assertion failure, and one
lookup alone took up to 1.8 seconds on the hosted emulator.

Every case's result line carries its duration and bounded probe facts, also
when the case does not qualify: per test, its terminal status, how many target
steps ran and the first step that did not pass, with its operation and stop
reason; read counts, failures and the slowest read per route, how many lookups
matched nothing, and for `cancel` and `blocked` how the checks for the app's
in-flight marker ended. A failed transfer, semantics or isolation test records
whether the app's window, another window or none had input focus. Those three
also read the fixture app's own log with `run-as`, outside the accessibility
reads the driver uses: that its activity was created and drew its content
(signed in or out for `isolation`), when its window gained or lost focus,
which watched control each touch that reached its window landed on (or
`other`), and which watched click handlers ran, with field lengths. They also
record how long each element click request took to answer. A failed text
assertion can then be told apart as a tap that never reached the app, one that
landed elsewhere, one whose handler did not run, a handler that ran with other
field contents, or a read that did not see what the app drew. After an
operation timeout the driver has already stopped the emulator, so the log
reads `unreachable` there; `cancel` and `blocked` keep their in-flight marker
for that case. These are counts, durations, lengths and fixed words; no screen
text, device output or identifiers are kept. The negative controls `defect`, `state-defect`,
`failure-capture` and `semantics-defect` qualify only when the log shows the
tapped handler ran: a tap the driver reported done but the app never handled
fails their assertion for another reason. That happened once to `defect` on a
local API 35 arm64 emulator while this check was being added.

To check preservation of a preexisting emulator, run
`node scripts/android-preservation.cjs` after building the healthy fixture.
This opens a stand-in session, retains a synthetic field value, runs a second
owned worker, then checks that the first session and its state survived and
the AVD configuration did not change. The stand-in is closed by the harness.

For a project-owned native app, construct the optional driver in the shared
target fixture. Keep its APK digest in reviewed build metadata:

```ts
import {createTargetTest} from '@9l/playwright';
import {androidTargetDriver} from '@9l/playwright/android';

const test = createTargetTest({
  native: target => androidTargetDriver({
    appId: target.appId,
    avd, // sdkRoot and toolsRoot default to androidDefaults()
    apk, apkSHA256, appPackage, activity,
  }),
});
```

Declare a Playwright project named `android:<app-alias>` and invoke it with
`9l run <spec> --sdk --config <config> --target android:<app-alias>`.
Resource IDs and explicit control aliases are scoped to the configured package.
This driver requires Go's private attempt scratch directory and cannot run from
an ordinary standalone Playwright invocation.

The app uses Android resource IDs through the shared `getByTestId` interface.
A test ID matches the app's prefixed resource name (`<package>:id/<id>`) or
the same name reported without a prefix, as Flutter reports
`Semantics(identifier:)`, in one package-scoped query; a control reported
both ways is ambiguous. IDs contain 1–128 ASCII letters, digits, `_`, `.`
or `-`, starting with a letter, digit or `_`.
`fill` acts as a user would: one tap on the field focuses it, then its text
is cleared and set. Only a text field is tapped: the resolved control must
report an EditText class, or an input type in the hierarchy on that same
element, else `fill` fails as `not_editable` before touching it, so a
selector that names a button never presses it. Because the field is
focused first, checks an app runs when a field gains or loses focus run
(setting text alone never focused it). On a phone the tap opens the
keyboard, as for a user; `hideKeyboard()` closes it. With
`fill(value, {submit: true})` the driver then sends the input method's done
action (`mobile: performEditorAction`), only while the field reports focus,
so it never reaches another field; a field without focus fails as
`field_not_focused`.
Beyond tap, fill and the visibility, text and state checks, the driver maps
each added operation to one supported command with fixed arguments: `back`
to the WebDriver back endpoint (the Back key), `scroll` to UiAutomator2's
`mobile: scrollGesture` inside the resolved element, `tapAt` to one W3C
touch pointer down/up at device pixels, `bounds` and `viewport` to the
element and window rectangles, and `terminate`/`activate` to
a force-stop of the configured package (`am force-stop`, then a bounded wait until `pidof` finds no process) and `mobile: activateApp`, for the configured package only
(a force-stop that keeps the app's data, then a relaunch). The keyboard is
read from `mInputShown` of `dumpsys input_method`; `hideKeyboard` presses
Back only while it is shown and reads the result back, so the selected input
method is never changed and no keyboard capability is set on a phone. On an
owned emulator the selected method is Appium's empty one
(`io.appium.settings/.EmptyIME`), which draws no window: while a field has
focus Android still reports it as shown, but nothing covers the app and a
Back would reach the app instead of closing it. That method therefore reads
as hidden: `toHaveKeyboard(false)` holds and `hideKeyboard()` sends nothing.
The selected method is read from `mCurMethodId` (API 35) or
`mSelectedMethodId` (API 36, where the field was renamed); the behaviour is
qualified on the documented API 35 emulators. An
unreadable keyboard state fails the operation rather than guessing. Nothing
is retried.
Open is bounded at 180 seconds; `reset`, `terminate` and `activate`
default to 60 seconds, and every operation and assertion accepts up to 180
seconds. `foreground()` reads the focused window's package from the same
filtered `dumpsys window` lines as the baseline (an unreadable dump fails as
`unavailable_state`), and `deviceSerial()` returns the owned emulator's
`emulator-<port>` serial, or the configured phone serial, while the session
is open. The adapter never attaches to an existing emulator. Its private console
handshake identifies its own emulator child, and a private HTTP base path
identifies its Appium server. Before installing the declared APK on its owned
emulator, the driver compares the APK's minimum SDK and native code (read
with `aapt2 dump badging`) with the emulator's API level and ABI list: an
APK the device cannot run stops the test as `setup mismatch` (`build-sdk`,
`build-abi`) before anything is installed. When the AVD already holds the
package as a newer build or with another signer, `adb install -r` refuses
the update in place; on the read-only emulator the driver then uninstalls
that build and installs the declared APK fresh, without changing the AVD,
so the test does not inherit the AVD's app data in that case. Any other
install failure is reported by adb's failure code alone (for example
`open_failed_apk_install_insufficient_storage`); nothing else adb printed is
kept. Reset clears only the configured app on that owned
emulator. Close aborts transports and stops its emulator/server. Go also stops
the worker process group on outer cancellation/deadline and removes temporary
native output after the worker exits. The shared adb daemon is preserved.

Reset force-stops the app, requires `pm clear` to report success and relaunches
it. It clears private app data; it does not revoke server-side sessions or
assign a new device identity to an AVD. Apps that restore authentication from
a server or device identity need an app-owned account/session cleanup contract.
A successful reset command also does not establish that asynchronously loaded
content is ready: assert the first expected screen with a bounded readiness
allowance before continuing.

Receipts contain only the exact APK SHA-256, package, Android API/release/ABI,
emulator/Appium/UiAutomator2 versions, the targeted display (its physical id or
`unknown`, size, density and fold posture) and a hashed worker identity bound
to the required test and attempt. They do not establish independent verification:
`targetAssertions` counts worker-reported checks and `verifiedAssertions` stays
0. `9l assess` credits the target assertions it finds in the spec's source as
`target:<matcher>`; that is recognition, not verification. The independently
sourced M2 outcome mapping is
in `testdata/android/requirements.json`; annotations are claims to review.

Native Playwright output is forced into attempt-owned temporary storage and
removed. An opted-in [failure capture](#failure-screenshot-and-view-tree-opt-in)
is taken only when `--failure-details --keep-attachments` will copy its
two files out of it; nothing else there is ever copied. Screenshot, video and trace recording are off in the supplied config;
Appium logcat capture and page-source-on-error are disabled. Application values
and transient native responses stay in memory. Arbitrary custom fixtures or
user code can still write their own files and must follow the same privacy
contract.

This experimental M2 adapter has local qualification evidence on Android 15 /
API 35 arm64-v8a with emulator 37.1.11.0, Appium 3.8.0 and UiAutomator2 8.7.0.
On 2026-11-02 the smoke gained the native operations (viewport, bounds, a
touch at a point, Back, a hidden assertion, scrolling, the keyboard read,
terminate and activate), opt-in diagnostics with real window counts, and
display-width and build-version requirements; the healthy case also checks the
recorded display, posture, version and signer. On 2026-11-03 it gained
Flutter-shaped controls on a synthetic native fixture that reports what
Flutter's accessibility bridge reports (bare identifiers, a hint-only field,
a merged multi-line label with regex metacharacters, `content-invalid`):
`semantics` passes eight assertions through bare IDs, the hint and
merged-label aliases and the invalid state, and `semantics-defect`, whose
app never reports the field error, fails at exactly that assertion. No
Flutter build was run. The `Android` workflow runs the
whole smoke on an API 35 x86_64 emulator on hosted Linux runners.
The real runs cover healthy behavior, relevant wrong-text and wrong-state
failures, missing/ambiguous IDs, unavailable/install/disconnect/digest/version
failures, and cancellation/deadline during an app-confirmed click with an
injected stalled transport. A separate two-session run confirms preservation
of preexisting app state and AVD configuration and cleanup of the second worker.

Evidence is revision-specific: the full 12-case run qualified 11 cases; its
state control failed before reaching the intended assertion. After the polling
classification fix, healthy, amount and state controls were requalified against
the unchanged native driver. This is combined per-case evidence, not one final
all-green matrix. The local index is `.context/android-qualification-summary.json`.
That emulator matrix does not qualify physical devices, iOS, semantic screen
checks, verified replay or the combined finance demo.

## Failure screenshot and view tree (opt-in)

By default the driver keeps no screen content, hierarchy or text, and
nothing below happens. With `diagnostics: {failureCapture: true}` a test that
failed also gets a screenshot of the target display and the UiAutomator2 view
hierarchy as Playwright attachments, the evidence a person reaches for first
when a native assertion fails. The capture is taken only in a run that keeps
it:

```ts
androidTargetDriver({
  appId: target.appId, avd: 'Pixel_API_35', apk, apkSHA256, appPackage, activity,
  diagnostics: {failureCapture: true}, // may sit beside gestures: true
});
```

```sh
9l run tests/transfer.spec.ts --sdk --config playwright.config.ts \
  --target android:finance --failure-details --keep-attachments
```

The option is off by default; there is no run flag that turns it on for a
spec that did not ask for it. `diagnostics` must name at least one of
`gestures`, `failureCapture` and `onPhone`, each a boolean; anything else
(`{}`, a string, another key) is rejected as `invalid_configuration` when
the driver is constructed.

**Attached phones need their own confirmation.** An attached phone is a
person's device: its screen can show other apps' notifications and windows,
messages and accounts that have nothing to do with the test. There
`failureCapture: true` alone captures nothing; the capture also needs
`onPhone: true`:

```ts
androidTargetDriver({..., deviceSerial, diagnostics: {failureCapture: true, onPhone: true}});
```

`onPhone` is accepted only with `failureCapture: true` and only with
`deviceSerial`; on an owned emulator, or without `failureCapture`, it is
`invalid_configuration`. Owned emulators need only `failureCapture`. Before
setting it, make sure the phone holds test accounts only, and consider what
its notifications can show.

**When.** Once per test, when the test body has ended and before the fixture
closes the driver, and only for a test whose Playwright status is `failed`
or `timedOut` (target tests cannot use `test.fail()`). The engine tells the
worker, per attempt, whether this attempt keeps a capture: only a run with
`--sdk --failure-details --keep-attachments` on an `android:` target does.
Otherwise nothing is read from the device, since the files would only be
deleted. A test that passed, was skipped or was interrupted reads nothing.
Nothing is read either when the driver is no longer open: an operation that
ran into its deadline or a cancellation releases it at once, an operation
still pending when the test timed out keeps the session busy, and a spec
that calls `app.close()` itself has closed it. The SDK takes the capture
only from the target fixture. A spec is the user's own code and can still
reach the driver or the process environment to read the device or skip
the capture; what is kept is decided by the run's flags in the engine,
which a spec cannot change. A failed operation
that ended by itself leaves the driver open, so its capture shows the
screen as it was when the test gave up.

**What.** One PNG of the targeted display (`adb exec-out screencap -p -d
<display>`, the display `screenDigest()` reads) and UiAutomator2's page
source (`GET /session/<id>/source`), the XML hierarchy of every window on
that display: text, content descriptions, hints, resource IDs, classes,
bounds and state attributes. Both show the whole display, not only the app:
the status bar, an open notification shade, the keyboard and other apps'
windows are included.

**Limits.** The screenshot is read with an 8 MiB output bound and the view
tree must be at most 2 MiB (its JSON response at most 4 MiB). The capture as
a whole gets at most 10 seconds and at most half of the test's Playwright
timeout, since teardown shares one time slot with closing the driver, and
never runs past the attempt deadline. A driver hook still running 250 ms
after that bound is abandoned. Each part that fails, times out, exceeds its
limit or is not a PNG (respectively markup) is left out on its own; nothing
is retried. A capture never throws, never changes the test's outcome or the
run's result, and never delays teardown beyond that bound.

**Attachments and evidence.** The SDK attaches the parts it validated as
files named `9l.failure-screenshot` (`image/png`) and `9l.failure-view-tree`
(`text/xml`); Playwright copies each into its output directory. These names
are reserved: the engine reporter refuses, as invalid evidence, either one
on a test that did not fail, on a web target, twice, or as an inline body.
The evidence stream and the receipt never carry the image or the XML: the
stream lists each attachment only as an opaque `artifacts` entry (`kind`
`image` or `other`, `retained: false`), as for every Playwright attachment.

**Retention.** A native target's Playwright output is the attempt's private
scratch in the system temporary directory, removed when the attempt ends.
With `--sdk --failure-details --keep-attachments` the capture's two files,
and only they, are copied from it into
`<receipt-dir>/<run>/<job>/<attempt>/attachments/` by the existing
[retention](cli.md) rules (at most 32 MiB per file and 128 MiB per
attempt); the receipt's `failures[].attachments` names each with its path,
size and SHA-256, and `retainedAttachments` counts them. Everything else in
that output is never referenced or copied and is deleted with it as
before: a trace (which embeds the attachments, the view tree unredacted),
an error context, videos and screenshots that the project's `use` options
produce. A capture file that cannot be retained (over a limit) is not
referenced either. Without all three flags no capture is taken.

The retained view tree is text, so it is redacted like other text
evidence: authorization headers, bearer tokens, URL credentials and values
after `token`, `secret`, `password` or `api_key` become `[REDACTED]`. That
also rewrites UiAutomator2's `password="false"` attribute to
`password="[REDACTED]"`. Redaction targets credential-shaped strings only;
names, messages, addresses, balances and any other on-screen text remain.
The PNG is copied byte for byte; an image cannot be redacted.

**Privacy.** Turning this on retains screen content, which 9l otherwise
never does. A screenshot and a view tree hold whatever was on the display
when the test failed: personal data, account details, messages, one-time
codes, a credential a field or a dialog shows, and on an attached phone the
notifications and windows of other apps on a person's real device. Use it
with test accounts and synthetic data, and treat receipt directories made
with `--keep-attachments` as sensitive as the screens they show. Spec
authors who attach their own screenshots remain responsible for them, as
before.

On the web, Playwright's own `screenshot: 'only-on-failure'` (and `trace`)
in the project's `use` options already covers this and follows the same
`--keep-attachments` rules; 9l adds nothing there. The iOS driver has no
failure capture.

The capture is tested against a stubbed device transport through the
target fixture (`packages/playwright/test/android.test.cjs`) and end to end
through the engine's native retention path with the fake driver under an
`android:` target (`npm run smoke:targets`, whose config also keeps a trace
there). The emulator case `failure-capture`
(`node scripts/android-smoke.cjs failure-capture`) runs it against the
synthetic defect APK with `--failure-details --keep-attachments`. On
2026-11-26 it qualified in three of three runs on the documented API 35
emulator, with the engine's keep signal and the post-teardown stop check:
an 85 KB PNG and a 14 KB view tree were retained each time, the receipt
held only their references, and the emulator had stopped after teardown.
An attached phone, and with it the phone confirmation, is not qualified.

## Biometrics on an owned emulator

Apps that sign in with a fingerprint can be tested on an owned emulator, and
only there. The rule for phones below is unchanged: on an attached device
the driver never requests, stores, sets or bypasses a PIN and has no
fingerprint operation. On an owned emulator, which the driver boots
read-only and discards when the test closes, the policy is:

- **Opt-in.** The driver declares `biometrics: {snapshot}`; without it
  nothing changes. Declaring it with `deviceSerial` is a configuration
  error.
- **No credential reaches the driver.** A dedicated AVD is prepared once by
  `npx 9l-prepare-biometric-avd --avd <dedicated AVD> --wipe
  [--snapshot 9l-biometric]` (the command ships with `@9l/playwright`; in
  this repository `node scripts/prepare-biometric-avd.cjs` runs the same
  code), which erases that AVD, sets a random lock PIN
  that is never printed or stored, enrolls fingerprint 1 through Settings
  with the emulator's virtual sensor and saves the unlocked state as a named
  snapshot. The driver boots that snapshot `-read-only -snapshot <name>
  -no-snapshot-save`, so every test starts from the same state and the
  driver never sees, sets or enters a PIN. Use an AVD only for this: its
  data is erased and its PIN is unrecoverable by design. When the snapshot
  stops loading (an emulator or image update), run the script again.
- **Checked before the test.** After boot the driver reads the keyguard and
  `dumpsys fingerprint`; a snapshot that resumes locked or without an
  enrolled fingerprint stops the test as `setup mismatch`
  (`biometrics-unavailable`) before any application operation.
- **Recorded, not hidden.** `app.fingerprint(finger)` (1 to 10) waits,
  within its bound, until two screen captures 250 ms apart are equal (a
  prompt drops a touch that arrives while it is still animating in), then
  touches the virtual sensor, holds it about a second and lifts it, through
  the emulator console. It is made once and never repeated, and the console
  refusing it (`KO`) fails the operation. It is recorded as an executed
  `fingerprint` action, never as an assertion, so a biometric sign-in still
  needs an assertion to pass. On a phone, on the web, or without the opt-in
  it fails as `unsupported_operation` before touching anything.
- **Portable form.** `app.biometric('match')` touches finger 1, the one the
  snapshot enrolled, and `app.biometric('nomatch')` finger 2, through the
  same touch, recorded as a `biometric` action. The same call presents a face
  on an owned iOS Simulator.

The system `BiometricPrompt` is another package's window, so
`app.foreground()` reads `foreign` while it is in front and `app` after it
closes. The opt-in `biometric` smoke case qualifies the enrolled finger
signing in on an API 35 arm64 emulator, and `biometric-unenrolled` qualifies
that finger 2, which is not enrolled, leaves the sign-in assertion failed:

```sh
npx 9l-prepare-biometric-avd --avd 9l_biometric_API_35 --wipe
NINELIVES_ANDROID_BIOMETRIC_AVD=9l_biometric_API_35 node scripts/android-smoke.cjs biometric biometric-unenrolled
```

Both cases are opt-in, outside the default matrix and the hosted CI
workflow, because they need a prepared AVD. Face unlock, device credentials
(PIN or pattern prompts) and biometric prompts on phones are not supported.

## Experimental installed-app attachment

Pass `deviceSerial` instead of `avd` and `apk` to attach to an explicitly selected,
authorized physical device. Keep the serial in a private local file outside
source control. `apkSHA256` must match the installed single-base APK; split APKs
are rejected. The driver checks the build before and after session creation.
A different installed digest stops the test as `setup mismatch`
(`build-digest`) before any application operation. `expected` declares
more of the build: `apk` names a local artifact (a downloaded build) whose
digest the installation must match, and may replace `apkSHA256`;
`versionCode` and `versionName` are compared with the device's package
manager (`build-version`); `signerSHA256` with the SHA-256 of the installed
base APK's first signing certificate (`build-signer`), read by pulling that
APK into the attempt's private scratch directory, only when declared, and
through the local `apksigner`. The runtime receipt records `versionCode`,
`versionName` and `signer` (or `unknown`) beside the verified `apkSHA256`.

To update the app in place from a project script, `installInPlace({sdkRoot,
deviceSerial, apk})` takes the same exclusive device lease a test takes
(`device_in_use` while a test or another installer holds it), copies the new
APK into a private snapshot that it hashes, inspects and installs, so a build
replacing the file meanwhile changes nothing, reads its package and signer, compares the signer with the installed app's and
refuses on a difference before any device change; a package not yet
installed is a first installation. Otherwise it runs `adb install -r`,
verifies the installed digest, releases only its own lease and returns the
package, digest, signer and versions for the reviewed build metadata. It
never uninstalls, clears data or runs inside a test.
It installs the pinned automation helpers, preserves the installed app and its
data, and rejects `app.reset()` before issuing a device command.

For apps without resource IDs, provide finite aliases through `controls`:

```ts
androidTargetDriver({
  appId, deviceSerial, apkSHA256, appPackage, activity,
  controls: {
    overview: {accessibilityIdSuffix: ', Overview', className: 'android.view.View'},
    heading: {text: 'Overview', className: 'android.widget.TextView', selected: false},
    unread: {accessibilityIdContains: '3 unread'},
    email: {hint: 'Email address', className: 'android.widget.EditText'},
  },
});
```

Each alias names one matcher: exact text (`text`), an exact accessibility
description (`accessibilityId`), a literal description suffix
(`accessibilityIdSuffix`) or substring (`accessibilityIdContains`), or an
exact input hint (`hint`), with optional native class and selected-state
filters. A merged multi-line label, such as Flutter's, is one description
whose lines are joined by line breaks: the suffix and substring matchers
reach part of it across those breaks, and the exact matchers need the whole
label. Values are literals, never patterns; the text and description values
may not contain a backslash. A hint alias, typically a Flutter text field
with no label, is found with one generated package-scoped XPath, since
UiSelector has no hint matcher. Aliases remain package-scoped and must
resolve uniquely before each operation. They do not perform semantic screen
analysis. `9l assess` reports each use of such an alias as a suspected
`fragile-selector` (`copy-alias`, `partial-copy-alias` or `data-alias`), and
`9l inspect --android` lists the controls on a screen that lack a resource ID.

`toHaveState('invalid', boolean)` reads the node's `content-invalid` flag,
the accessibility error a validation failure reports (Flutter's field
errors, `TextView.setError`). UiAutomator2 8.7.0 exposes the flag in its
hierarchy but not as an element attribute, and no single request returns
both a control and its flag. The driver brackets the read: the control
resolves to one element, the same package-scoped criteria are matched
against nodes flagged invalid, and the control resolves again. The state
counts only when both resolutions return the same element (element IDs are
stable per node) and any flagged node is that element; a control that
vanished, was replaced or became ambiguous in between is a failed read that
the assertion polls past, never a state. The error text itself is not read.

`toHaveError(text)` checks that text. UiAutomator2 8.7.0 writes a node's
accessibility error (`AccessibilityNodeInfo.getError()`, which Flutter's
`errorText` and `TextView.setError` set) as the hierarchy's `error`
attribute, also not as an element attribute. The read is bracketed the same
way, with the expected text inside the package-scoped XPath
(`[@error="…"]`): the comparison happens on the device and only whether the
control matched comes back, so the text the app shows is never returned to
the worker or recorded.

Before any application operation the driver reads the keyguard and
wakefulness markers of `dumpsys window` and `dumpsys power`, filtered on the
device and kept in memory. A locked phone or a sleeping screen stops the test
as `setup blocked` (`device-locked`, `screen-off`) before the automation
helpers, hidden-API policies or an Appium session touch the device. After a
completed assertion failure it rechecks the same markers, so a phone that
locked while a readiness poll was running is classified instead of looking
like a missing control; a build whose markers it does not recognize is
reported as `unknown`, and a phone whose state cannot be read before the
test is an `unknown` setup classification rather than an open failure. The driver never requests, stores or bypasses a PIN,
never attempts to unlock and never changes the lock or timeout settings.

After launching the activity it compares the app's state with a declared
baseline: the number of windows titled with the app's package (its activity
and dialogs; anonymous popups are not attributed), the focused window's
package, and `mInputShown` of `dumpsys input_method`. `baseline: {windows}`
defaults to one. An extra window, foreign focus or an open keyboard stops
the test as `setup dirty` (`extra-window`, `foreign-focus`, `ime-showing`):
state a previous test left behind, not an application failure. With
`recovery: {back: N}` the driver presses Back up to N times, half a second
apart, until the baseline is observed again, and records the method, the
press count and whether it restored the baseline. Restored means the
declared window count, the app's focus and a hidden keyboard were all read
back; a reading the driver cannot interpret after Back is not a restored
baseline. A restored baseline lets the test run and keeps the record in the
receipt; an unrestored one fails the test. A reading the driver cannot
interpret before any Back press classifies nothing and the test proceeds. A
phone whose state cannot be read at a recheck (disconnected, adb gone) is
reported as `unknown`.
Recovery never clears data, force-stops or changes settings, and nothing
beyond those counts and enums is retained. Both checks apply to attached
devices only; an owned emulator starts fresh for each test. The markers are
tested against dumpsys excerpts; they are not qualified on arbitrary phones
or Android builds.

With `diagnostics: {gestures: true}` the driver reads the app's window count
and focus (the same filtered `dumpsys window windows` lines) before each
gesture and again when an operation fails, and attaches one record per
failed test: the operation, its stop reason, elapsed time, the requested
coordinates of a touch, the viewport and the window counts before and after.
It is printed under the job and kept as `targetDiagnostics`. The record
describes what was observed and claims no cause; screenshot stability is
not read as input readiness and nothing is retried. With the opt-in, a
`toBeHidden()` that fails after the window manager dropped one of the app's
windows since the last gesture is classified `setup observer`
(`window-gone-element-displayed`): the accessibility snapshot still listed
an element whose window was gone, as the field investigation of an
intermittent modal dismissal established. This is a heuristic: UiAutomator2
does not report which window an element belongs to, so the driver applies it
only to a dismissal sequence (a gesture, then a hidden assertion on the next
operation; a fill, text or state read in between disarms it) and only with
the opt-in, and the diagnostic keeps the window counts either way. A control
in a window that is still listed, or any failure outside that sequence, is a
real failure. A window dump that cannot be read counts as unobserved, never
as zero windows. No screenshot, hierarchy, text or log is
captured, and the reads are off without the opt-in; the separate
[failure capture](#failure-screenshot-and-view-tree-opt-in) opt-in is the one
that captures a screenshot and the hierarchy.

At attachment the driver reads the physical displays (`DisplayDeviceInfo`
lines of `dumpsys display`: id, size, density, power state) and the committed
fold posture of `dumpsys device_state`. The session targets the single
powered display; two powered displays or none readable record `unknown`.
`app.screenDigest()` captures that display by id with `screencap -d` and
returns only the SHA-256 of the PNG, computed in memory, for settling checks
such as comparing two captures; the image is never written, attached or
returned. The recorded size and density are the display's current logical layout from
its `DisplayInfo` (the app size follows rotation, the density follows
overrides), falling back to the physical metrics when the dump has none.
`requires: {posture, minWidthDp}` declares what a layout-dependent spec
needs: another posture, or an active display whose logical width in dp
(width times 160 over density) is below the minimum, stops the test as
`setup mismatch` (`posture`, `display-width`). The check runs after
attachment or boot and before the Appium session that would launch the app,
so a mismatch changes nothing on the device. Posture names are matched
exactly against the device's state names (`OPENED`, `HALF_OPENED`,
`CLOSED`); any other name is `unknown` and a device without the service is
`flat`. No foldable
was available for qualification.

The phone is leased exclusively by a host-local lock. Existing UiAutomator2
automation is rejected. Normal close stops only the leased helper, removes its
ADB forward and restores the hidden-API policy values and any preexisting
Appium Settings mock-location permission/index. The app is left running. The
keyboard capability is omitted to preserve the selected input method. Physical
receipts identify `device: "physical"` and a SHA-256 of the serial instead of an
emulator version; raw serials are excluded.

An interrupted host, disconnected phone or externally killed worker can prevent
cleanup. A remaining lease blocks reuse and contains private recovery metadata;
inspect the phone and restore those values before removing that lock. Outer Go
cancellation and hard-kill recovery on a physical phone are not qualified.

A private local installed-app smoke on Android 17 / API 37 arm64-v8a passed
four navigation destinations with eight worker-reported assertions. The unchanged
assertions rejected an opt-in wrong-heading response control at `assert-text`.
App-specific tests, selectors, configuration and execution evidence remain local
outside source control. These checks do not establish independent verification
or qualify arbitrary phones, keyboards, fold transitions or cancellation.

### Qualifying an attached phone

`node scripts/android-phone-smoke.cjs` (after `npm run build`) qualifies
attached-device mode on one authorized phone selected with
`NINELIVES_ANDROID_SERIAL`; the serial stays in the environment and receipts
hold only its hash. It installs the synthetic fixture app with
`installInPlace` (first installation, the same signer again, and a second
build's foreign signer refused before any change), then runs the native spec
attached: the transfer with a declared version and signer, native operations
with the phone's own keyboard (a touch opens it, `hideKeyboard` closes it),
opt-in diagnostics around a dialog, a dialog left open before the test with
and without a declared Back recovery, posture and width requirements the
phone does not meet, screen-off and, when waking shows a lock screen, a
locked device. The fixture app is uninstalled at the end unless
`NINELIVES_ANDROID_KEEP_FIXTURE=1`. The locked case leaves the phone locked.

On 2026-11-02 a folded Samsung foldable on Android 17 / API 37 (cover display
1248 x 1972 at 420 dpi, posture `closed`) qualified every case, as combined
evidence from two runs of the same script: the setup classifications in one,
and the transfer, operations, diagnostics (window counts 1 -> 2, focus `app`)
and recovered baseline after the launch-intent and focus fixes in the next.
One phone does not qualify arbitrary phones, fold transitions or cancellation.
