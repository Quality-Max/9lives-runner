# 9lives framework engineering

Build a standalone, goal-driven testing framework whose results are backed by
executed assertions and attributable evidence. Developers should be able to
start locally with ordinary Playwright tests, add bounded natural-language
goals, and replay verified actions without depending on a platform account.

## Architecture and delivery

- Go owns planning, budgets, scheduling, attempt identity, cancellation,
  completeness accounting and execution receipts. TypeScript owns Playwright
  fixtures and browser handles. Keep the engine protocol versioned and bounded.
- Preserve existing Playwright specs and project configuration. New framework
  behavior is opt-in until compatibility and execution evidence justify a
  cutover. The local SDK package is `@9l/playwright`; do not publish it as
  part of routine implementation.
- Reuse the proven execution and receipt core here. Reuse the platform's
  observed-target grounding, action traces, deterministic generation, healing
  contracts and framework integrations through explicit contracts; do not
  duplicate those engines or introduce a platform-account dependency locally.
- Every required test must have a terminal outcome. Missing, foreign, stale,
  duplicate, malformed or truncated evidence cannot produce a passing receipt.
  A process starting, an action completing, or an LLM claiming success does not
  establish the required behavior. Never invent assertion coverage.
- Healing must preserve test intent and assertions, run the candidate in
  isolation and verify the result before application. Cache/replay admission
  requires verified behavior and source/environment provenance; replay must
  recheck the expected outcome and invalidate stale entries.
- Separate working features from planned capabilities. Android execution is
  experimental and qualified only on the emulators and phone documented in
  `docs/android-driver.md`. Independent behavioral verification, verified
  replay, iOS and broader device qualification are subsequent milestones. Claim support only after real runtime qualification;
  deterministic synthetic results are not production benchmark results.
- Natural-language goals use finite observed controls and typed actions; reuse
  the existing Go provider transports. Keep parameters in browser workers and
  provider credentials in Go. Recheck raw stop policy before redaction or
  execution. Cancellation must stop in-flight actions, and neither Go nor
  Playwright retries may repeat goal mutations. Goal completion is unverified.
- Work on one coherent Linear slice at a time. Keep evidence in `.context/`;
  update the ticket with actual validation and remaining work. Do not rename
  the Conductor branch unless the user requests it. Use `origin/main` as base.

## Validation

- Run `gofmt`, focused Go tests, `go test -race ./...` and `go vet ./...` for core
  changes. Offline Python compatibility checks require their pinned fixtures
  and validators; distinguish skipped checks from passed checks.
- For SDK changes run `npm ci`, `npm test`, install Chromium with
  `npm exec playwright install chromium`, then `npm run smoke`. CI installs
  browser system dependencies on Linux. The smoke must verify the successful
  checkout, a real business failure and owned worker/browser timeout/cancel.
- Keep ordinary Playwright execution covered separately. Review the final diff
  for correctness, privacy, cleanup and completeness before committing.
- Test failure modes and observable behavior. Avoid tests that merely reproduce
  implementation details or report green without exercising assertions.

## Releases

- Release only when a maintainer explicitly asks for a release. Implementation
  work never tags, publishes or promotes anything on its own.
- Follow the [release runbook](docs/releases.md#release-runbook) step by step.
  The CLI and `@9l/playwright` share one version; a beta is `X.Y.Z-beta.N`.
- Tag only the merged release-preparation commit on `main`, after its CI (and
  the Android workflow when it ran) passed, with a signed annotated tag:
  `git tag -s v<version> -m "9lives <version>" <sha>`.
- Never move, delete or reuse a pushed tag, never publish to npm by hand and
  never edit a published release's assets. A defective release is replaced
  by a new version.
- On a failed release job, read its log first. Rerun only the failed jobs of
  the same run (`gh run rerun <id> --failed`), and only for a known
  infrastructure flake; any other failure stops the release for a fix and a
  new version.
- A beta never becomes Latest or npm `latest`. A stable release becomes
  Latest only through the workflow's own `latest` job.
- Re-check tags, GitHub releases and npm immediately before any statement
  about what is released, and report each verification as passed, failed or
  not run; a skipped check is never a pass.

## Mandatory secret handling

- Never run, display or capture commands that dump all environment variables,
  configuration, secrets, credentials or deployment logs. This includes raw
  `env`, `printenv`, `set`, `cat .env*`, `railway variables`,
  `railway variables --json`, container inspection and unfiltered CI/provider
  logs.
- Inspect an explicit allowlist of non-sensitive keys only. Filter at the
  source; return presence, type, status or a redacted value, never a secret.
- API keys, tokens, passwords, private keys, database/Redis URLs, webhook and
  signing secrets, service-role keys and encrypted payloads are secrets even
  locally. Never put them in output, source, commits, PRs, tests or docs.
- If a secret enters agent context, stop exposing it, notify the user without
  repeating its value, record credential names only and recommend rotation.
  Do not rotate credentials without authorization.
- Never expose privileged server credentials through `NEXT_PUBLIC_*`, browser
  or client-side environment variables.
- Keep protocol data separate from test logs. Bound and sanitize evidence before
  persistence; do not retain page content, attachment bodies or raw error
  payloads by default. Pass test environment keys explicitly with `--pass-env`.
