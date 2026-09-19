import type { Metadata } from "next";
import Dashboard from "./dashboard-client";
import { SITE_NAME, websiteJsonLdScript } from "../lib/siteName";

// Issue #276: per-page meta for the landing/marketing page. The dashboard is
// a client component (it shows the Landing page to anonymous visitors), so
// this server component owns the metadata that search engines + AI read.
export const metadata: Metadata = {
  title: "JomForm — Order forms, agent payments & sales for everyone",
  description:
    "Build beautiful order forms, accept CHIP payments, manage sales. Free forever.",
  alternates: { canonical: "https://jomform.com/" },
  openGraph: {
    title: "JomForm — Order forms, agent payments & sales for everyone",
    description:
      "Build beautiful order forms, accept CHIP payments, manage sales. Free forever.",
    url: "https://jomform.com/",
    siteName: SITE_NAME,
    type: "website",
    // Issue #411: brand og:image so sharing the landing shows a preview.
    images: [{ url: "https://jomform.com/og-default.png", width: 1200, height: 630, alt: "JomForm" }],
  },
  twitter: {
    card: "summary_large_image",
    title: "JomForm — Order forms, agent payments & sales for everyone",
    description:
      "Build beautiful order forms, accept CHIP payments, manage sales. Free forever.",
    images: ["https://jomform.com/og-default.png"],
  },
};

export default function Page() {
  return (
    <>
      {/* Site name: the strongest signal Google documents for the name it prints
          above a result's title. The home page is the only page that may carry
          it — Google supports one site name per domain, never per subdirectory —
          so this markup lives here and nowhere else in the app. It is rendered
          into the server HTML because a search crawler never runs the client
          bundle. See ../lib/siteName.ts. */}
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: websiteJsonLdScript() }}
      />
      <Dashboard />
    </>
  );
}
