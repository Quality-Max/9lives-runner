# 9lives overview and demo

The [GitHub Pages overview](https://quality-max.github.io/9lives-runner/) mirrors
the evidence-first [9lives.run homepage](https://9lives.run): the recorded fix,
five demo chapters, the reproduction/confirmation/fault-testing workflow, local
installation, and explicit available/experimental/planned boundaries.

`index.html` is a framework-free export of the homepage. It uses local official
QualityMax fonts, logos, tokens, and both themes. Inline scripts handle theme
and clipboard controls only; no external runtime or analytics is loaded.
`demo.html` is the canonical standalone interactive demo, with embedded assets.
The recorded chapters show saved evidence, not live agent execution.

## Update the overview

Edit the homepage in `website/app/page.tsx` and its styles first. Start
`npm run dev` in `website/`, then from the repository root run:

```sh
node scripts/export-overview.cjs
node scripts/sync-demo.cjs
node scripts/site-smoke.cjs
```

The exporter copies shared styles and brand assets alongside the rendered
header, main content, and footer. It removes framework scripts and tracking
controls, changes local demo/asset paths, and adds portable theme/copy controls.
Review the generated page at mobile and desktop sizes before committing.
Do not hand-edit the generated HTML or mirrored styles.

Open it locally with:

```sh
python3 -m http.server 8080 --directory docs/explainer
```

The `Pages` workflow publishes this directory after changes merge to `main`
and rejects external runtime scripts in the overview. The Vercel website is a
separate deployment; see [website documentation](../website.md).

## Evidence boundaries

Confirmation and fault testing are experimental. The fix chapter qualifies
seven Chromium executions on one synthetic profile app. A passing test is not
independent requirement coverage; a caught fault means an assertion failed.
Goal completion is unverified. Independent behavioral verification, verified
replay, and native mobile qualification remain planned. Keep those limits
consistent with the README, demo, and `AGENTS.md`.
