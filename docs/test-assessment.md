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
mkdir -p .context
go build -o .context/9l ./cmd/9l
.context/9l assess testdata/assessment/tests/checkout.spec.ts --requirements testdata/assessment/requirements.json --format json
.context/9l assess testdata/assessment/tests/checkout.spec.ts --titles
```

`--requirements` is optional. Without it, the code-level checks still run
(fixed waits, exclusive, disabled and conditionally skipped tests, missing and
unawaited assertions, analysis limits). Requirement references are not
checked, `purpose` and `intentAlignment` stay unknown, `requirementsSHA256` is
omitted and the report lists the missing contract as a limit. An empty
`--requirements` value is a usage error rather than a silent skip. The text
report ends with a per-rule summary in which findings at the same location,
such as one suite modifier applying to several tests, count once. `--titles` adds literal test titles (first 200
characters; computed titles are omitted) to the text and JSON report for local
use. Text output quotes them so control characters are not written raw.

### Suites

```sh
.context/9l assess tests/ --format json
.context/9l assess 'tests/**/*.spec.ts' tests/smoke.ts --requirements requirements.json
```

A single regular file gives the single-file report described below. Several
inputs, a directory or a quoted pattern give one combined suite report
(`version` 1) with the shared `policy`, `requirementsSHA256`, `execution` and
`completeness`, one `files` entry per file holding its single-file `report` or
an `error` code, a `summary` (files, assessed, not assessed, tests, findings and
per-rule counts) and the union of limits. A directory contributes files named
like Playwright's default `testMatch` (`*.spec.*`/`*.test.*` with `js`, `ts`,
`jsx`, `tsx`, `mjs`, `cjs`, `mts` or `cts`). A pattern matches path segments,
with `**` matching any number of directories, and takes every matching file. A
named file is taken as given. Walks skip `node_modules`, hidden directories and
symbolic links; repeated files are assessed once and listed in path order.

Each file keeps the single-file budgets. A suite is limited to 2,048 files and
64 MiB of source, which are checked before analysis, and to ten minutes
overall, with four files analyzed at a time. A file that cannot be assessed is
recorded with its code (the diagnostics below, or `source-limit`,
`suite-timeout`), and the other files are still assessed. The suite report is
then still written, with a limit noting the missing files, and the command
exits 2 with the count on stderr. `--agent-provenance` applies to a single spec
and is rejected for suites.

Assessment needs Node 22 or newer. A plain Go build includes the qualified
TypeScript 5.9.3 parser; it does not need npm, consumer TypeScript, a separate
parser installation or a runtime download. The parser is extracted into a
private temporary directory per assessment and removed after the owned helper
stops. Consumer `node_modules`, `NODE_PATH` and `NODE_OPTIONS` do not choose or
preload code. This also works when the consuming TypeScript package has no
JavaScript parser API. It parses source without importing the spec,
configuration or application. Go owns the
helper's ten-second budget, cancellation, output limits and report validation.
Source is limited to 1 MiB, requirements to 256 KiB, recognized tests to 256
and combined assertions/waits/limit reasons plus distinct conditional suite
modifiers to 2,048, with at most 16 conditional modifiers applying to one test. Malformed, unavailable or over-limit
analysis exits 2; advisory findings exit 0. This is not a gate. Failures write
fixed diagnostics on stderr and no partial report on stdout: `node-unavailable`,
`parser-unavailable`, `syntax`, `annotation`, `limit`, `output-limit`, `timeout`,
`cancelled`, `helper-failed` or `invalid-evidence`. Raw helper errors and source
are suppressed. Newer syntax unsupported by the pinned parser remains a syntax
failure; independence from consumer TypeScript is not support for every future
language feature.

When you check requirements, use a shared contract, not a document per test:

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

// @9l-requirement checkout-order
test('checkout creates an order', async ({page}) => {
  await page.getByRole('button', {name: 'Place order'}).click();
  // @9l-outcome order-count
  expect(await ordersForThisCheckout()).toHaveLength(1);
});
```

Named `test`/`expect` imports from Playwright or the local SDK, including aliases,
and inline block callbacks are recognized regardless of whether the title is
a literal, template or expression. A declaration inside a loop or generation
callback is inspected once; its runtime expansion is never guessed or executed.
Helpers, custom fixtures, branches, dynamic callbacks and shadowed bindings can
prevent complete analysis. Every report is explicitly partial. Test locations
identify recognized syntax; this inventory is not authoritative runtime
discovery. Limit findings carry one bounded code and location per reason:
`dynamic-callback`, `declaration-generation`, `nested-function`,
`conditional-flow`, `shadowed-binding`, `runtime-skip`, `unresolved-helper` and
`unknown-matcher`.

Calls to helpers in the same file are resolved. A call to a non-generator
function declaration or `const` function is inlined when that name is bound
exactly once in the file, in a scope enclosing the call and outside the
function being scanned; any other binding of the name, in any scope, leaves
the call unresolved. The helper's direct `expect` facts, waits and limits are
attributed to the test at the call site, in execution order, up to four levels
deep and 64 inlined calls per test. Inlining also stops before it could exhaust
the fact limit left after the file's own matcher and wait calls, or a fixed
work budget; an abandoned call is rolled back
and reported as `unresolved-helper` rather than failing the file. A
parameter given a browser fixture value (`page`, `request`, `context`,
`browser` or a chain on one) is a fixture inside the helper. When an `async`
helper's call is neither awaited nor returned, every assertion in it is
unawaited. Outcome annotations inside a helper apply to every test that calls
it. Recursion, deeper calls, `let`/`var` functions, parameters, custom fixtures,
method calls such as page objects, and imports stay `unresolved-helper`; their
assertions remain unknown and are never assumed.

A `// @9l-assertion-helper` comment, or a JSDoc line consisting of the tag,
directly before a function declaration, a `const` function or an import
declaration marks those bindings as reviewed assertion helpers. Only the last
comment before the declaration counts, and a mention of the tag inside other
comment text marks nothing. Each call counts as one attributable assertion at the call site,
taking `@9l-outcome` annotations from the calling statement; the helper body is
not inferred. A marker on an import applies to every binding it imports, so
split imports to mark only some. A marked `async` function whose call is
neither awaited nor returned is an unawaited assertion; any other unconsumed
marked helper gives `unknown-matcher`, because whether it returns a promise is
unknown. Like outcome annotations, the marker is a
reviewed claim, not proof. Following imports into other files is not yet
supported. Other calls outside the imported test/expect chains and built-in
browser fixture chains can produce `unresolved-helper`; this can include
harmless utility calls. A shadowed expect binding's matcher
calls are excluded conservatively. A call through a test binding redeclared in
an enclosing scope, such as a parameter or loop variable named `test`, is not a
declaration and adds nothing to the inventory. Limit findings retain independently visible
waits, exclusive/disabled syntax and recognized asynchronous matcher findings.
Unsupported analysis never produces `no-direct-assertion`; a supported body
with no recognized assertion still produces a suspected absence finding.

| Finding | Classification | Meaning |
| --- | --- | --- |
| Required outcome lacks an assertion annotation | suspected | A declared mapping is missing; helpers may protect it. |
| No direct expect matcher in a supported body | suspected | No recognized direct assertion; unsupported bodies keep adequacy unknown. |
| Async matcher neither directly awaited nor returned | suspected | Inspect how its promise is consumed. |
| `waitForTimeout` or `test.only` syntax | demonstrated | That syntax exists; runtime effect still needs context. |
| Absence assertion first after a fixed wait (`absence-after-wait`) | suspected | A slow application also passes it; assert a positive completion signal first. |
| `test.skip`/`test.fixme` declaration, unconditional suite modifier or enclosing suite | demonstrated | Disabled syntax is present; review before relying on this test. |
| Conditional or after-hook `test.skip(condition, reason)`/`test.fixme(...)` suite modifier | informational | Whether it skips this test is not evaluated; no concern is raised. |
| Suite modifier whose condition reads `process.env` | suspected | The test may not run in some environments, such as CI; confirm a required run still executes it. |
| Unknown requirement or unsupported analysis | unsupported | Supply reviewed intent or inspect manually. |

A known requirement mapping supports **purpose** only. Even complete outcome
annotations leave semantic alignment and assertion adequacy unknown. Static
assessment always says execution `not_run` and runtime evidence `unknown`.
Suite modifiers (`test.skip()`, `test.skip(condition, reason)` or the fixture
callback form, at file or `describe` level or in a `beforeEach`/`beforeAll`/
`afterEach`/`afterAll` hook) are not test declarations. They apply to every
recognized test in that suite, including tests declared before them.
`test.skip()` and `test.skip(true, ...)` disable those tests; `false` is
ignored; any other condition, a modifier under a branch, or one in an
`afterEach`/`afterAll` hook produces `conditional-skip` at the modifier's
location. After hooks run once test bodies have run: Playwright 1.61.1 reports
a passing test skipped from `afterEach` and only some results skipped from
`afterAll`, so neither disables the suite. Modifiers inside other functions
cannot be attributed from source and are ignored. Playwright treats a string
followed by a function as a declaration at runtime, so only calls that cannot
be one are modifiers: no arguments, one non-title argument, a fixture callback
or syntactically boolean first argument (`true`, `!x`, a comparison), or a
non-title condition followed by a literal reason. Ambiguous calls such as
`test.skip(name, run)` stay disabled declarations; `test.skip(isMobile,
REASON)` is therefore misread as a disabled test. Body-level conditional skip/fixme calls remain
unsupported; their conditions are not evaluated. A modifier whose condition
argument, or an enclosing `if`, conditional or logical expression, reads
`process.env` gives `environment-skip` instead of `conditional-skip`; values
copied out of `process.env` first are not traced.
Every `waitForTimeout` gives `fixed-wait`. When the first recognized assertion
after a wait, in execution order through resolved helpers, checks that something is absent or did not happen, the test also
gets `absence-after-wait` at that assertion, which raises an assertion adequacy
concern: if the application is merely slow, the check still passes. Absence
assertions are `toBeHidden`, `toBeFalsy`, `toBeNull`, `toBeUndefined`,
`toHaveCount(0)`, `toHaveLength(0)`, `toBe`/`toEqual`/`toStrictEqual` with `0`
or `false`, `toBeVisible({visible: false})`, `toBeAttached({attached: false})`
and any other matcher negated with `.not`; negating an absence matcher, such as
`not.toBeHidden()`, makes it a presence check. A recognized positive assertion
between the wait and the absence check counts as a completion signal. An
unresolved helper in between might be one too, so the finding stays suspected.
Async matcher detection follows the pinned Playwright 1.61.1
API, including locator, page, API-response and function assertions, and any
matcher chained through `resolves` or `rejects`. Generic and snapshot matchers
are synchronous. Any other matcher, such as a custom `expect.extend` matcher or
one added in a later Playwright release, that is neither awaited nor returned
gives the `unknown-matcher` limit, because whether it returns a promise is
unknown.
Findings carry locations, requirement/outcome IDs, rationale and suggested
action. Reports bind source, contract, TypeScript and policy versions; changed
inputs invalidate prior assessments. Report version 3, helper version 5 and policy
`assessment-source-v6` replace version 2/source-v3. Every finding now carries
`code`: the limit reason for `analysis-limit` findings and the rule name for
all others, so `rule` is the finding family and `code` the specific reason.
Consumers must also accept the `informational` classification, the
`conditional-skip`, `environment-skip` and `absence-after-wait` rules, the `unknown-matcher` limit
code, an absent `requirementsSHA256` and an optional test `title` before
upgrading. Requirement contract and agent provenance snapshot
versions remain 1. Reports omit source, requirement prose and raw diagnostic
payloads, and omit titles unless `--titles` is given.

## Maintaining the bundled parser

The compressed parser, Apache license and SHA-256 manifest live in
`internal/assessment/parser/` and are embedded into the binary. The Go helper
verifies the compressed asset, decompressed code and license before use. To
reproduce assets from the locked dependency:

```sh
npm ci
node scripts/bundle-assessment-parser.cjs
npm run parser:check
npm run smoke:assessment-portability
```

CI checks decompressed source and license bytes against the locked package,
and verifies the committed compressed stream's checksum. Gzip encodings may
differ between Node/zlib releases without changing the parser. CI exercises an actual Go-built binary
from consumer directories with no TypeScript and a controlled incompatible
package. Changing the pin requires changing the bundler/analyzer/Go version
checks, updating the locked npm dependency and manifest, and running syntax,
portability and browser qualification again. The asset adds approximately
1.6 MiB to the Go artifact; Node remains a runtime prerequisite.

See the [assessment validation and handoff](validation/assessment-and-efficiency-2026-10-08.md)
for prior review fixes, consumer review boundaries and measured runner costs.

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
