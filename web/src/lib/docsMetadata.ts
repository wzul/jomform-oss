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

/**
 * The `<title>` of a screenshot guide (`/docs/guides/<slug>`).
 *
 * WHY THIS EXISTS (card t_31ee7580)
 *   Each guide page hand-wrote `"<subject> — screenshot guide — JomForm Docs"`, so its tab
 *   read TWO em dashes of the same kind ("Collect donations — screenshot guide — JomForm
 *   Docs") while the other 22 /docs pages carry exactly one. Nothing failed: a title is a
 *   plain string, it type-checks, and no guard looked at it. The tab is one string with one
 *   dash, the shape every sibling page already uses: `"<subject> — JomForm Docs"`.
 *
 * The subject is the guide's own README `# …` line (through `guides.gen.ts`), with its
 * descriptor dropped rather than separated by a second dash. Both authored forms are
 * accepted — `"<subject> — setup guide"` and `"<subject>: setup guide"` — so editing the
 * guide's prose cannot leave the tab behind with a stale subject.
 *
 * It THROWS when the subject itself carries an em dash: the result would be the very
 * double-dash this function exists to prevent, so it fails at build time rather than
 * shipping a tab the reader sees as two separators.
 */
export function guideTabTitle(guideTitle: string): string {
  // The descriptor is the LAST "<sep> … setup guide" run, and only when that run carries
  // no separator of its own — so a subject that legitimately contains a dash is left whole
  // (and then refused below) instead of being silently truncated at its first dash.
  const subject =
    guideTitle.replace(/\s*(?:—|:)\s+(?:(?![—:]).)*\b(?:setup|screenshot)\s+guide\s*$/i, "").trim() ||
    guideTitle.trim();
  const title = `${subject} — JomForm Docs`;

  if (title.split(" — ").length !== 2) {
    throw new Error(
      `guideTabTitle: ${JSON.stringify(guideTitle)} still carries an em dash after its setup-guide ` +
        `descriptor is removed, so the tab would read two of them. Rename the guide's subject ` +
        `(docs/guides/<slug>/README.md, its \`# …\` line) or use a different word than "—".`,
    );
  }
  return title;
}
