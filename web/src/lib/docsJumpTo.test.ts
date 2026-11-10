// The docs "jump to" strip (card t_0356ed4c).
//
// WHAT THIS GUARDS. The strip is plain anchors rendered above the first section, and
// its whole job is to make a very tall docs page navigable from the top of the BODY
// (the "On this page" rail is a collapsed <details> on a phone). Two ways it can stop
// doing that job while still rendering something:
//
//   1. it grows into the wall it was added to break — /docs/customize has 31 sections,
//      and 31 pills measured 800-850px tall at 390px, more than half a phone screen;
//   2. it appears on every page, including 900-word pages where four chips are
//      decoration and push the actual article down.
//
// This is a SOURCE guard: it pins the two decisions. The rendered proof (chip counts,
// heights, a clicked chip landing on its section) is the card's browser evidence, and
// `docsStyle.test.ts` keeps the pages themselves inside the reading rules.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";

import { filesUnder } from "./docsRoutes.ts";

const here = dirname(fileURLToPath(import.meta.url));
const RENDERER = join(here, "..", "app", "docs", "docsProse.tsx");
const src = readFileSync(RENDERER, "utf8");

const docsRoot = join(here, "..", "app", "docs");

/** Every page that renders through the shared prose renderer, with its section count. */
function pagesWithSections(): { file: string; count: number }[] {
  const out: { file: string; count: number }[] = [];
  for (const file of filesUnder(docsRoot)) {
    if (!file.endsWith("/page.tsx")) continue;
    const page = readFileSync(file, "utf8");
    if (!/from "(?:\.\.\/)+docsProse"/.test(page)) continue;
    // Section objects are the only place a docs page writes `id: "…"` at the start of
    // a line inside SECTIONS; the renderer's own file is not in this walk.
    const count = [...page.matchAll(/^\s{4}id: "[a-z0-9-]+",/gm)].length;
    out.push({ file, count });
  }
  return out;
}

test("the guard reads the real docs tree (it cannot pass vacuously)", () => {
  const pages = pagesWithSections();
  assert.ok(pages.length >= 20, `only ${pages.length} converted docs pages were found`);
  assert.ok(
    pages.some((p) => p.count >= 6),
    "no docs page has six or more sections — the threshold below would never be met",
  );
  assert.ok(
    pages.some((p) => p.count > 12),
    "no docs page has more than twelve sections — the cap below would never be exercised",
  );
});

test("the strip is capped, and the rest of the anchors are one tap away", () => {
  const cap = src.match(/const JUMP_CAP = (\d+);/);
  assert.ok(cap, `${RENDERER} no longer declares JUMP_CAP — the strip can grow without bound again`);
  const n = Number(cap[1]);
  assert.ok(n > 0 && n <= 12, `JUMP_CAP is ${n}; measured, 31 pills cost 800-850px at 390px`);

  assert.match(
    src,
    /const shown = links\.slice\(0, JUMP_CAP\);/,
    "the capped slice is gone — every section would render as a chip again",
  );
  assert.match(
    src,
    /const rest = links\.slice\(JUMP_CAP\);/,
    "the remainder is not sliced out — sections past the cap would be lost, not folded",
  );
  // The remainder must be reachable, and by a control the keyboard can open: a native
  // <details>/<summary>. A <div hidden> or a JS-only toggle is not that.
  assert.match(
    src,
    /<details[\s\S]{0,600}?<summary[\s\S]{0,300}?More sections \(\{rest\.length\}\)[\s\S]{0,400}?rest\.map/,
    "the folded sections are not behind a <details> whose <summary> names them",
  );
});

test("the strip only renders on a page long enough to need it, or by explicit opt-in", () => {
  assert.match(
    src,
    /const showJumps = jumpTo \|\| sections\.length >= 6;/,
    "the render threshold moved — re-measure which pages should carry the strip before changing it",
  );
  // The two hand-rendered reference walls are the pages that opt in: their SECTIONS are
  // two or three entries and the height is one long table, so a section count cannot see
  // it. If an opt-in disappears, that page loses its only in-body nav.
  for (const rel of ["../app/docs/mcp/tools/page.tsx", "../app/docs/api/endpoints/page.tsx"]) {
    const page = readFileSync(join(here, rel), "utf8");
    assert.match(page, /^\s+jumpTo$/m, `${rel} no longer opts into the jump-to strip`);
  }
});
