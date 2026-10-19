// t_0fe14ba5 — the grid/modal must not preview a CODE asset through <img>.
//
// Why this is a test and not just two inline mime checks: the failure is silent
// (a broken-image icon, HTTP 200 on the media route) and it only appears for
// SVG — the one media type where "image/*" and "tenant-scoped code asset" are
// both true. The API side of the same decision is objstore.IsCodeAsset.

import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { canInlineMediaPreview, extBadge, isCodeAssetMime } from "./mediaThumb.ts";

test("code assets are recognised by their stored content-type", () => {
  for (const mime of [
    "text/css; charset=utf-8",
    "text/javascript; charset=utf-8",
    "application/javascript; charset=utf-8",
    "text/html; charset=utf-8",
    "image/svg+xml",
    "font/woff2",
  ]) {
    assert.equal(isCodeAssetMime(mime), true, `${mime} is a code asset on the API side too`);
  }
  for (const mime of ["image/png", "image/jpeg", "image/webp", "image/gif", "application/pdf"]) {
    assert.equal(isCodeAssetMime(mime), false, `${mime} is not a code asset`);
  }
});

test("an SVG never gets an <img> preview (it is a tenant-scoped code asset)", () => {
  assert.equal(canInlineMediaPreview("image/svg+xml", false), false);
});

test("plain images keep their real thumbnail", () => {
  for (const mime of ["image/png", "image/jpeg", "image/webp", "image/gif"]) {
    assert.equal(canInlineMediaPreview(mime, false), true, `${mime} must still render`);
  }
});

test("a generated PDF preview is an image on the media host, so it renders", () => {
  assert.equal(canInlineMediaPreview("application/pdf", true), true);
  // …and a PDF with no preview (Ghostscript was unavailable) falls back to the badge.
  assert.equal(canInlineMediaPreview("application/pdf", false), false);
});

test("CSS/JS/HTML/WOFF2 get the extension badge, not an <img>", () => {
  for (const mime of [
    "text/css; charset=utf-8",
    "text/javascript; charset=utf-8",
    "text/html; charset=utf-8",
    "font/woff2",
  ]) {
    assert.equal(canInlineMediaPreview(mime, false), false, `${mime} must not be inlined`);
  }
});

// ── the LOGO PICKER uses this same rule (t_04ea7f88) ───────────────────────
//
// THE DEFECT this pins: `6ae0806e` taught app/media/page.tsx to badge a code
// asset, but components/MediaPicker.tsx kept its own copy of the decision
// (`mime.startsWith("image/")`) and rendered every row through
// `<img src={m.preview || m.sizes?.thumbnail || m.url}>`. For an SVG there is
// no preview and no sizes, so src is the row's url — which for a code asset is
// now the WORKSPACE host (https://<workspace>.jomform.com/media/…
// or the verified custom domain), served `Cross-Origin-Resource-Policy:
// same-origin`. The dashboard is a different origin, so the browser refuses it.
//
// WHY THIS IS A SOURCE-SCAN AND NOT A RENDER TEST: the failure is SILENT —
// the media route answers HTTP 200 image/svg+xml, nothing throws, no console
// error the app can see, and the picker still lists and selects the row. The
// only symptom is a broken-image icon in the tile, which is why it survived a
// green suite. There is no jsdom here, so the guard reads the component source:
// the shared helper must be what gates the <img>.
test("the logo picker gates its <img> on canInlineMediaPreview (t_04ea7f88)", () => {
  const picker = readFileSync(
    fileURLToPath(new URL("../components/MediaPicker.tsx", import.meta.url)),
    "utf8",
  );

  // 1. it imports the SHARED rule rather than comparing mime prefixes itself.
  assert.match(
    picker,
    /import\s*\{[^}]*canInlineMediaPreview[^}]*extBadge[^}]*\}\s*from\s*"\.\.\/lib\/mediaThumb"/,
    "MediaPicker no longer imports canInlineMediaPreview/extBadge from lib/mediaThumb — a " +
      "local copy of the rule is exactly how the SVG tile became a broken <img>",
  );

  // 2. the render is behind that guard, and the badge is the else-branch.
  //    Anchor on the JSX open tag itself (<img src={m.preview …>), not on the
  //    substring "<img" — the comments above mention it in prose.
  const guard = picker.indexOf("canInlineMediaPreview(m.mime, !!m.preview)");
  assert.ok(guard > 0, "MediaPicker does not call canInlineMediaPreview(m.mime, !!m.preview)");
  const imgTag = /<img\s+src=\{m\.preview \|\| m\.sizes\?\.thumbnail \|\| m\.url\}/.exec(picker);
  assert.ok(imgTag, "MediaPicker no longer renders an <img src={m.preview || m.sizes?.thumbnail || m.url}>");
  const img = imgTag.index;
  const badge = picker.indexOf("extBadge(m.mime)");
  assert.ok(
    img > guard,
    "MediaPicker renders the <img> outside the shared guard — an SVG row (tenant-host URL, " +
      "CORP same-origin) paints a broken image and nothing errors",
  );
  assert.ok(
    badge > guard,
    "MediaPicker has no extBadge(m.mime) branch — a row the guard refuses would render nothing",
  );
  assert.ok(
    img < badge,
    "the badge is not the else-branch of the guard — the <img> is outside it again",
  );

  // 3. SVG is still OFFERED (the fix must not be a silent de-scope): it stays in
  //    ACCEPTED_TYPES and the row filter stays image-based.
  assert.ok(
    picker.includes('"image/svg+xml"'),
    "image/svg+xml was dropped from the picker's ACCEPTED_TYPES — excluding SVG is a product " +
      "decision, not a side effect of this fix",
  );
  assert.ok(
    /\.filter\(\(m: MediaItem\) => isImage\(m\.mime\)\)/.test(picker),
    "the picker no longer lists rows by image type — an SVG logo would silently disappear " +
      "from the picker instead of rendering as a badge",
  );
});

test("extBadge is declared once, in lib/mediaThumb, and used by both surfaces", () => {
  const mediaPage = readFileSync(
    fileURLToPath(new URL("../app/media/page.tsx", import.meta.url)),
    "utf8",
  );
  assert.ok(
    !/function extBadge\s*\(/.test(mediaPage),
    "app/media/page.tsx declares its own extBadge again — the picker drifted from this " +
      "page once already by keeping a private copy of the tile rule",
  );
  assert.match(
    mediaPage,
    /import\s*\{[^}]*extBadge[^}]*\}\s*from\s*"\.\.\/\.\.\/lib\/mediaThumb"/,
    "app/media/page.tsx no longer imports extBadge from lib/mediaThumb",
  );
  // The label itself, including the SVG case that was invisible in the picker.
  assert.equal(extBadge("image/svg+xml"), "SVG");
  assert.equal(extBadge("text/css; charset=utf-8"), "CSS");
  assert.equal(extBadge("application/pdf"), "PDF");
  // Fallback for a content-type the table does not name: first four characters.
  assert.equal(extBadge("application/zip"), "APPL");
});

