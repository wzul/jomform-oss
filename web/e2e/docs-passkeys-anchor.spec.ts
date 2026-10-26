// /docs#passkeys — the RENDERED anchor, driven in a real browser (card t_140e99fa).
//
// WHY THIS FILE EXISTS. `src/lib/passkeyDocs.test.ts` guards the deep link by
// reading `app/docs/page.tsx`'s SOURCE through the TypeScript parser, and it says
// so in its own header: it proves the link is DECLARED in the passkey section,
// not that the browser paints it — "a render branch that never runs still counts
// as 'rendered'". Measured at `48e0e948`: `grep -rn '#passkeys' web/src web/e2e`
// returned web/src only, i.e. no spec loaded a docs ANCHOR at all. So deleting
// `scroll-mt-24` from the section render, or dropping `id={f.slug}` from it,
// shipped green — nothing rendered the page and looked.
//
// WHAT A USER ACTUALLY DEPENDS ON (each arm below measures one):
//   1. exactly one `id="passkeys"` is in the SERVED HTML and in the DOM, and the
//      fragment RESOLVES (`:target`) — the deep link from Profile → Passkeys "?"
//      reaches a section, and the section really rendered its content;
//   2. the section carries the 96px scroll margin (`scroll-mt-24` = 6rem), so a
//      fragment landing is offset past the header band instead of pinned to the
//      top of the scrollport;
//   3. the browser APPLIES that margin — measured on anchors of the same page
//      that still have scroll room below them, where the landing top is 96px.
//
// WHY ARM 3 IS NOT MEASURED ON `#passkeys` ITSELF, and why that is not a gap
// left open: the passkeys section is the LAST one on the page, so the document
// runs out of scroll before the margin can take effect. Measured on this tree in
// a fresh browser state, `/docs#passkeys` lands at `scrollY 3969 == maxScroll
// 3969` with `rect.top 308` — the SAME numbers with and without `scroll-mt-24`,
// because the browser clamps at the end of the document either way. Asserting a
// 96px landing there would therefore pass on the broken tree, which is exactly
// the vacuous guard this card exists to avoid. What DOES change when the class
// is removed is the computed value arm 2 reads (96px -> 0px); arm 3 then proves
// the same declaration is really applied by the browser, on the same page and
// stylesheet, at anchors where the clamp cannot hide it.
//
// FRESH STATE. Playwright gives every test a fresh context (no cookies, no
// storage), so the page renders in the visitor's default locale — the docs prose
// is English-only by design and nothing here sets `jf_lang`.
import { test, expect, type Page } from "@playwright/test";

// `scroll-mt-24` is Tailwind's 24 × 0.25rem = 6rem = 96px at the default root
// font size. Layout can land a subpixel off it (measured 95.5–96.25px across
// the anchors below), so the offset band allows 2px — far tighter than the 96px
// error the missing class produces.
const SCROLL_MARGIN = 96;
const BAND = 2;

type Landed = {
  top: number;
  bottom: number;
  height: number;
  scrollMarginTop: string;
  scrollY: number;
  maxScroll: number;
  viewport: number;
  isTarget: boolean;
};

/** Follow `#id` on /docs as a visitor does and read where the browser put it. */
async function follow(page: Page, id: string): Promise<Landed> {
  await page.goto(`/docs#${id}`);
  return page.evaluate((anchor: string) => {
    const el = document.getElementById(anchor);
    if (!el) throw new Error(`no #${anchor} on /docs — the fragment cannot resolve`);
    const r = el.getBoundingClientRect();
    const doc = document.documentElement;
    return {
      top: r.top,
      bottom: r.bottom,
      height: r.height,
      scrollMarginTop: getComputedStyle(el).scrollMarginTop,
      scrollY: window.scrollY,
      maxScroll: doc.scrollHeight - window.innerHeight,
      viewport: window.innerHeight,
      isTarget: document.querySelector(`#${anchor}:target`) !== null,
    };
  }, id);
}

test("the served HTML carries exactly one #passkeys anchor and the fragment resolves to it", async ({ page }) => {
  // The SERVED bytes, before any hydration: a section rendered by a branch that
  // never runs cannot put this attribute in the HTML. Asserted on the document
  // the browser really loaded (page.request shares the context's origin).
  const served = await page.request.get("/docs");
  expect(served.status()).toBe(200);
  const html = await served.text();
  expect(html.match(/id="passkeys"/g) ?? []).toHaveLength(1);

  await page.goto("/docs#passkeys");
  await expect(page.locator("#passkeys")).toHaveCount(1);

  const landed = await follow(page, "passkeys");
  // `:target` is resolved by the browser's own fragment handling — the anchor a
  // `#passkeys` link names is this element, not merely an id that exists.
  expect(landed.isTarget).toBe(true);

  // The section rendered its CONTENT, not an empty shell: its own heading is the
  // passkey documentation a reader is sent to.
  await expect(page.locator("#passkeys h3")).toHaveText(/passkeys/i);
});

test("the passkeys section keeps the 96px scroll margin and lands past the header band", async ({ page }) => {
  const landed = await follow(page, "passkeys");

  // The property the offset comes from. Removing `scroll-mt-24` from the
  // FAQ section render turns this into "0px" — the arm that bites on the
  // section this card is about.
  expect(landed.scrollMarginTop).toBe(`${SCROLL_MARGIN}px`);

  // The fragment moved the page (not a top-of-page no-op), and the section sits
  // in the viewport, clear of the top 96px header band rather than under it.
  expect(landed.scrollY).toBeGreaterThan(0);
  expect(landed.top).toBeGreaterThanOrEqual(SCROLL_MARGIN);
  expect(landed.bottom).toBeGreaterThan(0);
  expect(landed.top).toBeLessThan(landed.viewport);

  // Documented consequence, asserted so a reader is not misled by the numbers
  // above: this is the last section, so the landing is clamped at the end of
  // the document. If content is ever added below the FAQ, the fragment will
  // have room and the landing MUST sit in the offset band — the branch that
  // would have caught a missing margin on THIS section.
  if (landed.scrollY < landed.maxScroll) {
    expect(Math.abs(landed.top - SCROLL_MARGIN)).toBeLessThanOrEqual(BAND);
  } else {
    expect(landed.scrollY).toBe(landed.maxScroll);
  }
});

test("the browser applies the 96px scroll margin — measured where the clamp cannot hide it", async ({ page }) => {
  // Group anchors that sit well above the end of the document, so the margin is
  // free to take effect. These are the same `scroll-mt-24` sections the passkeys
  // FAQ entry uses (all rendered by app/docs/page.tsx with that class), so this
  // arm measures the DECLARATION the passkeys section carries, applied.
  for (const id of ["account", "security"]) {
    const landed = await follow(page, id);

    // A guard on this arm: an anchor that clamps cannot show the offset, so the
    // measurement is only evidence while the document really has scroll room.
    expect(landed.scrollY).toBeLessThan(landed.maxScroll);

    // The bite, asserted BEFORE the declarative check below so a removed class
    // fails on the MEASUREMENT rather than on the property: with `scroll-mt-24`
    // gone the browser lands the section at the top of the scrollport
    // (top ≈ 0). 96 ± 2px is nowhere near that.
    expect(Math.abs(landed.top - SCROLL_MARGIN)).toBeLessThanOrEqual(BAND);
    expect(Math.abs(landed.top)).toBeGreaterThan(BAND);

    expect(landed.scrollMarginTop).toBe(`${SCROLL_MARGIN}px`);
  }
});
