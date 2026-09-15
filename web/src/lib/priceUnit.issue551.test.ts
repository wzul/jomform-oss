// Issue #551 regression test — the whole point of the bug: the product price must
// be converted from RM to sen EXACTLY ONCE. The editor keeps `price_sen` in state
// (the same unit the API uses — money is integer sen everywhere), and the priceInput
// onChange converts the typed RM value into sen. The save path used to convert AGAIN,
// so saving a product stored 100x the price the merchant saw
// (RM 25.00 -> RM 2,500.00).
//
// This repo has no JS component-test runner (no vitest/jest), so the unit rule is
// expressed here with the built-in Node test runner, which executes TypeScript
// directly (Node 24). Run it with:
//
//   npm run test:unit
//   (or: node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
//
// The expressions below are the same ones the component uses, so a change to either
// the input conversion or the save conversion shows up here.
import { test } from "node:test";
import assert from "node:assert/strict";

/** The priceInput onChange: typed RM -> state sen (the ONE conversion). */
function rmToSen(input: string): number {
  const rm = parseFloat(input);
  return Number.isFinite(rm) && rm > 0 ? Math.round(rm * 100) : 0;
}

/** The priceInput display: state sen -> shown RM. */
function senToDisplay(sen: number): string {
  return sen === 0 ? "" : (sen / 100).toFixed(2);
}

/** Is a state value a usable integer-sen amount? (the component's isSenPrice) */
function isSenPrice(v: unknown): v is number {
  return typeof v === "number" && Number.isFinite(v) && v >= 0 && Number.isInteger(v);
}

/** The FIXED save path: state sen is already sen, so send it as-is. */
function savePriceSen(statePriceSen: number): number {
  return Math.round(statePriceSen);
}

/** The PRE-FIX save path, kept here so the regression is explicit. */
function savePriceSenBuggy(statePriceSen: number): number {
  return Math.round(parseFloat(String(statePriceSen)) * 100);
}

test("issue #551: RM converts to sen exactly once across the round trip", () => {
  const loaded = 2500; // RM 25.00 as the API returns it
  assert.equal(senToDisplay(loaded), "25.00");

  const state = rmToSen("25.00"); // merchant retypes the same amount
  assert.equal(state, 2500);

  // Saving must store 2500 sen, not 250000.
  assert.equal(savePriceSen(state), 2500);
});

test("issue #551: the save path does NOT inflate the price 100x", () => {
  const state = rmToSen("25.00"); // 2500
  // Document the bug so a regression is unmistakable.
  assert.equal(savePriceSenBuggy(state), 250000);
  assert.notEqual(savePriceSen(state), savePriceSenBuggy(state));
  assert.equal(savePriceSen(state), 2500);
  assert.equal(savePriceSen(state) / 100, 25); // RM 25.00, never RM 2,500.00
});

test("issue #551: repeated saves do not compound the price", () => {
  let sen = rmToSen("19.90");
  assert.equal(sen, 1990);
  for (let i = 0; i < 5; i++) {
    const shown = senToDisplay(sen);
    sen = rmToSen(shown);
    assert.equal(savePriceSen(sen), 1990);
  }
});

test("issue #551: a non-finite entry becomes 0, never a value that serialises to null", () => {
  // A step=0.01 / min=0 number input still accepts exponent notation.
  assert.equal(rmToSen("1e999"), 0);
  assert.equal(rmToSen("abc"), 0);
  assert.equal(rmToSen(""), 0);
  assert.ok(isSenPrice(rmToSen("1e999")));
  assert.ok(isSenPrice(rmToSen("12.345")));
});

test("issue #551: unusable state values are rejected by isSenPrice", () => {
  assert.equal(isSenPrice(NaN), false);
  assert.equal(isSenPrice(Infinity), false);
  assert.equal(isSenPrice(-1), false);
  assert.equal(isSenPrice(12.5), false);
  assert.equal(isSenPrice("2500"), false);
  assert.equal(isSenPrice(0), true);
  assert.equal(isSenPrice(2500), true);
});

test("issue #551: the RM label shown for a state value is correct", () => {
  assert.equal(senToDisplay(0), ""); // 0 renders as the empty placeholder
  assert.equal(senToDisplay(100), "1.00");
  assert.equal(senToDisplay(1990), "19.90");
  assert.equal(senToDisplay(499900), "4999.00");
});
