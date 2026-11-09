#!/usr/bin/env node
// Docs registry + search index generator: app/docs → the two files the chrome reads.
//
// WHY A GENERATOR (card t_45fd1bee, the /docs documentation restructure)
//   /docs was 30 hand-written pages with a hand-written sidebar: adding a page meant
//   editing the nav, the index, the sitemap and the search — four lists that could drift.
//   The ONE authored source stays where it already was (`NAV_GROUPS` in
//   src/app/docs/layout.tsx, which docsIndex.test.ts reads as source text), and this
//   script derives from the pages themselves:
//
//     src/lib/docsNav.gen.ts        the registry: path, label, layer, section, order,
//                                   updated, the related-guide mapping, and the flattened
//                                   reading order breadcrumb / prev-next / related use
//     src/lib/docsSearch.gen.json   the search index: every page's title, description,
//                                   headings and prose, built HERE at build-prep time
//
//   The search index is generated rather than fetched: the dialog filters this file in
//   memory, so typing never issues a request, and a new page is indexed by existing on
//   disk (it does not need a line in an index file).
//
// WHAT IT REFUSES RATHER THAN GUESSES
//   * a NAV_GROUPS row whose page file does not exist → reported, not silently indexed;
//   * a guide in guides.gen.ts with no page file → reported;
//   * a page on disk that the nav does not list → still indexed (appended under "More"),
//     which is the acceptance test "add a page and it appears in the index"; the nav
//     omission is printed as a warning because docsIndex.test.ts is the guard that
//     decides whether an unlisted page is deliberate.
//
// The two generated files are COMMITTED, like src/lib/guides.gen.ts and
// src/lib/apiExamples.gen.ts: the image build does not carry this script's inputs in the
// order a build needs. src/lib/docsNav.test.ts regenerates in memory and fails when the
// committed output drifts.
//
// WHY `updated` IS PINNED TO THE PAGE BYTES, NOT TO A COMMIT DATE (card t_746c9c5d)
//   It used to be `git log -1 --format=%cs -- <page>`, the committer date of the last commit
//   that touched the page. That is not landing-stable: this repo lands by SQUASH, and a
//   squash commit contains every file in the PR diff, so each docs page in it gets a new
//   committer date — the landing time — while the registry holds the date of the last page
//   edit. Merging a docs PR on any day after its last edit then made a clean checkout of
//   `main` fail `docs:nav:check` without anybody touching a page.
//   So each entry now carries `digest`, the sha256 of the page bytes the other fields were
//   generated from. While those bytes are unchanged the committed `updated` stands (a
//   landing commit cannot re-date a page nobody edited); when they change, the date is
//   recomputed from git and the changed digest makes `--check` fail until `npm run docs:nav`
//   is run — which is the drift this generator exists to catch.
//
// Run:  node scripts/docsNav.mjs            (write the generated files)
//       node scripts/docsNav.mjs --check    (write nothing; exit 1 if out of date)

import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import ts from "typescript";

const WEB_DIR = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const DOCS_DIR = join(WEB_DIR, "src", "app", "docs");
const LAYOUT_FILE = join(DOCS_DIR, "layout.tsx");
const GUIDES_FILE = join(WEB_DIR, "src", "lib", "guides.gen.ts");
const OUT_NAV = join(WEB_DIR, "src", "lib", "docsNav.gen.ts");
const OUT_SEARCH = join(WEB_DIR, "src", "lib", "docsSearch.gen.json");

const CHECK = process.argv.includes("--check");
const problems = [];
const warnings = [];

/** Every `page.tsx` under a directory, recursively. A directory that is only a parent is
 *  not a route — same key the four repo-wide docs walks use (src/lib/docsRoutes.ts). */
function pageFiles(dir) {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) out.push(...pageFiles(full));
    else if (entry.name === "page.tsx") out.push(full);
  }
  return out.sort();
}

/** `/docs` for the tree root, otherwise `/docs/<posix relative dir>`. */
function routeOf(file, docsDir = DOCS_DIR) {
  const rel = relative(docsDir, dirname(file)).split("\\").join("/");
  return rel === "" ? "/docs" : `/docs/${rel}`;
}

/** Drop comments before reading the nav as data: the layout's prose names routes. */
function stripComments(src) {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, " ")
    .replace(/^[ \t]*\/\/.*$/gm, " ")
    .replace(/[ \t]\/\/.*$/gm, " ");
}

/** The index of the `]` matching the `[` at `open`, skipping string literals. */
function matchArrayEnd(src, open) {
  let depth = 0;
  for (let i = open; i < src.length; i++) {
    const c = src[i];
    if (c === '"') {
      i += 1;
      while (i < src.length && src[i] !== '"') {
        if (src[i] === "\\") i += 1;
        i += 1;
      }
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

/**
 * The authored nav, read out of layout.tsx:
 *   { layer, label, sections: [{ href, label, guides: [...] }] }
 * The shape is the one docsIndex.test.ts already parses (`label: "…", sections: [`), so
 * this generator and that guard agree on what a nav row IS.
 */
function readNav(src) {
  const text = stripComments(src);
  const start = text.indexOf("const NAV_GROUPS");
  if (start === -1) throw new Error("layout.tsx no longer declares NAV_GROUPS");
  const end = text.indexOf("\n];", start);
  if (end === -1) throw new Error("could not find the end of NAV_GROUPS in layout.tsx");
  const region = text.slice(start, end);
  const groups = [];
  for (const m of region.matchAll(/sections:\s*\[/g)) {
    const open = (m.index ?? 0) + m[0].length - 1;
    const close = matchArrayEnd(region, open);
    if (close === -1) throw new Error("unterminated sections array in layout.tsx");
    const labelAt = region.slice(0, m.index).lastIndexOf('label: "');
    if (labelAt === -1) throw new Error("a NAV_GROUPS group declares sections but no label");
    const label = region.slice(labelAt + 8, region.indexOf('"', labelAt + 8));
    const layerAt = region.slice(0, labelAt).lastIndexOf('layer: "');
    const layer = layerAt === -1 ? "Product docs" : region.slice(layerAt + 8, region.indexOf('"', layerAt + 8));
    const sections = [];
    for (const obj of region.slice(open, close).matchAll(/\{([^{}]*)\}/g)) {
      const body = obj[1];
      const href = body.match(/href:\s*"([^"]+)"/);
      const rowLabel = body.match(/label:\s*"([^"]+)"/);
      if (!href) continue;
      const guidesBlock = body.match(/guides:\s*\[([^\]]*)\]/);
      const guides = guidesBlock
        ? [...guidesBlock[1].matchAll(/"([^"]+)"/g)].map((g) => g[1])
        : [];
      sections.push({ href: href[1], label: rowLabel ? rowLabel[1] : href[1], guides });
    }
    groups.push({ layer, label, sections });
  }
  if (groups.length === 0) throw new Error("no NAV_GROUPS group was parsed from layout.tsx");
  return groups;
}

/** The screenshot guides, read out of the generated guides module (guides.gen.ts). */
function readGuides() {
  const src = readFileSync(GUIDES_FILE, "utf8");
  const sf = ts.createSourceFile(GUIDES_FILE, src, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
  const out = [];
  const visit = (node) => {
    if (
      ts.isVariableDeclaration(node) &&
      ts.isIdentifier(node.name) &&
      node.name.text === "GUIDES" &&
      node.initializer &&
      ts.isArrayLiteralExpression(node.initializer)
    ) {
      for (const element of node.initializer.elements) {
        if (!ts.isObjectLiteralExpression(element)) continue;
        const record = {};
        for (const prop of element.properties) {
          if (!ts.isPropertyAssignment(prop)) continue;
          const nameNode = prop.name;
          const key =
            ts.isIdentifier(nameNode) || ts.isStringLiteral(nameNode) || ts.isNoSubstitutionTemplateLiteral(nameNode)
              ? nameNode.text
              : "";
          if (!["slug", "title", "summary", "body"].includes(key)) continue;
          if (ts.isStringLiteral(prop.initializer) || ts.isNoSubstitutionTemplateLiteral(prop.initializer)) {
            record[key] = prop.initializer.text;
          }
        }
        if (record.slug && record.title) out.push(record);
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return out;
}

/** Plain text of a guide markdown body: markers stripped, images and links reduced to
 *  their text. The index is for searching, not for rendering. */
function markdownText(md) {
  return md
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/!\[[^\]]*\]\([^)]*\)/g, " ")
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/^#{1,6}\s+/gm, "")
    .replace(/[*_`>|]/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}

/** Plain text of one docs page, read through the TypeScript parser so a comment can never
 *  become prose (the same reason docsProseConvention.test.ts parses rather than greps). */
const PROSE_PROPS = new Set(["body", "purpose", "summary", "description", "q", "a", "title", "label", "text"]);

function flattenJsx(node) {
  let out = "";
  const walk = (n) => {
    if (ts.isJsxText(n)) {
      out += ` ${n.text} `;
      return;
    }
    if (ts.isStringLiteral(n) || ts.isNoSubstitutionTemplateLiteral(n)) {
      out += ` ${n.text} `;
      return;
    }
    if (ts.isJsxExpression(n) && n.expression) {
      if (ts.isStringLiteral(n.expression) || ts.isNoSubstitutionTemplateLiteral(n.expression)) {
        out += ` ${n.expression.text} `;
        return;
      }
      return; // `{s.title}` is resolved at runtime; the DOM-level ToC reads it live.
    }
    ts.forEachChild(n, walk);
  };
  walk(node);
  return out.replace(/\s+/g, " ").trim();
}

function readPage(file, route) {
  const src = readFileSync(file, "utf8");
  const sf = ts.createSourceFile(file, src, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const texts = [];
  const headings = [];
  const visit = (node) => {
    if (ts.isPropertyAssignment(node) && ts.isIdentifier(node.name) && PROSE_PROPS.has(node.name.text)) {
      const init = node.initializer;
      if (ts.isStringLiteral(init) || ts.isNoSubstitutionTemplateLiteral(init)) {
        if (init.text.trim().length > 1) texts.push(init.text);
      } else if (ts.isTemplateExpression(init)) {
        const staticText = [init.head.text, ...init.templateSpans.map((s) => s.literal.text)].join(" ");
        if (staticText.trim().length > 1) texts.push(staticText);
      }
    }
    if (ts.isJsxText(node)) {
      const text = node.text.replace(/\s+/g, " ").trim();
      if (text.length > 1) texts.push(text);
    }
    if (ts.isJsxElement(node)) {
      const tag = node.openingElement.tagName.getText(sf);
      const level = /^h[1-3]$/.test(tag) ? Number(tag.slice(1)) : 0;
      if (level > 0) {
        const text = flattenJsx(node);
        if (text) headings.push({ level, text });
        if (text) texts.push(text);
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);

  const description = (() => {
    const block = src.match(/docsMetadata\(\{([\s\S]*?)\}\);/);
    if (!block) return "";
    const m = block[1].match(/description:\s*"((?:[^"\\]|\\.)*)"/);
    if (!m) return "";
    try {
      return JSON.parse(`"${m[1]}"`);
    } catch {
      return m[1];
    }
  })();

  // De-duplicate identical strings (a page often renders the same heading and body line),
  // then cap the text so one page cannot dominate the shared index payload.
  const seen = new Set();
  const body = texts
    .map((t) => t.replace(/\s+/g, " ").trim())
    .filter((t) => {
      if (!t || seen.has(t)) return false;
      seen.add(t);
      // Class-name-ish and path-ish strings are attributes, never prose.
      if (/^(?:[a-z-]+:)?(?:flex|grid|block|inline|hidden|text-|bg-|border|rounded|shadow|p[xy]?-|m[xy]?-|w-|h-|min-|max-|gap-|space-|font-|leading-|tracking-|items-|justify-|overflow|sticky|absolute|relative|uppercase|underline|hover:|sm:|md:|lg:|xl:)/.test(t)) return false;
      if (/^[./@\w-]+\.(?:tsx?|mjs|css|png)$/.test(t)) return false;
      return true;
    })
    .join(" ")
    .slice(0, 2400);

  return { description, headings, body };
}

/** sha256 of a page file's bytes, truncated to 16 hex chars. This is what "the page did not
 *  change" means to the freshness check: `updated` is pinned to it, not to a commit. */
function pageDigest(file) {
  return createHash("sha256").update(readFileSync(file)).digest("hex").slice(0, 16);
}

/** The last commit date of a file, or "" when git is unavailable (the image build has no
 *  repository and no git binary). An empty value keeps whatever the committed generated
 *  file already has, so a container build does not rewrite history it cannot see. `cwd` is
 *  where `git log` runs — the web/ directory in production, a throwaway fixture in the
 *  tests — and `gitBin` is the executable, so a test can point it at a stand-in instead of
 *  requiring a real `git` on PATH. */
function lastUpdated(file, cwd = WEB_DIR, gitBin = "git") {
  try {
    return execFileSync(gitBin, ["log", "-1", "--format=%cs", "--", file], {
      cwd,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    }).trim();
  } catch {
    return "";
  }
}

/** The registry as committed: path → { updated, digest }. The digest is the page bytes the
 *  committed `updated` describes; see the header note on squash landings. */
function readPreviousEntries(outNav = OUT_NAV) {
  if (!existsSync(outNav)) return new Map();
  const src = readFileSync(outNav, "utf8");
  const out = new Map();
  // One entry object per `"path":` — the next `"updated":` and `"digest":` after it belong
  // to it (the generator emits them in that order, before `guides`).
  const parts = src.split(/"path":\s*"/).slice(1);
  for (const part of parts) {
    const path = part.slice(0, part.indexOf('"'));
    const updated = part.match(/"updated":\s*"([^"]*)"/);
    const digest = part.match(/"digest":\s*"([^"]*)"/);
    if (path) out.set(path, { updated: updated ? updated[1] : "", digest: digest ? digest[1] : "" });
  }
  return out;
}

/**
 * Build the registry entries for a docs tree. Exported so the guard can drive it against a
 * TEMPORARY tree — that is how the "a new page is indexed without an index edit" acceptance
 * is tested (see src/lib/docsNav.test.ts) instead of asserted in a comment.
 */
export function buildRegistry({
  docsDir = DOCS_DIR,
  layoutSource = readFileSync(LAYOUT_FILE, "utf8"),
  guides = readGuides(),
  previousEntries = readPreviousEntries(),
  gitCwd = WEB_DIR,
  gitBin = "git",
} = {}) {
  const problems = [];
  const warnings = [];
  const nav = readNav(layoutSource);
  const files = pageFiles(docsDir);
  const known = new Map(files.map((file) => [routeOf(file, docsDir), file]));

  // ── the authored nav, in order ──────────────────────────────────────────────
  const navRows = [];
  for (const group of nav) {
    for (const row of group.sections) {
      if (!known.has(row.href)) {
        problems.push(
          `NAV_GROUPS lists ${row.href} but ${relative(WEB_DIR, join(docsDir, `${row.href.slice("/docs".length)}/page.tsx`))} does not exist`,
        );
        continue;
      }
      if (navRows.some((r) => r.path === row.href)) {
        problems.push(`NAV_GROUPS lists ${row.href} twice — the sidebar cannot own one route in two places`);
        continue;
      }
      navRows.push({
        path: row.href,
        label: row.label,
        layer: group.layer,
        section: group.label,
        kind: "product",
        guides: row.guides,
        file: known.get(row.href),
      });
    }
  }

  // ── guides, inserted where the nav puts the screenshot-guides index ──────────
  const guideIndexAt = navRows.findIndex((r) => r.path === "/docs/guides");
  const guideRows = [];
  for (const guide of guides) {
    const path = `/docs/guides/${guide.slug}`;
    const file = known.get(path);
    if (!file) {
      problems.push(`GUIDES has ${guide.slug} but ${path}/page.tsx does not exist`);
      continue;
    }
    guideRows.push({
      path,
      label: guide.title,
      layer: "Learn",
      section: "Screenshot guides",
      kind: "guide",
      guides: [],
      file,
      summary: guide.summary,
      body: guide.body,
    });
  }
  const ordered = guideIndexAt === -1 ? [...navRows, ...guideRows] : [...navRows.slice(0, guideIndexAt + 1), ...guideRows, ...navRows.slice(guideIndexAt + 1)];

  // ── every other page on disk is still indexed (a new page needs no index edit) ──
  const described = new Set(ordered.map((r) => r.path));
  const extra = [...known.keys()].filter((route) => !described.has(route));
  for (const route of extra) {
    const file = known.get(route);
    warnings.push(`${route} exists on disk but is not in NAV_GROUPS — indexed under "More"; add it to the sidebar`);
    ordered.push({
      path: route,
      label: route === "/docs" ? "Docs" : route.split("/").slice(-1)[0].replace(/-/g, " "),
      layer: "Product docs",
      section: "More",
      kind: "product",
      guides: [],
      file,
    });
  }

  if (ordered.length !== files.length) {
    problems.push(`the registry covers ${ordered.length} routes and the tree has ${files.length} — they disagree`);
  }

  // ── build both outputs ──────────────────────────────────────────────────────
  const entries = ordered.map((row, order) => {
    const page = row.kind === "guide"
      ? {
          description: row.summary,
          headings: [...row.body.matchAll(/^(#{2,3})\s+(.+)$/gm)].map((m) => ({ level: m[1].length, text: m[2].trim() })),
          body: markdownText(row.body),
        }
      : readPage(row.file, row.path);
    const digest = pageDigest(row.file);
    const previous = previousEntries.get(row.path);
    // Landing-stable by construction: while the page BYTES are the ones the committed
    // registry describes, its `updated` stands — a squash landing re-dates every file in the
    // PR diff, and must not re-date a page nobody edited. Only a real page edit (a new
    // digest) recomputes the date from git, and the new digest is what makes `--check` fail
    // until the registry is regenerated.
    const unchanged = Boolean(previous?.digest) && previous.digest === digest;
    let updated = "";
    if (unchanged && previous.updated) {
      updated = previous.updated;
    } else {
      updated = lastUpdated(relative(gitCwd, row.file), gitCwd, gitBin) || previous?.updated || "";
    }
    return {
      path: row.path,
      label: row.label,
      layer: row.layer,
      section: row.section,
      order,
      kind: row.kind,
      updated,
      digest,
      guides: row.guides,
      description: page.description,
      headings: page.headings,
      text: page.body,
    };
  });

  return { entries, problems, warnings };
}

/**
 * The generated `docsNav.gen.ts` text for a set of entries. Exported so the guard can render a
 * candidate registry and byte-compare it the way `--check` does — the squash-landing test in
 * src/lib/docsNav.test.ts does exactly that against a throwaway git repository.
 */
export function renderNavSource(entries) {
  return [
    "// GENERATED FILE — do not edit. Run `npm run docs:nav` (web/scripts/docsNav.mjs).",
    "//",
    "// The one registry the /docs chrome reads: breadcrumb, prev/next, related guides and",
    "// the search index all come from here. The authored source is NAV_GROUPS in",
    "// src/app/docs/layout.tsx (which docsIndex.test.ts reads as source text) plus the",
    "// page files themselves; docsNav.test.ts fails when this file drifts from them.",
    "",
    'export type DocsNavLayer = "Learn" | "Product docs" | "Reference";',
    "",
    'export type DocsNavKind = "product" | "guide";',
    "",
    "export type DocsNavEntry = {",
    "  /** The page's route, e.g. `/docs/shipping`. */",
    "  path: string;",
    "  /** Its sidebar label (the guide's own title for a guide page). */",
    "  label: string;",
    "  layer: DocsNavLayer;",
    "  section: string;",
    "  /** Position in the reading order — the sidebar order, with the guides after their index. */",
    "  order: number;",
    "  kind: DocsNavKind;",
    "  /** Last edit date (YYYY-MM-DD) of the page bytes `digest` names, or the value the",
    "   *  committed file carried when git was absent. A landing commit never changes it. */",
    "  updated: string;",
    "  /** sha256 (first 16 hex chars) of the page file these fields were generated from: the",
    "   *  freshness check pins `updated` to it, so a squash landing cannot re-date a page. */",
    "  digest: string;",
    "  /** Related screenshot guides, for a product page. */",
    "  guides: string[];",
    "};",
    "",
    `export const DOCS_NAV: DocsNavEntry[] = ${JSON.stringify(
      entries.map((e) => ({
        path: e.path,
        label: e.label,
        layer: e.layer,
        section: e.section,
        order: e.order,
        kind: e.kind,
        updated: e.updated,
        digest: e.digest,
        guides: e.guides,
      })),
      null,
      2,
    )};`,

    "",
  ].join("\n");
}

function run() {
  const { entries, problems, warnings } = buildRegistry();

  // ── build both outputs ──────────────────────────────────────────────────────
  const navSource = renderNavSource(entries);

  const searchIndex = {
    generatedBy: "web/scripts/docsNav.mjs — run `npm run docs:nav`",
    docs: entries.map((e) => ({
      path: e.path,
      title: e.label,
      description: e.description,
      layer: e.layer,
      section: e.section,
      order: e.order,
      headings: e.headings.map((h) => h.text),
      text: e.text,
    })),
  };
  const searchSource = `${JSON.stringify(searchIndex, null, 1)}\n`;

  if (!CHECK) {
    writeFileSync(OUT_NAV, navSource);
    writeFileSync(OUT_SEARCH, searchSource);
  }

  for (const warning of warnings) console.warn(`warning: ${warning}`);
  if (problems.length > 0) {
    for (const problem of problems) console.error(`error: ${problem}`);
    process.exit(1);
  }

  if (CHECK) {
    const stale = [];
    if (!existsSync(OUT_NAV) || readFileSync(OUT_NAV, "utf8") !== navSource) stale.push(relative(WEB_DIR, OUT_NAV));
    if (!existsSync(OUT_SEARCH) || readFileSync(OUT_SEARCH, "utf8") !== searchSource) stale.push(relative(WEB_DIR, OUT_SEARCH));
    if (stale.length > 0) {
      console.error(`the docs registry is out of date: ${stale.join(", ")} — run \`npm run docs:nav\``);
      process.exit(1);
    }
  }

  console.log(
    `docs: ${entries.length} pages indexed (${entries.filter((e) => e.kind === "guide").length} guides), ` +
      `${entries.length} registry entries, search payload ${(searchSource.length / 1024).toFixed(1)} KiB`,
  );
}

const invokedDirectly =
  process.argv[1] !== undefined && resolve(process.argv[1]) === fileURLToPath(import.meta.url);

if (invokedDirectly) run();
