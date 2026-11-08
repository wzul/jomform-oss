// Guard for the /docs pages' Open Graph metadata (card t_e21f6147).
//
// THE DEFECT THIS GUARDS
//   Each /docs page exported its own `title`, `description` and
//   `alternates.canonical` but no `openGraph`, so it inherited the /docs
//   layout's SECTION object: a share of https://jomform.com/docs/customize
//   previewed as "/docs", with the section's og:title, og:description and
//   og:url while its <title> and <meta name="description"> were correct.
//   Next merges metadata shallowly — a nested field such as `openGraph` is
//   inherited whole from the last segment that defines it, and a page that
//   defines it REPLACES the inherited one entirely.
//
// WHAT IS ASSERTED HERE
//   1. The builder publishes the page's own title / description / URL: og:url
//      equals the canonical, and both are the page's own route on the apex.
//   2. It keeps the shared social card (`OG_IMAGE` / `OG_IMAGE_URL`), because
//      the page's `openGraph` object replaces the layout's — a page-local block
//      that forgot `images` would drop the card silently.
//   3. It refuses a path that is not a /docs route, so a copy-paste slip cannot
//      publish an og:url pointing at a page that does not exist.
//   4. EVERY route on disk under src/app/docs builds its metadata through the
//      builder and names its OWN route — including any page added later. That
//      is the leg that would have caught the original defect: the old pages had
//      no `openGraph` at all.
//
// WHAT IT CANNOT SEE
//   It reads the source, not a served page. That the tags really reach
//   `<head>` is checked against a running server (built page, read the DOM).

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { docsMetadata } from "./docsMetadata.ts";
import { docsRoutesOnDisk } from "./docsRoutes.ts";
import { OG_IMAGE, OG_IMAGE_URL } from "./ogImage.ts";
import { SITE_NAME, SITE_URL } from "./siteName.ts";

const LIB_DIR = dirname(fileURLToPath(import.meta.url)); // <repo>/web/src/lib
const WEB_DIR = resolve(LIB_DIR, "..", ".."); //            <repo>/web
const DOCS_DIR = join(WEB_DIR, "src", "app", "docs");
const ORIGIN = "https://jomform.com";

// The emitted fields, read through a flat shape so the assertions stay readable
// (Next's own unions are wider than the four fields this card is about).
type Og = { title?: string; description?: string; url?: string; siteName?: string; type?: string; images?: unknown[] };
type Tw = { card?: string; title?: string; description?: string; images?: unknown[] };

test("og:url and the canonical are the page's own URL, with no trailing slash", () => {
  const md = docsMetadata({
    title: "A page title",
    description: "A page description.",
    path: "/docs/customize",
  });
  const og = md.openGraph as Og;
  const canonical = (md.alternates as { canonical?: string }).canonical;

  assert.equal(md.title, "A page title");
  assert.equal(md.description, "A page description.");
  assert.equal(canonical, `${ORIGIN}/docs/customize`);
  assert.equal(og.url, `${ORIGIN}/docs/customize`);
  assert.equal(
    og.url,
    canonical,
    "og:url and the canonical disagree — the unfurl would advertise a different page than the one indexed",
  );
  assert.equal(SITE_URL, `${ORIGIN}/`, "the apex moved; the URL rule above assumes it");
});

test("og:title / og:description / twitter come from the PAGE, not the section", () => {
  const md = docsMetadata({
    title: "Design → AI agent → MCP customization — JomForm Docs",
    description: "How to design a form with an AI agent.",
    path: "/docs/customize",
  });
  const og = md.openGraph as Og;
  const tw = md.twitter as Tw;

  // The defect was exactly this: <title> said one thing, og:title said
  // "JomForm Docs — knowledge base".
  assert.equal(og.title, md.title);
  assert.equal(og.description, md.description);
  assert.equal(tw.title, md.title);
  assert.equal(tw.description, md.description);
  assert.equal(og.siteName, SITE_NAME);
  assert.equal(og.type, "website");
});

test("the shared card survives the page-level openGraph object", () => {
  const md = docsMetadata({ title: "t", description: "d", path: "/docs" });
  const og = md.openGraph as Og;
  const tw = md.twitter as Tw;

  // A page's openGraph REPLACES the layout's, so the card has to be restated
  // here or it disappears — this is the trap the helper exists to close.
  assert.deepEqual(og.images, [OG_IMAGE]);
  assert.deepEqual(tw.images, [OG_IMAGE_URL]);
  assert.equal(tw.card, "summary_large_image");
  assert.match(OG_IMAGE_URL, /^https:\/\/jomform\.com\/og-default-[0-9a-f]{12}\.png$/);
});

test("a path that is not a /docs route is refused instead of published", () => {
  for (const bad of ["/docs/", "/docsfoo", "/customize", "/docs/customize/", "https://jomform.com/docs/customize", ""]) {
    assert.throws(
      () => docsMetadata({ title: "t", description: "d", path: bad }),
      /not a \/docs route/,
      `accepted ${JSON.stringify(bad)} — that would publish an og:url for a page that does not exist`,
    );
  }
  // The index itself is a route and stays valid.
  assert.doesNotThrow(() => docsMetadata({ title: "t", description: "d", path: "/docs" }));
  // A nested route (the docs walk supports them) is valid too.
  assert.doesNotThrow(() => docsMetadata({ title: "t", description: "d", path: "/docs/seg/sub" }));
});

test("every /docs route on disk builds its own og:url through the builder", () => {
  const routes = docsRoutesOnDisk(DOCS_DIR);
  // Today's tree carries 20 route files; a smaller set means the walk broke
  // and the loop below would pass while looking at nothing.
  assert.ok(routes.length >= 18, `found only ${routes.length} /docs route(s) under ${DOCS_DIR}`);

  const problems: string[] = [];
  for (const route of routes) {
    const relSlug = route.slice("/docs".length); // "" for the index
    const segments = relSlug ? relSlug.slice(1).split("/") : [];
    const file = join(DOCS_DIR, ...segments, "page.tsx");
    const rel = relative(WEB_DIR, file);
    const src = readFileSync(file, "utf8");

    if (!/export const metadata: Metadata = docsMetadata\(\{/.test(src)) {
      problems.push(
        `${rel} does not build its metadata through docsMetadata() — it would inherit the /docs ` +
          `section og:title/og:description/og:url`,
      );
      continue;
    }
    if (!src.includes(`path: "${route}",`)) {
      problems.push(
        `${rel} does not name its own route ${JSON.stringify(route)} — its og:url would point somewhere else`,
      );
    }
    // A second, hand-written object would win or lose silently next to the
    // builder; one source per page.
    if (/\bopenGraph:\s*\{/.test(src)) {
      problems.push(
        `${rel} still declares its own openGraph object — use docsMetadata() so the card and the URL stay in one place`,
      );
    }
  }
  assert.deepEqual(problems, [], problems.join("\n"));
});
