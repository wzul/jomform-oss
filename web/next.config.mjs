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
//   JOMFORM_DEV_HOSTS=jomform-staging.example.com npm run dev
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
        `Use e.g. "jomform-staging.example.com" — no scheme, no port, no path, no "*".`,
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

const nextConfig = {
  output: "standalone",
  agentRules: false,
  allowedDevOrigins: DEV_HOSTS,
  async headers() {
    const staticHeaders =
      process.env.NODE_ENV === "development"
        ? [{ key: "Cache-Control", value: devCacheHeader }]
        : [{ key: "Cache-Control", value: "public, max-age=31536000, immutable" }];
    return [{ source: "/_next/static/:path*", headers: staticHeaders }];
  },
};

export default nextConfig;