#!/usr/bin/env node
// Guides generator: docs/guides/<slug>/ → the web app.
//
// WHY THIS EXISTS (card t_7b9f6d18)
//   Two complete screenshot guides already lived in the repo — `docs/guides/payment-fees`
//   and `docs/guides/donations` — and nobody could see them: `docs/` is not served by Next,
//   `web/src/app/docs` has no route for them, and `web/public` held none of the PNGs. So the
//   landing page told every visitor the guide was "coming soon" while the guide sat in git.
//
//   Next cannot read `docs/` at request time (the app is statically rendered, and the Docker
//   build does not even carry that directory), so the guides are materialised at BUILD-PREP
//   time into two places the app already understands:
//
//     web/src/lib/guides.gen.ts          the README text + the screenshot manifest
//     web/public/guides/<slug>/<file>.png the screenshots, at the URL the page renders
//
//   The generated module is COMMITTED, exactly like src/lib/apiExamples.gen.ts: the image
//   build must not depend on a repo-root directory it does not copy. Drift between
//   docs/guides and the generated output is caught by src/lib/guidesContent.test.ts on the
//   host (and by `node scripts/guides.mjs --check`); the image build keeps the intra-web half
//   of that guard (page files, public assets, dimensions).
//
// WHAT IT VALIDATES (and refuses rather than guesses)
//   * every `![alt](file)` in the README is a BARE filename that exists in the guide folder
//     and is a PNG — the renderer accepts bare filenames only, so an absolute http(s) URL or
//     a path with a slash is rejected HERE, where the author can see why;
//   * every PNG in the folder is referenced by the README — an unreferenced screenshot is a
//     step whose image never renders, which is the defect this card exists to prevent;
//   * the README starts with a title (`# …`), which becomes the page's <h1> and is removed
//     from the body (so the page does not render two h1s);
//   * the README's FIRST PARAGRAPH becomes the page's lede (`summary`) and is removed from
//     the body, so the sentence has ONE owner and cannot be printed twice (card t_b5ea9103);
//     it must be a single sentence of at most SUMMARY_MAX characters — the generator refuses
//     a longer one instead of cutting it with a "…" in the middle of the page's intro.
//
// Run:  node scripts/guides.mjs            (write the generated files)
//       node scripts/guides.mjs --check    (write nothing; exit 1 if the tree is out of date)

import { readFileSync, writeFileSync, readdirSync, mkdirSync, existsSync, statSync, unlinkSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const WEB_DIR = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const REPO_ROOT = resolve(WEB_DIR, "..");
const SRC_DIR = join(REPO_ROOT, "docs", "guides");
const OUT_MODULE = join(WEB_DIR, "src", "lib", "guides.gen.ts");
const OUT_PUBLIC = join(WEB_DIR, "public", "guides");
const OUT_PRIVATE_MODULE = join(WEB_DIR, "src", "lib", "guidesPrivate.gen.ts");

// ── PRIVATE GUIDES ───────────────────────────────────────────────────────────
//
// A private guide is authored EXACTLY like a public one — `docs/guides/<slug>/README.md` plus
// one screenshot per step, same red-box convention, same alt text — and it goes through the
// same validation below. The only difference is where the output lands:
//
//   public guide   → src/lib/guides.gen.ts            + public/guides/<slug>/         (+ /docs/guides)
//   private guide  → src/lib/guidesPrivate.gen.ts     + public/<assets>/             (no index entry)
//
// The map is `slug → the asset directory the private page renders its screenshots from`. A slug
// listed here is REMOVED from GUIDES, so it cannot reach /docs/guides, the docs nav/search
// registry (web/scripts/docsNav.mjs walks the page tree, not this list), the apex sitemap or
// llms.txt — the four surfaces that would make it findable. The page that does render it lives
// outside app/docs (app/panduan/surau) and is noindex by its own metadata; see
// src/lib/panduanSurau.test.ts for the guards that hold all of that in place.
const PRIVATE_GUIDES = new Map([["surau-donation", "/panduan/surau"]]);

const CHECK = process.argv.includes("--check");

const problems = [];
const fail = (msg) => problems.push(msg);

// ── DETAIL CROPS (card t_e1ea03a2) ───────────────────────────────────────────
//
// A capture is a 1900 px full-page screenshot: correct for a desktop docs page, unreadable as a
// phone thumbnail — at the 358 px article column of a 390 px viewport it is a 0.19x scale, so a
// 16 px label inside the capture lands at 3 px. A guide whose figures ARE the instructions
// (the private surau guide, read by a volunteer committee) therefore renders a CROP of each
// capture and links to the full PNG for pinch-zoom.
//
// The crop is declared as DATA — `crops.json` next to the README, one rect per screenshot — and
// applied HERE, at sync time. Deriving it rather than committing a second set of PNGs is what
// makes it safe to re-capture: the thumbnail is a function of the full capture and the rect, so
// replacing a capture cannot silently leave a stale thumbnail behind, and no rect can drift from
// the bytes on disk. The full PNG is written unchanged; it stays the zoom target.
//
// `crops.json`:
//   { "shots": { "<file>.png": { "x": 120, "y": 560, "width": 820, "height": 310 } } }
//
// Only a PRIVATE guide may declare crops: the zoom target is a URL its own page's image resolver
// builds (src/lib/panduanSurauPage.ts), and a public /docs/guides figure has no such link — it
// would render a crop a reader could not enlarge. So a crops.json in a public guide is refused.
const DETAIL_DIR = "detail";
let sharpModule = null;
/** sharp, loaded only when a guide actually declares crops (the sync path is otherwise image-free). */
async function loadSharp() {
  if (!sharpModule) {
    try {
      sharpModule = (await import("sharp")).default;
    } catch (err) {
      console.error(
        `guides: docs/guides declares detail crops but "sharp" is not installed in ${WEB_DIR} ` +
          `(${err.message}). It ships as Next's image dependency, so \`npm install\` in web/ ` +
          `provides it — the crop cannot be derived without it.`,
      );
      process.exit(1);
    }
  }
  return sharpModule;
}

/** Width/height from a PNG's IHDR chunk — the page needs them so an <img> cannot shift layout. */
function pngSize(buf, file) {
  const sig = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a];
  for (let i = 0; i < sig.length; i++) {
    if (buf[i] !== sig[i]) {
      fail(`${file}: not a PNG (bad signature)`);
      return { width: 0, height: 0 };
    }
  }
  if (buf.toString("ascii", 12, 16) !== "IHDR") {
    fail(`${file}: no IHDR chunk — cannot read its dimensions`);
    return { width: 0, height: 0 };
  }
  return { width: buf.readUInt32BE(16), height: buf.readUInt32BE(20) };
}

/**
 * The cap on the lede paragraph, in characters. It is the same 200 `plainSummary` cuts at —
 * but the cap is ENFORCED (the generator fails) rather than paid for with a "…" in the middle
 * of the guide's opening sentence.
 */
const SUMMARY_MAX = 200;

/** Plain text of a markdown paragraph, for the index page's one-line summary. */
function plainSummary(md) {
  const text = md
    .replace(/!\[[^\]]*\]\([^)]*\)/g, " ")
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/[`*_]/g, "")
    .replace(/\s+/g, " ")
    .trim();
  if (text.length <= 200) return text;
  const cut = text.slice(0, 200);
  const at = cut.lastIndexOf(" ");
  return `${cut.slice(0, at > 120 ? at : 200)}…`;
}

/**
 * The guide's detail crops, from `crops.json` next to the README (see DETAIL CROPS above), or
 * null when the guide renders its captures whole. Every rect is checked against the capture it
 * crops, and a guide may not crop only some of its steps: a half-cropped guide is a guide whose
 * remaining steps are still 0.19x on a phone.
 */
function readCrops(dir, slug, shots) {
  const file = join(dir, "crops.json");
  if (!existsSync(file)) return null;
  let doc;
  try {
    doc = JSON.parse(readFileSync(file, "utf8"));
  } catch (err) {
    fail(`docs/guides/${slug}/crops.json: not valid JSON (${err.message})`);
    return null;
  }
  const declared = doc?.shots;
  if (!declared || typeof declared !== "object" || Array.isArray(declared)) {
    fail(`docs/guides/${slug}/crops.json: no "shots" object mapping <file>.png to {x,y,width,height}`);
    return null;
  }
  const byFile = new Map(shots.map((s) => [s.file, s]));
  const crops = new Map();
  for (const [name, rect] of Object.entries(declared)) {
    const shot = byFile.get(name);
    if (!shot) {
      fail(
        `docs/guides/${slug}/crops.json declares a crop for "${name}", which the README never ` +
          `references — a crop of an image nothing displays.`,
      );
      continue;
    }
    const integral = rect && ["x", "y", "width", "height"].every((k) => Number.isInteger(rect[k]) && rect[k] >= 0);
    if (
      !integral ||
      rect.width === 0 ||
      rect.height === 0 ||
      rect.x + rect.width > shot.width ||
      rect.y + rect.height > shot.height
    ) {
      fail(
        `docs/guides/${slug}/crops.json: "${name}" is not an integer {x,y,width,height} rect inside ` +
          `the ${shot.width}x${shot.height} capture.`,
      );
      continue;
    }
    crops.set(name, { x: rect.x, y: rect.y, width: rect.width, height: rect.height });
  }
  const missing = shots.filter((s) => !crops.has(s.file)).map((s) => s.file);
  if (missing.length > 0) {
    fail(
      `docs/guides/${slug}/crops.json crops ${crops.size} of ${shots.length} screenshots; ` +
        `${missing.join(", ")} would still render at its full width (0.19x on a 390px phone). ` +
        `Crop every shot, or delete crops.json — a guide cannot be half-legible.`,
    );
  }
  return crops.size > 0 ? crops : null;
}

/** One guide directory → the record the page renders. */
function readGuide(slug) {
  const dir = join(SRC_DIR, slug);
  const readme = join(dir, "README.md");
  if (!existsSync(readme)) {
    fail(`docs/guides/${slug}: no README.md`);
    return null;
  }
  let body = readFileSync(readme, "utf8").replace(/\r\n?/g, "\n");

  // Title: the first `# ` line. Removed from the body — the page renders it as its own <h1>.
  const lines = body.split("\n");
  const titleAt = lines.findIndex((l) => /^#\s+\S/.test(l));
  if (titleAt === -1) {
    fail(`docs/guides/${slug}/README.md: no "# Title" line`);
    return null;
  }
  const title = lines[titleAt].replace(/^#\s+/, "").trim();
  lines.splice(titleAt, 1);
  body = lines.join("\n").replace(/^\n+/, "").replace(/\s+$/, "") + "\n";

  // Every image reference, in document order; each must be a bare filename in this folder.
  const shots = [];
  const seen = new Set();
  for (const m of body.matchAll(/!\[([^\]]*)\]\(([^)\s]+)\)/g)) {
    const file = m[2];
    if (!/^[A-Za-z0-9][A-Za-z0-9._-]*\.png$/.test(file) || file.includes("..")) {
      fail(
        `docs/guides/${slug}/README.md: image "${file}" is not a bare <name>.png in the guide ` +
          `folder. The /docs/guides renderer accepts local screenshots only — host the image ` +
          `in docs/guides/${slug}/ and reference it by filename.`,
      );
      continue;
    }
    if (seen.has(file)) continue;
    seen.add(file);
    const abs = join(dir, file);
    if (!existsSync(abs)) {
      fail(`docs/guides/${slug}/README.md references ${file}, which is not in docs/guides/${slug}/`);
      continue;
    }
    const { width, height } = pngSize(readFileSync(abs), `docs/guides/${slug}/${file}`);
    if (width === 0 || height === 0) continue;
    shots.push({ file, width, height });
  }
  if (shots.length === 0) fail(`docs/guides/${slug}: the README references no screenshots`);

  // An orphan screenshot is a step whose image never renders. Refuse rather than copy it.
  for (const f of readdirSync(dir).sort()) {
    if (!f.endsWith(".png")) continue;
    if (!seen.has(f)) {
      fail(
        `docs/guides/${slug}/${f} is a screenshot the README never references — it would be ` +
          `copied into web/public and never displayed. Add its step, or delete the file.`,
      );
    }
  }

  // ONE OWNER FOR THE SUMMARY (card t_b5ea9103). The README's first paragraph is the
  // guide's LEDE: the page renders it ONCE, from `summary` (GuideArticle), and the
  // renderer must not print it a second time out of `body` — which is exactly what
  // happened before this card: the live page showed the same sentence twice, once as
  // `{guide.summary}` and once as the body's first paragraph. So the paragraph is
  // REMOVED from `body` here, where the two halves are split, and not hidden with CSS
  // or deduplicated in the component: one sentence, one owner, and the generated
  // module cannot carry it twice.
  //
  // The lede is REFUSED rather than truncated when it is too long. `plainSummary` used
  // to cut at SUMMARY_MAX characters and end on "…", and the page rendered that cut
  // sentence at the top of the guide (qr-table-ordering and tuition-class shipped with
  // a dangling "…"). A lede is one sentence; keep it one.
  const paragraphs = body.split("\n\n");
  const ledeAt = paragraphs.findIndex(
    (p) => p.trim() !== "" && !p.trim().startsWith("#") && !p.trim().startsWith("!["),
  );
  const lede = ledeAt === -1 ? "" : paragraphs[ledeAt].trim().replace(/\s+/g, " ");
  if (lede === "") {
    fail(
      `docs/guides/${slug}/README.md: no lede paragraph after the title — the page renders ` +
        `the first paragraph as its intro, so the guide would open with no summary.`,
    );
  } else if (lede.length > SUMMARY_MAX) {
    fail(
      `docs/guides/${slug}/README.md: the lede paragraph is ${lede.length} characters, over the ` +
        `${SUMMARY_MAX}-character limit. The page renders this paragraph as the guide's intro and ` +
        `as its meta description, so it has to be ONE sentence — trim it rather than let it be cut.`,
    );
  } else {
    paragraphs.splice(ledeAt, 1);
    body = `${paragraphs.join("\n\n").replace(/^\n+/, "").replace(/\s+$/, "")}\n`;
  }

  return { slug, title, summary: plainSummary(lede), body, shots, crops: readCrops(dir, slug, shots) };
}

// ── read the source guides ───────────────────────────────────────────────────
if (!existsSync(SRC_DIR)) {
  console.error(`guides: no ${SRC_DIR} — run this from a repo checkout`);
  process.exit(1);
}
const slugs = readdirSync(SRC_DIR, { withFileTypes: true })
  .filter((e) => e.isDirectory())
  .map((e) => e.name)
  .sort();
// A slug in PRIVATE_GUIDES is read by the SAME reader and validated by the SAME rules, then
// routed to the private outputs — it never enters `guides`, so it cannot be listed publicly.
const publicSlugs = slugs.filter((s) => !PRIVATE_GUIDES.has(s));
const privateSlugs = slugs.filter((s) => PRIVATE_GUIDES.has(s));
const guides = publicSlugs.map(readGuide).filter((g) => g !== null);
const privateGuides = privateSlugs
  .map(readGuide)
  .filter((g) => g !== null)
  .map((g) => ({ ...g, assets: PRIVATE_GUIDES.get(g.slug) }));

// A PUBLIC guide may not declare crops (see DETAIL CROPS above): /docs/guides figures are not
// links, so a crop there is a figure the reader can never enlarge — the exact complaint this
// crop exists to answer. Only the private page's resolver knows the full-size URL.
for (const g of guides) {
  if (g.crops) {
    fail(
      `docs/guides/${g.slug}/crops.json: only a PRIVATE guide may declare detail crops. A ` +
        `/docs/guides figure renders through GuideArticle, which does not link it to its full ` +
        `capture, so a crop there would leave the reader with no way to enlarge it.`,
    );
  }
}

// A private guide that has a page but no source is an orphan page whose prose nobody can edit;
// a source directory for a private page the app does not render is a guide nobody can reach.
for (const g of privateGuides) {
  const page = join(WEB_DIR, "src", "app", g.assets.replace(/^\//, ""), "page.tsx");
  if (!existsSync(page)) {
    fail(`docs/guides/${g.slug} is listed as a private guide but ${page} does not exist — the guide would never render`);
  }
}

if (problems.length > 0) {
  console.error(`guides: ${problems.length} problem(s), nothing written:\n  ${problems.join("\n  ")}`);
  process.exit(1);
}

// ── the generated module ─────────────────────────────────────────────────────
const q = (s) => JSON.stringify(s);
const generated = `// GENERATED FILE — do not edit. Run \`npm run guides:sync\` (web/scripts/guides.mjs).
//
// Source of truth: docs/guides/<slug>/README.md + its screenshots. This module exists
// because the app is statically rendered and the Docker image does not carry the repo-root
// docs/ tree, so the guide text and its screenshot manifest are materialised at build-prep
// time. src/lib/guidesContent.test.ts fails when this file drifts from docs/guides.
//
// The screenshots themselves are copied to web/public/guides/<slug>/ (same script) — the
// URLs the rendered <img> tags use.

export type GuideShot = {
  /** Bare filename, exactly as the README writes it: ![](01-step.png). */
  file: string;
  /** Intrinsic PNG size, so the <img> is emitted with width/height and cannot shift layout. */
  width: number;
  height: number;
};

export type Guide = {
  slug: string;
  /** The README's \`# …\` line. The page renders it as its <h1>; it is removed from body. */
  title: string;
  /** One plain-text line for the /docs/guides index. */
  summary: string;
  /** The README with the title line removed, rendered through <Markdown mode="docs">. */
  body: string;
  shots: GuideShot[];
};

export const GUIDES: Guide[] = ${JSON.stringify(guides.map(({ crops, ...g }) => g), null, 2)};

/** The guide a page renders. Throws at build time rather than shipping a blank page. */
export function guideBySlug(slug: string): Guide {
  const guide = GUIDES.find((g) => g.slug === slug);
  if (!guide) {
    throw new Error(
      \`no guide "\${slug}" in src/lib/guides.gen.ts — run \\\`npm run guides:sync\\\`. \` +
        \`Known slugs: \${GUIDES.map((g) => g.slug).join(", ")}\`,
    );
  }
  return guide;
}
`;

// ── the private module ───────────────────────────────────────────────────────
const privateGenerated = `// GENERATED FILE — do not edit. Run \`npm run guides:sync\` (web/scripts/guides.mjs).
//
// The PRIVATE guides: authored in docs/guides/<slug>/ exactly like the public ones, but with a
// page outside app/docs that is noindex and linked from nowhere in the repo. They are kept in a
// separate module so /docs/guides (which maps GUIDES) cannot list one by accident, and the docs
// registry, sitemap, search index and llms.txt never see them.
//
// Source of truth: docs/guides/<slug>/README.md + its screenshots. The screenshots are copied to
// web/public/<assets>/ (the \`assets\` field below is that directory), NOT to public/guides/.

export type PrivateGuideShot = {
  /** Bare filename, exactly as the README writes it: ![](01-step.png). */
  file: string;
  /** Intrinsic PNG size of the FULL capture — the zoom target, and the intrinsic size of the
   *  <a href> it is opened at. The figure itself renders \`detail\` when there is one. */
  width: number;
  height: number;
  /**
   * The INLINE figure: a crop of \`file\` (docs/guides/<slug>/crops.json), sized to stay legible at
   * the width of a phone — see src/lib/panduanSurau.test.ts for the scale floor it must meet.
   * \`path\` is relative to the guide's \`assets\` directory. null: the capture renders whole.
   */
  detail: PrivateGuideDetail | null;
};

export type PrivateGuideDetail = {
  /** Path under the guide's \`assets\` directory: "detail/<file>". */
  path: string;
  width: number;
  height: number;
};

export type PrivateGuide = {
  slug: string;
  /** The README's \`# …\` line. The page renders it as its <h1>; it is removed from body. */
  title: string;
  /** The README's first paragraph, rendered as the page's lede (and removed from body). */
  summary: string;
  /** The README with the title line and the lede removed, rendered through <Markdown mode="docs">. */
  body: string;
  shots: PrivateGuideShot[];
  /** Public directory the screenshots were copied to, e.g. "/panduan/surau". */
  assets: string;
};

export const PRIVATE_GUIDES: PrivateGuide[] = ${JSON.stringify(
  privateGuides.map(({ crops, ...g }) => ({
    ...g,
    shots: g.shots.map((s) => ({
      ...s,
      detail: crops?.get(s.file)
        ? { path: `${DETAIL_DIR}/${s.file}`, width: crops.get(s.file).width, height: crops.get(s.file).height }
        : null,
    })),
  })),
  null,
  2,
)};

/** The private guide a page renders. Throws at build time rather than shipping a blank page. */
export function privateGuideBySlug(slug: string): PrivateGuide {
  const guide = PRIVATE_GUIDES.find((g) => g.slug === slug);
  if (!guide) {
    throw new Error(
      \`no private guide "\${slug}" in src/lib/guidesPrivate.gen.ts — run \\\`npm run guides:sync\\\`. \` +
        \`Known slugs: \${PRIVATE_GUIDES.map((g) => g.slug).join(", ")}\`,
    );
  }
  return guide;
}
`;

// ── the screenshots ──────────────────────────────────────────────────────────
const writes = []; // { path, data }
for (const g of guides) {
  for (const shot of g.shots) {
    writes.push({ path: join(OUT_PUBLIC, g.slug, shot.file), data: readFileSync(join(SRC_DIR, g.slug, shot.file)) });
  }
}
for (const g of privateGuides) {
  const outDir = join(WEB_DIR, "public", g.assets.replace(/^\//, ""));
  for (const shot of g.shots) {
    const src = join(SRC_DIR, g.slug, shot.file);
    writes.push({ path: join(outDir, shot.file), data: readFileSync(src) });
    // The inline figure: the SAME bytes as the capture, cropped to the declared rect. Derived
    // here (not committed) so a re-captured screenshot is re-cropped by the same command that
    // copies it — see DETAIL CROPS above.
    const crop = g.crops?.get(shot.file);
    if (crop) {
      const sharp = await loadSharp();
      writes.push({
        path: join(outDir, DETAIL_DIR, shot.file),
        data: await sharp(src)
          .extract({ left: crop.x, top: crop.y, width: crop.width, height: crop.height })
          // compressionLevel 9: these are static assets served from public/, and a crop of a
          // mostly-white dashboard UI compresses well. No resize, no palette: the crop is 1:1.
          .png({ compressionLevel: 9 })
          .toBuffer(),
      });
    }
  }
}
writes.push({ path: OUT_MODULE, data: Buffer.from(generated, "utf8") });
writes.push({ path: OUT_PRIVATE_MODULE, data: Buffer.from(privateGenerated, "utf8") });

// Files that are no longer generated — a renamed or removed step would otherwise linger in
// public/ and be shipped forever. Only the directories this script owns are swept: one per public
// guide, plus one per private guide's asset directory. Nothing else under public/ is touched.
const stale = [];
const ownedDirs = [
  ...guides.map((g) => ({ dir: join(OUT_PUBLIC, g.slug), slug: g.slug, shots: g.shots })),
  ...privateGuides.map((g) => ({
    dir: join(WEB_DIR, "public", g.assets.replace(/^\//, "")),
    slug: g.slug,
    shots: g.shots,
    // The detail crops live in a subdirectory this script also owns; without this entry a
    // renamed step's crop would linger there forever (the parent sweep cannot descend).
    subdirs: g.crops ? [DETAIL_DIR] : [],
  })),
  ...privateGuides.map((g) => ({
    dir: join(WEB_DIR, "public", g.assets.replace(/^\//, ""), DETAIL_DIR),
    slug: g.slug,
    shots: g.crops ? g.shots : [],
    subdirs: [],
  })),
];
if (existsSync(OUT_PUBLIC)) {
  for (const slug of readdirSync(OUT_PUBLIC, { withFileTypes: true })) {
    if (!slug.isDirectory()) {
      stale.push(join(OUT_PUBLIC, slug.name));
      continue;
    }
    const wanted = new Set((guides.find((g) => g.slug === slug.name)?.shots ?? []).map((s) => s.file));
    for (const f of readdirSync(join(OUT_PUBLIC, slug.name))) {
      if (!wanted.has(f)) stale.push(join(OUT_PUBLIC, slug.name, f));
    }
    if (!guides.some((g) => g.slug === slug.name)) stale.push(join(OUT_PUBLIC, slug.name));
  }
}
for (const owned of ownedDirs) {
  if (!existsSync(owned.dir)) continue;
  const wanted = new Set([...owned.shots.map((s) => s.file), ...(owned.subdirs ?? [])]);
  for (const f of readdirSync(owned.dir)) {
    if (!wanted.has(f)) stale.push(join(owned.dir, f));
  }
}

if (CHECK) {
  const drift = [];
  for (const w of writes) {
    if (!existsSync(w.path)) drift.push(`${w.path} is missing`);
    else if (!readFileSync(w.path).equals(w.data)) drift.push(`${w.path} differs from docs/guides`);
  }
  for (const s of stale) drift.push(`${s} is not generated from docs/guides any more`);
  if (drift.length > 0) {
    console.error(`guides: out of date — run \`npm run guides:sync\`:\n  ${drift.join("\n  ")}`);
    process.exit(1);
  }
  console.log(
    `guides: ${guides.length} guide(s) in sync (${guides.map((g) => g.slug).join(", ")})` +
      (privateGuides.length > 0 ? ` + ${privateGuides.length} private (${privateGuides.map((g) => g.slug).join(", ")})` : ""),
  );
  process.exit(0);
}

for (const w of writes) {
  mkdirSync(dirname(w.path), { recursive: true });
  writeFileSync(w.path, w.data);
}
for (const s of stale) {
  if (statSync(s).isDirectory()) {
    // A guide folder that no longer exists: its screenshots were already listed individually.
    try {
      unlinkSync(s);
    } catch {
      /* a non-empty directory: its files were unlinked above, and an empty one is harmless */
    }
  } else {
    unlinkSync(s);
  }
}
console.log(
  `guides: wrote ${guides.length} guide(s) and ${writes.length - 2} screenshot(s) ` +
    `→ src/lib/guides.gen.ts + public/guides/` +
    (privateGuides.length > 0
      ? `; ${privateGuides.length} private guide(s) → src/lib/guidesPrivate.gen.ts + ${privateGuides
          .map((g) => `public${g.assets}/`)
          .join(", ")}` +
        (privateGuides.some((g) => g.crops)
          ? ` (${privateGuides.reduce((n, g) => n + (g.crops ? g.crops.size : 0), 0)} detail crop(s) → public${privateGuides
              .filter((g) => g.crops)
              .map((g) => `${g.assets}/${DETAIL_DIR}/`)
              .join(", ")})`
          : "")
      : ""),
);
