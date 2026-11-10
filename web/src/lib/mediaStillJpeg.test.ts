// t_d55ce0d4: an animated GIF upload is stored as a still JPEG — the optimiser
// decodes the first frame and JPEG-encodes it — while the handler's comment
// claimed GIFs were "stored as-is" and nothing told the merchant.
//
// THE DECISION (recorded on the card): keep the conversion and SAY SO where the
// merchant uploads. The stored cap is 2MB and real animated GIFs are routinely
// bigger, so storing the original bytes would either reject most of them or
// break the stored-size invariant the quota is built on; preserving animation is
// a product/storage decision, not a bug fix.
//
// So this test guards the half that IS a bug fix: the loss is no longer silent.
//   (1) the sentence exists in all FIVE locales and names JPEG + the animation
//       loss in each (a half-translated disclaimer is not a disclaimer);
//   (2) the media page shows it in the upload zone — before the upload;
//   (3) uploading a GIF reports it at the moment of success, not just in the
//       hint ("Uploaded ✓" for a GIF would be the same silence again);
//   (4) the logo picker, which uploads through the same endpoint, shows it;
//   (5) the picker's own SUCCESS message branches on image/gif like the media
//       page — the hint before the upload is not the result line after it;
//   (6) the MCP docs row for upload_media states it, so an agent uploading on
//       the merchant's behalf is told too (the Go parity test keeps that row
//       equal to the registered tool description).
//
// The server side of the same decision is pinned by
// api/internal/httpapi/media_animation_test.go, which uploads a real 2-frame
// GIF through the handler and requires a single still JPEG back.
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
  fileURLToPath(new URL("../lib/mcpToolGroups.ts", import.meta.url)),
  "utf8",
);

// What each locale must say. The two animation fragments are that language's
// words for "animated GIF" and "first frame" — a locale that kept only the
// English sentence (or dropped the animation half) fails here. The stored
// FORMAT is asserted separately: t_ffc7f502 keeps a transparent image as PNG, so
// only the pre-upload storage sentence names JPEG; the GIF success message names
// the loss (a still image), not a format.
const REQUIRED: Record<Lang, string[]> = {
  en: ["animated GIF", "first frame"],
  ms: ["GIF animasi", "bingkai pertama"],
  zh: ["动画", "第一帧"],
  ta: ["அனிமேஷன்", "முதல் சட்டக"],
  ar: ["المتحرك", "الإطار الأول"],
};

function slice(source: string, startAnchor: string, endAnchor: string): string {
  const at = source.indexOf(startAnchor);
  assert.ok(at >= 0, `anchor not found: ${startAnchor}`);
  const stop = source.indexOf(endAnchor, at);
  assert.ok(stop > at, `end anchor not found after ${startAnchor}: ${endAnchor}`);
  return source.slice(at, stop);
}

// ── (1) the disclaimer exists, in every locale ─────────────────────────────

test("(1) the still-JPEG disclaimer is complete in all five locales", () => {
  for (const key of ["media.imageStorage", "media.uploadedGifStill"] as const) {
    const entry = DICT[key];
    assert.ok(entry, `DICT is missing ${key} — the merchant is not told the GIF becomes a still image`);
    for (const lang of LANGS) {
      const text = entry[lang];
      assert.ok(
        typeof text === "string" && text.trim().length > 0,
        `${key}.${lang} is empty — every locale must carry the disclaimer`,
      );
      for (const fragment of REQUIRED[lang]) {
        assert.ok(
          text.includes(fragment),
          `${key}.${lang} does not carry ${JSON.stringify(fragment)}: ${JSON.stringify(text)}`,
        );
      }
    }
  }
  // The storage sentence still has to name the format an opaque image gets:
  // "we keep your transparency" is not a licence to stop saying what happens to
  // everything else.
  for (const lang of LANGS) {
    assert.ok(
      DICT["media.imageStorage"][lang].includes("JPEG"),
      `media.imageStorage.${lang} no longer names JPEG — an opaque image is still stored as JPEG`,
    );
  }
});

// ── (2) it is shown where the merchant uploads ─────────────────────────────

test("(2) the media page shows the disclaimer inside the upload zone", () => {
  const zone = slice(mediaPage, 't("media.dropHint")', "accept={ACCEPTED_TYPES.join");
  assert.ok(
    zone.includes('t("media.imageStorage")'),
    "the drag-and-drop upload zone no longer renders the still-JPEG disclaimer — " +
      "the merchant would only find out after uploading",
  );
  // Order matters: the disclaimer must come BEFORE the upload message, i.e. it
  // is read before an upload happens, not as a post-hoc explanation.
  assert.ok(
    zone.indexOf('t("media.imageStorage")') < zone.indexOf("{uploadMsg &&"),
    "the still-JPEG disclaimer renders after the upload result instead of before it",
  );
});

// ── (3) a GIF upload says so at the moment of success ──────────────────────

test("(3) a successful GIF upload reports the still-JPEG conversion", () => {
  assert.match(
    mediaPage,
    /file\.type === "image\/gif"\s*\?\s*"media\.uploadedGifStill"\s*:\s*"media\.uploaded"/,
    'the upload success message no longer branches on image/gif — an animated GIF would ' +
      'report a bare "Uploaded ✓" again, which is the silence this card fixes',
  );
});

// ── (4) the logo picker uploads through the same optimiser ─────────────────

test("(4) the logo picker shows the same disclaimer", () => {
  assert.ok(
    mediaPicker.includes('t("media.imageStorage")'),
    "MediaPicker uploads through /uploads/image but no longer states that the image is " +
      "stored as a still JPEG",
  );
});

// ── (5) the picker's own success line branches too ─────────────────────────
//
// The picker uploads through the SAME endpoint and renders the same pre-upload
// hint, but its success line used to be the generic `t("media.uploaded")` — so
// a GIF uploaded from the logo picker answered a bare "Uploaded ✓" while /media
// answered "…saved as a still image (first frame only)". Item (4) above only
// pins the hint, which is why that slipped through.

test("(5) the logo picker's success message branches on image/gif like the media page", () => {
  // Scope the assertion to uploadFile's body: a ternary parked anywhere else in
  // the file would not be the line that runs after an upload.
  const uploader = slice(mediaPicker, "async function uploadFile", "if (!open) return null;");
  assert.ok(
    uploader.includes("setUploadMsg"),
    "the MediaPicker uploadFile slice does not contain the success line — the slice " +
      "anchors moved and this guard would pass vacuously",
  );
  assert.match(
    uploader,
    /file\.type === "image\/gif"\s*\?\s*"media\.uploadedGifStill"\s*:\s*"media\.uploaded"/,
    "the logo picker's upload success message no longer branches on image/gif — an animated " +
      'GIF uploaded from the picker would report a bare "Uploaded ✓" again',
  );
  // The generic line must not survive as a second, unconditional assignment: a
  // branch followed by an unconditional reset would still leave the bare message.
  assert.ok(
    !/setUploadMsg\(t\("media\.uploaded"\)\)/.test(uploader),
    'the picker still sets t("media.uploaded") unconditionally somewhere in uploadFile',
  );
});

// ── (6) agents are told too ────────────────────────────────────────────────

test("(6) the MCP docs row for upload_media states the GIF conversion", () => {
  const row = slice(docsMCP, '{ name: "upload_media"', '{ name: "delete_media"');
  assert.ok(
    row.includes("an animated GIF becomes a still image (first frame only)"),
    "the upload_media docs row no longer states that an animated GIF is stored as one still " +
      "image (first frame only)",
  );
});
