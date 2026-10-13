import {defineConfig} from '@playwright/test';
export default defineConfig({
  testDir: './tests', workers: 1, retries: 0, timeout: 15000,
  expect: {timeout: 1500},
  use: {browserName: 'chromium', viewport: {width: 1280, height: 900},
    actionTimeout: 2500, trace: 'off', screenshot: 'off', video: 'off',
    launchOptions: {slowMo: process.env.LAB_HEADED === '1' ? 120 : 0}},
});
