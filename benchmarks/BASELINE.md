# Initial performance baseline

Measured 2026-09-10 on an Apple Silicon Mac using the `golang:1.25.13` Linux
arm64 container for Go benchmarks and a native darwin/arm64 release-style
binary for CLI/Playwright checks. These numbers are an initial reproducible
baseline, not an SLO.

| Slice | Cold/warm | Result | What is included |
|---|---:|---:|---|
| CLI startup (`9l version`) | cold / warm median | 249 ms / 2.29 ms | first native load / 30 subsequent process starts |
| Plan one installed Playwright spec | cold / warm median | 2.14 ms / 1.87 ms | CLI, discovery, local-tool resolution, JSON output |
| Core plan 100 fake-adapter specs | warm, 200 iterations | 61.4 µs/op | discovery, sorting, IDs, command planning |
| Bounded throughput, 4 × 120 ms jobs, 2 workers | warm, 10 runs | 254 ms/run | scheduling, process starts, events, receipts |
| External cancel of active Playwright process | warm | 54 ms | cancel marker polling, process-group termination, final receipt |
| Local Playwright fixture to final evidence | cold / warm median | 383 ms / 361 ms | Node/Playwright startup, one test, JSON validation, receipt |

The Playwright number intentionally uses a browser-free assertion so runner
overhead is not confused with browser launch. Model calls and provisioning are
not part of this runner slice.

Reproduce the Go measurements with:

```bash
go test -run '^$' -bench . -benchmem -benchtime=200x ./internal/runner
go test -run TestBoundedConcurrencyAndRunIdentity -count=10 ./internal/runner
```

Reproduce the real adapter measurement after installing the fixture:

```bash
npm ci --prefix testdata/playwright
go build -trimpath -o 9l ./cmd/9l
./9l run testdata/playwright/tests/pass.spec.ts --format json
```

Time to first persisted evidence currently equals attempt completion because
output is bounded in memory and atomically published afterward. Streaming safe
evidence earlier is a follow-up optimization; this baseline makes that latency
explicit rather than implying evidence exists before it is durable.
