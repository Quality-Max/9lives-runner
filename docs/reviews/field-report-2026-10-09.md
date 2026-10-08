> **Independent review.** This is an external field report on CLI 0.0.0 and
> 0.1.0, reproduced unedited below this note. The reviewer removed details that
> identify their product; the numbers describe 0.1.0, not later changes.
>
> Maintainer status as of 2026-10-09:
>
> | Report item | Status |
> | --- | --- |
> | §1 `9l` name clash with Python `9lives` | Documented in [Install](../install.md) |
> | §3 `toEqual([])` after a wait not treated as absence | Fix proposed in PR #24 (open) |
> | §5a `.catch()` on an awaited matcher flagged as unawaited | Fix proposed in PR #24 (open) |
> | §5b Concise arrow returning a matcher flagged as unawaited | Fix proposed in PR #24 (open) |
> | §5c Helper findings reported at every caller | Fix proposed in PR #24 (open) |
> | §5d `unmapped-outcome` checked per test | Per file in PR #24 (open); per suite tracked in #20 |
> | §5e Provenance covers only the spec file | Tracked in #22 |
> | §5f `--attempts 1` does not override Playwright `retries` | Tracked in #19 |
> | §5g Project global setup runs under `9l run` | Expected; the project's Playwright config is kept |

---

# 9l on a real Playwright suite: a field report

Two days of trying the 9lives Go runner (`9l` 0.0.0, then 0.1.0) on a production end-to-end suite. Everything identifying the product has been removed. The numbers, commands and code patterns are real.

**The suite:** 87 spec files and 760 tests across two web apps and their HTTP APIs. A scheduling product whose calendar lives in Europe/Berlin while its backend runs on UTC. It is full of time-zone and daylight-saving logic. Playwright 1.x, Node 26, macOS on Apple Silicon.

---

## TL;DR

| What I tried | Outcome |
|---|---|
| `9l assess` on the whole suite | 87 files in **2.7 s**, nothing crashed, nothing skipped |
| New `absence-after-wait` rule | Flagged **2 of 3** real "assert nothing happened after a sleep" checks, which had been fixed by hand a day earlier |
| Full authoring loop (`provenance → assess → run`) | Worked end to end. The new tests failed on **3 of 3** planted defects |
| Remaining findings on the suite | **All 16** `unawaited-assertion` findings were false positives, from 2 code patterns |

To be clear about what this was not: 9l did **not** find a new product defect. It reviews tests, not the application. What it found was a class of weak test, and that is the value shown below.

---

## 1. Install

```sh
gh release download v0.1.0 --repo Quality-Max/9lives-runner \
  --pattern 9l-darwin-arm64.tar.gz --pattern SHA256SUMS
awk '$2 == "9l-darwin-arm64.tar.gz"' SHA256SUMS > selected.sha256
shasum -a 256 -c selected.sha256          # 9l-darwin-arm64.tar.gz: OK
tar -xzf 9l-darwin-arm64.tar.gz && ./9l-darwin-arm64/9l --version
# 9l 0.1.0 (Go runner)
```

**Gotcha:** the Python `9lives` package also installs a command called `9l`. With both installed, the one on `PATH` was the Python one. I called the Go binary by its full path, and later uninstalled the Python package.

## 2. Assess the whole suite: no contract, one command

v0.0.0 needed a requirements file and one spec at a time. 0.1.0 takes a glob:

```sh
9l assess 'tests/**/*.spec.ts'
```

```
Summary: 87 files (87 assessed, 0 not assessed), 760 tests, 1036 findings
  analysis-limit/unresolved-helper: 439
  analysis-limit/nested-function: 279
  analysis-limit/conditional-flow: 240
  analysis-limit/declaration-generation: 42
  unawaited-assertion: 16
  fixed-wait: 11
  conditional-skip: 8
  analysis-limit/runtime-skip: 1
```

The ~1000 `analysis-limit` entries are 9l saying "I can't see inside this", not reporting a problem. The ones to read are the last four rules. The per-rule summary added in 0.1.0 is what makes that split visible at a glance.

Skip guards are also read correctly now. The suite has 8 guards of the form `test.skip(isWeekend(), '…')` at `describe` level. 0.0.0 counted them as tests; 0.1.0 reports them as `conditional-skip [informational]`. That's correct: they are deliberate.

## 3. What it caught: "nothing happened" after a sleep

The suite had four `waitForTimeout` calls followed by an assertion that something did **not** happen. Such a test can only fail one way, by passing when it shouldn't: if the app is merely slow, the sleep ends first and the "it didn't happen" check passes without testing anything.

```ts
// Before: asserts a popup did NOT open
let popupOpened = false;
context.on('page', () => { popupOpened = true; });
await page.getByRole('button', { name: 'Join' }).click();
await page.waitForTimeout(1_000);
expect(popupOpened).toBe(false);
```

These had been found and fixed by hand the day before: the listener is armed *before* the action, and the test waits for the event itself.

```ts
// After: wait for the event; only a timeout means "did not happen"
const popupOpened = context.waitForEvent('page', { timeout: 5_000 })
  .then(() => true, () => false);
await page.getByRole('button', { name: 'Join' }).click();
expect(await popupOpened).toBe(false);
```

So I ran 0.1.0 on the **pre-fix** versions to see whether the new rule would have found them:

```
fixed-wait [demonstrated] at 89:11:  A waitForTimeout call is present; …
absence-after-wait [suspected] at 90:5:  The first recognized assertion after a
  fixed wait checks that something is absent or did not happen; if the
  application is merely slow, it passes without the outcome having occurred.
fixed-wait [demonstrated] at 149:11: …
absence-after-wait [suspected] at 156:11: …
```

- **Caught:** `expect(flag).toBe(false)` and `expect(locator).not.toBeVisible()` after a sleep.
- **Missed:** `expect(mutations).toEqual([])` after a sleep. It asserts "no request was sent", which is an absence check by meaning, but the rule doesn't count `toEqual([])` as one.
- **Correctly not flagged:** a fixed wait followed by a normal positive assertion.

On the fixed branch the rule reports nothing. That's the right result too.

## 4. The authoring loop: provenance → assess → run

9l has no command that writes tests. The workflow is: the coding agent writes the spec, and 9l records where it came from, checks it against a requirements contract, and runs it with a receipt.

I used it for a real gap. The date helpers that choose which DST switch Sunday every time-switch test books into had no direct tests. If they picked the wrong Sunday, every DST test would quietly move to an ordinary week and keep passing.

**Contract** (5 expected outcomes, written from the ticket, not from the code):

```json
{ "id": "switch-dates", "reference": "<ticket>", "revision": "1",
  "expectedOutcomes": [
    { "id": "last-sunday",   "description": "Switches fall on the last Sunday of March and of October." },
    { "id": "offsets",       "description": "UTC+1 → UTC+2 in March, the reverse in October." },
    { "id": "straddle-days", "description": "The bookable pair is the Friday before and the Monday after." } ] }
```

**Loop:**

```sh
9l provenance tests/dst-switch-helpers-api.spec.ts --agent claude > .context/creation.json
9l assess     tests/dst-switch-helpers-api.spec.ts --requirements reqs.json \
              --agent-provenance .context/creation.json
9l run        tests/dst-switch-helpers-api.spec.ts --agent-provenance .context/creation.json \
              --pass-env E2E_POSTGRES_PORT --timeout 60s --deadline 120s --attempts 1 --format json
```

```
Agent branch/source provenance: matched
…
complete: true  passed: 1  executedTests: 5  durationMs: 564  status: passed
```

**Is the green result meaningful?** A pass alone doesn't show that. So I planted three defects, one at a time, and ran each through `9l run`:

| Planted defect | Run | Failed on |
|---|---|---|
| Straddle day is Thursday instead of Friday | ✘ | both straddle-day assertions |
| Selector skips the nearest switch | ✘ | the "next two switches" assertion |
| "Last Sunday" of the wrong month | ✘ | the published-dates table, plus a guard in the helper itself |

All three fail where they should. The spec then went into the suite as an ordinary pull request.

## 5. What got in the way: minimal repros

**a) `.catch()` on an awaited matcher is reported as unawaited**

```ts
await expect(header).not.toContainText(prev, { timeout: 2_000 })
  .catch(() => nextArrow.click());          // ← "neither directly awaited nor returned"
```

The chain *is* awaited. This one pattern, inside one date-picker helper, produced 12 of the 16 findings.

**b) A concise arrow function that returns a matcher**

```ts
const rendered = {
  Leaders: () => expect(page.getByRole('heading', { name })).toBeVisible(),  // flagged
};
await rendered[tab]();
```

The promise is returned and awaited by the caller. This produced the other 4.

**c) Helper findings are reported at every caller, not at the helper**

One 50 ms polling loop in a helper became **9** `fixed-wait` findings, one per calling test. Each was reported at the line of the *call* (`await fillStep1(…)`), so nothing pointed at the line to fix. Reporting the origin once, and listing its callers, would turn 12 + 9 findings into 2.

**d) `unmapped-outcome` is checked per test, not per suite**

Each test is tagged `@9l-requirement switch-dates`, but covers only some of its outcomes, while the other tests cover the rest. 9l reports 6 "unmapped" outcomes although every outcome is asserted somewhere. It only goes quiet with one requirement per test, which most suites aren't written that way.

**e) Provenance covers only the spec file**

During the mutation runs above, the helper under test was edited and the receipt still said `provenance: matched`. That is documented ("cannot fingerprint the whole dirty application"), but a reader of the receipt can easily take "matched" to mean the code under test.

**f) `--attempts 1` doesn't override the project's own `retries`**

With `retries: 1` in the Playwright config, every failure appeared twice in the evidence.

**g) Watch the global setup**

`9l run` uses the project's normal Playwright config, including global setup and teardown. Ours cleans a database on a default port, and on this machine that port belonged to *another* project's running stack. I pointed it at an unused port and passed it through with `--pass-env`. That's not 9l's fault, but anyone running it on a shared machine should know.

## 6. Verdict

| | |
|---|---|
| **Use it now** | `9l assess` as an occasional lint run by hand. It's fast, it doesn't need a contract any more, and `absence-after-wait` targets a real, under-detected weakness. |
| **Not yet** | As a CI gate. On this suite the false positives outnumber the real findings, and a noisy check teaches people to ignore it. Fixing patterns (a)–(c) would remove 21 of the 27 rule findings. |
| **Promising** | Requirement contracts. Mapping assertions to a ticket's expected outcomes makes "which test proves what" explicit. It needs per-suite coverage (d) to suit how real suites are laid out. |
| **Little extra value here** | `9l run`, in a project that already has CI reports, traces and a published report. The receipt mostly repeats what CI keeps, and its provenance covers the spec, not the build. |

Overall, the static checks are cheap and point the right way. The best result in this trial came from combining them with the other step: when the tool reports a pass, plant a defect and check the test fails on it.
