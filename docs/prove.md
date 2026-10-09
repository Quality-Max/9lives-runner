# Prove: can this test fail?

Status: experimental. `9l prove` needs an `@9l/playwright` build that includes
prove support; the published SDK 0.1.0 does not. Qualified with real Chromium
on one synthetic fixture. No accuracy, latency or cost claim is made for other
applications.

A passing run shows that a test's assertions passed. It does not show that
they would fail if the application broke. `9l prove` checks that directly: it
injects network faults into the browser requests a spec makes and records
whether an assertion fails under each one.

```sh
9l prove tests/checkout.spec.ts
9l prove tests/checkout.spec.ts --paths --format json   # local: include request URLs
```

```
prove tests/checkout.spec.ts: 3 request(s) observed in 1 instrumented test attempt(s)
  CAUGHT                     abort      req-1 GET
  CAUGHT                     http-500   req-1 GET
  CAUGHT                     empty-json req-1 GET
  SURVIVED                   abort      req-2 GET
  SURVIVED                   http-500   req-2 GET
  SURVIVED                   empty-json req-2 GET
  ...
Summary: 9 fault(s): 6 caught, 3 survived, 0 inconclusive, 0 not run
```

A survived fault is the finding: that request can fail, return an error or
return nothing, and the test still passes. A caught fault means an assertion
failed while the fault was injected. It does not show that the assertion checks
the intended behavior, only that it is not indifferent to this request.

## How it works

1. **Baseline.** The spec runs once as an ordinary receipted `9l run --sdk`
   attempt. The SDK's `context` fixture passively records every fetch and XHR
   request (method, origin and path; never query strings, headers or bodies)
   and whether its response was successful JSON. The baseline must pass and be
   complete; a failing test proves nothing about faults.
2. **Plan.** Requests are ordered by method, origin and path. Each gets an
   `abort` fault (network failure) and an `http-500` fault (status 500, empty
   body); a request whose baseline response was successful JSON also gets an
   `empty-json` fault (the same response with `[]` or `{}` as its body).
   `--max-faults` (default 24, at most 256) bounds the runs; the rest are
   reported as `not-run`, never dropped.
3. **Fault runs.** Each fault runs the spec again, alone, in its own receipted
   run with Playwright retries disabled (`--retries=0`). The fault applies to
   every request with the same method, origin and path in that run.
4. **Classification**, from the run's validated engine evidence and the
   fault's application count:

| Result | Meaning |
| --- | --- |
| `caught` | The fault was applied and a failed test contains a failed assertion step |
| `survived` | The fault was applied and every test passed |
| `failed-without-assertion` | The fault was applied and a test failed, but no assertion step failed (for example an action timed out) |
| `not-exercised` | The fault was never applied in its run, so nothing is credited to it |
| `not-applicable` | `empty-json` met a response that was not a JSON object or array |
| `incomplete` | The run was canceled, timed out, errored or its evidence was invalid |
| `not-run` | Beyond `--max-faults`, or the run was interrupted first |

Only `caught` and `survived` are conclusive; the summary counts the rest as
inconclusive.

## Use it

The spec must import `test` from `@9l/playwright` and use the built-in `page`
or `context` fixture. Tests that never use a browser context are untouched.

- **Stable origin.** A fault targets the baseline's exact origin. An
  application started on a random port in each run is never matched, and every
  fault reports `not-exercised`. Start it on a fixed port and pass it with
  `--pass-env`, as the [fixture](../testdata/sdk/tests/prove-shop.spec.ts) does.
- **One spec at a time.** Each fault re-runs the whole spec; keep it focused.
- **Limits.** `--timeout` bounds each run, `--deadline` the whole session,
  `--workers` each run's concurrency. `--pass-env` and `--pin-skip` behave as
  in `9l run`.
- **Exit status.** 0 when the proof finished, whatever it found; 1 when the
  baseline was not green, nothing was instrumented, or the session was
  interrupted; 2 for usage errors. Survived faults do not change the exit
  status, as `9l assess` findings do not.

## Evidence and privacy

Every run, baseline and fault, is an ordinary run with its own receipt under
`--receipt-dir`, inspectable with `9l result <run-id>`. The proof report is
saved to `<receipt-dir>/proofs/<baseline-run-id>.json`. It identifies requests
by ID, method, resource type and a SHA-256 digest of method, origin and path,
and never contains URLs. `--paths` adds `url` (origin and path) to the printed
output only, for local use.

The SDK writes prove records to a private per-test file in a directory Go
creates and removes; Go validates them against a closed schema and bounded
sizes before using them. They never enter the `9l.engine/1` stream, whose
metadata excludes URLs. Outside `9l prove`, the fixture passes the context
through unchanged.

## Boundaries

- Only fetch and XHR requests in contexts from the `@9l/playwright` `context`
  fixture are observed and faulted. Documents, scripts, styles, images,
  WebSockets, `APIRequestContext` and contexts created with
  `browser.newContext()` are not.
- A request the test fulfills with its own `page.route` or `context.route` is
  never faulted, because test routes take precedence.
- Faults are network-level only. Delays, partial bodies, changed fields and
  application-side mutations are not implemented.
- Results describe one execution per fault. A nondeterministic application
  can produce a different result on another run.

The policy name `prove-network-v1` in each report identifies these fault kinds
and rules; it changes whenever either does.
