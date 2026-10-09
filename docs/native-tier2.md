# Native Tier 2 healing

`9l heal <spec>` (alias `heal-native`) is the native healing command; it
replaced the Python bridge. Normal `9l run` is unchanged. Without a provider,
healing is offline Tier 1 only: a spec Tier 1 cannot repair ends `unverified`
without any provider call.

The command executes the original spec through the Go Playwright kernel. A
passing, non-zero test result makes no provider call. It tries exactly one
offline Tier 1 proposal before Tier 2. A Tier 1 proposal executes only when it
meets the same direct-action selector-only boundary as Tier 2; broader Tier 1
wait/control-flow transforms are refused for native verified healing. Each Tier 2
proposal uses the pinned `REASONING:` / `CHANGES:` / `CODE:` fenced complete-file
envelope (the parser also accepts case-insensitive `js`/`ts`/`py` fence aliases,
the earlier native order, and the existing one-fence CLI response). Native
CHANGES entries are deliberately bounded to one through five nonblank lines.
A literal `CODE:` inside a bare fenced source file remains source text. The candidate
may change only the literal selector of the failed direct Playwright action;
imports, assertions, comments, tests, control flow, and unrelated actions are
byte-for-byte preserved. Every proposal, including the last allowed one, runs
in a fresh copy and receipt directory. A failed, malformed, timed-out, or
zero-test candidate is never saved or applied.

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
then configured-key order. `--pass-env` forwards explicitly named values only to test processes, never to a provider. `claude-code` remains an alias for `claude`.
CLI prompts use stdin for Claude/Codex and OpenCode receives its documented
argument with closed stdin. Provider processes use the runner's owned process
group so cancellation and a leader exit clean descendants. If a CLI fails
without cancellation and a configured API transport is available, it falls
back once. HTTP uses bounded requests/responses, no redirects, sanitized
status errors, Anthropic text blocks or OpenAI chat content, and the pinned
API defaults `claude-haiku-4-5-20251001` / `gpt-4o-mini` unless `--model` or
`NINELIVES_MODEL` selects one. Provider responses, stderr, and credentials are
never printed or stored in receipts.


Prompt contracts label Playwright and Cypress files as JavaScript and Selenium
files as Python + pytest. The current native execution kernel only executes
installed Playwright projects; Cypress and Selenium execution adapters remain
explicitly deferred to QUA-2153/QUA-2155. `NINELIVES_RUN_TIMEOUT` supplies the
per-run default when `--run-timeout` is absent (positive whole seconds;
otherwise five minutes). Optional page HTML is bounded to 3,000 bytes, error diagnostics to 4 KiB,
and console diagnostics to ten 1 KiB entries with a 4 KiB aggregate when a
future adapter provides them. The complete source remains intact within the one
MiB native input limit for original execution and Tier 1. Before Tier 2 calls a
provider, native healing admits complete files through 8 KiB only; larger files
return an unverified result without a provider call or mutation. Provider
prompts are capped at 32 KiB, including instructions and bounded diagnostics;
this keeps OpenCode's documented argument transport below OS argv limits. HTTP
requests use `max_tokens: 16384`, enough for the admitted complete-file
response. Response fences are structural whole lines with matching delimiters;
the prompt chooses an outer delimiter longer than any backtick run in source.
