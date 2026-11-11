// /panduan/surau — the PRIVATE surau donation guide.
//
// WHY THIS EXISTS
//   Wan's colleague asked for a hand-held guide for a surau committee that takes donations with
//   JomForm but must NOT absorb CHIP's processing fee. The capability ships (Settings → Payment
//   fees, /docs/payment-fees); the guide is a private page Wan hands out himself, and the whole
//   point of it is that NOTHING ELSE CAN FIND IT. "Private" is therefore not a flag on the page —
//   it is a set of surfaces that must stay empty:
//
//     1. the page answers and carries `robots: noindex, nofollow`;
//     2. it is absent from the apex sitemap (app/sitemap.ts), from the docs registry
//        (docsNav.gen.ts) and its search index (docsSearch.gen.json), and from llms.txt /
//        llms-full.txt;
//     3. no file under web/src links to it — the ONLY exceptions are the route's own directory
//        (which necessarily names its own asset URLs) and this test (which names the path it
//        searches for). A future link from the landing page, the docs, the footer or the
//        dashboard fails here, which is the leak that would make the page findable;
//     4. it is not a public screenshot guide: it is not in GUIDES, so /docs/guides cannot list it;
//     5. its worked example is the ENGINE's example. The prose says RM1,000.00 and RM1,010.10;
//        api/internal/payments/fee_test.go's TestGrossUpWorkedExample pins
//        GrossUp(100000, 0, 100) == (101010, 1010). The page's two figures are asserted below by
//        FORMATTING those sen values, so a guide that drifts from the engine (a different fee, a
//        naive price+percentage total) fails here rather than teaching the committee arithmetic
//        the server does not do.
//
// WHAT IT CANNOT SEE (stated honestly)
//   * It reads the built tree and the generated module, not a served page. The 2xx, the emitted
//     <meta name="robots">, the rendered screenshots and the QR are measured on the running app
//     and recorded with the change that added it.
//   * It does not judge whether a screenshot's red box marks the right control — that was checked
//     with vision when the shots were captured, and it is a review question now.
//
// Run with: npm run test:unit

import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

// The page module is .tsx, which `node --test` cannot import (ERR_UNKNOWN_FILE_EXTENSION: the
// runner strips types from .ts only). Its body + metadata therefore live in a .ts module that
// this guard imports, and app/panduan/surau/page.tsx re-exports them — so the assertions below
// read the REAL metadata object and the REAL article tree the route serves, and the first test
// also pins that page.tsx still re-exports exactly those two things rather than its own copy.
import { PANDUAN_SURAU_METADATA as metadata, PanduanSurauArticle } from "./panduanSurauPage.ts";
import { GUIDES } from "./guides.gen.ts";
import { PRIVATE_GUIDES } from "./guidesPrivate.gen.ts";
const THIS_FILE = fileURLToPath(import.meta.url);
const WEB_DIR = resolve(dirname(THIS_FILE), "..", "..");
const SRC_DIR = join(WEB_DIR, "src");
const PUBLIC_DIR = join(WEB_DIR, "public");
const ROUTE = "/panduan/surau";

const read = (p: string) => readFileSync(p, "utf8");
const visible = (html: string) =>
  html
    .replace(/<[^>]+>/g, " ")
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#x27;/g, "'")
    .replace(/\s+/g, " ");

const guide = PRIVATE_GUIDES.find((g) => g.slug === "surau-donation");
assert.ok(guide, "src/lib/guidesPrivate.gen.ts has no surau-donation guide — run `npm run guides:sync`");

/** The rendered article, exactly as the page builds it (Markdown mode="docs" + its own shots). */
function renderPage(): string {
  return renderToStaticMarkup(createElement(PanduanSurauArticle));
}

/** Money for Malay prose: 101010 sen → "RM1,010.10" (thousands separator, two decimals). */
function rm(sen: number): string {
  const whole = String(Math.trunc(sen / 100));
  const centsIn = String(sen % 100).padStart(2, "0");
  const grouped = whole.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  return `RM${grouped}.${centsIn}`;
}

test("the page renders and is noindex/nofollow", () => {
  const html = renderPage();
  const text = visible(html);

  assert.ok(html.length > 2000, `the page rendered only ${html.length} bytes — it is empty`);
  assert.match(
    html,
    /<h1[^>]*>[^<]*Panduan derma surau/,
    "the page has no <h1> carrying the guide's title — the generated module and the page disagree",
  );
  // The body is Malay and marked as such. The root layout's <html lang="en"> cannot be changed
  // per page, so the article itself carries the language.
  assert.match(html, /lang="ms"/, "the article does not declare lang=\"ms\"");
  assert.match(text, /Caj pemprosesan/, "the page does not mention the processing fee in Malay");

  // The robots directive itself — the whole invisibility story starts here.
  assert.deepEqual(
    metadata.robots,
    { index: false, follow: false },
    "metadata.robots must be exactly { index: false, follow: false } — a page that may be indexed " +
      "is a page the committee's link is not private",
  );
  // …and no canonical, because a canonical advertises the page it names.
  assert.equal(metadata.alternates, undefined, "the private page must not declare a canonical");

  // The ROUTE must serve this object: app/panduan/surau/page.tsx re-exports the tested metadata
  // and article rather than keeping its own copy (which could drift from the guard).
  const route = read(join(SRC_DIR, "app", "panduan", "surau", "page.tsx"));
  assert.match(
    route,
    /export const metadata: Metadata = PANDUAN_SURAU_METADATA;/,
    "app/panduan/surau/page.tsx no longer serves the metadata this guard asserts",
  );
  assert.match(
    route,
    /return PanduanSurauArticle\(\)/,
    "app/panduan/surau/page.tsx no longer renders the article this guard asserts",
  );
});

test("the route is absent from the sitemap, the docs registry, the search index and llms.txt", () => {
  // 1. app/sitemap.ts — parse the APEX_PAGES array the same way sitemapCoverage.test.ts does
  //    (comments stripped, so the file's own prose cannot be mistaken for an entry).
  const sitemap = read(join(SRC_DIR, "app", "sitemap.ts"))
    .replace(/\/\*[\s\S]*?\*\//g, " ")
    .replace(/^[ \t]*\/\/.*$/gm, " ")
    .replace(/[ \t]\/\/.*$/gm, " ");
  assert.ok(sitemap.includes("const APEX_PAGES"), "app/sitemap.ts no longer declares APEX_PAGES");
  assert.ok(
    !sitemap.includes(ROUTE),
    `app/sitemap.ts lists ${ROUTE} — a private page in the apex sitemap is advertised to every ` +
      `crawler, which is the one thing this page must not be`,
  );
  assert.ok(!/\/panduan\//.test(sitemap), "app/sitemap.ts lists a /panduan/ route");

  // 2. The docs registry + search index (#1657) — what builds /docs nav, prev/next and search.
  const nav = read(join(SRC_DIR, "lib", "docsNav.gen.ts"));
  assert.ok(!nav.includes(ROUTE), `docsNav.gen.ts carries ${ROUTE} — it would appear in the docs sidebar`);
  const search = read(join(SRC_DIR, "lib", "docsSearch.gen.json"));
  assert.ok(!search.includes("panduan"), "docsSearch.gen.json indexes the private page");

  // 3. llms.txt / llms-full.txt — the AI-agent surface.
  for (const f of ["llms.txt", "llms-full.txt"]) {
    const p = join(PUBLIC_DIR, f);
    assert.ok(existsSync(p), `web/public/${f} is missing`);
    assert.ok(!read(p).includes("panduan"), `web/public/${f} describes the private page`);
  }

  // 4. The public screenshot-guides list — /docs/guides maps GUIDES, so membership is the leak.
  assert.ok(
    !GUIDES.some((g) => g.slug === guide.slug),
    `${guide.slug} is in GUIDES, so /docs/guides lists the private guide`,
  );
  assert.ok(
    !existsSync(join(SRC_DIR, "app", "docs", "guides", guide.slug)),
    `app/docs/guides/${guide.slug} exists — a page under app/docs is a public, canonical-carrying ` +
      `guide that sitemapCoverage.test.ts requires in the apex sitemap`,
  );
  assert.equal(guide.assets, ROUTE, `the private guide's assets directory is ${guide.assets}, not ${ROUTE}`);
});

test("no file under web/src links to the route (it cannot leak into the public site)", () => {
  // Every source file under web/src, minus the page's own modules (which necessarily name their
  // own route and asset URLs) and the tests (which ship nowhere and must name what they search
  // for). A hit anywhere else is a link from a public surface — the landing page, the docs, the
  // footer, the dashboard — and that is what makes a "private" page findable.
  const OWN = new Set([
    join(SRC_DIR, "app", "panduan", "surau", "page.tsx"),
    join(SRC_DIR, "lib", "panduanSurauPage.ts"),
    join(SRC_DIR, "lib", "guidesPrivate.gen.ts"),
  ]);
  const files: string[] = [];
  const walk = (dir: string) => {
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      const full = join(dir, e.name);
      if (e.isDirectory()) walk(full);
      else files.push(full);
    }
  };
  walk(SRC_DIR);

  assert.ok(files.length > 100, `walked only ${files.length} files — the scan is not looking at the tree`);

  const leaks = files
    .filter((f) => !OWN.has(f) && !f.endsWith(".test.ts") && f !== THIS_FILE)
    .filter((f) => {
      const body = readFileSync(f);
      // A binary asset cannot link anywhere; guard the scan against a future generated blob
      // rather than crashing on it.
      if (body.includes(0)) return false;
      return body.toString("utf8").includes(ROUTE);
    })
    .map((f) => f.slice(WEB_DIR.length + 1));

  assert.deepEqual(
    leaks,
    [],
    `these files under web/src link to ${ROUTE}, which would make the private guide discoverable:\n  ` +
      `${leaks.join("\n  ")}\nThe page is reachable ONLY through the URL Wan hands to the committee.`,
  );
});

test("the worked example is the engine's numbers, not the naive price + percentage", () => {
  // api/internal/payments/fee_test.go (TestGrossUpWorkedExample) pins
  //   GrossUp(100000, 0, 100) == (total 101010, fee 1010)
  // for a RM1,000 asking price at 1% (100 basis points) with no fixed charge. The guide must say
  // the same thing, so the figures below are FORMATTED from those sen values rather than typed:
  // a page that says RM1,010.00 (the naive total) fails here.
  const askingSen = 100_000;
  const totalSen = 101_010;
  const feeSen = 1_010;
  assert.equal(askingSen + feeSen, totalSen, "the worked example's own arithmetic is inconsistent");

  const html = renderPage();
  const text = visible(html);

  assert.ok(
    text.includes(rm(totalSen)),
    `the page does not contain ${rm(totalSen)} — the grossed-up total the engine charges ` +
      `(payments.GrossUp(100000, 0, 100) = 101010 sen)`,
  );
  assert.ok(
    text.includes(rm(askingSen)),
    `the page does not contain ${rm(askingSen)} — the amount the merchant nets ` +
      `(total − fee = 100000 sen)`,
  );
  assert.ok(
    text.includes(rm(feeSen)),
    `the page does not contain the fee ${rm(feeSen)} (payments.GrossUp's fee sen)`,
  );
  // The naive answer (price + 1% = RM1,010.00) is printed in the guide, but ONLY as the thing that
  // is WRONG: it must sit in the same sentence as the merchant's shortfall (RM999.90, 10 sen
  // short). A guide that prints it anywhere else teaches arithmetic the engine does not do.
  const naive = rm(askingSen + feeSen);
  const occurrences = [...text.matchAll(new RegExp(naive.replace(/[.,]/g, "\\$&"), "g"))].map((m) => m.index ?? -1);
  const qualified = occurrences.some((at) => {
    const around = text.slice(Math.max(0, at - 200), at + 200);
    return /RM999\.90/.test(around) && /kurang 10 sen|10 sen kurang/.test(around);
  });
  assert.ok(
    occurrences.length === 0 || qualified,
    `the page prints ${naive} (price + 1%) without showing, in the same breath, that it leaves the ` +
      `merchant RM999.90 — 10 sen short. A reader cannot tell that figure apart from the total ` +
      `to charge, and CHIP would keep the 10 sen.`,
  );
  assert.ok(
    text.includes("RM999.90"),
    "the page does not show what the naive price + 1% leaves the merchant (RM999.90)",
  );
});

// ── the figures a phone reader gets (card t_e1ea03a2) ────────────────────────────────────────
//
// The capture is a 1900 px full-page screenshot. Rendered at the width of the article column on
// a 390 px phone (358 px) it is a 0.19x thumbnail whose 16 px labels land at ~3 px — measured on
// the live page, and Wan's complaint about it ("gambar screenshot tu terlalu kecik … yg baca ni
// mungkin org berusia"). So every step renders a CROP of its capture (docs/guides/<slug>/
// crops.json → the generator) and links to the full PNG, which is what a phone opens so the
// reader can pinch-zoom.
//
// The floor is the card's: at a 390 px viewport each figure renders at >= 0.45x its natural
// width. Because the figure is full-bleed there, the rendered width IS 390, so the floor is a
// property of the manifest alone: a detail crop may be at most 390 / 0.45 = 866 px wide. This
// test is what fails if someone re-captures the screenshots and the figures go back to being
// 1900 px wide.
const PHONE_VIEWPORT = 390;
const SCALE_FLOOR = 0.45;
const MAX_DETAIL_WIDTH = Math.floor(PHONE_VIEWPORT / SCALE_FLOOR); // 866

/** Intrinsic size from a PNG's IHDR chunk; the same read the generator does. */
function pngSize(file: string): { width: number; height: number } {
  const buf = readFileSync(file);
  assert.equal(buf.toString("ascii", 1, 4), "PNG", `${file} is not a PNG`);
  return { width: buf.readUInt32BE(16), height: buf.readUInt32BE(20) };
}

const attr = (tag: string, name: string) => new RegExp(`\\b${name}="([^"]*)"`).exec(tag)?.[1];

test("every figure is a readable crop that links to its full-size capture", () => {
  // One <figure> per step, in order, each holding one <a> around one <img>.
  const html = renderPage();
  const figures = [...html.matchAll(/<figure\b[^>]*>([\s\S]*?)<\/figure>/g)].map((m) => m[0]);
  assert.equal(figures.length, guide.shots.length, `the page renders ${figures.length} figures, not ${guide.shots.length}`);

  const problems: string[] = [];
  const measured: string[] = [];
  for (const [i, figure] of figures.entries()) {
    const shot = guide.shots[i];
    const img = /<img\b[^>]*>/.exec(figure)?.[0] ?? "";
    const link = /<a\b[^>]*>/.exec(figure)?.[0] ?? "";
    const caption = /<figcaption\b[^>]*>([\s\S]*?)<\/figcaption>/.exec(figure)?.[1] ?? "";
    const label = `step ${i + 1} (${shot.file})`;

    if (!img) {
      problems.push(`${label}: the figure has no <img>`);
      continue;
    }
    if (!link) {
      problems.push(`${label}: the figure is not a link — there is no way to enlarge it`);
    } else {
      // The full-size capture, opened in a new tab. Never the crop itself: the crop is what the
      // reader already has.
      const href = attr(link, "href");
      const wantHref = `${guide.assets}/${shot.file}`;
      if (href !== wantHref) problems.push(`${label}: the link goes to ${href}, not the full capture ${wantHref}`);
      if (attr(link, "target") !== "_blank") problems.push(`${label}: the link does not open a new tab`);
      if (!/noreferrer/.test(attr(link, "rel") ?? "")) problems.push(`${label}: the link has no rel="noreferrer"`);
      // "Langkah 5: <the step's heading> — ketik untuk lihat penuh".
      const name = attr(link, "aria-label") ?? "";
      if (!new RegExp(`^Langkah ${i + 1}: .+ — ketik untuk lihat penuh$`).test(name)) {
        problems.push(`${label}: its accessible name is ${JSON.stringify(name)}, which does not name the step and say it can be enlarged`);
      }
    }
    // The caption says it can be enlarged, in Malay.
    if (!/Ketik untuk lihat penuh/.test(caption)) {
      problems.push(`${label}: the caption does not say "Ketik untuk lihat penuh"`);
    }
    // The INLINE image is the crop, not the capture, and it clears the scale floor.
    const detail = shot.detail;
    if (!detail) {
      problems.push(`${label}: the manifest carries no detail crop — this figure still renders its ${shot.width}px capture (${(PHONE_VIEWPORT / shot.width).toFixed(2)}x on a phone)`);
      continue;
    }
    const wantSrc = `${guide.assets}/${detail.path}`;
    if (attr(img, "src") !== wantSrc) problems.push(`${label}: the <img> renders ${attr(img, "src")}, not the crop ${wantSrc}`);
    if (attr(img, "width") !== String(detail.width) || attr(img, "height") !== String(detail.height)) {
      problems.push(`${label}: the <img> is rendered without the crop's intrinsic size (${detail.width}x${detail.height})`);
    }
    if (detail.width > MAX_DETAIL_WIDTH) {
      problems.push(
        `${label}: its figure is ${detail.width} px wide, so a 390 px phone draws it at ` +
          `${(PHONE_VIEWPORT / detail.width).toFixed(2)}x — under the ${SCALE_FLOOR}x floor ` +
          `(max ${MAX_DETAIL_WIDTH} px). Re-crop it in docs/guides/${guide.slug}/crops.json ` +
          `and run \`npm run guides:sync\` to rebuild it.`,
      );
    }
    if (detail.width >= shot.width) {
      problems.push(`${label}: its "crop" is as wide as the capture (${detail.width} of ${shot.width}) — it is not a crop`);
    }
    measured.push(`${shot.file} ${detail.width}x${detail.height} = ${(PHONE_VIEWPORT / detail.width).toFixed(3)}x`);

    // …and the bytes exist at both URLs: the crop at the size the manifest claims, the full
    // capture still 1900 px (the zoom target a phone opens).
    const detailFile = join(PUBLIC_DIR, guide.assets.replace(/^\//, ""), detail.path);
    if (!existsSync(detailFile)) {
      problems.push(`${label}: ${wantSrc} is not on disk — run \`npm run guides:sync\` to write it`);
    } else {
      const size = pngSize(detailFile);
      if (size.width !== detail.width || size.height !== detail.height) {
        problems.push(`${label}: ${wantSrc} is ${size.width}x${size.height}, the manifest says ${detail.width}x${detail.height}`);
      }
    }
    const fullFile = join(PUBLIC_DIR, guide.assets.replace(/^\//, ""), shot.file);
    if (!existsSync(fullFile)) {
      problems.push(`${label}: the full capture ${shot.file} is not on disk — the link would 404`);
    } else {
      const size = pngSize(fullFile);
      if (size.width !== shot.width || size.height !== shot.height) {
        problems.push(`${label}: the full capture is ${size.width}x${size.height}, the manifest says ${shot.width}x${shot.height}`);
      }
    }
  }
  // Printed so the measured pairs are in the run log.
  console.log(`\n${ROUTE} figures at ${PHONE_VIEWPORT}px:\n  ${measured.join("\n  ")}\n`);
  assert.deepEqual(problems, [], `these figures would not be readable on a phone:\n  ${problems.join("\n  ")}`);
});

test("the nine steps carry their nine boxed screenshots", () => {
  assert.equal(guide.shots.length, 9, `the guide has ${guide.shots.length} screenshots, not 9`);
  const html = renderPage();
  const text = visible(html);

  // One heading per step, in order — "## 1." … "## 9." survive into the rendered <h2>s.
  for (let n = 1; n <= 9; n += 1) {
    assert.match(text, new RegExp(`\\b${n}\\. `), `step ${n} has no numbered heading`);
  }
  // Every screenshot reaches the browser: one <img> per manifest entry. WHICH url each one
  // renders (the detail crop) and how big it is — the phone-legibility floor — is the previous
  // test's subject; this one is that no step lost its picture.
  assert.equal(
    (html.match(/<img /g) ?? []).length,
    guide.shots.length,
    "the page renders a different number of <img> tags than the guide has screenshots",
  );
  // Alt text, not a filename: every <img> must carry an alt that names the control.
  const alts = [...html.matchAll(/<img [^>]*alt="([^"]*)"/g)].map((m) => m[1]);
  assert.equal(alts.length, guide.shots.length, "an <img> has no alt attribute");
  for (const alt of alts) {
    assert.ok(alt.length > 20, `alt text too thin to describe a step: ${JSON.stringify(alt)}`);
    assert.ok(/dibulatkan|boxed/.test(alt), `alt text does not say what is boxed: ${JSON.stringify(alt)}`);
  }
});

test("the page stays inside the guides' word budget", () => {
  // The same 700-word ceiling guidesContent.test.ts applies to /docs/guides pages: a step guide
  // is a checklist, not an essay. This page is private but its reader is a volunteer treasurer.
  const words = visible(renderPage()).split(/\s+/).filter(Boolean).length;
  assert.ok(
    words <= 700,
    `the page runs ${words} words, over the 700-word budget — trim the steps and move caveats to Nota`,
  );
});
