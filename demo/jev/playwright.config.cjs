const { defineConfig } = require('@playwright/test');
module.exports = defineConfig({ testDir: '.', testMatch: 'picture-hunt.spec.mjs', workers: 1, retries: 0,
  timeout: 60000, expect: { timeout: 1200 }, outputDir: '../../.context/jev-playwright-output',
  use: { browserName: 'chromium', viewport: { width: 850, height: 600 }, deviceScaleFactor: 1,
    actionTimeout: 2000, trace: 'off', screenshot: 'off', video: 'off' } });
