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
//     source of the page. The rendered result is checked in a browser.
//   * It does not judge the prose quality, only that the two load-bearing
//     statements are still there and that the numbers the page states about
//     itself agree with the contract (the throttled count and the total).
//   * The OpenAPI document's own validity and route parity are proven in Go
//     (api/internal/httpapi/openapi_parity_test.go), not here.
//
// Run with:  npm run test:unit
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
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

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const path = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));

/**
 * The page's SOURCE with JSX text wrapping collapsed and comments dropped.
 *
 * Both matter and both are why a naive `src.includes(...)` is useless here:
 *   * JSX wraps prose across lines, so "do not design a client around a uniform
 *     quota" is four lines apart in the source and NOT a substring of it. A
 *     text assertion that does not normalise whitespace silently checks nothing.
 *   * The page's header comment quotes `openapiSpec.gen.ts` while explaining why
 *     the page must NOT import it — so a raw `includes` check reads a comment as
 *     code. Comment lines are stripped, like buildContext.test.ts does.
 */
function normalized(src: string): string {
  return src
    .split("\n")
    .filter((l) => !l.trim().startsWith("//") && !l.trim().startsWith("*") && !l.trim().startsWith("/*"))
    .join(" ")
    .replace(/\s+/g, " ")
    .toLowerCase();
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

const PAGE = "../app/docs/api/page.tsx";
const pageRaw = read(PAGE);
const pageSrc = pageRaw;
const pageText = normalized(pageRaw);
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
  assert.ok(
    pageSrc.includes("{group.endpoints.map"),
    `${PAGE} does not iterate the grouped endpoints — the reference would render nothing`,
  );

  // EVERY /v1/rest/ path the page mentions must be a real operation. Prose is
  // allowed to name the base path or a specific endpoint (a quickstart step has
  // to say which route to call), but it may not invent one: a path in that prose
  // that is not in the contract is documentation for an endpoint that 404s.
  const contractPaths = new Set(specEndpoints().map((e) => e.path));
  const mentioned = [...pageRaw.matchAll(/\/v1\/rest\/[A-Za-z0-9/{}_.-]*/g)].map((m) => m[0]);
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
  const hasCorsSection = pageSrc.includes('id="cors"');
  assert.ok(hasCorsSection, `${PAGE} lost the CORS section — the API's browser behaviour is the ` +
    "single most expensive thing to leave undocumented");

  const lower = pageSrc.toLowerCase();
  assert.ok(
    lower.includes("no cors") || lower.includes("no cors headers at all"),
    "the page no longer says there is no CORS",
  );
  assert.ok(
    pageSrc.includes("Access-Control-Allow-Origin"),
    "the page no longer names the header it does not send",
  );
  assert.ok(
    lower.includes("server-to-server"),
    "the page no longer frames the API as server-to-server",
  );
  // And it must not promise the opposite anywhere.
  assert.ok(
    !/adds? (the )?(cors|Access-Control-Allow)/i.test(pageSrc),
    "the page suggests CORS headers are sent, which contradicts the API",
  );
});

test("the in-page nav covers every section, and every link lands on one", () => {
  // The hand-written nav listed 7 of the 9 sections, so two headings were
  // reachable only by scrolling. Both directions are checked: a section with no
  // nav entry (unfindable) and a nav entry with no section (a dead anchor).
  const sectionIds = [...pageRaw.matchAll(/<section id="([^"]+)"/g)].map((m) => m[1]);
  assert.ok(sectionIds.length >= 8, `parsed only ${sectionIds.length} sections from ${PAGE}`);

  const navIds = [...pageRaw.matchAll(/\{ id: "([^"]+)", title: /g)].map((m) => m[1]);
  assert.ok(navIds.length > 0, "the page no longer declares PAGE_SECTIONS — the nav is hand-written again");

  const missingFromNav = sectionIds.filter((id) => !navIds.includes(id));
  assert.deepEqual(
    missingFromNav,
    [],
    `these sections are rendered but missing from the in-page nav (reachable only by scrolling):\n  ` +
      missingFromNav.join("\n  "),
  );
  const dangling = navIds.filter((id) => !sectionIds.includes(id));
  assert.deepEqual(
    dangling,
    [],
    `the in-page nav links to anchors that no section declares (a dead click):\n  ${dangling.join("\n  ")}`,
  );
  assert.ok(
    pageSrc.includes("{PAGE_SECTIONS.map"),
    "the nav no longer renders from PAGE_SECTIONS — the two lists would drift apart",
  );
});

test("the page states that most routes are NOT rate limited", () => {
  assert.ok(pageSrc.includes('id="limits"'), `${PAGE} lost the rate-limit section`);

  assert.ok(
    pageText.includes("most of this api has no rate limit"),
    "the page no longer states that most routes are unmetered — the honest fact about this API",
  );
  assert.ok(
    pageText.includes("do not design a client around a uniform quota"),
    "the page no longer warns against assuming a uniform quota",
  );
  // The page must list ONLY the throttled routes, and the list must come from the
  // contract rather than a hand-typed enumeration.
  assert.ok(
    pageSrc.includes("rateLimitedEndpoints"),
    `${PAGE} does not derive its throttled-route list from the contract`,
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
  // And the access vocabulary the page explains is the vocabulary in use.
  const levels = accessLevels();
  for (const level of levels) {
    assert.ok(
      pageSrc.includes(`${level}:`),
      `the contract uses access "${level}" but the page never explains it`,
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
  assert.ok(
    pageSrc.includes("apiExamples.map"),
    `${PAGE} does not render the examples from the generated data`,
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
  // 2. The page's anchors must POINT AT that constant. Asserting only that the
  //    identifier appears in the file is satisfied by the identifier left in the
  //    anchor's link TEXT — both anchors print the URL as their visible label —
  //    so a typed href pointing anywhere at all still passed. The href is the
  //    half a reader follows, so the href attributes are what is read here.
  const hrefs = anchorHrefs(pageRaw);
  const toContract = hrefs.filter((h) => h === "OPENAPI_PUBLIC_URL");
  assert.ok(
    toContract.length >= 2,
    `${PAGE} must point BOTH of its contract anchors at href={OPENAPI_PUBLIC_URL} — ` +
      `${toContract.length} of the page's ${hrefs.length} hrefs do ` +
      `(${JSON.stringify(hrefs)}). The link text still names the constant, so only the ` +
      "anchor itself says where a reader is sent.",
  );
  // And a URL typed into an href is the same defect one step from the hole above:
  // a destination defined in the page instead of in the guarded constant, where
  // neither this test nor the fetcher on the other end can judge it. The page's
  // one absolute destination is the contract; its other hrefs are in-page
  // fragments and site-relative paths.
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
  const lower = pageSrc.toLowerCase();
  assert.ok(
    lower.includes("no separate sandbox or test key"),
    "the page must state that there is no sandbox/test key — the card decided against one, and " +
      "an unstated absence is a support ticket",
  );
  assert.ok(
    !/sandbox key|test api key/i.test(pageSrc),
    "the page offers a sandbox/test key that does not exist",
  );
});

test("the page never offers the legacy admin scope to a new key", () => {
  // api_keys.scopes accepts a legacy "admin" value that ScopeHas treats as a
  // superset. It exists for old keys in production; a new key must never be
  // steered towards it.
  assert.ok(
    !/scope[^.]{0,40}\badmin\b/i.test(pageSrc) ||
      pageSrc.includes("readwrite") ,
    "the page may be presenting admin as a key scope choice",
  );
  assert.ok(
    !pageSrc.includes("readonly\", \"readwrite\", \"admin\""),
    "the page lists admin as a key scope",
  );
  // The key-scope prose must name exactly the two supported values.
  assert.ok(
    pageSrc.includes("readonly") && pageSrc.includes("readwrite"),
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
  // And the PAGE must render it from that set, not from a typed sentence.
  assert.ok(
    pageSrc.includes("scopeOnlyWriteRoutes"),
    `${PAGE} does not derive the write legend's caveat from the contract — the promise can ` +
      "outrun the gate again the moment a route's gate changes",
  );
  assert.ok(
    pageSrc.includes("e.scopeOnly"),
    `${PAGE} does not mark the scope-only ROW itself, so a reader who reads only the row ` +
      "still meets the over-promise",
  );
});
