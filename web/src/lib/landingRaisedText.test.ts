// Regression guard for "/ pans 64px sideways at 320px with the browser font
// raised to 24px — the nav's Log in button is clipped off-screen" (card
// t_68456ead).
//
// THE DEFECT (measured on a build of this tree, 2026-11-05, Chromium 320x844
// dsf2, root font 24px via CDP Page.setFontSizes, fresh browser state, lang=en):
//
//   documentElement.scrollWidth 384 vs clientWidth 320  — a 64px pan
//   (25px at 360px wide, 2px at 390px; pan 0 at a 16px root).
//
//   Three render sites contributed, each in the same class of bug — a box whose
//   width is its own text at the raised size, inside a container that cannot
//   shrink:
//
//     nav.flex.items-center.justify-between  → the Log in anchor sat at
//       left 261.14 / right 346.72, i.e. 26.7px PAST the 320px edge, visibly cut
//       off, and its label had broken into "Log" / "in";
//     footer div.flex.justify-center.gap-6  → <a href="/brand"> right 357 (the
//       row's content needed 394px inside a 248px footer and overflowed on BOTH
//       sides of `justify-center`);
//     the operator clause <bdi>  → width 347.59 / right 383.59 in a 248px
//       paragraph (that is the 64px), because `whitespace-nowrap` makes it an
//       unbreakable run as wide as its glyphs.
//
// THE FIX, and why each half is shaped the way it is:
//   * nav: `flex-wrap` (+ `gap-y-2`) lets the three items reflow onto a second
//     row instead of pushing the last one off the page. At a 16px root they
//     still fit on one row, so nothing wraps there.
//   * the Log in label: `whitespace-nowrap`, so the flex row wraps the ITEM
//     rather than the words inside it ("Log" / "in" was the clipped shape
//     before).
//   * footer links: `flex-wrap` (+ `gap-y-1`) keeps every link whole and inside
//     the viewport. This is the one arm that changes 100% text as well, and the
//     old layout was already wrong there: with `nowrap` the row shrank its items,
//     so at a 16px root "Privacy Policy" sat in a 49.7px box and "Terms of
//     Service" in 60.5px and each label broke onto two lines inside its own link
//     (five intact labels plus four 24px gaps need 354px in a 272px row). The row
//     now wraps to a second line: labels intact, pan 0, footer 201 -> 205px.
//
//   * the operator clause: `inline-block` + `max-w-full` is what makes it BOTH
//     atomic and shrinkable — the line breaker treats it as one unit (so the
//     hyphen in PC0012291-H is never used as a break opportunity, which is what
//     a11y-docs-lang.spec.ts pins at a 692px paragraph) and, when it is wider
//     than the paragraph, the box takes the paragraph's width and wraps inside
//     it instead of overflowing the page. `whitespace-normal` replaces the old
//     `whitespace-nowrap`, and `[line-height:normal]` keeps the box exactly the
//     height of the text it holds so the surrounding line box does not move at
//     100% text size (measured: same <p> height, same <bdi> top).
//
// WHY A SOURCE-CONTRACT TEST AND NOT A BROWSER TEST
//   This repo has no DOM/JSX unit runner (see src/lib/docsApiProseWrap.test.ts)
//   and the a11y gate cannot measure layout deformation. The pixel numbers above
//   were measured in a real browser and are re-checked in the card; what a unit
//   test can pin is the shape that carries the behaviour — the classes on the
//   three render sites, that those sites are real, and that the browser arm which
//   owns the clause's atomicity is still in the suite, so deleting it is visible.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const LANDING = "../app/landing/page.tsx";
const OPERATOR = "../components/OperatorNotice.tsx";
const A11Y_SPEC = "../../e2e/a11y-docs-lang.spec.ts";

const landing = readFileSync(fileURLToPath(new URL(LANDING, import.meta.url)), "utf8");
const operator = readFileSync(fileURLToPath(new URL(OPERATOR, import.meta.url)), "utf8");
const a11ySpec = readFileSync(fileURLToPath(new URL(A11Y_SPEC, import.meta.url)), "utf8");

/** A class name that permits a break inside an unbreakable run. */
const BREAK_GUARD = /\[overflow-wrap:(?:anywhere|break-word)\]|\bbreak-words\b|\bbreak-all\b/;

function classOf(match: RegExpMatchArray) {
  return match[0].match(/className="([^"]*)"/)?.[1] ?? "";
}

const NAV = landing.match(/<nav\b([^>]*)>/);
const NAV_BLOCK = landing.match(/<nav\b[^>]*>([\s\S]*?)<\/nav>/);
const NAV_LOGIN = NAV_BLOCK?.[1].match(/<a\b([^>]*href="\/login"[^>]*)>/);
const FOOTER_ROW = landing.match(/<div\b([^>]*)>\s*<a href="\/legal\/privacy"/);
const OPERATOR_BDI = operator.match(/<bdi\b([^>]*)>\{regClause\}<\/bdi>/);

test("the guard is looking at the real render sites (it cannot pass vacuously)", () => {
  assert.ok(NAV, `no <nav> found in ${LANDING} — the nav assertions below would be pinning nothing`);
  assert.ok(
    NAV_BLOCK,
    `the <nav> block could not be read from ${LANDING} — the Log in anchor lookup depends on it`,
  );
  assert.ok(
    NAV_LOGIN,
    `no <a href="/login"> inside the landing <nav> — that anchor is the button the card measured at ` +
      "right 346.72 (26.7px past a 320px viewport)",
  );
  assert.ok(
    FOOTER_ROW,
    `no footer link row found in ${LANDING} (a <div> opening immediately before the /legal/privacy ` +
      "link) — the wrap assertion below would be pinning nothing",
  );
  assert.ok(
    OPERATOR_BDI,
    `no <bdi> in ${OPERATOR} — the operator clause is the 347.59px run that made up the 64px pan`,
  );
  // The clause's atomicity is owned by the browser spec; if that arm is deleted,
  // relaxing this component becomes a silent regression, so its presence is part
  // of this contract.
  assert.match(
    a11ySpec,
    /bdiLineTops/,
    `${A11Y_SPEC} no longer measures the clause's line boxes — the "never split across two lines" ` +
      "arm was what made the old whitespace-nowrap necessary, and this component's shape is judged " +
      "against it",
  );
});

test("the landing nav wraps instead of pushing Log in off the page", () => {
  const navCls = classOf(NAV as RegExpMatchArray);
  assert.match(
    navCls,
    /\bflex-wrap\b/,
    `the landing <nav> has className "${navCls}" with no flex-wrap — its three items are all ` +
      "min-content text and none can shrink, so at a raised root font the last one (Log in) is pushed " +
      "past the viewport edge (measured: right 346.72 in a 320px viewport, width 384 vs 320)",
  );
  const loginCls = classOf(NAV_LOGIN as RegExpMatchArray);
  assert.match(
    loginCls,
    /\bwhitespace-nowrap\b/,
    `the nav's Log in anchor has className "${loginCls}" without whitespace-nowrap — the squeezed ` +
      'row broke the label itself into "Log" / "in" (measured height 84px for a 36px button)',
  );
});

test("the footer link row wraps instead of overflowing on both sides", () => {
  const rowCls = classOf(FOOTER_ROW as RegExpMatchArray);
  assert.match(
    rowCls,
    /\bflex-wrap\b/,
    `the footer link row has className "${rowCls}" with no flex-wrap — five links plus four gaps are ` +
      "394px of min-content at a 24px root inside a 248px footer, and `justify-center` makes it " +
      "overflow BOTH edges (measured: left -37, right 357 in a 320px viewport)",
  );
  // Wrapping alone changes 100% text too, and it should: with `nowrap` the row
  // shrank its own items, so at a 16px root "Privacy Policy" sat in a 49.7px box
  // and "Terms of Service" in 60.5px and each label broke onto two lines inside
  // its own link. The row now takes a second line instead, so it needs a row
  // gap or the two lines touch.
  assert.match(
    rowCls,
    /\bgap-y-1\b/,
    `the footer link row has className "${rowCls}" without gap-y-1 — once the row wraps (which it also ` +
      "does at a 16px root, where five intact labels plus four 24px gaps need 354px in a 272px row) the " +
      "second line sits flush against the first",
  );
});

test("the operator clause is atomic AND shrinkable", () => {
  const attrs = OPERATOR_BDI as RegExpMatchArray;
  const cls = classOf(attrs);
  // Still isolated: the RTL fix (card t_d2f2e21f) is orthogonal to this one.
  assert.match(
    attrs[1],
    /dir="ltr"/,
    `${OPERATOR}: the <bdi> lost dir="ltr" — under dir="rtl" the clause renders with its parenthesis ` +
      "pair inverted and the sentence-final period stranded at the start of the line",
  );
  // Atomic at the line-break level: an inline-block is one unit to the
  // surrounding paragraph, whichever way it is internally white-spaced.
  assert.match(
    cls,
    /\binline-block\b/,
    `the operator clause <bdi> has className "${cls}" — without inline-block (or another atomic ` +
      "inline-level box) the surrounding paragraph can break INSIDE the clause at the hyphen in " +
      "PC0012291-H and strand `H).` at the paragraph edge (card t_bdcee56f, 692px paragraph)",
  );
  // …and it may not grow past its paragraph: max-width is what makes it wrap
  // its own words instead of making the page pan.
  assert.match(
    cls,
    /\bmax-w-full\b/,
    `the operator clause <bdi> has className "${cls}" without max-w-full — at a raised root font the ` +
      "clause is 347.59px wide in a 248px paragraph and drags the document 64px sideways",
  );
  // No nowrap: the old rule is exactly what made it wider than the viewport.
  assert.doesNotMatch(
    cls,
    /\bwhitespace-nowrap\b/,
    `the operator clause <bdi> still carries whitespace-nowrap ("${cls}") — that makes it an ` +
      "unbreakable run as wide as its glyphs, which IS the 64px pan this card reports",
  );
  assert.match(
    cls,
    /\bwhitespace-normal\b/,
    `the operator clause <bdi> has className "${cls}" without whitespace-normal — the clause must be ` +
      "able to wrap its own words once max-w-full has shrunk the box",
  );
  assert.match(
    cls,
    BREAK_GUARD,
    `the operator clause <bdi> has className "${cls}" with no break guard — a single word inside the ` +
      "clause wider than the paragraph at an extreme text size would overflow the box",
  );
  // The height guard is what keeps the 100%-text layout pixel-identical: without
  // it the inline-block's own line box (23px) is taller than the text (17px) and
  // the clause's baseline moves inside its line.
  assert.match(
    cls,
    /\[line-height:normal\]/,
    `the operator clause <bdi> has className "${cls}" without [line-height:normal] — the inline-block ` +
      "box would then be the paragraph's line-height tall (23px) instead of the text's own height " +
      "(17px), shifting the clause 2px within its line at 100% text size",
  );
  // It must still render the clause from the shared split, not a hand-written copy.
  assert.match(
    operator,
    /\{regClause \? <> <bdi[^>]*>\{regClause\}<\/bdi><\/> : null\}/,
    `${OPERATOR}: the clause is no longer rendered from operatorParts()'s regClause — the identifiers ` +
      "must stay in src/lib/operator.ts",
  );
});
