# Machine-readable CLI contract

Hosts such as qmax-code run `9l` as a subprocess, without a shell, and decode
its JSON output. This page is the contract they can rely on. Text output is for
people and may change in any release.

## Find the Go runner

The Python `9lives` package also installs a `9l` command. Confirm the Go
runner and its contract versions before decoding anything else:

```sh
9l version --format json
```

```json
{
  "version": 1,
  "name": "9l",
  "implementation": "go-runner",
  "cliVersion": "0.1.1",
  "contracts": {
    "plan": 1, "runResult": 1, "runStatus": 1, "receipt": 1,
    "progressEvent": 1, "assess": 3, "assessSuite": 1,
    "executionReceipt": "execution-receipt/1.0", "engine": "9l.engine/1"
  },
  "commands": ["plan", "run", "status", "result", "cancel", "assess", "provenance", "tier1", "heal-native", "heal", "version"]
}
```

`implementation` must be `go-runner`. Treat a missing or failing
`version --format json` as an older or different `9l`. `9l --version` keeps
its one-line text output.

## Exit codes of `9l run`

| Code | Outcome | Meaning |
| --- | --- | --- |
| 0 | `passed` | Every planned job has a validated passing receipt |
| 1 | `failed` | Every planned job ran to a validated receipt and at least one reported a test failure |
| 2 | — | Usage or setup error before execution: invalid flags, no specs, an unavailable provider, an invalid provenance record |
| 3 | `incomplete` | The run proves neither: skipped inputs, cancellation, run or job timeouts, infrastructure errors, missing or invalid evidence, provenance drift, or a goal that did not complete |

A goal that is policy-blocked, out of budget, interrupted or without a
provider makes the run incomplete, not failed, even if a later assertion failed
because the goal did not act. Playwright test timeouts and hook errors are
reported by Playwright as test failures, and 9l counts them as failures.

`9l plan` and `9l run --dry-run` exit 0 or 2. `9l status` and `9l result`
exit 0 whenever they could read the run, whatever its outcome, and 2
otherwise; read `outcome` from their JSON. `9l assess` exits 0 with advisory
findings and 2 when any input could not be assessed.

## Run result

`9l run --format json` writes one run result to stdout; `9l result <run-id>
--format json` reads the same object back. The cancel hint goes to stderr.

- `outcome` (`passed`, `failed`, `incomplete`) is the normative
  classification. The exit code is derived from it.
- `complete` is kept for compatibility. It is true only when `outcome` is
  `passed`, so it cannot separate a failure from an incomplete run.
- `plannedJobs` and `skippedInputs` come from the plan. A run with skipped
  inputs or fewer receipts than planned jobs is incomplete.
- Results written by CLI 0.1.1 or earlier have no `version`; `9l result`
  fills in version 1 fields from the persisted plan and receipts.

## Schemas and versioning

JSON Schemas (draft 2020-12) are generated from the Go output types and
checked by `go test ./cmd/9l`, so they cannot drift from what the CLI writes:

| Output | Schema |
| --- | --- |
| `9l version --format json` | [version.schema.json](contracts/version.schema.json) |
| `9l plan --format json` | [plan.schema.json](contracts/plan.schema.json) |
| `9l run` / `9l result --format json` | [run-result.schema.json](contracts/run-result.schema.json) |
| `9l status --format json` | [run-status.schema.json](contracts/run-status.schema.json) |
| `receipt.json` | [receipt.schema.json](contracts/receipt.schema.json) |
| `events.jsonl` line | [progress-event.schema.json](contracts/progress-event.schema.json) |
| `9l assess` on one spec | [assess.schema.json](contracts/assess.schema.json) |
| `9l assess` on several specs, a directory or a pattern | [assess-suite.schema.json](contracts/assess-suite.schema.json) |

Every top-level object carries an integer `version`, pinned with `const` in
its schema and listed under `contracts` in `9l version --format json`.

- Required fields are always present. Optional fields may be absent.
  Arrays, maps and objects without `omitempty` in Go may be `null`; the
  schemas say so.
- Adding a field, or a value to an open string field, keeps the version.
  Removing or renaming a field, changing its type, adding a value to a closed
  `enum`, or changing a field's meaning increments the version.
- Consumers should ignore unknown fields and reject versions they do not know.

The portable `execution-receipt-1.0.json` beside each receipt follows the
platform's `execution-receipt/1.0` schema, and `9l.engine/1` is the SDK bridge
protocol in [SDK bridge](sdk-bridge.md); neither is redefined here.

After an intended change to an output type, regenerate the schemas:

```sh
NINELIVES_UPDATE_CONTRACTS=1 go test ./cmd/9l -run TestPublishedContractSchemasMatchOutputTypes
```

Not yet in the contract: selecting changed specs, streaming progress events to
stdout, recording goal-provider egress in receipts, and Windows releases
(issue #27, items 5–8).
