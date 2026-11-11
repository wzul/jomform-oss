import type { Metadata } from "next";

import { PANDUAN_SURAU_METADATA, PanduanSurauArticle } from "../../../lib/panduanSurauPage";

// THE PRIVATE SURAU GUIDE — the route Wan hands to the surau committee.
//
// The page itself lives in src/lib/panduanSurauPage.ts, because the repo's unit runner cannot
// import a .tsx file and this page's guards have to render the REAL article (src/lib/
// panduanSurau.test.ts). This file is the Next route: it re-exports that module's metadata and
// component, so there is one owner for both and the guard asserts exactly what is served.
//
// WHY THE ROUTE IS SHAPED THIS WAY
//   * It is NOT under app/docs. Everything under app/docs is a public, canonical-carrying guide:
//     sitemapCoverage.test.ts requires each of those routes in the apex sitemap, and docsRoutesOnDisk
//     feeds the docs nav + search registry. A page outside that tree is outside all three walks by
//     construction, and no file under web/src links to it (panduanSurau.test.ts greps for that).
//   * It renders the SAME source as the public screenshot guides — docs/guides/<slug>/README.md plus
//     one boxed screenshot per step, materialised by web/scripts/guides.mjs. The generator routes a
//     slug listed in PRIVATE_GUIDES to src/lib/guidesPrivate.gen.ts and public/panduan/surau/
//     instead of GUIDES and public/guides/, so the red-box and alt-text conventions stay identical
//     while /docs/guides can never list it.
export const metadata: Metadata = PANDUAN_SURAU_METADATA;

export default function PanduanSurauPage() {
  return PanduanSurauArticle();
}
