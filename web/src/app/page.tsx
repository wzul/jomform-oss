import type { Metadata } from "next";
import Dashboard from "./dashboard-client";

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
    siteName: "JomForm",
    type: "website",
  },
};

export default function Page() {
  return <Dashboard />;
}
