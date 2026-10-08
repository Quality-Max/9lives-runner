# Changelog

## Unreleased

- Consumer installation, checkout demo, documentation index, contribution and
  security guides.
- CLI release gate with full CI qualification, native version/startup checks,
  four platform archives, license files and SHA-256 checksums.
- CLI source version aligned to 0.0.0; release builds embed the validated tag.
- `9l assess` no longer requires `--requirements`; code-level checks run
  without a contract and requirement mapping is reported as not run (#5).
- Suite-level `test.skip(condition, reason)`/`test.fixme` modifiers are no
  longer parsed as tests; conditional ones are reported as informational
  `conditional-skip`, unconditional ones keep `disabled-test` (#7). Calls that
  could be declarations, such as `test.skip(name, run)`, stay disabled tests;
  modifiers in after hooks are informational; a modifier shared by several
  tests counts once toward analysis limits and the text summary. An empty
  `--requirements` value is a usage error.
- Every assessment finding carries `code`; report version 3, policy
  `assessment-source-v4` (#10).
- Assessment text output omits empty requirement/outcome fields, ends with a
  per-rule summary and shows literal test titles with opt-in `--titles` (#11).
- `@9l/playwright` peer range is `@playwright/test` `>=1.61.1 <2`; 1.61.1
  remains the only qualified version (#9).

## SDK 0.0.0 — 2026-10-08

- First public `@9l/playwright` npm publication under Apache-2.0.
- Opt-in `n9l` fixtures, named steps and bounded goal contracts through
  `9l.engine/1`, with ordinary Playwright assertions retained.
- Qualified packed-artifact checkout success, business failure and ordinary
  Playwright execution. SDK smoke also covers worker/browser timeout and cancel.
- Independent behavioral verification, verified replay and native mobile
  support remain planned; live provider accuracy and cost remain unqualified.
