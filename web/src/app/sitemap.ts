import type { MetadataRoute } from "next";

// Issue #276: sitemap.xml for the jomform.com apex.
//
// Lists the apex marketing pages plus the indexed custom-domain spaces. The
// indexed custom-domain space URLs come from the Go API (which owns the
// account_domains + space_settings tables); the web app merges them in so a
// single sitemap.xml covers the apex + all spaces that opted into indexing.
const API = process.env.JOMFORM_API_URL || "http://localhost:8082";

const APEX_PAGES = [
  "",
  "/legal/privacy",
  "/legal/terms",
  "/brand",
  "/mcp",
];

export const dynamic = "force-dynamic";

export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  const entries: MetadataRoute.Sitemap = APEX_PAGES.map((p) => ({
    url: `https://jomform.com${p}`,
    changeFrequency: p === "" ? "weekly" : "monthly",
    priority: p === "" ? 1.0 : 0.5,
  }));

  // Merge indexed custom-domain spaces from the Go API. Best-effort: if the
  // API is unreachable, fall back to the apex pages only.
  try {
    const r = await fetch(`${API}/sitemap-spaces.json`, {
      cache: "no-store",
      signal: AbortSignal.timeout(3000),
    });
    if (r.ok) {
      const data = (await r.json()) as { spaces?: string[] };
      for (const url of data.spaces ?? []) {
        entries.push({ url, changeFrequency: "weekly", priority: 0.8 });
      }
    }
  } catch {
    // ignore — apex-only sitemap is still valid
  }

  return entries;
}
