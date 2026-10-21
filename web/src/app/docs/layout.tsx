import type { Metadata } from "next";
import Link from "next/link";

// Issue #395: shared layout for the JomForm knowledge base (/docs).
// A left sidebar lists the doc sections; content renders on the right.
// Static server-rendered pages (English-first, like the legal pages).
//
// t_d768342e: the sidebar follows the same categories as the /docs index, so the
// two surfaces describe one structure instead of two. The category labels are the
// English strings (the sidebar chrome is static, like every /docs page); the
// /docs index itself renders the same taxonomy through t().
//
// t_5425c958 (the rebase onto main): main's flat NAV carried two destinations the
// PR's taxonomy did not — /docs/builder (#671) and /docs/api (#680) — and dropping
// either would leave a published page unreachable from the nav. Both are in the
// taxonomy below, which is why this is a merge of the two lists rather than a
// choice between them.

type Section = { href: string; label: string };
type Group = { label: string; sections: Section[] };

const NAV_GROUPS: Group[] = [
  {
    label: "Start here",
    sections: [
      { href: "/docs", label: "Overview & features" },
      { href: "/docs/builder", label: "The form builder" },
    ],
  },
  {
    label: "Workspaces",
    sections: [
      { href: "/docs/workspaces", label: "Workspaces & deleting one" },
      // Card t_eb09b25a, rebased onto main (t_4c58a09e): the audit trail sits
      // with the workspace category, so the sidebar still describes the same
      // structure as the /docs index — docsIndex.test.ts fails the build when
      // a page the index links is absent from this list.
      { href: "/docs/activity", label: "Activity log" },
    ],
  },
  {
    label: "Sales & payments",
    sections: [
      { href: "/docs/sales", label: "Sales, responses & analytics" },
      { href: "/docs/subscriptions", label: "Subscriptions & recurring billing" },
    ],
  },
  {
    label: "Forms & fields",
    sections: [
      { href: "/docs/customize", label: "Design → AI agent → MCP" },
      { href: "/docs/custom-forms", label: "Custom HTML forms" },
      { href: "/docs/variations", label: "Variation products" },
    ],
  },
  {
    label: "Storefront & pages",
    sections: [{ href: "/docs/embed", label: "Embed on your site" }],
  },
  {
    label: "Email",
    sections: [
      { href: "/docs/email", label: "Email & notifications" },
      { href: "/docs/placeholders", label: "Placeholders reference" },
      { href: "/docs/ses-reputation", label: "Email suppression list" },
    ],
  },
  {
    label: "Security",
    sections: [
      { href: "/docs/spam-detection", label: "AI spam detection" },
      { href: "/docs/security", label: "Trusted devices (2FA)" },
    ],
  },
  {
    label: "For developers",
    sections: [
      { href: "/docs/mcp", label: "MCP tool reference" },
      { href: "/docs/api", label: "REST API reference" },
      { href: "/docs/chat", label: "In-app assistant" },
      { href: "/docs/feedback", label: "In-app feedback" },
    ],
  },
];

// Issue #513: deep links into the guide sections the dashboard's inline "?"
// buttons target, so the features are reachable from the docs nav.
const NAV_SECTIONS = [
  { href: "/docs/customize#affiliate-marketing", label: "Affiliate marketing guide" },
  { href: "/docs/customize#menu", label: "Restaurant menu guide" },
  { href: "/docs/customize#menu-basket", label: "Menu basket guide" },
  { href: "/docs/customize#language", label: "Language guide" },
  { href: "/docs/customize#workspace-address", label: "Reserved workspace names" },
  { href: "/docs/sales#realtime-notifications", label: "Notification beep guide" },
  { href: "/docs/sales#analytics", label: "Per-form counter guide" },
  { href: "/docs#workspaces", label: "Workspaces guide" },
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
      {/* t_d768342e / axe `landmark-unique`: the layout holds two <nav>
          elements, so each needs its own accessible name to be told apart
          (this one is the site bar, the one below is the docs sidebar). */}
      <nav
        aria-label="JomForm site"
        className="flex items-center justify-between border-b border-slate-100 px-5 py-3"
      >
        <Link href="/" className="text-sm font-medium text-slate-600 hover:text-slate-900">
          ← Back to JomForm
        </Link>
        <span className="text-sm font-semibold text-slate-900">JomForm Docs</span>
      </nav>

      <div className="mx-auto flex max-w-6xl gap-8 px-6 py-10">
        <aside className="hidden w-56 shrink-0 md:block">
          {/* axe `landmark-unique`: the second landmark needs a name too. */}
          <nav aria-label="Docs sections" className="sticky top-10 space-y-1">
            {NAV_GROUPS.map((g) => (
              <div key={g.label} className="pt-3 first:pt-0">
                <p className="px-3 text-[11px] font-semibold uppercase tracking-wide text-slate-600">
                  {g.label}
                </p>
                {g.sections.map((n) => (
                  <Link
                    key={n.href}
                    href={n.href}
                    className="mt-1 block rounded-md px-3 py-2 text-sm text-slate-600 hover:bg-slate-50 hover:text-slate-900"
                  >
                    {n.label}
                  </Link>
                ))}
              </div>
            ))}
            {/* Issue #513: deep links into the guide sections. */}
            <div className="pt-3">
              {/* axe `color-contrast`: text-slate-400 is 2.63:1 on the white
                  page at 11px, against a 4.5:1 AA minimum. slate-600 is
                  7.58:1 and still reads as a quieter heading than the links
                  it introduces. */}
              <p className="px-3 text-[11px] font-semibold uppercase tracking-wide text-slate-600">
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
