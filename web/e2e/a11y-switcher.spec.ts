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
// THE ACTIVE ROW'S PROGRAMMATIC STATE (card t_89797869). This file used to carry the note
// that the active workspace was conveyed by COLOUR plus a `✓` glyph and nothing else, so
// the probe below had to identify the active row by MEASURING its painted background — the
// guard worked AROUND a WCAG 4.1.2 gap instead of pinning the property. That gap is fixed:
// the component sets `aria-current="true"` on the active row (the same `s.slug === value`
// predicate the tint and the glyph use, so the three cannot disagree), and the first arm
// below now asserts THAT attribute and cross-checks the tint against it. The contrast
// measurement is unchanged — it is a different property.
//
// Run with:
//   npm run a11y                                  # the whole gate (this file included)
//   npx playwright test e2e/a11y-switcher.spec.ts # this file alone

import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means — before
// the gate existed an anonymous visit rendered the account shell, so this spec
// could (and did) rely on the bug. See e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});
import { DICT } from "../src/components/i18nDict";
import { blockersOf, expectGateSeesAriaViolation, gateScan } from "./axeGate";

// The gate's own rule — the axe rule SET and the failing IMPACTS — is defined
// once, in `e2e/axeGate.ts`, and `e2e/a11y.spec.ts` runs the same two functions.
// Card t_5aa6c8ac: the local copy that used to sit here declared
// `withTags(["wcag2aa", "wcag21aa"])`, which reached 6 of axe's 105 rules and
// hid every A-level ARIA rule; the "identical to a11y.spec.ts" claim in this
// file's header was false. See axeGate.ts for the measurement.

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

/**
 * The workspace this file's browser state SELECTS (`freshState` writes `jf_workspace=demo`,
 * and the switcher marks `s.slug === value`), and the other one. Named here so the assertion
 * that the marked row is the ACTIVE workspace reads off the bundle rather than a literal —
 * a renamed fixture then moves both halves instead of silently agreeing with itself.
 */
const ACTIVE_WORKSPACE = BUNDLE.workspaces[0];
const INACTIVE_WORKSPACE = BUNDLE.workspaces[1];

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
        // The PAINT. Kept for the contrast property this file owns — and, since card
        // t_89797869, cross-checked against the row's programmatic state below rather
        // than being the only way to tell the active row apart.
        onTint: hex(bg).toLowerCase() !== "#ffffff",
        // The PROGRAMMATIC STATE, i.e. what assistive tech reads. `role` is reported
        // beside it so an arm that changes the widget role re-anchors this probe
        // instead of silently finding null.
        role: el.getAttribute("role"),
        ariaCurrent: el.getAttribute("aria-current"),
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
  test("the open menu is scanned, its active row is marked aria-current, and its rows MEASURE above the contrast floor", async ({ page }) => {
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

    // (2) THE PROGRAMMATIC STATE — the property this arm exists for since card t_89797869.
    // The active workspace used to be marked by COLOUR alone, so this probe could find it
    // only by measuring a painted background; the state is now read straight off the row,
    // and the paint is cross-checked AGAINST it in (3). If the two ever disagree, this fails
    // here rather than measuring whichever row happens to be tinted.
    const marked = rows.filter((r) => r.ariaCurrent === "true");
    expect(
      marked.length,
      `the switcher marks ${marked.length} row(s) with aria-current="true"; exactly the ACTIVE ` +
        `workspace must carry it, or a screen reader cannot tell which one is active (WCAG 4.1.2 — ` +
        `the defect card t_89797869 fixed). Rows measured: ${JSON.stringify(rows)}`,
    ).toBe(1);
    expect(
      marked[0].text,
      `the row marked aria-current="true" is ${JSON.stringify(marked[0].text)}, not the ACTIVE ` +
        `workspace the bundle describes (${JSON.stringify(ACTIVE_WORKSPACE.name)}) — the state is ` +
        `on the wrong row`,
    ).toContain(ACTIVE_WORKSPACE.name);
    expect(
      marked[0].role,
      `the marked row's role changed to ${JSON.stringify(marked[0].role)}. This arm pins ` +
        `role="menuitem" + aria-current="true"; if the widget deliberately moves to the ` +
        `"menuitemradio" + aria-checked variant, re-anchor this assertion rather than dropping it.`,
    ).toBe("menuitem");
    // A state carried by EVERY row discriminates nothing, so the inactive workspace must be
    // clean — without this, marking both rows would satisfy the assertion above.
    const inactive = rows.filter((r) => r.text.includes(INACTIVE_WORKSPACE.name));
    expect(
      inactive.length,
      `the inactive workspace (${INACTIVE_WORKSPACE.name}) did not render as its own row, so this ` +
        `arm cannot show that aria-current DISCRIMINATES: ${JSON.stringify(rows)}`,
    ).toBe(1);
    expect(
      inactive[0].ariaCurrent,
      `the INACTIVE workspace row carries aria-current=${JSON.stringify(inactive[0].ariaCurrent)} ` +
        `as well — a state on every row tells a screen reader nothing`,
    ).toBe(null);

    // (3) The PAINT must agree with the state: the tinted row and the marked row are the same
    // element. This is what keeps the contrast measurement below anchored to the row the app
    // itself calls active.
    const active = rows.filter((r) => r.onTint);
    expect(
      active.length,
      `no row is painted on the tint, so the TINTED ACTIVE item never rendered and this ` +
        `scan would be green over nothing. Rows measured: ${JSON.stringify(rows)}`,
    ).toBe(1);
    expect(
      active[0],
      `the row painted on the tint is NOT the row marked aria-current="true" — the visual and the ` +
        `programmatic state disagree. Marked: ${JSON.stringify(marked[0])}, tinted: ${JSON.stringify(active[0])}`,
    ).toBe(marked[0]);

    const tokens = await page.evaluate(() => {
      const cs = getComputedStyle(document.documentElement);
      return {
        primary: cs.getPropertyValue("--jf-primary-on-tint").trim(),
        muted: cs.getPropertyValue("--jf-muted-on-tint").trim(),
      };
    });
    expect(tokens.primary, "the --jf-primary-on-tint token is not defined").not.toBe("");
    expect(tokens.muted, "the --jf-muted-on-tint token is not defined").not.toBe("");

    // (4) THE MEASUREMENT. This is the half the unit guard cannot do: it reads the
    // component as text and cannot compute a painted colour.
    console.log(
      "[switcher rows] " +
        rows
          .map(
            (r) =>
              `${JSON.stringify(r.text)} role=${r.role} aria-current=${JSON.stringify(r.ariaCurrent)} ` +
              `fg=${r.fg} bg=${r.bg} ratio=${r.ratio}${r.onTint ? " ACTIVE(tint)" : ""}`,
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

    // (5) The gate's axe rule, scoped to the OPEN MENU. `include()` makes axe THROW when
    // the subtree is absent, so this cannot pass vacuously either — and the control at
    // the end proves the scan can SEE an A-level ARIA defect in this menu.
    const results = await gateScan(page, scope);
    const blockers = blockersOf(results);
    expect(
      blockers.map((v) => `${v.id} (${v.impact}): ${v.help}`),
      "axe found critical/serious violations in the open switcher menu",
    ).toEqual([]);

    // (6) NON-VACUITY OF (5), the half card t_5aa6c8ac exists for: plant a
    // prohibited ARIA state in THIS menu and require the same scan to name it.
    await expectGateSeesAriaViolation(page, scope, "the open switcher menu");
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
 * The route this arm drives. It is the card's own route (and the stubs below
 * describe a form), but the route is no longer load-bearing for the AXE half:
 * see `hidePageBehindRow` (card t_711cefb9). History of why it used to be:
 * on some dashboard pages the dropdown panel overlaps the page content and axe
 * then CANNOT determine the row's background at all — it returns the node in
 * `incomplete` with "Element's background color could not be determined because
 * it partially overlaps other elements" (elmPartiallyObscuring), so a
 * 0-violation reading there would be a scan that gave up, not a pass. On
 * `/form-settings` the page content under the panel used to leave axe able to
 * judge the row, which is what made the axe arm below real (measured both ways
 * on 2026-10-11, axe-core 4.13.0). "Used to" is the point: PR 1259's eighth tab
 * moved one tab button under the second line of this row's wrapped label and
 * the same arm went red in BOTH directions, because axe's sample must agree
 * across every text rect. The arm now clears the page behind the row and
 * asserts paint order directly, so a page-layout change under the panel can no
 * longer decide whether the guard can see its own subject.
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

/**
 * Take the PAGE out of axe's background sample behind the open menu.
 *
 * WHY THIS STEP EXISTS (card t_711cefb9). axe's `color-contrast` calls
 * `getBackgroundStack`, which samples EVERY text rect of the node and requires
 * the samples to AGREE; when they do not, axe does not judge the node at all and
 * files it under `incomplete` with `elmPartiallyObscuring` — "Element's
 * background color could not be determined because it partially overlaps other
 * elements". This row's label WRAPS onto two lines inside the 256px panel, so
 * its two samples are two different points of whatever page content sits under
 * the dropdown.
 *
 * MEASURED (2026-10-26, this host, axe-core as vendored here): on `origin/main`
 * the /form-settings tab strip is under the row and BOTH samples land on the
 * strip's `nav` — equal stacks, row judged, arm green. PR 1259 adds an 8th tab,
 * which moves a tab `button` under the SECOND line: sample 1 is `nav`, sample 2
 * is `button.px-3.py-2`, so the same pair went red in BOTH directions —
 * `incomplete` in the arm and 0 violations in its own CONTROL. The arm was
 * balanced on the page's layout, not on the ink it guards, and any future tab /
 * heading / card that moves under the panel breaks it again.
 *
 * THIS IS NOT A RELAXATION, and it cannot hide a contrast defect:
 *   * what the row's text is painted ON is the ROW ITSELF (its HOVER background
 *     is opaque — `hovered.bg` is asserted to be HOVER_SURFACE) over the menu
 *     panel, which is opaque too. Both are asserted here BEFORE anything is
 *     hidden: whatever paints under an opaque stack is not part of what the user
 *     sees, so the page cannot influence this row's contrast;
 *   * the real concern behind `incomplete` — "something is in FRONT of the row"
 *     — is asserted directly instead: at every sample point the row (or its own
 *     text node) must be the TOPMOST painted element, so a page element that
 *     really did cover the menu still fails this arm, and fails it loudly;
 *   * nothing inside the scanned subtree is touched: only elements that are
 *     neither the row, nor its descendant, nor part of the menu, nor an ancestor
 *     of the row are hidden;
 *   * `visibility: hidden`, never `display: none`, so the row cannot move — the
 *     measurement taken after this step is asserted identical to the one before
 *     it, and every assertion below (no color-contrast violation, no undetermined
 *     node, the CONTROL must still name the pre-fix pair) is unchanged.
 *
 * Returns what it did, so the run says which page element was in the way.
 */
async function hidePageBehindRow(page: Page, scope: string, label: string) {
  return page.evaluate(
    ({ sel, want }) => {
      const desc = (el: Element | null) =>
        el
          ? `${el.tagName.toLowerCase()}${el.id ? "#" + el.id : ""}.${(el.className || "")
              .toString()
              .split(" ")
              .slice(0, 2)
              .join(".")}`
          : "nothing";
      const opaque = (c: string) => {
        // The tints are emitted in whatever colour space Tailwind v4 chose
        // (`lab(...)` here), so the string can NOT be parsed as rgb(). Paint the
        // colour over white and over black instead: an OPAQUE colour composites
        // to the same bytes on both, a translucent one cannot.
        const cv = document.createElement("canvas");
        cv.width = cv.height = 1;
        const ctx = cv.getContext("2d", { willReadFrequently: true })!;
        const over = (base: string) => {
          ctx.clearRect(0, 0, 1, 1);
          ctx.fillStyle = base;
          ctx.fillRect(0, 0, 1, 1);
          ctx.fillStyle = c;
          ctx.fillRect(0, 0, 1, 1);
          const d = ctx.getImageData(0, 0, 1, 1).data;
          return `${d[0]},${d[1]},${d[2]}`;
        };
        return over("#ffffff") === over("#000000");
      };
      const menu = document.querySelector(sel);
      if (!menu) throw new Error(`no menu for ${sel} — the switcher moved`);
      const items = Array.from(menu.querySelectorAll('[role="menuitem"]'));
      const row = items.find((el) => (el.textContent || "").trim() === want) as HTMLElement | undefined;
      if (!row) throw new Error(`no menu row reading ${JSON.stringify(want)} — the row moved`);

      const menuBg = getComputedStyle(menu).backgroundColor;
      const rowBg = getComputedStyle(row).backgroundColor;
      if (!opaque(menuBg))
        throw new Error(
          `the menu panel's own background is ${menuBg} — not opaque, so what paints under it CAN ` +
            `reach the row and this step would be hiding evidence`,
        );
      if (!opaque(rowBg))
        throw new Error(
          `the hovered row's own background is ${rowBg} — not opaque, so what paints under it CAN ` +
            `reach the row's text and this step would be hiding evidence`,
        );

      // The sample points are the row's OWN text rects — exactly what axe samples.
      const rects: DOMRect[] = [];
      row.childNodes.forEach((n) => {
        if (n.nodeType !== 3) return;
        const r = document.createRange();
        r.selectNodeContents(n);
        Array.from(r.getClientRects()).forEach((q) => rects.push(q));
      });
      const rowRect = row.getBoundingClientRect();
      if (!rects.length) rects.push(rowRect);
      const points = rects.map(
        (r) => [Math.ceil(r.left + r.width / 2), Math.ceil(r.top + r.height / 2)] as [number, number],
      );

      // (a) PAINT ORDER, asserted rather than left to axe to give up on: the row
      // must be what is painted at every one of those points.
      const covering = points
        .map(([x, y]) => {
          const top = document.elementFromPoint(x, y);
          return top && (top === row || row.contains(top)) ? null : `${desc(top)} at (${x},${y})`;
        })
        .filter((v): v is string => v !== null);
      if (covering.length)
        throw new Error(
          `the open menu's row is painted UNDER page content at ${covering.join("; ")} — the dropdown ` +
            `is obscured, which is a real defect (and not the axe incompleteness this step removes)`,
        );

      const overlapsRow = (el: Element) => {
        const r = el.getBoundingClientRect();
        if (!r.width || !r.height) return false;
        return r.left < rowRect.right && r.right > rowRect.left && r.top < rowRect.bottom && r.bottom > rowRect.top;
      };
      const isMenuPart = (el: Element) => el === row || row.contains(el) || menu.contains(el) || el.contains(row);
      const painted = (el: Element) => {
        const cs = getComputedStyle(el);
        return cs.display !== "none" && cs.visibility !== "hidden" && Number(cs.opacity) !== 0;
      };

      const candidates: HTMLElement[] = [];
      document.querySelectorAll<HTMLElement>("body *").forEach((el) => {
        if (isMenuPart(el) || !painted(el) || !overlapsRow(el)) return;
        candidates.push(el);
      });
      const topmost = candidates.filter((el) => !candidates.some((other) => other !== el && other.contains(el)));
      topmost.forEach((el) => el.style.setProperty("visibility", "hidden"));

      // (b) After the hide, NOTHING painted outside the menu may overlap the row.
      const remaining: string[] = [];
      document.querySelectorAll<HTMLElement>("body *").forEach((el) => {
        if (isMenuPart(el) || !painted(el) || !overlapsRow(el)) return;
        remaining.push(desc(el));
      });

      return {
        hidden: topmost.map(desc),
        remaining,
        menuBg,
        rowBg,
        samples: points.length,
      };
    },
    { sel: scope, want: label },
  );
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

    // (3b) Clear the PAGE out of axe's background sample. Without this the pair
    // below is balanced on what happens to sit under the dropdown (card
    // t_711cefb9) — see `hidePageBehindRow` for the measurement and the reasons
    // this cannot hide a contrast defect.
    const behind = await hidePageBehindRow(page, scope, SET_DEFAULT_LABEL);
    console.log(
      `[switcher menu, hovered] cleared ${behind.hidden.length} page element(s) behind the row: ` +
        `${JSON.stringify(behind.hidden)} over ${behind.samples} text sample(s); ` +
        `menu bg=${behind.menuBg}, row bg=${behind.rowBg}`,
    );
    expect(
      behind.remaining,
      `page content still paints behind the hovered row after the clear (${JSON.stringify(behind.remaining)}): ` +
        `axe's background sample would be about the page again and this arm would be back on the layout ` +
        `knife-edge this guard exists to get off`,
    ).toEqual([]);
    const cleared = await measureMenuRow(page, scope, SET_DEFAULT_LABEL);
    expect(
      cleared,
      `clearing the page behind the menu changed the row's OWN measurement (before ${JSON.stringify(hovered)}, ` +
        `after ${JSON.stringify(cleared)}) — the hide moved the row, so the reading below is not the reading ` +
        `asserted above`,
    ).toEqual(hovered);

    // (4) The gate's axe rule, scoped to the open menu WITH THE ROW STILL
    // HOVERED — this is the exact run shape that reported the card's node.
    const results = await gateScan(page, scope);
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

    // (5) NON-VACUITY OF (4): the same scan must SEE an A-level ARIA defect in this
    // menu (card t_5aa6c8ac) — a clean reading from a rule set that excludes those
    // rules would be worth nothing.
    await expectGateSeesAriaViolation(page, scope, "the hovered switcher menu");
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

    // …and the same clear, for the same reason (card t_711cefb9): without it this
    // control reports 0 violations because axe never JUDGED the row, which reads
    // identically to "the pre-fix ink is fine".
    const behind = await hidePageBehindRow(page, scope, SET_DEFAULT_LABEL);
    console.log(
      `[switcher menu CONTROL, hovered] cleared ${behind.hidden.length} page element(s) behind the row: ` +
        `${JSON.stringify(behind.hidden)} over ${behind.samples} text sample(s); ` +
        `menu bg=${behind.menuBg}, row bg=${behind.rowBg}`,
    );
    expect(
      behind.remaining,
      `page content still paints behind the hovered row after the clear (${JSON.stringify(behind.remaining)}) — ` +
        `axe would not judge the row and this control would read "no violation" for a reason that has nothing ` +
        `to do with the pre-fix ink`,
    ).toEqual([]);
    const clearedControl = await measureMenuRow(page, scope, SET_DEFAULT_LABEL);
    expect(
      clearedControl,
      `clearing the page behind the menu changed the row's OWN measurement (before ${JSON.stringify(hovered)}, ` +
        `after ${JSON.stringify(clearedControl)}) — the forced pre-fix state being measured is not the one ` +
        `asserted above`,
    ).toEqual(hovered);

    // And the axe arm must NAME it: a scoped run over the same hovered row has to
    // report the color-contrast node with the pre-fix pair, or the arm above is
    // not the guard the card asks for.
    const results = await gateScan(page, scope);
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

    // …and the rule set behind this control can see an A-level defect in the same
    // subtree (card t_5aa6c8ac), not only the contrast rule it is aimed at.
    await expectGateSeesAriaViolation(page, scope, "the pre-fix hover menu");
  });
});
