// /builder is the ONLY route of 62 whose rendered document carried no `<h1>`,
// and it was the densest sub-12px page in the app (card t_a5334287).
//
// MEASURED BY THE UX SWEEP (rendered document, every route):
//   * `/builder` h1Count = 0 — every other route rendered exactly 1. A screen
//     reader user got no page title on the page where the merchant works, and
//     the settings aside's own labels were 10-11px uppercase text, so the whole
//     panel read as one undifferentiated block.
//   * 36 leaf elements at 11px on this page alone: the palette's label spans,
//     the block-card type row, the condition-broken badge and the panel hints.
//
// THE TWO INVARIANTS THIS FILE MEASURES ON THE LIVE DOM:
//
//   1. EXACTLY ONE `<h1>`, and it NAMES the form. The heading wraps the
//      editable title input, which is how the bar looks without change, so the
//      name is asserted through the accessibility tree (`heading` level 1 with
//      an accessible name) rather than by counting tags — a heading whose only
//      content is a textbox counts as one `<h1>` and announces NOTHING. That
//      is the plausible wrong fix, and the "empty heading" control below
//      requires the same assertion to reject it.
//
//   2. NO TEXT under the builder's asides renders below 12px — computed, on
//      the page as served, walking the real text nodes. The palette labels,
//      the block-settings labels and the hints were all 9-11px tokens
//      (`text-jf-4xs` / `-3xs` / `-2xs`); the floor is `text-xs`, which is the
//      step the rest of the app already uses.
//
// WHY BOTH THIS AND `src/lib/builderHeadingType.test.ts`. That unit test reads
// the JSX, so it fails in seconds and names the utility to change — but it
// cannot see a computed value (an inherited class, a `.jf-*` rule, an embedded
// component). This file measures the computed values of the served page. A
// source guard over a page that renders nothing passes; a browser guard cannot
// tell you which class to fix.
//
// WHAT MAKES IT BITE: every assertion is REACHABLE first (one `<h1>` that has a
// name; 2 asides; the exact 67 / 14 leaf text nodes measured on the palette and
// the settings panel; min size known) and then the SAME scanner is handed the
// pre-fix shape — an 11px span planted in the aside, a second `<h1>`, an `<h1>`
// whose text node is removed — and must report it. A clean reading with no
// control proves nothing.
//
// The API is stubbed at the browser level (no Go server, no session, no DB) and
// the browser state is FRESH (storage cleared, `jf_lang` pinned to `en`) so a
// `jf_lang` left behind by an earlier card cannot render this in a locale these
// strings do not describe.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

// Dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const FORM_ID = "form_builder_heading_type";
const FORM_TITLE = "Guard form";
const NEW_FORM_TITLE = "New form"; // dictionary key `forms.new`, en
const FLOOR_PX = 12;

// REACHABILITY, MEASURED — how much text each panel really renders, so a scan
// that matched an empty or restructured panel cannot pass as evidence.
//
// Measured on the rebased tree (`origin/main` 08267e6a4 + this card's delta) at
// 1280x720 desktop Chrome, first block card selected: the palette carries 67
// leaf text nodes and the settings aside 14. The settings aside held 22 before
// #1678 moved the form-mode card and the "Form settings →" card into the
// builder header — the panel is field settings only now, which is why 14 is the
// current number and not a regression.
//
// These are EXACT on purpose. A floor ("more than 0", or even "more than 10")
// cannot tell a rendered panel from one that was restructured under it, and
// "this panel was actually read" is the whole claim the reading makes. A change
// to either panel's own markup must re-measure these two numbers.
const PALETTE_TEXT_NODES = 67;
const SETTINGS_TEXT_NODES = 14;
// The whole page (header + canvas + both asides) measured 115 leaf text nodes.
// A floor rather than an exact count: the header and the canvas carry chrome
// other cards edit, and this assertion is here to prove the page rendered, not
// to pin that chrome.
const WHOLE_PAGE_MIN_NODES = 100;

const BLOCK = (type: string, extra: Record<string, unknown> = {}) => ({
  type,
  value: type,
  ...extra,
});

const json = (body: unknown) => ({ status: 200, contentType: "application/json", body: JSON.stringify(body) });

// stubAPI answers every /api/* call the builder makes, so the page reaches its
// loaded state without a Go API, a session cookie or a database.
async function stubAPI(page: Page) {
  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) =>
    r.fulfill(
      json({
        me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "en", is_super_admin: false },
        workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
        chip_status: null, keys: [], domains: [], social: [], einvoice: null,
        webhooks: [], products: [], summary: { counts: {}, revenue_sen: 0 },
        sales: { items: [], page: 1, limit: 20, total: 0 },
        form_analytics: [], product_analytics: [],
      }),
    ),
  );
  await page.route(/(.*)\/api\/rest\/currency\/rates(\?.*)?$/, (r) => r.fulfill(json({ rates: [], cached: false })));
  await page.route(/(.*)\/api\/rest\/products(\?.*)?$/, (r) => r.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+(\?.*)?$/, (r) =>
    r.fulfill(
      json({
        id: FORM_ID,
        slug: "guard-form",
        title: FORM_TITLE,
        status: "draft",
        url: "",
        config: {
          mode: "payment",
          blocks: [
            BLOCK("text"),
            BLOCK("select", {
              choices: [
                { label: "One", value: "One" },
                { label: "Two", value: "Two" },
              ],
            }),
            BLOCK("name"),
            BLOCK("date"),
            BLOCK("number"),
          ],
        },
        theme: {},
      }),
    ),
  );
}

/** FRESH state: storage cleared, then the language pinned. */
async function freshState(page: Page) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_lang", "en");
      localStorage.setItem("jf_workspace", "demo");
    } catch {
      /* storage unavailable */
    }
  });
}

/** Opens /builder for the stubbed form and waits for the blocks to render. */
async function openBuilder(page: Page) {
  await stubAPI(page);
  await freshState(page);
  await page.goto(`/builder?form=${FORM_ID}`, { waitUntil: "domcontentloaded" });
  // REACHABILITY: a page that bounced to /landing or is still an empty shell
  // renders no block cards, and every assertion below would pass vacuously.
  await expect(
    page.locator('[data-jf-page="builder"] section div[draggable]').first(),
    "the builder must render its block cards — nothing below is evidence otherwise",
  ).toBeVisible({ timeout: 30_000 });
  await expect(page.locator('[data-jf-page="builder"] header input').first()).toHaveValue(FORM_TITLE);
}

/** The invariant, as a function, so the control test can hand it the defect. */
function assertOneNamedH1(headings: { count: number; text: string }): void {
  if (headings.count !== 1) {
    throw new Error(`expected exactly ONE <h1> on /builder, found ${headings.count}`);
  }
  if (!headings.text.trim()) {
    throw new Error("<h1> has no text of its own — a heading around an input names NOTHING");
  }
}

/**
 * Reads the document's level-1 headings: how many, and the TEXT they carry.
 *
 * The text is what a heading names itself from, and it is the cheap reading the
 * mutation controls below need (`toHaveAccessibleName` is the authoritative
 * check and is used in the clean test). A `<h1>` wrapping the title `<input>`
 * has an EMPTY textContent — the input contributes nothing — which is the
 * difference this pair of assertions exists to make.
 */
async function readHeadings(page: Page): Promise<{ count: number; text: string }> {
  const headings = page.getByRole("heading", { level: 1 });
  const count = await headings.count();
  const text = count === 1 ? ((await headings.first().textContent()) || "").trim() : "";
  return { count, text };
}

type LeafReading = {
  count: number;
  min: number;
  offenders: { text: string; tag: string; className: string; px: number }[];
};

/**
 * Computed font-size of every rendered TEXT NODE under `selector`.
 *
 * A text node's size is the size its parent element computes, so this walks the
 * text, not the elements: a `<div>` that also contains children cannot hide a
 * small text node behind them. Controls whose text is their VALUE (`option`)
 * are skipped — they are governed by the control's own layout, not by a label
 * or body step. Elements with no box (display:none / the inactive mobile face)
 * are skipped, because they do not paint for anyone.
 */
async function readLeafText(page: Page, selector: string): Promise<LeafReading> {
  return page.evaluate(
    ({ selector, floor }) => {
      const root = document.querySelector(selector);
      if (!root) return { count: 0, min: -1, offenders: [] as LeafReading["offenders"] };
      const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
      const seen = new Set<Element>();
      const offenders: LeafReading["offenders"] = [];
      let count = 0;
      let min = Infinity;
      for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        if (!(node.textContent || "").trim()) continue;
        const el = node.parentElement;
        if (!el || seen.has(el)) continue;
        seen.add(el);
        if (["option", "optgroup", "script", "style"].includes(el.tagName.toLowerCase())) continue;
        if (el.getClientRects().length === 0) continue;
        const px = Number.parseFloat(getComputedStyle(el).fontSize);
        if (!Number.isFinite(px)) continue;
        count += 1;
        min = Math.min(min, px);
        if (px < floor) {
          offenders.push({
            text: (node.textContent || "").trim().slice(0, 40),
            tag: el.tagName.toLowerCase(),
            className: typeof el.className === "string" ? el.className.slice(0, 140) : "",
            px,
          });
        }
      }
      return { count, min: count ? min : -1, offenders };
    },
    { selector, floor: FLOOR_PX },
  );
}

test.describe("builder: one named h1", () => {
  test("the form title is the page's only h1, and it has a name", async ({ page }) => {
    await openBuilder(page);

    // ONE heading, level 1, carrying the form's title as its accessible name.
    // `getByRole` is the accessibility tree a screen reader reads: a heading
    // that exists only as a `<div>` or only as the title input is not here.
    const h1 = page.getByRole("heading", { level: 1 });
    await expect(h1, "the builder must render exactly one <h1>").toHaveCount(1);
    await expect(h1).toHaveAccessibleName(FORM_TITLE);
    await expect(h1).toHaveJSProperty("tagName", "H1");
    // It is visually hidden on purpose (the visible control is the editable
    // title) — but hidden with `sr-only`, i.e. PRESENT for assistive tech, not
    // `display:none`, which would remove it from the accessibility tree again.
    expect(
      await h1.evaluate((el) => getComputedStyle(el).display),
      "the h1 must not be display:none — that is an h1 no screen reader sees",
    ).not.toBe("none");
    expect(await h1.evaluate((el) => el.closest("[aria-hidden='true']") === null)).toBe(true);

    // …and the page keeps its title control: the merchant can still rename the
    // form in the top bar, which is where the heading's text comes from.
    await expect(page.locator('[data-jf-page="builder"] header input[aria-label="Form title"]')).toHaveCount(1);

    // The invariant the control test hands the defect does NOT throw here.
    const headings = await readHeadings(page);
    expect(() => assertOneNamedH1(headings)).not.toThrow();
  });

  test("an unsaved form is titled 'New form' — not an empty heading", async ({ page }) => {
    await stubAPI(page);
    await freshState(page);
    await page.goto("/builder", { waitUntil: "domcontentloaded" });
    await expect(page.locator('[data-jf-page="builder"] section').first()).toBeVisible({ timeout: 30_000 });
    const h1 = page.getByRole("heading", { level: 1 });
    await expect(h1, "the new-form path must also carry exactly one h1").toHaveCount(1);
    await expect(h1).toHaveAccessibleName(NEW_FORM_TITLE);
  });

  test("the one-h1 invariant BITES: a second h1, and a nameless h1", async ({ page }) => {
    await openBuilder(page);

    // (a) A second heading — how "give the aside a title too" would go wrong.
    // The aside's own "Block settings" label was the tempting candidate.
    await page.evaluate(() => {
      const extra = document.createElement("h1");
      extra.textContent = "Block settings";
      document.querySelector('[data-jf-page="builder"] aside')!.append(extra);
    });
    const two = await readHeadings(page);
    expect(two.count, "the mutation must actually add a second h1").toBe(2);
    expect(
      () => assertOneNamedH1(two),
      "the one-h1 invariant did not bite on two headings — the clean reading above is not evidence",
    ).toThrow();
    await page.evaluate(() => {
      document.querySelectorAll('[data-jf-page="builder"] aside h1').forEach((el) => el.remove());
    });

    // (b) The NAMELESS heading: one <h1> with no text of its own. This is the
    // shape a "wrap the input in the h1" fix produces, and it is why the count
    // alone is not the assertion — one h1, announcing nothing.
    const emptied = await page.evaluate(() => {
      const h1 = document.querySelector('[data-jf-page="builder"] h1');
      if (!h1 || !(h1.textContent || "").trim()) return false;
      h1.textContent = "";
      return true;
    });
    expect(emptied, "the mutation reached no h1 text — the proof would be vacuous").toBe(true);
    const nameless = await readHeadings(page);
    expect(nameless.count, "the mutation must leave exactly one h1").toBe(1);
    expect(nameless.text, "the h1's text should be gone after the mutation").toBe("");
    // The authoritative reading: the SAME heading no longer reports the form
    // title as its accessible name.
    await expect(page.getByRole("heading", { level: 1 })).not.toHaveAccessibleName(FORM_TITLE);
    expect(
      () => assertOneNamedH1(nameless),
      "the name invariant did not bite on a nameless h1 — the clean reading above is not evidence",
    ).toThrow();
  });
});

test.describe("builder: no text under 12px", () => {
  test("the palette and the settings aside render no text below 12px", async ({ page }) => {
    await openBuilder(page);

    // BOTH asides, and the settings panel only has its labels once a block is
    // open: one click on a block card is the app's own flow (no DOM forcing).
    await page.locator('[data-jf-page="builder"] section div[draggable]').first().click();
    await expect(
      page.locator('[data-jf-aside="settings"] label, [data-jf-aside="settings"] input').first(),
      "the settings panel must render its controls — a scan of an empty panel proves nothing",
    ).toBeVisible();

    const asides = page.locator('[data-jf-page="builder"] [data-jf-aside]');
    await expect(asides, "the builder must render its palette AND its settings aside").toHaveCount(2);

    const expectedNodes: Record<string, number> = {
      palette: PALETTE_TEXT_NODES,
      settings: SETTINGS_TEXT_NODES,
    };
    for (const name of ["palette", "settings"]) {
      const reading = await readLeafText(page, `[data-jf-aside="${name}"]`);
      // REACHABILITY before judgement: the EXACT number of text nodes measured
      // on this tree. Anything else means the selector matched a collapsed or
      // restructured panel, and the clean verdict below would be about no
      // panel at all — re-measure the constant rather than lowering it.
      expect(
        reading.count,
        `${name}: expected the ${expectedNodes[name]} text nodes measured on this tree, read ` +
          `${reading.count} — the panel's markup changed, so re-measure the constant. ${JSON.stringify(reading)}`,
      ).toBe(expectedNodes[name]);
      expect(reading.min, `${name}: no finite font-size measured`).toBeGreaterThan(0);
      expect(
        reading.offenders,
        `${name}: ${reading.offenders.length} text node(s) render below ${FLOOR_PX}px — ` +
          "use text-xs (12px) for labels and text-sm (14px) for body, and fit density with spacing",
      ).toEqual([]);
    }

    // …and the same measurement over the WHOLE builder page (header + canvas +
    // both asides) so a 9-11px label outside an aside cannot reappear.
    const whole = await readLeafText(page, '[data-jf-page="builder"]');
    expect(
      whole.count,
      `the builder page rendered ${whole.count} text nodes — that is too little to judge (measured 115 on this tree)`,
    ).toBeGreaterThanOrEqual(WHOLE_PAGE_MIN_NODES);
    expect(whole.min).toBeGreaterThanOrEqual(FLOOR_PX);
    expect(whole.offenders, `below ${FLOOR_PX}px somewhere on /builder: ${JSON.stringify(whole.offenders)}`).toEqual([]);
  });

  test("the size scanner BITES: a palette label forced back to 11px is reported", async ({ page }) => {
    await openBuilder(page);

    // Force the defect back onto the REAL node the sweep measured: the palette's
    // label span (11px `text-jf-2xs` when this card started). Measuring a
    // planted element proves the scanner can see a small node; forcing the
    // shipping one proves it reaches THIS node.
    const mutated = await page.evaluate(() => {
      const spans = [...document.querySelectorAll('[data-jf-aside="palette"] span')].filter(
        (el) => (el.textContent || "").trim().length > 2 && el.querySelector("*") === null,
      );
      const target = spans.find((el) => el.className.includes("text-xs")) || spans[0];
      if (!target) return null;
      target.setAttribute("style", "font-size: 11px");
      return (target.textContent || "").trim();
    });
    expect(mutated, "the palette rendered no label span — the control would be vacuous").not.toBeNull();

    const reading = await readLeafText(page, '[data-jf-aside="palette"]');
    const found = reading.offenders.filter((o) => o.text === mutated);
    expect(
      found,
      `the size scanner did not report the 11px palette label "${mutated}" — it cannot be trusted to ` +
        `see a regression. Reading: ${JSON.stringify(reading)}`,
    ).toHaveLength(1);
    expect(found[0].px).toBe(11);
  });
});
