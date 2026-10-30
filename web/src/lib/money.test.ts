// t_71c67471 — /sales must label each amount in the currency its sale was
// recorded in, not a hardcoded "RM".
//
// THE BUG: `money()` was `RM${(sen/100).toLocaleString(...)}` and the REST row
// (`toSaleRow`) dropped `sales.currency`, so a workspace set to USD printed
// RM24.90 for a US$24.90 sale — the number right, the money wrong. The page
// could not have done better: the payload carried no currency at all.
//
// WHAT THIS GUARD COVERS
//   1. formatMoney: the required case — a row with currency "USD" and
//      total_sen 2490 renders "US$24.90"; MYR (and a missing currency) keeps the
//      exact string that shipped ("RM24.90"), so the default workspace does not
//      silently change; a currency with no settled symbol falls back to its ISO
//      code rather than to "RM".
//   2. formatTotals: amounts in different currencies are never added together
//      (that would be the silent conversion this product refuses to do).
//   3. The wire contract, pinned against the Go emitters the same way
//      src/lib/salesRows.test.ts pins its keys: `toSaleRow`/`saleOut` must emit
//      json:"currency" and fill it from the SALE (CurrencyOrMYR), not from the
//      workspace's current setting.
//   4. The page itself: no hardcoded `RM${` left, and EVERY money() call site
//      passes a currency argument. Asserted from source because this repo has no
//      JSX test runner (see src/lib/salesRows.ts for the same constraint).
//
// WHAT IT DOES NOT COVER (stated honestly): the rendered pixels. The page's
// strings are produced by formatMoney (unit-tested here); that the browser
// paints them is checked by driving /sales, not by this file.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DEFAULT_CURRENCY, currencySymbol, formatMoney, formatTotals, normaliseCurrency } from "./money.ts";

const here = fileURLToPath(new URL(".", import.meta.url));
const pageSource = readFileSync(here + "../app/sales/page.tsx", "utf8");
const restHandlers = readFileSync(here + "../../../api/internal/httpapi/rest_handlers.go", "utf8");
const mcpHandlers = readFileSync(here + "../../../api/internal/mcpsrv/handlers.go", "utf8");
const salesStore = readFileSync(here + "../../../api/internal/dbstore/sale.go", "utf8");

// THE REQUIRED BITE. This is the assertion the card names: a USD row with
// total_sen 2490 must render US$24.90. It fails on the pre-fix code by
// construction — there was no way to express a non-MYR amount at all, and the
// one formatter available printed "RM24.90".
test("a USD sale renders in US$, not RM", () => {
  assert.equal(formatMoney(2490, "USD"), "US$24.90");
  assert.notEqual(formatMoney(2490, "USD"), "RM24.90");
});

test("the default (and a missing) currency keeps the shipped MYR string", () => {
  // Byte-identical to the pre-fix output: the default workspace's page must not
  // change appearance because a currency parameter was added.
  assert.equal(formatMoney(2490), "RM24.90");
  assert.equal(formatMoney(2490, "MYR"), "RM24.90");
  assert.equal(formatMoney(2490, " myr "), "RM24.90");
  assert.equal(formatMoney(2490, ""), "RM24.90");
  assert.equal(formatMoney(2490, null), "RM24.90");
  assert.equal(DEFAULT_CURRENCY, "MYR");
});

test("each supported currency gets its own label", () => {
  assert.equal(formatMoney(2490, "SGD"), "S$24.90");
  assert.equal(formatMoney(2490, "EUR"), "€24.90");
  assert.equal(formatMoney(2490, "GBP"), "£24.90");
  assert.equal(formatMoney(2490, "AUD"), "A$24.90");
  assert.equal(formatMoney(2490, "usd"), "US$24.90");
  // A currency with no settled short symbol falls back to the ISO code — a
  // correct label, never a guess and never a silent "RM".
  assert.equal(formatMoney(2490, "XAF"), "XAF 24.90");
  assert.equal(formatMoney(2490, "AED"), "AED 24.90");
});

test("minor units: two decimals for every currency, incl. zero-decimal ones", () => {
  // Amounts are integer sen-style minor units (AGENTS.md). Printing ¥25 for a
  // row stored as 2490 would drop the unit the database actually holds.
  assert.equal(formatMoney(2490, "JPY"), "JP¥24.90");
  assert.equal(formatMoney(0, "JPY"), "JP¥0.00");
});

test("grouping, negatives and zero", () => {
  assert.equal(formatMoney(123456789, "MYR"), "RM1,234,567.89");
  assert.equal(formatMoney(0, "USD"), "US$0.00");
  assert.equal(formatMoney(-1250, "USD"), "-US$12.50");
  assert.equal(formatMoney(-1250), "-RM12.50");
  assert.equal(formatMoney(2490.4, "USD"), "US$24.90");
});

test("currencySymbol / normaliseCurrency are the single rule", () => {
  assert.equal(normaliseCurrency(undefined), "MYR");
  assert.equal(normaliseCurrency(" usd "), "USD");
  assert.equal(currencySymbol("USD"), "US$");
  assert.equal(currencySymbol(undefined), "RM");
  assert.equal(currencySymbol("THB"), "฿");
});

test("amounts in different currencies are never added together", () => {
  // The bulk-capture prompt adds up the pending authorizations. A workspace's
  // sales can span currencies (each keeps the one it was recorded in), so the
  // sum groups per currency instead of pretending 24.90 RM and 1.00 US$ are the
  // same number.
  assert.equal(
    formatTotals([
      { sen: 2490, currency: "MYR" },
      { sen: 100, currency: "USD" },
    ]),
    "RM24.90 + US$1.00",
  );
  // A uniform list reads exactly like one amount.
  assert.equal(
    formatTotals([
      { sen: 2490, currency: "USD" },
      { sen: 100, currency: "USD" },
    ]),
    "US$25.90",
  );
  // Rows with no currency are MYR (historical rows), and MYR comes first here
  // because it is the first currency seen.
  assert.equal(formatTotals([{ sen: 2490 }, { sen: 100, currency: "MYR" }]), "RM25.90");
  assert.equal(formatTotals([]), "");
});

// ── the wire contract, against the Go emitters ─────────────────────────────
test("the REST sale row emits currency, filled from the SALE", () => {
  // The key the page reads must be a key the Go struct emits, or the page
  // renders the MYR fallback for every row forever (the original defect).
  assert.match(restHandlers, /json:"currency,omitempty"/, "saleRow no longer emits `currency`");
  assert.match(
    restHandlers,
    /Currency:\s*s\.CurrencyOrMYR\(\)/,
    "toSaleRow no longer fills Currency from the sale record",
  );
  // And the rule it calls must read the SALE's column, never the workspace's
  // current setting: `sales.currency` is what the sale was paid in.
  assert.match(salesStore, /func \(s \*Sale\) CurrencyOrMYR\(\) string/, "the shared currency rule is gone");
  assert.match(salesStore, /s\.Currency/, "CurrencyOrMYR no longer reads the sale's own currency");
});

test("the MCP sale row carries the same currency (parity with REST)", () => {
  assert.match(mcpHandlers, /json:"currency,omitempty"/, "saleOut no longer emits `currency`");
  assert.match(
    mcpHandlers,
    /Currency:\s*sl\.CurrencyOrMYR\(\)/,
    "saleOutFromSale no longer fills Currency from the sale record",
  );
});

// ── the page's own source ───────────────────────────────────────────────────
// Every argument list of `money(...)` in the sales page, found with a balanced
// paren scan (the repo has no JSX runner, so this is a source assertion).
function moneyCalls(src: string): string[] {
  const out: string[] = [];
  const re = /(^|[^A-Za-z0-9_$.])money\(/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(src)) !== null) {
    const open = m.index + m[1].length + "money".length;
    let depth = 0;
    let i = open;
    for (; i < src.length; i++) {
      if (src[i] === "(") depth++;
      else if (src[i] === ")") {
        depth--;
        if (depth === 0) break;
      }
    }
    out.push(src.slice(open + 1, i));
  }
  return out;
}

test("the sales page no longer hardcodes the RM symbol", () => {
  assert.ok(!hasHardcodedRM(), "the page still formats money with a literal RM${…}");
});

function hasHardcodedRM(): boolean {
  // Strip comments so the guard reads code, not the explanation of the bug.
  const code = pageSource
    .split("\n")
    .filter((line) => !line.trim().startsWith("//"))
    .join("\n");
  return /`RM\$\{/.test(code) || /\bRM\$\{/.test(code);
}

test("every money() call site is given a currency", () => {
  const calls = moneyCalls(pageSource);
  assert.ok(calls.length >= 10, `found only ${calls.length} money() call sites — the guard is not reading the page`);
  const bad = calls.filter((args) => !/,\s*(s\.currency|detail\.currency|cur)\b/.test(args));
  assert.deepEqual(
    bad,
    [],
    "these money() call sites format without the row's currency, so a non-MYR sale " +
      "would read RM again: " + JSON.stringify(bad),
  );
});

test("the page reads the currency off the ROW, not off the workspace setting", () => {
  // currencyOf is the only rule, and it reads `s.currency` (the sale's own
  // column). Substituting a workspace-level value would relabel history the
  // moment a workspace switches currency — the exact defect this card fixes.
  assert.match(pageSource, /const currencyOf = \(s\?: Sale\) => normaliseCurrency\(s\?\.currency\)/);
  assert.ok(!/workspaceCurrency/.test(pageSource), "the page substituted a workspace-level currency for the row's");
  // And the row type declares the key, so TypeScript checks the call sites.
  assert.match(pageSource, /\n\s+currency\?: string;/, "the Sale type lost its currency field");
});

test("the prompts name the SALE's currency, not RM", () => {
  // Capture and refund ask for an amount in a unit; the unit is the sale's.
  assert.match(pageSource, /Capture amount in \$\{cur\}/);
  assert.match(pageSource, /Refund amount in \$\{cur\}/);
  assert.ok(!/Capture amount in RM/.test(pageSource), "the capture prompt still says RM");
  assert.ok(!/Refund amount in RM/.test(pageSource), "the refund prompt still says RM");
});
