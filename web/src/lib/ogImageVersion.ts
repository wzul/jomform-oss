// Content version for the default social card (`public/og-default.png`).
//
// WHY THIS EXISTS (card t_ae566270)
//
// The landing and every other public page named the card at ONE fixed URL —
// `https://jomform.com/og-default.png` — while the file behind it was replaced
// IN PLACE whenever the artwork changed (a4883d20: 70,643 bytes, old purple
// gradient → 143,664 bytes, the light order-card design). An unfurl client
// caches per URL, so a link shared before that deploy kept rendering the old
// card out of that client's cache: Telegram caches previews per client,
// WhatsApp per URL (~7 days), X per URL. The origin and the edge were both
// correct and the preview was still wrong, which is the worst shape of bug —
// nothing was broken that a re-deploy could fix.
//
// So the URL is now derived from the BYTES: `og-default-<hash>.png`, where the
// hash is the content hash of the current card. New bytes ⇒ new URL ⇒ a cached
// client is forced to fetch the new file. The unversioned `og-default.png` stays
// on disk (and is still served) so a link already shared elsewhere still points
// at a real image.
//
// WHERE THE PIECES LIVE
//   * this file — the hash → filename rule, PURE, so the generator script and
//     the test share one implementation instead of restating it;
//   * `scripts/og-image-version.mjs` — writes the versioned copy + the
//     `ogImage.gen.ts` constant. Runs from `prebuild`/`predev`, so a build
//     cannot emit a stale URL;
//   * `ogImage.gen.ts` — the generated constant, committed so the tree type-
//     checks before anything is built;
//   * `ogImage.ts` — what the metadata blocks import;
//   * `ogImage.test.ts` — re-derives the hash from the file and fails when the
//     committed constant no longer names the bytes on disk.
//
// This module imports `node:crypto`, so it is for the SCRIPT and the TESTS —
// never import it from a page or a component (the app reads `ogImage.ts`, which
// carries the constant and nothing else).

import { createHash } from "node:crypto";

/** The unversioned card. The source of truth on disk; also kept served. */
export const OG_IMAGE_SOURCE_FILE = "og-default.png";

/**
 * Hex characters of the sha256 kept in the filename. 12 (48 bits) is far more
 * than a collision needs for a handful of hand-drawn cards, and short enough
 * that the URL stays readable in a metadata dump.
 */
export const OG_IMAGE_HASH_CHARS = 12;

/** Full sha256 of the card bytes, lower-case hex. */
export function ogImageDigest(bytes: Uint8Array): string {
  return createHash("sha256").update(bytes).digest("hex");
}

/** The version component: the leading hex characters of {@link ogImageDigest}. */
export function ogImageHash(bytes: Uint8Array): string {
  return ogImageDigest(bytes).slice(0, OG_IMAGE_HASH_CHARS);
}

/**
 * The versioned file name for a hash. Rejects anything that is not the hex the
 * digest produced, so a mistyped or truncated value cannot become a path.
 */
export function ogImageFileName(hash: string): string {
  if (!new RegExp(`^[0-9a-f]{${OG_IMAGE_HASH_CHARS}}$`).test(hash)) {
    throw new Error(
      `og:image version ${JSON.stringify(hash)} is not ${OG_IMAGE_HASH_CHARS} lower-case hex ` +
        `characters — regenerate it with \`npm run og:image\` instead of editing it by hand`,
    );
  }
  return `og-default-${hash}.png`;
}
