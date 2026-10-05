// The apex sitemap must name every docs page that exists (card t_0447a70c).
//
// WHY THIS EXISTS
//   /docs/chat shipped as a public, canonical-carrying, load-bearing page — it is
//   the target of the chat panel's inline "?" and it documents the created-form /
//   bin boundary — while `web/src/app/sitemap.ts` did not list it. Measured on
//   prod, 2026-10-04: `curl -s https://jomform.com/sitemap.xml | grep -c docs/chat`
//   -> 0, while `/docs/chat` answers 200 and carries
//   `<link rel="canonical" href="https://jomform.com/docs/chat"/>`. The sitemap's
//   own comment already stated the rule that was broken: "A docs page that is not
//   in the sitemap is a page a search engine will not find (the nav is the other
//   half)." /docs/builder and /docs/feedback were missing the same way, and the
//   three are exactly the routes /docs deliberately does NOT index (each is
//   reached from its own feature's inline "?"), so a NAV decision — taken
//   deliberately and guarded in docsIndex.test.ts — had been silently read as a
//   CRAWLER decision, which nobody had decided or checked.
//
// WHAT IT PINS, in both directions
//   1. FORWARD: every real page under app/docs is an entry in APEX_PAGES. A new
//      guide cannot be added without its sitemap line again — which is the
//      defect: the three missing pages were never noticed by any guard, because
//      the only two sitemap assertions in the repo named /docs/activity and
//      /docs/api by hand (activityPage.test.ts:296, docsApi.test.ts:369) and no
//      guard read the LIST against the tree.
//   2. REVERSE: every APEX_PAGES entry resolves to a page file. A typo'd or
//      deleted entry is a 404 advertised to crawlers from the one file a crawler
//      trusts; this list is read by nothing else in the repo, so nothing else
//      could catch it. That covers the non-/docs apex entries too (the landing
//      page, /legal/*, /brand, /mcp), which is why the check is written as
//      "resolves to a page on disk" rather than "is a /docs route".
//   3. No duplicates: an entry listed twice emits the same URL twice.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It reads the SOURCE of sitemap.ts, not the served /sitemap.xml. The route
//     is `force-dynamic`, so the XML is produced by the running app; `next build`
//     is what proves the file still compiles into a route, and the served-XML
//     evidence for this card is a read against a built app, quoted in its
//     handoff rather than asserted here. A guard that read the built XML would
//     need the build ordering docsFacts.mjs works under (`npm run build` runs it
//     AFTER `next build`), which is a bigger change than this card has.
//   * It pins the LIST, not each entry's changeFrequency/priority.
//   * It does NOT decide whether a route SHOULD be public — it holds the already
//     taken decision that every page under app/docs is a public, canonically
//     advertised guide. A /docs page that must stay out of the index would need
//     an explicit allowlist here, and adding one is a review question (the same
//     shape docsIndex.test.ts uses for the three not-indexed-by-design routes).
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join } from "node:path";

// The recursive `app/docs` walk, shared with llmsFullCoverage.test.ts and
// docsProseConvention.test.ts — see that file for why ONE walk (card t_3a5aa9b5).
import { docsRoutesOnDisk } from "./docsRoutes.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const pathOf = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));

const SITEMAP_PAGE = "../app/sitemap.ts";
const SITEMAP = read(SITEMAP_PAGE);
const DOCS_DIR = pathOf("../app/docs");
const APP_DIR = pathOf("../app");

// Comments are dropped before the list is parsed: the file's own prose explains
// the rule and NAMES routes (`/docs/chat` appears in the comment this card
// added), so a reader that searched the raw text would take a comment for an
// entry — the same hole docsIndex.test.ts's stripUnrendered closes for the docs
// page, and it matters here because the comment above the three new entries
// names all three of them.
function stripComments(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, " ")
    .replace(/^[ \t]*\/\/.*$/gm, " ")
    .replace(/[ \t]\/\/.*$/gm, " ");
}

// The `"…"` entries of the APEX_PAGES array, in order.
//
// Sliced between `const APEX_PAGES = [` and the `\n];` that closes it, so an
// array added below (or an import above) cannot be read as part of this one.
function parseApexPages(src: string): string[] {
  const stripped = stripComments(src);
  const start = stripped.indexOf("const APEX_PAGES");
  assert.ok(start !== -1, `${SITEMAP_PAGE} no longer declares APEX_PAGES — this guard's anchor moved`);
  const end = stripped.indexOf("\n];", start);
  assert.ok(end !== -1, `could not find the end of the APEX_PAGES array in ${SITEMAP_PAGE}`);
  const region = stripped.slice(start, end);
  return [...region.matchAll(/"([^"]*)"/g)].map((m) => m[1]);
}

const APEX_PAGES = parseApexPages(SITEMAP);

// docsRoutesOnDisk (imported from ./docsRoutes.ts) returns every route under
// app/docs that is a real page, as `/docs` or `/docs/<slug>`. The walk is RECURSIVE
// and keys on `page.tsx`, so a nested route under a non-route parent is counted; it
// used to be a flat one-level walk, which skipped it silently (card t_3a5aa9b5).

// The page file an apex entry must resolve to: `/docs/x` -> app/docs/x/page.tsx,
// `/legal/privacy` -> app/legal/privacy/page.tsx, and "" -> the landing page.
function pageFileFor(entry: string): string {
  return entry === "" ? join(APP_DIR, "page.tsx") : join(APP_DIR, entry.slice(1), "page.tsx");
}

test("the guard is reading the real sitemap (it cannot pass vacuously)", () => {
  // The card's own acceptance, asserted first so a broken parse is not reported
  // as a missing page: the file must still build URLs the way the route does.
  assert.ok(
    SITEMAP.includes("https://jomform.com${p}"),
    `${SITEMAP_PAGE} no longer builds each entry as https://jomform.com\${p} — the URL this ` +
      `guard reasons about (and the prod reading that found the defect) would not be the one shipped`,
  );
  assert.ok(
    APEX_PAGES.length >= 20,
    `parsed only ${APEX_PAGES.length} entries out of ${SITEMAP_PAGE} — the guard is not looking at ` +
      `the list, so every check below would be vacuous`,
  );
  assert.ok(APEX_PAGES.includes(""), `${SITEMAP_PAGE} no longer lists the landing page ("")`);
  assert.ok(APEX_PAGES.includes("/docs"), `${SITEMAP_PAGE} no longer lists /docs itself`);
  // The list this guard checks must be the same list the route emits.
  assert.ok(
    /export\s+default\s+async\s+function\s+sitemap/.test(SITEMAP),
    `${SITEMAP_PAGE} no longer exports the route handler — the parsed list would not be served`,
  );
  assert.ok(
    SITEMAP.includes("APEX_PAGES.map"),
    `${SITEMAP_PAGE} no longer maps APEX_PAGES into the emitted URLs — the array is no longer what ` +
      `the sitemap route serves`,
  );
});

test("/docs/chat is in the sitemap (the route a crawler could not find)", () => {
  // The card's stated acceptance, spelled out: the array entry AND the URL the
  // route would emit for it. Named separately from the tree-wide check below so
  // the regression that produced this card fails with its own message.
  assert.ok(
    APEX_PAGES.includes("/docs/chat"),
    `"/docs/chat" is missing from ${SITEMAP_PAGE}'s APEX_PAGES — the page is public (it carries ` +
      `its own canonical), it is the target of the chat panel's inline "?" and it documents the ` +
      `created-form / bin boundary, so a crawler that only reads the sitemap cannot find it ` +
      `(measured on prod 2026-10-04: grep -c docs/chat on /sitemap.xml was 0 while /docs/chat answered 200)`,
  );
  assert.ok(
    `https://jomform.com${APEX_PAGES.find((p) => p === "/docs/chat") ?? ""}` ===
      "https://jomform.com/docs/chat",
    `the /docs/chat entry does not produce https://jomform.com/docs/chat — the URL a crawler reads ` +
      `must be the page's canonical (the page declares that exact canonical)`,
  );
});

test("every docs page on disk is in the sitemap (a new guide cannot go missing again)", () => {
  const routes = docsRoutesOnDisk(DOCS_DIR);
  // Vacuity: a walk that found nothing would satisfy the assertion below while
  // looking at no page at all. The docs tree carries 18 routes today.
  assert.ok(
    routes.length >= 15,
    `found only ${routes.length} docs routes under ${DOCS_DIR} — the walk is not looking at the tree`,
  );
  const missing = routes.filter((r) => !APEX_PAGES.includes(r));
  assert.deepEqual(
    missing,
    [],
    `these docs pages exist as routes but are absent from ${SITEMAP_PAGE}'s APEX_PAGES, so a search ` +
      `engine that reads the sitemap will not find them (the sitemap's own rule):\n  ${missing.join("\n  ")}\n` +
      `Add the route to APEX_PAGES, or — if the page is deliberately not public — say so in this ` +
      `guard's allowlist with the reason, rather than leaving it out silently.`,
  );
});

test("every sitemap entry resolves to a page (no 404 is advertised to crawlers)", () => {
  const broken = APEX_PAGES.filter((p) => !existsSync(pageFileFor(p)));
  assert.deepEqual(
    broken,
    [],
    `these ${SITEMAP_PAGE} entries have no page file on disk, so the sitemap advertises a 404 from ` +
      `the one file a crawler trusts:\n  ${broken.map((p) => `${p === "" ? "/" : p} (no ${pageFileFor(p)})`).join("\n  ")}`,
  );
  // And the shape of the list is what the route assumes: a leading "/" or the
  // empty string, never a trailing slash or a full URL (either would emit a URL
  // that is not the page's canonical).
  const malformed = APEX_PAGES.filter((p) => p !== "" && !/^\/[a-z0-9/-]+$/.test(p));
  assert.deepEqual(
    malformed,
    [],
    `these ${SITEMAP_PAGE} entries are not a bare path (leading "/", no trailing slash, no scheme) — ` +
      `the route concatenates them onto https://jomform.com, so each one would emit a URL that is ` +
      `not the page's canonical:\n  ${malformed.join("\n  ")}`,
  );
});

test("no entry is listed twice", () => {
  const dupes = APEX_PAGES.filter((p, i) => APEX_PAGES.indexOf(p) !== i);
  assert.deepEqual(
    dupes,
    [],
    `these entries appear more than once in ${SITEMAP_PAGE}, so the same URL is emitted twice:\n  ${dupes.join("\n  ")}`,
  );
});
