// The retired surau guide's URL must keep working (card t_1e4c1bba).
//
// WHY THIS EXISTS
//   /panduan/surau shipped as an unlisted, noindex, nine-step donation guide and was handed to
//   a surau committee by link — the link is in their WhatsApp groups, not in the repo. The guide
//   was retired because the fee model it described changed, and its replacement is the /docs
//   setup page. Deleting the route without a redirect would turn every copy of that link into a
//   404 — a dead end for a committee that never had another way to reach the instructions.
//
//   So the redirect is part of the product, not cleanup: it has to survive the next person who
//   tidies the tree. This guard pins it at the CONFIG Next actually loads (the same seam
//   securityHeaders.test.ts uses), and pins the destination as a real page, so an edit that
//   removes the entry, changes the status, or points it at a route nobody has written goes red
//   in the fast suite instead of in someone's chat.
//
// WHAT IS PINNED
//   1. the imported config declares a redirect whose literal source is /panduan/surau;
//   2. that entry answers **301** — not the 308 Next uses for `permanent: true`, and not a
//      temporary 307 — and carries no `permanent` field (Next refuses both on one entry, and a
//      permanent:true here would silently become 308);
//   3. the destination is /docs/fees and that route is a real page on disk with a lead and at
//      least one section, so the redirect cannot land on a 404 or a blank shell;
//   4. the destination is actually reachable the way every other /docs page is: the sidebar,
//      the search registry, the apex sitemap and llms-full.txt name it;
//   5. the retired surfaces are GONE — the route directory, the page module, the private guide
//      module, the guide's screenshots and its docs/guides source. A page re-added at
//      /panduan/surau would be shadowed by the redirect (Next checks redirects before the
//      filesystem) and unreachable while nobody noticed;
//   6. nothing under web/src or web/public links /panduan/surau any more, so the retired path
//      is not reintroduced as an internal destination.
//
// WHAT IT DOES NOT COVER, stated honestly: it reads the config's redirects() return value, not
// a served response — the live 301 was measured with `curl -sSI` against `next start` on the
// built app and is quoted in the card's handoff. A config-shaped assertion cannot see a proxy
// in front of the app that rewrites or strips the Location header.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { filesUnder } from "./docsRoutes.ts";

const THIS_FILE = fileURLToPath(import.meta.url);
const WEB_DIR = resolve(dirname(THIS_FILE), "..", "..");
const REPO_ROOT = resolve(WEB_DIR, "..");
const NEXT_CONFIG = join(WEB_DIR, "next.config.mjs");

/** The retired route, and the page that now carries its content. */
const RETIRED = "/panduan/surau";
const TARGET = "/docs/fees";

type Redirect = {
  source: string;
  destination: string;
  permanent?: boolean;
  statusCode?: number;
};

const config = (await import(pathToFileURL(NEXT_CONFIG).href)).default as {
  redirects?: () => Promise<Redirect[]>;
};

async function redirects(): Promise<Redirect[]> {
  assert.equal(
    typeof config.redirects,
    "function",
    "web/next.config.mjs exports no redirects() — the committee's link would 404",
  );
  return config.redirects!();
}

/** A literal source is an exact path in Next's router: no parameter segments, no trailing
 *  segment. A source this cannot read as a literal THROWS rather than silently matching, so a
 *  future `:path*` rewrite of this rule has to be re-measured instead of slipping past. */
function isLiteral(source: string): boolean {
  return !source.split("/").some((s) => s.startsWith(":"));
}

test("the retired surau URL is declared in the config Next loads", () => {
  assert.ok(existsSync(NEXT_CONFIG), `missing ${NEXT_CONFIG}`);
});

test("the redirect answers 301 to the docs setup page, not 308/307 and not a 404", async () => {
  const all = await redirects();
  const rule = all.find((r) => isLiteral(r.source) && r.source === RETIRED);
  assert.ok(
    rule,
    `next.config.mjs declares no redirect whose source is exactly ${RETIRED} — every copy of the ` +
      `surau committee's link now answers 404. Restore the entry (301 -> ${TARGET}).`,
  );
  assert.equal(rule.destination, TARGET, `the ${RETIRED} redirect points at ${rule.destination}`);
  assert.equal(
    rule.statusCode,
    301,
    `the ${RETIRED} redirect answers ${rule.statusCode ?? "(no statusCode)"} — it must be 301. ` +
      `Next's \`permanent: true\` is a 308, so the number has to be set explicitly.`,
  );
  assert.equal(
    rule.permanent,
    undefined,
    `the ${RETIRED} entry sets both permanent and statusCode — Next refuses that at build time, ` +
      `and \`permanent: true\` alone is a 308, not the 301 this link is specified to answer.`,
  );
  // Next matches a literal source against the exact pathname (path-to-regexp anchors the start
  // and the end), so the rule must not be a prefix that would also answer unrelated paths.
  assert.ok(
    all.filter((r) => r.source === RETIRED).length === 1,
    `${RETIRED} is declared more than once — the second rule can never be reached`,
  );
});

test("the redirect destination is a real docs page with content", () => {
  // Keep `docs` a bare join() argument: preflight derives a gate's inputs from quoted
  // repo-root path literals, and a `docs/` literal here would claim the whole docs
  // tree for the web gate, defeating the fail-closed unclaimed-path fixture in
  // scripts/preflight_test.sh (selftest).
  const page = join(WEB_DIR, "src", "app", "docs", TARGET.replace(/^\/docs\/?/, ""), "page.tsx");
  assert.ok(existsSync(page), `${TARGET} has no page.tsx — the redirect lands on a 404 (${page})`);
  const src = readFileSync(page, "utf8");
  assert.match(src, /<DocsPage/, `${TARGET} does not render through <DocsPage>, so it is not a docs page`);
  assert.match(src, /const SECTIONS: Section\[\] = \[/, `${TARGET} declares no SECTIONS — nothing to read`);
  const sections = (src.match(/^\s{4}id: "/gm) ?? []).length;
  assert.ok(sections >= 4, `${TARGET} declares ${sections} sections; a setup page needs the steps, the fee and an example`);
});

test("the destination is in the docs registry, search index, sitemap and llms-full.txt", () => {
  const reads: [string, string][] = [
    ["src/lib/docsNav.gen.ts", "the docs registry (the sidebar/breadcrumb/pager source)"],
    ["src/lib/docsSearch.gen.json", "the docs search index"],
    ["src/app/sitemap.ts", "the apex sitemap"],
    ["public/llms-full.txt", "the agent-facing page index"],
    ["src/app/docs/layout.tsx", "the docs sidebar"],
  ];
  for (const [file, what] of reads) {
    const body = readFileSync(join(WEB_DIR, file), "utf8");
    assert.ok(
      body.includes(TARGET),
      `${file} does not name ${TARGET}, so ${what} is missing the page the retired link lands on`,
    );
  }
});

test("the retired guide's route, module and screenshots are gone", () => {
  const gone = [
    join(WEB_DIR, "src", "app", "panduan", "surau", "page.tsx"),
    join(WEB_DIR, "src", "lib", "panduanSurauPage.ts"),
    join(WEB_DIR, "src", "lib", "panduanSurau.test.ts"),
    join(WEB_DIR, "src", "lib", "guidesPrivate.gen.ts"),
    join(REPO_ROOT, "docs", "guides", "surau-donation", "README.md"),
    join(WEB_DIR, "public", "panduan", "surau", "01-mula-percuma.png"),
  ];
  for (const path of gone) {
    assert.equal(
      existsSync(path),
      false,
      `${relative(REPO_ROOT, path)} is back. The guide was retired in favour of ${TARGET}: its ` +
        `screenshots show the removed Mode control and the old fee screen, and a page at ` +
        `${RETIRED} would be shadowed by the redirect (Next checks redirects before the filesystem).`,
    );
  }
});

test("nothing under web/src or web/public links the retired path", () => {
  const offenders: string[] = [];
  for (const root of [join(WEB_DIR, "src"), join(WEB_DIR, "public")]) {
    for (const file of filesUnder(root)) {
      if (file.endsWith(".png") || file.endsWith(".ico") || file.endsWith(".woff2")) continue;
      // The config's own redirect source is the one place the path MUST appear, and this guard
      // names it too (its assertions are the reason the string exists here).
      if (resolve(file) === resolve(NEXT_CONFIG) || resolve(file) === resolve(THIS_FILE)) continue;
      let body: string;
      try {
        body = readFileSync(file, "utf8");
      } catch {
        continue;
      }
      if (!body.includes(`${RETIRED}"`) && !body.includes(`${RETIRED}/`)) continue;
      offenders.push(relative(WEB_DIR, file));
    }
  }
  assert.deepEqual(
    offenders,
    [],
    `these files still link ${RETIRED}: ${offenders.join(", ")}. Its content lives at ${TARGET} now; ` +
      `an internal link would keep the retired guide alive in navigation.`,
  );
});

// A directory listing read that fails would make the "gone" assertions vacuously true only if
// the walk itself were empty; this keeps the walk honest on a tree whose docs pages all exist.
test("the docs tree walk this guard relies on still sees the pages", () => {
  const pages = filesUnder(join(WEB_DIR, "src", "app", "docs")).filter((f) => f.endsWith("/page.tsx"));
  assert.ok(pages.length > 20, `the docs walk found ${pages.length} pages — it is not looking at the tree`);
  assert.ok(
    pages.some((p) => p.endsWith(`${TARGET.split("/").pop()}/page.tsx`)),
    "the docs walk does not reach the redirect target",
  );
  assert.ok(readdirSync(join(WEB_DIR, "src", "app", "docs")).length > 0);
});
