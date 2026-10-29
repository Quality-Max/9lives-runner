# Local SDK and engine bridge

`@9l/playwright` is the chosen package name. This repository builds it as an
npm workspace at version `0.5.5`, the same version as the CLI; releases use npm trusted publishing. The
qualified runtime is Node 24 with `@playwright/test` 1.61.1 through 1.64.1. Node 22
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

Matching CLI and SDK release versions are the qualified pairing; see the
[compatibility table](../README.md#compatibility). Runtime admission checks
the protocol and capabilities below, not npm/CLI package versions or a
same-minor constraint. Different `0.4.x` package versions alone do not reject
a run, but mixed release pairs are unqualified. Align versions first when
investigating a bridge failure. A successful handshake still needs complete,
valid execution evidence before an attempt can pass.

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
| `hello` | Total tests and exact capabilities: steps, artifact metadata, terminal outcomes, declared outcomes |
| `test_begin` | Hashed Playwright test ID, retry index and the expected status declared before any attempt ran |
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

## Results relabeled while the test ran

Playwright's modifiers can change a test's expected status after its body
has run. Qualifying them for 9l found two upstream defects, reported as
[microsoft/playwright#43676](https://github.com/microsoft/playwright/issues/43676)
and [microsoft/playwright#43738](https://github.com/microsoft/playwright/issues/43738):
on 1.61.1 through 1.64.1, `test.skip()` in an `afterEach` hook reports a test
whose body ran and passed as skipped (`afterAll`: only the group's last test),
and `test.fail()` in an after hook or fixture teardown turns a failed test into
an expected failure, so the summary says it passed and the process exits 0.
Playwright's JSON report cannot tell either from a test that declared its
status, so `9l run` without `--sdk` cannot either.

The engine stream can, within the limits of what the reporter observes.
`test_begin` carries the expected status Playwright knew before any attempt
ran: `passed`, or `failed`/`skipped` from a `test.fail('title', ...)` or
`test.skip('title', ...)` declaration or an unconditional suite-level
modifier. It is the same on every retry. `test_end` carries the status the
attempt ended with. Go rejects a declaration that changes between attempts
and derives two counts from the rest:

- `runtimeExpectedFailures`: executed tests that failed and count as expected
  only because `test.fail()` ran during the attempt. The reporter cannot see
  whether that call was in the body (the documented
  `test.fail(browserName === 'webkit')` form), in a suite-level condition
  function, or in an after hook, so all three count. Playwright's verdict is
  kept: the test is executed and not a failure, and the attempt can pass.
- `skippedAfterAssertions`: skipped tests whose final attempt had already
  reported a passed assertion step, so the body ran before a modifier
  relabeled it. The skip still needs a `--pin-skip` and still counts as
  skipped, never as executed; a conditional `test.skip()` placed after an
  assertion counts too.

Both are in the receipt's validation summary and in its JSON, and `9l run`
and `9l result` print them under the job. They are the worker's report, not
verified outcomes: they make a relabeled result visible without claiming the
original one was right.

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
The receipt does count them: `reportedAssertions` is the number of assertion
steps the worker reported as passed in each executed test's last completed
attempt, and `passedWithoutAssertions` the tests that passed while reporting
none; `runtimeExpectedFailures` and `skippedAfterAssertions` are described
above. A retrying assertion (`expect.poll`, `toPass`, a custom matcher)
counts once, including the assertions it runs inside it on each retry, so
the count is of top-level assertion steps, not of every `expect` in the
source. Both are the worker's report, so a green test that asserted nothing is
visible without any claim that what it asserted was verified. The ordinary
JSON reporter serializes only `test.step` steps, so without `--sdk` these
counts are absent. The SDK reporter runs on any spec once `@9l/playwright` is
installed; the spec does not have to import it for the counts.

## Privacy and artifacts

Test IDs and skip keys are SHA-256 hashes. Step labels, test titles, locators, URLs, page content,
worker console output and raw error strings are omitted. Attachments record an
opaque ID, coarse kind and `retained: false`; bodies, names and file paths are not
copied. These records describe attachments produced by Playwright without
claiming that the attachment contents were retained or verified. File attachments
can remain in Playwright's own output directory under the user's configuration.

`9l run --sdk --failure-details` is the opt-in exception for receipts, not for
the protocol: Playwright's JSON reporter runs beside the engine reporter and
writes to a separate private per-attempt file. After the attempt validates
from the engine stream, the runner reads each failed test's title, failing
line, bounded redacted error and attachment paths from that file into the
receipt's `failures`, then deletes it. It never validates or replaces engine
evidence.

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
`malformed-json` fault was not applicable (`not-json` or `unreachable`) or a
`delay` fault had no response to hold (`unreachable`); a request whose upstream
fetch failed is aborted, not sent again. When
the engine sets `NINELIVES_PROVE_CAPABILITIES=1`, a baseline file also lists
the fault kinds this SDK can apply, right after the handshake; an engine that
does not ask never receives the record. These records never enter `9l.engine/1`. Go validates
them against a closed schema and size limits and removes the directory after
each run. See [Prove](prove.md).

Real-browser smoke validates checkout assertions, a defective outcome and owned
worker/browser cleanup on timeout and explicit cancellation. Protocol regressions
cover malformed, duplicate, foreign, late and incomplete evidence. Live provider accuracy,
verified replay and independent behavioral verification have separate milestones.

The opt-in [shared target contract](targets.md) extends this stream with
`9l.target/1`, a target identity digest, a declared test list and typed
deterministic operation outcomes. Ordinary SDK jobs retain the original
handshake. The native implementation is a fake protocol fixture only.
