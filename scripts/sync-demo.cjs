// Keep the downloadable GitHub Pages demo and the production site identical.
const { readFileSync, writeFileSync } = require('node:fs');
const { resolve } = require('node:path');
const demoPath = resolve(__dirname, '../docs/explainer/demo.html');
const analytics = readFileSync(resolve(__dirname, '../website/public/qualitymax-analytics.js'), 'utf8');
const demo = readFileSync(demoPath, 'utf8').replace(
  /<!-- qualitymax-analytics:start -->[\s\S]*?<!-- qualitymax-analytics:end -->/,
  () => `<!-- qualitymax-analytics:start -->\n<script>\n${analytics}</script>\n<!-- qualitymax-analytics:end -->`,
);
writeFileSync(demoPath, demo);
writeFileSync(resolve(__dirname, '../website/public/demo.html'), demo);
