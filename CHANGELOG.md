# Changelog

## CLI unreleased

- Add Windows amd64 and arm64 ZIP packaging, with `9l.exe`, legal files and
  SHA-256 checksums, for the next CLI release after v0.1.5.
- Own Windows launcher and descendant processes through kill-on-close Job
  Objects; launch the installed Playwright JavaScript CLI with Node directly.
  Goals use an owner-restricted Windows named pipe with the existing SDK protocol.
  Native CI covers real Chromium execution, timeout/cancel cleanup and isolated
  consumers on both Windows architectures. Windows arm64 tests omit `-race`.
- Clarify the README comparison with qmax-code's broader terminal agent.

Fixes from a CLI 0.1.5 trial on an 87-file suite:

- `9l assess` reports a nested function as a `nested-function` limit only
  when it holds a recognized assertion or wait, once, at the outermost such
  function. Projections such as `(f) => f.token` no longer are; calls and
  branches inside any nested function remain limits of their own. Functions
  passed to `evaluate`, `$eval`, `waitForFunction`, `addInitScript` and the
  other page-function methods on a browser fixture chain run in the browser
  and are not scanned. On the trial suite, `nested-function` findings fell
  from 280 to 79 with no other rule changing. Policy `assessment-source-v8`.
- `9l run` text output prints each unsuccessful receipt's reason under it.
  A spec Playwright's configuration does not select, such as one outside
  `testDir`, now says so instead of only "no completed tests", and a spec
  that fails to load says that instead; the run stays incomplete (exit 3).
- `9l heal` is native and no longer needs Python: it runs the verified
  selector-healing session of `heal-native`, which stays as an alias. Without
  a provider it heals with offline Tier 1 only instead of refusing to start.
  `--run-timeout` also accepts whole seconds, as the Python CLI did. Output is
  the native JSON session result. Python-only options such as `--framework`
  exit 2 and name `9lives heal`; Cypress, Selenium, watch and report remain in
  the Python package. Heal session run results now use lowercase JSON keys
  (`passed`, `executedTests`, `failure`, `receipt`).
- `9l mcp` is a native MCP stdio server with `run_test`, `heal_test` and
  `assess_test`, so MCP hosts configured with `9l mcp` work without Python.
  Paths stay inside the server's working directory, test processes receive
  only `--pass-env` names, calls are cancellable, and `heal_test` applies only
  with `apply: true`. See [MCP server](docs/mcp.md).

## CLI 0.1.5 — 2026-10-09

A machine-readable contract for host integrations such as qmax-code (#27).

- `9l run` exits 3 when the run is incomplete: skipped inputs, cancellation,
  timeouts, infrastructure errors, missing or invalid evidence, or a goal
  that did not complete. Exit 1 now means only that every planned job ran and
  a test failed. Scripts that treat any nonzero exit as not green are
  unaffected; scripts that compare with 1 should also accept 3.
- The run result (`run`/`result --format json`) gains `version: 1`,
  `outcome` (`passed`, `failed`, `incomplete`), `plannedJobs` and
  `skippedInputs`. `complete` is unchanged. `9l status` JSON gains
  `version: 1`. Results from earlier CLIs are upgraded when read.
- The text summary of a failed run says `FAILED` instead of `INCOMPLETE`.
- `9l version --format json` reports the implementation, CLI version and the
  version of every machine-readable output.
- JSON Schemas for every output under `docs/contracts/`, generated from the
  output types and checked in tests; real CLI documents are independently
  validated by `npm run smoke:contracts` in CI; see `docs/contracts.md`.
- Goal-failure classification is per test: a failed or caught goal in one
  test cannot hide an unexpected failure in a sibling test. SDK receipts add
  `nonGoalFailureCount`; old receipts without attribution stay conservative.
- Setup validation errors exit 2 without a result or startup/cancel hint.
  Operational storage errors remain exit 3; dependency-blocked plans remain
  incomplete because their descendants never ran.

- Visual mode: `9l run --headed` passes Playwright's `--headed` flag to every
  job, for both adapters, and runs one job at a time unless `--workers` is
  given. On Linux without `DISPLAY` or `WAYLAND_DISPLAY` the run is refused
  before planning, pointing at `xvfb-run`; `9l plan --headed` needs no display.
  `npm run smoke:visual` verifies from inside the page that the browser was
  headed, with a headless control that must fail.
- Experimental `9l prove <spec>`: a passing baseline records the spec's
  fetch/XHR requests, then each request is re-run under `abort`, `http-500`
  and, for successful JSON responses, `empty-json` faults, one receipted run
  per fault with the project's Playwright retries disabled. Each test is
  judged on its own evidence, so a fault is `caught` only when every test it
  reached failed on an assertion and `survived` when any passed; a test that
  Playwright retried inside the run (`test.describe.configure({retries})`)
  gives `retried`, never `survived`. The other inconclusive results are
  `failed-without-assertion`, `not-exercised`, `not-applicable` (with the
  reason `not-json` or `unreachable`) and `incomplete`; faults beyond
  `--max-faults` are `not-run`. A proof is complete only when every planned
  fault ran with valid evidence and at least one was applied; an incomplete
  proof names its reason and exits 1. The report (version 1, policy
  `prove-network-v2`) is saved under `<receipt-dir>/proofs/` without request
  URLs; `--paths` prints them locally. Prove needs `@9l/playwright` 0.1.1.
- `npm run smoke:prove` qualifies the command against a synthetic shop with
  real Chromium: 6 faults caught on asserted requests, 3 survived on an
  unasserted one.

## SDK 0.1.1 — 2026-10-09

- Published automatically: merging an SDK version bump to `main` now runs the
  full qualification and publishes the qualified tarball with npm trusted
  publishing, then pushes the matching `sdk-v<version>` tag with the release
  deploy key. Pushing the tag by hand still works, and a tag for a version
  already on the registry is skipped rather than published twice.
- `@9l/playwright` overrides the `context` fixture: outside `9l prove` it
  passes the context through unchanged; under it, it records requests or
  applies the one named fault through a private `9l.prove/1` channel, separate
  from `9l.engine/1`, naming each test attempt by the engine's hashed test ID.
  This is the first release the experimental `9l prove` command can use.
- Qualified with every published `@playwright/test` release from 1.61.1
  through 1.64.0: 1.61.1, 1.62.0, 1.62.1, 1.63.0 and 1.64.0. A new
  `playwright-compat` CI job runs the unit, browser, assessment and
  packed-consumer checks on each release, after
  `scripts/select-playwright.cjs` repins every workspace and fixture and
  verifies that each resolves exactly that release. The peer range is
  unchanged (`>=1.61.1 <2`); later releases install but are unqualified.

## CLI 0.1.1 — 2026-10-08

`9l assess` changes from the second external trial, on the same 87-file suite
and a new helper-function spec.

- A matcher whose `then`/`catch`/`finally` chain is awaited or returned is no
  longer `unawaited-assertion`, and the chained call is no longer counted as a
  second matcher.
- A nested function that returns the matcher, such as `() => expect(...)` in
  a map of checks, is no longer `unawaited-assertion`; it stays a
  `nested-function` limit. A callback passed directly to `forEach` still is.
- Parentheses around a matcher or its `then`/`catch`/`finally` chain, as in
  `await (expect(...).toHaveURL(...))` or `() => (expect(...))`, no longer
  hide that it is awaited or returned.
- Assertions and waits inside a resolved helper keep the helper's own
  location and carry the test's call site as `site` (`via` in text output).
  One wait in a shared helper therefore counts once in the summary, not once
  per calling test.
- `toEqual([])` and `toStrictEqual([])` are absence checks for
  `absence-after-wait`.
- `unmapped-outcome` is checked per file: an outcome mapped by any test in
  the file covers it for every test that references the requirement and maps
  at least one of its outcomes. A test mapping none of them is still reported
  for every outcome. The finding counts once per requirement and outcome.
- The async matcher list is checked against Playwright 1.61.1 through 1.64.0,
  whose matchers are identical, and `unknown-matcher` names that range.
- Helper facts version 6, policy `assessment-source-v7`; report version 3 gains
  the optional finding `site`.

## SDK 0.1.0 — 2026-10-08

- `@9l/playwright` peer range is `@playwright/test` `>=1.61.1 <2`, so projects
  on later Playwright 1.x releases can install it; 1.61.1 remains the only
  qualified version (#9).

## CLI 0.1.0 — 2026-10-08

`9l assess` changes from an external trial on an 87-file Playwright suite.

- `--requirements` is optional. Code-level checks run without a contract;
  requirement mapping is reported as not run. An empty value is a usage
  error (#5).
- Suites: several specs, a directory or a quoted pattern (`**` supported)
  give one combined report with per-file reports or error codes and a suite
  summary. A file that cannot be assessed does not stop the others; the
  command then exits 2. At most half the CPUs analyze at once, and a file
  whose analyzer timed out is retried once on its own (#6).
- Helpers: same-file helper functions are resolved, attributing their
  assertions, waits and limits to the calling test. `// @9l-assertion-helper`
  on a function or import declaration counts calls as reviewed assertions.
  Imports into other files are not followed yet (#4).
- Skip guards: file- and `describe`-level `test.skip(condition, reason)`/
  `test.fixme` modifiers are no longer parsed as tests. Unconditional ones
  give `disabled-test`; conditional and after-hook ones give informational
  `conditional-skip`; conditions reading `process.env` give suspected
  `environment-skip`. Calls that could be declarations, such as
  `test.skip(name, run)`, stay disabled tests (#7).
- New finding `absence-after-wait` (suspected) when the first assertion after
  a fixed wait only checks that something is absent or did not happen (#8).
- Unawaited `resolves`/`rejects` matchers are flagged; unawaited matchers
  outside the pinned Playwright API give an `unknown-matcher` limit.
- Every finding carries `code` (#10). Text output omits empty fields, ends with
  a per-rule summary and shows literal test titles with opt-in `--titles` (#11).
- Report version 3, policy `assessment-source-v6`. Consumers must accept the
  `informational` classification, the new rules and limit code, an absent
  `requirementsSHA256` and an optional test `title`.

## CLI 0.0.0 — 2026-10-08

- Consumer installation, checkout demo, documentation index, contribution and
  security guides.
- CLI release gate with full CI qualification, native version/startup checks,
  four platform archives, license files and SHA-256 checksums.
- CLI source version aligned to 0.0.0; release builds embed the validated tag.

## SDK 0.0.0 — 2026-10-08

- First public `@9l/playwright` npm publication under Apache-2.0.
- Opt-in `n9l` fixtures, named steps and bounded goal contracts through
  `9l.engine/1`, with ordinary Playwright assertions retained.
- Qualified packed-artifact checkout success, business failure and ordinary
  Playwright execution. SDK smoke also covers worker/browser timeout and cancel.
- Independent behavioral verification, verified replay and native mobile
  support remain planned; live provider accuracy and cost remain unqualified.
