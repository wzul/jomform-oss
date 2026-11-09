// The /docs style gate.
//
// Wan, twice: "aku xnak terlalu wordy. nak simple supaya reader tak terbeban utk
// baca" and "tak dak bold, tak dak emoji. rasa macam semua text. berat nak baca".
// Both are measurable, so they are measured here instead of described in a doc.
//
// It reads the page's `const SECTIONS` with the TypeScript compiler API — the same
// dependency docsProseConvention.test.ts already uses — rather than with a string
// heuristic. A heuristic would measure text that might not be the text the page
// renders; the AST node under `SECTIONS` is exactly what `<DocsPage>` receives.
//
// Rules, each with the reason it exists:
//   1. a section has an id            — it is a link target from /docs and the "?"s
//   2. ids are unique                 — two sections cannot share an anchor
//   3. a section has ONE emoji icon   — the scanner cue Wan asked for
//   4. a section has a bold term      — the reader needs one thing to land on
//   5. no string is over 30 words     — a paragraph is a line, not a wall
//   6. no section is over 8 blocks    — a section is one idea
//   7. no marker in a section title   — the renderer only parses `body`

import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import ts from "typescript";

const here = dirname(fileURLToPath(import.meta.url));
const docsRoot = join(here, "..", "app", "docs");

const MAX_WORDS = 30;
const MAX_BLOCKS = 8;
const EMOJI = /\p{Extended_Pictographic}/u;

type Section = {
  id: string;
  title: string;
  icon: string;
  blocks: number;
  strings: string[];
};

function stringOf(node: ts.Node): string | null {
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return node.text;
  return null;
}

function collectValues(node: ts.Node, into: string[]) {
  if (ts.isArrayLiteralExpression(node)) {
    for (const el of node.elements) collectValues(el, into);
    return;
  }
  if (ts.isObjectLiteralExpression(node)) {
    for (const p of node.properties) {
      if (ts.isPropertyAssignment(p)) collectValues(p.initializer, into);
    }
    return;
  }
  const s = stringOf(node);
  if (s !== null) into.push(s);
}

function sectionFrom(obj: ts.ObjectLiteralExpression): Section {
  const s: Section = { id: "", title: "", icon: "", blocks: 0, strings: [] };
  for (const prop of obj.properties) {
    if (!ts.isPropertyAssignment(prop)) continue;
    const key = prop.name.getText();
    if (key === "id") s.id = stringOf(prop.initializer) ?? "";
    else if (key === "title") s.title = stringOf(prop.initializer) ?? "";
    else if (key === "icon") s.icon = stringOf(prop.initializer) ?? "";
    else if (key === "body" && ts.isArrayLiteralExpression(prop.initializer)) {
      s.blocks = prop.initializer.elements.length;
      collectValues(prop.initializer, s.strings);
    }
  }
  return s;
}

function sectionsOf(source: string, file: string): Section[] {
  const sf = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  let out: Section[] | null = null;
  const visit = (node: ts.Node) => {
    if (
      ts.isVariableDeclaration(node) &&
      node.name.getText() === "SECTIONS" &&
      node.initializer &&
      ts.isArrayLiteralExpression(node.initializer)
    ) {
      out = node.initializer.elements.filter(ts.isObjectLiteralExpression).map(sectionFrom);
      return;
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return out ?? [];
}

function convertedPages(): { name: string; file: string; source: string }[] {
  const out: { name: string; file: string; source: string }[] = [];
  for (const entry of readdirSync(docsRoot)) {
    const dir = join(docsRoot, entry);
    if (!statSync(dir).isDirectory()) continue;
    const file = join(dir, "page.tsx");
    let source: string;
    try {
      source = readFileSync(file, "utf8");
    } catch {
      continue;
    }
    if (!/from "\.\.\/docsProse"/.test(source)) continue;
    out.push({ name: entry, file, source });
  }
  return out;
}

const words = (s: string) => s.trim().split(/\s+/).filter(Boolean).length;

test("every page that renders through docsProse follows the reading rules", () => {
  const pages = convertedPages();
  assert.ok(pages.length > 0, "at least one page renders through docsProse");

  const findings: string[] = [];
  for (const page of pages) {
    const sections = sectionsOf(page.source, page.file);
    if (sections.length === 0) {
      findings.push(`${page.name}: no const SECTIONS array found`);
      continue;
    }
    const seen = new Set<string>();
    for (const s of sections) {
      const where = `${page.name}#${s.id || "?"}`;
      if (!s.id) findings.push(`${where}: no id`);
      if (seen.has(s.id)) findings.push(`${where}: duplicate anchor`);
      seen.add(s.id);
      if (!s.icon || !EMOJI.test(s.icon)) findings.push(`${where}: no emoji icon`);
      if (/\*\*|`/.test(s.title)) findings.push(`${where}: marker in the title (only body is parsed)`);
      if (s.blocks > MAX_BLOCKS) findings.push(`${where}: ${s.blocks} blocks (max ${MAX_BLOCKS})`);
      if (!s.strings.some((t) => t.includes("**"))) findings.push(`${where}: no bold term`);
      for (const t of s.strings) {
        const n = words(t);
        if (n > MAX_WORDS) findings.push(`${where}: ${n} words — "${t.slice(0, 60)}…"`);
      }
    }
  }
  assert.deepEqual(findings, [], `style findings:\n  ${findings.join("\n  ")}`);
});

test("the gate is reading real sections (it cannot pass vacuously)", () => {
  let total = 0;
  for (const page of convertedPages()) total += sectionsOf(page.source, page.file).length;
  assert.ok(total >= 5, `the gate found ${total} sections across the converted pages`);
});
