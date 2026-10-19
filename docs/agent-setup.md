# Coding agent setup

Install the CLI and SDK using the [installation guide](install.md). The
optional [9lives QA skill](../skills/9lives-qa/SKILL.md) adds a requirements,
provenance, assessment and execution workflow. To give an agent tools
instead, run the [MCP server](mcp.md): `claude mcp add 9lives -- 9l mcp`.

Copy the repository's `skills/9lives-qa/` directory into your project's
`.agents/skills/9lives-qa/` for Codex or `.claude/skills/9lives-qa/` for Claude
Code, following your client's project-skill conventions. The skill includes
its supporting files; copy the directory rather than only `SKILL.md`.
Keep your existing agent instructions and Playwright configuration.
Run `9l doctor` from the application project before connecting the agent;
use `--sdk` when tests import `@9l/playwright`. Diagnostics do not verify
which executable the MCP host launches or whether a provider is logged in.
See [setup diagnostics](setup-diagnostics.md) for the remaining explicit checks.

Ask the agent to map the requested behavior to explicit assertions, record
source provenance before creation or repair, review assessment findings, and
run the exact selected spec. Report failures, skips and unverified behavior
alongside the execution receipt. A green result does not establish complete
requirement coverage.

To review findings reported by a reviewer or bot, ask the agent to follow the
skill's triage workflow: one disposition per finding, a failing and then
passing receipt from a reproduction spec before a finding is called
confirmed, and a closing note on what was and was not checked.

The skill can use additional QA skills when installed; this repository does
not install them automatically. Live goals require a separately configured
provider. The local demo and ordinary assertion execution do not.

## QualityMax-connected projects

For tests managed in QualityMax, use the
[QualityMax connected QA skill](../skills/qualitymax-connected-qa/SKILL.md).
Copy `skills/qualitymax-connected-qa/` into
`.claude/skills/qualitymax-connected-qa/` or
`.agents/skills/qualitymax-connected-qa/` using the same convention as above.
The connected skill is self-contained; the local QA skill is optional.

Connect the customer's QualityMax MCP server separately using the
[QualityMax MCP setup guide](https://docs.qualitymax.io/quickstart-mcp/).
Native `9l mcp` provides local tools; it does not proxy the hosted account.
Cloud-only investigation can begin without local browsers or the 9lives SDK.
For optional local assessment or reproduction, install the CLI and connect
`9l mcp` as described above.

For a one-off session, point Claude directly at the skill instead of installing
it. With QualityMax MCP already connected, give it this instruction:

```text
Read https://raw.githubusercontent.com/Quality-Max/9lives-runner/main/skills/qualitymax-connected-qa/SKILL.md and follow it for this QualityMax run: {run URL}.
Improve the test while preserving its requirements. Keep execution in QualityMax's cloud and report the exact revision and run evidence.
```

The skill routes multi-page discovery and generation to QualityMax, local
source inspection to the coding agent and available 9lives tools, and final
verification to the hosted runner. Jev stays within the project's existing
platform settings and policy checks. Loading the skill does not connect either
MCP server or change those settings.
