// MEASURED guard for the DASHBOARD's Recent-sales status pill (card t_df0857c6).
//
// THE DEFECT (found by QA while verifying v1.21.30 in prod, card t_c13b0ed1 —
// pre-existing, NOT a regression of PR #1103)
//
//   `web/src/app/dashboard-client.tsx`'s status-badge helper fell back to the
//   EMPTY STRING:
//
//     const badge = (s: string) => ({ … })[s] || "";
//
//   so any sale status its map did not name rendered the wrapper's classes only
//   (`whitespace-nowrap rounded-full border px-2.5 py-0.5 text-[11px]
//   font-medium `) — a pill with no background and no text colour. `refunded` and
//   `authorized` are BOTH legal statuses (`sales_status_check`, migration
//   000002_skip_capture.up.sql) and both reach this table. Measured live on prod
//   (v1.21.30, real Chromium, real prod stylesheet, the badge cell of the
//   dashboard table):
//
//     refunded   -> no colour token in the className; rgb(23, 24, 28) on #ffffff
//     authorized -> same
//     paid       -> bg-blue-50 text-blue-700
//     cancelled  -> bg-slate-100 + text-[var(--jf-muted-on-tint)]
//
//   `/sales` paints the same `refunded` sale orange, so the dashboard was the one
//   surface where a real status painted a colourless pill — and it disagreed with
//   `/sales` about what a refunded sale looks like.
//
// WHY THIS IS MEASURED AND NOT AN axe VIOLATION
//
//   An uncoloured pill is not a contrast failure: dark ink on the card's white
//   MEASURED 17.739:1 on prod. axe is right to pass it, and that is exactly why
//   this defect survived a release. It is a COVERAGE defect (a status with no
//   arm), so the assertion here is about the paint: the pill must carry a tint of
//   its own, and two statuses that differ on /sales must not paint the same pill
//   here. The sibling `src/lib/statusBadgeArms.test.ts` answers the same question
//   for every status the DATABASE accepts, from the source.
//
// WHAT IS ASSERTED, AND WHY EACH ARM IS NEEDED
//
//   1. NON-VACUITY: `/` renders (no redirect to /login), the Recent-sales table
//      holds the four fixture rows, and the named `paid` arm paints a surface that
//      is not white — so the measurement is reading a real painted pill.
//   2. `refunded` paints the SAME surface as a probe carrying `bg-orange-50`, the
//      class `/sales` uses for it, and a different surface from `paid`. That is
//      the card's claim (a). The probe is the design system's own class list,
//      measured through the same canvas path, so no palette value is hardcoded
//      here.
//   3. `authorized` (no map arm) paints the SAME surface and the SAME text colour
//      as `cancelled` (the named `bg-slate-100` + `--jf-muted-on-tint` arm) and as
//      the probe of that pair. That is the card's claim (b): an unnamed status
//      takes the on-tint pair rather than an uncoloured pill.
//   4. The pill's own text clears its floor on its own painted surface.
//   5. axe runs scoped to the table (`include()` THROWS on an absent subtree, so
//      it cannot pass over nothing) and reports no critical/serious violation.
//   6. CONTROL (the arm that makes the rest bite): the PRE-FIX class list is
//      replayed on the SHIPPED `refunded` element — the exact bytes the helper
//      produced before the fix — and it must land on the card's white, NOT on the
//      orange probe, and its text must not be the on-tint token. If that
//      discriminator could not see an uncoloured pill, the assertion above would
//      be green on the tree that rendered the defect.
//
// HOW IT IS REACHED WITHOUT A SESSION. `/` is gated on the SERVER
// (web/src/middleware.ts) on the `jf_session` cookie's presence, and the reads are
// stubbed at the browser level (the pattern `e2e/a11y-singlechar-text.spec.ts` and
// `e2e/a11y-admin-feedback-row.spec.ts` use): no Go server, no session, no
// database, and the REAL built page renders in a REAL browser. The rows come from
// the page's own `dashboard-bundle` round-trip, so the status map under test is the
// one the app ships.
//
// Run with:
//   npm run a11y                                                # the whole gate
//   npx playwright test e2e/a11y-dashboard-status-badge.spec.ts  # this file alone

import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import type { AxeResults, Result } from "axe-core";

import { signIn } from "./auth";

// The gate's own rule, kept identical to e2e/a11y.spec.ts so the two cannot drift.
const IMPACTS = ["critical", "serious"] as const;

function blockersOf(results: AxeResults): Result[] {
  return results.violations.filter((v) =>
    IMPACTS.includes((v.impact ?? "") as (typeof IMPACTS)[number]),
  );
}

/** The four statuses this spec paints. `authorized` is the unnamed one. */
const SALES = [
  { ref_code: "REF-PAID", status: "paid" },
  { ref_code: "REF-REFUNDED", status: "refunded" },
  { ref_code: "REF-AUTHORIZED", status: "authorized" },
  { ref_code: "REF-CANCELLED", status: "cancelled" },
].map((s) => ({
  ...s,
  total_sen: 4200,
  payment_method: "fpx",
  buyer: "Ali",
  created_at: "2026-10-01T02:00:00Z",
}));

/** The design system's own class lists, measured as PROBES in the same page. */
const PROBE_BLUE = "bg-blue-50";
const PROBE_ORANGE = "bg-orange-50";
const PROBE_ON_TINT = "bg-slate-100 text-[var(--jf-muted-on-tint)]";
/**
 * The wrapper the badge renders, with NO colour arm — i.e. exactly what the
 * pre-fix `|| ""` fallback produced on the shipped element. Kept as the class
 * list the control arm replays, byte for byte from the component.
 */
const PRE_FIX_PILL = "whitespace-nowrap rounded-full border px-2.5 py-0.5 text-[11px] font-medium ";

const PROBES = [PROBE_BLUE, PROBE_ORANGE, PROBE_ON_TINT, PRE_FIX_PILL];

// ─── the in-page measurement ────────────────────────────────────────────────
//
// Installed as an init script so it is present on the document under test. The
// surface is the composite of every ancestor layer, painted back-to-front through
// a canvas so the RENDERER does the compositing, and the text colour is painted
// over that surface the same way: nothing here is read off a class name and
// nothing is rounded (a rounded number is how a 4.46 reads as 4.5).
const MEASURE_SRC = `
window.__jfMeasure = (el) => {
  const cv = document.createElement("canvas");
  cv.width = cv.height = 1;
  const ctx = cv.getContext("2d", { willReadFrequently: true });
  const paint = (base, c) => {
    ctx.clearRect(0, 0, 1, 1);
    ctx.fillStyle = base;
    ctx.fillRect(0, 0, 1, 1);
    if (c) { ctx.fillStyle = c; ctx.fillRect(0, 0, 1, 1); }
    const d = ctx.getImageData(0, 0, 1, 1).data;
    return { r: d[0], g: d[1], b: d[2] };
  };
  const hex = (c) => "#" + [c.r, c.g, c.b].map((v) => v.toString(16).padStart(2, "0")).join("");
  const lin = (v) => (v / 255 <= 0.04045 ? v / 255 / 12.92 : Math.pow((v / 255 + 0.055) / 1.055, 2.4));
  const lum = (c) => 0.2126 * lin(c.r) + 0.7152 * lin(c.g) + 0.0722 * lin(c.b);
  const cs = getComputedStyle(el);
  const layers = [];
  for (let n = el; n; n = n.parentElement) {
    const c = getComputedStyle(n).backgroundColor;
    if (c !== "rgba(0, 0, 0, 0)" && c !== "transparent") layers.push(c);
    if (n === document.body) break;
  }
  let bg = { r: 255, g: 255, b: 255 };
  for (let i = layers.length - 1; i >= 0; i--) {
    bg = paint("rgb(" + bg.r + "," + bg.g + "," + bg.b + ")", layers[i]);
  }
  const fg = paint("rgb(" + bg.r + "," + bg.g + "," + bg.b + ")", cs.color);
  const ratio = (Math.max(lum(fg), lum(bg)) + 0.05) / (Math.min(lum(fg), lum(bg)) + 0.05);
  return {
    text: (el.textContent || "").trim().slice(0, 40),
    className: String(el.className),
    surface: hex(bg),
    fg: hex(fg),
    ratio: Math.round(ratio * 1000) / 1000,
    fontSize: cs.fontSize,
    fontWeight: cs.fontWeight,
    layers: layers,
  };
};

// Probe spans carrying a class list the APP also uses (the classes must exist in
// the built stylesheet, which is why they are the design system's own, never one
// invented here). Appended to the body on a white host so their composite has the
// same base a card gives the badges, measured by the same function, then removed.
window.__jfProbe = (classes) => {
  const host = document.createElement("div");
  host.setAttribute("data-jf-probe", "1");
  host.className = "bg-white";
  const spans = classes.map((c) => {
    const s = document.createElement("span");
    s.className = c;
    s.textContent = "probe";
    host.appendChild(s);
    return s;
  });
  document.body.appendChild(host);
  const out = spans.map((s) => window.__jfMeasure(s));
  host.remove();
  return out;
};
`;

type Measured = {
  text: string;
  className: string;
  surface: string;
  fg: string;
  ratio: number;
  fontSize: string;
  fontWeight: string;
  layers: string[];
};

/** The WCAG floor for the size/weight of the measured line (large text is 3:1). */
function floorFor(m: Measured): number {
  const px = parseFloat(m.fontSize);
  const bold = Number(m.fontWeight) >= 700;
  return px >= 24 || (bold && px >= 18.66) ? 3 : 4.5;
}

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
        me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "en", is_super_admin: false },
        workspaces: [{ id: "a1", name: "Demo", slug: "demo", role: "admin" }],
        chip_status: null,
        keys: [],
        domains: [],
        webhooks: [],
        social: [],
        einvoice: null,
        products: [],
        summary: { counts: { pending: 0, paid: 0, confirmed: 0, failed: 0 }, revenue_sen: 0 },
        sales: { items: SALES, page: 1, limit: 20, total: SALES.length },
        form_analytics: [],
        product_analytics: [],
      }),
    ),
  );
  await page.route(/(.*)\/api\/rest\/sales(\?.*)?$/, (r) =>
    r.fulfill(json({ items: SALES, page: 1, limit: 10, total: SALES.length })),
  );
  await page.route(/(.*)\/api\/auth\/me$/, (r) =>
    r.fulfill(json({ id: "u1", email: "merchant@example.com", name: "Merchant", is_super_admin: false })),
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

/** Open the dashboard on the fixture rows and assert it really rendered. */
async function openDashboard(page: Page, baseURL: string | undefined) {
  await freshState(page);
  await page.addInitScript({ content: MEASURE_SRC });
  await page.context().clearCookies();
  await signIn(page, baseURL);
  await stubAPI(page);
  await page.goto("/", { waitUntil: "domcontentloaded" });

  // No redirect: a dashboard path without the cookie is bounced to /login by
  // web/src/middleware.ts, and a scan of the login page would be a clean 0 over
  // the surface this file is about.
  expect(new URL(page.url()).pathname, "/ redirected — the page under test did not render").toBe("/");
  expect(
    await page.evaluate(() => window.localStorage.getItem("jf_lang")),
    "this run must start with NO stored locale (fresh state)",
  ).toBeFalsy();
  await expect(page.locator("html")).toHaveAttribute("lang", "en");

  const rows = page.locator("table tbody tr");
  await expect(rows, "the Recent-sales table did not render the fixture rows").toHaveCount(SALES.length);
  for (const s of SALES) {
    // The badge under test is the row's FIRST pill — the status cell comes before
    // the email cell, and asserting its text pins WHICH status we measured.
    await expect(
      rows.filter({ hasText: s.ref_code }).locator("span.rounded-full").first(),
      `the ${s.status} row's status badge is not the status cell this spec measures`,
    ).toHaveText(s.status);
  }
}

/** The status pill of the fixture row whose reference is `ref`. */
function badgeOf(page: Page, ref: string) {
  return page.locator("table tbody tr", { hasText: ref }).locator("span.rounded-full").first();
}

async function measure(page: Page, ref: string): Promise<Measured> {
  return (await badgeOf(page, ref).evaluate(
    (el) => (window as unknown as { __jfMeasure: (e: Element) => Measured }).__jfMeasure(el),
  )) as Measured;
}

async function probes(page: Page): Promise<Measured[]> {
  return (await page.evaluate(
    (classes) => (window as unknown as { __jfProbe: (c: string[]) => Measured[] }).__jfProbe(classes),
    PROBES,
  )) as Measured[];
}

test.describe("the dashboard's Recent-sales status pill", () => {
  test("MEASURES: `refunded` is the orange entry /sales uses, an unnamed status takes the on-tint pair", async ({
    page,
    baseURL,
  }) => {
    await openDashboard(page, baseURL);

    const [blue, orange, onTint] = await probes(page);
    const paid = await measure(page, "REF-PAID");
    const refunded = await measure(page, "REF-REFUNDED");
    const authorized = await measure(page, "REF-AUTHORIZED");
    const cancelled = await measure(page, "REF-CANCELLED");
    const all = { paid, refunded, authorized, cancelled };
    console.log(
      `[dashboard status pill] ${JSON.stringify(all)}\n[probes] orange=${orange.surface} ` +
        `onTint=${onTint.surface}/${onTint.fg} blue=${blue.surface}`,
    );

    // (1) NON-VACUITY: the named `paid` arm paints a surface of its own. If this
    // is the card's white, the measurement is not reading a painted pill and every
    // assertion below would be about nothing.
    expect(paid.surface.toLowerCase(), "the `paid` control pill painted no surface").not.toBe("#ffffff");
    expect(paid.surface, "the `paid` pill is not the blue arm's paint").toBe(blue.surface);

    // (2) CLAIM (a): `refunded` paints the orange class `/sales` uses for it — the
    // defect was that it painted NOTHING (prod: rgb(23,24,28) on #ffffff).
    expect(
      refunded.surface,
      `the \`refunded\` pill does not paint the orange surface /sales uses for it ` +
        `(measured ${JSON.stringify(refunded)})`,
    ).toBe(orange.surface);
    expect(
      refunded.surface,
      "the `refunded` and `paid` pills paint the same surface — the status maps have collapsed",
    ).not.toBe(paid.surface);

    // (3) CLAIM (b): a status the map does not name falls back to the design
    // system's on-tint pair — the SAME pill the named `cancelled` arm paints.
    expect(authorized.surface, "an unnamed status painted a different surface from the on-tint arm").toBe(
      onTint.surface,
    );
    expect(authorized.surface, "an unnamed status painted a different surface from `cancelled`").toBe(
      cancelled.surface,
    );
    expect(authorized.fg, "an unnamed status took a different text colour from `cancelled`").toBe(cancelled.fg);
    expect(onTint.fg, "the on-tint probe and the unnamed status disagree about the token").toBe(authorized.fg);

    // (4) THE PILL'S TEXT CLEARS ITS FLOOR on the surface it actually paints.
    for (const [status, m] of Object.entries(all)) {
      expect(
        m.ratio,
        `the ${status} pill's text is below its floor on the surface it paints: ${JSON.stringify(m)}`,
      ).toBeGreaterThanOrEqual(floorFor(m));
    }

    // (5) The gate's own rule, scoped to the table. `include()` makes axe THROW
    // when the subtree is absent, so this cannot pass over nothing. It is NOT the
    // detector for this card's defect (an uncoloured pill measures 17.7:1) — it is
    // here so a pill that becomes a real violation cannot ride along.
    const results = await new AxeBuilder({ page })
      .withTags(["wcag2aa", "wcag21aa"])
      .include("table")
      .analyze();
    const blockers = blockersOf(results);
    expect(
      blockers.map((v) => `${v.id} (${v.impact}): ${v.help} — ${v.nodes[0]?.failureSummary ?? ""}`),
      "axe found critical/serious violations in the Recent-sales table",
    ).toEqual([]);
    const judged = [
      ...results.passes.filter((p) => p.id === "color-contrast"),
      ...results.violations.filter((v) => v.id === "color-contrast"),
      ...results.incomplete.filter((i) => i.id === "color-contrast"),
    ].flatMap((r) => r.nodes.map((n) => n.target.join(" ")));
    expect(
      judged.length,
      "axe's color-contrast rule judged no node in the table, so the scan above covered something else",
    ).toBeGreaterThan(0);
  });

  test("CONTROL: the pre-fix class list lands off the orange, so the assertion above bites", async ({
    page,
    baseURL,
  }) => {
    await openDashboard(page, baseURL);

    // PROBES is [blue, orange, on-tint, pre-fix pill] — index 1 is the orange.
    const [, orange] = await probes(page);
    const refunded = await measure(page, "REF-REFUNDED");
    const authorized = await measure(page, "REF-AUTHORIZED");
    expect(
      refunded.surface,
      "the SHIPPED pill is not on the orange surface, so this control is not a control",
    ).toBe(orange.surface);

    // Replay the PRE-FIX bytes on the shipped element: the wrapper's classes only,
    // which is exactly what `({ … })[s] || ""` produced for a status the map did
    // not name. This is the state prod was in, on the element that painted it.
    const before = refunded.className;
    await badgeOf(page, "REF-REFUNDED").evaluate((el, cls) => {
      el.setAttribute("class", cls);
    }, PRE_FIX_PILL);
    const stripped = await measure(page, "REF-REFUNDED");
    console.log(`[control: pre-fix pill] ${JSON.stringify(stripped)}`);

    expect(stripped.text, "the control measured a different element").toBe("refunded");
    expect(
      stripped.surface,
      `an uncoloured pill is still on the orange surface, so the measurement cannot see the ` +
        `defect this file exists for: ${JSON.stringify(stripped)}`,
    ).not.toBe(orange.surface);
    expect(
      stripped.surface.toLowerCase(),
      "an uncoloured pill does not fall back to the card behind it — the composite is not being read",
    ).toBe("#ffffff");
    expect(
      stripped.fg,
      "the uncoloured pill took the on-tint token, so the two arms are not distinguishable",
    ).not.toBe(authorized.fg);

    // …and restoring the app's own class list puts it back on the orange, so the
    // discriminator is the class list and not the element, the viewport or a
    // one-off rendering.
    await badgeOf(page, "REF-REFUNDED").evaluate((el, cls) => {
      el.setAttribute("class", cls);
    }, before);
    const restored = await measure(page, "REF-REFUNDED");
    expect(restored.surface, "restoring the app's own classes did not restore the orange pill").toBe(
      orange.surface,
    );
  });
});
