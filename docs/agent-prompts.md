# Prompts for coding agents

Copy-paste prompts for Claude Code, Codex and other MCP hosts that have
`9l mcp` connected (see [MCP server](mcp.md) and [agent setup](agent-setup.md)).
Replace the `{placeholders}`. Each prompt names the tools it expects, asks for
evidence instead of a summary, and closes the usual ways an agent turns a
weak result into a green one.

Every prompt relies on the same three rules, so keep them when you edit one:

- Only `run_test` status `passed` is a pass. `incomplete` and `failed` are
  not, and neither is an agent saying it is done.
- An assertion failure may be a real bug. The agent shows it to you; it does
  not weaken the assertion, add a retry or skip the test to make it pass.
- A pass shows the assertions passed. It does not show they cover the
  requirement, so ask for the mapping and the gaps.

## Write a new test

```text
Use the 9lives MCP tools to write a Playwright test for: {behavior}.
The requirement is {ticket link or acceptance criteria}; treat it as fixed.

1. inspect_page {url} and build locators from its recommendedLocator values.
2. Write {path/to/spec.ts}. Map every acceptance criterion to an explicit
   expect() assertion. No fixed sleeps, conditional skips, test.only,
   test.fail() or try/catch around assertions.
3. assess_test the spec and resolve every concern, or tell me why one is a
   false positive.
4. run_test the spec.

Report the run_test status, the receipt path, each criterion with the
assertion that checks it, and any criterion you could not check. Do not call
the test done unless run_test returned status passed.
```

Add `requirements: "{requirements.json}"` to the `assess_test` step when the
project keeps a [requirements contract](test-assessment.md).

## Review an existing suite without changing it

```text
Use the 9lives assess_test tool on {tests/} and do not edit any file.
Group the findings by file with their locations. For each one, say whether
you checked it in the source and agree, or think it is a false positive and
why. List files that could not be assessed and their error codes. End with
the three findings that most weaken what the suite proves.
```

Assessment is static and advisory: its exit code says nothing about whether a
test is correct or would pass.

## Fix a failing test without hiding a bug

```text
{path/to/spec.ts} is failing. Use the 9lives MCP tools.

1. run_test the spec and show me the failure class, failing line and error.
2. If the failure is an assertion, stop: show me the expected and actual
   values and whether the app or the test looks wrong. Do not change the
   assertion.
3. If it is a broken locator or a timeout on a moved element, run heal_test
   without apply and show me the diff and its verified run.
4. If it is an environment failure (server down, missing variable), tell me
   what is missing instead of editing the test.

Do not add retries, longer timeouts, waits or skips to get a pass.
```

`heal_test` returns `needs_human` for an assertion failure on purpose. Approve
a healed candidate by asking for `heal_test` with `apply: true`, which writes
it only if the spec is unchanged since it was verified.

## Prove the test can fail

```text
{path/to/spec.ts} passes. Show me it would catch a regression.
Plant this defect in a local copy of the code under test: {defect, e.g.
"the cart total ignores the discount"}. run_test the spec against it and show
me that it fails on an assertion, not on a timeout or setup error. Then
revert the defect and run_test again to show it passes. Do not change the
spec between the two runs.
```

For a spec that imports `@9l/playwright`, `9l prove {path/to/spec.ts}` checks
which injected fetch/XHR failures its assertions catch; see [prove](prove.md).

## Confirm a bug fix

```text
Use the 9lives MCP tools to confirm the fix for: {bug description}.

1. Before changing any code, write a reproduction with
   generate_playwright_repro, sdk true and an output_path inside the
   Playwright testDir. Review it so it asserts the reported behavior.
2. run_test it and show me that it fails on an assertion.
3. Apply the fix in the working tree without committing it.
4. confirm_finding with the spec and unfixed set to HEAD.

Only verdict confirmed counts. Report the verdict and reportPath; for any
other verdict, show me what each revision did.
```

[Confirm](confirm.md) copies the spec from the working tree into a separate
checkout of each revision, so the code under test must be started or imported
from that checkout. A confirmed verdict shows the spec tells the revisions
apart; whether it tests the reported bug is still your review.

## Triage review findings

```text
Triage every finding in {PR review or list} before fixing any of them.
Give each exactly one disposition with its evidence: confirmed, refuted,
not demonstrated, intentional contract or residual limitation. Call a finding
confirmed only after a reproduction spec fails without the fix and passes
with it; keep both run_test receipts. Do not apply a suggestion that removes a
guard or weakens an assertion without a reproduced failure. Close with what
you checked and what you did not.
```

This is the triage workflow from the [QA skill](../skills/9lives-qa/SKILL.md),
which adds requirement mapping and provenance records when installed.

## Check a page before writing tests

```text
Run the 9lives scan_url tool on {url} with format markdown, then
inspect_page on it. Tell me which interactive controls lack a stable locator
and which findings are new against {previous scan.json}, if given.
These are observations of one page load: do not change the site.
```

Local servers need `allow_private_network: true`; see [page checks](scan.md).

## QualityMax-connected runs

For tests managed in QualityMax, use the one-off prompt in
[agent setup](agent-setup.md#qualitymax-connected-projects), which points the
agent at the connected QA skill and keeps execution in QualityMax's cloud.
