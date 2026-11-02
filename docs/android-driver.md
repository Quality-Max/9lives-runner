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

## Run the opt-in qualification

This checkout currently requires Node 22.12+, Java, Android SDK platform 35,
build-tools 35.0.0, platform-tools and an existing ARM64 or x86-64 API 35 AVD.
The adapter does not download an image or change the AVD configuration. Set
`ANDROID_HOME`, `JAVA_HOME` and `NINELIVES_ANDROID_AVD` to local paths/name as
needed. The local defaults match this development Mac; they are not a portable
SDK discovery mechanism.

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
    sdkRoot, toolsRoot, avd,
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
Beyond tap, fill and the visibility, text and state checks, the driver maps
each added operation to one supported command with fixed arguments: `back`
to the WebDriver back endpoint (the Back key), `scroll` to UiAutomator2's
`mobile: scrollGesture` inside the resolved element, `tapAt` to one W3C
touch pointer down/up at device pixels, `bounds` and `viewport` to the
element and window rectangles, and `terminate`/`activate` to
`mobile: terminateApp`/`mobile: activateApp` for the configured package only
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
emulator/Appium/UiAutomator2 versions and a hashed worker identity bound to the
required test and attempt. They do not establish independent verification:
`targetAssertions` counts worker-reported checks, `verifiedAssertions` stays 0
and source assertion coverage remains unknown. Source assessment does not yet
recognize `createTargetTest`. The independently sourced M2 outcome mapping is
in `testdata/android/requirements.json`; annotations are claims to review.

Native Playwright output is forced into attempt-owned temporary storage and
removed. Screenshot, video and trace recording are off in the supplied config;
Appium logcat capture and page-source-on-error are disabled. Application values
and transient native responses stay in memory. Arbitrary custom fixtures or
user code can still write their own files and must follow the same privacy
contract.

This experimental M2 adapter has local qualification evidence on Android 15 /
API 35 arm64-v8a with emulator 37.1.11.0, Appium 3.8.0 and UiAutomator2 8.7.0.
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
It installs the pinned automation helpers, preserves the installed app and its
data, and rejects `app.reset()` before issuing a device command.

For apps without resource IDs, provide finite aliases through `controls`:

```ts
androidTargetDriver({
  appId, sdkRoot, toolsRoot, deviceSerial, apkSHA256, appPackage, activity,
  controls: {
    overview: {accessibilityIdSuffix: ', Overview', className: 'android.view.View'},
    heading: {text: 'Overview', className: 'android.widget.TextView', selected: false},
  },
});
```

Aliases accept exact text, exact accessibility descriptions or a literal
description suffix, with optional native class and selected-state filters.
They remain package-scoped and must resolve uniquely before each operation.
They do not perform semantic screen analysis.

Before any application operation the driver reads the keyguard and
wakefulness markers of `dumpsys window` and `dumpsys power`, filtered on the
device and kept in memory. A locked phone or a sleeping screen stops the test
as `setup blocked` (`device-locked`, `screen-off`) before the automation
helpers, hidden-API policies or an Appium session touch the device. After a
completed assertion failure it rechecks the same markers, so a phone that
locked while a readiness poll was running is classified instead of looking
like a missing control; a build whose markers it does not recognize is
reported as `unknown`. The driver never requests, stores or bypasses a PIN,
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
