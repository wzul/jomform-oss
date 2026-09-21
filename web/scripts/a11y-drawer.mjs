// Authenticated a11y probe for the sidebar/drawer card (t_1930d754).
// The repo's own axe gate (web/e2e/a11y.spec.ts) covers PUBLIC routes only —
// authenticated dashboard chrome needs a session cookie it does not provision,
// so the drawer/nav redesign would never be scanned by `npm run a11y`.
// This script injects a session cookie minted for a locally seeded user, opens
// the mobile drawer, and runs axe over the open dialog + the desktop sidebar.
//
// Usage:
//   SESSION=<jf_session cookie value> BASE=http://127.0.0.1:3200 \
//     node scripts/a11y-drawer.mjs
//
// Get SESSION by signing in to the build you are checking and copying the
// jf_session cookie (DevTools → Application → Cookies). The scan must not be
// pointed at anything but a local build: it injects that cookie into a browser.
import { chromium } from "playwright";
import AxeBuilder from "@axe-core/playwright";

const BASE = process.env.BASE || "http://127.0.0.1:3200";
const SESSION = process.env.SESSION;
if (!SESSION) {
  console.error("SESSION (a jf_session token) is required");
  process.exit(2);
}

const url = new URL(BASE);
const browser = await chromium.launch();
const ctx = await browser.newContext({
  viewport: { width: 412, height: 915 },
  isMobile: true,
  hasTouch: true,
});
await ctx.addCookies([
  { name: "jf_session", value: SESSION, domain: url.hostname, path: "/", httpOnly: true, sameSite: "Lax" },
]);

const page = await ctx.newPage();
await page.goto(`${BASE}/forms`, { waitUntil: "networkidle" });
await page.waitForSelector("aside", { state: "attached", timeout: 60_000 });
// The shell re-renders when the session bundle lands (workspace chip appears),
// which detaches the hamburger mid-click. Wait for the settled state first.
await page.waitForFunction(() => {
  const a = document.querySelector("aside a[href='/forms']");
  return !!a && a.getAttribute("aria-current") === "page";
}, null, { timeout: 60_000 });

// Open the mobile drawer. The shell re-renders when the session bundle lands
// (the workspace chip appears and the nav gets its aria-current), which detaches
// the hamburger mid-click — so retry until the dialog is actually up instead of
// clicking a node React is about to replace.
let opened = false;
for (let i = 0; i < 20 && !opened; i++) {
  // Click only when the drawer is NOT already open — a second click toggles it
  // shut again, which is how this loop can end with no dialog to scan.
  if ((await page.locator('[role="dialog"]').count()) === 0) {
    await page.getByRole("button", { name: "Open menu" }).click({ timeout: 10_000 }).catch(() => {});
  }
  opened = await page
    .waitForSelector('[role="dialog"]', { state: "visible", timeout: 3_000 })
    .then(() => true)
    .catch(() => false);
}
if (!opened) throw new Error("the mobile drawer never opened");
// Settle: the shell re-renders when the session bundle lands.
await page.waitForTimeout(1500);
if ((await page.locator('[role="dialog"]').count()) === 0) {
  await page.getByRole("button", { name: "Open menu" }).click();
  await page.waitForSelector('[role="dialog"]', { state: "visible", timeout: 10_000 });
}
const drawer = await new AxeBuilder({ page }).include('[role="dialog"]').analyze();
const drawerBlockers = drawer.violations.filter((v) => v.impact === "critical" || v.impact === "serious");

// Desktop sidebar.
const desk = await browser.newContext({ viewport: { width: 1280, height: 800 } });
await desk.addCookies([
  { name: "jf_session", value: SESSION, domain: url.hostname, path: "/", httpOnly: true, sameSite: "Lax" },
]);
const dpage = await desk.newPage();
await dpage.goto(`${BASE}/profile`, { waitUntil: "networkidle" });
await dpage.waitForSelector("aside", { state: "attached", timeout: 60_000 });
await dpage.waitForSelector('aside a[href="/profile"][aria-current="page"]', { timeout: 60_000 });
const aside = await new AxeBuilder({ page: dpage }).include("aside").analyze();
const asideBlockers = aside.violations.filter((v) => v.impact === "critical" || v.impact === "serious");

const show = (label, list) => {
  console.log(`\n[${label}] ${list.length} critical/serious violation(s)`);
  for (const v of list) {
    console.log(`  - ${v.id} (${v.impact}): ${v.help}`);
    for (const n of v.nodes.slice(0, 3)) console.log(`      ${n.target.join(" ")}`);
  }
};
show("mobile drawer open", drawerBlockers);
show("desktop sidebar", asideBlockers);
console.log(
  `\nall drawer violations: ${drawer.violations.map((v) => `${v.id}:${v.impact}`).join(", ") || "none"}`,
);
console.log(
  `all sidebar violations: ${aside.violations.map((v) => `${v.id}:${v.impact}`).join(", ") || "none"}`,
);

await browser.close();
process.exit(drawerBlockers.length + asideBlockers.length === 0 ? 0 : 1);
