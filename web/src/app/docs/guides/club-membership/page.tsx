import type { Metadata } from "next";

import { GuideArticle } from "../../../../components/GuideArticle";
import { docsMetadata, guideTabTitle } from "../../../../lib/docsMetadata";
import { guideBySlug } from "../../../../lib/guides.gen";

// Source: docs/guides/club-membership/README.md + its screenshots, materialised by
// web/scripts/guides.mjs into src/lib/guides.gen.ts and web/public/guides/club-membership/.
// A static page rather than a [slug] route — see the note in components/GuideArticle.tsx.
const guide = guideBySlug("club-membership");

export const metadata: Metadata = docsMetadata({
  title: guideTabTitle(guide.title),
  description: guide.summary,
  path: "/docs/guides/club-membership",
});

export default function ClubMembershipGuidePage() {
  return <GuideArticle guide={guide} />;
}
