/**
 * JomForm web — Next.js 16 config.
 *
 * Cache headers for immutable static assets (/_next/static/*):
 * Content-hashed filenames mean these files never change for a given build,
 * so they are safe to cache for a year both in browsers and at the Cloudflare
 * edge. An explicit header pins this even if Next defaults ever change, and
 * gives Cloudflare an explicit Cache-Control to honor for Edge TTL
 * (standard caching) instead of relying on extension-based defaults.
 *
 * HTML must NOT be cached at the edge with a long TTL — it references the
 * content-hashed chunk names and changes on every deploy.
 * @type {import('next').NextConfig}
 */
const nextConfig = {
  async headers() {
    return [
      {
        source: "/_next/static/:path*",
        headers: [
          {
            key: "Cache-Control",
            value: "public, max-age=31536000, immutable",
          },
        ],
      },
    ];
  },
};

export default nextConfig;