import type { MetadataRoute } from "next";

// Issue #276: sitemap.xml for the jomform.com apex.
//
// Lists only the apex marketing pages. Custom-domain spaces serve their own
// /sitemap.xml via the Go API (handleSitemap), so they must NOT be merged
// into the apex sitemap (see issue #289).
const APEX_PAGES = [
  "",
  "/legal/privacy",
  "/legal/terms",
  "/brand",
  "/mcp",
  "/docs",
  // t_d768342e: the guide pages /docs links into. A docs page that is not in the
  // sitemap is a page a search engine will not find (the nav is the other half).
  "/docs/sales",
  "/docs/email",
  "/docs/security",
  // t_5425c958: the workspace guide carries the two entries main added after the
  // /docs restructure was written (what a workspace is, and how deleting one
  // works), and the Settings → Workspace danger zone's "?" deep-links into it.
  "/docs/workspaces",
  // Issue #513: the customize page carries the menu-basket and affiliate
  // guides (the docs nav links into both), so it belongs in the sitemap.
  "/docs/customize",
  "/docs/embed",
  "/docs/placeholders",
  "/docs/mcp",
  // Card t_0356ed4c: /docs/mcp is a hub now; its four task pages are real routes
  // and sitemapCoverage.test.ts requires every page on disk in this list.
  "/docs/mcp/connect",
  "/docs/mcp/tools",
  "/docs/mcp/oauth",
  "/docs/mcp/troubleshooting",
  "/docs/subscriptions",
  "/docs/variations",
  // t_bac29e18: the public REST API reference. It is a product surface — the
  // entry point for a developer integrating with JomForm.
  "/docs/api",
  // Card t_0356ed4c: /docs/api is a hub now; its four task pages are real routes
  // and sitemapCoverage.test.ts requires every page on disk in this list.
  "/docs/api/authentication",
  "/docs/api/endpoints",
  "/docs/api/webhooks",
  "/docs/api/errors",
  // Card t_eb09b25a: the activity log — the audit-trail surface the dashboard
  // Activity page links to.
  "/docs/activity",
  // Shipping by zone: the guide the Form Settings → Shipping tab and the
  // builder's shipping block both link at, and the /docs index carries an entry
  // for it (sitemapCoverage.test.ts pins this list against the pages on disk).
  "/docs/shipping",
  // Card t_135c3c72: the buyer-borne processing fee guide.
  "/docs/payment-fees",
  // Card t_7b9f6d18: the screenshot guides. docs/guides/<slug> is real content that had no
  // route; /docs/guides is the index and the two guides are the pages it lists. They are
  // public, canonical-carrying pages like every other guide (sitemapCoverage.test.ts pins
  // this list against the pages on disk in both directions).
  "/docs/guides",
  "/docs/guides/payment-fees",
  "/docs/guides/donations",
  // Card t_343a45c8: the six showcase guides that landed in docs/guides with PR #1644. They are
  // pages on disk, so sitemapCoverage.test.ts requires an entry for each.
  "/docs/guides/affiliate-programme",
  "/docs/guides/club-membership",
  "/docs/guides/design-with-mcp",
  "/docs/guides/qr-table-ordering",
  "/docs/guides/tuition-class",
  "/docs/guides/wedding-rsvp",
  // The per-workspace AI spam-detection toggle is documented here
  // and the Settings switch deep-links into it.
  "/docs/spam-detection",
  "/docs/ses-reputation",
  // t_0447a70c: the three guide pages reached from a feature's own inline "?"
  // instead of from the /docs index (/docs/builder from the builder,
  // /docs/chat from the chat panel, /docs/feedback from the feedback page).
  // They are public routes with their own canonical, and they were absent here —
  // which made /docs/chat, the target of the chat panel's "?" and the page that
  // documents the created-form/bin boundary, invisible to a crawler. The docs
  // index lists them as not-indexed-by-design (they are reached from the
  // feature), but that is a NAV decision, not a crawler one: the rule this file
  // already states applies to them too — a docs page that is not in the sitemap
  // is a page a search engine will not find.
  // web/src/lib/sitemapCoverage.test.ts now pins this list against the pages on
  // disk, so a new guide cannot be added without its entry here again.
  "/docs/builder",
  "/docs/chat",
  "/docs/feedback",
  // Issue #255's fully-custom HTML response form: the guide the /docs index
  // links (and the sidebar carries) for a feature that is only reachable from
  // the MCP surface. sitemapCoverage.test.ts pins this list against the pages
  // on disk, which is why the entry is added with the page.
  "/docs/custom-forms",
];

export const dynamic = "force-dynamic";

export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  return APEX_PAGES.map((p) => ({
    url: `https://jomform.com${p}`,
    changeFrequency: p === "" ? "weekly" : "monthly",
    priority: p === "" ? 1.0 : 0.5,
  }));
}
