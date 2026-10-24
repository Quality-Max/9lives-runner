import {defineConfig} from '@playwright/test';
export default defineConfig({testDir: './target-assertion', workers: 1, retries: 0, timeout: 5000,
  use: {screenshot: 'off', trace: 'off', video: 'off'}});
