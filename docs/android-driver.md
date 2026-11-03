# Local Android driver decision (QUA-2204)

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
Only bounded version/build/runtime metadata may enter execution evidence.
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
method is never changed and no keyboard capability is set on a phone. An
unreadable keyboard state fails the operation rather than guessing. Nothing
is retried.
Open is bounded at 180 seconds; the ordinary operation/assertion bounds still
apply. The adapter never attaches to an existing emulator. Its private console
handshake identifies its own emulator child, and a private HTTP base path
identifies its Appium server. Reset clears only the configured app on that owned
emulator. Close aborts transports and stops its emulator/server. Go also stops
the worker process group on outer cancellation/deadline and removes temporary
native output after the worker exits. The shared adb daemon is preserved.

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
removed. Screenshot, video and trace recording are off in the supplied config;
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
analysis.

`toHaveState('invalid', boolean)` reads the node's `content-invalid` flag,
the accessibility error a validation failure reports (Flutter's field
errors, `TextView.setError`). UiAutomator2 8.7.0 exposes the flag in its
hierarchy but not as an element attribute, so the driver first resolves the
control uniquely and then checks whether the same package-scoped criteria
match a node flagged invalid. The error text itself is not read.

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
captured, and the reads are off without the opt-in.

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
