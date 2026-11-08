import type { Metadata } from "next";

import { GuideArticle } from "../../../../components/GuideArticle";
import { docsMetadata } from "../../../../lib/docsMetadata";
import { guideBySlug } from "../../../../lib/guides.gen";

// Source: docs/guides/payment-fees/README.md + its 6 screenshots, materialised by
// web/scripts/guides.mjs into src/lib/guides.gen.ts and web/public/guides/payment-fees/.
// A static page rather than a [slug] route — see the note in components/GuideArticle.tsx.
const guide = guideBySlug("payment-fees");

export const metadata: Metadata = docsMetadata({
  title: "Payment fees — screenshot guide — JomForm Docs",
  description: guide.summary,
  path: "/docs/guides/payment-fees",
});

export default function PaymentFeesGuidePage() {
  return <GuideArticle guide={guide} />;
}
