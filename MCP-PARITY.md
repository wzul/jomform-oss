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
  (`archive_product` is "not on sale", NOT the bin — it stays in the list as
  Archived).
- Product trash (soft delete → restore or permanent purge) → `delete_product`,
  `list_deleted_products`, `restore_product`, `purge_product`.
  `delete_product` moves a product to the bin (a DIFFERENT state from
  `archive_product`'s `active=false`); each bin row carries the sale count, the
  subscription count and the pre-computed can_purge answer; `purge_product` requires the
  product's NAME typed as confirm_name (a product has no slug), and is REFUSED
  for a product with any sale (sales history is never destroyed —
  `sales.subscription_product_id` is ON DELETE SET NULL, so the delete would
  silently detach it) or with a live subscription
  (`subscriptions.product_id` is ON DELETE RESTRICT, so the refusal names the
  obstacle instead of returning a raw 23503).
  REST: `DELETE /v1/rest/products/{id}`, `GET /v1/rest/products/trash`,
  `POST /v1/rest/products/{id}/restore`,
  `DELETE /v1/rest/products/{id}/purge`.
- Form builder: create/update/publish/unpublish/list/get → `upsert_form`,
  `publish_form`, `unpublish_form`, `list_forms`, `get_form`
- Form trash (soft delete → restore or permanent purge) → `list_deleted_forms`,
  `restore_form`, `purge_form`.
  The existing soft-delete tool `delete_form` trashes a form; `restore_form`
  brings it back with no CASCADE and no silent rename (a slug re-taken while
  trashed refuses the restore); `purge_form` destroys the row AND its CASCADE
  children, requires the slug typed as confirm_slug, and is REFUSED for a form
  that has any sale — sales.form_id is ON DELETE SET NULL, so a purge would
  silently orphan the sale.
  REST: `GET /v1/rest/forms/trash`, `POST /v1/rest/forms/{id}/restore`,
  `DELETE /v1/rest/forms/{id}/purge`.
- Form slug rename (the Form Settings slug field → the slug parameter on
  `upsert_form`; same validation, same per-workspace uniqueness, same temporary
  301 for the old URL)
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
- Workspace lifecycle (#162, member/invite tools above) → `list_workspaces`,
  `create_workspace`, `delete_workspace`, `check_workspace_deletion`.
  `delete_workspace` is OWNER-ONLY and needs the workspace slug typed as
  confirm_slug; it is refused when
  the owner would be left with NO workspace (the "at least one workspace" rule)
  or when the workspace is not empty (blockers are listed per category, with
  counts). Restore is not offered: the delete is permanent, and the CHIP
  account goes with the workspace. `check_workspace_deletion` is the read half —
  the SAME emptiness definition, so the UI can state a reason instead of
  failing after the click. REST: `DELETE /v1/workspaces/{id}`,
  `GET /v1/workspaces/{id}/deletion-check`.
- Media library / asset upload (image + code, tenant-scoped, scan-gated,
  #361/#364) → `list_media`, `upload_media`
- Media trash (soft delete → restore or permanent purge) → `delete_media`,
  `list_deleted_media`, `restore_media`, `purge_media`.
  `delete_media` marks the row and leaves the OBJECT in place, so the bin is
  restorable and the URL keeps working; `restore_media` therefore works only
  while the bytes are still in the store (a row whose object was removed by an
  operator or a bucket lifecycle rule is REFUSED, rather than restored into a
  URL that 404s); `purge_media` is the ONLY place the bytes are deleted from the
  bucket and released from the 1GB workspace quota — trash is quota-neutral by
  construction. REST: `DELETE /v1/rest/media/{id}`,
  `GET /v1/rest/media/trash`, `POST /v1/rest/media/{id}/restore`,
  `DELETE /v1/rest/media/{id}/purge`.
- Analytics → `get_form_analytics`, `get_product_analytics`
- Custom domains → `list_domains`, `add_domain`, `verify_domain`,
  `remove_domain`, `auto_cname_domain`
- Tracking → `get_tracking`, `set_tracking`
- CHIP account settings → `get_chip_settings`, `set_chip_settings`
- Profile avatar → `set_avatar`
- Profile language (the dashboard UI language, stored per-profile on `users.lang`) →
  `update_profile_language`.
  **DASHBOARD ONLY, PER-PROFILE:** it decides what the signed-in user's own dashboard
  speaks, in every browser — it is NOT the buyer-facing language of a public form
  (Settings → Storefront sets the workspace default form language, and a form's own
  config.lang overrides that). The dashboard picker is on /profile.
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
  `reply_feedback`, `set_feedback_status`,
  `retry_feedback_notify`, `submit_feedback`.
  **USER-SCOPED, not account-scoped:** a feedback thread belongs to the person
  who filed it and carries no account id, so naming a workspace cannot widen
  the read. A super-admin sees every thread from the same tools (triage).
  `reply_feedback` writes as an admin message for a super-admin (moving an open
  thread to in-progress) and as the owner otherwise; `set_feedback_status` and
  `retry_feedback_notify` are super-admin only and audited. A notification that
  failed to reach the queue is marked on the thread (the notify_failed_at /
  notify_failed_kind / notify_failed_error columns), never dropped in silence,
  and `retry_feedback_notify` is the operator's way to re-send it. The retry
  answers with what the attempt actually DID: queued/sent (a mail left),
  failed (attempted and did not get through, marker stays), none (no
  recorded failure to re-send) or skipped (nothing attempted — no recipient
  configured), so a value meaning delivery always has a job behind it. REST:
  `POST`/`GET /v1/rest/feedback`, `GET /v1/rest/feedback/{id}`,
  `POST /v1/rest/feedback/{id}/messages`,
  `POST /v1/admin/feedback/{id}/notify`.
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

## In-app assistant — the proposal lane (phase 2 of #639)

**Deliberately NOT a checklist entry, and the reason matters.** The chat's
`propose_write` tool is named here as PROSE, not as a backticked name to the
right of an arrow, because it is not a registered MCP tool at all and the
checklist parser (and the guard behind it) would correctly reject it.

The in-app assistant has two tools that execute IN PROCESS rather than over
MCP, because neither one dispatches a tool the caller names:

- `submit_feedback` — registered (it is the checklist's in-app feedback entry)
  and also called in-process by the chat, so a filing from a conversation
  notifies and rate-limits exactly like the /feedback page.
- `propose_write` — NOT registered. It records a proposal and returns an opaque
  single-use token, and the step that actually performs the write is a
  **session-only** route (`POST /v1/rest/chat/confirm`), because a confirmation
  is a signed-in person's click. Registering it would ship an MCP tool whose
  only possible outcome is a token no MCP caller could ever confirm: the token
  goes to the browser and is never returned to the model. An agent that wants
  the write calls the write tool itself — its own client is the human's
  confirmation channel.

What that means for parity: there is no new dashboard capability here that MCP
lacks. The three tools the model may PROPOSE are pre-existing registered writers
(`update_product`, `update_product_variation`, `set_form_theme`), each already
reachable over MCP and each already covered by this checklist or the baseline
above. The chat adds no write path of its own — it adds a human confirmation
step in front of writes the merchant could already make.

Related routes (session-only, never API-key reachable): `POST /v1/rest/chat`
(message), `POST /v1/rest/chat/confirm` (apply a confirmed proposal),
`POST /v1/rest/chat/cancel` (kill one). Docs: `/docs/chat`.

