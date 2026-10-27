# 9lives runner

[![npm version](https://img.shields.io/npm/v/@9l/playwright?cacheSeconds=3600)](https://www.npmjs.com/package/@9l/playwright)
[![npm downloads](https://img.shields.io/npm/d18m/@9l/playwright)](https://www.npmjs.com/package/@9l/playwright)
[![CI](https://github.com/Quality-Max/9lives-runner/actions/workflows/ci.yml/badge.svg)](https://github.com/Quality-Max/9lives-runner/actions/workflows/ci.yml)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

[![9lives by QualityMax: Agent fixes. 9l checks the change.](docs/assets/9lives-overview.svg)](https://9lives.run/demo.html#proof)

Your coding agent proposes a fix. **9lives runs the test, compares the results,
and records what its assertions caught.** Keep your Playwright suite and inspect
the evidence locally. No QualityMax account or hosted service required.

[Quickstart](docs/run-existing-project.md) ·
[Runnable first example](demo/first-run/README.md) ·
[Interactive demo](https://9lives.run/demo.html#proof) ·
[Website](https://9lives.run) · [Memes](https://9lives.run/memes) · [Documentation](docs/README.md) ·
[npm SDK](https://www.npmjs.com/package/@9l/playwright) ·
[CLI releases](https://github.com/Quality-Max/9lives-runner/releases)

## Inspect a real experiment

The demo leads with an application fix: **the same reviewed test fails before
the patch and passes after it**. Experimental fix confirmation compares the two
Git revisions. Three injected request faults then each cause an assertion failure.
Inspect the patch, scroll the console, and download the evidence extracts.

| Chapter | What you can inspect | Evidence type |
| --- | --- | --- |
| [A renamed button](https://9lives.run/demo.html#locator) | A broken locator and a verified repair with assertions preserved | Illustrated walkthrough |
| [A missing check](https://9lives.run/demo.html#coverage) | A green test that misses a required outcome | Illustrated walkthrough |
| [Behind the agent](https://9lives.run/demo.html#flow) | Observed controls, bounded actions, and execution receipts | Algorithm walkthrough |
| [A real agent run](https://9lives.run/demo.html#live) | Saved provider decisions and browser execution logs | Recorded execution |
| [Prove the fix](https://9lives.run/demo.html#proof) | Before/after receipts and three caught faults | Recorded execution |

The last experiment records **seven Chromium executions on one synthetic profile
app**, captured on 13 October 2026. Opening the demo does not execute tests or call
an agent. It works in light and dark themes and as a [single offline HTML file](docs/explainer/demo.html).
To execute the eight-chapter lab yourself, follow the [release lab guide](demo/release-lab/README.md).
The earlier [Demo Bank locator repair](demo/README.md#qualitymax-demo-bank) is also reproducible.

From CLI v0.4.8, run `9l doctor` from the application project before the
first scan. It checks
local prerequisites and gives repair commands without downloading browsers or
loading project config. Use `9l doctor --sdk` for SDK runs. See the
[setup diagnostics guide](docs/setup-diagnostics.md) for limits and first-run steps.

## From a failure to a checked fix

1. **Reproduce:** run a reviewed Playwright spec with an assertion that fails on the broken app.
2. **Review:** inspect your agent's proposed application patch or locator repair; preserve the intended behavior.
3. **Confirm:** experimental `9l confirm` / MCP `confirm_finding` runs the same reproduction on each Git revision in isolation.
4. **Challenge:** experimental `9l prove` injects supported fetch/XHR faults and reruns the test.

A passing receipt means the assertions passed and the execution report was
validated. Confirmation shows that the test distinguishes the revisions. A
caught fault means an assertion failed under that fault. **None establishes full
requirement coverage.** See the [confirmation](docs/confirm.md) and [fault testing](docs/prove.md) contracts.

## Run your existing Playwright tests

Install the native CLI on macOS or Linux:

```sh
curl -fsSL https://9lives.run/install.sh | sh
export PATH="$HOME/.local/share/9lives-runner/bin:$PATH"
9l plan tests/checkout.spec.ts --format json
9l run tests/checkout.spec.ts --timeout 2m --attempts 1 --format json
```

Run from your project root and replace the path with an existing spec. Follow the
[quickstart](docs/run-existing-project.md) for prerequisites, expected results and
troubleshooting, or try the [synthetic fault example](demo/first-run/README.md).

Your project supplies its Playwright dependencies, browsers, configuration, and
assertions. The SDK is optional. Native v0.5.2 binaries support macOS, Linux and
Windows on x64 and ARM64. See [Windows ZIP installation](docs/install.md#windows)
and the [existing-project guide](docs/run-existing-project.md). Chromium is qualified;
other browser engines are unqualified.

Connect the tools to a coding agent through native MCP. For Claude Code:

```sh
claude mcp add 9lives -- 9l mcp
```

The [agent setup guide](docs/agent-setup.md) covers Codex, Claude, and the optional
[QA skill](skills/9lives-qa/SKILL.md). Native tools expose execution, assessment,
healing, page inspection, reproduction generation, and fix confirmation.
[qmax-code](https://github.com/Quality-Max/qmax-code) is the separate QA terminal
that can run standalone or connect to QualityMax.

## What is available

- **Local execution:** `plan`, `run`, `status`, `result`, and `cancel` bound execution and record attributable terminal receipts. Each failed test carries a class (assertion, timeout, action timeout, environment), receipts count flaky passes and, with the SDK, the assertion steps the worker reported. `run --wait-for` waits for the application first, so one that never comes up leaves the run incomplete instead of failing its tests.
- **Assessment:** `9l assess` advises on waits, skips, assertions, requirement mappings, and source provenance.
- **Page observations:** `9l scan` runs nine check categories; `9l inspect` ranks controls and locators.
- **Optional SDK:** named steps and bounded goals alongside explicit Playwright assertions. Goal completion is unverified.
- **Experimental:** supported locator healing, revision confirmation, and fetch/XHR fault testing. The demo qualifies one synthetic app in Chromium.
- **Planned:** independent behavioral verification, verified replay, and native mobile qualification.

## Add the optional SDK

Install the published SDK in your Playwright project:

```sh
npm install --save-dev @9l/playwright@0.5.2 @playwright/test@1.64.1
npm exec playwright install chromium
```

Use Node 24 for the qualified runtime (Node 22 minimum) and Playwright 1.61.1
through 1.64.1; an existing project can keep its version in that range. On Linux, use
`npm exec playwright install --with-deps chromium` for browser dependencies.

Install the Go CLI separately. With Go 1.27.3 or newer:

```sh
go install github.com/Quality-Max/9lives-runner/cmd/9l@latest
9l --version
```

Ensure Go's binary directory is on your `PATH`. Prebuilt CLI releases target
macOS, Linux and Windows on amd64 and arm64, so their users do not need Go. See
the [download and checksum instructions](docs/install.md) for availability and
platform selection. Windows ZIPs start with v0.2.0; earlier releases have
macOS/Linux assets only.
The npm package supplies the fixtures; it does not install
the CLI or browsers.

### Run with SDK steps

Keep your application's existing Playwright configuration and reviewed
assertions. For an application serving a checkout page:

```ts
import {test, expect} from '@9l/playwright';

test('checkout confirms an order', async ({page, n9l}) => {
  await page.goto('http://localhost:3000/checkout');
  await n9l.step('place the order', async () => {
    await page.getByRole('button', {name: 'Place order'}).click();
    await expect(page.getByRole('status')).toHaveText('Order confirmed');
  });
});
```

Start the application, then run from the test project:

```sh
9l run tests/checkout.spec.ts --sdk --format json
```

For a complete runnable example that needs no application server or model,
clone this repository and follow the [local checkout demo](demo/README.md).
For an existing application, see the [integration guide](docs/run-existing-project.md).

### What the result establishes

The runner records a terminal outcome for every required test and validates
SDK evidence against the owning run, job and attempt. Required failures,
missing evidence, unexplained skips and interruption prevent an overall green
result. Receipts persist under `.9lives/receipts/`; inspect them with
`9l result <run-id> --format json`.

A passing execution means the test's assertions passed and its report was
validated. It does not prove that the assertions fully cover the requirements.
Independent behavioral verification remains planned.

### Check the page you changed

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

Coding agents get the same checks from `9l mcp` as `scan_url` and
`inspect_page`, plus `generate_playwright_repro`, which writes a spec for a
finding that `run_test` executes and `confirm_finding` checks across a fix.
These checks were previously the separate qmax-mcp npm server.

## What you can do

| Capability | Available behavior | Boundary |
| --- | --- | --- |
| Plan and run | Discover specs, inspect plans, bound concurrency and deadlines | Runs locally installed Playwright; no automatic tooling downloads |
| Inspect and cancel | `status`, `result`, `cancel`, and terminal receipts | Interrupted runs are classified; automatic crash recovery is planned |
| SDK steps | `n9l.step(...)` with normal browser fixtures and assertions | Opt in with `--sdk`; existing execution stays available separately |
| Visual mode | `run --headed` shows the browser, one job at a time by default | Linux needs a display (`xvfb-run -a` on headless machines); slow motion comes from the project's `launchOptions` |
| Bounded goals | `n9l.goal(...)` over finite observed controls | Completion is unverified; live model accuracy and cost are unqualified |
| Test assessment | `assess` and source/branch provenance | Advisory static review; it does not establish executed assertion coverage |
| Fault proof | Experimental `prove`: re-run a spec once per injected fetch/XHR fault and report which an assertion caught | Needs `@9l/playwright` 0.1.1 (0.1.3 for the `--faults` opt-in kinds); a caught fault shows an assertion failed, not that it checks the right thing |
| Finding confirmation | Experimental `confirm`: run a reproduction spec on the unfixed and fixed Git revisions, each in its own checkout, and report confirmed, not-reproduced, fix-ineffective or regressed | Code under test must be started or imported from the checkout; confirmed shows the spec tells the revisions apart, not that it tests the finding |
| Healing | Offline Tier 1 proposals and experimental native Tier 2 verification | Preserve assertions; verify candidates in isolation before application |
| Page checks | `scan`: nine checks from one Chromium page load, baselines and `--fail-on-new`; `inspect`: ranked locators and testability | Observations, not test results; one cold load, not field data; loopback needs explicit consent |
| Reproductions | MCP `generate_playwright_repro` writes a deterministic spec for a finding, URL or goal | Runs nothing; review it, then `run_test` or `confirm_finding` |

See the [CLI reference](docs/cli.md), [assessment guide](docs/test-assessment.md), [prove](docs/prove.md), [confirm](docs/confirm.md),
[native Tier 1](docs/native-tier1.md) and [native Tier 2](docs/native-tier2.md).
Verified replay and native mobile qualification remain subsequent milestones.
Windows process ownership uses kill-on-close Job Objects; native Windows
amd64 and arm64 CI checks Chromium execution and process-tree cleanup.
Provider CLI shell integrations on Windows remain unqualified.

### Add a bounded goal

```ts
await n9l.goal('Fill Name using name, then click Continue.', {
  params: {name: 'Fixture Person'}, maxActions: 3, timeoutMs: 30_000,
});
await expect(page.getByRole('heading', {name: 'Review'})).toBeVisible();
```

Choose `--goal-provider anthropic` or `openai` and configure its credential
securely in Go's calling environment, or `claude`, `codex` or `opencode` to
use that CLI's own subscription login without an API key. Offline `--goal-script` fixtures exercise
the browser/engine contract without paid API calls. Purchase, deletion and
outreach controls stop by default. See [goal contracts](docs/goals.md).

## Requirement contracts

Give `9l assess` a shared `requirements.json` with reviewed intent:

```json
{
  "version": 1,
  "requirements": [{
    "id": "checkout-order",
    "reference": "requirements/checkout",
    "revision": "1",
    "expectedOutcomes": [{"id": "order-count", "description": "Checkout displays exactly one order."}]
  }]
}
```

Put a requirement comment immediately before the test statement and an outcome
comment immediately before the assertion statement:

```ts
import {test, expect} from '@playwright/test';

// @9l-requirement checkout-order
test('checkout displays an order', async ({page}) => {
  await page.goto('/checkout'); // uses your project's baseURL
  await page.getByRole('button', {name: 'Place order'}).click();
  // @9l-outcome order-count
  await expect(page.getByTestId('order')).toHaveCount(1);
});
```

Run `9l assess tests/checkout.spec.ts --requirements requirements.json`.
IDs contain 1–80 ASCII letters, digits, `_`, `.` or `-`, starting with a
letter or digit. Requirement IDs are unique; outcome IDs are unique across
the entire contract. This maps source to intent; assessment does not execute
the spec or prove its assertions protect the requirement. See the
[assessment guide](docs/test-assessment.md) for mapping rules and limits.

## Compatibility

| Component | Qualified | Boundary |
| --- | --- | --- |
| `@playwright/test` | 1.61.1, 1.62.0, 1.62.1, 1.63.0, 1.64.0, 1.64.1 | CI runs the unit, browser, assessment and packed-SDK checks on each. Later 1.x releases install (peer range `>=1.61.1 <2`) but are unqualified |
| `9l assess` matchers | Playwright 1.61.1 through 1.64.1 | The async matchers are identical across that range. A matcher outside it is reported as an `unknown-matcher` limit, not guessed |
| Browser | Chromium | Other engines are unqualified |
| Node | 24 (22 minimum) | |
| CLI | 0.5.2 on macOS, Linux and Windows, amd64 and arm64 | Provider CLI shell integrations on Windows are unqualified |
| SDK | `@9l/playwright` 0.5.2, released with the CLI | Installing the SDK does not install the CLI or browsers |

Use the matching CLI and SDK release above for the qualified pairing. The
runtime validates `9l.engine/1` and the exact capabilities `steps`,
`artifact-metadata`, `terminal-outcomes` (in that order), then validates the
execution evidence. It does not compare package versions or enforce a
same-minor rule. A mixed `0.4.x` pair is not rejected just for differing package
versions, but is not a qualified pairing; align both versions before diagnosing
a bridge failure. See the [SDK contract](docs/sdk-bridge.md).

An experimental [shared target contract](docs/targets.md) dispatches one
deterministic scenario to web and an in-memory native fixture. Real Android
execution remains a subsequent milestone.

Keep your project's existing Playwright version if it is in the qualified
range; upgrading is not required. To check a release locally, see the
[development guide](docs/development.md).

## Add it to your coding agent

The optional [9lives QA skill](skills/9lives-qa/SKILL.md) guides Codex and Claude
through requirements, source provenance, assessment and executed results.
Use the [agent setup guide](docs/agent-setup.md) to add it to a project.
For QualityMax-managed tests, the optional
[connected QA skill](skills/qualitymax-connected-qa/SKILL.md) combines local
inspection with hosted crawling, Jev-assisted decisions and cloud verification.

## Adjacent QualityMax tools

| Tool | Use it for |
| --- | --- |
| [qmax-mcp](https://github.com/Quality-Max/qmax-mcp) | The earlier standalone npm server for scans and inspection; its checks now ship in `9l scan`, `9l inspect` and `9l mcp`, and its hosted-account proxy is not included |
| [9lives Python](https://github.com/Quality-Max/9lives) | Python-only healing options, Cypress/Selenium adapters and watch/report commands |
| [qualitymax-grader](https://github.com/Quality-Max/qualitymax-grader) | An optional offline A–F hygiene score for a spec; its defect checks are now in `9l assess`, and the grade is not a 9lives result or coverage evidence |

These are separate distributions. Installing this runner or SDK does not
install or configure them. The Python package also provides a `9l` command;
use an explicit executable path when both CLIs are installed.
The [installation checks](docs/install.md#verify-the-selected-cli) show how to
find stale binaries; [MCP setup](docs/mcp.md#verify-the-server-your-host-launches)
checks the version and tools exposed by the host's running server.

## Safety and honest limits

Test code executes with the local user's permissions. Use approved targets
and explicitly pass required application environment keys with `--pass-env`.
Goal parameter values stay in the browser worker; provider credentials stay
in Go. Receipts omit page content and attachment bodies by default, and bound
and sanitize diagnostics. Review the [SDK evidence contract](docs/sdk-bridge.md)
for identity, privacy, skip pins and failure handling.

## Architecture

```mermaid
flowchart LR
  Specs[Playwright specs and assertions] --> Go[Go CLI: plan, budgets, scheduling]
  Go --> Worker[TypeScript SDK: fixtures and browser]
  Worker --> Browser[Local Playwright / Chromium]
  Browser --> Worker
  Worker --> Go
  Go --> Receipts[Validated terminal receipts]
  Go -. optional goal decisions .-> Provider[Configured model provider]
```

Go owns execution identity, cancellation and completeness. TypeScript owns
browser handles and fixtures. The bounded `9l.engine/1` protocol joins them.
See [architecture decisions](docs/architecture.md) and the
[performance baseline](benchmarks/BASELINE.md), which separates runner overhead
from Playwright and browser costs.

## Support and responsible disclosure

Use [GitHub Issues](https://github.com/Quality-Max/9lives-runner/issues) for
non-sensitive usage and documentation questions. Report vulnerabilities
privately using the [security policy](SECURITY.md).

## Development

```sh
npm ci
npm exec playwright install chromium
npm run demo
go test -race ./...
go vet ./...
```

The [development guide](docs/development.md) lists SDK qualification, pinned
Python compatibility checks and repository boundaries.
See [contributing](CONTRIBUTING.md) before proposing changes.

## Package and release metadata

The published [@9l/playwright 0.5.2](https://www.npmjs.com/package/@9l/playwright)
and the Go CLI are separate distributions with one shared version: a single
`v<version>` tag releases both. Binary archives include `LICENSE`, `NOTICE` and
release-wide SHA-256 checksums. See the [release runbook](docs/releases.md)
and [changelog](CHANGELOG.md).

## License

[Apache-2.0](LICENSE). Third-party components retain their own licenses and
notices. The SDK package and CLI archives include `LICENSE` and `NOTICE`.
