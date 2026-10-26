# Quickstart: run one existing Playwright spec

Start in your application's repository with a spec you already trust. Keep its
imports, assertions, dependencies and Playwright configuration. The ordinary
runner needs no SDK, model key or QualityMax account.

## 1. Check prerequisites

- Use Node 24 (Node 22 minimum) and your project's installed Playwright.
  The qualified range is Playwright 1.61.1–1.64.0 with Chromium.
- Install your application's dependencies using its existing lockfile and setup
  instructions. Start its local or approved staging server, or let its existing
  Playwright `webServer` configuration start it.
- Install the browser in that project: `npm exec playwright install chromium`.
  On Linux, use `npm exec playwright install --with-deps chromium` if needed.
- Select a spec with explicit assertions and controlled test data. Check that it
  already works with your project's normal Playwright command.

## 2. Install the native CLI

On macOS or Linux:

```sh
curl -fsSL https://9lives.run/install.sh | sh
export PATH="$HOME/.local/share/9lives-runner/bin:$PATH"
9l --version
```

Use the [Windows ZIP instructions](install.md#windows) on Windows. See
[installation](install.md) for source builds and release verification. If you
also have the Python `9lives` package, invoke the native executable by its full
path: the two packages share the `9l` command name.

## 3. Plan, then run one spec

From your application's project root, replace `tests/checkout.spec.ts` with an
existing spec path. These commands use ordinary Playwright, without `--sdk`:

```sh
9l plan tests/checkout.spec.ts --format json
9l run tests/checkout.spec.ts --timeout 2m --attempts 1 --format json
```

The plan lists the selected jobs and budgets; it does not execute assertions.
The run uses your installed Playwright and detected configuration. For a custom
config or a named project, pass the same selection explicitly:

```sh
9l run tests/checkout.spec.ts --config playwright.config.ts --project chromium --timeout 2m --attempts 1 --format json
```

Application environment settings are not inherited automatically. If your
configuration reads `BASE_URL`, configure it in your terminal and name it:

```sh
9l run tests/checkout.spec.ts --pass-env BASE_URL --timeout 2m --attempts 1 --format json
```

Forward only the keys your test needs. Do not print credential values or forward
model-provider credentials to browser workers. The CLI adds its JSON evidence
reporter; configured reporters are replaced unless added with `--reporter`.
Existing configuration settings for traces and screenshots still apply.

## 4. Read the outcome

A successful one-spec run exits zero and reports `outcome: "passed"`, `complete: true`, with a
validated receipt whose `status` is `passed`. Check `executedTests` against what
you expected to run. A spec can contain several tests; the ordinary adapter
records test outcomes, not an inventory of individual assertions.

Receipts are saved under `.9lives/receipts/`. Inspect a saved run using the
`runId` returned by the command:

```sh
9l result <run-id>
```

A test assertion failure reports `failed` and a nonzero exit.
Missing tests, interrupted workers, or invalid evidence cannot establish a pass.
Review skipped tests and annotations too: a passing test may not exercise every
required behavior. See the [JSON and exit-code contract](contracts.md).

## 5. Check that a failure is detectable

Run a known business failure in a disposable local fixture and check that the
expected assertion fails. A setup error or timeout does not demonstrate that
your test protects the requirement.

The [runnable first-run example](../demo/first-run/README.md) provides a local
shop, a passing ordinary test, and experimental HTTP-500 fault tests. It shows
both a missing assertion and the stronger test that catches the same fault.
It uses synthetic data and makes no model calls.

## Troubleshooting

| Symptom | Next check |
| --- | --- |
| `9l` is missing or shows the Python CLI | Add the installation directory to `PATH`, or invoke the native binary by its full path. |
| Playwright or Node cannot be found | Install the project's locked dependencies and run from its project root with Node 22 or newer. |
| Browser executable missing | Install Chromium in the same user/runtime environment that runs the test. |
| Connection refused or wrong application | Start the app, check the existing `baseURL`/`webServer` settings, and forward required settings with `--pass-env`. |
| No matching tests or wrong project | Inspect the plan, spec path, config `testDir`, and project name. Use `--config`/`--project` when needed. |
| Test failed | Inspect the failing assertion and receipt; preserve the intended behavior when fixing it. |
| Run timed out or was interrupted | Inspect application state before rerunning a test that mutates data. Review both Playwright's timeout and the CLI budget. |

On Unix, the runner stops its process group and tracks descendants to clean up
Playwright's detached `webServer` processes on exit, deadline or cancellation.
This requires `ps` on `PATH`. During execution, observations run every 500 ms;
cleanup uses a 50 ms interval. A missed periodic observation can recover on the
next snapshot. Startup and cleanup require a successful snapshot within three
attempts; otherwise the command remains unsuccessful. Snapshots contain only
process IDs, relationships, status and start time. Tracking is sampled, so a
process that detaches and loses
its parent entirely between observations can escape tracking. This is not
kernel process containment. Start times have one-second resolution. The detached
server cleanup regression is runtime-qualified on macOS with Chromium; other
Unix platforms need their own runtime qualification.

## Optional next steps

Add the [SDK](sdk-bridge.md) only when you want step/assertion evidence or
experimental fault testing. For bounded natural-language actions, use the
separate [login goal example](login-goal-example.md), which requires a matching
application fixture. Keep explicit assertions after every goal; completion of
the goal itself is unverified.

This guide qualifies a local Chromium workflow. It does not establish production
coverage, live-provider correctness, or support for other browser engines.
