# Assessment implementation, evidence and next-change handoff

This PR carries the portability/reliability implementation previously submitted
in PR #9, preserves the earlier assessment findings and adds measured runner
costs. PR #8 is already merged into the base; its features and review fixes are
documented here rather than reimplemented.

Earlier merged foundations are also retained: PR #5 added native provider
transports and the isolated healing verification lifecycle; PR #6 added the
local Playwright SDK and bounded typed browser goals; PR #7 moved ordinary
Playwright JSON evidence into the private per-attempt file and qualified state
redaction in a real browser. Regular-adapter stdout compatibility was reproduced
with Playwright 1.61.1. Goal completion remains unverified; offline provider
qualification does not establish live-provider accuracy or cost. These merged
changes are in the base and are not duplicated by this PR.

## Implemented in the base, PR #8

- Advisory `9l assess` with explicit shared requirement/outcome mapping,
  separate purpose/alignment/adequacy/runtime/engineering findings and unknown
  results when evidence is missing. Passing and adequate remain separate.
- Opt-in agent creation provenance and pre/post execution checks for selected
  source, workspace, branch and HEAD. Mismatch or unavailable requested
  provenance prevents a passing execution receipt. The snapshot declares an
  agent ID; it does not authenticate authorship or the entire dirty application.
- Canonical free `9lives-qa` skill with Codex/Claude discovery links. Independent
  live harness skill discovery remains unqualified.
- Real Chromium checkout control: a success-banner test misses discarded order
  creation; an order-count assertion detects it. Setup failure and timeout are
  inconclusive. This qualifies one isolated defect, not general semantic review.

Four review findings were verified and fixed before PR #8 merged: disabled
declarations/enclosing suites lost state; ten pinned async matchers were missed;
duplicate outcome IDs could hide a mapping gap; and explicitly requested but
unavailable provenance lacked a diagnostic. The last finding was partly valid:
the result already said unknown and not_run. Fixes retain disabled syntax,
cover all 31 Playwright 1.61.1 async matchers, require globally unique outcome IDs
and emit a sanitized warning without corrupting JSON or changing advisory exit.
Regression cases and browser control qualification passed. Earlier offline
Python comparisons were skipped until pinned fixtures were restored for PR #9.

## Implemented in this PR, including prior PR #9 work

- Embed TypeScript 5.9.3, its Apache license and checksum manifest in a plain
  Go-built artifact. Extract trusted code privately and clean it up after the
  owned helper stops. Consumer TypeScript/npm/cwd resolution and preload hooks
  do not choose code. Node 22+ remains required; no runtime parser download.
- Inspect inline callbacks with template/computed titles once per syntax
  declaration, retaining visible assertions without guessing runtime expansions.
  Preserve unknown for unresolved helpers, shadowed bindings and unsupported
  control flow. Report bounded reason codes and locations.
- Emit fixed parser/Node/syntax/annotation/limit/timeout/cancellation/evidence
  diagnostics with no raw source/errors or partial report. Report/helper version
  2 and policy `assessment-source-v3`; requirement/provenance schemas stay v1.
- CI qualifies locked parser contents, license, standalone portability and
  required assessment tests. The former compressed-byte reproduction check
  failed in Linux CI. It now checks decompressed source/license bytes and all
  manifest hashes; alternate valid gzip encoding is covered by a regression,
  along with rejection of changed source, license and checksum.
- Archive the [full supplied consumer review](consumer-e2e-review-2026-10-08.md),
  including its requirement mapping and all 70 runtime-case boundaries. It is
  historical evidence from another repository, not a fresh run by this PR.
- Add [resource/scaling results and reproduction scripts](../runner-efficiency.md).
  Measured source predates the documentation and gzip-check fix in this PR.

## Consumer review and unresolved test gaps

The supplied review reports 70 tests across five specs passing through pinned
source `2bfe3103a6109a902a2b2f1461faca17d283363a`, one worker/attempt, 1m54.3s.
An initial five-job setup failure was diagnosed as occupied fixture ports; it
is not counted as five assertion failures. Most API responses were intercepted.
UI approvals, exact payloads, failure states and local auth were meaningfully
checked; durability, concurrency and workers were not established by that run.

All six consumer findings remain open in that repository:

| Finding | Classification and next evidence |
| --- | --- |
| F1: durability, global capacity, durable pause, automatic admission | Source-confirmed scope limit; run existing isolated database/worker integration suites and preserve persisted states, attempts and restart/race outcomes. |
| F2: stale response may arrive after final assertion | Suspected adequacy gap; await response/client processing and qualify with a suppression-disabled control. |
| F3: phone plan is never opened; width/count checks incomplete | Missing actions/checks confirmed in source; open/edit the plan, assert nonzero fields, and verify intended target dimensions. |
| F4: capture and personalization matrix incomplete | Suspected coverage gaps; qualify complete/partial/empty/failed/stale captures and grounded claims against controlled evidence. |
| F5: 3.5-second wait cannot establish never retried | Demonstrated fixed-wait syntax; bound the claim and verify clock/worker terminal behavior. |
| F6: visual regression checks only campaigns | Source-confirmed scope limit; rename the claim or add representative panel baselines and interactive accessibility states. |

That source-v2 analyzer recognized 65 declarations versus 70 runtime cases and
misidentified assertions in generated callbacks. This PR addresses that analyzer
class, not the consumer's underlying assertions. The controlled incompatible
package portability fixture is not an actual TypeScript 7 consumer rerun.
Existing test creation provenance remains unknown. Backend/agentic-send suites
and live provider accuracy/cost remain unmeasured by the consumer E2E run.

## Validation record

Before the prior PR #9 commit, local validation passed: locked npm install,
13 Node tests, parser/license reproduction on that host, standalone artifact
portability with absent/incompatible consumer TypeScript and hostile Node hooks,
real Chromium SDK and checkout-control smokes, `go test -race ./...` with
required assessment and pinned Python compatibility checks, `go vet ./...`,
formatting and four CGO-disabled macOS/Linux amd64/arm64 builds.

Pinned Python sources were `8a40d8d5c83f27f84384f060aeed74a3ded7ab77` and
`568c7a6882441c13cdb9bfe8c0190ca0bf7d8240`; local Python 3.13 used pydantic
2.11.7 and jsonschema 4.26.0. CI uses Python 3.11 with the same validator pins.
Initial absent-fixture and Python 3.14 installation failures were resolved and
were never recorded as passes.

Replacement PR local validation passed: 14 Node tests with no skips,
locked parser source/license/checksum qualification, standalone portability,
real Chromium SDK goal/business-failure/redaction/timeout/cancel smoke and
checkout negative controls. A fresh required-assessment/required-contract
`go test -race -p 1 ./... -count=1` passed with the pinned Python interpreter;
`go vet ./...`, formatting and diff checks passed. Both profiling scripts parse
successfully, archived numeric counts were checked, and changed text assets
contained no credential-signature candidates. Final changed-hunk review found
no blocking correctness, privacy, cleanup or completeness issues.

An initial race run overlapped browser/build validation and hit the existing
one-second deadline in `TestCLIProviderPromptIsStdinAndErrorsAreSanitized`.
It was recorded as a failure, not a pass. The complete serial-package rerun
passed without changing that test or timeout; no performance result came from
this validation run.
Historical PR #9 had eight Go/build checks and QualityMax green; its SDK job
failed parser gzip reproduction and skipped downstream SDK steps. Those skipped
steps are not historical CI passes. The replacement fixes that check and starts
a fresh CI run; consult current PR checks for its status.

## Next coherent slice

Keep assessment advisory and per-dimension. General semantic review, automatic
repair, arbitrary mutation controls, MCP packaging and live-agent harness
qualification remain future work. First repair and execute one consumer gap
with a relevant negative control rather than adding an overall adequacy score.
For runner performance, batch/reuse workers only after qualifying cancellation,
failure attribution and complete per-attempt evidence. A standalone installation
flow could replace consumer clone/build wrapper scripts; it is not implemented
here. No live provider call, package publication or acceptance checklist closure
is part of this PR.
