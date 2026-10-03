// Accessibility guard for the WorkspaceSwitcher's tinted ACTIVE item.
//
// WHY THIS FILE EXISTS (card t_6da00908). The switcher's tinted active row shipped at
// 4.20:1 while the sidebar's nav row was fixed in the same redesign — only the switcher's
// row was left behind. Card t_a475d6c1 fixed it and added `web/scripts/a11y-switcher.mjs`
// as the "executed proof", and `src/lib/sidebarNav.test.ts` leans on that script as the
// executed half of its rule ("The EXECUTED proof is the axe run in the card (switcher
// open, 2+ workspaces); this is the part that keeps it from coming back").
//
// **Nothing ran it.** `npm run a11y` is `playwright test --project=a11y`, whose testMatch
// is `/a11y.*\.spec\.ts/`; the script lives under `scripts/`, needs a `SESSION` token and a
// local build, and no spec referenced it. Measured on the PR-731 head: `npm run a11y`
// reported 67 passed and `grep -c a11y-switcher <its log>` was 0. So the switcher's axe
// coverage was CLAIMED in three places and COLLECTED nowhere — and it is on an
// authenticated surface the public-route scan cannot reach, so there was no other automated
// measurement of it either (the unit guard reads the component as TEXT and cannot measure a
// painted colour).
//
// HOW IT REPLACES THE SCRIPT WITHOUT A SESSION. Card t_a475d6c1's own measurement is used
// as the shape: an authenticated surface renders from the browser's `dashboard-bundle`
// fetch, so stubbing that fetch at the browser level (the pattern `e2e/a11y-dialogs.spec.ts`
// and `e2e/builder-canvas-price-contrast.spec.ts` already use — "no Go server, no session,
// no DB") puts the real component in a real browser with no backend and no credential. The
// bundle carries TWO workspaces, because with one the switcher renders a read-only chip and
// the tinted active row never renders — a green reading for the wrong reason, which is
// exactly the vacuity this card is about.
//
// WHAT IS PINNED, AND WHAT MAKES IT BITE
//   1. The menu must be REACHABLE — a trigger or a menu that stops matching FAILS rather
//      than skipping, because a guard that passes when its subject vanished is the hole
//      this file closes.
//   2. The ACTIVE row must be found, so the scan cannot report a clean 0 for a menu that
//      renders no tinted row.
//   3. The ratio is MEASURED, not asserted from a class name (same reason as
//      builder-canvas-price-contrast.spec.ts). This is the half the unit guard cannot do.
//   4. The CONTROL is the non-vacuity arm the old script had: with the two on-tint tokens
//      overridden back to their PRE-FIX values for every paint, the measurement must move
//      BELOW the floor. Without it, a spec that accidentally measured the wrong node would
//      stay green while claiming to cover the defect.
//
// A FINDING NOTED, NOT FIXED HERE. The active workspace is conveyed by COLOUR plus a `✓`
// glyph (`s.slug === value` in the component) and carries no `aria-current`, so a screen
// reader user cannot tell which workspace is active from the menu rows. This probe
// identifies the row by MEASURING its painted background, which is the property under test;
// the missing programmatic state is a separate defect and deserves its own card rather than
// being silently assumed away here.
//
// Run with:
//   npm run a11y                                  # the whole gate (this file included)
//   npx playwright test e2e/a11y-switcher.spec.ts # this file alone

import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import type { AxeResults, Result } from "axe-core";

// The gate's own rule, kept identical to e2e/a11y.spec.ts so the two cannot drift.
const IMPACTS = ["critical", "serious"] as const;

function blockersOf(results: AxeResults): Result[] {
  return results.violations.filter((v) =>
    IMPACTS.includes((v.impact ?? "") as (typeof IMPACTS)[number]),
  );
}

/** The pre-fix on-tint tokens, from card t_a475d6c1's own measurement. */
const PRE_FIX_TOKENS = ":root{--jf-primary-on-tint:#5e6ad2;--jf-muted-on-tint:#6b7280;}";

// TWO workspaces. One renders a read-only chip and the tinted active row never appears,
// so the scan would be green over nothing.
const BUNDLE = {
  me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", is_super_admin: true },
  workspaces: [
    { id: "ws1", name: "Demo", slug: "demo", role: "admin" },
    { id: "ws2", name: "Second", slug: "second", role: "admin" },
  ],
  chip_status: null,
  keys: [],
  domains: [],
  webhooks: [],
  social: [],
  einvoice: null,
  products: [],
  summary: { counts: {}, revenue_sen: 0 },
  sales: { items: [], page: 1, limit: 20, total: 0 },
  form_analytics: [],
  product_analytics: [],
};

/** Every /api/* call the dashboard shell makes, stubbed at the browser level. */
async function stubAPI(page: Page) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST, specific routes AFTER it: Playwright resolves matching routes in
  // reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(BUNDLE.me)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) =>
    r.fulfill(json({ rates: [], cached: false })),
  );
}

/**
 * A FRESH browser state with one workspace selected, exactly as
 * `e2e/a11y-dialogs.spec.ts` does it: the browser keeps localStorage between runs, and a
 * `jf_workspace` left behind by an earlier run would scope the bundle to a workspace this
 * file never described.
 */
async function freshState(page: Page) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
}

/**
 * The switcher trigger and its menu. The trigger is the button carrying
 * `aria-haspopup="menu"` — the same anchor the old script used, so a rename of the
 * component's own affordance fails here rather than silently scanning nothing.
 */
async function openSwitcher(page: Page) {
  const trigger = page.locator('button[aria-haspopup="menu"]').first();
  await trigger.waitFor({ state: "visible", timeout: 30_000 });
  await trigger.click();
  const menu = page.locator('[role="menu"]').first();
  await menu.waitFor({ state: "visible", timeout: 30_000 });
  return { trigger, menu };
}

/**
 * Measure the painted colours of every row in the OPEN menu.
 *
 * Same technique as builder-canvas-price-contrast.spec.ts, and for the same reason: the
 * tints are emitted in whatever colour space Tailwind v4 chose (oklch/lab), so the colours
 * are round-tripped through a canvas to get the sRGB bytes the compositor actually paints,
 * with the renderer doing the alpha compositing for translucent layers. A guard that
 * trusted a class name, or rounded, would report this defect as a pass.
 */
async function measureMenuRows(page: Page, menuSelector: string) {
  return page.evaluate((sel) => {
    const cv = document.createElement("canvas");
    cv.width = cv.height = 1;
    const ctx = cv.getContext("2d", { willReadFrequently: true })!;
    const paint = (base: string, c?: string) => {
      ctx.clearRect(0, 0, 1, 1);
      ctx.fillStyle = base;
      ctx.fillRect(0, 0, 1, 1);
      if (c) {
        ctx.fillStyle = c;
        ctx.fillRect(0, 0, 1, 1);
      }
      const d = ctx.getImageData(0, 0, 1, 1).data;
      return { r: d[0], g: d[1], b: d[2] };
    };
    const hex = (c: { r: number; g: number; b: number }) =>
      "#" + [c.r, c.g, c.b].map((v) => v.toString(16).padStart(2, "0")).join("");
    const lin = (v: number) =>
      v / 255 <= 0.04045 ? v / 255 / 12.92 : ((v / 255 + 0.055) / 1.055) ** 2.4;
    const lum = (c: { r: number; g: number; b: number }) =>
      0.2126 * lin(c.r) + 0.7152 * lin(c.g) + 0.0722 * lin(c.b);
    const ratio = (a: { r: number; g: number; b: number }, b: { r: number; g: number; b: number }) => {
      const [x, y] = [lum(a), lum(b)];
      return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
    };

    const menu = document.querySelector(sel);
    if (!menu) throw new Error(`no menu for ${sel} — the switcher moved`);

    const rows = Array.from(
      menu.querySelectorAll('[role="menuitem"], [role="menuitemradio"], a, button'),
    ).filter((el) => (el.textContent || "").trim() !== "");

    return rows.map((el) => {
      const cs = getComputedStyle(el);
      const layers: string[] = [];
      for (let n: Element | null = el; n; n = n.parentElement) {
        const c = getComputedStyle(n).backgroundColor;
        if (c !== "rgba(0, 0, 0, 0)" && c !== "transparent") layers.push(c);
        if (n === document.body) break;
      }
      let bg = { r: 255, g: 255, b: 255 };
      for (let i = layers.length - 1; i >= 0; i--) bg = paint(`rgb(${bg.r}, ${bg.g}, ${bg.b})`, layers[i]);
      const fg = paint(`rgb(${bg.r}, ${bg.g}, ${bg.b})`, cs.color);
      return {
        text: (el.textContent || "").trim().slice(0, 40),
        // THE ACTIVE ROW IS CONVEYED BY COLOUR, NOT PROGRAMMATICALLY. The component marks
        // it with the indigo-50 tint plus a `✓` glyph (`s.slug === value`) and sets no
        // `aria-current`, so this probe identifies it by MEASURING the painted
        // background — which is also the property under test.
        onTint: hex(bg).toLowerCase() !== "#ffffff",
        fg: hex(fg),
        bg: hex(bg),
        ratio: Math.round(ratio(fg, bg) * 1000) / 1000,
        fontSize: cs.fontSize,
        fontWeight: cs.fontWeight,
      };
    });
  }, menuSelector);
}

/** The WCAG floor for the size/weight of a row (large text is 3:1). */
function floorFor(r: { fontSize: string; fontWeight: string }): number {
  const px = parseFloat(r.fontSize);
  const bold = Number(r.fontWeight) >= 700;
  return px >= 24 || (bold && px >= 18.66) ? 3 : 4.5;
}

test.describe("workspace switcher's tinted active item", () => {
  test("the open menu is scanned, and its rows MEASURE above the contrast floor", async ({ page }) => {
    await stubAPI(page);
    await freshState(page);
    // The Shell — which renders the switcher — is the authenticated chrome, so this is a
    // dashboard route. `/` serves the marketing page to a browser with no session.
    await page.goto("/settings?tab=workspace", { waitUntil: "domcontentloaded" });
    const { menu } = await openSwitcher(page);

    // (1) REACHABILITY, asserted before anything is measured. A trigger or menu that
    // stops matching fails rather than skipping.
    await expect(menu).toBeVisible();

    const scope = '[role="menu"]';
    const rows = await measureMenuRows(page, scope);

    // (2) The active row must exist — without it the scan would report a clean 0 over a
    // menu that renders no tinted row (two workspaces are needed for it to render).
    expect(rows.length, "the switcher menu rendered no rows").toBeGreaterThan(0);
    const active = rows.filter((r) => r.onTint);
    expect(
      active.length,
      `no row is painted on the tint, so the TINTED ACTIVE item never rendered and this ` +
        `scan would be green over nothing. Rows measured: ${JSON.stringify(rows)}`,
    ).toBe(1);

    const tokens = await page.evaluate(() => {
      const cs = getComputedStyle(document.documentElement);
      return {
        primary: cs.getPropertyValue("--jf-primary-on-tint").trim(),
        muted: cs.getPropertyValue("--jf-muted-on-tint").trim(),
      };
    });
    expect(tokens.primary, "the --jf-primary-on-tint token is not defined").not.toBe("");
    expect(tokens.muted, "the --jf-muted-on-tint token is not defined").not.toBe("");

    // (3) THE MEASUREMENT. This is the half the unit guard cannot do: it reads the
    // component as text and cannot compute a painted colour.
    console.log(
      "[switcher rows] " +
        rows
          .map(
            (r) =>
              `${JSON.stringify(r.text)} fg=${r.fg} bg=${r.bg} ratio=${r.ratio}${r.onTint ? " ACTIVE" : ""}`,
          )
          .join("\n  "),
    );
    const below = rows
      .filter((r) => r.ratio < floorFor(r))
      .map((r) => `${JSON.stringify(r.text)} fg=${r.fg} bg=${r.bg} ratio=${r.ratio} (floor ${floorFor(r)})`);
    expect(
      below,
      `these switcher rows are below their WCAG contrast floor — the tinted active item ` +
        `shipped at 4.20:1 once and this is the measurement that catches it:\n  ${below.join("\n  ")}`,
    ).toEqual([]);

    // (4) The gate's axe rule, scoped to the OPEN MENU. `include()` makes axe THROW when
    // the subtree is absent, so this cannot pass vacuously either.
    const results = await new AxeBuilder({ page })
      .withTags(["wcag2aa", "wcag21aa"])
      .include(scope)
      .analyze();
    const blockers = blockersOf(results);
    expect(
      blockers.map((v) => `${v.id} (${v.impact}): ${v.help}`),
      "axe found critical/serious violations in the open switcher menu",
    ).toEqual([]);
  });

  test("CONTROL: with the PRE-FIX on-tint tokens the measurement drops below the floor", async ({
    page,
  }) => {
    // The non-vacuity arm the old script carried. If this spec cannot SEE the tinted
    // active item at all, the control stays above the floor and the test fails — which is
    // what makes the green reading above evidence rather than a coincidence.
    await stubAPI(page);
    await freshState(page);
    // The tokens are injected as an INIT script so the override is in place BEFORE the app
    // renders — `addStyleTag` needs a loaded document and would land after first paint.
    await page.addInitScript((css: string) => {
      const add = () => {
        const el = document.createElement("style");
        el.textContent = css;
        document.head.appendChild(el);
      };
      if (document.head) add();
      else document.addEventListener("DOMContentLoaded", add);
    }, PRE_FIX_TOKENS);
    await page.goto("/settings?tab=workspace", { waitUntil: "domcontentloaded" });
    const { menu } = await openSwitcher(page);
    await expect(menu).toBeVisible();

    const rows = await measureMenuRows(page, '[role="menu"]');
    // The control must move the ACTIVE row, so assert the tint is still there.
    expect(
      rows.filter((r) => r.onTint).length,
      `the tinted active row did not render under the override either, so this control ` +
        `proves nothing: ${JSON.stringify(rows)}`,
    ).toBe(1);
    console.log(
      "[switcher control] " +
        rows
          .map(
            (r) =>
              `${JSON.stringify(r.text)} fg=${r.fg} bg=${r.bg} ratio=${r.ratio}${r.onTint ? " ACTIVE" : ""}`,
          )
          .join("\n  "),
    );
    const below = rows.filter((r) => r.ratio < floorFor(r));
    expect(
      below.length,
      `with the PRE-FIX tokens in effect, NOTHING measured below the floor, so this spec ` +
        `cannot see the tinted active item at all and its green reading proves nothing. ` +
        `Rows measured: ${JSON.stringify(rows)}`,
    ).toBeGreaterThan(0);
  });
});
