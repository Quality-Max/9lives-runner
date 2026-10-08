# Changelog

## CLI unreleased

`9l assess` changes from the second external trial, on the same 87-file suite
and a new helper-function spec.

- A matcher whose `then`/`catch`/`finally` chain is awaited or returned is no
  longer `unawaited-assertion`, and the chained call is no longer counted as a
  second matcher.
- A nested function that returns the matcher, such as `() => expect(...)` in
  a map of checks, is no longer `unawaited-assertion`; it stays a
  `nested-function` limit. A callback passed directly to `forEach` still is.
- Assertions and waits inside a resolved helper keep the helper's own
  location and carry the test's call site as `site` (`via` in text output).
  One wait in a shared helper therefore counts once in the summary, not once
  per calling test.
- `toEqual([])` and `toStrictEqual([])` are absence checks for
  `absence-after-wait`.
- `unmapped-outcome` is checked per file: an outcome mapped by any test in
  the file covers it for every test that references the requirement and maps
  at least one of its outcomes. A test mapping none of them is still reported
  for every outcome. The finding counts once per requirement and outcome.
- Helper facts version 6, policy `assessment-source-v7`; report version 3 gains
  the optional finding `site`.

## SDK 0.1.0 — 2026-10-08

- `@9l/playwright` peer range is `@playwright/test` `>=1.61.1 <2`, so projects
  on later Playwright 1.x releases can install it; 1.61.1 remains the only
  qualified version (#9).

## CLI 0.1.0 — 2026-10-08

`9l assess` changes from an external trial on an 87-file Playwright suite.

- `--requirements` is optional. Code-level checks run without a contract;
  requirement mapping is reported as not run. An empty value is a usage
  error (#5).
- Suites: several specs, a directory or a quoted pattern (`**` supported)
  give one combined report with per-file reports or error codes and a suite
  summary. A file that cannot be assessed does not stop the others; the
  command then exits 2. At most half the CPUs analyze at once, and a file
  whose analyzer timed out is retried once on its own (#6).
- Helpers: same-file helper functions are resolved, attributing their
  assertions, waits and limits to the calling test. `// @9l-assertion-helper`
  on a function or import declaration counts calls as reviewed assertions.
  Imports into other files are not followed yet (#4).
- Skip guards: file- and `describe`-level `test.skip(condition, reason)`/
  `test.fixme` modifiers are no longer parsed as tests. Unconditional ones
  give `disabled-test`; conditional and after-hook ones give informational
  `conditional-skip`; conditions reading `process.env` give suspected
  `environment-skip`. Calls that could be declarations, such as
  `test.skip(name, run)`, stay disabled tests (#7).
- New finding `absence-after-wait` (suspected) when the first assertion after
  a fixed wait only checks that something is absent or did not happen (#8).
- Unawaited `resolves`/`rejects` matchers are flagged; unawaited matchers
  outside the pinned Playwright API give an `unknown-matcher` limit.
- Every finding carries `code` (#10). Text output omits empty fields, ends with
  a per-rule summary and shows literal test titles with opt-in `--titles` (#11).
- Report version 3, policy `assessment-source-v6`. Consumers must accept the
  `informational` classification, the new rules and limit code, an absent
  `requirementsSHA256` and an optional test `title`.

## CLI 0.0.0 — 2026-10-08

- Consumer installation, checkout demo, documentation index, contribution and
  security guides.
- CLI release gate with full CI qualification, native version/startup checks,
  four platform archives, license files and SHA-256 checksums.
- CLI source version aligned to 0.0.0; release builds embed the validated tag.

## SDK 0.0.0 — 2026-10-08

- First public `@9l/playwright` npm publication under Apache-2.0.
- Opt-in `n9l` fixtures, named steps and bounded goal contracts through
  `9l.engine/1`, with ordinary Playwright assertions retained.
- Qualified packed-artifact checkout success, business failure and ordinary
  Playwright execution. SDK smoke also covers worker/browser timeout and cancel.
- Independent behavioral verification, verified replay and native mobile
  support remain planned; live provider accuracy and cost remain unqualified.
