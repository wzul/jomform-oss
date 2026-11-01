# JomForm

**Order-form builder for Malaysian social sellers — free and open source (MIT).**

Bina borang tempahan yang cantik dan pantas, terima bayaran melalui [CHIP](https://chip-in.asia) (FPX, kad, e-wallet, DuitNow QR), urus jualan — semua tanpa website. Control everything from your own AI agent via the built-in MCP server, or use the web dashboard.

- 💳 Payments: **CHIP Collect** — one-time **and** subscription/recurring via CHIP card tokenization
- 🤖 **MCP server built-in** — merchants connect their own AI agent (Claude, dsb.) and run the whole store through tools
- ⚡ Modern stack: Go 1.26 · PostgreSQL 18 · Valkey 9 · Next.js 16 (LTS) · S3-compatible storage
- 🇲🇾 BM-first UI, e-Invoice-ready data model

> Status: planning → MVP development. See `docs/SPEC.md`.

Additional reads: `docs/SES-REPUTATION.md` (issue #56: SES bounce/complaint
suppression, verification circuit breaker, MX pre-check, SNS feedback
endpoint), `docs/SITE-NAME-SEARCH.md` (what the home page's `WebSite` structured
data does — and does not — do for the name Search prints) and
`docs/PROXY-DECISION.md`.

## Self-host it

JomForm runs on your own server, with one command and one env file:

```bash
git clone https://github.com/wzul/jomform.git
cd jomform
bash deploy/selfhost/bootstrap.sh --base-url https://forms.example.com
```

That generates every secret, builds the stack (dashboard + API + PostgreSQL +
Valkey + S3 behind Caddy, which obtains the TLS certificate) and prints the
URL when it is healthy. Docker with the Compose v2 plugin, a hostname pointing at
the server, and ports 80/443 open are the only prerequisites.

Full instructions — first account, backups, upgrades, troubleshooting — are in
**[docs/SELF-HOST.md](docs/SELF-HOST.md)**.

## Repo layout (planned monorepo)

```
jomform/
├── api/            # Go backend (REST + MCP server + worker)
├── web/            # Next.js dashboard + form builder
├── render/         # Public order-form renderer (SSR, buyer-facing)
├── deploy/         # docker-compose + Traefik templates (incl. an S3 server)
└── docs/
```

## Components

| Component | Tech |
|---|---|
| `jomform-api` | Go 1.26.x, chi, pgx v5, sqlc |
| MCP server | Official `modelcontextprotocol/go-sdk` (1.7+) — HTTP transport + OAuth, per-account API key scope |
| DB | PostgreSQL 18.x |
| Cache/queue | Valkey 9.1.x (`valkey/valkey:9.1-alpine`) |
| Object storage | Any S3-compatible. Self-host: the bundled SeaweedFS. Cloud: Cloudflare R2, AWS S3 |
| Dashboard/builder | Next.js 16 (Active LTS), Tailwind, shadcn/ui |

## License

JomForm is open source under the [MIT License](LICENSE). You may use, copy,
modify, merge, publish, distribute, sublicense and sell copies of this software,
provided the copyright notice and this permission notice are included in all
copies or substantial portions of it. The software is provided "as is", without
warranty of any kind.

The hosted service at `jomform.com` is a separate offering governed by the
[Terms of Service](https://jomform.com/legal/terms) — the MIT licence covers the
code in this repository, not the hosted instance.

Copyright (c) 2026 Wan Zulkarnain.

## Community

Issues and PRs welcome. Project lead: Wan Zulkarnain.