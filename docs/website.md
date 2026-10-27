# Website and installer

`website/` is the [9lives.run](https://9lives.run) landing page, a Next.js app
deployed by Vercel (project `9lives-run`, root directory `website`). Merging a
change under `website/` to `main` deploys it to production; pull requests get
preview deployments. Vercel skips a build when a commit changes nothing under
`website/` or `install/` (`ignoreCommand` in `website/vercel.json`); when the
previous commit is unavailable the build runs. It is a separate npm project with its own lockfile and is
not one of the repository's npm workspaces.

`install/9l.run.sh` is the canonical installer. `website/public/install.sh`
must be a byte-identical copy: `curl -fsSL https://9lives.run/install.sh | sh`
serves it, and the site's proxy sends non-HTML requests for `/` to it. The
installer downloads one checksum-verified release, the one GitHub marks Latest
unless `NINELIVES_VERSION=vX.Y.Z` pins another, and installs `9l`,
`LICENSE`, `NOTICE` and `THIRD-PARTY-NOTICES.txt` by renaming them from a fresh
staging directory. It refuses a destination `9l` that is a symlink or another
CLI, and legal-file destinations that are not regular files.

## Checks

```sh
cd website && npm ci && npm run lint && npm test && npm run build && npm run build:vercel
python -m pytest -q install website/test_site.py   # needs pytest
node --test website/scripts/indexnow.check.mjs
```

The `Website` workflow runs the same checks for changes to `website/` or
`install/`. The installer tests use fixture releases and no network.

## Which release the installer installs

Without `NINELIVES_VERSION`, the installer follows the
`https://github.com/Quality-Max/9lives-runner/releases/latest` redirect and
accepts only a stable `vX.Y.Z` tag of this repository; it does not call the
GitHub API, so the API's rate limit does not apply. The release workflow
creates each release without marking it Latest, and its final `latest` job
marks it Latest only after the published archives, checksums and provenance
attestations were verified and the SDK was published, and only if it is the
highest stable version (see [Releasing](releases.md)). A release that fails
before then never reaches `curl -fsSL https://9lives.run/install.sh | sh`, and
no installer or website change is needed per release. The homepage names no
version for the same reason. `NINELIVES_VERSION=vX.Y.Z` still installs a
specific release, and the downloaded binary must report that version.

Search-engine submission after deploys is described in [IndexNow](indexnow.md).
Campaign images are listed in [meme assets](marketing/meme-assets.md).

## Branding, demo, and GitHub overview

The homepage and offline overview use original QualityMax wordmarks, DM Sans /
Space Grotesk fonts, and Zinc/Indigo tokens from branding revision
`d295da3755e06c679445ebfd43cd88bfb6a4ec15`. Assets are local in `public/brand/`.
Both themes use the demo's `9lives-demo-theme` preference. With no saved choice,
they follow the OS theme; blocked storage still allows a choice for the page.

Edit the homepage, run a local preview, then export its framework-free GitHub
Pages version with `node scripts/export-overview.cjs` from the repository root.
See [the overview workflow](explainer/README.md). The README banner and homepage
social image describe the same recorded experiment.

`docs/explainer/demo.html` is canonical; run `node scripts/sync-demo.cjs` to
refresh the Vercel mirror without losing its consent-gated analytics. All five
chapter hashes (`locator`, `coverage`, `flow`, `live`, `proof`) are direct links.

Only `/demo.html` allows QualityMax (`https://qualitymax.io` and
`https://www.qualitymax.io`) to embed it, through a separate non-overlapping CSP
rule. Other pages retain `frame-ancestors 'none'`. The QualityMax landing page
loads the iframe only on request and keeps a full-page fallback link. Opening
or embedding the demo displays saved results; it never launches Codex or tests.

`node scripts/site-smoke.cjs` verifies theme behavior, responsive overflow,
loaded assets, chapter links, portable overview controls, and allowed/blocked
cross-origin embedding. `node scripts/demo-smoke.cjs` verifies the demo itself.
