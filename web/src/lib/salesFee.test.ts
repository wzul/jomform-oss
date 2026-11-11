// Card t_4ca2da1f — /sales never showed the buyer-borne processing fee or what
// the merchant actually receives.
//
// THE DEFECT (measured on this tree against a private schema, FPX at 100 bp, one
// paid sale with subtotal 100000 / fee 1010 / total 101010): the row's JUMLAH cell
// read "RM1,010.10" and the detail drawer's totals block carried no fee line and
// no net figure, so a treasurer read what the BUYER paid as income when the
// merchant nets RM1,000.00. The sale record already held fee_sen and the frozen
// rate, and the buyer's receipt already printed the line; only the merchant's own
// dashboard could not see it.
//
// WHAT THIS GUARD COVERS
//   1. saleFee(): the arithmetic the page renders. total − fee in integer sen
//      (100000, never a re-derived percentage), null on a sale with no fee so
//      pre-fee rows are byte-identical, null on a shape it cannot price (a fee
//      with no total) rather than inventing a net.
//   2. feeLine(): the labelled fee line never leaves a dangling separator when a
//      sale carries no rate label.
//   3. THE WORDING PIN, across languages: the dictionary's word for the fee
//      equals the buyer checkout/receipt FeeLine in ALL FIVE locales, read from
//      api/internal/httpapi/formLang.go. The merchant and the buyer must not see
//      one charge called two things.
//   4. THE WIRE PIN: the REST row must emit `fee_rate_label` and fill it from
//      the sale's FROZEN snapshot via the receipt's own formatter
//      (payments.FeeRateLabel) — the page must never format basis points itself,
//      which is how the drawer and the receipt would drift. The MCP row
//      (list_sales) must carry the same fee/net, or an agent quotes the buyer's
//      total as income.
//   5. THE PAGE PIN: the page renders both dictionary keys, and the amount cell /
//      drawer read the server's label rather than recomputing a rate.
//
// WHAT IT DOES NOT COVER, stated plainly: the rendered pixels. That this really
// draws on /sales is checked by driving the built app in a browser, and the
// handoff quotes what was read out of the live DOM.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { feeLine, saleFee } from "./salesRows.ts";
import { formatMoney } from "./money.ts";
import { DICT } from "../components/i18nDict.ts";

const here = fileURLToPath(new URL(".", import.meta.url));
const pageSource = readFileSync(here + "../app/sales/page.tsx", "utf8");
const restHandlers = readFileSync(here + "../../../api/internal/httpapi/rest_handlers.go", "utf8");
const mcpHandlers = readFileSync(here + "../../../api/internal/mcpsrv/handlers.go", "utf8");
const formLang = readFileSync(here + "../../../api/internal/httpapi/formLang.go", "utf8");

// THE REQUIRED BITE. On the pre-fix page there was no net figure at all: a sale
// carrying a RM10.10 fee and a RM1,010.10 total renders net = RM1,000.00, and the
// row no longer claims the whole RM1,010.10 is the merchant's.
test("a fee-bearing sale nets total − fee, in integer sen", () => {
  const fee = saleFee({ fee_sen: 1010, total_sen: 101010, fee_rate_label: "FPX 1.00%" });
  assert.ok(fee, "saleFee returned null for a sale that carries a fee");
  assert.equal(fee.feeSen, 1010);
  assert.equal(fee.totalSen, 101010);
  assert.equal(fee.netSen, 100000);
  assert.equal(fee.rateLabel, "FPX 1.00%");
  // The two strings the page paints. Integer sen in, exact money out.
  assert.equal(formatMoney(fee.feeSen, "MYR"), "RM10.10");
  assert.equal(formatMoney(fee.netSen, "MYR"), "RM1,000.00");
  assert.notEqual(formatMoney(fee.netSen, "MYR"), formatMoney(fee.totalSen, "MYR"));
});

test("the sale's own currency labels the fee and the net (no hardcoded RM)", () => {
  const fee = saleFee({ fee_sen: 25, total_sen: 2525, fee_rate_label: "Card 2.00%" });
  assert.ok(fee);
  assert.equal(formatMoney(fee.feeSen, "USD"), "US$0.25");
  assert.equal(formatMoney(fee.netSen, "USD"), "US$25.00");
});

test("a sale with no fee renders no fee line — pre-fee rows are unchanged", () => {
  assert.equal(saleFee({ total_sen: 5000 }), null);
  assert.equal(saleFee({ fee_sen: 0, total_sen: 5000 }), null);
  assert.equal(saleFee(null), null);
  assert.equal(saleFee(undefined), null);
});

test("a fee with no total is not priced rather than guessed", () => {
  assert.equal(saleFee({ fee_sen: 1010 }), null);
  assert.equal(saleFee({ fee_sen: 1010, total_sen: Number.NaN }), null);
  assert.equal(saleFee({ fee_sen: -5, total_sen: 5000 }), null);
});

test("the fee line has no dangling separator without a rate label", () => {
  assert.equal(feeLine("Processing fee", "FPX 1.00%"), "Processing fee · FPX 1.00%");
  assert.equal(feeLine("Caj pemprosesan", "Card 2.00% + RM1.00"), "Caj pemprosesan · Card 2.00% + RM1.00");
  assert.equal(feeLine("Processing fee", ""), "Processing fee");
  assert.equal(feeLine("Processing fee", "   "), "Processing fee");
});

// ── The wording pin: one charge, one name, in every language ────────────────
//
// The buyer's checkout row and the receipt line come from formLang's FeeLine;
// the merchant's dashboard comes from DICT["sales.fee"]. Read the Go map, per
// locale, and require they are the SAME string. A reworded one side fails here.
function feeLineByLocale(): Record<string, string> {
  const blocks = [...formLang.matchAll(/\n\t"(en|ms|zh|ta|ar)":\s*\{/g)];
  const out: Record<string, string> = {};
  for (let i = 0; i < blocks.length; i++) {
    const lang = blocks[i][1];
    const start = blocks[i].index ?? 0;
    const end = i + 1 < blocks.length ? (blocks[i + 1].index ?? formLang.length) : formLang.length;
    const chunk = formLang.slice(start, end);
    const m = /FeeLine:\s*"([^"]*)"/.exec(chunk);
    if (m) out[lang] = m[1];
  }
  return out;
}

test("the dashboard's fee word is the buyer's own word in all five locales", () => {
  const go = feeLineByLocale();
  for (const lang of ["en", "ms", "zh", "ta", "ar"]) {
    assert.ok(go[lang], `formLang.go no longer declares FeeLine for ${lang}`);
    assert.equal(
      DICT["sales.fee"][lang as keyof (typeof DICT)["sales.fee"]],
      go[lang],
      `sales.fee in ${lang} drifted from the checkout/receipt FeeLine (${go[lang]})`,
    );
  }
  // The card's own wording: the Malay dashboard says "Caj pemprosesan", and the
  // net line is the phrase the card measured missing ("anda terima").
  assert.equal(DICT["sales.fee"].ms, "Caj pemprosesan");
  assert.equal(DICT["sales.feeNet"].ms, "Anda terima");
});

// ── The wire pin ─────────────────────────────────────────────────────────────

test("the REST sale row emits fee_rate_label from the receipt's own formatter", () => {
  assert.match(
    restHandlers,
    /FeeRateLabel\s+string\s+`json:"fee_rate_label,omitempty"`/,
    "saleRow no longer emits fee_rate_label — the drawer would have no rate to show",
  );
  assert.match(
    restHandlers,
    /row\.FeeRateLabel\s*=\s*payments\.FeeRateLabel\(s\.FeeMethod,\s*s\.FeeFixedSen,\s*s\.FeePercentBp\)/,
    "toSaleRow no longer builds the label with payments.FeeRateLabel over the sale's frozen snapshot",
  );
});

// The MCP parity rule: list_sales must carry what the dashboard now shows, or an
// agent reports the buyer's total as the merchant's income.
test("list_sales carries the fee, its frozen label and the net", () => {
  for (const field of ['FeeSen       int64  `json:"fee_sen,omitempty"`', 'FeeRateLabel string `json:"fee_rate_label,omitempty"`', 'NetSen       int64  `json:"net_sen"`']) {
    assert.ok(mcpHandlers.includes(field), `saleOut is missing ${field}`);
  }
  assert.match(mcpHandlers, /o\.NetSen = sl\.TotalSen - sl\.FeeSen/, "saleOutFromSale no longer derives net_sen from the sale");
});

// ── The page pin: the dashboard reads the numbers, it does not invent them ──

test("/sales renders the fee and the net, and never recomputes the rate", () => {
  assert.match(pageSource, /saleFee\(s\)/, "the sales rows no longer read the row's fee");
  assert.match(pageSource, /saleFee\(detail\)/, "the drawer no longer reads the sale's fee");
  assert.match(pageSource, /t\("sales\.fee"\)/, "the drawer/rows lost the fee label");
  assert.match(pageSource, /t\("sales\.feeNet"\)/, "the drawer lost the net label");
  assert.match(pageSource, /detailFee\.rateLabel/, "the drawer no longer shows the server's frozen rate label");
  // The rate is NEVER formatted in the browser: a percentage/basis-point
  // computation here is exactly how the drawer and the receipt drift apart.
  assert.doesNotMatch(pageSource, /fee_percent_bp|feePercentBp|basisPoint|\bbp\b/, "the page re-derives the fee rate — it must print the API's fee_rate_label");
  assert.doesNotMatch(pageSource, /fee_fixed_sen/, "the page re-derives the fixed fee — it must print the API's fee_rate_label");
});

// The drawer's row order matches the buyer's checkout breakdown: subtotal, then
// the fee, then the total. Asserted by SOURCE ORDER, because the order is the
// thing the card asked to keep and a reordering would still render.
test("the drawer orders subtotal → fee → total, then the net", () => {
  const block = pageSource.slice(pageSource.indexOf("{/* Totals */}"), pageSource.indexOf("{isPartiallyRefunded(detail) && ("));
  assert.ok(block.length > 0, "the totals block moved — this guard needs updating");
  const at = (needle: string) => {
    const i = block.indexOf(needle);
    assert.ok(i >= 0, `the totals block no longer contains ${needle}`);
    return i;
  };
  const subtotal = at('t("sales.subtotal")');
  const fee = at('t("sales.fee")');
  const total = at('t("sales.total")');
  const net = at('t("sales.feeNet")');
  assert.ok(subtotal < fee, "the fee line must come after the subtotal");
  assert.ok(fee < total, "the fee line must come before the total");
  assert.ok(total < net, "the net figure belongs after the total it is derived from");
});
