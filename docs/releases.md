# Releasing 9lives

The Go CLI and `@9l/playwright` are separate distributions; installing the SDK
does not install the CLI. From 0.2.0 they share one version and are released
together by one `v<version>` tag.

The SDK 0.0.0 publication is complete. Its exact CI-qualified tarball was
published manually with npm two-factor authentication, and verified through
anonymous registry download and a clean Chromium consumer run. That bootstrap
publication has no GitHub Actions provenance. Its source commit is
`4cbe5fc7b6402e0b296c2903adb667e92286f62d`. Do not republish 0.0.0.

## Release the CLI and SDK

Releases use stable `v<major.minor.patch>` tags. In one reviewed change, set
the version in `cmd/9l/main.go` (`var version`), `packages/playwright/package.json`
and the `@9l/playwright` dependency of `testdata/sdk/package.json`, regenerate
`package-lock.json` with `npm install --package-lock-only`, and update the docs
and changelog. `npm test` fails if those declarations differ. Merge it to
`main` and ensure CI passes before tagging the reviewed main commit:

```sh
git fetch origin
git tag v0.3.0 origin/main
git push origin v0.3.0
```

The commands above release CLI and SDK 0.3.0; choose the matching version for
a later release. Never move a published tag to different source. The tag
starts one workflow, `release.yml`, which runs in this order:

1. `check-tag` rejects a private repository, a noncanonical repository, a tag
   outside main's history, or a tag that differs from any declared CLI or SDK
   version.
2. `qualification` runs the entire reusable CI workflow once; its `sdk` job
   packs and qualifies the SDK tarball.
3. `binaries` builds and exercises the six CLI archives on native runners.
4. `github-release` validates the archives, writes `SHA256SUMS` and publishes
   the GitHub Release.
5. `npm` publishes the SDK tarball from step 2 (see
   [trusted publications](#subsequent-trusted-publications)).

The SDK therefore never reaches npm without its CLI release. If a job fails,
rerun the failed jobs of the same run: published GitHub assets are not
overwritten, and the `npm` job skips when npm already holds the identical
tarball (it fails if npm has that version with different contents).

Each executable is built and exercised on its native macOS/Linux/Windows amd64/arm64
runner. The build embeds the validated tag version, checks `--version`, startup
and fixture planning, and packages the executable with the root `LICENSE` and
`NOTICE` and `THIRD-PARTY-NOTICES.txt` (Go dependencies). Release artifact names are separate from CI's raw binaries and npm
package, so only the six intended CLI archives reach the release.

Before upload, archive validation checks exact member paths, regular file
and executable permissions, and byte-identical legal files. It writes
`SHA256SUMS` for all six archives. Windows uses ZIPs with `9l.exe`; macOS
and Linux use tarballs with executable permissions. Native Windows CI must
pass checkout assertions, business failure, owned timeout/cancel and clean
consumer checks before release packaging. Windows arm64 has no Go race
detector; its Go tests run without `-race`. Windows ZIPs start with
CLI v0.2.0. Only the final publishing job has repository
write permission; its actions are pinned to commit SHAs. Archives are not
Apple notarized.
Published assets are not overwritten by a workflow rerun.

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
npm run smoke:prove
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

The package's npm trusted publisher must name GitHub owner `Quality-Max`,
repository `9lives-runner` and workflow `release.yml`, with both the publish
and stage publish permissions. Through SDK 0.1.3 it named `npm-release.yml`,
which no longer exists. Before the first release from `release.yml`, an npm
owner of `@9l/playwright` moves it (npm 11.5.1 or newer):

```sh
npm trust list @9l/playwright
npm trust revoke @9l/playwright --id=<id of the npm-release.yml entry>
npm trust github @9l/playwright --file release.yml --repository Quality-Max/9lives-runner --allow-publish --allow-stage-publish
```

Without it the `npm` job fails with `ENEEDAUTH` after the GitHub Release is
published; fix the publisher and rerun that job. SDK 0.1.0 was the first
automated publication: tag `sdk-v0.1.0` on `48f9cf3`, with npm provenance.

The workflow runs a direct `npm publish`, so the publisher needs the publish
permission; a stage-only publisher fails with `403 OIDC permission denied`, and
a missing publisher fails with `ENEEDAUTH`. npm accepts one publisher per
matching workflow, so changing permissions means revoking the existing entry
first (`npm trust list`, then `npm trust revoke --id=<id>`) and creating it
again with `--allow-publish --allow-stage-publish`. After a configuration fix,
rerun the failed `npm` job; the qualified tarball artifact is reused.

Publication runs from the `v<version>` tag that also releases the CLI; it no
longer runs on a push to `main`, and no `sdk-v` tags are created. It publishes
the exact tarball the run's CI qualified, using short-lived OIDC
authentication on a GitHub-hosted runner. Public trusted publication
automatically receives npm provenance. SDK 0.1.0 through 0.1.3 were published
from `sdk-v` tags or pushes to `main`; SDK 0.2.0 is the first published from a
shared `v` tag.

The `sdk-release-tag` deploy key and its Actions secret `SDK_RELEASE_TAG_KEY`
only pushed `sdk-v` tags and are no longer used. Remove them:

```sh
gh repo deploy-key list
gh repo deploy-key delete <sdk-release-tag-key-id>
gh secret delete SDK_RELEASE_TAG_KEY
```

Never push a `v<version>` tag for a version npm already has from other source:
npm rejects republishing a version, so the `npm` job fails after the CLI was
released. Binary archives include the root LICENSE and NOTICE.
