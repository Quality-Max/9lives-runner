# Compatibility

The qualified pairings of the CLI, the SDK, Playwright, Node and the
experimental native targets. Update this page when a qualified pairing
changes.

The experimental Android target (Appium with UiAutomator2) ships since 0.6.0.
Install the matching 0.7.2 CLI and SDK together; Android remains opt-in with
the [setup and qualification limits](android-driver.md) below.

| Component | Qualified | Boundary |
| --- | --- | --- |
| `@playwright/test` | 1.61.1, 1.62.0, 1.62.1, 1.63.0, 1.64.0, 1.64.1 | CI runs the unit, browser, assessment and packed-SDK checks on each. Later 1.x releases install (peer range `>=1.61.1 <2`) but are unqualified |
| `9l assess` matchers | Playwright 1.61.1 through 1.64.1 | The async matchers are identical across that range. A matcher outside it is reported as an `unknown-matcher` limit, not guessed |
| Browser | Chromium | Other engines are unqualified |
| Node | 24 (22 minimum) | |
| CLI | 0.7.2 on macOS, Linux and Windows, amd64 and arm64 | Provider CLI shell integrations on Windows are unqualified |
| SDK | `@9l/playwright` 0.7.2, released with the CLI | Installing the SDK does not install the CLI or browsers |
| Android (experimental) | Appium 3.8.0 with UiAutomator2 8.7.0; API 35 emulators (arm64 on macOS, x86_64 in Linux CI); one Android 17 foldable in attached mode | Other phones and Android versions, fold transitions, cancellation on a phone, Windows hosts and iOS are unqualified |

An experimental [iOS Simulator driver](ios-driver.md) ships since 0.7.0
(first in `v0.7.0-beta.1`). It is qualified from a source checkout on one Mac
only and unqualified on hosted CI; it is not part of the support matrix above.

Use the matching CLI and SDK release above for the qualified pairing. The
runtime validates `9l.engine/1` and the exact capabilities `steps`,
`artifact-metadata`, `terminal-outcomes`, `declared-outcomes` (in that order), then validates the
execution evidence. It does not compare package versions or enforce a
same-minor rule. The shared target contract is `9l.target/5`; older target
contracts are rejected at the handshake. Mixed package versions are unqualified even when
their contracts match; update both distributions together before diagnosing
a bridge failure. See the [SDK contract](sdk-bridge.md).

An experimental [shared target contract](targets.md) (`9l.target/5`)
dispatches one deterministic scenario to web and to native targets with the
same `app` fixture. The experimental [Android target](android-driver.md)
drives an owned local emulator, or an installed app on an explicitly selected
phone, through Appium and UiAutomator2. It supports:

- **Native operations:** tap, fill, touch at a point, Back, scroll, bounds,
  viewport, keyboard, terminate and activate, with visible, hidden, text,
  state and invalid-state assertions. Controls are reached by resource ID
  (prefixed or bare, as Flutter reports them) or by text, description, hint
  and label aliases.
- **Queries that keep the session:** `isVisible()` for a screen that may or
  may not appear, and `foreground()` to tell a system window from the app;
  both are recorded as actions, never as assertions.
- **Reading a value:** `text()` returns a shown control's text for a later
  step, such as a code the app displays; recorded as an action, and the
  text is never recorded.
- **Fingerprint sign-in (opt-in):** on an owned emulator booted from a
  prepared snapshot, `fingerprint()` or the portable `biometric()` touches
  the virtual sensor; the driver never sees a PIN, and phones have no such
  operation. On an owned iOS Simulator, `biometric()` presents Face ID or
  Touch ID ([iOS biometrics](ios-driver.md#biometrics-on-the-owned-simulator)).
- **Setup classification:** a locked device, a screen that is off, state a
  previous test left behind, or a posture, width or build that doesn't match
  is reported as a setup problem, not an application failure.
- **Identity and evidence:** the installed build identity, the active display
  and fold posture, and opt-in metadata-only gesture diagnostics.

`9l doctor --android` checks Java, the SDK, AVDs, devices and the pinned
Appium tooling. Every emulator case runs nightly in CI on hosted Linux, three
times, with a pass rate per case; pull requests run a fast subset. The
attached-device cases passed on one Android 17 foldable. Target assertions are
reported by the worker and are not independently verified.

Keep your project's existing Playwright version if it is in the qualified
range; upgrading is not required. To check a release locally, see the
[development guide](development.md).
