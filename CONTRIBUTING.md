# Contributing

Use [GitHub Issues](https://github.com/Quality-Max/9lives-runner/issues) for
non-sensitive bugs and proposals. Include the CLI/SDK versions, platform,
expected behavior and a minimal reproduction. Sanitize receipts before
sharing; omit credentials, page content and private application data.
Security reports belong in the [private reporting channel](SECURITY.md).

Work from current `main` on a focused branch. Preserve existing Playwright
specs, configuration, test intent and assertions. Keep Go scheduling and
receipt ownership separate from TypeScript browser fixtures. Read
[AGENTS.md](AGENTS.md) for the engineering and secret-handling requirements.

Follow the [development guide](docs/development.md). For Go changes run
`gofmt`, focused tests, `go test -race ./...` and `go vet ./...`. For SDK changes
run the Node suite and real Chromium smoke, including business failure and
owned timeout/cancel controls. CI requires pinned Python fixtures; report
missing local checks as skipped. Review the final diff for correctness,
privacy, cleanup and completeness.

Describe the concrete before/after behavior and actual validation in your PR.
Keep private evidence in `.context/`. Do not publish npm packages as part of
routine contributions; releases follow the [release runbook](docs/releases.md).
Contributions use this repository's [Apache-2.0 license](LICENSE).
