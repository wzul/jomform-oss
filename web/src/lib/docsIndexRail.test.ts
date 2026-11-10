// The /docs index's right rail (card t_ff973d0b).
//
// THE DEFECT THIS PINS. The rail is labelled "On this page" and DocsToc builds it
// from the h2/h3 headings that actually rendered. The index renders its FEATURE
// CATEGORIES as h3s (🏢 Workspaces, 💰 Sales & payments …) and its FAQ questions
// as h3s too, so the rail carried the sidebar's whole taxonomy — emoji and all —
// plus a second FAQ index. Measured on the built page at 1440px before the fix,
// the rail read:
//
//   Start here | Features | 🏢Workspaces | 💰Sales & payments | … | FAQ |
//   How do I connect an AI agent? | …
//
// Two navigation systems listing the same destinations on one screen, while the
// page's actual structure (Start here / Popular references / Features / FAQ — the
// four h2 sections) was what the label promises.
//
// THE CONTRACT, both halves, because either one alone is satisfiable by a no-op:
//   1. every h3 the index renders is marked `data-toc-skip` — on this page an h3
//      is by construction a category or an FAQ entry, never page structure;
//   2. DocsToc refuses a heading inside `[data-toc-skip]` — a marker nothing
//      reads would leave the rail exactly as it was.
//
// What it cannot see: the rendered rail itself (that is the browser evidence on
// the card), and a NEW h2 added to the index would be listed by the rail, which
// is correct — an h2 IS page structure here.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";

const here = dirname(fileURLToPath(import.meta.url));
const INDEX = join(here, "..", "app", "docs", "page.tsx");
const TOC = join(here, "..", "app", "docs", "docsToc.tsx");

const indexSource = readFileSync(INDEX, "utf8");
const tocSource = readFileSync(TOC, "utf8");

/** The render markup only: JSX comments, block comments and `//` lines dropped,
 *  so a comment that spells an h3 tag (this file's own header does) is not
 *  counted as render. */
function rendered(src: string): string {
  return src
    .replace(/\{\s*\/\*[\s\S]*?\*\/\s*\}/g, " ")
    .replace(/^[ \t]*\/\/.*$/gm, " ")
    .replace(/[ \t]\/\/.*$/gm, " ");
}

test("the guard is reading the real files", () => {
  assert.ok(
    indexSource.includes("export default function DocsIndexPage"),
    `${INDEX} is not the docs index — the guard's anchor moved`,
  );
  assert.ok(
    tocSource.includes("function visibleHeadings"),
    `${TOC} no longer declares visibleHeadings — the guard's anchor moved`,
  );
});

test("every h3 on the /docs index is kept out of the rail", () => {
  const live = rendered(indexSource);
  // Every h3 the page renders, opening tag included. Non-vacuity first: the index
  // renders three (the category heading inside GROUPS.map, and the two FAQ
  // branches), so a scan that found none would be looking at the wrong file.
  const h3s = [...live.matchAll(/<h3\b[^>]*>/g)].map((m) => m[0]);
  assert.ok(
    h3s.length >= 3,
    `found ${h3s.length} <h3> opening tag(s) in ${INDEX} — the page renders its categories and FAQ ` +
      "entries as h3, so this scan is not looking at the render",
  );
  const unmarked = h3s.filter((tag) => !/\bdata-toc-skip\b/.test(tag));
  assert.deepEqual(
    unmarked,
    [],
    `these headings on the /docs index are NOT marked data-toc-skip, so the rail would list them ` +
      `again:\n  ${unmarked.join("\n  ")}`,
  );
});

test("DocsToc honours the marker before it mints an anchor", () => {
  // The skip has to run where the headings are collected, and BEFORE the id is
  // assigned: a skipped heading must not get an id minted for a rail entry that
  // will not exist.
  const collect = tocSource.slice(
    tocSource.indexOf("function visibleHeadings"),
    tocSource.indexOf("function TocList"),
  );
  assert.ok(collect.length > 0, `${TOC} no longer declares visibleHeadings — the guard's anchor moved`);
  const skip = collect.indexOf('closest("[data-toc-skip]")');
  const mint = collect.indexOf("element.id = id");
  assert.ok(
    skip !== -1,
    `${TOC} no longer skips [data-toc-skip] headings — the /docs index's categories and FAQ ` +
      "questions would be back in the \"On this page\" rail",
  );
  assert.ok(mint !== -1, `${TOC} no longer assigns an id — the marker check below is meaningless`);
  assert.ok(
    skip < mint,
    `the [data-toc-skip] check runs AFTER the id assignment in ${TOC} — a skipped heading would ` +
      "still get an anchor minted for it",
  );
});
