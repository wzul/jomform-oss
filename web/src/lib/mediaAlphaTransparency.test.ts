// t_ffc7f502: a transparent PNG uploaded through /uploads/image used to be
// stored as an opaque JPEG. Go's JPEG encoder has no alpha channel — it writes
// each pixel's RGB and ignores A — so the merchant's transparent logo came back
// as a solid box painted with whatever colour sat under alpha=0.
//
// THE DECISION (recorded on the card): keep the transparency. An image with any
// non-opaque pixel is stored as PNG (URL ends .png, mime image/png, variants
// PNG); an opaque image is stored as JPEG exactly as before. That is pinned
// server-side by api/internal/httpapi/media_alpha_test.go, which uploads a real
// transparent PNG through the handler and reads alpha back out of the stored
// bytes and every variant.
//
// This test guards the half the merchant actually reads:
//   (1) the pre-upload sentence exists in all FIVE locales and says — in that
//       language — that a transparent PNG stays PNG (a half-translated promise
//       is not a promise);
//   (2) the media page renders it in the drop zone, before the upload result;
//   (3) the logo picker renders it (same endpoint, same optimiser);
//   (4) the /docs/mcp row for upload_media states it too, so an agent uploading
//       on the merchant's behalf is told the same thing.
//
// What this test does NOT cover: that the sentence is what the app RENDERS in a
// browser. That is `npm run build` plus a live read of /media; the strings here
// are the inputs to that render.
//
// Run with: npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT, LANGS, type Lang } from "../components/i18nDict.ts";

const mediaPage = readFileSync(
  fileURLToPath(new URL("../app/media/page.tsx", import.meta.url)),
  "utf8",
);
const mediaPicker = readFileSync(
  fileURLToPath(new URL("../components/MediaPicker.tsx", import.meta.url)),
  "utf8",
);
const docsMCP = readFileSync(
  fileURLToPath(new URL("../app/docs/mcp/page.tsx", import.meta.url)),
  "utf8",
);

// Per locale: the word for "PNG" (universal) and that language's word for the
// transparency being kept. A locale that kept only the English sentence fails.
const REQUIRED: Record<Lang, string[]> = {
  en: ["PNG", "transparent", "see-through"],
  ms: ["PNG", "lutsinar"],
  zh: ["PNG", "透明"],
  ta: ["PNG", "ஒளிபுகும்"],
  ar: ["PNG", "الشفافة"],
};

function slice(source: string, startAnchor: string, endAnchor: string): string {
  const at = source.indexOf(startAnchor);
  assert.ok(at >= 0, `anchor not found: ${startAnchor}`);
  const stop = source.indexOf(endAnchor, at);
  assert.ok(stop > at, `end anchor not found after ${startAnchor}: ${endAnchor}`);
  return source.slice(at, stop);
}

// ── (1) the promise is made, in every locale ───────────────────────────────

test("(1) the transparency promise is complete in all five locales", () => {
  const entry = DICT["media.imageStorage"];
  assert.ok(
    entry,
    "DICT is missing media.imageStorage — the merchant is never told that a transparent " +
      "PNG keeps its transparency",
  );
  for (const lang of LANGS) {
    const text = entry[lang];
    assert.ok(
      typeof text === "string" && text.trim().length > 0,
      `media.imageStorage.${lang} is empty — every locale must carry the promise`,
    );
    for (const fragment of REQUIRED[lang]) {
      assert.ok(
        text.includes(fragment),
        `media.imageStorage.${lang} does not carry ${JSON.stringify(fragment)}: ${JSON.stringify(text)}`,
      );
    }
  }
});

// ── (2) it is shown where the merchant uploads ─────────────────────────────

test("(2) the media page shows the transparency promise inside the upload zone", () => {
  const zone = slice(mediaPage, 't("media.dropHint")', "accept={ACCEPTED_TYPES.join");
  assert.ok(
    zone.includes('t("media.imageStorage")'),
    "the drag-and-drop upload zone no longer renders the image-storage disclaimer — a " +
      "merchant uploading a transparent logo would not be told what it is stored as",
  );
  assert.ok(
    zone.indexOf('t("media.imageStorage")') < zone.indexOf("{uploadMsg &&"),
    "the image-storage disclaimer renders after the upload result instead of before it",
  );
});

// ── (3) the logo picker uploads through the same optimiser ─────────────────

test("(3) the logo picker shows the same promise", () => {
  assert.ok(
    mediaPicker.includes('t("media.imageStorage")'),
    "MediaPicker uploads through /uploads/image but no longer states what the optimiser " +
      "does to a transparent image",
  );
});

// ── (4) agents are told too ────────────────────────────────────────────────

test("(4) the MCP docs row for upload_media states the PNG/transparency rule", () => {
  const row = slice(docsMCP, '{ name: "upload_media"', '{ name: "delete_media"');
  assert.ok(
    row.includes("PNG") && row.includes("lutsinar"),
    "the upload_media docs row no longer states that an image with a transparent " +
      "background is stored as PNG (an agent would expect a .jpg back)",
  );
});
