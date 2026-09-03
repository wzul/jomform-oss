# JomForm Roadmap

Last updated: 1 Sep 2026 (MYT)

## ✅ Shipped (production, jomform.com)

- Auth (register/login/2FA-ready, change-password), EN/BM everywhere
- Drag-and-drop form builder + **conditional logic** (equals/contains/>/</not-empty)
- Products (one-time + subscription), **inventory** (atomic stock decrement, 409 sold-out)
- CHIP payments — Woo-parity settings, mode auto from key, validate-on-save,
  top-level redirect/callback (verified against portal DB)
- Per-form CHIP override + fallback to account default
- Recurring auto-charge (hourly sweep, 24h×3 retry, past_due, 7d cancel —
  **no card-token revoke**, merchant can re-activate)
- Skip-capture + capture/release via MCP
- Receipts: SES email + **PDF** + **e-Invoice LHDN fields** (TIN/MSIC per account)
- Sales dashboard, CSV export, analytics (global + per-form, REST + MCP)
- **Outbound webhooks** (HMAC-SHA256 signed, `sale.paid`)
- **Tracking scripts** — global + per-form head injection (sanitised:
  `<script src>`/`<meta>` only; event handlers & cookies stripped)
- Multi-domain per account (`account_domains`), content-based verify,
  **Traefik file-provider route** (auto route + LE cert on verify; removed on delete)
- Real-IP chain (Cloudflare ranges fetched at startup, 12h refresh,
  CF-Connecting-IP / XFF rightmost-trusted rewrite)
- Rate limiting (1 r/s, burst 10, per-IP, CF-aware) on public submit
- Landing page with MCP selling point; dashboard-less capable MCP (16 tools,
  OAuth 2.1 metadata + DCR at mcp.jomform.com)
- AGENTS.md parity rule (every dashboard feature must ship an MCP tool)

## 🔜 Next (priority order)

1. **Merchant-facing polish for announce**
   - Form themes (brand colour, logo upload to MinIO, custom CSS)
   - Public form success/failure redirect customisable per form
   - Form embed snippet (iframe + JS widget)
2. **Webhook + integration depth**
   - Retry queue w/ backoff + delivery log UI (outbound_webhook_deliveries)
   - Webhook event expansion: `form.created`, `sale.failed`, `sub.renewed`
   - Zapier/Make-compatible signed payload docs
3. **e-Invoice v2 (LHDN MyInvois)**
   - Buyer TIN/NRIC field on forms (B2B/B2C split)
   - Submit consolidated invoice to MyInvois sandbox (API v1.0)
4. **MCP/AGENT gaps** (per AGENTS.md parity rule)
   - `set_einvoice`, webhook CRUD tools, `create_api_key` (self-service)
   - SSE/streaming tools for live sale feed to agents
5. **Ops hardening**
   - select/scan column-count CI test (3rd 401-storm prevention)
   - `/healthz` apex router into Traefik compose (currently manual dynamic file)
   - PG backups (wal-g to R2), restore drill
6. **Growth**
   - Templates gallery (F&B, tuition, booking)
   - Public gallery + marketplace of form templates

## ❌ Scrapped / consciously dropped

- **Cloudflared tunnel** (hosted or merchant-run) — replaced by Traefik
  file-provider domain routing; merchant CF token storage no longer needed
- Any OnPay identity (repo stays clean)

## Architecture notes (kept)

- Money = integer sen; server-side price authority
- All CHIP create-purchase fields TOP-LEVEL
- DB select/scan must stay column-aligned; verify on every migration
- MCP: register in `registerTools()` (handlers alone won't ship)
- Never auto-revoke card tokens
- Latest stable Go; bump builder image with go.mod floor