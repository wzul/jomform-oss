// The browser-tab title of every /docs page carries ONE dash (card t_31ee7580).
//
// THE DEFECT THIS GUARDS
//   Every screenshot guide page hand-wrote its <title> as
//   "<subject> — screenshot guide — JomForm Docs", so the tab read TWO em dashes of the
//   same kind — "Collect donations — screenshot guide — JomForm Docs" — while all 22
//   sibling /docs pages carry exactly one ("Activity log — JomForm Docs"). Nothing failed:
//   a title is a plain string, it type-checks, the build is green, and no guard looked at
//   it. It was found by reading document.title in a browser.
//
// WHAT IS ASSERTED
//   1. guideTabTitle() reduces both authored README forms — "<subject> — setup guide" and
//      "<subject>: setup guide" — to "<subject> — JomForm Docs", and it refuses a subject
//      that would still need a second dash.
//   2. EVERY route on disk under src/app/docs — including any page added later — declares
//      a title with exactly one spaced em dash: a literal for a hand-written page, or the
//      derived helper for a guide.
//   3. Every guide page DERIVES its title from the guide's own title, so editing the
//      README's `# …` line cannot leave the tab behind with a stale subject (and a new
//      guide page cannot reintroduce the hand-written literal).
//
// WHAT IT CANNOT SEE
//   It reads source, not a served page. That the string really reaches <title> is checked
//   in the browser on the built tree (evidence on the card: document.title for a guide
//   page, a docs page and the guides index).

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { guideTabTitle } from "./docsMetadata.ts";
import { docsRoutesOnDisk } from "./docsRoutes.ts";
import { GUIDES } from "./guides.gen.ts";

const LIB_DIR = dirname(fileURLToPath(import.meta.url)); // <repo>/web/src/lib
const WEB_DIR = resolve(LIB_DIR, "..", ".."); //            <repo>/web
const DOCS_DIR = join(WEB_DIR, "src", "app", "docs");

/** The page file a route came from: "/docs/guides/x" -> app/docs/guides/x/page.tsx. */
function pageFile(route: string): string {
  const segments = route === "/docs" ? [] : route.slice("/docs/".length).split("/");
  return join(DOCS_DIR, ...segments, "page.tsx");
}

/**
 * The `title` argument of the page's docsMetadata() call, as source text. Scoped to the
 * call so a page's own section data (e.g. /docs/sales declares `title: "Sales"` for a
 * section) cannot be mistaken for the page title.
 */
function titleExpression(src: string): string | null {
  const start = src.indexOf("docsMetadata({");
  if (start === -1) return null;
  const end = src.indexOf("\n});", start);
  const block = end === -1 ? src.slice(start) : src.slice(start, end);
  const m = block.match(/^\s*title:\s*(.+?),\s*$/m);
  return m ? m[1].trim() : null;
}

const dashes = (s: string) => s.split(" — ").length - 1;

test("guideTabTitle collapses both README title forms to one dash", () => {
  const cases: [input: string, expected: string][] = [
    ["Collect donations — setup guide", "Collect donations — JomForm Docs"],
    ["Collect donations: setup guide", "Collect donations — JomForm Docs"],
    ["Payment fees — merchant setup guide", "Payment fees — JomForm Docs"],
    ["Design with an AI agent (MCP) — setup guide", "Design with an AI agent (MCP) — JomForm Docs"],
    ["Club membership", "Club membership — JomForm Docs"],
  ];
  for (const [input, expected] of cases) {
    assert.equal(guideTabTitle(input), expected, `guideTabTitle(${JSON.stringify(input)})`);
    assert.equal(dashes(guideTabTitle(input)), 1, `${JSON.stringify(input)} still renders two dashes`);
  }
  assert.ok(
    !guideTabTitle("Collect donations — setup guide").includes("screenshot guide"),
    "the tab kept the descriptor: two nouns and a dash is what the card removes",
  );
});

test("a subject that itself carries an em dash is refused instead of shipping two", () => {
  assert.throws(
    () => guideTabTitle("Yuran bulanan — Kelas Matematik — setup guide"),
    /two of them/,
    "a title with an em dash in the subject must fail at build time, not render as two",
  );
});

test("every guide in the registry gets a one-dash tab title", () => {
  assert.ok(GUIDES.length >= 8, `expected the eight screenshot guides, found ${GUIDES.length}`);
  for (const g of GUIDES) {
    const title = guideTabTitle(g.title);
    assert.match(title, /^.+ — JomForm Docs$/, `${g.slug}: ${JSON.stringify(title)}`);
    assert.equal(dashes(title), 1, `${g.slug}: ${JSON.stringify(title)} carries two dashes`);
    assert.ok(!title.includes("screenshot guide"), `${g.slug} kept the descriptor`);
  }
});

test("every /docs page on disk declares a tab title with one dash", () => {
  const routes = docsRoutesOnDisk(DOCS_DIR);
  // Today's tree carries 30 route files (22 product pages + 8 guides); a smaller set
  // means the walk broke and the loop below would pass while looking at nothing.
  assert.ok(routes.length >= 30, `found only ${routes.length} /docs route(s) under ${DOCS_DIR}`);

  const problems: string[] = [];
  for (const route of routes) {
    const rel = pageFile(route);
    const expr = titleExpression(readFileSync(rel, "utf8"));
    if (expr === null) {
      problems.push(`${route}: no docsMetadata({ title: … }) found — the tab would inherit the section title`);
      continue;
    }

    const isGuide = route.startsWith("/docs/guides/");
    const derived = /^guideTabTitle\(\s*guide\.title\s*\)$/.test(expr);

    if (isGuide && !derived) {
      problems.push(
        `${route}: title is ${JSON.stringify(expr)}, not guideTabTitle(guide.title) — a hand-written ` +
          `guide title goes stale when the README's \`# …\` line changes (and is how the double dash shipped)`,
      );
      continue;
    }
    if (!isGuide && derived) {
      problems.push(`${route}: uses guideTabTitle() but is not a guide route`);
      continue;
    }

    let title: string;
    if (derived) {
      const slug = route.slice("/docs/guides/".length);
      const guide = GUIDES.find((g) => g.slug === slug);
      if (!guide) {
        problems.push(`${route}: no guide named ${JSON.stringify(slug)} in guides.gen.ts`);
        continue;
      }
      title = guideTabTitle(guide.title);
    } else {
      const literal = expr.match(/^"((?:[^"\\]|\\.)*)"$/);
      if (!literal) {
        problems.push(`${route}: title ${JSON.stringify(expr)} is neither a string literal nor guideTabTitle(guide.title)`);
        continue;
      }
      title = JSON.parse(`"${literal[1]}"`);
    }

    if (dashes(title) !== 1) {
      problems.push(`${route}: title ${JSON.stringify(title)} carries ${dashes(title)} dashes — every /docs page carries one`);
    }
  }

  assert.deepEqual(problems, [], problems.join("\n"));
});
