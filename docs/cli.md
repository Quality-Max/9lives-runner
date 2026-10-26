# CLI reference

- `9l plan`: resolve Playwright specs before launching any process and explain
  every skipped input.
- `9l run`: execute planned jobs with a run-wide budget, bounded concurrency,
  attempt limits, per-job/shared deadlines, and process-tree cancellation.
- `9l status`, `9l result`, and `9l cancel`: inspect or control a foreground
  run from another terminal using its persisted run ID.
- Execution receipts: legacy `receipt.json` plus additive strict
  `execution-receipt-1.0.json`, with redacted stdout/stderr evidence under
  `.9lives/receipts/<run-id>/<job-id>/<attempt-id>/`. `receipt.json` is the
  commit point: if it cannot be written, the canonical export is removed too.
  A run whose heartbeat is more than 10 seconds stale is reported as
  interrupted; local receipt persistence does not claim recovery.
- Controlled environment: test processes inherit only what a browser test
  runner needs to start and reach the network (`PATH`, `HOME`, temp and locale
  variables, `DISPLAY`/`WAYLAND_DISPLAY`, `XDG_*`, `PLAYWRIGHT_BROWSERS_PATH`,
  proxy and CA-certificate variables). Application settings and credentials
  are forwarded only when named: `9l run tests/ --pass-env BASE_URL,TEST_USER`.
  Values are read from the caller's environment and never written to the plan
  or receipts.
- Visual mode: `9l run --headed` shows the browser while tests run (both
  adapters; Playwright's own `--headed` flag, which overrides `headless` in the
  config). Headed runs execute one job at a time unless `--workers` is given,
  so windows do not pile up. Linux needs an X11 or Wayland display; without one
  the run is refused before planning, so use `xvfb-run -a 9l run --headed ...`
  on a headless machine. Headed mode changes only how the browser is shown,
  never what is asserted or how evidence is validated. To slow actions down
  while watching, set `use: {launchOptions: {slowMo: 250}}` in the project's
  Playwright config: a config's `launchOptions` replaces any value a fixture
  library sets, so 9l does not offer a flag that could be silently ignored.
- Test selection: `9l run tests/login.spec.ts:12` runs only the test at that
  line (Playwright's `file:line` filter), and `--grep <regexp>` /
  `--grep-invert <regexp>` pass Playwright's title filters to every job.
  The plan shows each job's `selection`, the receipt's evidence command
  records the filter, and a selection that matches no test leaves the run
  incomplete with a reason naming the selection, never green.
- Readiness: `--wait-for <url|host:port>` (repeatable) waits, inside the
  started run and before any test process starts, until an http(s) target
  answers without a server error (a status below 500 other than 429) or a
  host:port accepts a TCP connection, polling every 500 ms for
  `--wait-timeout` (1s to 30m, default 1m). A target still not ready when
  the wait timeout passes leaves the run incomplete (exit 3): no test
  process starts, and each planned job gets an error receipt reading `not
  started: --wait-for target not ready (environment): <target> was not ready
  after 1m0s: dial: connection refused`, so `9l result` and `9l status` read
  the run back and an application that never came up is not reported as
  failing tests. The wait counts against `--deadline`, and `9l cancel` or
  Ctrl-C during it reports the run canceled, like any canceled run. A
  target is named only as `scheme://host:port` or `host:port`, never with
  its path, query, fragment or credentials; the reason is an HTTP status, a
  network error code, `timed out`, `certificate not trusted` or `tls
  handshake failed`. An https target with a self-signed certificate is not
  trusted and never becomes ready; wait for its `host:port` instead, which
  checks only that it accepts connections. The host is a name, an IPv4
  address or a bracketed IPv6 literal (`[::1]:8080`, `http://[fe80::1%25en0]/`);
  any other host with a colon, such as `http://localhost:80:90`, is a usage
  error. `plan` and `--dry-run` validate the targets but do not wait.
- Retries: `--attempts <n>` is 9l's own retry of the whole job, with a receipt
  per attempt. The project's Playwright `retries` still apply inside each
  attempt, so a failing test can run several times within one receipt.
  `--retries <n>` (0–10) replaces that setting for every job; `--retries 0
  --attempts 2` gives Go the whole retry policy and its evidence. Either way
  the receipt records `flakyTests`, the tests that failed and then passed on
  a Playwright retry inside the attempt, and `9l run` / `9l result` print
  `N test(s) passed only on a Playwright retry inside this attempt` under the
  job, so a flaky pass is not read as a clean one. Target jobs (`--target`)
  already run without retries and refuse `--retries`.
- Config and projects: `--config <file>` runs the specs with that Playwright
  config instead of the one Playwright finds (with `--sdk` it replaces the
  detected config), and `--project <name>` (repeatable, up to 16) runs only
  those configured projects, for example a second instance of the app on
  another `baseURL`. Both are recorded in each job's `selection` and the
  receipt's evidence command. A spec the config finds no tests in is
  incomplete with a reason naming the config file and its `testDir`; an
  unknown project is incomplete with Playwright's own "Available projects"
  line.
- Project reporters: `9l run` reads Playwright's JSON reporter as evidence,
  which replaces the reporters in the project's config. `--reporter html`
  (comma-separated names or module paths, up to 8) adds them back beside the
  evidence reporter, so the HTML report is still written; `json` is reserved.
  Traces and screenshots do not need this: they follow the config's `use`
  settings either way.
- Test annotations: receipts carry each test's annotations from its last
  attempt (`annotations`: test title, type, description; up to 100 per job,
  descriptions capped at 256 bytes and redacted), including ones a test pushes
  at run time, such as a note that it returned early on an older build. `9l run`
  and `9l result` print them under the job as `note  <test> — <type>:
  <description>`, except Playwright's own `skip`, `fixme`, `fail` and `slow`,
  which stay in the receipt only. A passed job with such a note may not have
  exercised everything its tests can check. With `--sdk`, annotations need
  `--failure-details`, because the engine protocol carries no titles.
- Assertion steps: with `--sdk`, the receipt records `reportedAssertions`,
  the assertion steps the worker reported as passed in each executed test's
  last completed attempt, and `passedWithoutAssertions`, the tests that passed
  while reporting none. A retrying assertion (`expect.poll`, `toPass`) counts
  once, including the assertions inside it. `9l run` and `9l result` print `N passed test(s)
  reported no assertion step` under such a job. These are the worker's
  report, not verified coverage: `assertionCoverage` stays `unknown`. Without
  `--sdk`, Playwright's JSON reporter carries only `test.step` steps, so the
  counts are absent. See [sdk-bridge.md](sdk-bridge.md).
- Failed tests: below each failed job, `9l run` and `9l result` name every
  failed test (up to 20 per job) with its failing line, the first lines of its
  error and the files Playwright attached to it, such as `error-context.md`
  (the error, an ARIA snapshot of the page and the source with the failing
  line marked), screenshots and traces. The receipt's `failures` field holds
  the same, with the error capped at 1 KiB and redacted, plus Playwright's
  `expectedStatus` for the test and the `status` of its last attempt. A
  `test.fail()` test that passes is a failure in Playwright's model and in
  9l's counts, but it has no error: it is listed with `⚠` instead of `✗`, the
  message `Expected to fail, but passed.`, `expectedStatus` `failed` and
  `status` `passed`, and the receipt's `validation` text counts such tests.
  A `test.fail()` test that fails is an expected outcome and counts as passed,
  also for `heal` and `confirm`. Each failure carries a `class`, read from
  Playwright's structured status and its own error prefixes, never from the
  message text: `assertion` (an `expect` failed), `timeout` (the test's time
  budget ran out), `action-timeout` (one action or navigation timed out),
  `environment` (navigation failed with a Chromium `net::ERR_*` or Firefox
  `NS_ERROR_*` code: connection refused, name not resolved, certificate
  rejected), `unexpected-pass`, or `other` when the error has no such mark,
  such as a test's own thrown error or a WebKit navigation failure. The text
  output shows the class after the failing line, as `[environment]`, so a
  target that never came up is not read as a broken test. An HTTP error
  status on navigation is not an error to Playwright and is not classed.
  By default the
  attachments are only referenced: they are Playwright's own files, which the
  project's next run may delete, and 9l does not retain page content unless
  asked. `--keep-attachments` copies them into the attempt's receipt
  directory with a SHA-256 digest (text redacted; at most 32 MiB per file and
  128 MiB per attempt; only regular files inside the project). The receipt
  records the request as `keepAttachments`, how many attachment files the
  listed failures reference as `failureAttachments` (inline bodies and files
  outside the project are never referenced) and how many were copied as
  `retainedAttachments`; when retention was requested and the listed
  failures reference none, such as a `test.fail()` pin that passed, `9l run`
  and `9l result` say so under the job. With `--sdk`
  the engine protocol carries no titles or errors by design, so failures are
  reported only with `--failure-details`: Playwright's JSON reporter then
  writes a separate private report beside the evidence stream, which explains
  failures and never validates the run.
- Honest completeness: execution and report validation are separate;
  unexplained skips, canceled, failed or unvalidated jobs prevent an overall
  green result.
- Exit codes: `run` exits 0 when every planned job passed, 1 when every job
  ran and a test failed, 2 for usage or setup errors and 3 when the run is
  incomplete. The JSON `outcome` field carries the same classification. See
  the [machine-readable contract](contracts.md) for hosts, schemas and
  `9l version --format json`.
- `9l assess`: advisory source assessment of a spec, or a combined report for
  several specs, a directory or a quoted pattern, optionally against a shared
  requirements contract (`--requirements`); `--titles` adds literal test titles
  for local use. Text output sums analysis limits in one line per file,
  counted per code; `--limits` lists each limit location instead. Static
  findings do not establish executed assertion coverage.
- Experimental `9l prove`: run one spec, then re-run it once per injected
  network fault (`abort`, `http-500`, `empty-json` by default; `http-401`,
  `http-403`, `http-429` and `malformed-json` with `--faults`) on each
  fetch/XHR request it made, and report which faults an assertion caught and
  which survived.
  Requires `@9l/playwright` 0.1.1 or newer, and 0.1.3 or newer for the
  `--faults` opt-in kinds; see [Prove](prove.md).
- Experimental `9l confirm`: run one reproduction spec on the revision a
  finding was reported against (`--unfixed`) and on the fixing revision
  (`--fixed`, default the working tree), each in its own Git worktree, and
  report `confirmed`, `not-reproduced`, `fix-ineffective`, `regressed` or
  `inconclusive`. Exits 0 only for confirmed; see [Confirm](confirm.md).
- `9l provenance`: record a declared agent and the current workspace, branch,
  commit and test source for later assessment/execution checks. A `matched`
  assessment has `scope: "spec"`: it compared the selected spec's bytes, its
  path, the branch, commit and workspace root. Helpers, page objects and the
  module under test are not compared, so `matched` does not mean unchanged
  dependencies; the text output says so.
- `9l tier1`: bounded offline healing proposals from a JSON request on stdin;
  proposals remain unverified and require approval.
- `9l heal`: execute a failing Playwright spec, verify a selector-only
  candidate from offline Tier 1 or a Tier 2 provider in isolation, and save it
  or apply it with approval. `heal-native` is an alias. See
  [Native Tier 2](native-tier2.md).
- `9l mcp`: serve `run_test`, `heal_test`, `assess_test`,
  `confirm_finding`, `scan_url`, `inspect_page` and
  `generate_playwright_repro` to coding agents over MCP stdio. See
  [MCP server](mcp.md).
- `9l scan <url>`: load the page once in Chromium and report console errors
  and failed requests, broken links, accessibility, the Core Web Vitals LCP
  and CLS (INP is not measured) with TTFB and FCP, SEO, security headers, cookies and
  trackers, mixed content and page weight, with a 0–100 score. The score is
  100 minus 35 points per critical, 20 per high, 10 per medium, 4 per low and
  0 per info finding after deduplication, floored at 0; the grade is A from
  90, B from 75, C from 60, D from 40 and F below. Ten medium findings alone
  reach 0. The JSON result states this in `scoring` (scale, weights, grade
  thresholds and grade) and the Markdown report prints it under the score. A
  finding seen more than once carries `occurrences`; the page's own aborted
  prefetch and RSC requests are one `info` finding per page listing up to five
  distinct `urls`, with `omittedUrls` counting the rest, rather than one
  finding per URL. Other failed requests stay one finding per URL. `--checks`
  selects checks by exact name (an unknown name is refused), `--format`
  is `markdown` (default), `json` or `issue` (one tracker-ready block per
  finding, filtered by `--min-severity`), and `--out` writes the report to a
  file. `--baseline <previous.json>` adds the new, fixed and unchanged
  findings, and `--fail-on-new` exits 1 when there is a new one.
  `--weight-budget total_bytes=500000,request_count=40` scans against your
  own page-weight budget (also `render_blocking`, `image_bytes`,
  `script_bytes`).
- `9l inspect <url>`: the page's ranked locators (test id, role and label
  before text and position), each control's locator stability and a page
  testability verdict, as Markdown or `--format json`;
  `--accessibility-tree` adds Chromium's accessibility tree. A control is
  quoted by its accessible name only; a `name` attribute is shown as
  `[name=…] (no accessible name)`.

Both take `--viewport WIDTHxHEIGHT` (default 1366x900) and, for a page behind
a login, `--storage-state <file>` with `--acknowledge-private-content`: the
Playwright storage-state file must be inside the working directory, its
cookies and storage are loaded into a throwaway browser context and never
printed, but the report may reflect private account data. Keep that file
out of version control and out of shared artifacts. Credential shapes in
returned text (tokens in URLs, `Bearer`/`Basic` values, JSON Web Tokens) are
redacted on a best-effort basis; that is not a guarantee. An authenticated
scan checks same-origin links with the session, except links whose path or
query names a sign-out or data change (`logout`, `sign_out`, `delete`,
`remove`, `unsubscribe` and similar): those are reported as skipped `info`
findings rather than requested. Use `--max-links 0` to request none.
Destinations are checked before each request; a hostname whose DNS answer
changes between that check and the connection is not yet pinned (#108).

`scan` and `inspect` are observations from one cold page load on this
machine, not field data. They run in a Node helper embedded in the binary,
with the project's Playwright and its installed Chromium: run them in the
project directory after `npx playwright install chromium`. The helper gets
only the variables it needs to find Node and the browser; credentials and
`--pass-env` values are not passed. Requests to private, link-local and
loopback addresses are refused, including through DNS, redirects and page
subrequests; `--allow-private-network` permits a loopback target such as a
local dev server, and only its origin. A `--baseline` file must be inside the
working directory. Both exit 0 with a result, 1 for new findings under
`--fail-on-new`, 2 for invalid arguments and 3 when no result was produced,
such as a refused or unreachable target, a missing browser or `--timeout`
(default 300 s, or `NINELIVES_RUN_TIMEOUT`).

```bash
9l scan https://example.com --format markdown
9l scan http://localhost:3000/ --allow-private-network --format json --out scan.json
9l scan http://localhost:3000/ --allow-private-network --baseline scan.json --fail-on-new
9l inspect http://localhost:3000/login --allow-private-network
9l inspect http://localhost:3000/account --allow-private-network \
  --storage-state playwright/.auth/user.json --acknowledge-private-content
```

Playwright execution supports existing projects with a local
`@playwright/test` dependency. The installed `node_modules/.bin/playwright` is
taken from the package that declares it or, in an npm, pnpm or Yarn workspace
whose dependencies are hoisted, from the nearest ancestor that has one; the
test runs in the declaring package's directory. It never uses `npx` to
download tooling during a run.

Release packaging targets macOS, Linux and Windows on amd64 and arm64.
Windows ZIPs start with CLI v0.2.0. Owned process-tree
cancellation uses Unix process groups or Windows kill-on-close Job Objects;
native CI exercises real Chromium timeout and cancellation cleanup.

The runner classifies interruption and persists terminal receipts. Automatic
restart, durable leases and crash recovery remain planned.

## Build and use

Go 1.27.3 or newer is required; CI pins 1.27.3. The Go runner and local SDK
do not require a platform account. Python is needed only for the optional
compatibility checks in the [development guide](development.md).

```bash
go build -o 9l ./cmd/9l
./9l plan --format json 'tests/*.spec.ts'
./9l run --workers 4 --timeout 5m tests/login.spec.ts
```

Flags may appear before or after spec arguments. Directories are searched
recursively; shell-style globs use Go's `filepath.Glob` rules.

```bash
./9l plan --max-jobs 20 tests/
./9l run tests/ --receipt-dir .9lives/receipts --pass-env BASE_URL
./9l status <run-id>
./9l cancel <run-id>
./9l result <run-id> --format json
```

`9l heal` and `9l mcp` are native and need no Python. Healing uses the
provider named with `--provider` or `NINELIVES_PROVIDER`, else an installed
`claude`, `codex` or `opencode` CLI, else a configured API key; with none, or
with `--provider none`, it is offline Tier 1 only. Auto-detection is on by
default; `NINELIVES_AUTODETECT_PROVIDER=off` limits healing to a provider
named with `--provider` or `NINELIVES_PROVIDER`. The resolved provider is
printed before healing starts, and the session JSON records `provider` and
`providerCalls`. `--provider claude`, `codex` or `opencode` uses that
CLI's own login, so a Claude, ChatGPT or OpenCode subscription works without
an API key; see
[native Tier 2](native-tier2.md#healing-on-a-claude-chatgpt-or-opencode-subscription). `--run-timeout` takes a duration or, like the Python CLI,
whole seconds. `--format json` (the default) prints the session result on
stdout; `--format text` prints its state and reason, one line per run
(`original`, `tier1`, `verified`; a run the session never reached is
`not attempted`), where a verified candidate was saved or applied, and the
candidate's diff. In the JSON, `tier1` and `verified` are `null` when that
stage was not attempted. The Python CLI's other healing options, its Cypress
and Selenium adapters and its watch/report commands remain in the Python
package: run them with `9lives`.

```bash
./9l heal tests/login.spec.ts --run-timeout 900 --pass-env BASE_URL
./9l heal tests/login.spec.ts --provider claude --yes
./9l heal tests/login.spec.ts --provider none --format text
```
