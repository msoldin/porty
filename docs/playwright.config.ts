import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./tests",
  outputDir: process.env.PORTY_DOCS_TEST_OUTPUT || "/tmp/porty-docs-tests",
  workers: 1,
  use: { baseURL: "http://127.0.0.1:4174/porty/", trace: "retain-on-failure" },
  webServer: {
    command: "bun run preview",
    url: "http://127.0.0.1:4174/porty/",
    reuseExistingServer: false,
  },
  projects: [
    {
      name: "desktop",
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 1440, height: 1000 },
      },
    },
    {
      name: "mobile",
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 390, height: 844 },
        isMobile: true,
        hasTouch: true,
      },
    },
  ],
});
