# AGENTS.md — JomForm repo agent guidance

## LOCAL BUILD/TEST BEFORE MERGE (RULE — there is NO CI; this gate is the gate)

**This repo has no GitHub Actions CI.** The workflows were removed
(`.github/workflows/` no longer exists) because every job failed with **0 steps**
on an account-wide Actions billing failure — a red check on a PR said nothing
about the code, so it was noise, not a gate. It will be rebuilt if/when the repo
goes public. Until then, **the local preflight is the only gate**, and there is
nothing upstream to catch what it misses: if it does not run, nothing ran.

Before merging ANY PR (or any agent change), run the full preflight and confirm
it is green:

```bash
source /tmp/jf-env.sh          # JOMFORM_TEST_DATABASE_URL + JOMFORM_TEST_VALKEY_URL
./scripts/preflight.sh
```

`scripts/preflight.sh` is the single entry point — the ported CI gates, in the
same order as the three CI jobs they replace:

| Gate | Command | What it catches |
|---|---|---|
| api build | `cd api && go build ./...` | compile errors |
| api vet | `cd api && go vet ./...` | suspicious constructs |
| api test | `cd api && go test ./... -race -count=1` | regressions **and data races** (the old local trio ran without `-race`) |
| api govuln | `govulncheck ./...` | known CVEs reachable from the module graph |
| web build | `cd web && npm run build` | `test:unit` (incl. the cross-language Go-contract guards) then the Next build |
| web a11y | `cd web && npm run a11y` | axe-core; fails on any **critical/serious** WCAG violation |
| web audit | `cd web && npm audit --audit-level=high` | high/critical npm advisories |
| secrets | `gitleaks detect` over the **full git history** | committed secrets (repo may go public) |

`deploy/build-and-ship.sh` calls `scripts/preflight.sh` itself, so a deploy
cannot skip it — the "check before deploy" instruction is now enforced by the
build script, not by asking the operator to remember. `--skip-tests` bypasses
the whole preflight and prints a loud warning; it is for a genuine emergency
only, never a routine release.

Every gate **fails loudly** when its prerequisite is missing rather than
silently skipping — an unset `JOMFORM_TEST_DATABASE_URL` makes `go test` skip
the DB-backed tests and report a green suite that proves nothing, and a worktree
without `web/node_modules` makes `npm run build` exit 0 with
`sh: 1: next: not found` having built nothing. Both shapes are treated as
failures.

Do NOT rely on (or wait for) GitHub checks — they do not exist for this repo
any more, and the branch-protection endpoint returns 403 on the free plan.
Local green is the source of truth.

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
  `run_due_charges`, `update_subscription_card`, `retry_subscription`
  (revive a past_due subscription so the engine retries it — round 13c)
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
  `admin_spam_queue`, `admin_email_deliveries`, `admin_system_health`,
  `admin_dead_letter_events`, `admin_requeue_dead_letter_event`
  (dead-letter visibility + requeue for parked inbound callbacks — issue #574;
  also exposed to the admin dashboard as `GET /v1/admin/dead-letters` +
  `POST /v1/admin/dead-letters/requeue` — issue #579)
- Email suppression list (gated on #385 super-admin) →
  `admin_list_suppressions`, `admin_add_suppression`, `admin_remove_suppression`
  (the addresses JomForm refuses to mail — SES hard bounces and complaints
  ingested asynchronously by the pre-send-check lane, plus manual entries;
  also exposed to the admin dashboard as `GET`/`POST /v1/admin/suppressions` +
  `DELETE /v1/admin/suppressions/{email}`)
- Affiliate marketing (#444) → `create_affiliate`, `list_affiliates`,
  `get_affiliate_sales`, `set_affiliate_commission`
- Realtime notifications (#447) → `get_realtime_status`, `set_realtime_sound`
  (the SSE stream `GET /v1/rest/events/stream` is the realtime channel)
- Trusted devices (#454) → `list_trusted_devices`, `revoke_trusted_device`
- In-app feedback (#639) → `list_feedback`, `get_feedback_thread`,
  `reply_feedback`, `set_feedback_status`.
  **USER-SCOPED, not account-scoped:** a feedback thread belongs to the person
  who filed it and carries no `account_id`, so naming a workspace cannot widen
  the read. A super-admin sees every thread from the same tools (triage).
  `reply_feedback` writes as `admin` for a super-admin (moving an `open` thread
  to `in_progress`) and as the owner otherwise; `set_feedback_status` is
  super-admin only and audited. REST: `POST`/`GET /v1/rest/feedback`,
  `GET /v1/rest/feedback/{id}`, `POST /v1/rest/feedback/{id}/messages`.
- Templates: gallery picker (#174) → `list_templates`, `use_template`
  (slug = system gallery); merchant-managed templates (#457) →
  `list_own_templates`, `create_template`, `update_template`, `delete_template`
  (`use_template` also accepts `template_id` for an own template)
- Restaurant menu (QR ordering, #438/#485/#497) → `get_menu_page`,
  `set_menu_page` (includes the optional order form for extra checkout
  fields), `generate_qr`.
  **PER-FORM (#497):** the menu config lives on the FORM (`forms.config.menu`,
  via `dbstore.MenuFromConfig`/`WithMenu`) — NOT on the workspace landing page.
  A menu form renders only its OWN product blocks and is served at the form's
  own URL; the workspace root always renders the normal landing page. REST:
  `GET/PUT /v1/rest/forms/{id}/menu`.

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
- **Caller-supplied image/URL fields MUST be scheme-validated `https://`-only**
  through `api/internal/urlguard` — ONE shared rule; never hand-roll another
  `strings.HasPrefix` copy. Applies to EVERY write path, REST **and** MCP:
  form theme `logo_url`/`og_image_url`, landing `image_url`/`logo_url`,
  product `images` + variation images, builder image-block `src`, and template
  `config`/`theme`. Reject a non-https value with 400 / `urlguard.InvalidMessage`;
  empty or omitted means "keep existing" and stays valid. `ObjectStore.PublicURL`
  returns an absolute config-derived https URL, so media uploads pass unchanged —
  do not add a relative-path exception. Do NOT type caller input `template.URL`,
  and do NOT use `ssrfguard.ValidateURL` here (it allows http and resolves DNS —
  it is for outbound webhooks).
  **Why:** an unsafe scheme reaching `<img src>` is silently rewritten by
  `html/template` to the `#ZgotmplZ` sentinel — the image never loads, HTTP is
  200, and nothing errors, so it only surfaces when someone looks at the page
  (#499/#501). The exception is a SERVER-GENERATED data URL (e.g. the QR stand
  PNG): type that field `template.URL` so it is emitted verbatim.

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
5. Run the fresh-DB migration guard — it creates a throwaway database, applies
   **every** `*.up.sql` from zero via the same `db.Migrate` runner the API uses
   at startup, and fails on the first error. This is what catches the 000047
   class of bug (a migration that only fails on a database where the object does
   not already exist — e.g. a `TEXT` column with a FK to a `uuid` PK,
   `SQLSTATE 42804`):
   `JOMFORM_TEST_DATABASE_URL=... go test ./internal/db/ -run 'TestMigrationsApplyFromZero|TestEveryMigrationHasDownFile'`.
   `TestEveryMigrationHasDownFile` also requires every new migration to ship a
   matching `.down.sql`; the two pre-existing gaps (`000005_form_overrides`,
   `000012_spaces`) are pinned in the test's allowlist.
6. Run the full suite: `go build ./... && go test ./...` in `api/`.

CI runs the migration + data-integrity tests against a fresh Postgres service,
so any column/scan mismatch or invariant violation fails the build.