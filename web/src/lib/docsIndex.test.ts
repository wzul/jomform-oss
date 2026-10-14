// /docs index structure guard (t_d768342e).
//
// WHY THIS EXISTS
//   /docs had become a 2,811-word wall: 25 consecutive <section> paragraphs, no
//   categories, and "Getting started" buried under all of it. Every entry was the
//   complete contract text for a feature that already had a guide page. The fix is
//   structural — one-sentence summaries grouped by category, each cut entry linking
//   to the guide that now carries its full text — and it fails in two silent ways
//   that nothing else in this repo would catch:
//
//     1. A summary REGROWING. The page is only readable while each entry stays one
//        sentence, and no type or build step stops a 259-word body coming back.
//        That is exactly how the page reached 2,811 words.
//     2. A DEAD LINK. "See the full guide" pointing at a page or an anchor that no
//        longer exists renders as a perfectly normal link that 404s, or that lands
//        at the top of a page. The docs pages are static and are not covered by any
//        e2e/a11y run, so nothing else checks this.
//
// HOW IT READS THE PAGE
//   /docs is a .tsx page and `node --test` cannot load JSX (the same constraint
//   that put the dictionary in src/lib's sibling i18nDict.ts), so this guard parses
//   the ENTRY declarations out of the source as text. It scans strings rather than
//   matching `body: "..."` with a regex because the bodies contain escaped quotes
//   and non-ASCII punctuation that a regex would split incorrectly.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It cannot execute the React page, so it does not see the rendered layout,
//     wrap points or spacing — only the declared structure.
//   * It reads the SOURCE, not the built HTML, so "renders" is approximated. The
//     comment forms that render nothing are stripped before a fact is searched
//     for — `{/* … */}` containers, module-scope `/* … */`, and `//` lines — which
//     closes the hole where a fact was only ever in a comment. What is left is
//     still an approximation, and stated rather than hidden: a fact can satisfy
//     this guard while a reader never sees it if the text sits in a comment
//     nested inside a rendered template literal, or in source that is
//     syntactically present but unreachable at runtime (a branch that never
//     renders). Closing those needs the built HTML rather than a better parser,
//     which is a bigger change than this guard has. The destination is also
//     scoped to the whole PAGE, so a fact that drifted into a neighbouring
//     section still passes; the anchor itself is checked by the link test above.
//   * The fact count is guarded per entry, but a count cannot judge worth: an edit
//     that removes facts and lowers that entry's floor in the same commit is
//     arithmetically identical to a legitimate re-scope. See the note on
//     PER_ENTRY_FLOOR — it is a speed bump on a silent shrink, not a proof that
//     the surviving facts are the right ones.
//   * It does not judge whether a summary is a GOOD summary, only that it is short
//     and that its link resolves. Prose quality is a review question.
//   * An anchor is checked as DECLARED on the destination page; that the browser
//     scrolls to it also needs the id rendered into the DOM, which is asserted
//     separately for the two anchors a dashboard link reads at runtime. What is
//     read now is the COMMENT-STRIPPED source, so a comment can no longer
//     satisfy this (t_7b24b035) — but an id that is real text the page never
//     renders still passes: a `<div id="x">` sitting in a template literal, or in
//     a branch that never runs, is "declared" here and absent from the built
//     page. Telling those apart needs the built HTML, the same limit stated
//     above for the fact check. Measured cost of the strip in the other
//     direction too: it is a text substitution, not a parser, so the one
//     line-comment form it cannot reach is a `//` that starts on the same line as
//     code (see stripUnrendered).
//   * The VACUITY test reads the same stripped `docsSrc` (t_fc8a9ff2), so it is
//     exactly as wide as the strip and no wider: its whole job is to catch the
//     case the paragraph above refuses to claim ("the slug reaches the DOM"), and
//     a fact in a template literal, in a branch that never renders, or in a `//`
//     that opens after code on the same line would still satisfy it. What the
//     strip closes is the case that was actually shipping — a token carried ONLY
//     by a comment — and that is measured in both directions by
//     web/scripts/biteproof_docs_index_render_sites.sh rather than asserted here.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import { DESTINATION_FACTS, PER_ENTRY_FLOOR } from "./docsFacts.ts";
import assert from "node:assert/strict";
import { readFileSync, existsSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT } from "../components/i18nDict.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const path = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));

const DOCS_PAGE = "../app/docs/page.tsx";

// t_fc8a9ff2: the page is read through the comment strip, not raw.
//
// Every search below asks whether a reader can SEE a fact on the page, and this
// file's own rule is that a comment never counts. `docsSrc` was the raw source,
// so the VACUITY test — the guard's stated answer to "an id that is real text the
// page never renders still passes" — was satisfied by a `//` line carrying the
// same token: measured, moving both render sites off `f.slug` and adding one
// comment line left the whole suite GREEN while /docs rendered no slug id at all
// (control, no comment: RED). It also made a commented-out `title:`/`body:` inside
// GROUPS a real feature entry, since `parseGroups` reads this text directly.
//
// `stripUnrendered` is a function DECLARATION (see its own note) precisely so this
// module-scope call is legal: a declaration hoists, a `const` arrow would still be
// in its temporal dead zone here.
const docsSrc = stripUnrendered(read(DOCS_PAGE));

// The word budget for one summary line. 45 is deliberately a little above the
// "~40 words" the page is designed around: it leaves room for a sentence that
// carries one clause more, while still failing any entry that is a paragraph.
const LIMIT = 45;

// The aggregate word budget for the feature summaries, PER ENTRY.
//
// IT IS NOT A FIXED PAGE CAP (card t_7f0dd2b7). This test asserted `total < 800`
// — a constant — and main arrived at 799: ONE word of headroom. A fixed page cap
// and a feature list that is REQUIRED to grow cannot both hold. The list is the
// contract (the parity test below pins it in both directions), `AGENTS.md` makes
// documentation mandatory for every feature, so every new feature adds an entry
// — which means a fixed cap can only be paid for by trimming an existing
// summary. That is exactly how #791 shipped the page RED at 806 words
// (`48943425`), and why the 9-word trim that bought PR 832's margin (797 at
// `15337da0`) was 799 again one merge later, when PR 804 re-cut one entry
// (`5803b567`): the margin was being spent, never held.
//
// The budget is therefore DERIVED from the structure the page is designed
// around, the way `EXPECTED_TOOL_COUNT` pins the tool registry: the page was
// sized as 800 words for 32 features (measured 798 at `25ac94bf`, the #691
// baseline the verify lane reproduced), i.e. 25 words per feature, and the
// ceiling now moves with the entries actually declared — so a new entry with a
// normal summary can never eat the headroom down to nothing.
//
// Measured on this tree: 33 entries, 799 words (24.2 each) against a budget of
// 25 x 33 = 825. What that still guards, stated honestly in both directions:
//   * a FATTER page reds — the 33 summaries may grow by 26 words in total (one
//     fat entry, or a paragraph smuggled in across several) before this fails;
//   * a TRIM makes it looser, which is the direction the restructure actually
//     wants, and the facts a trim would silently drop are guarded at the
//     destination by DESTINATION_FACTS / PER_ENTRY_FLOOR, not by this count;
//   * the old 800 was in fact the summaries ALONE (the five start-here steps and
//     the FAQ are not in `total` — they are separate consts below the GROUPS
//     table), which is why 33 entries at 799 left the chrome nothing: the fixed
//     cap had already stopped describing the page it was named after.
const PAGE_BUDGET_PER_ENTRY = 25;

// ── a minimal JS string reader ────────────────────────────────────────────────
// Returns the unescaped text of the "..." string starting at s[i] (which must be
// the opening quote) and the index just past its closing quote.
function jsString(s: string, i: number): [string, number] {
  assert.equal(s[i], '"', `expected a string literal at ${JSON.stringify(s.slice(i - 30, i + 30))}`);
  i += 1;
  const out: string[] = [];
  while (i < s.length) {
    const c = s[i];
    if (c === "\\") {
      const n = s[i + 1];
      if (n === "n") out.push("\n");
      else if (n === "t") out.push("\t");
      else out.push(n);
      i += 2;
      continue;
    }
    if (c === '"') return [out.join(""), i + 1];
    out.push(c);
    i += 1;
  }
  throw new Error("unterminated string literal in the docs page");
}

type Entry = { title: string; slug?: string; href?: string; body: string };

// ── parse the GROUPS table ────────────────────────────────────────────────────
function parseGroups(src: string): { entries: Entry[]; labelKeys: string[]; groupIds: string[] } {
  const start = src.indexOf("const GROUPS");
  assert.ok(start !== -1, `${DOCS_PAGE} no longer declares a GROUPS table — the guard's anchor moved`);
  const end = src.indexOf("\n];", start);
  assert.ok(end !== -1, `could not find the end of the GROUPS table in ${DOCS_PAGE}`);
  const region = src.slice(start, end);

  const entries: Entry[] = [];
  const titleAt: number[] = [];
  let pos = 0;
  for (;;) {
    const t = region.indexOf('title: "', pos);
    if (t === -1) break;
    titleAt.push(t);
    pos = t + 1;
  }

  for (let k = 0; k < titleAt.length; k++) {
    // Bound each entry at the next `title: "` so an `href` belonging to a later
    // entry (or a `more` link) can never be attributed to this one.
    const from = titleAt[k];
    const to = k + 1 < titleAt.length ? titleAt[k + 1] : region.length;
    const block = region.slice(from, to);
    // jsString expects the index OF the opening quote, not the one after it.
    const quoteAt = (prefix: string, base: number) => base + prefix.length - 1;
    const [title] = jsString(region, quoteAt('title: "', from));
    const bodyAt = block.indexOf('body: "');
    assert.ok(bodyAt !== -1, `the entry ${JSON.stringify(title)} has no body`);
    const [body] = jsString(region, quoteAt('body: "', from + bodyAt));
    const hrefAt = block.indexOf('href: "');
    const slugAt = block.indexOf('slug: "');
    const entry: Entry = { title, body };
    if (slugAt !== -1 && slugAt < bodyAt) {
      const [slug] = jsString(region, quoteAt('slug: "', from + slugAt));
      entry.slug = slug;
    }
    if (hrefAt !== -1 && hrefAt < bodyAt) {
      const [href] = jsString(region, quoteAt('href: "', from + hrefAt));
      entry.href = href;
    }
    entries.push(entry);
  }

  const labelKeys = [...region.matchAll(/labelKey: "([^"]+)"/g)].map((m) => m[1]);
  const groupIds = [...region.matchAll(/id: "([^"]+)",\n\s+labelKey/g)].map((m) => m[1]);
  return { entries, labelKeys, groupIds };
}

const { entries, labelKeys, groupIds } = parseGroups(docsSrc);

// Resolve a /docs href (with an optional #anchor) to the page that must carry it.
function resolve(pathname: string): string | undefined {
  if (pathname === "/docs") return DOCS_PAGE;
  if (!pathname.startsWith("/docs/")) return undefined;
  const seg = pathname.slice("/docs/".length);
  if (!/^[a-z0-9-]+$/.test(seg)) return undefined;
  // Every docs route is a static page directory under app/docs.
  return `../app/docs/${seg}/page.tsx`;
}

// A page "carries" an anchor when it declares the id — either in a data table
// (`id: "x"`) or directly in JSX (`id="x"`).
//
// t_5425c958: the /docs INDEX is a third form, and it is the only page that gets
// it. Its sections are DATA — an entry declares `slug: "x"` and the page renders
// `id={f.slug}` — so a source scan for `id: "x"` can never find the anchor. That
// gap only surfaced on the rebase: main added inline "?" links that point at
// /docs itself (`/docs#workspaces`, `/docs#two-factor`, `/docs#delete-workspace`)
// and every one of them was reported as a dead anchor while the anchor was real.
//
// Scoped to DOCS_PAGE deliberately, and safe because the vacuity test above
// asserts the index really renders `id={f.slug}` — the slug is not merely
// declared, it reaches the DOM. On any other page `slug:` would be a name with
// no anchor behind it, which is the exact defect this function exists to catch.
// (No guide page uses `slug:` today, so the scope is a guard against that future
// edit rather than something currently load-bearing — said plainly rather than
// implied.)
//
// t_7b24b035: `src` is the COMMENT-STRIPPED source, not `read(file)`. The four
// legs that call this all search a page for a fact a reader must be able to see,
// so text that renders nothing must not satisfy them — and this helper was the
// one reader in the file still searching the raw text while `stripUnrendered`
// was already applied to the nav parse and to every fact check. Measured: a
// sidebar row at `/docs/security#qa-fake-anchor` plus a `//` line containing
// `id: "qa-fake-anchor"` on that page left this guard, the whole `test:unit`
// suite (516/0) and `npm run build` GREEN, while the served HTML carried the
// `href` and no such `id` — the reader landed at the top of the page silently.
// `web/scripts/biteproof_docs_index_comment_anchor.sh` is that proof.
//
// `stripUnrendered` is declared below this function, which is safe rather than
// sloppy: it is only *called* from inside the tests, by which point the whole
// module is evaluated, so the order of the two declarations never matters. It was
// left where it is (next to the doc that explains the comment model) instead of
// being lifted above this helper, because splitting that explanation from its
// code is how one reader of the file gets updated and the other does not — which
// is the defect this card fixed.
function declaresId(file: string, id: string): boolean {
  const src = stripUnrendered(read(file));
  if (src.includes(`id: "${id}"`) || src.includes(`id="${id}"`)) return true;
  return file === DOCS_PAGE && src.includes(`slug: "${id}"`);
}

// The comments a .tsx source can carry that a reader never sees.
//
// A JSX expression container holding only a block comment — `{/* … */}` — is a
// real form on these pages (customize/page.tsx has five, mcp/page.tsx three), and
// it renders NOTHING: the text between the braces is a comment, not an
// expression. The same goes for a block comment declared in the module scope
// above the component, and for a `//` line comment. Stripping tags alone leaves
// all three in the haystack, so a fact could be "carried" by a page whose built
// HTML does not contain it at all. That is not hypothetical: the same class of
// edit — text present in the file, absent from the output — is the defect this
// whole guard was written for.
//
// Deliberately NOT stripped, because their text really does render:
//   * a container with an expression (`{CONST}`, `{SECTIONS.map(…)}`) — the
//     prose on the data-driven pages lives in a `const` and renders that way;
//   * a template literal (`` `…` ``) — customize/page.tsx:108's comment is an
//     ordinary JSX comment, but mcp/page.tsx renders `{"items": […]}` through
//     one, so template literals are prose, not comment.
// The cost of that conservatism is stated under WHAT IT DOES NOT COVER.
//
// Measured limit (t_7b24b035), so the reach of this strip is not over-read: the
// `//` rule is anchored to the start of a LINE, which is what keeps a URL or a
// `//` inside a rendered string from being eaten, and the cost is that a comment
// opening after code on the same line (`<p>x</p> // id: "y"`) survives. No page
// under src/app/docs carries that form today (measured: `grep -rn '[^:"[:space:]]//'`
// over the docs tree returns nothing), and the block-comment and JSX-container
// forms — the ones actually used — are covered. A real tokenizer would close it;
// this is a substitution, and it says which case it cannot see rather than
// implying it sees them all.
//
// A DECLARATION, not a `const` arrow (t_fc8a9ff2): the module-level `docsSrc` read
// above calls this before its own source line is evaluated, and only a hoisted
// function declaration makes that legal. Do not "tidy" it back to an arrow.
function stripUnrendered(src: string): string {
  return (
    src
      // A leading `{/* … */}` container. Matched at the `{` so the expression
      // containers above are untouched.
      .replace(/\{\s*\/\*[\s\S]*?\*\/\s*\}/g, " ")
      // A block comment in the module scope (the only place one can sit outside a
      // container without being a syntax error).
      .replace(/^[ \t]*\/\*[\s\S]*?\*\//gm, " ")
      // A `//` line comment — anchored to the start of a line so a URL, or a `//`
      // inside a rendered string, is not eaten.
      .replace(/^[ \t]*\/\/.*$/gm, " ")
  );
}

// The reader-visible text of a page.
//
// The facts asserted below are phrased the way a READER sees them, which is not
// how the source spells them: `{" "}` is a real space in the output but a dead
// end for a plain substring search, a phrase may be interrupted by <strong>, <a>
// or a highlighted <span>, and punctuation arrives as an entity. So comments are
// dropped first (above), then tags are dropped, entities are decoded, the JSX
// space idiom becomes a space, and runs of whitespace collapse. What is left is
// the prose the page renders.
function readerVisible(src: string): string {
  return stripUnrendered(src)
    .replace(/\{"\s"\}/g, " ")
    .replace(/<[^>]*>/g, " ")
    .replace(/&apos;/g, "'")
    .replace(/&quot;/g, '"')
    .replace(/&amp;/g, "&")
    .replace(/&mdash;/g, "—")
    .replace(/&rarr;/g, "→")
    .replace(/&nbsp;/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}

// The destination's reader-visible text.
//
// Scope is the whole page, deliberately. Section scope was tried first and
// rejected: on the data-driven docs pages the prose is declared in a `const`
// array above the JSX and the section only renders `{SECTIONS.map(...)}`, so
// slicing from an anchor to the next anchor loses the text and flags a CORRECT
// page. A guard that cries wolf gets weakened, which is the failure mode this
// test exists to avoid. What the page scope costs, stated plainly: a fact that
// drifted into a NEIGHBOURING section of the same guide still passes —
// page-existence is what is checked here, and the anchor itself is checked
// separately by the link test above.
function destinationText(file: string): string {
  return readerVisible(read(file));
}

// Every `href="/docs..."` in the app's own JSX (the inline "?" doc links), read
// from the source tree so a new one cannot be added without a destination.
function docsHrefsOutsideDocs(dirs: string[]): { file: string; href: string }[] {
  const found: { file: string; href: string }[] = [];
  const walk = (dir: URL) => {
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      if (e.name === "node_modules") continue;
      const child = new URL(`${e.name}${e.isDirectory() ? "/" : ""}`, dir);
      if (e.isDirectory()) {
        walk(child);
        continue;
      }
      if (!e.name.endsWith(".tsx") && !e.name.endsWith(".ts")) continue;
      if (e.name.endsWith(".test.ts")) continue;
      const file = fileURLToPath(child);
      const src = readFileSync(file, "utf8");
      // Relative to the app dir (web/src/app), which web/Dockerfile does copy —
      // the guard reads the repo-root name only to shorten the reported path.
      const relRoot = new URL("../", import.meta.url);
      for (const m of src.matchAll(/href="(\/docs[^"]*)"/g)) {
        found.push({ file: file.replace(fileURLToPath(relRoot), ""), href: m[1] });
      }
    }
  };
  for (const d of dirs) walk(new URL(d, import.meta.url));
  return found;
}

const ORIGINAL_TITLES = [
  "Order forms",
  "Products",
  "Variation products",
  "Product images on forms",
  // Arrived on main (#657) after this card's structure was written, with the
  // builder's inline "?" pointing at /docs#publishing. It is part of the
  // contract now: dropping it while restructuring would break that link.
  "Publishing a form",
  // t_5425c958: four more arrived on main after the restructure was written —
  // Workspaces and Deleting a workspace (e45ede2 / #682), Two-factor
  // authentication (2FA) (#667) and REST API (#680). Each is a live destination:
  // an inline "?" or a dashboard action links at its anchor, so dropping one
  // while resolving the rebase would break that link the same way.
  "Workspaces (your account's tenant)",
  "Deleting a workspace",
  // PR 691 (the rename). It is a live destination: the Settings → Workspace
  // identity card's inline "?" deep-links /docs#rename-workspace, so the entry
  // (and its slug) has to stay on the index.
  "Renaming a workspace",
  "REST API",
  "Two-factor authentication (2FA)",
  "Sales",
  "One list for sales and responses",
  "Responses (response-only forms)",
  "Form analytics (the per-form counter)",
  "Partial refunds",
  "Subscriptions",
  "Spam protection",
  "Realtime notifications & the order screen beep",
  "Email & notifications",
  "Media library",
  "Settings",
  "Custom domains",
  "Landing pages",
  "Restaurant menu (QR ordering)",
  "Menu basket (multi-item ordering)",
  "Affiliate marketing",
  "Templates",
  // Arrived on main (#679) after this card's structure was written. Form
  // Settings' inline "?" deep-links /docs#form-url-slug, so the entry (and its
  // slug) has to stay on the index; slugRenameErr.test.ts asserts it does.
  "Form URLs and slugs (rename a form)",
  "MCP (AI agents)",
  "Design → AI agent → MCP customization",
  "Embed on your site",
  "Trusted devices (2FA)",
  // t_4c58a09e (the rebase onto main): this branch's own entry. It arrived
  // after the restructure was written, so it is a summary here plus a guide
  // (/docs/activity) that carries the walk-through, like every other cut entry.
  "Activity log (audit trail)",
];

const words = (s: string) => s.trim().split(/\s+/).filter(Boolean).length;

test("the guard is looking at the real docs page (it cannot pass vacuously)", () => {
  assert.ok(
    docsSrc.includes("export default function DocsIndexPage"),
    `${DOCS_PAGE} is not the docs index source — the guard's anchor moved`,
  );
  assert.ok(entries.length >= 20, `parsed only ${entries.length} feature entries from ${DOCS_PAGE}`);
  // The page must still render every body, or the guard would be checking strings
  // that never reach a reader.
  assert.ok(
    docsSrc.includes("{f.body}"),
    `${DOCS_PAGE} does not render {f.body} — the entries would not be shown`,
  );
  assert.ok(
    docsSrc.includes("id={f.slug}"),
    `${DOCS_PAGE} does not render the slug as an id, so the "?" anchors would not scroll`,
  );
  // The labels are translated, and only through the one client island — the page
  // itself must stay a server component so its text (the docs index is in the
  // sitemap and is where /llms.txt and every "?" link point) exists without JS.
  assert.ok(
    !/^\s*"use client"/m.test(docsSrc),
    `${DOCS_PAGE} became a client component — the docs index would lose its static HTML`,
  );
  assert.ok(
    docsSrc.includes("<T k="),
    `${DOCS_PAGE} no longer renders its chrome through <T/> — the category labels would be untranslated`,
  );

  // EVERY ENTRY's TITLE and every guide LINK must still be RENDERED (card t_1f175168).
  //
  // ORIGINAL_TITLES and DESTINATION_FACTS are keyed on `e.title`, and every failure
  // message in this file names a title, so a page that renders no title at all still
  // satisfied the guard — and the parity and word-count tests read the same `entries`, so
  // they did not notice either. Measured on origin/main `8b1ea1c2`: replacing BOTH title
  // render sites with `{"Feature"}` (leaving the key={f.title} attribute alone) kept the
  // whole suite GREEN 10/0, and so did pointing every `href={f.href}` at "/docs".
  //
  // The attribute is NOT the render site: `key={f.title}` appears on those elements too,
  // so the assertions below need the ELEMENT form, not a bare mention of `f.title`. The
  // regexes are the JSX text slots — `>{f.title}` / `>{f.href}` — and each must appear on
  // at least two lines, because the entry heading is rendered twice (the GROUPS branch at
  // :430 and the FAQ branch at :475). One assert per shape, so a mutation that removes
  // one of the two sites is still caught by the count.
  const renderSites = (re: RegExp) => docsSrc.split("\n").filter((l) => re.test(l)).length;

  for (const [re, min, what, tag] of [
    // The JSX TEXT slot — `>{f.title}`. Two sites, so a mutation that removes one is caught.
    [/>\{f\.title\}/, 2, "the entry headings (GROUPS :430 and FAQ :475)", "[render-site-entry-title]"],
    // An ATTRIBUTE slot, not a text slot — the link's destination.
    [/href=\{f\.href\}/, 1, "the 'Full guide' link (:436)", "[render-site-guide-link]"],
  ] as const) {
    const n = renderSites(re);
    assert.ok(
      n >= min,
      `${tag} ${DOCS_PAGE} renders ${re} on ${n} line(s), want at least ${min} — ` +
        `${what} would stop reaching the reader (the guard's keys are the same titles, so ` +
        `it would still be green while every heading read the same word)`,
    );
  }

  // And the literal it would collapse to must not be there: an entry heading that renders
  // a constant is the mutation above, and it is cheap to name.
  assert.ok(
    !/>\{"Feature"\}/.test(docsSrc),
    `${DOCS_PAGE} renders a constant heading instead of {f.title}`,
  );
});

test("every feature entry is one short line (the wall does not come back)", () => {
  const long = entries
    .map((e) => `${words(e.body)} words — ${e.title}`)
    .filter((s, i) => words(entries[i].body) > LIMIT);
  assert.deepEqual(
    long,
    [],
    `these entries are longer than ${LIMIT} words, which is how /docs became a 2,811-word ` +
      `wall:\n  ${long.join("\n  ")}`,
  );

  // And the page as a whole: the original 2,811 words must stay gone, at the
  // PER-ENTRY budget (see PAGE_BUDGET_PER_ENTRY above). The ceiling is derived
  // from the entries the page actually declares, so a new feature moves it and
  // the next entry cannot arrive with one word of headroom left — the trap
  // t_7f0dd2b7 was filed for.
  const total = entries.reduce((n, e) => n + words(e.body), 0);
  const budget = PAGE_BUDGET_PER_ENTRY * entries.length;
  assert.ok(
    total <= budget,
    `the feature summaries total ${total} words against a ${budget}-word budget ` +
      `(${PAGE_BUDGET_PER_ENTRY} words x ${entries.length} entries) — the page is ` +
      `supposed to be scanned in one pass, so tighten the summaries instead of ` +
      `raising the cap. If the page genuinely needs a longer budget, raise ` +
      `PAGE_BUDGET_PER_ENTRY in the same commit and say in the message why.`,
  );
});

test("the five start-here steps are above the feature list", () => {
  const steps = docsSrc.indexOf("docs.gsTitle");
  const features = docsSrc.indexOf("docs.featuresTitle");
  assert.ok(steps !== -1 && features !== -1, "the page no longer renders both headings");
  assert.ok(
    steps < features,
    '"Start here" renders below the feature list — it is supposed to be the first thing on the page',
  );
});

test("every category is a named, translated group", () => {
  assert.ok(groupIds.length >= 6, `only ${groupIds.length} named categories — the page is not categorised`);
  assert.deepEqual([...new Set(groupIds)].length, groupIds.length, "two categories share an id");
  for (const id of groupIds) {
    // Each category id must be a real anchor target on the page.
    assert.ok(
      docsSrc.includes("id={g.id}"),
      "the page does not render the group id, so #<category> does not scroll",
    );
  }
  assert.ok(labelKeys.length === groupIds.length, "a category has no label");
  for (const key of labelKeys) {
    const entry = (DICT as Record<string, Record<string, unknown>>)[key];
    assert.ok(entry, `the dictionary has no ${key} — the category would render a raw key`);
    for (const lang of ["en", "ms", "zh", "ta", "ar"]) {
      const v = entry[lang];
      assert.ok(
        typeof v === "string" && v.trim() !== "",
        `${key}.${lang} is missing — that locale would render English`,
      );
    }
    assert.ok(
      docsSrc.includes("<T k={g.labelKey} />"),
      "the page does not translate its category labels",
    );
  }
});

test("no feature was lost in the reorganisation (docs parity)", () => {
  const titles = new Set(entries.map((e) => e.title));
  const missing = ORIGINAL_TITLES.filter((t) => !titles.has(t));
  assert.deepEqual(missing, [], `these features are gone from /docs:\n  ${missing.join("\n  ")}`);
  assert.deepEqual(
    [...titles].filter((t) => !ORIGINAL_TITLES.includes(t)),
    [],
    "an entry appeared that was not in the original page — the list is the contract",
  );
});

test('every "full guide" link and every "?" anchor reaches real content', () => {
  // 1. Each entry's guide link: the page must exist and carry the anchor.
  const broken: string[] = [];
  for (const e of entries) {
    if (!e.href) continue;
    const [pathname, anchor] = e.href.split("#");
    const file = resolve(pathname);
    if (!file || !existsSync(path(file))) {
      broken.push(`${e.title} → ${e.href} (no such page: ${pathname})`);
      continue;
    }
    if (anchor && !declaresId(file, anchor)) {
      broken.push(`${e.title} → ${e.href} (${pathname} declares no id "${anchor}")`);
    }
  }
  // 2. The dashboard's inline "?" links, read from the app's own source.
  for (const { file, href } of docsHrefsOutsideDocs(["../app/", "../components/"])) {
    const [pathname, anchor] = href.split("#");
    const target = resolve(pathname);
    if (!target || !existsSync(path(target))) {
      broken.push(`${file} → ${href} (no such page: ${pathname})`);
      continue;
    }
    if (anchor && !declaresId(target, anchor)) {
      broken.push(`${file} → ${href} (${pathname} declares no id "${anchor}")`);
    }
  }
  assert.deepEqual(
    broken,
    [],
    `these docs links point at content that does not exist:\n  ${broken.join("\n  ")}`,
  );
});

// ── the destination must carry the entry's FACTS, not just its anchor ────────
//
// Why this exists (the defect this guard is the fix for): the link test above
// checks that a destination page declares the id the entry points at. That
// passed while the Templates entry's gallery-metadata facts were on NO page —
// `/docs` linked /docs/customize#templates, the section existed, and the text
// that used to be in the entry had been dropped rather than moved. Anchor +
// page-exists is a link check; "every cut entry has a link that really reaches
// the full text" is a CONTENT check, and only this list makes it true.
//
// Shape of the list: for each entry that was cut, the facts a merchant must be
// able to read at the destination. They are deliberately FEW and each one is
// load-bearing — a name the engine uses (config.category, refunded_sen), the
// exact sentence a refusal carries, or the one enumeration that has no other
// home ("order forms, booking, donation, contact, survey and more"). A list of
// words lifted from the summary would fail on wording rather than on substance,
// so the facts are chosen as the things a reader would have to be told.
//
// Not asserted: entries with no `href` (Products, Landing pages, Media library,
// Settings) have no cut entry — their whole text is on /docs itself, and the
// length + parity tests cover those.
// DESTINATION_FACTS (the per-entry fact lists) lives in ./docsFacts.ts, imported
// above. It moved there so the SAME table can be checked against the BUILT page by
// web/scripts/docsFacts.mjs: this file reads the guide's source, and source that
// nothing references still satisfies a substring search — measured GREEN with the
// built page carrying the phrases 0 times (card t_5cfeb788).

test("every cut entry's facts are readable at the destination (not just its anchor)", () => {
  // (a) The list must cover every entry that has a destination. An entry added
  // with an `href` and no facts here is unguarded, which is how the Templates
  // entry's text was lost without anything going red.
  const linked = entries.filter((e) => e.href).map((e) => e.title);
  // Membership is asked as an OWN property, never with `in` (t_dd08bc5c). `in` walks
  // the prototype chain, so an entry titled after an Object.prototype member
  // (`constructor`, `toString`) reads as present in a map that has no entry for it at
  // all — exactly the state this assertion exists to report. The same rule applies to
  // the three mirrors below, and it is measured rather than argued: with `in`,
  // renaming a floored entry to `constructor` on EVERY side and deleting its floor
  // line was GREEN 10/10 exit 0 on f682e1c5, while the normal-named version of that
  // same edit was RED.
  assert.deepEqual(
    linked.filter((t) => !Object.hasOwn(DESTINATION_FACTS, t)),
    [],
    `these entries link to a guide but have no facts asserted at the destination — ` +
      `add them to DESTINATION_FACTS, or the entry can be re-cut with nothing to catch it`,
  );
  assert.deepEqual(
    Object.keys(DESTINATION_FACTS).filter((t) => !linked.includes(t)),
    [],
    `DESTINATION_FACTS names entries /docs no longer has — the list drifted from the page`,
  );
  // (a2) The list cannot be quietly thinned into uselessness. Deleting a whole
  // entry trips the parity check above, but SHORTENING one — dropping three of
  // Templates' seven facts so the four that remain still match — leaves a guard
  // that passes while the entry it is supposed to protect can be cut again.
  //
  // What this can and cannot do, measured rather than claimed. A TOTAL floor is
  // anonymous: three Templates facts removed *and* FLOOR lowered 81 -> 78 was
  // green, because those three phrases also render elsewhere on the page, so the
  // content check stays silent and only the number moved.
  //
  // A PER-ENTRY floor is strictly better in one measurable way — the edit that
  // shrinks an entry must now NAME that entry, not move an anonymous total:
  //   * three facts removed, floors untouched       -> RED (Templates: 4 < 7)
  //   * three facts removed and the per-entry floor moved down with them -> still
  //     GREEN. Measured, not a guess. The count check below is an equality, so a
  //     floor and a fact list edited together agree; it catches a shrink that
  //     forgot the floor, and a floor that no longer matches its facts.
  // What it cannot do, and this is the point of writing it down: no check that
  // COUNTS facts can tell a silent shrink from a legitimate re-scope. An editor
  // who deletes three facts, lowers Templates' floor to 4 and says why in the
  // commit gets the same arithmetic as an editor whose copy genuinely moved.
  // This is a speed bump that makes the shrink visible in the diff, not a proof
  // that the surviving facts are the RIGHT ones — choosing them is the review
  // question the file header already names. Replacing a fact with a phrase that
  // does not render is caught separately, by the content check in (b).
  // The floors and the fact lists now live in ./docsFacts.ts, imported above: the
  // build-time reader (web/scripts/docsFacts.mjs) checks the same tables against the
  // BUILT page, and two copies would drift with the drifting one deciding a verdict.
  const thinned = Object.keys(DESTINATION_FACTS)
    .filter((t) => DESTINATION_FACTS[t].length < (PER_ENTRY_FLOOR[t] ?? 0))
    .map((t) => `${t}: ${DESTINATION_FACTS[t].length} < ${PER_ENTRY_FLOOR[t]}`);
  assert.deepEqual(
    thinned,
    [],
    `these entries lost facts, so the guard that protects them got weaker in the same ` +
      `commit:\n  ${thinned.join("\n  ")}. If a fact genuinely no longer belongs (the copy ` +
      `it guarded is gone), lower THAT entry's floor in the same commit and say why; otherwise ` +
      `restore it.`,
  );
  // The count check: every fact in PER_ENTRY_FLOOR must still be one of the facts
  // asserted for that entry, so the surviving list is the one the floor was
  // written against rather than a shorter list the floor was moved down to.
  // OWN keys only, for two measured reasons. `DESTINATION_FACTS[t]` reads through the
  // prototype chain, so an orphan floor line NAMED after a prototype member was
  // compared against that member instead of being reported: `constructor: 1` matched
  // `Function.prototype.length` (which is 1), the line passed here, and the drift check
  // below hid it too — measured GREEN 10/10 exit 0 before this guard asked for own
  // properties. And an orphan line with an ordinary name threw `TypeError: Cannot read
  // properties of undefined (reading 'length')` from this line instead of naming the
  // drift. A key that is absent is the assertion BELOW's finding, not this one's: it is
  // skipped here and reported there, by name.
  const replaced = Object.keys(PER_ENTRY_FLOOR)
    .filter((t) => Object.hasOwn(DESTINATION_FACTS, t))
    .filter((t) => DESTINATION_FACTS[t].length !== PER_ENTRY_FLOOR[t])
    .map((t) => `${t}: ${DESTINATION_FACTS[t].length} facts, floor says ${PER_ENTRY_FLOOR[t]}`);
  assert.deepEqual(
    replaced,
    [],
    `these entries no longer hold the number of facts their floor was written against — ` +
      `facts were swapped for the floor rather than kept:\n  ${replaced.join("\n  ")}. ` +
      `Restore the facts, or write the new floor down next to the facts it now protects.`,
  );
  // The map must also stay in step with DESTINATION_FACTS as entries come and go,
  // in BOTH directions. Only the floor -> facts direction used to be checked, and
  // the other one is the cheaper attack: DELETE one line from PER_ENTRY_FLOOR and
  // that entry has no floor at all, which slips past both count checks above for
  // different reasons — the length check reads the missing value through its `?? 0`
  // as "floor 0" and 0 <= any count passes it, while the equality check iterates
  // PER_ENTRY_FLOOR's OWN keys, so a deleted key is never visited by it at all.
  // Measured GREEN 382/382 on the tree without this assertion, for the deleted
  // `Templates` line alone and for that line plus three of Templates' seven facts.
  // Shrinking an entry by removing its floor instead of lowering it is one line and
  // no number to justify, so a missing floor has to be its own failure rather than a
  // silent skip. (Stated per check because "the `?? 0` fallback" alone does not
  // explain the second one, and the two need different care if either is rewritten.)
  // …AND BOTH DIRECTIONS ASK FOR OWN PROPERTIES, not `in` (t_dd08bc5c). `in` walks
  // the prototype chain, so a name that happens to be an Object.prototype member reads
  // as PRESENT in a map with no entry for it: with `in`, renaming a floored entry to
  // `constructor` on every side and deleting its floor line left this assertion silent
  // (GREEN 10/10, exit 0 on f682e1c5), and the reverse check hid an orphan
  // `constructor: 1` line the same way. The two lists are plain object literals keyed on
  // entry titles, and every title is data a human types, so a title colliding with the
  // prototype is a trap rather than a present defect — 0 of the 31 entries does today —
  // which is exactly why it has to be closed before one does.
  const unfloored = Object.keys(DESTINATION_FACTS).filter((t) => !Object.hasOwn(PER_ENTRY_FLOOR, t));
  assert.deepEqual(
    unfloored,
    [],
    `these entries have no PER_ENTRY_FLOOR — the length check above reads the missing ` +
      `value as floor 0 and passes 0 <= any count, and the equality check only iterates ` +
      `PER_ENTRY_FLOOR's own keys, so it never visits the deleted one: the entry is left ` +
      `unguarded either way:\n  ${unfloored.join("\n  ")}. A floor line is not removed to ` +
      `re-scope an entry (that is lowering it); restore the line, or if the entry genuinely ` +
      `has no facts left to protect, remove its DESTINATION_FACTS entry too.`,
  );
  const drifted = Object.keys(PER_ENTRY_FLOOR).filter((t) => !Object.hasOwn(DESTINATION_FACTS, t));
  assert.deepEqual(
    drifted,
    [],
    `PER_ENTRY_FLOOR names entries DESTINATION_FACTS no longer has — the two lists ` +
      `drifted:\n  ${drifted.join("\n  ")}. Delete the floor line with the entry it ` +
      `protected, or restore the DESTINATION_FACTS list.`,
  );
  assert.ok(
    Object.values(DESTINATION_FACTS).every((f) => f.length >= 2),
    `an entry is down to a single fact — one fact is not enough to catch the entry being cut ` +
      `again, which is the whole point of the list`,
  );
  // (b) Every fact must be readable on the destination. A fact asserted here
  // can only pass if the page really renders it.
  const missing: string[] = [];
  for (const e of entries) {
    const facts = DESTINATION_FACTS[e.title];
    if (!e.href || !facts) continue;
    const [pathname] = e.href.split("#");
    const file = resolve(pathname);
    if (!file || !existsSync(path(file))) {
      missing.push(`${e.title} → ${e.href} (no such page)`);
      continue;
    }
    const lower = destinationText(file).toLowerCase();
    for (const fact of facts) {
      if (!lower.includes(fact.toLowerCase())) missing.push(`${e.title} → ${e.href}: "${fact}"`);
    }
  }
  assert.deepEqual(
    missing,
    [],
    `the guide these entries point at does not carry their text — the entry was cut and the ` +
      `destination is missing the fact, which is exactly how the Templates entry's ` +
      `gallery-metadata half went missing:\n  ${missing.join("\n  ")}`,
  );
});

test("the agent-facing llms-full.txt index points at the same pages", () => {
  // t_d768342e: llms-full.txt is the agent's view of the same knowledge base, so
  // it is kept deliberately (not regenerated) — but a link in it that 404s is the
  // same defect as a dead "?" link, and nothing else checks this file.
  const llms = read("../../public/llms-full.txt");
  const broken: string[] = [];
  for (const m of llms.matchAll(/\(https:\/\/jomform\.com(\/docs[^)#]*)(#[^)]+)?\)/g)) {
    const file = resolve(m[1]);
    if (!file || !existsSync(path(file))) broken.push(`${m[1]} (no such page)`);
    else if (m[2] && !declaresId(file, m[2].slice(1))) {
      broken.push(`${m[1]}${m[2]} (no id "${m[2].slice(1)}")`);
    }
  }
  assert.deepEqual(broken, [], `llms-full.txt links at content that does not exist:\n  ${broken.join("\n  ")}`);
});

// ── the sidebar and the index describe ONE structure, checked BOTH ways ──────
//
// WHY THIS IS NOT JUST "index -> sidebar". The test below has always SAID the
// contract is two-way — "A page that exists only in the sidebar, or a sidebar
// that forgets a page, sends the reader to a list that disagrees with the one
// they are on" — and only ever implemented the second half. The first half had
// never run, so it could not fail: on main 87040114 `/docs/builder` is a real
// route listed in the sidebar's "Start here" group with no entry anywhere on the
// /docs index, and the old body still passed 9/9. Two further holes in that body,
// both closed here: it matched the sidebar as a substring of the layout's SOURCE
// (`nav.includes('"/docs/x"')`), which a comment satisfies; and it read only
// `entries`, so a route reached through a group's `more` link or the quick-link
// row was never looked at.
//
// WHY THE TWO LISTS ARE NOT EQUAL, and what is asserted instead. The sidebar is
// deliberately a superset of the index — /docs/builder, /docs/chat and
// /docs/feedback are reached from each feature's own inline "?" and are not
// indexed. Asserting set equality would fail a correct tree, so the contract is
// checked as three properties:
//
//   (a) every sidebar row is a /docs route, and that route is a page file that
//       exists and declares the anchor the row points at. A typo'd or deleted
//       page in the nav is a 404 or a top-of-page landing that nothing else
//       reads: the sidebar's own deep links are `href:` data, so the app-wide
//       `href="/docs..."` scan above never sees them.
//   (b) every sidebar route is REACHABLE from the index — an entry's href, a
//       group's `more` link, or the quick-link row — unless it is named in
//       NOT_INDEXED_BY_DESIGN, and each of those must really be linked from the
//       app's own source outside the docs tree, so the allowlist cannot be used
//       to park a nav row that nothing links to at all.
//   (c) every route the index links is in the sidebar — the direction that did
//       exist, widened from `entries` to all three ways the index links.
//
// The allowlist is checked in both directions too (an entry the index can now
// reach is dead weight; an entry for a route the sidebar does not carry means
// the list drifted from the nav), because an allowlist that only grows is how a
// guard quietly stops guarding.
//
// What this still cannot see, stated plainly: it reads the SOURCE of the nav and
// of the index, not the built HTML, so a row rendered by a branch that never
// runs would be counted (nothing in this file's shape does that today), and a
// row whose label is wrong is a review question, not a failure here.
const DOCS_LAYOUT = "../app/docs/layout.tsx";

// The index of the `]` that closes the `[` at `open`, found by DEPTH rather than
// by a literal `\n  ],`. String literals are skipped so a `[` or `]` inside an
// href or a label cannot close the array early.
function matchArrayEnd(s: string, open: number): number {
  assert.equal(s[open], "[", `expected a "[" at ${JSON.stringify(s.slice(open - 20, open + 20))}`);
  let depth = 0;
  for (let i = open; i < s.length; i++) {
    const c = s[i];
    if (c === '"') {
      [, i] = jsString(s, i);
      i -= 1; // jsString returns the index JUST PAST the closing quote; the loop adds the 1 back.
      continue;
    }
    if (c === "[") depth += 1;
    else if (c === "]") {
      depth -= 1;
      if (depth === 0) return i;
    }
  }
  return -1;
}

// The sidebar's rows, read out of the layout's own NAV_GROUPS + NAV_SECTIONS.
// The `group` is the heading the row really sits under — it is carried into the
// failure message, so a wrong one sends the reader to the wrong heading. It is
// resolved per `sections: [` by BRACKET DEPTH, not by a
// `label: "…", sections: [ … \n  ],` regex: a group with exactly ONE section is
// written on ONE line in the layout
// (`sections: [{ href: "/docs/embed", … }]`, layout.tsx:57), so a regex that
// wants the closing `\n  ],` never matched that group, the scan resumed past its
// end, and the NEXT group's rows were attributed to it — the `Email` group's
// three rows read as "Storefront & pages" (found while verifying t_221f65fd;
// the guard's SET of rows was always right, only the heading was wrong). The
// pin test at the bottom of this file drives exactly that case.
function parseSidebar(src: string): { group: string; href: string }[] {
  const start = src.indexOf("const NAV_GROUPS");
  assert.ok(start !== -1, `${DOCS_LAYOUT} no longer declares NAV_GROUPS — the guard's anchor moved`);
  const groupsEnd = src.indexOf("\n];", start);
  assert.ok(groupsEnd !== -1, `could not find the end of NAV_GROUPS in ${DOCS_LAYOUT}`);
  // Comments are stripped before parsing: a commented-out row is not a row, and
  // the layout's prose comments do name routes (`/docs/builder (#671)`).
  const rows: { group: string; href: string }[] = [];
  const region = stripUnrendered(src.slice(start, groupsEnd));
  for (const m of region.matchAll(/sections:\s*\[/g)) {
    const open = (m.index ?? 0) + m[0].length - 1;
    const close = matchArrayEnd(region, open);
    assert.ok(close !== -1, `unterminated \`sections\` array in ${DOCS_LAYOUT}`);
    // A group's heading is the nearest `label: "…"` declared above its sections.
    const labelAt = region.slice(0, m.index).lastIndexOf('label: "');
    assert.ok(labelAt !== -1, `a group in ${DOCS_LAYOUT} declares sections but no label`);
    const [group] = jsString(region, labelAt + 'label: "'.length - 1);
    for (const h of region.slice(open, close).matchAll(/href:\s*"([^"]+)"/g)) {
      rows.push({ group, href: h[1] });
    }
  }
  // Issue #513's deep links, rendered under the same sidebar. They are routes
  // too, so they are held to the same existence and anchor rules.
  const sStart = src.indexOf("const NAV_SECTIONS", start);
  assert.ok(sStart !== -1, `${DOCS_LAYOUT} no longer declares NAV_SECTIONS — the guard's anchor moved`);
  const sEnd = src.indexOf("\n];", sStart);
  assert.ok(sEnd !== -1, `could not find the end of NAV_SECTIONS in ${DOCS_LAYOUT}`);
  for (const h of stripUnrendered(src.slice(sStart, sEnd)).matchAll(/href:\s*"([^"]+)"/g)) {
    rows.push({ group: "Guides", href: h[1] });
  }
  return rows;
}

// The sidebar rows that send the reader nowhere, each with the heading the row
// really sits under. Extracted from the structural test below so the pin test at
// the bottom of the file drives the SAME code — a copy of this loop in the pin
// would let the heading in the message regress while the pin stayed green.
function brokenSidebarRows(rows: { group: string; href: string }[]): string[] {
  const broken: string[] = [];
  for (const { group, href } of rows) {
    const [pathname, anchor] = href.split("#");
    if (pathname !== "/docs" && !pathname.startsWith("/docs/")) {
      broken.push(`${href} (${group}) — the docs sidebar lists a route outside /docs`);
      continue;
    }
    const file = resolve(pathname);
    if (!file || !existsSync(path(file))) {
      broken.push(`${href} (${group}) — no page for ${pathname}`);
      continue;
    }
    if (anchor && !declaresId(file, anchor)) {
      broken.push(`${href} (${group}) — ${pathname} declares no id "${anchor}"`);
    }
  }
  return broken;
}

// The routes the /docs index links, each with the surface it links them from.
// Three surfaces, because the page reaches a guide three ways and the old check
// read the first one only.
function indexLinkedRoutes(src: string): Map<string, string> {
  const out = new Map<string, string>();
  for (const e of entries) if (e.href) out.set(e.href.split("#")[0], `the entry ${JSON.stringify(e.title)}`);
  const gStart = src.indexOf("const GROUPS");
  assert.ok(gStart !== -1, `${DOCS_PAGE} no longer declares GROUPS — the guard's anchor moved`);
  const gEnd = src.indexOf("\n];", gStart);
  assert.ok(gEnd !== -1, `could not find the end of the GROUPS table in ${DOCS_PAGE}`);
  const more = stripUnrendered(src.slice(gStart, gEnd));
  for (const m of more.matchAll(/more:\s*\[([\s\S]*?)\]/g)) {
    for (const h of m[1].matchAll(/href:\s*"([^"]+)"/g)) out.set(h[1].split("#")[0], "a group's more link");
  }
  const qStart = src.indexOf("const QUICK_LINKS");
  assert.ok(qStart !== -1, `${DOCS_PAGE} no longer declares QUICK_LINKS — the guard's anchor moved`);
  const qEnd = src.indexOf("\n];", qStart);
  assert.ok(qEnd !== -1, `could not find the end of the QUICK_LINKS row in ${DOCS_PAGE}`);
  for (const h of stripUnrendered(src.slice(qStart, qEnd)).matchAll(/href:\s*"([^"]+)"/g)) {
    out.set(h[1].split("#")[0], "a quick link");
  }
  return out;
}

// Sidebar routes the index deliberately does NOT link, and why. Every one of
// these must be linked from the app's own source outside the docs tree (the
// per-feature inline "?" of AGENTS.md documentation-parity rule 4), which is
// what makes "unreachable from the index" a decision rather than an oversight:
// a reader looking for the form builder arrives from the builder.
const NOT_INDEXED_BY_DESIGN: Record<string, string> = {
  "/docs/builder": 'the builder page\'s inline "?" (src/app/builder/page.tsx)',
  "/docs/chat": 'the chat page\'s inline "?" (src/app/chat/page.tsx)',
  "/docs/feedback": 'the feedback page\'s inline "?" (src/app/feedback/page.tsx)',
};

test("the categories in the docs sidebar match the categories on the page", () => {
  const nav = read(DOCS_LAYOUT);
  const rows = parseSidebar(nav);
  // Vacuity: a parse that silently found nothing would satisfy every check
  // below without looking at a single row. The sidebar carries 26 today.
  assert.ok(
    rows.length >= 20,
    `parsed only ${rows.length} rows out of ${DOCS_LAYOUT} — the guard is not looking at the nav`,
  );
  const routes = [...new Set(rows.map((r) => r.href.split("#")[0]))].sort();

  // (a) Every row is a /docs route that is a page which exists — and, for the
  // deep links, one that declares the anchor it points at.
  const broken = brokenSidebarRows(rows);
  assert.deepEqual(
    broken,
    [],
    `these docs sidebar rows send the reader nowhere (a typo'd, renamed or deleted page, or an ` +
      `anchor that no longer exists):\n  ${broken.join("\n  ")}`,
  );

  // (b) The direction that was missing: the index can REACH every sidebar route.
  const linked = indexLinkedRoutes(docsSrc);
  assert.ok(
    linked.size >= 14,
    `the index parse found ${linked.size} linked routes — the surfaces it reads moved`,
  );
  const fromSource = new Set(
    docsHrefsOutsideDocs(["../app/", "../components/"]).map((f) => f.href.split("#")[0]),
  );
  const unreachable = routes.filter(
    (r) => r !== "/docs" && !linked.has(r) && !(r in NOT_INDEXED_BY_DESIGN),
  );
  assert.deepEqual(
    unreachable,
    [],
    `these pages exist only in the docs sidebar — the index never links them, so a reader on ` +
      `/docs cannot find them, and nothing said that was deliberate:\n  ${unreachable.join("\n  ")}. ` +
      `Add the route to the index (an entry, a group's \`more\` link or a quick link), or name it in ` +
      `NOT_INDEXED_BY_DESIGN together with the feature's own inline "?" that reaches it.`,
  );

  // (b2) …and the allowlist has to be honest in every direction.
  const allowlisted = Object.keys(NOT_INDEXED_BY_DESIGN);
  assert.deepEqual(
    allowlisted.filter((r) => !routes.includes(r)),
    [],
    `NOT_INDEXED_BY_DESIGN names routes the sidebar does not carry — the list drifted from the nav`,
  );
  // The exception is for a route the INDEX cannot reach, and only for that:
  // if the index links it after all, the row is not an exception, it is dead
  // weight in a list whose whole job is to be a short, readable set of "reached
  // from the feature, not from the index" cases. An allowlist that only grows
  // is how a guard stops guarding.
  assert.deepEqual(
    allowlisted.filter((r) => linked.has(r)),
    [],
    `NOT_INDEXED_BY_DESIGN calls these routes unreachable from the index, but the index links ` +
      `them — the exception is stale, and a list of stale exceptions hides the next real one:\n  ` +
      `${allowlisted.filter((r) => linked.has(r)).join("\n  ")}. Remove the row, or make the index ` +
      `stop linking the route if that link was the mistake.`,
  );
  // (b3) …and each exception must point at a route its own feature really links,
  // so the list cannot be used to park a sidebar row that nothing reaches at all.
  const deadEnds = allowlisted.filter((r) => {
    const file = resolve(r);
    if (!file || !existsSync(path(file))) return true;
    return !fromSource.has(r);
  });
  assert.deepEqual(
    deadEnds,
    [],
    `these routes are excused from the index but nothing in the app links them either — the ` +
      `sidebar row is a dead end for the reader as well, so the feature's inline "?" named beside ` +
      `it in NOT_INDEXED_BY_DESIGN is not there:\n  ${deadEnds.join("\n  ")}`,
  );

  // (c) The direction that did exist, now covering all three index surfaces.
  const missing = [...linked.keys()].filter((p) => !routes.includes(p));
  assert.deepEqual(
    missing,
    [],
    `these guide pages are linked from /docs but absent from the docs sidebar (unreachable in ` +
      `navigation):\n  ${missing.join("\n  ")}`,
  );
});

// ── the group a failure message names, pinned (t_d722f70c) ───────────────────
//
// The structural test above only ever reads the heading parseSidebar carries
// while the suite is RED — it asserts that NO row is broken, so on a correct nav
// every green run throws that heading away unread. It was wrong: a group with
// exactly ONE section is written on ONE line in the layout
// (`sections: [{ href: "/docs/embed", … }]`, layout.tsx:57), the old regex
// required the closing `\n  ],` on its own line, and the scan resumed past that
// group — so the NEXT group's rows were attributed to it. Measured on main
// 72092730: 7 groups parsed (Email missing) and `/docs/ses-reputation
// (Storefront & pages)`, while that row is declared in the `Email` group at
// layout.tsx:60-66. The row SET was always right (18 rows, the same routes);
// only the heading was wrong, which is why nothing went red.
//
// This test drives a missing page on EVERY row, one at a time, and asserts the
// heading the failure message names is the group that row is really declared
// under — through the same `brokenSidebarRows` the structural test uses.
//
// WHY EVERY ROW AND NOT THREE HAND-PICKED ONES. Every row is the case that
// actually regressed: a wrong attribution is a property of WHERE a row sits, and
// the three rows the defect happened to hit were the three after the one-line
// group. A per-row sweep is also the wider net — it catches the same class at a
// group boundary this card never measured (a two-row group's second row, a
// hand-edited one-row group elsewhere) — and it needs no premise about the
// layout's FORMATTING: the expectation is derived from the same structural fact
// parseSidebar keys on (`label: "…"` followed by `sections: [`), with any
// whitespace between the two, so it cannot cry wolf on a reformat the way a
// hand-written "a single-section group is on one line" assertion would. What it
// does assume is that a group is still spelled that way, and that assumption is
// asserted below rather than trusted (t_8e94f213).
//
// The expectation is derived independently of the text parse: for each group,
// the rows are exactly its declared `[start, end)` byte range in the source.
test("a broken sidebar row is reported under the group it is really declared in", () => {
  const nav = read(DOCS_LAYOUT);
  const rows = parseSidebar(nav);
  const hrefs = rows.map((r) => r.href);
  assert.ok(rows.length >= 20, `parsed only ${rows.length} rows out of ${DOCS_LAYOUT}`);
  assert.equal(
    new Set(hrefs).size,
    hrefs.length,
    `two sidebar rows share an href, so this pin cannot tell which one it mutated`,
  );
  // The nav region whose declared blocks the expectation is derived from — read
  // off the SOURCE, not off the parse under test.
  const navRegion = nav.slice(
    nav.indexOf("const NAV_GROUPS"),
    nav.indexOf("\n];", nav.indexOf("const NAV_GROUPS")),
  );

  // One mutation per row: point it at a route with no page, then ask the guard
  // which group it blames.
  const reported = new Map<string, string>();
  for (const href of hrefs) {
    const slug = href.slice("/docs/".length);
    const mutated = nav.replace(
      `href: "${href}"`,
      `href: "/docs/t-d722f70c-gone-${slug}"`,
    );
    assert.notEqual(mutated, nav, `could not mutate the ${href} row in ${DOCS_LAYOUT}`);
    const mutatedRows = parseSidebar(mutated);
    assert.equal(
      mutatedRows.length,
      rows.length,
      `mutating ${href} changed the number of rows parsed — the group parse is eating a row boundary`,
    );
    const broken = brokenSidebarRows(mutatedRows);
    assert.equal(
      broken.length,
      1,
      `expected exactly one broken row for ${href}, got:\n  ${broken.join("\n  ")}`,
    );
    reported.set(href, broken[0]);
  }

  // The expected attribution, derived from the source's own GROUP DECLARATIONS —
  // a `label: "…"` followed by `sections: [` — and the byte range each one owns,
  // NOT from the parse under test. The separator is `\s*`, NOT a literal
  // newline: `label: "Email", sections: [` on one line is a one-keystroke edit,
  // and the whole group on one line is the shape this pin was written for
  // (t_d722f70c), so a newline here drops that group from the derivation
  // entirely — its rows then fall into the PREVIOUS declaration's `[from, to)`
  // range, which keeps the row set and the order, and the run blames the group
  // above while the parse is right (t_8e94f213, measured against the shipped
  // parse). It states where a row is DECLARED, which is the question, and it
  // does so identically for a one-line group and a multi-line one.
  //
  // (`label:` is shared by groups and by sections, so "the last label before the
  // row" would pick a section's own text; a group's label is the one that
  // `sections:` follows.)
  const region = stripUnrendered(navRegion);
  const decls = [...region.matchAll(/label:[ \t]*"([^"]+)",\s*sections:[ \t]*\[/g)];
  const expected: string[] = [];
  const declaredUnder = new Map<string, string>();
  decls.forEach((d, i) => {
    const from = d.index ?? 0;
    const to = i + 1 < decls.length ? (decls[i + 1].index ?? region.length) : region.length;
    for (const h of region.slice(from, to).matchAll(/href:\s*"([^"]+)"/g)) {
      expected.push(h[1]);
      declaredUnder.set(h[1], d[1]);
    }
  });
  // Issue #513's deep links are declared in NAV_SECTIONS and parsed under the
  // hard-coded "Guides" heading; they are held to the same message rule, and
  // their hrefs carry an anchor, so they are a different shape from a group row.
  const sStart = nav.indexOf("const NAV_SECTIONS");
  assert.ok(sStart !== -1, `${DOCS_LAYOUT} no longer declares NAV_SECTIONS — the nav shape moved`);
  const sEnd = nav.indexOf("\n];", sStart);
  assert.ok(sEnd !== -1, `could not find the end of NAV_SECTIONS in ${DOCS_LAYOUT}`);
  const navSectionHrefs = [
    ...stripUnrendered(nav.slice(sStart, sEnd)).matchAll(/href:\s*"([^"]+)"/g),
  ].map((h) => h[1]);
  for (const href of navSectionHrefs) {
    expected.push(href);
    declaredUnder.set(href, "Guides");
  }

  // Vacuity, EXACTLY — and this is the assertion the t_8e94f213 defect needed.
  // The floor that used to sit here (`decls.length >= 6`) is a floor on the
  // COUNT: the layout declares 8 groups in NAV_GROUPS, so 6 satisfies it and a
  // group that drops out of `decls` is invisible. Neither is the
  // `expected`/`hrefs` order check below a substitute — a dropped group keeps
  // the row SET and the source ORDER, so it stays green while every row in that
  // group is measured against the group above it. So compare the GROUPS the
  // parse actually carried (minus the NAV_SECTIONS rows, which this derivation
  // attributes to "Guides" explicitly) with the groups this derivation found. A
  // group missing here is this pin's OWN premise breaking, so it has to fail as
  // such — BEFORE the heading comparison below reads a wrong heading out of it
  // and blames the group above, which is the one thing this pin exists to
  // prevent. The other direction (a declaration the parse does not carry) is the
  // parse's regression, and the row-level comparison below is what names it, so
  // that one is checked last.
  const navSectionSet = new Set(navSectionHrefs);
  const parsedGroups = new Set(
    rows.filter((r) => !navSectionSet.has(r.href)).map((r) => r.group),
  );
  const undeclared = [...parsedGroups].filter((g) => !decls.some((d) => d[1] === g));
  assert.deepEqual(
    undeclared,
    [],
    `the parse carried rows under ${undeclared.length} group(s) that this pin's own ` +
      `group-declaration scan did not find in ${DOCS_LAYOUT}: ${undeclared.join(", ")}. Every ` +
      `row in such a group would be measured against the declaration BEFORE it, and the failure ` +
      `message under test would then name the wrong heading — re-derive this expectation before ` +
      `trusting a verdict`,
  );
  assert.deepEqual(
    expected,
    hrefs,
    `${DOCS_LAYOUT}'s declared group order and the parsed row order have drifted — this pin's ` +
      `expectation is derived from the source's own group blocks, so re-derive it before ` +
      `trusting a verdict`,
  );

  const wrong: string[] = [];
  for (const [href, heading] of declaredUnder) {
    const message = reported.get(href) ?? "(no row parsed)";
    if (!message.includes(`(${heading})`)) {
      wrong.push(`${href} is declared under ${heading} but reported as: ${message}`);
    }
  }
  assert.deepEqual(
    wrong,
    [],
    `the docs sidebar failure message names the wrong group for these rows (a reader is sent to ` +
      `a heading that does not contain the row):\n  ${wrong.join("\n  ")}`,
  );
  // …and the same disagreement in the OTHER direction: a declaration this
  // derivation found that the parse carried no rows under (a repeated group
  // label, or a group whose `sections` array is empty). Asserted after the
  // row-level comparison, so a PARSE regression is still reported as the rows it
  // misattributes rather than as a count.
  assert.equal(
    decls.length,
    parsedGroups.size,
    `this pin's group-declaration scan found ${decls.length} declarations in ${DOCS_LAYOUT} but ` +
      `the parse carried rows under ${parsedGroups.size} of them — the derivation above and ` +
      `parseSidebar disagree about the nav's shape (a repeated group label, or an empty ` +
      `\`sections\`); re-derive this expectation against parseSidebar before trusting a verdict`,
  );
  // Vacuity: every row really was mutated and every reported heading was read.
  assert.equal(reported.size, hrefs.length);
});
