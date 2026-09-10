# 9lives runner

A fast Go execution runner for [9lives](https://github.com/Quality-Max/9lives).

- Repository: `https://github.com/Quality-Max/9lives-runner` (private during the initial development phase)
- Go module: `github.com/qualitymax/9lives-runner`
- Release targets: macOS and Linux on amd64 and arm64

This repository owns test discovery, inspectable execution plans, bounded
parallel execution, cancellation, completeness accounting, and local evidence
receipts. Healing remains in the Python `9lives` package until the compatibility
contract is proven and migrated explicitly.

## Current scope

- `9l plan`: resolve Playwright specs before launching any process and explain
  every skipped input.
- `9l run`: execute planned jobs with a run-wide budget, bounded concurrency,
  attempt limits, per-job/shared deadlines, and process-tree cancellation.
- `9l status`, `9l result`, and `9l cancel`: inspect or control a foreground
  run from another terminal using its persisted run ID.
- Execution receipts: atomic JSON receipts plus redacted stdout/stderr evidence
  under `.9lives/receipts/<run-id>/<job-id>/<attempt-id>/`.
- Honest completeness: execution and report validation are separate; skipped,
  canceled, failed, or unvalidated jobs prevent an overall green result.
- `9l heal`: compatibility bridge to `python3 -m ninelives.cli heal`.

The first adapter supports existing Playwright projects with a local
`@playwright/test` dependency. It never uses `npx` to download tooling during a
run.

Supported release targets are macOS and Linux on amd64 and arm64. Windows is
not declared yet because this slice only guarantees owned process-tree
cancellation on Unix platforms.

This slice classifies interruption and persists terminal receipts, but it does
not claim automatic restart or crash recovery. Durable leases and recovery are
owned by QUA-1925.

## Build and use

Go 1.25.13 is used to match `qmax-code`.

```bash
go build -o 9l ./cmd/9l
./9l plan --format json 'tests/*.spec.ts'
./9l run --workers 4 --timeout 5m tests/login.spec.ts
```

Flags may appear before or after spec arguments. Directories are searched
recursively; shell-style globs use Go's `filepath.Glob` rules.

```bash
./9l plan --max-jobs 20 tests/
./9l run tests/ --receipt-dir .9lives/receipts
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
go test -run '^$' -bench . -benchmem ./internal/runner
```

## Design notes

See [architecture decisions](docs/architecture.md) for package boundaries,
qmax-code reuse decisions, and the design reference-derived contracts. The initial
[performance baseline](benchmarks/BASELINE.md) separates runner overhead from
Playwright and browser costs.

The runner borrows design reference's useful separation of plan, execution, and evidence,
plus explicit budget skip reasons and the distinction between “called” and
“validated.” Run IDs are checked on every receipt, and every reserved job gets
a terminal receipt even when cancellation arrives before it starts.

`qmax-receipt` was evaluated but is intentionally not used for these receipts:
that package is a signed manifest of outbound network exposure, while these are
test-execution evidence. If this runner later makes outbound requests, it should
emit a qmax exposure receipt alongside—not instead of—the execution receipt.
