// Regression guard: the ACTION BUTTONS on a `/subscriptions` row must clear
// WCAG AA on the surface each one is ACTUALLY painted on — in every state the
// pointer puts it in, not just the resting one.
//
// THE DEFECT (card t_0fdbe189, whose two named nodes are these)
//
//   The card named the row's update-card button — `text-sky-600` (#0084d1),
//   4.022:1 on white — and required the number MEASURED on the painted pixel
//   rather than argued from source. Measuring it, in Chromium, on the built app,
//   with the row rendered and the API stubbed, found the sibling too:
//
//     update card   text-sky-600  #0084d1   rest #ffffff 4.022:1   (the named node)
//     cancel        text-rose-600 #ec003f   rest #ffffff 4.527:1   PASSES at rest
//
//   …and then measured the state nobody had looked at, because the class list
//   said `hover:bg-rose-50` and everyone read the hover surface off the class:
//
//     both buttons, HOVERED, are painted on #f3f4f6 — NOT on the rose-50/sky-50
//     their `hover:bg-*` named, and NOT on white. (Those utilities were dead —
//     the unlayered rule won — and card t_ac414593 has since DELETED them. This
//     file still pins the pixel, so deleting them is proved to be a no-op and a
//     later move of the `.jf-*` rules into a layer cannot pass unnoticed.)
//
//   `.jf-btn-ghost` declares its own three backgrounds in `globals.css` and those
//   rules are UNLAYERED, so they beat Tailwind's `@layer utilities` regardless of
//   specificity (measured, this file pins the pixel). On that surface the two
//   tokens measured 5.321:1 (sky-600 is already gone) and **4.114:1 for the rose
//   cancel button — under the 4.5:1 floor**, and 3.657:1 while pressed. The
//   resting surface PASSES, which is why neither axe nor the source guards had a
//   finding: axe judges the rendered state and never hovers, and a source guard
//   that attributes a token to the nearest `bg-*` utility cannot see a background
//   that lives in a CSS rule (that blind spot is closed by
//   `src/lib/ghostButtonInk.test.ts`, owned by this card).
//
// WHY A BROWSER MEASUREMENT AND NOT A CLASS-NAME ASSERTION
//
//   The acceptance for this card is a RATIO on the surface the node is painted
//   on, and the ratio depends on the composited pixel — the button's own
//   background changing with the pointer, and the opacity of a disabled button.
//   A grep cannot do that arithmetic, and the cascade that decides it is not
//   visible in the class list at all. So this file drives the REAL page:
//
//     1. an axe scan scoped to the row, with the repo's own rule (critical /
//        serious fail), which is the same assertion the page-level gate makes; and
//     2. a canvas-painted measurement of each button AT REST, HOVERED and
//        PRESSED, so the numbers are in the run log and a near-miss cannot round
//        into a pass.
//
//   The colours and the COMPOSITED background are read from `getComputedStyle`
//   and round-tripped through a 1x1 canvas, so the renderer does the colour-space
//   conversion (Tailwind v4 emits oklch/lab, not rgb) and the compositing for
//   both the element's own `opacity` and every ancestor's background.
//
// HOW THE ROW IS REACHED WITHOUT A SESSION OR A BACKEND. The page renders from
//   `dashboard-bundle` + `/api/rest/subscriptions`, so both are stubbed at the
//   browser level (the pattern `e2e/a11y-products.spec.ts` and
//   `e2e/a11y-chat-card.spec.ts` use): the real component, in a real browser,
//   with no Go server, no session cookie and no database.
//
// Run with:
//
//   npm run a11y                                           # the whole family (this file included)
//   npx playwright test e2e/a11y-subscriptions-row.spec.ts  # this file alone
import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import type { AxeResults, Result } from "axe-core";

import { signIn } from "./auth";
import { DICT } from "../src/components/i18nDict";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means. See e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

// The gate's own rule, kept identical to e2e/a11y.spec.ts so the two cannot drift.
const IMPACTS = ["critical", "serious"] as const;
const IMPACT_LABEL = IMPACTS as readonly string[];

function blockersOf(results: AxeResults): Result[] {
  return results.violations.filter((v) => IMPACT_LABEL.includes(v.impact ?? ""));
}

/** WCAG 2.x AA floor. */
const AA_FLOOR = 4.5;

/**
 * The PRE-FIX ratios, pinned as floors so a green run has to have MEASURED
 * better than the defect rather than merely cleared 4.5:
 *   * 4.022 — `text-sky-600` (#0084d1) on white, the number this card's body
 *     reports for the update-card button (and the value the sibling a11y specs
 *     pinned on the /chat badge);
 *   * 4.114 — `text-rose-600` (#ec003f) on the ghost button's own hover surface
 *     `#f3f4f6`, measured in Chromium on this worktree with the row rendered.
 */
const PRE_FIX_UPDATE_RATIO = 4.022;
const PRE_FIX_CANCEL_HOVER_RATIO = 4.114;

/** The button's own three backgrounds, from `globals.css` (`.jf-btn-ghost`). */
const GHOST_REST = "#ffffff";
const GHOST_HOVER = "#f3f4f6";
const GHOST_PRESSED = "#e5e7eb";
/** The shipped steps: `text-sky-700` / `text-rose-700`. */
const SKY_700 = "#0069a8";
const ROSE_700 = "#c70036";

const EN = {
  updateCard: DICT["sub.updateCard"].en,
  cancel: DICT["sub.cancel"].en,
  active: DICT["sub.active"].en,
};

const CUSTOMER = "Ali";

const ME = {
  id: "u1",
  email: "merchant@example.com",
  name: "Merchant",
  lang: "en",
  is_super_admin: false,
};

const BUNDLE = {
  me: ME,
  workspaces: [{ id: "a1", name: "Demo", slug: "demo", role: "admin" }],
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
 * One ACTIVE row, so both write actions render (`canWrite && status !==
 * "cancelled"`), on a merchant whose card needs updating — that is the state the
 * rose chip beside the buttons also paints.
 */
const SUBS = {
  items: [
    {
      id: "s1",
      customer: CUSTOMER,
      product_id: "p1",
      product_name: "Alpha plan",
      amount_sen: 2500,
      period: "month",
      status: "active",
      next_charge_at: "2026-11-01T00:00:00Z",
      retry_attempts: 0,
      card_update_pending: true,
    },
  ],
  page: 1,
  limit: 10,
  total: 1,
};

async function stubAPI(page: Page) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST, specific routes after: Playwright resolves matching routes in
  // reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(ME)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/rest\/subscriptions(\?.*)?$/, (r) => r.fulfill(json(SUBS)));
}

/**
 * A FRESH browser state — cleared storage, English, one workspace selected —
 * because this host keeps localStorage between runs and a `jf_lang` left by an
 * earlier card renders a locale this file never asserts.
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

/** The row, by the customer its own data renders. */
const ROW = "table tbody tr";

/** The two write buttons of the row, by the `title` their own markup carries. */
const UPDATE_BTN = `button[title="${EN.updateCard}"]`;
const CANCEL_BTN = `button[title="${DICT["sub.tokenRetained"].en}"]`;

/** Get the subscriptions row on screen and return its locator. */
async function openRow(page: Page) {
  await stubAPI(page);
  await freshState(page);
  await page.goto("/subscriptions", { waitUntil: "networkidle" });
  const row = page.locator(ROW).filter({ hasText: CUSTOMER });
  // REACHABILITY IS ASSERTED: a page that rendered the loading shell or bounced
  // to /landing has no row, and a scan or a measurement of it would prove nothing.
  await expect(
    row,
    "the subscription row must render — otherwise this file proves nothing",
  ).toHaveCount(1, { timeout: 30_000 });
  for (const [sel, name] of [
    [UPDATE_BTN, EN.updateCard],
    [CANCEL_BTN, EN.cancel],
  ] as const) {
    await expect(
      page.locator(sel),
      `the row's ${name} button must render — the node this file measures is not on the page`,
    ).toHaveCount(1);
  }
  return row;
}

type Measure = {
  what: string;
  text: string;
  fg: string;
  bg: string;
  ownBg: string;
  opacity: string;
  fontSize: string;
  ratio: number;
};

/**
 * Measure the painted contrast of one node, in the page.
 *
 * Both the foreground and the background go through a 1x1 canvas: the background
 * is every ancestor's colour painted in order (so what is measured is the pixel
 * the user sees, not the element's own often-transparent background), and the
 * foreground is the element's own colour composited over it at the element's
 * `opacity` — which matters for a `disabled:` button, whose label is painted at
 * 0.55 of it.
 */
async function measure(page: Page, selector: string, what: string): Promise<Measure> {
  return page.evaluate(
    ({ selector, what }) => {
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

      const el = document.querySelector(selector);
      if (!el) throw new Error(`no element at ${selector} — the row's markup moved`);

      // The composited background: outermost first, then each layer over it.
      const layers: string[] = [];
      for (let n: Element | null = el; n; n = n.parentElement) {
        const c = getComputedStyle(n).backgroundColor;
        if (c !== "rgba(0, 0, 0, 0)" && c !== "transparent") layers.push(c);
        if (n === document.body) break;
      }
      let bg = { r: 255, g: 255, b: 255 };
      for (let i = layers.length - 1; i >= 0; i--) {
        bg = paint(`rgb(${bg.r}, ${bg.g}, ${bg.b})`, layers[i]);
      }
      const cs = getComputedStyle(el);
      // The element's own opacity is applied by the compositor AFTER the colour
      // is resolved, so the painted pixel is `alpha * colour + (1-alpha) * behind`.
      const alpha = Number(cs.opacity);
      const painted = paint(`rgb(${bg.r}, ${bg.g}, ${bg.b})`, cs.color);
      const composited =
        alpha < 1
          ? {
              r: Math.round(alpha * painted.r + (1 - alpha) * bg.r),
              g: Math.round(alpha * painted.g + (1 - alpha) * bg.g),
              b: Math.round(alpha * painted.b + (1 - alpha) * bg.b),
            }
          : painted;

      return {
        what,
        text: (el.textContent || "").trim(),
        fg: hex(composited),
        bg: hex(bg),
        ownBg: cs.backgroundColor,
        opacity: cs.opacity,
        fontSize: cs.fontSize,
        ratio: ratio(composited, bg),
      };
    },
    { selector, what },
  );
}

/**
 * The ghost button carries `transition: background-color 0.15s`, so a measurement
 * taken immediately after `hover()` reads a MID-TRANSITION pixel (measured: the
 * first attempt read #fbfbfc, on its way from #ffffff to #f3f4f6). The transition
 * is waited out, so what is asserted is a settled state and not a frame of an
 * animation.
 */
const TRANSITION_SETTLE_MS = 500;

test.describe("the /subscriptions action-row buttons clear AA on the surfaces they paint", () => {
  test("axe finds no critical/serious violation in the row", async ({ page }) => {
    await openRow(page);

    // Scoped to the row, so the scan is about the markup this card exists for and
    // cannot be satisfied by an unrelated clean region. axe THROWS when the
    // included context is absent, so this cannot pass vacuously — the row's
    // presence is asserted first anyway, so the failure says which happened.
    const results = await new AxeBuilder({ page }).include(ROW).analyze();
    const blockers = blockersOf(results);

    // The numbers are the acceptance evidence for this card, not just pass/fail.
    console.log(
      `subscriptions row axe: ${blockers.length} critical/serious violation(s)\n` +
        blockers
          .map(
            (v) =>
              `  - ${v.id} (${v.impact}): ${v.help}\n` +
              v.nodes
                .map((n) => `      ${n.target.join(" ")} — ${n.failureSummary?.split("\n")[0] ?? ""}`)
                .join("\n"),
          )
          .join("\n"),
    );

    // The rule that RAN is asserted: a scan that analysed nothing must not read as
    // a clean row. axe always emits `color-contrast` in `passes`, `incomplete` or
    // `violations` when it ran over text.
    const ranContrast = [...results.passes, ...results.incomplete, ...results.violations].some(
      (v) => v.id === "color-contrast",
    );
    expect(
      ranContrast,
      "the color-contrast rule did not run over the row at all — a scan that analysed nothing " +
        "cannot be read as a clean row",
    ).toBe(true);

    expect(blockers, "the row must have no critical/serious axe violations").toEqual([]);
  });

  test("every button label MEASURES above the AA floor at rest, hovered and pressed", async ({
    page,
  }) => {
    test.setTimeout(120_000);
    await openRow(page);

    const update = page.locator(UPDATE_BTN);
    const cancel = page.locator(CANCEL_BTN);

    const measured: Measure[] = [];
    const record = async (what: string, selector: string) => {
      const m = await measure(page, selector, what);
      measured.push(m);
      console.log(
        `${m.what}: text=${JSON.stringify(m.text)} fg=${m.fg} on bg=${m.bg} (element bg ` +
          `${m.ownBg}, opacity ${m.opacity}) ${m.fontSize} — ${m.ratio.toFixed(3)}:1`,
      );
      return m;
    };

    // AT REST — the state every axe scan sees, and the only state the source
    // guards that could not read `.jf-btn-ghost`'s CSS could reason about.
    const updateRest = await record("update card (rest)", UPDATE_BTN);
    const cancelRest = await record("cancel (rest)", CANCEL_BTN);

    // HOVERED — the surface moves to the ghost button's own :hover paint.
    await update.hover();
    await page.waitForTimeout(TRANSITION_SETTLE_MS);
    const updateHover = await record("update card (hover)", UPDATE_BTN);
    await cancel.hover();
    await page.waitForTimeout(TRANSITION_SETTLE_MS);
    const cancelHover = await record("cancel (hover)", CANCEL_BTN);

    // PRESSED — the ghost button darkens one more step while the mouse is down.
    // The pointer is RELEASED OFF the button (`mouse.move` before `mouse.up`), so
    // no `click` fires: a click here would open the cancel confirmation and put
    // the card-update flow in flight, changing the row under the measurement.
    await page.mouse.down();
    await page.waitForTimeout(TRANSITION_SETTLE_MS);
    const cancelDown = await record("cancel (pressed)", CANCEL_BTN);
    await page.mouse.move(0, 0);
    await page.mouse.up();
    await page.waitForTimeout(TRANSITION_SETTLE_MS);

    await update.hover();
    await page.waitForTimeout(TRANSITION_SETTLE_MS);
    await page.mouse.down();
    await page.waitForTimeout(TRANSITION_SETTLE_MS);
    const updateDown = await record("update card (pressed)", UPDATE_BTN);
    await page.mouse.move(0, 0);
    await page.mouse.up();
    // Reset the pointer so a later assertion cannot inherit a hovered state.
    await page.mouse.move(0, 0);
    await page.waitForTimeout(TRANSITION_SETTLE_MS);

    // The LABELS measured are asserted, not assumed: a selector that stopped
    // matching would otherwise measure whatever moved into its place.
    expect(updateRest.text, "the node measured must be the update-card button").toBe(EN.updateCard);
    expect(cancelRest.text, "the node measured must be the cancel button").toBe(EN.cancel);

    // The SURFACES are pinned, not only the ratios. `#f3f4f6` is the whole point
    // of this file: the class list says `hover:bg-sky-50` / `hover:bg-rose-50` and
    // those utilities do NOT take effect (they are in `@layer utilities`, the
    // `.jf-btn-ghost:hover` rule is not layered, so the unlayered rule wins). If
    // this expectation moves, the cascade has moved — re-measure and re-derive
    // `src/lib/ghostButtonInk.test.ts`, which reads its surfaces from globals.css.
    for (const m of [updateRest, cancelRest]) {
      expect(m.bg, `${m.what} is painted on ${m.bg}, not the ghost button's own resting white`).toBe(
        GHOST_REST,
      );
    }
    for (const m of [updateHover, cancelHover]) {
      expect(
        m.bg,
        `${m.what} is painted on ${m.bg}, not the ghost button's own #f3f4f6 :hover paint — the ` +
          `hovered pixel is where the pre-fix cancel button measured ${PRE_FIX_CANCEL_HOVER_RATIO}:1`,
      ).toBe(GHOST_HOVER);
    }
    for (const m of [cancelDown, updateDown]) {
      expect(
        m.bg,
        `${m.what} is painted on ${m.bg}, not the ghost button's own ${GHOST_PRESSED} :active paint`,
      ).toBe(GHOST_PRESSED);
    }

    // The INK is pinned too, so a green run cannot come from a token that moved
    // somewhere else: these are the two steps the census shipped.
    expect(updateRest.fg, "the update-card label must be text-sky-700").toBe(SKY_700);
    expect(cancelRest.fg, "the cancel label must be text-rose-700").toBe(ROSE_700);

    // The named node must be BETTER than the 4.022:1 the card reports for it, in
    // every state — not merely over the floor.
    for (const m of [updateRest, updateHover, updateDown]) {
      expect(
        m.ratio,
        `${m.what} measured ${m.ratio.toFixed(3)}:1, which is NOT above the pre-fix ` +
          `${PRE_FIX_UPDATE_RATIO}:1 — re-measure before believing a green run`,
      ).toBeGreaterThan(PRE_FIX_UPDATE_RATIO);
    }
    // …and so must the sibling: its defect lived in the HOVER state, which is the
    // one a source guard attributing a (since deleted, dead) `hover:bg-rose-50`
    // would have measured wrongly (4.121:1 rather than the painted 4.114:1).
    for (const m of [cancelRest, cancelHover, cancelDown]) {
      expect(
        m.ratio,
        `${m.what} measured ${m.ratio.toFixed(3)}:1, which is NOT above the pre-fix ` +
          `${PRE_FIX_CANCEL_HOVER_RATIO}:1 — re-measure before believing a green run`,
      ).toBeGreaterThan(PRE_FIX_CANCEL_HOVER_RATIO);
    }

    // Exact comparison, never a rounded one: this repo has shipped a defect
    // 0.008 under the floor (card t_e0784043), so a value in [4.4995, 4.5) must
    // fail here and cannot be rounded up into a pass.
    for (const m of measured) {
      expect(
        m.ratio,
        `${m.what} (${JSON.stringify(m.text)}) measures ${m.ratio.toFixed(3)}:1 ` +
          `(fg=${m.fg} on bg=${m.bg}, element bg ${m.ownBg}, opacity ${m.opacity}, ${m.fontSize}) — ` +
          `under the ${AA_FLOOR}:1 AA floor. `.concat(
          m.bg === GHOST_REST
            ? `The ghost button paints its own three surfaces; ` +
              `src/lib/ghostButtonInk.test.ts reads them from globals.css and names the step that clears all three.`
            : `This is the ghost button's own hover/pressed surface — a \`hover:bg-*\` ` +
              `utility on the element cannot apply to it (unlayered rule wins; card t_ac414593 ` +
              `deleted the last nine, and src/lib/ghostButtonInk.test.ts now forbids new ones), ` +
              `so judge the pixel, not the class list.`,
        ),
      ).toBeGreaterThanOrEqual(AA_FLOOR);
    }
  });
});
