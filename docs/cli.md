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
- Honest completeness: execution and report validation are separate;
  unexplained skips, canceled, failed or unvalidated jobs prevent an overall
  green result.
- `9l assess`: advisory source assessment, optionally against a shared
  requirements contract (`--requirements`); `--titles` adds literal test titles
  for local use. Static findings do not establish executed assertion coverage.
- `9l provenance`: record a declared agent and the current workspace, branch,
  commit and test source for later assessment/execution checks.
- `9l tier1`: bounded offline healing proposals from a JSON request on stdin;
  proposals remain unverified and require approval.
- Experimental `9l heal-native`: execute a failing Playwright spec, verify a
  selector-only candidate in isolation, and save it or apply it with approval.
- `9l heal`: compatibility bridge to `python3 -m ninelives.cli heal`; this
  command requires the separately installed Python package.

Playwright execution supports existing projects with a local
`@playwright/test` dependency. It never uses `npx` to download tooling during a
run.

Supported release targets are macOS and Linux on amd64 and arm64. Windows is
not declared yet; owned process-tree cancellation is qualified on Unix
platforms.

The runner classifies interruption and persists terminal receipts. Automatic
restart, durable leases and crash recovery remain planned.

## Build and use

Go 1.25.13 or newer is required; CI pins 1.25.13. The Go runner and local SDK
do not require a platform account. Python is needed only for `9l heal` and the
optional compatibility checks in the [development guide](development.md).

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

To select a particular Python interpreter for the compatibility bridge:

```bash
NINELIVES_PYTHON=.venv/bin/python ./9l heal tests/login.spec.ts --yes
```
