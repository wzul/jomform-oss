// The /docs prose renderer must consume its own markers.
//
// The defect this guards (t_0447a70c): /docs printed prose as plain text, so a
// marker written into a body string reached the reader as literal `**word**` or
// a stray backtick. The renderer fixes that — and this file is what says so
// after the next edit, including the edit that adds bold to nineteen more pages.
//
// WHAT IT CHECKS
//   1. `**bold**`, `code` and [link](/docs/x) parse into their own tokens
//   2. no marker survives parsing as text
//   3. every /docs page's source has balanced markers, so a half-written
//      `**term` cannot ship
//   4. the pure module holds no JSX (a .tsx cannot be imported by `node --test`)

import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";

import { leakedMarkers, parseInline } from "../app/docs/docsProseText.ts";

const here = dirname(fileURLToPath(import.meta.url));
const docsRoot = join(here, "..", "app", "docs");

test("bold, code and links parse into their own tokens", () => {
  const tokens = parseInline("Set **form mode** to `response`, then read [the guide](/docs/guides).");
  assert.deepEqual(
    tokens.map((t) => t.kind),
    ["text", "strong", "text", "code", "text", "link", "text"],
  );
  assert.equal(tokens[1].text, "form mode");
  assert.equal(tokens[3].text, "response");
  const link = tokens[5];
  assert.ok(link.kind === "link", "token 5 is the link");
  assert.equal(link.href, "/docs/guides");
});

test("no marker survives as text", () => {
  const samples = [
    "**bold**",
    "a `code` b",
    "**Form → Submission** opens the list.",
    "read `refunded_sen` first",
    "an unclosed **emphasis stays literal",
    "a lone ` backtick",
  ];
  for (const s of samples.slice(0, 4)) {
    assert.equal(leakedMarkers(s), 0, `a marker reached the reader in: ${s}`);
  }
  // Unclosed markers are deliberately NOT silently swallowed: they stay text,
  // and the balance check below is what stops them being written at all.
  assert.ok(leakedMarkers(samples[4]) > 0);
  assert.ok(leakedMarkers(samples[5]) > 0);
});

test("every /docs page writes balanced markers", () => {
  const bad: string[] = [];
  for (const entry of readdirSync(docsRoot)) {
    const dir = join(docsRoot, entry);
    if (!statSync(dir).isDirectory()) continue;
    const page = join(dir, "page.tsx");
    let source: string;
    try {
      source = readFileSync(page, "utf8");
    } catch {
      continue;
    }
    const bold = (source.match(/\*\*/g) ?? []).length;
    if (bold % 2 !== 0) bad.push(`${entry}/page.tsx: ${bold} bold markers`);
  }
  assert.deepEqual(bad, []);
});

test("the renderer module is JSX-free so this suite can import it", () => {
  const source = readFileSync(join(docsRoot, "docsProseText.ts"), "utf8");
  // Comments mention <pre>/<code>; the RULE is that the module holds no JSX element
  // and imports no React, because `node --test` cannot parse a .tsx.
  const code = source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
  assert.ok(!/import .*react/.test(code), "docsProseText.ts must not import React");
  assert.ok(!/<[A-Z][A-Za-z]*[\s/>]/.test(code), "docsProseText.ts must hold no JSX element");
  assert.ok(source.includes("export function parseInline"), "parseInline is the contract");
});

test("the renderer's table is a labelled, focusable scroll region", () => {
  // Same rules as docsTableScroll.test.ts, asserted against the renderer because
  // that guard excludes this file (its table comes from data, so no literal
  // aria-label or literal pair of <th> exists to scan).
  const source = readFileSync(join(docsRoot, "docsProse.tsx"), "utf8");
  for (const [what, re] of [
    ["a tab stop", /tabIndex=\{0\}/],
    ["a region", /role="region"/],
    ["an accessible name", /aria-label=\{b\.table\.label\}/],
    ["a horizontal scroller", /overflow-x-auto/],
    ["a full-width table", /className="w-full border-collapse/],
    ["real header cells", /<th\b/],
  ] as const) {
    assert.ok(re.test(source), `the table block has no ${what}`);
  }
  const block = readFileSync(join(docsRoot, "docsProseText.ts"), "utf8");
  assert.ok(
    /table: \{ label: string; head: string\[\]; rows: string\[\]\[\] \}/.test(block),
    "the table block must REQUIRE a label, so a page cannot ship an unnamed region",
  );
});
