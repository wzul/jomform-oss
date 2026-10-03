// Regression guard: the BUILDER CANVAS price spans must clear WCAG AA on the
// surface they are actually painted on.
//
// THE DEFECT (measured, then fixed on this branch)
//   The product block's price and the `total` block's amount were both
//   `text-[var(--jf-primary)]`, i.e. #5e6ad2. Both sit on Tailwind's slate-50
//   (#f8fafc): the product block's card is `bg-slate-50` and the total row
//   carries `bg-slate-50` itself. Measured on a production build in a real
//   browser:
//
//     --jf-primary #5e6ad2 on slate-50  #f8fafc = 4.492:1  <- 0.008 UNDER the floor
//     --jf-primary #5e6ad2 on white     #ffffff = 4.700:1  <- passes
//
//   That gap is the whole trap: the token was fine everywhere it had been
//   checked, and the same class list reads as green on white. Axe reported it as
//   `color-contrast (serious)`, "insufficient color contrast of 4.49", on both
//   nodes; they are byte-identical on origin/main, so this is a pre-existing
//   defect the panel-scoped axe work never looked at.
//
// THE FIX
//   Both spans use the existing on-tint token `--jf-primary-on-tint` (#4338ca),
//   which measures 7.554:1 on slate-50 and 7.903:1 on white — so it is safe
//   unconditionally, and the token's own comment already says a site whose
//   surface is a tint should use it. `--jf-primary` itself is NOT darkened:
//   that was rejected as a decision because it shifts every other use of the
//   token in the app.
//
// WHY THIS IS A BROWSER MEASUREMENT AND NOT A CLASS-NAME ASSERTION
//   A source grep for the class list would pass on a tree where the surface
//   changed under it, and — worse — it cannot measure the 0.008 margin at all.
//   Two things make this guard bite:
//
//     1. The colours are read from `getComputedStyle` AND resolved by PAINTING
//        them onto a 1x1 canvas and reading the pixels back. Tailwind v4 emits
//        `bg-slate-50` as `lab(98.1434 -0.369519 -1.05966)` here, not an rgb()
//        or hex value, so a regex over the computed string would either miss the
//        colour or hand-roll a Lab conversion. The canvas does the conversion
//        and the alpha compositing with the renderer itself.
//     2. The background is the COMPOSITED one: every ancestor's colour is
//        painted in order, so the value measured is the pixels the user sees and
//        not the element's own (often transparent) background.
//
//   The assertions below therefore measure the ratio. Verified to bite: on the
//   pre-fix tree (origin/main, d0eef02) both nodes measure 4.492:1 and this file
//   is RED for exactly those two nodes; after the fix they measure 7.554:1 and
//   it is green.
//
//   The ratio floor is asserted as an exact comparison, not a rounded one: the
//   failure is 0.008 below the floor, so rounding to one decimal — or measuring
//   on #ffffff — would report this defect as a pass.
//
// The API is stubbed at the browser level (no Go server, no session, no DB), so
// this runs in the axe gate (`npm run a11y`, part of scripts/preflight.sh)
// against the build that gate already serves. It lives in its own file because
// `a11y.spec.ts` scans unauthenticated PUBLIC routes and cannot reach the
// builder at all.
import { test, expect, type Page } from "@playwright/test";

const FORM_ID = "form_canvas_price";

// The two surfaces the card names, and the token values they must clear.
const AA_FLOOR = 4.5;
// The card's own computed value for #5e6ad2 on #f8fafc. Pinned so the guard
// states what it is guarding against, and so a re-measure of the pre-fix tree is
// comparable to this number rather than to a memory of it.
const PRE_FIX_RATIO = 4.492;

type Price = {
  text: string;
  fg: string;
  bg: string;
  ratio: number;
  fontSize: string;
  fontWeight: string;
};

// stubAPI answers every /api/* call the builder makes, so `/builder?form=<id>`
// reaches its loaded state without a Go API, a session cookie or a database.
// Playwright resolves matching routes in REVERSE registration order (the last
// registered wins), so the broad `forms` list route is registered BEFORE the
// specific `forms/<id>` one — otherwise it shadows the form and the canvas
// renders the template picker instead.
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
  await page.route("**/api/rest/products**", (route) =>
    route.fulfill(json([{ id: "prod_1", name: "Widget", price_sen: 1234, active: true }])),
  );
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
          // Exactly the two block types this guard is about. The product block
          // points at a real product so the price renders as a formatted amount
          // ("RM12.34") rather than the "—" fallback.
          blocks: [
            { type: "product", value: "Widget", product_id: "prod_1", quantity: true },
            { type: "total", value: "Total" },
          ],
        },
      }),
    ),
  );
}

// measurePrices reads the live DOM: for every element whose text is a formatted
// ringgit amount inside the canvas it takes the painted foreground and the
// COMPOSITED background, and returns the WCAG contrast ratio between them.
//
// Everything happens in the page: getComputedStyle returns the colour in
// whatever colour space Tailwind emitted (oklch/lab for the slate ramp), so both
// colours are round-tripped through a canvas to get the sRGB bytes the
// compositor paints, with the renderer doing the alpha compositing for the
// translucent layers.
async function measurePrices(page: Page): Promise<Price[]> {
  return page.evaluate(() => {
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
    const lin = (v: number) => (v / 255 <= 0.04045 ? v / 255 / 12.92 : ((v / 255 + 0.055) / 1.055) ** 2.4);
    const lum = (c: { r: number; g: number; b: number }) =>
      0.2126 * lin(c.r) + 0.7152 * lin(c.g) + 0.0722 * lin(c.b);
    const ratio = (a: { r: number; g: number; b: number }, b: { r: number; g: number; b: number }) => {
      const [x, y] = [lum(a), lum(b)];
      return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
    };

    const canvas = document.querySelector("section");
    if (!canvas) throw new Error("no <section> on /builder — the canvas moved");

    const out: Price[] = [];
    for (const el of canvas.querySelectorAll("span, p, div, td, th, strong, em, code, b, i")) {
      const text = (el.textContent || "").trim();
      // A leaf whose whole content is one formatted ringgit amount. The
      // `children.length` guard keeps a card wrapper that merely CONTAINS a
      // price from being measured as one.
      if (!/^RM[0-9,]+\.[0-9]{2}$/.test(text) || el.children.length > 0) continue;
      const cs = getComputedStyle(el);
      if (cs.display === "none" || cs.visibility === "hidden") continue;

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
      for (let i = layers.length - 1; i >= 0; i--) bg = paint(`rgb(${bg.r}, ${bg.g}, ${bg.b})`, layers[i]);
      const fg = paint(`rgb(${bg.r}, ${bg.g}, ${bg.b})`, cs.color);

      out.push({
        text,
        fg: hex(fg),
        bg: hex(bg),
        // THE FULL VALUE, NOT A ROUNDED ONE (card t_e0784043). Rounding to 3
        // decimals here made the guard's effective AA floor 4.4995 instead of 4.5: a
        // colour measuring in [4.4995, 4.5) rounds UP to exactly 4.5 and passed the
        // floor assertion below. Enumerating the trap box (949 colours that pass on
        // white and fail on slate-50), two land in that sliver — #6268d5 (4.50000 on
        // #f8fafc) and #6269d1 (4.49965) — and both slipped through. The renderer is
        // still the one doing the colour-space conversion, so the value is exact, not a
        // hand-rolled approximation.
        ratio: ratio(fg, bg),
        fontSize: cs.fontSize,
        fontWeight: cs.fontWeight,
      });
    }
    return out;
  });
}

test("the canvas price spans clear AA on the surface they are painted on", async ({ page }) => {
  test.setTimeout(120_000);

  // Fresh storage: the browser keeps localStorage and cookies between runs on
  // this host, and a workspace/language left behind by another card would render
  // a state no new visitor gets. The context is new, so this is belt-and-braces
  // on top of Playwright's isolation.
  await page.addInitScript(() => {
    localStorage.clear();
    sessionStorage.clear();
    localStorage.setItem("jf_workspace", "demo");
    localStorage.setItem("jf_lang", "en");
  });

  await stubAPI(page);
  await page.goto(`/builder?form=${FORM_ID}`, { waitUntil: "networkidle" });

  // Both blocks must actually have rendered, or the measurements below are of an
  // empty canvas: the product price needs its product to resolve, and the total
  // row is a fixed RM0.00.
  await expect(page.locator("text=RM12.34").first()).toBeVisible({ timeout: 45_000 });
  await expect(page.locator("text=RM0.00").first()).toBeVisible({ timeout: 45_000 });

  const prices = await measurePrices(page);
  const report = prices
    .map((p) => `${p.text}: fg=${p.fg} on bg=${p.bg} — ${p.ratio}:1 (${p.fontSize}/${p.fontWeight})`)
    .join("\n  ");
  // The numbers are the acceptance evidence for this card, not just pass/fail.
  console.log(`canvas price spans:\n  ${report}`);

  // A guard that finds nothing passes vacuously. Both nodes are named by the
  // card, so both must be present and measured.
  expect(prices.length, `expected 2 measured price spans, found ${prices.length}`).toBe(2);
  expect(prices.map((p) => p.text).sort()).toEqual(["RM0.00", "RM12.34"]);

  for (const p of prices) {
    // The surface is pinned, not the class list: the defect is that the span is
    // painted on a slate-50 card. If the surface legitimately moves, the ratio
    // assertion below is what must still hold — re-anchor this expectation to
    // the new surface rather than deleting the guard.
    expect(
      p.bg,
      `${p.text} is painted on ${p.bg}; this guard is anchored to the canvas card's slate-50 surface`,
    ).toBe("#f8fafc");
    expect(
      p.ratio,
      `${p.text} measures ${p.ratio}:1 (${p.fg} on ${p.bg}) — under the 4.5:1 AA floor. ` +
        `The base --jf-primary (#5e6ad2) measured ${PRE_FIX_RATIO}:1 here; use ` +
        `--jf-primary-on-tint (#4338ca, 7.554:1 on this surface) instead of darkening the ` +
        `base token, which passes on white (4.700:1) and is used across the app.`,
    ).toBeGreaterThanOrEqual(AA_FLOOR);
    // The FLOOR is the one that must not be rounded, which is why the measured value is
    // no longer rounded at all: `>= 4.5` on a number that is the real ratio cannot be
    // satisfied by a colour in [4.4995, 4.5). The sliver is named rather than assumed —
    // these two colours sit in it and MUST fail here (card t_e0784043).
    expect(p.ratio).toBeGreaterThan(PRE_FIX_RATIO);
  }
});
