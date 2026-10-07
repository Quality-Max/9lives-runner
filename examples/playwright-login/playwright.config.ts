import {defineConfig} from '@playwright/test';
export default defineConfig({
  testDir: './tests', workers: 1, retries: 0, timeout: 90_000,
  use: {baseURL: process.env.QA_BASE_URL || 'http://127.0.0.1:8000', headless: true},
});
