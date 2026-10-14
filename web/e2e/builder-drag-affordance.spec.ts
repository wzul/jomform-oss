// Regression guard: the builder's DRAG AFFORDANCE — the palette dim, the
// insertion gap that says where the block will land, and the drop-zone tint —
// must PAINT while a block is held mid-drag, and must be CLEARED when the drag
// ends, wherever it ends. (Card t_769a5cb9.)
//
// THE DEFECT THIS FILE EXISTS FOR
//   `web/src/app/builder/page.tsx` used to compute
//       const dragging = dragType.current !== null || dragIdx.current !== null;
//   DURING RENDER, while the drag-start handlers only mutated those two REFS.
//   Refs schedule no render, so every component committed before the drag kept
//   reading `false` and stayed on the rest branch — the tint, the insertion gap
//   and the palette dim were all dead while the DROP itself still worked
//   (`canvasDrop` reads the refs directly). Measured on a production build with a
//   real held drag, on the pre-fix tree: the empty-canvas drop ghost kept
//   `border-slate-300` with a transparent background, no insertion gap rendered,
//   and no palette item carried `opacity-60`.
//
//   The reachability half was fixed on card t_93b097c6 (PR #1120, `bf2e5b40`),
//   which reads the refs live in `canvasDragOver` and so reaches `setDropAt` and
//   the re-render that follows. ITS SPEC
//   (`builder-canvas-dropzone-contrast.spec.ts`) guards the EMPTY-canvas drop
//   ghost only, and no spec covered the two symptoms reachable only on a
//   NON-EMPTY canvas — the insertion gap and the palette dim — or the drag
//   state's cleanup. Those are what card t_769a5cb9's remaining work is, and
//   what this file pins:
//
//   * the painted flag is STATE now, so the palette dims the moment a block is
//     picked up. Measured on the tree that had `canvasDragOver` fixed but still
//     read the refs in render: a real held drag delivered `dragstart` and every
//     palette item stayed at `opacity: 1`, because nothing re-rendered until a
//     `dragover` reached the canvas. That window is arm 1 below.
//   * an ABANDONED drag (released outside the canvas) left the affordance
//     painted. Measured on that tree: `dragend` was delivered, and afterwards the
//     trailing drop zone was still h=40 with its tint and every palette item was
//     still at `opacity: 0.6`, because `dropAt`/`dragType`/`dragIdx` were cleared
//     only on the canvas drop path and the palette items had no `onDragEnd` at
//     all. Both drag paths now end in one `endDrag()`; that is arm 1's tail.
//
// WHAT THIS FILE PINS, against a real held drag (real mouse input, no synthetic
// DragEvents, no DOM forcing) with `dragstart`/`dragover`/`dragend`/`drop`
// recounted IN THE PAGE, so a gesture that never started cannot pass vacuously:
//   arm 1 — the rest branch (told apart by measurement, not by assumption); the
//           palette dimming while the drag is in progress, including BEFORE the
//           pointer has reached the canvas; the insertion gap at the index the
//           pointer is over (read off the PAINTED box: height + dashed #7170ff
//           border + the translucent tint) re-measured at a second index so a gap
//           wired to one slot cannot pass; and the affordance cleared when the
//           drag is abandoned outside the canvas.
//   arm 2 — the drop still lands, at the index the gap pointed at, and takes the
//           affordance with it.
//
// TWO MEASUREMENT NOTES, both of them things the rig had to get right:
//   * Chromium coalesces drag events, so a single `mouse.move(target, {steps:
//     12})` can finish WITHOUT a `dragover` at the final position — measured on
//     this tree, the canvas stayed on the rest branch after such a move and
//     painted as soon as the pointer moved 2px further. A 2px nudge is a real
//     pointer move, not a synthetic event, and it is what makes each reading a
//     reading of the position the spec names.
//   * The gap is `transition-all` and GROWS while the pointer hovers it (8px ->
//     32px), which moves every card below it. So each hop re-reads the target's
//     CURRENT box instead of aiming at a box measured before the layout moved.
//
// The API is stubbed at the browser level (no Go server, no session, no DB), so
// this runs in the `e2e` gate (`npm run e2e`, part of scripts/preflight.sh)
// against the build that gate already serves.
import { test, expect, type Locator, type Page } from "@playwright/test";

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means — see
// e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const FORM_ID = "form_drag_affordance_guard";
// Two blocks, so the canvas renders BOTH shapes the gap has: the insertion gap
// between/next to the cards, and the trailing drop zone at the end of the canvas.
const BLOCKS = [
  { type: "text", value: "First field" },
  { type: "text", value: "Second field" },
];

/** The tinted gap as PAINTED: h-8 (32px) for an insertion gap, h-10 (40px) for
 *  the trailing drop zone. The rest branch is h-2 (8px) with no border at all, so
 *  height + the dashed indigo border say which branch is on screen without
 *  grepping a class list. */
const GAP_REST_MAX = 24;
const GAP_INSERTION = 32;
const GAP_TRAILING = 40;
const INDIGO = "rgb(113, 112, 255)"; // border-[#7170ff]
const TRANSPARENT = "rgba(0, 0, 0, 0)";
const DIM = "0.6"; // opacity-60

type Point = { x: number; y: number };

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
        slug: "drag-affordance-guard",
        title: "Drag affordance guard",
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

/** Counts the REAL drag events the page receives, so every arm can state that a
 *  drag actually happened instead of assuming the gesture worked. */
async function instrument(page: Page) {
  await page.evaluate(() => {
    const w = window as unknown as { __drag: Record<string, number> };
    w.__drag = { dragstart: 0, dragover: 0, dragend: 0, drop: 0 };
    for (const k of Object.keys(w.__drag)) {
      document.addEventListener(k, () => (w.__drag[k] += 1), true);
    }
  });
}

async function counters(page: Page): Promise<Counters> {
  return page.evaluate(() => (window as unknown as { __drag: Counters }).__drag);
}

/** One reading of the whole affordance, taken from the live DOM. */
type Reading = {
  /** Palette items and their computed opacity — the drag dim. */
  paletteOpacity: string[];
  /** Every element PAINTING a tinted insertion gap: height + its own background. */
  gaps: { h: number; bg: string }[];
  /** Block cards on the canvas — the drop's own count. */
  cards: number;
  /** A transition still running means the reading is mid-fade, not the state a
   *  user holds: the gaps are `transition-all`, so the height animates 8px -> 32px. */
  animating: boolean;
};

async function read(page: Page): Promise<Reading> {
  return page.evaluate(
    ({ restMax, indigo }) => {
      const palette = document.querySelector("aside");
      const items = palette ? [...palette.querySelectorAll<HTMLElement>('[draggable="true"]')] : [];
      const all = [...document.querySelectorAll<HTMLElement>("div")];
      const gaps = all.flatMap((el) => {
        // The empty-canvas drop ghost renders text; an insertion gap is an empty
        // strip. The ghost is excluded here on purpose — the sibling contrast spec
        // owns it, and this file is about the non-empty canvas.
        if ((el.textContent || "").trim() !== "") return [];
        const cs = getComputedStyle(el);
        if (cs.borderTopStyle !== "dashed" || cs.borderTopColor !== indigo) return [];
        const h = Math.round(el.getBoundingClientRect().height);
        if (h <= restMax) return [];
        return [{ h, bg: cs.backgroundColor }];
      });
      return {
        paletteOpacity: items.map((el) => getComputedStyle(el).opacity),
        gaps,
        cards: document.querySelectorAll("section div[draggable]").length,
        animating: all.some((el) => el.getAnimations().some((a) => a.playState === "running")),
      };
    },
    { restMax: GAP_REST_MAX, indigo: INDIGO },
  );
}

/** Waits until the affordance is exactly what the arm expects AND has finished
 *  fading, then returns that reading. The wait IS the assertion that the state
 *  happens at all: it fails rather than silently measuring the rest branch. */
async function settled(page: Page, want: { gap: number | null; dim: boolean }): Promise<Reading> {
  await expect
    .poll(
      async () => {
        const r = await read(page);
        if (r.animating) return "fading";
        const gapOk = want.gap === null ? r.gaps.length === 0 : r.gaps.length === 1 && r.gaps[0].h === want.gap;
        if (!gapOk) return `gap:${JSON.stringify(r.gaps)}`;
        const dimmed = r.paletteOpacity.length > 0 && r.paletteOpacity.every((o) => o === DIM);
        if (dimmed !== want.dim) return `dim:${JSON.stringify(r.paletteOpacity)}`;
        return "ok";
      },
      {
        timeout: 15_000,
        message: `the drag affordance never painted (expected gap=${want.gap}, dim=${want.dim})`,
      },
    )
    .toBe("ok");
  return read(page);
}

/** The CURRENT centre of a target — re-read per hop, because the gap grows under
 *  the pointer and moves every card below it. */
async function centreOf(locator: Locator): Promise<Point> {
  const b = await locator.boundingBox();
  if (!b) throw new Error("the drag target has no box on screen");
  return { x: b.x + b.width / 2, y: b.y + b.height / 2 };
}

/** Moves the held pointer onto `to` and then nudges it 2px, so a fresh `dragover`
 *  is delivered AT that position (see the measurement notes in this file's head). */
async function dragOnto(page: Page, to: Point) {
  await page.mouse.move(to.x, to.y, { steps: 12 });
  await page.mouse.move(to.x + 2, to.y + 2, { steps: 2 });
}

/** Loads the builder with two blocks, counts the page's real drag events, and
 *  hands back the elements the drag runs between. */
async function openBuilder(page: Page, baseURL: string) {
  await page.context().clearCookies();
  await signIn(page, baseURL);
  await freshState(page);
  await stubAPI(page);
  await page.goto(`/builder?form=${FORM_ID}`, { waitUntil: "domcontentloaded" });

  const cards = page.locator("section div[draggable]");
  const paletteItems = page.locator("aside").first().locator('[draggable="true"]');
  await expect(cards.first()).toBeVisible({ timeout: 45_000 });
  await expect(cards).toHaveCount(BLOCKS.length);
  await expect(paletteItems.first()).toBeVisible();
  // The DEFAULT locale, stated: a leftover `jf_lang` from an earlier session
  // would render another language, i.e. a state no new visitor has.
  expect(await page.evaluate(() => document.documentElement.lang)).toBe("en");
  await instrument(page);

  // The palette column's own padding — off every palette item, so a release here
  // cannot also be a click on one (which would add a block and make "the
  // affordance was cleared" indistinguishable from "a second drag started").
  const aside = await page.locator("aside").first().boundingBox();
  if (!aside) throw new Error("the palette column has no box");
  return {
    palette: paletteItems.first(),
    cards,
    abandon: { x: aside.x + 4, y: aside.y + 8 },
  };
}

test("the palette dims the moment a block is picked up, the insertion gap tracks the pointer, and an abandonment clears both", async ({
  page,
  baseURL,
}) => {
  test.setTimeout(120_000);
  const { palette, cards, abandon } = await openBuilder(page, baseURL!);

  // The REST branch first: nothing dimmed, no gap painted. The two states are
  // told apart by measurement rather than assumed to differ.
  const rest = await settled(page, { gap: null, dim: false });
  expect(rest.paletteOpacity.length, "the palette rendered no draggable item").toBeGreaterThan(0);
  expect(rest.cards).toBe(BLOCKS.length);
  console.log(`[rest] palette=${JSON.stringify(rest.paletteOpacity)} gaps=${JSON.stringify(rest.gaps)}`);

  // The drag is picked up, and the palette says so BEFORE the pointer has reached
  // the canvas: the dim is render input, not an effect of a `dragover`.
  const from = await centreOf(palette);
  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  await page.mouse.move(from.x + 6, from.y + 6, { steps: 2 });
  const started = await settled(page, { gap: null, dim: true });
  const atStart = await counters(page);
  console.log(`[picked up] palette=${JSON.stringify(started.paletteOpacity)} counters=${JSON.stringify(atStart)}`);
  expect(atStart.dragstart, "no real drag was started — the reading would be vacuous").toBe(1);
  expect(atStart.drop, "something was dropped before the pointer reached the canvas").toBe(0);

  // Over the FIRST card the app targets index 1, so the INSERTION GAP (h-8)
  // between the two cards is the one that paints.
  await dragOnto(page, await centreOf(cards.nth(0)));
  const first = await settled(page, { gap: GAP_INSERTION, dim: true });
  console.log(`[held over card 0] palette=${JSON.stringify(first.paletteOpacity)} gaps=${JSON.stringify(first.gaps)}`);
  expect(first.gaps[0].bg, "the insertion gap painted no tint of its own").not.toBe(TRANSPARENT);
  expect(first.cards, "the block count changed while the pointer was still down").toBe(BLOCKS.length);

  // …and at a SECOND index: over the last card the app targets the end of the
  // canvas, so the TRAILING zone (h-10) paints instead. A gap wired to one fixed
  // slot, or a `dropAt` that does not track the pointer, fails here.
  await dragOnto(page, await centreOf(cards.nth(1)));
  const second = await settled(page, { gap: GAP_TRAILING, dim: true });
  console.log(`[held over card 1] palette=${JSON.stringify(second.paletteOpacity)} gaps=${JSON.stringify(second.gaps)}`);
  expect(second.gaps[0].bg, "the trailing drop zone painted no tint of its own").not.toBe(TRANSPARENT);
  expect(second.cards, "the block count changed while the pointer was still down").toBe(BLOCKS.length);

  // The pointer leaves the canvas and is released over the palette column. The
  // browser fires `dragend` on the SOURCE, the only signal an abandoned drag
  // produces — no `drop` runs at all.
  await page.mouse.move(abandon.x, abandon.y, { steps: 12 });
  await page.mouse.move(abandon.x, abandon.y + 2, { steps: 2 });
  await page.mouse.up();

  const c = await counters(page);
  expect(c.drop, "a drop ran — this arm did not exercise the abandoned-drag path").toBe(0);
  expect(c.dragend, "the browser delivered no `dragend` for the abandoned drag").toBe(1);

  const after = await settled(page, { gap: null, dim: false });
  console.log(`[abandoned] palette=${JSON.stringify(after.paletteOpacity)} gaps=${JSON.stringify(after.gaps)}`);
  expect(after.cards, "an abandoned drag dropped a block").toBe(BLOCKS.length);
});

test("the drop still lands where the gap pointed, and takes the affordance with it", async ({
  page,
  baseURL,
}) => {
  test.setTimeout(120_000);
  const { palette, cards } = await openBuilder(page, baseURL!);

  const from = await centreOf(palette);
  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  // Straight onto the last card — the target index the trailing gap names — with
  // no hop in between, so the release happens with the geometry the reading saw.
  await dragOnto(page, await centreOf(cards.nth(1)));
  const held = await settled(page, { gap: GAP_TRAILING, dim: true });
  console.log(`[held] gaps=${JSON.stringify(held.gaps)} cards=${held.cards}`);

  await page.mouse.up();

  const c = await counters(page);
  console.log(`[released] counters=${JSON.stringify(c)}`);
  expect(c.drop, "the browser delivered no `drop` for a release over the canvas").toBe(1);
  await expect(cards).toHaveCount(BLOCKS.length + 1, { timeout: 15_000 });
  const after = await settled(page, { gap: null, dim: false });
  console.log(`[after the drop] palette=${JSON.stringify(after.paletteOpacity)} gaps=${JSON.stringify(after.gaps)}`);
  expect(after.cards).toBe(BLOCKS.length + 1);
});
