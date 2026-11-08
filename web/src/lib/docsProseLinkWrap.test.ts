// Regression guard: no /docs PROSE ANCHOR may render an unbreakable URL run
// without a break guard (card t_1f7bda12).
//
// THE DEFECT (measured on a production build of the tree that fixes the prose
//   inline <code> tokens — PR #1558, BUILD_ID zTqU4IpFRgQfHFRpOzRdj — Chromium
//   390x844 dsf2, root font 24px via CDP Page.setFontSizes, fresh browser
//   state, lang=en):
//
//     /docs/api at a 360px viewport: scrollWidth 379 vs clientWidth 360 (19px pan)
//       the ONLY out-of-bounds element: <a>https://jomform.com/openapi.json</a>
//       — right edge 373, width 337, computed overflow-wrap: normal
//     /docs/api at a 320px viewport: scrollWidth 379 vs clientWidth 320 (59px pan)
//
//   The intro paragraph prints the ABSOLUTE URL of the OpenAPI contract as the
//   anchor's own text (`{OPENAPI_PUBLIC_URL}`, api/page.tsx). That value is
//   `https://jomform.com/openapi.json` — 32 characters, ONE whitespace-free run.
//   With no break opportunity the anchor is as wide as its glyphs (337px at a
//   24px root font), which is wider than the ~296px prose column at a 360px
//   viewport, so the run floored the column and the whole page panned. At
//   390px/24px it happens to fit (right edge 348) — which is why the 390px
//   reading the parent card fixed was clean while this one still panned.
//
//   Injection control on that same build: adding
//   `p a[href*='openapi.json'] { overflow-wrap: anywhere }` gave 360 == 360 with
//   0 out-of-bounds descendants; removing it restored 379. So the anchor alone
//   accounted for the 360px pan.
//
// THE FIX: `[overflow-wrap:anywhere]` on the anchor's className, the same rule
//   the sibling surfaces already carry (src/lib/docsMcpProseWrap.test.ts for the
//   /docs/mcp prose <code> tokens, src/lib/docsApiProseWrap.test.ts for the
//   /docs/api ones). `anywhere` rather than `break-word` because it also drops
//   the element's min-content width to one character, so the link can shrink
//   INSIDE the column instead of flooring it. The URL is NOT shortened: the link
//   still reads https://jomform.com/openapi.json in full, wrapped over lines.
//
// WHAT THIS FILE PINS, AND WHY IT IS A SOURCE CONTRACT TEST
//   The repo has no DOM/JSX unit runner (no vitest/jest) and the Playwright a11y
//   gate cannot measure layout deformation, so the pixel numbers above were
//   measured in a real browser against a production build and are re-checked in
//   the PR. What a unit test can pin is the class that carries the behaviour and
//   the fact that the URL that needed it is still the full URL:
//
//     1. the AST extractor really reads the docs tree and really finds the one
//        prose anchor that renders a URL (it cannot pass vacuously), and it
//        really skips a code sample (proved on a probe source, because the real
//        tree has no <a> inside <pre> to exercise the skip on);
//     2. EVERY prose anchor in the docs tree whose rendered text is one
//        whitespace-free absolute URL carries a break guard — a new unguarded
//        URL link fails here, not in a browser 200px from now;
//     3. the constant is still a whitespace-free https URL of at least the
//        measured length, so shortening the URL to fit the column is not a green
//        path (the page prints the constant, so a shortened literal would have
//        to replace `{OPENAPI_PUBLIC_URL}` and that fails assertion 2's own
//        extractor: the text would no longer resolve to the URL);
//     4. BOTH anchors that point at the contract carry the guard. This one is a
//        CONVENTION pin, not a measured requirement: the versioning anchor
//        prints the words "openapi.json" (12 characters, cannot pan), but the
//        two contract anchors have already diverged once — that divergence is
//        exactly how the intro one ended up printing the raw URL — so the page
//        cannot start panning again merely because a later edit swaps the
//        versioning label back to the URL.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It reads the source, not the served HTML. Whether the class really
//     compiles to `overflow-wrap: anywhere` (Tailwind arbitrary property) and
//     whether the column then measures 360 == 360 is the browser measurement in
//     the PR, not this file.
//   * It only judges ANCHORS whose rendered text is an absolute URL. The same
//     defect class in a <span>/<td> (measured elsewhere in the tree: shipping and
//     customize print long REST paths in plain <span>/<td> cells) is a different
//     card, and a tree-wide "every long run" rule would be red on pages this
//     card does not touch.
//   * It cannot tell a URL apart from any other 28+ character unbreakable token
//     written as an anchor's text; it keys on `https?://`, which is the shape
//     that actually occurs and the shape a reader is sent to.
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
// The value the page prints. Imported rather than re-typed: the anchor's text is
// `{OPENAPI_PUBLIC_URL}`, so this is the string a reader sees.
import * as openapi from "./openapi.ts";

const DOCS_DIR = fileURLToPath(new URL("../app/docs", import.meta.url));

/** A class name that permits a break inside an unbreakable run. */
const BREAK_GUARD = /\[overflow-wrap:(?:anywhere|break-word)\]|\bbreak-words\b|\bbreak-all\b/;

/** A prose anchor's rendered text is ONE absolute URL and nothing else. */
const URL_RUN = /^https?:\/\/\S+$/;

/**
 * Resolve a JSX expression identifier to the string the page would print.
 *
 * Only the OpenAPI contract's own exported constants are resolvable, and that is
 * deliberate: those are the strings this defect is about (a published URL). An
 * identifier this map does not know makes the element's text UNKNOWN, and an
 * unknown text is not judged — an anchor rendering `{slug}` cannot be a 337px
 * absolute URL, and guessing what some other module exports would let this file
 * certify a link it never read.
 */
function resolveIdentifier(name: string): string | undefined {
  const v = (openapi as unknown as Record<string, unknown>)[name];
  return typeof v === "string" ? v : undefined;
}

/** The element's className, and whether it could be read as a literal string. */
function classAttr(el: ts.JsxOpeningElement | ts.JsxSelfClosingElement): {
  known: boolean;
  value: string;
} {
  for (const a of el.attributes.properties) {
    if (!ts.isJsxAttribute(a) || a.name.getText() !== "className") continue;
    const init = a.initializer;
    if (init && ts.isStringLiteral(init)) return { known: true, value: init.text };
    // A computed / templated className cannot be certified by this guard. It is
    // reported as UNKNOWN and the rule below refuses it rather than skipping it,
    // so a future author cannot dodge the guard by moving the class into an
    // expression — the same argument docsApi.test.ts makes for a spread <a>.
    return { known: false, value: init?.getText() ?? "" };
  }
  return { known: true, value: "" };
}

/** True when the element sits inside a code sample (<code>/<pre>). */
function insideCodeSample(node: ts.Node): boolean {
  for (let p = node.parent; p; p = p.parent) {
    if (ts.isJsxElement(p)) {
      const name = p.openingElement.tagName.getText();
      if (name === "code" || name === "pre") return true;
    }
  }
  return false;
}

/**
 * The text an anchor renders, or null when it is not one plain run.
 *
 * A nested element or an expression this file cannot resolve returns null: the
 * text the reader sees is then not a single URL run, and this guard refuses to
 * guess at it.
 */
function textOf(el: ts.JsxElement): string | null {
  const parts: string[] = [];
  for (const c of el.children) {
    if (ts.isJsxText(c)) {
      parts.push(c.text);
      continue;
    }
    if (ts.isJsxExpression(c)) {
      const e = c.expression;
      if (!e) continue; // a {/* comment */} renders nothing
      if (ts.isStringLiteral(e) || ts.isNoSubstitutionTemplateLiteral(e)) {
        parts.push(e.text);
        continue;
      }
      if (ts.isIdentifier(e)) {
        const v = resolveIdentifier(e.text);
        if (v === undefined) return null;
        parts.push(v);
        continue;
      }
      return null;
    }
    return null; // a nested element or fragment: not one plain text run
  }
  return parts.join("").replace(/\s+/g, " ").trim();
}

/** One prose anchor found in the docs tree. */
type Site = { file: string; line: number; className: string; classKnown: boolean; text: string };

/**
 * Every prose anchor in `src` whose rendered text is one absolute URL run.
 *
 * Anchors inside <code>/<pre> are SKIPPED: they are code samples, they scroll
 * horizontally by design, and `white-space: pre` ignores break properties — the
 * same exemption docsApiProseWrap.test.ts makes for prose <code> tokens.
 */
function urlTextAnchors(src: string, file: string): Site[] {
  const sf = ts.createSourceFile(file, src, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const found: Site[] = [];
  const visit = (node: ts.Node) => {
    if (ts.isJsxElement(node) && node.openingElement.tagName.getText() === "a") {
      const text = textOf(node);
      if (text !== null && URL_RUN.test(text) && !insideCodeSample(node)) {
        const cls = classAttr(node.openingElement);
        const { line } = sf.getLineAndCharacterOfPosition(node.getStart(sf));
        found.push({ file, line: line + 1, className: cls.value, classKnown: cls.known, text });
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return found;
}

const FILES = filesUnder(DOCS_DIR).filter((f) => f.endsWith(".tsx") && !f.endsWith(".d.ts"));
const SITES: Site[] = FILES.flatMap((f) => urlTextAnchors(readFileSync(f, "utf8"), f));
const rel = (f: string) => f.replace(DOCS_DIR, "src/app/docs");
const describe = (s: Site) => `${rel(s.file)}:${s.line}  ${JSON.stringify(s.text.slice(0, 80))}`;

test("the extractor reads the real docs tree and finds the URL anchor (it cannot pass vacuously)", () => {
  // The walk must really cover the pages: a glob that matched nothing would let
  // the rule below pass while looking at no anchor at all.
  assert.ok(
    FILES.length >= 15,
    `found only ${FILES.length} .tsx files under ${DOCS_DIR} — the guard is not looking at the docs tree`,
  );
  const apiPage = FILES.find((f) => f.endsWith("/docs/api/page.tsx"));
  assert.ok(apiPage, "docs/api/page.tsx is not in the walked set — the page this card is about is not being read");

  // And it must really find the site this card is about, resolved through the
  // identifier map: the anchor prints `{OPENAPI_PUBLIC_URL}`, so the extractor
  // resolving that expression to the URL is the whole mechanism under test.
  assert.ok(
    SITES.some((s) => s.file === apiPage && s.text === openapi.OPENAPI_PUBLIC_URL),
    `the /docs/api intro anchor rendering {OPENAPI_PUBLIC_URL} was not found among the ` +
      `${SITES.length} URL-text anchors this extractor read ` +
      `(${JSON.stringify(SITES.map(describe))}) — the identifier resolution or the walk is broken, ` +
      "so an unguarded URL link would be invisible to this guard",
  );

  // The page really prints the constant (not a shortened literal), so the site
  // above is the one a reader sees.
  const apiSrc = readFileSync(apiPage!, "utf8");
  assert.ok(
    apiSrc.includes("{OPENAPI_PUBLIC_URL}"),
    "docs/api/page.tsx no longer renders {OPENAPI_PUBLIC_URL} as text — this guard's known site moved",
  );

  // The <code>/<pre> skip is REAL, proved on a probe source: the docs tree has
  // no <a> inside a code sample today, so without this the exemption would be an
  // untested branch that could break (or swallow a real prose anchor) silently.
  const probe = `<div>
  <p>published at <a href="https://example.com/probe" className="underline">https://example.com/probe</a></p>
  <pre><a href="https://example.com/sample" className="underline">https://example.com/sample</a></pre>
  <p>and <a href={OPENAPI_PUBLIC_URL} className="underline">{OPENAPI_PUBLIC_URL}</a>.</p>
</div>`;
  const probeSites = urlTextAnchors(probe, "probe.tsx");
  assert.deepEqual(
    probeSites.map((s) => s.text),
    ["https://example.com/probe", openapi.OPENAPI_PUBLIC_URL],
    "the extractor did not read the probe exactly as designed — it must find a prose anchor with a " +
      "literal URL and one with a resolved constant, and must SKIP the anchor inside <pre> (a code " +
      "sample scrolls by design). Findings: " +
      JSON.stringify(probeSites.map((s) => s.text)),
  );
});

test("no /docs prose anchor renders an unbreakable URL without a break guard", () => {
  const unguarded = SITES.filter((s) => !s.classKnown || !BREAK_GUARD.test(s.className));
  assert.deepEqual(
    unguarded.map((s) => `${rel(s.file)}:${s.line}  className=${JSON.stringify(s.className)}  ${s.text}`),
    [],
    `these /docs anchors render an absolute URL as their own text, one whitespace-free run, with no ` +
      `break guard. Measured on a production build at Chromium 390x844 dsf2 with the browser default ` +
      `font size raised to 24px: /docs/api printed https://jomform.com/openapi.json in exactly this ` +
      `shape (337px wide at a 360px viewport, computed overflow-wrap: normal), which floored the ~296px ` +
      `prose column and panned the page 19px (373 vs 360); at 320px the same run panned 59px. Add ` +
      `[overflow-wrap:anywhere] to the anchor's className — "anywhere", not "break-word", because it ` +
      `also drops the element's min-content width to one character so the link shrinks INTO the column ` +
      `instead of widening it. Do NOT shorten the URL: the reader must still be able to read and copy ` +
      `the address. A computed className cannot be certified by this guard and is reported here too; ` +
      `write the class out as a literal string. ` +
      `Findings:\n  ${SITES.map(describe).join("\n  ")}`,
  );
});

test("the contract URL is still the full URL that needed the guard (shortening it is not a fix)", () => {
  const url = openapi.OPENAPI_PUBLIC_URL;
  assert.ok(url.startsWith("https://"), `OPENAPI_PUBLIC_URL must be https (got ${url})`);
  assert.ok(
    !/\s/.test(url),
    `OPENAPI_PUBLIC_URL contains whitespace (${JSON.stringify(url)}) — it would wrap naturally and this ` +
      "guard would be asserting a run that cannot happen",
  );
  // 28 characters: the shortest absolute URL that measured wider than the prose
  // column at 360px/24px is the 32-character contract URL; anything at least this
  // long is the unbreakable shape this card is about. A shortened URL (e.g. a
  // bare host, or a path-only fragment) fails here rather than quietly making the
  // anchor fit.
  assert.ok(
    url.length >= 28,
    `OPENAPI_PUBLIC_URL is only ${url.length} characters (${JSON.stringify(url)}) — the URL was ` +
      "shortened. The anchor measured 337px at a 24px root font and that length is what the break " +
      "guard is for; shortening the published contract URL to fit the column would break readers' " +
      "copies of it.",
  );
  // And the page's anchor text is that URL, printed in full — the measurement's
  // premise, asserted rather than assumed.
  const site = SITES.find((s) => s.text === url);
  assert.ok(site, `no docs anchor renders ${url} any more — re-measure before deleting this guard`);
  assert.equal(
    site!.text,
    url,
    "the anchor's rendered text is not the full contract URL — the guard is protecting a shorter run",
  );
});

test("both contract anchors carry the break guard (convention pin, so the label can swap back)", () => {
  const apiPage = FILES.find((f) => f.endsWith("/docs/api/page.tsx"))!;
  const src = readFileSync(apiPage, "utf8");
  // An anchor whose href is the contract constant. Both attributes are read from
  // the source, which is what docsApi.test.ts already pins as the page's only two
  // absolute destinations.
  const anchors = [...src.matchAll(/<a\b([^>]*)>([\s\S]*?)<\/a>/g)].map((m) => ({ attrs: m[1], body: m[2] }));
  const contract = anchors.filter((a) => /href=\{OPENAPI_PUBLIC_URL\}/.test(a.attrs));
  assert.equal(
    contract.length,
    2,
    `docs/api/page.tsx has ${contract.length} anchors pointing at the contract, not 2 — this pin is ` +
      "reading the wrong set of anchors (docsApi.test.ts pins the same two)",
  );
  for (const [i, a] of contract.entries()) {
    assert.ok(
      BREAK_GUARD.test(a.attrs),
      `contract anchor ${i + 1} of 2 in docs/api/page.tsx has no break guard in its className ` +
        `(${JSON.stringify(a.attrs.replace(/\s+/g, " ").trim().slice(0, 120))}). The intro anchor prints ` +
        "the URL itself and needs it; the versioning anchor prints the words \"openapi.json\" and does " +
        "not need it for layout today — it carries it so that swapping its label back to the URL cannot " +
        "reintroduce the 360px pan. Keep both.",
    );
  }
});
