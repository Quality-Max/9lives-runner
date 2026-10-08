# Releasing 9lives

The initial SDK version is **0.0.0**. The Go CLI and `@9l/playwright` are
separate distributions; installing the SDK does not install the CLI.

The SDK 0.0.0 publication is complete. Its exact CI-qualified tarball was
published manually with npm two-factor authentication, and verified through
anonymous registry download and a clean Chromium consumer run. That bootstrap
publication has no GitHub Actions provenance. Its source commit is
`4cbe5fc7b6402e0b296c2903adb667e92286f62d`. Do not republish 0.0.0.

## Release the Go CLI

CLI releases use stable `v<major.minor.patch>` tags. Set `var version` in
`cmd/9l/main.go` to the intended version in a reviewed change; the initial
version is 0.0.0. Merge it to `main` and ensure CI passes before tagging the
reviewed main commit:

```sh
git fetch origin
git tag v0.0.0 origin/main
git push origin v0.0.0
```

Run those commands only for the first CLI release, after the release workflow
changes have merged. Never move a published tag to different source.
The workflow rejects a private repository, a mismatched source version,
noncanonical repository or a tag outside main's history. It runs the entire
reusable CI workflow before building release executables.

Each executable is built and exercised on its native macOS/Linux amd64/arm64
runner. The build embeds the validated tag version, checks `--version`, startup
and fixture planning, and packages the executable with the root `LICENSE` and
`NOTICE`. Release artifact names are separate from CI's raw binaries and npm
package, so only the four intended CLI archives reach the release.

Before upload, archive validation checks exact member paths, regular file
and executable permissions, and byte-identical legal files. It writes
`SHA256SUMS` for all four archives. Only the final publishing job has repository
write permission; its actions are pinned to commit SHAs. The release workflow
publishes GitHub assets, not the npm SDK. Archives are not Apple notarized.

After publication, download the matching native archive and checksum file,
verify them using the [installation guide](install.md), inspect `9l --version`,
and execute the checkout demo with the downloaded binary. Cross-compilation
alone is not native runtime qualification. If a release is defective, stop
recommending it and publish a fixed version; do not silently replace its bytes
or reuse a version number.

## Qualify the SDK

From a clean checkout:

```sh
npm ci
npm test
npm exec playwright install chromium
npm run smoke
npm run smoke:assessment
npm run smoke:package
```

The last command installs the packed SDK in an isolated consumer, exercises
real checkout assertions, rejects a missing-order defect and runs an ordinary
Playwright spec. Only a successful qualification writes the tarball and its
SHA-256 record to `.context/npm-pack/`. The SDK smoke separately verifies
worker and browser cleanup on timeout and cancellation.

Run the Go race tests and vet as well. CI requires the pinned Python
compatibility sources and validators; missing local fixtures are skipped
checks, not passes.

## Bootstrap npm publication (completed)

The first SDK 0.0.0 artifact was published manually after full qualification.
For future bootstrap situations an authorized npm account must authenticate
locally, satisfy npm's two-factor requirements and publish only the qualified
tarball. Keep credentials out of repository files and agent output. Local
publication does not receive GitHub Actions provenance.

## Subsequent trusted publications

Before the next automated SDK release, configure the package's npm trusted
publisher for GitHub owner `Quality-Max`,
repository `9lives-runner`, and workflow `npm-release.yml`. Enable direct
publishing in npm's publisher settings; a publisher restricted to staging
does not authorize the workflow's direct `npm publish` command.

For a future release, update the SDK version and lockfile in a reviewed change,
merge it to `main`, and push a matching `sdk-v<version>` tag. The workflow
rejects private repositories, mismatched versions and commits outside main's
history. It runs the complete CI workflow, then publishes the exact qualified
tarball using short-lived OIDC authentication on a GitHub-hosted runner.
Public trusted publication automatically receives npm provenance.

Do not push `sdk-v0.0.0`: npm will reject that
duplicate version. Go binary releases use separate `v*` tags and include the
root LICENSE and NOTICE in their archives.
