import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests", retries: 0, workers: 1,
  use: { baseURL: "https://qualitymax-demo-bank.vercel.app", browserName: "chromium", viewport: { width: 1280, height: 720 }, trace: "off", screenshot: "off", video: "off" },
});
