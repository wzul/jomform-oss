import type { Metadata } from "next";
import { cookies } from "next/headers";
import Dashboard from "./dashboard-client";
import Landing from "./landing/page";
import { SITE_NAME, websiteJsonLdScript } from "../lib/siteName";
import { SESSION_COOKIE } from "../lib/sessionCookie";

// Issue #276: per-page meta for the landing/marketing page. The dashboard is
// a client component (it shows the Landing page to anonymous visitors), so
// this server component owns the metadata that search engines + AI read.
export const metadata: Metadata = {
  title: "JomForm — Order forms, agent payments & sales for everyone",
  description:
    "Build beautiful order forms, accept CHIP payments, manage sales. Open source (MIT) & free forever.",
  alternates: { canonical: "https://jomform.com/" },
  openGraph: {
    title: "JomForm — Order forms, agent payments & sales for everyone",
    description:
      "Build beautiful order forms, accept CHIP payments, manage sales. Open source (MIT) & free forever.",
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
      "Build beautiful order forms, accept CHIP payments, manage sales. Open source (MIT) & free forever.",
    images: ["https://jomform.com/og-default.png"],
  },
};

// WHY THIS PAGE DECIDES "ANONYMOUS" ON THE SERVER
//
// The landing markup used to reach a crawler as one spinner. The dashboard is a
// client component that resolves the session from a fetch, so on the server it
// only ever rendered its loading shell — `/` was served as
// `<main class="...text-slate-400">…</main>` and nothing else. A crawler that
// does not run the client bundle (search, link previews, AI readers) saw a page
// with no `<h1>` and no body copy, which is the weakest possible signal for the
// site name and the page content. Both halves of that are now served from the
// server-rendered HTML.
//
// The split is made HERE, from the session cookie, because the cookie is the one
// signal the server has before any JavaScript runs. The API sets it on login
// (HttpOnly, `jf_session`) and never sends it to a crawler, so:
//
//   * no cookie  -> the marketing landing markup, server-rendered. This is the
//                   crawler path, and it is the branch that carries the `<h1>`
//                   and the brand.
//   * cookie     -> <Dashboard /> unchanged. That component still resolves the
//                   real session from the dashboard-bundle fetch and still
//                   flips to the landing page itself on a 401 or the wedge
//                   timeout, so an expired or revoked session behaves exactly as
//                   before. Nothing about the authenticated path changes.
//
// "use client" components are still rendered to HTML on the server — `/landing`
// serves its `<h1>` that way today — so rendering the SAME component here puts
// real markup in the crawler's response without a second copy to keep in sync.
// The alternative (a prerendered `/(marketing)` route tree) still needs this
// same per-request decision to send a cookie-less visitor there, so it buys a
// duplicate route tree and no extra crawler-visible content.
export default async function Page() {
  const cookieStore = await cookies();
  const hasSession = cookieStore.has(SESSION_COOKIE);

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
      {hasSession ? <Dashboard /> : <Landing />}
    </>
  );
}
