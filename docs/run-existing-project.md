# Run 9lives against playwright-login

This example opens the existing application's login modal and asserts its heading
and both fields. It exercises the real UI without signing in or changing data.
The harness lives outside the monorepo and needs no changes to its test setup.
Current goal execution supports local Chromium on macOS/Linux with Node 22+ and
Go 1.25.13. The SDK has not been published; install the locally built tarball.

## 1. Build the executable and SDK

From this runner workspace:

```sh
cd <LOCAL_CHECKOUT>
npm ci
npm run build
mkdir -p .context/build
go build -trimpath -o .context/build/9l ./cmd/9l
npm pack --workspace @9l/playwright --pack-destination .context/build
```

This creates `.context/build/9l` and
`.context/build/9lives-playwright-0.0.0.tgz`. The executable is native to your
machine. It launches the installed Playwright worker; it is not a bundled browser.

## 2. Start your existing application

Use your usual local playwright-login environment. In its own terminal, for an already
configured Python environment:

```sh
cd <LOCAL_CHECKOUT>
uvicorn backend.main:app --reload --host 127.0.0.1 --port 8000
```

If you normally use Docker, follow that repository's `DOCKER_DEVELOPMENT.md`
instead. Application dependencies and server credentials remain managed by the
application. First open `http://127.0.0.1:8000/app` and confirm that clicking
**Login** opens **Welcome back**. A healthy server endpoint alone does not prove
that the UI has loaded. Set the URL below to your actual local/staging origin.

## 3. Install an isolated test harness

Run in a separate terminal:

```sh
mkdir -p "$HOME/9lives-qa-rag-smoke"
cp -R <LOCAL_CHECKOUT>/examples/playwright-login/. "$HOME/9lives-qa-rag-smoke/"
cd "$HOME/9lives-qa-rag-smoke"
npm init -y
npm install --save-dev @playwright/test@1.61.1 <LOCAL_CHECKOUT>/.context/build/9lives-playwright-0.0.0.tgz
npm exec playwright install chromium
export QA_BASE_URL=http://127.0.0.1:8000
```

Linux may require `npm exec playwright install --with-deps chromium`.
The copied spec is `tests/login-modal.spec.ts`; keep its normal Playwright
assertions after the goal. The expected page comes from playwright-login's `/app`
route, `static/index.html` Login button and `static/js/auth-modal.js` form
(source reviewed at `c209fca62`). If those labels change, review the assertion
and fixture intentionally rather than weakening the check.

## 4. First run without a model key

From the harness directory:

```sh
<LOCAL_CHECKOUT>/.context/build/9l run tests/login-modal.spec.ts --sdk --goal-script login-script.json --pass-env QA_BASE_URL --timeout 2m --format json
```

The scripted provider chooses the currently observed **Login** control and
finishes its action sequence. The assertions wait for the actual modal.
This proves setup and browser execution; it does not measure LLM accuracy.
A zero exit code plus `complete: true` and a validated passing receipt means
the required Playwright test passed. `goals[].status: completed` alone means
only the goal's action loop completed. Receipts are under `.9lives/receipts/`.

## 5. Run the natural-language provider

Configure `ANTHROPIC_API_KEY` securely in the calling terminal using your existing
secret manager; do not put the value in the spec, shell history or command line.
Then, from the same harness directory:

```sh
<LOCAL_CHECKOUT>/.context/build/9l run tests/login-modal.spec.ts --sdk --goal-provider anthropic --goal-max-actions 3 --goal-max-decisions 12 --goal-timeout-ms 60000 --pass-env QA_BASE_URL --timeout 2m --format json
```

Alternatively select `--goal-provider openai` with `OPENAI_API_KEY` configured.
Provider credentials remain in Go; never forward them with `--pass-env`.
`--goal-model` overrides the existing transport's pinned default. This run sends
your goal and bounded, redacted control labels/headings/status text to the
selected provider and can incur API charges. Do not put secrets in goal text.
Named `params` values stay in the worker. Receipts retain decision IDs, typed
actions, outcomes and reported usage, rather than page text or model responses.

Provider usage is marked unavailable when absent. Token reservations are
conservative bounds, not measured usage. To enforce a monetary reservation cap,
supply all three flags: `--goal-max-cost-micros`,
`--goal-input-micros-per-million`, and `--goal-output-micros-per-million`.
Prices are caller-supplied conservative upper bounds in micro-USD per million
tokens; consult your provider/account prices. The receipt's estimated cost is
not a billing receipt. Live model correctness/cost remain unqualified until
measured against representative applications.

## Use it in an existing Playwright suite

Install the SDK tarball beside that suite's pinned Playwright 1.61.1 dependency,
change the selected spec's import to `@9l/playwright`, and add
`n9l.goal(...)` where needed. Keep the existing config and explicit
assertions. Run from the suite's directory with `9l run ... --sdk` plus the
chosen provider. For authenticated tests, prepare an approved Playwright
`storageState` through your existing setup; password filling is outside this
first goal executor. Add only needed application settings to `--pass-env`.

## Diagnose a failure

- **No local Playwright/SDK:** install both into the harness and build the SDK.
- **Browser missing:** install Chromium in the same user/runtime environment.
- **Connection refused:** start the app and check `QA_BASE_URL`; `--pass-env` is
  required because application environment is opt-in.
- **Provider required/error:** choose exactly one provider or script, and check
  key presence through your secret manager without printing its value.
- **Budget exhausted/unresolved:** inspect the bounded receipt; reduce the goal
  to a clear UI task, then intentionally adjust budgets if necessary.
- **Policy blocked:** purchase, deletion and outreach controls stop by default.
  Use ordinary approved Playwright code for such actions in controlled fixtures.
- **Unknown effect/interrupted:** inspect application state before rerunning;
  the engine does not blindly retry a possibly completed action.
- **Goal completed but test failed:** inspect the business assertion; fix the
  application or the test's intended expectation, not the goal completion flag.

Qualification: the packaged binary and SDK were installed into a fresh harness
and this spec passed against the checked-out application's actual auth-modal
JavaScript served by an isolated local fixture. The full playwright-login backend was
not running during qualification; follow step 2 to validate the complete app.
No live model request was made.
