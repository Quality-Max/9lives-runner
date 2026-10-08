# Runner efficiency measurements — 2026-10-08

The Go coordinator has low measured resource use. Repeated Playwright process
launches are the main execution cost for many small files in this experiment.
This is a local synthetic browser qualification, not a production benchmark or
a performance guarantee for an application's database/provider stack.

Measured source: `c7cbcc7f10fda133b4d06d35717729c87234f13e`. Apple M2 Pro,
10 cores, 32 GiB RAM, macOS arm64, Go 1.25.13, Playwright 1.61.1. A native Go
binary and native Node/Chromium ran against generated checkout fixtures.
Each test clicks a real browser control and asserts both confirmation text and
exactly one order with the selected SKU. Build/install work is excluded.

## 500 real-browser tests across 100 files

| Workers | 9l wall time | Direct Playwright | Go peak RSS | 9l process-tree peak RSS | 9l sampled peak CPU |
| --- | --- | --- | --- | --- | --- |
| 1 | 142.22 s | 47.12 s | 19.7 MiB | 0.68 GiB | 2.39 cores |
| 4 | 42.92 s | 18.37 s | 19.7 MiB | 2.61 GiB | 7.25 cores |
| 8 | 34.55 s | 19.95 s | 20.8 MiB | 5.26 GiB | 9.65 cores |

Four-worker time is the median of three successful measurements per engine;
RAM/CPU are maximum observed peaks across those measurements. One/eight workers
have one successful measurement per engine. Four-worker 9l timings ranged
42.66–43.59 s versus 17.82–18.39 s directly. Median CPU time was 206.47 CPU-seconds
for 9l versus 71.30 directly. Thus this file layout costs about 2.34 times the
wall time and 2.90 times the CPU time at four workers.

At four workers, 50/250/500 tests across 10/50/100 files took 5.31/22.40/42.92 s
through 9l. Going from one to four workers improved 500-test time by 3.31 times.
Going from four to eight saved 19.5% wall time while roughly doubling RSS.

The same 500 test cases in **one file**, one worker, took **46.82 s through 9l
versus 47.16 s directly**. This single comparison and the adapter's per-file
command planning point to repeated Node/Playwright/browser startup as the
dominant difference, rather than the Go coordinator. This is an inference,
not a measured attribution of every CPU-second. No batching change is in this PR.

## Planning and earlier measurements

| Planned files, not executed tests | Median CLI time, 3 samples | Maximum Go RSS |
| --- | --- | --- |
| 100 | 15.31 ms | 13.38 MiB |
| 1,000 | 46.15 ms | 19.09 MiB |
| 10,000 | 329.17 ms | 41.38 MiB |

Earlier warm measurements on the same source recorded 7.04 ms CLI startup,
7.14 ms small-plan CLI time, and 188.75 ms source assessment. Ten alternating
browser-free Playwright/9l pairs had medians 544.47/559.55 ms and a **median
paired delta of 13.07 ms**. One cancellation sample returned in 64.65 ms;
that is not a general cancellation latency bound.

The in-process Go 100-file planning microbenchmark measured 0.151 ms/op,
112,493 B/op and 746 allocations/op. A fake-adapter scheduling benchmark with
four 120 ms jobs and two workers measured 256.8 ms versus a 240 ms ideal delay.
These microbenchmarks are synthetic and distinct from CLI/browser measurements.

## Evidence and limits

Numeric evidence is in [benchmarks/2026-10-08](benchmarks/2026-10-08/):
[resource matrix](benchmarks/2026-10-08/resources.json),
[planning](benchmarks/2026-10-08/planning.json),
[paired comparison](benchmarks/2026-10-08/paired-comparison.json),
[CLI](benchmarks/2026-10-08/cli.json), and
[excluded attempts](benchmarks/2026-10-08/excluded.json).
Raw reports, private receipts and process samples remain local under `.context/`.

All 22 included executions checked expected counts and produced 7,210 passing
test executions across repeated fixtures; this is not 7,210 unique tests.
There were no skips or runner retries and no observed owned process survivors.
An interrupted serial run with two browser launch failures and a concentrated
run exceeding the harness's original 30-second cap were excluded and rerun;
their unsuccessful evidence was retained. An initial x86_64 profiler attempt
was rejected for incorrect CPU accounting; reported results use native arm64.

CPU/RSS were sampled every 100 ms. 100% CPU represents one core; the table
expresses sampled process-tree peaks in cores. Summed RSS can count shared pages
twice and sampling can miss brief peaks. Runs used warm caches on a shared Mac,
without an OS cache purge. No application server, database, network business
flow, build, provider call or goal/healing workload is included. 10,000-file
planning does not qualify execution of a 10,000-test browser suite.

Live-provider cost remains **unmeasured**: `OPENAI_API_KEY` and
`ANTHROPIC_API_KEY` were absent. No tokens, dollars or production accuracy are
inferred from offline fixtures.

## Reproduce locally

These profiling scripts are qualified on native arm64 macOS only. Use an
arm64 Python interpreter; the resource harness rejects other architectures.
Install locked project dependencies and Chromium, then install the isolated
numeric profiler dependency:

```sh
npm ci
npm exec playwright install chromium
python3 -m venv .context/resource-profiler-arm64
.context/resource-profiler-arm64/bin/python -m pip install psutil==7.0.0
caffeinate -i .context/resource-profiler-arm64/bin/python scripts/benchmark-resources.py
caffeinate -i .context/resource-profiler-arm64/bin/python scripts/benchmark-planning-scale.py
```

The resource harness builds current source before timing and records commit,
binary hash and tracked-worktree status. It runs commands sequentially and
checks executed outcomes rather than substituting synthetic passing reports.
It uses an allowlist of environment keys, no provider credentials and no process
arguments/environment dumps. `--resume .context/efficiency-resources-<stamp>`
preserves unsuccessful cases and skips successful cases with the same source
commit/binary. Planning selects the latest completed, non-excluded local matrix.

Next performance slice: explore reusing Playwright/browser workers across files
and a combined outer/inner worker budget, while preserving per-attempt identity,
terminal outcomes, private reports, cancellation and cleanup. Qualify failures,
timeouts and cancellation before claiming equivalent batching support.
