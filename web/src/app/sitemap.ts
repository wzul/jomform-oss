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
  "/docs/subscriptions",
  "/docs/variations",
  // t_bac29e18: the public REST API reference. It is a product surface — the
  // entry point for a developer integrating with JomForm.
  "/docs/api",
  // Card t_eb09b25a: the activity log — the audit-trail surface the dashboard
  // Activity page links to.
  "/docs/activity",
  // The per-workspace AI spam-detection toggle is documented here
  // and the Settings switch deep-links into it.
  "/docs/spam-detection",
  "/docs/ses-reputation",
];

export const dynamic = "force-dynamic";

export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  return APEX_PAGES.map((p) => ({
    url: `https://jomform.com${p}`,
    changeFrequency: p === "" ? "weekly" : "monthly",
    priority: p === "" ? 1.0 : 0.5,
  }));
}
