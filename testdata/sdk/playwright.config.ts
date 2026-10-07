import {defineConfig} from '@playwright/test';
// Configuration commonly prints to stdout (dotenv 17 does by default). The engine
// protocol uses its own channel, so this line must never affect evidence.
console.log('[dotenv@17.2.3] injecting env (0) from .env');
export default defineConfig({
  testDir: './tests', workers: 1, retries: 0, timeout: 30000,
  use: {browserName: 'chromium', headless: true, viewport: {width: 777, height: 600}},
});
