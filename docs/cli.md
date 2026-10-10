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
  for local use. Static findings do not establish executed assertion coverage.
- Experimental `9l prove`: run one spec, then re-run it once per injected
  network fault (`abort`, `http-500`, `empty-json` by default; `http-401`,
  `http-403`, `http-429` and `malformed-json` with `--faults`) on each
  fetch/XHR request it made, and report which faults an assertion caught and
  which survived.
  Requires `@9l/playwright` 0.1.1 or newer; see [Prove](prove.md).
- Experimental `9l confirm`: run one reproduction spec on the revision a
  finding was reported against (`--unfixed`) and on the fixing revision
  (`--fixed`, default the working tree), each in its own Git worktree, and
  report `confirmed`, `not-reproduced`, `fix-ineffective`, `regressed` or
  `inconclusive`. Exits 0 only for confirmed; see [Confirm](confirm.md).
- `9l provenance`: record a declared agent and the current workspace, branch,
  commit and test source for later assessment/execution checks.
- `9l tier1`: bounded offline healing proposals from a JSON request on stdin;
  proposals remain unverified and require approval.
- `9l heal`: execute a failing Playwright spec, verify a selector-only
  candidate from offline Tier 1 or a Tier 2 provider in isolation, and save it
  or apply it with approval. `heal-native` is an alias. See
  [Native Tier 2](native-tier2.md).
- `9l mcp`: serve `run_test`, `heal_test`, `assess_test` and
  `confirm_finding` to coding agents over MCP stdio. See
  [MCP server](mcp.md).

Playwright execution supports existing projects with a local
`@playwright/test` dependency. It never uses `npx` to download tooling during a
run.

Release packaging targets macOS, Linux and Windows on amd64 and arm64.
Windows ZIPs start with the next CLI release after v0.1.5. Owned process-tree
cancellation uses Unix process groups or Windows kill-on-close Job Objects;
native CI exercises real Chromium timeout and cancellation cleanup.

The runner classifies interruption and persists terminal receipts. Automatic
restart, durable leases and crash recovery remain planned.

## Build and use

Go 1.25.13 or newer is required; CI pins 1.25.13. The Go runner and local SDK
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
`claude`, `codex` or `opencode` CLI, else a configured API key; with none it is
offline Tier 1 only. `--run-timeout` takes a duration or, like the Python CLI,
whole seconds. The Python CLI's other healing options, its Cypress and
Selenium adapters and its watch/report commands remain in the Python package:
run them with `9lives`.

```bash
./9l heal tests/login.spec.ts --run-timeout 900 --pass-env BASE_URL
./9l heal tests/login.spec.ts --provider claude --yes
```
