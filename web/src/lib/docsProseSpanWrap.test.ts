// Regression guard: no /docs PROSE <span> may render an unbreakable run without
// a break guard (card t_91474d8f — the <span> leg of t_1f7bda12's anchor leg).
//
// THE DEFECT (measured on a production build of the tree that carries the
//   /docs/api anchor fix, BUILD_ID At48aw0ziH4fZ1CrO-IUM, Chromium 844-high dsf2
//   mobile emulation, fresh browser state — Network.clearBrowserCookies +
//   localStorage.clear(), lang="en", storedLang null — root font 24px via CDP
//   Page.setFontSizes {standard:24,fixed:24}):
//
//   /docs/shipping   scrollWidth 561 vs clientWidth 360 at 360px (201px pan)
//                    scrollWidth 561 vs clientWidth 320 at 320px (241px pan)
//                    three prose <span>s outside <pre>, every one computed
//                    overflow-wrap: normal:
//                      "POST /v1/rest/shipping/couriers/{provider}/connect"      right 522.1  (page.tsx:266)
//                      "POST /v1/rest/shipping/couriers/{provider}/disconnect"   right 554.5  (page.tsx:268)
//                      "/api/shipping/easyparcel/callback"                       right 392.5  (page.tsx:270)
//   /docs/customize  scrollWidth 345 vs clientWidth 320 at 320px (25px pan)
//                      "your-workspace.jomform.com/your-form-slug"               right 327.7  (page.tsx:375)
//                    at 360px the document does NOT pan: the wide TOOLS table on
//                    that page is already clipped by its wrapper, so its 67
//                    out-of-bounds descendants are intentionally NOT fixed here.
//
//   A REST path or a host is ONE whitespace-free run, so ordinary word wrapping
//   has no break opportunity inside it. At the default 16px root font the run
//   fits the prose column; raise the browser's default font size (Chrome "Very
//   large" = 24px, the text-scaling a user turns on) and the glyph width scales
//   with it, the run floors the column, and the whole page pans sideways.
//
// THE FIX: `[overflow-wrap:anywhere]` on every prose <span> in the docs tree
//   that renders a literal whitespace-free run of >= MIN_RUN characters — the
//   same rule and the same token the sibling surfaces already carry
//   (docsMcpProseWrap.test.ts for /docs/mcp prose <code>, docsProseLinkWrap.test.ts
//   for prose <a>, docsApiProseWrap.test.ts for /docs/api prose <code>).
//   `anywhere` rather than `break-word` because it also drops the element's
//   min-content width to one character, so the span shrinks INTO the column
//   instead of flooring it. No text was shortened: the reader still gets the
//   full path/host, wrapped over lines.
//
// WHY THIS FILE IS SEPARATE FROM docsProseLinkWrap.test.ts
//   That file is the ANCHOR leg, added by t_1f7bda12, and it is not on `main`
//   yet (PR #1570 open). This card must not stack on an unmerged branch: a
//   separate file merges cleanly in either order, which is exactly the argument
//   t_1f7bda12 used when it did not widen docsApiProseWrap.test.ts. When both
//   have landed, one prose-wrap rule lives in each file: anchors there, spans
//   here.
//
// WHAT THIS FILE PINS, AND WHY IT IS A SOURCE CONTRACT TEST
//   The repo has no DOM/JSX unit runner (no vitest/jest) and the Playwright a11y
//   gate cannot measure layout deformation, so the pixel numbers above were
//   measured in a real browser against a production build and are re-checked in
//   the PR. What a unit test can pin is the class that carries the behaviour and
//   the fact that the paths that needed it are still the full paths:
//
//     1. the extractor really walks the docs tree, really finds the measured span
//        sites, and really implements its three skips (<pre>/<code>, a clipped or
//        horizontally scrolling box, an expression it cannot resolve) — proved on
//        a probe source, so no branch of it is untested;
//     2. EVERY prose <span> in the docs tree whose rendered literal run is
//        >= MIN_RUN characters carries a break guard — a new long run fails here
//        instead of in a browser 200px from now;
//     3. the REST paths and hosts this card measured are still rendered IN FULL
//        (a shortened token is not a fix; the reader must be able to read and
//        copy them), so a later edit that trims a path to make it fit fails
//        here.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It reads the source, not the served HTML. Whether the class compiles to
//     `overflow-wrap: anywhere` and whether the page then measures
//     scrollWidth == clientWidth is the browser measurement in the PR.
//   * A run whose length depends on a value this file cannot resolve (an imported
//     constant, a prop, `{t.name}`) is NOT judged: it is reported as unknown and
//     the flush at that point keeps the surrounding literal runs honest. Guessing
//     what an unknown expression prints would let this file certify text it never
//     read.
//   * It only judges <span>. The same defect in a <code>/<td> is guarded by the
//     sibling files named above; a run inside <pre> scrolls by design and is
//     skipped here on purpose.
//
// WHAT CHANGED AFTER THIS FILE WAS WRITTEN (and why it is not a weakening)
//   Nineteen /docs pages now render through src/app/docs/docsProse. Their prose
//   is `const SECTIONS: Section[]` data and the renderer emits the elements, so
//   the tree's literal long-run <span> count fell to zero and the measured
//   pages' tokens are `code` markers rendered as Inline's chip (or, for a bare
//   URL, plain text inside the block's <p>/<ul>/<ol>). The guard was re-pointed,
//   not relaxed:
//     * the measured tokens are still asserted IN FULL against the page's prose
//       (a shortened path fails), and each is proved to be one code token;
//     * the ELEMENT that decides whether each can break is now the renderer's
//       chip or one of its prose containers, and those are asserted to carry the
//       same `[overflow-wrap:anywhere]` (the containers matter because
//       overflow-wrap inherits and an unmarked run has no element of its own);
//     * the tree-wide <span> rule still runs over every hand-written page and is
//       proved non-vacuous against the chip surface plus the extractor probe;
//     * the non-span measurements (the tool-name <h3>, the `{s.body}` paragraph,
//       the text-3xl <h1>) are kept with their numbers and their pages — only the
//       element that prints them moved into the renderer.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import ts from "typescript";

// The recursive `app/docs` walk shared with docsProseConvention.test.ts,
// llmsFullCoverage.test.ts and sitemapCoverage.test.ts (card t_3a5aa9b5).
import { filesUnder } from "./docsRoutes.ts";
import { pageProse, usesDocsProse } from "./docsProseReader.ts";
import { parseInline } from "../app/docs/docsProseText.ts";

const DOCS_DIR = fileURLToPath(new URL("../app/docs", import.meta.url));

/** A class name that permits a break inside an unbreakable run. */
const BREAK_GUARD = /\[overflow-wrap:(?:anywhere|break-word)\]|\bbreak-words\b|\bbreak-all\b/;

/**
 * A box whose content cannot pan the document: it scrolls or clips on the x
 * axis. A long run inside one of these is not this card's defect — the
 * /docs/customize TOOLS table is clipped exactly this way and was left alone on
 * purpose.
 */
const EXEMPT_BOX = /(?:^|\s)overflow(?:-[xy])?-(?:auto|scroll|hidden)(?:\s|$)/;

/**
 * The shortest literal run this guard judges, in characters.
 *
 * The measured offenders are 33 (the EasyParcel callback path), 41 (the form URL
 * host), 45 and 48 (the courier POST paths, the longest whitespace-free run
 * inside each span). 24 is chosen below them — at a 24px
 * root font a `text-xs` (0.75rem = 18px) monospace advance is ~10.8px, so a
 * 24-character run is ~259px: wider than the ~228px prose column a 320px viewport
 * leaves once the page padding is taken out. The two 24/26-character identifier
 * spans this guard also covers (set_affiliate_commission, set_workspace_landing_page)
 * are the same unbreakable shape and are one character of data away from
 * overflowing, so they carry the guard rather than a second threshold.
 */
const MIN_RUN = 24;

const ENTITIES: Record<string, string> = {
  amp: "&",
  apos: "'",
  quot: '"',
  lt: "<",
  gt: ">",
  nbsp: " ",
  hellip: "…",
  mdash: "—",
  ndash: "–",
  lsquo: "‘",
  rsquo: "’",
  ldquo: "“",
  rdquo: "”",
};

/** Decode the HTML entities a JSX text node keeps verbatim, so runs read as rendered. */
function decode(text: string): string {
  return text.replace(/&#(\d+);|&([a-z]+);/g, (all, num: string, name: string) =>
    num ? String.fromCodePoint(Number(num)) : (ENTITIES[name] ?? all),
  );
}

/** The element's className, and whether it could be read as a literal string. */
function classAttr(el: ts.JsxOpeningElement | ts.JsxSelfClosingElement | ts.JsxClosingElement): {
  known: boolean;
  value: string;
} {
  if (!ts.isJsxOpeningElement(el) && !ts.isJsxSelfClosingElement(el)) return { known: true, value: "" };
  for (const a of el.attributes.properties) {
    if (!ts.isJsxAttribute(a) || a.name.getText() !== "className") continue;
    const init = a.initializer;
    if (init && ts.isStringLiteral(init)) return { known: true, value: init.text };
    // A computed / templated className cannot be certified by this guard. It is
    // reported as UNKNOWN and the rule refuses it rather than skipping it, so a
    // future author cannot dodge the guard by moving the class into an
    // expression — the same argument docsApi.test.ts makes for a spread <a>.
    return { known: false, value: init?.getText() ?? "" };
  }
  return { known: true, value: "" };
}

/** True when the element sits inside a code sample or a clipped/scrolling box. */
function inExemptBox(node: ts.Node): boolean {
  for (let p = node.parent; p; p = p.parent) {
    if (ts.isJsxElement(p) || ts.isJsxSelfClosingElement(p)) {
      const open = ts.isJsxElement(p) ? p.openingElement : p;
      const name = open.tagName.getText();
      if (name === "code" || name === "pre") return true;
      const cls = classAttr(open);
      if (cls.known && EXEMPT_BOX.test(cls.value)) return true;
    }
  }
  return false;
}

/**
 * The literal whitespace-free runs a span renders, and whether part of its text
 * could not be resolved.
 *
 * JSX text and a resolvable expression (a string literal or a template literal
 * with no substitution, e.g. {`{id}`}) are concatenated, because nothing between
 * them inserts a break opportunity. An expression this file cannot resolve flushes
 * the buffer and sets `unknown`: the run on either side is real, but how long the
 * whole rendered token is cannot be known here.
 */
function literalRuns(el: ts.JsxElement): { runs: string[]; text: string; unknown: boolean } {
  const runs: string[] = [];
  const parts: string[] = [];
  let buf = "";
  let unknown = false;
  const flush = () => {
    for (const run of decode(buf).split(/\s+/)) if (run) runs.push(run);
    buf = "";
  };
  for (const c of el.children) {
    if (ts.isJsxText(c)) {
      buf += c.text;
      continue;
    }
    if (ts.isJsxExpression(c)) {
      const e = c.expression;
      if (!e) continue; // a {/* comment */} renders nothing
      if (ts.isStringLiteral(e) || ts.isNoSubstitutionTemplateLiteral(e)) {
        buf += e.text;
        parts.push(e.text);
        continue;
      }
      unknown = true;
      flush();
      continue;
    }
    // A nested element or fragment: the text boundary is not one plain run.
    unknown = true;
    flush();
  }
  flush();
  return { runs, text: decode(parts.join("") + " " + runs.join(" ")), unknown };
}

/** One prose span found in the docs tree. */
type Site = {
  file: string;
  line: number;
  className: string;
  classKnown: boolean;
  longest: number;
  runs: string[];
  unknown: boolean;
};

/**
 * Every non-exempt <span> in `src` whose literal text carries a run of at least
 * MIN_RUN characters (or an unreadable className, which is reported regardless).
 */
function longRunSpans(src: string, file: string): Site[] {
  const sf = ts.createSourceFile(file, src, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const found: Site[] = [];
  const visit = (node: ts.Node) => {
    if (ts.isJsxElement(node) && node.openingElement.tagName.getText() === "span" && !inExemptBox(node)) {
      const { runs, unknown } = literalRuns(node);
      const longest = runs.reduce((n, r) => Math.max(n, r.length), 0);
      const cls = classAttr(node.openingElement);
      if (longest >= MIN_RUN || !cls.known) {
        const { line } = sf.getLineAndCharacterOfPosition(node.getStart(sf));
        found.push({ file, line: line + 1, className: cls.value, classKnown: cls.known, longest, runs, unknown });
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return found;
}

const FILES = filesUnder(DOCS_DIR).filter((f) => f.endsWith(".tsx") && !f.endsWith(".d.ts"));
const SITES: Site[] = FILES.flatMap((f) => longRunSpans(readFileSync(f, "utf8"), f));
const rel = (f: string) => f.replace(DOCS_DIR, "src/app/docs");
const describe = (s: Site) =>
  `${rel(s.file)}:${s.line}  className=${JSON.stringify(s.className)}  run=${s.longest}  ` +
  JSON.stringify(s.runs.reduce((a, b) => (b.length > a.length ? b : a), ""));

// ── the surface the measured sites MOVED TO (the prose renderer) ──────────────
//
// Nineteen /docs pages render through src/app/docs/docsProse now. Their inline
// runs are no longer hand-written <span>s: a backticked token becomes Inline's
// <code> chip, and an UNMARKED run (customize's bare MCP URL) is a plain text
// node inside the block's <p>/<ul>/<ol>. So the element that decides whether the
// run can break is the renderer's chip or its prose container, and that is where
// this guard has to look for a converted page. The page half stays: the token
// itself must still be there, in full (asserted below per measured site).
const RENDERER_FILE = fileURLToPath(new URL("../app/docs/docsProse.tsx", import.meta.url));
const RENDERER_SRC = readFileSync(RENDERER_FILE, "utf8");

/**
 * The renderer's SOURCE WITH COMMENTS STRIPPED. The tag scans below must not read
 * a tag a comment names — docsProse.tsx documents the very elements this guard is
 * about (`<p>/<ul>/<ol>`, `<pre>`, `<code>`), and a comment is text no reader ever
 * gets. The strip is deliberately the simple one used elsewhere in this suite: the
 * file holds no string literal containing `//` or a block-comment opener.
 */
const RENDERER_CODE = RENDERER_SRC.replace(/\/\*[\s\S]*?\*\//g, " ").replace(/^[ \t]*\/\/.*$/gm, " ");

/** The Inline function: the ONE place a prose marker becomes a chip. */
const INLINE_AT = RENDERER_CODE.indexOf("export function Inline(");
const INLINE_END = INLINE_AT === -1 ? -1 : RENDERER_CODE.indexOf("function Tip(", INLINE_AT);
const INLINE = INLINE_AT === -1 || INLINE_END === -1 ? "" : RENDERER_CODE.slice(INLINE_AT, INLINE_END);

const tagClass = (tag: string) => tag.match(/className="([^"]*)"/)?.[1] ?? "";

/** The renderer's inline <code> chips (one tag; every `code` marker goes through it). */
const CHIP_TAGS = [...INLINE.matchAll(/<code\b[^>]*>/g)].map((m) => m[0]);

/**
 * The renderer tags that RENDER prose text: a paragraph, a bullet/numbered list,
 * the block sub-heading and the page heading. `overflow-wrap` inherits, so the
 * token on one of these covers every descendant run — including a plain text
 * Fragment, which has no element of its own to guard. A table cell is NOT here:
 * a table sits inside an `overflow-x-auto` region (docsProse.tsx), so a long cell
 * scrolls inside its box instead of panning the document — the same exemption
 * EXEMPT_BOX makes for a hand-written page.
 */
const PROSE_CONTAINERS = ["p", "ul", "ol", "h1", "h3"] as const;
const PROSE_TAGS = PROSE_CONTAINERS.flatMap((name) => [
  ...RENDERER_CODE.matchAll(new RegExp(`<${name}\\b[^>]*>`, "g")),
]).map((m) => m[0]);

/** A long inline-code marker in a converted page's prose, and its longest run. */
type ChipSite = { file: string; text: string; longest: number };

function chipSitesIn(src: string, file: string): ChipSite[] {
  if (!usesDocsProse(src)) return [];
  const prose = pageProse(src, file);
  return [...prose.matchAll(/`([^`]+)`/g)]
    .map((m) => ({ file, text: m[1], longest: Math.max(0, ...m[1].split(/\s+/).map((r) => r.length)) }))
    .filter((c) => c.longest >= MIN_RUN);
}

const CHIP_SITES: ChipSite[] = FILES.flatMap((f) => chipSitesIn(readFileSync(f, "utf8"), f));
const describeChip = (c: ChipSite) => `${rel(c.file)}  chip=${JSON.stringify(c.text)}  run=${c.longest}`;

/**
 * The tokens this card was filed on, in full. Kept as the rendered text, so a
 * shortened path fails the assertion below instead of quietly fitting.
 *
 * `full` is the span's whole text (the HTTP verb and the path, one space apart);
 * `run` is its longest whitespace-free run, which is what this guard measures.
 */
const MEASURED = [
  {
    page: "/docs/shipping/page.tsx",
    full: "POST /v1/rest/shipping/couriers/{provider}/connect",
    run: "/v1/rest/shipping/couriers/{provider}/connect",
    longest: 45,
  },
  {
    page: "/docs/shipping/page.tsx",
    full: "POST /v1/rest/shipping/couriers/{provider}/disconnect",
    run: "/v1/rest/shipping/couriers/{provider}/disconnect",
    longest: 48,
  },
  {
    page: "/docs/shipping/page.tsx",
    full: "/api/shipping/easyparcel/callback",
    run: "/api/shipping/easyparcel/callback",
    longest: 33,
  },
  {
    page: "/docs/customize/page.tsx",
    full: "your-workspace.jomform.com/your-form-slug",
    run: "your-workspace.jomform.com/your-form-slug",
    longest: 41,
  },
];

const rendered = (s: Site) => s.runs.reduce((a, b) => (b.length > a.length ? b : a), "");

/**
 * The non-<span> sites this card also had to guard, and why.
 *
 * The card was filed on prose spans and the leg above is the tree-wide rule for
 * them. But wrapping the spans was NOT enough for the acceptance the card states
 * (`scrollWidth == clientWidth` at 320/360/390px): re-measured on a production
 * build of this branch at a 24px root font, /docs/shipping still panned 16px at
 * 360px and 56px at 320px from the tool-name <h3>s, and /docs/customize still
 * panned 25px at 320px from three bare runs that are not in any span. Same
 * mechanism (one whitespace-free run, no break opportunity), different element,
 * so the element carries the same guard.
 *
 * This leg is an explicit measured list rather than a tree-wide rule: "every
 * element with a long word" would red pages this card never measured, and the
 * honest statement is which sites were measured and why.
 *
 * THE ELEMENTS MOVED, THE TOKENS DID NOT. Both pages render through docsProse
 * now, so there is no `<h3>{t.name}</h3>` and no `<p>{s.body}</p>` to read: the
 * tool name is a `code` chip in a table cell, and the URL sits in a prose
 * list/paragraph. `carrier` says which of the renderer's two guarded shapes the
 * token goes through today — a marker (the Inline chip) or plain text (the
 * block's prose container). The page half is the CONTENT: the token must still be
 * there, in full, or the "fix" was shortening it.
 */
const NON_SPAN_SITES = [
  {
    page: "/docs/shipping/page.tsx",
    token: "disconnect_shipping_courier",
    carrier: "chip",
    why:
      "it is the tool name in mono at 21px under a 24px root font: " +
      '"disconnect_shipping_courier" (27 chars) measured 340px wide in the 288px prose column at a ' +
      "320px viewport, which panned /docs/shipping 16px at 360px and 56px at 320px",
  },
  {
    page: "/docs/customize/page.tsx",
    token: "https://mcp.jomform.com/mcp.",
    carrier: "prose",
    why:
      "it is the bare URL the STEPS body carries " +
      '("https://mcp.jomform.com/mcp.", 28 chars, 309px measured) under a 24px root font, which ' +
      "panned /docs/customize 25px at a 320px viewport",
  },
] as const;

test("the non-span sites this card measured carry the guard too (they floored the same pages)", () => {
  for (const site of NON_SPAN_SITES) {
    const page = FILES.find((f) => f.endsWith(site.page));
    assert.ok(page, `${site.page} is missing from the walked docs tree`);
    const src = readFileSync(page!, "utf8");
    // Page half: the token is still rendered, in full.
    assert.ok(
      pageProse(src, page!).includes(site.token),
      `${site.page} no longer renders ${JSON.stringify(site.token)} — the element this card measured has ` +
        `moved or the token was trimmed; re-measure before deleting this pin`,
    );
    // Carrier half: which renderer shape it goes through, and that shape carries
    // the guard (asserted once, for every chip and container, in the test below).
    if (site.carrier === "chip") {
      assert.ok(
        src.includes("`" + site.token + "`"),
        `${site.page}: ${JSON.stringify(site.token)} is no longer written as a code marker, so it does not ` +
          `go through the renderer's guarded chip — it needs a break guard on whatever element prints it, ` +
          `because ${site.why}`,
      );
    } else {
      assert.ok(
        !src.includes("`" + site.token + "`"),
        `${site.page}: ${JSON.stringify(site.token)} is a code marker now, not plain prose — this pin says its ` +
          `carrier is the prose container; re-point it if the token was deliberately marked`,
      );
    }
  }

  // The heading the 320px measurement found: a normal English word at text-3xl
  // (45px under a 24px root font) is wider than the 288px prose column, so the
  // page's <h1> carries the same guard. The <h1> is the renderer's now (the page
  // passes `title`), so the page half is the word it prints and the element half
  // is the renderer's heading.
  const custPage = FILES.find((f) => f.endsWith("/docs/customize/page.tsx"))!;
  const cust = readFileSync(custPage, "utf8");
  const title = cust.match(/\btitle="([^"]*)"/)?.[1] ?? "";
  assert.ok(
    title.includes("customization"),
    "docs/customize/page.tsx no longer passes a title containing the word that was measured out of bounds " +
      "(\"customization\", 298px at a 24px root font) — re-measure before deleting this guard",
  );
  const h1Tags = PROSE_TAGS.filter((t) => /^<h1\b/.test(t));
  assert.ok(
    h1Tags.length >= 1,
    "the renderer emits no <h1> — docs/customize's heading is rendered somewhere this guard cannot see",
  );
  for (const tag of h1Tags) {
    assert.ok(
      BREAK_GUARD.test(tagClass(tag)),
      "the renderer's <h1> has no break guard. Its longest word (\"customization\", 13 chars) measured " +
        "298px wide at a 24px root font — 10px wider than the 288px prose column at a 320px viewport, where " +
        "docs/customize panned 25px. Add [overflow-wrap:anywhere].",
    );
  }

  // Load-bearing, in both directions: the runs that need the guard are still in
  // the data. Deleting the sentence that carries a long URL is not the fix this
  // card asked for.
  const ship = readFileSync(FILES.find((f) => f.endsWith("/docs/shipping/page.tsx"))!, "utf8");
  assert.ok(
    pageProse(ship, "shipping").includes("disconnect_shipping_courier"),
    'docs/shipping no longer documents a tool named "disconnect_shipping_courier" (27 chars) — that ' +
      "name is what measured 340px wide; if it was renamed, re-measure the new one",
  );
  for (const token of ["https://mcp.jomform.com/mcp.", "set_workspace_landing_page"]) {
    assert.ok(
      pageProse(cust, "customize").includes(token),
      `docs/customize no longer contains ${JSON.stringify(token)} — the docs lost the sentence this ` +
        "card measured instead of letting it wrap",
    );
  }
});

test("the renderer's inline chip and prose containers carry the break guard (where the runs moved)", () => {
  // Non-vacuity first: the surfaces this leg is about must be READ, not assumed.
  assert.ok(
    INLINE.includes("export function Inline"),
    "docsProse.tsx no longer declares the Inline function — the chip every converted page's prose token " +
      "renders through is not the code this guard is reading",
  );
  assert.ok(
    CHIP_TAGS.length >= 1,
    "the Inline slice of docsProse.tsx holds no <code> tag — a backticked token would render as bare text",
  );
  assert.ok(
    PROSE_TAGS.length >= 5,
    `found only ${PROSE_TAGS.length} prose container tag(s) (${PROSE_CONTAINERS.join("/")}) in docsProse.tsx — ` +
      "the tags that render an UNMARKED run are not being read, so a bare URL there would be invisible",
  );

  const unguarded = [...CHIP_TAGS, ...PROSE_TAGS].filter((t) => !BREAK_GUARD.test(tagClass(t)));
  assert.deepEqual(
    unguarded.map((t) => `${t.slice(0, 60)}…  className=${JSON.stringify(tagClass(t))}`),
    [],
    `the renderer emits ${unguarded.length} prose element(s) with no break guard. This is the defect the card ` +
      `measured, on the element that renders it now: at a 24px root font a REST path or host is ONE ` +
      `whitespace-free run wider than the 288px column, and at 320px it floored /docs/shipping by 241px and ` +
      `/docs/customize by 25px. The inline chip guards the marked tokens; the prose containers guard the ` +
      `unmarked ones (overflow-wrap inherits, and a plain text node has no element of its own). Use ` +
      `[overflow-wrap:anywhere] — not break-word — because it also drops the min-content width to one ` +
      `character so the run shrinks INTO the column.`,
  );

  // And the tree really carries long runs on that surface, so the assertions
  // above are guarding something: the chips found by the same MIN_RUN the span
  // rule uses.
  assert.ok(
    CHIP_SITES.length >= 3,
    `found only ${CHIP_SITES.length} ${MIN_RUN}+ character inline-code run(s) in the converted pages' prose ` +
      `(${CHIP_SITES.map(describeChip).join("; ")}) — the runs this card measured are markers on that ` +
      `surface, so a count this low means the guard is no longer looking at them`,
  );
});

test("the extractor reads the real docs tree and finds the measured spans (it cannot pass vacuously)", () => {
  assert.ok(
    FILES.length >= 15,
    `found only ${FILES.length} .tsx files under ${DOCS_DIR} — the guard is not looking at the docs tree`,
  );
  for (const page of ["/docs/shipping/page.tsx", "/docs/customize/page.tsx"]) {
    assert.ok(
      FILES.some((f) => f.endsWith(page)),
      `${page} is not in the walked set — the pages this card measured are not being read`,
    );
  }

  // The exact tokens this card was filed on must still be found, in full, and
  // each must be the `code` marker whose chip the renderer guards. Before the
  // conversion this looked for a <span>; the token's element is the chip now, and
  // the run length is measured on the token so a trimmed path still fails.
  for (const m of MEASURED) {
    const page = FILES.find((f) => rel(f).endsWith(m.page));
    assert.ok(page, `${m.page} is not in the walked set — the page this card measured is not being read`);
    const prose = pageProse(readFileSync(page!, "utf8"), page!);
    assert.ok(
      prose.includes(m.full),
      `the token ${JSON.stringify(m.full)} is no longer rendered in full on ${m.page} — either the path was ` +
        "shortened (the reader must still get the full address) or it moved out of the prose this guard can " +
        "see; re-measure before relaxing this pin",
    );
    const longest = Math.max(...m.full.split(/\s+/).map((r) => r.length));
    assert.equal(
      longest,
      m.longest,
      `${m.page}'s ${JSON.stringify(m.run)} now resolves to a ${longest}-character run, not ${m.longest} — ` +
        "the token measured is not the token being read",
    );
    const tokens = parseInline("`" + m.full + "`");
    assert.deepEqual(
      tokens.map((t) => t.kind),
      ["code"],
      `${m.page}'s ${JSON.stringify(m.full)} is not read by the renderer as one code token — the chip ` +
        "assertion would be guarding an element this page's prose never reaches",
    );
    assert.equal(tokens[0].text, m.full, `${m.page}: the chip would print a shortened token`);
  }

  // The three SKIPS are real, proved on a probe source: the docs tree happens to
  // have no long span inside <pre> or inside its clipped table today, so without
  // this the exemptions would be untested branches that could swallow a real
  // prose span silently.
  const LONG = "POST /v1/rest/shipping/couriers/&#123;provider&#125;/connect";
  const probe = `<div>
  <p><span className="font-mono text-xs">${LONG}</span></p>
  <p><span className="font-mono text-xs [overflow-wrap:anywhere]">${LONG}</span></p>
  <p><span className="font-mono text-xs">short_one</span></p>
  <pre><span className="font-mono text-xs">${LONG}</span></pre>
  <div className="mt-4 overflow-x-auto"><span className="font-mono text-xs">${LONG}</span></div>
  <p><span className="font-mono text-xs">{tool.name}</span></p>
  <p><span className="font-mono text-xs">PUT /v1/rest/templates/{\`{template_id}\`}</span></p>
</div>`;
  const probeSites = longRunSpans(probe, "probe.tsx");
  assert.deepEqual(
    probeSites.map((s) => rendered(s)),
    [
      "/v1/rest/shipping/couriers/{provider}/connect",
      "/v1/rest/shipping/couriers/{provider}/connect",
      "/v1/rest/templates/{template_id}",
    ],
    "the extractor did not read the probe exactly as designed: it must report the unguarded long span " +
      "and the resolvable template-literal span, SKIP the guarded one (not a finding), SKIP the short " +
      "run, SKIP the span inside <pre> (a code sample scrolls by design) and SKIP the span inside the " +
      "overflow-x-auto box (it cannot pan the document), and an unresolvable {tool.name} contributes no " +
      "literal run. Findings: " + JSON.stringify(probeSites.map(describe)),
  );
  assert.equal(
    probeSites[0].unknown || probeSites[2].unknown,
    false,
    "the probe's literal spans were marked unknown — the resolvable-expression path is not working",
  );
});

test("no /docs prose <span> renders an unbreakable run without a break guard", () => {
  // The subject must EXIST in one of the two shapes, or this rule proves nothing:
  // a hand-written long-run span (what remains of the original surface — 0 today,
  // because nineteen pages moved to the renderer) or a long inline-code run on the
  // renderer's chip surface. A tree with neither would pass the deepEqual below
  // while rendering nothing this guard cares about.
  assert.ok(
    SITES.length + CHIP_SITES.length > 0,
    `found no ${MIN_RUN}+ character run at all — neither a prose <span> nor an inline code marker in a ` +
      `converted page's prose. The runs this card measured are still in the docs (as chips), so the scan is ` +
      `looking at the wrong surface and the rule below would pass vacuously`,
  );
  const unguarded = SITES.filter((s) => !s.classKnown || !BREAK_GUARD.test(s.className));
  assert.deepEqual(
    unguarded.map((s) => `${rel(s.file)}:${s.line}  className=${JSON.stringify(s.className)}  ${rendered(s)}`),
    [],
    `these /docs <span>s render a ${MIN_RUN}+ character whitespace-free run with no break guard. ` +
      "Measured on a production build at Chromium 844-high dsf2 with the browser default font size " +
      "raised to 24px: /docs/shipping panned 201px at a 360px viewport (561 vs 360) and 241px at " +
      "320px, and /docs/customize panned 25px at 320px (345 vs 320), with exactly these spans " +
      "out of bounds and computed overflow-wrap: normal. Add [overflow-wrap:anywhere] to the span's " +
      'className — "anywhere", not "break-word", because it also drops the element\'s min-content ' +
      "width to one character so the token shrinks INTO the column instead of widening it. Do NOT " +
      "shorten the path or host: the reader must still be able to read and copy it. A computed " +
      "className cannot be certified by this guard and is reported here too; write the class out " +
      `as a literal string. Findings:\n  ${SITES.map(describe).join("\n  ")}` +
      `\n\nThe renderer-backed pages carry the same kind of run as inline-code chips; their guard is ` +
      `asserted against docsProse.tsx in the test above (${CHIP_SITES.length} long chip run(s) found ` +
      `now: ${CHIP_SITES.map(describeChip).join("; ") || "none"}).`,
  );
});

test("the measured paths are still rendered in full and carry the guard (shortening is not a fix)", () => {
  for (const m of MEASURED) {
    const page = FILES.find((f) => f.endsWith(m.page));
    assert.ok(page, `${m.page} is missing from the walked docs tree`);
    const src = readFileSync(page!, "utf8");
    const markers = [...pageProse(src, page!).matchAll(/`([^`]+)`/g)].map((x) => x[1]);
    const holders = markers.filter((token) => token === m.full);
    assert.equal(
      holders.length,
      1,
      `${m.page} renders ${JSON.stringify(m.full)} in ${holders.length} code chip(s), not 1 — either the ` +
        "path was shortened (the reader must still get the full address) or it moved out of a code marker " +
        "this guard can see; re-measure before relaxing this pin",
    );
    // The chip is the element that prints it now; the guard it must carry is the
    // renderer's, asserted globally but named here against this exact token.
    for (const tag of CHIP_TAGS) {
      assert.ok(
        BREAK_GUARD.test(tagClass(tag)),
        `${m.page}'s ${JSON.stringify(m.full)} is rendered by the renderer's inline chip, whose className ` +
          `(${JSON.stringify(tagClass(tag))}) has no break guard. This is the exact token the card measured ` +
          "out of bounds; it must carry [overflow-wrap:anywhere].",
      );
    }
  }
});
