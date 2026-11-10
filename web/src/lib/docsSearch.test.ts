// The /docs search scorer guard (card t_45fd1bee).
//
// WHY THIS EXISTS
//   The search dialog's whole value is ranking: a query that returns the right page first,
//   and a query whose tokens must ALL match. A scorer that quietly degrades to a substring
//   sweep still "works" on the happy path, so this checks the cases a reader actually types
//   (a product name, a guide topic, a two-word query) and the index's coverage — 38 pages,
//   8 guides, real prose on every entry.
//
// WHAT THE COVERAGE HALF MISSED (card t_2799edde)
//   "every indexed page carries ≥200 characters" was TRUE while the extractor skipped a
//   converted page's whole SECTIONS body: its description, lead and h3 headings were enough
//   to clear 200, so every page passed while no body paragraph was searchable at all —
//   /docs/builder indexed 470 characters of a page with thousands of words. A floor is not
//   a coverage check. The guard below instead DERIVES a body-only phrase from each page's
//   source and requires it in that page's index entry, so a page shape the extractor stops
//   reading fails here by name.
//
// THE BUDGET IS ASSERTED, NOT DESCRIBED
//   The index is a static import in the docs layout, so every byte is in the docs route's
//   client chunk. `web/scripts/docsNav.mjs` refuses to emit an index over
//   SEARCH_PAYLOAD_BUDGET and this file asserts the same ceiling on the committed bytes,
//   plus the gzip number and the per-page budget — see the measurements in the test below.
//
// NO NETWORK: the acceptance says typing must not issue a request. The dialog filters the
// generated JSON in memory; the last test pins that the client code has no fetch/XHR/axios.
//
// Run with: npm run test:unit

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";

import ts from "typescript";

import { PAGE_TEXT_BUDGET, SEARCH_PAYLOAD_BUDGET, proseText, readPage } from "../../scripts/docsNav.mjs";
import {
  flattenHits,
  searchDocs,
  searchTokens,
  type SearchDoc,
} from "./docsSearch.ts";
import { sectionsOf, usesDocsProse } from "./docsProseReader.ts";

const WEB_DIR = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const INDEX_PATH = join(WEB_DIR, "src", "lib", "docsSearch.gen.json");
const INDEX_BYTES = readFileSync(INDEX_PATH);
const DOCS = (
  JSON.parse(INDEX_BYTES.toString("utf8")) as {
    docs: SearchDoc[];
  }
).docs;

test("the index covers the whole docs tree", () => {
  assert.ok(DOCS.length >= 30, `the index holds ${DOCS.length} pages — the tree has 30`);
  const guides = DOCS.filter((doc) => doc.path.startsWith("/docs/guides/")).map((doc) => doc.path);
  assert.equal(guides.length, 8, `expected the eight screenshot guides, found ${guides.length}`);
  assert.ok(DOCS.some((doc) => doc.path === "/docs/shipping"));
  assert.ok(DOCS.some((doc) => doc.path === "/docs/guides/donations"));
});

test("every indexed page carries prose, a title and a section", () => {
  for (const doc of DOCS) {
    assert.ok(doc.title.trim().length > 0, `${doc.path} has no title`);
    assert.ok(doc.section.trim().length > 0, `${doc.path} has no section`);
    assert.ok(doc.text.length >= 200, `${doc.path} indexed only ${doc.text.length} characters of prose`);
    assert.ok(doc.description.length > 0, `${doc.path} has no description`);
  }
  assert.ok(
    DOCS.reduce((sum, doc) => sum + doc.headings.length, 0) >= 30,
    "too few headings were indexed — the extractor is not reading the page headings",
  );
});

/** Every `page.tsx` under src/app/docs, as `{ route, file, source }`. */
function docsPages(): { route: string; file: string; source: string }[] {
  const root = join(WEB_DIR, "src", "app", "docs");
  const out: { route: string; file: string; source: string }[] = [];
  for (const file of readdirSync(root, { recursive: true }) as string[]) {
    if (!file.endsWith("page.tsx")) continue;
    const rel = relative(root, join(root, file)).split(file.includes("\\") ? "\\" : "/").join("/");
    const dir = rel.slice(0, -"/page.tsx".length);
    out.push({
      route: dir === "" ? "/docs" : `/docs/${dir}`,
      file: join(root, file),
      source: readFileSync(join(root, file), "utf8"),
    });
  }
  return out.sort((a, b) => a.route.localeCompare(b.route));
}

const CONVERTED = docsPages().filter(
  (page) => usesDocsProse(page.source) && sectionsOf(page.source, page.route).length > 0,
);

/**
 * The strings a page writes inside `{ code: … }` blocks. The extractor skips them on purpose
 * (a command to copy is not a phrase a reader types into a search box), so the coverage
 * guard has to know which strings were never owed rather than treat them as gaps.
 */
function codeSamplesOf(source: string): Set<string> {
  const sf = ts.createSourceFile("page.tsx", source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const out = new Set<string>();
  const collect = (node: ts.Node) => {
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) out.add(node.text);
    else ts.forEachChild(node, collect);
  };
  const walk = (node: ts.Node) => {
    if (ts.isPropertyAssignment(node) && node.name.getText() === "code") {
      collect(node.initializer);
      return;
    }
    ts.forEachChild(node, walk);
  };
  walk(sf);
  return out;
}

test("a phrase that only a converted page's BODY prose carries finds that page", () => {
  // The acceptance for card t_2799edde, as a reader would type it: this sentence is a
  // `body:` string on /docs/builder and appears in no title, description or heading, so
  // before the extractor read SECTIONS bodies the search below returned nothing for it.
  const phrase = "Drag a palette item onto the canvas";
  const builder = DOCS.find((doc) => doc.path === "/docs/builder");
  assert.ok(builder, "the index has no /docs/builder entry");
  const metadata = [builder.title, builder.description, ...builder.headings].join(" ").toLowerCase();
  assert.ok(
    !metadata.includes("drag a palette item"),
    "this phrase now sits in the page's title/description/headings — it is no longer a body-only probe",
  );

  const hits = flattenHits(searchDocs(DOCS, phrase));
  assert.ok(hits.length > 0, `'${phrase}' matched nothing — body prose is not in the index`);
  assert.equal(
    hits[0].path,
    "/docs/builder",
    `'${phrase}' ranked ${hits.map((hit) => hit.path).join(", ")} — the page that says it must be first`,
  );
});

test("every converted page indexes its SECTIONS body, not only its lead and headings", () => {
  // Derived from the page SOURCE, not from a hand-listed phrase: a page shape the extractor
  // stops reading (a new block kind, a renamed prop, an array it no longer walks) fails here
  // with the page and the missing paragraph named. This is the arm the ≥200-char floor could
  // not supply — headings and leads cleared it while every body paragraph was skipped.
  assert.ok(
    CONVERTED.length >= 20,
    `only ${CONVERTED.length} pages render through docsProse — the walk under src/app/docs found almost nothing`,
  );
  for (const page of CONVERTED) {
    // The extractor with an unbounded budget: what it READ, before the per-page cut. Asking
    // for the whole page is what makes this a shape guard rather than a truncation test.
    const whole = readPage(page.file, page.route, Number.MAX_SAFE_INTEGER).body;
    const code = codeSamplesOf(page.source);
    for (const section of sectionsOf(page.source, page.route)) {
      const paragraph = section.strings.find((text) => text.length >= 24 && !code.has(text));
      assert.ok(paragraph, `${page.route} section "${section.id}" has no prose paragraph`);
      assert.ok(
        whole.includes(proseText(paragraph)),
        `${page.route} section "${section.id}" is not read: "${paragraph.slice(0, 60)}…" never reaches the index`,
      );
    }

    // And the SHIPPED entry carries the opening body prose, not just the lead and headings.
    const doc = DOCS.find((entry) => entry.path === page.route);
    assert.ok(doc, `${page.route} renders through docsProse but is not in the search index`);
    const opening = sectionsOf(page.source, page.route)[0].strings.find(
      (text) => text.length >= 24 && !code.has(text),
    );
    assert.ok(opening, `${page.route} has no prose in its first section`);
    assert.ok(
      doc.text.includes(proseText(opening)),
      `${page.route} does not index its body prose: "${opening.slice(0, 60)}…" is missing`,
    );
  }
});

test("the index stays inside its measured payload budget", () => {
  // MEASURED on this tree (38 pages, 8 guides) by running the generator at each budget and
  // gzipping the file it emitted. This sweep ran BEFORE guides were capped, which is why
  // 1800 here is not the shipped 77,359 B — the shipped file is the last row of the comment
  // in docsNav.mjs, and the assertion below is what keeps it there.
  //
  //   body prose NOT indexed (before t_2799edde)  60,714 B / 17,786 B gzip
  //   PAGE_TEXT_BUDGET  800                       64,535 B / 19,062 B gzip
  //   PAGE_TEXT_BUDGET 1200                       75,016 B / 22,722 B gzip
  //   PAGE_TEXT_BUDGET 1800                       88,578 B / 27,307 B gzip
  //   PAGE_TEXT_BUDGET 2400                      101,604 B / 31,538 B gzip
  //   no cap at all                              194,354 B / 62,731 B gzip
  //
  // 1,800 characters per page is the choice: most pages are indexed whole at that size, the
  // index lands at 77,359 B / 23,406 B gzip with the guide cap, and the feature costs about
  // 5.4 KiB gzip over headings-only. Uncapped it is 3.5x that, which is why the generator
  // REFUSES to emit past SEARCH_PAYLOAD_BUDGET instead of leaving the ceiling to whoever
  // notices.
  const rawBytes = INDEX_BYTES.byteLength;
  const gzipBytes = gzipSync(INDEX_BYTES).byteLength;
  const GZIP_BUDGET = 26 * 1024; // measured 22.9 KiB + room for five more pages

  assert.ok(
    rawBytes <= SEARCH_PAYLOAD_BUDGET,
    `docsSearch.gen.json is ${(rawBytes / 1024).toFixed(1)} KiB, over the ` +
      `${(SEARCH_PAYLOAD_BUDGET / 1024).toFixed(0)} KiB budget — trim PAGE_TEXT_BUDGET or raise it with the measurement`,
  );
  assert.ok(
    gzipBytes <= GZIP_BUDGET,
    `docsSearch.gen.json gzips to ${(gzipBytes / 1024).toFixed(1)} KiB, over the ` +
      `${(GZIP_BUDGET / 1024).toFixed(0)} KiB budget — the docs layout imports this file on every docs page`,
  );
  for (const doc of DOCS) {
    assert.ok(
      doc.text.length <= PAGE_TEXT_BUDGET,
      `${doc.path} indexed ${doc.text.length} characters, over the ${PAGE_TEXT_BUDGET}-character page budget`,
    );
  }
});

test("a product name ranks its own page first", () => {
  const hits = flattenHits(searchDocs(DOCS, "shipping"));
  assert.ok(hits.length > 0, "searching for a documented feature returned nothing");
  assert.equal(hits[0].path, "/docs/shipping", `'shipping' ranked ${hits[0].path} first`);
});

test("a guide topic reaches its guide", () => {
  const hits = flattenHits(searchDocs(DOCS, "donation"));
  assert.ok(
    hits.some((hit) => hit.path === "/docs/guides/donations"),
    `no hit for /docs/guides/donations: ${hits.map((hit) => hit.path).join(", ")}`,
  );
});

test("every token must match (a second word narrows, it does not widen)", () => {
  // The PAIR is data, and it had to change with card t_b5ea9103: the trimmed guides no longer
  // carry both "donation" and "wedding" on one page (the wedding guide used to name the gallery's
  // other templates, which was 40 words of padding in a nine-step checklist). "donation amount"
  // is the same shape of query a reader would type, and it narrows the same way — measured on
  // this tree: "donation" matches 2 pages, "donation amount" 1.
  const single = flattenHits(searchDocs(DOCS, "donation")).map((hit) => hit.path);
  const pair = flattenHits(searchDocs(DOCS, "donation amount")).map((hit) => hit.path);
  assert.ok(pair.length > 0, "the two-word query matched nothing");
  // The second token must EXCLUDE some pages the first token matched: otherwise the
  // scorer is OR-ing, and a reader who narrows the query gets a longer list.
  assert.ok(
    single.some((path) => !pair.includes(path)),
    "the second token excluded nothing — the scorer is OR-ing tokens",
  );
  for (const hit of flattenHits(searchDocs(DOCS, "donation amount"))) {
    const haystack = [hit.title, hit.description, ...hit.headings, hit.text].join(" ").toLowerCase();
    assert.ok(haystack.includes("donation"), `${hit.path} matched the pair without the first token`);
    assert.ok(haystack.includes("amount"), `${hit.path} matched the pair without the second token`);
  }
});

test("an unknown word returns nothing, not every page", () => {
  assert.deepEqual(searchDocs(DOCS, "xylophonequux"), []);
  assert.deepEqual(searchDocs(DOCS, "   "), []);
  assert.deepEqual(searchDocs(DOCS, "a"), [], "a one-letter word is not a search");
});

test("results are grouped by sidebar section and capped", () => {
  const groups = searchDocs(DOCS, "form");
  assert.ok(groups.length > 0);
  for (const group of groups) {
    for (const hit of group.hits) {
      assert.equal(hit.section, group.section);
      assert.equal(hit.layer, group.layer);
    }
  }
  assert.ok(flattenHits(groups).length <= 12, "the result list is not capped");
});

test("tokenising folds case and punctuation", () => {
  assert.deepEqual(searchTokens("  Shipping, ZONE!  "), ["shipping", "zone"]);
  assert.deepEqual(searchTokens("⌘K"), [], "a keyboard glyph is not a word");
});

// ── the result row's second line (card t_a92da047) ─────────────────────────────────────
// A hit's subtitle used to be `description || snippet`, and the snippet itself anchored on the
// EARLIEST query token — which the page text opens with (title, then description). So a
// multi-word query showed the same description for every page it matched, never the sentence
// that matched, and a reader could reasonably call the result unrelated. These pin both
// halves: the window is chosen by the token that actually explains the match, and the row
// renders that window.

function syntheticDoc(
  path: string,
  description: string,
  body: string,
  overrides: Partial<SearchDoc> = {},
): SearchDoc {
  const title = overrides.title ?? "Field ordering";
  return {
    path,
    title,
    description,
    layer: "More",
    section: "More",
    order: 0,
    headings: [],
    // The page text opens with the title and description, exactly as the generator builds it.
    text: `${title} — JomForm Docs ${description} ${body}`,
    ...overrides,
  };
}

test("a multi-word query shows the sentence that matched, not the description it opens with", () => {
  const description =
    "How the field palette and the canvas work: the field list, the settings panel, and what each block does on a phone.";
  const docs = [
    syntheticDoc(
      "/docs/target",
      description,
      "Adding a field Drag a palette item onto the canvas, or tap it. A tap appends the field to the end of the list.",
    ),
    syntheticDoc("/docs/other", description, "The canvas holds the field list."),
  ];

  const hit = flattenHits(searchDocs(docs, "palette appends the field")).find(
    (entry) => entry.path === "/docs/target",
  );
  assert.ok(hit, "the query matched nothing — the page carries every token");
  assert.notEqual(
    hit.snippet,
    description,
    "the row still shows the description: the snippet anchored on the earliest token again",
  );
  assert.ok(
    hit.snippet.toLowerCase().includes("appends"),
    `the snippet "${hit.snippet}" does not contain the word the query was about`,
  );
});

test("the snippet's window is the rarest query token's, not the first one's", () => {
  // Equal weighting would anchor on "alpha" purely because it comes first, and the row would
  // explain the match with the word six pages share. The rarity weight is what makes the row
  // show the word that makes this page the answer.
  const filler =
    "The canvas holds the fields in the order a buyer sees them and the settings panel edits the selected block. ".repeat(
      3,
    );
  const hit = syntheticDoc(
    "/docs/alpha",
    "How the canvas and the settings panel work on desktop and on a phone.",
    `alpha lands where you drop it. ${filler} A tap appends beta to the end of the list.`,
  );
  const others = [1, 2, 3, 4, 5].map((n) =>
    syntheticDoc(`/docs/filler-${n}`, `Filler page ${n}: alpha only appears here.`, "Nothing else."),
  );

  const found = flattenHits(searchDocs([hit, ...others], "alpha beta")).find(
    (entry) => entry.path === "/docs/alpha",
  );
  assert.ok(found, "the two-token query matched nothing");
  assert.ok(
    found.snippet.toLowerCase().includes("beta"),
    `the snippet "${found.snippet}" anchored on the common token — the rare one is the reason this page matched`,
  );
});

test("a guide whose body does not repeat the summary's word still explains the match", () => {
  // Guide entries index the markdown body as `text` and the summary as `description`, so a
  // token can be in the row's description and nowhere in the prose. There is no window to
  // show; the description is the explanation and must not be replaced by an unrelated slice.
  const guide: SearchDoc = {
    path: "/docs/guides/example",
    title: "Example guide",
    description: "A walkthrough of the screenshot workflow.",
    layer: "Guides",
    section: "Guides",
    order: 0,
    headings: [],
    text: "Open the page and follow the steps.",
  };
  const hit = flattenHits(searchDocs([guide], "screenshot"))[0];
  assert.ok(hit, "the query matched nothing");
  assert.equal(hit.snippet, guide.description);
});

test("a page the description itself matches keeps showing the description", () => {
  const hits = flattenHits(searchDocs(DOCS, "shipping"));
  assert.equal(hits[0].path, "/docs/shipping");
  assert.equal(
    hits[0].snippet,
    hits[0].description,
    "a title/description match must not be replaced by an arbitrary window of prose",
  );
});

test("a body-prose hit shows that prose, measured on the shipped index", () => {
  // The card's own reproduction: the phrase is in /docs/builder's body, the page's description
  // shares "palette", "canvas" and "field" with the query, and the row used to show the
  // description for every one of those queries.
  for (const [query, mustContain] of [
    ["Drag a palette item onto the canvas", "drag a palette item onto the canvas"],
    ["palette appends the field", "appends the field"],
  ] as const) {
    const hit = flattenHits(searchDocs(DOCS, query)).find((entry) => entry.path === "/docs/builder");
    assert.ok(hit, `'${query}' did not reach /docs/builder`);
    assert.ok(
      hit.snippet.toLowerCase().includes(mustContain),
      `'${query}' showed "${hit.snippet}" — the matched sentence is missing`,
    );
    assert.notEqual(hit.snippet, hit.description, `'${query}' still shows the page description`);
  }
});

test("the result row renders the snippet, not the description", () => {
  // The fix lives in the scorer, but the reader only sees it through this expression. It was
  // `description || snippet`, so a page with a description never showed its snippet at all.
  const source = readFileSync(join(WEB_DIR, "src", "app", "docs", "docsSearch.tsx"), "utf8");
  assert.ok(
    source.includes("{hit.snippet || hit.description}"),
    "the result row no longer renders the snippet first — the matched sentence is invisible again",
  );
  assert.ok(
    !source.includes("hit.description || hit.snippet"),
    "the result row still prefers the description — every query would show the same line",
  );
});

test("the client search path never touches the network", () => {
  for (const relative of ["src/app/docs/docsSearch.tsx", "src/lib/docsSearch.ts"]) {
    const source = readFileSync(join(WEB_DIR, relative), "utf8");
    for (const forbidden of ["fetch(", "XMLHttpRequest", "axios", "EventSource", "WebSocket"]) {
      assert.ok(
        !source.includes(forbidden),
        `${relative} uses ${forbidden} — the search index is built at build time and must not be fetched while typing`,
      );
    }
  }
});
