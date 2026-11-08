// /docs/guides: the guides that exist in the repo must be RENDERED, every screenshot must
// reach the browser, and the pages and the guide list must agree in both directions.
// (card t_7b9f6d18)
//
// WHY THIS EXISTS
//   `docs/guides/payment-fees` (6 screenshots, PR #1598) and `docs/guides/donations` (8,
//   PR #1621) were complete guides that NOBODY could see: `docs/` is not served by Next, there
//   was no route under `app/docs` that named them, and `web/public` held none of the PNGs. The
//   landing page said "coming soon" instead — honestly, given no route existed, but the guide
//   was already written. The routes (app/docs/guides/…) and the generator
//   (web/scripts/guides.mjs → src/lib/guides.gen.ts + public/guides/) are the fix; this guard is
//   what keeps the three halves from drifting apart afterwards.
//
// WHAT IT PINS, in both directions
//   1. LIST → PAGE. Every guide in GUIDES has a page file under app/docs/guides/<slug>/, so a
//      guide cannot be listed here and 404 when a reader clicks it.
//   2. PAGE → LIST. Every guide page under app/docs/guides/ is a guide in GUIDES, so a page
//      cannot outlive the guide it renders (guideBySlug throws at build time, but this names the
//      file instead of failing inside next build).
//   3. SCREENSHOTS REACH THE BROWSER. Every shot in the manifest exists in
//      web/public/guides/<slug>/, its PNG header matches the width/height the manifest claims,
//      and the rendered HTML emits ONE <img> per shot pointing at /guides/<slug>/<file>. That is
//      the acceptance criterion "the pages display all 6 and 8 images", checked on the real
//      renderer output rather than on the README text.
//   4. NOTHING LEAKS AS MARKDOWN. The rendered visible text carries no `## `, `![` or pipe
//      table — a marker left in the output means the docs-mode block parser lost a shape, which
//      is the defect this card is about (the guide would look like source, not a guide).
//   5. SOURCE → GENERATED (host only). docs/guides/<slug>/README.md + its PNGs are the source
//      of truth; the generated module and the copied assets must match them byte for byte. The
//      image build does not carry the repo-root docs/ tree (web/Dockerfile copies web/, api/
//      and MCP-PARITY.md), so this leg skips itself there with imageBuildSkipReasonAt — the same
//      mechanism src/lib/a11yScanProse.test.ts uses, and web/src/lib/buildContext.test.ts
//      records that exception.
//
// WHAT IT CANNOT SEE (stated honestly)
//   * It does not judge whether a screenshot's red box marks the right control — that was
//     verified with vision when each guide was captured, and it is a review question now.
//   * It reads the rendered tree, not a browser: layout, contrast and the a11y of the pages are
//     the a11y gate's job (the three routes are listed in web/e2e/a11y.spec.ts).
//
// Run with: npm run test:unit

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { Markdown } from "../components/Markdown.ts";
import { GUIDES } from "./guides.gen.ts";

// Derived from this file's own location (web/src/lib) rather than from `new URL("...")`
// literals: those literals are read targets to src/lib/buildContext.test.ts, and the only one
// that belongs to that rule is the docs/guides read below (declared HOST_ONLY there).
const THIS_FILE = fileURLToPath(import.meta.url);
const WEB_DIR = resolve(dirname(THIS_FILE), "..", "..");
const REPO_ROOT = resolve(WEB_DIR, "..");
const GUIDE_PAGES = join(WEB_DIR, "src", "app", "docs", "guides");
const PUBLIC_GUIDES = join(WEB_DIR, "public", "guides");
// The SOURCE guides: the ONE host-only read in this file. It only succeeds in a checkout —
// see leg 5 and the header, and the HOST_ONLY entry in src/lib/buildContext.test.ts.
const DOCS_GUIDES = fileURLToPath(new URL("../../../docs/guides", import.meta.url));

/**
 * The web image build has no repository and no repo-root docs/ tree: web/Dockerfile copies web/,
 * api/ and MCP-PARITY.md, and the root .dockerignore drops docs/. A real checkout always has at
 * least one checkout-only root entry, so an ABSENT docs/guides there still fails below.
 * (Same mechanism as src/lib/a11yScanProse.test.ts; buildContext.test.ts pins that it is here.)
 */
const CHECKOUT_ONLY_ROOT_ENTRIES = [".gitignore", "Makefile", "AGENTS.md"];

function imageBuildSkipReasonAt(root: string): string | false {
  if (existsSync(join(root, "docs", "guides"))) return false;
  const rootEntries = CHECKOUT_ONLY_ROOT_ENTRIES.filter((e) => existsSync(join(root, e)));
  if (existsSync(join(root, ".git")) || rootEntries.length > 0) return false; // a real checkout
  return (
    `the web image build has no repository and no repo-root docs/ tree: ${root} has no .git and ` +
    `no checkout-only root entry (${CHECKOUT_ONLY_ROOT_ENTRIES.join(", ")}), and web/Dockerfile ` +
    `does not copy docs/ — so this leg's subject is absent by construction, not by regression`
  );
}

function skipInImageBuild(t: { skip: (reason?: string) => void }): boolean {
  const reason = imageBuildSkipReasonAt(REPO_ROOT);
  if (reason === false) return false;
  t.skip(reason);
  return true;
}

/** Intrinsic size from a PNG's IHDR chunk; the same read the generator does. */
function pngSize(file: string): { width: number; height: number } {
  const buf = readFileSync(file);
  assert.equal(buf.toString("ascii", 1, 4), "PNG", `${file} is not a PNG`);
  assert.equal(buf.toString("ascii", 12, 16), "IHDR", `${file} has no IHDR chunk`);
  return { width: buf.readUInt32BE(16), height: buf.readUInt32BE(20) };
}

/** The guide pages that exist on disk: every directory under app/docs/guides with a page.tsx. */
function pagesOnDisk(): string[] {
  return readdirSync(GUIDE_PAGES, { withFileTypes: true })
    .filter((e) => e.isDirectory() && existsSync(join(GUIDE_PAGES, e.name, "page.tsx")))
    .map((e) => e.name)
    .sort();
}

/** The visible text of a rendered tree, with every tag stripped. */
const visible = (html: string) =>
  html
    .replace(/<[^>]+>/g, "")
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#x27;/g, "'");

const renderGuide = (slug: string) => {
  const guide = GUIDES.find((g) => g.slug === slug);
  assert.ok(guide, `no guide ${JSON.stringify(slug)} in src/lib/guides.gen.ts`);
  const shots = new Map(guide.shots.map((s) => [s.file, s]));
  return renderToStaticMarkup(
    createElement(Markdown, {
      text: guide.body,
      mode: "docs",
      resolveImage: (file: string) => {
        const shot = shots.get(file);
        return shot ? { src: `/guides/${guide.slug}/${shot.file}`, width: shot.width, height: shot.height } : null;
      },
    }),
  );
};

test("the guard is looking at real guides, not an empty list", () => {
  assert.ok(GUIDES.length >= 2, `GUIDES carries ${GUIDES.length} guide(s) — expected the two that exist`);
  for (const g of GUIDES) {
    assert.ok(g.slug !== "" && /^[a-z0-9][a-z0-9-]*$/.test(g.slug), `bad slug ${JSON.stringify(g.slug)}`);
    assert.ok(g.title.trim() !== "", `${g.slug} has no title`);
    assert.ok(g.body.trim() !== "", `${g.slug} has an empty body`);
    assert.ok(g.shots.length > 0, `${g.slug} lists no screenshots`);
  }
  assert.deepEqual(
    GUIDES.map((g) => g.slug),
    [...new Set(GUIDES.map((g) => g.slug))].sort(),
    "GUIDES is not sorted or carries a duplicate slug",
  );
});

test("every guide has a page, and every guide page has a guide", () => {
  const listed = GUIDES.map((g) => g.slug).sort();
  assert.deepEqual(
    pagesOnDisk(),
    listed,
    "the guide pages on disk and the guides in src/lib/guides.gen.ts disagree. A guide with no " +
      "page is a link a reader cannot follow; a page with no guide throws in guideBySlug. Add the " +
      "page (src/app/docs/guides/<slug>/page.tsx), or re-run `npm run guides:sync`.",
  );
  for (const slug of listed) {
    const page = readFileSync(join(GUIDE_PAGES, slug, "page.tsx"), "utf8");
    assert.match(
      page,
      new RegExp(`path: "/docs/guides/${slug}"`),
      `app/docs/guides/${slug}/page.tsx does not pass its own route to docsMetadata`,
    );
  }
});

test("every screenshot is public, measurable, and one <img> per step in the rendered guide", () => {
  const problems: string[] = [];
  for (const g of GUIDES) {
    const dir = join(PUBLIC_GUIDES, g.slug);
    if (!existsSync(dir)) {
      problems.push(`${g.slug}: web/public/guides/${g.slug}/ does not exist — run npm run guides:sync`);
      continue;
    }
    const onDisk = readdirSync(dir).sort();
    assert.deepEqual(
      onDisk,
      [...g.shots.map((s) => s.file)].sort(),
      `public/guides/${g.slug} and the ${g.slug} screenshot manifest disagree`,
    );
    for (const shot of g.shots) {
      const file = join(dir, shot.file);
      if (!existsSync(file)) {
        problems.push(`${g.slug}/${shot.file}: missing from public/guides`);
        continue;
      }
      const size = pngSize(file);
      if (size.width !== shot.width || size.height !== shot.height) {
        problems.push(
          `${g.slug}/${shot.file}: the manifest says ${shot.width}x${shot.height}, the PNG is ` +
            `${size.width}x${size.height} — re-run npm run guides:sync`,
        );
      }
    }

    // The acceptance criterion, read off the REAL renderer output: one <img> per screenshot,
    // every src under this guide's public folder, and an alt on each (axe image-alt).
    const html = renderGuide(g.slug);
    const imgs = [...html.matchAll(/<img\b[^>]*>/g)].map((m) => m[0]);
    if (imgs.length !== g.shots.length) {
      problems.push(`${g.slug}: rendered ${imgs.length} <img> for ${g.shots.length} screenshot(s)`);
    }
    for (const img of imgs) {
      const src = /src="([^"]+)"/.exec(img)?.[1] ?? "";
      const alt = /alt="([^"]*)"/.exec(img)?.[1] ?? "";
      if (!src.startsWith(`/guides/${g.slug}/`)) problems.push(`${g.slug}: an <img> points at ${src}`);
      if (alt.trim() === "") problems.push(`${g.slug}: an <img> (${src}) has an empty alt`);
      if (!/width="\d+"/.test(img) || !/height="\d+"/.test(img)) {
        problems.push(`${g.slug}: ${src} has no width/height — the page would shift as it loads`);
      }
    }
  }
  assert.deepEqual(problems, [], `these screenshots do not reach the page:\n  ${problems.join("\n  ")}`);
});

test("no markdown marker survives into the rendered guide", () => {
  // The guide is the fix for a page that showed "coming soon" while the text existed; a page
  // that renders `## 3. Add the other amounts` or a row of pipes instead of a heading/table is
  // the same failure with a different symptom.
  for (const g of GUIDES) {
    const text = visible(renderGuide(g.slug))
      .split("\n")
      .map((l) => l.trim())
      .join("\n");
    assert.doesNotMatch(text, /^#{1,6}\s/m, `${g.slug}: a "## " heading marker reached the reader`);
    assert.doesNotMatch(text, /!\[[^\]]*\]\(/, `${g.slug}: raw image syntax reached the reader`);
    assert.doesNotMatch(text, /^\|.*\|$/m, `${g.slug}: a pipe table row reached the reader`);
    assert.doesNotMatch(text, /\*\*/, `${g.slug}: a ** marker reached the reader`);
  }
});

test("the screenshot count is the one the card promises (6 and 8)", () => {
  // Pinned per guide on purpose: this is the acceptance criterion ("/docs/guides/payment-fees
  // and /docs/guides/donations display all their images — 6 and 8"). A guide that loses a step
  // in an edit has to say so here, not ship a page with a missing screenshot.
  const counts = Object.fromEntries(GUIDES.map((g) => [g.slug, g.shots.length]));
  assert.equal(counts["payment-fees"], 6, `payment-fees has ${counts["payment-fees"]} screenshots, not 6`);
  assert.equal(counts["donations"], 8, `donations has ${counts["donations"]} screenshots, not 8`);
});

test("the generated content matches docs/guides byte for byte (checkout only)", (t) => {
  if (skipInImageBuild(t)) return;
  const problems: string[] = [];
  const slugs = readdirSync(DOCS_GUIDES, { withFileTypes: true })
    .filter((e) => e.isDirectory())
    .map((e) => e.name)
    .sort();
  assert.deepEqual(
    GUIDES.map((g) => g.slug),
    slugs,
    "docs/guides and src/lib/guides.gen.ts carry different guides — run `npm run guides:sync`",
  );
  for (const g of GUIDES) {
    const dir = join(DOCS_GUIDES, g.slug);
    const readme = readFileSync(join(dir, "README.md"), "utf8").replace(/\r\n?/g, "\n");
    // The generated body is the README minus its title line (the page renders that as its h1).
    const lines = readme.split("\n");
    const titleAt = lines.findIndex((l) => /^#\s+\S/.test(l));
    const title = titleAt === -1 ? "" : lines[titleAt].replace(/^#\s+/, "").trim();
    lines.splice(titleAt, 1);
    const body = lines.join("\n").replace(/^\n+/, "").replace(/\s+$/, "") + "\n";
    if (title !== g.title) problems.push(`${g.slug}: title is "${g.title}", the README says "${title}"`);
    if (body !== g.body) problems.push(`${g.slug}: the generated body differs from docs/guides/${g.slug}/README.md`);

    // The manifest must be exactly the PNGs the README references, in document order.
    const refs = [...readme.matchAll(/!\[[^\]]*\]\(([^)\s]+)\)/g)].map((m) => m[1]);
    const unique = [...new Set(refs)];
    if (JSON.stringify(unique) !== JSON.stringify(g.shots.map((s) => s.file))) {
      problems.push(`${g.slug}: the screenshot manifest is not the README's image list in order`);
    }
    for (const shot of g.shots) {
      const src = join(dir, shot.file);
      if (!existsSync(src)) {
        problems.push(`${g.slug}/${shot.file}: no such file in docs/guides`);
        continue;
      }
      if (!readFileSync(src).equals(readFileSync(join(PUBLIC_GUIDES, g.slug, shot.file)))) {
        problems.push(`${g.slug}/${shot.file}: public/guides copy differs from docs/guides — run npm run guides:sync`);
      }
    }
  }
  assert.deepEqual(problems, [], `docs/guides and the generated output drifted:\n  ${problems.join("\n  ")}`);
});
