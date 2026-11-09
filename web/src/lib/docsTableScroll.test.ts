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
// WHAT CHANGED UNDER THIS FILE, AND WHY IT STILL BITES
//   Every one of those five tables used to be literal <div><table> markup, and
//   this file counted literal markup. Nineteen /docs pages now render through
//   src/app/docs/docsProse, so their tables are `{ table: { label, head, rows } }`
//   BLOCKS and the markup exists ONCE, in the renderer. The coverage floor
//   therefore counts TWO surfaces — the literal tables that remain (none today,
//   kept for the shape and for a page that still hand-writes one) and the blocks
//   the converted pages declare (docsProseReader.tableBlocksOf) — and the five
//   measured tables are pinned by LABEL, so "the scan stopped seeing them" and
//   "someone deleted a table" both fail here instead of quietly shrinking the
//   count. The markup rules (wrapper scrolls instead of clipping, tab stop,
//   labelled region, w-full, real <th>) moved WITH the markup into the renderer
//   and are asserted against docsProse.tsx at the end of each rule below. Nothing
//   was lowered: a block's HEAD must still carry at least two columns and its
//   label must still be unique on its page, because that is what the block turns
//   into at render time.
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
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import { docsRoutesOnDisk, filesUnder } from "./docsRoutes.ts";
import { tableBlocksOf, usesDocsProse } from "./docsProseReader.ts";

const DOCS_DIR = fileURLToPath(new URL("../app/docs", import.meta.url));

/**
 * The shared prose renderer. Its table is ONE generic <div>/<table> pair whose
 * cells and label come from data, so it cannot be judged as five hand-written
 * tables — it is judged once, directly, by every rule below.
 */
const RENDERER_FILE = join(DOCS_DIR, "docsProse.tsx");

/** Every .tsx under app/docs except the renderer — the pages AND their chrome. */
const SOURCES = filesUnder(DOCS_DIR)
  .filter((f) => f.endsWith(".tsx"))
  .filter((f) => f !== RENDERER_FILE);

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
const RENDERER_TABLES = tablesIn(RENDERER_FILE);
const RENDERER_SRC = readFileSync(RENDERER_FILE, "utf8");

/**
 * The table BLOCKS the converted pages declare, read through the same SECTIONS
 * parser the content guards use. This is the surface the five measured tables
 * live on now: the page names the label and the header cells, the renderer emits
 * the wrapper.
 */
type BlockAt = { file: string; section: string; label: string; head: string[]; rows: number | null };
const BLOCKS: BlockAt[] = SOURCES.filter((f) => usesDocsProse(readFileSync(f, "utf8"))).flatMap((f) =>
  tableBlocksOf(readFileSync(f, "utf8"), f).map((b) => ({ file: f, ...b })),
);

const rel = (f: string) =>
  f === RENDERER_FILE ? "app/docs/docsProse.tsx" : `app/docs/${f.slice(DOCS_DIR.length).replace(/^\//, "")}`;

/** A literal table's aria-label, when it writes one. */
const literalLabel = (t: TableAt) => t.wrapperTag.match(/aria-label="([^"]*)"/)?.[1]?.trim() ?? "";

/**
 * The five tables the defect was measured on, by the label the block/cell now
 * carries. Pinning the labels is what stops the count below going green after a
 * table is deleted: five OTHER tables would satisfy a bare floor.
 */
const MEASURED_TABLES = [
  "MCP tools used", // /docs/customize — 405.1px in a 270px box
  "Affiliate REST endpoints", // /docs/customize — 526.9px in the same box
  "Recipient placeholders", // /docs/placeholders — 270.0px, clipped at 24px root
  "Subject and body placeholders", // /docs/placeholders — 270.0px, clipped at 24px root
  "postMessage fields", // /docs/embed — 270.0px, clipped at 24px root
];

test("the walk actually reads the docs tree (this rule cannot pass vacuously)", () => {
  const routes = docsRoutesOnDisk(DOCS_DIR);
  assert.ok(
    routes.length >= 11 && routes.includes("/docs/customize"),
    `docsRoutesOnDisk found ${routes.length} route(s) (${routes.join(", ")}) — the docs walk is broken, so nothing below proved anything`,
  );
  // The coverage floor counts BOTH shapes: literal <table> markup and the
  // `{ table: … }` blocks the converted pages declare. It used to count markup
  // only; that number fell to 0 when the pages moved to the renderer, so counting
  // the blocks is what keeps the floor proving coverage instead of failing on a
  // shrinking literal count. The number 5 is unchanged — it is the docs surface's
  // five measured tables, not a number fitted to what the scan happens to see.
  assert.ok(
    TABLES.length + BLOCKS.length >= 5,
    `found ${TABLES.length} literal <table> and ${BLOCKS.length} table block(s) under app/docs; the docs ` +
      `surface carries at least 5 (all five measured tables today are blocks). A table that moved — or a ` +
      `scan that stopped reading the new surface — is a coverage loss, not a pass`,
  );
  assert.ok(
    BLOCKS.length >= 10,
    `parsed only ${BLOCKS.length} { table: … } block(s) out of the converted pages — docsProseReader.tableBlocksOf ` +
      `has drifted from the block shape, so the rules below would be judging almost nothing`,
  );
  assert.equal(
    RENDERER_TABLES.length,
    1,
    `docsProse.tsx renders ${RENDERER_TABLES.length} <table>(s), not the one generic table every block goes ` +
      `through — the renderer-side rules below are aimed at the wrong source`,
  );
  const labels = [...TABLES.map(literalLabel), ...BLOCKS.map((b) => b.label)].filter(Boolean);
  for (const label of MEASURED_TABLES) {
    assert.ok(
      labels.includes(label),
      `the measured table ${JSON.stringify(label)} is gone from app/docs (labels found: ${JSON.stringify(labels)}). ` +
        `It is one of the five this defect was measured on; if it was renamed, re-measure before re-pointing this pin`,
    );
  }
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
  // The renderer's table is the wrapper every converted table now gets: same rule,
  // asserted once against the source that writes it.
  for (const t of RENDERER_TABLES) {
    const where = `app/docs/docsProse.tsx @${t.index}: the table BLOCK wrapper every converted page's table uses`;
    assert.equal(
      t.wrapperGap.trim(),
      "",
      `${where}: the <div> before the renderer's <table> is not its direct parent — the rules below would be checking the wrong element`,
    );
    const tokens = classTokens(t.wrapperTag);
    assert.ok(
      !tokens.includes("overflow-hidden"),
      `${where} carries overflow-hidden. That re-clips every converted table at once — the t_7c3a76c0 defect, 405.1px of table in a 270px box with the last column unreachable. Use overflow-x-auto.`,
    );
    assert.ok(
      tokens.some((c) => SCROLLS_X.has(c)),
      `${where} is neither overflow-x-auto nor overflow-x-scroll. A block table can be wider than a 320px column at a raised font size, so its wrapper has to be the horizontal scroller.`,
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
  for (const t of RENDERER_TABLES) {
    const where = "app/docs/docsProse.tsx: the table BLOCK wrapper";
    assert.ok(
      /\btabIndex=\{0\}/.test(t.wrapperTag),
      `${where} has no tabIndex={0} — every converted table would then be a keyboard-unreachable scroller, and axe fails it with scrollable-region-focusable (serious).`,
    );
    assert.ok(
      /role="region"/.test(t.wrapperTag),
      `${where} carries no role="region" — a focusable <div> is announced as a bare generic.`,
    );
    // A BOUND label, not a literal: the renderer cannot invent a name that stays
    // unique across a page's tables, so the block's required `label` is the name.
    // A literal here would name every table the same and fail axe landmark-unique.
    assert.ok(
      /aria-label=\{b\.table\.label\}/.test(t.wrapperTag),
      `${where} does not take its accessible name from the block's required label — a region with no name (or ` +
        `one constant name for every table) tells a screen-reader user nothing and collides on landmark-unique.`,
    );
  }
});

test("scroll-container names are unique per page (axe landmark-unique)", () => {
  for (const file of SOURCES) {
    const names = tablesIn(file)
      .map((t) => t.wrapperTag.match(/aria-label="([^"]*)"/)?.[1]?.trim())
      .filter((n): n is string => Boolean(n));
    // The converted pages carry their names as data now, so the same rule reads
    // the block labels too: a duplicate label on one page is exactly the
    // landmark-unique collision the literal check above was written for.
    for (const b of BLOCKS.filter((b) => b.file === file)) names.push(b.label);
    const dupes = names.filter((n, i) => names.indexOf(n) !== i);
    assert.deepEqual(
      dupes,
      [],
      `${rel(file)}: two scroll containers share the aria-label ${[...new Set(dupes)].join(", ")} — axe fails duplicate landmark names with landmark-unique (moderate).`,
    );
  }
  // The renderer's region name comes from the block, so its uniqueness is the
  // per-page label uniqueness asserted above; what this pins is that the name is
  // bound from data rather than a constant (a constant would collide by design).
  for (const t of RENDERER_TABLES) {
    assert.ok(
      /aria-label=\{b\.table\.label\}/.test(t.wrapperTag),
      "app/docs/docsProse.tsx: the table region's name is not taken from the block's label — a constant name " +
        "would give every table on a page the same accessible name and fail landmark-unique",
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
  // A block's head is the table's header row, so the same "not by deleting a
  // column" rule is a data rule here: at least two header cells, and at least one
  // row to put under them.
  for (const b of BLOCKS) {
    const where = `${rel(b.file)} ${b.section} — table ${JSON.stringify(b.label)}`;
    assert.ok(
      b.head.length >= 2,
      `${where}: the block declares ${b.head.length} header cell(s). The defect was the SECOND column being ` +
        `unreachable; dropping a column to make the table fit deletes the content the docs exist to state.`,
    );
    assert.notEqual(
      b.rows,
      0,
      `${where}: the block declares an EMPTY rows array — the table would render its header with nothing under it.`,
    );
  }
  for (const t of RENDERER_TABLES) {
    assert.ok(
      classTokens(t.tableTag).includes("w-full"),
      "app/docs/docsProse.tsx: the block table lost w-full — every converted table would stop filling its column " +
        "on a wide screen, which is a layout change nobody asked for.",
    );
    const end = RENDERER_SRC.indexOf("</table>", t.index);
    const body = RENDERER_SRC.slice(t.index, end === -1 ? undefined : end);
    assert.ok(
      /b\.table\.head\.map/.test(body) && /<th\b/.test(body) && /scope="col"/.test(body),
      "app/docs/docsProse.tsx: the block table no longer renders a real <th scope=\"col\"> per entry of the " +
        "block's head — a row of <td>s is not a header, and a screen reader loses the column association.",
    );
  }
});
