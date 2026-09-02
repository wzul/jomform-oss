# JomForm

**Order-form builder for Malaysian social sellers — open source, forever free.**

Bina borang tempahan yang cantik dan pantas, terima bayaran melalui [CHIP](https://chip-in.asia) (FPX, kad, e-wallet, DuitNow QR), urus jualan — semua tanpa website. Control everything from your own AI agent via the built-in MCP server, or use the web dashboard.

- 🄯 Open source (Apache-2.0) — self-host bebas, source code percuma
- 💳 Payments: **CHIP Collect** — one-time **and** subscription/recurring via CHIP card tokenization
- 🤖 **MCP server built-in** — merchants connect their own AI agent (Claude, dsb.) and run the whole store through tools
- ⚡ Modern stack: Go 1.26 · PostgreSQL 18 · Valkey 9 · Next.js 16 (LTS) · MinIO/S3
- 🇲🇾 BM-first UI, e-Invoice-ready data model

> Status: planning → MVP development. See `docs/SPEC.md`.

Additional reads: `docs/SES-REPUTATION.md` (issue #56: SES bounce/complaint
suppression, verification circuit breaker, MX pre-check, SNS feedback
endpoint) and `docs/PROXY-DECISION.md`.

## Repo layout (planned monorepo)

```
jomform/
├── api/            # Go backend (REST + MCP server + worker)
├── web/            # Next.js dashboard + form builder
├── render/         # Public order-form renderer (SSR, buyer-facing)
├── deploy/         # docker-compose + Dokploy templates (incl. MinIO)
└── docs/
```

## Components

| Component | Tech |
|---|---|
| `jomform-api` | Go 1.26.x, chi, pgx v5, sqlc |
| MCP server | Official `modelcontextprotocol/go-sdk` (1.7+) — HTTP transport + OAuth, per-account API key scope |
| DB | PostgreSQL 18.x |
| Cache/queue | Valkey 9.1.x (`valkey/valkey:9.1-alpine`) |
| Object storage | Any S3-compatible. Self-host: MinIO (Dokploy). Cloud: Cloudflare R2 |
| Dashboard/builder | Next.js 16 (Active LTS), Tailwind, shadcn/ui |

## License

Apache-2.0 (initial proposal — see discussion).

## Community

Issues and PRs welcome. Project lead: Wan Zulkarnain.