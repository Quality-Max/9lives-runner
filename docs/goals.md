# Bounded local goal execution

`n9l.goal(instruction, options?)` observes up to 25 visible enabled controls
from at most 100 semantic DOM matches, plus eight bounded heading/status labels.
Go accepts exactly one typed decision per observation, validates its target and
parameter names, then awaits an execution acknowledgement. The worker rechecks
the owned element handle, visibility and role/label/link fingerprint immediately
before acting; the fingerprint includes the raw stop-policy text, so a match
proves the observed policy verdict still holds. No model-provided JavaScript, selector, URL,
input value or extra field can execute. Candidate IDs are scoped to the decision.
Unsupported controls and cross-origin links are omitted. Password/file inputs,
iframes, custom contenteditable controls and native/mobile work are unsupported.

Controls that share role and label, such as a "Submit" button in two forms,
also carry `context`, the accessible name of their nearest form, fieldset,
dialog or landmark (from `aria-label`, `aria-labelledby`, a fieldset legend, a
heading or the form name; redacted and capped at 160 bytes), and a 1-based
`ordinal` among those duplicates. The context is part of the fingerprint
rechecked before acting. If the chosen control still matches another on role,
label and context, the engine stops the goal as `ambiguous_target` instead of
acting, because any choice between them would be a guess. Scripted goal steps
can name `context` or `ordinal` to pick one of several controls that share a
label.

Actions are `click`, `fill`, `select`, `check`, `wait`, `complete`, `unresolved`.
`check` sets a checkbox true; `select` uses a named local option value. Ordinary
Playwright navigation, authentication setup and assertions surround the goal.
Page text is untrusted provider input. Purchase, deletion and outreach labels
stop by default, including before redaction/truncation. This conservative label
policy does not establish the safety of arbitrary unlabeled application effects;
choose controlled local/staging tasks. Direct ordinary test actions remain the
test author's responsibility.

## Ownership and transport

Go starts a private per-attempt `9l.goal/1` transport: a Unix socket in a
mode-0700 temporary directory, or a Windows named pipe restricted to the
engine user. Windows pipes reject remote clients. Each bounded, strict JSON
request must carry the owning run/job/attempt
identity. Only the socket path and identity reach the worker. API credentials
remain with the existing Go HTTP transport; provider redirects are rejected.
Agent CLI providers run in Go too and receive no API keys.
The runner withholds the provider's credential variables from workers even
when they are named with `--pass-env` (compared case-insensitively).
A decision wrapped in a single Markdown code fence is unwrapped; its JSON is
still decoded strictly, and any other surrounding text is an invalid decision.
Provider round trips run outside the engine lock, so goals in parallel workers
do not wait on each other's calls; budgets are reserved before each call.
The transport is a local same-user boundary, not a sandbox for hostile test code.
`--goal-provider` is explicit: `anthropic` or `openai` use the Go HTTP
transport with an API key, and `claude`, `codex` or `opencode` use that
agent CLI's own login, so a Claude, ChatGPT or OpenCode subscription can drive
goals without an API key. A named provider never falls back to another.
`--goal-script file.json` is an offline qualification provider, not a language
model or verified replay cache. Providers/scripts are exclusive.

### Agent CLI providers

Each decision runs the CLI once, in an empty temporary directory, with the
heal provider environment (its login locations and the user name, no API
keys) and in a decision-only mode:

| Provider | Runs as |
| --- | --- |
| `claude` | `claude -p --output-format json` with no tools (`--tools ""`), no MCP servers, no user settings, no saved session and a replaced system prompt |
| `codex` | `codex exec --json --sandbox read-only --ephemeral --output-schema <decision schema>` |
| `opencode` | `opencode run --agent plan --format json` (read-only agent) |

The answer is decoded and checked exactly like an API answer. A CLI cannot cap
generation the way an API's `max_tokens` does; each call reports its usage
instead (input including cache, output plus reasoning), and a decision whose
reported usage exceeds its reservation stops the goal as `budget_exhausted`.
A CLI answer without reported usage is a provider error, never an unmetered
decision.
The reservation adds each CLI's fixed input overhead, its own system prompt
and tools: 8,192 tokens for Claude Code and 32,768 for Codex and OpenCode
(measured at about 0.8k, 18–19k and 17k). With Codex or OpenCode the default
200,000-token attempt cap therefore allows about five decisions; raise
`--goal-max-tokens` for longer goals. A CLI decision takes about 3–7 seconds,
so give goal tests a Playwright timeout above 30 seconds (`test.setTimeout`)
and a `--goal-timeout-ms` that covers every decision. Token prices for the
cost cap are API-equivalent estimates; a subscription bills its own way.

## Limits and interruption

Defaults per owning attempt: 12 actions, 24 decisions, 200,000 reserved tokens
and 60 seconds (including worker startup); the `--goal-*` flags set them.
Goal options can lower these caps; an option the test omits keeps the CLI cap.
Repeated goals share attempt caps; they cannot reset action/token/cost/time
allowances. The surrounding runner still owns job/run budgets and deadlines.
Each call reserves UTF-8 prompt bytes plus 1,024 envelope/system tokens, an
agent CLI's fixed input overhead, and at most 512 output tokens, with no refund
after errors or absent usage. Each prompt lists the goal's earlier actions
(typed action, target role and label, parameter name and outcome; never
values), so a model does not repeat a fill it cannot see; this history stays
in memory and is not written to receipts. This is a
conservative reservation, not a tokenizer measurement. Explicit conservative
input/output prices allow a micro-USD reservation cap. Reported usage remains
separate, with availability explicit. Actual billing/cost is not certified.

Parent cancellation closes the provider context and owned worker/browser tree.
`GoalOptions.signal` also closes the goal's page, because Playwright's in-flight
actions do not support AbortSignal. After an issued action errors, its effect is
unknown; the engine stops instead of repeating it. A stale target discovered
before issuance can be reobserved. Go worker retries stop after any goal receipt;
Playwright retry attempts cannot invoke goals. An application state check is
required before an intentional rerun after interruption.

## Evidence and qualification

`receipt.json` adds `goals`: goal ID, terminal status/duration, provider name,
conservative reservations and decisions with IDs, typed action/current target
ID, outcome and reported usage availability. Instructions, parameter values,
page labels and raw provider bodies/errors are not retained. Canonical execution
receipts preserve their existing schema. A caught unresolved/blocked/failed goal
still prevents an overall green receipt.

A returned `{status: 'completed', verified: false}` means only action execution
finished. Required Playwright assertions establish the expected behavior;
independent assertion coverage remains unknown. Current deterministic real
Chromium smoke qualifies multi-step execution, delayed controls/style drift,
business defects remaining red, stop policy surviving redaction, aborting a
covered in-flight click, Playwright retry suppression and worker/browser cleanup.
Go tests qualify strict decisions, ownership, shared budgets, cancellation,
usage/error privacy and uncertain-action abstention. Live provider accuracy,
representative drift, measured latency/billing, independent behavioral
verification and replay admission remain follow-up qualification.

The grounding contracts are ported from an existing application's
`services/ai_crawl/discovery_grounding.py`, `step_evidence.py` and
`services/jev_decision_client.py` reviewed at `c209fca62`: finite current targets,
untrusted labels, typed decisions, abstention, fresh revalidation and default
stop policies. Browser handles stay local; no platform account is required.
See [the existing-project guide](run-existing-project.md) for a runnable harness.

The `n9l` fixture uses the selected test's Playwright `page` fixture, so
SDK tests using it require an installed browser even when they only call `step`.
A failed goal invocation has its own categorical SDK evidence, including failures
before a provider starts. Catching that exception cannot make the engine pass.
Observed Playwright test counts remain unchanged; the canonical receipt records
a separate `goal.failed` policy failure when otherwise passing tests caught it.

The SDK reserves the exact step title `9l goal` for goal evidence. Use a
different title for ordinary `n9l.step` or Playwright steps.
