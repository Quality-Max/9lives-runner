import {defineConfig} from '@playwright/test';

export default defineConfig({
  testDir: './targets', workers: 1, retries: 2, timeout: 10000,
  projects: [{name: 'web:finance'}, {name: 'android-fake:finance'}, {name: 'web:unemulated', use: {viewport: null}},
    // The fake driver under a real Android target name, so the engine gives it
    // the native private output and retention path. A failed test also
    // leaves a trace there, which must never be retained.
    {name: 'android:finance', use: {trace: 'retain-on-failure'}}],
  use: {browserName: 'chromium', headless: true, screenshot: 'off', trace: 'off'},
});
