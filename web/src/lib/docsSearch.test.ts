// The /docs search scorer guard (card t_45fd1bee).
//
// WHY THIS EXISTS
//   The search dialog's whole value is ranking: a query that returns the right page first,
//   and a query whose tokens must ALL match. A scorer that quietly degrades to a substring
//   sweep still "works" on the happy path, so this checks the cases a reader actually types
//   (a product name, a guide topic, a two-word query) and the index's coverage — 30 pages,
//   8 guides, real prose on every entry.
//
// NO NETWORK: the acceptance says typing must not issue a request. The dialog filters the
// generated JSON in memory; the last test pins that the client code has no fetch/XHR/axios.
//
// Run with: npm run test:unit

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { flattenHits, searchDocs, searchTokens, type SearchDoc } from "./docsSearch.ts";

const WEB_DIR = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const DOCS = (
  JSON.parse(readFileSync(join(WEB_DIR, "src", "lib", "docsSearch.gen.json"), "utf8")) as {
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
  const single = flattenHits(searchDocs(DOCS, "donation")).map((hit) => hit.path);
  const pair = flattenHits(searchDocs(DOCS, "donation wedding")).map((hit) => hit.path);
  assert.ok(pair.length > 0, "the two-word query matched nothing");
  // The second token must EXCLUDE some pages the first token matched: otherwise the
  // scorer is OR-ing, and a reader who narrows the query gets a longer list.
  assert.ok(
    single.some((path) => !pair.includes(path)),
    "the second token excluded nothing — the scorer is OR-ing tokens",
  );
  for (const hit of flattenHits(searchDocs(DOCS, "donation wedding"))) {
    const haystack = [hit.title, hit.description, ...hit.headings, hit.text].join(" ").toLowerCase();
    assert.ok(haystack.includes("donation"), `${hit.path} matched the pair without the first token`);
    assert.ok(haystack.includes("wedding"), `${hit.path} matched the pair without the second token`);
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
