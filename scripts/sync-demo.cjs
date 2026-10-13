// Keep the downloadable GitHub Pages demo and the production site identical.
const { copyFileSync } = require('node:fs');
const { resolve } = require('node:path');
copyFileSync(resolve(__dirname, '../docs/explainer/demo.html'), resolve(__dirname, '../website/public/demo.html'));
