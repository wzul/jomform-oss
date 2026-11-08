// The default social card, as the page metadata names it.
//
// ONE constant for every metadata block, and its URL carries the card's content
// hash (`og-default-<hash>.png`). Why the version matters, and why it is derived
// from the bytes rather than from the release version, is documented in
// `ogImageVersion.ts` and `scripts/og-image-version.mjs`; the short form is that
// unfurl clients cache per URL, so a card replaced in place at a fixed URL keeps
// showing as the old preview in every client that already saw it (t_ae566270).
//
// The value itself is GENERATED into `ogImage.gen.ts` from the file on disk, so
// swapping `public/og-default.png` moves the URL without anyone remembering to
// edit this file. Read `OG_IMAGE_URL` / `OG_IMAGE` from here in page metadata —
// never write the path out again, and never import `ogImageVersion.ts` (it pulls
// in node:crypto and is for the build script and the tests only).
//
//   openGraph: { images: [OG_IMAGE] }
//   twitter:   { images: [OG_IMAGE_URL] }

import { SITE_URL } from "./siteName.ts";
import { OG_IMAGE_FILE, OG_IMAGE_HASH } from "./ogImage.gen.ts";

export { OG_IMAGE_FILE, OG_IMAGE_HASH };

/** Absolute URL of the current card: https://jomform.com/og-default-<hash>.png */
export const OG_IMAGE_URL = `${SITE_URL}${OG_IMAGE_FILE}`;

/** The card's published dimensions; they are part of the unfurl contract. */
export const OG_IMAGE_WIDTH = 1200;
export const OG_IMAGE_HEIGHT = 630;

/** Alt text for the card, used by the clients that surface it. */
export const OG_IMAGE_ALT = "JomForm";

/** The `openGraph.images` entry, ready to drop into a metadata block. */
export type OgImage = {
  url: string;
  width: number;
  height: number;
  alt: string;
};

export const OG_IMAGE: OgImage = {
  url: OG_IMAGE_URL,
  width: OG_IMAGE_WIDTH,
  height: OG_IMAGE_HEIGHT,
  alt: OG_IMAGE_ALT,
};
