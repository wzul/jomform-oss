// Measured contrast guard for the selected ADMIN FEEDBACK THREAD ROW's sub-line.
//
// WHY THIS FILE EXISTS (card t_2f2ad15a). The row is the one site in the
// translucent `bg-indigo-50/NN` family that failed WCAG AA, and it was found by
// measuring `--jf-muted`'s painted composite rather than by reading a class name:
//
//   /admin, selected thread row   web/src/app/admin/page.tsx (row tint + sub-line)
//     row tint `bg-indigo-50/25`, ground `--jf-bg` #f7f8f8
//       painted surface #f5f7fa -> --jf-muted #6b7280 = 4.505:1   (12px, floor 4.5)
//     the same row at the /40 tint it carried until card t_2f2ad15a
//       painted surface #f3f6fb -> --jf-muted = 4.463:1  -> axe: serious
//         "insufficient color contrast of 4.46 (foreground color: #6b7280,
//          background color: #f3f6fb, font size: 9.0pt (12px), font weight: normal)"
//     after the fix, `--jf-muted-on-tint` #5b6472 on the same surfaces
//       /25 5.574:1 | /40 5.522:1 | /50 5.481:1 | /60 5.447:1 | white 5.982:1
//
// THE CARD'S LIVE FAILURE DOES NOT REPRODUCE ON CURRENT MAIN, AND THAT IS THE
// POINT OF THE PROBE. Card t_2f2ad15a was filed against `bg-indigo-50/40`, and a
// later change (PR #753) moved this row to `/25`, which lifts --jf-muted to
// 4.505:1 — a PASS by 0.005 under a floor the tint's alpha and the page ground
// behind it own jointly. A 0.005 margin is not a passing state worth defending:
// the same row at the alpha it had one change ago measures 4.463:1 and axe names
// it (the fixture arm below reproduces exactly that). So the token was replaced
// with the design system's own text-on-a-tint pair, which clears the floor on
// every alpha this row can paint and on white, and this file pins the measurement
// rather than the class list.
//
// WHY IT CANNOT BE A UNIT GUARD. `src/lib/tintedSurface.test.ts` deliberately
// owns SOLID tints only: it reads the source, and a translucent tint's composite
// is decided by what is BEHIND it, which no class-name scan can see. That guard is
// correct and is NOT weakened here. This file is the measurement half, and it is
// the only kind of guard that could own `/NN`.
//
// HOW IT IS REACHED WITHOUT A SESSION. /admin needs a super-admin session from the
// Go API, so the reads are stubbed at the browser level (the pattern
// `e2e/feedback-notify-marker.spec.ts` and `e2e/a11y-dialogs.spec.ts` use) — no Go
// server, no session, no database, and the REAL built page renders in a REAL
// browser. `?thread=<id>` is the page's own deep link (it is what the "new feedback
// received" notification sends an operator to), so the SELECTED row — the only
// state where the tint paints — is reached by the app's own route, not by a
// synthetic hover.
//
// WHAT MAKES IT BITE
//   1. REACHABILITY is asserted first: the row must exist and must be PAINTED on
//      the tint (a measured surface that is not white). Without that, a scan of a
//      row that never got selected would report a clean 0 over nothing.
//   2. The ratio is MEASURED from the painted pixel, through a canvas so the
//      renderer does the compositing of the oklab/color-mix tint — never from a
//      class name and never rounded (a rounded number is how a 4.46 reads as 4.5).
//   3. axe runs scoped to the row. `AxeBuilder().include()` THROWS when the subtree
//      is absent, so the axe arm cannot pass vacuously either.
//   4. The CONTROL arm pins the surface this card was filed about — the /40
//      composite, injected with the exact declaration Tailwind emits for the /40
//      utility, `color-mix(in oklab, var(--color-indigo-50) 40%, transparent)` —
//      and reverts the on-tint token to the PRE-FIX `#6b7280`. It must land BELOW
//      the floor and axe must name the violation on that row. If that arm stays
//      above the floor, this spec cannot see the defect class at all and its green
//      reading above proves nothing.
//
// WHICH ARM BITES ON WHAT (measured, so a reader does not have to guess)
//   * Arm 1 (the shipped state) pins the floor on the surface the row ACTUALLY
//     paints, and that axe agrees. **It cannot see a revert of the token**: at the
//     current /25 alpha the pre-fix token measures 4.505:1, still above 4.5, so
//     reverting this fix leaves arm 1 green. Verified by reverting the token and
//     rebuilding — arm 1 passed, arm 2's first measurement failed.
//   * Arm 2's first measurement is therefore the arm that BITES on a revert: it
//     measures the row's own token on the /40 composite of the same tint (5.522:1
//     fixed, 4.463:1 pre-fix) and requires the floor. The measured run with the
//     token reverted: `RED — the replacement token is below the floor on the /40
//     surface: ratio 4.463`.
//   * Arm 2's second measurement is the non-vacuity control: the same surface with
//     the pre-fix token must be BELOW the floor and axe must NAME it (measured:
//     4.463:1, "insufficient color contrast of 4.46 (foreground color: #6b7280,
//     background color: #f3f6fb, font size: 9.0pt (12px), font weight: normal)").
//
// SCOPE BOUNDARIES (deliberate, not oversights)
//   * The SOLID `bg-indigo-50` class is PR 710's and stays guarded by
//     `src/lib/tintedSurface.test.ts`.
//   * The `/50` admin message bubble (both on /admin and on /feedback) sits on a
//     WHITE panel: painted surface #f6f9ff, `--jf-muted` 4.584:1, axe scoped 0
//     violations — a measured PASS, so it is untouched.
//   * The /forms template picker's blank card is `/40` on hover over white:
//     #f8faff -> 4.629:1, a measured PASS.
//   * The builder canvas drop ghost is `bg-indigo-50/50` with text
//     `--jf-primary-on-tint` (`#4338ca`, 7.36:1 on that composite) and its tinted
//     state is not reachable in a held drag: `canvasDragOver` reads `dragging` from
//     refs during render, so the first `dragover` runs the stale closure and
//     returns before `setDropAt` — the tint never paints. Reported, not silently
//     "passed".
//
// Run with:
//   npm run a11y                                 # the whole gate (this file included)
//   npx playwright test e2e/a11y-admin-feedback-row.spec.ts   # this file alone

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
import { blockersOf, expectGateSeesAriaViolation, gateScan } from "./axeGate";

// The gate's own rule — the axe rule SET and the failing IMPACTS — is defined
// once, in `e2e/axeGate.ts`, and `e2e/a11y.spec.ts` runs the same two functions.
// Card t_5aa6c8ac: the local copy that used to sit here declared
// `withTags(["wcag2aa", "wcag21aa"])`, which reached 6 of axe's 105 rules and
// hid every A-level ARIA rule. See axeGate.ts for the measurement.

/** The tokens this card replaced, from its own measurement. */
const PRE_FIX_TOKENS = ":root{--jf-muted:#6b7280;--jf-muted-on-tint:#6b7280;}";

/**
 * The /40 composite this view carried until card t_2f2ad15a's fix, reproduced
 * with the declaration Tailwind emits for `bg-indigo-50/40`:
 * `background-color:color-mix(in oklab, var(--color-indigo-50) 40%, transparent)`.
 * Pinned by attribute so it applies to the REAL row element and axe sees it too.
 */
const FORTY_TINT = (selector: string) =>
  `${selector}{background-color:color-mix(in oklab, var(--color-indigo-50) 40%, transparent) !important;}`;

const SUBJECT = "Pembayaran tersekat";
const THREAD = {
  id: "11111111-1111-1111-1111-111111111111",
  subject: SUBJECT,
  status: "open",
  user_id: "u-2",
  user_email: "filer@example.test",
  created_at: "2026-09-20T02:00:00Z",
  last_activity_at: "2026-09-20T03:00:00Z",
  message_count: 2,
};

/** Every /api/* read the page needs, so it renders the section under test. */
async function stubAdmin(page: Page) {
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
        me: { id: "u1", email: "ops@example.test", name: "Ops", lang: "", is_super_admin: true },
        workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
      }),
    ),
  );
  await page.route(/(.*)\/api\/auth\/me$/, (r) =>
    r.fulfill(json({ id: "u1", email: "ops@example.test", name: "Ops", is_super_admin: true })),
  );
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  // The Feedback section renders only after the overview has loaded
  // (`!data ? Loading…`), so an unstubbed overview means no row to measure.
  await page.route("**/api/admin/overview", (r) =>
    r.fulfill(
      json({
        totals: { workspaces: 0, forms: 0, products: 0, sales: 0, users: 0, media_bytes: 0 },
        growth_24h: { workspaces: 0, forms: 0, products: 0, sales: 0, users: 0 },
        growth_7d: { workspaces: 0, forms: 0, products: 0, sales: 0, users: 0 },
        email: { delivered: 0, bounce: 0, complaint: 0, open: 0, click: 0 },
        spam: { submissions: 0, sales: 0 },
        recent_signups: [],
        active_workspaces: [],
        storage: [],
        quota_bytes: 0,
      }),
    ),
  );
  await page.route("**/api/admin/dead-letters*", (r) => r.fulfill(json({ items: [], total: 0 })));
  await page.route("**/api/admin/suppressions*", (r) =>
    r.fulfill(json({ items: [], page: 1, limit: 20, total: 0 })),
  );
  await page.route("**/api/admin/feedback?*", (r) =>
    r.fulfill(
      json({
        items: [THREAD],
        page: 1,
        limit: 20,
        total: 1,
        counts: { open: 1, in_progress: 0, resolved: 0, closed: 0 },
      }),
    ),
  );
  await page.route("**/api/admin/feedback/*", (r) =>
    r.fulfill(
      json({
        thread: THREAD,
        messages: [
          { id: "m-1", author_kind: "user", body: "pembayaran saya tersekat", created_at: "2026-09-20T02:00:00Z" },
          { id: "m-2", author_kind: "admin", body: "kami sedang menyemak", created_at: "2026-09-20T03:00:00Z" },
        ],
      }),
    ),
  );
}

/**
 * A FRESH browser state at the app's DEFAULT locale: storage cleared and NO
 * `jf_lang` written, so the page renders what a new visitor gets. The browser
 * keeps localStorage between runs, and a `jf_lang` left behind by an earlier run
 * has already produced a false reading on this board.
 */
async function freshState(page: Page) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      localStorage.setItem("jf_workspace", "demo");
    } catch {
      /* storage unavailable */
    }
  });
}

/** The measurement, in the page: paint every ancestor layer back-to-front through
 *  a canvas so the RENDERER composites the tint, then ratio the MEASURED surface
 *  against the element's own computed text colour. */
const MEASURE = (sel: string) => {
  const el = document.querySelector(sel);
  if (!el) throw new Error(`no element for ${sel} — the row moved or never rendered`);
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

/** The WCAG floor for the size/weight of the measured line (large text is 3:1). */
function floorFor(m: Measured): number {
  const px = parseFloat(m.fontSize);
  const bold = Number(m.fontWeight) >= 700;
  return px >= 24 || (bold && px >= 18.66) ? 3 : 4.5;
}

/** Tag the selected row so every measurement below names THAT element, and not a
 *  same-shaped row elsewhere on the page. */
const ROW_SEL = '[data-jf-fb-row="1"]';
const SUB_SEL = `${ROW_SEL} span.break-all`;

/**
 * Open /admin on the SELECTED thread (the page's own `?thread=` deep link) and
 * return the row locator. Asserted: the row exists, the sub-line inside it is
 * unique, so every measurement below is of the element it names.
 */
async function openSelectedRow(page: Page) {
  await freshState(page);
  await stubAdmin(page);
  await page.goto(`/admin?thread=${encodeURIComponent(THREAD.id)}`, {
    waitUntil: "domcontentloaded",
  });
  // The default locale, asserted: a leftover stored language would render this
  // page in a state a new visitor never has.
  expect(
    await page.evaluate(() => window.localStorage.getItem("jf_lang")),
    "this run must start with NO stored locale (fresh state)",
  ).toBeFalsy();
  await expect(page.locator("html")).toHaveAttribute("lang", "en");

  const row = page.getByRole("button", { name: new RegExp(SUBJECT) });
  await expect(row).toHaveCount(1);
  await expect(row).toBeVisible();
  await expect(row.locator("span.break-all")).toHaveCount(1);
  await row.scrollIntoViewIfNeeded();
  await row.evaluate((el: HTMLElement) => el.setAttribute("data-jf-fb-row", "1"));
  return row;
}

/** Measure the ROW's own painted background, so "the tint really painted" is a
 *  measurement rather than an assumption. */
async function measureRow(page: Page): Promise<Measured> {
  return page.evaluate(MEASURE, ROW_SEL);
}

/** Measure the sub-line of the selected row, by its own element. */
async function measureSubLine(page: Page): Promise<Measured> {
  return page.evaluate(MEASURE, SUB_SEL);
}

test.describe("the selected admin feedback row's sub-line", () => {
  test("MEASURES above the contrast floor on the tint, and axe agrees", async ({ page }) => {
    const row = await openSelectedRow(page);

    // (1) NON-VACUITY: the selected row must be PAINTED on the tint. A white
    // surface means the selection never rendered, and a scan of it would be a
    // clean 0 over a state this card is not about.
    const surface = (await measureRow(page)).surface;
    expect(
      surface.toLowerCase(),
      "the selected row is not painted on a tint, so the state under test never rendered",
    ).not.toBe("#ffffff");

    const m = await measureSubLine(page);
    console.log(
      `[admin feedback row] surface=${m.surface} fg=${m.fg} ratio=${m.ratio} ` +
        `${m.fontSize}/${m.fontWeight} layers=${JSON.stringify(m.layers)}`,
    );
    expect(m.ratio, `the sub-line ${JSON.stringify(m.text)} is below its floor`).toBeGreaterThanOrEqual(
      floorFor(m),
    );

    // (2) THE GATE'S OWN RULE, scoped to the row. `include()` makes axe THROW when
    // the subtree is absent, so this cannot pass over nothing.
    const results = await gateScan(page, ROW_SEL);
    const blockers = blockersOf(results);
    expect(
      blockers.map((v) => `${v.id} (${v.impact}): ${v.help} — ${v.nodes[0]?.failureSummary ?? ""}`),
      "axe found critical/serious violations in the selected feedback row",
    ).toEqual([]);

    // (3) The sub-line is an element the axe run actually judged: axe reports it
    // either as a pass or as a violation, never as neither.
    const analysed = [
      ...results.passes.filter((p) => p.id === "color-contrast"),
      ...results.violations.filter((v) => v.id === "color-contrast"),
    ].flatMap((r) => r.nodes.map((n) => n.target.join(" ")));
    expect(
      analysed.some((t) => t.includes("break-all")),
      `axe's color-contrast rule produced no node for the sub-line, so the scan above covered ` +
        `something else. Nodes: ${JSON.stringify(analysed)}`,
    ).toBe(true);

    // (4) NON-VACUITY OF (2), the half card t_5aa6c8ac exists for: a prohibited
    // ARIA state planted in THIS row must be reported by the same scan.
    await expectGateSeesAriaViolation(page, ROW_SEL, "the selected admin feedback row");
    await expect(row).toBeVisible();
  });

  test("CONTROL: the /40 surface this card was filed about fails with the pre-fix token, and the fix holds on it", async ({
    page,
  }) => {
    // The /40 composite this view carried until this card's fix. Injected as a
    // style tag naming the REAL row element, so axe sees the same computed
    // background the measurement does.
    await openSelectedRow(page);
    await page.evaluate(() => {
      document.querySelector('[data-jf-fb-row="1"]')!.setAttribute("data-tint-probe", "40");
    });
    await page.addStyleTag({
      content: FORTY_TINT('button[data-tint-probe="40"]'),
    });

    // ARM 1 — the FIXED token on the /40 surface. This is the claim the fix
    // makes: it holds for the tint alpha, not merely for the /25 one, so a later
    // change to the alpha cannot put this row back under the floor.
    const fixed = await measureSubLine(page);
    console.log(`[control /40 fixed token] surface=${fixed.surface} fg=${fixed.fg} ratio=${fixed.ratio}`);
    expect(
      fixed.surface.toLowerCase(),
      "the /40 control surface did not paint, so this arm measured something else",
    ).not.toBe("#ffffff");
    expect(
      fixed.ratio,
      `the replacement token is below the floor on the /40 surface: ${JSON.stringify(fixed)}`,
    ).toBeGreaterThanOrEqual(floorFor(fixed));

    // ARM 2 — the SAME surface with the PRE-FIX token. If this stays above the
    // floor the spec cannot see the defect class at all.
    await page.addStyleTag({ content: PRE_FIX_TOKENS });
    const pre = await measureSubLine(page);
    console.log(`[control /40 pre-fix token] surface=${pre.surface} fg=${pre.fg} ratio=${pre.ratio}`);
    expect(
      pre.surface,
      "both control arms must run on the same painted surface, or the comparison is not one",
    ).toBe(fixed.surface);
    expect(
      pre.ratio,
      `with the PRE-FIX token on the /40 surface NOTHING measured below the floor, so this spec ` +
        `cannot see the defect this card is about. Measured: ${JSON.stringify(pre)}`,
    ).toBeLessThan(floorFor(pre));

    // And axe must name it on that same row — the arm is a real reproduction of
    // the card's original reading, not just a low number.
    const results = await gateScan(page, ROW_SEL);
    const named = results.violations
      .filter((v) => v.id === "color-contrast")
      .flatMap((v) => v.nodes.map((n) => n.failureSummary ?? ""));
    expect(
      named.join(" | "),
      "axe did not name the control surface as a color-contrast violation, so this arm is not a " +
        "reproduction of the defect the card was filed about",
    ).toMatch(/insufficient color contrast/i);

    // …and the rule set behind this control can see an A-level ARIA defect in the
    // same row (card t_5aa6c8ac), not only the contrast rule it is aimed at.
    await expectGateSeesAriaViolation(page, ROW_SEL, "the admin feedback row control");
  });
});
