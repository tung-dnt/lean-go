import process from "node:process";
import { defineConfig } from "@playwright/test";

const baseURL = process.env.LEANGO_BROWSER_URL;
if (!baseURL) throw new Error("Run just browser to start the isolated server and database");

export default defineConfig({
  testDir: "./scripts",
  testMatch: "browser.test.ts",
  forbidOnly: true,
  workers: 1,
  retries: 0,
  timeout: 60_000,
  expect: { timeout: 10_000 },
  reporter: [["list"], ["html", { open: "never" }]],
  use: {
    baseURL,
    browserName: "chromium",
    screenshot: "on",
    actionTimeout: 10_000,
    trace: "retain-on-failure",
  },
});
