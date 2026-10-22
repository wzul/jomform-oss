// Regression guard: a release over the builder's INSERTION GAP must land the
// block at the index that gap painted. (Card t_82df6ead.)
//
// THE DEFECT THIS FILE EXISTS FOR
//   The gap between two block cards is an EMPTY 8px strip that carries the
//   `-my-0.5` utility, so its box overlaps the card BELOW it by 2px. While a
//   palette block is dragged over the gap the strip GROWS to 32px, which moves
//   that card down — so on the seam (the card's top edge) the pointer is over
//   the CARD while the gap is at rest and over the STRIP once it has grown:
//
//     * over the card  -> its `onDragOver` targets `i + 1`, the gap shrinks back
//     * over the strip -> its `onDragOver` targets `i`,       the gap grows again
//
//   Because the card paints AFTER the strip in DOM order it wins that 2px seam,
//   so the loop is self-sustaining: the browser keeps re-targeting the drag
//   (dragenter/dragleave) without ever delivering an accepted `dragover` for the
//   element that is finally under the pointer. A release is then refused —
//   `dragend` reports `dropEffect: "none"`, no `drop` fires — and the block is
//   silently NOT inserted while the gap is still painted, promising that it will
//   be. Measured on the pre-fix tree: `drop: 0`, cards unchanged, and the same
//   read after a 300ms hold (so it is a dead seam, not a race with the grow
//   animation).
//
// WHAT THIS FILE PINS
//   arm A (deterministic, NO drag) — every gap owns its WHOLE painted box: for
//     every pixel row of the gap's box, the element under that point is that gap
//     and not a card it overlaps. That is the property the defect violates, and
//     the reason the browser re-targets the drag at all: an affordance that paints
//     "land HERE" must be the thing "here" resolves to. Read off the live DOM with
//     `elementFromPoint`, so it cannot be satisfied by a class list.
//   arm B (end to end, real held drag) — for EVERY aim point across a gap (its
//     whole 8px rest box and the 2px seams it shares with its neighbours), the
//     release lands ONE block, and it lands at the index the PAINTED gap named.
//     The index is read off the live DOM twice: which gap is painted before the
//     release, and where the new card sits in the label sequence after it. So no
//     arm can pass by asserting a constant — the affordance is compared with the
//     outcome. Both gaps are walked (index 0 above the first card, index 1 between
//     the cards), so a fix wired to one slot cannot pass.
//   arm C (end to end, the REPORTED gesture) — dragged onto the gap's seam and
//     released with no further pointer movement, which is what a merchant does
//     when the gap has just opened under the cursor. Arm B nudges until a
//     `dragover` is delivered at the aim point, which can rescue a churning seam
//     that one nudge leaves churning — so the two arms are not redundant.
//   * the mapping the rest of the canvas already had: over a card the block
//     lands BELOW it (`i + 1`), which is what the next gap paints.
//   arm D (clicks, NO drag) — the ACCEPTED CLICK TRADE-OFF and its EXTENT.
//     `relative` makes the gap the thing that "here" resolves to, and the price
//     is that the top of every card belongs to the gap above it: a click there
//     opens nothing. That price is accepted, but its extent is a property of the
//     box (`h-2` with `-my-0.5`, i.e. an 8px strip overlapping each neighbour by
//     2px). Widen the overlap — `-my-1`, `h-3`, padding, a border on the strip —
//     and the dead sliver doubles silently: arm A above still passes, because a
//     bigger gap still owns a bigger box. So arm D measures the overlap off the
//     card's and the gap's rects, asserts it IS 2px, and then clicks: 0.5px below
//     the card's top edge must open NO panel (dead, by decision), 2.5px must open
//     it (live from 2.0), and mid-card must open it (the card's own click path).
//     Every click is counted at the window, so "no panel" cannot be satisfied by
//     a click that never happened. Verified to bite: with `-my-1` in page.tsx
//     the measured overlap reads 4 and the 2.5px click lands on the gap — this
//     arm fails and nothing else in the file does.
//
// MEASUREMENT NOTES, because both of these decide whether a reading is a reading:
//   * Chromium coalesces drag events, so a stepped `mouse.move` can finish
//     WITHOUT a `dragover` at the final position — the page then paints nothing
//     and refuses the release for a reason that has nothing to do with the
//     defect. Every arm therefore waits until the page reports a `dragover`
//     delivered AT the aim point (the coordinates are recorded in the page) and
//     only then reads the paint. The pointer is nudged away and back to the SAME
//     aim point, so the nudge can never move the release.
//   * the gap is `transition-all` and grows 8px -> 32px, so each reading waits
//     for that grow to finish before the paint is compared with the drop.
//
// The API is stubbed at the browser level (no Go server, no session, no DB), so
// this runs in the `e2e` gate (`npm run e2e`, part of scripts/preflight.sh)
// against the build that gate already serves.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means — see
// e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const FORM_ID = "form_insertion_gap_drop_guard";
// Two blocks of DIFFERENT types, neither of them the palette block this spec
// drags (`text`, the palette's first item): the card LABELS then say where the
// new block landed, so the drop's index is measured instead of assumed.
const BLOCKS = [
  { type: "phone", value: "Phone field" },
  { type: "number", value: "Number field" },
];
const DRAGGED_LABEL = "Text";

async function stubAPI(page: Page) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

  // Playwright resolves matching routes in REVERSE registration order, so the
  // broad catch-all is registered FIRST and the specific routes after it.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (route) =>
    route.fulfill(
      json({
        me: { id: "u1", email: "merchant@example.com", name: "Merchant" },
        workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
      }),
    ),
  );
  await page.route(/(.*)\/api\/rest\/products(\?.*)?$/, (route) => route.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+(\?.*)?$/, (route) =>
    route.fulfill(
      json({
        id: FORM_ID,
        slug: "insertion-gap-drop-guard",
        title: "Insertion gap drop guard",
        status: "draft",
        url: "",
        config: { mode: "payment", blocks: BLOCKS },
        theme: {},
      }),
    ),
  );
}

/** A FRESH browser state, English — the default a new visitor gets. */
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

type Counters = { dragstart: number; dragover: number; dragend: number; drop: number };
type DragState = Counters & { lastOver: { x: number; y: number } | null };

/** Counts the REAL drag events the page receives — so every arm can state that a
 *  drag actually happened — and remembers WHERE the last `dragover` was
 *  delivered, which is how an arm knows its reading is of the position it names. */
async function instrument(page: Page) {
  await page.evaluate(() => {
    const w = window as unknown as { __drag: DragState };
    w.__drag = { dragstart: 0, dragover: 0, dragend: 0, drop: 0, lastOver: null };
    for (const k of ["dragstart", "dragover", "dragend", "drop"] as const) {
      document.addEventListener(
        k,
        (e) => {
          w.__drag[k] += 1;
          if (k === "dragover") {
            const de = e as DragEvent;
            w.__drag.lastOver = { x: de.clientX, y: de.clientY };
          }
        },
        true,
      );
    }
  });
}

async function dragState(page: Page): Promise<DragState> {
  return page.evaluate(() => (window as unknown as { __drag: DragState }).__drag);
}

type Rect = { x: number; y: number; w: number; h: number };

/** Every gap candidate on the canvas, in DOM order: the insertion strips (one
 *  above each card) and then the trailing drop zone. That order IS the index a
 *  gap promises — `insertAt(i)` — which is what makes the painted gap readable
 *  as an expected landing index.
 *
 *  The classes are how the canvas says "this is a gap": an empty strip with a
 *  NEGATIVE VERTICAL MARGIN (insertion) or the `-mt-1` collapse (trailing).
 *  Selecting them by that shape rather than by their PAINT is deliberate — an
 *  unpainted gap must still occupy its index, or the painted one could not be
 *  counted. The selector keeps the `-my-` PREFIX and drops the VALUE on purpose:
 *  how far the strip overlaps its neighbours is the thing arm D pins (2px), so a
 *  widened overlap must fail THAT arm by name, not make every arm in the file
 *  die with "no gap on the canvas" — a selector that hard-codes `.5` cannot tell
 *  the defect from a rename. */
async function gaps(page: Page): Promise<Rect[]> {
  return page.evaluate(() => {
    const els = document.querySelectorAll<HTMLElement>(
      'section div[class*="-my-"], section div[class*="-mt-1"]',
    );
    return [...els].map((el) => {
      const b = el.getBoundingClientRect();
      return { x: b.x, y: b.y, w: b.width, h: b.height };
    });
  });
}

/** The index of the gap that is PAINTED as the landing zone right now, or null.
 *  The highlighted branch is the only one that carries a border at all
 *  (`border-2 border-dashed`; the rest branch is a bare `h-2`/`h-4` box), and it
 *  GROWS to 32px/40px — both read off the live DOM, neither from a colour
 *  token, and the height is reported so an arm can state that the reading waited
 *  for the grow to finish. */
async function paintedGap(page: Page): Promise<{ index: number; h: number } | null> {
  return page.evaluate(() => {
    const els = document.querySelectorAll<HTMLElement>(
      'section div[class*="-my-"], section div[class*="-mt-1"]',
    );
    const painted = [...els].flatMap((el, i) => {
      const cs = getComputedStyle(el);
      if (cs.borderTopStyle !== "dashed" || cs.borderTopWidth === "0px") return [];
      return [{ index: i, h: Math.round(el.getBoundingClientRect().height) }];
    });
    if (painted.length === 0) return null;
    // More than one painted gap would be a different defect; report it as an
    // impossible index rather than silently taking the first.
    return painted.length === 1 ? painted[0] : { index: -painted.length, h: 0 };
  });
}

/** The cards' type labels, top to bottom — where the new block landed. */
async function cardLabels(page: Page): Promise<string[]> {
  return page.evaluate(() =>
    [...document.querySelectorAll<HTMLElement>("section div[draggable]")].map(
      (card) => (card.querySelector("span")?.textContent || "").trim(),
    ),
  );
}

/** Loads the builder with the two blocks, counts the page's real drag events,
 *  and hands back what the drag runs between. */
async function openBuilder(page: Page, baseURL: string) {
  await page.context().clearCookies();
  await signIn(page, baseURL);
  await freshState(page);
  await stubAPI(page);
  await page.goto(`/builder?form=${FORM_ID}`, { waitUntil: "domcontentloaded" });

  const cards = page.locator("section div[draggable]");
  const paletteItem = page.locator("aside").first().locator('[draggable="true"]').first();
  await expect(cards.first()).toBeVisible({ timeout: 45_000 });
  await expect(cards).toHaveCount(BLOCKS.length);
  await expect(paletteItem).toBeVisible();
  // Two insertion gaps over two cards, plus the trailing zone: exactly the three
  // candidates the index arithmetic below assumes.
  await expect
    .poll(async () => (await gaps(page)).length, { timeout: 5_000 })
    .toBe(BLOCKS.length + 1);
  // The DEFAULT locale, stated: a leftover `jf_lang` from an earlier session
  // would render another language, i.e. a state no new visitor has.
  expect(await page.evaluate(() => document.documentElement.lang)).toBe("en");
  await instrument(page);

  const pb = await paletteItem.boundingBox();
  if (!pb) throw new Error("the palette item has no box on screen");
  return { from: { x: pb.x + pb.width / 2, y: pb.y + pb.height / 2 } };
}

/** Picks the palette block up with real mouse input and holds it. */
async function pickUp(page: Page, from: { x: number; y: number }) {
  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  await page.mouse.move(from.x + 6, from.y + 6, { steps: 2 });
}

const SETTLE_MS = 300; // > the gap's 150ms `transition-all` grow

/** Holds the pointer EXACTLY at `(x, y)` and does not return until the page has
 *  delivered a `dragover` at that point AND the gap has finished growing, so the
 *  reading that follows is of this position and of the settled paint.
 *
 *  The retry nudges AWAY and BACK to the same point: the last pointer move is
 *  always the aim point, so no amount of retrying can move the release. Two
 *  things about the timing are measured, not guessed:
 *   * Chromium fires `dragenter` when the pointer arrives at a new element and
 *     follows it with a `dragover` only on a LATER tick — measured on this tree,
 *     a single `mouse.move(target, {steps: 12})` ends on a `dragenter` and no
 *     `dragover`, which is why the wait comes AFTER the move and why the nudge
 *     exists at all.
 *   * the browser coalesces bursts of `mouse.move`, so each attempt is followed
 *     by a wait long enough for the events of that attempt to be dispatched.
 *  The coordinate match is exact to within half a pixel, which is the rounding a
 *  fractional aim point (a box centre) is reported with; it cannot reach the next
 *  pixel, which is what keeps a seam reading a seam reading. */
async function holdAt(page: Page, x: number, y: number) {
  await page.mouse.move(x, y, { steps: 12 });
  await page.waitForTimeout(150);
  for (let tries = 0; tries < 8; tries += 1) {
    // The nudge is HORIZONTAL on purpose: the gaps and the cards are horizontal
    // bands, so a sideways move cannot change which of them owns the pointer, and
    // the reading stays a reading of this row. A diagonal nudge would cross into
    // the card below, which is a different landing index and re-lays-out the
    // canvas under the pointer.
    await page.mouse.move(x + 2, y, { steps: 2 });
    await page.waitForTimeout(120);
    await page.mouse.move(x, y, { steps: 2 });
    await page.waitForTimeout(180);
    const s = await dragState(page);
    const over = s.lastOver;
    if (over && Math.abs(over.x - x) <= 0.5 && Math.abs(over.y - y) <= 0.5) {
      await page.waitForTimeout(SETTLE_MS);
      const painted = await paintedGap(page);
      if (painted) return painted;
    }
  }
  const s = await dragState(page);
  throw new Error(
    `the canvas never answered a held pointer at ${x},${y} — last dragover was ${JSON.stringify(s.lastOver)}, dragstart ${s.dragstart}`,
  );
}

/** Holds at an absolute aim point, then releases, and reports what the page said
 *  (the painted gap) and what it did (the drop, and where the block landed). */
async function releaseAt(page: Page, x: number, releaseY: number) {
  const painted = await holdAt(page, x, releaseY);
  const before = (await cardLabels(page)).length;
  await page.mouse.up();
  const c = await dragState(page);
  await expect
    .poll(async () => (await cardLabels(page)).length, { timeout: 5_000 })
    .toBe(before + (c.drop > 0 ? 1 : 0));
  return { painted, counters: c, labels: await cardLabels(page), before };
}

/** One aim point, start to finish. */
async function aimAtGap(page: Page, baseURL: string, gapIndex: number, delta: number) {
  const { from } = await openBuilder(page, baseURL);
  const box = (await gaps(page))[gapIndex];
  if (!box) throw new Error(`gap ${gapIndex} is not on the canvas`);
  await pickUp(page, from);
  const r = await releaseAt(page, box.x + box.w / 2, box.y + delta);
  const at = `gap ${gapIndex} @${delta}px`;

  // The gesture must really have been a drag, or every reading below is vacuous.
  expect(r.counters.dragstart, `${at}: no real drag was started`).toBe(1);
  expect(r.counters.dragend, `${at}: the browser delivered no dragend`).toBe(1);

  // THE PROMISE: the gap that was painted is where the block goes, and a release
  // the affordance invited always lands exactly one block.
  expect(
    r.counters.drop,
    `${at}: the release was refused — the painted gap promised a landing zone that accepted nothing (dragover ${r.counters.dragover}, drop ${r.counters.drop}, and dragend reports dropEffect "none")`,
  ).toBe(1);
  expect(
    r.labels.length,
    `${at}: released over the painted gap ${r.painted.index} (h=${r.painted.h}) and the canvas holds ${JSON.stringify(r.labels)}`,
  ).toBe(BLOCKS.length + 1);
  expect(
    r.labels.indexOf(DRAGGED_LABEL),
    `${at}: the painted gap said index ${r.painted.index} but the block landed at ${r.labels.indexOf(DRAGGED_LABEL)} — ${JSON.stringify(r.labels)}`,
  ).toBe(r.painted.index);
  return r;
}

// The gap's rest box is 8px tall and overlaps its neighbours by 2px, so an aim
// point is named for every pixel of it plus the seams: 0..9. The seam with the
// card BELOW is 6 (= 8 - 2), which is the pixel this file was filed for.
const DELTAS = [0, 1, 2, 3, 4, 5, 6, 7, 8, 9];

// ARM A — the property the defect violates, with NO drag involved: the gap owns
// every pixel row of the box it paints. Runs per gap and per row, so the rows
// that fail are named.
test("every insertion gap owns the whole box it paints, so 'land here' is what the pointer resolves to", async ({
  page,
  baseURL,
}) => {
  test.setTimeout(120_000);
  await openBuilder(page, baseURL!);
  const boxes = await gaps(page);
  const failures: string[] = [];
  for (let i = 0; i < BLOCKS.length; i += 1) {
    const box = boxes[i];
    for (let row = Math.floor(box.y); row < Math.ceil(box.y + box.h); row += 1) {
      const owner = await page.evaluate(
        ({ x, y }) => {
          const el = document.elementFromPoint(x, y) as HTMLElement | null;
          if (!el) return "nothing";
          const all = [
            ...document.querySelectorAll<HTMLElement>(
              'section div[class*="-my-"], section div[class*="-mt-1"]',
            ),
          ];
          const gapEl = el.closest?.(
            'section div[class*="-my-"], section div[class*="-mt-1"]',
          ) as HTMLElement | null;
          if (gapEl) return `gap ${all.indexOf(gapEl)}`;
          if (el.closest?.("section div[draggable]")) return "a block card";
          return `${el.tagName}.${String(el.className).slice(0, 30)}`;
        },
        { x: Math.round(box.x + box.w / 2), y: row },
      );
      if (owner !== `gap ${i}`) {
        failures.push(`gap ${i} row ${row} (its box is ${box.y}..${box.y + box.h}) resolves to ${owner}`);
      }
    }
  }
  expect(
    failures,
    `the insertion gap does not own the box it paints — the pixels it highlights resolve to something else, so the drag re-targets under a still pointer and the release can be refused:\n${failures.join("\n")}`,
  ).toEqual([]);
});

for (const gapIndex of [0, 1]) {
  test(`a release over insertion gap ${gapIndex} lands the block at the index that gap painted`, async ({
    page,
    baseURL,
  }) => {
    test.setTimeout(300_000);
    for (const delta of DELTAS) {
      const r = await aimAtGap(page, baseURL!, gapIndex, delta);
      console.log(
        `[gap ${gapIndex} @${delta}px] painted=${r.painted.index} h=${r.painted.h} drop=${r.counters.drop} labels=${JSON.stringify(r.labels)}`,
      );
    }
  });
}

// ARM C — the reported gesture itself: the block is dragged onto the seam and
// released with no further pointer movement, which is what a merchant does when
// the gap has just opened under the cursor.
test("the reported gesture — released on the gap's own seam with no further movement — still lands", async ({
  page,
  baseURL,
}) => {
  test.setTimeout(120_000);
  const { from } = await openBuilder(page, baseURL!);
  const gap0 = (await gaps(page))[0];
  // The seam: the last 2px of the gap's rest box, where `-my-0.5` lets the card
  // below overlap it. That row is the card's at rest and the gap's once the gap
  // has grown, which is exactly the reading this card was filed on.
  const x = Math.round(gap0.x + gap0.w / 2);
  const seam = Math.round(gap0.y + gap0.h) - 2;
  await pickUp(page, from);
  await page.mouse.move(x - 2, seam - 2, { steps: 12 });
  await page.mouse.move(x, seam, { steps: 2 });
  await page.waitForTimeout(SETTLE_MS);
  const painted = await paintedGap(page);
  const before = await cardLabels(page);
  await page.mouse.up();
  const c = await dragState(page);
  const after = await cardLabels(page);

  expect(c.dragstart, "the palette block was never picked up").toBe(1);
  expect(painted, "no gap was painted for the release").not.toBeNull();
  expect(
    c.drop,
    `released on the gap's seam while it was painted (gap ${painted?.index}, h=${painted?.h}) and the browser refused the drop: drop ${c.drop}, dragend dropEffect "none"`,
  ).toBe(1);
  expect(before.length, "the canvas never grew").toBe(BLOCKS.length);
  expect(
    after.indexOf(DRAGGED_LABEL),
    `the seam promised index ${painted?.index} and the block landed at ${after.indexOf(DRAGGED_LABEL)} — ${JSON.stringify(after)}`,
  ).toBe(painted!.index);
});

test("a release over a block card still lands BELOW it, and the next gap says so", async ({
  page,
  baseURL,
}) => {
  test.setTimeout(120_000);
  const { from } = await openBuilder(page, baseURL!);
  await pickUp(page, from);
  // The MIDDLE of the first card: the block belongs after it, i.e. index 1 — the
  // gap between the cards, which is the one that must paint.
  const card = page.locator("section div[draggable]").nth(0);
  const box = await card.boundingBox();
  if (!box) throw new Error("the first card has no box on screen");
  const r = await releaseAt(
    page,
    Math.round(box.x + box.width / 2),
    Math.round(box.y + box.height / 2),
  );

  expect(r.counters.drop, "the browser delivered no drop for a release over a block card").toBe(1);
  expect(r.painted.index, "the gap after the card under the pointer must be the painted one").toBe(1);
  expect(r.labels, `the block landed at ${JSON.stringify(r.labels)}`).toEqual([
    "Phone",
    DRAGGED_LABEL,
    "Number",
  ]);
});

// ─── arm D: the accepted click trade-off, and its EXTENT ────────────────────

type ClickState = { count: number; last: { x: number; y: number } | null };

/** Counts the REAL clicks the page receives, so "the panel did not open" can be
 *  told apart from "the click never happened" — the two look identical in the
 *  DOM, and only one of them is evidence. Re-installed after every navigation. */
async function instrumentClicks(page: Page) {
  await page.evaluate(() => {
    const w = window as unknown as { __clicks: ClickState };
    w.__clicks = { count: 0, last: null };
    document.addEventListener(
      "click",
      (e) => {
        const m = e as MouseEvent;
        w.__clicks.count += 1;
        w.__clicks.last = { x: m.clientX, y: m.clientY };
      },
      true,
    );
  });
}

async function clickState(page: Page): Promise<ClickState> {
  return page.evaluate(() => (window as unknown as { __clicks: ClickState }).__clicks);
}

/** The card at `index` and the insertion gap that sits directly ABOVE it, both
 *  as `getBoundingClientRect()` reports them. The gap's box overlaps the card's
 *  top by the accepted sliver, so this pair — never a constant — is what the
 *  click extent is a property of. */
async function cardWithGapAbove(
  page: Page,
  index: number,
): Promise<{ card: Rect; gap: Rect }> {
  const measured = await page.evaluate((i) => {
    const rect = (el: Element): Rect => {
      const b = el.getBoundingClientRect();
      return { x: b.x, y: b.y, w: b.width, h: b.height };
    };
    const cards = [...document.querySelectorAll<HTMLElement>("section div[draggable]")];
    const strips = [
      ...document.querySelectorAll<HTMLElement>(
        'section div[class*="-my-"], section div[class*="-mt-1"]',
      ),
    ];
    const card = cards[i];
    // DOM order is one insertion strip per card, then the trailing zone, so the
    // strip at the same index is the one above this card.
    const gap = strips[i];
    if (!card || !gap) return null;
    return { card: rect(card), gap: rect(gap) };
  }, index);
  if (!measured) throw new Error(`card ${index}, or the gap above it, is not on the canvas`);
  return measured;
}

/** What a pointer at `(x, y)` resolves to, in the same vocabulary as arm A. */
async function ownerAt(page: Page, x: number, y: number): Promise<string> {
  return page.evaluate(
    ({ px, py }) => {
      const el = document.elementFromPoint(px, py) as HTMLElement | null;
      if (!el) return "nothing";
      const all = [
        ...document.querySelectorAll<HTMLElement>(
          'section div[class*="-my-"], section div[class*="-mt-1"]',
        ),
      ];
      const gapEl = el.closest?.(
        'section div[class*="-my-"], section div[class*="-mt-1"]',
      ) as HTMLElement | null;
      if (gapEl) return `gap ${all.indexOf(gapEl)}`;
      if (el.closest?.("section div[draggable]")) return "a block card";
      return `${el.tagName}.${String(el.className).slice(0, 30)}`;
    },
    { px: x, py: y },
  );
}

/** How many of the settings panel's own controls are on screen. The panel's ids
 *  are `jf-blk-<block>-<key>` (page.tsx `fid`) and it renders ONLY for a selected
 *  block, so this is exactly "is a block's settings panel open" — zero at rest,
 *  on the live DOM rather than inferred from state. */
async function panelFieldCount(page: Page): Promise<number> {
  return page.evaluate(
    () =>
      [...document.querySelectorAll<HTMLElement>('[id^="jf-blk-"]')].filter(
        (el) => el.offsetParent !== null,
      ).length,
  );
}

/** Arm D: the ACCEPTED trade-off is 2px of dead card top, and it is exactly 2px.
 *
 *  WHY IT EXISTS. Arm A pins that the gap owns the box it paints. It says
 *  nothing about how far that box reaches into the card below it, and that reach
 *  is the price a merchant pays: the top of every card cannot be clicked. The
 *  price is a property of `h-2` minus `-my-0.5`, so a layout edit widens it with
 *  no warning — arm A still passes, because a bigger box is still owned. This
 *  arm therefore measures the overlap and states the number, then proves both
 *  sides of the boundary with real clicks:
 *
 *    * at `card.y + 0.5` — inside the accepted sliver — the gap owns the point
 *      and NO panel opens. Stated explicitly: this is a decision, not an
 *      accident, and a future reader must not "fix" it back into the refused-
 *      drop defect this file was filed for.
 *    * at `card.y + 2.5` — the first half-pixel below the sliver — the card owns
 *      the point and its panel opens.
 *    * mid-card — the card's own click path opens the panel (the prod card's
 *      item 4, which had no guard).
 *
 *  The anchors (`card.y`, the card's height, the gap's box) come from the live
 *  rects; the 0.5/2.5 offsets are not layout constants but the EXTENT itself —
 *  half a pixel inside the accepted sliver, half a pixel below it — so an
 *  overlap that widens fails here instead of drifting unnoticed.
 */
test("the accepted click trade-off is exactly 2px: card.y+0.5 is dead, card.y+2.5 opens the panel", async ({
  page,
  baseURL,
}) => {
  test.setTimeout(240_000);

  for (const cardIndex of [0, 1]) {
    const at = `card ${cardIndex}`;

    // Dead click and first-live click share ONE page state on purpose: the dead
    // click must leave no panel, so the live reading that follows is a real
    // transition rather than a panel that was already there.
    await openBuilder(page, baseURL!);
    await instrumentClicks(page);
    const { card, gap } = await cardWithGapAbove(page, cardIndex);

    // THE EXTENT, measured rather than assumed. This is the assertion that fails
    // if the overlap is widened: 4px of dead card top is a different product
    // decision from 2px.
    const overlap = Math.round((gap.y + gap.h - card.y) * 100) / 100;
    expect(
      overlap,
      `${at}: the gap above the card reaches ${overlap}px into its top, not 2px (gap box ${gap.y}..${gap.y + gap.h}, card top ${card.y}). That reach is the part of the card a click cannot open — widening it (a bigger -my, a taller h-, padding, a border) makes the top of every card dead, and arm A cannot see it because a bigger gap still owns a bigger box.`,
    ).toBe(2);

    const cx = card.x + card.w / 2;

    // (1) THE ACCEPTED TRADE-OFF. Half a pixel below the card's top edge the gap
    // owns the point and a click there opens nothing.
    expect(
      await ownerAt(page, cx, card.y + 0.5),
      `${at}: the point 0.5px below the card's top edge (card ${card.y}..${card.y + card.h}) does not resolve to the gap above it — the accepted dead sliver moved`,
    ).toBe(`gap ${cardIndex}`);
    expect(
      await panelFieldCount(page),
      `${at}: a settings panel was already open on the fresh load — the dead-click reading below would be vacuous`,
    ).toBe(0);
    await page.mouse.click(cx, card.y + 0.5);
    const dead = await clickState(page);
    expect(
      dead.count,
      `${at}: the dead click never reached the page (0 click events at ${cx},${card.y + 0.5}), so "no panel" would prove nothing`,
    ).toBe(1);
    expect(
      await panelFieldCount(page),
      `${at}: a click 0.5px below the card's top edge opened the block panel — the accepted 2px dead sliver has shrunk or gone, which is a change to the decision this arm pins (and the reason it is 2px is documented above)`,
    ).toBe(0);

    // (2) THE FIRST LIVE PIXEL. Half a pixel below the sliver the CARD owns the
    // point, and the click opens its panel: dead 0.00..1.99, live from 2.0.
    const liveY = card.y + 2.5;
    expect(
      await ownerAt(page, cx, liveY),
      `${at}: the point 2.5px below the card's top edge still resolves to the gap, not the card — the dead sliver is now wider than the 2px this arm pins`,
    ).toBe("a block card");
    await page.mouse.click(cx, liveY);
    const live = await clickState(page);
    expect(
      live.count,
      `${at}: the live click never reached the page (expected 2 click events, saw ${live.count})`,
    ).toBe(2);
    const fields = await panelFieldCount(page);
    expect(
      fields,
      `${at}: a click at card.y+2.5 (${cx},${liveY}) opened no settings panel, so the card's top 2.5px is dead and the accepted sliver is wider than the 2px the layout claims`,
    ).toBeGreaterThan(0);
    console.log(
      `[${at}] overlap=${overlap}px dead@+0.5 fields=0 live@+2.5 fields=${fields}`,
    );

    // (3) MID-CARD, on its own page state: the card's own click path still opens
    // the panel. A fresh load, because the panel opened in (2) would make this
    // reading vacuous.
    await openBuilder(page, baseURL!);
    await instrumentClicks(page);
    const again = await cardWithGapAbove(page, cardIndex);
    expect(
      await panelFieldCount(page),
      `${at}: a settings panel was already open on the fresh load — the mid-card reading would be vacuous`,
    ).toBe(0);
    const midX = again.card.x + again.card.w / 2;
    const midY = again.card.y + again.card.h / 2;
    await page.mouse.click(midX, midY);
    expect(
      (await clickState(page)).count,
      `${at}: the mid-card click never reached the page (${midX},${midY})`,
    ).toBe(1);
    const midFields = await panelFieldCount(page);
    expect(
      midFields,
      `${at}: a click in the middle of the card (${midX},${midY}) opened no settings panel — the card's own click path is broken`,
    ).toBeGreaterThan(0);
    console.log(`[${at}] mid-card fields=${midFields}`);
  }
});
