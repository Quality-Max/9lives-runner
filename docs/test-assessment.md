# Test assessment

Status: local advisory source assessment and opt-in creation provenance are
implemented. One isolated checkout defect is qualified with real Chromium.
General semantic review, automatic repair, arbitrary mutation controls and MCP
packaging remain planned.

A passing run establishes only the behavior asserted in that attempt. It does
not establish that a test protects the right requirement. `9l assess` reports
purpose, intent alignment, assertion adequacy, runtime evidence and engineering
quality separately, using `supported`, `concern` or `unknown`. No overall score
or model opinion upgrades unknown coverage.

## Local assessment

```sh
npm ci
mkdir -p .context
go build -o .context/9l ./cmd/9l
.context/9l assess testdata/assessment/tests/checkout.spec.ts --requirements testdata/assessment/requirements.json --format json
```

Assessment needs Node and TypeScript resolvable from the working directory
(this repository pins TypeScript and Playwright dependencies). It parses
source without importing the spec, configuration or application. Go owns the
helper's ten-second budget, cancellation, output limits and report validation.
Source is limited to 1 MiB, requirements to 256 KiB, recognized tests to 256
and combined assertions/waits to 2,048. Malformed, unavailable or over-limit
analysis exits 2; advisory findings exit 0. This is not a gate.

Use a shared requirement contract, not a document per test:

```json
{
  "version": 1,
  "requirements": [{
    "id": "checkout-order",
    "reference": "requirements/checkout",
    "revision": "1",
    "expectedOutcomes": [{"id": "order-count", "description": "Checkout creates exactly one order."}]
  }]
}
```

Reference independently reviewed intent from a ticket, contract or supplied
outcomes. Agent-inferred intent remains a proposal until reviewed. The command
records the supplied contract hash; it does not retrieve or authenticate the
referenced requirement. Outcome IDs must be unique across the entire contract,
because outcome annotations have no requirement namespace. Annotate each mapped
test and assertion:

```ts
import {test, expect} from '@playwright/test';

// @9lives-requirement checkout-order
test('checkout creates an order', async ({page}) => {
  await page.getByRole('button', {name: 'Place order'}).click();
  // @9lives-outcome order-count
  expect(await ordersForThisCheckout()).toHaveLength(1);
});
```

Named `test`/`expect` imports from Playwright or the local SDK, including aliases,
and inline callbacks are recognized. Helpers, custom fixtures, branches,
dynamic generation and shadowed bindings can prevent complete analysis. Every
report is explicitly partial. Test locations identify recognized syntax;
this inventory is not authoritative runtime discovery.

| Finding | Classification | Meaning |
| --- | --- | --- |
| Required outcome lacks an assertion annotation | suspected | A declared mapping is missing; helpers may protect it. |
| No direct expect matcher | suspected | No recognized direct assertion; helper behavior is unknown. |
| Async matcher neither directly awaited nor returned | suspected | Inspect how its promise is consumed. |
| `waitForTimeout` or `test.only` syntax | demonstrated | That syntax exists; runtime effect still needs context. |
| `test.skip`/`test.fixme` declaration or enclosing suite | demonstrated | Disabled syntax is present; review before relying on this test. |
| Unknown requirement or unsupported analysis | unsupported | Supply reviewed intent or inspect manually. |

A known requirement mapping supports **purpose** only. Even complete outcome
annotations leave semantic alignment and assertion adequacy unknown. Static
assessment always says execution `not_run` and runtime evidence `unknown`.
Body-level conditional skip/fixme calls remain unsupported; their conditions
are not evaluated. Async matcher detection follows the pinned Playwright 1.61.1
API, including locator, page, API-response and function assertions.
Findings carry locations, requirement/outcome IDs, rationale and suggested
action. Reports bind source, contract, TypeScript and policy versions; changed
inputs invalidate prior assessments. Reports omit source, titles, requirement
prose and raw diagnostic payloads.

## Agent creation and execution provenance

Capture a snapshot immediately after an agent creates or deliberately revises
the selected spec:

```sh
.context/9l provenance path/to/checkout.spec.ts --agent codex > .context/checkout-creation.json
.context/9l assess path/to/checkout.spec.ts --requirements requirements.json --agent-provenance .context/checkout-creation.json --format json
.context/9l run path/to/checkout.spec.ts --agent-provenance .context/checkout-creation.json --timeout 30s --deadline 60s --attempts 1 --format json
```

Use `claude` or another declared agent ID as appropriate. Snapshot version 1
contains declared agent, branch, HEAD, selected source digest, relative-path
digest and workspace-root digest. No Git remote URLs, configuration contents or
credentials are collected. An attached branch and one exact spec are required. Identifiers
and branch names use a bounded ASCII allowlist; unsupported names and detached
HEAD fail capture.

Assessment compares the supplied snapshot with the current checkout; mismatches
remain advisory and unavailable capture remains unknown, with a generic warning
on stderr when capture was explicitly requested. Raw Git errors are not emitted.
It also rejects a source change between reading input and capturing provenance.
Execution checks
before launching the worker and again after completion. A mismatch or unavailable
snapshot prevents success, records an error receipt and is not retried.
Plan/dry-run do not execute these checks. Existing runs without the flag are
unchanged; absent provenance means unknown.

This is boundary snapshot verification, not authenticated authorship or a
continuous worktree lock. It cannot detect a transient branch/source switch
that is reversed, hash the whole dirty application, verify a remote deployment
or prove the named agent wrote the spec. Separate worktrees and a recorded
application build are needed for stronger isolation. Do not replace a mismatched
record just to obtain green; explain a deliberate revision and retain its old
record.

## Checkout qualification

The independent fixture requirements are: checkout creates exactly one order
with selected items and quantities; separately, checkout shows confirmation.
The three fixture tests check a banner against the order requirement, a banner
against the confirmation requirement, and order count/items against checkout.

```sh
npm exec playwright install chromium
npm run smoke:assessment
```

The script copies the fixture into a temporary isolated project and executes it
through the ordinary Go/Playwright adapter with real Chromium, finite budgets
and retries disabled. It checks receipt validation, test completeness, evidence
checksum and exact assertion attribution. No model or account is required.

| Control | Order-requirement banner | Confirmation banner | Count/items test |
| --- | --- | --- | --- |
| Baseline | passes | passes | passes |
| Success shown, order discarded | misses defect | passes confirmation requirement | detects at order-count assertion |
| Setup failure | inconclusive | inconclusive | inconclusive |
| Test timeout | inconclusive | inconclusive | inconclusive |

Only rejection at the fixture's count matcher/source location qualifies as
detected. Setup errors, unrelated assertions and timeouts never qualify. The
selected-item assertion has a passing baseline but no independent defect
control. This experiment supports one synthetic defect hypothesis, not general
correctness, real-provider accuracy or a production benchmark. It does not
upgrade the static report's runtime dimension.

Bounded assessment and qualification summaries are written to
`.context/assessment/`. Temporary raw reports are removed; the summary retains
their digests and attempt IDs, not replayable raw evidence. CI runs this smoke
in its Chromium SDK job.

## Combining the free QA skills

The repository [9lives-qa skill](../skills/9lives-qa/SKILL.md) coordinates available
`test-quality-review`, `flaky-selector-scan`, `qa-triage` and, when release review
is requested, `qa-quality-gate`. They propose findings and repairs while 9lives
owns bounded execution, provenance and receipts. Missing specialist skills are
disclosed; direct review still works. Broader website audits apply only when
requested. These are optional instructions, not runner plugins or an automatic
quality gate. They require no platform account; agent/model usage may cost money.

One canonical skill is linked into `.agents/skills/9lives-qa` and
`.claude/skills/9lives-qa`, the documented project discovery locations for
[Codex](https://learn.chatgpt.com/docs/build-skills) and
[Claude Code](https://code.claude.com/docs/en/skills). Both document support for
symlinked skill folders. Packaging is validated locally; automatic agent
selection has not been qualified in a separate live harness session.

Repairs must preserve reviewed intent and assertions and run in isolation before
application. Future gates need independently demonstrated accuracy for each
required check, and unknown evidence cannot satisfy a required dimension.
