// Guard: pay-what-you-want (type `custom_amount`) is reachable from the
// Products page, and it is created with LIMITS rather than a bogus asking price.
//
// THE BUG (card t_7eb3b623, measured on main 121c0fb8)
//   PR #1617 shipped custom_amount — migration 000085, REST + MCP create
//   support, and a buyer-side amount input — but the merchant UI could not
//   create one:
//
//     * products/page.tsx' add row offered only one_time + subscription;
//     * its inline edit dialog offered the same two;
//     * <option value="custom_amount"> existed ONLY in ProductEditor.tsx, on
//       /products/new and /products/<id>/edit — and `git grep 'products/new'`
//       across web/ returned NOTHING: no href, no router.push, no button.
//
//   So a merchant browsing Products could not create or convert a product to
//   pay-what-you-want without typing a URL, and a docs author verifying the
//   feature saw two types and wrote that the feature "is not in this tree".
//
//   Fixing it is not only an <option>. A custom_amount product has no asking
//   price (createProduct accepts a request with no price_sen for this type and
//   derives the display price from the limits), while the add row opened by
//   parsing a REQUIRED price — so the naive wiring would have created every
//   donation product at zero, or refused the request before it left the browser.
//
// WHAT THIS PINS
//   1. `customAmountBody` — the payload a custom_amount product is written with
//      — carries the three limits and NO `price_sen`. Money is integer sen, a
//      malformed amount never becomes 0, and a blank optional maximum stays
//      `null` (no cap) rather than 0 (a cap of nothing).
//   2. `limitsToInputs` — the edit dialog seeds from the stored limits and an
//      unset maximum/suggestion stays BLANK; a non-custom product seeds the
//      RM1.00 floor so switching it to custom_amount starts from a value the
//      server accepts.
//   3. The Products page itself: BOTH type selectors (add row and inline edit
//      dialog) offer all three types, both write paths build the custom payload
//      through the shared helper (so neither can grow its own price_sen again),
//      and the page links to /products/new and /products/<id>/edit — the routes
//      that were orphaned.
//
// WHAT IT DOES NOT COVER: the rendered pixels. That is checked by driving the
// built app in a browser (card evidence), not here — this file cannot see
// whether a control is wired to the state it claims.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { customAmountBody, limitsToInputs, parseRmToSen } from "./productAmounts.ts";

const PAGE = fileURLToPath(new URL("../app/products/page.tsx", import.meta.url));
const src = readFileSync(PAGE, "utf8");

/** The source of one function, from its declaration to the next top-level
 *  `function`/`export` at the same indent — enough to answer "does THIS write
 *  path build a custom payload" without reading the whole page. */
function functionBody(name: string): string {
  const start = src.indexOf(name);
  assert.notEqual(start, -1, `${name} is missing from products/page.tsx`);
  const rest = src.slice(start + name.length);
  const end = rest.search(/\n  (?:async )?function |\n  \/\/ ──|\n  return \(/);
  return end === -1 ? rest : rest.slice(0, end);
}

/** The <select> that starts at `anchor`, up to its closing tag. */
function selectBlock(anchor: string): string {
  const start = src.indexOf(anchor);
  assert.notEqual(start, -1, `no <select> anchored at ${anchor}`);
  const end = src.indexOf("</select>", start);
  assert.notEqual(end, -1, `select anchored at ${anchor} never closes`);
  return src.slice(start, end);
}

const ALL_THREE = ['value="one_time"', 'value="subscription"', 'value="custom_amount"'];

test("customAmountBody: limits in integer sen, and never a price_sen", () => {
  const parsed = customAmountBody("1.00", "", "");
  assert.equal(parsed.ok, true);
  if (!parsed.ok) return;
  assert.deepEqual(parsed.body, {
    min_price_sen: 100,
    max_price_sen: null,
    suggested_price_sen: null,
  });
  // The whole point: a custom_amount product has no asking price, so no
  // `price_sen` may ride along. This is the assertion the naive UI wiring
  // (price required, price_sen always sent) fails.
  assert.equal("price_sen" in parsed.body, false);
});

test("customAmountBody: rounding, fractional sen and the optional fields", () => {
  const parsed = customAmountBody("12.34", "50", "25.5");
  assert.equal(parsed.ok, true);
  if (!parsed.ok) return;
  assert.deepEqual(parsed.body, {
    min_price_sen: 1234,
    max_price_sen: 5000,
    suggested_price_sen: 2550,
  });
});

test("customAmountBody: a malformed amount is refused, never silently 0", () => {
  // Infinity serialises to null in JSON, which Go decodes into a non-pointer
  // int64 as 0 — the RM 0.00 product of issue #551.
  assert.deepEqual(customAmountBody("1e999", "", ""), { ok: false, field: "min" });
  assert.deepEqual(customAmountBody("RM10", "", ""), { ok: false, field: "min" });
  assert.deepEqual(customAmountBody("-1", "", ""), { ok: false, field: "min" });
  assert.deepEqual(customAmountBody("1.00", "nope", ""), { ok: false, field: "max" });
  assert.deepEqual(customAmountBody("1.00", "", "-2"), { ok: false, field: "suggested" });
  // The minimum is required by the server for this type: a blank one is not a
  // request worth making.
  assert.deepEqual(customAmountBody("", "", ""), { ok: false, field: "min" });
});

test("parseRmToSen: blank is 'not set', a number is sen, junk is refused", () => {
  assert.equal(parseRmToSen(""), null);
  assert.equal(parseRmToSen("   "), null);
  assert.equal(parseRmToSen("0"), 0);
  assert.equal(parseRmToSen("0.01"), 1);
  assert.equal(parseRmToSen("19.99"), 1999);
  assert.equal(parseRmToSen("1e999"), undefined);
  assert.equal(parseRmToSen("abc"), undefined);
});

test("limitsToInputs: seeds the floor for a non-custom product and blanks unset caps", () => {
  // A one_time product stores min_price_sen = 0, which the server would refuse
  // as a minimum — switching it to custom_amount must start at the floor.
  assert.deepEqual(
    limitsToInputs({ min_price_sen: 0, max_price_sen: null, suggested_price_sen: null }),
    { min: "1.00", max: "", suggested: "" },
  );
  assert.deepEqual(
    limitsToInputs({ min_price_sen: 500, max_price_sen: 2500, suggested_price_sen: 1000 }),
    { min: "5.00", max: "25.00", suggested: "10.00" },
  );
  // An absent document (the add row's starting point) is the same floor.
  assert.deepEqual(limitsToInputs({}), { min: "1.00", max: "", suggested: "" });
});

test("the add row's type selector offers pay-what-you-want", () => {
  const block = selectBlock('aria-label={t("prod.type")}');
  for (const opt of ALL_THREE) {
    assert.ok(block.includes(opt), `the add row type selector is missing ${opt}`);
  }
  assert.ok(block.includes('t("prod.customAmount")'), "the option is labelled from the dictionary");
});

test("the inline edit dialog's type selector offers pay-what-you-want", () => {
  const block = selectBlock("value={editForm.type}");
  for (const opt of ALL_THREE) {
    assert.ok(block.includes(opt), `the edit dialog type selector is missing ${opt}`);
  }
});

test("both write paths build the custom payload through the shared helper", () => {
  // If either path stops calling it, that path is back to sending a price_sen
  // (or nothing) for a product with no asking price.
  assert.ok(functionBody("async function add(").includes("customAmountBody("));
  assert.ok(functionBody("async function saveEdit(").includes("customAmountBody("));
  // The add row must collect the limits — the minimum is required server-side.
  assert.ok(
    src.includes('t("prod.minAmount"'),
    "the add row and the dialog must surface the minimum amount",
  );
});

test("the add row does not require a price for a pay-what-you-want product", () => {
  // The price input is `required`; leaving it mounted while the type is
  // custom_amount makes the browser refuse to submit a donation product.
  const addForm = src.slice(src.indexOf("<form onSubmit={add}"), src.indexOf("</form>", src.indexOf("<form onSubmit={add}")));
  assert.ok(
    /type === "custom_amount"\s*\?/.test(addForm),
    "the add row must branch on custom_amount instead of always rendering the price field",
  );
});

test("the product editor routes are reachable from the list", () => {
  // Before this fix `git grep 'products/new'` across web/ returned nothing:
  // the only page that could create a custom_amount product was orphaned.
  assert.match(src, /href="\/products\/new"/, "/products/new must be linked");
  assert.match(
    src,
    /href=\{`\/products\/\$\{[^}]+\}\/edit`\}/,
    "/products/<id>/edit must be linked from a row",
  );
});
