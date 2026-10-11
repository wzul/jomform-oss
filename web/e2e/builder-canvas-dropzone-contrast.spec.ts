// Regression guard: the BUILDER CANVAS drop zone's DRAG-OVER text must clear
// WCAG AA on the translucent tint it is painted on (card t_93b097c6).
//
// THE DEFECT
//   With no blocks on the canvas the builder renders a dashed drop zone. While a
//   palette block is dragged over it, `web/src/app/builder/page.tsx` swaps
//     "border-slate-300 text-[var(--jf-muted-on-tint)]"
//   for
//     "border-[#7170ff] bg-indigo-50/50 text-[var(--jf-primary-on-tint)]"
//   and the last token is what this guard is about. Measured on a production
//   build in a real browser, on the COMPOSITED surface:
//
//     --jf-primary-on-tint #4338ca on #f2f5fc = 7.241:1  <- AA-safe
//     --jf-primary         #5e6ad2 on #f2f5fc = 4.306:1  <- under the 4.5 floor
//
//   The base token is the trap: it measures 4.700:1 on WHITE, which is where it
//   was checked, and the drag-over text is never painted on white — it is
//   painted on `bg-indigo-50/50` composited over the page background
//   (`--jf-bg` #f7f8f8). The card's own arithmetic said #f3f5fc and 4.309:1; the
//   renderer's oklab mix of the translucent tint lands one step darker at
//   #f2f5fc, which is what is measured here. The card attributed this to PR 701
//   and then corrected itself on the board: `git show 0d64a33` shows 701
//   CHANGING `--jf-primary` -> `--jf-primary-on-tint`, i.e. 701 FIXED this line,
//   so the failing value is the pre-701 one and the defect is long-standing
//   rather than a regression.
//
// THE SECOND HALF OF THIS CARD: THE STATE WAS NOT REACHABLE AT ALL
//   A contrast guard for a state nothing can enter is vacuous, and this one
//   could not be written on the tree this card started from. `dragging`
//   (`page.tsx:975`) is `dragType.current !== null || dragIdx.current !== null`
//   evaluated DURING A RENDER, and refs do not schedule a render, so the first
//   `dragover` after `dragstart` ran inside the previous render's closure —
//   where both refs were still null — and returned before `setDropAt`. Measured
//   on a production build with a real held mouse drag over the empty canvas, on
//   the pre-fix tree: 35 `dragover` events delivered to the drop zone, ZERO
//   class mutations on it, and the drop still landed (the drop path reads the
//   refs directly). The highlight never appeared for a user. This branch reads
//   the refs live in `canvasDragOver` instead; the same measurement then shows
//   the tint painted. Both arms below are therefore real: the first drives a
//   genuine held drag, the second the exact `dragstart` + `dragover` shape the
//   card prescribes, and either one goes red if the highlight stops rendering.
//
// WHY THIS IS A BROWSER MEASUREMENT AND NOT A CLASS-NAME ASSERTION
//   A source grep for the class list passes on a tree where the surface under it
//   moved, and cannot measure the 1.2 margin at all. Two things make this guard
//   bite:
//     1. The colours are read from `getComputedStyle` AND resolved by PAINTING
//        them onto a 1x1 canvas and reading the pixels back. Tailwind v4 emits
//        the tint as `oklab(0.961997 0.000716895 -0.0179618 / 0.5)` and the
//        slate border as `lab(...)` — not rgb() — so a regex over the computed
//        string would either miss the colour or hand-roll a colour-space
//        conversion. The renderer does the conversion and the alpha compositing.
//     2. The background is the COMPOSITED one: every ancestor's colour is
//        painted in order, so what is measured is the pixel the user sees rather
//        than the element's own (translucent) background.
//   The AA floor is asserted as an EXACT comparison, and separately against the
//   pre-fix ratio, so a rounded value cannot be argued into a pass.
//
// The API is stubbed at the browser level (no Go server, no session, no DB), so
// this runs in the `e2e` gate (`npm run e2e`, part of scripts/preflight.sh)
// against the build that gate already serves. `/builder?new=1` is the only state
// that renders the drop zone — every `?form=<id>` hydrates at least one block.
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
import { DICT } from "../src/components/i18nDict";

// The drop zone's own sentence, from the app's dictionary, in the DEFAULT
// locale: anchoring on the rendered string rather than on a Tailwind class keeps
// this guard measuring the element the user reads.
const LABEL = DICT["bld.dragHere"].en;

const AA_FLOOR = 4.5;
// What the base --jf-primary (#5e6ad2) measures on this exact composited
// surface. Pinned so the guard states what it guards against, and so a
// re-measure of the pre-fix tree is comparable to a number rather than to a
// memory of one. (The card's arithmetic on #f3f5fc gave 4.309; on the
// composited #f2f5fc the pre-fix build measures 4.306467, pinned here truncated
// as the sibling guards are.)
const PRE_FIX_RATIO = 4.306;
// The composited surface of `bg-indigo-50/50` over --jf-bg. Pinned like the
// sibling price-contrast guard pins slate-50, so a surface change fails with a
// message telling the reader what to re-anchor.
const DRAG_OVER_SURFACE = "#f2f5fc";
const TRANSPARENT = "rgba(0, 0, 0, 0)";
// The dashed border that marks the drag-over state — the non-colour affordance
// alongside the tint, and what distinguishes this element from the idle branch.
const INDIGO_BORDER = "rgb(113, 112, 255)"; // border-[#7170ff]

type Zone = {
  text: string;
  /** The element's OWN background. Transparent in the idle branch. */
  ownBg: string;
  /** The composited background behind the text, resolved from real pixels. */
  bg: string;
  fg: string;
  ratio: number;
  borderColor: string;
  borderStyle: string;
  fontSize: string;
  fontWeight: string;
  inCanvas: boolean;
};

// stubAPI answers every /api/* call the builder makes, so `/builder?new=1`
// reaches its blank-canvas state without a Go API, a session cookie or a
// database. Playwright resolves matching routes in REVERSE registration order,
// so the broad catch-all is registered FIRST and the specific routes after it.
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
  await page.route("**/api/rest/forms**", (route) => route.fulfill(json([])));
}

// measureZone reads the live DOM: it finds the drop zone by the sentence it
// renders, then reports its painted foreground and the COMPOSITED background
// behind it, plus the two attributes that say which branch is on screen.
//
// Everything happens in the page: `getComputedStyle` returns the colour in
// whatever colour space Tailwind emitted (oklab/lab here), so both colours are
// round-tripped through a canvas to get the sRGB bytes the compositor paints,
// with the renderer doing the alpha compositing for the translucent tint.
async function measureZone(page: Page, label: string): Promise<Zone> {
  return page.evaluate((text) => {
    const cv = document.createElement("canvas");
    cv.width = cv.height = 1;
    const ctx = cv.getContext("2d", { willReadFrequently: true })!;
    /** Paint `c` (or nothing) over `base` and return the resulting pixel. */
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

    // Located by the sentence it renders, not by its class list: the whole point
    // of this guard is that the class list can change under the measurement.
    const el = [...document.querySelectorAll("div")].find(
      (d) => (d.textContent || "").trim() === text && d.children.length === 0,
    );
    if (!el) {
      throw new Error(
        `no drop zone rendering "${text}" — it moved or its copy changed. Div texts: ` +
          JSON.stringify(
            [...document.querySelectorAll("div")]
              .map((d) => (d.textContent || "").trim())
              .filter((s) => s.length > 0 && s.length < 60)
              .slice(0, 25),
          ),
      );
    }
    const cs = getComputedStyle(el);

    // The composited background: outermost opaque ancestor first, then each
    // layer painted over it in turn. Transparent layers contribute nothing and
    // are skipped rather than treated as black.
    const layers: string[] = [];
    for (let n: Element | null = el; n; n = n.parentElement) {
      const c = getComputedStyle(n).backgroundColor;
      if (c !== "rgba(0, 0, 0, 0)" && c !== "transparent") layers.push(c);
      if (n === document.body) break;
    }
    let bg = { r: 255, g: 255, b: 255 };
    for (let i = layers.length - 1; i >= 0; i--)
      bg = paint(`rgb(${bg.r}, ${bg.g}, ${bg.b})`, layers[i]);
    const fg = paint(`rgb(${bg.r}, ${bg.g}, ${bg.b})`, cs.color);

    return {
      text: (el.textContent || "").trim(),
      ownBg: cs.backgroundColor,
      bg: hex(bg),
      fg: hex(fg),
      ratio: ratio(fg, bg),
      borderColor: cs.borderTopColor,
      borderStyle: cs.borderTopStyle,
      fontSize: cs.fontSize,
      fontWeight: cs.fontWeight,
      inCanvas: !!el.closest("section"),
    };
  }, label);
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

/** Loads the blank builder and returns when the drop zone is on screen. */
async function openBlankBuilder(page: Page) {
  await freshState(page);
  await stubAPI(page);
  await page.goto("/builder?new=1", { waitUntil: "networkidle" });
  await expect(page.getByText(LABEL, { exact: true })).toBeVisible({ timeout: 45_000 });
  expect(await page.evaluate(() => document.documentElement.lang)).toBe("en");
}

/**
 * Waits until the zone has actually PAINTED the drag-over branch and has
 * finished fading it in, then returns the measurement taken with the pointer
 * still down.
 *
 * The zone's classes include `transition-colors`, so the tint fades in over
 * ~150ms: the first frame that is no longer transparent reads a MID-FADE colour
 * (measured: alpha 0.087 of the 0.5 the state settles at) and none of the
 * assertions below would be measuring the state a user holds. So the wait is
 * "painted AND no transition still running".
 *
 * The wait is also the assertion that the state happens at all: it fails (rather
 * than silently measuring the idle branch) when the highlight stops rendering.
 */
async function measureDragOver(page: Page): Promise<Zone> {
  await expect
    .poll(
      async () =>
        page.evaluate((text) => {
          const el = [...document.querySelectorAll("div")].find(
            (d) => (d.textContent || "").trim() === text && d.children.length === 0,
          ) as HTMLElement | undefined;
          if (!el) return -1;
          if (getComputedStyle(el).backgroundColor === "rgba(0, 0, 0, 0)") return 0;
          return el.getAnimations().some((a) => a.playState === "running") ? 1 : 2;
        }, LABEL),
      {
        timeout: 15_000,
        message:
          "the drop zone never painted its drag-over tint (`bg-indigo-50/50`) while a block was " +
          "dragged over it — the highlight is unreachable, so there is nothing to measure",
      },
    )
    .toBe(2);
  return measureZone(page, LABEL);
}

/** The assertions both arms make, against the measurement with the pointer down. */
function assertDragOver(zone: Zone, arm: string) {
  console.log(
    `[${arm}] drag-over drop zone: fg=${zone.fg} on bg=${zone.bg} — ${zone.ratio}:1 ` +
      `(${zone.fontSize}/${zone.fontWeight}), border=${zone.borderColor} ${zone.borderStyle}, ` +
      `own bg=${zone.ownBg}`,
  );

  // A guard that measures the wrong element, or the wrong branch of it, passes
  // vacuously. The element must be the canvas's dashed drop zone, and it must be
  // showing the drag-over branch — not the idle one this file also measures.
  expect(zone.text).toBe(LABEL);
  expect(zone.inCanvas, "the measured element must be the canvas's own drop zone").toBe(true);
  expect(zone.borderStyle).toBe("dashed");
  expect(
    zone.borderColor,
    "the drag-over branch paints the indigo border; a different border means the measured " +
      "state is not the drag-over one",
  ).toBe(INDIGO_BORDER);

  // The surface is pinned, not the class list: the defect is that this text is
  // painted on a translucent tint. If the surface legitimately moves, the ratio
  // assertion below is what must still hold — re-anchor this expectation to the
  // new surface rather than deleting the guard.
  expect(
    zone.bg,
    `${zone.text} is painted on ${zone.bg}; this guard is anchored to the drag-over tint ` +
      `composited over --jf-bg`,
  ).toBe(DRAG_OVER_SURFACE);
  // The measured foreground is REPORTED, not pinned to a token: any colour that
  // clears the floor on this surface is an acceptable fix (the pre-fix reading
  // below names the one this guard was filed about). Pinning the token here
  // would fail the guard for a legitimate darker replacement, i.e. for a reason
  // that is not the defect.
  expect(zone.fg, "the foreground must not be the surface itself").not.toBe(zone.bg);

  expect(
    zone.ratio,
    `the drag-over drop zone measures ${zone.ratio}:1 (${zone.fg} on ${zone.bg}) — under the ` +
      `4.5:1 AA floor. The base --jf-primary (#5e6ad2) measured ${PRE_FIX_RATIO}:1 here; use ` +
      `--jf-primary-on-tint (#4338ca) instead of darkening the base token, which passes on ` +
      `white (4.700:1) and is used across the app.`,
  ).toBeGreaterThanOrEqual(AA_FLOOR);
  // Whitespace the exact comparison: without this, a rounded reading of the
  // pre-fix 4.306 (or a claim that 4.31 is "close enough") would be arguable.
  expect(
    zone.ratio,
    `the drag-over drop zone measures ${zone.ratio}:1, which is not above the pre-fix ` +
      `${PRE_FIX_RATIO}:1 — the base token is still in place`,
  ).toBeGreaterThan(PRE_FIX_RATIO);
}

test("the builder drop zone's drag-over text clears AA (real held drag)", async ({ page }) => {
  test.setTimeout(120_000);
  await openBlankBuilder(page);

  // The IDLE branch first, so the two states are told apart by measurement and
  // not by assumption: idle carries no background of its own.
  const idle = await measureZone(page, LABEL);
  console.log(
    `[idle] drop zone: fg=${idle.fg} on bg=${idle.bg} — ${idle.ratio}:1, own bg=${idle.ownBg}`,
  );
  expect(idle.ownBg, "the idle drop zone must carry no tint of its own").toBe(TRANSPARENT);

  // A REAL drag gesture: real mouse input, held down over the zone. This is
  // what a user does, and it is the only way to reach a state that `hover`
  // cannot (`border-dashed` has no hover branch at all).
  const palette = page.locator('[draggable="true"]').first();
  const src = await palette.boundingBox();
  const dst = await page.getByText(LABEL, { exact: true }).boundingBox();
  if (!src || !dst) throw new Error("the palette item or the drop zone has no box to drag between");
  const from = { x: src.x + src.width / 2, y: src.y + src.height / 2 };
  const to = { x: dst.x + dst.width / 2, y: dst.y + dst.height / 2 };

  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  for (let i = 1; i <= 10; i++) {
    await page.mouse.move(from.x + ((to.x - from.x) * i) / 10, from.y + ((to.y - from.y) * i) / 10);
    await page.waitForTimeout(40);
  }

  const zone = await measureDragOver(page);
  assertDragOver(zone, "held drag");

  // The gesture was a real one the app accepted: releasing it drops the block,
  // so the canvas stops rendering the empty drop zone.
  await page.mouse.up();
  await expect(page.getByText(LABEL, { exact: true })).toHaveCount(0, { timeout: 15_000 });
});

test("the drag-over branch is reachable from a dragstart + dragover (the card's shape)", async ({
  page,
}) => {
  test.setTimeout(120_000);
  await openBlankBuilder(page);

  // The reproduction the card prescribes, which needs no pointer movement: a
  // real `dragstart` on the first palette item, then a real `dragover` on the
  // dashed zone. This arm is what goes red when the drag-over branch stops
  // rendering at all (the pre-fix `dragging` snapshot did exactly that), so the
  // file guards reachability and contrast separately rather than assuming one
  // implies the other.
  await page.locator('[draggable="true"]').first().evaluate((el) => {
    el.dispatchEvent(
      new DragEvent("dragstart", { bubbles: true, cancelable: true, dataTransfer: new DataTransfer() }),
    );
  });
  await page.getByText(LABEL, { exact: true }).evaluate((el) => {
    el.dispatchEvent(
      new DragEvent("dragover", { bubbles: true, cancelable: true, dataTransfer: new DataTransfer() }),
    );
  });

  const zone = await measureDragOver(page);
  assertDragOver(zone, "dragstart+dragover");
});
