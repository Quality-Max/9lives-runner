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
`cmd/9l/main.go` to the intended version in a reviewed change. Merge it to
`main` and ensure CI passes before tagging the reviewed main commit:

```sh
git fetch origin
git tag v0.2.0 origin/main
git push origin v0.2.0
```

The commands above publish CLI 0.2.0; choose the matching version for a later
release. Never move a published tag to different source.
The workflow rejects a private repository, a mismatched source version,
noncanonical repository or a tag outside main's history. It runs the entire
reusable CI workflow before building release executables.

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
write permission; its actions are pinned to commit SHAs. The release workflow
publishes GitHub assets, not the npm SDK. Archives are not Apple notarized.
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

The package's npm trusted publisher is configured for GitHub owner
`Quality-Max`, repository `9lives-runner` and workflow `npm-release.yml`, with
both the publish and stage publish permissions. SDK 0.1.0 was the first
automated publication: tag `sdk-v0.1.0` on `48f9cf3`, with npm provenance.

The workflow runs a direct `npm publish`, so the publisher needs the publish
permission; a stage-only publisher fails with `403 OIDC permission denied`, and
a missing publisher fails with `ENEEDAUTH`. npm accepts one publisher per
matching workflow, so changing permissions means revoking the existing entry
first (`npm trust list`, then `npm trust revoke --id=<id>`) and creating it
again with `--allow-publish --allow-stage-publish`. After a configuration fix,
rerun the failed publish job; the qualified tarball artifact is reused.

For a future release, update the SDK version, lockfile, fixtures, docs and
changelog in a reviewed change and merge it to `main`. The publish workflow
runs on every push to `main` that touches `packages/playwright/package.json`:
it reads the version, skips when the registry already has it, otherwise runs
the complete CI workflow, publishes the exact qualified tarball using
short-lived OIDC authentication on a GitHub-hosted runner, waits until the
registry serves the new version (up to ten minutes; npm takes a minute or
more), and then pushes the `sdk-v<version>` tag on the published commit. The
run that tag starts sees the version and skips. Pushing a matching
`sdk-v<version>` tag by hand still works, for example to retry after a
registry outage; a tag for a version the registry already has is verified and
skipped. A tag run looks for the version for up to five minutes before it
concludes the version is unpublished. The workflow rejects private repositories, mismatched tags and
commits outside main's history. Public trusted publication automatically
receives npm provenance. SDK 0.1.1 was the first automatic publication.

The tag is pushed with the repository deploy key `sdk-release-tag`, whose
private half is the Actions secret `SDK_RELEASE_TAG_KEY`, because the
"Protect release tags" ruleset refuses tag creation by the workflow token
(`Resource not accessible by integration`) and GitHub does not accept the
Actions app as a ruleset bypass actor. The ruleset lets deploy keys bypass it;
the `main` ruleset has no bypass, so the key cannot push to `main`. Secrets are
not exposed to workflows from forks. To rotate:

```sh
ssh-keygen -t ed25519 -N '' -C '9lives-runner sdk-release-tag' -f sdk-release-tag
gh repo deploy-key add sdk-release-tag.pub --allow-write --title sdk-release-tag
gh secret set SDK_RELEASE_TAG_KEY < sdk-release-tag
rm sdk-release-tag sdk-release-tag.pub
gh repo deploy-key delete <old-key-id>
```

Keep the key files out of the repository and shell history. If the tag job
fails, push the tag by hand from the published commit:
`git push origin <sha>:refs/tags/sdk-v<version>`.

Do not push `sdk-v0.0.0`: npm will reject that
duplicate version. Go binary releases use separate `v*` tags and include the
root LICENSE and NOTICE in their archives.
