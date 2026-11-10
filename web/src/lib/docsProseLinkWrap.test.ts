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
//   The intro paragraph printed the ABSOLUTE URL of the OpenAPI contract as the
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
//   INSIDE the column instead of flooring it. The URL is NOT shortened: a link
//   to the contract still points at https://jomform.com/openapi.json in full.
//
// WHAT THIS FILE PINS, AND WHY IT IS A SOURCE CONTRACT TEST
//   The repo has no DOM/JSX unit runner (no vitest/jest) and the Playwright a11y
//   gate cannot measure layout deformation, so the pixel numbers above were
//   measured in a real browser against a production build and are re-checked in
//   the PR. What a unit test can pin is the shape that carries the behaviour and
//   the fact that the URL that needed it is still the full URL:
//
//     1. the extractors really read the docs tree and really find the one prose
//        anchor that prints a URL (it cannot pass vacuously), and they really
//        skip a code sample (proved on a probe source, because the real tree has
//        no <a> inside <pre> to exercise the skip on);
//     2. EVERY prose anchor in the docs tree whose rendered text is one
//        whitespace-free absolute URL carries a break guard — a new unguarded
//        URL link fails here, not in a browser 200px from now;
//     3. the constant is still a whitespace-free https URL of at least the
//        measured length, so shortening the URL to fit the column is not a green
//        path;
//     4. the /docs/api contract link is still a link to the constant, and its
//        LABEL is not the URL. This one is a CONVENTION pin: the versioning line
//        already prints the words "openapi.json" (12 characters, cannot pan),
//        and the intro anchor that printed the raw URL is gone — but the two have
//        diverged once, and that divergence is exactly how the page ended up
//        printing a 32-character run in an anchor, so the page cannot drift back
//        without going red.
//
// SINCE THE PROSE RENDERER
//   The /docs pages are `const SECTIONS` literals rendered by
//   src/app/docs/docsProse, so a prose link is now a MARKDOWN LINK in a string —
//   `[label](href)` — and the <a> exists exactly ONCE, in the renderer. The
//   defect class therefore has two shapes and this file judges both:
//
//     * a hand-written JSX anchor whose text is a URL (the original shape; the
//       tree currently has none, and the extractor is proved on a probe);
//     * a prose link whose LABEL is a URL (the new shape — the anchor's rendered
//       text is the label, so a 32-character label is a 32-character unbreakable
//       run, and its wrapping then depends on whatever container it lands in).
//
//   The renderer's anchor is pinned as the single render site: its href comes
//   from the parsed token, its className is a literal, and its text is the
//   token's label — so a page cannot smuggle in a destination or a class.
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
// Who renders a converted page's prose, and whether THIS page is converted.
import { usesDocsProse } from "./docsProseReader.ts";
// The value the page links to. Imported rather than re-typed: the page's link is
// `[openapi.json](${OPENAPI_PUBLIC_URL})`, so this is the destination a reader
// follows.
import * as openapi from "./openapi.ts";

const DOCS_DIR = fileURLToPath(new URL("../app/docs", import.meta.url));
const RENDERER = `${DOCS_DIR}/docsProse.tsx`;

/** A class name that permits a break inside an unbreakable run. */
const BREAK_GUARD = /\[overflow-wrap:(?:anywhere|break-word)\]|\bbreak-words\b|\bbreak-all\b/;

/** A prose anchor's rendered text is ONE absolute URL and nothing else. */
const URL_RUN = /^https?:\/\/\S+$/;

/** A prose link: `[label](href)` — the shape a converted page writes. */
const PROSE_LINK = /\[([^\]\n]+)\]\(([^)\s]*)\)/g;

/** The label of a prose link that is a bare URL is as wide as the URL itself. */
const MIN_URL_LABEL = 28;

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
    return null; // a nested element, a fragment or a spread: not one plain run
  }
  return parts.join("").replace(/\s+/g, " ").trim();
}

/**
 * One prose anchor found in the docs tree.
 *
 * `kind` says which shape it is:
 *   jsx   — a hand-written `<a>`; its class is on the element and is judged.
 *   label — a `[label](href)` prose link in a converted page; the page cannot
 *           put a class on it, the renderer's <a> is the element a reader sees.
 */
type Site = {
  kind: "jsx" | "label";
  file: string;
  line: number;
  className: string;
  classKnown: boolean;
  text: string;
};

function lineOf(src: string, index: number): number {
  return src.slice(0, index).split("\n").length;
}

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
        found.push({ kind: "jsx", file, line: line + 1, className: cls.value, classKnown: cls.known, text });
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return found;
}

/**
 * Every prose link in `src` whose LABEL is one absolute URL run.
 *
 * This is the converted-page shape of the same defect: the label is the anchor's
 * rendered text, so `[https://…](https://…)` prints a 32-character unbreakable
 * run — and the renderer's <a> has no break guard of its own.
 */
function urlLabelLinks(src: string, file: string): Site[] {
  const found: Site[] = [];
  for (const m of src.matchAll(PROSE_LINK)) {
    const label = m[1].replace(/\s+/g, " ").trim();
    if (!URL_RUN.test(label)) continue;
    found.push({
      kind: "label",
      file,
      line: lineOf(src, m.index ?? 0),
      className: "",
      classKnown: false,
      text: label,
    });
  }
  return found;
}

const FILES = filesUnder(DOCS_DIR).filter((f) => f.endsWith(".tsx") && !f.endsWith(".d.ts"));
const SITES: Site[] = FILES.flatMap((f) => {
  const src = readFileSync(f, "utf8");
  return [...urlTextAnchors(src, f), ...urlLabelLinks(src, f)];
});
/** Every prose link in the tree, URL-labelled or not — the label scan's own body. */
const LINKS = FILES.flatMap((f) =>
  [...readFileSync(f, "utf8").matchAll(PROSE_LINK)].map((m) => ({
    file: f,
    label: m[1].trim(),
    href: m[2],
  })),
);
const rel = (f: string) => f.replace(DOCS_DIR, "src/app/docs");
// Card t_0356ed4c: /docs/api was split into a hub plus four task pages, and the
// contract link now lives on /docs/api/errors. So the guard finds the page that
// CARRIES the link instead of naming a path that a docs edit may legitimately
// move — "the page that links the contract" is the fact being pinned, not the
// file name it happens to sit in.
const CONTRACT_LINK_TOKEN = "${OPENAPI_PUBLIC_URL}";
const contractPages = () =>
  FILES.filter((f) =>
    [...readFileSync(f, "utf8").matchAll(PROSE_LINK)].some((m) => m[2] === CONTRACT_LINK_TOKEN),
  );
const describe = (s: Site) =>
  `${rel(s.file)}:${s.line}  ${s.kind === "label" ? "link label " : ""}${JSON.stringify(s.text.slice(0, 80))}`;

test("the extractor reads the real docs tree and finds the URL anchor (it cannot pass vacuously)", () => {
  // The walk must really cover the pages: a glob that matched nothing would let
  // the rule below pass while looking at no anchor at all.
  assert.ok(
    FILES.length >= 15,
    `found only ${FILES.length} .tsx files under ${DOCS_DIR} — the guard is not looking at the docs tree`,
  );
  const apiPage = contractPages()[0];
  assert.ok(
    apiPage,
    "no docs page renders a prose link to \\${OPENAPI_PUBLIC_URL} — the page this card is about " +
      "is not being read, or the contract link moved and this guard did not follow it",
  );

  // The site this card is about MOVED: the intro anchor that printed the raw URL
  // is gone (its line now prints the short label "openapi.json"), and the page
  // renders through the prose renderer, so the contract link is a markdown link
  // whose destination is the constant. Prove that new shape is really there: if
  // the link moves again, this arm must fail rather than certify a page that no
  // longer links the contract at all.
  const apiSrc = readFileSync(apiPage!, "utf8");
  assert.ok(
    usesDocsProse(apiSrc),
    "docs/api/page.tsx no longer renders through src/app/docs/docsProse — its links are no " +
      "longer markdown links in a prose string, so this guard's known site moved",
  );
  const contractLink = [...apiSrc.matchAll(PROSE_LINK)].filter(
    (m) => m[2] === "${OPENAPI_PUBLIC_URL}",
  );
  assert.ok(
    contractLink.length >= 1,
    `docs/api/page.tsx has no prose link to \${OPENAPI_PUBLIC_URL} — the page's contract ` +
      "destination moved and this guard is reading a page it no longer knows",
  );
  assert.ok(
    !URL_RUN.test(contractLink[0][1].trim()),
    `docs/api/page.tsx labels the contract link with the URL itself ` +
      `(${JSON.stringify(contractLink[0][1])}) — that is the 337px anchor this card is about, ` +
      "reintroduced in the prose shape",
  );

  // The renderer's <a> is the ONE render site for a page's prose link. Its href
  // must come from the parsed token (so the destination is the one the page
  // wrote), it must render the token's label as its text, and its className must
  // be a literal — a computed class cannot be certified by the extractor above.
  const rendererSrc = readFileSync(RENDERER, "utf8");
  assert.ok(
    rendererSrc.includes('if (t.kind === "link")') &&
      rendererSrc.includes("href={t.href}") &&
      rendererSrc.includes("{t.text}"),
    "docsProse.tsx no longer renders a parsed link token as <a href={t.href}>{t.text}</a> — a " +
      "page's prose link would lose its destination or its label",
  );
  assert.match(
    rendererSrc,
    /<a\b[^>]*className="[^"]+"/,
    "docsProse.tsx's prose <a> has no literal className — the extractor above cannot certify a " +
      "computed class, and a link's wrap behaviour would be invisible to this guard",
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

  // And the LABEL extractor is proved the same way: it must see a URL-labelled
  // prose link and must not see one whose label is words.
  const labelProbe = [
    "const SECTIONS = [",
    '  "See [openapi.json](${OPENAPI_PUBLIC_URL}) for the schema.",',
    '  "Mirror: [https://example.com/a-long-absolute-url](https://example.com/a-long-absolute-url)",',
    "]",
  ].join("\n");
  assert.deepEqual(
    urlLabelLinks(labelProbe, "probe.tsx").map((s) => s.text),
    ["https://example.com/a-long-absolute-url"],
    "the prose-link label extractor did not read the probe as designed — it must flag a link " +
      "whose LABEL is a URL and leave a worded label alone",
  );

  // The label scan must be looking at a real set of links, not an empty tree.
  assert.ok(
    LINKS.length >= 10,
    `found only ${LINKS.length} prose link(s) across the docs tree — the label scan would be ` +
      "passing over almost nothing",
  );
});

test("no /docs prose anchor renders an unbreakable URL without a break guard", () => {
  const unguarded = SITES.filter((s) =>
    s.kind === "label" ? true : !s.classKnown || !BREAK_GUARD.test(s.className),
  );
  assert.deepEqual(
    unguarded.map((s) => `${describe(s)}  href/class=${JSON.stringify(s.className)}`),
    [],
    `these /docs anchors render an absolute URL as their own text, one whitespace-free run, with no ` +
      `break guard. Measured on a production build at Chromium 390x844 dsf2 with the browser default ` +
      `font size raised to 24px: /docs/api printed https://jomform.com/openapi.json in exactly this ` +
      `shape (337px wide at a 360px viewport, computed overflow-wrap: normal), which floored the ~296px ` +
      `prose column and panned the page 19px (373 vs 360); at 320px the same run panned 59px. ` +
      `A hand-written <a> needs [overflow-wrap:anywhere] on its className — "anywhere", not ` +
      `"break-word", because it also drops the element's min-content width to one character so the ` +
      `link shrinks INTO the column instead of widening it. A PROSE link cannot carry a class: ` +
      `give it a short label (the words, not the URL) — a URL label is only held inside the ` +
      `column by the break guard on whatever container it lands in, and a table cell (which ` +
      `scrolls instead) or a new container is one edit away from not having one. ` +
      `Do NOT shorten the URL itself: the reader must still be able to read and copy the ` +
      `address. A computed className cannot be certified by this guard and is reported here too; ` +
      `write the class out as a literal string. ` +
      `Findings:\n  ${SITES.map(describe).join("\n  ") || "(none)"}`,
  );
  // A URL-labelled prose link is reported by its KIND, not by a class it cannot
  // have; assert the report names it so the message above is actionable.
  for (const s of SITES) {
    assert.ok(
      s.kind === "label" || s.classKnown,
      `${describe(s)}: an anchor whose class is an expression is reported, never skipped`,
    );
  }
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
  // link fit.
  assert.ok(
    url.length >= MIN_URL_LABEL,
    `OPENAPI_PUBLIC_URL is only ${url.length} characters (${JSON.stringify(url)}) — the URL was ` +
      "shortened. The anchor measured 337px at a 24px root font and that length is what the break " +
      "guard is for; shortening the published contract URL to fit the column would break readers' " +
      "copies of it.",
  );

  // And the docs still LINK it, in full: the page's contract link points at the
  // constant, and that constant is the URL asserted above. (Before the prose
  // renderer this was an anchor printing the URL; the destination is the fact
  // that matters, and it is read from the link the page writes.)
  const apiPage = contractPages()[0]!;
  const apiSrc = readFileSync(apiPage, "utf8");
  const toConstant = [...apiSrc.matchAll(PROSE_LINK)].filter((m) => m[2] === CONTRACT_LINK_TOKEN);
  assert.ok(
    toConstant.length > 0,
    `no docs link targets \${OPENAPI_PUBLIC_URL} any more — re-measure before deleting this guard`,
  );
  // The token resolves to the imported constant, so the destination a reader
  // follows is the full URL this test measured.
  assert.equal(
    openapi.OPENAPI_PUBLIC_URL,
    url,
    "the anchor's destination is not the full contract URL — the guard is protecting a shorter run",
  );
  assert.ok(
    ![...apiSrc.matchAll(PROSE_LINK)]
      .map((m) => m[2])
      .some((h) => /^https?:\/\//.test(h)),
    "docs/api/page.tsx types an absolute URL as a link destination instead of the constant — " +
      "the destination this guard measured would be a second, unguarded definition",
  );
});

test("every contract link is short-labelled and points at the pinned constant (convention pin, so the label cannot swap back)", () => {
  const apiPage = contractPages()[0]!;
  const src = readFileSync(apiPage, "utf8");
  // A link whose destination is the contract constant. Both the label and the
  // destination are read from the source, which is what docsApi.test.ts already
  // pins as the page's only absolute destination.
  const contract = [...src.matchAll(PROSE_LINK)].filter((m) => m[2] === "${OPENAPI_PUBLIC_URL}");
  assert.ok(
    contract.length >= 1,
    "docs/api/page.tsx has no link to the contract — this pin is reading the wrong set of links " +
      "(docsApi.test.ts pins the same destination)",
  );
  // EVERY link to the contract, not just the first: a second one is exactly how
  // the two diverged the first time.
  for (const [, rawLabel, href] of contract) {
    const label = rawLabel.replace(/\s+/g, " ").trim();
    assert.ok(
      !URL_RUN.test(label),
      `a contract link's label is the URL itself (${JSON.stringify(label)}). The intro anchor ` +
        `used to print it and measured 337px at a 360px viewport / 24px root font; in prose the ` +
        `label is the anchor's rendered text, so it is a 32-character unbreakable run whose ` +
        `wrapping depends on the container rather than on anything the link controls. Label the ` +
        `link with words ("openapi.json") and keep the URL as the destination.`,
    );
    assert.ok(
      label.length < MIN_URL_LABEL,
      `a contract link's label is ${label.length} characters (${JSON.stringify(label)}) — that is ` +
        "the unbreakable-run length the break guard exists for; the label must be short words, " +
        "with the full URL kept as the destination",
    );
    // Keep the destination: a short label must not have replaced the link with
    // plain text.
    assert.equal(
      href,
      "${OPENAPI_PUBLIC_URL}",
      "a contract link's destination is no longer the constant — a short label is not a licence " +
        "to drop the machine-readable document",
    );
  }
});
