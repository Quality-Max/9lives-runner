# Local checkout demo

Exercise the Go runner, TypeScript SDK and a real Chromium assertion without
an application server, model key or QualityMax account.

From a fresh checkout, with Go 1.25.13 or newer and Node 24:

```sh
git clone https://github.com/Quality-Max/9lives-runner.git
cd 9lives-runner
npm ci
npm exec playwright install chromium
npm run demo
```

On Linux use `npm exec playwright install --with-deps chromium`.
The command builds the local SDK and runs the existing
[checkout fixture](../testdata/sdk/tests/checkout.spec.ts). It clicks **Place
order** and asserts that the status becomes **Order confirmed: 1 item**.
A mismatch fails the test; completing the click alone cannot pass it.

Inspect the JSON result and saved `.9lives/receipts/` evidence. A successful run
has `complete: true` and a validated passing test receipt. The independently
verified assertion count remains zero: report validation does not establish
semantic coverage. This synthetic fixture qualifies the execution path, not a
production checkout or live model correctness.

To exercise the real business failure and owned timeout/cancel controls too:

```sh
npm run smoke
```

Use [the application guide](../docs/run-existing-project.md) for a separate
harness against your own approved application.
