// Issue #598 — the dashboard's per-form counter: what it counts, and what the
// label says.
//
// THE BUG (reported): the per-form card rendered ONE label, "Submissions:", next
// to a figure that was `COUNT(sales.id)` for every form. A response-only form
// therefore read 0 forever (it writes `submissions`, never `sales`) — "a working
// counter reading zero" — and a payment form's count of ORDERS was called
// "Submissions".
//
// THE DECISION (pinned here, stated in the API's `FormStats.Submissions` and in
// src/lib/dashboardStats.ts): the figure is "entries recorded for this form" —
// the rows the form actually produced, read from the table it writes to. The
// mode is exclusive (the public submit path writes exactly one table), so the
// count is never a sum of both and cannot double count. The money figures stay
// their own separately named fields.
//
// WHAT THIS GUARD COVERS
//   1. The label follows the mode: response -> Submissions, payment -> Entries,
//      and an unknown/absent mode reads as payment (the load-bearing default).
//   2. The dashboard page actually calls the rule instead of the hardcoded key
//      that caused the defect, and its payload type carries `mode` — without
//      the field the data never reaches the label.
//   3. "Paid+Confirmed" is `paid_count`, NOT `paid_count + confirmed_count`.
//      `paid_count` is already the union of both statuses in the Go SQL and
//      `confirmed_count` is a subset of it, so the old sum counted every
//      confirmed sale twice.
//   4. The Go rule this mirrors — read from source, so the JS and the SQL
//      cannot drift apart silently (the same cross-language pattern
//      src/lib/salesRows.test.ts and src/lib/formMode.test.ts use).
//
// WHAT IT DOES NOT COVER (stated honestly): the rendered pixels. This repo has
// no JSX test runner (see src/lib/i18n.test.ts); the label/i18n completeness of
// the dictionary is covered by src/lib/i18n.test.ts.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT } from "../components/i18nDict.ts";
import { formEntriesCountLabel, paidConfirmedCount } from "./dashboardStats.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const dashboard = read("../app/dashboard-client.tsx");
const dict = read("../components/i18nDict.ts");
// The Go SQL this label describes. If the CASE or the paidSet predicate moves,
// the JS side must be re-read against it — that is what this pins.
const analyticsGo = read("../../../api/internal/dbstore/analytics.go");

test("a response form's counter is labelled Submissions", () => {
  assert.equal(formEntriesCountLabel("response"), "dash.submissions");
});

test("a payment form's counter is NOT labelled Submissions", () => {
  // The reported defect: a count of orders called "Submissions". The label is
  // Entries, and the assertion is the inequality so a rename cannot quietly
  // bring the defect back under a new key.
  assert.equal(formEntriesCountLabel("payment"), "dash.entries");
  assert.notEqual(formEntriesCountLabel("payment"), "dash.submissions");
});

test("an unknown or absent mode reads as a payment form", () => {
  // The load-bearing default, mirroring dbstore.FormMode: every pre-#171 form
  // has no mode key and IS a payment form whose figure counts sales. A naive
  // implementation that treated unknown as response would label those
  // "Submissions" — the reported bug — and would read the wrong table.
  for (const m of [undefined, null, "", "PAYMENT", "order", "RESPONSE"]) {
    assert.equal(
      formEntriesCountLabel(m),
      "dash.entries",
      `mode=${JSON.stringify(m)} must read as a payment form`,
    );
  }
});

test("both counter labels exist in all five locales", () => {
  for (const key of ["dash.submissions", "dash.entries"] as const) {
    const row = dict.split("\n").find((l) => l.includes(`"${key}":`));
    assert.ok(row, `${key} is missing from the dictionary`);
    for (const lang of ["en", "ms", "zh", "ta", "ar"]) {
      assert.match(row as string, new RegExp(`\\b${lang}:\\s*"`), `${key} has no ${lang} value`);
    }
  }
  // The two labels must be different words, or the mode branch is pointless.
  assert.notEqual(DICT["dash.entries"].en, DICT["dash.submissions"].en);
});

test("the dashboard labels the counter through the rule, not a hardcoded key", () => {
  // The defect was a hardcoded `t("dash.submissions")` for every form. This is
  // the mutation check: restoring that literal fails here by name.
  assert.match(
    dashboard,
    /t\(formEntriesCountLabel\(f\.mode\)\)/,
    "the per-form counter no longer takes its label from formEntriesCountLabel — a payment form is called Submissions again",
  );
  assert.doesNotMatch(
    dashboard,
    /t\("dash\.submissions"\)/,
    'dashboard-client.tsx hardcodes t("dash.submissions") again — that is the reported defect',
  );
});

test("the dashboard payload type carries the mode the label needs", () => {
  // Without `mode` on the type the field never reaches the rule and every form
  // falls back to the payment label.
  assert.match(dashboard, /mode\?:\s*string;/, "the FormStats type lost its mode field");
});

test("Paid+Confirmed counts a confirmed sale once, not twice", () => {
  // paid_count is the paid SET; confirmed_count is a SUBSET of it. The old
  // `paid_count + confirmed_count` reported 2 for a single confirmed sale.
  assert.equal(
    paidConfirmedCount({ paid_count: 1, confirmed_count: 1 }),
    1,
    "a sole confirmed sale must count once — paid_count already includes confirmed",
  );
  assert.equal(paidConfirmedCount({ paid_count: 3, confirmed_count: 2 }), 3);
  assert.equal(paidConfirmedCount(null), 0);
  assert.equal(paidConfirmedCount({}), 0);

  // And the page must use the rule, not the sum.
  assert.match(dashboard, /paidConfirmedCount\(f\)/);
  assert.doesNotMatch(
    dashboard,
    /f\.paid_count\s*\+\s*f\.confirmed_count/,
    "the dashboard adds confirmed_count onto paid_count again — every confirmed sale is counted twice",
  );
});

test("the Go SQL agrees with both rules (cross-language pin)", () => {
  // 1. paid_count is the UNION of the two statuses, and confirmed_count is the
  //    subset — which is exactly why summing them on the page double counts.
  assert.match(
    analyticsGo,
    /const paidSet = `\('paid','confirmed'\)`/,
    "the paid set changed — re-read paidConfirmedCount against it",
  );
  assert.match(
    analyticsGo,
    /COUNT\(s\.id\) FILTER \(WHERE s\.status IN `\+paidSet\+`\) AS paid_count/,
    "paid_count no longer counts the whole paid set — the Paid+Confirmed figure is wrong",
  );
  assert.match(
    analyticsGo,
    /COUNT\(s\.id\) FILTER \(WHERE s\.status = 'confirmed'\)\s+AS confirmed_cnt/,
    "confirmed_count is no longer the confirmed subset — the double-count reasoning above is stale",
  );

  // 2. The submissions figure still branches on the form's mode and reads the
  //    submissions table for a response form. If this CASE is deleted the
  //    response count silently returns to a sales count (issue #598/#604).
  //
  //    The predicate itself is now the shared `responseFormSQL` fragment so the
  //    rollup and dbstore.FormMode cannot drift (card t_827b0a78), so this pins
  //    the fragment AND that this CASE uses it.
  assert.match(
    analyticsGo,
    /const responseFormSQL = `\(f\.config->>'mode' = 'response'[\s\S]*?custom_html[\s\S]*?html[\s\S]*?<> ''\)`/,
    "responseFormSQL no longer carries both arms of dbstore.FormMode — the mode label and the entry count disagree with the Go rule the page branches on",
  );
  assert.match(
    analyticsGo,
    /CASE WHEN `\+responseFormSQL\+`\s*\n\s*THEN \(SELECT COUNT\(\*\) FROM submissions sub WHERE sub\.form_id = f\.id\)\s*\n\s*ELSE COUNT\(s\.id\) END\s+AS submissions/,
    "the submissions CASE is gone, no longer reads the `submissions` table for a response form, or no longer uses the shared mode predicate — the response count regresses to 0",
  );
  // 3. It must be ONE branch, never a sum of both tables (double counting).
  const submissionsExpr = analyticsGo.slice(
    analyticsGo.indexOf("AS submissions"),
  );
  assert.doesNotMatch(
    submissionsExpr.slice(0, 200),
    /\+\s*\(SELECT COUNT\(\*\) FROM submissions/,
    "the submissions figure adds the two tables together — a form with rows in both double counts",
  );

  // 4. The mode the label branches on is the same rule as dbstore.FormMode.
  assert.match(
    analyticsGo,
    /THEN 'response' ELSE 'payment' END\s+AS mode/,
    "the analytics payload no longer carries the mode, or its default is not 'payment'",
  );
});
