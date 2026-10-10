# 9lives explainer

A single static page that explains what a 9lives test looks like, that tests
run without an AI model, how 9lives compares with Vibium and hosted test
vendors, and which capabilities are shipped versus planned.

- `index.html` and `explainer.css`: the page. Plain HTML with inline styles,
  design tokens from the QualityMax design system and one small script for
  the hero copy button. No framework runtime and no CDN scripts. Webfonts are
  loaded from Google Fonts and the page degrades to system fonts offline.
- `assets/qualitymax-logo-white.png`: the header logo.
- `PROMPT.md`: the Claude Design prompt that produced the page. Regenerate
  from it with the Blank template, then export and flatten the rendered DOM
  as described below.

The page is published with GitHub Pages at
<https://quality-max.github.io/9lives-runner/>. The `Pages` workflow in
`.github/workflows/pages.yml` deploys this directory on every push to `main`
that touches it, and refuses to deploy if the page gains an external script.

Open it locally with any static server, for example:

```sh
python3 -m http.server 8080 --directory docs/explainer
```

## Keeping it honest

The page must stay consistent with the README and `AGENTS.md`. The
"Shipped today" list may only contain capabilities that exist on `main`.
Independent behavioral verification, verified replay, native mobile execution
and measured model correctness or cost are planned and must stay under
"Planned, not shipped" until they land with evidence. Do not add accuracy,
latency or cost numbers to the page; none are qualified.

## Versioning

The status section names the release it describes. When a release changes
what is shipped or planned, update the lists in `index.html` and `PROMPT.md`
together and move the version line, so the page never describes an older
release as current.

## Regenerating

Claude Design exports a `.dc.html` file that depends on a React runtime
loaded from a CDN. The committed page is the rendered DOM of that export,
captured with headless Chromium at desktop and narrow widths, with the
runtime removed and the comparison matrix included in both table and card
form, switched by a CSS media query. Capture at both widths, replace the
matrix container, strip the `data-dc-*` attributes and interpolation spans,
and keep the design tokens as a single stylesheet.
