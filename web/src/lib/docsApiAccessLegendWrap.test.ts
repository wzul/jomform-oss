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
// WHAT A UNIT TEST CAN PIN is the shape that carries the behaviour: the row
// lets its text column shrink AND break, its bullet stays a non-breaking mark,
// and the data that made the row wide is still rendered in full (so "shorten
// the legend" is not a green path). The pixel numbers were measured in a real
// browser against a production build and are re-checked in the PR.
//
// SINCE THE PROSE RENDERER
//   The page is a `const SECTIONS` literal rendered by src/app/docs/docsProse,
//   so the legend is no longer a hand-written <dl>: it is the list block
//   `{ list: ACCESS_LEGEND }`, and the row a reader sees is the renderer's
//   <li className="flex gap-2.5"> — a non-shrinking bullet plus a text column
//   that renders every token through <Inline>. The split follows the fact:
//   the page half asserts the legend is still ACCESS_LABELS + the contract's
//   scope-only caveat and that it ROUTES through the renderer; the renderer
//   half asserts the row's shrink/break shape, which now exists exactly once,
//   in docsProse.tsx.
//
//   The <dd>'s min-w-0 has no counterpart any more: the text column is an
//   inline-flow <span> whose longest runs are the route tokens, and those are
//   rendered as prose <code> with `[overflow-wrap:anywhere]` — which drops
//   their min-content contribution to one character. That is what the wrap
//   assertion below pins: if a token stops being breakable the row is floored
//   again, exactly as the <dd> was.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { accessLevels, scopeOnlyWriteRoutes } from "./openapi.ts";
import { pageProse, sectionsOf, usesDocsProse } from "./docsProseReader.ts";

// Card t_0356ed4c: the Access-level legend moved to /docs/api/endpoints when
// /docs/api was split by task; this guard reads the page that RENDERS the legend.
const PAGE = "../app/docs/api/endpoints/page.tsx";
const RENDERER = "../app/docs/docsProse.tsx";
const page = readFileSync(fileURLToPath(new URL(PAGE, import.meta.url)), "utf8");
const renderer = readFileSync(fileURLToPath(new URL(RENDERER, import.meta.url)), "utf8");

/** A class that permits a break inside a run with no whitespace in it. */
const BREAK_GUARD = /\[overflow-wrap:(?:anywhere|break-word)\]|\bbreak-words\b|\bbreak-all\b/;

/** The one render site on the page that produces the Access-level legend. */
const LEGEND_RENDER = "{ list: ACCESS_LEGEND }";

/**
 * A slice of a source file, from `startMarker` to `endMarker`; "" when either
 * marker is missing. Callers assert the slice is non-empty, so a rename shows
 * up as a failed assertion with the marker's name rather than as an empty scan
 * that passes over nothing.
 */
function between(source: string, startMarker: string, endMarker: string): string {
  const start = source.indexOf(startMarker);
  if (start < 0) return "";
  const end = source.indexOf(endMarker, start + startMarker.length);
  return end < 0 ? "" : source.slice(start, end);
}

/** The renderer's bullet-list branch, and the row template inside it. */
const listBranch = between(renderer, 'if ("list" in b)', 'if ("steps" in b)');
const rows = [...listBranch.matchAll(/<li\b[\s\S]*?<\/li>/g)].map((m) => m[0]);
/** The list container itself: `overflow-wrap` inherits, so it may carry the guard. */
const listOpen = listBranch.match(/<ul\b[\s\S]*?>/)?.[0] ?? "";

/** The two halves of a row: the bullet mark and the text column. */
function bulletOf(row: string): string {
  return row.match(/<span aria-hidden="true"[^>]*>/)?.[0] ?? "";
}
function textColumnOf(row: string): string {
  // The last <span> of the row is the one wrapping the Inline tokens.
  const spans = [...row.matchAll(/<span\b[^>]*>/g)].map((m) => m[0]);
  return spans[spans.length - 1] ?? "";
}
function classOf(tag: string) {
  return tag.match(/className="([^"]*)"/)?.[1] ?? "";
}

test("the guard is looking at the real legend rows (it cannot pass vacuously)", () => {
  // The page renders through the prose renderer, and the legend is the list
  // block inside the Endpoints section — one block, rendered once.
  assert.ok(
    usesDocsProse(page),
    `${PAGE} no longer renders through src/app/docs/docsProse — this guard reads the legend ` +
      "row out of the renderer, so it must know the page still uses it",
  );
  assert.ok(
    sectionsOf(page, PAGE).some((s) => s.id === "endpoints"),
    `${PAGE} has no "endpoints" section any more — the access legend lived there and this ` +
      "guard is reading a page that moved its ground",
  );
  const legendRenderSites = page.split(LEGEND_RENDER).length - 1;
  assert.equal(
    legendRenderSites,
    1,
    `found ${legendRenderSites} "${LEGEND_RENDER}" site(s) on ${PAGE} — expected exactly one ` +
      "list block for the Access legend, so the assertions below are aimed at a moved target",
  );
  assert.match(
    page,
    /const ACCESS_LEGEND: string\[\] = accessLevels\(\)\.map\(/,
    "the Access legend no longer renders its rows from accessLevels() — the guard below is " +
      "written against the rendered rows, so a hand-written replacement needs re-reading",
  );
  const levels = accessLevels();
  assert.ok(
    levels.length >= 4,
    `accessLevels() returned ${levels.length} level(s) — the page renders one row per level ` +
      "and the wrap assertion below would be passing over almost nothing",
  );
  // One row PER LEVEL still: the legend's label map must define every level, or
  // a row renders with an empty definition.
  for (const level of levels) {
    assert.match(
      page,
      new RegExp(`\\b${level}:\\s*"[^"]+"`),
      `accessLevels() returns "${level}" but ACCESS_LABELS has no label for it — one legend row ` +
        "would render with nothing after the level name",
    );
  }

  // The legend text is DATA, not a literal: the labels come from ACCESS_LABELS
  // and the caveat from the OpenAPI contract. That is what made the row long
  // enough to overflow, and it means a future route can widen it again.
  assert.match(
    page,
    /ACCESS_LABELS\[level\]/,
    "the Access legend no longer renders its label from ACCESS_LABELS — if the label became a " +
      "literal this guard is pinning the wrong text",
  );
  assert.ok(
    scopeOnlyWriteRoutes().length > 0,
    'scopeOnlyWriteRoutes() is empty — the long "Not every write route:" caveat would no longer ' +
      "render, so re-check whether the wrap guard is still load-bearing",
  );

  // And the render site really exists: exactly one <li> template in the list
  // branch, carrying exactly one bullet and one text column.
  assert.equal(
    rows.length,
    1,
    `found ${rows.length} <li> template(s) in docsProse.tsx's list branch — expected exactly ` +
      "one, rendered once per legend entry",
  );
  if (rows.length === 1) {
    assert.equal(
      (rows[0].match(/<li\b/g) ?? []).length,
      1,
      `the list row template is not a single <li>: ${rows[0].slice(0, 200)}`,
    );
    assert.ok(
      bulletOf(rows[0]) !== "",
      `the list row carries no aria-hidden bullet mark — the legend row lost the mark that ` +
        "tells a reader a new level starts",
    );
    assert.match(
      rows[0],
      /<Inline text=\{item\} \/>/,
      "the list row no longer renders its text through <Inline> — the tokens that made the " +
        "legend wide (the route paths) would stop being rendered as breakable prose <code>",
    );
  }
});

test("every Access legend row lets an unbreakable run break", () => {
  // The row is the renderer's <li>: a flex row with a short bullet and a text
  // column. For the row to fit at 320px / root 24px, the mark must not shrink
  // (it is the row's key) and everything the text column renders must be able to
  // break — the route tokens are `GET /v1/rest/...` with no spaces in them.
  const offenders: string[] = [];
  for (const row of rows) {
    const bullet = bulletOf(row);
    const bulletClass = classOf(bullet);
    const textColumn = textColumnOf(row);

    if (!/\bshrink-0\b/.test(bulletClass)) {
      offenders.push(
        `the row's bullet <span> has className "${bulletClass}" without shrink-0 — the mark is ` +
          "the row's key and squeezing it is not how this row was allowed to fit",
      );
    }

    // The text column must not be forced onto one line: whitespace-nowrap or an
    // intrinsic width on it floors the row wider than the viewport, which is the
    // 331px pan this guard exists for.
    if (/whitespace-nowrap|\bw-max\b|\bmin-w-\[/.test(classOf(textColumn))) {
      offenders.push(
        `the row's text column has className "${classOf(textColumn)}" — an intrinsic width or ` +
          "nowrap keeps the row wider than the viewport instead of letting the caveat wrap",
      );
    }
    // And the caveat's own runs must be able to break. `overflow-wrap` inherits,
    // so the list CONTAINER can carry it for the row — that is where the renderer
    // puts it (the <p>/<ul>/<ol> prose containers) — but one of the row, the
    // column or the container must, or the route tokens floor the row exactly as
    // the <dd> did.
    const breakable =
      BREAK_GUARD.test(classOf(textColumn)) ||
      BREAK_GUARD.test(classOf(row.match(/<li\b[^>]*>/)?.[0] ?? "")) ||
      BREAK_GUARD.test(classOf(listOpen));
    if (!breakable) {
      offenders.push(
        `no break guard on the row, its text column, or the list container (` +
          `li="${classOf(row.match(/<li\b[^>]*>/)?.[0] ?? "")}", ` +
          `container="${classOf(listOpen)}") — the caveat's route tokens are unbreakable runs, ` +
          "so the row floors the page wider than the viewport (measured: <dd> right 331 in a " +
          "320px viewport at root 24px)",
      );
    }
    if (!/<Inline text=\{item\} \/>/.test(row)) {
      offenders.push(
        "the row does not render its text through <Inline>, so the inline <code> tokens in the " +
          "caveat are not the break-guarded sites asserted below",
      );
    }
  }
  assert.deepEqual(
    offenders,
    [],
    `${offenders.length} Access legend row(s) on ${PAGE} cannot shrink and break:\n` +
      offenders.map((o) => `  - ${o}`).join("\n"),
  );

  // The break guard itself: the prose <code> every inline token renders as. This
  // is the site that replaced the <dd>'s min-w-0 + break guard — `anywhere` also
  // drops the element's min-content width to one character, so a 34-character
  // route token shrinks inside the column instead of flooring it.
  const inlineCode = renderer.match(/<code\b[^>]*>/)?.[0] ?? "";
  assert.ok(
    inlineCode !== "",
    "docsProse.tsx no longer renders an inline <code> element — the legend's route tokens have " +
      "no break-guarded render site",
  );
  assert.match(
    classOf(inlineCode),
    BREAK_GUARD,
    `the inline <code> has className "${classOf(inlineCode)}" with no break guard — a route ` +
      "token's width is measured in the page's font, not in a fixed number of pixels, so at a " +
      "raised root font size the legend row floors the page again (measured: <dd> right 331 in " +
      "a 320px viewport at root 24px)",
  );
});

test("the legend text that needed the guard is still rendered in full", () => {
  // Shortening the label is the one way to go green that the defect report rules
  // out: the legend DEFINES what the access level means, so the fix is wrapping,
  // not truncation.
  //
  // Both assertions are anchored to the RENDER SITE (the ACCESS_LABELS entry,
  // the caveat template), not to a bare substring: a comment that quotes the
  // text must not keep the guard green after the text itself is gone. M5 in the
  // PR's mutation battery does exactly that.
  assert.match(
    page,
    /write:\s*"readwrite key; manage\/admin role inside a \?workspace= scope"/,
    'the "write" access label no longer reads "readwrite key; manage/admin role inside a ' +
      '?workspace= scope" — if it was shortened to make the row fit, that is not a fix; the ' +
      "label is what tells the reader which key the row needs",
  );
  assert.match(
    page,
    /\*\*Not every write route:\*\* \$\{routes\}/,
    'the "Not every write route:" caveat is gone from the legend — it is rendered from the ' +
      "contract (scopeOnlyWriteRoutes()) and is the reason the write row is the widest",
  );
  // And the caveat's route list still comes from the contract, in full: the
  // routes are printed as `METHOD /path`, which is the run that has no spaces.
  assert.match(
    page,
    /const routes = scopeOnlyWriteRoutes\(\)\s*\.map\(\(e\) => `\$\{e\.method\} \$\{e\.path\}`\)/,
    "the legend's caveat no longer prints the scope-only routes as `METHOD /path` from " +
      "scopeOnlyWriteRoutes() — the caveat would name nothing a reader can look up",
  );
  const routes = scopeOnlyWriteRoutes().map((e) => `${e.method} ${e.path}`);
  assert.ok(
    routes.length > 0,
    "scopeOnlyWriteRoutes() is empty — the caveat that made the write row the widest legend " +
      "row renders nothing, so re-check whether this guard is still load-bearing",
  );
  // And the text is visible to a reader, not only in the data: the label map and
  // the caveat are in the page, and the page's prose section that renders it is
  // the Endpoints section's list block (proved by LEGEND_RENDER above).
  assert.ok(
    pageProse(page, PAGE).length > 0,
    `${PAGE} has no readable prose — the reader can no longer read the legend this guard fits`,
  );
});
