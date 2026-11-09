import type { Metadata } from "next";
import Link from "next/link";
import { DocsBreadcrumb } from "./docsBreadcrumb";
import { DocsNavLink } from "./docsNavLink";
import { DocsNavSearch } from "./docsNavSearch";
import { DocsPager } from "./docsPager";
import { DocsRelated } from "./docsRelated";
import { DocsSearch } from "./docsSearch";
import { DocsToc } from "./docsToc";
import { OG_IMAGE, OG_IMAGE_URL } from "../../lib/ogImage";

// Issue #395: shared layout for the JomForm knowledge base (/docs).
//
// t_45fd1bee made this file the /docs CHROME rather than a sidebar plus whatever each
// page happened to render:
//
//   * three layers (Learn · Product docs · Reference), the Stripe shape — the sidebar
//     groups its rows under them instead of one long list;
//   * a breadcrumb, an "On this page" table of contents and a previous/next pair on
//     EVERY page, rendered here once from the registry (src/lib/docsNav.ts);
//   * a search field in the sidebar and in the top bar, filtering a build-time index
//     (src/lib/docsSearch.gen.json) in memory — typing issues no request.
//
// WHAT STAYS AUTHORED HERE. `NAV_GROUPS` is the one list of pages and their order.
// docsIndex.test.ts reads it as SOURCE TEXT (it is the guard that proves every sidebar
// row reaches a real page that the /docs index also links), and scripts/docsNav.mjs
// reads the same shape to emit the registry. A row's optional `guides` names the
// screenshot guides for that page; the guide pages link back automatically.
//
// The category labels are the English strings (the sidebar chrome is static, like every
// /docs page); the /docs index itself renders the same taxonomy through t().

type Section = {
  href: string;
  label: string;
  /** Related screenshot guides (`/docs/guides/<slug>`); rendered at the end of the page. */
  guides?: string[];
};
type Group = {
  /** The top-level layer this group belongs to — one header per layer in the sidebar. */
  layer: "Learn" | "Product docs" | "Reference";
  label: string;
  sections: Section[];
};

const NAV_GROUPS: Group[] = [
  {
    layer: "Learn",
    label: "Start here",
    sections: [
      { href: "/docs", label: "Overview & features" },
      { href: "/docs/builder", label: "The form builder", guides: ["/docs/guides/qr-table-ordering", "/docs/guides/tuition-class", "/docs/guides/wedding-rsvp"] },
      { href: "/docs/guides", label: "Screenshot guides" },
    ],
  },
  {
    layer: "Product docs",
    label: "Workspaces",
    sections: [
      { href: "/docs/workspaces", label: "Workspaces & deleting one" },
      // Card t_eb09b25a: the audit trail sits with the workspace category, so the
      // sidebar still describes the same structure as the /docs index — docsIndex.test.ts
      // fails the build when a page the index links is absent from this list.
      { href: "/docs/activity", label: "Activity log" },
    ],
  },
  {
    layer: "Product docs",
    label: "Sales & payments",
    sections: [
      { href: "/docs/sales", label: "Sales, responses & analytics", guides: ["/docs/guides/qr-table-ordering", "/docs/guides/donations", "/docs/guides/wedding-rsvp", "/docs/guides/tuition-class"] },
      { href: "/docs/subscriptions", label: "Subscriptions & recurring billing", guides: ["/docs/guides/club-membership", "/docs/guides/tuition-class"] },
      // Shipping by zone: the managed rate source a form's shipping block can use.
      { href: "/docs/shipping", label: "Shipping by zone" },
      // Card t_135c3c72: the buyer-borne processing fee.
      { href: "/docs/payment-fees", label: "Processing fees paid by the buyer", guides: ["/docs/guides/payment-fees"] },
    ],
  },
  {
    layer: "Product docs",
    label: "Forms & fields",
    sections: [
      { href: "/docs/customize", label: "Design → AI agent → MCP", guides: ["/docs/guides/affiliate-programme", "/docs/guides/qr-table-ordering", "/docs/guides/design-with-mcp"] },
      { href: "/docs/custom-forms", label: "Custom HTML forms" },
      { href: "/docs/variations", label: "Variation products" },
    ],
  },
  {
    layer: "Product docs",
    label: "Storefront & pages",
    sections: [
      { href: "/docs/embed", label: "Embed on your site", guides: ["/docs/guides/donations", "/docs/guides/wedding-rsvp"] },
    ],
  },
  {
    layer: "Product docs",
    label: "Email",
    sections: [
      { href: "/docs/email", label: "Email & notifications" },
      { href: "/docs/placeholders", label: "Placeholders reference" },
      { href: "/docs/ses-reputation", label: "Email suppression list" },
    ],
  },
  {
    layer: "Product docs",
    label: "Security",
    sections: [
      { href: "/docs/spam-detection", label: "AI spam detection" },
      { href: "/docs/security", label: "Trusted devices (2FA)" },
    ],
  },
  {
    layer: "Product docs",
    label: "In-app & support",
    sections: [
      { href: "/docs/chat", label: "In-app assistant", guides: ["/docs/guides/design-with-mcp"] },
      { href: "/docs/feedback", label: "In-app feedback" },
    ],
  },
  {
    layer: "Reference",
    label: "Reference",
    sections: [
      { href: "/docs/api", label: "REST API reference", guides: ["/docs/guides/design-with-mcp"] },
      { href: "/docs/mcp", label: "MCP tool reference", guides: ["/docs/guides/design-with-mcp"] },
    ],
  },
];

// Issue #513: deep links into the guide sections the dashboard's inline "?" buttons
// target. They are shortcuts, not a layer — rendered collapsed under the Learn layer so
// they stay reachable without turning the sidebar back into one long list.
const NAV_SECTIONS = [
  { href: "/docs/customize#affiliate-marketing", label: "Affiliate marketing guide" },
  { href: "/docs/customize#menu", label: "Restaurant menu guide" },
  { href: "/docs/customize#menu-basket", label: "Menu basket guide" },
  { href: "/docs/customize#language", label: "Language guide" },
  { href: "/docs/customize#workspace-address", label: "Reserved workspace names" },
  { href: "/docs/sales#realtime-notifications", label: "Notification beep guide" },
  { href: "/docs/sales#analytics", label: "Per-form counter guide" },
  { href: "/docs/workspaces#currency", label: "Currency & exchange rates guide" },
  { href: "/docs#workspaces", label: "Workspaces guide" },
];

// t_e21f6147: this block is the SECTION default only. Next merges metadata
// shallowly, so every child page that did not define `openGraph` inherited it
// whole — og:url stayed https://jomform.com/docs for /docs/customize and every
// sibling. Each page under this layout now builds its own block from the title,
// description and route it passes to lib/docsMetadata.ts; this object remains
// the /docs index/section default and the fallback. Keep the card here as
// OG_IMAGE — ogImage.test.ts asserts this file publishes it.
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
    // Issue #411: brand og:image so sharing shows a preview. Content-versioned
    // URL (og-default-<hash>.png) so a client that cached an older card is
    // forced to the new file.
    images: [OG_IMAGE],
  },
  twitter: {
    card: "summary_large_image",
    title: "JomForm Docs — knowledge base",
    description:
      "Documentation for JomForm: every feature, every MCP tool, and all dynamic placeholders.",
    images: [OG_IMAGE_URL],
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
        className="flex items-center justify-between gap-3 border-b border-slate-100 px-4 py-3 sm:px-5"
      >
        <Link href="/" className="shrink-0 text-sm font-medium text-slate-600 hover:text-slate-900">
          ← Back to JomForm
        </Link>
        <div className="flex min-w-0 items-center gap-3">
          <span className="hidden text-sm font-semibold text-slate-900 sm:inline">JomForm Docs</span>
          {/* The one search instance: a compact trigger in the top bar (the only search
              surface a phone reader gets), opening a portalled dialog. The sidebar's
              field dispatches the same event — see DocsNavSearch. */}
          <DocsSearch />
        </div>
      </nav>

      <div className="mx-auto flex w-full max-w-7xl gap-8 px-4 py-8 sm:px-6 lg:py-10">
        <aside className="hidden w-60 shrink-0 md:block">
          <div className="sticky top-10 max-h-[calc(100vh-5rem)] overflow-y-auto pb-10">
            <DocsNavSearch />
            {/* axe `landmark-unique`: the second landmark needs a name too. */}
            <nav aria-label="Docs sections" className="mt-4">
              {NAV_GROUPS.map((g, i) => (
                <div key={g.label} className="pt-3 first:pt-0">
                  {i === 0 || NAV_GROUPS[i - 1].layer !== g.layer ? (
                    <p className="px-3 pb-1 text-jf-2xs font-semibold uppercase tracking-wide text-slate-900">
                      {g.layer}
                    </p>
                  ) : null}
                  <p className="px-3 text-jf-2xs font-semibold uppercase tracking-wide text-slate-600">
                    {g.label}
                  </p>
                  {g.sections.map((n) => (
                    <DocsNavLink key={n.href} href={n.href}>
                      {n.label}
                    </DocsNavLink>
                  ))}
                </div>
              ))}
              {/* Issue #513: deep links into guide sections, collapsed by default. */}
              <details className="mt-3">
                <summary className="cursor-pointer rounded-md px-3 py-2 text-jf-2xs font-semibold uppercase tracking-wide text-slate-600 hover:bg-slate-50">
                  Guides & shortcuts
                </summary>
                {NAV_SECTIONS.map((n) => (
                  <DocsNavLink key={n.href} href={n.href}>
                    {n.label}
                  </DocsNavLink>
                ))}
              </details>
            </nav>
          </div>
        </aside>

        <div
          data-docs-content
          className="min-w-0 flex-1 [&_h2]:scroll-mt-24 [&_h3]:scroll-mt-24"
        >
          <DocsBreadcrumb />
          <DocsToc variant="inline" />
          {children}
          <DocsRelated />
          <DocsPager />
        </div>

        <DocsToc variant="sidebar" />
      </div>
    </main>
  );
}
