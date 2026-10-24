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
Only resource IDs belonging to the configured package are supported. This
driver requires Go's private attempt scratch directory and cannot attach to a
device from an ordinary standalone Playwright invocation.

The app uses Android resource IDs through the shared `getByTestId` interface.
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
It does not qualify other Android runtimes, physical devices, iOS, semantic
screen checks, verified replay or the combined finance demo.
