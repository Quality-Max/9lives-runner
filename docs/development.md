# Development and validation

Use Go 1.25.13 or newer, Node 24, and Chromium. Node 22 is the SDK minimum;
Node 24 is the CI-qualified runtime. CI pins Go 1.25.13 and Playwright 1.61.1.

```sh
npm ci
npm exec playwright install chromium
npm test
npm run demo
npm run smoke
npm run smoke:assessment
npm run smoke:package
```

On Linux install browser system dependencies with
`npm exec playwright install --with-deps chromium`.
The SDK smoke runs a real Chromium checkout, rejects a business defect, and
verifies that timeout and cancellation terminate the owned worker and browser.
Package qualification installs the exact packed SDK in an isolated consumer
and separately exercises an ordinary Playwright spec. No model or platform
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
