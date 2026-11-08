// Regression guard for "/docs/api pans 11px at 320px with the browser font at 150%".
//
// THE BUG (measured, production build BUILD_ID zTqU4IpFRgQfHFRpOzRdj,
// Chromium 390x844 dsf2, root font 24px via CDP Page.setFontSizes, viewport
// 320px wide):
//
//   documentElement.scrollWidth 331 vs clientWidth 320  — an 11px sideways pan
//   only at 320px; the same legend fits at 360px and 390px.
//
//   The overflowing element was the Access-level legend — a <dl> whose rows are
//   <div className="flex gap-2"> with a non-shrinking <dt> label and a bare
//   <dd>. A flex item's default min-width is `auto`, i.e. its min-content size,
//   and at 18px (text-xs under a 24px root) the "write" row's <dd> — the label
//   plus the "Not every write route:" caveat rendered from
//   scopeOnlyWriteRoutes() — had a min-content width wider than the space left
//   by the <dt>. The row could not shrink, so the <dd> box ended at right 331
//   and dragged the document with it:
//
//     <dd>     right 331
//     <strong> "Not every write route:"  right 326
//
//   This is a DIFFERENT element class from the prose <code> tokens fixed for
//   the 390px reading and from the openapi.json prose link: with both of those
//   neutralised the legend still measured 331. The fix is min-w-0 + a break
//   guard on the <dd>, so the row shrinks and the caveat wraps.
//
// What a unit test can pin is the shape that carries the behaviour: every row in
// this legend lets its <dd> shrink AND break, its label stays a non-breaking
// <dt>, and the data that made the row wide is still rendered in full (so
// "shorten the legend" is not a green path). The pixel numbers were measured in
// a real browser against a production build and are re-checked in the PR.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { accessLevels, scopeOnlyWriteRoutes } from "./openapi.ts";

const PAGE = "../app/docs/api/page.tsx";
const page = readFileSync(fileURLToPath(new URL(PAGE, import.meta.url)), "utf8");

/** A class that permits a break inside a run with no whitespace in it. */
const BREAK_GUARD = /\[overflow-wrap:(?:anywhere|break-word)\]|\bbreak-words\b|\bbreak-all\b/;

/**
 * The Access-level legend's <dl>, by its own class string.
 *
 * Matched on the class rather than "the first <dl>" so that adding another
 * definition list to the page cannot silently move this guard onto it.
 */
const LEGEND_OPEN = '<dl className="mt-3 space-y-1 text-xs leading-5 text-slate-600">';
const legendBlocks = [...page.matchAll(/<dl\b[^>]*>[\s\S]*?<\/dl>/g)].map((m) => m[0]);
const legend = legendBlocks.filter((b) => b.startsWith(LEGEND_OPEN));

/** Every row template of the legend, as source text. */
const rows = legend.length === 1 ? [...legend[0].matchAll(/<div\b[^>]*>[\s\S]*?<\/div>/g)].map((m) => m[0]) : [];

/** The level a row is for, when the row is a single literal; "" for the map template. */
function levelOf(row: string) {
  const dt = row.match(/<dt\b[^>]*>([\s\S]*?)<\/dt>/)?.[1] ?? "";
  const literal = dt.replace(/^\{|\}$/g, "").trim();
  return /^[a-z]+$/.test(literal) && !literal.includes("level") ? literal : "(the accessLevels() row)";
}

function classOf(attrs: string) {
  return attrs.match(/className="([^"]*)"/)?.[1] ?? "";
}

test("the guard is looking at the real legend rows (it cannot pass vacuously)", () => {
  assert.equal(
    legend.length,
    1,
    `found ${legend.length} <dl> block(s) on ${PAGE} opening with the Access legend's class ` +
      `"${LEGEND_OPEN}" — the guard below is written against exactly one legend, so its shape ` +
      "changed and this test must be re-read before it is trusted",
  );
  assert.match(
    legend[0],
    /accessLevels\(\)\.map\(/,
    `the Access legend no longer renders its rows from accessLevels() — the guard below is ` +
      `written against the rendered rows, so a hand-written replacement needs re-reading`,
  );
  assert.ok(
    accessLevels().length >= 4,
    `accessLevels() returned ${accessLevels().length} level(s) — the page renders one row per ` +
      "level and the wrap assertion below would be passing over almost nothing",
  );
  // One row TEMPLATE in source, rendered once per access level.
  assert.equal(
    rows.length,
    1,
    `found ${rows.length} row template(s) inside the Access legend — expected exactly one, ` +
      "rendered per access level",
  );
  for (const row of rows) {
    assert.equal(
      (row.match(/<dt\b/g) ?? []).length,
      1,
      `a legend row does not carry exactly one <dt>: ${row.slice(0, 120)}`,
    );
    assert.equal(
      (row.match(/<dd\b/g) ?? []).length,
      1,
      `a legend row does not carry exactly one <dd>: ${row.slice(0, 120)}`,
    );
  }

  // The legend text is DATA, not a literal: the labels come from ACCESS_LABELS
  // and the caveat from the OpenAPI contract. That is what made the row long
  // enough to overflow, and it means a future route can widen it again.
  assert.match(
    legend[0],
    /ACCESS_LABELS\[level\]/,
    `the Access legend no longer renders its label from ACCESS_LABELS — if the label became a ` +
      `literal this guard is pinning the wrong text`,
  );
  assert.ok(
    scopeOnlyWriteRoutes().length > 0,
    "scopeOnlyWriteRoutes() is empty — the long \"Not every write route:\" caveat would no longer " +
      "render, so re-check whether the wrap guard is still load-bearing",
  );
});

test("every Access legend <dd> can shrink and lets an unbreakable run break", () => {
  const offenders: string[] = [];
  for (const row of rows) {
    const dtAttrs = row.match(/<dt\b([^>]*)>/)?.[1] ?? "";
    const ddAttrs = row.match(/<dd\b([^>]*)>/)?.[1] ?? "";
    const level = levelOf(row);
    const dtClass = classOf(dtAttrs);
    const ddClass = classOf(ddAttrs);

    // The label must stay on one line: it is short and it is the row's key.
    if (!/\bshrink-0\b/.test(dtClass)) {
      offenders.push(
        `the <dt> for "${level}" has className "${dtClass}" without shrink-0 — the label is the ` +
          "row's key and squeezing it is not how this row was allowed to fit",
      );
    }

    // A flex item's min-width defaults to `auto` (min-content). Without min-w-0
    // the <dd> cannot shrink below its longest run, which is exactly the 331px
    // row that panned the page at 320px / root 24px.
    if (!/\bmin-w-0\b/.test(ddClass)) {
      offenders.push(
        `the <dd> for "${level}" has className "${ddClass}" without min-w-0 — a flex item will ` +
          "not shrink below its min-content width, so the caveat floors the row wider than the " +
          "viewport (measured: <dd> right 331 in a 320px viewport at root 24px)",
      );
    }
    if (!BREAK_GUARD.test(ddClass)) {
      offenders.push(
        `the <dd> for "${level}" has className "${ddClass}" with no break guard — min-w-0 alone ` +
          "only shrinks the box; the text still overflows it unless a run may break " +
          "(overflow-wrap:anywhere / break-words)",
      );
    }
  }
  assert.deepEqual(
    offenders,
    [],
    `${offenders.length} Access legend row(s) on ${PAGE} cannot shrink and break:\n` +
      offenders.map((o) => `  - ${o}`).join("\n"),
  );
});

test("the legend text that needed the guard is still rendered in full", () => {
  // Shortening the label is the one way to go green that the defect report rules
  // out: the legend DEFINES what the access level means, so the fix is wrapping,
  // not truncation.
  //
  // Both assertions are anchored to the RENDER SITE (the ACCESS_LABELS entry,
  // the <strong> element), not to a bare substring: a comment that quotes the
  // text must not keep the guard green after the text itself is gone. M5 in the
  // PR's mutation battery does exactly that.
  assert.match(
    page,
    /write:\s*"readwrite key; manage\/admin role inside a \?workspace= scope"/,
    "the \"write\" access label no longer reads \"readwrite key; manage/admin role inside a " +
      "?workspace= scope\" — if it was shortened to make the row fit, that is not a fix; the " +
      "label is what tells the reader which key the row needs",
  );
  assert.match(
    page,
    /<strong>Not every write route:<\/strong>/,
    "the \"Not every write route:\" caveat is gone from the legend — it is rendered from the " +
      "contract (scopeOnlyWriteRoutes()) and is the reason the write row is the widest",
  );
});
