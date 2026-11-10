import { defineConfig, devices } from "@playwright/test";
import { E2E_ORIGIN, E2E_PORT } from "./e2e/auth";

// Accessibility scan config (issue #104). Runs axe-core against the key
// public pages of the Next.js app. The web server is started automatically
// by Playwright (webServer) so the scan is self-contained.
//
// The PUBLIC PAGES list lives in `e2e/a11y.spec.ts` and holds routes reachable
// with no session. Authenticated dashboard pages (/, /forms, /builder,
// /settings, /profile, /products) do NOT go in that list — without a session
// cookie from the Go API they render the loading shell / bounce to /landing, so
// an entry for one would scan nothing and pass green (t_81b2a928). They are
// scanned by the sibling `a11y-*.spec.ts` files, which stub the
// `dashboard-bundle` at the browser level and drive the real client components
// (`e2e/a11y-products.spec.ts`, `e2e/a11y-settings-first-run-tab.spec.ts`,
// `e2e/a11y-profile-tabs.spec.ts` — the last one added by card t_b589a807, whose
// defect was 5 + 10 critical `aria-required-parent` nodes on /profile and
// /settings that this gate structurally could not see). The `a11y` project
// selects every one of them by FILENAME PREFIX, so a new `a11y-*.spec.ts` is in
// the gate by its name alone; `src/lib/a11yAuthedCoverage.test.ts` keeps the
// signed-in routes pinned to specs that really run axe.
//
// PORT: override with A11Y_PORT. This matters because the scan is now run by
// `scripts/preflight.sh` on a shared agent host where a previous run's
// `next-server` may still be holding the default port. With a fixed port and
// `reuseExistingServer` on, playwright would silently scan THAT leftover build
// and axe would report a clean pass for code this tree never built. So:
//   * `reuseExistingServer` is FALSE unless A11Y_REUSE_SERVER=1, i.e. the gate
//     never trusts a server it did not start itself, and
//   * preflight picks a free A11Y_PORT, so a leftover cannot collide either.
// In CI (a fresh container) the port was always free; that assumption does not
// hold locally, which is why this knob exists.
const PORT = E2E_PORT;
const BASE = E2E_ORIGIN;

export default defineConfig({
  testDir: "./e2e",
  timeout: 60_000,
  fullyParallel: true,
  reporter: [["list"]],
  use: {
    baseURL: BASE,
    trace: "retain-on-failure",
  },
  // THREE NAMED FAMILIES, selected by name so none of them can drift (t_34fd25cb,
  // split further by card t_ec2aac42).
  //
  // The `a11y` gate scans axe; every fixture whose name starts with `a11y`
  // belongs to it, and nothing else does. The feature specs in web/e2e/ are real
  // end-to-end regressions that happened to live under this config; they are
  // selected by the `feature-builder` / `feature-core` projects and run by the
  // `e2e-builder` / `e2e-core` gates.
  //
  // WHY THE FEATURE FAMILY IS SPLIT IN TWO. It is the most expensive thing in
  // the preflight (measured 2026-11-10 on this host: the whole family, 348+
  // tests, ~570s — 21% of a release run). One `next start` serving three
  // workers is the ceiling: the 17 `builder-*` specs alone go 565s at one worker
  // and only 303s at three (1.86x, not 3x), so the server, not the worker count,
  // is what saturates. TWO servers on TWO reserved ports, each running one half,
  // removes that ceiling; the preflight runs both halves concurrently and the
  // `a11y` gate alongside them (see the split block in `scripts/preflight.sh`).
  //
  // THE SPLIT IS BY FILENAME PREFIX ON PURPOSE: a list of explicit spec paths
  // here would have to be edited every time a spec is added, and a spec that is
  // in no project is a silent coverage loss — the exact failure this split must
  // not introduce. A PREFIX is a rule: a new `a11y-*.spec.ts` is scanned by the
  // a11y gate automatically, a new `builder-*.spec.ts` joins the builder half,
  // and any other new spec runs under `feature-core`. `node --test`'s guard in
  // the `selftest` gate (check 39) proves every spec belongs to exactly one
  // project, so a rule that stops matching anything cannot pass unnoticed.
  projects: [
    { name: "a11y", use: { ...devices["Desktop Chrome"] }, testMatch: /a11y.*\.spec\.ts/ },
    // `testMatch` regexes are evaluated against the FILE'S FULL PATH (measured on
    // this tree: `^builder-` matched 0 specs while `/builder-` matched the family),
    // so each pattern must name the file it wants where the basename starts, not
    // anchor at the start of the string.
    { name: "feature-builder", use: { ...devices["Desktop Chrome"] }, testMatch: /\/builder-.*\.spec\.ts$/ },
    { name: "feature-core", use: { ...devices["Desktop Chrome"] }, testMatch: /^(?!.*a11y)(?!.*\/builder-).*\.spec\.ts$/ },
  ],
  webServer: {
    command: `npm run start -- -p ${PORT}`,
    url: BASE,
    reuseExistingServer: process.env.A11Y_REUSE_SERVER === "1",
    timeout: 120_000,
  },
});
