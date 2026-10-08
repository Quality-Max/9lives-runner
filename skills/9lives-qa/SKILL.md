---
name: 9lives-qa
description: Assess, create or repair Playwright tests with 9lives, combining requirement mapping, local execution receipts, branch provenance and available QA skills. Use for test intent or quality work in a 9lives project, not unrelated website audits.
---

Protect the user's reviewed requirement and distinguish a passing run from an
adequate test. Work from the project root. Read `docs/test-assessment.md` in this
repository for command limits and the contract; in another project use the
installed 9lives runner's corresponding documentation.

Use the existing free QA skills when installed, reading only those relevant:

| Task | Skill | Evidence to bring back to 9lives |
| --- | --- | --- |
| Intent and assertion review | `test-quality-review` | Requirement, location, suspected gap and a stronger assertion proposal |
| Brittle UI locators | `flaky-selector-scan` | Locator location and observed role or test ID for a proposed replacement |
| Failed execution | `qa-triage` | Receipt, failing assertion and evidence for defect, flake or environment classification |
| Requested release review | `qa-quality-gate` | Required tests, terminal outcomes and unresolved concerns |

These skills are optional agent instructions, not executable runner plugins.
If unavailable, review the same evidence directly and disclose the missing
specialist check. Website, accessibility, performance and security skills apply
only when the requested scope includes those concerns; do not run every scan.
Skill advice does not establish assertion coverage or satisfy a required gate.

Keep requirements independently sourced: use a ticket, contract or supplied
expected outcomes, with a reference and revision. Agent-inferred intent remains
a proposal until reviewed. Reuse one shared requirements contract and map
`@9l-requirement` on tests and `@9l-outcome` on assertions. An annotation
is a coverage claim to inspect, not semantic proof. Do not change a requirement
or weaken an assertion to make an execution pass.

Use the existing `9l` binary, or in this repository build locally:

```sh
mkdir -p .context
go build -o .context/9l ./cmd/9l
.context/9l assess path/to/checkout.spec.ts --requirements requirements.json --format json
```

Without `--requirements`, assessment still reports waits, skips, exclusive tests
and missing or unawaited assertions, but leaves requirement mapping unchecked.
Same-file helper functions are resolved. Mark a reviewed imported or local
assertion helper with `// @9l-assertion-helper` on its declaration or import so
calls count as assertions; do not mark helpers that only act.
To assess a whole suite in one call, pass a directory or a quoted pattern, for
example `.context/9l assess tests/ --format json`; files that cannot be
assessed are listed with an error code and make the command exit 2.
Assessment needs Node 22 or newer; the runner owns its pinned parser. Do not
install or downgrade consumer TypeScript, change application dependencies or
change cwd to select an assessment parser. Respect specific analysis-limit
codes; helper or unsupported syntax does not establish missing assertions.

Report per-dimension findings with locations, source/contract hashes and clear
analysis limits. Missing mappings are suspected gaps; helper assertions and
dynamic generation can remain unknown. Static assessment exits zero with
advisory concerns. Never describe that exit code as test correctness or runtime
success.

After creating or deliberately revising a test, capture its creation snapshot
immediately and retain it in `.context`. Choose an accurate declared agent ID:

```sh
.context/9l provenance path/to/checkout.spec.ts --agent codex > .context/checkout-creation.json
.context/9l assess path/to/checkout.spec.ts --requirements requirements.json --agent-provenance .context/checkout-creation.json --format json
.context/9l run path/to/checkout.spec.ts --agent-provenance .context/checkout-creation.json --timeout 30s --deadline 60s --attempts 1 --format json
```

Use `--agent claude` for Claude-created tests. The ID is declared, not
authenticated. Keep the original record when investigating a mismatch; never
refresh it merely to bypass the check. An intended new revision needs an
explicit new record and explanation. The runner checks the workspace, branch,
HEAD, selected path and source before and after execution. It cannot prove
authorship, observe a transient switch that is reversed, or fingerprint the
entire dirty application. Without a creation record, provenance is unknown.

For a demonstrated requirement gap, define one defect hypothesis and run a
bounded control in an isolated local fixture. Establish a passing baseline,
then attribute rejection to the relevant assertion. Setup failures, unrelated
errors, retries and timeouts are inconclusive. This repository provides
`npm run smoke:assessment` for the missing-order checkout experiment; its
result qualifies that fixture and defect only. Never mutate production data.

Propose repairs from the reviewed requirement, run the candidate in isolation
and preserve its assertions before applying it. Summarize execution outcomes,
assessment findings, provenance and remaining unknowns separately. Keep
bounded evidence in `.context`; never dump environment/configuration, provider
logs, credentials, page content or raw error payloads into reports. No platform
account is needed for assessment or the fixture. Model usage through an agent
or live provider can still incur costs; free skill instructions do not make
model calls free.
