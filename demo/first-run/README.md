# First run: a green checkout can miss a broken requirement

Already have Playwright? Start with the [existing-spec quickstart](../../docs/run-existing-project.md).
This separate local example makes the evidence and its limits tangible.

## Run from a fresh checkout

Use Node 24 (Node 22 minimum), Go 1.25.13 or newer, and Chromium. No QualityMax
account, model key, Python runtime, or external application is needed.

```sh
git clone https://github.com/Quality-Max/9lives-runner.git
cd 9lives-runner
npm ci
npm exec playwright install chromium
npm run demo:first-run
```

On Linux, install browser system dependencies with
`npm exec playwright install --with-deps chromium`. Dependency installation
needs network access; execution uses only a synthetic localhost shop and makes
no model calls. Allow for the Go build and nine Chromium test executions.

## What it checks

The example reuses the [release lab](../release-lab/README.md)'s app, requirements
and unchanged tests, copied into a new `.context/first-run-<id>/project/`. Your
application and test configuration are untouched. Orders have no payment,
persistence or real-world effect.

1. Run the ordinary `@playwright/test` spec without the SDK. Two cart items and
   an order confirmation for two items must pass.
2. Run the SDK checkout spec on the healthy app, then inject HTTP 500 into each
   of three observed requests, one at a time. Cart and order faults must cause
   assertion failures. The recommendation fault survives: the test omits it.
3. Run the stronger, separate spec on the same healthy app and the same faults.
   Its additional recommendation assertion must catch the previously surviving
   fault. The weaker test is retained for comparison.

| Synthetic requirement | Fault target | Expected weak test | Expected stronger test |
| --- | --- | --- | --- |
| Display exactly two cart items | `/api/cart` | Assertion fails | Assertion fails |
| Confirm an order of two items | `/api/orders` | Assertion fails | Assertion fails |
| Display the pencil set recommendation | `/api/recommendations` | Passes; missing check | Assertion fails |

These are agent-authored demonstration requirements, not production requirements.
Review the shared [contract](../release-lab/requirements.json) and the
[weaker](../release-lab/tests/checkout.spec.ts) and
[stronger](../release-lab/tests/checkout-complete.spec.ts) assertions.

## Expected result and evidence

```text
1/3 Ordinary Playwright: one executed test, validated passing receipt.
2/3 missing recommendation check: healthy baseline passed; 2 faults caught, 1 survived.
3/3 complete synthetic contract: healthy baseline passed; 3 faults caught, 0 survived.
```

The script checks every receipt's validation, executed test count and terminal
status, plus fault application and failed-assertion attribution. It exits nonzero
on unexpected or incomplete results. A complete `prove` experiment can contain
surviving faults; the script checks the expected counts explicitly.

Open the printed directory's `summary.json`, `checkout-proof.json`,
`checkout-complete-proof.json`, and `receipts/`. The summary records the checkout
revision and fixture hashes. Each run gets a fresh directory; partial runs stay
`complete: false`. Evidence contains known synthetic request paths, not page bodies.

To inspect the actual commands, see [run-demo.cjs](run-demo.cjs). In the copied
project, using the locally built `../9l` binary (`../9l.exe` on Windows), the core
commands are:

```sh
../9l run tests/ordinary.spec.ts --timeout 30s --attempts 1 --format json
../9l prove tests/checkout.spec.ts --faults http-500 --timeout 30s --pass-env LAB_PORT --format json
../9l prove tests/checkout-complete.spec.ts --faults http-500 --timeout 30s --pass-env LAB_PORT --format json
```

For manual `prove` runs, set `LAB_PORT` to one available, fixed local port so
baseline and fault runs share an origin. The script chooses it automatically.

If setup fails, run `npm ci` and install Chromium in the same runtime. If the
experiment fails, inspect its incomplete summary and receipts before rerunning;
an environment error is not a caught business fault.

## Limits

This is one synthetic app in Chromium with one supported fault kind, HTTP 500,
on three fetch/XHR endpoints. A caught fault means an applied fault caused an
assertion failure. It does not establish independent requirement coverage or
qualify other apps, browser engines, databases, live providers, healing, or replay.
See the experimental [fault-testing contract](../../docs/prove.md).
