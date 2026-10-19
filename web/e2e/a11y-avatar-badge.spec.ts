// Measured contrast guard for the two AVATAR-INITIAL badges on `bg-indigo-100`.
//
// WHY THIS FILE EXISTS (card t_86755e43)
//   The tinted-surface guard owns SOLID `bg-indigo-50` (#eef2ff) and its own
//   scope note records `bg-indigo-100` as "a DIFFERENT, darker tint (3.81:1 with
//   --jf-primary) … tracked separately, not folded in". This is the separate
//   owner. Both sites are the initial-letter circle a user without an uploaded
//   avatar sees, and both painted the BASE `--jf-primary` on Tailwind indigo-100:
//
//     web/src/components/Shell.tsx:75        the sidebar account badge (text-xs)
//     web/src/app/profile/page.tsx:487       the 80px identity badge (text-2xl)
//
//   MEASURED on the real values (`--color-indigo-100` is `oklch(93% 0.034
//   272.788)` in `tailwindcss/theme.css`, i.e. #e0e7ff):
//
//     --jf-primary         #5e6ad2 on #e0e7ff = 3.814:1  <- under the 4.5 floor
//     --jf-primary-on-tint #4338ca on #e0e7ff = 6.414:1
//     --jf-muted           #6b7280 on #e0e7ff = 3.924:1  <- under the 4.5 floor
//     --jf-muted-on-tint   #5b6472 on #e0e7ff = 4.855:1
//
// THE DECISION, PER SITE (the card asked for one, and the two sites differ)
//   1. THE INITIAL IS NOT DECORATION, at either site. WCAG defines "pure
//      decoration" as "serving only an aesthetic purpose, providing no
//      information, and having no functionality", with the note that text "is
//      only purely decorative if the words can be rearranged or substituted
//      without changing their purpose"
//      (https://www.w3.org/WAI/WCAG21/Understanding/contrast-minimum.html#dfn-pure-decoration).
//      Substituting another letter changes whose account this is — the initial is
//      the text stand-in for the avatar image whose `alt` is the user's name — so
//      no decoration exemption is claimed and the SC applies.
//   2. THE FLOOR IS PER SITE, because the SIZES differ. WCAG large-scale text is
//      "at least 18 point or 14 point bold", and the same Understanding page
//      gives the conversion: "The ratio between sizes in points and CSS pixels is
//      1pt = 1.333px, therefore 14pt and 18pt are equivalent to approximately
//      18.5px and 24px."
//        * Shell badge    text-xs  = 12px -> NORMAL text, floor 4.5 -> 3.814
//          FAILED. Fixed to `--jf-primary-on-tint` (card t_86755e43), which
//          measures 6.414:1 on this tint.
//        * /profile badge text-2xl = 24px = 18pt -> LARGE text, floor 3.0 -> 3.814
//          PASSES. Its token is deliberately UNCHANGED: swapping a colour to
//          satisfy a floor the text is not subject to is not a fix. Arm 2 pins
//          that verdict BY MEASUREMENT, and `src/lib/indigo100Tint.test.ts`
//          requires the large-scale size to be declared on the element, so the
//          pass cannot survive the badge being resized smaller.
//   3. `--jf-primary` itself is NOT darkened — that was already rejected as a
//      decision because it shifts every other use of the token.
//
// WHY IT IS A MEASUREMENT AND NOT ONLY THE SOURCE GUARD
//   `src/lib/indigo100Tint.test.ts` reads the tree; it cannot paint a pixel. Both
//   arms below read the ELEMENT's own computed colour and the RENDERER's
//   composite through a canvas (the technique `builder-canvas-price-contrast.spec.ts`,
//   `a11y-switcher.spec.ts` and `a11y-admin-feedback-row.spec.ts` use) — Tailwind
//   emits these colours in oklab/oklch, so a class-name reading or a rounded
//   number would report the defect as a pass.
//
// HOW THE AUTHENTICATED CHROME IS REACHED
//   The badges live behind the dashboard chrome, and both routes are gated on the
//   SERVER now (`web/src/middleware.ts`): a request that carries no `jf_session`
//   cookie is redirected to /login BEFORE the page renders. So this spec DECLARES
//   the session it means, through the shared `signIn()` in `e2e/auth.ts` (added in
//   main's re-cut of this file — card t_44c9a306 — after the server gate landed
//   and the three arms below were measured landing on /login instead). The gate
//   reads PRESENCE only and never the value, and the READS are still stubbed at
//   the browser level (the pattern `a11y-dialogs.spec.ts` and `a11y-switcher.spec.ts`
//   use): no Go server, no database, and the REAL built page renders in a REAL
//   browser. The stubbed `me` carries NO `avatar_url`, which is the only state in
//   which the initial-letter fallback renders at all.
//
// WHAT MAKES IT BITE
//   1. The badge must EXIST and be painted on the tint: the measured surface is
//      asserted to BE #e0e7ff (not merely "not white"), so a scan of an element
//      that lost the tint fails instead of reporting a clean 0.
//   2. The floor is derived from the MEASURED font size, exactly as axe derives
//      it, rather than asserted from a class name.
//   3. The CONTROL arm reverts `--jf-primary-on-tint` to the base token the site
//      carried before this card and requires the sidebar badge to measure BELOW
//      the floor, with axe not clearing that paint (see below on what axe can
//      and cannot do here). If that arm stays above the floor, this spec cannot
//      see the defect class at all and arm 1's green proves nothing.
//   4. The /profile arm refuses a THIRD colour: the badge's measured foreground
//      must be one of the two vetted tokens, and when it is the BASE one the
//      measured size must be >= 24px — i.e. the large-text justification is
//      re-derived from the live DOM instead of remembered.
//   5. THE INITIAL IS WAITED FOR, never measured on first paint (card t_129fdb48).
//      `Shell.tsx:176` paints the tinted circle from its FIRST render and only
//      fills the letter once the session bundle's `me` lands (`session.tsx:427`,
//      the /api/rest/dashboard-bundle read — the client keeps NO per-page
//      /api/auth/me round-trip), so "tinted" and "showing the initial" are TWO
//      different states. Waiting for the ELEMENT returned in the first one, and
//      the text assertion then measured `?`: on tree b7f0eca1 one full a11y gate
//      run failed exactly this way and the re-run of the SAME bytes passed
//      (measured text="?", surface=#e0e7ff, ratio=6.414). `assertTinted` now
//      polls the MEASUREMENT — bounded, see INITIAL_WAIT_MS — until the badge
//      shows a letter, and the slow-session arm delays the stub so the wait
//      itself is what the suite exercises instead of host load.
//
// AXE CANNOT JUDGE THESE TWO SITES — MEASURED, AND WHY THE MEASUREMENT IS THE PROOF
//   Both badges render a SINGLE CHARACTER, and axe-core refuses a verdict on that
//   shape: measured on this tree, with the pre-fix paint in place, its
//   color-contrast node for the badge lands in `incomplete` with "Element content
//   is too short to determine if it is actual text content" — not in
//   `violations`. The a11y gate's own rule is critical/serious violations, so a
//   single-character contrast failure is invisible to the axe family; the same
//   element PASSES when the paint is correct, which is the difference the control
//   arm measures. That is why the ratio here is computed from the PAINTED PIXEL
//   (`MEASURE`, through a canvas, so the renderer composites Tailwind's oklab
//   tint) and why the control asserts "axe does not clear it" rather than a
//   violation message axe would never produce for this content.
//
// SCOPE BOUNDARIES (deliberate, not oversights)
//   * Solid `bg-indigo-50` (the page-level chips) — `src/lib/tintedSurface.test.ts`
//     and PR 710. This file neither covers nor weakens it.
//   * Translucent `bg-indigo-50/NN` — measured per site (card t_2f2ad15a).
//   * `border-indigo-100` on /landing, /admin and /feedback paints no surface
//     under any text, so it is not a text-contrast site at all.
//
// Run with:
//   npm run a11y                                            # the whole gate (this file included)
//   npx playwright test e2e/a11y-avatar-badge.spec.ts       # this file alone

import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import type { AxeResults, Result } from "axe-core";

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means — before
// the gate existed an anonymous visit rendered the account shell, so this spec
// could (and did) rely on the bug. See e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

// The gate's own rule, kept identical to e2e/a11y.spec.ts so the two cannot drift.
const IMPACTS = ["critical", "serious"] as const;

function blockersOf(results: AxeResults): Result[] {
  return results.violations.filter((v) =>
    IMPACTS.includes((v.impact ?? "") as (typeof IMPACTS)[number]),
  );
}

/** The two tokens this surface is allowed to be painted with. */
const BASE_TOKEN = "#5e6ad2"; // --jf-primary
const ON_TINT_TOKEN = "#4338ca"; // --jf-primary-on-tint

/** The tint under test, as the renderer composites it (asserted, never assumed). */
const TINT = "#e0e7ff";

/** The sidebar account badge: the avatar inside the desktop aside's /profile link. */
const SHELL_BADGE = 'aside a[href="/profile"] div.bg-indigo-100';
/** The identity badge on /profile: the only 80px tinted circle on the page. */
const PROFILE_BADGE = "div.h-20.bg-indigo-100";

/**
 * The pre-fix paint: `--jf-primary-on-tint` was the base colour before the fix,
 * with the same tint re-spelled as `rgb()` FOR AXE ONLY.
 *
 * WHY THE RE-SPELLING. Measured on this tree: Tailwind v4 emits indigo-100 as
 * oklab, so the badge's computed background arrives as
 * `lab(91.6577 1.04591 -12.7199)`, and axe-core answers `color-contrast` on that
 * element with `incomplete` (cantTell) instead of a verdict — it cannot do the
 * arithmetic, which is exactly why the canvas measurement is the proof here.
 * For the axe half of the control the surface is therefore pinned to the SAME
 * colour in a form axe can read, and the measurement below asserts the painted
 * surface did not move, so both halves stay comparable.
 */
const PRE_FIX_TOKENS =
  `:root{--jf-primary-on-tint:${BASE_TOKEN};}\n` +
  `${SHELL_BADGE}{background-color:rgb(224, 231, 255) !important;}`;


const ME = { id: "u1", email: "ops@example.com", name: "Ops", lang: "" };

/**
 * The bundle an authenticated merchant sees. `me` carries NO `avatar_url` on
 * purpose — that is the state in which `UserAvatar` (Shell) and the /profile
 * identity badge paint the INITIAL LETTER instead of the picture.
 */
const BUNDLE = {
  me: ME,
  workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
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
 * Every /api/* read the chrome and the two pages make, stubbed in the browser.
 *
 * `bundleDelayMs` holds the SESSION read (dashboard-bundle, the one that carries
 * `me`) open for that long, which is how the slow-session arm below makes the
 * "tinted, still `?`" window deterministic instead of waiting for a loaded host.
 */
async function stubAPI(page: Page, opts: { bundleDelayMs?: number } = {}) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST, specific routes AFTER it: Playwright resolves matching
  // routes in reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, async (r) => {
    if (opts.bundleDelayMs) await new Promise((res) => setTimeout(res, opts.bundleDelayMs));
    await r.fulfill(json(BUNDLE));
  });
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(ME)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/rest\/currency\/rates$/, (r) =>
    r.fulfill(json({ rates: [], cached: false })),
  );
}

/**
 * A FRESH browser state at the app's DEFAULT locale: storage cleared and NO
 * `jf_lang` written, so these pages render what a new visitor gets. The browser
 * keeps localStorage between runs, and a `jf_lang` left behind by an earlier run
 * has already produced a false reading on this board.
 */
async function freshState(page: Page) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
    } catch {
      /* storage unavailable */
    }
  });
}

/**
 * The measurement, in the page: paint every ancestor layer back-to-front through
 * a canvas so the RENDERER composites the tint, then ratio the MEASURED surface
 * against the element's own computed text colour.
 */
const MEASURE = (sel: string) => {
  const el = document.querySelector(sel);
  if (!el) throw new Error(`no element for ${sel} — the badge moved or never rendered`);
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
    surface: hex(bg),
    fg: hex(fg),
    ratio: Math.round(ratio(fg, bg) * 1000) / 1000,
    fontSize: cs.fontSize,
    fontWeight: cs.fontWeight,
    layers,
  };
};

type Measured = {
  text: string;
  surface: string;
  fg: string;
  ratio: number;
  fontSize: string;
  fontWeight: string;
  layers: string[];
};

/** The WCAG floor for the size/weight of the measured text (large text is 3:1). */
function floorFor(m: Measured): number {
  const px = parseFloat(m.fontSize);
  const bold = Number(m.fontWeight) >= 700;
  return px >= 24 || (bold && px >= 18.66) ? 3 : 4.5;
}

/**
 * Open a dashboard route at a FRESH, DEFAULT-LOCALE state and return after the
 * document has rendered. Both assertions are the card's own rule: the locale a
 * new visitor gets is asserted, not assumed.
 *
 * `bundleDelayMs` is passed through to `stubAPI` for the one arm that has to
 * prove the badge's initial is WAITED FOR rather than measured on first paint.
 */
async function openFresh(page: Page, path: string, opts: { bundleDelayMs?: number } = {}) {
  await stubAPI(page, opts);
  await freshState(page);
  await page.goto(path, { waitUntil: "domcontentloaded" });
  expect(
    await page.evaluate(() => window.localStorage.getItem("jf_lang")),
    "this run must start with NO stored locale (fresh state)",
  ).toBeFalsy();
  await expect(page.locator("html")).toHaveAttribute("lang", "en");
}

/**
 * How long the badge may take to fill in its initial before the measurement
 * gives up. BOUNDED on purpose: an unbounded poll converts a false RED into a
 * hang, which is worse to receive, and a wait for the expected state must still
 * FAIL when that state never arrives. The stubbed session read resolves in
 * milliseconds, so this bound is orders of magnitude above the measured gap —
 * it exists to fail, not to accommodate a slow build.
 */
const INITIAL_WAIT_MS = 10_000;

/**
 * Wait, BOUNDED, for the badge to show a real initial instead of the
 * pre-session `?` placeholder, and return the measurement taken in THAT state.
 *
 * WHY (card t_129fdb48): `Shell.tsx:176` renders the tinted circle from the
 * first paint and fills the letter from the session bundle (`session.tsx:427`),
 * so "tinted" and "showing the initial" are two DIFFERENT states. Waiting for
 * the ELEMENT returns in the first one, and the text assertion then measures
 * `?` — measured on tree b7f0eca1 as one failed a11y gate run followed by a
 * pass on the same bytes. The rule this follows is "wait for the state the
 * assertion is about": poll the MEASUREMENT that names the state (`m.text`),
 * never something merely adjacent to it, and never a fixed sleep.
 *
 * The app upper-cases the initial (`toUpperCase`), so a single A-Z letter is the
 * settled shape. On timeout this returns the LAST measurement, so the caller's
 * own assertion fails naming the value it actually saw (`"?"`) instead of a
 * bare timeout with no measurement in it.
 */
async function waitForInitial(page: Page, sel: string): Promise<Measured> {
  const deadline = Date.now() + INITIAL_WAIT_MS;
  let m: Measured = await page.evaluate(MEASURE, sel);
  while (!/^[A-Z]$/.test(m.text)) {
    if (Date.now() >= deadline) break;
    await page.waitForTimeout(100);
    m = await page.evaluate(MEASURE, sel);
  }
  return m;
}

/** The shared non-vacuity check: the badge exists, is visible, and is TINTED. */
async function assertTinted(page: Page, sel: string) {
  const badge = page.locator(sel);
  await expect(badge, `no element matched ${sel}`).toHaveCount(1);
  await expect(badge).toBeVisible();
  // ONE measurement, taken in the SETTLED state (see waitForInitial), so the
  // tint, the text and the ratio every arm asserts on all describe the same
  // instant — a second measurement here would reintroduce the window.
  const m: Measured = await waitForInitial(page, sel);
  expect(
    m.surface.toLowerCase(),
    `${sel} is painted ${m.surface}, not the indigo-100 tint (#e0e7ff) this card is ` +
      `about — the state under test did not render`,
  ).toBe(TINT);
  return m;
}

test.describe("avatar-initial badges on bg-indigo-100", () => {
  test("the sidebar badge MEASURES above its floor, and axe agrees", async ({ page }) => {
    await openFresh(page, "/settings?tab=workspace");
    const m = await assertTinted(page, SHELL_BADGE);

    console.log(
      `[sidebar badge] text=${JSON.stringify(m.text)} surface=${m.surface} fg=${m.fg} ` +
        `ratio=${m.ratio} ${m.fontSize}/${m.fontWeight} layers=${JSON.stringify(m.layers)}`,
    );
    expect(m.text, "the sidebar badge is not rendering the user's initial").toMatch(/^O$/);
    expect(
      m.ratio,
      `the sidebar avatar initial is below its floor: ${JSON.stringify(m)}`,
    ).toBeGreaterThanOrEqual(floorFor(m));

    // THE GATE'S OWN RULE, scoped to the badge. `include()` makes axe THROW when
    // the subtree is absent, so this cannot pass over nothing.
    const results = await new AxeBuilder({ page })
      .withTags(["wcag2aa", "wcag21aa"])
      .include(SHELL_BADGE)
      .analyze();
    const blockers = blockersOf(results);
    expect(
      blockers.map((v) => `${v.id} (${v.impact}): ${v.help} — ${v.nodes[0]?.failureSummary ?? ""}`),
      "axe found critical/serious violations in the sidebar avatar badge",
    ).toEqual([]);

    // The badge is an element the axe run actually judged: colour-contrast
    // reports a node for it as a pass, a violation, or an "incomplete" it will
    // not decide — never as none of the three (see the header on why a
    // single-character element lands in `incomplete` when it FAILS).
    const analysed = [
      ...results.passes,
      ...results.violations,
      ...results.incomplete,
    ]
      .filter((r) => r.id === "color-contrast")
      .flatMap((r) => r.nodes.map((n) => n.target.join(" ")));
    expect(
      analysed.some((t) => t.includes("bg-indigo-100")),
      `axe's color-contrast rule produced no node for the badge, so the scan above covered ` +
        `something else. Nodes: ${JSON.stringify(analysed)}`,
    ).toBe(true);
  });

  test("the initial is WAITED FOR when the session read is slow — the t_129fdb48 flake", async ({
    page,
  }) => {
    // THE RACE, MADE DETERMINISTIC. The badge is tinted from the first render
    // and only fills its letter once the session bundle lands, so a measurement
    // taken as soon as the element exists can read `?`. On a host also running
    // another lane's gate that window opened by itself; held open here instead,
    // so the bounded wait in `assertTinted` is under test on every run rather
    // than left to host load. Pre-fix this arm measured `?` and failed with the
    // same output the full gate produced on tree b7f0eca1 — that is the bug.
    await openFresh(page, "/settings?tab=workspace", { bundleDelayMs: 2000 });
    const m = await assertTinted(page, SHELL_BADGE);
    console.log(
      `[slow session] text=${JSON.stringify(m.text)} surface=${m.surface} fg=${m.fg} ` +
        `ratio=${m.ratio} ${m.fontSize}/${m.fontWeight}`,
    );
    expect(
      m.text,
      "the sidebar badge was measured before the session read landed, so this is the false " +
        "RED of card t_129fdb48 (the pre-hydration `?`), not a contrast defect",
    ).toMatch(/^O$/);
    expect(
      m.ratio,
      `the sidebar avatar initial is below its floor: ${JSON.stringify(m)}`,
    ).toBeGreaterThanOrEqual(floorFor(m));
  });

  test("the /profile identity badge is LARGE text — MEASURED against ITS floor", async ({ page }) => {
    await openFresh(page, "/profile");
    const m = await assertTinted(page, PROFILE_BADGE);

    console.log(
      `[profile badge] text=${JSON.stringify(m.text)} surface=${m.surface} fg=${m.fg} ` +
        `ratio=${m.ratio} ${m.fontSize}/${m.fontWeight}`,
    );
    expect(m.text, "the profile badge is not rendering the user's initial").toMatch(/^O$/);

    // A THIRD colour is refused: the badge must be painted with one of the two
    // vetted tokens, so "it passes" cannot come from an unmeasured colour.
    expect(
      [BASE_TOKEN, ON_TINT_TOKEN],
      `the profile badge is painted ${m.fg}, which is neither --jf-primary (#5e6ad2) nor ` +
        `--jf-primary-on-tint (#4338ca) — measure the new colour before trusting this arm`,
    ).toContain(m.fg);

    // ... and when it is the BASE token, the LARGE-TEXT justification is
    // re-derived from the live DOM: 24px is 18pt is WCAG large-scale text.
    if (m.fg === BASE_TOKEN) {
      expect(
        parseFloat(m.fontSize),
        `the profile badge paints the base token at ${m.fontSize}, where its 3.814:1 is only ` +
          `legal as WCAG large-scale text (18pt = 24px). Either restore the size or move the ` +
          `badge to --jf-primary-on-tint. Measured: ${JSON.stringify(m)}`,
      ).toBeGreaterThanOrEqual(24);
    }
    expect(
      m.ratio,
      `the profile avatar initial is below ITS floor: ${JSON.stringify(m)}`,
    ).toBeGreaterThanOrEqual(floorFor(m));

    const results = await new AxeBuilder({ page })
      .withTags(["wcag2aa", "wcag21aa"])
      .include(PROFILE_BADGE)
      .analyze();
    const blockers = blockersOf(results);
    expect(
      blockers.map((v) => `${v.id} (${v.impact}): ${v.help} — ${v.nodes[0]?.failureSummary ?? ""}`),
      "axe found critical/serious violations in the profile identity badge",
    ).toEqual([]);
  });

  test("CONTROL: with the PRE-FIX token the sidebar badge drops below its floor, and axe does not clear it", async ({
    page,
  }) => {
    // The non-vacuity arm. If this spec cannot see the badge at all, the ratio
    // stays above the floor and the test fails — which is what makes arm 1's
    // green reading evidence rather than a coincidence.
    await openFresh(page, "/settings?tab=workspace");
    const fixed = await assertTinted(page, SHELL_BADGE);

    // The token the site carried before this card's fix. Added AFTER load, so the
    // override lands on the rendered element the way a stylesheet would.
    await page.addStyleTag({ content: PRE_FIX_TOKENS });
    const pre: Measured = await page.evaluate(MEASURE, SHELL_BADGE);
    console.log(
      `[control] surface=${pre.surface} fg=${pre.fg} ratio=${pre.ratio} ${pre.fontSize}/${pre.fontWeight} layers=${JSON.stringify(pre.layers)}`,
    );
    expect(
      pre.surface,
      "both control arms must run on the same painted surface, or the comparison is not one",
    ).toBe(fixed.surface);
    expect(
      pre.fg,
      "the pre-fix override did not reach the badge's text colour, so this arm proves nothing",
    ).toBe(BASE_TOKEN);
    expect(
      pre.ratio,
      `with the PRE-FIX token nothing measured below the floor, so this spec cannot see the ` +
        `defect this card is about. Measured: ${JSON.stringify(pre)}`,
    ).toBeLessThan(floorFor(pre));

    // ... and axe must NOT CLEAR it. This is deliberately not "axe names the
    // violation": measured on this tree, axe's color-contrast node for a
    // SINGLE-CHARACTER element lands in `incomplete` when the paint fails —
    // "Element content is too short to determine if it is actual text content" —
    // so an arm asserting a violation message would be asserting something axe
    // does not do here. What it must not do is PASS the pre-fix paint, and arm 1
    // measured the fixed paint of this same element as a PASS, so this is the
    // difference between the two states:
    const results = await new AxeBuilder({ page })
      .withTags(["wcag2aa", "wcag21aa"])
      .include(SHELL_BADGE)
      .analyze();
    const cleared = [...results.passes, ...results.violations]
      .filter((r) => r.id === "color-contrast")
      .flatMap((r) => r.nodes.map((n) => n.target.join(" ")));
    const undecided = results.incomplete
      .filter((r) => r.id === "color-contrast")
      .flatMap((r) =>
        r.nodes.map((n) => `${n.target.join(" ")} — ${n.any.map((c) => c.message).join("; ")}`),
      );
    console.log(`[control axe] not-passed-by-axe=${JSON.stringify(undecided)}`);
    expect(
      cleared,
      `axe PASSED the pre-fix paint of the badge, so this arm is not a reproduction of the ` +
        `defect. Nodes axe cleared: ${JSON.stringify(cleared)}`,
    ).toEqual([]);
    expect(
      undecided.join(" | "),
      "axe produced no color-contrast node for the pre-fix paint at all, so this arm proves " +
        "nothing about it",
    ).toMatch(/bg-indigo-100/);
  });
});
