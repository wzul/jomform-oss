// Card t_8064a965 — the dashboard now headlines what the merchant KEEPS
// (collected minus the buyer-borne processing fee) and names the fee beside it.
//
// Two things can silently rot here, and neither is visible in a type check:
//
//   1. The fee WORD. The receipt (`payments.SaleRef.FeeLine`) and the checkout
//      (api/internal/httpapi/formLang.go) already call the charge something in
//      each of the five locales. If the dashboard invents a second name, the
//      same charge reads as two different things on two surfaces. So the
//      dictionary is pinned against formLang.go, parsed from the Go source.
//   2. The RULE. The stat, the 14-day chart and the per-form panel must plot the
//      NET figure; plotting total_sen quietly restores the defect. Asserted
//      from the page and from the Go emitters this repo has no JSX runner for.
//
//   npm run test:unit
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT, LANGS } from "../components/i18nDict.ts";

const here = fileURLToPath(new URL(".", import.meta.url));
const page = readFileSync(here + "../app/dashboard-client.tsx", "utf8");
const formLang = readFileSync(here + "../../../api/internal/httpapi/formLang.go", "utf8");
const dashboardGo = readFileSync(here + "../../../api/internal/httpapi/rest_dashboard.go", "utf8");
const analyticsGo = readFileSync(here + "../../../api/internal/dbstore/analytics.go", "utf8");
const mcpHandlers = readFileSync(here + "../../../api/internal/mcpsrv/handlers.go", "utf8");

// The buyer-facing label per locale, read from the Go dictionary the checkout
// and the receipt use: `"ms": { … FeeLine: "Caj pemprosesan", … }`.
function goFeeLine(lang: string): string | undefined {
  const m = formLang.match(
    new RegExp(`\\n\\t"${lang}":\\s*\\{[^}]*?FeeLine:\\s*"([^"]+)"`),
  );
  return m?.[1];
}

test("the fee word is the buyer's own word in all five locales", () => {
  for (const lang of LANGS) {
    const want = goFeeLine(lang);
    assert.ok(want, `could not read the ${lang} FeeLine from api/internal/httpapi/formLang.go`);
    assert.equal(
      DICT["dash.fee"][lang],
      want,
      `dash.fee ${lang} = ${JSON.stringify(DICT["dash.fee"][lang])}, want the checkout/receipt wording ${JSON.stringify(want)}`,
    );
  }
});

test("the revenue stat headlines the net figure and names the fee", () => {
  // Strip comment lines: the page's comments discuss the bug.
  const code = page
    .split("\n")
    .filter((line) => !line.trim().startsWith("//"))
    .join("\n");
  assert.ok(
    code.includes("summary.net_revenue_by_currency ?? summary.revenue_by_currency"),
    "the revenue stat no longer reads the net figure (what the merchant keeps)",
  );
  assert.ok(
    code.includes('t("dash.collected")') && code.includes('t("dash.fee")'),
    "the revenue stat no longer names the collected amount and the fee behind it",
  );
  assert.ok(
    code.includes('href: "/docs/payment-fees#reporting"'),
    "the revenue figure lost its link to the rule that defines it",
  );
});

test("the 14-day chart plots total - fee, not the buyer's total", () => {
  const code = page
    .split("\n")
    .filter((line) => !line.trim().startsWith("//"))
    .join("\n");
  assert.ok(
    code.includes("s.total_sen - (s.fee_sen ?? 0)"),
    "the chart sums total_sen again — the buyer-borne fee is back on the trend line as income",
  );
});

test("the API emits the fee and the net, from one subtraction", () => {
  assert.match(dashboardGo, /"fees_sen"/, "summaryData no longer emits fees_sen");
  assert.match(dashboardGo, /"net_revenue_sen"/, "summaryData no longer emits net_revenue_sen");
  assert.match(
    dashboardGo,
    /netByCurrency\[cur\] \+= net - s\.FeeSen/,
    "summaryData no longer derives the per-currency net as collected - fee",
  );
  // Per-form rollup: the fee is read on the same paid rows and the net is the
  // one subtraction.
  assert.match(analyticsGo, /AS fees/, "FormAnalytics no longer reads fee_sen on the rollup");
  assert.match(
    analyticsGo,
    /fs\.NetRevenueSen = fs\.RevenueSen - fs\.FeesSen/,
    "FormAnalytics no longer derives net revenue as revenue - fees",
  );
  // MCP parity: an agent must receive the same two figures.
  assert.match(mcpHandlers, /NetRevenueSen/, "get_sales_summary dropped net_revenue_sen");
});
