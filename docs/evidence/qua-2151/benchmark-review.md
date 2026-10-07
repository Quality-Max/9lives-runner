# QUA-2151 accepted v4 benchmark evidence review

**ACCEPT — bounded offline process measurement and frozen source bindings.** No confirmed blocker in the supplied JSON/Markdown or unchanged harness. This accepts the recorded evidence, not general language performance, browser verification, deployment readiness or full Python cutover. The separate v4 correctness ACCEPT remains the source-quality decision. No timings were rerun.

## Samples and response evidence

Independently parsed all **90 raw samples, 30 per engine**, including each first invocation. Reconstructed all 30 seeded permutations with `random.Random(2151)` and checked every sample's unique round/position against the recorded order. All elapsed values are positive and finite. Every `raw_stdout` parses to its recorded response. Independently recomputed first, subsequent count29, median, minimum and maximum from raw values; both JSON summaries and every rounded Markdown row agree. No sample is removed or substituted.

| Engine | First recorded ms | Subsequent29 median ms | Subsequent29 min ms | Subsequent29 max ms |
|---|---:|---:|---:|---:|
| Python0.1.3 | 232.335 | 221.740 | 210.777 | 772.579 |
| Python0.2.1 | 214.730 | 232.160 | 213.810 | 568.542 |
| Go native | 330.623 | 5.354 | 4.784 | 6.831 |

The first Go invocation **330.623ms** is retained and is slower than both first Python invocations. Its cause is unestablished. The Markdown's Min/Max columns describe subsequent29 samples, not all30; the explicit First column separately discloses the initial sample. Measurement UTC window: **2026-10-06T17:02:31.867816+00:00–17:03:19.045407+00:00**.

All engines receive the one moved-ID fixture `#save` with `<button id="save-new">Save</button>` and original `await page.locator('#save').click();` plus newline. All90 outputs contain the exact replacement `await page.locator('#save-new').click();` plus newline and confidence0.85. All native outputs remain approval-required, `apply:false`, and `provenance:"unverified"`; selected tier is tier2_ai_suggest and attempted tier tier1_auto. The two actual upstreams independently report success and their original `requiresApproval:false`; the evidence preserves this difference instead of claiming governance parity.

All60 Python responses have isolated source-only proofs bound to the correct revision, version and runtime packageVersion, and the full expected owned import sets: nine modules for0.1.3, ten for0.2.1. For every imported module in every sample, checked its name/path confinement, exact tracked-source membership and SHA256 against current source bytes and the artifact's full tracked-source manifest. The module sets/digests are consistent within each pin. Independently revalidated exact clean pinned checkouts through the unchanged helper's read-only `verify_checkout` and enumerated their tracked source manifests:

- 0.1.3: revision `8a40d8d5c83f27f84384f060aeed74a3ded7ab77`, 17 tracked sources, runtime packageVersion0.1.0, manifest `34a203794da487d5c123eb818fe1dde3cc3dade31f15d7bbe2c222ca9afeb1bf`.
- 0.2.1: revision `568c7a6882441c13cdb9bfe8c0190ca0bf7d8240`, 25 tracked sources, runtime packageVersion0.2.1, manifest `dca7b888ceccd8c09757e3d6e34a41b5a005ff66b06bdeab5bc6bb6b950b15d5`.

## Binding and architecture checks

Recomputed the frozen handoff's sorted-JSON self-digest excluding its digest field; independently checked all15 declared owned files and all33 Go files plus go.mod/go.sum. Current hashes before and after review match the accepted v4 handoff and the measurement's equal pre/post provenance. Exact tracked/index/unstaged/untracked patch identity also matches; HEAD remains `75c675b242c007cce657cd58671ae191646fd18f` with the truthful dirty source patch.

| Identity | Accepted freeze / measurement pre+post / review pre+post |
|---|---|
| Frozen manifest | `b7a282535ea6d57816fded39fa12f6f07dee85c30be7bfd5d1dcff9d1086cbe5` |
| All Go/module inputs | `68c24c2eb270259cfcf75d5e49b095b3a9b742dc34c6c3d4d56f99d86db1569f` |
| Exact patch | `b2fe447dff884334f9e9b108f8b543927c07c202b440b5cbffda098e6fe78aa5` |
| Durable and invoked harness | `a6c6791da899136d9ac57d66576f4b1d58450e56d06fb6d5a3d34ad2670854b7` |
| Oracle helper | `0bc11a38052bc0ad9bc65e20ef8610fa6c4f54c8bd0d3186ca958ba54119edd0` |

Recorded platform evidence is native ARM64 coordinator and isolated Python3.11.8, ARM64 target and actual executable-header check. Compiler host is truthfully `go1.25.13 darwin/amd64`; this is not mistaken for binary architecture. Build command uses trimpath/stripping, explicit GOARCH=arm64, CGO_ENABLED=0, GOWORK=off and GOENV=off; recorded Go module metadata confirms ARM64/Darwin, x/net v0.58.0, exact base revision and vcs.modified=true.

Measured binary's recorded before/after SHA256 is `bbd453dfa621c7697774950a3b66423bba03da0cc9cf74aa2e5ce26fdabc337f`. Inspected harness checks source identity after build, immediately before every sample, and finally after all samples; checks binary digest before every sample and at completion; validates responses after each successful subprocess; and records executable-header architecture plus build metadata. The binary resides in a TemporaryDirectory and has been deleted as designed. This review validates those recorded checks and their unchanged implementation; it does not claim a current measured-binary rehash. No binary was rebuilt and no measurement entry point was invoked.

## Scope and artifact digests

Each sample times one fresh subprocess and one offline proposal. Python includes isolated startup, imports, source-only compilation and pinned provenance/bootstrap checks; Go includes startup and native proposal. Build, coordinator identity checks and coordinator response validation are outside the timer. First invocation is first recorded per engine, not proof of a cold filesystem/OS cache; subsequent invocations also use fresh processes. There is no native-Python language ratio, cold-cache claim, provider/browser run, application verification, visibility proof or cutover qualification. This fixture does not measure general healing throughput or end-to-end execution.

- Raw JSON: `bb03d353e819e7bb39f4e5bedb9e290c512bf00c9dc7aebed91842a4a039cf28`.
- Markdown: `03f458c1009188ece2e9efec0df7b9a3585c7134a8b3155fb6cea12ac9c31110`.
- Frozen handoff: `775e2be759d9272133ffc371b57e2089e8f54e0373802042c346e946061de4d1`.
- Independent correctness ACCEPT: `421aa395b96e854fb356a333cc3c0eb09e90d7c97149b4e723addc272efb1444`.

Only this review artifact was written. Read-only source compilation loaded the unchanged harness/helper identity functions; neither `main` nor `sample` was called. No source/test/harness edits, staging, commits, providers/credentials, automated review triggers, external targets or timings occurred. Source and exact patch remain frozen after report creation. Parent may proceed to preserve durable evidence, with first-sample and process-scope caveats intact.
