// t_0fe14ba5 — the grid/modal must not preview a CODE asset through <img>.
//
// Why this is a test and not just two inline mime checks: the failure is silent
// (a broken-image icon, HTTP 200 on the media route) and it only appears for
// SVG — the one media type where "image/*" and "tenant-scoped code asset" are
// both true. The API side of the same decision is objstore.IsCodeAsset.

import assert from "node:assert/strict";
import test from "node:test";
import { canInlineMediaPreview, isCodeAssetMime } from "./mediaThumb.ts";

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
