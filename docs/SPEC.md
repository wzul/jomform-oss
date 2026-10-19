# JomForm — Technical Specification v0.2

**Status:** Draft untuk semakan · Last updated: 31 Ogos 2026 (MYT)

## 0. Prinsip

1. **Free hosted service** — semua pengguna guna percuma; bukan open source buat masa ini (repo private). Tiada billing engine dalam app; kalau hosted version mahu monetise nanti, itu lapisan berasingan.
2. **Modern look** — UI baharu 2026; tiada warisan visual dari mana-mana produk sedia ada.
3. **CHIP sahaja** — satu-satunya payment gateway. Pay-through model: setiap merchant guna akaun CHIP mereka sendiri (Brand ID + Secret Key, dienkripsi at-rest).
4. **Identiti sendiri** — jenama, copy, navigation, data model semuanya original.
5. **AI-agent first (MCP)** — dashboard ialah optional surface. Semua operasi merchant tersedia sebagai MCP tools; merchant boleh kendalikan kedai 100% melalui AI agent masing-masing.
6. Semua amount = **integer sen**. Go backend. Bukan-USD timezone: Asia/Kuala_Lumpur default per account.

## 1. Stack (semua latest stable)

| Lapisan | Pilihan | Versi |
|---|---|---|
| Backend | Go | 1.26.x |
| DB | PostgreSQL | 18.x (18.6) |
| Cache/queue | Valkey (bukan Redis) | 9.1.x (`valkey/valkey:9.1-alpine`) |
| Storage | S3-compatible: MinIO (self-host) atau R2 | MinIO image `minio/minio` (pulled & verified Aug 2026) |
| Dashboard + builder | Next.js | 16.3.8 (security release, Sep 2026 — patched for GHSA-vcvr-r3jv-pc5j) |
| UI kit | Tailwind CSS + shadcn/ui | latest |
| MCP SDK | `github.com/modelcontextprotocol/go-sdk` | v1.7.x (official) |
| Migrations | golang-migrate | latest |
| Go deps | chi v5 · pgx v5 + sqlc · go-redis/v9 (kekal — API-compatible dengan Valkey) · golang-jwt/v5 · x/crypto argon2id | latest |

Client Valkey: `go-redis/v9` works with Valkey (RESP protocol sama). Kalau mahu native client: `github.com/valkey-io/valkey-go`.

## 2. Arsitektur

```
[Buyer] → render (SSR form, Go) → api (Go)
                                    ├─ PostgreSQL 18 (data)
                                    ├─ Valkey 9 (queue: emails, webhooks, subscription charges)
                                    ├─ S3/MinIO (images, proof, exports)
                                    └─ CHIP Collect API (one-time + recurring)
[Merchant] → web (Next.js dashboard) → api
[Merchant's AI agent] → jomform MCP server (HTTP + OAuth) → api (scope = account)
```

- `api` = satu binary Go: REST `/v1/*`, MCP endpoint `/mcp`, worker loop (Valkey streams).
- Public borang di-host di `jomform.com/f/<slug>`; merchant custom domain (CNAME → render).
- Semua handler idempotent-by-key; webhook handler respond 2xx dahulu, proses async via Valkey queue.

## 3. Data model (Postgres 18)

```
users(id, email UNIQUE, password_hash argon2id, totp_secret, created_at)
accounts(id, owner_user_id FK, name, slug UNIQUE, custom_domain, timezone 'Asia/Kuala_Lumpur', currency 'MYR',
         chip_brand_id, chip_secret_key_enc, chip_mode live|test, bnm_rounding bool DEFAULT false)
api_keys(id, account_id FK, key_hash, name, scopes text[], last_used_at, revoked_at)          -- untuk MCP & API access
products(id, account_id FK, name, description, price_sen, images jsonb, stock int NULL,
         type one_time | subscription,                                      -- ← requirement baharu
         billing_period month|year NULL,          -- subscription sahaja
         trial_days int DEFAULT 0, active bool)
forms(id, account_id FK, slug UNIQUE, title, config jsonb, theme jsonb, status draft|published, is_main bool)
sales(id, account_id FK, form_id FK, ref_code UNIQUE, client jsonb, items jsonb,
      subtotal_sen, shipping_sen, discount_sen, total_sen,
      sale_type one_time | subscription,
      payment_method chip | bank_direct, status pending|paid|confirmed|cancelled|failed,
      chip_purchase_id, chip_checkout_url, invoice_no, created_at)
subscriptions(id, account_id FK, client_email, product_id FK, status active|past_due|cancelled,
              chip_token_purchase_id,   -- Purchase yang membawa is_recurring_token=true
              next_charge_at, period month|year, amount_sen, created_at)
subscription_charges(id, subscription_id FK, chip_purchase_id, amount_sen, status pending|paid|failed, charged_at)
bank_direct_proofs(id, sale_id FK, file_url, paid_at, note, verified_by)
webhook_events(id, account_id FK, source, event_type, payload jsonb, dedup_key UNIQUE, processed_at)
outbound_webhooks(id, account_id FK, url, secret, events text[], active)                      -- merchant receive
audit_logs(id, account_id FK, actor, action, entity, entity_id, diff jsonb, created_at)
```

## 4. Fungsi MVP

**P0 (lancar):**
1. Auth: email+argon2id, 2FA TOTP, session secure-cookie; account slug boleh ditukar (308 redirect lama→baru, method kekal)
2. Produk: one-time **atau subscription** (billing period bulan/tahun, trial days)
3. Form builder: drag-drop blok (header/gambar/teks/produk/penghantaran), live preview, autosave, tema preset, BM/EN
4. Public form: SSR pantas, jualan one-time & langganan
5. Pembayaran CHIP:
   - One-time: `POST /purchases/` → redirect `checkout_url` → verify webhook `purchase.paid`
   - **Subscription**: (a) checkout pertama dengan `remember_card=on` pada `direct_post_url` → Purchase jadi token (`is_recurring_token=true`); (b) simpan Purchase ID sebagai `chip_token_purchase_id`; (c) worker Valkey jadualkan charge: `POST /purchases/{new}/charge/` dengan body `{"recurring_token": "<token_purchase_id>"}`; (d) events: `purchase.paid`, `purchase.pending_charge`, `purchase.payment_failure`, `purchase.pending_recurring_token_delete`, `purchase.recurring_token_deleted` semua dihandle; (e) retry policy: 3 cubaan 24 jam → status `past_due`; (f) delete token → `POST /purchases/{id}/delete_recurring_token/` bila merchant cancel; (g) senarai token client via List Recurring Tokens API untuk audit
   - Bank Direct manual: nota + upload bukti → sah manual
   - BNM rounding toggle per form
6. Jualan: senarai/filter/confirm/cancel, eksport CSV, invoice PDF ringkas
7. Dashboard: ringkasan + graf 30 hari + top produk; dark mode
8. Email transaksional (SMTP): order, pembayaran disahkan, invois langganan
9. **MCP server (P0 — bukan P1)**: tools lihat §5
10. Free semua: tiada plan gating; self-host = unlimited

**P1:** sistem ejen (link, komisen auto, ledger, tuntutan), kupon, auto follow-up, outbound webhooks UI, multi-bahasa penuh, LTV report.
**P2:** e-Invoice MyInvoice (schema siap dari hari 1), dropship/pengedar hierarchy, funnel multi-step, marketplace, PWA admin.

## 5. MCP server (fitur signature)

- Official Go SDK (`modelcontextprotocol/go-sdk` v1.7.x), transport **Streamable HTTP** di `https://api.jomform.com/mcp`
- Auth: bearer per-account **API key** (scopes: `readonly` / `readwrite` / `admin`) — MCP tools hormati scope sama seperti REST
- Tools (v1):
  - `list_products` / `create_product` / `update_product` / `archive_product`
  - `list_forms` / `get_form` / `update_form_config` / `publish_form` / `get_form_stats`
  - `list_sales` / `get_sale` / `confirm_sale` / `cancel_sale` / `export_sales_csv`
  - `create_payment_link` (one-time) / `create_subscription` / `cancel_subscription` / `list_subscriptions` / `list_subscription_charges`
  - `verify_bank_proof` / `get_sales_summary` (angka dashboard)
  - `manage_webhooks` / `manage_bank_settings` (admin scope)
  - Analytics: `get_form_analytics` / `get_product_analytics`
  - Custom domains: `list_domains` / `add_domain` / `verify_domain` /
    `remove_domain` / `auto_cname_domain` / `set_domain_tls_mode` /
    `set_domain_custom_cert` (per-domain TLS config + status, issue #397)
  - Tracking: `get_tracking` / `set_tracking`
  - CHIP account settings: `get_chip_settings` / `set_chip_settings` /
    `clear_chip_settings`
  - Profile avatar: `update_profile_avatar`
  - Passkeys: `list_passkeys` / `delete_passkey`
  - Social accounts: `list_social_accounts` / `unlink_social_account`
  - 2FA: `get_2fa_status` / `setup_2fa` / `enable_2fa` / `disable_2fa`
  - System settings: `get_system_settings` / `set_system_setting`
  - Sales export: `export_sales_csv`
  - Cloudflare: `get_cloudflare_status` / `connect_cloudflare_api_token` /
    `connect_cloudflare_global_key` / `disconnect_cloudflare`
  - Webhook signing key: `get_webhook_signing_key`
- Semua tool = thin wrapper ke service layer sama seperti REST (satu logik, dua interface) + audit log `actor=mcp:<key_name>`
- Docs: contoh config Claude/situasi self-host; tool descriptions dua bahasa

## 6. CHIP integration detail

- Base: `https://gate.chip-in.asia/api/v1` (test mode: kunci test, domain sama)
- Paths **plural + trailing slash** (`/purchases/`, `/purchases/{id}/charge/`) — convention rasmi CHIP docs
- Auth: `Authorization: Bearer <merchant_secret_key>`
- Payload one-time: `client{email,full_name,phone}` + `purchase{products[],currency,reference,success_redirect,success_callback}` + `brand_id`
- Amount dalam sen; produk: `{"name","price","quantity"}`
- Webhook keys: public key dari `GET /public_key/` (callback) / `Webhook.public_key` (webhook object) — verify selepas parse, sebelum trust
- Subscribe events: `purchase.created/paid/payment_failure/pending_charge/charge_failure(?) — verify nama event dari spec masa implement`
- Test cards: `4444 3333 2222 1111` (non-3DS), `5555 5555 5555 4444` (3DS), CVC 123
- Test mode UI: badge "TEST MODE" jelas dalam borang & dashboard

## 7. Deployment (plain server + Traefik)

- `deploy/docker-compose.yml`: api · web · render · postgres:18 · valkey:9.1-alpine · minio (+ createbuckets init container)
- MinIO console internal sahaja; S3 endpoint expose via subpath/domain sendiri (cth. `s3.<domain>`); TLS via Traefik; volume persisten
- Production vars: `DATABASE_URL`, `VALKEY_URL`, `S3_ENDPOINT/KEY/SECRET/BUCKET`, `CHIP_*` tiada global (per-account), `ENCRYPTION_KEY` (AES-GCM untuk chip_secret_key_enc)
- Health checks: `/healthz` api, `/readyz` DB+Valkey+S3
- Backups: `pg_dump` nightly + S3 offsite; MinIO bucket versioning untuk media bucket

## 8. DNS (jomform.com @ Cloudflare)

Persekitaran sekarang: VPS IPv4 `203.0.113.11`, IPv6 `2407:3640:2318:7266::1`. Cadangan bila dah nak deploy (TBD — sahkan IP final):

| Record | Name | Value | Proxy |
|---|---|---|---|
| A | `@` | `203.0.113.11` | Proxied |
| AAAA | `@` | `2407:3640:2318:7266::1` | Proxied |
| A | `www` | `203.0.113.11` | Proxied |
| A | `app` (dashboard) | `203.0.113.11` | Proxied (atau CNAME app→@) |
| A | `api` | `203.0.113.11` | DNS-only (webhook CHIP + MCP perlu direct; pastikan TLS ok) |
| A | `s3` | `203.0.113.11` | DNS-only (jika expose MinIO) |
| CNAME | `f` | `@` (wildcard alternatif: `*.jomform.com`) | Proxied — form subdomain per-merchant: `<slug>.jomform.com` lebih baik: wildcard A record juga ke IP sama |

Nota: wildcard `*.jomform.com` → host borang per-merchant; api & s3 DNS-only supaya webhook delivery & S3 signature tidak terganggu Cloudflare proxy. Aktifkan **DNSSEC** once deployed. IP boleh berubah bila deploy — aku sahkan semula sebelum beri arahan DNS akhir.

## 9. Kejayaan MVP

- Self-host dalam <30 minit (clone → docker compose up → borang pertama live)
- 100% operasi asas boleh buat via MCP tanpa buka dashboard
- Subscription charge berjalan automatik 3 bulan berturut tanpa manual intervention
- Lighthouse borang awam ≥ 95 (mobile)

## 10. Risks

| Risiko | Mitigasi |
|---|---|
| MinIO image status "Archived" di Docker Hub | Sudah verified masih pullable (Aug 2026); pin digest; alternatif: Garantio/Vespa-style S3 alternatif atau R2 terus — keputusan semasa deploy |
| Valkey go client maturity | Pakai go-redis/v9 (battle-tested, RESP sama); valkey-go sebagai option kedua |
| MCP SDK changes fast | Pin version go.mod; subscribe releases |
| Tokenization card-only | Semua product subscription UI kena communicate "subskripsi = kad sahaja" (CHIP constraint); FPX tak boleh recurring |
| Solo-dev scope creep | P0 dulu; MCP & subscription dah masuk P0 sebab requirement tetap |