import { defineConfig, devices } from "@playwright/test";

// Accessibility scan config (issue #104). Runs axe-core against the key
// public pages of the Next.js app. The web server is started automatically
// by Playwright (webServer) so the scan is self-contained.
//
// Authenticated dashboard pages (/, /forms, /builder, /settings, /profile,
// /products) require a live session cookie from the Go API, which the scan does
// not provision. Without a session they render the loading shell / bounce to
// /landing, so the automated scan covers the fully-rendered public routes
// below; the authenticated surfaces are exercised by the manual
// screen-reader + browser-matrix checklist (docs/accessibility.md).
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
const PORT = Number(process.env.A11Y_PORT || 3100);
const BASE = `http://127.0.0.1:${PORT}`;

export default defineConfig({
  testDir: "./e2e",
  timeout: 60_000,
  fullyParallel: true,
  reporter: [["list"]],
  use: {
    baseURL: BASE,
    trace: "retain-on-failure",
  },
  // TWO NAMED FAMILIES, selected by name so neither gate can drift (t_34fd25cb).
  //
  // The `a11y` gate scans axe; every fixture whose name starts with `a11y`
  // belongs to it, and nothing else does. The feature specs in web/e2e/ are real
  // end-to-end regressions that happened to live under this config — they are
  // selected by the `feature-e2e` project and run by the `e2e` script.
  //
  // THE SPLIT IS BY FILENAME PREFIX ON PURPOSE: a list of explicit spec paths
  // here would have to be edited every time a spec is added, and a spec that is
  // in no project is a silent coverage loss — the exact failure this split must
  // not introduce. A PREFIX is a rule: a new `a11y-*.spec.ts` is scanned by the
  // a11y gate automatically, and any other new spec runs under `e2e`.
  //
  // `npm run a11y` is therefore NOT the whole directory any more; the gate's own
  // label and GATE-INVENTORY.md row say what it is.
  projects: [
    { name: "a11y", use: { ...devices["Desktop Chrome"] }, testMatch: /a11y.*\.spec\.ts/ },
    { name: "feature-e2e", use: { ...devices["Desktop Chrome"] }, testMatch: /^(?!.*a11y).*\.spec\.ts$/ },
  ],
  webServer: {
    command: `npm run start -- -p ${PORT}`,
    url: BASE,
    reuseExistingServer: process.env.A11Y_REUSE_SERVER === "1",
    timeout: 120_000,
  },
});
