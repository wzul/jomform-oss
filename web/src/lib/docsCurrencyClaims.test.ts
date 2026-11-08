// Guard for card t_ae023ffb: /docs/sales told the reader the per-form counter
// card shows "revenue **in RM** net of refunds" — a currency claim the product
// stopped making in v1.22.73 (issue #185).
//
// THE DEFECT (measured on prod 2026-11-01, api+web v1.22.73 @ ff7c25f2). The
// per-form card renders `formatCurrencyTotals(f.revenue_by_currency,
// summary?.currency)`, so a USD workspace reads `US$24.90` (mixed fixture
// `US$24.90 + RM10.00`). The same page already carried the corrected CSV
// sentence ("…so a USD row reads USD + 24.90 rather than an RM label it was
// never charged in"), and /docs/workspaces already stated the per-currency rule
// — so /docs/sales contradicted itself, and a reader of that sentence would
// believe a USD sale is reported to them as RM.
//
// WHAT THIS PINS (each is a way the false claim comes back)
//   1. The CLAIM SHAPE is gone from every /docs page: `in RM` / `RM net of`, the
//      wording that denies a money figure can be in any other currency. The
//      legitimate MYR EXAMPLE is deliberately NOT matched ("RM24.90", "RM10.00",
//      "RM {{amount}}"), so the guard cannot be satisfied by deleting the
//      currency words from the guides.
//   2. The corrected sentence is present and carries BOTH halves: revenue is
//      still described as net of refunds, AND it is labelled in the currency the
//      sale was recorded in. Deleting the paragraph instead of fixing it fails —
//      otherwise "make the claim disappear" is a passing fix.
//   3. The scan is not vacuous: it reads the whole docs tree (a floor on the
//      page count), the analytics section it slices really is the per-form
//      counter's, and the sales guide still carries its USD/MYR example pair —
//      so "fix" cannot mean "delete the currency words".
//
// HOW IT READS THE PAGES. node --test cannot load JSX, so the pages are read as
// SOURCE, through `codeOnly` (src/lib/jsxSource.ts). Comments are blanked before
// every assertion, so a comment that quotes the old wording — or one explaining
// this correction in future — can neither raise a finding nor hide one; the M4
// case in this guard's bite proof is exactly that plant, and it stays green.
//
// WHAT IT DOES NOT COVER
//   * That a reader SEES the corrected sentence: the built-HTML reachability
//     half is docsFacts (src/lib/docsFacts.ts, run by scripts/docsFacts.mjs) and
//     the rendered half is checked by driving the page in a browser (handoff
//     evidence), not here.
//   * Every other way of asserting a currency. `hasil (RM)` in the /docs/mcp
//     rows is OUT of this scan on purpose: those strings are the REGISTERED Go
//     descriptions (api/internal/mcpsrv/tools.go) byte for byte — pinned by
//     api/internal/mcpsrv/docs_mcp_page_description_test.go — and the tools
//     behind them still emit `revenue_rm` / `refunded_rm` with no currency field
//     (api/internal/mcpsrv/analytics.go). Fixing that changes the MCP wire
//     contract, not this page's prose; it is a separate card.
//
//   npm run test:unit
import { test } from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";

import { codeOnly, readSource, tsxFiles } from "./jsxSource.ts";

const DOCS_DIR = fileURLToPath(new URL("../app/docs/", import.meta.url));

// Every docs .tsx as RAW source, keyed by its path under the docs tree;
// codeOnly() is applied at each assertion so a comment never counts.
const pages = tsxFiles(DOCS_DIR).map((rel) => ({ rel, src: readSource(DOCS_DIR + rel) }));

// The wording that asserts a money figure is denominated in ringgit. Deliberately
// the PREPOSITION plus the code — "RM24.90" is a legitimate MYR example and must
// stay legal, which is why a bare /\bRM\b/ would be the wrong guard.
const CLAIM = /\bin RM\b|\bRM net of\b/;

const lineOf = (src: string, idx: number) => src.slice(0, idx).split("\n").length;

test("no /docs page claims a money figure is 'in RM'", () => {
  assert.ok(
    pages.length >= 15,
    `read only ${pages.length} docs source file(s) under ${DOCS_DIR} — the scan is not reading the docs tree`,
  );
  const findings: string[] = [];
  for (const { rel, src } of pages) {
    const code = codeOnly(src);
    for (const m of code.matchAll(new RegExp(CLAIM, "g"))) {
      const line = lineOf(code, m.index ?? 0);
      findings.push(`${rel}:${line}: ${code.split("\n")[line - 1].trim()}`);
    }
  }
  assert.deepEqual(
    findings,
    [],
    "these docs sentences claim a money figure is in RM. Since v1.22.73 (issue #185) the dashboard, the CSV " +
      "and the API label each amount in the currency its sale was recorded in, so the sentence is false for " +
      "every non-MYR workspace — say 'labelled in the currency each sale was recorded in' instead:\n  " +
      findings.join("\n  "),
  );
});

test("the /docs/sales per-form counter description keeps both facts and asserts no currency", () => {
  const sales = pages.find((p) => p.rel === "sales/page.tsx");
  if (!sales) throw new Error("sales/page.tsx is missing from the docs tree — the guard would pass vacuously");
  const code = codeOnly(sales.src);

  const start = code.indexOf('id: "analytics"');
  const end = code.indexOf('id: "refunds"');
  assert.ok(start > 0, 'sales/page.tsx declares no section `id: "analytics"` — the per-form counter section moved');
  assert.ok(end > start, "the analytics section has no following `id: \"refunds\"` section to bound it");
  const section = code.slice(start, end);
  // Non-vacuity: the slice really is the per-form counter section.
  assert.ok(
    section.includes("Paid+Confirmed"),
    "the slice between id analytics and id refunds no longer contains Paid+Confirmed — it is not the per-form counter section",
  );

  assert.ok(
    !CLAIM.test(section),
    "the per-form counter section claims its revenue is in RM again; the card labels it in the currency each " +
      "sale was recorded in, so the sentence is false for a USD workspace",
  );
  assert.ok(
    /revenue net of refunds/.test(section),
    "the per-form counter section lost 'revenue net of refunds' — the figure is still the NET paid+confirmed " +
      "total, and deleting the clause hides that instead of fixing the currency claim",
  );
  assert.ok(
    /currency each sale was recorded in|currency the sale was recorded in/.test(section),
    "the per-form counter section no longer says its revenue is labelled in the currency each sale was " +
      "recorded in — the currency claim was dropped rather than corrected, or the correction was reverted",
  );
});

test("the sales guide still shows the USD/MYR example pair, so the claim scan is not won by deletion", () => {
  // A guard that only forbids words is satisfiable by deleting every currency
  // word on the page. The guide shows the two labels side by side on purpose —
  // that example pair is what tells a reader a USD sale reads US$ and an MYR one
  // reads RM, so it must survive the correction.
  const sales = pages.find((p) => p.rel === "sales/page.tsx");
  if (!sales) throw new Error("sales/page.tsx is missing from the docs tree");
  assert.ok(
    /US\$24\.90 for a USD sale, RM24\.90 for an MYR one/.test(codeOnly(sales.src)),
    "sales/page.tsx no longer shows the 'US$24.90 for a USD sale, RM24.90 for an MYR one' example pair — the " +
      "currency explanation was stripped rather than corrected, which passes the claim scan above for the wrong reason",
  );
});
