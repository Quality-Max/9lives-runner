# Changelog

From 0.2.0 the CLI and `@9l/playwright` share one version and are released
together by one `v<version>` tag. Earlier entries keep their separate CLI and
SDK versions.

## Unreleased

### Evidence

- Results Playwright relabels after a test body has run are visible in
  `--sdk` receipts. Qualifying Playwright's modifiers for 9l found two
  upstream defects, reported as microsoft/playwright#43676 and
  microsoft/playwright#43738: on 1.61.1 through 1.64.1, `test.skip()` in an
  `afterEach` hook reports a test that ran and passed as skipped, and
  `test.fail()` in an after hook or fixture teardown turns a failed test into
  an expected failure, so the summary says `1 passed` and the process exits 0.
  The engine reporter now sends, on `test_begin`, the expected status
  Playwright knew before any attempt ran, and Go compares it with the status
  the attempt ended with. Receipts gain `runtimeExpectedFailures`, failed
  tests that count as expected only because `test.fail()` ran during the
  test (in the body, a suite-level condition function or an after hook; the
  reporter cannot tell which), and `skippedAfterAssertions`, skipped tests
  whose final attempt had already reported a passed assertion step. `9l run` and
  `9l result` print both under the job. Neither changes the status:
  Playwright's semantics for expected failures and pinned skips are kept, and
  an unpinned skip still makes the attempt incomplete. The counts are the
  worker's report, not verified outcomes. The `9l.engine/1` capability list
  is now `steps`, `artifact-metadata`, `terminal-outcomes`,
  `declared-outcomes`; an SDK without it is rejected at the handshake and
  `9l doctor` reports the pair as incompatible, so update the CLI and
  `@9l/playwright` together. Regression fixtures for both defects run in
  `npm run smoke`.

### Assessment

- `9l assess` reports `test.fail()` in an `afterEach`/`afterAll` hook as
  `after-hook-fail`: demonstrated when unconditional, suspected with a
  condition or under a branch. The declaration and body-level forms are not
  reported. Policy is `assessment-source-v15`; the analyzer helper version is
  12 and facts carry `afterHookFails`; report version 3 and suite version 1
  are unchanged.

## 0.6.0-beta — 2026-10-28

### Android (experimental)

- Add the opt-in `@9l/playwright/android` adapter using pinned Appium 3.8.0
  and UiAutomator2 8.7.0. Run native assertions on an owned Android emulator
  or attach explicitly to an installed app on a connected device. Ordinary
  Playwright execution remains separate. Android tooling is installed separately.
- Require exact resource IDs, reject missing or ambiguous controls, check APK
  identity, and keep mutations to one attempt. Runtime metadata is bounded;
  screenshots, page source, device logs and private application content are
  excluded from execution receipts.
- Add cancellation, deadline, device lease and cleanup controls. Reject late
  transport completions even when they run before an overdue timer. Installed-device
  attachment remains experimental: physical disconnect and outer process hard-kill
  cleanup, arbitrary phones, software keyboards and fold layouts are unqualified.
- Target assertions are executed but not independently verified. Semantic screen
  checks, a live combined reporter, the shared finance demo and full epic
  qualification remain later milestones.

### Releases

- Support explicit beta tags through the same CLI and SDK release gates.
  GitHub beta releases are prereleases; npm uses the `beta` dist-tag. Stable
  default installs stay on the current stable release.

## 0.5.5 — 2026-10-28

### Security

- `9l scan`, `9l inspect` and the `scan_url` and `inspect_page` MCP tools no
  longer let a redirect inside the browser reach a destination the network
  policy refuses. Chromium follows redirects without asking the browser
  context's route handler again, so a navigation, image, script, in-page
  fetch or same-origin link check whose first URL passed the policy could be
  redirected to a private, link-local or another loopback address (a cloud
  metadata endpoint, for example) and was requested. A navigation redirected
  that way loaded, and `inspect` could return what it showed. The browser now
  reaches the network only through a local proxy in the helper, which checks
  every hop of every request and connects only to an address that passed the
  policy when it resolved the name. This also pins the browser's connections
  against DNS rebinding, closing the browser half of #108. A hop the proxy
  refuses, or cannot connect to, ends as a network failure in the page:
  `net::ERR_TUNNEL_CONNECTION_FAILED` for HTTPS, which goes through a CONNECT
  tunnel, and `net::ERR_EMPTY_RESPONSE` for plain HTTP, rather than the
  browser's own connection error. A first URL the policy refuses is still
  blocked before it leaves the browser, as before.
- `golang.org/x/net` is 0.62.0, which fixes GO-2026-6860 (quadratic time
  coalescing text nodes when parsing HTML, reachable through the page snapshot
  healing parses) and GO-2026-6866. The HTTP/2 advisories fixed in 0.60.0 did
  not reach code 9l calls.

## 0.5.4 — 2026-10-28

### CLI

- `9l update` checks the stable release marked Latest and asks before
  replacing the running CLI. `--check` checks without installing, and `--yes`
  explicitly approves a non-interactive update. Downloads, checksums, archive
  contents and the candidate's runner/version identity are bounded and
  validated before replacement. Project SDK dependencies are left to the
  project's developer.
- After an interactive text `9l run` finishes, the CLI checks for updates at
  most once per day and offers installation for future runs. CI, JSON output,
  redirected streams, canceled/incomplete runs and MCP do not prompt or check.
  `NINELIVES_NO_UPDATE_CHECK=1` disables automatic checks; explicit `9l update`
  remains available. Update failure does not change the test run's result.

## 0.5.3 — 2026-10-27

### CLI

- A Windows output drain no longer hangs when its initial wait ends while
  another goroutine is reading a synchronous pipe. After stopping the owned
  process job, Windows drains to end of file without inspecting the pipe;
  the byte and time bounds remain as backstops (#198).
- A run deadline reached during `--wait-for` is reported as the run's
  deadline, rather than as an environment failure saying the target was not
  ready. The run remains `timed_out` (#190).
- Output a command wrote before it exited is no longer lost when the machine
  is busy. Test-runner stdout and stderr, healing provider CLIs and the
  helper processes the runner owns used to drop output the runner had not yet
  read 50 ms after the process exited: a failing provider then reported no
  reason (`codex provider failed (exit 1)`), and a successful exit could
  return truncated output as complete. Output is now read until the process
  group has been stopped and every byte already written has reached its
  destination, however slowly. Only a pipe that a writer outside the group
  still holds open, with nothing left unread, is closed after 5 s. After that
  the runner reads at most what an exited writer can have left in a pipe
  (1 MiB) for at most 30 s, so a writer outside the group that never stops
  writing cannot keep the command from returning. Cancelling the run or
  reaching its deadline ends the 5 s wait at once and fails the command. A
  destination that rejects output, or a pipe the runner had to close without
  showing it was drained, fails an otherwise successful command.
- `9l doctor --sdk <spec>` no longer reports `spec/import` as `missing`, and
  so exits 2, for a valid spec that registers its tests through a named
  import from a fixtures module, such as `import { it } from './fixtures'`
  where the module exports `base.extend(…)` from `@9l/playwright`, beside an
  unused `test` from `@playwright/test`. A named import the spec uses the way
  a test function is used (called with a title and a callback outside any
  test or hook body, or through `describe`, a hook, `extend` or `only`) is
  now a warning that the module is not followed. Helpers called any other
  way, such as `login(page)`, a member call `api.it(…)` or a `step('x', fn)`
  wrapper inside a test, do not count, so a plain spec that uses them is
  still `missing`; a local alias or extension of the plain test, such as
  `const t = test.extend({})`, is the plain test. The callback may be a
  function the spec declares once and passes by name, `it('x', body)`,
  typed or not; a named function expression's own name declares nothing
  outside it. A function passed by name to the plain test or a hook,
  `test('x', body)` or `test.beforeEach(seed)`, is a test body, so a step
  wrapper in it does not count either. `spec/fixtures` reads the callbacks
  of such a test function too. Type arguments are read in balance, so
  `base.extend<{ values: Array<string> }>(…)` from a fixtures module may
  register tests, and `test.extend<{ get: () => string }>(…)` is still the
  plain test. Every declarator of a `const`, `let` or `var` list is read, so
  `const other = 1, body = async ({ page }) => {}` declares `body` and
  `const other = 1, t = test.extend({})` is still the plain test. A callback
  may declare type parameters, inline, `it('x', async <T,>({ page }) => {})`,
  or by name, `async function body<T extends Record<string, unknown>>(…)`.
  Doctor reads call arguments, annotations, type arguments and a declared
  callback's head only within a bound; a call or callback it reads only in
  part, such as one behind a 5,000-character comment or a 600-character one
  between `async` and its parameters, is not verified and counts as a
  possible test registration rather than as proof that there is none.

### SDK

- Qualified with `@playwright/test` 1.64.1, which fixes Chromium 155 and
  later aborting a preflighted cross-origin redirect with `net::ERR_ABORTED`
  whenever any route is registered. The `playwright-compat` CI job runs the
  unit, browser, assessment and packed-consumer checks on it, and install
  examples and `9l doctor` repair commands now name 1.64.1. 1.64.0 stays
  qualified; the peer range is unchanged (`>=1.61.1 <2`).

### Release process

- SDK qualification publishes worker and server readiness records by rename,
  so the smoke cannot parse a partially written PID record while checking
  owned timeout and cancellation. A controlled subprocess test checks that
  readiness stays unpublished until its JSON is complete.
- `curl -fsSL https://9lives.run/install.sh | sh` installs the release GitHub
  marks Latest instead of a version pinned in the script, so a release no
  longer needs an installer or website change. The release workflow creates
  each release without marking it Latest and marks it only after the
  published archives, checksums and provenance attestations were verified and
  the SDK was published, and only if it is the highest stable version; a run
  that fails earlier leaves installs on the previous release. The installer
  follows the github.com redirect, not the GitHub API, and accepts only a
  stable tag of this repository; `NINELIVES_VERSION=vX.Y.Z` still installs a
  specific release. The homepage no longer names a CLI version.

## 0.5.2 — 2026-10-27

### CLI

- On Unix, owned process cleanup tracks detached Playwright `webServer`
  descendants as well as the original process group. Deadline, cancellation
  and normal completion stop observed owned children before returning, so
  the server does not retain its listening port for the next run. Process
  observations run every 500 ms during execution and every 50 ms during
  cleanup. Temporary inspection failures can recover; startup and cleanup
  still require fresh successful snapshots and persistent failures remain
  unsuccessful. Tracking is sampled, not kernel containment: a process that
  detaches and loses its parent entirely between observations can escape it
  (#191).
- `9l assess` reads a matcher whose failure is caught as a conditional
  probe, not an assertion: one consumed through `.catch(handler)`, a `.then`
  rejection handler, `Promise.allSettled`, a `.catch` on the `Promise.all`,
  `race` or `any` call carrying it, or awaited inside a `try` block whose
  `catch` does not rethrow, including through a helper called that way. Such a
  matcher cannot fail the test, so it no longer satisfies
  `no-direct-assertion`, counts toward outcome mapping or acts as the positive
  completion signal that clears `absence-after-wait`. It gets the new
  `caught-assertion` finding (informational, assertion adequacy) at the
  matcher. Promise handlers catch only an asynchronous matcher's rejection,
  not a synchronous matcher's throw, which a `try` block catches with or
  without `await`; a promise returned from a `try` block without `await`, by
  a matcher or a helper, is not caught there. `expect.soft` matchers, and
  those through `expect.configure` whose outermost `soft` option is `true`,
  are never probes, since Playwright records their failure on the test either
  way; a caught matcher whose `soft` option cannot be read (a variable, a
  spread, a computed key or a non-literal value) stays an assertion and gives
  the new `possibly-soft` limit. A call made on `expect.configure(...)`, such
  as `.configure(...)` or `.soft(...)`, is no longer counted as a matcher. A
  matcher of unknown timing, or a marked helper not declared `async`, caught
  for only one of a throw and a rejection stays an assertion and gives
  `unknown-matcher`. A chain ending only in `.then(onFulfilled)` or
  `.finally`, a `.catch()` with no handler, a handler that is not a function
  (a literal of any kind, an array or object literal, a template, `null`,
  `undefined`, `void`, `typeof` or a prefix operator), or a handler that
  throws or returns `Promise.reject`, including through parentheses or type
  wrappers, still rejects and stays an assertion. A matcher on
  `expect.poll(...)` is asynchronous however it is spaced and also on an
  `expect.configure(...)` chain, so a caught poll is a probe. Policy
  `assessment-source-v14`; the analyzer helper version is 11, and report
  version 3 and suite version 1 are unchanged (#21).
- `9l assess` no longer reports a plain `for`, `for…of`, `for…in`, `while`
  or `do` loop as `analysis-limit/conditional-flow`, which stays for
  branches that can skip an assertion (`if`, `switch`, `try`, `?:`, `&&`,
  `||`, `??`). A loop gives no limit of its own unless it repeats a fixed
  wait in its body or in a condition or update, directly or through an
  inlined helper (a wait in a `for` initializer or a `for…of` or `for…in`
  input runs once and does not count); that gives the new
  `analysis-limit/loop`, since the order of waits and assertions across
  iterations is not analyzed. Whether a loop runs at all is not evaluated
  (#141).
- Each test in the `9l assess` JSON report lists the assertions it is
  credited with, as `assertions: {"count", "matchers"}`, whatever its limits:
  recognized matchers in its body and resolved same-file helpers, and marked
  helper calls, excluding caught probes. Matcher names come from the pinned
  Playwright API; anything else is `custom` or `assertion-helper`. The text
  report prints `recognized assertions: 2 (toBe, toEqual)` under such a
  test, so direct assertions beside an unresolved helper call are visible
  while the `unresolved-helper` limit stays at that call. No dimension
  changes: static rules still never mark assertion adequacy or runtime
  evidence `supported` (#141).
- A file in which no test declaration is recognized, such as a helper module
  passed as a path, is no longer counted as assessed. Suite summaries count
  it in the new `noTests` field and the text summary as `N with no tests`;
  the file is listed as `file "<path>": no tests`, and its report, like a
  single-file report, carries a `no-tests` limit. Exit codes are unchanged
  (#141).
- The `assess` and `assess-suite` schemas add the test `assertions` summary
  and the summary's `noTests` count (additive).
- `9l scan` and the `scan_url` MCP tool apply the network policy to the
  address a Node-side link check connects to, not only to the answer seen
  before the request. Links to another origin, and same-origin links the page
  could not fetch itself, are requested with `node:http`/`node:https` and a
  lookup that resolves the name again at connect time and refuses the
  connection unless every answer passes, on each redirect hop. A hostname
  whose DNS answer changes between the check and the connection (DNS
  rebinding) can no longer reach a private, link-local or loopback address,
  or a second loopback origin under `--allow-private-network`; the refusal
  reads as before and names no address. Redirect re-validation, the response
  size limit and the 10 s timeout are unchanged, and the timeout now also
  ends a response body nobody reads. The browser's navigation, subrequests
  and WebSockets are not yet pinned (#108).

### Release process

- The release workflow attests SLSA build provenance for every CLI archive
  and for `SHA256SUMS` before publishing them, then downloads the published
  assets, requires the bytes it attested and checks each with
  `gh attestation verify`; a mismatch stops the run before npm publication.
  Only the GitHub Release job gains `attestations: write` and
  `id-token: write`; no permission is added at the workflow level. [Installation](docs/install.md#verify-build-provenance)
  shows the `gh attestation verify` command; the checksum steps remain for
  users without `gh`, and releases up to 0.5.1 have no attestations (#112).
- Every action in the repository's workflows is pinned to a full commit SHA
  with its version in a comment, including CI, which the release runs as its
  qualification gate, and the Pages deployment.

## 0.5.1 — 2026-10-26

### SDK

- A retrying assertion (`expect.poll`, `toPass`, a custom matcher that calls
  `expect`) counts once in `reportedAssertions`, including the assertions it
  runs inside it on every retry, even inside a `test.step`. Previously each
  passing inner assertion was counted as well, and failed retries were
  reported as failed assertion steps. The count is of top-level assertion
  steps, not of every `expect` in the source.
- The prove channel applies two more opt-in faults. `redirect-cross-origin`
  answers 302 with `Location: http://redirect.invalid/`, a reserved name that
  never resolves, so the browser's redirect fails without contacting an
  application host (DNS may still be asked for the name). `delay` holds the
  real response for 35 s, longer than Playwright's default test and `expect`
  timeouts, then lets it through; it is credited when the hold starts and the
  hold ends as soon as the context closes, so a fault run never outlives its
  test. A redirect is held as the server sent it and Chromium follows it
  itself, so the page sees the same redirect as without the fault; one close
  listener per context serves every hold, however often the page polls. The
  manifest's `"9l".faults` lists both.
- When the upstream request of an `empty-json`, `malformed-json` or `delay`
  fault fails in a fault run, the request is now aborted after the
  `not-applicable` (`unreachable`) record instead of being sent a second time,
  which could repeat a request the application had already acted on.

### CLI

- `9l run --wait-for <url|host:port>` (repeatable) and `--wait-timeout` (1s
  to 30m, default 1m) wait, inside the started run and before any test
  process starts, until an http(s) target answers without a server error
  (5xx or 429) or a host:port accepts connections. A target still not ready
  leaves the run incomplete (exit 3) with an error receipt per job reading
  `not started: --wait-for target not ready (environment): …`, instead of a
  test failure against an application that never came up; `9l result` and
  `9l status` read the run back. The wait counts against `--deadline`, and
  `9l cancel` or Ctrl-C during it reports the run canceled. Targets are
  named only as `scheme://host:port` or `host:port`, never with a path,
  query, fragment or credentials. A self-signed https target is reported as
  `certificate not trusted`; wait for its `host:port` instead.
- Agent provenance assessments carry `scope: "spec"` and the text output says
  what a `matched` status compared: the selected spec, its path, branch,
  commit and workspace root. Imported modules are not compared, and the
  wording no longer invites reading `matched` as unchanged dependencies.
- `9l prove --faults` accepts `redirect-cross-origin` and `delay`, planned
  after the existing kinds for every observed request. Defaults stay
  `abort,http-500,empty-json`. An installed SDK that does not report the new
  kinds is refused for them after the baseline with exit 2, as before, and
  `9l doctor --sdk` names them under `sdk/faults`. Reports use policy
  `prove-network-v4`; the rules are unchanged from `v3`. The text output
  widens the kind column so every kind aligns. The real Chromium smoke
  qualifies both kinds on the shop fixture (#43).
- `9l doctor` adds an `executable` check: it compares the file it runs from
  with the `9l` first on PATH, without running either, and warns when they
  differ, since terminals, scripts and MCP hosts start the one on PATH unless
  configured with a full path. It also warns when no `9l` is on PATH, or
  when a relative PATH entry found it. The paths appear only with the new
  `--paths` flag, which is for local reading and never persisted, and then
  in every case, including when the two are the same file.
  `9l doctor --sdk <spec>` reads one spec as text and adds `spec/import`
  (`missing`, so exit 2, only when the binding `test` comes from
  `@playwright/test` and nothing else could provide it; a `test` from a local
  fixtures module, or a package default or namespace import the spec takes
  `.test` from, is a warning, and so is a spec that takes the SDK but also
  registers tests with the plain `test`), `spec/fixtures` (whether a `test`
  or hook callback destructures `page` or `context`) and `spec/config` (the
  nearest `playwright.config.*` and, for a single literal `testDir` in a
  config that neither names `projects` nor spreads or merges other values,
  whether the spec lies under it). In spec mode the
  workspace and package checks describe the project `prove` would use for
  the spec. Neither the spec nor the config is executed; basenames only
  unless `--paths`. (#165)
- `9l assess` text output no longer lists every `analysis-limit` finding
  under its test. After a file's tests, one unindented line counts the limits
  per code (`analysis limits in this file: unresolved-helper ×37,
  nested-function ×12 (--limits lists them)`), counted as in the per-rule
  summary so the two agree; rule findings keep their per-location lines.
  `--limits` restores the per-location listing. The JSON report is unchanged
  and identical with and without `--limits` (#23).

## 0.5.0 — 2026-10-23

### SDK

- `package.json` declares the engine protocol, capabilities and prove fault
  kinds the build implements under `"9l"`, so `9l doctor` can compare an
  installed SDK with the CLI without loading it. A test keeps the field equal
  to the constants the bridge and prove channel send.

### CLI

- `9l run --retries <n>` passes Playwright's own per-test retries to every
  job, replacing the project's setting, so `--retries 0 --attempts N` lets 9l
  own all retries and their evidence. The `--attempts` help says that
  Playwright's configured retries still apply inside each attempt. Receipts
  record `flakyTests`, the tests that failed and then passed on a Playwright
  retry inside the attempt, with or without `--sdk`, and `9l run` / `9l
  result` print `N test(s) passed only on a Playwright retry inside this
  attempt` under the job (additive).
- With `--sdk`, receipts record `reportedAssertions`, the assertion steps the
  worker reported as passed in each executed test's last completed attempt,
  and `passedWithoutAssertions`, the tests that passed while reporting none.
  `9l run` and `9l result` print `N passed test(s) reported no assertion
  step` under such a job, so a green test that asserted nothing is visible.
  These are the worker's report, not verified coverage; `assertionCoverage`
  stays `unknown`. Without `--sdk` the counts are absent, because Playwright's
  JSON reporter serializes only `test.step` steps. Receipt, run-result and
  run-status contracts gain the two optional fields (additive).
- Receipts record `keepAttachments` when `--keep-attachments` was given,
  `failureAttachments` for the attachment files the listed failures
  reference and `retainedAttachments` for those copied into the receipt
  directory. A request that found nothing to copy prints `--keep-attachments:
  the listed failures referenced no attachment files` under the job instead
  of silently doing nothing (additive).
- Each failed test in a receipt carries a `class`: `assertion`, `timeout`,
  `action-timeout`, `environment` (a connection, name-resolution or
  certificate error on navigation, by Chromium or Firefox error code),
  `unexpected-pass` or `other`. It comes from Playwright's structured status
  and its own error prefixes, never from message text, so a test quoting
  those words is still `other`. `9l run` and `9l result` show it after the
  failing line, so a target that never came up is not read as a broken test
  (additive).
- `9l scan --format json` adds `scoring` to the result: the 0–100 scale, the
  points each severity costs (35 critical, 20 high, 10 medium, 4 low, 0 info),
  the grade thresholds (A 90, B 75, C 60, D 40) and the grade. The Markdown
  report prints the same rule under the score, and `docs/cli.md` and the
  `scan_url` description state it, so a `score` of 0 on a page with ten
  medium findings can be checked rather than guessed at.
- `9l scan` reports the page's own aborted prefetch and RSC requests as one
  `info` finding per page, with `occurrences` and up to five distinct `urls`
  (`omittedUrls` counts the rest), instead of one finding per URL. Other
  failed requests stay one finding per URL. The grouped finding has no `url`,
  so its `id` no longer depends on which prefetch was aborted first; against a
  baseline from an earlier release the per-URL findings show as fixed and the
  grouped one as new, once.
- `9l doctor` judges the SDK pair by the protocol and capabilities the
  installed `@9l/playwright` declares, the contract `run --sdk` checks in the
  handshake, instead of by version string. A different release that this CLI
  admits is reported as admitted but unqualified, with the install command
  for the matching release; a different protocol or capability set is
  incompatible and, under `--sdk`, a missing prerequisite. Under `--sdk`, a
  new `sdk/faults` check lists the prove fault kinds the SDK applies. An SDK
  that predates the declaration is reported as such.
- `9l doctor` no longer tells a Node release above the qualified 24 to
  install Node 24. Below 22 stays missing with the install step; any other
  supported release is a warning without one.
- `9l prove` accepts `--config <file>` and `--project <name>` (repeatable),
  applied to the baseline and every fault run as `9l run` applies them. A spec
  outside the nearest config's `testDir` no longer has to move to be proved.
- When `9l run --sdk`, `9l prove` or `9l confirm` cannot run a spec, the
  reason names only the missing package: `@9l/playwright`, `@playwright/test`
  or a `package.json` that declares it. Previously it asked for both packages
  even when one was installed.
- `9l heal` says when Tier 1 and verification were not attempted. In the
  session JSON, and in the MCP `heal_test` result, `tier1` and `verified` are
  `null` when the session ended before that stage ran (the original passed,
  ran zero tests or failed an assertion, or no candidate was proposed);
  previously both read as a failed zero-test run. `9l heal` accepts
  `--format json` (the default, the session JSON) and `--format text` (the
  state and reason, one line per run with `not attempted`, and the verified
  candidate's diff; no JSON). An unknown option is reported as such; only a
  Python-only option such as `--framework` is referred to `9lives heal`.

## 0.4.9 — 2026-10-22

### Security

- Rebuild all native CLI archives with Go 1.27.3 so `crypto/x509` rejects the
  compromised emSign root certificates covered by GO-2026-6809.

### SDK

- No runtime changes; `@9l/playwright` 0.4.9 is released with CLI 0.4.9.

### CLI

- A missing Playwright browser in an ordinary `9l run` is reported as an
  incomplete run (exit 3, `outcome: incomplete`) instead of a validated test
  failure, whether it fails in a test, a persistent-context fixture or global
  setup. The reason names the missing build and the install command to run
  from the Playwright project directory, without the Playwright banner or the
  cache path. Native healing proposes no repair for it and MCP confirmation is
  inconclusive. A `test.fail()` expectation cannot turn it into a pass. SDK
  runs are unchanged.
- `inspect` takes each control's `accessibleName` from Chromium's
  accessibility tree, the name its tree node carries, instead of estimating
  it from the DOM; the estimate remains only as a fallback, with a warning,
  when the tree cannot be read or a control cannot be matched to it. A
  control the page does not render, such as a hidden input, has no tree
  node and keeps its estimate without a warning. A `<select>`'s option text
  is no longer taken as its name, so an unlabelled select found by an id
  gets the "easy to locate, not accessible" caveat, and an
  `<input type="submit">` is named by its value. Locator names follow the
  same rules: input buttons are named by their value and image inputs by
  their alt text, and content names a control only for roles that take
  their name from content.
- The Markdown report adds `(no accessible name)` only for roles that need
  a name; an unnamed `list` or `group`, or a hidden input, is no longer
  flagged. The report, the "easy to locate, not accessible" caveat and the
  accessibility tree's `unnamed` flag share one list of roles that require
  a name, which now also covers roles such as `progressbar`, `meter`,
  `heading`, `table` and `region`.

## 0.4.8 — 2026-10-20

### SDK

- No runtime changes; `@9l/playwright` 0.4.8 is released with CLI 0.4.8.

### CLI

- Add `9l doctor [--sdk] [--format text|json]`, a read-only first-run check
  of Node, the workspace manifest, the locally resolved `@playwright/test`
  and optional SDK versions, and the Chromium and headless-shell files of
  both the test and scan runtimes. It prints repair commands without
  downloading, launching a browser, loading project config or writing project
  files. Exit 0 means no required prerequisite is missing; exit 2 means setup
  is missing or diagnostics could not complete. `--sdk` makes the SDK
  required. Browser files are checked only for the qualified Playwright
  1.61.1 through 1.64.0 registries.
- A missing-browser error from `9l scan`, `9l inspect` and the MCP browser
  tools names the install command pinned to the project's resolved Playwright
  version, with `--with-deps` guidance for Linux.
- An unreachable scan or inspect target from the CLI or MCP reports shared
  guidance about starting the application, its published port and
  `--allow-private-network`, and no longer echoes the navigation URL, query
  string or Playwright call log.
- `9l version` lists the `scan`, `inspect` and `doctor` commands (additive).

### Qualification

- CI runs the doctor and unreachable-target checks in real Chromium on Linux
  and Windows, and the doctor check on every qualified Playwright version.
- Add `npm run demo:first-run`: an ordinary Playwright run on the release
  lab's synthetic shop, then HTTP-500 faults that a weaker test partly misses
  and a stronger test catches. It rejects incomplete or unexpected results and
  makes no model calls.

### Website and documentation

- Make one ordinary Playwright spec the default first-run path in the
  README, existing-project guide and homepage; the login harness moves to a
  separate optional [login goal example](docs/login-goal-example.md).
- Add the [setup diagnostics guide](docs/setup-diagnostics.md).
- Document requirement contracts with an annotated-test example, explain
  that SDK admission checks protocol and capabilities rather than package
  versions, and describe selecting the right `9l` executable on POSIX and
  PowerShell plus MCP server identity and tool discovery.
- Add the `qualitymax-connected-qa` skill for coding agents using QualityMax
  MCP, routed from the existing `9lives-qa` skill.
- The default installer, homepage and overview install verified v0.4.7.

## 0.4.7 — 2026-10-15

### SDK

- No runtime changes; `@9l/playwright` 0.4.7 is released with CLI 0.4.7.

### Assess

Policy `assessment-source-v13`; the analyzer helper version is 10 and report
version 3 is unchanged.

- Parse `.js`, `.mjs`, `.cjs` and `.jsx` specs as Playwright loads them:
  JavaScript with JSX and no TypeScript transform. Type annotations, `as`,
  `interface`, `enum`, `import type`, non-null `!` and other TypeScript-only
  syntax in such a file now fail with the new `typescript-in-javascript`
  diagnostic (exit 2, or the file's `error` in a suite) instead of a clean
  report for a spec Playwright refuses to load. TypeScript specs and sources
  with no known extension still parse as TSX. The check builds a program over
  the single in-memory file; no library, module or type root is resolved.

### CLI

- `9l run`, `9l result` and the MCP `run_test` tool tell an unexpected pass
  apart from a broken test. A `test.fail()` test that passed is still a
  failure, but it is listed with `⚠`, the message `Expected to fail, but
  passed.` and, in the receipt's `failures`, Playwright's `expectedStatus`
  and the last attempt's `status`; the receipt's `validation` text counts
  such tests. Previously it was a bare `✗` with no error text.
- Failure diagnostics come from each test's final completed attempt only. A
  `test.fail()` test that timed out and then passed on retry no longer
  inherits the earlier attempt's error, failing line or attachments. The
  failure context passed to healing and returned by `run_test` drops tests
  with an expected, skipped or flaky outcome and keeps top-level errors such
  as a failed global teardown, so an expected failure's error is no longer
  attributed to the run.
- `9l prove` and `9l confirm` report why a spec cannot run, such as a missing
  `@9l/playwright` build, instead of `prove requires exactly one spec file`
  or `not a Playwright spec this runner can execute`. The help text states
  that both need `@9l/playwright` and that `prove` reaches only fetch and XHR
  requests made through its `page` or `context` fixtures.
- Receipt, run-result and run-status contracts gain the optional
  `expectedStatus` and `status` fields on each failure (additive).

### Website and documentation

- Link the 60-second launch film from the homepage and GitHub overview.

## 0.4.6 — 2026-10-14

### SDK

- No runtime changes; `@9l/playwright` 0.4.6 is released with CLI 0.4.6.

### Qualification

- Add a deterministic duplicate-target fixture with two records sharing an
  Edit button label. Through ordinary Playwright and `9l run`, verify that a
  visibility-only assertion misses a wrong-record defect, while a visible
  editor plus exact record-ID assertion catches it. Reordered rows preserve
  the stable-ID target, and a deliberately wrong expected-ID canary must fail
  at its assertion. CI checks six attributable receipts; this qualifies the
  fixture and assertions, not automatic locator healing.
- Stabilize the heartbeat deadline test on loaded runners by synchronizing
  executor termination and observing heartbeat updates before release.

### Website and documentation

- Align the homepage and GitHub overview with the recorded agent experiment,
  before/after confirmation and controlled-fault evidence; add original
  QualityMax branding, light/dark themes and a Memes gallery.
- Restore the npm downloads badge and SDK link. Add consent-gated analytics
  and document the demo's permitted QualityMax embedding.

## 0.4.5 — 2026-10-13

### SDK

- No runtime changes; `@9l/playwright` 0.4.5 is released with CLI 0.4.5.

### Assess

Policy `assessment-source-v12`; the analyzer helper version is 9 and report
version 3 is unchanged.

- `debugging-leftover` reports `page.pause()` on a browser fixture and
  `debugger` statements (codes `page-pause`, `debugger-statement`).
- `fragile-selector` reports XPath selectors, positional CSS such as
  `:nth-child`, and `first()`, `last()`, `nth(<literal>)`, `nth=` and
  `:nth-match()` (codes `xpath`, `positional-css`, `position`). Values typed
  into locators and key presses are not read as selectors.
- Remove the noisy `unnamed-role` advisory introduced in unreleased policy
  v11. A unique role such as `status`, `alert` or `button` needs no name.
- These findings have their own budget of 512 per file, so they never cut
  assertion analysis short; a report that reaches it says so in its limits.

### Skill

- The `9lives-qa` skill can run the pinned QualityMax grader
  (`qualitymax-grader@0.2.0`) as an advisory hygiene pass. It reports the grade
  separately and never as a 9lives result or as coverage evidence.

## 0.4.0 — 2026-10-13

### SDK

- No changes; `@9l/playwright` 0.4.0 is released with CLI 0.4.0.

### CLI

- `9l scan <url>` and `9l inspect <url>` bring the qmax-mcp page checks into
  the runner, relicensed under Apache-2.0: console errors and failed requests,
  broken links, accessibility, Core Web Vitals, SEO, security headers,
  cookies and trackers, mixed content and page weight from one Chromium page
  load, findings with stable ids, `--baseline` comparison with
  `--fail-on-new`, Markdown and tracker-issue reports, and ranked locators
  with a testability verdict. They run in a Node helper embedded in the
  binary with the project's own Playwright; the helper inherits only what it
  needs to find Node and the browser. Exit 3 means no result was produced.
- `9l mcp` adds `scan_url`, `inspect_page` and `generate_playwright_repro`,
  with snake_case arguments like the other tools. Reproductions are written
  by Go inside the working directory, only as `.spec`/`.test` files and
  never over an existing file without `overwrite`; `sdk: true` targets
  `confirm_finding`. qmax-mcp's `run_playwright_test` and its hosted-account
  proxy are not carried over: `run_test` executes a generated reproduction.
- `9l scan` and `9l inspect` accept `--storage-state` with
  `--acknowledge-private-content` and `--viewport`, and `scan` accepts
  `--weight-budget`, matching the MCP tools.
- `inspect` no longer presents a control's `name` attribute as its accessible
  name: each control gains `accessibleName`, and an unnamed control found by
  a test id or id keeps its stable rating with an "easy to locate, not
  accessible" caveat.
- A refused destination says which rule applied: a loopback target without
  consent names `--allow-private-network`, and with consent a second local
  origin or a private host name (such as a Docker Compose service) is
  explained without disclosing what it resolved to. A missing browser names
  `npx playwright install chromium` instead of Playwright's banner.
- An authenticated scan no longer requests same-origin links that look like
  they sign out or change data (`logout`, `sign_out`, `delete`, `remove`,
  `unsubscribe` and similar); each is reported as a skipped `info` finding.
  Redaction also covers `id_token`, `refresh_token`, session ids, `Basic`
  credentials and JSON Web Tokens.

## 0.3.0 — 2026-10-11

### SDK

- Goal observation sends `context` and `ordinal` for controls that share
  role and label (#46). An older CLI rejects those fields, so with it a goal
  on a page with such duplicates stops as an invalid request instead of
  acting; pages without duplicates are unaffected. Use the matching CLI.
- The goal stop policy also checks the raw name of a control's form,
  fieldset, dialog or landmark, so a generic "Continue" inside a form named
  "Delete account" is blocked.

### CLI

- SDK goals run on a subscription: `--goal-provider claude`, `codex` or
  `opencode` uses that CLI's own login, with no API key and no fallback.
  Each decision runs the CLI in a decision-only mode (Claude Code with no
  tools, MCP servers, user settings or saved session; Codex read-only with
  the decision schema; OpenCode's read-only plan agent), and its reported
  usage is checked against a reservation that includes the CLI's fixed
  input overhead. A CLI answer without reported usage is refused, and an
  OpenCode error event keeps its message, redacted.
- Goal prompts list the goal's earlier actions (typed action, target role
  and label, parameter name, outcome; never values) and say when to `wait`
  for a control that has not appeared. Before, a model could not see that a
  fill had happened and repeated it until the budget ran out.
- `9l run --config <file>` and `--project <name>` (repeatable) choose the
  Playwright config and projects, recorded in each job's `selection` and
  the receipt's evidence command (#38). A spec the config finds no tests in
  now names the config file and its `testDir`, and a run whose test runner
  exits before writing a report, such as for an unknown project, carries
  the runner's own `Error:` line.
- Goals tell apart controls that share role and label: such controls carry
  the name of their form, fieldset, dialog or landmark (`context`) and an
  `ordinal`, and a choice the engine still cannot distinguish stops the
  goal as `ambiguous_target` instead of guessing (#46). The engine also
  applies the stop policy to that context. Scripted goal steps accept
  `context` and `ordinal`.
- Receipts carry each test's annotations (`annotations`, additive), and
  `9l run`/`9l result` print the non-built-in ones under the job, so a test
  that recorded why it returned early is no longer an unexplained green
  (#39, part 1).
- Evidence and diagnostics are cleaner and safer: redaction now covers
  `Authorization: Bearer|Basic|Token …` and bare `Bearer <token>` values;
  terminal escapes are stripped including OSC sequences such as hyperlinks;
  heal failure text is redacted before it is clipped to 3,000 bytes; MCP
  `failureContext` drops values typed into fields like the provider prompt
  does; failed-test titles are capped at 512 bytes; and heal refuses a
  whitespace-only `getBy` name, which would match nearly every control.

### Assess

Policy `assessment-source-v10` covers all of the changes below; report version
3 is unchanged.

- Conditional pre-execution skips no longer lend outcome coverage to sibling
  tests; modifiers in after hooks still do because the test body ran, and
  they are reported as such.
- Outcome mapping no longer credits disabled or unrelated tests: only an
  enabled test that references the same requirement covers a sibling test's
  outcome (#31). `unmapped-outcome` findings carry the code
  `maps-no-outcome` (counted per test) or `unmapped-in-file` (counted once
  per requirement and outcome), so summaries count
  `unmapped-outcome/maps-no-outcome` and `unmapped-outcome/unmapped-in-file`.
- `await Promise.all([expect(a)…, expect(b)…])` and the other Promise
  combinators no longer report their matchers as `unawaited-assertion` or
  count as unresolved helpers, and a matcher whose promise is dropped by
  `[…].map(l => expect(l)…);` or a parenthesized `forEach` callback is now
  reported (#30).

## 0.2.5 — 2026-10-10

### SDK

- Release alongside CLI 0.2.5. SDK runtime behavior and protocols are
  unchanged from 0.2.0.

### CLI

- `9l heal --provider claude` works on a Claude subscription. Provider CLIs
  now receive the user name (`USER`, `LOGNAME`, `USERNAME`), temp, locale,
  Windows profile, proxy and CA-certificate variables. Without `USER`,
  Claude Code could not find a macOS keychain login and every call failed
  as logged out. API keys are still not inherited.
- `--provider codex` and `--provider opencode` heal on a ChatGPT or OpenCode
  subscription through the CLI's own login; `CODEX_HOME`,
  `CLAUDE_CONFIG_DIR`, `OPENCODE_CONFIG` and `OPENCODE_CONFIG_DIR` now reach
  the CLI. Codex runs with `--sandbox read-only --color never` and OpenCode
  with its read-only `plan` agent.
- Provider use is explicit: `9l heal` prints the resolved provider and how
  it is reached before healing starts, `--provider none` /
  `NINELIVES_PROVIDER=none` keeps healing offline with an agent CLI
  installed, and the session JSON and MCP `heal_test` result record
  `provider` and `providerCalls`. `9l mcp` names its heal provider in its
  `initialize` instructions. Auto-detection of an installed CLI or API key
  stays on by default; the setting `NINELIVES_AUTODETECT_PROVIDER=off` turns
  it (and a named CLI's API-key fallback) off, so only a named provider is
  used.
- A failed provider call records why: the session's `providerDiagnostic` and
  `reason` carry the exit code and the last output lines, redacted and capped
  at 512 bytes, instead of "provider did not return a usable candidate". A
  named CLI that is not installed is named ("claude CLI was not found on
  PATH") instead of failing without a reason. The OpenCode prompt is a
  command-line argument and therefore visible in the local process list to
  other users of the machine; the docs now say so (Claude and Codex read the
  prompt on stdin).
- Refusals name the boundary: "not an editable locator action" now says
  what the call log waited for (or how the failure was classified) and which
  call shapes healing repairs. `9l heal` prints the reason of an
  `unverified` or `needs_human` session on stderr.
- Heal failure text (`original.failure` and the others in the session JSON,
  and the Tier 2 prompt) keeps one copy of each Playwright error, without
  terminal escapes and in a stable order. Before, each error appeared twice
  with ANSI codes, using half of the 4 KiB failure budget.
- A refused provider answer says why: the parse error (for example "must
  return exactly one fenced complete file", with the response's first 160
  characters as `providerDiagnostic`), or the lines it changed beyond the
  failed locator's literal.
- Healing edits `getByRole` and other `getBy*` locators. A renamed button,
  `page.getByRole('button', { name: 'Anmelden' })` failing in the call log,
  can be repaired by changing only the name literal; the role, `exact`
  option and everything else must stay the same. Before, such failures were
  refused as "not an editable locator action" before any provider call.
  Chained and regular-expression locators are still not edited.
- Offline Tier 1 re-finds a renamed `getByRole` or `getByLabel` name from
  the ARIA snapshot in the failed run's `error-context.md`, when exactly one
  matching element remains (or exactly one shares a word with the old name).
  A renamed button now heals with no provider at all. The `9l tier1` request
  gains an optional `ariaSnapshot` field. The same snapshot, with values typed
  into fields dropped, redacted and bounded to 3,000 bytes, is the page
  state in the Tier 2 prompt.
- Healing accepts the `page.fill('#id', …)` / `page.click('#id')` shorthand
  for the repairable actions. A correct provider answer for that shape used
  to be paid for and then discarded.
- Native healing skips Tier 1's `#id` -> `[id="id"]` rewrite, which selects
  the same missing element, instead of spending a verification run on it.
  `9l tier1` still reports it.
- Tier 2 no longer asks a provider when the spec has no source shape a
  proposal could be admitted for, such as `page.fill('#id', …)` shorthand,
  a locator used twice or an action sharing its line. Those sessions end
  `unverified` with a reason naming the boundary and make no provider call;
  before, the call was made and its answer always discarded.

- `9l run` without `--sdk`, `9l heal` and MCP `run_test`/`heal_test` find a
  workspace-hoisted Playwright in an ancestor `node_modules/.bin`, as the
  SDK adapter already did. In an npm-workspace monorepo they used to skip
  every spec with "local binary is missing".
- `9l --help` lists `confirm_finding` among the MCP tools, and the run and
  heal options added in this release; a test keeps the MCP line in sync
  with the advertised tools.
- `9l run` selects tests: `spec.ts:LINE` inputs and `--grep` /
  `--grep-invert` pass Playwright's filters through, recorded in each job's
  `selection` (plan, additive) and the receipt's evidence command. A
  selection that matches nothing stays incomplete and says so.
- `9l run --reporter html` (comma-separated) runs the project's reporters
  beside 9l's evidence reporter, so the HTML report is still produced;
  `json` is reserved for evidence.
- `9l run` and `9l result` name each failed test below its job: title,
  failing line, the first lines of the error, and Playwright's
  `error-context.md`, screenshot and trace paths. Receipts carry the same as
  `failures` (additive; receipt version 1). MCP `run_test` returns
  `failures` and the first failure's `error-context.md` as `failureContext`.
- `9l run --sdk --failure-details` reports the same per-test failures. The
  engine protocol still carries no titles or errors; they come from
  Playwright's JSON reporter, written to a separate private file beside the
  evidence stream that never validates the attempt.
- `9l run --keep-attachments` copies those attachments into the receipt
  directory with SHA-256 digests, so the project's next run cannot delete
  them. Without it they are only referenced.

- `9l run` and `9l result` list each skipped input with its reason
  (`SKIP <input> — <reason>`), and the run result carries them as `skipped`.
  A run that is incomplete only because inputs were skipped now says so
  instead of blaming planned jobs.

### Assess

- A file that imports `test` only from a module assess does not recognise
  (a fixture file or another test runner) now names it: the report carries
  `unrecognizedTestImports` and an `unrecognized-test-import` limit, and
  suite summaries count it as `unrecognized` and "not assessed" instead of
  assessed with 0 tests. Exit codes are unchanged.

## 0.2.0 — 2026-10-10

### Release process

- One `v<version>` tag and one `Release` workflow release both
  distributions: CI qualifies the commit once, the CLI archives are published
  as a GitHub Release, and then the SDK tarball that run qualified is
  published to npm. `npm-release.yml` is removed; the SDK is no longer
  published on a push to `main`, and no `sdk-v` tags are created.
- The workflow refuses a tag unless the CLI source version, the SDK manifest,
  the lockfile and the SDK test fixture all declare it, and `npm test` fails
  as soon as those declarations drift apart.
- Rerunning a failed `npm` job skips publication when npm already holds the
  identical tarball and fails when it holds different contents.

### SDK

- 0.2.0 has the same runtime, protocols and fault kinds as 0.1.3; only the
  version changes, to match the CLI.

### CLI

- Add Windows amd64 and arm64 ZIP packaging, with `9l.exe`, legal files and
  SHA-256 checksums. v0.2.0 is the first release with Windows assets.
- Own Windows launcher and descendant processes through kill-on-close Job
  Objects; launch the installed Playwright JavaScript CLI with Node directly.
  Goals use an owner-restricted Windows named pipe with the existing SDK protocol.
  Native CI covers real Chromium execution, timeout/cancel cleanup and isolated
  consumers on both Windows architectures. Windows arm64 tests omit `-race`.
- Clarify the README comparison with qmax-code's broader terminal agent.
- Experimental `9l confirm <spec> --unfixed REV [--fixed REV]` runs a
  reproduction spec on the revision a finding was reported against and on
  the fixing revision (default: the working tree), each as an ordinary SDK
  run in its own detached Git worktree with hooks disabled, and classifies
  each test and the pair as `confirmed`, `not-reproduced`, `fix-ineffective`,
  `regressed` or `inconclusive` (policy `confirm-v1`). The spec comes from
  the working tree, so both sides run identical bytes; `node_modules` is
  linked from the working tree, and a revision whose manifest or lockfile
  differs is flagged. Exit 0 only for confirmed, 1 for another conclusive
  verdict, 2 for usage or setup errors and 3 when inconclusive. Finding text
  is stored only as its SHA-256. `9l mcp` serves it as `confirm_finding`.
  A revision that commits a link or file in place of one of the spec's
  parent directories is refused before anything is written, and a run whose
  goal failed counts as incomplete on either side.
  `npm run smoke:confirm` qualifies it with real Chromium (#42).
- `9l prove --faults` selects fault kinds. New opt-in kinds: `http-401`,
  `http-403`, `http-429` (with `Retry-After: 1`) and `malformed-json` (a
  successful JSON body cut to its first half, so it no longer parses). The
  default is still `abort,http-500,empty-json`. Selecting a kind the installed
  SDK does not report exits 2 after the baseline, before any fault run.
  Reports gain `faultKinds` and use policy `prove-network-v3`. The real
  Chromium smoke qualifies the new kinds on the shop fixture (#43).

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
  only `--pass-env` names, calls (including queued heals and assessments) are
  cancellable, and `heal_test` applies only with `apply: true`. A heal that
  verifies a candidate but cannot save or apply it, or whose provider fails,
  is an error result that keeps its evidence. See [MCP server](docs/mcp.md).

Validator audit (#50): inputs that were accepted too generously now fail.

- SDK evidence frames and prove records must be exactly one JSON object per
  LF-terminated line. A CRLF stream, or whitespace before or after a valid
  object, is malformed instead of being trimmed.
- Playwright JSON reports without a per-test `status` take Playwright's own
  outcome precedence over every attempt instead of the last one. A failed
  attempt followed by a skipped or interrupted retry is a failure, not a
  skip, and a failure followed by a pass is flaky.
- A structured report cut at the capture limit is invalid for every adapter
  ("structured report exceeded capture limit"), even if the prefix parses.
- The persisted output's recorded size describes the redacted file that its
  SHA-256 covers. The largest SDK stream the validator accepts is tested to
  persist unchanged, so `prove` and `confirm` read back what was validated.
- The install guide's checksum selection accepts exactly one line made of a
  lowercase SHA-256, two spaces and the archive name. The shell and PowerShell
  commands are tested as documented against trailing content, duplicates,
  CRLF and wrong digests.

## SDK 0.1.3 — 2026-10-10

- The prove channel applies `http-401`, `http-403`, `http-429` and
  `malformed-json` faults. When the engine sets
  `NINELIVES_PROVE_CAPABILITIES=1`, a baseline reports the kinds this build
  applies; older engines never ask and never receive the record.

## SDK 0.1.2 — 2026-10-10

- Refresh the npm package and installation examples from current main. SDK
  runtime behavior and protocol are unchanged from 0.1.1.
- Release qualification now covers native Windows amd64 and arm64, including
  real Chromium assertions, timeout/cancel cleanup and isolated consumers;
  Windows relies on inherited directory ACLs rather than POSIX mode bits.

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
