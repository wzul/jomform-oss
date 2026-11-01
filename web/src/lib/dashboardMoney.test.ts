// Card t_06a16aeb — the dashboard's revenue figures, the 14-day chart and the
// recent-sales rows must be labelled in the currency the money was RECORDED in,
// not a hardcoded "RM".
//
// THE BUG: dashboard-client.tsx defined its own
//   const money = (sen: number) => `RM${(sen / 100).toLocaleString(...)}`
// and used it for the revenue stat, the chart tooltip and axis, the per-form and
// per-product revenue and the recent rows. The API had no currency to give it
// either: summary/form_analytics/product_analytics exposed one scalar per
// figure. So a workspace whose sales were recorded in USD (issue #185) showed
// "RM24.90" for a US$24.90 sale — the number right, the money wrong.
//
// WHAT THIS GUARD COVERS
//   1. formatCurrencyTotals: a per-currency total map renders ONE LABEL PER
//      CURRENCY ("RM24.90 + US$1.00"), never a sum across currencies under one
//      symbol; the shipped MYR string is byte-identical; an empty total is
//      labelled in the workspace's own currency, not RM.
//   2. The page's own source: no local `money()` helper, no literal `RM${`,
//      every aggregate reads a `*_by_currency` map, and every recent row passes
//      `s.currency` to formatMoney. Asserted from source because this repo has
//      no JSX test runner (see src/lib/salesRows.ts).
//   3. The wire contract, pinned against the Go emitters: the analytics structs
//      must emit `revenue_by_currency` / `paid_revenue_by_currency`, summaryData
//      must group by the SALE's currency, and BOTH CSV writers (REST and MCP)
//      must emit a `currency` column and no `_rm` header.
//
// WHAT IT DOES NOT COVER: the rendered pixels. That is checked by driving the
// dashboard in a browser (handoff evidence), not by this file.
//
//   npm run test:unit
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { formatCurrencyTotals, formatMoney } from "./money.ts";

const here = fileURLToPath(new URL(".", import.meta.url));
const page = readFileSync(here + "../app/dashboard-client.tsx", "utf8");
const analyticsStore = readFileSync(here + "../../../api/internal/dbstore/analytics.go", "utf8");
const dashboardGo = readFileSync(here + "../../../api/internal/httpapi/rest_dashboard.go", "utf8");
const exportGo = readFileSync(here + "../../../api/internal/httpapi/rest_export.go", "utf8");
const mcpExportGo = readFileSync(here + "../../../api/internal/mcpsrv/parity_issue396.go", "utf8");

// Strip `//` comment lines so a guard reads code, not the explanation of the bug.
const code = page
  .split("\n")
  .filter((line) => !line.trim().startsWith("//"))
  .join("\n");

// ── the formatter ───────────────────────────────────────────────────────────
test("a USD total renders in US$, not RM", () => {
  // The card's required case: US$24.90 recorded in a workspace set to USD.
  assert.equal(formatCurrencyTotals({ USD: 2490 }), "US$24.90");
  assert.notEqual(formatCurrencyTotals({ USD: 2490 }), "RM24.90");
});

test("the default workspace keeps the shipped MYR string", () => {
  // A currency parameter must not silently restyle the default workspace.
  assert.equal(formatCurrencyTotals({ MYR: 2490 }), "RM24.90");
  assert.equal(formatCurrencyTotals({ MYR: 2490 }), formatMoney(2490, "MYR"));
});

test("mixed currencies are listed, never added", () => {
  // 24.90 RM + 1.00 US$ is not 25.90 of anything. Order is biggest-first, so the
  // string does not depend on the JSON object's key order.
  assert.equal(formatCurrencyTotals({ MYR: 2490, USD: 100 }), "RM24.90 + US$1.00");
  assert.equal(formatCurrencyTotals({ USD: 100, MYR: 2490 }), "RM24.90 + US$1.00");
  assert.equal(formatCurrencyTotals({ EUR: 1000, USD: 2490 }), "US$24.90 + €10.00");
});

test("an empty total is labelled in the workspace's currency", () => {
  // A form with no sales yet has no currency to infer; the fallback comes from
  // the bundle's summary.currency (the scope's own setting), never a bare RM.
  assert.equal(formatCurrencyTotals({}, "USD"), "US$0.00");
  assert.equal(formatCurrencyTotals(undefined, "USD"), "US$0.00");
  assert.equal(formatCurrencyTotals(null, "USD"), "US$0.00");
  assert.equal(formatCurrencyTotals(undefined), "RM0.00");
  // A currency that only holds unpaid sales keeps its entry at 0, so the label
  // still names the right money.
  assert.equal(formatCurrencyTotals({ USD: 0 }), "US$0.00");
});

// ── the page's own source ───────────────────────────────────────────────────
test("the dashboard no longer formats money with a literal RM", () => {
  assert.ok(!/`RM\$\{/.test(code), "the page still contains a literal RM${…} template");
  assert.ok(
    !/const money = \(sen: number\)/.test(code),
    "the page still defines its own money() helper instead of using lib/money",
  );
  // The chart's Y axis used to print `RM${v}` regardless of the currency.
  assert.ok(!/tickFormatter=\{\(v\) => `RM/.test(code), "the chart axis still hardcodes RM");
});

test("every aggregate is formatted from a per-currency map", () => {
  // Each of the four aggregate call sites (revenue stat, per-form revenue,
  // per-product paid revenue, per-product gross) must read the map, not the
  // cross-currency scalar.
  const sites = [
    "formatCurrencyTotals(summary.revenue_by_currency, summary.currency)",
    "formatCurrencyTotals(f.revenue_by_currency, summary?.currency)",
    "formatCurrencyTotals(pr.paid_revenue_by_currency, summary?.currency)",
    "formatCurrencyTotals(pr.revenue_by_currency, summary?.currency)",
  ];
  for (const site of sites) {
    assert.ok(code.includes(site), `the dashboard no longer formats an aggregate as \`${site}\``);
  }
  assert.ok(
    !/money\(summary\.revenue_sen\)|money\(f\.revenue_sen\)|money\(pr\.(paid_)?revenue_sen\)/.test(code),
    "an aggregate is still rendered from the cross-currency scalar",
  );
});

test("every recent-sales row passes the row's own currency", () => {
  const rowCalls = code.match(/formatMoney\(s\.total_sen, s\.currency\)/g) ?? [];
  // Two render paths: the mobile card list and the desktop table.
  assert.equal(rowCalls.length, 2, `found ${rowCalls.length} per-row formatMoney call(s), want 2 (mobile + desktop)`);
  assert.ok(!/money\(s\.total_sen\)/.test(code), "a recent row still formats without its currency");
  assert.match(code, /\n\s+currency\?: string;/, "the SaleRow type lost its currency field");
});

test("the 14-day chart keeps one band per currency", () => {
  // The chart used to sum every row into one `value` under one RM axis.
  assert.ok(code.includes("v_${cur}"), "the chart no longer keys its series by currency");
  assert.ok(code.includes("currencySymbol(bands[0])"), "the chart axis no longer uses the band's currency");
  assert.ok(!/dataKey="value"/.test(code), "the chart still plots one cross-currency value");
});

// ── the wire contract, against the Go emitters ──────────────────────────────
test("the API splits the aggregate money by the sale's currency", () => {
  assert.match(
    analyticsStore,
    /RevenueByCurrency map\[string\]int64 `json:"revenue_by_currency,omitempty"`/,
    "FormStats no longer emits revenue_by_currency",
  );
  assert.match(
    analyticsStore,
    /PaidRevenueByCurrency map\[string\]int64 `json:"paid_revenue_by_currency,omitempty"`/,
    "ProductStats no longer emits paid_revenue_by_currency",
  );
  // Grouped by the SALE's own currency, and MYR for a row written before the
  // column existed — never by the workspace's current setting.
  assert.match(analyticsStore, /s\.currency/, "the analytics split no longer reads the sale's currency column");
  assert.match(analyticsStore, /NormaliseCurrency\(/, "the analytics split no longer normalises the currency code");
  assert.match(
    dashboardGo,
    /revenueByCurrency\[s\.CurrencyOrMYR\(\)\]/,
    "summaryData no longer groups revenue by the sale's currency",
  );
});

test("both CSV writers name the currency and drop the _rm headers", () => {
  // Strip `//` comment lines: the Go files EXPLAIN the rename, so the old names
  // appear in prose by design. The guard reads the code.
  const codeOf = (src: string) =>
    src
      .split("\n")
      .filter((line) => !line.trim().startsWith("//"))
      .join("\n");
  for (const [name, src] of [
    ["REST", exportGo],
    ["MCP", mcpExportGo],
  ] as const) {
    const go = codeOf(src);
    assert.ok(
      !/total_rm|refunded_rm|outstanding_rm/.test(go),
      `the ${name} export still heads a money column with _rm`,
    );
    assert.ok(
      go.includes('"currency", "total"'),
      `the ${name} export has no currency/total header pair`,
    );
    assert.ok(
      /CurrencyOrMYR\(\)/.test(go),
      `the ${name} export does not write the sale's own currency into the row`,
    );
  }
});
