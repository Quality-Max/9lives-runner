# Independent postcondition checks (experimental)

`9l run <one spec> --verify <checks.json>` lets the engine check the outcome
of a test for itself. After the test process tree has stopped, the `9l`
process sends a small number of HTTP requests, declared in a reviewed file
outside the spec, to the application and compares the answers. A test that
reported itself passed is not passed unless every check holds.

This is the first slice of independent behavioural verification. It is
opt-in and experimental: nothing changes without `--verify`. Broader
verification and verified replay remain planned.

## Why

Without `--verify`, every assertion result in a receipt is reported by code
running inside the test process. A test whose assertion was weakened, such as
one that checks what the page says instead of what the application stored,
still passes. A postcondition check asks the application directly, from
outside that process, in a file the spec does not control.

## Checks file

A closed, versioned JSON document of at most 64 KiB:

```json
{
  "version": 1,
  "checks": [
    {
      "id": "order-stored-confirmed",
      "outcomeId": "order-confirmed",
      "method": "GET",
      "url": "http://127.0.0.1:3000/api/orders/1",
      "expectStatus": 200,
      "jsonPointer": "/status",
      "equals": "confirmed",
      "withinMs": 2000
    }
  ]
}
```

| Field | Meaning |
| --- | --- |
| `id` | Required, distinct. 1 to 80 letters, digits, `_`, `.` or `-`, starting with a letter or digit: the rules of requirement identifiers. |
| `outcomeId` | Optional. The `expectedOutcomes` identifier of a [requirements contract](test-assessment.md) this check belongs to. It is recorded as written; `9l run` does not read a contract or check that the identifier exists. |
| `method` | `GET` or `HEAD`. No other method is accepted, and no request body is sent. |
| `url` | An `http` or `https` URL of at most 2048 bytes, with a host name or IP address as `--wait-for` accepts them. User info and fragments are refused. |
| `expectStatus` | The exact HTTP status the answer must have, 100 to 599. |
| `jsonPointer`, `equals` | Optional, given together, `GET` only. The body is decoded as one JSON document, the [RFC 6901](https://www.rfc-editor.org/rfc/rfc6901) pointer is resolved and the value compared with `equals`: same type, same members, same array order. Integers compare digit for digit; numbers with a fraction or exponent compare as doubles. |
| `withinMs` | Optional, 0 to 30000. Probe again every 250 ms until the check holds or the window closes. Without it there is exactly one probe. |
| `header` | Optional: `{"name": "Authorization", "env": "ORDERS_API_TOKEN"}`. See below. |

The file is rejected, as a usage error (exit 2) before anything runs, if it is
not one JSON object, has an unknown or duplicate member, contains `null`,
nests deeper than eight levels, or holds no check or more than 16. The
messages name a check's position, never its content. `equals` therefore
cannot be `null` or contain one.

### Authenticated endpoints

`header` adds one request header whose value is the content of the named
environment variable, for example `Bearer <token>`. The variable is read by
the `9l` process when the file is loaded and is refused if it is unset,
empty, longer than 8 KiB or not one line. It is also refused if the same
variable is named by `--pass-env` or is one every test process inherits
(such as `HOME` or `PATH`), so the value authenticates the engine's probe
and is not handed to the test. Neither the name nor the value is written to
a receipt, a report or the terminal. Headers that frame or route a request
(`Host`, `Content-Length`, `Connection` and similar) cannot be set. The
checks file never holds a header value. Redirects are not followed, so the
header is sent only to the host the file names; over plain `http` it is sent
unencrypted.

## What the engine does

1. `9l run` reads the file once, validates it and records its SHA-256, before
   the run is stored and before any test process starts. `9l plan` and
   `--dry-run` validate it the same way.
2. The run must plan exactly one job from one exact spec, as
   `--agent-provenance` requires: a job running beside the checked one could
   change the state being observed. More than one job, or a skipped input,
   is a usage error.
3. The test process is started exactly as it is without `--verify`. No
   environment variable, argument or file about the checks is passed to it.
4. When the attempt's process tree has stopped and its evidence has been
   validated, `9l` re-reads the file. If its digest changed, no check runs
   and the attempt cannot pass.
5. Otherwise the `9l` process probes each check in file order. A redirect is
   an answer and is compared, not followed. Each probe has 5 seconds, at most
   1 MiB of a body is read, and all checks share 60 seconds.

Checks run only for an attempt whose own evidence validated. A canceled,
timed-out or invalid attempt records each check as `not_run`.

Network access follows `--wait-for`: the request goes wherever the URL
names, through the same proxy environment variables the Go HTTP client
honours. The private-network policy of `9l scan` and `9l inspect` is
separate and unchanged.

## Results

Each check has one result:

| Result | Meaning | Reason tokens |
| --- | --- | --- |
| `verified` | The application answered and the answer is the declared postcondition. | |
| `contradicted` | The application answered and the answer is not the declared postcondition. | `status`, `json_value`, `json_pointer_missing`, `not_json` |
| `unreachable` | The engine got no answer it could compare. | `connection`, `timeout`, `tls`, `body_too_large` |
| `not_run` | The engine made no observation. | `attempt_not_validated`, `checks_file_changed`, `interrupted`, `time_budget` |

How they decide the attempt:

| Worker's own result | Checks | Receipt `status` | Run `outcome` | Exit |
| --- | --- | --- | --- | --- |
| passed | all `verified` | `passed` | `passed` | 0 |
| passed | any `contradicted` | `failed`, stop `verification_contradicted` | `failed` | 1 |
| passed | none contradicted, any `unreachable` or `not_run` | `error`, stop `verification_incomplete` | `incomplete` | 3 |
| passed | checks file changed during the attempt | `error`, stop `verification_changed` | `incomplete` | 3 |
| failed | anything | `failed`, as without `--verify` | as without `--verify` | as without `--verify` |
| canceled, timed out, invalid evidence | `not_run` | as without `--verify` | `incomplete` | 3 |

A contradicted check is a failure, not an incomplete run, because the engine
itself observed an answer that differs from the declared one. A check that
was not verified never leaves an attempt passed, whatever the test reported.
An attempt the engine probed is not retried by `--attempts`: running the
test again would change the state the checks observed.

## Receipt, text output and JUnit

The receipt gains `verifications`, one closed record per check in file
order, and `verificationSha256`, the digest of the checks file:

```json
{"id": "order-stored-confirmed", "outcomeId": "order-confirmed", "method": "GET",
 "result": "contradicted", "reason": "json_value", "httpStatus": 200, "observedBy": "engine"}
```

`observedBy` is always `engine`: the record comes from the engine's own
probe, never from a worker. `httpStatus` is the status of the last answer
received and is absent when there was none. A record never holds the URL or
its query string, a header name or value, the expected value, or any part of
a response body.

`verifiedAssertions` is the number of verified checks. `assertionCoverage`
stays `unknown`: the checks say that the declared postconditions held, not
how much of the test's behaviour is asserted.

The text output of `9l run` and `9l result` names each check with its
result, reason and observed status. In a `--junit` report each check is a
testcase named `verify <id>`: passing when verified, `<failure
type="verification_contradicted">` when contradicted, and `<error>` when
unreachable or not run.

## What this does not establish

- **Only state reachable by an HTTP `GET` or `HEAD`.** UI-only state,
  transient state such as a toast, and anything that needs another protocol
  (a database, a queue, a mail server) are not covered.
- **No native app state.** A check can probe the backend an Android or iOS
  test talks to, not the device or the app.
- **Whether the checks are right.** The engine compares what the file
  declares. A check that expects the wrong value, or too little, verifies
  the wrong thing. The file needs the same review as a test.
- **State removed by teardown.** Checks run after the whole test process
  tree has stopped, including `afterEach`, `afterAll` and global teardown.
  State they delete is gone before the engine looks.
- **Requirement coverage.** `outcomeId` is a label. Nothing checks that
  every expected outcome has a check, or that a contract exists.
- **Secrecy or integrity of the checks file.** A process running as the same
  user, including the test, can read the file and can reach the same
  endpoints. The digest only detects that the file changed between the two
  reads; it does not stop a test from reading it or from changing and
  restoring it.
- **A moment, not a history.** A check observes the state at the time of the
  probe. Another process changing the application at the same time can make
  a check hold or fail.
- **Not** MCP tools, `9l prove`, `9l confirm`, healing or replay: none of
  them runs these checks.

The portable `execution-receipt-1.0.json` export has no field for these
checks. For an attempt failed only by a contradicted check its `verdict` is
`unknown`, never `passed`.
