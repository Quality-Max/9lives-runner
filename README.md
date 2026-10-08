# 9lives runner

A Go execution core and local Playwright SDK for [9lives](https://github.com/Quality-Max/9lives).

- Repository: [Quality-Max/9lives-runner](https://github.com/Quality-Max/9lives-runner)
- Go module: `github.com/Quality-Max/9lives-runner`
- Release targets: macOS and Linux on amd64 and arm64
- Release workflow: packages `9l-<os>-<arch>.tar.gz` archives containing `9l`,
  `LICENSE` and `NOTICE`; no releases are published yet

This repository owns test discovery, inspectable execution plans, bounded
parallel execution, cancellation, completeness accounting, and local evidence
receipts. Native offline Tier 1 proposals and experimental native Tier 2
healing sessions are available alongside the Python compatibility bridge. See
[native Tier 1](docs/native-tier1.md) and [native Tier 2](docs/native-tier2.md).

## Framework direction

We are building a standalone testing framework that combines ordinary code and
assertions with bounded goal-driven execution. Our standard is a verified
behavioral outcome, with evidence tied to the exact test and attempt. Actions
completing or a model declaring success are insufficient.

The delivery sequence is the local TypeScript SDK and Go bridge, bounded live
goals over observed targets, independent behavioral verification, replay of
verified actions, native mobile qualification, and developer/CI workflows with
measured correctness, latency and cost. Grounding, generation, healing and mobile
contracts should reuse the QualityMax platform's existing work. Local execution
must remain usable without a platform account.

The SDK/bridge and bounded natural-language goal loop run locally. Independent
behavioral verification, verified replay and mobile support remain planned.
Native healing preserves assertions and verifies candidates in isolation before
application; future replay must recheck the expected outcome.

`9l assess` provides opt-in advisory assessment from shared requirements and
syntax-aware Playwright checks. Creation snapshots can also check whether an
agent's test is assessed and executed in the same workspace, branch, commit
and source revision. The [QA skill](skills/9lives-qa/SKILL.md) combines our
available QA skills with this workflow for Codex and Claude. See
[test assessment](docs/test-assessment.md) for usage, the executed checkout
control and analysis limits. Passing execution alone does not establish test
correctness; general semantic review remains planned.

## Local Playwright SDK

`@9l/playwright` is the local Playwright SDK. It adds the
`n9l` fixture with `step` and `goal` methods while retaining normal Playwright
assertions and browser fixtures. Use Node 24 for the qualified runtime (Node 22
is the declared minimum) and `@playwright/test` 1.61.1. Go launches the
installed Playwright runtime with an opt-in reporter and validates the bounded
`9l.engine/1` evidence stream against the owning run, job and attempt. Existing
execution without `--sdk` is unchanged. Under `--sdk`, a skipped test fails the attempt unless it is declared with
`--pin-skip "<file> › <title>"` (see [skip pins](docs/sdk-bridge.md#skip-pins)).

```sh
npm ci
npm run build
npm exec playwright install chromium
mkdir -p .context
go build -o .context/9l ./cmd/9l
.context/9l run testdata/sdk/tests/checkout.spec.ts --sdk --format json
npm test
npm run smoke
```

On Linux, use `npm exec playwright install --with-deps chromium` to install
browser system dependencies too. The smoke uses a real local Chromium checkout,
checks that a business defect stays red, and verifies timeout/cancel terminate
the owned worker and browser. It needs no model, application server or account.

```ts
import {test, expect} from '@9l/playwright';

test('checkout', async ({page, n9l}) => {
  await page.goto('http://localhost:3000/checkout');
  await n9l.step('place the order', async () => {
    await page.getByRole('button', {name: 'Place order'}).click();
    await expect(page.getByRole('status')).toHaveText('Order confirmed');
  });
});
```

## Natural-language goals

```ts
await n9l.goal('Fill Name using name, then click Continue.', {
  params: {name: 'Fixture Person'}, maxActions: 3, timeoutMs: 30_000,
});
await expect(page.getByRole('heading', {name: 'Review'})).toBeVisible();
```

Run with `9l run tests/flow.spec.ts --sdk --goal-provider anthropic` (or
`openai`) and securely configure the corresponding provider key in Go's calling
environment. The provider sees finite observed controls and parameter names;
values stay in the browser worker. Every decision is validated, targets are
rechecked, and actions share bounded attempt budgets. Purchase, deletion and
outreach controls stop by default. Model completion is unverified; explicit
assertions remain the test oracle. Live model accuracy and cost have not yet
been qualified. Offline `--goal-script` fixtures qualify browser/engine behavior
without paid API calls.

For build, install and run commands against an existing application, see
[run an existing project](docs/run-existing-project.md) and the
[ready test harness](examples/playwright-login). See [goal contracts](docs/goals.md) for
limits, cancellation, receipts and qualification boundaries.

See [the SDK bridge contract](docs/sdk-bridge.md) for supported configuration,
identity, limits, evidence privacy and failure behavior. See [AGENTS.md](AGENTS.md)
and [CLAUDE.md](CLAUDE.md) for engineering intentions and delivery standards.

Compatibility fixtures are reviewed against the offline Python `9lives` source:
pinned releases `0.1.3` (`8a40d8d5c83f27f84384f060aeed74a3ded7ab77`) and
`0.2.1` (`568c7a6882441c13cdb9bfe8c0190ca0bf7d8240`). They do not invoke hosted
models or apply a proposed repair without the Python package's approval path.

## Current scope

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
- `9l assess`: advisory source assessment against a shared requirements
  contract; static findings do not establish executed assertion coverage.
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
optional compatibility checks described below.

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

## Verification

```bash
go test -race ./...
go vet ./...
go test -run '^$' -bench . -benchmem -benchtime=200x ./internal/runner
# Offline contract tests, failing instead of skipping when the toolchain is missing:
NINELIVES_REQUIRE_CONTRACT=1 NINELIVES_CONTRACT_PYTHON=/path/to/python3 go test ./...
# Browser-free CLI startup, planning, execution evidence, and active cancellation:
python3 scripts/benchmark.py
```

The canonical receipt snapshot is copied from platform commit
`767d634ba664f090db15cb99a4de19ef1c4de922` under
`testdata/contracts/execution-receipt/`. CI installs Python 3.11 with
`pydantic==2.11.7` and `jsonschema==4.26.0`, then validates the snapshot without
requiring an adjacent platform checkout.

The healing compatibility test runs the real Python Tier 1 healer from the
pinned `9lives` revisions, checked out by CI under `testdata/upstream/`
(`ninelives-0.1.3/` and `ninelives-0.2.1/`; override the parent directory with
`NINELIVES_UPSTREAM_DIR`). Its output is mapped through
`healingbridge.FromUpstream`, which always requires approval for a proposal
even though the Python Tier 1 healer marks locator repairs as auto-applicable.

Without the Python validators (`pydantic`, `jsonschema`) or the upstream
sources these contract tests are skipped locally. CI sets
`NINELIVES_REQUIRE_CONTRACT=1`, so there they fail instead.

## Design notes

See [architecture decisions](docs/architecture.md) for package boundaries,
planning, execution, budgets and evidence contracts. The initial
[performance baseline](benchmarks/BASELINE.md) separates runner overhead from
Playwright and browser costs.

The runner separates planning, execution and evidence. It records explicit
budget skip reasons and distinguishes action execution from validated behavior.
Run IDs are checked on every receipt, and every reserved job gets a terminal
receipt even when cancellation arrives before it starts.

See [release instructions](docs/releases.md) for SDK package qualification and
npm publishing.

## License

This runner and `@9l/playwright` are licensed under [Apache 2.0](LICENSE).
Third-party components retain their own licenses and notices.
