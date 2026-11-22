import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  timeout: 30000,
  workers: 1,
  use: {
    baseURL: process.env.BASE_URL || "http://127.0.0.1:18767",
    viewport: { width: 1400, height: 900 },
    locale: "zh-CN",
    launchOptions: {
      // The dev machine has a global HTTP proxy that swallows 127.0.0.1; force
      // Chromium to connect directly so the local runvm http server is reached.
      args: ["--no-proxy-server"],
    },
  },
});
