# 9lives demos

## QualityMax Demo Bank

The [README GIF](demo-bank.gif) follows one Playwright test against the public
[QualityMax Demo Bank](https://qualitymax-demo-bank.vercel.app/). It uses large
text and one step per frame so you can read the commands inside GitHub.

The test checks that submitting an empty sign-in form leaves the visitor signed
out and focuses the empty, required username field. We seed a stale button name
(`Log In` instead of `Sign In`) to demonstrate locator repair. No credentials are
entered and no banking transactions are made.

### Run the full example

With Go 1.25.13 or newer and Node 24 installed:

```sh
git clone https://github.com/Quality-Max/9lives-runner.git
cd 9lives-runner
npm ci
npm exec playwright install chromium
npm run demo:bank
```

On Linux, use `npm exec playwright install --with-deps chromium` when installing
the browser. The commands are the same in a macOS, Linux or Windows shell;
this particular bank replay was executed on macOS. The target is a live public
site, so network access is required and future UI changes may change the result.

The script builds the Go CLI and works in a temporary copy under `.context/`.
It stops on any unexpected result and removes its temporary files when finished.
It exports browser captures and the checked result summary into `demo/` only
after verification succeeds. It does not change the checked-in test.

### What happens

1. **Draft:** load the [agent-authored Playwright test](bank/tests/bank.spec.ts)
   and capture its source with `9l provenance`. The replay uses the checked-in
   draft; it does not generate a new test or call a model. The declared agent
   name does not authenticate authorship.
2. **Assess:** run `9l assess` with the [requirement contract](bank/requirements.json).
   It flags the seeded fixed wait. The script explicitly removes that line;
   assessment is advisory and does not edit the test.
3. **Run:** execute the test through `9l run`. The deliberately stale `Log In`
   button locator fails.
4. **Heal:** `9l heal --provider none` re-runs the original and verifies an
   offline candidate in isolation. The script checks that the candidate changes
   exactly one literal, from `Log In` to `Sign In`, with every other byte intact.
5. **Verify:** require one executed, passing test and zero provider calls. Check
   that the candidate is saved separately and the original remains unchanged.

The test checks the bank title, required-field validity, focus on the username,
the remaining sign-in prompt and the empty username value. A passing rerun
establishes those checks passed. It does not establish complete coverage of the
bank, authentication security, or other banking workflows.

### Read the GIF

[Open the GIF](demo-bank.gif) · [Still image](demo-bank.png) ·
[Checked result summary](demo-bank-evidence.json)

This is an edited replay of actual browser captures and checked CLI results,
not a continuous terminal recording. The nine frames last 68.5 seconds, with
32 px code text on a 1280 × 880 canvas. Browser images are close-up captures of
the actual form. The public credential hint is removed before screenshots or
failure snapshots. No credentials, cookies or session data are published.

The example shows one seeded locator failure, not an observed application bug
or a comparative speed benchmark. Native healing is experimental. See the
[healing contract](../docs/native-tier2.md) for the supported edits and limits.

### Rebuild the GIF

First run `npm run demo:bank` to refresh the checked evidence and captures.
Install Pillow and Python Playwright in your own Python environment, then:

```sh
python -m playwright install chromium
python demo/render-workflow.py
```

The renderer checks the evidence before producing `demo-bank.gif` and
`demo-bank.png`. It also checks every frame for clipped text and saves frame
previews under `.context/demo-gif-preview/`. It never executes tests or calls a
model. The [HTML storyboard](workflow-illustration.html) can be opened locally;
use the left/right arrow keys to inspect its frames.

The legacy terminal recording below remains available as a separate SDK example.

## Local checkout demo

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

### Recording

The [original checkout animation](9l-demo.gif) is generated by
[`recording/record.tape`](recording/record.tape) with
[VHS](https://github.com/charmbracelet/vhs). From the repository root, after
`npm ci` and installing Chromium, run `vhs demo/recording/record.tape`. It
builds the CLI and copies the checkout fixture into `/tmp/9l-demo`; the
planted defect is the fixture's `missing-order` control from
[checkout qualification](../docs/test-assessment.md#checkout-qualification).
