import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  workers: 1,
  retries: 0,
  timeout: 5000,
  use: { browserName: 'chromium' },
});
