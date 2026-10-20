---
name: qualitymax-connected-qa
description: Investigate and improve QualityMax-managed tests with a local coding agent and optional 9lives tools, using QualityMax MCP for advanced discovery, generation and cloud verification. Use for QualityMax project, script or run links, including Jev-assisted workflows.
---

Start from the user's QualityMax project, script or failed-run link and existing
session instructions. The coding agent coordinates the tools; this skill does
not install an MCP server or turn native `9l mcp` into a hosted-account proxy.

## Route the work

| Need | Owner | Use |
| --- | --- | --- |
| Understand requirements and prepare a patch | Local coding agent | Relevant repository files, current managed source and bounded failure evidence |
| Static assessment | Local 9lives | `assess_test` on a local copy; no local browser required |
| Inspect a page or reproduce locally | Local 9lives, when useful and authorized | `inspect_page`, `scan_url`, `generate_playwright_repro`; `run_test` and `heal_test` execute locally |
| Discover journeys or generate managed tests | QualityMax MCP | Project memory, advanced crawling and generation |
| Verify a managed script or framework suite | QualityMax MCP | Hosted execution and terminal evidence for the exact source revision |
| Jev framework, grounding, failure or healing decisions | QualityMax service | Existing platform decision paths, project settings and available decision receipts |

For standalone local projects, use the `9lives-qa` skill instead. A QualityMax
account is optional for 9lives. Do not install a browser or SDK merely to
investigate a cloud-only run.

## Establish context

Inspect the connected MCP servers' advertised tools and schemas. Resolve names
against the correct server: local `run_test` differs from hosted `run_tests`.
Use the customer's configured QualityMax connection; do not ask for API keys
in chat. If unavailable, finish the local analysis and report the missing
cloud access or evidence rather than inventing a result.

Resolve project, test case, script and run IDs from the link or scoped lookups.
Read `get_test_capabilities` and, for an existing project, `get_project_memory`
before generating or repairing tests. Fetch only relevant current source and
compact failure evidence. Ask for a project/run reference only when it cannot
be resolved from context.

Record the source of truth (managed script, framework file or repository), its
version/digest, expected behavior, target environment and auth-profile ID.
Establish whether execution stays in the cloud or local reproduction is also
in scope. Preserve existing session permissions and cost/run limits. An MCP
connection or project setting is not permission for new data-changing actions.
For signup, messaging, purchase or deletion journeys, establish the authorized
test fixture and side effects before execution.

## Investigate, discover and repair

Classify from evidence: application defect, test defect, environment/setup
failure or unresolved. Preserve reviewed assertions and requirements. Do not
remove checks, add skips, catch assertion errors or replace expected values
with observed ones to obtain a pass. Missing evidence remains unknown.

Use local source inspection and optional `assess_test` to propose a diff.
Preserve project Playwright configuration. Local page inspection can ground a
locator in the same authorized state; it does not prove the cloud session saw
that state. Keep product regressions visible.

Reuse project memory and existing crawl evidence first. For missing journey
evidence or a coverage gap, use QualityMax's advertised crawl/generation tools
rather than recreate its crawler locally. Bound the target, depth, page count
and flows to the task. For example, `start_ai_crawl` uses `pages_limit`, not
`max_pages`; poll `check_ai_crawl_status` and read `get_ai_crawl_results` after
completion. If a tool returns a virtual test plan requiring approval, present
it and wait for that approval before submitting the full approved plan. A
script repair does not authorize an unrestricted recrawl. Review generated
tests against the original requirement before adopting them.

Keep Jev behind platform decisions. Respect each project's Off, Shadow or
Enabled configuration; do not toggle settings as part of a repair. Shadow
records an opinion without applying it. Enabled still depends on independent
policy and verification checks. Use available decision receipts to explain
proposals, choices and verification at that time. A generation or healing
receipt alone is not proof of a current passing run. Do not invent a direct
Jev MCP tool or request Jev credentials. If connected tools expose no decision
evidence, report it as unavailable and point to the project's Jev Live panel.
See [QualityMax self-healing](https://docs.qualitymax.io/self-healing/).

## Submit and verify the exact revision

For managed scripts, read the current version and use `apply_patch_and_run`
with `expected_version` when supported. This persists the patch and executes
it; it is not a dry run. On a version conflict, reread and review the new source
before proposing another patch. Poll `get_execution` with the returned
execution ID until a terminal outcome or the agreed time limit.

For framework suites, track the exact framework file and snapshot. Updating
a managed script does not synchronize its framework copy. Inspect the supported
framework write path first: `add_script_to_framework` writes a file and can
overwrite an existing one. Use it only for the intended file update, then
verify the resulting framework source. Preserve unrelated files/configuration.
`trigger_framework_run` starts the project's suite; its framework run ID is
not a managed execution ID. Use an available framework status/read path rather
than passing that ID to `get_execution` by assumption. If the server cannot
update, read or verify the relevant framework revision, provide the exact file
patch and capability gap instead of claiming completion.

Treat local runs as supplementary evidence when cloud verification is
requested. Select the compatible managed/cloud runner advertised by the server;
do not silently fall back to a local agent. Check the returned source revision,
target, required test counts and per-test outcomes. Queued, incomplete, skipped,
stale or missing results
cannot establish a passing suite. Stop at the agreed attempt limit; without
one, stop after two unsuccessful repair candidates and report the remaining
diagnosis. Do not retry application defects into a green result.

Return the cause and evidence, changed source/version and diff, cloud run link
or ID, passed/failed/skipped/incomplete counts, and remaining unknowns.
Distinguish local assessment, local execution, Jev advice and cloud verification.
Keep only bounded, sanitized evidence in `.context`; never dump credentials,
full logs, authentication state or private page content. Use secret-free
auth-profile references for hosted execution.

Use the connected server's schema over remembered arguments. Hosted contracts
are documented in the [QualityMax MCP reference](https://docs.qualitymax.io/reference/mcp/).
