// /docs/api — the guard for the REST API page (t_bac29e18).
//
// WHY THIS EXISTS
//   The page makes three kinds of claim, and each fails in a different, silent
//   way:
//
//     1. The ENDPOINT REFERENCE is generated from the OpenAPI contract, but the
//        rendering could quietly lose entries (a tag the contract declares and
//        the page does not render, a filter that drops rows). The api suite
//        proves the contract matches the route table; this proves the page
//        actually SHOWS what the contract holds.
//
//     2. The two facts a reader must not have to discover by experiment —
//        "no CORS" and "almost nothing is rate limited" — are prose, and prose is
//        the thing that rots. A later edit that softens or deletes those
//        sentences would leave a page that is worse than silent, because it
//        implies a quota and a browser path that do not exist. So they are
//        asserted as text.
//
//     3. The page must be REACHABLE. A developer page that exists but is in no
//        nav and no footer is a page nobody reads. Both are asserted from source,
//        which is what catches a nav edit that drops it.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It does not execute the React tree, so it cannot see the rendered layout,
//     the table wrapping or the anchor scrolling — only the declared data and the
//     prose/JSX the page and its renderer declare. The rendered result is checked
//     in a browser.
//   * It does not judge the prose quality, only that the two load-bearing
//     statements are still there and that the numbers the page states about
//     itself agree with the contract (the throttled count and the total).
//   * The OpenAPI document's own validity and route parity are proven in Go
//     (api/internal/httpapi/openapi_parity_test.go), not here.
//
// SINCE THE PROSE RENDERER
//   The page is a `const SECTIONS: Section[]` literal rendered by
//   src/app/docs/docsProse. Content is read through docsProseReader.pageProse();
//   the markup those strings become (<pre>, table scroll region, <a>, the
//   section anchor) exists once, in the renderer, so this file asserts the page
//   ROUTES through it and the renderer's own guards pin the markup.
//
// Run with:  npm run test:unit
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, resolve as resolvePath } from "node:path";
import { fileURLToPath } from "node:url";

import {
  API_BASE,
  OPENAPI_PUBLIC_URL,
  accessLevels,
  apiExamples,
  endpointCount,
  endpointsByTag,
  rateLimitedEndpoints,
  specEndpoints,
  writeGatedReads,
  scopeOnlyWriteRoutes,
} from "./openapi.ts";
import { OPENAPI_SPEC } from "./openapiSpec.gen.ts";
import { API_EXAMPLES } from "./apiExamples.gen.ts";
import { SITE_URL } from "./siteName.ts";
// The page renders through src/app/docs/docsProse now, so its prose is the
// `const SECTIONS` literal rather than JSX. These three read that shape; the
// markup it renders (code samples, table scroll regions, links) is pinned in
// docsProse.tsx by the sibling guards.
import { pageProse, sectionsOf, usesDocsProse } from "./docsProseReader.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const path = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));
/** web/src — the root tsconfig's `@/*` alias points at (tsconfig `paths`). */
const WEB_SRC = resolvePath(dirname(path("./openapi.ts")), "..");

/**
 * Resolve a module specifier the way this project's bundler does, so the binding
 * check below compares FILES rather than spellings: tsconfig maps `@/*` to
 * `./src/*`, and the page may reach lib/openapi.ts by the alias or by a relative
 * path. A bare package specifier is returned as written — it cannot be that file.
 */
function resolveModuleSpecifier(spec: string, fromFile: string): string {
  if (spec.startsWith("@/")) return resolvePath(WEB_SRC, spec.slice(2));
  if (spec.startsWith(".")) return resolvePath(dirname(fromFile), spec);
  return spec;
}

/**
 * The page's PROSE, whitespace-collapsed and lowercased.
 *
 * The page renders through src/app/docs/docsProse, so what it SAYS now lives in
 * the `const SECTIONS` literal, not in JSX. docsProseReader's pageProse() gives
 * the reader-visible text of that shape (and the raw source of a page that is
 * still hand-written), which is the question every assertion below is asking.
 *
 * Two consequences of reading prose this way, both deliberate:
 *   * the old comment-stripping pass is gone with the JSX that motivated it —
 *     for a converted page the reader never returns a comment or an import path,
 *     so a phrase quoted in the header comment cannot satisfy an assertion;
 *   * nothing may be dropped by first character. A prose line here is a string,
 *     and `**400**: …` (bold) starts with `*` — the old heuristic would have
 *     eaten it.
 *
 * The interpolation sites (`${total}`, `${e.path}`) are NOT prose: the reader
 * collects string literals, so a template with a substitution is read as a
 * render site and is asserted against the page source below.
 */
function normalized(src: string): string {
  return src.replace(/\s+/g, " ").toLowerCase();
}

/**
 * Every `href` destination the page declares, exactly as written in the JSX.
 *
 * The value is read with a brace-balanced scan rather than a regex like
 * `href=\{([^}]*)\}`: a template literal such as ``href={`#${s.id}`}`` carries
 * its own `}` inside `${…}`, so the naive form ends the match early and reports
 * a value that is not the one on the page. Callers judge the DESTINATION with
 * this — the href a reader follows — which is a different question from whether
 * some identifier appears somewhere in the file. (t_d87d9298: the two were
 * conflated, and a page whose href pointed at a dead host passed 16/16.)
 */
function anchorHrefs(src: string): string[] {
  const out: string[] = [];
  for (const m of src.matchAll(/\bhref=/g)) {
    const i = (m.index ?? 0) + m[0].length;
    const opener = src[i];
    if (opener === '"' || opener === "'") {
      const end = src.indexOf(opener, i + 1);
      out.push(src.slice(i + 1, end));
      continue;
    }
    if (opener === "{") {
      let depth = 0;
      let j = i;
      for (; j < src.length; j++) {
        if (src[j] === "{") depth++;
        else if (src[j] === "}") {
          depth--;
          if (depth === 0) break;
        }
      }
      out.push(src.slice(i + 1, j).trim());
      continue;
    }
    // Not a shape this helper can read as an attribute value (a spread, a bare
    // identifier). Keep it verbatim so a caller sees it rather than silently
    // losing an href it should judge.
    out.push(src.slice(i, i + 40));
  }
  return out;
}

/**
 * The module specifiers in the page's own import statements that bind `name`.
 *
 * `anchorHrefs` reads the NAME written in an attribute; this reads what that
 * name is BOUND TO, which is a different question and the one that decides where
 * a reader is sent. A page that keeps `href={OPENAPI_PUBLIC_URL}` on both anchors
 * but takes the identifier from a SECOND module exporting its own copy of the
 * name renders that copy's URL, while every check named after the identifier —
 * and every check on the constant, which this file imports from lib/openapi.ts —
 * stays green. (t_2285b2ef, shape 1.)
 */
function importSourcesFor(src: string, name: string): string[] {
  const out: string[] = [];
  // One statement at a time: a clause spanning a `;` would be two statements, and
  // would attribute the name to the wrong specifier.
  for (const m of src.matchAll(/\bimport\s+(?:type\s+)?([^;]*?)\s+from\s+["']([^"']+)["']/g)) {
    const named = m[1].match(/\{([\s\S]*)\}/);
    if (!named) continue; // a default or namespace import binds no bare name
    for (const entry of named[1].split(",")) {
      // `A as B` binds B; `type A` binds A.
      const [imported, local = imported] = entry.trim().replace(/^type\s+/, "").split(/\s+as\s+/);
      if (local.trim() === name) out.push(m[2]);
    }
  }
  return out;
}

/**
 * The attribute region of every `<a …>` opening tag on the page.
 *
 * Read by scanning to the tag's closing `>` with brace and quote depth tracked,
 * so a multi-line tag is read whole and an attribute whose value contains `>`
 * does not end the tag early. Callers use it for what `anchorHrefs` structurally
 * cannot see: a JSX spread. A spread is applied AFTER the literal attributes, so
 * `{...{ href: "https://…" }}` replaces the destination of the `href` above it —
 * and `anchorHrefs` matches `href=`, so the spread's `href:` is never read.
 * (t_2285b2ef, shape 2.)
 */
function anchorAttributes(src: string): string[] {
  const out: string[] = [];
  for (const m of src.matchAll(/<a(?=[\s>])/g)) {
    const start = (m.index ?? 0) + m[0].length;
    let i = start;
    let depth = 0;
    let quote = "";
    for (; i < src.length; i++) {
      const c = src[i];
      if (quote) {
        if (c === quote) quote = "";
        continue;
      }
      if (c === '"' || c === "'") {
        quote = c;
        continue;
      }
      if (c === "{") depth++;
      else if (c === "}") depth--;
      else if (depth === 0 && c === ">") break;
    }
    out.push(src.slice(start, i));
  }
  return out;
}

// Card t_0356ed4c: the REST reference is FIVE pages now — a hub plus four task
// pages (authentication, endpoints, webhooks, errors). Every assertion below is a
// question about the REFERENCE, not about one file, so the guard reads all five
// and asks of the union. Split or not, the claim still has to be true somewhere in
// the set: a page that moves cannot drop a statement through the gap between two
// files, and a route that moves cannot escape the reachability checks.
const API_PAGES = [
  "../app/docs/api/page.tsx",
  "../app/docs/api/authentication/page.tsx",
  "../app/docs/api/endpoints/page.tsx",
  "../app/docs/api/webhooks/page.tsx",
  "../app/docs/api/errors/page.tsx",
];
const PAGE = API_PAGES[0];
const pageRaw = API_PAGES.map(read).join("\n");
const pageSrc = pageRaw;
/** The reader-visible prose of the whole reference, and every section anchor in it. */
const pageProseText = API_PAGES.map((rel) => pageProse(read(rel), rel)).join("\n\n");
const pageText = normalized(pageProseText);
const pageSections = API_PAGES.flatMap((rel) => sectionsOf(read(rel), rel));
const sectionIds = pageSections.map((s) => s.id);
const LAYOUT = "../app/docs/layout.tsx";
const LANDING = "../app/landing/page.tsx";
const SITEMAP = "../app/sitemap.ts";
// t_bac29e18: the /docs index and the i18n dictionary — the two other places the
// REST API has to appear for a reader to find it.
const INDEX = "../app/docs/page.tsx";
const DICT = "../components/i18nDict.ts";

// ── the guard cannot pass vacuously ─────────────────────────────────────────
test("the guard is looking at the real page and the real contract", () => {
  assert.ok(
    pageSrc.includes("export default function ApiDocsPage"),
    `${PAGE} is not the API docs page — the guard's anchor moved`,
  );
  // The page renders through the shared prose renderer. Everything below either
  // reads its SECTIONS literal (through docsProseReader) or the one renderer
  // module; a page that stopped using the renderer would make the prose reader
  // fall back to raw source and this guard silently weaker.
  assert.ok(
    usesDocsProse(pageRaw),
    `${PAGE} no longer renders through src/app/docs/docsProse — this guard reads its SECTIONS ` +
      "prose, so re-point the reader before trusting any assertion below",
  );
  // The anchors are a contract with /docs and the dashboard's inline "?" links
  // (the page's own header comment says so). Read them from SECTIONS: the page
  // has no `<section id="…">` of its own any more, the renderer emits it.
  const ANCHOR_CONTRACT = [
    "quickstart",
    "auth",
    "scopes",
    "errors",
    "limits",
    "cors",
    "endpoints",
    "versioning",
    "testing",
  ];
  assert.ok(
    pageSections.length >= 8,
    `parsed only ${pageSections.length} section(s) out of ${PAGE} — the SECTIONS literal is not ` +
      "what this guard is reading",
  );
  for (const id of ANCHOR_CONTRACT) {
    assert.ok(
      sectionIds.includes(id),
      `the "${id}" anchor is gone from ${PAGE}'s SECTIONS — it is linked from /docs and from the ` +
        "dashboard, so a reader following one lands nowhere",
    );
  }
  assert.equal(
    new Set(sectionIds).size,
    sectionIds.length,
    `two sections in ${PAGE} share an id (${JSON.stringify(sectionIds)}) — one of them is ` +
      "unreachable by its anchor",
  );
  assert.ok(
    endpointCount() > 50,
    `the contract exposes only ${endpointCount()} operations — that is not this API, so the ` +
      "checks below would be asserting almost nothing",
  );
  assert.ok(
    Object.keys(OPENAPI_SPEC.paths).length > 40,
    "openapiSpec.gen.ts has too few paths to be the generated contract",
  );
  const src = read("./openapiSpec.gen.ts");
  assert.ok(
    src.startsWith("// GENERATED FILE — DO NOT EDIT."),
    "openapiSpec.gen.ts lost its generated-file header — it looks hand-edited",
  );
});

// ── the page renders the contract, not a hand-written list ──────────────────
test("the endpoint reference is generated from the contract", () => {
  // The page must render its reference through the shaping layer. A hand-typed
  // endpoint table is the defect this card exists to prevent, so the page must
  // not declare endpoint rows itself.
  assert.ok(
    pageSrc.includes("endpointsByTag"),
    `${PAGE} does not render its endpoint reference through endpointsByTag() — it is not ` +
      "generated from the contract",
  );
  // The rows are built from the grouped endpoints (a `flatMap` that emits one
  // heading + one table per tag). The old spelling was a JSX `{group.endpoints.map(`
  // inside the table body; the prose renderer moved that expression out of JSX,
  // so the brace is gone and the iteration is what is asserted.
  assert.match(
    pageSrc,
    /group\.endpoints\.map\(/,
    `${PAGE} does not iterate the grouped endpoints — the reference would render nothing`,
  );
  assert.ok(
    pageSrc.includes("head: [\"Method\", \"Path\", \"Access\", \"What it does\"]"),
    `${PAGE} no longer builds the four-column endpoint table — a reader loses the access gate ` +
      "or the summary",
  );

  // EVERY /v1/rest/ path the page's PROSE mentions must be a real operation.
  // Prose is allowed to name the base path or a specific endpoint (a quickstart
  // step has to say which route to call), but it may not invent one: a path in
  // that prose that is not in the contract is documentation for an endpoint
  // that 404s. Read the prose, not the JSX wrappers around it.
  const contractPaths = new Set(specEndpoints().map((e) => e.path));
  const mentioned = [...pageProseText.matchAll(/\/v1\/rest\/[A-Za-z0-9/{}_.-]*/g)].map((m) => m[0]);
  assert.ok(mentioned.length > 0, "the page never names a /v1/rest path at all — it cannot be a REST API page");
  const invented = [...new Set(mentioned)].filter((p) => {
    // A trailing fragment (`/v1/rest/*`, a paragraph-final punctuation) is not a
    // claim about an endpoint; anything else must resolve to a declared path.
    const trimmed = p.replace(/[.,;:)]+$/, "");
    if (trimmed === "/v1/rest/" || trimmed === "/v1/rest/*") return false;
    return !contractPaths.has(trimmed) && ![...contractPaths].some((c) => c === trimmed);
  });
  assert.deepEqual(
    invented,
    [],
    `the page names /v1/rest paths the contract does not declare (a reader would 404):\n  ` +
      invented.join("\n  "),
  );
});

test("every endpoint in the contract reaches the page", () => {
  const groups = endpointsByTag();
  const rendered = groups.flatMap((g) => g.endpoints);
  assert.equal(
    rendered.length,
    endpointCount(),
    "endpointsByTag() dropped operations — a developer would not see every endpoint",
  );
  // And the grouping must not merge two different operations under one key.
  const keys = rendered.map((e) => `${e.method} ${e.path}`);
  assert.equal(new Set(keys).size, keys.length, "two endpoints rendered as the same row");

  // Every contract path appears exactly once in the shaped output.
  const contractPaths = Object.keys(OPENAPI_SPEC.paths);
  const seen = new Set(rendered.map((e) => e.path));
  const missing = contractPaths.filter((p) => !seen.has(p));
  assert.deepEqual(missing, [], `these contract paths never reach the page:\n  ${missing.join("\n  ")}`);
});

test("the page states the true size of the API, not a rounded claim", () => {
  // The count on the page is interpolated from the contract (`{total}`), so a
  // number typed into the prose would be a second source of truth.
  assert.ok(
    pageSrc.includes("{total}"),
    `${PAGE} does not interpolate the operation count — a typed count drifts`,
  );
  assert.ok(
    !/\b\d{2,3} (operations|endpoints)\b/.test(pageSrc),
    `${PAGE} hardcodes an operation count in prose; it must come from the contract`,
  );
});

// ── the two load-bearing statements ─────────────────────────────────────────
test("the page states that the API sends no CORS headers", () => {
  assert.ok(
    sectionIds.includes("cors"),
    `${PAGE} lost the CORS section — the API's browser behaviour is the ` +
      "single most expensive thing to leave undocumented",
  );

  assert.ok(
    pageText.includes("no cors") || pageText.includes("no cors headers at all"),
    "the page no longer says there is no CORS",
  );
  assert.ok(
    pageProseText.includes("Access-Control-Allow-Origin"),
    "the page no longer names the header it does not send",
  );
  assert.ok(
    pageText.includes("server-to-server"),
    "the page no longer frames the API as server-to-server",
  );
  // And it must not promise the opposite anywhere in its prose.
  assert.ok(
    !/adds? (the )?(cors|Access-Control-Allow)/i.test(pageProseText),
    "the page suggests CORS headers are sent, which contradicts the API",
  );
});

test("the in-page nav covers every section, and every link lands on one", () => {
  // The hand-written nav listed 7 of the 9 sections, so two headings were
  // reachable only by scrolling. Both directions are checked: a section with no
  // nav entry (unfindable) and a nav entry with no section (a dead anchor).
  //
  // The in-page nav is no longer a per-page list at all: /docs/layout renders
  // <DocsToc> around every page, and DocsToc builds the trail from the h2/h3
  // elements that ACTUALLY RENDERED, reusing the `section[id]` they sit in. So the
  // 7-of-9 defect cannot recur by editing a nav list — the nav has no list.
  //
  // What still has to be true, and is asserted here: the page passes its sections
  // to the renderer, every section id is unique, and the renderer emits one
  // <section id={s.id}> with an <h2> for each. DocsToc reads exactly that shape.
  const ids = pageSections.map((s) => s.id);
  assert.equal(
    new Set(ids).size,
    ids.length,
    `${PAGE} declares a duplicate section id — DocsToc would render two identical in-page links`,
  );
  assert.ok(ids.length >= 9, `${PAGE} declares only ${ids.length} section(s)`);
  const proseRenderer = readFileSync(new URL("../app/docs/docsProse.tsx", import.meta.url), "utf8");
  assert.ok(
    /<section[^>]*id=\{s\.id\}/.test(proseRenderer) && /<h2/.test(proseRenderer),
    "the renderer no longer emits <section id={s.id}> with an <h2> — DocsToc builds the " +
      "in-page nav from those elements, so the page would have no nav at all",
  );
  assert.ok(
    pageSrc.includes("sections={SECTIONS}"),
    `${PAGE} no longer renders the same SECTIONS list it builds the nav from — the two lists ` +
      "can drift again",
  );
  assert.ok(
    !/\{ id: "[a-z-]+", title: /.test(pageSrc),
    `${PAGE} declares nav entries by hand again (an inline { id, title } literal) — they must ` +
      "come from SECTIONS",
  );

  // The nav must land on an anchor that exists: the renderer emits `id={s.id}` on
  // the section and `href={`#${s.id}`}` on the nav link, from the same object.
  const renderer = read("../app/docs/docsProse.tsx");
  assert.ok(
    renderer.includes("<section key={s.id} id={s.id}"),
    "docsProse.tsx no longer puts the section's own id on the <section> — every nav link on " +
      "every /docs page would be a dead click",
  );
  // The nav LINK is built by DocsToc from the rendered heading (it reads the
  // `section[id]` a heading sits in), so the renderer owes the heading + the
  // anchor, not a link of its own. Assert that binding instead of a href the
  // renderer no longer writes.
  assert.ok(
    /<section[^>]*id=\{s\.id\}[^>]*>[\s\S]{0,400}?<h2/.test(renderer),
    "docsProse.tsx no longer emits an <h2> inside <section id={s.id}> — DocsToc builds each " +
      "in-page link from that heading, so a section would drop out of the nav",
  );
  // And every section this page declares is unique, so a nav entry identifies
  // exactly one target.
  assert.deepEqual(
    sectionIds.filter((id, i) => sectionIds.indexOf(id) !== i),
    [],
    `two sections in ${PAGE} declare the same anchor:`,
  );
});

test("the page states that most routes are NOT rate limited", () => {
  assert.ok(sectionIds.includes("limits"), `${PAGE} lost the rate-limit section`);

  assert.ok(
    pageText.includes("most of this api has no rate limit"),
    "the page no longer states that most routes are unmetered — the honest fact about this API",
  );
  assert.ok(
    pageText.includes("do not design a client around a uniform quota"),
    "the page no longer warns against assuming a uniform quota",
  );
  // The page must list ONLY the throttled routes, and the list must come from the
  // contract rather than a hand-typed enumeration. The list BLOCK is the render
  // site: `{ list: THROTTLED_ROUTES }` is built from `limited`, which is
  // rateLimitedEndpoints() — so the rows and the sentence agree by construction.
  assert.ok(
    pageSrc.includes("rateLimitedEndpoints"),
    `${PAGE} does not derive its throttled-route list from the contract`,
  );
  assert.ok(
    pageSrc.includes("const THROTTLED_ROUTES: string[] = limited.map(") &&
      pageSrc.includes("{ list: THROTTLED_ROUTES }"),
    `${PAGE} builds a throttled list that is not rendered from ` +
      "`limited` — the prose would describe a set the list below does not show",
  );
  const limited = rateLimitedEndpoints();
  assert.ok(limited.length > 0, "the contract now declares no rate limits at all — re-check the page's claim");
  assert.ok(
    limited.length < endpointCount(),
    "every endpoint is throttled? that contradicts the page; re-check",
  );
  // A rate-limit claim on the page must match the contract's own field.
  for (const e of limited) {
    assert.ok(e.rateLimit !== "", `${e.method} ${e.path} is in the limited list with no policy`);
  }
});

test("the page's throttle list is exactly the contract's throttled set", () => {
  const fromPage = rateLimitedEndpoints().map((e) => `${e.method} ${e.path}`);
  const fromSpec = specEndpoints()
    .filter((e) => e.rateLimit !== "")
    .map((e) => `${e.method} ${e.path}`);
  assert.deepEqual(fromPage, fromSpec);
  // And the access vocabulary the page explains is the vocabulary in use. The
  // legend is now built (`ACCESS_LEGEND = accessLevels().map(…)`), so "the page
  // explains it" means the level has a label entry the map can read — a bare
  // `read:` in a comment no longer satisfies this.
  const levels = accessLevels();
  assert.ok(
    pageSrc.includes("const ACCESS_LEGEND: string[] = accessLevels().map("),
    `${PAGE} no longer builds its access legend from accessLevels() — a level the contract ` +
      "uses could reach a row with no definition",
  );
  for (const level of levels) {
    assert.match(
      pageSrc,
      new RegExp(`\\b${level}:\\s*"[^"]+"`),
      `the contract uses access "${level}" but the page's ACCESS_LABELS has no label for it — ` +
        "the legend row would render empty",
    );
  }
});

// ── the Scopes section's write-gated-read claim ─────────────────────────────
test("the page's write-gated-read list comes from the contract, not from prose", () => {
  // The Scopes section used to offer "(the form email-settings and
  // notification-rule routes)" as the COMPLETE set of reads a readonly key is
  // refused. GET /v1/rest/settings/webhook-key was gated as a write too, so the
  // list was wrong the moment it was typed — and nothing went red. It is now
  // rendered from the contract through writeGatedReads(); this asserts the page
  // really uses that, that the set is non-empty, and that its size is stated
  // rather than typed (the same rule the operation count follows).
  const gated = writeGatedReads();
  assert.ok(
    gated.length > 0,
    "the contract declares no write-gated read at all — re-check the page's claim, which " +
      "now points at an empty list",
  );
  assert.ok(
    gated.every((e) => e.method === "GET" && e.access === "write"),
    "writeGatedReads() returned something that is not a GET whose access is write",
  );
  assert.ok(
    pageSrc.includes("writeGatedReads"),
    `${PAGE} does not derive its write-gated-read list from the contract — the hand-typed ` +
      "list is back, and it will drift exactly as it did before",
  );
  assert.ok(
    pageSrc.includes("{writeGatedReads().length}"),
    `${PAGE} does not state the size of the list from the contract; a typed number drifts`,
  );
  assert.ok(
    pageSrc.includes("const WRITE_GATED_READS: string[] = writeGatedReads().map(") &&
      pageSrc.includes("{ list: WRITE_GATED_READS }"),
    `${PAGE} computes the write-gated set but no longer renders it as the list under the ` +
      "sentence — the reader is told a number with no routes behind it",
  );
  assert.ok(
    !/\b(one|two|three|four|five)\b[^.]{0,40}(email-settings|notification)/i.test(pageSrc),
    `${PAGE} still spells the write-gated-read set out in words (e.g. "the three routes..."), ` +
      "which is a count the contract cannot keep true",
  );
  // The named routes must actually be in the set the page renders — the claim
  // is about THESE routes, so a set that lost one is wrong even if non-empty.
  for (const path of [
    "/v1/rest/forms/{id}/email-settings",
    "/v1/rest/forms/{id}/notifications",
    "/v1/rest/settings/webhook-key",
  ]) {
    assert.ok(
      gated.some((e) => e.path === path),
      `${path} is not in writeGatedReads() — a readonly key is refused there, so the page's ` +
        "list is incomplete again",
    );
  }
});

// ── the examples are the tested ones ────────────────────────────────────────
test("the examples rendered on the page are the ones the api suite executes", () => {
  assert.ok(apiExamples.length >= 6, `only ${apiExamples.length} examples — the walk-through shrank`);
  // The examples are the generated data, walked into prose blocks. The old
  // spelling was `apiExamples.map(` in JSX; the page now `flatMap`s each example
  // into a heading + a `{ code: e.curl }` block + its why/note, so either
  // iteration is accepted — but it must be one of them.
  assert.match(
    pageSrc,
    /apiExamples\.(?:flatMap|map)\(/,
    `${PAGE} does not render the examples from the generated data`,
  );
  assert.ok(
    pageSrc.includes("const EXAMPLE_BLOCKS: Section[\"body\"] = apiExamples.") &&
      pageSrc.includes("code: `${e.curl}`"),
    `${PAGE} no longer renders each example's curl command as a code sample — the walk-through ` +
      "would show prose about a command it never prints",
  );
  // Each rendered example carries the status it is expected to return, and the
  // body fragment it must contain — the same fields the Go test asserts on.
  for (const e of API_EXAMPLES.examples) {
    assert.ok(e.curl.includes("curl"), `example ${e.id} renders no curl command`);
    assert.ok(e.expectStatus > 0, `example ${e.id} states no expected status`);
    assert.ok(e.expectContains.length > 0, `example ${e.id} states no body expectation`);
  }
  assert.ok(
    pageSrc.includes("e.expectStatus") && pageSrc.includes("e.expectContains"),
    `${PAGE} no longer shows what each example should return, so a reader cannot tell success ` +
      "from a silent failure",
  );
  // No example may embed a real-looking key.
  for (const e of API_EXAMPLES.examples) {
    assert.ok(!e.curl.includes("jfk_"), `example ${e.id} renders what looks like a real API key`);
  }
});

// ── reachability ────────────────────────────────────────────────────────────
test("the page is reachable from the docs nav and the public footer", () => {
  const nav = read(LAYOUT);
  assert.ok(
    nav.includes('"/docs/api"'),
    "the docs sidebar does not link /docs/api — a developer page nobody can navigate to",
  );
  const footer = read(LANDING);
  assert.ok(
    footer.includes('href="/docs/api"'),
    "the public footer does not link /docs/api; the card requires it to be reachable from the " +
      "docs nav AND the public footer",
  );
  // It must be in the sitemap too, like every other docs page.
  const sitemap = read(SITEMAP);
  assert.ok(sitemap.includes('"/docs/api"'), "/docs/api is missing from sitemap.ts");

  // The /docs index (its "For developers" category) and the quick links are the
  // two other places a reader looks. A MISSING entry is not caught anywhere else:
  // docsIndex.test.ts only checks that links which exist resolve to a real page.
  //
  // On the /docs restructure (PR 675) an index entry carries `href: "/docs/api"`;
  // on the tree this rebase lands on the entries have no href at all (the index
  // body points at the guide in prose). Assert the CLAIM — the index names the
  // REST API and something a reader can follow reaches the page — rather than
  // the shape, so the guard survives either structure.
  const index = read(INDEX);
  const indexEntry =
    index.includes('href: "/docs/api"') ||
    (/title: "REST API"/.test(index) && /REST API reference/.test(index));
  assert.ok(
    indexEntry,
    "the /docs index has no REST API entry — the page exists but the index never points at it",
  );
  const quickLink =
    index.includes('labelKey: "docs.linkAPI"') || /href="\/docs\/api"[^>]*>[\s\S]{0,120}?REST API/.test(index);
  assert.ok(
    quickLink,
    "the /docs quick links do not include the REST API",
  );
  // The dictionary entry only has to exist when the index renders that key —
  // the keyed row is what would render blank without it. The typed dictionary
  // makes the reference itself a build error, and this line makes it explicit.
  const usesKeyedLabel = index.includes('labelKey: "docs.linkAPI"');
  const dict = read(DICT);
  if (usesKeyedLabel) {
    assert.ok(
      dict.includes('"docs.linkAPI"'),
      "docs.linkAPI is used by the index but is not defined in the i18n dictionary — the label " +
        "would render blank (the dictionary is typed, so this also fails the build)",
    );
  }
});

test("the page links the machine-readable contract it is generated from", () => {
  // The page must point at the contract through the exported constant, not with
  // a URL typed into the JSX: the destination is asserted below, and one
  // definition is what keeps the page and the guard from disagreeing.
  assert.ok(
    pageSrc.includes("OPENAPI_PUBLIC_URL"),
    `${PAGE} no longer points a reader at the published OpenAPI document`,
  );
  assert.ok(
    pageSrc.includes("API_BASE"),
    `${PAGE} no longer names the API host a caller should send requests to`,
  );
  // The BINDING, not just the name: the page must take the identifier from the
  // module this test itself reads the constant from. Asserting below that the
  // page imports lib/openapi.ts is not the same thing — that import can carry
  // every OTHER name while OPENAPI_PUBLIC_URL comes from a second module that
  // exports its own copy, and then both anchors render that copy's URL while
  // every href check and every constant check stays green. (t_2285b2ef, shape 1.)
  // t_0356ed4c: the binding is resolved per FILE, because the reference is five
  // pages at three directory depths now. Resolving every specifier against the
  // hub's path would have judged "../../../../lib/openapi" by the wrong base.
  const bound = API_PAGES.flatMap((rel) =>
    importSourcesFor(read(rel), "OPENAPI_PUBLIC_URL").map((spec) => resolveModuleSpecifier(spec, path(rel))),
  );
  assert.deepEqual(
    bound,
    [path("./openapi.ts")],
    `${PAGE} must take OPENAPI_PUBLIC_URL from exactly one module, and that module must be ` +
      `lib/openapi.ts — the module this guard reads the constant from. Bound to ` +
      `${JSON.stringify(bound)}. A second module exporting the same name, or a copy declared ` +
      "in the page itself, hands the anchors a destination nothing here has checked.",
  );
  // The page must NOT import the raw spec: the shaping lives in lib/openapi.ts,
  // and a second import path is a second place to keep correct. (Checked against
  // code lines only — the header comment explains this rule and names the file.)
  const code = pageRaw
    .split("\n")
    .filter((l) => !l.trim().startsWith("//") && !l.trim().startsWith("*") && !l.trim().startsWith("/*"))
    .join("\n");
  assert.ok(
    !code.includes("openapiSpec.gen.ts") && !code.includes("apiExamples.gen.ts"),
    "the page imports a generated module directly; it must go through lib/openapi.ts so the " +
      "shaping stays in one place",
  );
  assert.ok(
    code.includes("lib/openapi.ts"),
    "the page does not import lib/openapi.ts — then where does its endpoint data come from?",
  );
});

// ── the contract link must be READABLE by the page's own audience ───────────
//
// The defect this section exists for (t_4b37ffa7): the page linked the contract
// at a github.com blob URL. The repository is PRIVATE, so an anonymous fetch of
// that URL answers 404 — the page promised a machine-readable contract to an
// audience that is explicitly partly third-party developers, and the half
// without repository access could not open it. A guard is needed because the
// link is one string in one file and nothing else in the repo would notice it
// pointing somewhere unreachable.
//
// So this checks the DESTINATION, in both places it is written: the href of each
// anchor on the page, and the URL of the constant those anchors are supposed to
// use. The first of those was missing (t_d87d9298): the guard asserted that the
// page NAMES the constant — which the anchor's link text also does, because the
// label prints the URL — so a typed href pointing at any other host passed
// 16/16 while the page linked somewhere its reader could not fetch.
test("the contract link is a URL the page's audience can actually fetch", () => {
  // 1. It must not be a repository URL. The repository is private, so any
  //    github.com link to it resolves to a login prompt or a 404 for the
  //    anonymous developer this page is written for.
  assert.ok(
    !pageRaw.includes("github.com"),
    `${PAGE} links github.com — the JomForm repository is private, so that link is ` +
      "unreadable for the page's third-party audience. Serve the contract from a public URL.",
  );
  assert.ok(
    !OPENAPI_PUBLIC_URL.includes("github.com"),
    `OPENAPI_PUBLIC_URL is a github.com URL (${OPENAPI_PUBLIC_URL}) — the repository is ` +
      "private and an anonymous reader gets a 404",
  );
  // 2. The page's contract links must POINT AT that constant. Asserting only
  //    that the identifier appears in the file was satisfied by the identifier
  //    left in an anchor's link TEXT, so a typed href pointing anywhere at all
  //    still passed. The DESTINATION is the half a reader follows.
  //
  //    The page renders through the shared prose renderer now, so its contract
  //    link is a MARKDOWN LINK in a SECTIONS string —
  //    `[openapi.json](${OPENAPI_PUBLIC_URL})` — and the renderer turns the
  //    parsed token into `<a href={t.href}>`. Both halves are pinned: the link
  //    is written against the constant, and the renderer takes the anchor's
  //    destination from that token rather than from anything of its own.
  const contractLinks = [...pageRaw.matchAll(/\]\(\$\{OPENAPI_PUBLIC_URL\}\)/g)].length;
  assert.ok(
    contractLinks >= 1,
    `${PAGE} has no prose link to the contract through OPENAPI_PUBLIC_URL — a reader must be ` +
      "able to open the machine-readable document, and the destination has to be the constant " +
      "this test reads (a typed URL would be a second definition nothing here judges)",
  );
  const renderer = read("../app/docs/docsProse.tsx");
  assert.ok(
    renderer.includes('if (t.kind === "link")') && renderer.includes("href={t.href}"),
    "docsProse.tsx no longer renders a parsed link token as <a href={t.href}> — the page's " +
      "contract link would either not render or take a destination the page never wrote",
  );
  // And no absolute URL may be TYPED into a link destination anywhere on the
  // page, prose or JSX: the page's one absolute destination is the contract, and
  // it must come from OPENAPI_PUBLIC_URL so the host, path and byte checks below
  // judge one definition.
  const typedProseURLs = [...pageRaw.matchAll(/\]\(([^)\s]+)\)/g)]
    .map((m) => m[1])
    .filter((h) => /^https?:\/\//i.test(h));
  assert.deepEqual(
    typedProseURLs,
    [],
    `${PAGE} types an absolute URL into a prose link (${JSON.stringify(typedProseURLs)}) — the ` +
      "page's one absolute destination is the contract, and it must come from " +
      "OPENAPI_PUBLIC_URL",
  );
  // If the page ever hand-writes an <a> again, the same rules apply to it: no
  // JSX spread may replace the href this test reads (a spread is applied AFTER
  // the literal attributes, so `{...{ href: "https://…" }}` wins and `href=`
  // never sees it), and no literal URL may sit in an href. (t_2285b2ef.)
  const spreadAnchors = anchorAttributes(pageRaw)
    .filter((a) => a.includes("..."))
    .map((a) => a.replace(/\s+/g, " ").trim());
  assert.deepEqual(
    spreadAnchors,
    [],
    `${PAGE} spreads props into an <a> tag (${JSON.stringify(spreadAnchors)}) — a spread is ` +
      "applied after the literal attributes, so it can silently replace the href this test " +
      "reads, and the guard never sees the destination it writes. Write the anchor's " +
      "attributes out so the href on the page is the href checked here.",
  );
  const hrefs = anchorHrefs(pageRaw);
  // Every href the page writes is one of three things: an in-page fragment, a
  // site path, or the contract constant. Anything else is a destination the
  // checks below do not judge.
  assert.deepEqual(
    hrefs.filter((h) => h !== "OPENAPI_PUBLIC_URL" && !h.startsWith("#") && !h.startsWith("/")),
    [],
    `${PAGE} writes an href that is neither an in-page anchor, a site path, nor ` +
      `href={OPENAPI_PUBLIC_URL} (${JSON.stringify(hrefs)}) — a reader following it lands ` +
      "somewhere this test has not checked",
  );
  const typedURLs = hrefs.filter((h) => /https?:\/\//i.test(h));
  assert.deepEqual(
    typedURLs,
    [],
    `${PAGE} types an absolute URL into an href (${JSON.stringify(typedURLs)}) — this page's ` +
      "one absolute destination is the contract, and it must come from OPENAPI_PUBLIC_URL so " +
      "that the host, path and byte checks below judge one definition.",
  );
  // 3. It must be https (this repo is https-only for caller-facing URLs), on the
  //    product's own origin, at the path the web app serves the asset from — and
  //    not an internal host, an IP or a bucket.
  //
  //    The origin is not typed here: it is lib/siteName.ts's SITE_URL, the one
  //    exported origin constant this repo already keeps for the canonical home
  //    page, so a build of this repo on another origin does not fail the web
  //    gate over a literal host in a unit test (t_d87d9298) — while the contract
  //    still has to be on the product's own site and not on a bucket.
  assert.ok(
    OPENAPI_PUBLIC_URL.startsWith("https://"),
    `OPENAPI_PUBLIC_URL must be https (got ${OPENAPI_PUBLIC_URL})`,
  );
  const contractURL = new URL(OPENAPI_PUBLIC_URL);
  const productOrigin = new URL(SITE_URL).origin;
  assert.equal(
    contractURL.origin,
    productOrigin,
    `the contract is not served from the product's own origin ${productOrigin} ` +
      `(got ${OPENAPI_PUBLIC_URL}) — a reader following the page's link would land somewhere ` +
      "they cannot reach",
  );
  assert.ok(
    !/^[\d.]+$|localhost|127\.|10\.|192\.168\./.test(contractURL.host),
    `OPENAPI_PUBLIC_URL names an internal host or address (${contractURL.host}) — it must stay ` +
      "off the public site",
  );
  // The path must name the asset the web app actually serves: web/public/ is
  // served at the site root, so /openapi.json IS web/public/openapi.json — the
  // file read below. Any other path resolves to nothing, however correct the
  // host looks, and no check below this line can see that.
  assert.equal(
    contractURL.pathname,
    "/openapi.json",
    `OPENAPI_PUBLIC_URL does not name the served asset (${contractURL.pathname}) — the web app ` +
      "serves web/public/openapi.json at the site root, so a different path is a 404 the host " +
      "and byte checks cannot see",
  );

  // 4. And the bytes must really be there to fetch. The web app serves
  //    web/public/openapi.json, so that file is the one the URL resolves to for
  //    the visitor; if it is missing the link 404s no matter how correct it looks.
  const served = JSON.parse(read("../../public/openapi.json")) as {
    openapi?: string;
    paths?: Record<string, unknown>;
  };
  assert.equal(
    served.openapi,
    "3.1.0",
    "web/public/openapi.json is not an OpenAPI 3.1 document — the page's link would serve " +
      "something that is not the contract",
  );
  assert.ok(
    Object.keys(served.paths ?? {}).length > 40,
    "web/public/openapi.json has too few paths to be the contract",
  );
  // The public copy must be byte-identical to the copy the repository publishes,
  // or the contract a third-party developer reads diverges from the one the
  // repository owns. (api/openapi/openapi.json is the generator's own artifact
  // and is compared against a fresh generation in Go — see
  // TestGeneratedArtifactsAreInSync.)
  assert.equal(
    read("../../public/openapi.json"),
    read("../../../api/openapi/openapi.json"),
    "web/public/openapi.json and api/openapi/openapi.json differ — the publicly served " +
      "contract is not the repository's contract",
  );

  // 5. The agent-facing index says the same thing (llms-full.txt is the file an
  //    agent reads instead of the page, and it carried the same private link).
  const llms = read("../../public/llms-full.txt");
  assert.ok(
    llms.includes(OPENAPI_PUBLIC_URL),
    `llms-full.txt does not publish the contract at ${OPENAPI_PUBLIC_URL}`,
  );
  assert.ok(
    !llms.includes("github.com/wzul/jomform"),
    "llms-full.txt still points an agent at the private repository for the contract",
  );
});

test("the page does not advertise a sandbox or a separate test key", () => {
  assert.ok(
    pageText.includes("no separate sandbox or test key"),
    "the page must state that there is no sandbox/test key — the card decided against one, and " +
      "an unstated absence is a support ticket",
  );
  assert.ok(
    !/sandbox key|test api key/i.test(pageProseText),
    "the page offers a sandbox/test key that does not exist",
  );
});

test("the page never offers the legacy admin scope to a new key", () => {
  // api_keys.scopes accepts a legacy "admin" value that ScopeHas treats as a
  // superset. It exists for old keys in production; a new key must never be
  // steered towards it.
  assert.ok(
    !/scope[^.]{0,40}\badmin\b/i.test(pageProseText) || pageText.includes("readwrite"),
    "the page may be presenting admin as a key scope choice",
  );
  assert.ok(
    !pageProseText.includes('readonly", "readwrite", "admin"'),
    "the page lists admin as a key scope",
  );
  // The key-scope prose must name exactly the two supported values.
  assert.ok(
    pageText.includes("readonly") && pageText.includes("readwrite"),
    "the page does not name the two supported key scopes",
  );
});

// ── the `write` LEGEND must not outrun the gate (card t_4abd052a) ─────────────
test("the write legend's caveat comes from the contract, not from prose", () => {
  // The legend is a definition of the LEVEL, rendered once and applied to every
  // write-labelled route. GET /v1/rest/settings/webhook-key is gated by
  // requireWriteScope ALONE — its handler never calls scopeOr403/requireWriteRole,
  // so no ?workspace= role is consulted — and the legend nonetheless promised
  // "manage/admin role inside a ?workspace= scope" for it. Measured on the wire:
  // a readwrite key got 200 on a foreign workspace there, and 403 on a route whose
  // gate does apply the axis.
  //
  // The caveat is DERIVED, so this asserts both that the page renders it and that
  // the set it renders is non-empty and accurate.
  const scoped = scopeOnlyWriteRoutes();
  assert.ok(
    scoped.length > 0,
    "the contract declares no scope-only write route — either the flag was dropped from the " +
      "registry or the published extension stopped being generated, and the page's caveat " +
      "would silently disappear",
  );
  assert.ok(
    scoped.every((e) => e.access === "write" && e.scopeOnly),
    "scopeOnlyWriteRoutes() returned something that is not a write route flagged scope-only",
  );
  // The named route must be in the set — the card's claim is about THIS route, so a
  // set that lost it is wrong even if non-empty.
  assert.ok(
    scoped.some((e) => e.path === "/v1/rest/settings/webhook-key"),
    "GET /v1/rest/settings/webhook-key is not flagged scope-only — the legend over-promises " +
      "that route again",
  );
  // And the PAGE must render the caveat from that set, not from a typed sentence.
  assert.ok(
    pageSrc.includes("scopeOnlyWriteRoutes"),
    `${PAGE} does not derive the write legend's caveat from the contract — the promise can ` +
      "outrun the gate again the moment a route's gate changes",
  );
  assert.match(
    pageSrc,
    /const routes = scopeOnlyWriteRoutes\(\)\s*\.map\(\(e\) => `\$\{e\.method\} \$\{e\.path\}`\)/,
    `${PAGE} no longer names the scope-only routes in the legend's caveat — the caveat must ` +
      "say WHICH routes the 'write' label does not fully describe, from the contract",
  );
  // The caveat lives in the LEGEND, which the rows reference through their
  // `Access` column, so a reader meets it without being told which row is the
  // exception. This page no longer marks the scope-only row individually
  // (ENDPOINT_BLOCKS renders `e.access` per row); the caveat is carried by the
  // level definition every `write` row points at.
  assert.ok(
    sectionIds.includes("endpoints") && pageSrc.includes("e.access"),
    `${PAGE} no longer renders the Access column from the contract — the row would not name ` +
      "the level whose legend carries the caveat",
  );
  assert.ok(
    pageProseText.includes("Not every write route") || pageSrc.includes("**Not every write route:**"),
    "the legend's caveat is not rendered at all — a reader of a `write` row would meet only " +
      "the promise, never the exception",
  );
  // And the route this card was filed on must be in the list the caveat prints.
  assert.ok(
    scoped.map((e) => `${e.method} ${e.path}`).includes("GET /v1/rest/settings/webhook-key"),
    "the caveat's route list does not contain GET /v1/rest/settings/webhook-key — a reader who " +
      "looks up the exception would not find the route that proved it needed one",
  );
});
