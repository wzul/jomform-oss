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
import { DICT } from "../src/components/i18nDict";

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
  // The form-settings stubs (same set as e2e/form-settings-tabstrip-contrast.spec.ts):
  // the hover-state arm below drives `/form-settings?form=<id>`, which is the
  // route the card's own axe run used — see the note there for why the route
  // matters to axe.
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+$/, (r) =>
    r.fulfill(
      json({
        id: FORM_ID,
        slug: "demo-form",
        title: "Demo form",
        status: "published",
        url: "https://jomform.example/f/demo-form",
        mode: "payment",
        config: { blocks: [] },
        theme: {},
      }),
    ),
  );
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/chip-feeds(\?.*)?$/, (r) =>
    r.fulfill(json({ feeds: null })),
  );
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/confirmations(\?.*)?$/, (r) =>
    r.fulfill(json({ confirmations: [] })),
  );
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/notifications(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/menu(\?.*)?$/, (r) =>
    r.fulfill(json({ enabled: false, categories: [] })),
  );
  await page.route(/(.*)\/api\/rest\/webhooks(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/products(\?.*)?$/, (r) => r.fulfill(json([])));
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
      sessionStorage.clear();
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
 *
 * The click is RETRIED until the app reports `aria-expanded="true"`, because the
 * trigger is visible in the server-rendered HTML BEFORE React hydrates: a click
 * that lands in that window is a no-op, and clicking blind was measured as a
 * FLAKE on this file (a run where the menu never opened and `waitFor` timed out
 * after 30s on the first test a worker ran). `aria-expanded` is the app's own
 * truth for "the menu is open", so nothing here depends on timing.
 */
async function openSwitcher(page: Page) {
  const trigger = page.locator('button[aria-haspopup="menu"]').first();
  await trigger.waitFor({ state: "visible", timeout: 30_000 });
  await expect
    .poll(
      async () => {
        if ((await trigger.getAttribute("aria-expanded")) !== "true") await trigger.click();
        return trigger.getAttribute("aria-expanded");
      },
      { timeout: 30_000, message: "the switcher never reported aria-expanded=true after a click" },
    )
    .toBe("true");
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

// ---------------------------------------------------------------------------
// Card t_9d8f0431 — the "Make this the default workspace" row, while HOVERED.
//
// THE DEFECT (measured by axe on a production build, not arithmetic)
//   The row's two STATES paint on two different surfaces, and only one of them
//   was ever measured:
//
//     resting  --jf-muted   #6b7280 on white   #ffffff = 4.834:1  PASS
//     hovered  --jf-primary #5e6ad2 on slate-50 #f8fafc = 4.492:1  FAIL
//
//   axe names the hovered node (`.hover\:text-\[var\(--jf-primary\)\]`) with
//   "insufficient color contrast of 4.49 (foreground color: #5e6ad2, background
//   color: #f8fafc, font size: 10.5pt (14px), font weight: normal)". The
//   background the HOVER introduces is what drops the pair under the 4.5 floor —
//   the same mechanism as the builder canvas (card t_5f315891) — and the arm
//   above this one never saw it, because it measures the rows at REST. axe only
//   reads `:hover` colours if the element is actually hovered while it scans,
//   which is why this arm drives a REAL pointer onto the row first.
//
//   Nothing in the repo caught it: `src/lib/tintedSurface.test.ts` is scoped to
//   solid `bg-indigo-50` (a `bg-slate-50` surface is outside that rule), and a
//   source scan cannot measure a 0.008 margin anyway.
//
// THE FIX — the TOKEN, per state, not a class swap
//   The hover ink moves to the existing `--jf-primary-on-tint` (#4338ca, 7.554:1
//   on slate-50), while the resting ink STAYS `--jf-muted` on white — a naive
//   swap of the whole class list would have darkened the resting state and
//   regressed a state that already passed, so both are asserted below.
//
// WHY IT BITES
//   1. The row is found by the app's own dictionary label; if the row or the
//      menu disappears the test FAILS rather than skipping.
//   2. Both states are MEASURED from the live DOM (colours round-tripped through
//      a canvas: Tailwind v4 does not emit these as rgb()), and the hover state
//      is asserted to have actually applied (the painted ink and surface must
//      MOVE) — a spec that measured the resting state twice would be green while
//      claiming to cover a hover.
//   3. axe is scoped to the open menu WITH THE ROW HOVERED, and the
//      `color-contrast` rule is asserted to have actually run.
//   4. The CONTROL re-creates the pre-fix artifact: with the pre-fix hover ink
//      forced back onto `:hover`, the measurement must drop to the pre-fix
//      4.492:1 AND axe must name the node again. Without it, a spec that
//      accidentally measured another element would stay green.
// ---------------------------------------------------------------------------

/** The row under this guard, read from the app's own dictionary (default locale). */
const SET_DEFAULT_LABEL = DICT["workspace.setDefault"].en;

/**
 * The route this arm drives. It is the card's own route, and the route matters:
 * on some dashboard pages the dropdown panel overlaps the page content and axe
 * then CANNOT determine the row's background at all — it returns the node in
 * `incomplete` with "Element's background color could not be determined because
 * it partially overlaps other elements" (elmPartiallyObscuring), so a
 * 0-violation reading there would be a scan that gave up, not a pass. On
 * `/form-settings` the panel overlaps nothing that defeats the analysis and axe
 * judges the row, which is what makes the axe arm below real. Measured both
 * ways on 2026-10-11 (axe-core 4.13.0) while writing this guard.
 */
const FORM_ID = "form_hover_guard";

/** The surface the row's HOVER paints (Tailwind `bg-slate-50`). */
const HOVER_SURFACE = "#f8fafc";
/** The row's resting surface: the dropdown panel is `bg-white`. */
const RESTING_SURFACE = "#ffffff";
/** The row's resting ink, `--jf-muted`. */
const RESTING_INK = "#6b7280";
/** The PRE-FIX hover ink, `--jf-primary`, and what it measures on HOVER_SURFACE. */
const PRE_FIX_INK = "#5e6ad2";
const PRE_FIX_HOVER_RATIO = 4.492;
/** The fix's hover ink, `--jf-primary-on-tint`. */
const FIXED_INK = "#4338ca";

/**
 * The CONTROL: the pre-fix hover ink forced back onto the hover state of every
 * menu item, so the pre-fix artifact is re-created in the live DOM (the same
 * shape card t_4046f900 used for its own control).
 */
const PRE_FIX_HOVER_CSS = `[role="menu"] [role="menuitem"]:hover{color:${PRE_FIX_INK} !important;}`;

type MenuRow = {
  /** Index among the menu's `role="menuitem"` elements, so the row can be hovered. */
  index: number;
  text: string;
  fg: string;
  bg: string;
  ratio: number;
  fontSize: string;
  fontWeight: string;
  /** The row's own computed `background-color` — transparent unless hovered. */
  ownBg: string;
  all: number;
};

/**
 * Measure ONE row of the open menu, in whatever state the pointer has left it
 * in. Throws when the row is absent, so a moved/renamed row fails loudly instead
 * of being measured as nothing. Same canvas round-trip as the arm above, and the
 * background is the COMPOSITED pixel (every ancestor's colour painted in order),
 * so a translucent layer composites the way the renderer paints it.
 */
async function measureMenuRow(page: Page, scope: string, label: string): Promise<MenuRow> {
  return page.evaluate(
    ({ sel, want }) => {
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
      const ratio = (
        a: { r: number; g: number; b: number },
        b: { r: number; g: number; b: number },
      ) => {
        const [x, y] = [lum(a), lum(b)];
        return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
      };

      const menu = document.querySelector(sel);
      if (!menu) throw new Error(`no menu for ${sel} — the switcher moved`);
      const items = Array.from(menu.querySelectorAll('[role="menuitem"]'));
      const index = items.findIndex((el) => (el.textContent || "").trim() === want);
      if (index < 0) {
        throw new Error(
          `no menu row reading ${JSON.stringify(want)} — the row moved. Rows: ${JSON.stringify(
            items.map((el) => (el.textContent || "").trim()),
          )}`,
        );
      }
      const el = items[index];
      const cs = getComputedStyle(el);
      const layers: string[] = [];
      for (let n: Element | null = el; n; n = n.parentElement) {
        const c = getComputedStyle(n).backgroundColor;
        if (c !== "rgba(0, 0, 0, 0)" && c !== "transparent") layers.push(c);
        if (n === document.body) break;
      }
      let bg = { r: 255, g: 255, b: 255 };
      for (let i = layers.length - 1; i >= 0; i--)
        bg = paint(`rgb(${bg.r}, ${bg.g}, ${bg.b})`, layers[i]);
      const fg = paint(`rgb(${bg.r}, ${bg.g}, ${bg.b})`, cs.color);
      return {
        index,
        text: (el.textContent || "").trim(),
        fg: hex(fg),
        bg: hex(bg),
        ratio: Math.round(ratio(fg, bg) * 1000) / 1000,
        fontSize: cs.fontSize,
        fontWeight: cs.fontWeight,
        ownBg: cs.backgroundColor,
        all: items.length,
      };
    },
    { sel: scope, want: label },
  );
}

/** The menu's rows, by the selector this spec hovers through. */
function rowAt(page: Page, scope: string, index: number) {
  return page.locator(`${scope} [role="menuitem"]`).nth(index);
}

test.describe("workspace switcher's default-workspace row (hover state)", () => {
  test("the row clears AA in BOTH states, and axe sees no color-contrast while it is hovered", async ({
    page,
  }) => {
    test.setTimeout(120_000);
    await stubAPI(page);
    await freshState(page);
    await page.goto(`/form-settings?form=${FORM_ID}`, { waitUntil: "domcontentloaded" });
    const { menu } = await openSwitcher(page);
    await expect(menu).toBeVisible();

    // The DEFAULT locale, asserted before measuring: a `jf_lang` left behind by an
    // earlier run would change the row's label and the row would not be found —
    // or worse, be found as a different string.
    expect(await page.evaluate(() => document.documentElement.lang)).toBe("en");

    const scope = '[role="menu"]';

    // (1) RESTING. This is the state that already passed, and the one a naive
    // whole-class-list swap would regress: it must still be --jf-muted on the
    // panel's white.
    const resting = await measureMenuRow(page, scope, SET_DEFAULT_LABEL);
    console.log(
      `[switcher setDefault row RESTING] fg=${resting.fg} on bg=${resting.bg} — ${resting.ratio}:1 ` +
        `(${resting.fontSize}/${resting.fontWeight}), ownBg=${resting.ownBg}, rows=${resting.all}`,
    );
    expect(resting.text).toBe(SET_DEFAULT_LABEL);
    expect(resting.bg, "the row's RESTING surface moved — this guard is anchored to the panel's white").toBe(
      RESTING_SURFACE,
    );
    expect(
      resting.fg,
      `the resting ink changed to ${resting.fg}; it must stay --jf-muted (${RESTING_INK}), which ` +
        `measures ${resting.ratio}:1 on white. The hover fix is a HOVER-only token change.`,
    ).toBe(RESTING_INK);
    expect(resting.ratio, `the RESTING row measures ${resting.ratio}:1 — below the AA floor`).toBeGreaterThanOrEqual(
      4.5,
    );

    // (2) HOVERED. A REAL pointer onto the row: axe only reads `:hover` colours
    // if the element is hovered while it scans, and so does this measurement.
    await rowAt(page, scope, resting.index).hover();
    const hovered = await measureMenuRow(page, scope, SET_DEFAULT_LABEL);
    console.log(
      `[switcher setDefault row HOVERED] fg=${hovered.fg} on bg=${hovered.bg} — ${hovered.ratio}:1 ` +
        `(${hovered.fontSize}/${hovered.fontWeight}), ownBg=${hovered.ownBg}`,
    );

    // The hover must have actually APPLIED, or the rest of this test is measuring
    // the resting state twice and its green reading proves nothing.
    expect(
      hovered.bg,
      `hovering the row did not paint the hover surface: measured bg=${hovered.bg}, ownBg=${hovered.ownBg} ` +
        `(expected ${HOVER_SURFACE}). If :hover stopped applying, this arm is measuring the resting state ` +
        `and cannot see the defect it exists for.`,
    ).toBe(HOVER_SURFACE);
    expect(hovered.fg, "the hover did not change the ink, so the hover state never rendered").not.toBe(
      resting.fg,
    );

    // (3) THE MEASUREMENT, with the pre-fix value pinned so a rounded number
    // (4.492 -> 4.49, "close enough") cannot be argued into a pass.
    expect(
      hovered.fg,
      `the HOVERED row paints ${hovered.fg} on ${hovered.bg} (${hovered.ratio}:1). It must use ` +
        `--jf-primary-on-tint (${FIXED_INK}, 7.554:1 here): the base --jf-primary ${PRE_FIX_INK} measures ` +
        `${PRE_FIX_HOVER_RATIO}:1 on ${HOVER_SURFACE} — under the AA floor, which is what axe named.`,
    ).toBe(FIXED_INK);
    expect(
      hovered.ratio,
      `the HOVERED row measures ${hovered.ratio}:1 — under the 4.5:1 AA floor (pre-fix ${PRE_FIX_HOVER_RATIO}:1)`,
    ).toBeGreaterThanOrEqual(4.5);
    expect(
      hovered.ratio,
      `the HOVERED row measures ${hovered.ratio}:1, which is not above the pre-fix ${PRE_FIX_HOVER_RATIO}:1 — ` +
        `the base token is still in the hover state`,
    ).toBeGreaterThan(PRE_FIX_HOVER_RATIO);

    // (4) The gate's axe rule, scoped to the open menu WITH THE ROW STILL
    // HOVERED — this is the exact run shape that reported the card's node.
    const results = await new AxeBuilder({ page })
      .withTags(["wcag2aa", "wcag21aa"])
      .include(scope)
      .analyze();
    const contrast = results.violations.filter((v) => v.id === "color-contrast");
    if (contrast.length > 0) {
      console.log(
        `\n[switcher menu, hovered] color-contrast:\n` +
          contrast
            .map((v) => v.nodes.map((n) => `  ${n.target.join(" ")} — ${n.failureSummary ?? ""}`).join("\n"))
            .join("\n"),
      );
    }
    expect(
      contrast.map((v) => v.impact),
      "the open switcher menu, with the default-workspace row hovered, must have no color-contrast violation",
    ).toEqual([]);
    expect(blockersOf(results).map((v) => `${v.id} (${v.impact})`), "critical/serious in the hovered menu").toEqual(
      [],
    );

    // Both arms above are vacuous if axe never evaluated the rule here.
    const ran = [...results.passes, ...results.incomplete, ...results.violations].some(
      (r) => r.id === "color-contrast",
    );
    expect(
      ran,
      `axe never evaluated color-contrast over the hovered menu (${results.passes.length} passing rule-nodes, ` +
        `${results.incomplete.length} incomplete) — a 0-violation reading from a scan that did not run the rule ` +
        `is vacuous`,
    ).toBe(true);

    // …and they are EQUALLY vacuous if axe evaluated the rule but could not
    // determine the row's background: `incomplete` is "not judged", not "clean".
    // Measured on /settings?tab=workspace (where the panel overlaps page content)
    // axe returns this very row as incomplete with `elmPartiallyObscuring`, so a
    // 0-violation reading from that route would have proved nothing. This guard is
    // driven on the route where axe CAN judge the row, and it fails loudly if that
    // stops being true.
    const undetermined = results.incomplete
      .filter((r) => r.id === "color-contrast")
      .flatMap((r) =>
        r.nodes.map((n) => ({ target: n.target.join(" "), summary: n.failureSummary ?? "" })),
      )
      .filter((n) => /background color could not be determined/.test(n.summary));
    expect(
      undetermined,
      `axe ran color-contrast over the open menu but could NOT determine the background of ${undetermined.length} ` +
        `node(s), so it did not judge them — and an unjudged node is not a passing one:\n  ` +
        undetermined.map((n) => `${n.target} — ${n.summary}`).join("\n  ") +
        `\nThis is the axe ` +
        `elmPartiallyObscuring` +
        ` case (the dropdown panel overlapping the page content, measured on ` +
        `/settings?tab=workspace). Re-anchor this arm to a route where axe can judge the row rather than ` +
        `accepting the 0-violation reading.`,
    ).toEqual([]);
  });

  test("CONTROL: the PRE-FIX hover ink still drops below the floor and axe still names it", async ({
    page,
  }) => {
    // The non-vacuity arm. If this spec cannot SEE the pre-fix state, the
    // measurement stays above the floor and the axe arm stays clean — and the
    // green reading in the test above would prove nothing.
    test.setTimeout(120_000);
    await stubAPI(page);
    await freshState(page);
    // Injected as an INIT script so the override is in place before first paint,
    // exactly like the token control in the arm above. `!important` beats the
    // utility: specificity alone would lose to the hover utility's own `:hover`.
    await page.addInitScript((css: string) => {
      const add = () => {
        const el = document.createElement("style");
        el.textContent = css;
        document.head.appendChild(el);
      };
      if (document.head) add();
      else document.addEventListener("DOMContentLoaded", add);
    }, PRE_FIX_HOVER_CSS);
    await page.goto(`/form-settings?form=${FORM_ID}`, { waitUntil: "domcontentloaded" });
    const { menu } = await openSwitcher(page);
    await expect(menu).toBeVisible();

    const scope = '[role="menu"]';
    const resting = await measureMenuRow(page, scope, SET_DEFAULT_LABEL);
    await rowAt(page, scope, resting.index).hover();
    const hovered = await measureMenuRow(page, scope, SET_DEFAULT_LABEL);
    console.log(
      `[switcher setDefault row CONTROL, hovered] fg=${hovered.fg} on bg=${hovered.bg} — ${hovered.ratio}:1 ` +
        `(pre-fix ${PRE_FIX_HOVER_RATIO}:1)`,
    );

    // The override must have landed on the hovered row, and the row must still be
    // the row this guard owns.
    expect(hovered.text).toBe(SET_DEFAULT_LABEL);
    expect(hovered.bg, "the control did not land on the hover surface either").toBe(HOVER_SURFACE);
    expect(hovered.fg, "the forced pre-fix ink did not apply — this control proves nothing").toBe(PRE_FIX_INK);
    // The pre-fix number, re-measured: this is the defect, reproduced.
    expect(
      hovered.ratio,
      `with the pre-fix hover ink forced on, the row measured ${hovered.ratio}:1 — this control cannot ` +
        `re-create the ${PRE_FIX_HOVER_RATIO}:1 defect, so it proves nothing about the fix`,
    ).toBeLessThan(4.5);
    expect(
      Math.abs(hovered.ratio - PRE_FIX_HOVER_RATIO),
      `the control measured ${hovered.ratio}:1, not the pre-fix ${PRE_FIX_HOVER_RATIO}:1 — the card's number ` +
        `is not reproduced`,
    ).toBeLessThanOrEqual(0.005);

    // And the axe arm must NAME it: a scoped run over the same hovered row has to
    // report the color-contrast node with the pre-fix pair, or the arm above is
    // not the guard the card asks for.
    const results = await new AxeBuilder({ page })
      .withTags(["wcag2aa", "wcag21aa"])
      .include(scope)
      .analyze();
    const contrast = results.violations.filter((v) => v.id === "color-contrast");
    const summaries = contrast.flatMap((v) => v.nodes.map((n) => n.failureSummary ?? ""));
    console.log(
      `\n[switcher menu CONTROL] color-contrast:\n` +
        (contrast.length === 0
          ? "  (none reported — the control is vacuous)"
          : contrast
              .map((v) => v.nodes.map((n) => `  ${n.target.join(" ")} — ${n.failureSummary ?? ""}`).join("\n"))
              .join("\n")),
    );
    expect(
      contrast.length,
      `with the pre-fix hover ink in effect, axe reported NO color-contrast violation on the open menu. The ` +
        `card's node is live on the pre-fix artifact, so a clean reading here means this scan cannot see the ` +
        `hovered row at all and the arm above proves nothing.`,
    ).toBeGreaterThan(0);
    const named = summaries.some(
      (s) => s.includes(PRE_FIX_INK) && s.includes(HOVER_SURFACE) && /4\.49/.test(s),
    );
    expect(
      named,
      `axe reported color-contrast, but none of its summaries named the pre-fix pair (foreground ${PRE_FIX_INK} ` +
        `on background ${HOVER_SURFACE}, 4.49). Summaries: ${JSON.stringify(summaries)}`,
    ).toBe(true);
  });
});
