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
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import ts from "typescript";

import { leakedMarkers, parseInline } from "../app/docs/docsProseText.ts";
import { usesDocsProse } from "./docsProseReader.ts";
import { filesUnder } from "./docsRoutes.ts";

const here = dirname(fileURLToPath(import.meta.url));
const docsRoot = join(here, "..", "app", "docs");

/**
 * The body strings a renderer-backed page hands to the INLINE parser, in source
 * order — the population the whole-tree leak scan judges.
 *
 * Read with the TypeScript parser, not a text scan: the block KINDS decide what
 * is parsed, and only the AST knows them. `docsProse.tsx` passes a string, a
 * `list`/`steps` item, a `tip`/`note`/`warn` and a table cell through <Inline>;
 * it prints a `{ code: … }` sample verbatim inside <pre> (with its caption in
 * <figcaption>) and a `{ heading: … }` as plain text. Judging a code sample
 * would be a false finding — docs/api/authentication ships
 * `{ code: "Authorization: Bearer ***" }` on purpose.
 *
 * A `${…}` template in a body position is skipped: its static parts are not
 * standalone strings (`…to \`${API_BASE}\`` would read as a leaked backtick),
 * and docsProseConvention.test.ts already collects those shapes.
 */
function inlineProseOf(source: string, file: string): string[] {
  const sf = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const out: string[] = [];
  const str = (n: ts.Node | undefined): string | null =>
    n && (ts.isStringLiteral(n) || ts.isNoSubstitutionTemplateLiteral(n)) ? n.text : null;
  const field = (obj: ts.ObjectLiteralExpression, key: string) =>
    obj.properties.find(
      (p): p is ts.PropertyAssignment => ts.isPropertyAssignment(p) && p.name.getText() === key,
    )?.initializer;
  const pushArray = (n: ts.Node | undefined) => {
    if (!n || !ts.isArrayLiteralExpression(n)) return;
    for (const el of n.elements) {
      const s = str(el);
      if (s !== null) out.push(s);
    }
  };
  const visit = (node: ts.Node) => {
    if (
      ts.isVariableDeclaration(node) &&
      node.name.getText() === "SECTIONS" &&
      node.initializer &&
      ts.isArrayLiteralExpression(node.initializer)
    ) {
      for (const section of node.initializer.elements) {
        if (!ts.isObjectLiteralExpression(section)) continue;
        const body = field(section, "body");
        if (!body || !ts.isArrayLiteralExpression(body)) continue;
        for (const block of body.elements) {
          const whole = str(block);
          if (whole !== null) {
            out.push(whole);
            continue;
          }
          if (!ts.isObjectLiteralExpression(block)) continue;
          if (field(block, "code")) continue; // a sample: verbatim, never parsed
          const callout = str(field(block, "tip")) ?? str(field(block, "note")) ?? str(field(block, "warn"));
          if (callout !== null) {
            out.push(callout);
            continue;
          }
          pushArray(field(block, "list"));
          pushArray(field(block, "steps"));
          const table = field(block, "table");
          if (table && ts.isObjectLiteralExpression(table)) {
            pushArray(field(table, "head"));
            const rows = field(table, "rows");
            if (rows && ts.isArrayLiteralExpression(rows)) for (const row of rows.elements) pushArray(row);
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

test("a link written inside bold is still a link, not literal markdown", () => {
  // THE DEFECT (card t_e101869b, measured on a served build of main 4c0590fc9,
  // fresh browser state, /docs/mcp/connect lang=en): the body string
  //   "Without the email, type the code at **[Profile → MCP agents](/profile?tab=mcp)** — …"
  // reached the reader WITH its brackets. `document.querySelectorAll("main a")`
  // held no anchor whose text was "MCP agents", and the page text was the raw
  // `[Profile → MCP agents](/profile?tab=mcp)`.
  //
  // WHY: MARKER tries `\*\*[^*]+\*\*` BEFORE the link alternative and `[^*]+`
  // happily matches `[Profile …](/profile?tab=mcp)`, so the whole run became ONE
  // strong token whose inner text was never re-parsed; docsProse.tsx printed
  // `t.text` inside <strong>. The old leakedMarkers() counted only `text` tokens,
  // so it scored this 0 and the guard stayed green while the page printed
  // markdown — a guard that cannot see a marker cannot fail on it.
  const body =
    "Without the email, type the code at **[Profile → MCP agents](/profile?tab=mcp)** — `kx9mx5bs` and `KX9M-X5BS` both work.";
  assert.equal(leakedMarkers(body), 0, "a link inside bold leaked as literal markdown");

  const tokens = parseInline(body);
  assert.deepEqual(
    tokens.map((t) => t.kind),
    ["text", "strong", "text", "code", "text", "code", "text"],
    "the bold run is one strong token, and the two codes are their own tokens",
  );
  const strong = tokens[1];
  assert.ok(strong.kind === "strong", "token 1 is the bold run");
  assert.equal(strong.text, "[Profile → MCP agents](/profile?tab=mcp)");
  assert.deepEqual(
    strong.children?.map((t) => t.kind),
    ["link"],
    "the bold run's inner text is parsed, so its link survives as a link token",
  );
  const link = strong.children![0];
  assert.ok(link.kind === "link", "the nested token is the link");
  assert.equal(link.text, "Profile → MCP agents");
  assert.equal(link.href, "/profile?tab=mcp", "the destination the page wrote is the one rendered");
});

test("a code span inside bold renders as code, not as a backtick at the reader", () => {
  // The same class one marker over: `**read `x` first**`. The `**…**`
  // alternative swallows the backticks too, so the inner text has to be parsed
  // for the nesting to be general rather than a special case for links.
  const tokens = parseInline("Set **`spam_judge_enabled` off** first.");
  assert.equal(leakedMarkers("Set **`spam_judge_enabled` off** first."), 0);
  const strong = tokens.find((t) => t.kind === "strong");
  assert.ok(strong && strong.kind === "strong", "the bold run is a strong token");
  assert.deepEqual(strong.children?.map((t) => t.kind), ["code", "text"]);
  const code = strong.children![0];
  assert.ok(code.kind === "code", "the nested token is the code span");
  assert.equal(code.text, "spam_judge_enabled");
});

test("no /docs page prints an unparsed marker (whole-tree scan)", () => {
  // The class, not the one line: EVERY body string a renderer-backed page hands
  // to the inline parser is judged here, so the next `**[…](…)**` fails on the
  // page that writes it instead of at a reader. The unit cases above pin the
  // parser's behaviour; this is what makes the file a GUARD for the tree.
  //
  // What is judged, and what is deliberately not (see inlineProseOf):
  //   * judged: a body string, a list/steps item, a tip/note/warn, a table cell
  //     — the four shapes docsProse passes through <Inline>, which the reader
  //     sees as styled text.
  //   * NOT judged: a `{ code: … }` sample and its caption (printed verbatim
  //     inside <pre>/<figcaption>, never parsed — docs/api/authentication's
  //     `Authorization: Bearer ***` is exactly that, and flagging it would be a
  //     false finding), a `{ heading: … }` and a section title (printed as text,
  //     and docsStyle already bans a marker in a title), and a body string built
  //     by a `${…}` template (its static parts cannot be balanced on their own —
  //     the half-written marker in `…to \`${API_BASE}\`` would read as a leak.
  //     docsProseConvention.test.ts owns template-literal markers; measured 14
  //     such nodes in this tree, none carrying a link or a bold run).
  const findings: string[] = [];
  let pages = 0;
  let judged = 0;
  for (const file of filesUnder(docsRoot)) {
    if (!file.endsWith(".tsx") || file.endsWith(".d.ts")) continue;
    const source = readFileSync(file, "utf8");
    if (!usesDocsProse(source)) continue;
    pages++;
    for (const body of inlineProseOf(source, file)) {
      judged++;
      const leaks = leakedMarkers(body);
      if (leaks > 0) {
        findings.push(
          `${relative(docsRoot, file)}: ${leaks} leaked marker(s) in ${JSON.stringify(body.slice(0, 100))}`,
        );
      }
    }
  }
  // A scan that read no page or no string would satisfy the rule vacuously.
  assert.ok(pages >= 15, `scanned only ${pages} renderer-backed docs page(s) — the walk is not reaching the tree`);
  assert.ok(judged >= 1000, `judged only ${judged} inline prose string(s) — the block reader is not descending`);
  assert.deepEqual(findings, [], "a /docs page renders a markdown marker instead of styling it");
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
