// Regression guard: the /chat CONFIRMATION CARD must clear WCAG AA on the sky
// tint it is painted on — and the axe gate must be able to REACH it.
//
// THE DEFECT (card t_f5f269a8, filed by the QA lane on v1.12.0 / 43133c1)
//   The proposal card is new markup in this release (`api/internal/chat/
//   proposal.go` does not exist before 43133c1; the card it renders did not
//   either). Measured on the shipped revision with a signed-in page and the card
//   on screen, viewport 1280x900, cleared storage, `lang=en`, `wcag2aa`:
//
//     ARM 1  resting /chat, no card           -> critical+serious = 0
//     ARM 2  the SAME page WITH the card open -> critical+serious = 1 (5 nodes)
//
//   axe, verbatim:
//
//     .bg-sky-600                       4.02  (#ffffff on #0084d1, 10px)
//     .gap-2:nth-child(1) > dt          4.11  (#497f9b on #f0f9ff, 12px)
//     .gap-2:nth-child(2) > dt          4.11
//     .font-mono (the tool name)        3.24  (#6190a9 on #f0f9ff, 10px)
//     .flex-wrap.mt-2.gap-2 > .opacity-70 4.11
//
//   The tool name is the worst at 3.24, and it is not decoration: `update_product`
//   is part of what the user is asked to judge before they press Confirm.
//
// THE ROOT CAUSE IS THE ALPHA, NOT THE COLOUR
//   The card paints its text with `opacity-*`, and an opacity composites the
//   token over the tint rather than drawing it at full strength. The card's own
//   base token is fine — #014a70 (`text-sky-900` here, measured from the axe
//   report: it is the only base that reproduces ALL THREE reported values at
//   their own alphas) is 8.9:1 on #f0f9ff at full strength. What fails is 0.7 and
//   0.6 of it: 4.11 and 3.24. The card's own summary and note lines already use
//   `opacity-80`, which is 5.32:1 on the same tint — so the fix is to move the
//   three lighter sites onto the step the card already uses, not to re-token or
//   to delete the hierarchy.
//
// WHY A BROWSER MEASUREMENT AND NOT A CLASS-NAME ASSERTION
//   `web/e2e/a11y.spec.ts` scans PUBLIC routes only, so the axe gate has never
//   seen this card (that is the general gap tracked as t_d334fb02, and this card
//   is a concrete instance of it). A source grep would also be the wrong tool
//   here: the question is a RATIO, and the ratio depends on the alpha compositing
//   over the card's tint — which is exactly the arithmetic a grep cannot do.
//   This file therefore does both halves:
//
//     1. an axe scan scoped to the card subtree, with the repo's own rule
//        (critical/serious fail), which is the assertion that fails today; and
//     2. a canvas-painted measurement of each named node, so the numbers are in
//        the run log and a near-miss (the repo has shipped a 0.008 miss before,
//        card t_e0784043) cannot round into a pass.
//
//   The colours and the COMPOSITED background are read from `getComputedStyle`
//   and round-tripped through a 1x1 canvas, so the renderer does the colour-space
//   conversion (Tailwind v4 emits oklch/lab, not rgb) and the alpha compositing
//   for both the element's own `opacity` and every ancestor's background.
//
// HOW THE CARD IS REACHED WITHOUT A SESSION OR A BACKEND. The card renders from
//   the browser's own `dashboard-bundle` + chat fetches, so stubbing them at the
//   browser level (the pattern `e2e/chat-scope-400.spec.ts` and
//   `e2e/a11y-dialogs.spec.ts` use) puts the real component in a real browser
//   with no Go server, no session cookie and no credential.
//
// Run with:
//
//   npm run a11y                                      # the whole family (this file included)
//   npx playwright test e2e/a11y-chat-card.spec.ts    # this file alone
import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import type { AxeResults, Result } from "axe-core";
import { DICT } from "../src/components/i18nDict";
import { sendChatPrompt } from "./chatComposer";

// The gate's own rule, kept identical to e2e/a11y.spec.ts so the two cannot
// drift: critical/serious fail, everything else is reported for triage.
const IMPACTS = ["critical", "serious"] as const;
const IMPACT_LABEL = IMPACTS as readonly string[];

function blockersOf(results: AxeResults): Result[] {
  return results.violations.filter((v) => IMPACT_LABEL.includes(v.impact ?? ""));
}

/** WCAG 2.x AA floor, and the pre-fix value the tool name measured at. */
const AA_FLOOR = 4.5;
const PRE_FIX_TOOL_RATIO = 3.24;

const EN = {
  title: DICT["chat.title"].en,
  send: DICT["chat.send"].en,
  badge: DICT["chat.proposalBadge"].en,
  confirmHint: DICT["chat.confirmHint"].en,
  confirm: DICT["chat.confirm"].en,
};

const TOKEN = "tok-card-1";
const WS = "ws-b";

// The proposal QA measured: a rename, the tool name that carried information the
// user must judge, and TWO field rows (the two `dt` nodes in the defect).
const PROPOSAL = {
  token: TOKEN,
  args_hash: "hash-card-1",
  tool: "update_product",
  title: "Rename the product",
  summary: "Tee 2026 → Tee 2026 (XL)",
  fields: [
    { label: "Name", value: "Tee 2026 (XL)" },
    { label: "Price", value: "RM25.00" },
  ],
  expires_at: "2027-01-01T00:00:00Z",
  reversible: true,
};

const BUNDLE = {
  me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", is_super_admin: false },
  workspaces: [{ id: "ws-id-b", name: "Second", slug: WS, role: "admin" }],
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

/** Every call the page makes, with the proposal on the reply. */
async function stubAPI(page: Page, decisionStatus = 200) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST, specific routes after: Playwright resolves matching routes in
  // reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(BUNDLE.me)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/rest\/chat(\?.*)?$/, (r) =>
    r.request().method() === "GET"
      ? r.fulfill(json({ available: true, limits: { messages_per_hour: 60, max_message_chars: 2000 } }))
      : r.fulfill(json({ reply: "Here is the change I would make.", proposals: [PROPOSAL] })),
  );
  // The decision route, so the card can be settled into its OTHER badge states.
  await page.route(/(.*)\/api\/rest\/chat\/(confirm|cancel)(\?.*)?$/, (r) =>
    r.fulfill({
      status: decisionStatus,
      contentType: "application/json",
      body: JSON.stringify({ error: "ralat dalaman" }),
    }),
  );
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
      localStorage.setItem("jf_workspace", "ws-b");
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
}

/** The card, by the class its own markup carries. */
const CARD = ".border-sky-200";

/** Get the proposal card on screen and return its locator. */
async function openCard(page: Page, baseURL: string | undefined) {
  // The page-then-composer prologue is SHARED with the other /chat specs
  // (e2e/chatComposer.ts, card t_b9eea741): reaching the page and typing into it
  // before React has hydrated is what redded THIS test at 1 of 4 runs, because
  // the server-rendered composer is already `enabled` and the `fill` that lands
  // on it is discarded by the hydration that follows. `sendChatPrompt` waits for
  // the availability probe's response — the one unambiguous "the page is live"
  // signal — and retries the type until Send follows.
  await sendChatPrompt(page, baseURL, "rename my product");

  const card = page.locator(CARD).filter({ hasText: PROPOSAL.title });
  // A guard whose subject never rendered passes vacuously, so the card is
  // asserted to exist before anything is measured or scanned.
  await expect(card, "the proposal card must render — otherwise this file proves nothing").toHaveCount(
    1,
    { timeout: 20_000 },
  );
  // And the two things the defect's node set is made of must be on screen.
  await expect(card.locator("dt"), "the card's field rows must render").toHaveCount(2);
  await expect(card.getByText(PROPOSAL.tool), "the card's tool name must render").toBeVisible();
  return card;
}

type Measure = {
  what: string;
  text: string;
  fg: string;
  bg: string;
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
 * `opacity` — which is the whole defect here, since `opacity-70` is what turns a
 * passing 8.9:1 token into a failing 4.11:1.
 */
async function measure(page: Page, selector: string, index: number, what: string): Promise<Measure> {
  return page.evaluate(
    ({ selector, index, what }) => {
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

      const el = document.querySelectorAll(selector)[index];
      if (!el) throw new Error(`no element at ${selector}[${index}] — the card's markup moved`);

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
      bg = { ...bg };
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
        opacity: cs.opacity,
        fontSize: cs.fontSize,
        ratio: ratio(composited, bg),
      };
    },
    { selector, index, what },
  );
}

test.describe("the /chat confirmation card clears AA on its own tint", () => {
  test("axe finds no critical/serious violation inside the card", async ({ page, baseURL }) => {
    await stubAPI(page);
    await freshState(page);
    const card = await openCard(page, baseURL);

    // Scoped to the card subtree, so the scan is about the markup this card
    // exists for and cannot be satisfied by an unrelated clean region. axe
    // THROWS when the included context is not there, so this cannot pass
    // vacuously — but the card's presence is asserted first anyway, so the
    // failure says which of the two happened.
    await expect(card).toBeVisible();
    const results = await new AxeBuilder({ page }).include(CARD).analyze();
    const blockers = blockersOf(results);

    // The numbers are the acceptance evidence for this card, not just pass/fail.
    console.log(
      `chat card axe: ${blockers.length} critical/serious violation(s)\n` +
        blockers
          .map(
            (v) =>
              `  - ${v.id} (${v.impact}): ${v.help}\n` +
              v.nodes
                .map(
                  (n) =>
                    `      ${n.target.join(" ")} — ${n.failureSummary?.split("\n")[0] ?? ""}`,
                )
                .join("\n"),
          )
          .join("\n"),
    );

    // The rule that RAN is asserted too: a scan that analysed nothing must not
    // read as a clean card. axe always emits `color-contrast` in `passes`,
    // `incomplete` or `violations` when it ran over text.
    const ranContrast = [...results.passes, ...results.incomplete, ...results.violations].some(
      (v) => v.id === "color-contrast",
    );
    expect(
      ranContrast,
      "the color-contrast rule did not run over the card at all — a scan that analysed nothing " +
        "cannot be read as a clean card",
    ).toBe(true);

    expect(blockers, "the card must have no critical/serious axe violations").toEqual([]);
  });

  test("every text node on the card MEASURES above the AA floor", async ({ page, baseURL }) => {
    test.setTimeout(120_000);
    await stubAPI(page);
    await freshState(page);
    const card = await openCard(page, baseURL);

    const nodes: Array<{ selector: string; index: number; what: string }> = [
      // The three nodes axe named with a file:line on the QA card, plus the
      // field row that repeats once per rendered field.
      { selector: `${CARD} span.rounded-full`, index: 0, what: "badge" },
      { selector: `${CARD} dt`, index: 0, what: "field label 1" },
      { selector: `${CARD} dt`, index: 1, what: "field label 2" },
      { selector: `${CARD} p.font-mono`, index: 0, what: "tool name" },
      { selector: `${CARD} span.opacity-80, ${CARD} span.opacity-70`, index: 0, what: "confirm hint" },
    ];

    const measured: Measure[] = [];
    for (const n of nodes) measured.push(await measure(page, n.selector, n.index, n.what));

    const report = measured
      .map(
        (m) =>
          `${m.what} (${JSON.stringify(m.text)}): fg=${m.fg} on bg=${m.bg} ` +
          `opacity=${m.opacity} at ${m.fontSize} — ${m.ratio.toFixed(3)}:1`,
      )
      .join("\n  ");
    console.log(`chat card contrast measurements:\n  ${report}`);

    // The hint is located by the class the card's own markup uses; if that moved,
    // measure() throws rather than measuring something else.
    const badge = measured.find((m) => m.what === "badge")!;
    const tool = measured.find((m) => m.what === "tool name")!;
    // The tint is pinned, not just the ratio: this guard is about the card's sky
    // surface, and if the surface legitimately moves the ratios below are what
    // must still hold — re-anchor this expectation rather than deleting the file.
    // The badge is exempt from the tint expectation because its own label is
    // painted on its own opaque pill — that is the node the card names first, and
    // its label/background pair is asserted separately below.
    for (const m of measured) {
      if (m.what === "badge") continue;
      expect(m.bg, `${m.what} is painted on ${m.bg}, not the card's sky-50 tint`).toBe("#f0f9ff");
    }
    // The badge's own opaque background IS the surface behind its label, and the
    // label must be white on it.
    expect(badge.fg, "the badge's label must be white").toBe("#ffffff");
    expect(
      badge.bg,
      `the badge's pill is painted on ${badge.bg}; the on-screen pill must be an opaque ` +
        `background, not the card tint`,
    ).not.toBe("#f0f9ff");
    // The tool name is the node the card calls out as the worst (3.24:1).
    expect(
      tool.ratio,
      `the tool name measured ${tool.ratio.toFixed(3)}:1, which is NOT above the pre-fix ` +
        `${PRE_FIX_TOOL_RATIO}:1 — re-measure before believing a green run`,
    ).toBeGreaterThan(PRE_FIX_TOOL_RATIO);

    for (const m of measured) {
      // Exact comparison, never a rounded one: this repo has shipped a defect
      // 0.008 under the floor (card t_e0784043), so a value in [4.4995, 4.5) must
      // fail here and cannot be rounded up into a pass.
      expect(
        m.ratio,
        `${m.what} (${JSON.stringify(m.text)}) measures ${m.ratio.toFixed(3)}:1 ` +
          `(fg=${m.fg} on bg=${m.bg}, opacity=${m.opacity}, ${m.fontSize}) — under the ${AA_FLOOR}:1 ` +
          `AA floor. The card's own summary line already shows the passing step for this tint: ` +
          `raise the opacity to 0.80 (5.320:1) rather than deleting the hierarchy, or move the ` +
          `node onto an on-tint token.`,
      ).toBeGreaterThanOrEqual(AA_FLOOR);
    }
  });

  test("the OTHER badge states clear AA too, not just the open one", async ({ page, baseURL }) => {
    // The open pill is the state that was on screen in the axe run, but the card
    // has four more pill classes and a fix that moved only `bg-sky-600` would
    // leave a real node behind: the "did not go through" pill was `bg-red-500`,
    // and white on #ef4444 measures 3.76:1 — worse than the defect this card
    // reported, and axe would never have shown it because the card only reaches
    // that state after a refused confirmation.
    await stubAPI(page, 500);
    await freshState(page);
    const card = await openCard(page, baseURL);

    await card.getByRole("button", { name: EN.confirm }).click();
    // The pill must settle before it is measured, or this reads a stale class.
    await expect(card, "the refused confirmation must settle the card's badge").toContainText(
      DICT["chat.proposalBadgeFailed"].en,
      { timeout: 20_000 },
    );

    const pill = await measure(page, `${CARD} span.rounded-full`, 0, "badge (failed)");
    console.log(
      `chat card FAILED-pill contrast: fg=${pill.fg} on bg=${pill.bg} — ${pill.ratio.toFixed(3)}:1`,
    );
    expect(pill.text, "the pill measured must be the failure badge").toBe(
      DICT["chat.proposalBadgeFailed"].en,
    );
    expect(pill.fg, "the badge's label must be white").toBe("#ffffff");
    // The state is pinned as well as the ratio: a badge that silently fell back
    // to the settled slate pill would pass the ratio check for the wrong reason.
    expect(
      pill.bg,
      `${DICT["chat.proposalBadgeFailed"].en} is painted on ${pill.bg} — the refusal state must be ` +
        `red, not the settled slate`,
    ).not.toBe("#64748b");
    expect(
      pill.ratio,
      `the failure badge measures ${pill.ratio.toFixed(3)}:1 (fg=${pill.fg} on bg=${pill.bg}) — ` +
        `under the ${AA_FLOOR}:1 AA floor. white on bg-red-500 (#ef4444) is 3.76:1; bg-red-600 ` +
        `(#dc2626) is 4.83:1 and bg-red-700 (#b91c1c) is 6.47:1.`,
    ).toBeGreaterThanOrEqual(AA_FLOOR);
  });
});
