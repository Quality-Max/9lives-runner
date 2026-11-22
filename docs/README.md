# 9lives documentation

| Guide | Start here when |
| --- | --- |
| [Quickstart](run-existing-project.md) | You want to run one existing Playwright spec without adding the SDK |
| [Runnable first example](../demo/first-run/README.md) | You want a passing local checkout, a missed fault, and the assertion that catches it |
| [Interactive demo](https://9lives.run/demo.html#proof) | You want to inspect recorded agent runs, before/after receipts, and caught faults |
| [Release lab](../demo/release-lab/README.md) | You want to execute the eight-chapter local walkthrough |
| [Install](install.md) | You need the npm SDK and Go CLI |
| [Explainer](https://quality-max.github.io/9lives-runner/) ([source](explainer/README.md)) | You want the framework overview, local setup, cloud workflow, and capability boundaries |
| [Android driver](android-driver.md) | You want experimental Appium/UiAutomator2 execution and its device qualification limits |
| [Local checkout demo](../demo/README.md) | You want an executed result without an application server |
| [Login goal example](login-goal-example.md) | You want an optional isolated SDK harness for bounded login-modal actions |
| [Compatibility](compatibility.md) | You need the qualified CLI, SDK, Playwright, Node and native-target pairings |
| [Page checks](scan.md) | You want `9l scan` and `9l inspect`: nine observation categories from one page load and ranked locators |
| [Shared targets](targets.md) | You want one deterministic scenario dispatched to web and native targets |
| [iOS driver](ios-driver.md) | You want the experimental iOS Simulator target (0.7.0 beta) and its limits |
| [Setup diagnostics](setup-diagnostics.md) | You want `9l doctor` checks before the first run |
| [Healing targets](healing-targets.md) | You are checking which locator repairs healing supports |
| [Runner efficiency](runner-efficiency.md) | You want the runner's measured overhead and its limits |
| [CLI reference](cli.md) | You need plans, budgets, receipts or cancellation |
| [CLI contract](contracts.md) | You are integrating `9l` into another tool through its JSON output and exit codes |
| [Agent setup](agent-setup.md) | You want the optional QA skill workflow |
| [MCP server](mcp.md) | You want an agent to run, heal, assess and confirm specs through MCP tools |
| [QualityMax coding sessions](qualitymax-coding-sessions.md) | You want to know how QualityMax runs 9lives in hosted agent sessions and reads its receipts |
| [Test assessment](test-assessment.md) | You are reviewing requirements and source provenance |
| [Prove](prove.md) | You want to know whether a test's assertions fail when its requests fail (experimental) |
| [Confirm](confirm.md) | You want to know whether a reported finding's reproduction fails before a fix and passes after it (experimental) |
| [Goals](goals.md) | You are configuring bounded observed-control execution |
| [SDK bridge](sdk-bridge.md) | You need protocol, privacy and evidence boundaries |
| [Native Tier 1](native-tier1.md) / [Native Tier 2](native-tier2.md) | You are evaluating healing proposals and isolated candidate verification |
| [Architecture](architecture.md) | You need package boundaries and design decisions |
| [Development](development.md) | You are validating a contribution |
| [Release runbook](releases.md) | You are qualifying and publishing a distribution |
| [Website and installer](website.md) | You are changing 9lives.run or the `install.sh` it serves |

Dated benchmark and validation records document their own source and limits.
They are not claims of production performance or independent semantic proof.

[Independent reviews](reviews/) are external field reports, reproduced unedited
with a maintainer status note. They describe the release the reviewer used.

## What is available

- **Local execution:** `plan`, `run`, `status`, `result`, and `cancel` bound execution and record attributable terminal receipts. Each failed test carries a class (assertion, timeout, action timeout, environment), receipts count flaky passes and, with the SDK, the assertion steps the worker reported. `run --wait-for` waits for the application first, so one that never comes up leaves the run incomplete instead of failing its tests.
- **Assessment:** `9l assess` advises on waits, skips, assertions, requirement mappings, and source provenance.
- **Page observations:** `9l scan` runs nine check categories; `9l inspect` ranks controls and locators.
- **Optional SDK:** named steps and bounded goals alongside explicit Playwright assertions. Goal completion is unverified.
- **Experimental:** supported locator healing, revision confirmation, and fetch/XHR fault testing. The demo qualifies one synthetic app in Chromium.
- **Android (experimental):** Appium and UiAutomator2 execution on the documented emulators and phone; see the [Android driver guide](android-driver.md).
- **iOS Simulator (experimental, beta):** in the 0.7.0 beta releases, qualified from a source checkout on one Mac; see the [iOS driver guide](ios-driver.md).
- **Planned:** independent behavioral verification, verified replay, iOS qualification on hosted CI and real iPhones, and broader Android device qualification.

## Capabilities and their boundaries

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

See the [CLI reference](cli.md), [assessment guide](test-assessment.md), [prove](prove.md), [confirm](confirm.md),
[native Tier 1](native-tier1.md) and [native Tier 2](native-tier2.md).
Verified replay and native mobile qualification remain subsequent milestones.
Windows process ownership uses kill-on-close Job Objects; native Windows
amd64 and arm64 CI checks Chromium execution and process-tree cleanup.
Provider CLI shell integrations on Windows remain unqualified.

## Adjacent QualityMax tools

| Tool | Use it for |
| --- | --- |
| [qmax-mcp](https://github.com/Quality-Max/qmax-mcp) | The earlier standalone npm server for scans and inspection; its checks now ship in `9l scan`, `9l inspect` and `9l mcp`, and its hosted-account proxy is not included |
| [9lives Python](https://github.com/Quality-Max/9lives) | Python-only healing options, Cypress/Selenium adapters and watch/report commands |
| [qualitymax-grader](https://github.com/Quality-Max/qualitymax-grader) | An optional offline A–F hygiene score for a spec; its defect checks are now in `9l assess`, and the grade is not a 9lives result or coverage evidence |

These are separate distributions. Installing this runner or SDK does not
install or configure them. The Python package also provides a `9l` command;
use an explicit executable path when both CLIs are installed.
The [installation checks](install.md#verify-the-selected-cli) show how to
find stale binaries; [MCP setup](mcp.md#verify-the-server-your-host-launches)
checks the version and tools exposed by the host's running server.

## Safety and honest limits

Test code executes with the local user's permissions. Use approved targets
and explicitly pass required application environment keys with `--pass-env`.
Goal parameter values stay in the browser worker; provider credentials stay
in Go. Receipts omit page content and attachment bodies by default, and bound
and sanitize diagnostics. Review the [SDK evidence contract](sdk-bridge.md)
for identity, privacy, skip pins and failure handling.
