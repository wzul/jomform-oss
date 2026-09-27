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
//     separately for the two anchors a dashboard link reads at runtime.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, existsSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT } from "../components/i18nDict.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const path = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));

const DOCS_PAGE = "../app/docs/page.tsx";
const docsSrc = read(DOCS_PAGE);

// The word budget for one summary line. 45 is deliberately a little above the
// "~40 words" the page is designed around: it leaves room for a sentence that
// carries one clause more, while still failing any entry that is a paragraph.
const LIMIT = 45;

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
function declaresId(file: string, id: string): boolean {
  const src = read(file);
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
const stripUnrendered = (src: string): string =>
  src
    // A leading `{/* … */}` container. Matched at the `{` so the expression
    // containers above are untouched.
    .replace(/\{\s*\/\*[\s\S]*?\*\/\s*\}/g, " ")
    // A block comment in the module scope (the only place one can sit outside a
    // container without being a syntax error).
    .replace(/^[ \t]*\/\*[\s\S]*?\*\//gm, " ")
    // A `//` line comment — anchored to the start of a line so a URL, or a `//`
    // inside a rendered string, is not eaten.
    .replace(/^[ \t]*\/\/.*$/gm, " ");

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

  // And the page as a whole: the original 2,811 words must stay gone. 800 leaves
  // room for the summaries, the five start-here steps and the FAQ.
  const total = entries.reduce((n, e) => n + words(e.body), 0);
  assert.ok(
    total < 800,
    `the feature summaries total ${total} words — the page is supposed to be read in ~2 minutes`,
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
const DESTINATION_FACTS: Record<string, string[]> = {
  "Sales": [
    "reference code",
    "pending/confirmed/failed/refunded",
    "confirm bank-transfer sales",
    "export sales to csv",
    "reports the mode as",
  ],
  "One list for sales and responses": [
    "one combined record count",
    "record_type",
    "the status filter is a payment question",
    "list_sales",
  ],
  "Responses (response-only forms)": [
    "mode to response in the builder",
    "detail drawer",
    "list_form_submissions",
  ],
  "Form analytics (the per-form counter)": [
    "Paid+Confirmed",
    "never added together",
    "get_form_analytics",
  ],
  "Partial refunds": ["refunded_sen", "outstanding_sen", "partially refunded", "per-sale lock"],
  "Subscriptions": [
    "billing period",
    "number of charges",
    "schedule mode",
    "retries automatically",
    "never revokes the customer's saved card token",
  ],
  "Realtime notifications & the order screen beep": [
    "sound_enabled",
    "Live pill",
    "get_realtime_status",
    "buyers are never played a sound",
  ],
  "Order forms": ["drag-and-drop builder", "products", "custom domain"],
  "Variation products": ["its own name, price, stock and image", "list_product_variations"],
  "Product images on forms": ["image-top", "product_image_size"],
  // The entry QA falsified. Every fact below was absent from the tree this
  // guard was written against; the last three are the gallery-metadata half
  // that had been dropped instead of moved.
  "Templates": [
    "config.category",
    "first text block",
    "set a category and put a short intro line in a text block",
    "refused with a clear message",
    "bare title",
    "bulk publish skips such a template",
    "order forms, booking, donation, contact, survey and more",
  ],
  "Publishing a form": [
    "Open form",
    "Copy link",
    "something to sell",
    "bulk_forms",
    "acknowledge",
  ],
  "Restaurant menu (QR ordering)": [
    "set_menu_page",
    "generate_qr",
    "table number",
    "order form",
  ],
  "Menu basket (multi-item ordering)": [
    "add-to-cart",
    "quantity stepper",
    "Clear basket",
    "reload",
  ],
  "Custom domains": ["custom.jomform.com", "auto-create the cname", "Let's Encrypt"],
  "Email & notifications": [
    "resolution order is form → workspace → built-in default",
    "sale.paid",
    "response.submitted",
    "delivered → opened → clicked",
  ],
  "Affiliate marketing": ["?aff=CODE", "create_affiliate", "get_affiliate_sales"],
  "Spam protection": ["un-flag", "30 days", "per workspace", "spam_judge_enabled"],
  "Trusted devices (2FA)": [
    "trusted device",
    "Revoke",
    "revoke all / log out everywhere",
  ],
  "MCP (AI agents)": ["Model Context Protocol", "OAuth 2.1", "mcp.jomform.com/mcp"],
  "Design → AI agent → MCP customization": ["Canva", "custom_css", "upload_media"],
  "Embed on your site": ["jomform:height", "640px", "postMessage"],
  // t_5425c958: the four entries that arrived on main after the restructure. Each
  // is a summary on /docs with a guide carrying the walk-through, so each gets the
  // same treatment as the entries above: the facts a reader would have to be told
  // are asserted at the destination, chosen from what the guide actually says.
  //
  // "Workspaces (your account's tenant)" and "Deleting a workspace" both point at
  // the guide this rebase added (/docs/workspaces), whose two sections carry the
  // 231- and 251-word bodies main had on the index. The delete facts are the three
  // conditions and the two things a merchant would otherwise discover by pressing
  // the button: the typed slug and the refusal to destroy sales.
  "Workspaces (your account's tenant)": [
    "CHIP credentials",
    "{slug}.jomform.com",
    "asks you to create one",
    "New workspace",
    // t_e3ebe979: main's #743/#750 prose — the legacy-provisioning chain and the
    // one-public-host rule — lived in the /docs entry's own body. The restructure
    // cuts that entry to a summary, so the paragraph moved to the guide; these
    // two facts are what keeps it from being cut again.
    "first five characters of your email address",
    "a form has exactly one public host",
  ],
  "Deleting a workspace": [
    "typed confirmation",
    "leaves through Settings → Members instead",
    "the last workspace can never be deleted",
    "sales block the delete",
    "delete a form for good",
    "check_workspace_deletion",
  ],
  // PR 691: the entry whose 259-word body the /docs restructure refuses. It is a
  // summary pointing at /docs/workspaces#rename-workspace, so its facts are the
  // things a merchant has to be told before changing an address they already
  // published: the two different authorities, what a reserved label does, and
  // that the old address keeps working (a permanent redirect, and — unlike a
  // form-slug rename — one that no other workspace can ever claim).
  "Renaming a workspace": [
    "counted in characters rather than bytes",
    "Only the OWNER can change it",
    "redirecting permanently (HTTP 301) to the new one",
    "never handed to another workspace",
    "requires confirm=true for an address change",
  ],
  "Two-factor authentication (2FA)": [
    "6-digit",
    "magic link",
    "Disabling 2FA also clears every trusted device",
    "single-use",
  ],
  "REST API": ["OpenAPI 3.1", "Settings → API keys", "readonly"],
  // Arrived on main (#679) after the restructure was written; the entry is a
  // summary with the walk-through on the customize guide, like every other cut
  // entry, so its facts are asserted at that anchor. They are the three things a
  // merchant has to be told before renaming a URL they already published: the
  // character rule, what the refusal is, and that the old URL keeps working
  // through a temporary redirect (never a permanent alias).
  "Form URLs and slugs (rename a form)": [
    "1–40 characters",
    "refused with an error",
    "temporary redirect (http 301)",
  ],
};

test("every cut entry's facts are readable at the destination (not just its anchor)", () => {
  // (a) The list must cover every entry that has a destination. An entry added
  // with an `href` and no facts here is unguarded, which is how the Templates
  // entry's text was lost without anything going red.
  const linked = entries.filter((e) => e.href).map((e) => e.title);
  assert.deepEqual(
    linked.filter((t) => !(t in DESTINATION_FACTS)),
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
  const PER_ENTRY_FLOOR: Record<string, number> = {
    Sales: 5,
    "One list for sales and responses": 4,
    "Responses (response-only forms)": 3,
    "Form analytics (the per-form counter)": 3,
    "Partial refunds": 4,
    Subscriptions: 5,
    "Realtime notifications & the order screen beep": 4,
    "Order forms": 3,
    "Variation products": 2,
    "Product images on forms": 2,
    Templates: 7,
    "Publishing a form": 5,
    "Restaurant menu (QR ordering)": 4,
    "Menu basket (multi-item ordering)": 4,
    "Custom domains": 3,
    "Email & notifications": 4,
    "Affiliate marketing": 3,
    "Spam protection": 4,
    "Trusted devices (2FA)": 3,
    "MCP (AI agents)": 3,
    "Design → AI agent → MCP customization": 3,
    "Embed on your site": 3,
    // t_5425c958: the four entries this rebase added/kept, at the count each
    // DESTINATION_FACTS list above holds. The equality check below fails if a fact
    // is dropped without this floor moving with it.
    "Workspaces (your account's tenant)": 6,
    "Deleting a workspace": 6,
    // PR 691: the rename entry's summary, at the count its DESTINATION_FACTS list
    // above holds. The facts are the two authorities, the character-counted cap,
    // the permanent redirect and the no-claimant rule — the things the old
    // paragraph carried that a reader must still be able to find.
    "Renaming a workspace": 5,
    "Two-factor authentication (2FA)": 4,
    "REST API": 3,
    // t_e3ebe979: main's #679 entry, at the count its DESTINATION_FACTS list
    // above holds (the equality check below fails if a fact is dropped without
    // this floor moving with it).
    "Form URLs and slugs (rename a form)": 3,
  };
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
  const replaced = Object.keys(PER_ENTRY_FLOOR)
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
  const unfloored = Object.keys(DESTINATION_FACTS).filter((t) => !(t in PER_ENTRY_FLOOR));
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
  assert.deepEqual(
    Object.keys(PER_ENTRY_FLOOR).filter((t) => !(t in DESTINATION_FACTS)),
    [],
    `PER_ENTRY_FLOOR names entries DESTINATION_FACTS no longer has — the two lists drifted`,
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

test("the categories in the docs sidebar match the categories on the page", () => {
  // One structure, described twice (the /docs page and the docs layout's
  // sidebar). A page that exists only in the sidebar, or a sidebar that forgets a
  // page, sends the reader to a list that disagrees with the one they are on.
  const nav = read("../app/docs/layout.tsx");
  const pages: string[] = [];
  for (const e of entries) if (e.href) pages.push(e.href.split("#")[0]);
  const missing = [...new Set(pages)].filter((p) => !nav.includes(`"${p}"`));
  assert.deepEqual(
    missing,
    [],
    `these guide pages are linked from /docs but absent from the docs sidebar (unreachable in ` +
      `navigation):\n  ${missing.join("\n  ")}`,
  );
});
