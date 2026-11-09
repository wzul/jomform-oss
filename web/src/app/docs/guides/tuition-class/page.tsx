import type { Metadata } from "next";

import { GuideArticle } from "../../../../components/GuideArticle";
import { docsMetadata, guideTabTitle } from "../../../../lib/docsMetadata";
import { guideBySlug } from "../../../../lib/guides.gen";

// Source: docs/guides/tuition-class/README.md + its screenshots, materialised by
// web/scripts/guides.mjs into src/lib/guides.gen.ts and web/public/guides/tuition-class/.
// A static page rather than a [slug] route — see the note in components/GuideArticle.tsx.
const guide = guideBySlug("tuition-class");

export const metadata: Metadata = docsMetadata({
  title: guideTabTitle(guide.title),
  description: guide.summary,
  path: "/docs/guides/tuition-class",
});

export default function TuitionClassGuidePage() {
  return <GuideArticle guide={guide} />;
}
