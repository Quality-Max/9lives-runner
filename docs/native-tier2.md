# Native Tier 2 healing

`9l heal <spec>` (alias `heal-native`) is the native healing command; it
replaced the Python bridge. Normal `9l run` is unchanged. Without a provider,
healing is offline Tier 1 only: a spec Tier 1 cannot repair ends `unverified`
without any provider call.

The command executes the original spec through the Go Playwright kernel. A
passing, non-zero test result makes no provider call. It tries exactly one
offline Tier 1 proposal before Tier 2; a proposal that selects exactly the
same elements, such as `#save` rewritten as `[id="save"]`, is skipped rather
than verified, because it cannot fix a missing element. A Tier 1 proposal executes only when it
meets the same direct-action selector-only boundary as Tier 2; broader Tier 1
wait/control-flow transforms are refused for native verified healing. Each Tier 2
proposal uses the pinned `REASONING:` / `CHANGES:` / `CODE:` fenced complete-file
envelope (the parser also accepts case-insensitive `js`/`ts`/`py` fence aliases,
the earlier native order, and the existing one-fence CLI response). Native
CHANGES entries are deliberately bounded to one through five nonblank lines.
A literal `CODE:` inside a bare fenced source file remains source text. The candidate
may change only one literal of the failed direct Playwright action: the CSS
selector of `page.locator('…')` or of the shorthand `page.fill('…', …)`,
`page.click('…')` and the other repairable actions (`click`, `fill`, `check`,
`uncheck`, `hover`, `press`, `focus`, `dblclick`, `selectOption`), or the accessible name or text of a single
`page.getByRole('<role>', { name: '…' })`, `getByText`, `getByLabel`,
`getByPlaceholder`, `getByAltText`, `getByTitle` or `getByTestId` call. The
role and any `exact` option must stay the same, and chained (`getByRole('form').getByRole(…)`)
or regular-expression locators are not edited. The failed locator is read from
Playwright's call log (`waiting for getByRole('button', { name: 'Anmelden' })`);
imports, assertions, comments, tests, control flow, and unrelated actions are
byte-for-byte preserved. Every proposal, including the last allowed one, runs
in a fresh copy and receipt directory. A failed, malformed, timed-out, or
zero-test candidate is never saved or applied. Before each Tier 2 call the
session checks that the spec holds exactly one direct
`await page.<failed locator>.<action>(...)` statement outside an assertion; otherwise no proposal could be admitted, so it ends `unverified`
with a `Tier 2 was not asked: …` reason and makes no provider call.

The command prints only a JSON session result on stdout; the terminal diff and prompt use stderr. With `--yes`, it applies only after
the original file SHA-256 still matches the bytes that were tested. On a
terminal, it first prints a candidate diff and accepts only `y` or `yes`.
Declining, or running without a terminal, saves the verified candidate as
`<spec>.healed`. Apply and save use same-directory atomic renames and preserve
the original apply mode. Owned copies and provider workspaces are removed on
all return paths. On session startup, a conservative sweep removes only an
owned copy with valid same-spec ownership metadata whose recorded process no
longer exists; malformed, foreign, live, and potentially reused-PID files are
preserved. Cancellation during terminal approval selects cancellation independently
of a terminal read, so the command exits without blocking source mutation; any
blocked command-owned reader is reclaimed with the terminating CLI process. The MCP `heal_test` tool runs the same session with no
terminal approval: it applies only when the caller passes `apply: true`, and
otherwise saves the verified candidate as `<spec>.healed` (see
[MCP server](mcp.md)).

Providers resolve in explicit option, `NINELIVES_PROVIDER`, installed CLI,
then configured-key order. `--provider none` (or `NINELIVES_PROVIDER=none`)
heals with offline Tier 1 only even when an agent CLI or API key is present.
Auto-detection is on by default; the setting `NINELIVES_AUTODETECT_PROVIDER=off`
turns it off, so healing never picks an installed CLI or a configured API key
on its own and a named CLI does not fall back to an API: only a provider
named with `--provider` or `NINELIVES_PROVIDER` is used, otherwise healing is
offline. Set it in the shell profile, CI environment or MCP server
configuration (`claude mcp add 9lives -e NINELIVES_AUTODETECT_PROVIDER=off -- 9l mcp`).
Values other than on/off (`1`/`0`, `true`/`false`, `yes`/`no`) are a usage
error.
Before healing starts, the command prints the resolved provider and how it is
reached to stderr, for example
`9l: healing provider: claude (CLI, its own login); a proposal sends the spec, the failure and a redacted page snapshot; --provider none heals offline`.
The session JSON records `provider` and `providerCalls`, the number of
proposals requested; the MCP `heal_test` result carries the same fields, and
the MCP server names its provider in its `initialize` instructions. `--pass-env` forwards explicitly named values only to test processes, never to a provider. `claude-code` remains an alias for `claude`.
CLI prompts use stdin for Claude/Codex and OpenCode receives its documented
argument with closed stdin. A CLI runs in an empty temporary directory with
only what it needs to find its own login and reach its service: `PATH`,
`HOME`, the XDG directories, the user name (`USER`, `LOGNAME`, `USERNAME`),
temp and locale variables, the Windows profile and system variables, and the
proxy and CA-certificate variables. API keys and other credentials are not
inherited. Provider processes use the runner's owned process
group so cancellation and a leader exit clean descendants. If a CLI fails
without cancellation and a configured API transport is available, it falls
back once. HTTP uses bounded requests/responses, no redirects, sanitized
status errors, Anthropic text blocks or OpenAI chat content, and the pinned
API defaults `claude-haiku-4-5-20251001` / `gpt-4o-mini` unless `--model` or
`NINELIVES_MODEL` selects one. Provider responses and credentials are never
printed or stored in receipts. When a call fails, the session's
`providerDiagnostic` and `reason` carry the CLI's exit code and the last lines
it printed, redacted and capped at 512 bytes, so a cause such as
`claude provider failed (exit 1): Not logged in · Please run /login` is
visible.
A refused answer is explained too: the `reason` names the parse error, with
the response's first 160 characters in `providerDiagnostic`, or the lines the
candidate changed beyond the failed locator's literal.

Prompt contracts label Playwright and Cypress files as JavaScript and Selenium
files as Python + pytest. The current native execution kernel only executes
installed Playwright projects; Cypress and Selenium execution adapters remain
explicitly deferred to QUA-2153/QUA-2155. `NINELIVES_RUN_TIMEOUT` supplies the
per-run default when `--run-timeout` is absent (positive whole seconds;
otherwise five minutes). The page state in the prompt is the failed run's ARIA
snapshot from Playwright's `error-context.md`: roles, accessible names and the
page's visible text, no HTML. Values typed into text fields, comboboxes,
spin buttons and sliders are dropped, the rest is redacted like evidence and
bounded to 3,000 bytes; it goes to the provider with the source and is never
stored in the session. Error diagnostics are bounded to 4 KiB, and
console diagnostics to ten 1 KiB entries with a 4 KiB aggregate when a future
adapter provides them. The complete source remains intact within the one
MiB native input limit for original execution and Tier 1. Before Tier 2 calls a
provider, native healing admits complete files through 8 KiB only; larger files
return an unverified result without a provider call or mutation. Provider
prompts are capped at 32 KiB, including instructions and bounded diagnostics;
this keeps OpenCode's documented argument transport below OS argv limits. HTTP
requests use `max_tokens: 16384`, enough for the admitted complete-file
response. Response fences are structural whole lines with matching delimiters;
the prompt chooses an outer delimiter longer than any backtick run in source.

## Healing on a Claude, ChatGPT or OpenCode subscription

The agent CLIs use their own login, so a subscription pays for the call and
no API key is needed:

| Provider | Login | Check without a model call | Runs as |
| --- | --- | --- | --- |
| `--provider claude` | Claude Pro, Max or Team via `claude` login | `claude auth status` | `claude -p --output-format text` |
| `--provider codex` | ChatGPT plan via `codex login` | `codex login status` | `codex exec --sandbox read-only` |
| `--provider opencode` | whatever `opencode auth login` set up, such as GitHub Copilot or a ChatGPT plan; with none, OpenCode's free hosted model | `opencode auth list` | `opencode run --agent plan` |

9l passes `USER`, so Claude Code can find a keychain login on macOS, and
`CODEX_HOME`, `CLAUDE_CONFIG_DIR`, `OPENCODE_CONFIG` and the XDG directories,
so a relocated login is found. API keys in the environment are not passed: an
OpenCode setup that relies on `OPENAI_API_KEY` or similar needs
`opencode auth login` instead. The spec source and the bounded failure text
go to the CLI's service under that account, with the redacted ARIA snapshot
of the failed page. Codex runs in its read-only
sandbox and OpenCode with its read-only `plan` agent, both in an empty
temporary directory. SDK goals (`9l run --sdk`) are different: they accept
only an Anthropic or OpenAI API key, because goal decisions need the bounded
output-token contract an agent CLI does not offer.
