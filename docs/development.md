# Development and validation

Use Go 1.25.13 or newer, Node 24, and Chromium. Node 22 is the SDK minimum;
Node 24 is the CI-qualified runtime. CI pins Go 1.25.13, and the workspace pins
Playwright 1.61.1. The `playwright-compat` CI job repeats the unit, assessment,
browser and packed-consumer checks on 1.62.0, 1.62.1, 1.63.0 and 1.64.0, after
`scripts/select-playwright.cjs` repins every workspace and fixture and verifies
that each resolves exactly that release. To reproduce one locally:

```sh
node scripts/select-playwright.cjs 1.64.0
npm install && npm install --prefix testdata/playwright
node scripts/select-playwright.cjs --check 1.64.0
```

Restore the pins with `git checkout -- package.json package-lock.json testdata/`
and `npm ci`.

```sh
npm ci
npm exec playwright install chromium
npm test
npm run demo
npm run smoke
npm run smoke:assessment
npm run smoke:package
npm run smoke:prove
# Independent CLI schema qualification (Python 3.11+ with jsonschema==4.26.0):
npm run smoke:contracts
```

On Linux install browser system dependencies with
`npm exec playwright install --with-deps chromium`.
The SDK smoke runs a real Chromium checkout, rejects a business defect, and
verifies that timeout and cancellation terminate the owned worker and browser.
It also checks that a failed goal in one test cannot hide a sibling browser
assertion failure, whether or not the goal error was caught. The contract
smoke validates emitted JSON against every published schema with a separate
Python validator and rejects deliberately malformed documents.
Package qualification installs the exact packed SDK in an isolated consumer
and separately exercises an ordinary Playwright spec. The prove smoke proves
the synthetic shop fixture with real Chromium and checks that faults on the
asserted requests are caught and faults on the unasserted one survive, see
[Prove](prove.md#qualification). No model or platform
account is needed for these checks.

## Go and offline compatibility


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


## Repository layout

| Path | Responsibility |
| --- | --- |
| `cmd/9l/` | Go CLI and command-level tests |
| `internal/` | Planning, scheduling, receipts, assessment and provider transports |
| `packages/playwright/` | TypeScript fixtures and opt-in reporter |
| `testdata/` | Executable controls, protocol fixtures and pinned compatibility inputs |
| `examples/` | Application integration examples |
| `demo/` | Reproducible local checkout walkthrough |
| `docs/` | User guides, contracts, decisions and dated evidence |
| `scripts/` | Qualification, benchmarks and release checks |

Keep private scratch work in `.context/`. Preserve existing test intent and
assertions; distinguish skipped checks from passes. See
[contributing](../CONTRIBUTING.md) and [engineering instructions](../AGENTS.md).
