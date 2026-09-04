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
];

export const dynamic = "force-dynamic";

export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  return APEX_PAGES.map((p) => ({
    url: `https://jomform.com${p}`,
    changeFrequency: p === "" ? "weekly" : "monthly",
    priority: p === "" ? 1.0 : 0.5,
  }));
}
