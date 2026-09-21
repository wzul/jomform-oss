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
const nextConfig = {
  output: "standalone",
  agentRules: false,
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