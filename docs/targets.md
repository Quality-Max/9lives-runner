# Shared target contract (experimental)

QUA-2203 / Demo M1 adds an opt-in deterministic surface shared by a web
worker and a **fake native worker**. The fake runs in memory. The optional M2
`android:<app-alias>` adapter uses a real local emulator or explicit experimental
physical-device attachment; see
[Android setup and qualification](android-driver.md). The fake fixture does not
qualify Android, iOS, semantic screen checks, independent verification or replay.
Existing specs and Playwright configuration keep their existing behavior.

## Run the local contract fixture

From a source checkout after `npm ci`, `npm run build` and
`npm exec playwright install chromium`:

```sh
go run ./cmd/9l run testdata/sdk/targets/shared.spec.ts --sdk \
  --config testdata/sdk/targets.config.ts \
  --target web:finance --target android-fake:finance \
  --grep 'one deterministic scenario' --format json
npm run smoke:targets
```

Each target is also a project name in the selected Playwright configuration.
The part after `:` is a public app alias of 1–64 letters, digits, `_` or `-`;
never put a URL, device serial, credential or user data there. Targets must
be distinct, require `--sdk`, and cannot be combined with `--project`.
Go reserves one job per spec/target within the existing job, parallelism,
output and time budgets. A budget that omits a target makes the run incomplete.
Target workers have one attempt, even when Go or Playwright retries are configured.
User-authored loops and retries inside lifecycle/driver functions remain the
author's responsibility; drivers must never retry mutations.

Every input spec runs on every target, so pass only target specs. An ordinary
test without a target assertion, or a project dependency such as a setup project,
makes the attempt invalid. A target test may skip itself (for example on one
platform); as with any SDK skip, the run is complete only with a matching
`--pin-skip "<file> › <title>"`. Target tests cannot use `test.fail()`.

## API

`createTargetTest({web, native})` returns a Playwright test with an `app` fixture.
The fixture exposes `open({timeoutMs}?)`, `reset({timeoutMs}?)`, `cancel()` and
`getByTestId(id)`. Observed controls expose `tap({timeoutMs}?)`,
`fill(value, {timeoutMs}?)`, `toBeVisible(timeoutMs?)`,
`toHaveText(exactText, timeoutMs?)` and
`toHaveState('enabled' | 'checked', boolean, timeoutMs?)`.
Specs load without 9l, so `playwright test --list` and editors can discover
them; using `app` requires `9l run --sdk --target`.
See [the shared fixture](../testdata/sdk/targets/shared.spec.ts) for one scenario
written once, and [the fake driver](../testdata/sdk/targets/fake-finance.ts) for
the worker-side contract.

The web lifecycle provides `open(page, context)` and `reset(page, context)`;
ordinary Playwright owns the page. `native(target)` synchronously constructs a
`TargetDriver`; resource acquisition belongs in its bounded `open(context)`.
Native fixture resolution has no implicit browser, context or page dependency.
`page` and ordinary `expect` remain available to existing web tests.

Drivers declare `9l.target/1`, the selected kind/app alias, and the exact ordered
`targetCapabilities`. They observe at most two handles for a test ID: zero is
missing, two is ambiguous. Mutations require exactly one observed handle and
never silently select the first match. The web driver acts through strict
Playwright locators: a control replaced by re-rendering is followed, and a
second match at action time fails instead of being chosen. Only test-ID lookup is supported; CSS,
XPath, coordinates and role lookup have no API in this contract. Unsupported
states, driver versions and capabilities fail the test.

An operation receives an abort signal and absolute deadline. Operations and
assertions default to 5 seconds and accept up to 60 seconds, always capped
by Go's worker deadline. Android open defaults to 180 seconds and accepts up to
180 seconds. Assertions make at most 200 observations. At their own polling
bound, a completed mismatch reports `assertion_failed`, even if a later read
stalls. A bound reached before any completed check reports `deadline`;
cancellation and Go's outer deadline retain their own reason. If every completed
read fails, its operation error is preserved rather than claiming a false
business comparison. There is one operation in flight per app.

Any failure permanently stops the session; catching the error cannot make the
receipt pass. Errors name the reason (`missing_test_id`, `ambiguous_test_id`,
`assertion_failed`, `deadline`, `canceled`...) and keep the local cause. When
an operation fails by itself, the driver stays open until fixture teardown, so
Playwright can still capture failure screenshots, traces and error context.
An operation is aborted and fails at its deadline, so a late result never
succeeds. Cancellation or a concurrent call closes the driver immediately; an
operation still pending 250 ms after its deadline also closes it to cancel
outstanding work. Driver
`close()` must cancel outstanding transport work and release owned resources.
Go also terminates its owned worker tree on timeout/cancel. Cleanup remains
bounded by that outer process deadline if a driver fails to settle its own
close promise.

## Evidence and limits

The existing `9l.engine/1` stream negotiates the additional `9l.target/1`
capability. Every frame carries the selected target's SHA-256 ID (a fixed
digest of the public target name, not a secret) alongside run/job/attempt
identity. Before execution, the reporter declares each test ID it will run; Go
checks every frame against that list and requires a single test attempt,
unique step IDs, terminal test results and a consistent final outcome. The
list comes from the same worker, so it detects inconsistent evidence, not
tests the worker chose to omit. Missing, foreign, duplicate, stale, malformed
and truncated evidence cannot pass.

Typed target steps record only the operation enum, category and outcome. The
reporter counts only steps created by the SDK's own target module; a user step
titled `9l target ...` is an ordinary step. Passed target tests require at
least one successful target assertion and no failed target operation.
`targetAssertions` counts these worker-reported checks; `verifiedAssertions`
stays 0 and `assertionCoverage` stays `unknown`. This trusts the test worker
and driver: code in the worker can still forge a typed step deliberately. It is
not independent behavioral proof or proof that every business requirement was
asserted.

Existing size limits and closed schemas apply. IDs and enums cross the bridge;
lookup IDs, fill values, expected/actual text, raw errors, page content and
attachment bodies do not. Explicit existing opt-ins such as `--failure-details`
and `--keep-attachments` retain their documented privacy implications.

The smoke checks a shared passing scenario, a planted wrong finance result
that fails the exact-text check, missing/ambiguous controls and their reason
codes, incompatible drivers, a caught failure, a forged step title,
cancellation, a worker-side operation timeout, a driver that ignores the
deadline and Go's deadline during a pending mutation, unpinned and pinned skips, a re-rendered web control,
one-attempt behavior, plain `--list` discovery and ordinary Playwright
compatibility. Bounded receipts
are saved under `.context/target-contract/`. Creation provenance currently
supports one job: single-target controls use it; the two-target baseline does
not claim runtime provenance. Source assessment does not yet recognize
`createTargetTest` declarations, so static assertion/requirement coverage is
unknown for this fixture.
