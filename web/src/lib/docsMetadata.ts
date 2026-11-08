// Per-page Open Graph metadata for the /docs knowledge base.
//
// THE DEFECT THIS EXISTS FOR (card t_e21f6147)
//   Every page under `/docs` exported `title`, `description` and
//   `alternates.canonical` but no `openGraph`. Next merges metadata SHALLOWLY:
//   a nested field such as `openGraph` is not merged property by property — the
//   whole object is inherited from the nearest segment that defines it
//   (https://nextjs.org/docs/app/api-reference/functions/generate-metadata#merging).
//   The /docs layout defines one for the SECTION, so every child page inherited
//   it verbatim: a share of `/docs/customize` previewed as `/docs`, with the
//   right `<title>` and `<meta name="description">` but the section's
//   `og:title`, `og:description` and `og:url`.
//
// WHY A BUILDER AND NOT TWENTY HAND-WRITTEN BLOCKS
//   The same shallow merge cuts the other way: a page that sets `openGraph`
//   REPLACES the layout's object, so a page-local block that forgets
//   `images: [OG_IMAGE]` silently drops the brand card — the layout's `images`
//   do not come back. One builder keeps the page's own title / description /
//   url and the shared card in the same object, so a page cannot fix its
//   og:url by breaking its og:image. `docsMetadata.test.ts` fails when a docs
//   page stops using this function, or passes a path that is not its own route.
//
// The card itself stays content-versioned: `OG_IMAGE` / `OG_IMAGE_URL` come from
// `lib/ogImage.ts` (never a literal), so `og:image.test.ts`'s rule holds here too.

import type { Metadata } from "next";

import { OG_IMAGE, OG_IMAGE_URL } from "./ogImage.ts";
import { SITE_NAME, SITE_URL } from "./siteName.ts";

/** The apex, without a trailing slash — canonicals in this tree have none. */
const ORIGIN = SITE_URL.replace(/\/+$/, "");

export type DocsMetadataInput = {
  /** The page's own `<title>`; also becomes its `og:title` / `twitter:title`. */
  title: string;
  /** The page's own description; also `og:description` / `twitter:description`. */
  description: string;
  /** The page's own route, e.g. `/docs/customize` — its canonical and og:url. */
  path: string;
};

/**
 * The metadata for one /docs page: its own title, description and URL, plus the
 * shared social card. Import it in the page's `metadata` export:
 *
 *   export const metadata: Metadata = docsMetadata({
 *     title: "...",
 *     description: "...",
 *     path: "/docs/<slug>",
 *   });
 */
export function docsMetadata({ title, description, path }: DocsMetadataInput): Metadata {
  // A path that is not a /docs route would publish a canonical and an og:url
  // pointing somewhere the page is not — fail at build time instead, loudly.
  if (path !== "/docs" && !/^\/docs\/[a-z0-9-]+(\/[a-z0-9-]+)*$/.test(path)) {
    throw new Error(
      `docsMetadata: path ${JSON.stringify(path)} is not a /docs route. Pass the page's own ` +
        `route with no trailing slash (e.g. "/docs/customize").`,
    );
  }

  const url = `${ORIGIN}${path}`;

  return {
    title,
    description,
    alternates: { canonical: url },
    openGraph: {
      title,
      description,
      url,
      siteName: SITE_NAME,
      type: "website",
      images: [OG_IMAGE],
    },
    twitter: {
      card: "summary_large_image",
      title,
      description,
      images: [OG_IMAGE_URL],
    },
  };
}
