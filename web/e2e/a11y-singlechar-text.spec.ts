// The a11y gate is BLIND to single-character text (card t_07eb69c8).
//
// THE GAP. axe-core 4.13.0 cannot return a verdict for text one character long.
// In `web/node_modules/axe-core/axe.js`:
//
//   27197  if (ignoreUnicode && textIsEmojis(visibleText)) { messageKey: 'nonBmp'; return void 0; }
//   27277  var shortTextContent = visibleText.length === 1;
//   27280  } else if (!isValid && shortTextContent && !ignoreLength) missing = 'shortTextContent';
//   27294  if (... || shortTextContent && !ignoreLength && !isValid) { ...; return void 0; }   // cantTell
//   31351  shortTextContent: 'Element content is too short to determine if it is actual text content',
//   31352  nonBmp: 'Element content contains only non-text characters',
//
// and the `a11y` gate fails only on critical/serious VIOLATIONS (GATE-INVENTORY.md).
// So a single-character element whose paint is UNDER its contrast floor is reported
// as `incomplete` and the gate says nothing. The 27280 branch is the sharp edge:
// a 1-character element that PASSES is a `pass`, and one that FAILS is an
// `incomplete` — the rule never reports a violation for it either way.
//
// MEASURED LIVE on this tree, before the fix below, `/` with the sidebar's own nav
// icon (12px/400, `--jf-muted` #6b7280 at `opacity-70` over the sidebar's white =
// #979ca6, **2.755:1** against a 4.5 floor):
//
//   axe, scoped to `nav`, `runOnly color-contrast`:
//     violations: []                                    <- the gate sees NOTHING
//     incomplete: span[data-jf-icon] — "Element content contains only non-text
//                 characters" / a[href$="subscriptions"] — "Element content is too
//                 short to determine if it is actual text content"
//
//   the SAME paint on a 2-CHARACTER glyph planted in the same nav:
//     violations: color-contrast (serious) — "Element has insufficient color
//                 contrast of 2.58 (foreground color: #9298a6, background color:
//                 #eef2ff, font size: 9.0pt (12px))"    <- the rule IS alive
//
// That pair is the CONTROL arm at the bottom of this file: the only difference
// between the node axe refuses to judge and the node it names is the length of the
// text. The rule itself, the token and the surface are identical.
//
// THE SCAN THIS FILE PINS, AND WHAT IT FOUND (measured 2026-10-15 on this tree, a
// production build served by playwright's own `next start`, a FRESH browser state
// — storage cleared, no `jf_lang`, `html lang="en"` — and the API stubbed at the
// browser level, so no Go server, no session, no database):
//
//   28 routes visited; 21 rendered a page with at least one single-character
//   element; 319 single-character elements measured in total, on their own painted
//   surface and at their own composited `opacity` (a canvas paints the ancestor
//   background stack and the ink over it, so the renderer does the oklab/color-mix
//   compositing and nothing is rounded).
//
//   ONE site was under its floor, and it was on EVERY signed-in page:
//
//     the sidebar nav icon  web/src/components/Shell.tsx (NavList, one span per
//     nav item; the SAME component paints the desktop <aside> and the mobile drawer)
//       resting  `--jf-muted` #6b7280 at opacity-70 -> #979ca6 on white
//                = 2.755:1   <- under the 4.5 floor, 14 icons per page
//       active   `--jf-primary-on-tint` #4338ca at opacity-70 on `bg-indigo-50`
//                = 3.714:1   <- under it too
//       after the fix (the glyph paints the row's own token, undimmed)
//                resting 4.834:1 on white | active 7.068:1 on the tint
//
//   The fix reuses the pair the row ALREADY paints — no token was invented, and
//   the same shape was already the fix for the /chat confirmation card's
//   `opacity-70` lines (card t_f5f269a8: "the root cause is the alpha, not the
//   colour"). The two avatar-initial badges are NOT touched here — PR 1102 /
//   card t_51e5df31 fixed them (its own measured spec and source rule both came
//   with that PR, and are on this tree) — and this scan JUDGES them like every
//   other element rather than exempting them, so a reversion of either token is
//   red here as well as in that card's own guards.
//
// WHY THIS IS A BROWSER MEASUREMENT AND NOT A CLASS-NAME SCAN. The defect is a
// RATIO: the ink is the token composited at an alpha the class name does not spell
// out, over a surface decided by the ancestor stack. `src/lib/mutedText.test.ts`
// reads the source and cannot see either — and it is not weakened here.
//
// WHY ONE TEST PER ROUTE. `fullyParallel` gives each route its own worker, so the
// census costs ~2.5s of route time rather than a serial walk of all 21. Each test
// asserts, in this order:
//   1. the request was NOT redirected — a page that bounced to /login has no nav,
//      and a scan of it would be a clean 0 over nothing;
//   2. the page's own shell rendered (`nav` for the signed-in routes, `main` +
//      real body text for the public ones);
//   2b. the route's LATE-MOUNTING panel is on the page, when it has one (card
//      t_1bd2b132): `/settings` resolves the session role after the shell paints,
//      so it waits for `#set-panel-chip` and requires the grid's twelve method
//      labels inside it. Without this step the arm measured the pre-role frame —
//      15 nav+initial glyphs, exactly its floor — and passed with the payment grid
//      out of the census entirely, which is the vacuity that made a run of the
//      white-on-white tick (card t_5dc6bb86) green on a tree the gate reds;
//   3. it found at least the MEASURED number of single-character elements — the
//      floor is the census, so a route that stops rendering fails instead of
//      passing over an empty page;
//   4. every one of them clears the floor its own MEASURED size and weight imply
//      (24px, or 18.66px when bold, is WCAG large text -> 3:1; otherwise 4.5:1) —
//      with NO exemption: the two avatar-initial circles PR 1102 fixed are judged
//      here too (6.414:1 for the 12px sidebar initial, 3.814:1 at 24px for the
//      /profile one, against its 3:1).
//
// Run with:
//   npm run a11y                                              # the whole gate
//   npx playwright test e2e/a11y-singlechar-text.spec.ts       # this file alone

import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import type { AxeResults, Result } from "axe-core";

import { signIn } from "./auth";
import {
  ANIMATION_SETTLE_TIMEOUT_MS,
  SETTLE_TIMEOUT_MS,
  animationSettleScript,
  animationsLine,
  settleAnimations,
  settledLine,
  waitForSettled,
  type AnimationsSettled,
} from "./settle";

// The gate's own rule, kept identical to e2e/a11y.spec.ts so the two cannot drift.
const IMPACTS = ["critical", "serious"] as const;

function blockersOf(results: AxeResults): Result[] {
  return results.violations.filter((v) =>
    IMPACTS.includes((v.impact ?? "") as (typeof IMPACTS)[number]),
  );
}

type Found = {
  text: string;
  tag: string;
  path: string;
  cls: string;
  surface: string;
  fg: string;
  ratio: number;
  fontSize: string;
  fontWeight: string;
  opacity: number;
  floor: number;
  /** An ancestor (or the element itself) has an animation that has NOT finished. */
  animating: boolean;
};

/**
 * Every VISIBLE element whose own text is exactly one character, measured.
 *
 * The background is every ancestor's colour painted back-to-front through a 1x1
 * canvas (so a translucent `bg-indigo-50/NN` is composited by the renderer, not by
 * arithmetic here), and the ink is the element's own colour painted over that at
 * the element's `opacity` chain — `opacity-70` is what turned this card's defect
 * into a 2.755:1 paint, so the alpha is part of the measurement, not a detail.
 * `[...text].length === 1` counts CODE POINTS, so an emoji glyph is one character.
 *
 * IT SETTLES THE PAGE'S ANIMATIONS FIRST, IN THIS SAME EVALUATE (t_a4adf2f1). The
 * route container animates `opacity` for 180ms (`.jf-enter`), so a sample taken
 * mid-fade reads a FRAME, not a paint — and the `/sales` arm reddened on one run and
 * passed on the next for exactly that reason. Doing the settle HERE rather than in a
 * separate `page.evaluate` before the scan is what makes it deterministic: a second
 * round trip leaves a window in which a late-committing route body (measured on
 * `/activity`: its `jf-rise-in` animation is created ~250ms AFTER the sidebar nav is
 * on the page) starts a fresh fade between the settle and the measurement.
 */
const SCAN = `(async () => {
  const settle = await ${animationSettleScript(ANIMATION_SETTLE_TIMEOUT_MS)};
  const cv = document.createElement("canvas");
  cv.width = cv.height = 1;
  const ctx = cv.getContext("2d", { willReadFrequently: true });
  const paint = (base, c, alpha) => {
    ctx.clearRect(0, 0, 1, 1);
    ctx.fillStyle = base;
    ctx.fillRect(0, 0, 1, 1);
    if (c) {
      ctx.save();
      ctx.globalAlpha = alpha === undefined ? 1 : alpha;
      ctx.fillStyle = c;
      ctx.fillRect(0, 0, 1, 1);
      ctx.restore();
    }
    const d = ctx.getImageData(0, 0, 1, 1).data;
    return { r: d[0], g: d[1], b: d[2] };
  };
  const lin = (v) => (v / 255 <= 0.04045 ? v / 255 / 12.92 : Math.pow((v / 255 + 0.055) / 1.055, 2.4));
  const lum = (c) => 0.2126 * lin(c.r) + 0.7152 * lin(c.g) + 0.0722 * lin(c.b);
  const ratio = (a, b) => {
    const [x, y] = [lum(a), lum(b)];
    return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
  };
  const hex = (c) => "#" + [c.r, c.g, c.b].map((v) => v.toString(16).padStart(2, "0")).join("");
  const ownText = (el) => {
    let s = "";
    for (const n of el.childNodes) if (n.nodeType === 3) s += n.nodeValue || "";
    return s.trim();
  };
  const pathOf = (el) => {
    const bits = [];
    for (let n = el; n && n !== document.body; n = n.parentElement) {
      const cls = typeof n.className === "string" ? n.className.trim().split(/\\s+/).slice(0, 3).join(".") : "";
      bits.unshift(n.tagName.toLowerCase() + (cls ? "." + cls : ""));
    }
    return bits.join(" > ");
  };
  const out = [];
  // Every element that a NOT-YET-FINISHED animation is currently painting (card
  // t_a4adf2f1). An element's ratio is computed from its ancestor \`opacity\` chain,
  // so an unfinished ancestor animation means the number below is a FRAME of that
  // animation, not a settled paint. The settle at the top of this script finishes
  // the finite ones first; this set is what is LEFT after that, and the caller marks
  // those elements \`MID-ANIMATION\` instead of reading their ratio as a paint.
  const animatingEls = new Set();
  for (const a of document.getAnimations()) {
    if (a.playState === "finished") continue;
    const target = a.effect && a.effect.target;
    if (target) animatingEls.add(target);
  }
  for (const el of document.querySelectorAll("body *")) {
    const t = ownText(el);
    if ([...t].length !== 1) continue;
    const cs = getComputedStyle(el);
    if (cs.display === "none" || cs.visibility === "hidden") continue;
    const r = el.getBoundingClientRect();
    if (r.width === 0 || r.height === 0) continue;
    let op = 1;
    let animating = false;
    for (let n = el; n; n = n.parentElement) {
      const o = parseFloat(getComputedStyle(n).opacity);
      op *= isNaN(o) ? 1 : o;
      if (animatingEls.has(n)) animating = true;
      if (n === document.body) break;
    }
    if (op === 0) continue;
    const layers = [];
    for (let n = el; n; n = n.parentElement) {
      const c = getComputedStyle(n).backgroundColor;
      if (c !== "rgba(0, 0, 0, 0)" && c !== "transparent") layers.push(c);
      if (n === document.body) break;
    }
    let bg = { r: 255, g: 255, b: 255 };
    for (let i = layers.length - 1; i >= 0; i--) {
      bg = paint("rgb(" + bg.r + ", " + bg.g + ", " + bg.b + ")", layers[i]);
    }
    const fg = paint("rgb(" + bg.r + ", " + bg.g + ", " + bg.b + ")", cs.color, op);
    const px = parseFloat(cs.fontSize);
    const bold = Number(cs.fontWeight) >= 700;
    out.push({
      text: t,
      tag: el.tagName.toLowerCase(),
      path: pathOf(el),
      cls: typeof el.className === "string" ? el.className : "",
      surface: hex(bg),
      fg: hex(fg),
      ratio: Math.round(ratio(fg, bg) * 1000) / 1000,
      fontSize: cs.fontSize,
      fontWeight: cs.fontWeight,
      opacity: op,
      floor: px >= 24 || (bold && px >= 18.66) ? 3 : 4.5,
      animating: animating,
    });
  }
  return {
    found: out,
    animatingCount: out.filter((m) => m.animating).length,
    settle: settle,
    navLinks: document.querySelectorAll("nav a[href]").length,
    hasMain: !!document.querySelector("main"),
    bodyText: (document.body.innerText || "").replace(/\\s+/g, " ").trim().length,
  };
})()`;

/** One census line per element, so the numbers the doc quotes are in the run log. */
function censusLine(route: string, m: Found, verdict: string): string {
  return (
    `[single-char] ${route} ${JSON.stringify(m.text)} ${m.tag} ${m.fontSize}/${m.fontWeight} ` +
    `op=${m.opacity} fg=${m.fg} surface=${m.surface} ratio=${m.ratio} floor=${m.floor} ${verdict} | ${m.path}`
  );
}

/**
 * The two avatar-INITIAL circles this scan used to count out are now judged like
 * everything else here. PR 1102 (card t_51e5df31) fixed them: the sidebar's 12px
 * initial takes the on-tint token and measures 6.414:1, the /profile one is 24px —
 * WCAG large text — and keeps the base token at 3.814:1 against its 3:1 floor. Both
 * are measured by that card's own spec and source rule as well; this scan measures
 * them too, so a reversion of either token is red in TWO places.
 */

type Route = {
  name: string;
  path: string;
  authed: boolean;
  /** Measured number of single-character elements; a route that renders fewer did not render. */
  min: number;
  what: string;
  /**
   * A panel this route paints only AFTER it has resolved something the Shell does
   * not wait for (card t_1bd2b132). `open()` will not return until `anchor` is on
   * the page, and the arm asserts BOTH that it appeared and that its `labels` are
   * inside it — a panel that never mounts is then RED instead of a census that
   * happens to match the floor over a page without it.
   */
  panel?: Panel;
};

/**
 * A late-mounting panel, named by what it is rather than by the count it happens
 * to produce. `labels` is the panel's CONTENT: its own visible strings, which must
 * all be in the page the census measures. They are matched by text, not by a
 * `data-testid`, because the panel's markup is free to rename its attributes
 * (the payment-method grid changed from buttons to labelled checkboxes in
 * PR #1682) while its twelve method labels did not.
 */
type Panel = {
  /** CSS selector that only exists once the panel has mounted. */
  anchor: string;
  /** What the panel is, for the failure message. */
  what: string;
  /** Visible strings that must ALL be present inside the anchor. */
  labels: readonly string[];
};

// MEASURED 2026-10-15 on this tree (production build, fresh state, API stubbed).
// Every `min` is the census count at the time this file was added; the count is a
// FLOOR, so a page that gains a single-character element is measured by this scan
// automatically, and one that renders fewer than the census fails loudly. Re-measure
// with the run log (`[single-char]` lines) rather than editing a number by hand.
//
// A FLOOR IS NOT A PRESENCE CHECK (card t_1bd2b132). `/settings` reached its floor of
// 15 from the sidebar's 14 nav icons + the profile initial BEFORE the CHIP tab — and
// therefore the twelve payment-method controls this route is really about — had
// mounted, so "the grid is there and clean" and "the grid never rendered" produced
// the SAME census. A number cannot tell those apart; a selector can. That is what
// `panel` is for on the one route measured with a late-mounting panel.
//
// THE SWEEP BEHIND THAT "ONE ROUTE" (measured on this tree, production build, the
// spec's own stubs, super-admin bundle): every authed route was sampled at the frame
// the settle returned and again 250/1250 ms later. The ROLE arrives after the shell
// everywhere — the sidebar's profile chip is painted with a "?" placeholder that is
// swapped for the real initial, and a role-gated nav item appears, on `/products`,
// `/sales`, `/templates`, `/activity` and `/settings` — but only on `/settings` does
// that role gate the CONTENT this scan judges: the CHIP tab, and with it the twelve
// payment-method controls. The other routes' late work is chrome the census already
// counts on both sides of the swap, so their `min` floors moved by nothing (measured:
// products 18/18, sales 16/16, activity 16/16; templates 16/16 with the placeholder
// initial replaced by the real one). `/settings` was the one route whose census could
// legitimately be taken over a page with no grid in it — hence the single `panel`.
const ROUTES: Route[] = [
  { name: "login", path: "/login", authed: false, min: 1, what: "the key glyph in the 2FA card" },
  { name: "landing", path: "/landing", authed: false, min: 33, what: "24 ✓ marks + the emoji glyphs" },
  { name: "mcp", path: "/mcp", authed: false, min: 5, what: "the four numbered steps" },
  { name: "docs", path: "/docs", authed: false, min: 1, what: "the '·' separator in the index" },
  { name: "docs-builder", path: "/docs/builder", authed: false, min: 3, what: "the ↑ ↓ ⋯ key hints" },
  { name: "dashboard", path: "/", authed: true, min: 15, what: "the sidebar's nav icons" },
  { name: "forms", path: "/forms", authed: true, min: 15, what: "the sidebar's nav icons" },
  { name: "products", path: "/products", authed: true, min: 15, what: "nav icons + the sort glyphs" },
  { name: "sales", path: "/sales", authed: true, min: 15, what: "the sidebar's nav icons" },
  { name: "media", path: "/media", authed: true, min: 15, what: "the sidebar's nav icons" },
  { name: "subscriptions", path: "/subscriptions", authed: true, min: 15, what: "the sidebar's nav icons" },
  { name: "templates", path: "/templates", authed: true, min: 15, what: "the sidebar's nav icons" },
  { name: "deliveries", path: "/deliveries", authed: true, min: 15, what: "the sidebar's nav icons" },
  { name: "spam", path: "/spam", authed: true, min: 15, what: "the sidebar's nav icons" },
  { name: "activity", path: "/activity", authed: true, min: 15, what: "the sidebar's nav icons" },
  {
    name: "settings",
    path: "/settings",
    authed: true,
    min: 15,
    what: "the sidebar's nav icons (the CHIP grid's twelve method controls are multi-character labels)",
    // The CHIP tab is ADMIN-ONLY, and `activeTab` derives from the role the page
    // fetches AFTER the Shell has painted: the pre-role frame renders the Domains
    // tab. `#set-panel-chip` is the CHIP panel's own landmark (`panelDomId("set",
    // "chip")`, web/src/lib/tablist.ts) and it exists in BOTH shapes of the grid —
    // the twelve pill buttons this route had, and the labelled checkboxes PR #1682
    // replaced them with — so the anchor is the panel, not one markup's testid.
    panel: {
      anchor: "#set-panel-chip",
      what: "CHIP payment-method panel",
      // The twelve labels are `METHODS` in web/src/app/settings/page.tsx, in source
      // order. If a method is added or renamed, this list is what says the scan is
      // still looking at the grid — update it WITH the source change.
      labels: [
        "FPX",
        "FPX B2B1",
        "Card (Visa, Mastercard, Maestro)",
        "Google Pay",
        "Apple Pay",
        "Atome",
        "GrabPay",
        "Maybank QRPay",
        "ShopeePay",
        "Touch 'n Go eWallet",
        "DuitNow QR",
        "Crypto Coin",
      ],
    },
  },
  { name: "profile", path: "/profile", authed: true, min: 15, what: "nav icons + BOTH avatar circles" },
  { name: "chat", path: "/chat", authed: true, min: 15, what: "the sidebar's nav icons" },
  { name: "builder", path: "/builder", authed: true, min: 30, what: "the block palette's glyphs (the builder has its own chrome, not the sidebar)" },
  { name: "feedback", path: "/feedback", authed: true, min: 15, what: "the sidebar's nav icons" },
  { name: "form-settings", path: "/form-settings", authed: true, min: 15, what: "the sidebar's nav icons" },
];

/** Every `/api/*` read these pages make, stubbed at the browser level. */
async function stubAPI(page: Page) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST, specific routes AFTER it: Playwright resolves matching routes
  // in reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) =>
    r.fulfill(
      json({
        me: {
          id: "u1",
          email: "merchant@example.com",
          name: "Merchant",
          lang: "en",
          is_super_admin: true,
        },
        workspaces: [
          { id: "a1", name: "Demo", slug: "demo", role: "admin" },
          { id: "a2", name: "Other", slug: "other", role: "admin" },
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
      }),
    ),
  );
  await page.route(/(.*)\/api\/auth\/me$/, (r) =>
    r.fulfill(
      json({
        id: "u1",
        email: "merchant@example.com",
        name: "Merchant",
        is_super_admin: true,
      }),
    ),
  );
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "en" })));
}

/**
 * A FRESH browser state at the app's DEFAULT locale: storage cleared and NO
 * `jf_lang` written, so the page renders what a new visitor gets (a `jf_lang` left
 * behind by an earlier run has already produced a false reading on this board).
 */
async function freshState(page: Page) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
    } catch {
      /* storage unavailable */
    }
  });
}

async function open(page: Page, baseURL: string | undefined, route: Route) {
  await freshState(page);
  await page.context().clearCookies();
  if (route.authed) await signIn(page, baseURL);
  await stubAPI(page);
  await page.goto(route.path, { waitUntil: "domcontentloaded" });
  // A BOUNDED CONDITION WAIT, not a fixed sleep (t_14837ea0). This used to be
  // `await page.waitForTimeout(1500)` — a bet that the stubbed `dashboard-bundle`
  // fetch plus hydration finishes in 1500 ms. The Shell renders AFTER that fetch
  // resolves, so on a loaded host the bet lost and `/` reddened with "rendered no
  // sidebar nav" on a page that was correct (measured: 1 failure in 2 runs of the
  // same tree). The wait now returns on the first frame the page is really
  // rendered and reaches its ceiling otherwise, WITHOUT throwing — the arm's own
  // reachability assertion below is still what fails, so a shell that genuinely
  // never renders is still RED, with the state the wait saw in the message.
  const settled = await waitForSettled(page, {
    wantsShell: route.authed && route.path !== "/builder",
    // A route's late-mounting panel is part of "this page has rendered" (card
    // t_1bd2b132): without it the wait is satisfied by the frame BEFORE the panel
    // exists, and the census below measures a page that does not contain the
    // content this route is scanned for.
    anchor: route.panel?.anchor,
  });
  console.log(settledLine(route.path, settled));
  return settled;
}

/** The locale the page is rendering in, read after the app has hydrated. */
async function storedLocale(page: Page): Promise<string | null> {
  return page.evaluate(() => window.localStorage.getItem("jf_lang"));
}

test.describe("a11y: single-character text is measured against its own floor", () => {
  for (const route of ROUTES) {
    test(`${route.path} — every single-character element (${route.what}) clears its floor`, async ({
      page,
      baseURL,
    }) => {
      const settled = await open(page, baseURL, route);

      // (1) NO REDIRECT. A dashboard path without the cookie is bounced to /login
      // by web/src/middleware.ts, and a scan of the login page would be a clean 0
      // over the surface this test is about.
      expect(new URL(page.url()).pathname, `${route.path} redirected — the page under test did not render`).toBe(
        route.path,
      );

      const waited = `waited ${settled.waitedMs}ms (ceiling ${SETTLE_TIMEOUT_MS}ms): hydrated=${settled.hydrated}, bodyText=${settled.bodyText}`;

      // (1b) THE ROUTE'S LATE-MOUNTING PANEL IS IN THE PAGE (card t_1bd2b132).
      // BEFORE the census, because this is what makes the census a measurement of
      // the ROUTE rather than of the chrome that was already there. `/settings`
      // resolves the session role after the Shell paints, and the CHIP tab — the
      // ONLY place the twelve payment-method controls live — mounts a frame
      // later. `open()` now waits for the panel, and this is the state that wait
      // reports: a panel that never appeared is RED here, on its own message,
      // instead of a 15-element census over a page that has no grid in it.
      const panel = route.panel;
      if (panel) {
        expect(
          settled.anchored,
          `${route.path} rendered no ${panel.what} — ${panel.anchor} never appeared within the ` +
            `${SETTLE_TIMEOUT_MS}ms settle ceiling, so the census would have measured this page WITHOUT its ` +
            `grid and could have matched the floor anyway. This is the settle, NOT a contrast verdict [${waited}]`,
        ).toBe(true);
        const anchored = page.locator(panel.anchor);
        const missing: string[] = [];
        for (const label of panel.labels) {
          if ((await anchored.getByText(label, { exact: true }).count()) === 0) missing.push(label);
        }
        expect(
          missing,
          `${route.path} — ${panel.what} mounted, but ${missing.length} of its ${panel.labels.length} content ` +
            `label(s) are not in it: ${JSON.stringify(missing)}. The page is not the grid this route is scanned ` +
            `for, so the census below would judge something else [${waited}]`,
        ).toEqual([]);
      }

      const { found, settle, navLinks, hasMain, bodyText } = (await page.evaluate(SCAN)) as {
        found: Found[];
        settle: AnimationsSettled;
        navLinks: number;
        hasMain: boolean;
        bodyText: number;
      };
      // What the scan's own settle had to do, before anything is measured — so a run
      // log shows whether the entrance was still painting when the scan started.
      console.log(animationsLine(route.path, settle));

      // (2) THE PAGE'S OWN SHELL RENDERED. Without this the count assertion below
      // could be satisfied by chrome that is not the route's. The wait in `open()`
      // is bounded, not a sleep, so a shell that genuinely never renders is still
      // RED here — the message carries what the wait saw and how long it waited.
      if (route.authed && route.path !== "/builder") {
        // The sidebar routes: the Shell's nav is the thing that carries the fixed
        // glyphs, so its absence is the failure this arm is about.
        expect(
          navLinks,
          `${route.path} rendered no sidebar nav — the shell did not render [${waited}]`,
        ).toBeGreaterThanOrEqual(10);
      }
      // EVERY route in this table renders a `<main>` landmark: the public pages
      // and the builder paint their own, and the signed-in Shell routes get the
      // one the Shell wraps their body in (card t_25a8fc88 — the authed shell
      // used to render none, which axe reported as `landmark-one-main` while
      // this arm asserted only the nav count).
      expect(hasMain, `${route.path} rendered no <main> landmark [${waited}]`).toBe(true);
      expect(
        bodyText,
        `${route.path} rendered almost no text — the loading shell is not a scan [${waited}]`,
      ).toBeGreaterThan(200);
      // FRESH STATE, asserted: the init script clears storage on every navigation,
      // so a non-default locale here can only be one the APP itself persisted —
      // a leftover `jf_lang` from an earlier run (this board has had `ar` change a
      // reading) fails. `en` is the default a new visitor gets, and the document
      // declaration is asserted with it.
      expect(
        [null, "", "en"],
        "this run must start at the DEFAULT locale (storage cleared, no leftover jf_lang)",
      ).toContain(await storedLocale(page));
      await expect(page.locator("html")).toHaveAttribute("lang", "en");

      // (3) EVERY element found, logged BEFORE any assertion, so the run log
      // carries the census the doc quotes even when the run is red. This is the
      // number the `min` floors below are measured from. An element whose ancestor
      // was STILL animating even after the settle is marked `MID-ANIMATION` — its
      // ratio is a frame of that animation, and compositing ink toward its surface
      // can only move a ratio toward 1, so such a sample can understate contrast
      // but never overstate it.
      for (const m of found) {
        console.log(censusLine(route.path, m, m.animating ? "MID-ANIMATION" : ""));
      }

      // (4) THE CENSUS FLOOR: measured, never typed from a guess.
      expect(
        found.length,
        `${route.path} rendered ${found.length} single-character element(s), fewer than the ${route.min} ` +
          `measured when this scan was added. Either the page stopped rendering or its content changed — ` +
          `re-measure from the [single-char] lines in this run rather than lowering this number.`,
      ).toBeGreaterThanOrEqual(route.min);

      // (4b) NOT an assertion: an element still animating after the settle is
      // REPORTED, not failed (t_a4adf2f1). Compositing ink toward its own surface can
      // only move a ratio toward 1, so a mid-animation sample can understate contrast
      // but never overstate it — failing here would turn a passing element into a red
      // for a defect it does not have. It is attached to the real verdict below
      // instead, so a sub-floor element says whether its ratio was a paint or a frame.
      const midFlight = found.filter((m) => m.animating);

      // (5) EVERY one of them — no exemption, and the two avatar-initial circles
      // PR 1102 fixed are judged here as well — against its OWN measured floor.
      const failing = found.filter((m) => m.ratio < m.floor);
      expect(
        failing.map((m) => censusLine(route.path, m, m.animating ? "BELOW FLOOR (MID-ANIMATION)" : "BELOW FLOOR")),
        `${route.path}: ${failing.length} single-character element(s) are painted under their own AA floor. ` +
          (midFlight.length
            ? `${midFlight.length} of them had an ancestor still animating even after this scan settled the ` +
              `page, so those ratios are a FRAME of that animation — an entrance that cannot be settled is its ` +
              `own defect, and the entry's end state is what the floor is about. `
            : "") +
          `axe CANNOT report these — a one-character node that fails is returned as \`incomplete\` ` +
          `(shortTextContent / nonBmp, axe.js:27277-27294), never as a violation — which is what this scan exists for.`,
      ).toEqual([]);
    });
  }
});

// ── the control: the arm bites, AND axe's blindness is pinned live ────────────
//
// Without this arm a green reading above proves nothing: a scan whose measurement
// cannot see a sub-floor single-character paint is a scan that would pass over the
// defect it was written for. It reproduces the pre-fix paint on the exact element
// this card fixed, requires the measurement to drop BELOW the floor, and then asks
// live axe-core for a verdict — recording, in the gate's own run, that the answer
// for a one-character node is `incomplete` while the IDENTICAL paint on a
// two-character node is a `color-contrast` VIOLATION.
test.describe("a11y: the single-character scan bites, and axe is blind to it", () => {
  test("the pre-fix paint is measured BELOW the floor, axe sees nothing, and the same paint on 2 characters IS named", async ({
    page,
    baseURL,
  }) => {
    const route = ROUTES.find((r) => r.name === "dashboard")!;
    const settled = await open(page, baseURL, route);

    // THE ARM PROVES IT IS ON THE PAGE IT MEASURES BEFORE IT COUNTS ANYTHING
    // (card t_d3efc352). This used to count `nav a > span` straight after
    // `open()` and fail with ONE sentence — "the sidebar rendered no nav glyph
    // spans" — for three different states, so a red could not be attributed
    // without a full gate re-run (measured on t_d9cf1ddd's own gate: 1 failed /
    // 120 passed at this line, then the SAME bytes green on a re-run). The three
    // are separated here, in the order they can happen, and each names itself:
    //
    //   (1/3) BOUNCED   — the arm landed somewhere other than the route under
    //                     test: the page's own client bounce sends an expired
    //                     session to /login. (`/` itself is NOT in
    //                     web/src/middleware.ts's matcher — the server gate
    //                     cannot bounce THIS route, and a cookie-less `/` is the
    //                     marketing landing, which is the bodyText case below.)
    //                     Either way there is no sidebar shell, so 0 glyphs
    //                     there is a reading over nothing — never the sidebar
    //                     this arm is FOR.
    //   (2/3) UNPAINTED — the signed-in Shell did not render inside the settle
    //                     ceiling `open()` already waited: host load, hydration,
    //                     or a page that is not the shell at all — not the
    //                     glyphs.
    //   (3/3) NO GLYPHS — the Shell IS rendered (N nav links, real body text)
    //                     and its rows carry no glyph spans. That is the only
    //                     state that may report the sidebar defect.
    const landed = new URL(page.url()).pathname;
    const saw =
      `landed=${landed} expected=${route.path} waited=${settled.waitedMs}ms ` +
      `(ceiling ${SETTLE_TIMEOUT_MS}ms) timedOut=${settled.timedOut} hydrated=${settled.hydrated} ` +
      `navLinks=${settled.navLinks} bodyText=${settled.bodyText}`;
    expect(
      landed,
      `(1/3) BOUNCED to ${landed} instead of ${route.path} — this run is not on the page under test, ` +
        `and a page with no sidebar has no glyphs to count. This is NOT the sidebar defect [${saw}]`,
    ).toBe(route.path);
    expect(
      settled.timedOut,
      `(2/3) UNPAINTED — the Shell did not render within the ${SETTLE_TIMEOUT_MS}ms settle ceiling ` +
        `(host load or hydration), so nothing was counted. This is NOT the sidebar defect [${saw}]`,
    ).toBe(false);
    expect(
      settled.navLinks,
      `(2/3) UNPAINTED — the settle wait ended with ${settled.navLinks} sidebar link(s) where the Shell ` +
        `paints 10+, so there is no sidebar here to count glyphs in. A large bodyText with few nav links ` +
        `means the page is not the signed-in shell (a cookie-less / renders the marketing landing). ` +
        `This is NOT the sidebar defect [${saw}]`,
    ).toBeGreaterThanOrEqual(10);

    const icons = page.locator("nav a > span");
    const iconCount = await icons.count();
    expect(
      iconCount,
      `(3/3) NO GLYPHS — the Shell IS rendered (${settled.navLinks} nav links, ` +
        `bodyText=${settled.bodyText}) but its rows carry ${iconCount} glyph span(s): the sidebar markup ` +
        `under test is missing its glyphs [${saw}]`,
    ).toBeGreaterThanOrEqual(13);

    /** The nav glyphs' own measurement, by index, from the live DOM. */
    const measureIcons = () =>
      page.evaluate(() => {
        const cv = document.createElement("canvas");
        cv.width = cv.height = 1;
        const ctx = cv.getContext("2d", { willReadFrequently: true })!;
        const paint = (base: string, c?: string, alpha?: number) => {
          ctx.clearRect(0, 0, 1, 1);
          ctx.fillStyle = base;
          ctx.fillRect(0, 0, 1, 1);
          if (c) {
            ctx.save();
            ctx.globalAlpha = alpha === undefined ? 1 : alpha;
            ctx.fillStyle = c;
            ctx.fillRect(0, 0, 1, 1);
            ctx.restore();
          }
          const d = ctx.getImageData(0, 0, 1, 1).data;
          return { r: d[0], g: d[1], b: d[2] };
        };
        const lin = (v: number) => (v / 255 <= 0.04045 ? v / 255 / 12.92 : Math.pow((v / 255 + 0.055) / 1.055, 2.4));
        const lum = (c: { r: number; g: number; b: number }) => 0.2126 * lin(c.r) + 0.7152 * lin(c.g) + 0.0722 * lin(c.b);
        const ratio = (a: { r: number; g: number; b: number }, b: { r: number; g: number; b: number }) =>
          (Math.max(lum(a), lum(b)) + 0.05) / (Math.min(lum(a), lum(b)) + 0.05);
        const hex = (c: { r: number; g: number; b: number }) =>
          "#" + [c.r, c.g, c.b].map((v) => v.toString(16).padStart(2, "0")).join("");
        const out: { text: string; fg: string; surface: string; ratio: number; floor: number }[] = [];
        for (const el of document.querySelectorAll("nav a > span")) {
          const text = (el.textContent || "").trim();
          if ([...text].length !== 1) continue;
          let op = 1;
          for (let n: Element | null = el; n; n = n.parentElement) {
            op *= parseFloat(getComputedStyle(n).opacity);
            if (n === document.body) break;
          }
          const layers: string[] = [];
          for (let n: Element | null = el; n; n = n.parentElement) {
            const c = getComputedStyle(n).backgroundColor;
            if (c !== "rgba(0, 0, 0, 0)" && c !== "transparent") layers.push(c);
            if (n === document.body) break;
          }
          let bg = { r: 255, g: 255, b: 255 };
          for (let i = layers.length - 1; i >= 0; i--) bg = paint(`rgb(${bg.r}, ${bg.g}, ${bg.b})`, layers[i]);
          const fg = paint(`rgb(${bg.r}, ${bg.g}, ${bg.b})`, getComputedStyle(el).color, op);
          const px = parseFloat(getComputedStyle(el).fontSize);
          const bold = Number(getComputedStyle(el).fontWeight) >= 700;
          out.push({
            text,
            fg: hex(fg),
            surface: hex(bg),
            ratio: Math.round(ratio(fg, bg) * 1000) / 1000,
            floor: px >= 24 || (bold && px >= 18.66) ? 3 : 4.5,
          });
        }
        return out;
      });

    // ARM 0 — the SHIPPED state, on the exact elements this card fixed. This is
    // the claim the fix makes: the glyph clears its floor, undimmed.
    const shipped = await measureIcons();
    expect(shipped.length).toBeGreaterThanOrEqual(13);
    for (const m of shipped) {
      console.log(`[control shipped] ${JSON.stringify(m.text)} fg=${m.fg} surface=${m.surface} ratio=${m.ratio} floor=${m.floor}`);
    }
    const shippedFailing = shipped.filter((m) => m.ratio < m.floor);
    expect(
      shippedFailing.map((m) => `${JSON.stringify(m.text)} ${m.ratio} < ${m.floor}`),
      "the shipped sidebar glyphs are not all above their floor",
    ).toEqual([]);

    // ARM 0b — THE SETTLE IS WHAT MAKES THAT READING A PAINT, and this arm proves it
    // in BOTH directions (card t_a4adf2f1). A fade frozen at its midpoint on an
    // ancestor of the glyphs is exactly the shape the /sales flake had — a
    // single-character element measured while an ancestor was mid-entrance — so:
    //   (a) the raw measurement MUST drop below the floor (the state that reddened a
    //       correct tree), and
    //   (b) after `settleAnimations` the SAME elements MUST clear it.
    // If the settle were removed, (b) fails. If the injected fade were not really
    // applied, (a) fails (and so does the "it had to be forced" assertion). Neither
    // half is decorative.
    await page.evaluate(() => {
      const nav = document.querySelector("nav")!;
      nav.style.animation = "jf-rise-in 30s linear both";
      nav.style.animationDelay = "-15s"; // frozen at the midpoint: opacity ~0.5
    });
    const midFlight = await measureIcons();
    expect(
      midFlight.length,
      `the injected fade left ${midFlight.length} glyph(s) to measure — this arm did not run on the sidebar`,
    ).toBeGreaterThanOrEqual(13);
    for (const m of midFlight) {
      console.log(
        `[control mid-flight] ${JSON.stringify(m.text)} fg=${m.fg} surface=${m.surface} ratio=${m.ratio} floor=${m.floor}`,
      );
    }
    expect(
      midFlight.every((m) => m.ratio < m.floor),
      "with a 30s fade frozen at its midpoint on the nav, NOT every glyph measured below its floor, so " +
        `this arm does not reproduce a mid-animation frame: ${JSON.stringify(midFlight)}`,
    ).toBe(true);

    const settledAnim = await settleAnimations(page);
    console.log(animationsLine("/", settledAnim));
    expect(
      settledAnim.forced,
      `the settle forced ${settledAnim.forced} animation(s) at its ${ANIMATION_SETTLE_TIMEOUT_MS}ms ceiling: ` +
        `the injected 30s fade cannot finish on its own inside that ceiling, so either it was never applied ` +
        `or the settle is not seeing it`,
    ).toBeGreaterThan(0);
    const afterSettle = await measureIcons();
    for (const m of afterSettle) {
      console.log(
        `[control settled] ${JSON.stringify(m.text)} fg=${m.fg} surface=${m.surface} ratio=${m.ratio} floor=${m.floor}`,
      );
    }
    expect(
      afterSettle
        .filter((m) => m.ratio < m.floor)
        .map((m) => `${JSON.stringify(m.text)} ${m.ratio} < ${m.floor}`),
      "after settling the animation the sidebar glyphs are still under their floor — the settle did not " +
        "reach the injected fade, so a scan would keep folding a frame into the ratio",
    ).toEqual([]);
    // Leave the page as ARM 1 expects it (the shipped nav, no injected declaration).
    await page.evaluate(() => {
      const nav = document.querySelector("nav")!;
      nav.style.animation = "";
      nav.style.animationDelay = "";
    });

    // ARM 1 — THE PRE-FIX DECLARATION, restored on the real elements. This is the
    // exact class the fix removed (`opacity-70`), injected as the declaration
    // Tailwind emits for it, so the measurement below is of the defect itself.
    await page.addStyleTag({ content: "nav a > span { opacity: 0.7 !important; }" });
    const preFix = await measureIcons();
    for (const m of preFix) {
      console.log(`[control pre-fix] ${JSON.stringify(m.text)} fg=${m.fg} surface=${m.surface} ratio=${m.ratio} floor=${m.floor}`);
    }
    expect(
      preFix.every((m) => m.ratio < m.floor),
      `with opacity-70 restored, NOT every sidebar glyph measured below its floor, so this arm does not ` +
        `reproduce the defect: ${JSON.stringify(preFix)}`,
    ).toBe(true);

    // ARM 2 — axe, on that SAME subtree, must see NOTHING. Scoped with `include()`,
    // which THROWS when the subtree is absent, so this cannot pass over nothing.
    const blind = await new AxeBuilder({ page })
      .withRules(["color-contrast"])
      .include("nav")
      .analyze();
    const blindViolations = blind.violations.filter((v) => v.id === "color-contrast");
    const incompleteNodes = blind.incomplete
      .filter((v) => v.id === "color-contrast")
      .flatMap((v) => v.nodes.map((n) => ({ target: n.target.join(" "), summary: n.failureSummary ?? "" })));
    console.log(`[control axe] violations=${JSON.stringify(blindViolations.map((v) => v.nodes.length))}`);
    console.log(`[control axe] incomplete=${JSON.stringify(incompleteNodes)}`);
    expect(
      blindViolations.flatMap((v) => v.nodes.map((n) => n.failureSummary ?? "")),
      "axe REPORTED a color-contrast violation for these single-character glyphs — if axe can see them, this " +
        "file's premise (and docs/accessibility.md's note) is wrong and must be re-measured",
    ).toEqual([]);
    expect(
      incompleteNodes.some(
        (n) => /too short to determine|only non-text characters/i.test(n.summary),
      ),
      `axe did not return the single-character glyphs as \`incomplete\` at all — the blindness this file records ` +
        `is not reproducible here. Incomplete nodes: ${JSON.stringify(incompleteNodes)}`,
    ).toBe(true);

    // ARM 3 — THE RULE IS ALIVE. The SAME paint on a TWO-character node in the same
    // nav is a violation, so the only thing blinding axe in arm 2 is the LENGTH.
    await page.evaluate(() => {
      const link = document.querySelector("nav a[href]")!;
      const s = document.createElement("span");
      s.setAttribute("data-jf-twchar", "1");
      s.setAttribute("style", "color:#6b7280;opacity:0.7;font-size:12px;display:inline-block");
      s.textContent = "AB";
      link.appendChild(s);
    });
    const planted = await page.evaluate(() => {
      const el = document.querySelector('[data-jf-twchar="1"]') as HTMLElement;
      const cs = getComputedStyle(el);
      let op = 1;
      for (let n: Element | null = el; n; n = n.parentElement) {
        op *= parseFloat(getComputedStyle(n).opacity);
        if (n === document.body) break;
      }
      const cv = document.createElement("canvas");
      cv.width = cv.height = 1;
      const ctx = cv.getContext("2d", { willReadFrequently: true })!;
      const paint = (base: string, c?: string, alpha?: number) => {
        ctx.clearRect(0, 0, 1, 1);
        ctx.fillStyle = base;
        ctx.fillRect(0, 0, 1, 1);
        if (c) {
          ctx.save();
          ctx.globalAlpha = alpha === undefined ? 1 : alpha;
          ctx.fillStyle = c;
          ctx.fillRect(0, 0, 1, 1);
          ctx.restore();
        }
        const d = ctx.getImageData(0, 0, 1, 1).data;
        return { r: d[0], g: d[1], b: d[2] };
      };
      const lin = (v: number) => (v / 255 <= 0.04045 ? v / 255 / 12.92 : Math.pow((v / 255 + 0.055) / 1.055, 2.4));
      const lum = (c: { r: number; g: number; b: number }) => 0.2126 * lin(c.r) + 0.7152 * lin(c.g) + 0.0722 * lin(c.b);
      const ratio = (a: { r: number; g: number; b: number }, b: { r: number; g: number; b: number }) =>
        (Math.max(lum(a), lum(b)) + 0.05) / (Math.min(lum(a), lum(b)) + 0.05);
      const layers: string[] = [];
      for (let n: Element | null = el; n; n = n.parentElement) {
        const c = getComputedStyle(n).backgroundColor;
        if (c !== "rgba(0, 0, 0, 0)" && c !== "transparent") layers.push(c);
        if (n === document.body) break;
      }
      let bg = { r: 255, g: 255, b: 255 };
      for (let i = layers.length - 1; i >= 0; i--) bg = paint(`rgb(${bg.r}, ${bg.g}, ${bg.b})`, layers[i]);
      // The element's own `opacity` is part of the paint — the same alpha the
      // pre-fix sidebar glyph carried, and the reason axe's own report reads 2.58.
      const fg = paint(`rgb(${bg.r}, ${bg.g}, ${bg.b})`, cs.color, op);
      return Math.round(ratio(fg, bg) * 1000) / 1000;
    });
    console.log(`[control 2-char] ratio=${planted}`);
    expect(planted, "the planted two-character control does not reproduce a sub-floor paint").toBeLessThan(4.5);

    const alive = await new AxeBuilder({ page })
      .withRules(["color-contrast"])
      .include("nav")
      .analyze();
    const named = alive.violations
      .filter((v) => v.id === "color-contrast")
      .flatMap((v) => v.nodes.map((n) => ({ target: n.target.join(" "), summary: n.failureSummary ?? "" })));
    console.log(`[control axe 2-char] ${JSON.stringify(named)}`);
    expect(
      named.some((n) => /insufficient color contrast/i.test(n.summary)),
      "axe did not name the two-character node with the same paint — the rule itself is not firing, so arm 2 " +
        `proves nothing about the LENGTH. Nodes: ${JSON.stringify(named)}`,
    ).toBe(true);
    // My measurement and axe's own arithmetic must agree on the SAME node — the
    // whole claim of this file is that the numbers are measured, not asserted.
    const axeRatio = Number(/contrast of (\d+\.\d\d)/.exec(named[0].summary)?.[1] ?? "0");
    expect(
      Math.abs(axeRatio - planted),
      `axe reports ${axeRatio} for the planted node where this scan measured ${planted} — the two measurements ` +
        `of the same element disagree`,
    ).toBeLessThanOrEqual(0.05);
    expect(blockersOf(alive).length, "the scoped axe scan found a critical/serious violation").toBeGreaterThan(0);
  });
});
