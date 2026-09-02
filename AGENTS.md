# AGENTS.md — JomForm repo agent guidance

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
- Webhooks → `create_webhook`, `list_webhooks`, `delete_webhook`,
  `webhook_deliveries`
- API keys → `list_api_keys`, `create_api_key`, `revoke_api_key`
- Event tracking / audit log → `list_events` (account-scoped, read-only)

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