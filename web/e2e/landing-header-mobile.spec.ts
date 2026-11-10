// THE LANDING HEADER ON A PHONE (card t_2b2da6a7) — five desktop controls on a
// row that has no phone layout, wrapped into two.
//
// THE DEFECT (measured on the shipped page with this same harness: 390x844,
// isMobile, hasTouch, dpr 2, fresh browser state, lang=en):
//
//   header height 111px   (desktop: 71px — 13% of an 844px first screen)
//   Docs x=20 34x20 · Log in x=70 41x20 · Build a form x=126 113x36 (filled) ·
//   EN x=256 44x44 · BM x=300 44x44 — three baselines (y=65 / 57 / 53), two
//   20px-tall text targets, and 25px of slack in a 350px content row.
//
// WHAT IS ASSERTED, at the viewport the card measured:
//   1. ONE ROW. Header height <= 72px, every visible control's vertical centre
//      within 4px of the others (a wrapped second row moves the centres by half
//      a control height), and document scrollWidth == clientWidth.
//   2. TAP TARGETS. No visible control shorter than 44px on the phone (the
//      shipped row had two at 20px), and the filled primary is >= 44px.
//   3. ONE PRIMARY. Exactly one control with a painted background in the header,
//      and it is /builder — the hierarchy card t_53bcd451 established and this
//      card must not spend (docs and log in are TEXT links at both widths).
//   4. THE SHEET. The menu button opens it, it holds Docs + Log in + both
//      languages as >= 44px rows, opening it does not change the header's
//      height, Escape closes it, and picking Bahasa Melayu really switches the
//      document language (the reason the switch may not be buried).
//   5. DESKTOP IS UNTOUCHED. At 1280px the row is Docs · Log in · Build a form ·
//      EN|BM at the same geometry it had before this change — the numbers are
//      measured on a build of this tree with the change stashed, and that run's
//      header screenshots at 1280px and 1440px are BYTE-IDENTICAL to the
//      after-build's, so a desktop regression fails here rather than in review.
//   6. THE 320px ARM STILL HOLDS (cards t_68456ead, t_1aed910e): at a 320px
//      viewport, with the root font raised to 24px through CDP, the page still
//      does not pan sideways and the sheet is still reachable.
//
// BITE PROOF (run on this tree before hand-off, with the phone branch sabotaged
// by removing `hidden … sm:inline` from Docs and Log in and `sm:hidden` from the
// menu button — i.e. the five-item desktop row back on a phone): arms 1 and 2
// red at 111px and at two 20px targets. Restoring the branch turns them green.
//
// FRESH BROWSER STATE on every arm: storage and cookies are cleared and the
// language is asserted to be the default `en` BEFORE anything is measured, so a
// `jf_lang` left behind by another card cannot render a page these assertions do
// not describe.
import { test, expect, type Page } from "@playwright/test";

// The phone the card measured, and the narrow phone the raised-root arms use.
const PHONE = { width: 390, height: 844 };
const NARROW = { width: 320, height: 844 };
const DESKTOP = { width: 1280, height: 800 };

test.use({ viewport: PHONE, isMobile: true, hasTouch: true, deviceScaleFactor: 2 });

/** Clear every trace of an earlier card, then prove the page is the default one. */
async function fresh(page: Page): Promise<void> {
  await page.goto("/landing");
  await page.evaluate(() => {
    localStorage.clear();
    sessionStorage.clear();
  });
  await page.context().clearCookies();
  await page.reload();
  await expect(page.locator("nav").first()).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.lang)).toBe("en");
}

/** The header's visible controls plus its own box and the document's width. */
async function header(page: Page) {
  return page.evaluate(() => {
    const nav = document.querySelector("nav") as HTMLElement;
    const navBox = nav.getBoundingClientRect();
    const visible = (el: Element) => {
      const cs = getComputedStyle(el);
      const b = el.getBoundingClientRect();
      return cs.display !== "none" && cs.visibility !== "hidden" && b.width > 0 && b.height > 0;
    };
    const painted = (el: Element) => {
      const bg = getComputedStyle(el).backgroundColor;
      return bg !== "rgba(0, 0, 0, 0)" && bg !== "transparent";
    };
    const controls = [...nav.querySelectorAll("a,button")].filter(visible).map((el) => {
      const b = el.getBoundingClientRect();
      return {
        tag: el.tagName,
        text: (el.textContent || "").trim().slice(0, 24),
        href: el.getAttribute("href"),
        x: Math.round(b.x),
        y: Math.round(b.y),
        w: Math.round(b.width),
        h: Math.round(b.height),
        filled: painted(el),
      };
    });
    return {
      navHeight: Math.round(navBox.height),
      scrollWidth: document.documentElement.scrollWidth,
      clientWidth: document.documentElement.clientWidth,
      controls,
    };
  });
}

const menuButton = (page: Page) => page.locator('nav button[aria-controls="landing-header-menu"]');
const sheet = (page: Page) => page.locator("#landing-header-menu");

test("390px: one row, 72px or less, one filled primary, no 20px tap targets", async ({ page }) => {
  await fresh(page);
  const h = await header(page);

  // SOFT for the three that make up the card's stated spec, so a build that is
  // wrong in more than one way reports ALL of them in one run: the sabotage leg
  // (the pre-fix five-item row) must show the height AND the hit size, not just
  // whichever assertion happens to come first.
  expect.soft(h.navHeight, "the header has wrapped or grown past the 72px budget").toBeLessThanOrEqual(72);
  expect(h.scrollWidth, "the header panned the document sideways").toBe(h.clientWidth);

  // ONE ROW: every visible control is centred on the same line. A wrapped row
  // puts the second group half a control lower, which this catches.
  const centres = h.controls.map((c) => c.y + c.h / 2);
  const spread = Math.max(...centres) - Math.min(...centres);
  expect.soft(spread, `the header's controls sit on ${new Set(h.controls.map((c) => c.y)).size} baselines: ${JSON.stringify(h.controls)}`).toBeLessThanOrEqual(4);

  // TAP TARGETS: no control under 44px on a phone.
  const small = h.controls.filter((c) => c.h < 44);
  expect.soft(small, `header controls under the 44px touch target: ${JSON.stringify(small)}`).toEqual([]);

  // ONE PRIMARY, and it is the build path.
  const filled = h.controls.filter((c) => c.filled);
  expect(filled.map((c) => c.href), "the header must paint exactly one filled control, and it must be /builder").toEqual(["/builder"]);
  expect(filled[0].h, "the filled primary is under the 44px touch target").toBeGreaterThanOrEqual(44);

  // The phone bar is the brand, that primary and the menu button — the four
  // desktop-only controls are not in it.
  const hrefs = h.controls.map((c) => c.href).sort();
  expect(hrefs, `the 390px header still shows: ${JSON.stringify(h.controls.map((c) => c.text))}`).toEqual(["/builder", null]);
  await expect(menuButton(page)).toBeVisible();
  await expect(menuButton(page)).toHaveAttribute("aria-expanded", "false");
});

test("390px: the menu opens onto Docs, Log in and both languages, each a 44px row", async ({ page }) => {
  await fresh(page);
  const closed = (await header(page)).navHeight;
  await menuButton(page).click();
  await expect(menuButton(page)).toHaveAttribute("aria-expanded", "true");
  await expect(sheet(page)).toBeVisible();

  // Opening a popover must not move the bar: the sheet is absolutely
  // positioned, so the header keeps the height arm 1 measured.
  expect((await header(page)).navHeight, "opening the sheet grew the header").toBe(closed);

  const rows = sheet(page).locator("a, button");
  const count = await rows.count();
  expect(count, "the sheet does not hold four rows (Docs, Log in, English, Bahasa Melayu)").toBe(4);

  const measured = await rows.evaluateAll((els) =>
    els.map((el) => {
      const b = el.getBoundingClientRect();
      return {
        text: (el.textContent || "").trim(),
        href: el.getAttribute("href"),
        label: el.getAttribute("aria-label"),
        h: Math.round(b.height),
        w: Math.round(b.width),
      };
    }),
  );
  const short = measured.filter((r) => r.h < 44);
  expect(short, `sheet rows under the 44px touch target: ${JSON.stringify(short)}`).toEqual([]);

  // Every destination the desktop row carries is in the sheet, by href — not by
  // a label the two could drift apart on.
  expect(measured.filter((r) => r.href === "/docs").length, "no /docs row in the sheet").toBe(1);
  expect(measured.filter((r) => r.href === "/login").length, "no /login row in the sheet").toBe(1);
  const langs = measured.filter((r) => r.label);
  expect(langs.map((r) => r.label)).toEqual(["EN — English", "BM — Bahasa Melayu"]);

  // …and the language switch WORKS from the sheet: the one thing a BM reader
  // needs from the phone header.
  await rows.nth(3).click();
  await expect(page.locator("html")).toHaveAttribute("lang", "ms");
  await expect(page.locator('nav a[href="/builder"]')).toHaveText("Bina borang");
});

test("390px: Escape closes the sheet", async ({ page }) => {
  await fresh(page);
  await menuButton(page).click();
  await expect(sheet(page)).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(menuButton(page)).toHaveAttribute("aria-expanded", "false");
  await expect(sheet(page)).toBeHidden();
});

test("1280px: the desktop row is exactly the row it was", async ({ page }) => {
  await page.setViewportSize(DESKTOP);
  await fresh(page);
  const h = await header(page);

  // The BEFORE numbers are not typed from memory: they were read off a build of
  // this same tree with this change stashed (origin/main's page.tsx), which
  // measured this exact row at 1280px and 1440px. The header screenshot of the
  // same run is byte-identical to the after-build's, and these are the sizes
  // that produced it.
  expect(h.navHeight, "the desktop header changed height").toBe(71);
  expect(h.scrollWidth).toBe(h.clientWidth);
  expect(
    h.controls.map((c) => [c.text, c.w, c.h]),
    "the desktop row's controls changed size or order",
  ).toEqual([
    ["Docs", 34, 20],
    ["Log in", 41, 20],
    ["Build a form", 113, 36],
    ["EN", 44, 44],
    ["BM", 44, 44],
  ]);
  // The desktop row's own spacing: 16px between the text links and the CTA, and
  // the two language buttons touching (the segmented control). Compared with a
  // 1px tolerance because these are integers read off fractional layout boxes.
  const [docs, login, cta, en, bm] = h.controls;
  const gaps = [login.x - (docs.x + docs.w), cta.x - (login.x + login.w), en.x - (cta.x + cta.w), bm.x - (en.x + en.w)];
  expect(
    gaps.map((g, i) => Math.abs(g - [16, 16, 17, 0][i]) <= 1),
    `the desktop row's gaps changed: ${JSON.stringify(gaps)} (was [16, 16, 17, 0])`,
  ).toEqual([true, true, true, true]);
  expect(h.controls.filter((c) => c.filled).map((c) => c.href), "the desktop header paints one control, /builder").toEqual(["/builder"]);
  // The phone-only controls are gone at this width.
  await expect(menuButton(page)).toBeHidden();
  await expect(sheet(page)).toBeHidden();
});

test("320px at a 24px root: the page still does not pan, and the sheet still opens", async ({ page, context }) => {
  await page.setViewportSize(NARROW);
  const cdp = await context.newCDPSession(page);
  await cdp.send("Page.enable");
  await cdp.send("Page.setFontSizes", { fontSizes: { standard: 24, fixed: 24 } });
  await fresh(page);

  const rootFont = await page.evaluate(() => getComputedStyle(document.documentElement).fontSize);
  expect(rootFont, "the raised-root arm did not take effect — this test would pass while measuring nothing").toBe("24px");

  const h = await header(page);
  expect(h.scrollWidth, "the header panned the document sideways at 320px / 24px root").toBe(h.clientWidth);

  // The wordmark may stand down at this width (card item 6) but the menu must
  // still expose Docs, Log in and the languages.
  await menuButton(page).click();
  await expect(sheet(page)).toBeVisible();
  await expect(sheet(page).locator('a[href="/docs"]')).toBeVisible();
  await expect(sheet(page).locator('a[href="/login"]')).toBeVisible();
  expect(await sheet(page).locator("button[aria-pressed]").count(), "the language rows left the sheet at 320px").toBe(2);
});
