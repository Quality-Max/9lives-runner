# Run 9lives against an existing application

The supplied login example opens a **Login** modal and asserts **Welcome back**,
**Email** and **Password**. Use it with an application that has those controls,
or intentionally adapt the goal and assertions to your reviewed requirements.
It does not sign in or change application data. The harness lives outside the
application repository and preserves its existing Playwright setup.

Use Node 24 (Node 22 minimum), Playwright 1.61.1 through 1.64.0, and Chromium.
The SDK is published as `@9l/playwright` 0.1.2.

## 1. Install the CLI and get the example

Install the executable using the [CLI installation guide](install.md). With
Go 1.25.13 or newer, `go install github.com/Quality-Max/9lives-runner/cmd/9l@latest`
installs it from public source. Clone the repository for the example harness:

```sh
git clone https://github.com/Quality-Max/9lives-runner.git
RUNNER_ROOT="$PWD/9lives-runner"
9l --version
```

The executable launches the project's installed Playwright worker; it does not
bundle a browser. If Python `9lives` is installed too, use the explicit path to
the Go executable to avoid their shared `9l` command name.

## 2. Start your application

Start your application's development server using its own documented setup.
Application dependencies and server credentials stay managed by the application.
Open its login page and confirm that **Login** opens **Welcome back** with
**Email** and **Password** fields. The example navigates to `/app`; adjust that
path intentionally if your application uses another route.

## 3. Install an isolated test harness

In a separate terminal, set the same runner checkout path and your application's
origin:

```sh
RUNNER_ROOT=/path/to/9lives-runner
mkdir -p "$HOME/9lives-login-smoke"
cp -R "$RUNNER_ROOT/examples/playwright-login/." "$HOME/9lives-login-smoke/"
cd "$HOME/9lives-login-smoke"
npm init -y
npm install --save-dev @playwright/test@1.64.0 @9l/playwright@0.1.2
npm exec playwright install chromium
export QA_BASE_URL=http://127.0.0.1:8000
```

On Linux use `npm exec playwright install --with-deps chromium` if browser
system dependencies are missing. Set `QA_BASE_URL` to your approved local or
staging origin. Keep the normal Playwright assertions after the goal.

## 4. First run without a model key

From the harness directory:

```sh
9l run tests/login-modal.spec.ts --sdk --goal-script login-script.json --pass-env QA_BASE_URL --timeout 2m --format json
```

The scripted provider chooses the observed **Login** control and finishes its
bounded action sequence. Assertions wait for the actual modal. This verifies
setup and browser behavior for the selected fixture; it does not measure LLM
accuracy. A zero exit code, `complete: true` and a validated passing receipt
mean the required test passed. A completed goal alone is unverified. Receipts
are under `.9lives/receipts/`.

## 5. Run a natural-language provider

Configure `ANTHROPIC_API_KEY` securely in the calling terminal through your
existing secret manager. Do not put its value in the spec, command line or
shell history. From the same harness directory:

```sh
9l run tests/login-modal.spec.ts --sdk --goal-provider anthropic --goal-max-actions 3 --goal-max-decisions 12 --goal-timeout-ms 60000 --pass-env QA_BASE_URL --timeout 2m --format json
```

Alternatively use `--goal-provider openai` with `OPENAI_API_KEY` configured.
Credentials remain in Go; never forward them with `--pass-env`. `--goal-model`
overrides the transport's pinned default. This sends the goal and bounded,
redacted control labels to the selected provider and can incur API charges.
Named parameter values stay in the worker. Receipts omit page text and raw
provider responses. Do not put secrets in goal text.

Provider usage is marked unavailable when absent. Token reservations are
conservative bounds, not measured usage. A monetary reservation cap needs all
three flags: `--goal-max-cost-micros`, `--goal-input-micros-per-million` and
`--goal-output-micros-per-million`. Supply conservative prices in micro-USD per
million tokens from your provider/account. Estimated cost is not a billing
receipt. Live model correctness and cost remain unqualified.

## Use it in an existing Playwright suite

Install the SDK beside the suite's pinned Playwright dependency (1.61.1 through 1.64.0). Change
selected imports to `@9l/playwright` and add `n9l.goal(...)` where useful. Keep
the existing configuration and explicit assertions. Run from the suite with
`9l run ... --sdk` and one provider or script. Prepare authenticated state
through the existing approved setup; password filling is unsupported by the
goal executor. Forward only needed application settings with `--pass-env`.

## Diagnose a failure

- **SDK unavailable:** install the package and the pinned Playwright dependency.
- **Browser missing:** install Chromium in the same user/runtime environment.
- **Connection refused:** start the app and pass `QA_BASE_URL` explicitly.
- **Provider error:** choose one provider or script and check credential presence
  in your secret manager without printing its value.
- **Budget exhausted:** review the goal and its limits against the intended task.
- **Policy blocked:** purchase, deletion and outreach controls stop by default.
  Use approved ordinary Playwright actions in controlled fixtures when needed.
- **Interrupted or unknown effect:** inspect application state before rerunning;
  goal mutations are not automatically retried.
- **Goal completed but test failed:** inspect the business assertion; goal
  completion cannot replace its expected outcome.

Historical qualification used an isolated local fixture with a login modal.
That does not qualify every application, a complete backend or live providers.
