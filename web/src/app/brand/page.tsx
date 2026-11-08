import type { Metadata } from "next";
import BrandPage from "./brand-client";
import { OG_IMAGE, OG_IMAGE_URL } from "../../lib/ogImage";

// Issue #276: per-page meta for the brand/marketing page.
export const metadata: Metadata = {
  title: "Brand assets — JomForm",
  description: "JomForm logos, colors, and usage guidelines.",
  alternates: { canonical: "https://jomform.com/brand" },
  openGraph: {
    title: "Brand assets — JomForm",
    description: "JomForm logos, colors, and usage guidelines.",
    url: "https://jomform.com/brand",
    siteName: "JomForm",
    type: "website",
    // Issue #411: brand og:image so sharing shows a preview. Content-versioned
    // URL (og-default-<hash>.png) so a client that cached an older card is
    // forced to the new file.
    images: [OG_IMAGE],
  },
  twitter: {
    card: "summary_large_image",
    title: "Brand assets — JomForm",
    description: "JomForm logos, colors, and usage guidelines.",
    images: [OG_IMAGE_URL],
  },
};

export default function Page() {
  return <BrandPage />;
}
