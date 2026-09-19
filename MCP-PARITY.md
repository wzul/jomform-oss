# MCP parity checklist — every dashboard feature must have an MCP tool

This file holds the **tool checklist** parsed by `api/internal/mcpsrv/mcp_parity_test.go`
(both directions: every registered tool must be named here, and every name here must
be registered).

**Why this is a separate file.** The checklist used to live in `AGENTS.md`, which is a
write-gated agent-instruction file: a headless worker that registered a new MCP tool
and needed the matching checklist line **timed out at the approval prompt and blocked**
(AGENTS.md carries an *always-ask* gate that `--yolo` and allowlists do not bypass, and
that fails closed with no human channel). Every parity update therefore became a
human-only edit. The RULE stays in AGENTS.md; the DATA the guard parses lives here,
where agents can update it.

Add a line here in the SAME change that registers a tool, and keep the list honest.
The parser reads only backticked **snake_case identifiers to the right of a `→`** — so
do not backtick prose in an entry: a backticked `account_id` or `in_progress` is read
as a claimed tool name and fails the guard in the opposite direction.

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
  who filed it and carries no account id, so naming a workspace cannot widen
  the read. A super-admin sees every thread from the same tools (triage).
  `reply_feedback` writes as an admin message for a super-admin (moving an open
  thread to in-progress) and as the owner otherwise; `set_feedback_status` is
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

