// Guard: the landing hero's demo column is the width the hero was measured at,
// and the demo's figures stay internally true (card t_a818aa21).
//
// THE CARD. Wan: "gambar yang belah kanan tu terlalu besar… cuma aku nak kecil
// lagi sikit untuk kolum mockup." The hero's right column was `lg:grid-cols-2`,
// i.e. two equal 524px columns, so the order-form card was 2.46x the height of
// the copy beside it. It is now a FIXED 380px column from `lg` up.
//
// WHY 380 AND NOT "AS SMALL AS IT GOES" — MEASURED in a browser at 1440x900 by
// editing the real DOM, not a mockup:
//
//     column 420 -> card 420x752, hero 912
//     column 380 -> card 380x783, hero 943   <- chosen
//     column 360 -> card 380x783, hero 943   (the card stops shrinking; its rows
//                                             wrap TALLER instead, so the hero
//                                             grows again)
//
// So the number is not a taste: it is the floor of a measured curve, and moving
// it in either direction is a decision that needs its own measurement.
//
// THE SECOND HALF IS ARITHMETIC, and it is the half a "trim the mockup" card is
// most likely to break. The demo shows TWO rows of RM89.00, a subtotal of
// RM178.00, RM8.00 shipping and a total of RM186.00 — the sum a buyer can check
// by eye. A previous trim (one product row deleted, the figures left alone)
// read "qty 1 x RM89 but subtotal RM178", i.e. the demo contradicted itself in
// the one place a payment demo cannot: its own total. Nothing in the suite
// could see it, because the figures are JSX text in a component no DOM runner
// can render. This file therefore pins the DERIVATION, not the pixels:
// `formatMoney(subtotalSen)` / `formatMoney(totalSen)` over PRICE_SEN,
// SHIPPING_SEN and the row list, with a fixture arm that shows the check
// failing for a one-row card.
//
// WHAT IT DOES NOT COVER (stated, not hidden): it reads SOURCE and arithmetic,
// not pixels. The rendered geometry for this card was measured in a browser at
// 390px and 1440px and recorded in the card — hero 946.2px, stage 380x786.2
// (298,756 px2), no horizontal overflow at 390 (scrollWidth == clientWidth ==
// 390), totals RM178.00 / RM8.00 / RM186.00 against two RM89.00 rows.
//
// Run with: npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { formatMoney } from "./money.ts";

const PAGE = "../app/landing/page.tsx";
const HERO_FORM = "../components/landing/HeroOrderForm.tsx";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

/**
 * Comments are stripped before any code assertion: this card documents itself in
 * prose that names the OLD class (`lg:grid-cols-2`) and the old figures, and a
 * scanner that read comments would fail on its own explanation.
 */
function stripComments(src: string): string {
  return src.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(?<![:"'`])\/\/[^\n]*/g, "");
}

/** The hero `<section>` of the landing page — the only place the demo grid lives. */
function heroSection(src: string): string {
  const start = src.indexOf("hero — the product IS the first screen");
  assert.ok(start > 0, `${PAGE} no longer carries the hero section anchor — the assertions below would read nothing`);
  const end = src.indexOf("</section>", start);
  assert.ok(end > start, `the hero <section> in ${PAGE} is not closed — cannot scope the grid assertion`);
  return src.slice(start, end);
}

const page = read(PAGE);
const heroForm = read(HERO_FORM);
const heroCode = stripComments(heroSection(page));
const formCode = stripComments(heroForm);

test("the harness reads the real hero, not an empty page", () => {
  assert.ok(existsSync(fileURLToPath(new URL(PAGE, import.meta.url))), `${PAGE} must exist`);
  assert.ok(existsSync(fileURLToPath(new URL(HERO_FORM, import.meta.url))), `${HERO_FORM} must exist`);
  assert.ok(
    page.includes("<HeroOrderForm />"),
    `the landing page no longer renders <HeroOrderForm /> — the column this file pins would hold something else`,
  );
  assert.ok(
    formCode.includes("Raya Collection 2026"),
    `the hero form no longer renders its demo card — the row/arithmetic assertions would read nothing`,
  );
});

test("the demo column is a fixed 380px from lg up, not a 50/50 split", () => {
  const grid = heroCode.match(/<div className="([^"]*\bgrid\b[^"]*)"/);
  assert.ok(grid, `no grid container could be read from the hero section of ${PAGE}`);
  const cls = grid![1];
  assert.ok(
    /\blg:grid-cols-\[minmax\(0,1fr\)_380px\]/.test(cls),
    `the hero grid is "${cls}" — the demo column must stay the measured ` +
      `lg:grid-cols-[minmax(0,1fr)_380px]. 380px is the floor of a measured curve ` +
      `(see this file's header): 420 makes the card too large, 360 makes it taller instead.`,
  );
  assert.ok(
    !/\blg:grid-cols-2\b/.test(cls),
    `the hero grid is back to "lg:grid-cols-2" (two equal 524px columns) — that is the layout ` +
      `this card measured as 449k px2 of card in a 1017px hero`,
  );
});

test("the demo's money is derived from its two rows, so its totals stay true", () => {
  // The row list: one `{ thumb: … }` entry per product line.
  const rows = [...formCode.matchAll(/^\s*\{\s*thumb:/gm)];
  assert.equal(
    rows.length,
    2,
    `the demo renders ${rows.length} product row(s). Its figures are two rows of RM89.00 = ` +
      `RM178.00 + RM8.00 shipping = RM186.00: dropping or adding a row without re-deriving ` +
      `them makes the demo contradict its own total.`,
  );

  const price = formCode.match(/const PRICE_SEN = (\d+);/);
  const shipping = formCode.match(/const SHIPPING_SEN = (\d+);/);
  assert.ok(price, `PRICE_SEN is not declared as an integer sen constant in ${HERO_FORM}`);
  assert.ok(shipping, `SHIPPING_SEN is not declared as an integer sen constant in ${HERO_FORM}`);
  assert.equal(Number(price![1]), 8900, "each row is RM89.00 = 8900 sen");
  assert.equal(Number(shipping![1]), 800, "shipping is RM8.00 = 800 sen");

  // What the card must read at rest, computed from those constants and the rows.
  const subtotalSen = Number(price![1]) * rows.length;
  const totalSen = subtotalSen + Number(shipping![1]);
  assert.equal(formatMoney(subtotalSen), "RM178.00", "subtotal at qty 1 per row");
  assert.equal(formatMoney(totalSen), "RM186.00", "total including shipping");

  // And the component renders those DERIVED values — a hardcoded "RM186.00" would
  // pass the arithmetic above while the button said something else.
  for (const expr of ["formatMoney(PRICE_SEN * qty[i])", "formatMoney(subtotalSen)", "formatMoney(totalSen)"]) {
    assert.ok(
      formCode.includes(expr),
      `${HERO_FORM} no longer renders ${expr} — the figures must come from the rows, not a literal`,
    );
  }

  // No money literal anywhere in the rendered code (a trimmed variant hardcoded
  // the subtotal and total, which is how it contradicted itself).
  assert.ok(
    !/["'`]RM\s?\d/.test(formCode),
    `a literal "RM…" string is back in ${HERO_FORM} — money must be rendered through formatMoney`,
  );
  for (const literal of [subtotalSen, totalSen]) {
    assert.ok(
      !new RegExp(`(?<![\\d.])${literal}(?![\\d.])`).test(formCode),
      `${literal} sen is written as a literal in ${HERO_FORM} — the demo's figures must be derived from ` +
        `PRICE_SEN, SHIPPING_SEN and the row list`,
    );
  }
});

test("the arithmetic check BITES: a one-row card would not read RM186.00", () => {
  // The fixture the card's own review caught (a trimmed variant kept the figures
  // and deleted a row). Recomputing the same rule for it must NOT produce the
  // figures the demo prints — otherwise the assertion above is vacuous.
  const oneRowSubtotal = 8900 * 1;
  const oneRowTotal = oneRowSubtotal + 800;
  assert.equal(formatMoney(oneRowSubtotal), "RM89.00");
  assert.equal(formatMoney(oneRowTotal), "RM97.00");
  assert.notEqual(formatMoney(oneRowTotal), "RM186.00", "the one-row fixture must disagree with the card's total");
  assert.notEqual(formatMoney(oneRowSubtotal), "RM178.00", "the one-row fixture must disagree with the card's subtotal");
});

test("the card is not shrunk with transform: scale()", () => {
  // Measured: 380px + scale(.9) -> visual 342x705 but the hero stayed 943px,
  // because a transform leaves the layout box in place. It also makes the demo's
  // text smaller, which is the opposite of what a "beautiful order form" hero is
  // selling. Narrowing the column is the fix; scaling the paint is not.
  assert.ok(
    !/\bscale[-(]/.test(formCode),
    `${HERO_FORM} scales the demo — a transform shrinks the paint and leaves the layout box, so the ` +
      `hero does not shrink and the demo's text gets smaller`,
  );
});
