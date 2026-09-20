import type { Metadata } from "next";
import Link from "next/link";

// Issue #395: shared layout for the JomForm knowledge base (/docs).
// A left sidebar lists the doc sections; content renders on the right.
// Static server-rendered pages (English-first, like the legal pages).

const NAV = [
  { href: "/docs", label: "Overview & features" },
  { href: "/docs/customize", label: "Design → AI agent → MCP" },
  { href: "/docs/embed", label: "Embed on your site" },
  { href: "/docs/subscriptions", label: "Subscriptions & recurring billing" },
  { href: "/docs/variations", label: "Variation products" },
  { href: "/docs/spam-detection", label: "AI spam detection" },
  { href: "/docs/ses-reputation", label: "Email suppression list" },
  { href: "/docs/placeholders", label: "Placeholders reference" },
  { href: "/docs/mcp", label: "MCP tool reference" },
  { href: "/docs/feedback", label: "In-app feedback" },
];

// Issue #513: deep links into the two sections this change added, so the
// features are reachable from the docs nav (the affiliate tab and the menu
// settings link here with an inline "?" button).
const NAV_SECTIONS = [
  { href: "/docs/customize#affiliate-marketing", label: "Affiliate marketing guide" },
  { href: "/docs/customize#menu-basket", label: "Menu basket guide" },
  { href: "/docs/customize#language", label: "Language guide" },
];

export const metadata: Metadata = {
  title: "JomForm Docs — knowledge base",
  description:
    "Documentation for JomForm: every feature, every MCP tool, and all dynamic placeholders.",
  alternates: { canonical: "https://jomform.com/docs" },
  openGraph: {
    title: "JomForm Docs — knowledge base",
    description:
      "Documentation for JomForm: every feature, every MCP tool, and all dynamic placeholders.",
    url: "https://jomform.com/docs",
    siteName: "JomForm",
    type: "website",
    // Issue #411: brand og:image so sharing shows a preview.
    images: [{ url: "https://jomform.com/og-default.png", width: 1200, height: 630, alt: "JomForm" }],
  },
  twitter: {
    card: "summary_large_image",
    title: "JomForm Docs — knowledge base",
    description:
      "Documentation for JomForm: every feature, every MCP tool, and all dynamic placeholders.",
    images: ["https://jomform.com/og-default.png"],
  },
};

export default function DocsLayout({ children }: { children: React.ReactNode }) {
  return (
    <main className="min-h-screen bg-white">
      <nav className="flex items-center justify-between border-b border-slate-100 px-5 py-3">
        <a href="/" className="text-sm font-medium text-slate-600 hover:text-slate-900">
          ← Back to JomForm
        </a>
        <span className="text-sm font-semibold text-slate-900">JomForm Docs</span>
      </nav>

      <div className="mx-auto flex max-w-6xl gap-8 px-6 py-10">
        <aside className="hidden w-56 shrink-0 md:block">
          <nav className="sticky top-10 space-y-1">
            {NAV.map((n) => (
              <Link
                key={n.href}
                href={n.href}
                className="block rounded-md px-3 py-2 text-sm text-slate-600 hover:bg-slate-50 hover:text-slate-900"
              >
                {n.label}
              </Link>
            ))}
            {/* Issue #513: deep links to the affiliate + menu-basket sections. */}
            <div className="pt-3">
              <p className="px-3 text-[11px] font-semibold uppercase tracking-wide text-slate-400">
                Guides
              </p>
              {NAV_SECTIONS.map((n) => (
                <Link
                  key={n.href}
                  href={n.href}
                  className="mt-1 block rounded-md px-3 py-2 text-sm text-slate-600 hover:bg-slate-50 hover:text-slate-900"
                >
                  {n.label}
                </Link>
              ))}
            </div>
          </nav>
        </aside>
        <div className="min-w-0 flex-1">{children}</div>
      </div>
    </main>
  );
}
