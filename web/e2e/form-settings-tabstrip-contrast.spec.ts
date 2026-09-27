// Regression guard: the /form-settings tab strip's ACTIVE tab label must clear
// WCAG AA on the surface the strip is actually painted on.
//
// THE DEFECT (measured by axe on a production build, not a source grep)
//   The active tab was `text-[var(--jf-primary)]` — #5e6ad2 — and its <nav>
//   declares no background, so the nearest painting ancestor is the PAGE
//   background (`web/src/app/globals.css`: `html, body { background: var(--jf-bg) }`,
//   #f7f8f8). Measured in a real browser:
//
//     --jf-primary #5e6ad2 on --jf-bg #f7f8f8 = 4.417:1   <- below the 4.5 floor
//     --jf-primary #5e6ad2 on white     #ffffff = 4.700:1   <- passes
//
//   That gap is the whole trap, and it is why this is a PER-SITE guard rather
//   than a blanket token swap: the byte-identical active-tab class string also
//   appears at `web/src/app/builder/page.tsx:1559`, but THAT strip's <nav>
//   declares `bg-white` (`builder/page.tsx:1546`), so it reads 4.700:1 and is
//   fine. Only the /form-settings strip sits directly on --jf-bg.
//
//   axe reported `color-contrast (serious)`, "insufficient color contrast of
//   4.41 (foreground color: #5e6ad2, background color: #f7f8f8, font size:
//   10.5pt (14px), font weight: normal)", on the `.border-b-2` node.
//
// THE FIX
//   The active label uses the existing on-tint token `--jf-primary-on-tint`
//   (#4338ca), which measures 7.428:1 on --jf-bg (and 7.903:1 on white), so it
//   is safe on either surface this app paints it on. `--jf-primary` itself is
//   NOT darkened: it passes on white at 4.700:1 and is used ~100 times across
//   the app, so changing the base token shifts every one of those.
//
// WHY THIS IS A BROWSER MEASUREMENT AND NOT A CLASS-NAME ASSERTION
//   A source grep for the class list would pass on a tree where the surface
//   moved under it (the nav gains a `bg-white` and the class is fine again, or
//   the page background changes to a tint) and it cannot measure the 0.4 margin
//   at all. Two things make this guard bite:
//
//     1. The colours are read from `getComputedStyle` AND resolved by PAINTING
//        them onto a 1x1 canvas and reading the pixels back, with the renderer
//        doing the colour-space conversion and the alpha compositing. Tailwind
//        v4 does not emit these as rgb(): a regex over the computed string
//        would either miss the value or hand-roll a Lab conversion.
//     2. The background is the COMPOSITED one — every ancestor's colour painted
//        in order — so what is measured is the pixel the user sees, not the
//        element's own (transparent) background.
//
//   The ratio floor is asserted as an EXACT comparison against BOTH 4.5 and the
//   pre-fix 4.417, so rounding to one decimal (4.41 -> 4.4) cannot be used to
//   argue the defect is a pass. The axe arm additionally asserts the
//   `color-contrast` rule actually RAN over the strip, so a scan that analysed
//   nothing cannot read as a clean pass.
//
//   Verified to bite: with the pre-fix token restored on this tree, this file
//   is RED for the active tab at 4.417:1; with the fix it is green.
//
// The API is stubbed at the browser level (no Go server, no session, no DB), so
// this runs in the axe gate (`npm run a11y`, part of scripts/preflight.sh)
// against the build that gate already serves. It lives in its own file because
// `a11y.spec.ts` scans unauthenticated PUBLIC routes and cannot reach
// /form-settings at all.
import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import type { AxeResults } from "axe-core";
import { DICT } from "../src/components/i18nDict";

const FORM_ID = "form_tabstrip_guard";

// The AA floor, and the value this strip measured BEFORE the fix. Both are
// pinned so the guard states what it is guarding against and a re-measure of
// the pre-fix tree is comparable to a number rather than to a memory of one.
const AA_FLOOR = 4.5;
const PRE_FIX_RATIO = 4.417;

// The indigo underline that makes the active tab readable as active. It is the
// non-colour affordance, so it must survive the token change.
const UNDERLINE = "rgb(113, 112, 255)"; // border-[#7170ff]

type Strip = {
  labels: string[];
  activeIndex: number;
  fg: string;
  bg: string;
  ratio: number;
  fontSize: string;
  fontWeight: string;
  underline: string;
  /** A CSS path to the strip element, so axe can be scoped to it exactly. */
  selector: string;
};

/**
 * A deterministic CSS path to `el`, built from what is actually rendered.
 *
 * `AxeBuilder().include()` needs a selector, and scoping the axe arm to THIS
 * strip (rather than to every `nav` landmark on the page) is what keeps the two
 * arms measuring the same element — a page-wide `nav` include would let a
 * violation elsewhere be reported here, and the settings sidebar's own `nav`
 * would be dragged in too.
 */

// stubAPI answers every /api/* call the page makes, so `/form-settings?form=<id>`
// reaches its loaded state without a Go API, a session cookie or a database.
// Registered as a catch-all FIRST and the specific routes after it: Playwright
// resolves matching routes in reverse registration order, so the later
// (specific) ones win.
async function stubAPI(page: Page) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (route) =>
    route.fulfill(
      json({
        me: { id: "u1", email: "merchant@example.com", name: "Merchant" },
        workspaces: [{ id: "ws1", name: "Demo", slug: "demo", role: "admin" }],
      }),
    ),
  );
  // lang:"" (never set) keeps the browser value in effect, which is how the
  // init script below pins the locale to en.
  await page.route(/(.*)\/api\/auth\/profile$/, (route) => route.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+$/, (route) =>
    route.fulfill(
      json({
        id: FORM_ID,
        slug: "demo-form",
        title: "Demo form",
        status: "published",
        url: "https://jomform.example/f/demo-form",
        mode: "payment",
        config: { blocks: [] },
        theme: {},
      }),
    ),
  );
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/chip-feeds(\?.*)?$/, (route) =>
    route.fulfill(json({ feeds: null })),
  );
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/confirmations(\?.*)?$/, (route) =>
    route.fulfill(json({ confirmations: [] })),
  );
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/notifications(\?.*)?$/, (route) =>
    route.fulfill(json([])),
  );
  await page.route(/(.*)\/api\/rest\/forms\/[^/]+\/menu(\?.*)?$/, (route) =>
    route.fulfill(json({ enabled: false, categories: [] })),
  );
  await page.route(/(.*)\/api\/rest\/webhooks(\?.*)?$/, (route) => route.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/products(\?.*)?$/, (route) => route.fulfill(json([])));
  await page.route(/(.*)\/api\/rest\/forms(\?.*)?$/, (route) => route.fulfill(json([])));
}

/**
 * A FRESH browser state, English. Storage is cleared first: the browser keeps
 * localStorage between runs on this host, and a `jf_lang` left behind by an
 * earlier card would render this page in a locale this file never asserts (the
 * app defaults to English for a new visitor, which is what the pinned label
 * below checks).
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
 * measureActiveTab reads the live DOM: it finds the tab strip, reports the
 * ACTIVE tab's painted foreground and the COMPOSITED background behind it, and
 * returns the WCAG contrast ratio between them.
 *
 * Everything happens in the page: `getComputedStyle` returns the colour in
 * whatever colour space Tailwind emitted, so both colours are round-tripped
 * through a canvas to get the sRGB bytes the compositor paints, with the
 * renderer doing the alpha compositing for any translucent layer.
 */
async function measureActiveTab(page: Page, activeLabel: string): Promise<Strip> {
  return page.evaluate((label) => {
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
    /** A unique CSS path, so the axe arm can be scoped to this exact strip. */
    const cssPath = (el: Element) => {
      const parts: string[] = [];
      for (let n: Element | null = el; n && n.tagName !== "BODY"; n = n.parentElement) {
        let sel = n.tagName.toLowerCase();
        const sibs = [...(n.parentElement?.children ?? [])].filter((c) => c.tagName === n!.tagName);
        if (sibs.length > 1) sel += `:nth-of-type(${sibs.indexOf(n) + 1})`;
        parts.unshift(sel);
      }
      return parts.join(" > ");
    };

    // Located by LABEL, then by the button's own parent: the strip is the one
    // element this guard owns, so anchoring on the tab rather than on the page's
    // `nav` landmarks stays correct whatever else those contain.
    const all = [...document.querySelectorAll("button")] as HTMLButtonElement[];
    const el = all.find((b) => (b.textContent || "").trim() === label);
    if (!el) {
      throw new Error(
        `no button reading "${label}" — the strip moved. Buttons: ${JSON.stringify(
          all.map((b) => (b.textContent || "").trim()).filter((t) => t.length > 0 && t.length < 30),
        )}`,
      );
    }
    const strip = el.parentElement as HTMLElement;
    const buttons = [...strip.querySelectorAll("button")] as HTMLButtonElement[];
    const activeIndex = buttons.indexOf(el);
    const cs = getComputedStyle(el);

    // The composited background: outermost opaque ancestor first, then each
    // layer painted over it. Transparent layers contribute nothing and are
    // skipped rather than treated as black.
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
      labels: buttons.map((b) => (b.textContent || "").trim()),
      activeIndex,
      fg: hex(fg),
      bg: hex(bg),
      ratio: Math.round(ratio(fg, bg) * 1000) / 1000,
      fontSize: cs.fontSize,
      fontWeight: cs.fontWeight,
      underline: cs.borderBottomColor,
      selector: cssPath(strip),
    };
  }, activeLabel);
}

/** Scans the tab strip's own subtree with the repo's axe config. */
async function scanStrip(page: Page, selector: string): Promise<AxeResults> {
  return new AxeBuilder({ page }).include(selector).analyze();
}


test("the /form-settings active tab clears AA on the surface it is painted on", async ({
  page,
}) => {
  test.setTimeout(120_000);
  await freshState(page);
  await stubAPI(page);
  await page.goto(`/form-settings?form=${FORM_ID}`, { waitUntil: "networkidle" });

  // The strip must actually have rendered, and in the DEFAULT locale, or the
  // measurement below is of something else. A tab label from the app's own
  // dictionary is the anchor: "General" is the first tab and the page's
  // default selection.
  const general = DICT["fs.general"].en;
  const notifications = DICT["fs.notifications"].en;
  await expect(page.locator(`nav button:text-is("${general}")`).first()).toBeVisible({
    timeout: 45_000,
  });
  expect(await page.evaluate(() => document.documentElement.lang)).toBe("en");

  // A real click, so the ACTIVE branch is the one under measurement. Clicking
  // also proves the strip is reachable/functional rather than inert markup.
  await page.locator(`nav button:text-is("${notifications}")`).first().click();

  const strip = await measureActiveTab(page, notifications);
  // The numbers are the acceptance evidence for this card, not just pass/fail.
  console.log(
    `active tab "${strip.labels[strip.activeIndex]}" (index ${strip.activeIndex}): ` +
      `fg=${strip.fg} on bg=${strip.bg} — ${strip.ratio}:1 ` +
      `(${strip.fontSize}/${strip.fontWeight}), underline=${strip.underline}`,
  );

  // A guard that measures the wrong element passes vacuously. The label the
  // card names must be the ACTIVE one, and the strip must render all of its
  // tabs — a strip that lost its tabs would otherwise be a clean reading of
  // nothing.
  expect(strip.labels.length, `the strip rendered ${strip.labels.length} tabs`).toBeGreaterThanOrEqual(
    6,
  );
  expect(strip.labels[strip.activeIndex]).toBe(notifications);

  // The surface is pinned, not the class list: the defect is that this label is
  // painted on the PAGE background, not on a card. If the strip legitimately
  // gains a surface, the ratio assertion below is what must still hold —
  // re-anchor this expectation to the new surface rather than deleting the guard.
  expect(
    strip.bg,
    `the active tab is painted on ${strip.bg}; this guard is anchored to the app's page background`,
  ).toBe("#f7f8f8");

  expect(
    strip.ratio,
    `the active tab measures ${strip.ratio}:1 (${strip.fg} on ${strip.bg}) — under the 4.5:1 AA ` +
      `floor. Base --jf-primary (#5e6ad2) measured ${PRE_FIX_RATIO}:1 here; use ` +
      `--jf-primary-on-tint (#4338ca, 7.428:1 on this surface) rather than darkening the base ` +
      `token, which passes on white (4.700:1) and is used across the app.`,
  ).toBeGreaterThanOrEqual(AA_FLOOR);
  // Whitespace the exact comparison: a rounded ratio (4.417 -> 4.4, or a claim
  // that 4.41 is "close enough") would otherwise be arguable from these numbers.
  expect(
    strip.ratio,
    `the active tab measures ${strip.ratio}:1, which is not above the pre-fix ${PRE_FIX_RATIO}:1 — ` +
      `the base token is still in place`,
  ).toBeGreaterThan(PRE_FIX_RATIO);
  // The non-colour affordance that marks the tab active, unchanged.
  expect(strip.underline, "the active tab must keep its indigo underline").toBe(UNDERLINE);
});

test("the /form-settings tab strip has no color-contrast violation (axe, scoped)", async ({
  page,
}) => {
  test.setTimeout(120_000);
  await freshState(page);
  await stubAPI(page);
  await page.goto(`/form-settings?form=${FORM_ID}`, { waitUntil: "networkidle" });

  const notifications = DICT["fs.notifications"].en;
  const general = DICT["fs.general"].en;
  await expect(page.locator(`nav button:text-is("${general}")`).first()).toBeVisible({
    timeout: 45_000,
  });
  await page.locator(`nav button:text-is("${notifications}")`).first().click();

  // The same measurement the other test makes, so the axe arm is scoped to the
  // very element whose ratio is asserted above — a page-wide scope would let a
  // violation elsewhere be read as the strip's, and the settings sidebar's own
  // `<nav>` would be pulled in with it.
  const strip = await measureActiveTab(page, notifications);
  const results = await scanStrip(page, strip.selector);
  const contrast = results.violations.filter((v) => v.id === "color-contrast");
  if (contrast.length > 0) {
    console.log(
      `\n[form-settings tab strip] color-contrast:\n` +
        contrast
          .map((v) => v.nodes.map((n) => `  ${n.target.join(" ")} — ${n.failureSummary ?? ""}`).join("\n"))
          .join("\n"),
    );
  }
  expect(contrast.map((v) => v.impact), "the tab strip must have no color-contrast violation").toEqual(
    [],
  );

  // A scoped axe run over an absent subtree, or one where the colour engine gave
  // up, would also report 0 violations — and that reading would prove nothing.
  // The rule must appear in the RESULTS, i.e. axe really evaluated it here.
  const ran = [...results.passes, ...results.incomplete, ...results.violations].some(
    (r) => r.id === "color-contrast",
  );
  expect(
    ran,
    `axe never evaluated color-contrast over the strip (analyzed ${results.passes.length} passing ` +
      `rule-nodes, ${results.incomplete.length} incomplete) — a 0-violation reading from a scan ` +
      `that did not run the rule is vacuous`,
  ).toBe(true);
});
