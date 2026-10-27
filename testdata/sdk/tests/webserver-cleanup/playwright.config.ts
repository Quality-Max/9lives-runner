import {defineConfig} from '@playwright/test';

export default defineConfig({
  testDir: '.', testMatch: 'owned.spec.ts', workers: 1, retries: 0, timeout: 60000,
  use: {browserName: 'chromium', headless: true},
  webServer: {
    command: 'node server.cjs',
    url: `http://127.0.0.1:${process.env.NINELIVES_SMOKE_PORT}`,
    reuseExistingServer: false,
    timeout: 10000,
  },
});
