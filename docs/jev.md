# Optional Jev foundation

QUA-2228 adds an internal Go decision client and evidence contract, `9l.jev/1`.
It supports Noul, Choice, fractional Score and Box responses. It shares the
bounded HTTP transport with native Tier 2 providers and uses the runner's
attempt identity and cancellation context. Ordinary Playwright runs remain
provider-free.

The client is infrastructure for subsequent diagnosis, locator repair, semantic
checks and goal adapters. Those integrations are not enabled by this change.
There is no new SDK method or `run --provider jev` option. A schema-valid answer
is an inference; it cannot create a passing execution receipt or execute an
action. Live accuracy and model availability have not been qualified here.

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
the visual report. Offline mode displays no simulated model answer.

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
at most eight questions, 16 KiB of snapshot text, 24 MiB of encoded request and
1 MiB of response. Request, question and byte reservations are atomic across
concurrent calls, include failures and are never refunded. Retries and redirects
are disabled. HTTP failures expose only a numeric status. Missing credentials,
invalid evidence/schema, exhausted budgets, cancellation and stale results
return no answers, with a truthful status rather than fabricated success.

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
