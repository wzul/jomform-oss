// The a11y gate must actually SCAN the docs surfaces (card t_f5a8730d).
//
// THE BUG. `web/e2e/a11y.spec.ts` is the axe scan run by the a11y gate
// (`npm run a11y` = `playwright test` over `playwright.config.ts` testDir
// "./e2e"), and at the time of the report its PAGES list held the seven public
// routes with no `/docs/*` path. So PR 676 could edit
// `web/src/app/docs/customize/page.tsx` and a full `npm run a11y` run would
// still pass 7/7 without ever looking at the changed page — the docs knowledge
// base was outside the scan. (Other specs have since been added to ./e2e; this
// one is still the only place the axe PAGE LIST lives.) Measured in a real
// browser against this tree's own production build, all 11 docs routes carried
// at least one critical/serious axe node (color-contrast on the sidebar label,
// plus landmark-unique, plus link-in-text-block on the pages with an in-prose
// cross-link, plus the scrollable <pre>).
//
// WHY A SOURCE-CONTRACT TEST AND NOT A BROWSER TEST. This repo has no DOM/JSX
// unit runner (no vitest/jest — see src/lib/buttonWrap.test.ts and
// src/lib/chatPage.test.ts, written the same way). What CANNOT regress silently
// is the two-part contract that makes the gate meaningful:
//
//   (a) COVERAGE: every docs page that exists is a page the scan visits. A page
//       directory added later without a PAGES entry is a page outside the gate
//       again, and nothing else would notice — that is the exact bug here. It
//       caught main's new /docs/builder on the merge of this very branch.
//   (b) THE FIXES THE ENLARGED GATE DEPENDS ON: each docs `<pre>` that can
//       scroll sideways stays keyboard reachable, and each in-prose
//       cross-page link stays distinguishable without relying on colour.
//       Without these the enlarged gate is red on arrival.
//
// The colour-contrast rule is NOT pinned here: no source-level scan can tell
// slate-400 (2.63:1) from slate-600 (7.58:1) without re-implementing axe's
// compositing, and axe itself is what the gate runs. Its fix is asserted
// indirectly — `text-slate-400` is checked out of the docs tree entirely, which
// is the shape of the change. The real proof is the gate run in the PR evidence.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { docsRoutesOnDisk, routesOnDisk } from "./docsRoutes.ts";
import { pageProse } from "./docsProseReader.ts";
import { isEnglishOnlyProsePath, ENGLISH_ONLY_PROSE_PREFIXES } from "./proseSurfaces.ts";

const pathOf = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));

const SPEC = readFileSync(pathOf("../../e2e/a11y.spec.ts"), "utf8");

/**
 * How a spec reaches axe: `new AxeBuilder(...)` directly, or `gateScan()` from
 * `e2e/axeGate.ts` — the ONE definition of the rule set + failing impacts that
 * `a11y.spec.ts` and the nine scoped arms share (card t_5aa6c8ac; the arms used
 * to carry nine copies of `withTags(["wcag2aa","wcag21aa"])`, which reached 6 of
 * axe's 105 rules). This assertion exists to prove the file we parsed IS the
 * axe spec, so it accepts either entry point rather than pretending only one
 * shape exists.
 */
const AXE_SCAN = /(?:AxeBuilder|gateScan)/;
const DOCS_DIR = pathOf("../app/docs");

// Route -> source file. app/docs/layout.tsx is the shared chrome, not a route.
//
// THE ROUTE LIST IS THE SHARED RECURSIVE WALK (card t_2cf201b8). This file used
// to carry its own one-level `readdirSync(DOCS_DIR)` that mapped EVERY directory
// to `<dir>/page.tsx`, which had two measured defects the sibling guards
// (llmsFullCoverage / sitemapCoverage / docsProseConvention, fixed by t_3a5aa9b5)
// did not have:
//
//   A. `app/docs/chat/zzsub/page.tsx` — a real Next route whose parent directory
//      IS also a route — was not in `DOCS`, so its `<pre>`/`<a>`/contrast rules
//      were never applied to it and the suite stayed 7/7 GREEN. The guard's own
//      "every docs page is scanned" test cannot catch that, because `DOCS` is the
//      list that omits it.
//   B. `app/docs/zz-probe/` with NO `page.tsx` — a directory that is only a
//      parent, or a probe left behind — crashed the four file-reading tests with
//      `ENOENT … zz-probe/page.tsx`, because every directory was mapped to
//      `<dir>/page.tsx` whether or not it was a route.
//
// `docsRoutesOnDisk` (web/src/lib/docsRoutes.ts) keys on `page.tsx` and walks
// recursively, so a nested route under a non-route parent is counted and a
// directory with no `page.tsx` is only ever a parent. The `/docs` index is the
// walk's own `""` relative path; the mapping below renders it as the page file
// the rest of this guard reads.
const fileFor = (route: string): string =>
  route === "/docs"
    ? pathOf("../app/docs/page.tsx")
    : pathOf(`../app/docs/${route.slice("/docs/".length)}/page.tsx`);

const DOCS: { route: string; file: string }[] = docsRoutesOnDisk(DOCS_DIR)
  .map((route) => ({ route, file: fileFor(route) }))
  .sort((a, b) => a.route.localeCompare(b.route));

const LAYOUT_FILE = pathOf("../app/docs/layout.tsx");

/**
 * The shared prose renderer (src/app/docs/docsProse.tsx). Every converted docs
 * page emits its code samples and its in-prose links THROUGH this file, so the
 * two rules below are asserted against it as well as against any page that still
 * hand-writes the markup. The rules did not move off the pages; the markup did.
 */
const RENDERER_FILE = pathOf("../app/docs/docsProse.tsx");
const RENDERER_SRC = readFileSync(RENDERER_FILE, "utf8");

// Paths the scan visits, read out of the real spec file.
const scannedPaths = [...SPEC.matchAll(/path:\s*"([^"]+)"/g)].map((m) => m[1]);

// Every opening `<pre …>` / `<a …>` tag in a file. Matching the OPENING TAG (not
// the whole element) is what stops a className on an unrelated element from
// satisfying a rule. `<Link>` is deliberately not read: next/link renders an
// anchor carrying the caller's class, and the docs pages use it for
// button-styled cards, not for in-prose links.
function tags(src: string, name: string): string[] {
  return [...src.matchAll(new RegExp(`<${name}\\b[^>]*>`, "g"))].map((m) => m[0]);
}

// Exact class TOKEN match. A substring check (`.includes("underline")`) would
// pass `hover:underline`, which is the pre-fix shape this card removes.
function hasClass(tag: string, token: string): boolean {
  const m = tag.match(/className=(?:"([^"]*)"|\{`([^`]*)`\})/);
  if (!m) return false;
  return (m[1] ?? m[2] ?? "").split(/\s+/).includes(token);
}

test("the guard is looking at real files (it cannot pass vacuously)", () => {
  assert.ok(AXE_SCAN.test(SPEC), "e2e/a11y.spec.ts is not the axe spec");
  assert.ok(scannedPaths.length >= 7, `only ${scannedPaths.length} scanned paths were parsed out of the spec`);
  assert.ok(DOCS.length >= 11, `expected the docs tree, found ${DOCS.length} routes`);
  assert.ok(
    DOCS.some((d) => d.route === "/docs") && DOCS.some((d) => d.route === "/docs/customize"),
    "the docs tree is not the one this guard was written against",
  );
  for (const d of DOCS) {
    assert.ok(readFileSync(d.file, "utf8").length > 0, `${d.file} could not be read`);
  }
  assert.ok(LAYOUT_FILE.endsWith("docs/layout.tsx"));
});

test("the scan visits the seven public routes it always did (nothing was dropped to make room)", () => {
  for (const p of [
    "/login",
    "/landing",
    "/reset-password",
    "/magic-link",
    "/brand",
    "/legal/privacy",
    "/legal/terms",
  ]) {
    assert.ok(scannedPaths.includes(p), `the a11y gate no longer scans ${p}`);
  }
});

test("every docs page is scanned — including the one a docs PR edits", () => {
  const missing = DOCS.filter((d) => !scannedPaths.includes(d.route)).map((d) => d.route);
  assert.deepEqual(
    missing,
    [],
    `docs route(s) outside the a11y gate: ${missing.join(", ")} — a page not in PAGES cannot be checked by the gate`,
  );
  assert.ok(
    scannedPaths.includes("/docs/customize"),
    "/docs/customize must be scanned: it is the page a docs PR actually edits",
  );
});

test("no route is listed twice in the scan", () => {
  const dupes = scannedPaths.filter((p, i) => scannedPaths.indexOf(p) !== i);
  assert.deepEqual(dupes, [], `duplicate PAGES entries: ${dupes.join(", ")}`);
});

test("every sideways-scrolling docs <pre> is keyboard reachable (axe scrollable-region-focusable)", () => {
  let checked = 0;
  for (const d of DOCS) {
    const src = readFileSync(d.file, "utf8");
    for (const tag of tags(src, "pre")) {
      if (!hasClass(tag, "overflow-x-auto")) continue;
      checked++;
      assert.ok(
        /\btabIndex=/.test(tag),
        `${d.route}: a scrollable <pre> has no tabIndex — axe fails it with scrollable-region-focusable (serious)`,
      );
    }
  }
  // The code SAMPLE's <pre> is the renderer's now: a converted page carries its
  // sample as a `{ code: "…" }` block, and docsProse emits the one scrollable
  // <pre> that renders it. The page loop above cannot see that tag, so the same
  // rule is asserted against the file that writes it — with the sample's own
  // <code> child checked too, so this is provably the code block and not a
  // stray <pre> somewhere else in the renderer.
  const rendererPres = tags(RENDERER_SRC, "pre").filter((t) => hasClass(t, "overflow-x-auto"));
  assert.ok(
    rendererPres.length >= 1,
    "app/docs/docsProse.tsx emits no overflow-x-auto <pre> — the code-sample block every converted page's " +
      "samples render through is gone, so this rule would be asserting nothing",
  );
  for (const tag of rendererPres) {
    checked++;
    assert.ok(
      /\btabIndex=/.test(tag),
      "app/docs/docsProse.tsx: the code sample's scrollable <pre> has no tabIndex — every converted page's code " +
        "block would fail axe scrollable-region-focusable (serious), measured on the scrollable <pre> the gate flags",
    );
  }
  assert.ok(
    /<pre\b[\s\S]{0,400}?<code>/.test(RENDERER_SRC),
    "app/docs/docsProse.tsx: the scrollable <pre> is not followed by the sample's <code> child — this rule is " +
      "reading the wrong element",
  );
  assert.ok(checked > 0, "no overflow-x-auto <pre> was found, so this rule proved nothing");
});

test("every in-prose docs cross-page link carries a permanent underline (axe link-in-text-block)", () => {
  let checked = 0;
  for (const d of DOCS) {
    const src = readFileSync(d.file, "utf8");
    for (const tag of tags(src, "a")) {
      const href = tag.match(/href=(?:"([^"]*)"|\{`([^`]*)`\})/);
      const target = href?.[1] ?? href?.[2];
      // A cross-page docs link rendered as plain text: not a hash anchor, not a
      // route that is not docs, not a URL built at runtime.
      if (!target || !target.startsWith("/docs")) continue;
      checked++;
      assert.ok(
        hasClass(tag, "underline"),
        `${d.route}: <a href="${target}"> has no bare \`underline\` class — axe fails link-in-text-block (serious); hover:underline is not enough`,
      );
    }
  }
  // A converted page writes its cross-page link as `[label](/docs/…)` in a body
  // string, and the renderer's Inline <a> is what emits it. The anchor is one tag
  // for every such link, so one assertion covers them all — and it is the SAME
  // bare `underline` token the page rule above demands.
  const rendererAnchors = tags(RENDERER_SRC, "a");
  assert.ok(
    rendererAnchors.length >= 1,
    "app/docs/docsProse.tsx emits no <a> — the in-prose link every converted page declares would render as " +
      "plain text, and this rule would be asserting nothing",
  );
  for (const tag of rendererAnchors) {
    checked++;
    assert.ok(
      hasClass(tag, "underline"),
      "app/docs/docsProse.tsx: the Inline <a> has no bare `underline` class — every cross-page link on a " +
        "converted page would fail axe link-in-text-block (serious); hover:underline is not enough",
    );
  }
  // The page half: the links still have to be DECLARED. A tree that stopped
  // writing cross-page links would satisfy both loops above while linking
  // nothing, so the markdown sites are counted from the prose the pages render.
  const declared: string[] = [];
  for (const d of DOCS) {
    const prose = pageProse(readFileSync(d.file, "utf8"), d.file);
    for (const m of prose.matchAll(/\[[^\]]+\]\((\/docs[^)]*)\)/g)) declared.push(`${d.route} -> ${m[1]}`);
  }
  assert.ok(
    declared.length >= 10,
    `found only ${declared.length} in-prose cross-page docs link(s) across the docs tree — the markdown link ` +
      `sites the renderer emits are the subject of this rule, so the guard is looking at the wrong prose`,
  );
  assert.ok(checked > 0, "no in-prose cross-page docs link was found, so this rule proved nothing");
});

test("no docs page or the docs chrome still carries text-slate-400 (2.63:1 at 11px)", () => {
  // Read class TOKENS out of every className in the file, not the raw text, so
  // a comment that names the old class cannot be mistaken for a live usage.
  const classesIn = (src: string): string[] =>
    [...src.matchAll(/className=(?:"([^"]*)"|\{`([^`]*)`\})/g)].flatMap((m) =>
      (m[1] ?? m[2] ?? "").split(/\s+/),
    );
  const offenders: string[] = [];
  let checked = 0;
  for (const file of [LAYOUT_FILE, ...DOCS.map((d) => d.file)]) {
    const tokens = classesIn(readFileSync(file, "utf8"));
    checked += tokens.length;
    if (tokens.includes("text-slate-400")) offenders.push(file);
  }
  assert.ok(checked > 0, "no className was found, so this rule proved nothing");
  assert.deepEqual(
    offenders,
    [],
    `text-slate-400 is 2.63:1 on the white page — a serious color-contrast failure: ${offenders.join(", ")}`,
  );
});

// ══ the document declaration must match the PROSE (card t_c9d23e2c) ══════════
//
// THE DEFECT. `components/i18n.tsx` applied the visitor's DASHBOARD locale to
// `document.documentElement` on every route:
//
//     document.documentElement.dir  = lang === "ar" ? "rtl" : "ltr";
//     document.documentElement.lang = lang;
//
// The knowledge base is English-only by design (`app/docs/docsChrome.tsx`: "The
// docs PROSE stays English: every /docs guide page is static English, and
// half-translating the knowledge base would be worse than not translating it" —
// only the index's chrome LABELS are translated), so an Arabic-preferring
// visitor reading a guide got an ENGLISH document declared as Arabic and
// mirrored right-to-left. Measured live on jomform.com (v1.18.0, fresh browser
// state) by card t_6c4ccef4: with `jf_lang=ar`, /docs/embed read `lang="ar"` /
// `dir="rtl"` with the h1 "Embed a form on your site", every paragraph and every
// nav label in English; the same reading on /legal/privacy. `jf_lang` is a real
// user setting (the /profile "Bahasa dashboard" select persists it), so this was
// user-reachable, and it is WCAG 3.1.1 (Language of Page): the declaration must
// describe the text the page actually contains.
//
// WHAT IS PINNED HERE, and what is NOT:
//   (a) COVERAGE: every route ON DISK under `src/app/docs`, `src/app/legal` and
//       `src/app/brand` is matched by the shared rule
//       (`src/lib/proseSurfaces.ts`) that the provider gates the write on. A new
//       /legal page, a nested docs route, a second /brand page or a typo'd
//       prefix fails HERE rather than silently shipping an Arabic declaration
//       over English prose. (/brand is the third tree, added by card
//       t_4d3691b9: its prose is static English and it was declaring the
//       visitor's locale over it.)
//   (b) THE RULE'S SHAPE: it matches on a SEGMENT boundary (a future
//       `/docsomething` is not swept in) and it does NOT match a translated
//       surface, so the RTL an Arabic dashboard needs is untouched.
//   (c) THE PROVIDER REALLY USES IT, read from `components/i18n.tsx`: the
//       declaration depends on the rule AND on the ROUTE (a client-side
//       navigation out of /docs must move the declaration back), and the
//       direction write is still there for the surfaces whose text is Arabic.
//
// WHAT IT DOES NOT COVER. It reads SOURCE and the filesystem, not the served
// HTML — the executed proof is `web/e2e/a11y-docs-lang.spec.ts` (the `a11y`
// gate), which drives the default state and `jf_lang=ar` in a real browser and
// reads `html` lang/dir plus the computed direction out of the live DOM.

const I18N_FILE = pathOf("../components/i18n.tsx");
const LEGAL_DIR = pathOf("../app/legal");
const BRAND_DIR = pathOf("../app/brand");

// The provider's source, and the ONE declaration effect inside it, sliced out
// around its first statement so a rule below cannot be satisfied by a match
// somewhere else in the file (the file also builds class strings and reads
// localStorage).
const I18N_SRC = readFileSync(I18N_FILE, "utf8");
const EFFECT_ANCHOR = "const prose = isEnglishOnlyProsePath(pathname);";
const EFFECT_AT = I18N_SRC.indexOf(EFFECT_ANCHOR);
const DECLARATION_EFFECT = EFFECT_AT === -1 ? "" : I18N_SRC.slice(EFFECT_AT, EFFECT_AT + 400);

// The English-only prose routes, read from the filesystem by the SAME recursive
// walk the docs guards already share (docsRoutes.ts).
const DOCS_DISK = docsRoutesOnDisk(DOCS_DIR);
const LEGAL_ROUTES = routesOnDisk(LEGAL_DIR, "/legal");
const BRAND_ROUTES = routesOnDisk(BRAND_DIR, "/brand");

test("the prose-declaration guard is reading real files (it cannot pass vacuously)", () => {
  assert.ok(I18N_SRC.includes("export function I18nProvider"), `${I18N_FILE} is not the i18n provider`);
  assert.notEqual(
    EFFECT_AT,
    -1,
    "the provider no longer calls isEnglishOnlyProsePath — the declaration is applied to the " +
      "visitor's locale again on every route, which is the defect this guard exists for",
  );
  assert.ok(
    ENGLISH_ONLY_PROSE_PREFIXES.length > 0,
    "the shared rule carries no prefix at all — nothing can be matched by it",
  );
  assert.ok(
    DOCS_DISK.length >= 11 && DOCS_DISK.includes("/docs") && DOCS_DISK.includes("/docs/customize"),
    `the docs walk found ${DOCS_DISK.length} routes (${DOCS_DISK.slice(0, 3).join(", ")}) — not the tree this ` +
      "rule was written against",
  );
  assert.ok(
    LEGAL_ROUTES.includes("/legal/privacy") && LEGAL_ROUTES.includes("/legal/terms"),
    `the /legal walk found ${JSON.stringify(LEGAL_ROUTES)} — the privacy policy and the terms are the two ` +
      "pages card t_c9d23e2c measured as English prose declared Arabic",
  );
  assert.ok(
    BRAND_ROUTES.includes("/brand"),
    `the /brand walk found ${JSON.stringify(BRAND_ROUTES)} — the public brand-assets page is the surface ` +
      "card t_4d3691b9 measured declaring the visitor's locale over English prose",
  );
});

test("every English-only prose route on disk is matched by the rule the provider uses", () => {
  const routes = [...DOCS_DISK, ...LEGAL_ROUTES, ...BRAND_ROUTES];
  const missing = routes.filter((r) => !isEnglishOnlyProsePath(r));
  assert.deepEqual(
    missing,
    [],
    `these routes are English-only prose but the shared rule does not match them: ${missing.join(", ")} — ` +
      `an English /docs, /legal or /brand page reached in a non-default locale would declare that locale ` +
      `(WCAG 3.1.1) and render RTL over English text`,
  );
  assert.ok(
    routes.length >= 14,
    `only ${routes.length} routes were read off disk — the walk is not reaching the three trees`,
  );
});

test("the rule is a SEGMENT match, and it does not claim a translated surface", () => {
  for (const r of [
    "/docs",
    "/docs/embed",
    "/docs/api",
    "/docs/embed/",
    "/legal",
    "/legal/privacy",
    "/legal/terms",
    "/brand",
    "/brand/",
  ]) {
    assert.ok(
      isEnglishOnlyProsePath(r),
      `${r} is an English-only prose surface (its text is static English) but the rule does not match it`,
    );
  }
  for (const r of [
    "/docssomething",
    "/legals",
    "/docsx/y",
    "/brands",
    "/branding/x",
    "/",
    "/landing",
    "/mcp",
    "/products/p1",
    "",
    null,
    undefined,
  ]) {
    assert.equal(
      isEnglishOnlyProsePath(r),
      false,
      `${JSON.stringify(r)} is NOT an English-only prose surface, but the rule claims it is — a bare ` +
        `string prefix would sweep in /docsomething, and matching a translated page would strip the RTL ` +
        `an Arabic surface needs`,
    );
  }
});

test("the provider declares English-only prose English, and keeps the direction write", () => {
  assert.ok(DECLARATION_EFFECT.includes(EFFECT_ANCHOR), "the declaration effect could not be sliced out");
  // The LANGUAGE follows the rule, not the visitor's locale.
  assert.match(
    DECLARATION_EFFECT,
    /document\.documentElement\.lang\s*=[^;]*prose[^;]*"en"/,
    "the declaration no longer pins the language of an English-only prose page to English — with " +
      "jf_lang=ar the document would be declared Arabic over English text (measured on prod: " +
      "/docs/embed read lang=\"ar\")",
  );
  // The DIRECTION is gated the same way, and RTL survives for the surfaces that
  // need it — "do not simply drop the dir write".
  assert.match(
    DECLARATION_EFFECT,
    /document\.documentElement\.dir\s*=[^;]*prose[^;]*"rtl"/,
    "the direction write is no longer gated on the rule — an English-only prose page would render RTL " +
      "again, or the RTL of a genuinely Arabic surface was dropped",
  );
  assert.match(
    DECLARATION_EFFECT,
    /document\.documentElement\.dir\s*=[^;]*"ltr"/,
    "the direction write has no ltr branch left",
  );
  // The unconditional shape this card removed, named directly.
  assert.doesNotMatch(
    I18N_SRC,
    /document\.documentElement\.lang\s*=\s*lang;/,
    "the provider writes the visitor's locale to the document again, unqualified — the pre-fix shape",
  );
});

test("the declaration follows the ROUTE, not only the language", () => {
  // A client-side navigation from /docs to a translated page must move the
  // declaration back: an implementation that read `window.location` once on
  // mount satisfies every rule above and still leaves the next page wrong.
  assert.match(
    I18N_SRC,
    /import \{ usePathname \} from "next\/navigation";/,
    "the provider no longer reads the route from the router — the declaration cannot follow a " +
      "client-side navigation out of the prose tree",
  );
  assert.match(
    I18N_SRC,
    /\},\s*\[lang,\s*pathname\]\);/,
    "the declaration effect does not depend on the pathname — it would only run when the language " +
      "changes, so navigating from /docs to a translated page would keep the English declaration",
  );
});

// ══ a translated chrome label declares its own language (card t_0e2d71f4) ════
//
// THE DEFECT, the residue of the fix above. `/docs` now correctly declares the
// document `lang="en"` (its prose is English) — and measured live on prod
// v1.21.119 with `jf_lang=ar` (card t_86a8aabd), 46 of its elements contained
// Arabic while 0 of them declared a language of their own. So "وثائق JomForm",
// the section headings and the doc-link buttons were a language CHANGE inside an
// English document with nothing announcing it: WCAG 3.1.2 (Language of Parts), a
// screen reader pronounces them with an English voice. It is not a regression of
// PR 1201 — before that fix the WHOLE document was declared `ar`, so the
// undeclared part was the English prose, the larger error. This is what is left.
//
// axe does NOT flag this class (it has no rule for text in another language
// inside a correctly declared document), so the gate cannot see it. The executed
// proof is the DOM sweep in `web/e2e/a11y-docs-lang.spec.ts`; this leg pins the
// SOURCE, so a revert fails `npm run test:unit` without a browser.
//
// THE SINGLE RENDER SITE is the `T` island in `app/docs/docsChrome.tsx`: every
// translated label on the /docs index goes through it (the sidebar and every
// guide page are static English — measured: every other prose route renders 0
// Arabic elements). So the island's own body is what is read, and it is read with
// COMMENTS STRIPPED: the island's header names `lang` and `en` in prose, and a
// rule a comment can satisfy is the shape this repo's guards already refuse.
const CHROME_FILE = pathOf("../app/docs/docsChrome.tsx");
const CHROME_SRC = readFileSync(CHROME_FILE, "utf8");

// `/* … */` blocks and `//` lines removed. Deliberately simple — the island holds
// no string literal containing `//` or `/*` (asserted below), so the strip cannot
// eat live code.
const stripJsComments = (src: string): string =>
  src.replace(/\/\*[\s\S]*?\*\//g, " ").replace(/(^|[^:])\/\/.*$/gm, "$1");

const ISLAND_AT = CHROME_SRC.indexOf("export function T(");
const ISLAND = ISLAND_AT === -1 ? "" : stripJsComments(CHROME_SRC.slice(ISLAND_AT));

test("the label-language guard is reading the T island (it cannot pass vacuously)", () => {
  assert.notEqual(
    ISLAND_AT,
    -1,
    `${CHROME_FILE} no longer declares the T island — it is the one place a translated /docs label ` +
      `is rendered, so this guard would prove nothing`,
  );
  assert.ok(ISLAND.includes("useI18n"), `the T island slice is not the island: ${JSON.stringify(ISLAND.slice(0, 80))}`);
  assert.ok(
    /return\s*<?/.test(ISLAND),
    "the island slice holds no return, so the rules below could pass without reading the render",
  );
  assert.ok(ISLAND.includes("t("), "the island no longer looks a label up in the dictionary");
});

test("the T island declares the language of a translated label (WCAG 3.1.2)", () => {
  // The label is rendered inside an element carrying `lang`, bound to a VALUE —
  // a literal would freeze one language onto all five locales.
  assert.match(
    ISLAND,
    /lang=\{/,
    "the T island renders its label with no `lang` attribute of its own — inside the English /docs " +
      "document a translated label is a language change nothing declares (WCAG 3.1.2; measured live " +
      "on prod v1.21.119 with jf_lang=ar: 46 Arabic elements on /docs, 0 carrying a lang)",
  );
  assert.doesNotMatch(
    ISLAND,
    /lang="(en|ms|zh|ta|ar)"/,
    "the T island hard-codes a label language — the label's language is the RESOLVED locale, and a " +
      "constant would declare an English label as Arabic (or the reverse) for four of the five locales",
  );
  // English is the document's own language: a label already in it must add
  // nothing, so the island has to single English out.
  assert.match(
    ISLAND,
    /===?\s*"en"/,
    "the island no longer treats English differently — every /docs label would then carry a lang " +
      "attribute, declaring the visitor's locale over text that is already the document's language",
  );
  // The pre-fix shape, named directly, so the revert cannot come back green with
  // the comment above still in place.
  assert.doesNotMatch(
    ISLAND,
    /return\s*<>\{t\(k\)\}<\/>;/,
    "the T island is back to returning the label as a bare text node with no language — the exact " +
      "shape card t_0e2d71f4 removed",
  );
});

test("the label-language rules cannot be satisfied by a comment in the island", () => {
  // Non-vacuity for the strip, and it is self-contained: the file's header (the
  // island's own documentation) names both `lang` and the criterion, so if the
  // strip were a no-op the rules above could be satisfied by PROSE. This test
  // fails only if that stops being true — never merely because the fix is gone,
  // which the test above reports.
  assert.match(
    CHROME_SRC,
    /WCAG 3\.1\.2/,
    "the island's header no longer names the criterion the strip exists to remove — re-point this check " +
      "at whatever comment the rules above could be satisfied by",
  );
  assert.doesNotMatch(
    stripJsComments(CHROME_SRC),
    /WCAG 3\.1\.2/,
    "a comment survived the strip, so the rules above could be satisfied by prose instead of code",
  );
});

