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
  },
};

export default function Page() {
  return <BrandPage />;
}
