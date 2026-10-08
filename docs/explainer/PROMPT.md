# Claude Design prompt: 9lives explainer

Paste everything below the line into Claude Design.

---

Design a single-page explainer for **9lives**, an open-source, developer-first
testing framework for Playwright. The audience is senior engineers and QA leads
who are sceptical of "AI testing" marketing. The tone is calm, precise and
honest. Nothing may be described as shipped unless it is listed under "Shipped
today" below. Avoid hype words such as autonomous, magic, zero-effort or
self-driving.

## Page goal

A reader should finish the page able to answer four questions:

1. What does a 9lives test look like, and how is it different from plain Playwright?
2. Does it need Claude or any AI model to run? (Answer: no.)
3. How is it different from Vibium and the hosted "self-healing" test vendors?
4. Why would anyone build it this way, and what is still unproven?

## Structure (six sections, in this order)

### 1. Hero

Headline idea: "Ordinary Playwright tests. Bounded AI help. Evidence for every attempt."
One sentence beneath it: a model finishing a task is never a passing test; your
assertions are the oracle and Go keeps the receipts.
Show a tiny, real code sample in the hero. Use this exactly:

```ts
import {test, expect} from '@9l/playwright';

test('checkout confirms an order', async ({page, n9l}) => {
  await page.goto('http://localhost:3000/checkout');
  await n9l.step('place the order', async () => {
    await page.getByRole('button', {name: 'Place order'}).click();
  });
  await expect(page.getByRole('status')).toHaveText('Order confirmed');
});
```

### 2. What a test looks like

Three side-by-side cards, each with a short code block and one line of
explanation:

- **Steps.** `n9l.step('label', fn)`. Ordinary code wrapped in a labelled unit
  so the Go core can tie evidence to the exact run, job and attempt.
- **Goals.** `n9l.goal('Click the Login button. Stop when Welcome back is visible. Do not submit the form.')`
  A bounded natural-language action with an explicit stop condition, followed
  by normal `expect` assertions. The model sees only the names of observed
  controls and parameter names. Values stay in the browser worker.
- **Budgets.** `{params: {name: 'Fixture Person'}, maxActions: 3, timeoutMs: 30_000}`
  Action caps and deadlines are enforced by Go, not by the model loop.
  Purchase, delete and outreach controls stop by default.

Caption under the cards: "Everything else is stock Playwright. Same config, same
fixtures, same assertions."

### 3. Runs without an AI model

A clear two-column comparison.

Left column, "No model needed":
- Step-only tests never contact a model.
- Goal tests can run with `--goal-script file.json`, an offline scripted
  provider that replays a fixed, bounded action list. No API key, no charge.
- Local execution needs no account and no network beyond your own app.

Right column, "Model is opt-in":
- `--goal-provider anthropic` or `openai`, chosen explicitly per run.
- No silent fallback to a CLI tool or an agent.
- API keys stay in the Go process. They are never written to plans or receipts.

Small callout: "An agent such as Claude can help author a spec. Once written,
the spec is a file that runs without the agent."

### 4. How it compares

A compact matrix, rows are capabilities, columns are: 9lives, Vibium, hosted
self-healing vendors (Testim, mabl, Momentic and similar), plain Playwright.
Use filled, half and empty markers, no text in cells. Rows:

- Ordinary code and assertions as the oracle
- Bounded natural-language actions
- Hard action and time budgets enforced outside the model
- Per-attempt evidence receipts, redacted, stored locally
- Runs locally with no account
- Browser driver layer (BiDi or CDP)
- Agent-facing MCP surface
- Model completion counts as a pass

Under the matrix, one paragraph: Vibium replaces the browser driver and gives
agents a compact command surface. 9lives sits one layer up, on top of
Playwright, and owns execution policy and evidence. They are complements, not
competitors; Vibium could in principle become a 9lives backend.

### 5. Shipped today vs planned

Two honest lists. This section is mandatory and must be visually equal in
weight to the comparison.

**Shipped today**
- `n9l.step` and bounded `n9l.goal` in `@9l/playwright`
- `9l plan`, `9l run --sdk`, `9l status`, `9l result`, `9l cancel`
- Explicit Anthropic or OpenAI provider, or offline scripted provider
- Run-wide budgets, bounded concurrency, attempt limits, process-tree cancellation
- Per-attempt receipts with redacted stdout and stderr under `.9lives/receipts/`
- Scrubbed test environment; env vars forwarded only when named
- A skipped test fails the attempt unless explicitly pinned
- `9l assess`: opt-in advisory assessment of a Playwright spec against shared requirements

**Planned, not shipped**
- Independent behavioral verification (the `result.verified` field exists but is always false today)
- Replay of verified actions
- Native mobile execution
- Measured correctness, latency and cost of live model runs
- General semantic review of generated tests

### 6. Why build it this way

Four short statements, no more than two sentences each:

- **It sells worse on purpose.** "The model finished so the test passed" demos
  well and is wrong. 9lives refuses that framing.
- **Local-first removes lock-in.** Hosted vendors need the account because the
  recordings and healing data are the product. Here they are files in your repo.
- **Policy lives in Go, browsers live in Playwright.** Two runtimes and a
  versioned protocol cost more to maintain, but attempt identity, budgets and
  cancellation stay deterministic.
- **The hard part is still ahead.** Independent verification is an open
  problem for everyone. Until it lands with measured results, a fair reviewer
  can call this Playwright plus a bounded goal loop plus good receipts.

Closing line: "Run it locally. Read the receipts. Keep your assertions."

## Visual direction

- Dark, technical, lots of whitespace. Monospace for all code and flag names.
- One accent colour for the "shipped" markers and a muted grey for "planned".
  Never use green for planned items.
- Code blocks must be real, copyable text, not images.
- No stock illustrations, no robots, no sparkles. Diagrams may show the
  boundary between the Go core and the Playwright worker as two boxes with a
  single labelled protocol arrow (`9l.engine/1`).
- Responsive: the comparison matrix collapses to stacked cards on mobile.

## Hard constraints

- Do not add capabilities beyond the "Shipped today" list.
- Do not use the words autonomous, self-healing, magic, zero-effort or AI-powered.
- Do not claim performance, accuracy or cost numbers. None are qualified yet.
- Keep every code sample exactly as given.
