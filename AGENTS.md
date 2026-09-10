# AGENTS.md — JomForm repo agent guidance

## LOCAL BUILD/TEST BEFORE MERGE (RULE — jangan guna CI sebagai gate)

**GitHub Actions CI is NOT a merge gate for this repo.** Before merging ANY
PR (or any agent change), the agent MUST run the full build + test suite
**locally** and confirm it is green:

1. `cd api && go build ./...` — must pass.
2. `cd api && go test ./...` — must pass (against the local test Postgres +
   Valkey; see the test DB setup in the pipeline notes).
3. `cd web && npm run build` — must pass (Next.js build, 0 errors).

Only merge after all three are green locally. Do NOT rely on (or wait for)
GitHub Actions checks — they may be unavailable (e.g. billing) or flaky
(npm audit network timeouts). Local green is the source of truth.

## MCP FEATURE PARITY (RULE — jangan tertinggal/terlepas)

**Every dashboard feature MUST have a matching MCP tool.** When adding any
new feature to the web dashboard, you MUST:

1. Add/upate the corresponding MCP tool in `api/internal/mcpsrv/`.
2. Register it in `api/internal/mcpsrv/tools.go` (`registerTools`).
3. Re-check the FULL parity list below — don't ship half.

Parity checklist (dashboard → MCP):
- Products: list/create/update/archive → `list_products`, `create_product`,
  `update_product`, `archive_product`
- Form builder: create/update/publish/unpublish/list/get → `upsert_form`,
  `publish_form`, `unpublish_form`, `list_forms`, `get_form`
- Sales: list/summary/confirm → `list_sales`, `get_sales_summary`,
  `confirm_sale`
- Skip-capture flow → `capture_sale`, `release_sale`
- Subscriptions → `cancel_subscription` (no token revoke — Wan rule),
  `run_due_charges`
- Analytics → `get_form_analytics`, `get_product_analytics`
- CHIP per-form override → `set_form_chip_override`, `clear_form_chip_override`
- e-Invoice settings → `set_einvoice_settings`, `get_einvoice_settings`
- Webhooks → `create_webhook`, `list_webhooks`, `update_webhook`,
  `delete_webhook`, `webhook_deliveries`
- API keys → `list_api_keys`, `create_api_key`, `revoke_api_key`
- Event tracking / audit log → `list_events` (account-scoped, read-only)
- Space landing page (customizable storefront, #360/#362) →
  `get_workspace_landing_page`, `set_workspace_landing_page` (incl. embedded `forms[]`)
- Media library / asset upload (image + code, tenant-scoped, scan-gated,
  #361/#364) → `list_media`, `upload_media`
- Analytics → `get_form_analytics`, `get_product_analytics`
- Custom domains → `list_domains`, `add_domain`, `verify_domain`,
  `remove_domain`, `auto_cname_domain`
- Tracking → `get_tracking`, `set_tracking`
- CHIP account settings → `get_chip_settings`, `set_chip_settings`
- Profile avatar → `set_avatar`
- Passkeys → `list_passkeys`, `delete_passkey`
- Social accounts → `list_social_accounts`, `disconnect_social_account`
- 2FA → `get_2fa_status`, `enable_2fa`, `disable_2fa`
- System settings → `get_system_settings`, `set_system_settings`
- Sales export → `export_sales_csv`
- Cloudflare → `set_cloudflare_api_token`, `set_cloudflare_global_key`,
  `disconnect_cloudflare`
- Webhook signing key → `get_webhook_signing_key`
- CHIP refund → `refund_sale`
- Domain TLS config (#397) → `get_domain_tls`, `set_domain_tls_mode`,
  `set_domain_tls_cert`
- Domain ACME email (#398) → `get_domain_tls_email`, `set_domain_tls_email`
- Super-admin platform-wide monitoring (#386, gated on #385 super-admin) →
  `admin_list_workspaces`, `admin_media_usage`, `admin_list_sales`,
  `admin_spam_queue`, `admin_email_deliveries`, `admin_system_health`

## DOCUMENTATION PARITY (RULE — jangan tertinggal/terlepas)

**Every feature MUST be documented** — not just have MCP parity. When adding any
new feature (dashboard, MCP tool, placeholder, setting, or behavior), you MUST
add/update the corresponding docs page in the knowledge base (`docs.jomform.com`,
issue #395) so users know what JomForm offers and how to use it:

1. Document the feature in the docs site (how it works, how to use it).
2. Document any new **MCP tool** (name, purpose, inputs, outputs) — keep it in
   sync with the tool registry in `api/internal/mcpsrv/`.
3. Document any new **dynamic placeholder** (e.g. `{admin_email}`, `{{ref_code}}`)
   in the placeholders reference.
4. Add an **inline "?" doc link** in the UI next to the feature/field where
   relevant (e.g. next to the `{admin_email}` field → placeholders doc).
5. Re-check the FULL docs coverage — don't ship a feature with no docs.

## Conventions

- Money = integer **sen** everywhere.
- DB select/scan MUST stay column-aligned (every migration that adds
  columns: update `accountSelect` + ALL Scan call sites — ByID, ByOwner,
  ByEmail — or you get silent 401 storms).
- CHIP create-purchase: `success_redirect`/`failure_redirect`/
  `cancel_redirect`/`success_callback`/`reference` are **TOP-LEVEL** fields.
- Never auto-revoke a card token on cancellation (Wan's rule).
- Repo must stay free of any OnPay mention (open-source constraint).

## Adding a migration safely (checklist)

The migration/scan-drift guard in `api/internal/dbstore/migration_scan_drift_test.go`
fails the build if a migration adds a column to a table that has a `*Select`
constant without that column appearing in the select (and struct + scan sites).
Follow this checklist so the guard stays green:

1. Create `api/internal/db/migrations/NNNNN_<name>.up.sql` (and `.down.sql`).
   Use `make migrate-new NAME=<name>` to get the next sequence number.
2. If you `ALTER TABLE ... ADD COLUMN` on a table with a `*Select` constant
   (`accounts`, `forms`, `products`, `sales`, `spaces`, `subscriptions`):
   - Add the column to the matching `*Select` constant.
   - Add the field to the matching struct (`Account`, `Form`, `Product`,
     `Sale`, `Space`, `Subscription`).
   - Update EVERY Scan call site that scans that select (the `*Select`'s
     scan functions — e.g. `accountSelect` → `AccountStore.ByID` +
     `AccountStore.ByOwner`; `formSelect` → `FormStore.scanOne`; etc.).
   - If the column is read only via a dedicated accessor query (e.g.
     `Tracking`, `SigningKeys`, `CFToken`, `ChipOverrideFor`), add it to the
     `intentionallyUnselected` allowlist in the guard test instead — do NOT
     add it to the `*Select`.
3. Run `go test ./internal/dbstore/ -run 'TestSelectScanColumnAlignment|TestMigrationColumnsAppearInSelect'`
   — both must pass.
4. Run the data-integrity invariants against a real Postgres:
   `JOMFORM_TEST_DATABASE_URL=... go test ./internal/dbstore/ -run 'TestMoneyIsIntegerSen|TestRefCodeUniqueness|TestNoOrphanedSalesOnFormDelete|TestSlugUniquenessPerSpace|TestSuppressionListConsistency'`.
5. Run the full suite: `go build ./... && go test ./...` in `api/`.

CI runs the migration + data-integrity tests against a fresh Postgres service,
so any column/scan mismatch or invariant violation fails the build.