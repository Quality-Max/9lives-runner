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
installer downloads one pinned, checksum-verified release.

## Checks

```sh
cd website && npm ci && npm run lint && npm test && npm run build && npm run build:vercel
python -m pytest -q install website/test_site.py   # needs pytest
node --test website/scripts/indexnow.check.mjs
```

The `Website` workflow runs the same checks for changes to `website/` or
`install/`. The installer tests use fixture releases and no network.

## Bumping the installed release

Change the installer's default `NINELIVES_VERSION` only after the release is
published and verified (see [Releasing](releases.md)): production serves the
installer as soon as the change merges. Update `install/9l.run.sh`, copy it to
`website/public/install.sh`, and update the version the site and
`website/test_site.py` show.

Search-engine submission after deploys is described in [IndexNow](indexnow.md).
Campaign images are listed in [meme assets](marketing/meme-assets.md).
