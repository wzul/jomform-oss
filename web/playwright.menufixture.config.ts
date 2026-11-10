import { defineConfig, devices } from "@playwright/test";

// Standalone config for the NON-PROD menu-checkout fixture spec (card
// t_2244109a). It exists so that spec can be run on its own, in seconds, without
// the Next.js dashboard.
//
// The main config (`playwright.config.ts`) owns two families that both run
// against `next start`, because every spec in `web/e2e/` drives the dashboard.
// The fixture spec is the one spec that drives the GO public handler instead:
// its server is `api/cmd/menu-fixture`, started by
// `scripts/menu-fixture-e2e.sh`, and it reads the page's URL from
// MENU_FIXTURE_URL. Pointing the main config at it would have two consequences,
// both wrong:
//
//   - `nginx`-free but not free: the main config's `webServer` would start
//     `next start` for every run of this one spec — a build-dependent 30s+ cost
//     for a server the spec never visits, and a hard failure on a tree that has
//     not run `next build`.
//   - `reuseExistingServer` is deliberately false there (a leftover server must
//     never answer a gate), so it could not be talked out of it.
//
// The main config STILL selects this spec through its `feature-core` project
// (filename prefix rule), so it is enumerated by `scripts/spec-counts.sh` and
// runs in the `e2e` gate — where, with no MENU_FIXTURE_URL, it skips and says so.
// That keeps the spec in the family (a spec in no project is a silent coverage
// loss) while giving the lane that WANTS the measurement a one-command path.
export default defineConfig({
  testDir: "./e2e",
  testMatch: /menu-checkout-option-group\.spec\.ts/,
  timeout: 60_000,
  fullyParallel: false,
  reporter: [["list"]],
  use: { ...devices["Desktop Chrome"] },
});
