// axe probe for the WorkspaceSwitcher's tinted active item (card t_a475d6c1).
//
// The repo's own a11y gate (web/e2e/a11y.spec.ts) covers PUBLIC routes only, so
// the authenticated chrome — including this switcher — is never scanned by
// `npm run a11y`. That is how the tinted active item shipped at 4.20:1 while the
// sidebar's nav row was fixed in the same redesign: only the switcher's row was
// left behind.
//
// This probe opens the switcher menu on desktop and inside the mobile drawer,
// runs THE GATE'S OWN axe rule over the OPEN menu (and over the page it sits
// in), and prints the measured computed colours of every menu row so the verdict
// is readable without axe's own wording. With `CONTROL=1` it ALSO re-runs the
// scan with only the two on-tint tokens overridden back to their pre-fix values,
// which proves the 0 is not vacuous: if the scan cannot see the tinted active
// item at all, the control stays green and this script fails.
//
// THE RULE SET IS NOT DEFINED HERE (card t_c912998e). This probe used to carry
//     new AxeBuilder({ page }).withTags(["wcag2aa", "wcag21aa"])
// which reaches 6 of axe-core's 105 rules: every A-level ARIA rule (WCAG 4.1.2,
// all 63 rules carrying the `wcag2a` tag) was excluded, so `critical/serious: 0`
// here could describe a menu the gate's own scan fails. It now calls `gateScan()`
// and `blockersOf()` from `../e2e/axeGate.ts` — the same two functions
// `e2e/a11y.spec.ts` and the nine scoped arms run — so a hand-run reading and a
// gate reading cannot disagree about which rules ran or which impacts fail.
//
// That import is TypeScript, so this file needs a Node that strips types
// (>= 22.18; the repo's `npm run test:unit` already requires it). The
// MODULE_TYPELESS_PACKAGE_JSON warning on load is expected and harmless — add
// `--disable-warning=MODULE_TYPELESS_PACKAGE_JSON` if it bothers you.
//
// Usage:
//   SESSION=<jf_session token> BASE=http://127.0.0.1:3202 \
//     node scripts/a11y-switcher.mjs
//   SESSION=… CONTROL=1 node scripts/a11y-switcher.mjs
//
// The account MUST have >=2 workspaces: with one, the switcher renders a minimal
// read-only chip and the tinted active item never renders. Get SESSION by signing
// in to the build you are checking and copying the jf_session cookie. The scan
// must not be pointed at anything but a local build: it injects that cookie into
// a browser.
import { chromium } from "playwright";
import { blockersOf, gateScan } from "../e2e/axeGate.ts";

const BASE = process.env.BASE || "http://127.0.0.1:3202";
const SESSION = process.env.SESSION;
const CONTROL = process.env.CONTROL === "1";
if (!SESSION) {
  console.error("SESSION (a jf_session token) is required");
  process.exit(2);
}
const url = new URL(BASE);

async function openSwitcher(page, scope) {
  // The trigger is the button carrying aria-haspopup="menu". On mobile the
  // desktop topbar chip is still in the DOM but hidden, so the trigger has to be
  // scoped to the drawer when scanning inside it or the click lands on nothing.
  const root = scope ? page.locator(scope) : page;
  const trigger = root.locator('button[aria-haspopup="menu"]').first();
  await trigger.waitFor({ state: "visible", timeout: 60_000 });
  await trigger.click();
  const menu = scope ? page.locator(`${scope} [role="menu"]`) : page.locator('[role="menu"]');
  await menu.first().waitFor({ state: "visible", timeout: 20_000 });
}

function report(label, blockers) {
  console.log(`\n===== ${label} =====`);
  console.log(`critical/serious: ${blockers.length}`);
  for (const v of blockers) {
    for (const n of v.nodes) {
      console.log(`  ${v.id} (${v.impact}) target=${n.target.join(" ")}`);
      console.log(`  ${(n.failureSummary || "").split("\n").join(" | ")}`);
    }
  }
}

async function scanMenu(page, label, include) {
  // The gate's rule set (default set) and the gate's failing impacts. `include`
  // throws when it matches nothing, so a scan cannot pass over an absent menu.
  const res = await gateScan(page, include);
  const blockers = blockersOf(res);
  report(label, blockers);
  return blockers.length;
}

/** The rendered colours of every row in the open switcher menu. */
function readRows(scope) {
  const menu = document.querySelector(scope);
  if (!menu) return { error: `no open menu at ${scope}` };
  const items = [...menu.querySelectorAll('[role="menuitem"]')];
  return {
    menuItems: items.length,
    rows: items.map((el) => {
      const cs = getComputedStyle(el);
      const sub = el.querySelector(".break-all");
      return {
        text: el.textContent.trim().slice(0, 48),
        background: cs.backgroundColor,
        color: cs.color,
        subLineColor: sub ? getComputedStyle(sub).color : null,
      };
    }),
  };
}

const browser = await chromium.launch();
let total = 0;

// ── desktop 1280x800, switcher open ───────────────────────────────────────
const desk = await browser.newContext({ viewport: { width: 1280, height: 800 } });
await desk.addCookies([
  { name: "jf_session", value: SESSION, domain: url.hostname, path: "/", httpOnly: true, sameSite: "Lax" },
]);
const dp = await desk.newPage();
// Fresh state: a jf_lang left by an earlier run would render this for the
// operator and no new visitor.
await dp.addInitScript(() => {
  try {
    localStorage.clear();
  } catch {}
});
await dp.goto(`${BASE}/forms`, { waitUntil: "networkidle" });
await dp.waitForSelector("aside", { state: "attached", timeout: 60_000 });
await openSwitcher(dp);
console.log("\n===== desktop switcher: measured =====");
console.log(JSON.stringify(await dp.evaluate(readRows, '[role="menu"]'), null, 1));
total += await scanMenu(dp, "desktop 1280x800, switcher OPEN, [role=menu]", '[role="menu"]');
total += await scanMenu(dp, "desktop 1280x800, whole page with switcher OPEN");
await dp.screenshot({ path: "/tmp/a11y-switcher-desktop.png" });

if (CONTROL) {
  const asShipped = await dp.evaluate(
    () => getComputedStyle(document.querySelector('[role="menu"] [role="menuitem"]')).color,
  );
  await dp.addStyleTag({
    content: `:root { --jf-primary-on-tint: #5e6ad2; --jf-muted-on-tint: #6b7280; }`,
  });
  await dp.waitForTimeout(300);
  const reverted = await dp.evaluate(
    () => getComputedStyle(document.querySelector('[role="menu"] [role="menuitem"]')).color,
  );
  console.log(`\nactive item colour: as shipped ${asShipped} -> reverted ${reverted}`);
  const control = await scanMenu(
    dp,
    "CONTROL: same DOM, on-tint tokens reverted to --jf-primary / --jf-muted",
    '[role="menu"]',
  );
  if (total !== 0) {
    console.log("\nFAIL: the fixed tree is not clean — the fix did not land");
    process.exit(1);
  }
  if (control === 0) {
    console.log("\nFAIL: 0 even with the defect re-planted — the scan is vacuous");
    process.exit(1);
  }
  console.log("\nCONTROL OK: 0 as shipped, RED with the tokens reverted");
}

// ── mobile 412x915, drawer open, switcher open inside the dialog ──────────
const mob = await browser.newContext({
  viewport: { width: 412, height: 915 },
  isMobile: true,
  hasTouch: true,
});
await mob.addCookies([
  { name: "jf_session", value: SESSION, domain: url.hostname, path: "/", httpOnly: true, sameSite: "Lax" },
]);
const mp = await mob.newPage();
await mp.addInitScript(() => {
  try {
    localStorage.clear();
  } catch {}
});
await mp.goto(`${BASE}/forms`, { waitUntil: "networkidle" });
await mp.waitForSelector("aside", { state: "attached", timeout: 60_000 });

let opened = false;
for (let i = 0; i < 20 && !opened; i++) {
  // Click only when the drawer is shut: a second click toggles it closed and the
  // loop ends with no dialog to scan.
  if ((await mp.locator('[role="dialog"]').count()) === 0) {
    await mp.getByRole("button", { name: "Open menu" }).click({ timeout: 10_000 }).catch(() => {});
  }
  opened = await mp
    .waitForSelector('[role="dialog"]', { state: "visible", timeout: 3_000 })
    .then(() => true)
    .catch(() => false);
}
if (!opened) throw new Error("the mobile drawer never opened");
await mp.waitForTimeout(1000);
await openSwitcher(mp, '[role="dialog"]');
console.log("\n===== mobile drawer switcher: measured =====");
console.log(
  JSON.stringify(await mp.evaluate(readRows, '[role="dialog"] [role="menu"]'), null, 1),
);
total += await scanMenu(
  mp,
  "mobile 412x915, drawer OPEN + switcher OPEN, [role=dialog]",
  '[role="dialog"]',
);
await mp.screenshot({ path: "/tmp/a11y-switcher-drawer.png" });

await browser.close();
console.log(`\n===== TOTAL critical/serious: ${total} =====`);
process.exit(total === 0 ? 0 : 1);
