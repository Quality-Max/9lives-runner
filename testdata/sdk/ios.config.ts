import {defineConfig} from '@playwright/test';
export default defineConfig({
  testDir: './ios', workers: 1, retries: 0, timeout: 240000,
  projects: [{name: 'ios:finance'}],
  use: {screenshot: 'off', trace: 'off', video: 'off'},
});
