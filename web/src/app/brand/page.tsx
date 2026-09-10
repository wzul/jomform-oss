import type { Metadata } from "next";
import BrandPage from "./brand-client";

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
    // Issue #411: brand og:image so sharing shows a preview.
    images: [{ url: "https://jomform.com/og-default.png", width: 1200, height: 630, alt: "JomForm" }],
  },
  twitter: {
    card: "summary_large_image",
    title: "Brand assets — JomForm",
    description: "JomForm logos, colors, and usage guidelines.",
    images: ["https://jomform.com/og-default.png"],
  },
};

export default function Page() {
  return <BrandPage />;
}
