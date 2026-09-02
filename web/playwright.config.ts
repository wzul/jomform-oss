import { defineConfig, devices } from "@playwright/test";

// Accessibility scan config (issue #104). Runs axe-core against the key
// public pages of the Next.js app. The web server is started automatically
// by Playwright (webServer) so the scan is self-contained in CI.
//
// Authenticated dashboard pages (/, /forms, /builder, /settings, /profile,
// /products) require a live session cookie from the Go API, which CI does not
// provision. Without a session they render the loading shell / bounce to
// /landing, so the automated scan covers the fully-rendered public routes
// below; the authenticated surfaces are exercised by the manual
// screen-reader + browser-matrix checklist (docs/accessibility.md).
export default defineConfig({
  testDir: "./e2e",
  timeout: 60_000,
  fullyParallel: true,
  reporter: [["list"]],
  use: {
    baseURL: "http://127.0.0.1:3100",
    trace: "retain-on-failure",
  },
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"] } },
  ],
  webServer: {
    command: "npm run start -- -p 3100",
    url: "http://127.0.0.1:3100",
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
});
