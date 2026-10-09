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

Ask the agent to map the requested behavior to explicit assertions, record
source provenance before creation or repair, review assessment findings, and
run the exact selected spec. Report failures, skips and unverified behavior
alongside the execution receipt. A green result does not establish complete
requirement coverage.

The skill can use additional QA skills when installed; this repository does
not install them automatically. Live goals require a separately configured
provider. The local demo and ordinary assertion execution do not.
