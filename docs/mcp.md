# MCP server

`9l mcp` serves the runner to coding agents over the MCP stdio transport:
newline-delimited JSON-RPC 2.0, one message per line. It needs no Python,
account or network beyond what a heal provider uses. Start it in the project
root:

```bash
claude mcp add 9lives -- 9l mcp
claude mcp add 9lives -- 9l mcp --pass-env BASE_URL,TEST_USER
```

Other hosts use the same command and arguments, for example
`{"mcpServers": {"9lives": {"command": "9l", "args": ["mcp"]}}}`. If the
Python `9lives` package is also installed, its `9l` command may come first on
PATH; use the Go binary's absolute path in the host configuration.

## Tools

| Tool | Arguments | Result |
| --- | --- | --- |
| `run_test` | `spec`, optional `run_timeout` (seconds) | `status` `passed`, `failed` or `incomplete`; test counts; `reason`; bounded `failure` context; `failures` (each failed test's title, failing line, error start and attachments); `failureContext` (the first failure's Playwright `error-context.md`, redacted, at most 8 KiB); receipt path |
| `heal_test` | `spec`, optional `apply` (default false), `max_proposals` (1–5, default 1), `run_timeout` | Native healing session: `state`, run results, `savedPath` or `applied`, `diff`, and a `note` for `needs_human` |
| `assess_test` | `path` (spec or directory), optional `requirements` | The [assessment](test-assessment.md) report under `report`; a suite with unassessed files also carries `error` |
| `confirm_finding` | `spec`, `unfixed` (Git revision), optional `fixed` (default the working tree), `finding_id`, `finding`, `run_timeout` (seconds, per run) | The [confirmation](confirm.md) report under `report` and its `reportPath`; only `verdict` `confirmed` means the reproduction failed before and passed after |

Each result is the JSON text content of the tool result, with `version: 1`.
Only `run_test` status `passed` means the spec passed with complete,
validated evidence; `incomplete` is never a pass. Errors in the arguments,
such as a missing or out-of-root path, return `isError: true` with an
`error` message. Run and heal use the ordinary Playwright adapter and the
project's own Playwright configuration, exactly like `9l run` and `9l heal`.

`heal_test` is the [native healing session](native-tier2.md): one offline
Tier 1 attempt, then Tier 2 proposals from the provider `9l mcp` was started
with (`--provider`, `NINELIVES_PROVIDER`, an installed agent CLI or a
configured API key; no provider, or `--provider none`, means Tier 1 only).
The server names that provider in its `initialize` instructions, and each
result records `provider` and `providerCalls`; a failed call carries
`providerDiagnostic`. Every candidate runs in an
isolated copy first. Without `apply`, a verified candidate is saved as
`<spec>.healed`; `apply: true` is the caller's explicit approval to write it
in place, which still happens only if the spec is unchanged since it was
tested. An assertion failure returns `needs_human`: a possible real bug that
healing does not mask. A session that ends in an error, such as a provider
failure or a verified candidate that could not be saved or applied, returns
`isError: true` with its evidence and an `error` message; only `savedPath` or
`applied` means something was written. Heal calls run one at a time; a heal
waiting for its turn can be cancelled.

## Boundaries

- Every path must resolve, after symbolic links, inside the directory the
  server started in, because running or healing a spec executes it. Set
  `NINELIVES_MCP_UNRESTRICTED=1` to lift this.
- Test processes receive only the environment variables named with
  `--pass-env` when the server starts. Tools cannot add names.
- Up to four tool calls run at once; more are refused rather than queued.
  `notifications/cancelled` stops the named call, including a queued heal and
  the analysis helper of an assessment, along with its test processes, and
  that call gets no response. Closing stdin cancels running calls, waits for
  them and exits 0.
- Stdout carries only protocol messages. Returned failure context and reasons
  are limited to 4 KiB each with terminal escapes removed. Receipts and heal
  verification receipts are written under `--receipt-dir` (default
  `.9lives/receipts`) and `--heal-receipt-dir` (default
  `.9lives/healing-receipts`).
- Protocol versions `2025-06-18`, `2025-03-26` and `2024-11-05` are accepted;
  the server offers tools only.
