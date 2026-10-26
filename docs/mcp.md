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

## Verify the server your host launches

First follow the [CLI selection checks](install.md#verify-the-selected-cli)
with the absolute Go binary path. Its `--help` should include `scan` and
`inspect` on releases with the browser tools. Configure that same path in
your host, for example (replace the placeholder):

```json
{
  "mcpServers": {
    "9lives": {"command": "/absolute/path/to/9l", "args": ["mcp"]}
  }
}
```

Restart the host's MCP server after changing the path or replacing the binary.
In the host's MCP inspector, compare the `initialize` response's
`serverInfo.version` with that binary's `--version`; `serverInfo.name` is
`9lives` and `serverInfo.title` is `9lives Go runner`. Then refresh its tool
list (`tools/list`). Go runner releases from 0.4.0 expose these seven tools:
`run_test`, `heal_test`, `assess_test`, `confirm_finding`, `scan_url`,
`inspect_page`, `generate_playwright_repro`.

If only four appear, the host may still be running a Go release from before
0.4.0 or displaying cached discovery. Recheck the configured absolute path,
restart the server and refresh discovery. If the identity differs, check for
the separate Python CLI. A terminal's `9l --version` alone does not establish
which executable an already running GUI host selected. `9l doctor` checks
local prerequisites and warns when the `9l` first on PATH is not the
executable it runs from; it cannot inspect the binary a running host
selected or its tool cache.

## Tools

| Tool | Arguments | Result |
| --- | --- | --- |
| `run_test` | `spec`, optional `run_timeout` (seconds) | `status` `passed`, `failed` or `incomplete`; test counts; `reason`; bounded `failure` context; `failures` (each failed test's title, failing line, error start, `expectedStatus`, last `status` and attachments; a `test.fail()` test that passed has `status` `passed` and the message `Expected to fail, but passed.`); `failureContext` (the first failure's Playwright `error-context.md`, redacted, at most 8 KiB); receipt path |
| `heal_test` | `spec`, optional `apply` (default false), `max_proposals` (1–5, default 1), `run_timeout` | Native healing session: `state`, run results (`original`, and `tier1` and `verified`, each `null` when that stage was not attempted), `savedPath` or `applied`, `diff`, and a `note` for `needs_human` |
| `assess_test` | `path` (spec or directory), optional `requirements` | The [assessment](test-assessment.md) report under `report`; a suite with unassessed files also carries `error` |
| `confirm_finding` | `spec`, `unfixed` (Git revision), optional `fixed` (default the working tree), `finding_id`, `finding`, `run_timeout` (seconds, per run) | The [confirmation](confirm.md) report under `report` and its `reportPath`; only `verdict` `confirmed` means the reproduction failed before and passed after |
| `scan_url` | `url`, optional `checks`, `max_links` (0–250, default 50), `baseline` (a previous result or a path to one), `weight_budget`, `viewport`, `screenshot`, `allow_private_network`, `storage_state_path` with `acknowledge_private_content: true`, `format` (`json`, `markdown`, `issue`), `min_severity`, `timeout` (seconds) | The [scan](cli.md) result: `score` with `scoring` (scale, severity weights, grade thresholds and grade), `findings` with stable `id`s, reproduction steps and fixes, `metrics`, and with a baseline a `delta` of `new`, `fixed` and `unchanged`. `markdown` and `issue` return the rendered text instead of JSON |
| `inspect_page` | `url`, optional `include_accessibility_tree`, `include_forms`, `viewport`, `allow_private_network`, `storage_state_path` with `acknowledge_private_content: true`, `format` (`json`, `markdown`), `timeout` | Title, headings, forms and `interactive` controls with ranked `recommendedLocator`, `stability` and the page testability verdict; optionally Chromium's accessibility tree |
| `generate_playwright_repro` | `url`, optional `goal`, `finding` (a `scan_url` finding), `test_name`, `output_path`, `overwrite`, `sdk` | `filePath` (relative), `code` and `assumptions`. Nothing is run |

Each result is the JSON text content of the tool result, with `version: 1`.
Only `run_test` status `passed` means the spec passed with complete,
validated evidence; `incomplete` is never a pass. Errors in the arguments,
such as a missing or out-of-root path, return `isError: true` with an
`error` message. Run and heal use the ordinary Playwright adapter and the
project's own Playwright configuration, exactly like `9l run` and `9l heal`.

`heal_test` is the [native healing session](native-tier2.md): one offline
Tier 1 attempt, then Tier 2 proposals from the provider `9l mcp` was started
with (`--provider`, `NINELIVES_PROVIDER`, an installed agent CLI or a
configured API key; no provider, or `--provider none`, means Tier 1 only;
`NINELIVES_AUTODETECT_PROVIDER=off` in the server's environment disables the
installed-CLI and API-key detection).
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

`scan_url` and `inspect_page` are observations of one cold page load, not
authorization to change a site and not field data. They run in the same Node
helper as [`9l scan` and `9l inspect`](cli.md), with the project's Playwright
and Chromium, and share their network policy: private, link-local and
loopback destinations are refused, through DNS, redirects and subrequests,
and `allow_private_network: true` permits only a loopback target's origin.
Link checks requested from Node check the address they connect to as well,
so a hostname whose DNS answer changes after the first check (DNS rebinding)
is still refused; the browser's navigation, subrequests and WebSockets are
not yet pinned that way (#108). One more gap: same-origin links the page itself requests are followed
through redirects inside Chromium, where only the first URL is checked, so
such a link that redirects to a private or another loopback address is not
yet refused (#108).
`storage_state_path` loads a Playwright storage-state file inside the working
directory into a throwaway browser context; its credentials are not returned,
but findings and inspected structure may reflect private account data, so
`acknowledge_private_content: true` is required. A `markdown` or `issue`
`format` returns the report as text instead of the versioned JSON.

`generate_playwright_repro` writes a deterministic spec for a finding, URL or
goal and runs nothing. `output_path` is relative to the working directory and
must name a `.spec` or `.test` file; by default a new file is written below
`.9lives/repros`, which a Playwright `testDir` usually does not include, so
pass a path inside it before `run_test`. An existing file is replaced only
with `overwrite: true`, never through a symbolic link. `sdk: true` imports
`test` and `expect` from `@9l/playwright`, which `confirm_finding` requires.
Use `run_test` to execute a generated reproduction; there is no separate tool
that runs inline code.

## Boundaries

- Every path must resolve, after symbolic links, inside the directory the
  server started in, because running or healing a spec executes it. Set
  `NINELIVES_MCP_UNRESTRICTED=1` to lift this for the test tools; files read
  by `scan_url` and `inspect_page` and written by
  `generate_playwright_repro` stay inside that directory regardless.
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
