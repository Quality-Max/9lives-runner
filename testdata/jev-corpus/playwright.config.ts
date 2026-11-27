import {defineConfig} from '@playwright/test';

// Fixed viewport and failure evidence, so every case captures the same kind of
// snapshot and screenshot a user's failing run would.
export default defineConfig({
  testDir: './cases', workers: 1, retries: 0, timeout: 20000, expect: {timeout: 1000},
  use: {browserName: 'chromium', headless: true, viewport: {width: 1000, height: 700}, actionTimeout: 1500, screenshot: 'only-on-failure', trace: 'off', video: 'off'},
});
