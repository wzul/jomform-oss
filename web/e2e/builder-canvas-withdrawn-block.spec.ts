// Regression guard: a STORED block whose palette entry was withdrawn must
// render honestly on the builder canvas (card t_3abab22c).
//
// THE DEFECT (measured on the production canvas, then fixed on this branch)
//   The form `builder-demo` still carries a block of a type the palette no
//   longer offers. Two canvas surfaces trusted the stored type:
//     1. the card's badge — `typeLabel` builds `t("blk.<type>")`, and `t()`
//        returns the KEY when the dictionary has no entry left, so the badge
//        painted `BLK.COUPON` (the card's own `uppercase` styling) above the
//        merchant's label `Kupon`;
//     2. the card's preview — `BlockPreview` has no arm for that type, so it
//        fell through to `default`, which draws `{value}` over an empty,
//        bordered 32px box. That reads as an input: the same control-shaped
//        sketch the public renderers deliberately replaced with the merchant's
//        label plus a note, because nothing on the server applies a discount.
//   Read out of the live DOM on prod: `document.querySelectorAll("span")` gave
//   `["BLK.COUPON", "blk.coupon"]`, and the card carried the empty box.
//
// WHY THIS IS A BROWSER MEASUREMENT
//   Both symptoms are RENDERED facts: one is the dictionary fallback firing on
//   a key nothing else names, the other is which branch of a `switch` paints
//   what. A source scan can see neither — it cannot tell that the badge reads a
//   word rather than a key, and it cannot see an element in the card. So this
//   spec drives the real client component and reads the DOM, and the box is
//   measured by COMPUTED STYLE (a short element with a painted border), not by
//   its class list: a class rename must not be able to hide the shape.
//
// The API is stubbed at the browser level (no Go server, no session, no DB), so
// this runs in the `e2e` gate (`npm run e2e`, part of scripts/preflight.sh)
// against the build that gate already serves. The unit half of the guard is
// `src/lib/withdrawnBlock.test.ts` (the helpers and the wiring).
import { test, expect, type Page } from "@playwright/test";

import { DICT } from "../src/components/i18nDict";
import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means — see
// e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const FORM_ID = "form_stored_withdrawn_block";

// The merchant's own stored label for the withdrawn block, and the type string
// the saved config carries. This spec names them because it FIXTURES a stored
// form — it does not re-offer the block (the palette is asserted below).
const STORED_LABEL = "Kupon";
const STORED_TYPE = "coupon";
// The note the canvas must render instead of the control sketch, from the app's
// own dictionary in the DEFAULT locale — so the guard reads the sentence the
// user sees rather than a copy pasted here.
const NOTE = DICT["bld.withdrawnBlock"].en;

// stubAPI answers every /api/* call the builder makes, so `/builder?form=<id>`
// reaches its loaded state without a Go API, a session cookie or a database.
// Playwright resolves matching routes in REVERSE registration order (the last
// registered wins), so the broad catch-alls are registered FIRST and the
// specific routes after them.
async function stubAPI(page: Page) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

  await page.route("**/api/**", (route) => route.fulfill(json({})));
  await page.route("**/api/rest/dashboard-bundle**", (route) =>
    route.fulfill(
      json({
        me: { id: "u1", email: "merchant@example.com", name: "Merchant" },
        workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
      }),
    ),
  );
  await page.route("**/api/auth/profile", (route) => route.fulfill(json({ lang: "en" })));
  await page.route("**/api/rest/products**", (route) => route.fulfill(json([])));
  await page.route("**/api/rest/forms", (route) => route.fulfill(json([])));
  await page.route(`**/api/rest/forms/${FORM_ID}**`, (route) =>
    route.fulfill(
      json({
        id: FORM_ID,
        slug: "demo-form",
        title: "Demo form",
        status: "published",
        url: "https://jomform.com/f/demo-form",
        theme: {},
        config: {
          mode: "payment",
          blocks: [
            // Exactly the shape the production row has: a withdrawn type with
            // the merchant's stored label and no id.
            { type: STORED_TYPE, value: STORED_LABEL },
            // A normal short-answer field beside it, so the guard can prove the
            // change is scoped: a real field keeps its control sketch.
            { type: "text", value: "Notes" },
          ],
        },
      }),
    ),
  );
}

/** A FRESH browser state, English — what a new merchant session gets. */
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

type CardFacts = {
  /** The card's badge text, raw textContent (no CSS uppercase applied). */
  badge: string;
  /** Form controls inside the card — a stored label must render none. */
  controls: number;
  /**
   * Control-SHAPED boxes: descendants that are 24-40px tall and paint a real
   * border. That is what the `default` preview arm draws for every field, and
   * the shape this card is about. Measured by computed style, so a class rename
   * cannot hide it.
   */
  boxes: number;
  /** True when the card's own text carries the note sentence. */
  hasNote: boolean;
};

type Facts = {
  /** Any raw `blk.<type>` key anywhere in the rendered canvas. */
  rawKeys: string[];
  stored: CardFacts | null;
  normal: CardFacts | null;
  /** Palette items that would ADD the withdrawn block — must be none. */
  offeredInPalette: string[];
};

// readCanvas reads the live DOM: it finds the two block cards by their stored
// labels, then reports the badge text, the controls and the control-shaped
// boxes each one paints.
async function readCanvas(page: Page): Promise<Facts> {
  return page.evaluate(
    ({ storedLabel, storedType, note }) => {
      const cards = [...document.querySelectorAll('section div[draggable="true"]')] as HTMLElement[];
      const card = (label: string) =>
        cards.find((c) => (c.textContent || "").includes(label)) ?? null;

      const facts = (el: HTMLElement | null) => {
        if (!el) return null;
        // The badge is the first span of the card's header row (`typeLabel`).
        const header = el.querySelector(":scope > div");
        const badge = header ? (header.querySelector("span")?.textContent || "").trim() : "";
        const controls = el.querySelectorAll("input, select, textarea").length;
        let boxes = 0;
        for (const child of el.querySelectorAll("*")) {
          const cs = getComputedStyle(child);
          const h = child.getBoundingClientRect().height;
          if (cs.borderTopStyle !== "none" && parseFloat(cs.borderTopWidth) > 0 && h >= 24 && h <= 40) boxes++;
        }
        return {
          badge,
          controls,
          boxes,
          hasNote: (el.textContent || "").includes(note),
        };
      };

      // Any raw key the dictionary failed to translate, anywhere on the page.
      const rawKeys = [...new Set((document.body.textContent || "").match(/blk\.[a-z][a-z-]*/gi) ?? [])];

      // The palette must not offer the withdrawn block again. Palette items are
      // the draggable divs in the left <aside>; their text is the icon plus the
      // translated label, so a re-added item would carry the stored type's name.
      const offeredInPalette = [...document.querySelectorAll('aside div[draggable="true"]')]
        .map((el) => (el.textContent || "").trim())
        .filter((text) => text.toLowerCase().includes(storedType));

      return { rawKeys, stored: facts(card(storedLabel)), normal: facts(card("Notes")), offeredInPalette };
    },
    { storedLabel: STORED_LABEL, storedType: STORED_TYPE, note: NOTE },
  );
}

test("a stored block the palette no longer offers gets a real label and no control sketch", async ({ page }) => {
  await freshState(page);
  await stubAPI(page);
  await page.goto(`/builder?form=${FORM_ID}`, { waitUntil: "networkidle" });

  // The canvas has to HYDRATE the stored config before anything here means
  // anything: wait for the stored label itself, then for the card's badge to be
  // painted. The badge wait is deliberately NOT a wait for the fixed text — a
  // guard that waits for the expected label reports "not visible" instead of
  // naming the defect. It waits for a NON-EMPTY badge, and the assertions below
  // say what it must read.
  await expect(page.getByText(STORED_LABEL, { exact: true })).toBeVisible({ timeout: 45_000 });
  await expect
    .poll(async () => (await readCanvas(page)).stored?.badge ?? "", {
      timeout: 15_000,
      message: "the stored block's card never painted a type badge",
    })
    .not.toBe("");
  expect(await page.evaluate(() => document.documentElement.lang)).toBe("en");

  const facts = await readCanvas(page);
  console.log(
    `[canvas] stored card=${JSON.stringify(facts.stored)} normal card=${JSON.stringify(facts.normal)} ` +
      `rawKeys=${JSON.stringify(facts.rawKeys)} palette=${JSON.stringify(facts.offeredInPalette)}`,
  );

  // ── DEFECT 1: the badge ────────────────────────────────────────────────
  // Nothing on the canvas may read as a dictionary key. This is the symptom
  // exactly as it was measured on prod.
  expect(
    facts.rawKeys,
    `the canvas still renders a raw dictionary key (the badge was "BLK.COUPON" before the fix): ${JSON.stringify(facts.rawKeys)}`,
  ).toEqual([]);
  expect(facts.stored, "the stored block's card is not on the canvas — the fixture or the hydration path moved").not.toBeNull();
  expect(
    facts.stored!.badge,
    "the stored block's badge must be a WORD, not its type key",
  ).toBe("Coupon");

  // ── DEFECT 2: the preview ──────────────────────────────────────────────
  // No control, and no control-SHAPED box: the preview says what buyers get.
  expect(facts.stored!.controls, "a stored label must not render a form control").toBe(0);
  expect(
    facts.stored!.boxes,
    "the stored block's preview paints a control-shaped (bordered, input-height) box again",
  ).toBe(0);
  expect(facts.stored!.hasNote, "the stored block's preview lost the honest note").toBe(true);

  // ── SCOPED, NOT FLATTENED ──────────────────────────────────────────────
  // The field beside it must still be drawn as a field, or the "fix" would be
  // an empty canvas.
  expect(facts.normal, "the normal text block's card is missing").not.toBeNull();
  expect(facts.normal!.badge).toBe("Text");
  expect(facts.normal!.boxes, "the change leaked: a real field lost its input sketch").toBeGreaterThan(0);

  // ── THE WITHDRAWAL IS NOT UNDONE ───────────────────────────────────────
  expect(facts.offeredInPalette, "the palette offers the withdrawn block again").toEqual([]);
});
