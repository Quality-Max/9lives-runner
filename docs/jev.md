# Optional Jev foundation

QUA-2228 adds an internal Go decision client and evidence contract, `9l.jev/1`.
It supports Noul, Choice, fractional Score and Box responses. It shares the
bounded HTTP transport with native Tier 2 providers and uses the runner's
attempt identity and cancellation context. Ordinary Playwright runs remain
provider-free.

The client is infrastructure for diagnosis, locator repair, semantic checks and
goal adapters. Failed-step diagnosis (QUA-2229) is available as `9l diagnose` and grounded
locator repair (QUA-2230) as `9l heal --jev`, and goal decisions (QUA-2232) as
`9l run --sdk --goal-provider jev`. Visual semantic checks are separate work.
There is no new SDK method or `run --provider jev` option. A schema-valid answer
is an inference; by itself it cannot create a passing execution receipt or
execute an action. Live accuracy and model availability have not been qualified here.

## Diagnose a failed step

```sh
9l diagnose tests/checkout.spec.ts                      # observed facts only
9l diagnose tests/checkout.spec.ts --jev                # plus one Jev request on failure
9l diagnose tests/checkout.spec.ts --jev --jev-images synthetic
```

`9l diagnose` runs the spec once through the normal runner. A passing test is
not diagnosed. On failure it reports **observed facts** from the receipt: the
runner's failure class, the native deterministic class, the failed locator,
hashes of the attached accessible snapshot and screenshot, and a comparison
for supported Playwright assertions. With `--jev` and an accessible snapshot
or accepted screenshot, it makes at most one request within a minute of the
run ending, bound to that attempt and the exact source that ran. Evidence age
is measured from job completion, so a spec that keeps running after a failure
can send older evidence.

Jev answers typed questions only: a choice of failure family (locator drift,
obstructing overlay, transient state, wrong page, application defect or
unresolved) and independent yes/no observations (overlay, loading, visible
error and, for a named locator on a non-assertion failure, target presence).
Observed page text is sent as data, never as instructions or criteria. Go maps
the answers to a disposition: `eligible-for-repair`, `investigate-test`, `investigate-app`,
`require-new-observation` or `unresolved`. A family needs confidence and
probability of at least 0.6; contradictory observations, such as drift with an
absent target, are unresolved. Missing or stale evidence, provider failure and
malformed answers are explicit `unresolved` or `error` states. A supported
assertion mismatch routes to `investigate-app`, meaning the observed behavior
and the test expectation need review. When the inferred family is transient
and the page shows strong evidence of loading, the disposition is
`require-new-observation`. A comparison alone routes locally with `--jev`.
Without a supported comparison, accessible snapshot, or accepted screenshot,
diagnosis returns `insufficient_evidence`.

An assertion failure, such as a wrong price, quantity or status, is never
eligible for repair, even when the page looks plausible or Jev suggests drift.
The diagnosis never changes the receipt, retries, dismisses an overlay or acts
on the application. As with `9l run`, the command exits 0 when the test passed,
1 when a test failed, 2 for a usage error and 3 when the run did not complete
(timed out, canceled or without valid evidence). Without `--jev`, or when Jev
is unavailable, the observed facts are still reported. `--jev` refuses
`--pass-env TYPESAFE_API_KEY`: the credential stays in the `9l` process.

By default the request uses a redacted accessible snapshot, including the
matcher snapshot where Playwright attaches one. For assertion failures,
supported comparisons (`toBe`, `toHaveText`, `toHaveCount`, `toBeVisible`
and `toBeHidden`) carry the matcher, value kind and mismatch. Boolean values,
visibility states and element counts can carry the expected and observed
values. Arbitrary text and scalar numbers stay out of the comparison. The
request omits raw assertion errors and selectors. Other failures send the
bounded error and locator details.
`--jev-images synthetic` also sends the failure screenshot; use it only when the
screen shows test data you are allowed to upload. Screenshots require
`screenshot: 'only-on-failure'` in the Playwright config. The record is saved as
`diagnosis.json` beside the run's receipts in `.9lives/diagnoses/`. The
offline tests establish wiring and policy only; diagnosis quality is measured
separately (QUA-2233).

## Grounded locator repair

```sh
9l heal tests/checkout.spec.ts --jev            # save a verified candidate as .healed
9l heal tests/checkout.spec.ts --jev --yes      # apply it after verification
```

`--jev` adds one optional stage to native healing, after offline Tier 1 and
before any Tier 2 provider. It runs only for a failed direct
`getByRole(...)` or `getByLabel(...)` action whose accessible name no longer
matches, when Tier 1 could not pick a single renamed control. The candidates are
the distinct accessible names of the same role (or labelled controls) in the
failure's ARIA snapshot; names shared by several controls are left out. Jev
uses the old locator's name, candidate labels and accessible page context to
compare each candidate's function with the intended action, destination or
input purpose. When the test immediately follows a failed click by filling
a field with a literal label, Jev can also use that label to infer the
intended form. The fill value stays in the test. Jev answers three typed
questions covering the best candidate (or none), whether any candidate serves
the intended function, and whether the evidence supports multiple candidates
about equally well. Questions refer to candidates by position, with labels
kept in observation data. A proposal needs presence of at least 0.7,
ambiguity of at most 0.3, and choice confidence and probability of at least
0.7.

Go builds the candidate by replacing only the failed locator's name literal.
The role, `exact` flag, imports, assertions, comments and other actions stay
byte-identical, and the session's existing exact-literal check rejects anything
else before running. The candidate then runs in a fresh isolated copy with its
own receipt. Only a passing run with the original test count is verified. A
wrong choice that reaches the test's assertion stays failing (`needs_human`),
so real application defects and wrong business outcomes are never repaired.
Without `--yes` a verified candidate is saved as `<spec>.healed`; with `--yes`
it is applied after the existing file-hash concurrency check. The session JSON
records the stage as `groundedBy`, `groundedDecision` (Jev's answers and the
candidate list) and `grounded` (the verification run).

The stage abstains for assertion failures, CSS or text locators, missing or
stale evidence (older than a minute; it uses the original run's evidence),
duplicate or missing candidates, a locator inside an assertion, and unclear
answers. Jev Box localization is not used: Playwright's failure evidence carries
no control geometry to match a box against. `--jev-images synthetic` also sends
the failure screenshot of test-only data. As with `9l diagnose --jev`,
`--pass-env TYPESAFE_API_KEY` is refused before anything runs: the credential
stays in the `9l` process and never reaches the original or verification run.

## Bounded goal decisions

```sh
9l run tests/signup.spec.ts --sdk --goal-provider jev [--goal-model jev-1.14.0]
```

Jev is an experimental provider for the existing `n9l.goal` engine. The engine
still builds the finite observed controls, and keeps every candidate limit,
action/decision/token/time budget, cancellation, the destructive/outreach label
policy, duplicate-control stops and unknown-effect handling. Jev answers one
typed choice per decision among engine-built options: each allowed action on
each observed control, plus `wait`, `complete` and `unresolved`. Options are
referred to by id; observed labels and state stay in the observation data. A
choice needs confidence and probability of at least 0.7, otherwise the
decision is `unresolved`. For `fill` or `select` with several named parameters,
a second typed question picks which parameter name; values never leave the
browser worker. Jev never produces selectors, URLs, code or input values.

The client is bound to each attempt (run, job, attempt) and to a digest of the
planned specs' bytes, and is closed with the attempt. `complete` stays
completed-but-unverified: the test's own assertions decide the result, and a
goal that completes against a wrong business outcome still fails. The receipt
records `jev` as the goal provider and each decision as usual, with the Jev
requests it made (`requests`: one or two, zero when the engine-built options
or observation did not fit a request) and the model Jev reported (`model`).
Cost is not estimated; use the engine's decision and token budgets.

## Qualification

`testdata/jev-corpus` is the frozen, labeled corpus (33 synthetic cases across
diagnosis, repair and goals, half held out). `npm run qualify:jev -- --mode
validate` proves the labels with real Chromium runs, including the goal cases
that must stop, through deterministic probes; `--mode offline` records the
baseline without Jev; `--mode jev` is the only paid mode and needs
`--confirm-paid` and a whole-number request cap. A CLI error, an incomplete run
or missing execution evidence fails the run as unusable instead of being
scored. See `testdata/jev-corpus/README.md`.

Offline baseline (20 November 2026, darwin): diagnosis reports facts only, so
all 16 diagnosis cases are `unresolved`. Offline Tier 1 repair verifies 1 of 7
repairable renames (recovery 14%, precision 100%). Goals need a provider and are
not run. There were zero false greens. These are policy and wiring results,
not Jev quality measurements; live results are recorded with model, date and
request counts when they are run.

See the [0.8.0 locator repair validation report](reviews/jev-locator-repair-0.8.0.md)
for the November 2026 held-out rerun and campaign check.

## Configuration and API key

Construct `jev.Config` explicitly in Go: `Enabled` defaults to false, `Model`
defaults to pinned `jev-1.14.0`, and `Images` defaults to `off`. Merely setting
`TYPESAFE_API_KEY` never enables the provider. An enabled decision reads only
that credential, after local evidence and contract validation, and sends it in
the Authorization header to the fixed TypeSafe endpoint. Keep it in the Go
process. `Client.CredentialNames` identifies it for worker-environment
withholding when adapters are added; never pass it to browser workers.

An API key is needed for the first real enabled decision request. Development,
offline tests, disabled execution and local preflight need no key. Supply real
credentials through your secret manager or local process environment; never put
them in source, command arguments, screenshots or checked-in configuration.

For an executable illustration, run `go run ./demo/jev` from the repository
root after installing Playwright and Chromium. Add `--live` for one real request
and `--images` to include only its synthetic screenshot. The
[local demo guide](../demo/jev/README.md) explains setup, credential entry and
the visual report. `npm run demo:jev` opens the interactive shop and a four-level
picture-hunt game. Its fixed “Watch 9lives play” example runs an ordinary
Playwright spec through the existing runner; optional Jev choices must pass an
independent synthetic answer-map check before any click, followed by real score
assertions. This is not the general goal adapter. Offline mode displays no
simulated model answer.

These commands perform local configuration checks without contacting TypeSafe:

```sh
9l doctor --jev --format json
9l doctor --jev --jev-model jev-1.14.0 --jev-images masked --format json
9l doctor --jev=false --format json
```

`--jev` checks credential presence and returns exit 2 for `missing_credential`;
`configured` and `disabled` return 0. Reports contain only status, model, image
policy and a presence boolean. `connectivity: "not_checked"` means availability,
authentication and model support remain unverified. This preflight does not
require Node, load project configuration or save settings for future runs.
The model/image flags require an explicit `--jev` flag; SDK diagnostics run
separately with `9l doctor --sdk`.

## Caller and evidence contract

Create one client per owning attempt with `jev.New`, the existing
`runner.AttemptIdentity`, a Git revision, exact source SHA-256, limits and an
attempt context. Reuse it for every decision in that attempt and call `Close`
on completion. Child callers may add cancellation but cannot extend its
deadline. The engine remains responsible for overall scheduling and limits.

`Decide` takes an expected engine-owned `Binding`, an `Observation` and a
versioned `Contract`. Bindings include run/job/attempt, target, revision, exact
source hash, test, step and observation IDs. The observation must match that
binding exactly, contain a bounded accessibility snapshot and capture time,
and be fresh before the request and after inference. Applicable failures are
categorical runner failure classes, never raw errors. The expected binding must
come from the engine's current observation, not from page text or the provider.

The contract supplies task, question and criteria versions, plus finite questions
and owned candidate descriptions. State contains observed text as data;
it cannot set the model, questions, image policy, credentials or request options.
There is no tool, code or executable action field. Choice answers must name
owned candidates and return the exact probability keys. Scores use ordered
string levels and fractional weighted values, with matching legends. Responses
reject duplicate/null fields, unknown properties, excess nesting, missing or
foreign answers and non-finite/out-of-range probabilities. Box coordinates must
be ordered, finite and normalized to 0..1.

## Images and coordinate mapping

Uploads require explicit `synthetic` or `masked` policy in both client config
and image preparation. `PrepareImage` copies and re-encodes upright still
PNG/JPEG input, removing metadata. It rejects corrupt, animated PNG,
unsupported, oversized and out-of-dimension input before uploading. Input and
prepared bytes each have a 16 MiB cap; each dimension must be 280..1568 pixels.
There is no automatic resizing. The outgoing asset is PNG and every image/Box
request includes `TypeSafe-Danger-Zone-Images: true`.

`synthetic` is an explicit caller assertion that pixels contain no real data.
`masked` requires 1..64 explicit rectangles in uploaded-image pixels, which are
painted opaque black. Callers must identify all sensitive regions. Accessibility
text receives the runner's best-effort redaction; this does **not** sanitize
pixels or guarantee arbitrary text is safe. Raw snapshots, prompts, answers,
provider error bodies and pixel buffers remain transient in memory.

Preparation requires a finite transform mapping uploaded-image pixels into
viewport CSS coordinates:

```text
xCSS = xPixel * scaleX + offsetX
yCSS = yPixel * scaleY + offsetY
```

For a normalized Box, first multiply horizontal edges by uploaded width and
vertical edges by uploaded height, then apply that transform. Callers that
crop/resize beforehand must provide the resulting exact mapping. Image metadata
retains the transform, capture time, binding, prepared content hash, dimensions,
bytes, policy and mask count. Image and accessibility evidence cannot be mixed
across bindings or capture times. Box always returns a location even when the
target is absent; callers must establish presence/ambiguity and verify an
observed target before any future action or isolated repair.

## Bounds, failures and persistence

Default attempt limits are four requests, 32 total questions, 32 MiB outgoing
JSON, 60 seconds total lifetime and 30 seconds evidence age. Each call accepts
at most nine questions, 16 KiB of snapshot text, 24 MiB of encoded request and
1 MiB of response. Request, question and byte reservations are atomic across
concurrent calls, include failures and are never refunded. A call that
alone exceeds the question or byte budget reports `request_too_large`; other
refusals report `budget_exhausted`. Retries and redirects are disabled. HTTP
failures expose only a numeric status. Missing credentials, invalid
evidence/schema, exhausted budgets, cancellation and stale results return no
answers, with a truthful status rather than fabricated success.

Persist only `Result.Record`. It records versions, binding, status, observed
time, redacted snapshot hash, optional image metadata, reserved request/question/
byte counts, latency, configured and actual model, and available provider token
usage. `Usage.Available` distinguishes missing usage from zero usage.
`CostAvailable` stays false; there is no invented token estimate, price or paid
budget guarantee. The documented endpoint has no output-token cap option.
Future adapters must reconcile these calls with their owning engine's budgets.
JSON encoding a `Result`, `Observation` or prepared `Image` excludes raw answers,
snapshots and image bytes. No artifacts or response bodies are written by the
client. Hash references identify content without implying a saved image exists.

## Token usage and estimated savings

Jev-enabled `9l run`, `9l diagnose`, and `9l heal` display the number
of requests that reported usage and their input and output token totals.
With `--format json`, the same data is under `jevUsage`. Saved run results
retain it for `9l result`, `9l run --repeat` totals it across member runs,
and live qualification reports total it across all case runs.

`requestsWithUsage` says how many requests contributed to the token counts.
A failed or incomplete request can lack usage. Estimates cover only reported
input and are omitted when no request reports usage. Money in JSON is in
integer nano-USD (one billionth of a US dollar). The separate provider billing
availability field in individual decision records still describes billing
data returned by the provider.

The estimate prices Jev's reported input tokens at each model's published
standard uncached input rate. TypeSafe charges only for input. The comparison
uses equal input volume and excludes baseline output, caching, batch discounts,
and long-context surcharges. Model tokenization and task outcomes can differ.

| Model | USD per million input tokens |
| --- | ---: |
| [Jev](https://help.typesafe.ai/articles/5187202169-how-much-does-jev-cost) | 0.042 |
| [GPT-6 Luna](https://developers.openai.com/api/docs/models/gpt-6-luna) | 0.10 |
| [Gemini 3.1 Flash-Lite](https://ai.google.dev/gemini-api/docs/pricing#gemini-3.1-flash-lite) | 0.25 |
| [GPT-6 Sol](https://developers.openai.com/api/docs/models/gpt-6-sol) | 2.00 |
| [Claude Opus 5.5](https://platform.claude.com/docs/en/models/opus-5-5/overview) | 4.00 |

Rates checked on 2026-11-27. For cost per successful outcome on a common
workload, use [TokenHunger's costbench](https://github.com/Desperado/token-hunger)
with the same cases and assertions for each provider.

## Validation and upstream references

Offline HTTP fixtures exercise typed requests and responses, auth headers,
masked pixels, image headers, malformed input, evidence attribution, concurrent
budgets, cancellation/timeouts, late results, 401/429/service failures, response
limits and sanitized preflight. These synthetic cases establish the client
contract, not visual accuracy or browser-action qualification.

The implementation follows the [System One API](https://docs.typesafe.ai/api/system-one),
[image limits](https://docs.typesafe.ai/images), [Box](https://docs.typesafe.ai/primitives/box),
[Score](https://docs.typesafe.ai/primitives/score) and
[error handling](https://docs.typesafe.ai/api/errors) documentation reviewed
on 24 October 2026. It reuses the platform's versioned question, owned candidate
and attributable evidence concepts without importing platform orchestration.
