# Page checks: scan and inspect

Before a coding agent calls a web change done, give it evidence from the page
itself. `9l scan` loads the page once in Chromium with the project's own
Playwright and reports graded findings with reproduction steps and suggested
fixes:

```sh
9l scan https://your-app.dev --format markdown
9l scan http://localhost:3000/ --allow-private-network --format json --out scan.json
9l scan http://localhost:3000/ --allow-private-network --baseline scan.json --fail-on-new
9l inspect http://localhost:3000/login --allow-private-network
```

| Check | What it reports |
| --- | --- |
| `console` | JavaScript errors, warnings and failed requests |
| `links` | Broken and redirecting links, up to `--max-links` |
| `accessibility` | Missing alt text, unlabelled controls, nameless interactive elements, heading structure |
| `performance` | Core Web Vitals LCP and CLS, plus TTFB and FCP load timings (INP, the third Core Web Vital, needs real interaction and is not measured) |
| `seo` | Title and meta description |
| `security_headers` | CSP, HSTS, `X-Content-Type-Options`, `Referrer-Policy` |
| `cookies` | Missing `Secure`/`HttpOnly`/`SameSite`, third-party cookies, known trackers, tracking before consent |
| `mixed_content` | HTTP subresources and form actions on an HTTPS page |
| `weight` | Transfer bytes, request count, render-blocking resources, oversized and uncompressed assets |

The Markdown report opens with the grade and the categories to fix first:

> **Grade: 🔴 F  (0 / 100)**   ·   15 issues found
>
> | Category | Issues | Worst |
> |----------|--------|:-----:|
> | Console errors | `██████████` 4 | 🔴 high |
> | Accessibility | `████████░░` 3 | 🔴 high |
> | Security headers | `██████████` 4 | 🟠 medium |

`--baseline` compares against an earlier JSON result and reports new, fixed
and unchanged findings, so a pipeline can fail only on regressions.
`9l inspect` returns the page's ranked locators (test id, role and label
before text and position), each control's stability and a testability
verdict. A page behind a login is checked with a Playwright storage-state
file and `--acknowledge-private-content`. Loopback targets need
`--allow-private-network`; other private addresses stay blocked. Scans are
observations from one cold page load, not test results.

## Test IDs on an Android screen

`9l inspect --android` asks the same question of a native screen: which
controls a test can find by a test ID, and which only by their copy or
their position. It reads the active window of an attached device or
emulator with one `uiautomator dump` through adb. It installs nothing and
does not tap, type or write to the device.

```sh
9l inspect --android                                    # the only attached device
9l inspect --android --serial emulator-5554 --package com.example.app --format json
```

Each tappable, checkable or editable control gets the handle the
[Android driver](android-driver.md) would use for it:

| Handle | Meaning |
| --- | --- |
| 🟢 stable | A resource ID no other node has: `app.getByTestId("save")`. A Flutter `Semantics(identifier:)` is reported bare; `<package>:id/` is removed. |
| 🟡 repeated ID | A resource ID other nodes on the screen share, such as one per list item. `getByTestId` fails as ambiguous while they are all shown. |
| 🟠 copy only | No ID, but a unique text, description or hint, or that of a label inside the control. The report gives the `controls` alias that would reach it, such as `{accessibilityId: "Alerts", className: "android.widget.Switch"}`. A copy edit or another device language breaks it. |
| 🔴 none | No ID and no label that resolves to it alone, so only coordinates (`tapAt`) reach it. |

The score is the share of controls with a stable ID, and the weakest
handles come first. Matching follows the driver: package-scoped, exact
values, unique among all nodes. The dump covers only what is on screen; a
list scrolled out of view or a screen that is still loading is not
counted, and a screen that keeps animating can refuse the dump. The dump
also fails while an Appium or UiAutomator2 session holds uiautomator, so run
it between tests. The report quotes on-screen text; treat it like a
screenshot of that screen. iOS is not supported yet.

## Agents

Coding agents get the same checks from `9l mcp` as `scan_url`,
`inspect_page` and `inspect_screen` (`platform: "android"`), plus `generate_playwright_repro`, which writes a spec for a
finding that `run_test` executes and `confirm_finding` checks across a fix.
These checks were previously the separate qmax-mcp npm server.

See the [CLI reference](cli.md) for every flag and the
[MCP server](mcp.md) for the agent tools.
