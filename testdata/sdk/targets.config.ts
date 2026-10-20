import {defineConfig} from '@playwright/test';

export default defineConfig({
  testDir: './targets', workers: 1, retries: 2, timeout: 10000,
  projects: [{name: 'web:finance'}, {name: 'android-fake:finance'}],
  use: {browserName: 'chromium', headless: true, screenshot: 'off', trace: 'off'},
});
