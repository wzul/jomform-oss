import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";

export const metadata: Metadata = docsMetadata({
  title: "MCP tool reference — JomForm Docs",
  description:
    "Every MCP tool JomForm exposes to AI agents: name, purpose, inputs and outputs. Kept in sync with the tool registry in the api/internal/mcpsrv package.",
  path: "/docs/mcp",
});

// This page has two halves: the prose a client has to read, then the tool rows.
// The tool rows mirror the registry in the api/internal/mcpsrv package
// (issue #395) and are NOT auto-generated:
// this list is edited by hand, which is exactly how it fell ten tools behind the
// registry. Keep it in step when tools are added or changed, because
// src/lib/mcpToolInventory.test.ts fails the build if you forget.
//
// Every row here must be a REGISTERED tool, and this page lists the registry in
// full: both directions are enforced by src/lib/mcpToolInventory.test.ts, which
// derives both sets and fails on either kind of drift. A group may carry a `note`
// for prose that is not a tool; a note is NOT a tool row (the old
// "Session safety" group's em-dash row inflated the rendered count and listed
// nothing).
//
// NOTE for anyone editing these comments: src/lib/docsToolClaims.test.ts scans
// this file for a name presented as a tool, so a SNAKE_CASE identifier in brackets
// a few words after the word "tool"/"tools" is read as a claim and must be a
// registered tool. Ordinary prose and a card id in brackets there are fine, since
// that shape is strict and it skips this repo's card-id spelling (t_3926a5dd).

const TOOL_GROUPS: { group: string; note?: string; tools: { name: string; desc: string }[] }[] = [
  {
    group: "Forms",
    tools: [
      { name: "list_forms", desc: "List your account's forms: id, slug, title, status (draft|private|published|unlisted), mode (payment|response) and public URL when live. Read-only — use for form id discovery before reading or writing." },
      { name: "get_form", desc: "Read ONE form in full (read-only): id, slug, title, status, mode, config — INCLUDING current blocks — theme, registered_fields, public URL, created_at and updated_at. Read the form first, change only the fields you want, then resend the full config via upsert_form. Writing without reading first can destroy existing blocks." },
      { name: "upsert_form", desc: "Create/update a form (empty form_id = new). Read the existing one with get_form first and send the full config so existing blocks are not lost. Theme (optional) = custom style/branding. slug (optional) = the form's URL path: it must be 1-40 characters of a-z, 0-9 and hyphens and must not be a name JomForm reserves for itself (login, api, docs, privacy, …) — a malformed slug is rejected as form.slugInvalid and a reserved one as form.slugReserved. On create it is used when filled in; on update it RENAMES the form and the old URL keeps working through a temporary redirect (301) — leave it empty to keep the existing slug, and a slug already taken by another form in this workspace is rejected with an error. An option/shipping block with no price on its choices is also REJECTED — set price_sen on every choice (0 for free), because that price is what the buyer is charged." },
      { name: "set_form_theme", desc: "Set the form's custom style/branding (issue #254): primary, background, text, card_background, card_border_radius, font_family, logo_url (must be https:// — issue #501), layout (centered|full_width|split), spacing (compact|comfortable|spacious), product_image_size (small|medium|large|hidden, issue #471), product_image_layout (thumbnail-left|image-top — menu card), custom_css, custom_js (issue #388 — custom JS for form behavior; owner-gated + scanned). Agents are free to set CSS/JS; JomForm keeps authority over pricing & payment." },
      { name: "register_form_fields", desc: "Register the form's input fields (issue #254) with a strict contract: {field_id, type, label, required, validation?, product_id?}. type must be a JomForm input type (name|email|phone|textarea|text|number|select|radio|checkbox|date|time|website|address|consent|hidden|list|repeater|image-choice|captcha). Replaces all existing fields. Prices stay from the product DB (server authority)." },
      { name: "generate_form", desc: "Generate a form config (blocks + style) from a natural-language prompt (issue #256) via deepseek-v4.1-flash:1017. Output is validated server-side against JomForm block types; every option/shipping choice must carry price_sen (0 for free) and that price is what is charged; product prices stay from the DB (server authority). Review/edit first, then save using upsert_form." },
      { name: "publish_form", desc: "Publish the form — live at <workspace>.jomform.com/<slug> or a custom domain. status=private (with a password) makes it a password-protected form; status=draft withdraws it. A payment form with no products is rejected ONCE with a clear message (it cannot accept payment) — send acknowledge=true to proceed. Response forms and forms that are already live are never asked. An account with no workspace yet is REJECTED (workspace.required): that form has no address at all, so create a workspace first — status=draft is still allowed. Gallery templates (is_template) are EXEMPT during status=published: their address is the gallery GET /v1/templates, not a workspace." },
      { name: "unpublish_form", desc: "Withdraw the form from live (make it a draft)." },
      { name: "set_form_password", desc: "Set/remove the access password of a private form (argon2id-hashed; never returned). Empty password = remove the password." },
      { name: "get_form_restrictions", desc: "Read form restrictions (issue #207): start/end schedule (start_at/end_at) + entry limit (entry_limit) + stage messages." },
      { name: "set_form_restrictions", desc: "Set form restrictions (issue #207): start/end schedule + entry limit + message. Empty = remove restrictions." },
      { name: "get_form_og_image", desc: "Read the form's Open Graph image (og:image) (issue #411): the custom image URL when set, empty = use the generated default /og/{slug}.png." },
      { name: "set_form_og_image", desc: "Set the form's Open Graph image (og:image) (issue #411): custom image URL (must be https://) for the social preview when the form is shared. Empty = use the generated default." },
      { name: "delete_form", desc: "Soft-delete the form (it goes to the dashboard trash; the slug is released). Can be restored — use restore_form." },
      { name: "list_deleted_forms", desc: "List forms in the TRASH (deleted_at IS NOT NULL): id, slug, title, deleted_at, sales_count and can_purge. Forms with sales cannot be permanently deleted (can_purge=false)." },
      { name: "restore_form", desc: "Restore a form from the trash (deleted_at=NULL). Nothing is destroyed — the form comes back with its original slug. If the slug has already been taken by another form, the restore is REJECTED (no automatic renaming) — change that form's slug first." },
      { name: "purge_form", desc: "PERMANENTLY delete a form from the trash (submissions/notifications/confirmations/the form's email settings/webhooks drop with it). WARNING: cannot be undone. Required confirm_slug = that form's slug. Forms that HAVE SALES are rejected (sales are never destroyed or left orphaned — sales.form_id is ON DELETE SET NULL, so a permanent delete would silently orphan the sale). Trash-only forms — delete first using delete_form." },
      { name: "bulk_forms", desc: "Bulk action over many forms at once: action=publish|draft|delete with ids[] (max 500). Payment forms without products are EXCLUDED from publish and returned in needs_products — send acknowledge=true to publish them too. Templates without a category or a description block (Section/text) are SKIPPED from publish and returned in skipped + skipped_reason — updated counts only rows that actually changed, and acknowledge=true does not override it. action=publish is REJECTED entirely (workspace.required) if the account has no workspace — that form has no address; action=draft|delete is still allowed. A batch containing ONLY gallery templates is exempt from that condition (its address is the gallery)." },
    ],
  },
  {
    group: "Products",
    tools: [
      { name: "list_products", desc: "List all active products in your JomForm account." },
      { name: "create_product", desc: "Create a new product. For subscriptions, set type='subscription' and billing_period='month'|'year'." },
      { name: "archive_product", desc: "Deactivate a product (shelve without deleting data). This is NOT trash — it stays in the list as an archive. To remove it from the list use delete_product." },
      { name: "list_product_variations", desc: "List a product's variations (name, price_sen, stock, images, sort_order, and its own subscription cycle when set) for one product. Simple product = empty list. The billing_period/trial_days/subscription_charges fields are present ONLY when the variation sets them itself; if absent, that variation inherits the product's cycle." },
      { name: "create_product_variation", desc: "Add ONE variation to a product (e.g. Size M), with its own price, stock, images and subscription cycle (billing_period month|year, trial_days, subscription_charges). Leave empty = the variation inherits the product's cycle." },
      { name: "update_product_variation", desc: "Update ONE variation (name, price, stock, images, subscription cycle). Fields not supplied stay unchanged. Set clear_billing_period / clear_trial_days / clear_subscription_charges = true to REMOVE that variation's cycle so it inherits the product's cycle again." },
      { name: "delete_product_variation", desc: "Delete ONE variation from a product. Deleting the last variation turns that product back into a simple product." },
      { name: "update_product", desc: "Update a product: name, price (price_sen), stock, type, billing_period (subscription), active/inactive, images (image URLs must be https:// — issue #501; omit = unchanged). Omit a field to leave it unchanged." },
      { name: "bulk_products", desc: "Archive many products at once (active=false). ids[] max 500." },
      { name: "delete_product", desc: "Send a product to TRASH (deleted_at set) — it leaves the product list and can be restored with restore_product. This differs from archive_product (active=false), which keeps it in the list. To deactivate only, use archive_product." },
      { name: "list_deleted_products", desc: "List products in TRASH: id, name, price_sen, deleted_at, sales_count, subscription_count and can_purge. can_purge=false means the product is still referenced by sales (history) or subscriptions (billing still running) — a permanent delete will be REJECTED." },
      { name: "restore_product", desc: "Restore a product from trash (deleted_at=NULL). Nothing is destroyed and no name has to be resolved — products have no slug — so a restore cannot collide." },
      { name: "purge_product", desc: "PERMANENTLY delete a product from trash (its variations fall with it via CASCADE). WARNING: cannot be undone. Required confirm_name = that product's name. REJECTED when the product has sales (sales history is not destroyed) or still has subscriptions (FK RESTRICT). Only products in the trash — send to the bin first with delete_product." },
    ],
  },
  {
    group: "Sales",
    tools: [
      { name: "list_sales", desc: "List recent records (newest first). Without form_id this is the ENTIRE account record set: sales AND form responses for mode 'response' (sale_type 'response', submission_id + data) — the same as the Sales page, not an empty or partial list. With form_id, the form's mode decides: payment form → sales, response form → responses. Each sale: refunded_sen (already refunded), outstanding_sen (remaining refundable), refund_status (none|partial|full) — issue #518. Status is a payment question, so setting status excludes the response side." },
      { name: "get_sales_summary", desc: "Sales summary: counts by status (including refunded), refunded_sen (total refunded) and revenue_sen (net value after refunds) — issue #518." },
      { name: "confirm_sale", desc: "Confirm a sale (example: after checking bank transfer proof)." },
      { name: "describe_lifecycle", desc: "Describe an aggregate's declarative lifecycle table: every state, the events it accepts, the state each event lands in, whether a guard can refuse it, and the dashboard label key of a person's action. With id (a sale ref_code) the answer is that sale's current state plus available_events — the events it accepts right now, each with enabled and, when a guard refuses it, the English reason; an entry with an empty label_key is a gateway callback (pay, fail), not an action a caller can invoke. Use this BEFORE proposing or attempting a state change: the same table decides what the write endpoints accept, so an enabled=false event WILL be refused." },
      { name: "get_sale_email_status", desc: "Receipt email status for one sale: Sent → Delivered → Opened → (Clicked) with timestamps. Use ref_code from list_sales." },
      { name: "export_sales_csv", desc: "Export a set of account records as CSV. Without form_id it contains both sales AND responses (a record_type column marks each row, plus the recorded answer columns), matching the list that is shown. Filterable by form_id/status/date_from/date_to/q — the same filters as the list. The refund column exposes partial refunds (issue #518). Returns the csv content." },
    ],
  },
  {
    group: "Shipping",
    tools: [
      { name: "get_shipping_rates", desc: "Read the shipping zone set + rates for one account/workspace: every zone (state/district/postcode range, priority, default zone) and its rates (price in SEN, weight range in grams, free-shipping threshold)." },
      { name: "set_shipping_rates", desc: "Replace ALL shipping zone sets + rates for one account/workspace. Sets absent from sets are DELETED (rejected if any form still references them). A rate must have a price in SEN; min/max_weight_g in grams (0 = no limit); free_over_sen = free-shipping threshold." },
      { name: "get_shipping_couriers", desc: "List shipping providers (lalamove, easyparcel) for one account/workspace: whether server credentials exist, whether the workspace is already connected, the environment (sandbox/live), and WHAT IS STILL MISSING (credentials/connect/grant/origin) before a real quote can be made. No credential values are returned." },
      { name: "connect_shipping_courier", desc: "Connect one shipping provider for one account/workspace. Supply the PICKUP ADDRESS (postcode+state for easyparcel; latitude+longitude for lalamove). For easyparcel this tool returns authorize_url — that link is for the ACCOUNT OWNER to open themselves (any device) and grant permission; do not open that link, do not type or ask for a password. Links expire: call again for a new link." },
      { name: "disconnect_shipping_courier", desc: "Disconnect one shipping provider for one account/workspace and remove the stored grant. The pickup address is KEPT so reconnecting is easy." },
    ],
  },
  {
    group: "Payment fees",
    tools: [
      { name: "get_payment_fees", desc: "Read the processing fee rates per CHIP payment method (fpx, card, ewallet, duitnow_qr): active/not, fixed fee in SEN, percentage fee in BASIS POINTS (100 = 1%), and whether the account owner has given the disclosure acknowledgement. Without that acknowledgement the fee is not charged to the payer even if the rate is stored." },
      { name: "set_payment_fees", desc: "Replace ALL processing fee rates for one account/workspace. Methods absent from rates are DELETED. Enabling a fee (fee_enabled + rate > 0) REQUIRES acknowledged=true — the account owner's acknowledgement that the fee is passed to the payer; the tool records who and when. The fee is calculated on the buyer's FINAL amount (gross-up), so the merchant does not bear a fee on the fee." },
    ],
  },
  {
    group: "Skip-capture",
    tools: [
      { name: "capture_sale", desc: "Capture skip_capture authorization (the buyer's card is charged now). amount_sen optional for partial." },
      { name: "release_sale", desc: "Release (void) skip_capture authorization — the buyer's card is NOT charged." },
      { name: "refund_sale", desc: "Refund a paid sale (paid/confirmed). amount_sen = partial refund amount; empty = full refund of the remaining balance. Must be > 0 and refunded_sen + amount_sen <= total_sen (a refund exceeding the balance is rejected before CHIP, issue #518). A partial refund records the amount and the sale stays paid/confirmed (refund_status=partial); only when the full balance is refunded does the status become refunded. Response: refunded_sen, outstanding_sen, refund_status, fully_refunded." },
      { name: "capture_all_pending_sales", desc: "Capture ALL skip_capture authorizations still pending. One failure does not stop the others; respond per-ref." },
    ],
  },
  {
    group: "Subscriptions",
    tools: [
      { name: "list_subscriptions", desc: "List account subscriptions: customer (email), product, amount/period, status (active/past_due/cancelled), next charge date. Filter ?status, page/limit." },
      { name: "cancel_subscription", desc: "Cancel a customer's subscription. The card token stays with the merchant — can be reactivated." },
      { name: "retry_subscription", desc: "Retry a past_due subscription (round 13c): return its status to active and set it due now so the next sweep charges the card again. Use after the customer tops up the account or fixes the card. Only past_due subscriptions qualify — cancelled/completed ones are rejected. Does not charge immediately (avoids double charging)." },
      { name: "update_subscription_card", desc: "Update the card on an existing subscription (issue #154): create a new purchase token so the customer can confirm the new card. Returns checkout_url to send to the customer. The old token stays at CHIP (not cancelled) but is no longer used." },
    ],
  },
  {
    group: "CHIP per-form",
    tools: [
      { name: "set_form_chip_override", desc: "Set CHIP credentials specific to ONE form (brand_id + secret_key). When set, that form uses these credentials instead of the account default. The secret is stored encrypted." },
      { name: "clear_form_chip_override", desc: "Clear a form's override — go back to using the account's default credentials." },
      { name: "get_form_chip_feeds", desc: "Read a form's CHIP feed list (issue #208). Each feed has brand_id + secret (hidden) + conditional logic. A feed without a condition = default (always matches)." },
      { name: "set_form_chip_feeds", desc: "Set a form's CHIP feed list (issue #208). The first matching feed (according to form field values) is used for payment; a feed without a condition = default. An empty secret on an existing feed = keep the old key." },
      { name: "get_form_chip_mapping", desc: "Read a form's CHIP field map (CHIP field → form block id). Empty = use default (name→full_name, email→email, phone→phone)." },
      { name: "set_form_chip_mapping", desc: "Set a form's CHIP field map (CHIP field → form block id), e.g. {\"full_name\":\"block_abc\",\"email\":\"block_def\"}. Empty mapping = use default." },
    ],
  },
  {
    group: "CHIP account",
    tools: [
      { name: "get_chip_settings", desc: "Read the account's CHIP credential status: configured, verified, mode, brand_id, secret_key_masked, email_fallback, whitelist. The full secret is never returned. Read-only." },
      { name: "set_chip_settings", desc: "Set the account's CHIP credentials (brand_id + secret_key). Empty secret = keep the existing key. Validated against CHIP before saving. Admin only." },
      { name: "clear_chip_settings", desc: "Clear the account's CHIP credentials. Admin only." },
    ],
  },
  {
    group: "Form email & notifications",
    tools: [
      { name: "get_form_email_settings", desc: "Read the email settings of ONE form (issue #453): per-form overrides (receipt subject/body, send receipt, merchant notification, HTML), the inherited workspace settings, and the effective values (per-form -> workspace -> default resolution). Empty/null = follow the workspace settings." },
      { name: "set_form_email_settings", desc: "Set email overrides for ONE form (issue #453): receipt_subject, receipt_body, receipt_enabled_override, notify_merchant_override, merchant_notify_email, html_override. Toggles are tri-state — true/false = explicit override, null = follow the workspace settings. Empty subject/body = follow the workspace. Omitted field = keep existing." },
      { name: "clear_form_email_overrides", desc: "Delete ALL per-form email overrides so this form returns to inheriting the workspace email settings (issue #453)." },
      { name: "get_form_notifications", desc: "Read the list of per-form email notification rules (issue #193): event, recipient, subject/body, conditional logic, active. Every new form ships with one default notification to the merchant: event sale.paid for payment forms, response.submitted for response forms (issue #597)." },
      { name: "set_form_notifications", desc: "Replace ALL per-form email notification rules (issue #193). notifications[] in full: {id?, name, event, to_email, subject, body, condition?, enabled?}. event: sale.paid|sale.failed|response.submitted|subscription.charged. to_email: an email, {admin_email}, or {field_id}. condition: {field_id, op, value}|null (op: equals|not_equals|contains|gt|lt|not_empty). Empty list = no email is sent." },
      { name: "get_form_confirmations", desc: "Read form confirmations (issue #179): the list of confirmations (message/redirect) + conditional logic. A default confirmation always exists." },
      { name: "set_form_confirmations", desc: "Set form confirmations (issue #179): replace all confirmations (name, type message|redirect, message/redirect_url, condition). The default confirmation is always kept. HTML messages are sanitised (no XSS)." },
      { name: "get_form_failure_message", desc: "Read the form's payment-failed message (issue #393): what the buyer sees when payment fails. Empty = use the default message." },
      { name: "set_form_failure_message", desc: "Set the form's payment-failed message (issue #393): the HTML the buyer sees when payment fails (sanitised, no XSS). Empty = use the default message." },
    ],
  },
  {
    group: "Email settings",
    tools: [
      { name: "get_email_settings", desc: "Read a workspace's email settings: receipt subject/body (placeholders), send receipt, merchant notification, HTML." },
      { name: "set_email_settings", desc: "Set workspace email settings: receipt_subject, receipt_body ({{ref_code}}, {{amount}}, {{items}}, {{form_url}}, {{buyer_name}}, {{date}}), receipt_enabled, notify_merchant, merchant_notify_email, html. Omitted field = keep existing." },
    ],
  },
  {
    group: "e-Invoice",
    tools: [
      { name: "set_einvoice", desc: "Set the LHDN TIN + MSIC for e-Invoice receipts (B2B)." },
      { name: "get_einvoice_settings", desc: "Read the current LHDN TIN + MSIC for e-Invoice receipts." },
    ],
  },
  {
    group: "Webhooks",
    tools: [
      { name: "create_webhook", desc: "Register a webhook endpoint (events: sale.paid|sale.failed|subscription.charged|*). HMAC-signed. form_id optional: set = webhook for that form only; empty = whole workspace/account." },
      { name: "list_webhooks", desc: "List the account's outbound webhooks. form_id optional: set = that form's webhooks only; empty = whole workspace/account webhooks." },
      { name: "delete_webhook", desc: "Delete a single webhook (id from list_webhooks)." },
      { name: "update_webhook", desc: "Update a webhook (url, events, secret optional — empty = keep existing). events: sale.paid|sale.failed|subscription.charged|*." },
      { name: "webhook_deliveries", desc: "Recent webhook delivery log (status_code, attempt, error)." },
      { name: "retry_webhook_delivery", desc: "Retry a failed webhook delivery immediately (id from webhook_deliveries). It is re-enqueued and re-sent on the next sweep." },
      { name: "get_webhook_signing_key", desc: "Read the account's Ed25519 signing keys (key_id, public_key, alg) to verify X-JomForm-Signature-Ed25519 webhook signatures. Reading existing keys does not require write scope; GENERATING a new key (the first time, or after it is deleted) requires readwrite." },
    ],
  },
  {
    group: "API keys",
    tools: [
      { name: "list_api_keys", desc: "List all of the account's API keys (id, name, scopes, created_at, last_used). Key material is never returned." },
      { name: "create_api_key", desc: "Create a new API key (name, scopes? readonly|readwrite, default readonly). The full key is shown ONCE only — save it immediately; the store keeps only the hash. SCOPES: read for readonly, read+write for readwrite." },
      { name: "revoke_api_key", desc: "PERMANENTLY delete an API key (id from list_api_keys; there is no disable/re-enable — create a new key). WARNING: if the id is the key authenticating this session, it will cut off its own access immediately — set confirm=true to confirm; otherwise the call is rejected." },
    ],
  },
  {
    // t_cde2e73a: the affiliate tools (#444) had no row of their own on this
    // page, so the reference could not be "kept in sync with the tool
    // registry" while claiming to be.
    group: "Affiliates",
    tools: [
      { name: "create_affiliate", desc: "Create an affiliate for a workspace: referral code (?aff=CODE), name, email, commission rate. Sales brought in by the affiliate are credited to it." },
      { name: "list_affiliates", desc: "List all affiliates in a workspace — id, code, name, commission, referral URL." },
      { name: "get_affiliate_sales", desc: "List sales brought in by a single affiliate (read-only, workspace scope)." },
      { name: "set_affiliate_commission", desc: "Set the affiliate commission rate (e.g. 10 = 10%)." },
    ],
  },
  {
    group: "Workspaces & storefronts",
    tools: [
      { name: "list_workspaces", desc: "List all your workspaces (storefronts) — id, name, slug and URL." },
      { name: "create_workspace", desc: "Create a new workspace. Name up to 120 characters (counted as CHARACTERS, not bytes). Slug 3-40 characters (a-z, 0-9, dash); the URL becomes https://<slug>.jomform.com. Names reserved for JomForm (e.g.: www, api, app, mail, s3, docs, mcp, templates, custom, support, help, status, billing, admin, account, login, payments, checkout, cdn, assets) are rejected — those names belong to JomForm or protect users from impersonation (e.g.: billing.jomform.com impersonating JomForm's official channel)." },
      { name: "update_workspace", desc: "Change the NAME and/or SLUG (public address) of a workspace. Empty field = keep existing. Name up to 120 CHARACTERS (not bytes; same limit as create). Changing the name requires the manage/admin role; changing the SLUG is OWNER-only, and requires confirm=true — the slug is the tenant's public host (https://<slug>.jomform.com) already printed in QR codes, iframes and affiliate links. The OLD slug is kept as an alias and KEEPS WORKING (permanent move to the new slug), so links already spread around do not die. Returns previous_url + previous_url_redirects. Names reserved for JomForm (same list as create_workspace: www, api, billing, support, docs, mcp, templates, …) are REJECTED as status=refused with code reserved_subdomain — that name belongs to JomForm or protects users from impersonation, and the workspace slug does not change. REST: PATCH /v1/workspaces/{id}." },
      { name: "get_workspace_settings", desc: "Read the storefront settings of one workspace: footer, company_reg, privacy/terms/refund URL, contact, menu, indexing, sound_enabled and spam_judge_enabled (true = on/default)." },
      { name: "set_workspace_settings", desc: "Set workspace storefront settings: footer_text, company_reg, privacy_url/terms_url/refund_url (must be https://), contact_phone, contact_address, business_name, menu_json, default_lang (default language of the PUBLIC form for this workspace — en, ms, zh, ta or ar; this is the buyer's language, not your dashboard language), indexing, sound_enabled and spam_judge_enabled (false = TURN OFF AI spam detection for this workspace — no AI job is submitted at all and is_spam is no longer set, so /spam stays empty). Omit a field = keep existing." },
      { name: "get_workspace_landing_page", desc: "Read the workspace's 'landing page' configuration (issue #360): hero, subtitle, body, image_url, theme colors, logo, CTA (label + form_slug). Empty if no landing page is set." },
      { name: "set_workspace_landing_page", desc: "Set the workspace storefront 'landing page' (issue #360): enabled (true = show, false/empty = fall back to the form list), hero, subtitle, body, image_url (must be https:// — issue #501), primary_color, background_color, logo_url (must be https:// — issue #501), ctas ([{label, form_slug}]). Send enabled:false to return to the form list." },
      { name: "generate_qr", desc: "Generate a QR code for a published form (issue #438): form_slug (e.g. 'menu') + optional table (table number, encoded as ?table=N). Returns qr_url (endpoint /qr/{slug}.png), data_url (PNG base64) and target_url (the encoded URL). Use for QR stands on restaurant tables." },
      { name: "get_menu_page", desc: "Read the restaurant menu configuration of one FORM (issue #497): form_id. Returns enabled, categories ([{name, product_ids}]), best_seller, best_seller_ids (list, #485), order_form_slug (form for additional order information, #491/#493). The menu is a PER-FORM setting and is displayed at that form's own URL. Empty if the form has no menu." },
      { name: "set_menu_page", desc: "Set the restaurant menu for one FORM (issue #497): form_id, enabled (true = this form becomes a menu at its URL; false = the form returns to normal), categories ([{name, product_ids}]), best_seller (product ID) or best_seller_ids (list of IDs for the Best Seller section, #485 — overrides best_seller), order_form_slug (optional, #491/#493 — a published form in the same account whose block fields become additional fields during menu checkout; fields are rendered with that form's label/required, validated server-side, and stored on the sale; empty = name + email checkout only, both still required for CHIP). Menu products come from that FORM's own product blocks (not all account products), so products from other accounts cannot leak in; categories/best-seller must reference products referenced by the form. The menu page shows the table number from ?table=N and has a category filter + search. A form without product blocks displays an empty state." },
      { name: "get_currency_rates", desc: "BNM currency exchange rates (Ringgit per unit of foreign currency), cached daily. Reference data — JomForm does not convert stored amounts; use convert_currency to convert a single amount." },
      { name: "convert_currency", desc: "Convert a single amount between currencies using the cached (daily) BNM rates. amount_minor must be an integer in the smallest unit (cents) of the source currency: 10000 = 100.00. Returns converted_minor in the smallest unit of the target currency (e.g. 260 = RM 2.60). For display/reporting only — not for charging: sales settle in the workspace currency and past sales are never converted." },
      { name: "set_workspace_currency", desc: "Set the workspace currency (default MYR). The change applies to new products/sales; past sales keep their original currency (not converted back)." },
      { name: "check_workspace_deletion", desc: "Check whether a workspace can be sent to the trash (read-only, no effect). Returns can_delete, empty, last_workspace, is_owner and blockers[] (each category + count). can_delete = owner AND not the last workspace — content NO LONGER blocks (blockers[] is reported for reference before purge). Use this first so you can state the reason, not fail after clicking." },
      { name: "delete_workspace", desc: "Send a workspace to the TRASH (owner only — admin members cannot; they leave via remove_workspace_member). Nothing is destroyed: forms, products, sales, CHIP accounts and API keys all remain, and restore_workspace brings it back. TWO conditions: (1) confirm_slug must match the workspace slug; (2) you must have at least one OTHER workspace — the last workspace cannot be removed, because a user must have one workspace. A FULL workspace can go to the trash (the 'must be empty' guard has been removed); PERMANENT delete is only via purge_workspace, and chat can never suggest that tool — it is an MCP tool for the owner, not for chat. Use check_workspace_deletion first to see can_delete." },
      { name: "list_trashed_workspaces", desc: "List the workspaces in your TRASH (deleted_at IS NOT NULL): id, slug, name, deleted_at. Own only — only the owner can restore or permanently delete." },
      { name: "restore_workspace", desc: "Restore a workspace from the trash (deleted_at=NULL). Nothing is destroyed — the workspace comes back with its accounts, forms and sales. If its original slug was taken by another workspace while it was in the trash, the restore is REJECTED (no automatic rename). Owner only." },
      { name: "purge_workspace", desc: "PERMANENTLY delete a workspace from the trash (its forms, products, API keys and sales fall with it; the CHIP account CREATED together with that workspace also falls, but an account that ALREADY EXISTED before the workspace — the signup account used when the first workspace was created — is NOT destroyed: it is released back as an account without a workspace, and the accounts_unlinked count reports it). WARNING: cannot be undone. Requires confirm_slug = that workspace's slug. Only a workspace IN THE TRASH — send it to the trash first with delete_workspace. A workspace WITH SALES is rejected (sales are never destroyed or orphaned). NEVER SUGGESTED BY CHAT: in-app chat must not suggest this tool; it is an MCP tool for owners, the same class as purge_form/purge_product/purge_media." },
    ],
  },
  {
    group: "Workspace members",
    tools: [
      { name: "invite_workspace_member", desc: "Invite a member to the workspace (admin only). If the email is already registered, add them directly as a member; if not, create a pending invitation. Role: read_only|manage|admin." },
      { name: "list_workspace_members", desc: "List workspace members + roles (admin only)." },
      { name: "update_workspace_member_role", desc: "Change a workspace member's role (admin only). Role: read_only|manage|admin." },
      { name: "remove_workspace_member", desc: "Remove a member from the workspace (admin only). The owner cannot be removed." },
      { name: "list_workspace_invites", desc: "List pending invitations for the workspace (admin only)." },
      { name: "cancel_workspace_invite", desc: "Cancel a pending invitation for the workspace (admin only)." },
      { name: "set_default_workspace", desc: "Set your default workspace (the workspace you enter directly after logging in, without a prompt). Empty workspace_id = remove the default. The workspace must belong to you." },
    ],
  },
  {
    group: "Profile & security",
    tools: [
      { name: "update_profile_phone", desc: "Set an optional phone number on your profile (empty = remove). Loose format: digits, +, spaces, 8-15 digits." },
      { name: "update_profile_timezone", desc: "Set an optional IANA timezone on your profile (empty = Auto, follow the browser/PC). Examples: Asia/Kuala_Lumpur, UTC." },
      { name: "update_profile_language", desc: "Set an optional DASHBOARD language on your profile (per-user, follows you to any browser): en, ms, zh, ta or ar (empty = auto, follow that browser's last value, then en). This is the DASHBOARD language only and it is per-profile — public form language is set per-form (config.lang) or per-workspace (default_lang via set_workspace_settings), not here." },
      { name: "update_profile_avatar", desc: "Set the profile avatar (the URL must be an image hosted by JomForm from the upload bucket). Empty = remove the avatar." },
      { name: "list_passkeys", desc: "List your account's passkeys (WebAuthn): id, created_at. Read-only." },
      { name: "delete_passkey", desc: "Delete one passkey (id from list_passkeys)." },
      { name: "list_social_accounts", desc: "List linked social accounts (google|azure_ad|github|cloudflare): provider, email, name, picture, linked_at. Read-only." },
      { name: "unlink_social_account", desc: "Unlink a social account (provider from list_social_accounts)." },
      { name: "get_2fa_status", desc: "Read your account's 2FA status: enabled (true/false). Read-only." },
      { name: "setup_2fa", desc: "Generate a TOTP secret for 2FA (not yet active). Returns secret + otpauth_url to be scanned by an authenticator app, then call enable_2fa with the code." },
      { name: "enable_2fa", desc: "Enable 2FA with the 6-digit code from the authenticator app (after setup_2fa)." },
      { name: "disable_2fa", desc: "Disable 2FA on your account. Accepted proof — EXACTLY ONE of the two, not both: the current 2FA code in totp_code (a 6-digit code from the authenticator app, checked once only and not requiring a password) OR the account password in password (for accounts that joined through an email link and have no usable password). Sending both is rejected by the schema, and without either the call is rejected. An API key alone is never enough to turn off the second factor. (The dashboard form (Profile → Security) asks for the current 2FA code.) Turning off 2FA also revokes EVERY trusted device on the account — a device that skipped the factor that has now been removed cannot stay exempt (same as REST and the dashboard profile form; use list_trusted_devices to review the remaining devices)." },
      { name: "list_trusted_devices", desc: "List your account's trusted 2FA devices (label, ip, first_seen, expires_at). Trusted devices skip the 2FA code for 30 days. Read-only." },
      { name: "revoke_trusted_device", desc: "Revoke one trusted device (device_id from list_trusted_devices). The next sign-in on that device will ask for a 2FA code again — immediately." },
      { name: "revoke_all_trusted_devices", desc: "Revoke ALL trusted devices on your account (\"log out everywhere\"). Every device will ask for a 2FA code again." },
    ],
  },
  {
    // t_cde2e73a: the feedback tools (#639) are user-scoped and were documented
    // only in /docs/feedback. The reference claimed to be the complete registry,
    // so they belong here too. Kept beside Profile & security: both are USER
    // scoped, not workspace scoped.
    group: "Feedback",
    tools: [
      { name: "list_feedback", desc: "List your own feedback threads (newest first), filterable by status (open|in_progress|resolved|closed) and paginated (page/limit). The scope is the authenticated USER, not a workspace — feedback is not account data. Super-admin sees ALL threads (triage). Read-only." },
      { name: "get_feedback_thread", desc: "Read one feedback thread together with all its messages (author_kind: user|admin), earliest first. A thread belonging to another user answers 'not found' — the same as an id that does not exist. Super-admin can read any thread. Read-only." },
      { name: "reply_feedback", desc: "Reply in a feedback thread. The author is determined by the caller: super-admin replies as admin (and an 'open' thread moves to in_progress), other users reply as the thread owner. Body minimum 10 characters. User scope — you cannot reply to someone else's thread." },
      { name: "set_feedback_status", desc: "Change the status of a feedback thread (open|in_progress|resolved|closed). A triage decision — super-admin only; audited. Status is per-thread, not per-message." },
      { name: "retry_feedback_notify", desc: "Resend the email notification that FAILED for one feedback thread (the notify_failed_at field on the thread). This is the recovery path for delivery failures: an email that never made it into the queue is marked on the thread so it is visible, and this tool retries through the same channel. Outcome: queued|sent|failed|none|skipped ('none' means no failure was recorded; 'skipped' means no super-admin recipient is configured, so no email was attempted and no job was queued — check the super-admin list). Super-admin only; audited." },
      { name: "submit_feedback", desc: "File feedback or a new feature request from a chat conversation. Same as submitting on the /feedback page: stored as a thread you can read and reply to, and the JomForm team is notified. Body minimum 10 characters, maximum 5000; maximum 10 submissions a day per user. Requires a USER token (OAuth/session) and readwrite scope — API keys are rejected because feedback is user personal data." },
    ],
  },
  {
    // The magic-link / step-up rule is prose, not a tool: this tree registers no
    // tool for it, so it is a group NOTE. It shipped as a `{ name: "—" }` row,
    // which the rendered count counted as a tool.
    group: "Session safety",
    note: "Sign-in sessions created through an email link (magic link) are marked magic. When 2FA is enabled, sensitive actions (change password, disable 2FA, add a passkey) require a fresh 2FA code. Users without 2FA, or with an ordinary password sign-in session, are unaffected.",
    tools: [],
  },
  {
    group: "Legal pages",
    tools: [
      { name: "generate_legal_page", desc: "Generate a draft legal page (privacy|terms|refund) for a workspace via the LLM (deepseek-v4.1-flash:1017) with a template filled with the business details. Returns the draft for review — not saved until set_legal_page is called." },
      { name: "get_legal_page", desc: "Read one workspace legal page (privacy|terms|refund): mode (auto/manual) + content." },
      { name: "set_legal_page", desc: "Save the workspace's legal page content (privacy|terms|refund) with mode auto|manual. The content is served at {workspace}.jomform.com/privacy|terms|refund." },
    ],
  },
  {
    group: "Event tracking",
    tools: [
      { name: "list_events", desc: "Account audit/behaviour log (newest first). Can filter by event_name, from/to (RFC3339), and page/limit. Read-only. On the dashboard the same log appears on the Activity page — and is workspace-scoped there via ?workspace=<slug>." },
    ],
  },
  {
    group: "Email delivery",
    tools: [
      { name: "list_email_deliveries", desc: "Account email delivery status (newest first): delivered/bounce/complaint/open/click, plus enqueue_failed (the email could not be queued at all, so it was never sent) — with email, time, sale_ref and detail. Can filter form_id, sale_ref, event_type, and page/limit. Read-only." },
    ],
  },
  {
    group: "Form submissions",
    tools: [
      { name: "list_form_submissions", desc: "List responses (submissions) for a response-only form (mode 'response', no payment). Returns field data + time. Filter form_id, page/limit." },
    ],
  },
  {
    group: "Spam review",
    tools: [
      { name: "list_spam_submissions", desc: "List submissions flagged as spam by AI (issue #245): field data, spam reason, time. Can filter page/limit. Read-only." },
      { name: "unspam_submission", desc: "Un-flag spam on a submission (false positive). Write tool (manage/admin role)." },
      { name: "list_spam_sales", desc: "List sales flagged as spam by AI (issue #245): ref, status, amount, buyer, spam reason, time. Can filter page/limit. Read-only." },
      { name: "unspam_sale", desc: "Un-flag spam on a sale (false positive). Write tool (manage/admin role)." },
    ],
  },
  {
    group: "Media",
    tools: [
      { name: "list_media", desc: "List media uploaded by the account (media library): url, name, date, size, and thumbnail/medium/full for images whose sizes are generated. Can filter form_id and source (merchant|customer). Read-only." },
      { name: "upload_media", desc: "Upload media/assets to your workspace (issue #363): kind='image' (png/jpeg/webp/gif — optimised, EXIF stripped; opaque images are stored as JPEG (URL ends .jpg), images with a transparent background are stored as PNG (URL ends .png) so the transparency is preserved; an animated GIF becomes a still image (first frame only); thumbnail/medium/full are generated for every image), kind='asset' (css/js/html/svg/woff2 — stored as-is, sniffed before being served; svg MUST be valid XML — text before the root element, for example banner /* */, is rejected because the browser will show a parsererror) or kind='pdf' (stored as-is, immediately usable). Send the file content as base64 in 'data'. Returns a URL to use as a landing image_url/logo_url or a landing page <link>/<script>. 'size' in the response is the TOTAL STORED (base object + image variants / PDF preview), the SAME number list_media returns for that id — not the number of bytes you sent." },
      { name: "delete_media", desc: "Send media to the TRASH (deleted_at set). Bytes are NOT destroyed and quota is NOT released — the object is still in storage, so it still counts against the 1GB limit. The same URL keeps working after restore_media. Use purge_media to destroy the bytes permanently." },
      { name: "list_deleted_media", desc: "List media in the TRASH: id, key, url, mime, size, form_id, deleted_at and object_present. object_present=false means the bytes are already gone from storage (deleted by an operator or a lifecycle rule) — restore will be REJECTED." },
      { name: "restore_media", desc: "Restore media from the trash (deleted_at=NULL). The SAME URL keeps working. REJECTED (409) when the object is already gone from storage — restoring the row would only hand back a URL that 404s." },
      { name: "purge_media", desc: "Permanently delete media from the trash. WARNING: cannot be undone. This is the only place bytes are deleted from storage AND the 1GB quota is released (size variants + PDF previews are deleted too), and the place where EDGE CDN copies are purged in the same call so those URLs stop being downloadable. The response carries 'edge' (purged | not_configured | no_urls | out_of_zone | failed) and 'edge_urls' only when the CDN accepted at least one purge request; on not_configured, no_urls and out_of_zone that field is absent entirely, and on failed it lists only the accepted ones; when there are no CDN credentials it mentions 'edge_max_age_s' — the maximum period the edge can still serve its copy. Only media in the trash — send it to the bin first with delete_media." },
    ],
  },
  {
    group: "Templates",
    tools: [
      { name: "list_templates", desc: "List system form templates (gallery): slug, title, category, mode (payment/response). Use with use_template for quickstart." },
      { name: "use_template", desc: "Clone a form template into a new draft in your account. Fill slug (from list_templates) for a system template, or template_id (from list_own_templates) for your own template. Returns form_id + slug to open in the builder." },
      { name: "list_own_templates", desc: "List your own templates (issue #457): id, slug, title, config, theme. Different from list_templates (system gallery)." },
      { name: "create_template", desc: "Save an existing form as a reusable template (issue #457): fill form_id (from list_forms) to copy that form, or title + config for a new template. Templates are owned by your account only." },
      { name: "update_template", desc: "Update your own template (issue #457): title, config and/or theme. Omit fields that do not change. template_id from list_own_templates." },
      { name: "delete_template", desc: "Delete (soft-delete) your own template (issue #457). template_id from list_own_templates. Can be created again at any time." },
    ],
  },
  {
    group: "Analytics",
    tools: [
      { name: "get_form_analytics", desc: "Per-form analytics: number of entries (submissions), paid/confirmed/failed/pending, and NET revenue after refunds. submissions = entries recorded for that form, read from the table the form writes: mode=response → submissions table; payment form → sales (all statuses). mode is included so the label is correct. Money in minor units: revenue_sen and refunded_sen are totals across currencies (do not label with a single symbol), broken down by the currency of each sale in revenue_by_currency and refunded_by_currency (keys are ISO codes, values in minor units). Across all your workspaces. Read-only." },
      { name: "get_product_analytics", desc: "Per-product analytics: units sold (paid), revenue. Money in minor units: revenue_sen (gross value of all sales) and paid_revenue_sen (paid, NET after refunds) are totals across currencies (do not label with a single symbol), broken down by the currency of each sale in revenue_by_currency and paid_revenue_by_currency (keys are ISO codes, values in minor units). Across all your workspaces. Read-only." },
    ],
  },
  {
    group: "Custom domains",
    tools: [
      { name: "list_domains", desc: "List the account's custom domains: id, domain, provider, verified, auto_cname. Read-only." },
      { name: "add_domain", desc: "Claim a new custom domain (e.g. kedai.com). Then set the CNAME to the target returned by add_domain and call verify_domain." },
      { name: "verify_domain", desc: "Verify a custom domain (id from list_domains). Only verified when the domain actually serves a JomForm page." },
      { name: "remove_domain", desc: "Remove a custom domain claim (id from list_domains)." },
      { name: "auto_cname_domain", desc: "Create a Cloudflare CNAME record automatically for the domain (Cloudflare must be linked). Returns auto_cname=created|failed." },
      { name: "get_domain_tls_email", desc: "Read the per-domain ACME/Let's Encrypt email for auto-TLS. Empty = fall back to the account email, then the global ACME_EMAIL. Read-only." },
      { name: "set_domain_tls_email", desc: "Set the per-domain ACME/Let's Encrypt email for auto-TLS. Empty = fall back to the account email, then the global ACME_EMAIL." },
      { name: "set_domain_tls_mode", desc: "Set the per-domain TLS mode: auto (Let's Encrypt, default) or custom (bring-your-own-cert). In custom mode, upload the certificate with set_domain_custom_cert." },
      { name: "set_domain_custom_cert", desc: "Upload your own TLS certificate (bring-your-own-cert) for the domain: cert + key in PEM format. The domain is served this certificate." },
    ],
  },
  {
    // Issue #513: the realtime channel (#447) was documented only in the tool
    // description. It gets its own reference group so the docs-coverage guard
    // passes on the shipped behaviour rather than on an exemption.
    group: "Realtime notifications",
    tools: [
      { name: "get_realtime_status", desc: "Read the workspace's real-time notification status (issue #447): stream_url (SSE /v1/rest/events/stream — real-time channel for sales/responses), list of events (sale.paid, sale.failed, response.submitted, subscription.charged, sale.refunded), and sound_enabled (beep sound on the dashboard). Read-only. Connect to the stream for instant notifications on a kitchen/order screen." },
      { name: "set_realtime_sound", desc: "Turn the workspace's real-time notification beep on/off (issue #447). enabled=true = the dashboard beeps when a new sale/response arrives (e.g. kitchen screen); false = silent. Write tool (manage/admin role)." },
    ],
  },
  {
    group: "Tracking",
    tools: [
      { name: "get_tracking", desc: "Read the account's global tracking scripts (Google Analytics, Facebook Pixel, etc.) that are injected into the <head> of public forms. Read-only." },
      { name: "set_tracking", desc: "Set the account's global tracking scripts. Only <script src=https://...>, <meta> and comments are allowed (sanitized)." },
    ],
  },
  {
    group: "System settings",
    tools: [
      { name: "get_system_settings", desc: "Read all platform system settings (site_*, landing_*). Read-only." },
      { name: "set_system_setting", desc: "Set one system setting (key must start with site_ or landing_)." },
    ],
  },
  {
    group: "Cloudflare",
    tools: [
      { name: "get_cloudflare_status", desc: "Read the account's Cloudflare link status: linked (this account's Cloudflare credentials can be read) and configured — same meaning as /rest/cf/status: this DEPLOYMENT has a Cloudflare OAuth app, so the auto-CNAME flow can be offered. The OAuth link can only be completed in a browser; from MCP use connect_cloudflare_api_token or connect_cloudflare_global_key. Read-only." },
      { name: "connect_cloudflare_api_token", desc: "Link Cloudflare via API token (verified immediately, stored encrypted)." },
      { name: "connect_cloudflare_global_key", desc: "Link Cloudflare via global API key (verified immediately, stored encrypted)." },
      { name: "disconnect_cloudflare", desc: "Unlink Cloudflare from the account." },
    ],
  },
  {
    group: "Account",
    tools: [
      { name: "get_account", desc: "Basic account info (name, slug, CHIP mode)." },
    ],
  },
  {
    group: "Admin (super-admin, platform-wide)",
    tools: [
      { name: "purge_unverified_accounts", desc: "Report the number of unverified accounts (email) older than the threshold, BROKEN DOWN into how many the sweep will delete and how many are BLOCKED (the account still holds sales or subscriptions, so it will not be deleted). Admin only; read-only — deletes nothing." },
      { name: "purge_test_accounts", desc: "Delete TEST accounts flagged is_test created through the QA signup channel. Super-admin only; audited. The DEFAULT is a DRY RUN (confirm=false): report exactly what WILL be deleted and delete nothing; set confirm=true to actually delete. It REJECTS accounts not flagged as test (including those whose email looks like a test account), and BLOCKS — does not delete — test accounts that still hold sales or subscriptions. Protected accounts are never touched. The response NAMES every account (id + email + counts): removed, removed_emails, removed_count, blocked (reason: sales|subscriptions|fk_restricted), blocked_count, scanned. Idempotent — a second run deletes nothing and reports that. There is no age limit and no creator filter: an account can be deleted as long as it IS FLAGGED." },
      { name: "admin_list_workspaces", desc: "List ALL workspaces (storefronts) on the platform: id, name, slug, owner email, created date, number of forms + products. Super-admin only; read-only; audited." },
      { name: "admin_media_usage", desc: "Media storage usage per workspace (bytes) against the 1GB quota per workspace. Super-admin only; read-only; audited." },
      { name: "admin_list_sales", desc: "List sales across ALL workspaces, filterable (date_from/date_to, workspace_id, status). Includes refunded_sen/outstanding_sen/refund_status (issue #518). Super-admin only; read-only; audited." },
      { name: "admin_spam_queue", desc: "List all spam items (submissions + sales) across all workspaces. Super-admin only; read-only; audited." },
      { name: "admin_email_deliveries", desc: "Email delivery status across ALL workspaces (delivered/bounce/complaint/open/click), filterable by sale_ref/event_type/form_id. Super-admin only; read-only; audited." },
      { name: "admin_dead_letter_events", desc: "List inbound callback events parked as dead letters (issue #574): dedup_key, event_type, attempt count, reason, time. After #569 unprocessable events are parked as 'dead' and STOP being retried — before this they were not visible at all (no MCP/REST reads webhook_events). Super-admin only; read-only; audited. On the /admin page the same list is shown with a requeue button per row (GET /v1/admin/dead-letters, issue #579), so an operator can see failed callbacks without needing an MCP client." },
      { name: "admin_requeue_dead_letter_event", desc: "Re-enable the dead letter event so the replay sweep tries it again (issue #574), after the cause is fixed. dedup_key from admin_dead_letter_events. The attempt count is reset so it is not re-parked by the same sweep before it gets to run. Super-admin only; audited. From the /admin page the same action can be run (POST /v1/admin/dead-letters/requeue, issue #579)." },
      { name: "admin_system_health", desc: "System health: uptime, number of recent MCP errors (24h), email queue depth. Super-admin only; read-only; audited." },
      { name: "admin_list_suppressions", desc: "List of addresses blocked from being sent email (issue #601): email, reason (hard_bounce|complaint|manual), source, note, who added it and when. Searchable (q, matches email or note) and paginated (page/limit). Super-admin only; read-only; audited. On the /admin page the same list is displayed with a search box and pager (GET /v1/admin/suppressions, issue #601)." },
      { name: "admin_add_suppression", desc: "Add an address to the suppression list manually so that no email is sent to it (issue #601). reason: hard_bounce|complaint|manual (default manual), note optional. Idempotent — an address already on the list does not produce an error. Super-admin only; audited. From the /admin page it is added through the manual form (POST /v1/admin/suppressions, issue #601)." },
      { name: "admin_remove_suppression", desc: "Remove an address from the suppression list so that email delivery to it resumes (issue #601) — for false-positive cases. Super-admin only; audited. From the /admin page it is removed with a button + confirmation (DELETE /v1/admin/suppressions/{email}, issue #601)." },
    ],
  },
];

// SHORT ON PURPOSE. Every tool name, field name and asserted behaviour survives,
// because /docs is what a developer greps. The section ids are link targets from
// /docs and from the dashboard's inline "?" links.
const SECTIONS: Section[] = [
  {
    id: "connect",
    title: "Connect an agent",
    icon: "🔌",
    body: [
      "JomForm speaks the **Model Context Protocol**. Point any MCP client at `https://mcp.jomform.com/mcp` and it discovers the tools on its own.",
      "This page is the **whole registry**: the `api/internal/mcpsrv` package at this release.",
      "A guard fails the build if the page and the registry drift apart in either direction.",
      {
        note: "The connector authenticates with **OAuth 2.1** using an authorization code and PKCE. There are no API keys to copy or rotate.",
      },
    ],
  },
  {
    id: "oauth-discovery",
    title: "OAuth discovery",
    icon: "🔐",
    body: [
      "A client discovers how to authenticate from the server itself. Every step below is a document or a challenge it can read.",
      {
        steps: [
          "An unauthenticated call to `https://mcp.jomform.com/mcp` is refused with `401` and `WWW-Authenticate: Bearer resource_metadata=\"…\"`.",
          "That header names the **protected-resource metadata** URL.",
          "The client fetches `/.well-known/oauth-protected-resource`, defined by RFC 9728.",
          "The document names the resource, the authorization server, the scopes and the token header.",
          "It then fetches `/.well-known/oauth-authorization-server`, RFC 8414, for the authorize, token, revoke and registration endpoints.",
          "It registers itself dynamically and opens the authorize URL in the browser, where you sign in and grant access.",
          "It exchanges the code for an access token, sending the `resource` parameter on both requests.",
          "JomForm rejects a `resource` that is not this MCP endpoint, and issues no token.",
          "Every later call to `/mcp` carries the token as a Bearer credential.",
        ],
      },
      "The resource is `https://mcp.jomform.com/mcp`. The authorization server is `https://mcp.jomform.com`, and the scopes are `read` and `readwrite`.",
    ],
  },
  {
    id: "structured-output",
    title: "Structured output",
    icon: "📦",
    body: [
      "Tool results come back as **structured content**. Every tool declares the shape of that result in its `outputSchema`.",
      "All tools declare an **object** schema. That is what keeps the server usable by a client that validates `tools/list` strictly.",
      "Every listing tool wraps its array in an object: the records sit under `items`.",
      "A paginated listing also carries `page`, `limit` and `total`.",
      "So `list_products` returns `{\"items\": [...]}`, not a bare array.",
      "Batch tools report their summary beside the per-item outcomes. `capture_all_pending_sales` returns `{\"requested\", \"captured\", \"failed\", \"results\": [...]}`.",
      "Single-record tools return their fields at the top level of the object.",
    ],
  },
  {
    id: "connector-scopes",
    title: "OAuth connector scopes",
    icon: "🎫",
    body: [
      "A connector's grant carries one of two **scopes**. The MCP client asks for the one it wants on the authorize request.",
      "The request carries `?scope=…`, space-delimited. JomForm grants exactly what was asked for and records it on the token.",
      "Both scopes are advertised in the protected-resource and authorization-server metadata, beside the other fields a client needs.",
      {
        list: [
          "`read`: read-only. Every listing and getter works. Every tool that creates, updates, deletes, archives, cancels, refunds or generates a stored key is refused with a scope error.",
          "A `read` grant cannot upgrade itself. `create_api_key` is a write, so a read-only connector can never mint a wider credential.",
          "`readwrite`: read and write. A request that omits `scope` defaults to readwrite, so an existing client is never silently downgraded.",
        ],
      },
      "The scope is **fixed for the life of the connection**. Refreshing an access token returns the same grant, so a read grant never widens.",
      "A request that names a scope the server does not advertise is refused with `invalid_scope`.",
      "It is never silently widened to readwrite, or narrowed to read.",
      "The token response echoes the **issued grant**, not the advertised list, so a client can read back what it holds.",
    ],
  },
  {
    id: "api-key-scopes",
    title: "API-key scopes",
    icon: "🔑",
    body: [
      "An API key carries a **scope**, which says what that credential may do at all.",
      {
        list: [
          "`readonly`: read tools only. Every read works.",
          "Every tool that creates, updates, deletes, archives, cancels, revokes or generates a stored key is refused with a scope error.",
          "A readonly key cannot mint another key, so it cannot escalate itself to readwrite.",
          "`readwrite`: read and write. It is the default grant for an OAuth connector unless the client asks for `read`.",
          "`admin`: kept for existing keys. It satisfies every scope, so it behaves as readwrite.",
        ],
      },
      "Scope is a separate **axis** from your workspace role: `read_only`, `manage` or `admin`.",
      "A write has to satisfy both. A `readwrite` key held by a `read_only` member is still refused, and so is a `readonly` key held by the owner.",
      "Reading never requires the write scope.",
      "One tool looks wider than it is. `get_webhook_signing_key` needs `readwrite` only when it must create the account's keypair on first use.",
      "Reading an existing key does not.",
    ],
  },
];


export default function McpReferencePage() {
  return (
    <div className="max-w-[36rem]">
      <DocsPage
        title="MCP tool reference"
        lead="Every tool JomForm exposes to a connected agent, with the OAuth discovery and scope rules a client has to follow."
        sections={SECTIONS}
      />

      <section id="tools" className="mt-10 scroll-mt-24">
        <h2 className="flex items-center gap-2 text-lg font-semibold text-[var(--jf-text)]">
          <span aria-hidden="true" className="text-base">
            🧰
          </span>
          Tool reference
        </h2>
        <p className="mt-2 text-base leading-7 text-[var(--jf-text-secondary)]">
          {TOOL_GROUPS.reduce((n, g) => n + g.tools.length, 0)} tools across{" "}
          {TOOL_GROUPS.filter((g) => g.tools.length > 0).length} categories. Every row below is a tool the registry
          registers.
        </p>

        {TOOL_GROUPS.map((g) => (
          <div key={g.group} className="mt-8">
            <h3 className="text-base font-semibold tracking-tight text-[var(--jf-text)]">{g.group}</h3>
            {g.note && (
              <p className="mt-2 text-base leading-7 text-[var(--jf-text-secondary)] [overflow-wrap:anywhere]">
                {g.note}
              </p>
            )}
            <div className="mt-3 space-y-3">
              {g.tools.map((t) => (
                <div key={t.name} className="rounded-xl border border-[var(--jf-border)] p-4">
                  <code className="break-all font-mono text-sm font-semibold text-[var(--jf-primary-strong)]">{t.name}</code>
                  <p className="mt-1 text-sm leading-6 text-[var(--jf-text-secondary)] [overflow-wrap:anywhere]">
                    {t.desc}
                  </p>
                </div>
              ))}
            </div>
          </div>
        ))}
      </section>
    </div>
  );
}
