// Regression guard for "/docs/customize: the MCP tool table is clipped by overflow-hidden".
//
// THE DEFECT (measured in Chromium 320x844 dsf2, root font 16px — a plain phone
//   with NO text scaling — fresh browser state, lang=en, against a production
//   build of this tree; /docs/customize):
//
//     <table className="w-full text-left text-sm">   405.1px wide (left 25, right 430.1)
//     its wrapper <div className="mt-4 overflow-hidden ...">   270px wide  (scrollWidth 405)
//
//   `overflow: hidden` does not let the reader scroll — it CLIPS. The last
//   header ("Purpose") ended at right 430.1 inside a box ending at 295, so 135px
//   of every row was painted nowhere and reachable by nothing: no `overflow-x`
//   scroller existed on the path (the only scrollers on the page were the <pre>
//   code samples). The same page's affiliate REST table was worse — 526.9px of
//   table in the same 270px box, 257px unreachable — and at a 24px root font
//   (Chrome's "Very large", the text scaling a user turns on) ALL FIVE docs
//   tables clipped, including the three that fit at 16px:
//
//     /docs/customize     MCP tools      405.1 -> 607.6px   in 270 -> 246px box
//     /docs/customize     REST endpoints 526.9 -> 790.3px   in 270 -> 246px box
//     /docs/placeholders  recipients     270.0 -> 347.7px   in 270 -> 246px box
//     /docs/placeholders  subject/body   270.0 -> 395.3px   in 270 -> 246px box
//     /docs/embed         contract       270.0 -> 330.8px   in 270 -> 246px box
//
// THE FIX: the wrapper is the horizontal scroller (`overflow-x-auto`) instead of
//   the clipper (`overflow-hidden`), so a table wider than the column scrolls
//   inside its own box and every cell is reachable. A scroll container is also a
//   keyboard trap unless it is focusable: axe's `scrollable-region-focusable`
//   rule (serious) fails a scrollable region with no tab stop, so every wrapper
//   carries `tabIndex={0}`, `role="region"` and a per-page-unique `aria-label`
//   (a named region is announced instead of a bare "group", and unique names keep
//   axe's `landmark-unique` green). The rounded corners are unchanged: a scroll
//   container clips its descendants exactly like `overflow: hidden` did.
//
// WHY THIS FILE IS A SOURCE-CONTRACT TEST AND NOT A BROWSER TEST
//   This repo has no DOM/JSX unit runner (no vitest/jest — see
//   src/lib/docsA11y.test.ts and src/lib/docsApiProseWrap.test.ts, written the
//   same way), so a unit test cannot measure layout deformation. The pixel
//   numbers above were measured in a real browser against a production build and
//   are re-checked in the PR; what this file pins is the part a later edit can
//   silently undo — the wrapper class, the tab stop, the accessible name, and the
//   fact that the fix was not "achieved" by deleting a column.
//
//   The axe gate covers only HALF of it, and this is measured, not assumed. On
//   the built page with axe-core 4.13.0's DEFAULT rule set (what e2e/axeGate.ts
//   runs) and the gate's critical/serious bar: with the fix, 0 blockers; drop
//   `tabIndex={0}` and it reports `scrollable-region-focusable` (serious) on this
//   very box, so the gate fails. The PRE-FIX shape — `overflow:hidden`, nothing
//   focusable — reports **0**: a clipper is not a scrollable region, so axe never
//   saw the original bug. That gap is the reason the class rule has to live here.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { docsRoutesOnDisk, filesUnder } from "./docsRoutes.ts";

const DOCS_DIR = fileURLToPath(new URL("../app/docs", import.meta.url));

/** Every .tsx under app/docs — the pages AND the chrome they share. */
const SOURCES = filesUnder(DOCS_DIR).filter((f) => f.endsWith(".tsx"));

/**
 * The class tokens of one JSX opening tag.
 *
 * Read out of `className="..."` ONLY, so a comment that happens to mention
 * `overflow-hidden` cannot satisfy or break the rule.
 */
function classTokens(tag: string): string[] {
  const m = tag.match(/className="([^"]*)"/);
  return m ? m[1].split(/\s+/).filter(Boolean) : [];
}

/** A class token that turns a box into a horizontal scroller. */
const SCROLLS_X = new Set(["overflow-x-auto", "overflow-x-scroll"]);

type TableAt = {
  file: string;
  index: number;
  tableTag: string;
  wrapperTag: string;
  wrapperGap: string;
};

function tablesIn(file: string): TableAt[] {
  const src = readFileSync(file, "utf8");
  const out: TableAt[] = [];
  for (const m of src.matchAll(/<table\b[^>]*>/g)) {
    const index = m.index ?? 0;
    // The wrapper is the <div ...> that OPENS immediately before the table. The
    // gap between the two tags must be whitespace: if it is not, the table's
    // real parent is something else and the class we are about to check does not
    // belong to the element that decides whether the content scrolls.
    const divStart = src.lastIndexOf("<div", index);
    if (divStart === -1) {
      out.push({ file, index, tableTag: m[0], wrapperTag: "", wrapperGap: "no <div> before it" });
      continue;
    }
    const tagEnd = src.indexOf(">", divStart);
    out.push({
      file,
      index,
      tableTag: m[0],
      wrapperTag: src.slice(divStart, tagEnd + 1),
      wrapperGap: src.slice(tagEnd + 1, index),
    });
  }
  return out;
}

const TABLES = SOURCES.flatMap(tablesIn);
const rel = (f: string) => `app/docs/${f.slice(DOCS_DIR.length).replace(/^\//, "")}`;

test("the walk actually reads the docs tree (this rule cannot pass vacuously)", () => {
  const routes = docsRoutesOnDisk(DOCS_DIR);
  assert.ok(
    routes.length >= 11 && routes.includes("/docs/customize"),
    `docsRoutesOnDisk found ${routes.length} route(s) (${routes.join(", ")}) — the docs walk is broken, so nothing below proved anything`,
  );
  assert.ok(
    TABLES.length >= 5,
    `found ${TABLES.length} <table> under app/docs; the docs surface has 5 — a table that moved (or a scan that stopped reading it) is a coverage loss, not a pass`,
  );
});

test("no docs table is wrapped by a box that CLIPS it instead of scrolling it", () => {
  for (const t of TABLES) {
    const where = `${rel(t.file)} @${t.index} (${t.tableTag})`;
    assert.equal(
      t.wrapperGap.trim(),
      "",
      `${where}: the <div> before this table is not its direct parent, so this guard would be checking the wrong element`,
    );
    const tokens = classTokens(t.wrapperTag);
    assert.ok(
      !tokens.includes("overflow-hidden"),
      `${where}: the table's wrapper carries overflow-hidden. That is the t_7c3a76c0 defect — a 405.1px table in a 270px box at 320px/16px, Purpose column clipped with no way to reach it. Use overflow-x-auto instead.`,
    );
    assert.ok(
      tokens.some((c) => SCROLLS_X.has(c)),
      `${where}: the table's wrapper is neither overflow-x-auto nor overflow-x-scroll. A docs table is wider than a 320px column at a raised font size, so the wrapper has to be the horizontal scroller — otherwise the last column is unreachable.`,
    );
  }
});

test("every docs table scroller is keyboard reachable and announced (axe scrollable-region-focusable)", () => {
  for (const t of TABLES) {
    const where = `${rel(t.file)} @${t.index}`;
    assert.ok(
      /\btabIndex=\{0\}/.test(t.wrapperTag),
      `${where}: the scroll container has no tabIndex={0} — a sideways-scrolling box with no tab stop cannot be scrolled from the keyboard, and axe fails it with scrollable-region-focusable (serious).`,
    );
    assert.ok(
      /role="region"/.test(t.wrapperTag),
      `${where}: the scroll container carries no role="region" — a focusable <div> is announced as a bare generic, and axe's scrollable-region-focusable fix is a labelled region.`,
    );
    const label = t.wrapperTag.match(/aria-label="([^"]*)"/)?.[1]?.trim();
    assert.ok(
      label,
      `${where}: the scroll container has no non-empty aria-label — a region with no accessible name fails axe's region rule and tells a screen-reader user nothing about what scrolled.`,
    );
  }
});

test("scroll-container names are unique per page (axe landmark-unique)", () => {
  for (const file of SOURCES) {
    const names = tablesIn(file)
      .map((t) => t.wrapperTag.match(/aria-label="([^"]*)"/)?.[1]?.trim())
      .filter((n): n is string => Boolean(n));
    const dupes = names.filter((n, i) => names.indexOf(n) !== i);
    assert.deepEqual(
      dupes,
      [],
      `${rel(file)}: two scroll containers share the aria-label ${[...new Set(dupes)].join(", ")} — axe fails duplicate landmark names with landmark-unique (moderate).`,
    );
  }
});

test("the fix was not 'make it fit' by deleting a column or dropping w-full", () => {
  for (const t of TABLES) {
    const where = `${rel(t.file)} @${t.index}`;
    assert.ok(
      classTokens(t.tableTag).includes("w-full"),
      `${where}: the table lost w-full — it no longer fills the column on a wide screen, which is a layout change nobody asked for.`,
    );
    const end = readFileSync(t.file, "utf8").indexOf("</table>", t.index);
    const body = readFileSync(t.file, "utf8").slice(t.index, end === -1 ? undefined : end);
    const headers = [...body.matchAll(/<th\b/g)].length;
    assert.ok(
      headers >= 2,
      `${where}: the table has ${headers} <th> header(s). The defect was the SECOND column being unreachable; making the table fit by dropping a column deletes the content the docs exist to state.`,
    );
  }
});
