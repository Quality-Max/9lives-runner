# Initial performance baseline

Measured 2026-10-06 on native darwin/arm64 with Go 1.25.13. These numbers are
a reproducible local baseline, not an SLO. The first sample is the first
process after a release-style build; it does not claim an OS-cache purge.

| Slice | Cold/warm | Result | What is included |
|---|---:|---:|---|
| CLI startup (`9l version`) | first / warm median | 297.90 ms / 6.27 ms | 30 subsequent process starts |
| Plan one installed Playwright spec | first / warm median | 4.52 ms / 5.07 ms | CLI, discovery, local-tool resolution, JSON output |
| Core plan 100 fake-adapter specs | warm, 200 iterations | 145.628 µs/op, 112514 B/op, 746 allocs/op | native Apple M2 Pro, discovery, sorting, IDs, command planning |
| Bounded throughput, 4 × 120 ms jobs, 2 workers | warm, 10 runs | 277.217 ms/run, 14.74 jobs/s | internal `Execute` timing; expected work delay 240 ms; receipts/events |
| External cancel of active Playwright process | warm | 58.90 ms | cancel marker polling, process-group termination, final receipt |
| Local Playwright fixture to final evidence | first / warm median | 1446.11 ms / 530.01 ms | Node/Playwright startup, one test, JSON validation, receipt |
| First durable output evidence | first / warm median | 1444.50 ms / 528.39 ms | receipt artifact publication |

The Playwright number intentionally uses a browser-free assertion so runner
overhead is not confused with browser launch. Model calls and provisioning are
not part of this runner slice.

Reproduce the Go measurements with:

```bash
go test -run '^$' -bench . -benchmem -benchtime=200x ./internal/runner
python3 scripts/benchmark.py
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
