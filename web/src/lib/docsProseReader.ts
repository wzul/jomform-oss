// Reading a /docs page's prose, whichever shape it is in.
//
// HISTORY, because this module exists for a reason worth writing down. Until the
// prose renderer landed, every /docs page hand-wrote its own markup, so a guard
// that wanted to ask "does this page SAY x" matched the page's JSX — `id="x"`
// followed by `</h2>`, `<p className={…}>{s.body}</p>`, `<dl>`, literal `<span>`.
// Those guards were checking CONTENT through a MARKUP reader, which worked only
// while there was exactly one way to write a page.
//
// There are two shapes now, and both are legitimate:
//   A. a page that renders through src/app/docs/docsProse — its prose is a
//      `const SECTIONS: Section[]` literal of strings and block objects
//   B. a page that still hand-writes its JSX
//
// `pageProse()` returns the reader-visible TEXT of either, so a content guard
// keeps its assertion and stops pinning a markup shape it never cared about. A
// guard that needs the STRUCTURE (anchors, block kinds) uses `sectionsOf()`.
//
// What this deliberately does NOT do is answer "what does the built page say" —
// that needs the built HTML, and web/scripts/docsFacts.mjs is the reader for that.

import { readFileSync } from "node:fs";
import ts from "typescript";

export type ProseSection = {
  id: string;
  title: string;
  icon: string;
  /** Every string in the section body, in source order. */
  strings: string[];
};

function stringOf(node: ts.Node): string | null {
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return node.text;
  return null;
}

function collectStrings(node: ts.Node, into: string[]) {
  if (ts.isArrayLiteralExpression(node)) {
    for (const el of node.elements) collectStrings(el, into);
    return;
  }
  if (ts.isObjectLiteralExpression(node)) {
    for (const p of node.properties) {
      if (ts.isPropertyAssignment(p)) collectStrings(p.initializer, into);
    }
    return;
  }
  const s = stringOf(node);
  if (s !== null) into.push(s);
}

/** The `const SECTIONS` array of a converted page, or [] when it has none. */
export function sectionsOf(source: string, file = "page.tsx"): ProseSection[] {
  const sf = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  let out: ProseSection[] | null = null;
  const visit = (node: ts.Node) => {
    if (
      ts.isVariableDeclaration(node) &&
      node.name.getText() === "SECTIONS" &&
      node.initializer &&
      ts.isArrayLiteralExpression(node.initializer)
    ) {
      out = node.initializer.elements.filter(ts.isObjectLiteralExpression).map((obj) => {
        const s: ProseSection = { id: "", title: "", icon: "", strings: [] };
        for (const prop of obj.properties) {
          if (!ts.isPropertyAssignment(prop)) continue;
          const key = prop.name.getText();
          if (key === "id") s.id = stringOf(prop.initializer) ?? "";
          else if (key === "title") s.title = stringOf(prop.initializer) ?? "";
          else if (key === "icon") s.icon = stringOf(prop.initializer) ?? "";
          else if (key === "body") collectStrings(prop.initializer, s.strings);
        }
        return s;
      });
      return;
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return out ?? [];
}

/**
 * The reader-visible prose of a page source: the SECTIONS strings when the page
 * has them, the raw source otherwise.
 *
 * The raw source is the fallback, not a merge: for a converted page, the JSX that
 * is left is imports and a `<DocsPage …/>` call, and including it would let a
 * phrase sitting in a comment or an import path satisfy a content assertion.
 */
export function pageProse(source: string, file = "page.tsx"): string {
  const sections = sectionsOf(source, file);
  if (sections.length === 0) return source;
  return sections.map((s) => [s.title, ...s.strings].join("\n")).join("\n\n");
}

/** The same, from a path. */
export function readPageProse(path: string): string {
  return pageProse(readFileSync(path, "utf8"), path);
}

/** Does this page render through the shared prose renderer? */
export function usesDocsProse(source: string): boolean {
  // Any relative depth: /docs/<page> imports "../docsProse", a nested task page
  // such as /docs/mcp/tools imports "../../docsProse". Card t_0356ed4c split two
  // reference pages into nested routes, and the old two-shape test reported every
  // one of them as "not renderer-backed" — which would have exempted them from
  // the prose-convention exemption and RED the build on their allowed markers.
  return /from "(?:\.\.\/)+docsProse"/.test(source) || /from "\.\/docsProse"/.test(source);
}

/** One `{ table: … }` block of a converted page, as the page writes it. */
export type ProseTable = {
  /** The id of the section the block sits in. */
  section: string;
  /** The block's required `label`, which the renderer uses as the region name. */
  label: string;
  /** The header cells, when they are a literal array. */
  head: string[];
  /** The number of literal rows, or null when the rows are built at runtime. */
  rows: number | null;
};

/**
 * The table BLOCKS a converted page declares, so a guard written against
 * hand-written `<table>` markup can still count (and judge) the tables that are
 * now data. Only a literal `table: { … }` object is read: a block built by a
 * `.map()` at module scope (docs/api's per-tag endpoint tables) is not a block
 * this reader can see, and saying so is honest — the renderer emits every one of
 * them through the same wrapper, which is where the rules live now.
 */
export function tableBlocksOf(source: string, file = "page.tsx"): ProseTable[] {
  const sf = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const out: ProseTable[] = [];
  const visit = (node: ts.Node) => {
    if (
      ts.isVariableDeclaration(node) &&
      node.name.getText() === "SECTIONS" &&
      node.initializer &&
      ts.isArrayLiteralExpression(node.initializer)
    ) {
      for (const sectionNode of node.initializer.elements) {
        if (!ts.isObjectLiteralExpression(sectionNode)) continue;
        const idProp = sectionNode.properties.find(
          (p): p is ts.PropertyAssignment => ts.isPropertyAssignment(p) && p.name.getText() === "id",
        );
        const sectionId = idProp ? (stringOf(idProp.initializer) ?? "") : "";
        for (const prop of sectionNode.properties) {
          if (!ts.isPropertyAssignment(prop) || prop.name.getText() !== "body") continue;
          if (!ts.isArrayLiteralExpression(prop.initializer)) continue;
          for (const block of prop.initializer.elements) {
            if (!ts.isObjectLiteralExpression(block)) continue;
            const tableProp = block.properties.find(
              (p): p is ts.PropertyAssignment => ts.isPropertyAssignment(p) && p.name.getText() === "table",
            );
            if (!tableProp || !ts.isObjectLiteralExpression(tableProp.initializer)) continue;
            const fields = tableProp.initializer.properties;
            const value = (key: string) => {
              const p = fields.find(
                (f): f is ts.PropertyAssignment => ts.isPropertyAssignment(f) && f.name.getText() === key,
              );
              return p?.initializer;
            };
            const labelNode = value("label");
            if (!labelNode) continue;
            const label = stringOf(labelNode);
            if (label === null) continue;
            const headNode = value("head");
            const head =
              headNode && ts.isArrayLiteralExpression(headNode)
                ? headNode.elements.map((e) => stringOf(e)).filter((s): s is string => s !== null)
                : [];
            const rowsNode = value("rows");
            out.push({
              section: sectionId,
              label,
              head,
              rows: rowsNode && ts.isArrayLiteralExpression(rowsNode) ? rowsNode.elements.length : null,
            });
          }
        }
      }
      return;
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return out;
}
