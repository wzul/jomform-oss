// Regression guard: a FAILED LOAD must be LEGIBLE. The sentence that says "this
// page could not load" must clear WCAG AA on the surface it is actually painted
// on, in both places that render one outside a card:
//
//   /form-settings   the `err` banner, painted directly on the page background
//                    (`globals.css`: `html, body { background: var(--jf-bg) }`)
//   /builder         the `loadErr` banner, inside `<main class="… bg-[var(--jf-bg)]">`
//
// THE DEFECT (card t_5c17adc9, measured — not a class-name reading)
//   Both banners were `text-red-400`, i.e. Tailwind v4's `#ff6467`:
//
//     text-red-400  #ff6467   --jf-bg #f7f8f8 = 2.71:1   white = 2.89:1
//     text-red-500  #fb2c36   --jf-bg         3.58:1      white = 3.81:1
//     text-red-600  #e7000b   --jf-bg         4.48:1      white = 4.77:1  (under AA on --jf-bg)
//     text-red-700  #c10007   --jf-bg         6.03:1      white = 6.42:1  PASSES
//
//   2.71:1 is the least legible ink the app can paint, on the one sentence whose
//   entire job is to tell the merchant that nothing loaded. `web/e2e/a11y.spec.ts`
//   scans PUBLIC routes only, so axe has never rendered either page, and no
//   source-level rule covered the red/sky family at all (that gap is closed by
//   `src/lib/dangerText.test.ts`, which this file is the rendered half of).
//
// WHY THIS IS A BROWSER MEASUREMENT AND NOT A CLASS-NAME ASSERTION
//   The class list is not the defect — the RATIO is, and the ratio depends on the
//   composited background behind the element. A grep for `text-red-700` would pass
//   on a tree where the banner moved onto a tint it cannot clear, and it cannot
//   measure the 4.48-vs-4.5 margin that made `text-red-600` wrong here. So the
//   colours are read from `getComputedStyle` AND resolved by PAINTING them onto a
//   1x1 canvas and reading the pixels back, with the renderer doing the
//   colour-space conversion (Tailwind v4 emits oklch/lab, not rgb()) and the alpha
//   compositing for every ancestor layer. The floor is asserted as an exact
//   comparison, so rounding 4.48 to 4.5 cannot be argued from these numbers.
//
// HOW THE PAGES ARE REACHED WITHOUT A SESSION OR A BACKEND. Both pages render
//   from `dashboard-bundle` + their own REST reads, so stubbing them at the
//   browser level (the pattern `e2e/form-settings-tabstrip-contrast.spec.ts` and
//   `e2e/action-failure-tone.spec.ts` use) puts the real component in a real
//   browser with no Go server, no session cookie and no credential. The FAILURE is
//   forced by making the specific load route answer 500 — the banner then renders
//   from the ordinary error path, not from a fixture.
//
// Run with (it is a `feature-e2e` spec, so the `e2e` preflight gate runs it):
//   cd web && npm run e2e
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

// The AA floor for normal-size text, and the value these banners measured BEFORE
// the fix. Both pinned so the guard states what it is guarding against, and a
// re-measure of the pre-fix tree is comparable to a number rather than a memory.
const AA_FLOOR = 4.5;
const PRE_FIX_RED_400 = 2.71;
const RED_700 = "#c10007"; // text-red-700 — the token both banners now use

const FORM_ID = "form_loadfail_guard";

type Ink = {
  /** The failure sentence the page actually rendered. */
  text: string;
  fg: string;
  bg: string;
  ratio: number;
  fontSize: string;
  fontWeight: string;
};

/**
 * The measurement, taken in the page. `selector` finds the banner; the
 * composited background is every opaque ancestor layer painted in order, and both
 * colours are round-tripped through a 1x1 canvas so the renderer resolves
 * oklch/lab and does the alpha compositing.
 */
async function measure(page: Page, text: string): Promise<Ink> {
  return page.evaluate((needle) => {
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
    const lin = (v: number) => (v / 255 <= 0.04045 ? v / 255 / 12.92 : ((v / 255 + 0.055) / 1.055) ** 2.4);
    const lum = (c: { r: number; g: number; b: number }) =>
      0.2126 * lin(c.r) + 0.7152 * lin(c.g) + 0.0722 * lin(c.b);
    const ratio = (a: { r: number; g: number; b: number }, b: { r: number; g: number; b: number }) => {
      const [x, y] = [lum(a), lum(b)];
      return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
    };

    // The element that carries EXACTLY the sentence — not an ancestor that
    // contains it as one of many children.
    const el = [...document.querySelectorAll("p")].find(
      (n) => (n.textContent || "").trim() === needle.trim(),
    ) as HTMLElement | undefined;
    if (!el) {
      throw new Error(
        `no <p> reading ${JSON.stringify(needle)} — the failure banner did not render. ` +
          `Paragraphs on screen: ${JSON.stringify(
            [...document.querySelectorAll("p")].map((p) => (p.textContent || "").trim().slice(0, 60)).filter(Boolean),
          )}`,
      );
    }

    const layers: string[] = [];
    for (let n: Element | null = el; n; n = n.parentElement) {
      const c = getComputedStyle(n).backgroundColor;
      if (c !== "rgba(0, 0, 0, 0)" && c !== "transparent") layers.push(c);
      if (n === document.body) break;
    }
    let bg = { r: 255, g: 255, b: 255 };
    for (let i = layers.length - 1; i >= 0; i--) bg = paint(`rgb(${bg.r}, ${bg.g}, ${bg.b})`, layers[i]);
    const cs = getComputedStyle(el);
    const fg = paint(`rgb(${bg.r}, ${bg.g}, ${bg.b})`, cs.color);

    return {
      text: (el.textContent || "").trim(),
      fg: hex(fg),
      bg: hex(bg),
      ratio: Math.round(ratio(fg, bg) * 1000) / 1000,
      fontSize: cs.fontSize,
      fontWeight: cs.fontWeight,
    };
  }, text);
}

/**
 * A FRESH browser state, English — the default a new visitor gets. Storage is
 * cleared first: this browser keeps localStorage between runs on this host, and a
 * `jf_lang` left behind by an earlier card would render the page in a locale this
 * file never asserts.
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

/**
 * The banner must be ON SCREEN before it is measured, and this is the whole
 * difference between a guard and a flake (card t_12d4f812, measured).
 *
 * `measure()` runs immediately after `goto(..., { waitUntil: "networkidle" })`.
 * networkidle says only that the NETWORK went quiet for 500ms — Playwright
 * documents it as DISCOURAGED for readiness for this exact reason ("rely on web
 * assertions to assess readiness instead") — and on a contended host the CLIENT
 * work that paints this banner is starved well past that moment. Measured on
 * 6 cores with 6 concurrent runners + 6 CPU spinners, pairing each round's
 * networkidle timestamp against the moment the banner actually appeared, over
 * 24 rounds: the banner was up when networkidle resolved in 15, and in 6 it
 * arrived 0.9s-48.9s AFTER it. Those are exactly the `measure()` presence
 * throws ("no <p> reading …") with the page still on `Loading…` or its bare
 * shell — the shape this card reports (11/36 runs at that contention, and 0 of
 * them a colour mismatch: the ratio assertions never got to run).
 *
 * So wait for the banner, THEN measure. The bite is preserved, and deliberately:
 * the wait is BOUNDED, so a banner that genuinely never renders still reds the
 * spec — as this timeout, which names the sentence and the page state, instead
 * of as a presence error inside `measure()`. `measure()` keeps its own positive
 * assertion; it is what locates the exact element and then checks the colour and
 * the ratio on the pixel it is painted on.
 *
 * 45s is not invented: it is the page-ready budget every sibling spec that waits
 * on these two pages already uses (`form-settings-tabstrip-contrast`,
 * `form-settings-status-chips`, `form-settings-header-actions`,
 * `builder-publish-panel` — all `toBeVisible({ timeout: 45_000 })`). The worst
 * banner latency measured in the real gate is ~15s, so this is ~3x headroom,
 * and it sits well inside `test.setTimeout(120_000)`.
 */
async function waitForBanner(page: Page, text: string) {
  try {
    await page.locator(`p:text-is(${JSON.stringify(text)})`).first().waitFor({ state: "visible", timeout: 45_000 });
  } catch {
    // A bounded wait is only as good as what it reports when it expires, so name
    // the page and WHICH way it failed: the sentence absent (the error state was
    // never reached) versus present but not visible (painted off-screen or
    // hidden). Those are different defects and the bare timeout cannot tell them
    // apart.
    const where = page.url();
    const seen = await page
      .evaluate(
        (needle) =>
          [...document.querySelectorAll("p")]
            .filter((p) => (p.textContent || "").trim() === needle)
            .map((p) => getComputedStyle(p as HTMLElement).display),
        text,
      )
      .catch(() => null);
    throw new Error(
      `the load-failure banner ${JSON.stringify(text)} was not visible within 45s (page at ${where}): ` +
        (seen === null
          ? "the page is gone"
          : seen.length === 0
            ? "the error state was never reached — no <p> carries that sentence"
            : `a matching <p> exists but is not visible (display: ${JSON.stringify(seen)})`),
    );
  }
}

const json = (body: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify(body),
});

/** The bundle every authenticated page needs to stop bouncing to /login. */
async function stubBundle(page: Page) {
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (route) =>
    route.fulfill(
      json({
        me: { id: "u1", email: "merchant@example.com", name: "Merchant" },
        workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
      }),
    ),
  );
  await page.route(/(.*)\/api\/auth\/profile$/, (route) => route.fulfill(json({ lang: "" })));
  // The catch-all is registered BEFORE the specific routes below: Playwright
  // resolves matching routes in reverse registration order, so the later
  // (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
}

test("/form-settings: a failed LOAD is legible on the page it is painted on", async ({ page }) => {
  test.setTimeout(120_000);
  await freshState(page);
  await stubBundle(page);
  // The form resolve probes the active workspace first. A 500 there is NOT this
  // banner — the page classifies a non-404/non-401 failure and falls through — so
  // the failure this card is about is the THROW path: `resolveFormAcrossWorkspaces`
  // rejecting (a transport failure) is what lands in the `catch` and sets `err` to
  // `fs.loadErr`, "Failed to load form settings.". Abort the request so `fetch`
  // really rejects; a fulfilled 500 would exercise a different branch.
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+(\?.*)?$/, (route) => route.abort("failed"));
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (route) => route.fulfill(json([])));

  await page.goto(`/form-settings?form=${FORM_ID}`, { waitUntil: "networkidle" });

  await waitForBanner(page, "Failed to load form settings.");
  const banner = await measure(page, "Failed to load form settings.");
  console.log(
    `/form-settings load-failure banner: fg=${banner.fg} on bg=${banner.bg} — ${banner.ratio}:1 ` +
      `(${banner.fontSize}/${banner.fontWeight})`,
  );

  // The surface is pinned, not the class list: the defect is that this sentence is
  // painted on the PAGE background. If the page legitimately gains a surface, the
  // ratio assertion below is what must still hold — re-anchor this expectation.
  expect(banner.bg, `the failure banner is painted on ${banner.bg}; this guard is anchored to --jf-bg`).toBe("#f7f8f8");
  expect(banner.fg, "the failure ink is not the AA-safe red-700").toBe(RED_700);
  expect(
    banner.ratio,
    `the /form-settings load-failure banner measures ${banner.ratio}:1 (${banner.fg} on ${banner.bg}) — ` +
      `under the ${AA_FLOOR}:1 AA floor. text-red-400 (#ff6467) measured ${PRE_FIX_RED_400}:1 here; ` +
      `use text-red-700 (#c10007, 6.03:1 on this surface).`,
  ).toBeGreaterThanOrEqual(AA_FLOOR);
});

test("/builder: a failed LOAD is legible on the surface it is painted on", async ({ page }) => {
  test.setTimeout(120_000);
  await freshState(page);
  await stubBundle(page);
  // The builder's own load is the PRODUCTS read (`load()`), and its failure is
  // what sets `loadErr`. Force that one route to 500 and leave the rest empty.
  await page.route(/(.*)\/api\/rest\/products(\?.*)?$/, (route) =>
    route.fulfill({ status: 500, contentType: "application/json", body: JSON.stringify({ error: "boom" }) }),
  );
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (route) => route.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/templates(\?.*)?$/, (route) => route.fulfill(json([])));

  // A bare /builder with no ?form= opens the template picker; closing it is not
  // needed — the canvas and its error line are behind the picker's overlay, so
  // take the NEW path, which renders the canvas immediately.
  await page.goto("/builder?new=1", { waitUntil: "networkidle" });

  await waitForBanner(page, "We couldn't load your products. Please try again.");
  const banner = await measure(page, "We couldn't load your products. Please try again.");
  console.log(
    `/builder load-failure banner: fg=${banner.fg} on bg=${banner.bg} — ${banner.ratio}:1 ` +
      `(${banner.fontSize}/${banner.fontWeight})`,
  );

  expect(banner.bg, `the failure banner is painted on ${banner.bg}; this guard is anchored to --jf-bg`).toBe("#f7f8f8");
  expect(banner.fg, "the failure ink is not the AA-safe red-700").toBe(RED_700);
  expect(
    banner.ratio,
    `the /builder load-failure banner measures ${banner.ratio}:1 (${banner.fg} on ${banner.bg}) — ` +
      `under the ${AA_FLOOR}:1 AA floor. text-red-400 (#ff6467) measured ${PRE_FIX_RED_400}:1 here; ` +
      `use text-red-700 (#c10007, 6.03:1 on this surface).`,
  ).toBeGreaterThanOrEqual(AA_FLOOR);
});
