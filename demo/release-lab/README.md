# Release lab: a green checkout with a blind spot

For a shorter executable introduction, use the [first-run example](../first-run/README.md).
For your own suite, start with the [quickstart](../../docs/run-existing-project.md).

**Just want to understand the idea?** Open the
[three-click demo](../../docs/explainer/demo.html) in a browser. It is a single,
offline HTML file with a locator inspector and a missing-check story, based
on a checked local run. Agent handoffs are illustrated, not live model calls.
No setup required. The walkthrough below executes the real experiments and
produces their evidence.

A practical walkthrough of 9l's experimental features, built around a tiny
local shop. The hook for a live demo: **the checkout passes, but one part of
the experience can break without the test noticing.** Inject failures, find
the missing assertion, then prove the stronger test rejects those failures.

The shop uses synthetic data, real Chromium, and no external service. Once
dependencies are installed, the default walkthrough needs no model, provider
credentials, platform account, or network access outside localhost.

## Run it

From the repository root, with Node 24 and Go 1.25.13 or newer:

```sh
npm ci
npm exec playwright install chromium
npm run demo:lab
```

Linux browser dependencies: `npm exec playwright install --with-deps chromium`.
The script builds the current CLI and SDK, then runs eight chapters. Allow a
few minutes for repeated browser starts and intentional assertion failures.
It prints the path to an HTML report, screenshots, JSON results, source
snapshots and execution receipts under `.context/release-lab-<unique-id>/`.
Open the printed `index.html` in your browser. Expand each card for commands
and evidence. Partial runs remain visibly incomplete; unexpected results
exit nonzero. Each run gets a new directory, so old evidence is preserved.

To watch the ordinary and SDK `run` chapters in browser windows:

```sh
npm run demo:lab -- --headed
```

This uses `--headed` and the project's `slowMo` setting for those chapters.
Scan, inspect, heal, prove, confirm and MCP keep their normal headless execution.
On Linux, headed mode requires a display (or `xvfb-run`).

To explore just the store:

```sh
npm run demo:shop -- 4173
```

Open `http://127.0.0.1:4173`. Omit the port to choose an available one. Stop
with Ctrl-C. Orders have no payment, persistence or real-world effect.

## Presenter walkthrough

| Chapter | What to show | Evidence required by the script |
| --- | --- | --- |
| 1. Scan + inspect | Start with a clean baseline; remove the promo label and break support. Observe ranked page controls. | New accessibility and broken-link findings; `--fail-on-new` exits 1. |
| 2. Assess + provenance | The demo contract asks for cart, order and recommendations. The draft maps only two. | An unmapped-outcome advisory finding and a creation snapshot. |
| 3. Run + SDK steps | Run the normal Playwright test, then a test with `n9l.step`. Both are green. | One executed test and a validated receipt per adapter. |
| 4. Bounded goals | Fill the customer name and continue. Repeat with a broken profile response, then try a purchase goal. | Good form passes; a completed goal plus failed assertion fails; purchase goal is policy-blocked and no order request occurs. |
| 5. Verified healing | Seed the old button name `Continue now`; the page says `Continue to review`. | Original fails; isolated candidate passes; exactly one locator literal changes; assertions and original source stay intact. |
| 6. Fault proof | Break cart, orders and recommendations with abort, HTTP 500 and empty JSON. Add the recommendation assertion and repeat. | Weak test: 6 faults caught, 3 survive. Strong test: all 9 caught. Both baselines pass; all 20 receipts validate. |
| 7. Fix confirmation | An order-count defect displays one item instead of two. Run the same test across the buggy and fixed commits. | `confirmed`: assertion failure before, pass after; both receipts validate. |
| 8. Native MCP | Generate a support-link repro through the stdio server, inspect its assertion, then execute it before and after repairing the route. | All 7 tools discovered; generated test fails on 404 and passes on 200, with validated receipts. |

For a short presentation, lead with chapter 6's **6 caught / 3 survived →
9 caught / 0 survived** result. Show the two small checkout specs side by side.
Then show chapter 7 to explain why detecting a bug and confirming its fix
are separate questions. The report is the presentation; the expandable JSON
links let someone challenge each result.

## The demo contract

This is an agent-authored **synthetic demo requirement proposal**, not a claim
about reviewed production requirements:

1. The cart displays exactly two items.
2. Placing the order confirms two items.
3. The pencil set recommendation is visible.

The [requirements contract](requirements.json) is separate from the tests.
[checkout.spec.ts](tests/checkout.spec.ts) deliberately omits the third
assertion. [checkout-complete.spec.ts](tests/checkout-complete.spec.ts) adds it.
Assessment supplies a suspected mapping gap; executed fault injections
establish the demonstrated blind spot. Nothing rewrites a requirement or
weakens an assertion to turn the run green.

The confirmation chapter creates two commits **inside its own throwaway
repository**, with the shop server started from each isolated checkout.
It does not modify this repository's branch, commits or checked-in tests.
Generated repros and healed candidates also live in the run's copy.
The copy and its evidence remain available for inspection; delete that run's
`.context/release-lab-…` directory when finished.

## Try individual features

After building `.context/9l` from the root (`go build -o .context/9l ./cmd/9l`)
and running `npm run build`, use a terminal in `demo/release-lab`:

```sh
../../.context/9l run tests/ordinary.spec.ts
../../.context/9l run tests/checkout.spec.ts --sdk --headed
../../.context/9l assess tests/checkout.spec.ts --requirements requirements.json
../../.context/9l run tests/goal.spec.ts --sdk --goal-script goal-script.json
../../.context/9l run tests/policy.spec.ts --sdk --goal-script policy-script.json
```

The policy example intentionally exits 3 (incomplete), even though its
assertions confirm that no order was sent. The stale-locator test intentionally
fails. Don't run this entire directory as a release gate expecting all green.

`prove` needs the **same free port on every rerun**. For an individual proof,
set `LAB_PORT` to a free port in your shell and pass `--pass-env LAB_PORT`:

```sh
../../.context/9l prove tests/checkout.spec.ts --pass-env LAB_PORT
../../.context/9l prove tests/checkout-complete.spec.ts --pass-env LAB_PORT
```

The full walkthrough picks the port for you. Omit neither the variable nor its
forwarding: a different origin in each worker would make fault targeting invalid.
Additional fault kinds can be explored with
`--faults http-401,http-403,http-429,malformed-json`; the report's default
walkthrough qualifies only abort, HTTP 500 and empty JSON.

## Optional live provider path

The goal chapter deliberately uses a deterministic `--goal-script`. This
qualifies goal execution and its assertion/policy boundaries; it is **not a
model demonstration or verified replay**. To try actual natural-language
planning with an already configured CLI provider, run the same spec with one
explicit provider instead of the script, for example:

```sh
../../.context/9l run tests/goal.spec.ts --sdk --goal-provider codex --headed
```

The provider uses its own login and may incur usage. Results depend on the
provider and are not part of the checked offline report. See [goal
providers](../../docs/goals.md) for other CLI/API providers and budgets.

Healing in this demo verifies **offline Tier 1**, with `--provider none` and
zero provider calls. [Native Tier 2](../../docs/native-tier2.md) is available
for locator repairs that Tier 1 cannot solve; naming a provider on this seeded
case does not guarantee a model call because Tier 1 gets the first attempt.
No live Tier 2 model quality is claimed by this demo.

Scan results are local observations, not a full accessibility audit or field
performance data. Caught network faults show an assertion failed, not that all
business behavior is covered. Confirm distinguishes these two revisions for
this repro, not every possible fix. Declared provenance does not authenticate
authorship. Verified replay and native mobile qualification remain planned.
