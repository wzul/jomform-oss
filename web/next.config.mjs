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
 *
 * `output: "standalone"` emits a self-contained .next/standalone/ server
 * (server.js + pruned node_modules) consumed by the Dockerfile runtime
 * stage, so the production image does not ship the full node_modules.
 * headers() below still applies in standalone mode — the standalone
 * server (server.js) serves /_next/static with these headers.
 *
 * `agentRules: false` — deliberate, do not re-enable.
 *
 * Next 16.3 ships a writer (`next/dist/server/lib/generate-agent-files.js`,
 * called from `start-server.js` → `ensureAgentRulesForDev`) that, when it
 * detects an AI coding agent in the environment and finds no managed
 * `<!-- BEGIN:nextjs-agent-rules -->` block, scaffolds `AGENTS.md` AND
 * `CLAUDE.md` next to this config. Measured on the tree: a single
 * `npm run dev` in an agent environment (AI_AGENT/CLAUDECODE/CODEX_* …) added
 * both files as untracked, so every agent worktree was dirty before it wrote a
 * line of code — the same class as the tracked `tsconfig.tsbuildinfo` churn
 * (#661).
 *
 * The writer's only value to this repo is a pointer to the version-matched
 * Next docs in `node_modules/next/dist/docs/`, because the repo's real agent
 * guidance lives in the ROOT `AGENTS.md` (preflight gate, MCP + docs parity,
 * sen, urlguard, migration checklist). Scaffolding a second `web/AGENTS.md`
 * would give every agent-context loader a nearer file with none of that in it.
 * `agentRules: false` is the switch Next documents for this
 * (`node_modules/next/dist/docs/01-app/02-guides/ai-agents.md`, "Opting out";
 * typed `agentRules?: boolean` on `NextConfig`), and the root `.gitignore`
 * additionally ignores the three paths Next writes into `web/` so a future
 * Next that drops the switch cannot dirty the tree either.
 * Guard: `web/src/lib/nextManagedFiles.test.ts`.
 * @type {import('next').NextConfig}
 */
// ── JOMFORM_DEV_HOSTS: the hosts a PROXIED `next dev` may be loaded from ──────
//
// WHY THIS EXISTS AT ALL. Next 16 refuses requests to its own internal paths
// (`/_next/*` — `next/dist/server/lib/router-utils/block-cross-site-dev.js`,
// `isInternalEndpoint()`) whose Origin/Referer is cross-site and whose host is
// not allowlisted. `localhost` and `**.localhost` are built in, so this is
// invisible on a laptop and breaks the moment dev sits behind a proxy on a real
// hostname: EVERY dev chunk is then cross-site. Measured on the staging stack —
// the /_next/static/*.js files were delivered 200 but CORS-blocked to the script
// export ("Blocked cross-origin request to Next.js dev resource /_next/hmr"),
// so React never mounted (`window.TURBOPACK` empty, a hydration error overlay on
// screen) and no control on the page responded to a click. The app looked
// broken; the environment was.
//
// WHY IT IS SAFE ON `main`. Every call site of `blockCrossSiteDEV` in
// router-server.js sits inside `if (development)` (verified: lines 207, 336,
// 669), so a built image — which sets NODE_ENV=production — never consults this
// list. It is inert in prod by construction, not by our restraint.
//
// WHY AN ENV VAR rather than a hardcoded hostname. The value is a property of
// the ENVIRONMENT (which proxy fronts this checkout), not of the tree, and there
// is more than one environment: the Dokploy staging stack, a tunnel, and any
// second preview host. A committed literal would have to be edited per
// environment, which is exactly the drift that made this a staging-only
// uncommitted patch the first time. Empty by default, so a plain `npm run dev`
// on localhost behaves as it always did.
//
//   JOMFORM_DEV_HOSTS=jomform-staging.wanzul-hosting.com npm run dev
//   JOMFORM_DEV_HOSTS="a.example.com,b.example.com"           # comma-separated
//
// A bad value fails LOUDLY at config load rather than silently reverting to the
// broken default: a typo that quietly disabled the allowlist would bring back
// the dead-page symptom with nothing in the log to explain it.
const DEV_HOSTS = (process.env.JOMFORM_DEV_HOSTS || "")
  .split(",")
  .map((h) => h.trim().toLowerCase())
  .filter(Boolean);

for (const host of DEV_HOSTS) {
  // A bare hostname: no scheme, no port, no path, no wildcard. Next matches on
  // the parsed hostname, so any of those would be accepted by us and then never
  // match a request — a silent no-op of the exact kind this guard exists for.
  if (!/^[a-z0-9.-]+$/.test(host)) {
    throw new Error(
      `JOMFORM_DEV_HOSTS entry ${JSON.stringify(host)} is not a bare hostname. ` +
        `Use e.g. "jomform-staging.wanzul-hosting.com" — no scheme, no port, no path, no "*".`,
    );
  }
}

// ── the dev cache header ─────────────────────────────────────────────────────
//
// The immutable header is right for a BUILT image (content-hashed names) and
// wrong for `next dev`, where a chunk keeps ONE name across every edit — its
// hash covers the module GRAPH, not the content. With `immutable,
// max-age=31536000` a CDN in front of dev holds the FIRST copy of any chunk
// whose import graph did not change, so the browser keeps running the previous
// code while the origin serves the edit: measured on staging, origin
// `--jf-switch-off: #64748b` vs the edge's `#94a3b8` at the same URL
// (`cf-cache-status: HIT, age: 1663`), and a new i18n key rendering as its own
// raw key string. `no-store` is the honest header for a source that changes in
// place, and it costs nothing in dev.
//
// The escape hatch exists because `next dev` is not the only way this stack
// runs — the same hostname can serve a BUILT image, which wants the immutable
// header back. Default on, so dev is safe by default.
const devCacheHeader =
  process.env.JOMFORM_DEV_CACHE === "immutable"
    ? "public, max-age=31536000, immutable"
    : "no-store, must-revalidate";

// ── baseline security headers for EVERY response ─────────────────────────────
//
// THE DEFECT (card t_1a491389, measured on prod v1.29.0 on 2026-11-09):
//
//   $ curl -sSI https://jomform.com/
//   cache-control: private, no-cache, no-store, max-age=0, must-revalidate
//   x-powered-by: Next.js            ← the ONLY security-relevant line
//
// The Go API on the same product answers /healthz with X-Content-Type-Options,
// Strict-Transport-Security, Referrer-Policy and X-Frame-Options
// (api/internal/httpapi/middleware/secure.go) — but every HTML page a person
// actually opens (/, /docs, /login, the dashboard) comes from THIS app and
// carried none of them, so the product's public surface was protected and its
// front door was not. `poweredByHeader` was also left at Next's default (`true`),
// which is free reconnaissance: the banner names the framework and its major
// version to any scanner picking a known CVE.
//
// WHY ONE CATCH-ALL RULE AND NOT PER-ROUTE. The headers belong to every
// response the app emits: a page added tomorrow is covered without anyone
// remembering to add its route here, and the failure mode of a per-route list is
// silent (the new page simply has no headers, and nothing errors). The static
// rule below is a SECOND entry, not a replacement — Next applies every matching
// rule, so `/_next/static/*` keeps its immutable Cache-Control and gains the
// security headers too.
//
// X-Frame-Options: SAMEORIGIN, and why that does not break embeds.
//   The form embed is `/embed/f/{workspace}/{slug}`, served by the GO API, never
//   by this app: Caddy routes `@api path … /embed/*`
//   (deploy/selfhost/Caddyfile) and Traefik's `jf-api` rule carries
//   `PathPrefix(`/embed/`)` (deploy/vps/traefik-jomform.yml). That route sets its
//   own permissive framing (`X-Frame-Options: ALLOWALL` + `frame-ancestors *`),
//   pinned in Go by `api/internal/httpapi/embed_docs_snippet_test.go`. Nothing in
//   this app frames a Next page either (no `<iframe>` in web/src/app or
//   web/src/components — the only iframe the dashboard renders is a form on the
//   workspace host, i.e. the API). So SAMEORIGIN costs no functionality, and it
//   matches the value the API's own public pages use. It is intentionally NOT
//   `DENY`: a same-origin frame is the one case a future dashboard feature (an
//   in-app preview) may legitimately need, and SAMEORIGIN already refuses every
//   cross-origin framer, which is the clickjacking threat.
//
// CSP: frame-ancestors ONLY, and why no nonce-based script policy.
//   `Content-Security-Policy: frame-ancestors 'self'` is the modern equivalent of
//   the X-Frame-Options line above (browsers prefer CSP where both are present);
//   it constrains FRAMING and cannot affect scripts, styles or network calls, so
//   it cannot break anything. A full script CSP was evaluated and rejected on
//   measured grounds, not taste:
//     * the built HTML contains inline scripts that MUST execute — Next's RSC
//       bootstrap `(self.__next_f=self.__next_f||[]).push([0])`, the Turbopack
//       chunk registry, and our own JSON-LD on `/` (3 on `/`, 2 on `/docs`,
//       measured on the prod v1.29.0 HTML);
//     * a nonce would have to be minted per response and threaded into every one
//       of those tags by Next, which means middleware and `'strict-dynamic'` —
//       and `headers()` here is STATIC (one value per build), so the nonce cannot
//       be produced at this layer at all;
//     * Cloudflare sits in front of prod and can inject its own scripts (Rocket
//       Loader, Web Analytics) from the zone's dashboard with no repo change.
//       Those cannot carry our nonce, so a strict script-src would break the zone
//       silently, in prod, after a settings change nobody reviewed here.
//   Shipping `script-src 'unsafe-inline'` to make that work would be theatre. So
//   this CSP carries the one directive this layer can honestly assert, and the
//   guard test reds if a script/style directive is ever added to it.
//
// Permissions-Policy denies only features the app never requests.
//   camera / geolocation / microphone / usb — no call site uses any of them.
//   `clipboard-write` is deliberately NOT denied: 8 call sites use
//   navigator.clipboard.writeText (Copy link / Copy code on /builder, /media,
//   /chat, /mcp, /brand, /profile). `payment` is left at the browser default too:
//   nothing in this app calls the Payment Request API, but blocking a payment
//   capability is a product decision, not a header tidy-up.
//
// HSTS mirrors the API's value in middleware/secure.go. It is inert over plain
// http (browsers ignore it), so a `http://localhost` self-host instance is
// unaffected, and it closes the one-response http window before the first
// redirect on a real hostname.
//
// Guards: `web/src/lib/securityHeaders.test.ts` (the config Next loads) and
// `web/e2e/security-headers.spec.ts` (real responses from the built app).
const SECURITY_HEADERS = [
  { key: "X-Content-Type-Options", value: "nosniff" },
  { key: "X-Frame-Options", value: "SAMEORIGIN" },
  { key: "Content-Security-Policy", value: "frame-ancestors 'self'" },
  { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
  { key: "Permissions-Policy", value: "camera=(), geolocation=(), microphone=(), usb=()" },
  { key: "Strict-Transport-Security", value: "max-age=31536000; includeSubDomains" },
];

const nextConfig = {
  output: "standalone",
  agentRules: false,
  // Removes `x-powered-by: Next.js` from every response. See the block above.
  poweredByHeader: false,
  allowedDevOrigins: DEV_HOSTS,
  // ── the retired surau guide's URL (card t_1e4c1bba) ────────────────────────
  //
  // /panduan/surau shipped as an unlisted, noindex, nine-step guide and was handed to a surau
  // committee by link (and to Wan's own WhatsApp groups). The guide is gone — the fee model it
  // described changed — but that link is still in people's hands, so the route must LAND
  // somewhere rather than 404. It 301s to the docs page that now carries the setup.
  //
  // 301, not `permanent: true`: Next maps `permanent: true` to **308** (its docs: "if true will
  // use the 308 status code"), and the card asked for a permanent 301. `statusCode` and
  // `permanent` are mutually exclusive in one entry, so this sets `statusCode: 301` alone.
  //
  // Guard: src/lib/panduanRetired.test.ts imports this config and fails if the entry, its 301,
  // or its destination page disappears — a later cleanup must not turn the committee's link
  // back into a 404.
  async redirects() {
    return [
      {
        source: "/panduan/surau",
        destination: "/docs/fees",
        statusCode: 301,
      },
    ];
  },
  async headers() {
    const staticHeaders =
      process.env.NODE_ENV === "development"
        ? [{ key: "Cache-Control", value: devCacheHeader }]
        : [{ key: "Cache-Control", value: "public, max-age=31536000, immutable" }];
    return [
      { source: "/:path*", headers: SECURITY_HEADERS },
      { source: "/_next/static/:path*", headers: staticHeaders },
    ];
  },
};

export default nextConfig;