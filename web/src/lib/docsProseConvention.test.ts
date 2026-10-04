// The /docs prose convention: a markdown marker must never reach the reader
// (card t_0447a70c).
//
// WHY THIS EXISTS (the defect, measured on prod)
//   Every /docs guide renders its prose as a plain text node — the section bodies
//   are `body: "…"` strings and the pages print them with `{p}`. There is no
//   markdown renderer on these pages (the ONE markdown renderer in the repo,
//   src/components/Markdown.ts, is the chat/feedback surface). So a backtick span
//   written into a body reaches the reader WITH its markers. Measured on
//   jomform.com, 2026-10-04, fresh browser state: /docs/chat showed
//
//     The fourth creates: `upsert_form` is proposable…
//     …deleting a form for good, `purge_form`: …
//
//   and `document.querySelectorAll("code").length === 0` on the page — the
//   markers are literal text, not a styled span. /docs/sales rendered 14 more
//   the same way, /docs/security 3, /docs/mcp 4, and /docs/chat 4 (two of them,
//   `upsert_form` / `purge_form`, added by PR #967 — the release whose docs page
//   this card's parent verified).
//
// WHY THE FIX IS THE PROSE AND NOT A RENDERER
//   The question was one decision, not one patch per page: either render inline
//   code on /docs or stop writing backticks there. The shipped convention already
//   answered it — EVERY comparable sentence in the tree writes the name as words.
//   Measured over the 18 docs pages before the fix: 11 of 775 prose strings used
//   a backtick span (25 spans), and all 11 rendered their markers. The other 764
//   name the same kinds of thing in plain prose — `sound_enabled` is written
//   sound_enabled (sales/page.tsx:79), `set_workspace_settings` and
//   `PATCH /v1/workspaces/{id}` are written bare (spam-detection/page.tsx:49,
//   workspaces/page.tsx:53), and so are `clear_billing_period`,
//   `spam_judge_enabled` and dozens more. Adding an inline-code renderer to /docs
//   would be a feature (and a second markdown path to keep in step with the
//   chat's), not a defect fix; making the 11 outliers match the other 764 is the
//   smaller change that removes the defect. Rendering code samples is still done,
//   and still correctly — but with a template literal in JSX (`` `…` `` inside
//   braces: api/page.tsx's curl blocks, embed/page.tsx's iframe snippet), which
//   prints the text verbatim instead of leaving a marker on it. That is why this
//   guard keys on STRING LITERALS: a JSX template literal is not one, and it is
//   the supported way to show code on these pages.
//
// HOW IT DECIDES (no heuristics, on purpose)
//   It parses each page with the TypeScript parser (`typescript` is already a
//   devDependency and src/lib/jsxSource.ts already depends on it for the same
//   reason) and collects every `ts.StringLiteral` whose text contains a backtick.
//   A parser is used rather than a text scan because the alternative has a
//   measured failure in this very tree: a byte-level comment scanner reads the
//   RENDERED text `/v1/rest/*` in api/page.tsx:175 as a `/*` comment opener and
//   blanks 41 lines of the file (that defect is documented at the top of
//   src/lib/jsxSource.ts). With the AST, a comment is not a node at all, so no
//   comment can raise or hide a finding, and there is no strip to get wrong.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It reads the source, not the served HTML. It proves the marker is not in
//     the string a page renders; that the page really prints `{p}` is pinned
//     elsewhere (docsIndex.test.ts's render-site checks and the fact reader in
//     web/scripts/docsFacts.mjs, which reads the BUILT page).
//   * It is scoped to `src/app/docs`. Backticks in the chat/feedback markdown
//     surface are CORRECT there — that text goes through Markdown.ts, which
//     renders inline code as a `<code>` chip — so a tree-wide rule would be
//     wrong and would start a fight with the one place backticks belong.
//   * It does not judge other markdown markers (`**bold**`, `_italic_`, `-` list
//     bullets) in docs prose. Measured before this guard was written: there are
//     none in any docs `body`/`desc`/`description` string, so a rule for them
//     would have no case to catch today and would be asserted blind — it is left
//     out rather than shipped untested. If one appears, the same argument
//     applies and this guard is where it should go.
//   * It cannot tell a backtick apart from a backtick the AUTHOR intended as a
//     literal apostrophe-ish mark. There is no such character in these pages;
//     that is a copy question, and this rule answers it the same way for every
//     one of them.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import ts from "typescript";

const DOCS_DIR = fileURLToPath(new URL("../app/docs", import.meta.url));

// Every .tsx under src/app/docs, including the shared layout and the docs
// chrome island (a marker in any of them renders just as literally).
function docsFiles(): string[] {
  const out: string[] = [];
  for (const e of readdirSync(DOCS_DIR, { withFileTypes: true })) {
    if (e.isFile() && e.name.endsWith(".tsx")) out.push(join(DOCS_DIR, e.name));
    if (e.isDirectory() && existsSync(join(DOCS_DIR, e.name, "page.tsx"))) {
      out.push(join(DOCS_DIR, e.name, "page.tsx"));
    }
  }
  return out.sort();
}

type Finding = { file: string; line: number; text: string };

// A string literal that carries a backtick, with `file:line` for the message.
function backtickStrings(file: string): Finding[] {
  const src = readFileSync(file, "utf8");
  const sf = ts.createSourceFile(file, src, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const found: Finding[] = [];
  const visit = (node: ts.Node) => {
    // ts.StringLiteral covers "…" and '…' but NOT a template literal (`…`),
    // which is a different node kind (NoSubstitutionTemplateLiteral) — the
    // exclusion is the point, see the header.
    if (ts.isStringLiteral(node) && node.text.includes("`")) {
      const { line } = sf.getLineAndCharacterOfPosition(node.getStart(sf));
      found.push({ file, line: line + 1, text: node.text });
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return found;
}

const FILES = docsFiles();
const FINDINGS: Finding[] = FILES.flatMap(backtickStrings);

test("the guard is reading the real docs tree (it cannot pass vacuously)", () => {
  // The walk must find the pages AND their prose: a parse that collected nothing
  // would satisfy the rule below while looking at no string at all.
  assert.ok(
    FILES.length >= 15,
    `found only ${FILES.length} .tsx files under ${DOCS_DIR} — the guard is not looking at the docs tree`,
  );
  const chat = FILES.find((f) => f.endsWith("/docs/chat/page.tsx"));
  assert.ok(chat, "docs/chat/page.tsx is not in the walked set — the page this card is about is not being read");
  const chatSrc = readFileSync(chat!, "utf8");
  assert.ok(
    chatSrc.includes("What it may propose"),
    "docs/chat/page.tsx is not the page this guard was written against (its section title moved)",
  );
  // Prove the AST walk really visits prose strings rather than stopping at the
  // module scope: assert a known prose body is among the visited literals.
  const bodies: string[] = [];
  const collect = (file: string) => {
    const sf = ts.createSourceFile(file, readFileSync(file, "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
    const visit = (n: ts.Node) => {
      if (ts.isStringLiteral(n)) bodies.push(n.text);
      ts.forEachChild(n, visit);
    };
    visit(sf);
  };
  for (const f of FILES) collect(f);
  assert.ok(
    bodies.length >= 300,
    `collected only ${bodies.length} string literals from the docs tree — the AST walk is not descending into the ` +
      `prose, so a backtick written into a body would be invisible to this guard`,
  );
  assert.ok(
    bodies.some((b) => b.startsWith("The assistant is a chat panel inside the dashboard")),
    "the docs/chat prose body is not among the visited literals — the walk is not reaching the section bodies",
  );
});

test("no /docs prose carries a backtick marker (these pages render plain text)", () => {
  const lines = FINDINGS.map(
    (f) => `${f.file.replace(DOCS_DIR, "src/app/docs")}:${f.line}  ${JSON.stringify(f.text.slice(0, 120))}`,
  );
  assert.deepEqual(
    FINDINGS,
    [],
    `these /docs strings carry a backtick span. Every /docs guide renders its prose as a plain text node ` +
      `(the bodies are "…" strings printed with {p}), so the markers reach the reader literally — measured on ` +
      `jomform.com 2026-10-04: /docs/chat showed "The fourth creates: \\\`upsert_form\\\` is proposable…" with ` +
      `document.querySelectorAll("code").length === 0. Write the name as plain words, which is what the rest of ` +
      `the tree already does (sound_enabled, set_workspace_settings, PATCH /v1/workspaces/{id} are all bare). ` +
      `To SHOW code on a docs page, use a JSX template literal — {\\\`…\\\`} — which prints the text verbatim ` +
      `(api/page.tsx's curl blocks do this). Findings:\n  ${lines.join("\n  ")}`,
  );
});
