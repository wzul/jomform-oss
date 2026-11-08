// Regression guard for "the landing hero still pans in ta with a phone's Tamil
// font (89px at 320px/24px root) — LTR h1 line unguarded" (card t_1aed910e).
//
// THE DEFECT (observed from outside on prod v1.23.3 @ 50ad8a50, and reproduced
// on the built page here: Chromium 320x844 dsf2 mobile viewport, root font 24px
// via CDP Page.setFontSizes, fresh context, `jf_lang` set and the page reloaded,
// Noto Sans Tamil — Android's ta fallback — forced on the hero). The previous
// fix scoped its break guards by DIRECTION (`rtl:[overflow-wrap:anywhere]`) and
// by an em BAND (`max-[14.7em]`), because the only locale that had been measured
// was ar and the host's Tamil font was FreeSans. Both keys miss the real case:
//
//   ta   is LTR, so it kept `overflow-wrap: normal`. At a 24px root `land.hero1`
//        lays out as a 370.6px run (right edge 406.6) and `land.hero2` as a
//        372.6px run (right edge 408.6) inside a 248px column:
//        documentElement.scrollWidth 409 vs clientWidth 320, innerWidth 409,
//        window.scrollX 0 -> 89. At 360px it pans 61 and at 390px 31.
//   ta   subtitle: one run of `land.sub` is 385px at the 27px subtitle size and
//        an unbreakable run lays out from the paragraph's start edge, so its
//        right edge is 36 + 385 = 421 — scrollWidth 421 at BOTH 360 and 390
//        (pan 61 / 31). The 14.7em band ends at 352.8px at a 24px root, so it
//        was off exactly where the defect is.
//   ta   the use-case cards: `land.qrtitle` holds one unbreakable Tamil word
//        341px wide in a 152px column, so the QR card's shrink-to-fit column
//        measured 341px (left -10.5, right 330.5) — a 10.5px document pan at
//        320px even with the hero lines fixed.
//
// WHAT THE FIX IS, and why it is shaped this way:
//   * the h1 and the subtitle take an UNCONDITIONAL `[overflow-wrap:anywhere]`.
//     `anywhere` only breaks a word that does not fit its line box — the exact
//     condition that pans — so it is inert anywhere nothing overflows, and it is
//     not keyed on direction or on a width band, which are what let ta through.
//   * the <main> takes the same guard inside `max-[17em]`, for the page's narrow
//     columns (the cards). It has to be `anywhere` and not `break-word`:
//     `anywhere` is the only value whose break opportunities count in the
//     min-content size, so the flex/`shrink-to-fit` column stops being floored
//     at the widest word. The band keeps the 100% render out of it: a 320px
//     viewport is 20em at a 16px root.
//   * the CTA row keeps its em-banded `flex-wrap`, but the band is re-derived
//     from the PHONE font: the ta pair is 201 + 152 + 18 = 371px = 15.46em at a
//     24px root (it was 14.59em with the host's FreeSans), and the centred row
//     pans exactly when the pair is wider than the viewport — measured left
//     -5.5 / right 365.5 at 360px, a 5.5px pan — so 15.5em is the first half-em
//     above it.
//
// WHY A SOURCE-CONTRACT TEST AND NOT A BROWSER TEST
//   This repo has no DOM/JSX unit runner (see src/lib/docsApiProseWrap.test.ts)
//   and the a11y gate cannot measure layout deformation. The pixel numbers above
//   were measured in a real browser (Chromium + Firefox, and WebKit at the
//   em-equivalent viewport) and are re-checked in the card; what a unit test can
//   pin is the shape that carries the behaviour — guards that are UNCONDITIONAL
//   where the defect is font-width-dependent, bands that stay outside the
//     100%-text em range where the defect is column-width-dependent, the copy the
//   measurements are derived from, and that the sites are real so the assertions
//   cannot pass vacuously.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const LANDING = "../app/landing/page.tsx";
const DICT = "../components/i18nDict.ts";

const landing = readFileSync(fileURLToPath(new URL(LANDING, import.meta.url)), "utf8");
const dict = readFileSync(fileURLToPath(new URL(DICT, import.meta.url)), "utf8");

/** The em widths the measurements below are stated in. */
const PHONE_PAIR_EM = 15.46; // ta CTA pair with Noto Sans Tamil at a 24px root (201+152+18 = 371px)
const HOST_PAIR_EM = 14.59; // the same pair with the host's FreeSans (350.1px) — no longer the binding number
const ROOT16_VIEWPORT_EM = 20; // 320px at a 16px root — the 100%-text reference
const CARD_RUN_EM = 14.21; // the 341px QR-card Tamil heading at a 24px root

/** A class name that permits a break inside an unbreakable run. */
const BREAK_GUARD = /\[overflow-wrap:(?:anywhere|break-word)\]|\bbreak-words\b|\bbreak-all\b/;
/** A guard scoped to a direction or a width band — neither can key on "the word does not fit". */
const SCOPED_GUARD = /(?:^|\s)(?:rtl|ltr|max-\[[0-9.]+em\]|min-\[[0-9.]+em\]|sm|md|lg|xl):\[overflow-wrap:/;

const HERO = landing.match(/<section\b[^>]*>([\s\S]*?)<\/section>/);
const HERO_BODY = HERO?.[1] ?? "";
const MAIN = landing.match(/<main className="([^"]*)"/);
const HERO_H1 = HERO_BODY.match(/<h1 className="([^"]*)"/);
const CTA_ROW = HERO_BODY.match(/<div className="([^"]*)"[^>]*>\s*<a href="\/login"/);
const CTA_BLOCK = HERO_BODY.match(
  /<div className="[^"]*"[^>]*>\s*<a href="\/login"[\s\S]*?<\/a>\s*<a href="#how"[\s\S]*?<\/a>\s*<\/div>/,
);
const HERO2_SPAN = HERO_BODY.match(/<span className="([^"]*)">\{t\("land\.hero2"\)\}<\/span>/);
const HERO_SUB = HERO_BODY.match(/<p className="([^"]*)">\{t\("land\.sub"\)\}<\/p>/);

/** The `lang` value of a dictionary key (the string whose width the numbers above were measured on). */
function dictValue(key: string, lang: string): string | undefined {
  const entry = dict.match(new RegExp(`"${key.replace(".", "\\.")}":\\s*\\{([^}]*)\\}`));
  return entry?.[1].match(new RegExp(`\\b${lang}:\\s*"([^"]*)"`))?.[1];
}

test("the guard is looking at the real render sites (it cannot pass vacuously)", () => {
  assert.ok(HERO, `no <section> found in ${LANDING} — the hero assertions would be pinning nothing`);
  assert.match(
    HERO_BODY,
    /land\.badge[\s\S]*land\.hero1[\s\S]*land\.hero2[\s\S]*land\.cta2/,
    `${LANDING}: the first <section> no longer looks like the hero (badge / hero1 / hero2 / cta2) — ` +
      "every assertion below is anchored on it",
  );
  assert.ok(MAIN, `no <main className="…"> in ${LANDING} — the page-wide band assertion would be pinning nothing`);
  assert.ok(
    HERO_H1,
    `no <h1 className="…"> in the hero of ${LANDING} — the h1 break guard is what keeps the ta hero at the ` +
      "phone's Tamil font from panning the document",
  );
  assert.ok(
    CTA_ROW,
    `no CTA row (a <div className="…"> opening immediately before the href="/login" hero button) in ` +
      `${LANDING} — the wrap assertion below would be pinning nothing`,
  );
  assert.ok(
    CTA_BLOCK,
    `the hero CTA row could not be read as the /login + #how pair in ${LANDING} — the wrap assertion ` +
      "below depends on both buttons living in that row",
  );
  assert.ok(
    HERO2_SPAN,
    `no <span className="…">{t("land.hero2")}</span> in ${LANDING} — that span is the second half of the ` +
      "hero headline and carries its colour",
  );
  assert.ok(
    HERO_SUB,
    `no <p className="…">{t("land.sub")}</p> in ${LANDING} — that paragraph holds the 385px ta run that ` +
      "panicked the document at 360 and 390",
  );
  // The numbers below are measurements of these strings; if the copy moves, the
  // guard and its measurements have to be re-taken rather than silently inherited.
  assert.equal(
    dictValue("land.hero2", "ta"),
    "CHIP மூலம் பணம் வசூலிக்கவும்",
    `${DICT}: land.hero2.ta changed — the 372.6px run that panned ta by 89px at 320px / a 24px root ` +
      "(Noto Sans Tamil) was measured on this exact string; re-measure the hero and update this test",
  );
  assert.equal(
    dictValue("land.sub", "ta"),
    "மலேசிய சமூக விற்பனையாளர்களுக்கான படிவங்கள், பணம், விற்பனை.",
    `${DICT}: land.sub.ta changed — the 385px subtitle run whose right edge landed at 421 (pan 61 at 360px, ` +
      "31 at 390px, 24px root) was measured on this exact string",
  );
  assert.equal(
    dictValue("land.qrtitle", "ta"),
    "உணவகங்களுக்கான QR ஆர்டர்",
    `${DICT}: land.qrtitle.ta changed — the 341px unbreakable heading word that floored the QR card's column ` +
      "and panned the document 10.5px at 320px / a 24px root was measured on this exact string",
  );
  assert.equal(
    dictValue("land.hero2", "ar"),
    "اقبل المدفوعات عبر CHIP",
    `${DICT}: land.hero2.ar changed — the 286.8px min-content run that pans ar by 3px was measured on ` +
      "this exact string; the h1 guard has to keep covering it",
  );
  assert.equal(
    dictValue("land.cta2", "ms"),
    "Macam mana ia berfungsi",
    `${DICT}: land.cta2.ms changed — the CTA pair width the wrap band is derived from was measured on this ` +
      "exact label; re-measure the row and update the max-[…em] band and this test",
  );
});

test("the hero CTA row wraps into two rows below 15.5em, and only below that band", () => {
  const cls = (CTA_ROW as RegExpMatchArray)[1];
  const scopedWrap = cls.match(/max-\[([0-9.]+)em\]:flex-wrap/);

  assert.match(
    cls,
    /(^|\s)flex(\s|$)/,
    `the hero CTA row has className "${cls}" and is no longer a flex row — the wrap arm below assumes ` +
      "the two buttons are flex items (min-content floors are what make the pair overflow)",
  );
  assert.ok(
    scopedWrap,
    `the hero CTA row has className "${cls}" with no em-scoped wrap — without it the two buttons ` +
      "(371px of min-content in a 288px row at 360px / a 24px root with the phone's Tamil font) are pushed " +
      "past the viewport edge: measured right -5.5 / right 365.5, a 5.5px pan",
  );
  // Unconditional wrap is a regression of its own: at 320px / a 16px root the
  // row currently fits on one line (measured en 126 + 134, ms 112 + 148,
  // ar 109 + 124), and an unconditional `flex-wrap` puts that 100%-text render
  // at the mercy of subpixel rounding.
  assert.doesNotMatch(
    cls,
    /(^|\s)flex-wrap(\s|$)/,
    `the hero CTA row has className "${cls}" with an UNCONDITIONAL flex-wrap — that also reflows the ` +
      "16px render (320px is 20em at a 16px root: the pair fits and must stay on one row). Scope the " +
      "wrap to a max-[…em] variant instead",
  );

  const threshold = Number(scopedWrap?.[1]);
  assert.ok(
    Number.isFinite(threshold) && threshold > PHONE_PAIR_EM && threshold < ROOT16_VIEWPORT_EM,
    `the hero CTA row wraps below ${scopedWrap?.[1]}em, outside the only window that both fixes the pan ` +
      `and leaves the 100% render alone: it must be above the ${PHONE_PAIR_EM}em the two ta labels ` +
      "actually need with the phone's Tamil font (" +
      `${HOST_PAIR_EM}em was the HOST font's number, and a band derived from it is what left this row ` +
      `5.5px over the edge at 360px / a 24px root) and below the ${ROOT16_VIEWPORT_EM}em that the same ` +
      "320px viewport measures at a 16px root (otherwise the two-button 100%-text row reflows)",
  );

  // Wrapping must stay centred — the original row centred the pair, and a
  // wrapped line that is left-aligned under a centred one reads as a bug.
  assert.match(
    cls,
    /\bjustify-center\b/,
    `the hero CTA row has className "${cls}" without justify-center — the wrapped second row would ` +
      "sit against the left edge under a centred first row",
  );
  assert.match(
    cls,
    /(^|\s)gap-3(\s|$)/,
    `the hero CTA row has className "${cls}" without gap-3 — once the row can wrap the two rows need a ` +
      "vertical gap, which gap-3 already provides (measured row-gap 18px at a 24px root)",
  );
});

test("the hero h1 (both lines) may break inside a word, unconditionally", () => {
  const cls = (HERO_H1 as RegExpMatchArray)[1];

  assert.match(
    cls,
    /\[overflow-wrap:anywhere\]/,
    `the hero <h1> has className "${cls}" without [overflow-wrap:anywhere] — at a 24px root the ta hero ` +
      "lays out as a 370.6px run (hero1) and a 372.6px run (hero2) in a 248px column, so the document " +
      "measures scrollWidth 409 / clientWidth 320 and scrolls 89px sideways at 320px, 61px at 360px and " +
      "31px at 390px with the phone's Tamil font",
  );
  // The whole point of the rework: a guard scoped by direction (or by an em band)
  // cannot key on the condition that actually pans — ta is LTR, and at a 24px
  // root it pans at 320/360/390, above the old 14.7em band.
  const scoped = cls.match(SCOPED_GUARD);
  assert.equal(
    scoped,
    null,
    `the hero <h1> has className "${cls}" with a SCOPED break guard (${scoped?.[0]}) — that is the shape ` +
      "that let ta through: the previous fix used rtl:[overflow-wrap:anywhere], ta is LTR, so it kept " +
      "overflow-wrap: normal and panned. Keep the guard unconditional: `anywhere` only breaks a word " +
      "that does not fit, so it is inert where nothing overflows",
  );
  // Scoped to the h1, both hero lines are covered. The span stays the coloured
  // half of the headline and must not carry its own narrower guard.
  assert.match(
    (HERO2_SPAN as RegExpMatchArray)[1],
    /text-\[var\(--jf-primary\)\]/,
    `the hero2 <span> has className "${(HERO2_SPAN as RegExpMatchArray)[1]}" and lost its primary colour — ` +
      "the span is the second half of the hero headline",
  );
  assert.doesNotMatch(
    (HERO2_SPAN as RegExpMatchArray)[1],
    BREAK_GUARD,
    `the hero2 <span> carries its own break guard ("${(HERO2_SPAN as RegExpMatchArray)[1]}") instead of ` +
      "the h1 carrying one for both lines — a guard on the span leaves land.hero1 unguarded, and hero1 is " +
      "also wider than the column at a 24px root (370.6px)",
  );
});

test("the hero subtitle may break inside a word, unconditionally", () => {
  const cls = (HERO_SUB as RegExpMatchArray)[1];

  assert.match(
    cls,
    /\[overflow-wrap:anywhere\]/,
    `the hero subtitle has className "${cls}" without [overflow-wrap:anywhere] — in ta at a 24px root one ` +
      "run of land.sub is 385px wide and lays out from the paragraph's start edge, so its right edge is " +
      "36 + 385 = 421: the document measured scrollWidth 421 at BOTH 360 and 390 (pan 61 and 31)",
  );
  const scoped = cls.match(SCOPED_GUARD);
  assert.equal(
    scoped,
    null,
    `the hero subtitle has className "${cls}" with a SCOPED break guard (${scoped?.[0]}) — the previous fix ` +
      "banded it at max-[14.7em], which is 352.8px at a 24px root, so the guard was off at exactly 360 and " +
      "390 where the 385px run panics the document. Key it on the run not fitting, not on the viewport",
  );
  // The measurement is of the 1.125rem subtitle at a 24px root; if the type
  // scale moves, the 385px run has to be re-measured.
  assert.match(
    cls,
    /(^|\s)text-lg(\s|$)/,
    `the hero subtitle has className "${cls}" without text-lg — the 385px Tamil run was measured at this ` +
      "type step (1.125rem = 27px at a 24px root); re-measure before changing it",
  );
});

test("the page's narrow columns may break inside a word below 17em, but not at the 100% render", () => {
  const cls = (MAIN as RegExpMatchArray)[1];
  const band = cls.match(/max-\[([0-9.]+)em\]:\[overflow-wrap:anywhere\]/);

  assert.ok(
    band,
    `the landing <main> has className "${cls}" with no em-scoped break guard — at 320px / a 24px root the ` +
      "QR card's land.qrtitle is one 341px unbreakable Tamil word in a 152px column, so its shrink-to-fit " +
      "column measures 341px (left -10.5, right 330.5) and the document pans 10.5px even with the hero " +
      "lines fixed",
  );
  assert.doesNotMatch(
    cls,
    /(^|\s)\[overflow-wrap:anywhere\](\s|$)/,
    `the landing <main> has className "${cls}" with an UNCONDITIONAL break guard — that would also break ` +
      "the cards' headings at a 16px root, where the same Tamil word already sticks out of its padding " +
      "box but does not pan. Band it instead",
  );

  const threshold = Number(band?.[1]);
  assert.ok(
    Number.isFinite(threshold) && threshold > CARD_RUN_EM && threshold < ROOT16_VIEWPORT_EM,
    `the landing <main> breaks words below ${band?.[1]}em, outside the only window that both fixes the ` +
      `card pan and leaves the 100% render alone: it must be above the ${CARD_RUN_EM}em (341px at a 24px ` +
      `root) the widest card word needs to be allowed to break, and below the ${ROOT16_VIEWPORT_EM}em a ` +
      "320px viewport measures at a 16px root",
  );
  // The band is a safety net for the page's NARROW columns; the hero lines have
  // their own font-independent guards. It must reach the widest such run (the
  // 341px card heading = 14.21em) with margin for a wider phone font, while
  // staying clear of the 20em a 320px viewport measures at a 16px root.
  assert.ok(
    threshold - CARD_RUN_EM > 1,
    `the landing <main> band is ${band?.[1]}em, less than 1em above the ${CARD_RUN_EM}em card run it ` +
      "exists for — a phone font a little wider than Noto Sans Tamil would pan again just above the band",
  );
});
