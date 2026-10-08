# QUA-2151 offline acceptance evidence

This directory records the native Tier-1 proposal slice, before release or platform cutover. Proposals require approval and retain unverified provenance; browser existence controls do not verify a repaired test.

- `frozen-handoff.md`: returned single-writer source manifest, full strict ARM64 race validation and actual pinned Python corpus counts (110 comparisons, nine provenance controls, zero failures/skips).
- `correctness-review.md`: independent read-only ACCEPT for the documented bounded subset; 39 CLI/JS-disabled Chrome controls, 14 supported returned selectors all count1 and whole-code syntax valid, 25 conservative refusals.
- `benchmark/` / `benchmark.md`: 30 seeded/interleaved fresh-process invocations per engine; first reported separately, 29 subsequent samples. Raw responses, imported module identities, source/binary/toolchain and matching pre/post provenance included.
  - Packaging note (2026-10-07): the raw benchmark was originally recorded as a single `benchmark.json` (5,730 lines, sha256 `bb03d353e819e7bb39f4e5bedb9e290c512bf00c9dc7aebed91842a4a039cf28`, retrievable at PR commit `d1c1d1381bbfaf4030017dd4f8295587aa921fa9`). It was mechanically split into `summary.json`, `samples-python-0.1.3.json`, `samples-python-0.2.1.json`, `samples-go-native.json`, `provenance-before.json`, `provenance-after.json` (recombined data verified identical) because GitHub omits diff patches for files this large, which blocked the QualityMax AI diff-review gate. No measurement values changed.
- `benchmark-review.md`: independent evidence-binding review; does not rerun measurements.
- `govulncheck.json`: fresh official ARM64 reachable-vulnerability scan, no published reachable vulnerabilities found; source identity unchanged.
- `host.json`: explicit non-sensitive host observations. Benchmark independently verifies coordinator/Python/binary ARM64. Go compiler itself is darwin/amd64 while output/runtime are ARM64.

The benchmark measures one offline moved-ID proposal per process. Python includes isolated source-only compilation, imports and pinned-provenance bootstrap; Go includes native startup/proposal. Build and coordinator identity checks are excluded. OS/filesystem cache state is uncontrolled. The first Go invocation took 330.623 ms; subsequent median 5.354 ms. Python 0.1.3/0.2.1 first 232.335/214.730 ms and subsequent medians 221.740/232.160 ms. No pure-language ratio, cold-cache, browser/provider throughput or verified-heal claim is made.

Measurement used dirty frozen source on base 75c675b242c007cce657cd58671ae191646fd18f; the raw artifact records that truthfully. Source hashes match the returned frozen handoff and independent review. Adding these evidence files and committing changes patch identity, but does not alter measured Go inputs, oracle or benchmark harness. Never relabel the measured binary as built from the eventual PR/merge commit. Current CI, all enabled advisory reviews on the final PR head and a fresh merged-CLI smoke are separate delivery gates.

The durable correctness report omits one final blank line to satisfy whitespace checks. Its original context artifact digest is recorded in the benchmark review; all substantive report bytes are unchanged.

## Publication copies

Absolute machine paths in these publication copies use `<RUNNER_CHECKOUT>` and
`<PYTHON_ENV>` placeholders. Measurement values and recorded source/binary
digests are unchanged. Path-bearing documents and JSON files have different
bytes from the original collected artifacts; their old whole-file digests do
not validate these edited copies. The original artifacts are retained privately.
