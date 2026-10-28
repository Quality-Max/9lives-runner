import {defineConfig} from '@playwright/test';
export default defineConfig({
  testDir: './android', workers: 1, retries: 0, timeout: 240000,
  projects: [{name: 'android:finance'}],
  use: {screenshot: 'off', trace: 'off', video: 'off'},
});
