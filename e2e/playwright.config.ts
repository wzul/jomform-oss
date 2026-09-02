import { defineConfig, devices } from '@playwright/test';

/**
 * Money-path E2E config (issue #99). Runs against a staging deploy with a
 * test-mode CHIP account seeded with sandbox credentials.
 *
 * Env vars:
 *   E2E_BASE_URL        staging base URL (default https://staging.jomform.com)
 *   E2E_CHIP_BRAND_ID   test-mode CHIP brand id
 *   E2E_CHIP_SECRET_KEY test-mode CHIP secret (sk_test_…)
 */
export default defineConfig({
  testDir: './specs',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: process.env.CI ? 'github' : 'list',
  use: {
    baseURL: process.env.E2E_BASE_URL || 'https://staging.jomform.com',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
  ],
});
