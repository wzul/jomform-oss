import type { Metadata } from "next";

import { GuideArticle } from "../../../../components/GuideArticle";
import { docsMetadata, guideTabTitle } from "../../../../lib/docsMetadata";
import { guideBySlug } from "../../../../lib/guides.gen";

// Source: docs/guides/qr-table-ordering/README.md + its screenshots, materialised by
// web/scripts/guides.mjs into src/lib/guides.gen.ts and web/public/guides/qr-table-ordering/.
// A static page rather than a [slug] route — see the note in components/GuideArticle.tsx.
const guide = guideBySlug("qr-table-ordering");

export const metadata: Metadata = docsMetadata({
  title: guideTabTitle(guide.title),
  description: guide.summary,
  path: "/docs/guides/qr-table-ordering",
});

export default function QrTableOrderingGuidePage() {
  return <GuideArticle guide={guide} />;
}
