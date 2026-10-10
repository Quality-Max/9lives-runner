# Local SDK and engine bridge

`@9l/playwright` is the chosen package name. This repository builds it as an
npm workspace at version `0.1.3`; releases use npm trusted publishing. The
qualified runtime is Node 24 with `@playwright/test` 1.61.1 through 1.64.0. Node 22
is the declared minimum. The peer range `>=1.61.1 <2` lets later Playwright 1.x
releases install; they and other browser engines remain unqualified.

Use `9l plan ... --sdk` to inspect the command and `9l run ... --sdk` to execute.
The nearest enclosing Playwright config is selected explicitly and its directory
is the working directory (falling back to the package declaring Playwright), so
configuration, project selection, hooks, imports, isolation and retry settings
remain owned by Playwright. Dependencies may be workspace-hoisted. The explicit
SDK mode replaces configured reporters with its engine reporter; arbitrary
reporter output cannot be mixed into the protocol. Without `--sdk`, ordinary
Playwright JSON reporting remains the default; that report also goes to a
private per-attempt file (`PLAYWRIGHT_JSON_OUTPUT_FILE`), so output on stdout
cannot corrupt it. The fixture requires a supported engine identity when
`n9l` is used.

## Contract

Go supplies `NINELIVES_ENGINE_PROTOCOL=9l.engine/1` and run/job/attempt IDs
after planning, overriding caller-provided identity. The worker acknowledges
the protocol and capabilities in a `hello` frame. Go controls the worker through
an owned Unix process group or Windows Job Object and cancellation/deadline mechanisms. Browser
handles never cross the bridge. This first version is a startup-control and
worker-evidence protocol. Interactive goals use the separate attempt-owned
`9l.goal/1` local IPC protocol (Unix socket or Windows named pipe); see [goals.md](goals.md).

Evidence is newline-delimited JSON in a private per-attempt file. Go creates a
temporary directory for each attempt (mode 0700 on Unix; inherited directory ACLs on Windows) and names the file in
`NINELIVES_ENGINE_EVENTS`; the reporter creates it exclusively and writes nothing
to stdout. Configuration, `globalSetup`, hooks and dependencies (dotenv logs its
startup line, for example) can print to stdout freely without affecting the
evidence, and stdout alone never validates. Each frame is exactly one JSON object
per LF-terminated line, with no CR or other surrounding whitespace. Every frame contains `version`,
`runId`, `jobId`, `attemptId`, a contiguous `seq` starting at 1 and `type`.

| Frame | Evidence |
| --- | --- |
| `hello` | Total tests and exact capabilities: steps, artifact metadata, terminal outcomes |
| `test_begin` | Hashed Playwright test ID and retry index |
| `step_end` | Owning test/retry, unique step ID, category and passed/failed outcome |
| `test_end` | Actual/expected attempt status and bounded attachment metadata |
| `test_result` | Final expected/unexpected/flaky/skipped outcome per test; skipped results add a `skipKey` digest |
| `end` | Passed/failed terminal suite outcome after all final results |
| `engine_error` | Fatal worker error with no raw error payload |

Limits are 16 KiB per frame (including newline), 10,000 frames, 4 MiB per
protocol stream and 32 attachment records per test attempt. The runner's smaller
capture limit takes precedence. Any truncation prevents validation. Unknown
versions/capabilities, malformed or duplicate fields, sequence gaps, foreign
identity, missing tests/results, late events and absent terminal evidence fail
closed. Validation state is local to an attempt and cannot update another receipt.

Retry attempts count as one test. Expected failures retain Playwright semantics;
skipped tests do not establish completeness. Suite exit code and reported
outcomes must agree.

## Skip pins

An unexplained skip fails the attempt. To keep a deliberate skip
(`test.skip(browserName === 'webkit')`, `test.fixme`) without failing the
run, declare it before running:

```sh
9l run tests --sdk --pin-skip "checkout.spec.ts › guest checkout › applies a gift card"
```

A pin is Playwright's title path below the project: the spec file relative to
the config's `testDir` (with `/` separators), then each `describe` title, then the
test title, joined by ` › `. It is the `npx playwright test --list` line without
the `[project]` prefix and the `:line:column` suffix. One pin covers that test in
every project. `--pin-skip` is repeatable (up to 256) and requires `--sdk`.

The reporter sends only `skipKey`, the SHA-256 of that string, for each skipped
test. Go accepts the skip only when the key matches a declared pin, so titles
never cross the bridge. A pinned skip is still counted as skipped and never as
executed. An attempt where every test was skipped, even if all were pinned,
still fails, because skips don't prove anything ran. A pin for a test that runs
normally has no effect. A step marked as an assertion records execution metadata,
not independently verified assertion coverage; receipts retain `unknown` coverage.

## Privacy and artifacts

Test IDs and skip keys are SHA-256 hashes. Step labels, test titles, locators, URLs, page content,
worker console output and raw error strings are omitted. Attachments record an
opaque ID, coarse kind and `retained: false`; bodies, names and file paths are not
copied. These records describe attachments produced by Playwright without
claiming that the attachment contents were retained or verified. File attachments
can remain in Playwright's own output directory under the user's configuration.

The runner persists validated bounded protocol output beside its local and
canonical execution receipts and applies its existing output redaction. The
worker's own stdout is not evidence and is not retained. Invalid/interrupted
streams and arbitrary stderr are omitted before persistence; the receipt keeps
the safe validation/termination reason. Core attempt progress remains in run events;
step progress is in the attempt evidence stream after persistence.

## Prove channel

The SDK overrides Playwright's `context` fixture. Outside `9l prove`, where
`NINELIVES_PROVE` is unset, it passes the context through unchanged. Under it,
the fixture requires `9l.prove/1`, the attempt identity and an absolute
`NINELIVES_PROVE_DIR`, and writes one exclusive `0600` file per worker process
and test attempt, whose handshake names the attempt by the engine's hashed
test ID and retry index. A baseline records fetch/XHR method, origin and path
(no query, headers or bodies) and response status and JSON media type; a fault
run records only how often its one fault, from the closed
`NINELIVES_PROVE_FAULT` schema, was applied, and why an `empty-json` or
`malformed-json` fault was not applicable (`not-json` or `unreachable`). When
the engine sets `NINELIVES_PROVE_CAPABILITIES=1`, a baseline file also lists
the fault kinds this SDK can apply, right after the handshake; an engine that
does not ask never receives the record. These records never enter `9l.engine/1`. Go validates
them against a closed schema and size limits and removes the directory after
each run. See [Prove](prove.md).

Real-browser smoke validates checkout assertions, a defective outcome and owned
worker/browser cleanup on timeout and explicit cancellation. Protocol regressions
cover malformed, duplicate, foreign, late and incomplete evidence. Live provider accuracy,
verified replay and independent behavioral verification have separate milestones.
