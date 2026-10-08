# Releasing 9lives

The initial SDK version is **0.0.0**. The Go CLI and `@9l/playwright` are
separate distributions; installing the SDK does not install the CLI.

Before public release, finish the repository history cleanup, retain private
backups and verify that the repository is public. Branch rewrites do not remove
GitHub's retained pull-request refs or cached commit views. Existing clones
must be replaced or carefully reconciled before anyone pushes old history.

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

## First npm publication

An npm account with permission to publish `@9l/playwright` must create the
package before its trusted publisher can be configured. Authenticate with
`npm login` on the publishing machine; keep credentials out of the repository
and agent output. After the public source changes have merged and the checks
pass, publish the exact qualified artifact:

```sh
npm publish .context/npm-pack/9l-playwright-0.0.0.tgz --access public
```

The account must meet npm's authentication and two-factor requirements. Local
publication does not receive GitHub Actions provenance. Do not reuse a
published version number; the initial publication remains 0.0.0.

## Subsequent trusted publications

Configure the package's npm trusted publisher for GitHub owner `Quality-Max`,
repository `9lives-runner`, and workflow `npm-release.yml`. Enable direct
publishing in npm's publisher settings; a publisher restricted to staging
does not authorize the workflow's direct `npm publish` command.

For a future release, update the SDK version and lockfile in a reviewed change,
merge it to `main`, and push a matching `sdk-v<version>` tag. The workflow
rejects private repositories, mismatched versions and commits outside main's
history. It runs the complete CI workflow, then publishes the exact qualified
tarball using short-lived OIDC authentication on a GitHub-hosted runner.
Public trusted publication automatically receives npm provenance.

Do not push `sdk-v0.0.0` after manually publishing 0.0.0: npm will reject that
duplicate version. Go binary releases use separate `v*` tags and include the
root LICENSE and NOTICE in their archives.
