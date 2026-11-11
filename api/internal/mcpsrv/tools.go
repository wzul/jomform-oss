// Package mcpsrv — server construction.
package mcpsrv

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// version reported in the MCP implementation info.
const version = "0.1.0"

// New builds the JomForm MCP server with all tools registered.
func New(deps *Deps, chipClient *chip.Client, decryptKey []byte, pay *payments.Service) *Server {
	s := &Server{
		mcp: mcp.NewServer(&mcp.Implementation{
			Name:    "jomform",
			Version: version,
		}, nil),
		deps:   deps,
		chip:   chipClient,
		decKey: decryptKey,
		pay:    pay,
		// Issue #386: uptime baseline for admin_system_health.
		startedAt: time.Now(),
	}
	s.registerTools()
	// The lifecycle table is also a resource, so a client can read it without
	// spending a tool call (and agents can list it under resources/list).
	s.registerLifecycleResource()
	return s
}

// Handler returns the root MCP server (for the streamable HTTP transport).
func (s *Server) Raw() *mcp.Server { return s.mcp }

func (s *Server) registerTools() {
	// ── Products ────────────────────────────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_products",
		Description: "List all active products in your JomForm account.",
	}, s.listProducts)

	addTool(s, &mcp.Tool{
		Name:        "create_product",
		Description: "Create a new product. For subscriptions, set type='subscription' and billing_period='month'|'year'.",
	}, s.createProduct)

	addTool(s, &mcp.Tool{
		Name:        "archive_product",
		Description: "Deactivate a product (shelve without deleting data). This is NOT trash — it stays in the list as an archive. To remove it from the list use delete_product.",
	}, s.archiveProduct)

	// Tong sampah produk: sejajar dengan Tong Sampah borang. delete_product
	// menghantar ke bin (bukan archive); restore_product memulihkan; purge_product
	// memusnahkan kekal selepas disemak (confirm_name) dan HANYA bila tiada
	// jualan/subscription.
	addTool(s, &mcp.Tool{
		Name:        "delete_product",
		Description: "Send a product to TRASH (deleted_at set) — it leaves the product list and can be restored with restore_product. This differs from archive_product (active=false), which keeps it in the list. To deactivate only, use archive_product.",
	}, s.deleteProductMCP)
	addTool(s, &mcp.Tool{
		Name:        "list_deleted_products",
		Description: "List products in TRASH: id, name, price_sen, deleted_at, sales_count, subscription_count and can_purge. can_purge=false means the product is still referenced by sales (history) or subscriptions (billing still running) — a permanent delete will be REJECTED.",
	}, s.listDeletedProducts)
	addTool(s, &mcp.Tool{
		Name:        "restore_product",
		Description: "Restore a product from trash (deleted_at=NULL). Nothing is destroyed and no name has to be resolved — products have no slug — so a restore cannot collide.",
	}, s.restoreProductMCP)
	addTool(s, &mcp.Tool{
		Name:        "purge_product",
		Description: "PERMANENTLY delete a product from trash (its variations fall with it via CASCADE). WARNING: cannot be undone. Required confirm_name = that product's name. REJECTED when the product has sales (sales history is not destroyed) or still has subscriptions (FK RESTRICT). Only products in the trash — send to the bin first with delete_product.",
	}, s.purgeProductMCP)

	addTool(s, &mcp.Tool{
		Name:        "list_product_variations",
		Description: "List a product's variations (name, price_sen, stock, images, sort_order, and its own subscription cycle when set) for one product. Simple product = empty list. The billing_period/trial_days/subscription_charges fields are present ONLY when the variation sets them itself; if absent, that variation inherits the product's cycle.",
	}, s.listProductVariations)

	addTool(s, &mcp.Tool{
		Name:        "create_product_variation",
		Description: "Add ONE variation to a product (e.g. Size M), with its own price, stock, images and subscription cycle (billing_period month|year, trial_days, subscription_charges). Leave empty = the variation inherits the product's cycle.",
	}, s.createProductVariation)

	addTool(s, &mcp.Tool{
		Name:        "update_product_variation",
		Description: "Update ONE variation (name, price, stock, images, subscription cycle). Fields not supplied stay unchanged. Set clear_billing_period / clear_trial_days / clear_subscription_charges = true to REMOVE that variation's cycle so it inherits the product's cycle again.",
	}, s.updateProductVariation)

	addTool(s, &mcp.Tool{
		Name:        "delete_product_variation",
		Description: "Delete ONE variation from a product. Deleting the last variation turns that product back into a simple product.",
	}, s.deleteProductVariation)

	// ── Sales ───────────────────────────────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_sales",
		Description: "List recent records (newest first). Without form_id this is the ENTIRE account record set: sales AND form responses for mode 'response' (sale_type 'response', submission_id + data) — the same as the Sales page, not an empty or partial list. With form_id, the form's mode decides: payment form → sales, response form → responses. Each sale: refunded_sen (already refunded), outstanding_sen (remaining refundable), refund_status (none|partial|full) — issue #518. Status is a payment question, so setting status excludes the response side.",
	}, s.listSales)

	addTool(s, &mcp.Tool{
		Name:        "get_sales_summary",
		Description: "Sales summary: counts by status (including refunded), refunded_sen (total refunded), revenue_sen (what buyers paid, net of refunds) and net_revenue_sen (revenue_sen minus the buyer-borne processing fees in fees_sen — the money you keep). Quote net_revenue_sen as income; revenue_sen includes a fee you never receive. Issue #518, card t_8064a965.",
	}, s.salesSummary)

	// The lifecycle table (card t_52429d6f). An agent can ask what a state
	// accepts BEFORE it proposes an action, instead of learning the rule from a
	// refusal. The answer comes from the same Go table the write paths apply.
	addTool(s, &mcp.Tool{
		Name:        "describe_lifecycle",
		Description: "Describe an aggregate's declarative lifecycle table: every state, the events it accepts, the state each event lands in, whether a guard can refuse it, and the dashboard label key of a person's action. With id (a sale ref_code) the answer is that sale's current state plus available_events — the events it accepts right now, each with enabled and, when a guard refuses it, the English reason; an entry with an empty label_key is a gateway callback (pay, fail), not an action a caller can invoke. Use this BEFORE proposing or attempting a state change: the same table decides what the write endpoints accept, so an enabled=false event WILL be refused.",
	}, s.describeLifecycle)

	addTool(s, &mcp.Tool{
		Name:        "confirm_sale",
		Description: "Confirm a sale (example: after checking bank transfer proof).",
	}, s.confirmSale)

	addTool(s, &mcp.Tool{
		Name:        "get_sale_email_status",
		Description: "Receipt email status for one sale: Sent → Delivered → Opened → (Clicked) with timestamps. Use ref_code from list_sales.",
	}, s.saleEmailStatusMCP)

	// ── Subscriptions ───────────────────────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_subscriptions",
		Description: "List account subscriptions: customer (email), product, amount/period, status (active/past_due/cancelled), next charge date. Filter ?status, page/limit.",
	}, s.listSubscriptions)
	addTool(s, &mcp.Tool{
		Name:        "cancel_subscription",
		Description: "Cancel a customer's subscription. The card token stays with the merchant — can be reactivated.",
	}, s.cancelSubscription)
	addTool(s, &mcp.Tool{
		Name:        "update_subscription_card",
		Description: "Update the card on an existing subscription (issue #154): create a new purchase token so the customer can confirm the new card. Returns checkout_url to send to the customer. The old token stays at CHIP (not cancelled) but is no longer used.",
	}, s.updateSubscriptionCard)
	addTool(s, &mcp.Tool{
		Name:        "retry_subscription",
		Description: "Retry a past_due subscription (round 13c): return its status to active and set it due now so the next sweep charges the card again. Use after the customer tops up the account or fixes the card. Only past_due subscriptions qualify — cancelled/completed ones are rejected. Does not charge immediately (avoids double charging).",
	}, s.retrySubscription)

	// ── Account ─────────────────────────────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_account",
		Description: "Basic account info (name, slug, CHIP mode).",
	}, s.getAccount)

	// ── Per-form CHIP override ──────────────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "set_form_chip_override",
		Description: "Set CHIP credentials specific to ONE form (brand_id + secret_key). When set, that form uses these credentials instead of the account default. The secret is stored encrypted.",
	}, s.setFormChipOverride)

	addTool(s, &mcp.Tool{
		Name:        "clear_form_chip_override",
		Description: "Clear a form's override — go back to using the account's default credentials.",
	}, s.clearFormChipOverride)

	// ── Per-form CHIP feeds (issue #208) ────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_form_chip_feeds",
		Description: "Read a form's CHIP feed list (issue #208). Each feed has brand_id + secret (hidden) + conditional logic. A feed without a condition = default (always matches).",
	}, s.getFormChipFeedsMCP)
	addTool(s, &mcp.Tool{
		Name:        "set_form_chip_feeds",
		Description: "Set a form's CHIP feed list (issue #208). The first matching feed (according to form field values) is used for payment; a feed without a condition = default. An empty secret on an existing feed = keep the old key.",
	}, s.setFormChipFeedsMCP)

	// ── Per-form CHIP field mapping (issue #186) ─────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_form_chip_mapping",
		Description: "Read a form's CHIP field map (CHIP field → form block id). Empty = use default (name→full_name, email→email, phone→phone).",
	}, s.getFormChipMapping)
	addTool(s, &mcp.Tool{
		Name:        "set_form_chip_mapping",
		Description: "Set a form's CHIP field map (CHIP field → form block id), e.g. {\"full_name\":\"block_abc\",\"email\":\"block_def\"}. Empty mapping = use default.",
	}, s.setFormChipMapping)

	// ── Capture flow ────────────────────────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "capture_sale",
		Description: "Capture skip_capture authorization (the buyer's card is charged now). amount_sen optional for partial.",
	}, s.captureSaleMCP)

	addTool(s, &mcp.Tool{
		Name:        "release_sale",
		Description: "Release (void) skip_capture authorization — the buyer's card is NOT charged.",
	}, s.releaseSaleMCP)
	addTool(s, &mcp.Tool{
		Name:        "refund_sale",
		Description: "Refund a paid sale (paid/confirmed). amount_sen = partial refund amount; empty = full refund of the remaining balance. Must be > 0 and refunded_sen + amount_sen <= total_sen (a refund exceeding the balance is rejected before CHIP, issue #518). A partial refund records the amount and the sale stays paid/confirmed (refund_status=partial); only when the full balance is refunded does the status become refunded. Response: refunded_sen, outstanding_sen, refund_status, fully_refunded.",
	}, s.refundSaleMCP)
	addTool(s, &mcp.Tool{
		Name:        "capture_all_pending_sales",
		Description: "Capture ALL skip_capture authorizations still pending. One failure does not stop the others; respond per-ref.",
	}, s.captureAllPendingMCP)

	// ── Parity: forms/products (dashboard coverage) ────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "update_product",
		Description: "Update a product: name, price (price_sen), stock, type, billing_period (subscription), active/inactive, images (image URLs must be https:// — issue #501; omit = unchanged). Omit a field to leave it unchanged.",
	}, s.updateProduct)
	addTool(s, &mcp.Tool{
		Name:        "bulk_products",
		Description: "Archive many products at once (active=false). ids[] max 500.",
	}, s.bulkProductsMCP)
	addTool(s, &mcp.Tool{
		Name:        "list_forms",
		Description: "List your account's forms: id, slug, title, status (draft|private|published|unlisted), mode (payment|response) and public URL when live. Read-only — use for form id discovery before reading or writing.",
	}, s.listForms)
	addTool(s, &mcp.Tool{
		Name:        "get_form",
		Description: "Read ONE form in full (read-only): id, slug, title, status, mode, config — INCLUDING current blocks — theme, registered_fields, public URL, created_at and updated_at. Read the form first, change only the fields you want, then resend the full config via upsert_form. Writing without reading first can destroy existing blocks.",
	}, s.getForm)
	addTool(s, &mcp.Tool{
		Name:        "upsert_form",
		Description: "Create/update a form (empty form_id = new). Read the existing one with get_form first and send the full config so existing blocks are not lost. Theme (optional) = custom style/branding. slug (optional) = the form's URL path: it must be 1-40 characters of a-z, 0-9 and hyphens and must not be a name JomForm reserves for itself (login, api, docs, privacy, …) — a malformed slug is rejected as form.slugInvalid and a reserved one as form.slugReserved. On create it is used when filled in; on update it RENAMES the form and the old URL keeps working through a temporary redirect (301) — leave it empty to keep the existing slug, and a slug already taken by another form in this workspace is rejected with an error. An option/shipping block with no price on its choices is also REJECTED — set price_sen on every choice (0 for free), because that price is what the buyer is charged.",
	}, s.upsertForm)
	// Issue #254: MCP-driven form creation — styling freedom + field
	// registration. JomForm keeps authority over field types, prices and the
	// payment flow.
	addTool(s, &mcp.Tool{
		Name:        "set_form_theme",
		Description: "Set the form's custom style/branding (issue #254): primary, background, text, card_background, card_border_radius, font_family, logo_url (must be https:// — issue #501), layout (centered|full_width|split), spacing (compact|comfortable|spacious), product_image_size (small|medium|large|hidden, issue #471), product_image_layout (thumbnail-left|image-top — menu card), custom_css, custom_js (issue #388 — custom JS for form behavior; owner-gated + scanned). Agents are free to set CSS/JS; JomForm keeps authority over pricing & payment.",
	}, s.setFormTheme)
	addTool(s, &mcp.Tool{
		Name:        "register_form_fields",
		Description: "Register the form's input fields (issue #254) with a strict contract: {field_id, type, label, required, validation?, product_id?}. type must be a JomForm input type (name|email|phone|textarea|text|number|select|radio|checkbox|date|time|website|address|consent|hidden|list|repeater|image-choice|captcha). Replaces all existing fields. Prices stay from the product DB (server authority).",
	}, s.registerFormFields)
	// Issue #256: AI form builder — MCP parity. The agent types a prompt and
	// gets a validated form config (blocks + styling) drafted by the LLM.
	addTool(s, &mcp.Tool{
		Name:        "generate_form",
		Description: "Generate a form config (blocks + style) from a natural-language prompt (issue #256) via deepseek-v4.1-flash:1017. Output is validated server-side against JomForm block types; every option/shipping choice must carry price_sen (0 for free) and that price is what is charged; product prices stay from the DB (server authority). Review/edit first, then save using upsert_form.",
	}, s.generateForm)
	addTool(s, &mcp.Tool{
		Name:        "publish_form",
		Description: "Publish the form — live at <workspace>.jomform.com/<slug> or a custom domain. status=private (with a password) makes it a password-protected form; status=draft withdraws it. A payment form with no products is rejected ONCE with a clear message (it cannot accept payment) — send acknowledge=true to proceed. Response forms and forms that are already live are never asked. An account with no workspace yet is REJECTED (workspace.required): that form has no address at all, so create a workspace first — status=draft is still allowed. Gallery templates (is_template) are EXEMPT during status=published: their address is the gallery GET /v1/templates, not a workspace.",
	}, s.publishFormMCP)
	addTool(s, &mcp.Tool{
		Name:        "unpublish_form",
		Description: "Withdraw the form from live (make it a draft).",
	}, s.deleteFormMCP)
	addTool(s, &mcp.Tool{
		Name:        "set_form_password",
		Description: "Set/remove the access password of a private form (argon2id-hashed; never returned). Empty password = remove the password.",
	}, s.setFormPassword)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "get_form_restrictions",
		Description: "Read form restrictions (issue #207): start/end schedule (start_at/end_at) + entry limit (entry_limit) + stage messages.",
	}, s.getFormRestrictions)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "set_form_restrictions",
		Description: "Set form restrictions (issue #207): start/end schedule + entry limit + message. Empty = remove restrictions.",
	}, s.setFormRestrictions)
	addTool(s, &mcp.Tool{
		Name:        "delete_form",
		Description: "Soft-delete the form (it goes to the dashboard trash; the slug is released). Can be restored — use restore_form.",
	}, s.deleteFormMCPSoft)
	// Tong sampah: padam kekal + pulihkan (Wan's decision on the delete-workspace
	// card — the bulk-delete copy promises \"can be restored later\", so the
	// restore path has to exist; and the workspace-empty guard is only
	// satisfiable if a trashed form can be destroyed for good).
	addTool(s, &mcp.Tool{
		Name:        "list_deleted_forms",
		Description: "List forms in the TRASH (deleted_at IS NOT NULL): id, slug, title, deleted_at, sales_count and can_purge. Forms with sales cannot be permanently deleted (can_purge=false).",
	}, s.listDeletedForms)
	addTool(s, &mcp.Tool{
		Name:        "restore_form",
		Description: "Restore a form from the trash (deleted_at=NULL). Nothing is destroyed — the form comes back with its original slug. If the slug has already been taken by another form, the restore is REJECTED (no automatic renaming) — change that form's slug first.",
	}, s.restoreFormMCP)
	addTool(s, &mcp.Tool{
		Name:        "purge_form",
		Description: "PERMANENTLY delete a form from the trash (submissions/notifications/confirmations/the form's email settings/webhooks drop with it). WARNING: cannot be undone. Required confirm_slug = that form's slug. Forms that HAVE SALES are rejected (sales are never destroyed or left orphaned — sales.form_id is ON DELETE SET NULL, so a permanent delete would silently orphan the sale). Trash-only forms — delete first using delete_form.",
	}, s.purgeFormMCP)
	addTool(s, &mcp.Tool{
		Name:        "bulk_forms",
		Description: "Bulk action over many forms at once: action=publish|draft|delete with ids[] (max 500). Payment forms without products are EXCLUDED from publish and returned in needs_products — send acknowledge=true to publish them too. Templates without a category or a description block (Section/text) are SKIPPED from publish and returned in skipped + skipped_reason — updated counts only rows that actually changed, and acknowledge=true does not override it. action=publish is REJECTED entirely (workspace.required) if the account has no workspace — that form has no address; action=draft|delete is still allowed. A batch containing ONLY gallery templates is exempt from that condition (its address is the gallery).",
	}, s.bulkFormsMCP)
	// ── Parity: settings/webhooks ──────────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "set_einvoice",
		Description: "Set the LHDN TIN + MSIC for e-Invoice receipts (B2B).",
	}, s.setEinvoice)
	addTool(s, &mcp.Tool{
		Name:        "get_einvoice_settings",
		Description: "Read the current LHDN TIN + MSIC for e-Invoice receipts.",
	}, s.getEinvoiceSettings)
	addTool(s, &mcp.Tool{
		Name:        "create_webhook",
		Description: "Register a webhook endpoint (events: sale.paid|sale.failed|subscription.charged|*). HMAC-signed. form_id optional: set = webhook for that form only; empty = whole workspace/account.",
	}, s.createWebhookMCP)
	addTool(s, &mcp.Tool{
		Name:        "list_webhooks",
		Description: "List the account's outbound webhooks. form_id optional: set = that form's webhooks only; empty = whole workspace/account webhooks.",
	}, s.listWebhooksMCP)
	addTool(s, &mcp.Tool{
		Name:        "delete_webhook",
		Description: "Delete a single webhook (id from list_webhooks).",
	}, s.deleteWebhookMCP)
	addTool(s, &mcp.Tool{
		Name:        "update_webhook",
		Description: "Update a webhook (url, events, secret optional — empty = keep existing). events: sale.paid|sale.failed|subscription.charged|*.",
	}, s.updateWebhookMCP)
	addTool(s, &mcp.Tool{
		Name:        "webhook_deliveries",
		Description: "Recent webhook delivery log (status_code, attempt, error).",
	}, s.webhookDeliveriesMCP)
	// Issue #197: manual retry of a failed webhook delivery (MCP parity).
	addTool(s, &mcp.Tool{
		Name:        "retry_webhook_delivery",
		Description: "Retry a failed webhook delivery immediately (id from webhook_deliveries). It is re-enqueued and re-sent on the next sweep.",
	}, s.retryWebhookDeliveryMCP)
	// ── Parity: API-key self-service ────────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_api_keys",
		Description: "List all of the account's API keys (id, name, scopes, created_at, last_used). Key material is never returned.",
	}, s.listAPIKeys)
	addTool(s, &mcp.Tool{
		Name:        "create_api_key",
		Description: "Create a new API key (name, scopes? readonly|readwrite, default readonly). The full key is shown ONCE only — save it immediately; the store keeps only the hash. SCOPES: read for readonly, read+write for readwrite.",
	}, s.createAPIKey)
	addTool(s, &mcp.Tool{
		Name:        "revoke_api_key",
		Description: "PERMANENTLY delete an API key (id from list_api_keys; there is no disable/re-enable — create a new key). WARNING: if the id is the key authenticating this session, it will cut off its own access immediately — set confirm=true to confirm; otherwise the call is rejected.",
	}, s.revokeAPIKey)

	// ── Connected apps (OAuth credentials an agent holds) ─────────────────────
	// Dashboard parity for Profile → MCP → Connected apps (card t_9a6cc85f). The
	// scope ladder is the same one the dashboard enforces: these tools can lower
	// a grant and can never raise it.
	addTool(s, &mcp.Tool{
		Name:        "list_connected_apps",
		Description: "List the live OAuth connections (AI agents) the signed-in person has authorised, one row per credential: id, client name, scopes, created and last used. Needs an OAuth connection, not an API key.",
	}, s.listConnectedApps)
	addTool(s, &mcp.Tool{
		Name:        "revoke_connected_app",
		Description: "Revoke ONE connected app: its access token and its refresh token die together. Takes the id from list_connected_apps. Revoking the connection this call is using ends the session.",
	}, s.revokeConnectedApp)
	addTool(s, &mcp.Tool{
		Name:        "set_connected_app_scope",
		Description: "Lower a connected app to read-only by rotating its credential (the old token pair stops working). Widening to readwrite is REFUSED here — it needs a human consent on the Connected apps page or a new device authorisation.",
	}, s.setConnectedAppScope)

	// ── Workspaces (multi-tenant storefronts) ───────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_workspaces",
		Description: "List all your workspaces (storefronts) — id, name, slug and URL.",
	}, s.listWorkspaces)
	// ── Affiliates (issue #444) ────────────────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "create_affiliate",
		Description: "Create an affiliate for a workspace: referral code (?aff=CODE), name, email, commission rate. Sales brought in by the affiliate are credited to it.",
	}, s.createAffiliate)
	addTool(s, &mcp.Tool{
		Name:        "list_affiliates",
		Description: "List all affiliates in a workspace — id, code, name, commission, referral URL.",
	}, s.listAffiliates)
	addTool(s, &mcp.Tool{
		Name:        "get_affiliate_sales",
		Description: "List sales brought in by a single affiliate (read-only, workspace scope).",
	}, s.getAffiliateSales)
	addTool(s, &mcp.Tool{
		Name:        "set_affiliate_commission",
		Description: "Set the affiliate commission rate (e.g. 10 = 10%).",
	}, s.setAffiliateCommission)
	addTool(s, &mcp.Tool{
		Name:        "create_workspace",
		Description: "Create a new workspace. Name up to 120 characters (counted as CHARACTERS, not bytes). Slug 3-40 characters (a-z, 0-9, dash); the URL becomes https://<slug>.jomform.com. Names reserved for JomForm (e.g.: www, api, app, mail, s3, docs, mcp, templates, custom, support, help, status, billing, admin, account, login, payments, checkout, cdn, assets) are rejected — those names belong to JomForm or protect users from impersonation (e.g.: billing.jomform.com impersonating JomForm's official channel).",
	}, s.createWorkspace)
	addTool(s, &mcp.Tool{
		Name:        "update_workspace",
		Description: "Change the NAME and/or SLUG (public address) of a workspace. Empty field = keep existing. Name up to 120 CHARACTERS (not bytes; same limit as create). Changing the name requires the manage/admin role; changing the SLUG is OWNER-only, and requires confirm=true — the slug is the tenant's public host (https://<slug>.jomform.com) already printed in QR codes, iframes and affiliate links. The OLD slug is kept as an alias and KEEPS WORKING (permanent move to the new slug), so links already spread around do not die. Returns previous_url + previous_url_redirects. Names reserved for JomForm (same list as create_workspace: www, api, billing, support, docs, mcp, templates, …) are REJECTED as status=refused with code reserved_subdomain — that name belongs to JomForm or protects users from impersonation, and the workspace slug does not change. REST: PATCH /v1/workspaces/{id}.",
	}, s.updateWorkspace)
	addTool(s, &mcp.Tool{
		Name:        "get_workspace_settings",
		Description: "Read the storefront settings of one workspace: footer, company_reg, privacy/terms/refund URL, contact, menu, indexing, sound_enabled and spam_judge_enabled (true = on/default).",
	}, s.getWorkspaceSettings)
	addTool(s, &mcp.Tool{
		Name:        "set_workspace_settings",
		Description: "Set workspace storefront settings: footer_text, company_reg, privacy_url/terms_url/refund_url (must be https://), contact_phone, contact_address, business_name, menu_json, default_lang (default language of the PUBLIC form for this workspace — en, ms, zh, ta or ar; this is the buyer's language, not your dashboard language), indexing, sound_enabled and spam_judge_enabled (false = TURN OFF AI spam detection for this workspace — no AI job is submitted at all and is_spam is no longer set, so /spam stays empty). Omit a field = keep existing.",
	}, s.setWorkspaceSettings)
	// Issue #360: customizable landing page (MCP parity).
	addTool(s, &mcp.Tool{
		Name:        "get_workspace_landing_page",
		Description: "Read the workspace's 'landing page' configuration (issue #360): hero, subtitle, body, image_url, theme colors, logo, CTA (label + form_slug). Empty if no landing page is set.",
	}, s.getWorkspaceLandingPage)
	addTool(s, &mcp.Tool{
		Name:        "set_workspace_landing_page",
		Description: "Set the workspace storefront 'landing page' (issue #360): enabled (true = show, false/empty = fall back to the form list), hero, subtitle, body, image_url (must be https:// — issue #501), primary_color, background_color, logo_url (must be https:// — issue #501), ctas ([{label, form_slug}]). Send enabled:false to return to the form list.",
	}, s.setWorkspaceLandingPage)
	// Issue #438: QR ordering flow (MCP parity).
	addTool(s, &mcp.Tool{
		Name:        "generate_qr",
		Description: "Generate a QR code for a published form (issue #438): form_slug (e.g. 'menu') + optional table (table number, encoded as ?table=N). Returns qr_url (endpoint /qr/{slug}.png), data_url (PNG base64) and target_url (the encoded URL). Use for QR stands on restaurant tables.",
	}, s.generateQR)
	// Issue #447: realtime notifications (MCP parity). The realtime channel is
	// the authenticated SSE stream GET /v1/rest/events/stream; these tools
	// document it and expose the per-workspace sound toggle.
	addTool(s, &mcp.Tool{
		Name:        "get_realtime_status",
		Description: "Read the workspace's real-time notification status (issue #447): stream_url (SSE /v1/rest/events/stream — real-time channel for sales/responses), list of events (sale.paid, sale.failed, response.submitted, subscription.charged, sale.refunded), and sound_enabled (beep sound on the dashboard). Read-only. Connect to the stream for instant notifications on a kitchen/order screen.",
	}, s.getRealtimeStatus)
	addTool(s, &mcp.Tool{
		Name:        "set_realtime_sound",
		Description: "Turn the workspace's real-time notification beep on/off (issue #447). enabled=true = the dashboard beeps when a new sale/response arrives (e.g. kitchen screen); false = silent. Write tool (manage/admin role).",
	}, s.setRealtimeSound)
	addTool(s, &mcp.Tool{
		Name:        "get_menu_page",
		Description: "Read the restaurant menu configuration of one FORM (issue #497): form_id. Returns enabled, categories ([{name, product_ids}]), best_seller, best_seller_ids (list, #485), order_form_slug (form for additional order information, #491/#493). The menu is a PER-FORM setting and is displayed at that form's own URL. Empty if the form has no menu.",
	}, s.getMenuPage)
	addTool(s, &mcp.Tool{
		Name:        "set_menu_page",
		Description: "Set the restaurant menu for one FORM (issue #497): form_id, enabled (true = this form becomes a menu at its URL; false = the form returns to normal), categories ([{name, product_ids}]), best_seller (product ID) or best_seller_ids (list of IDs for the Best Seller section, #485 — overrides best_seller), order_form_slug (optional, #491/#493 — a published form in the same account whose block fields become additional fields during menu checkout; fields are rendered with that form's label/required, validated server-side, and stored on the sale; empty = name + email checkout only, both still required for CHIP). Menu products come from that FORM's own product blocks (not all account products), so products from other accounts cannot leak in; categories/best-seller must reference products referenced by the form. The menu page shows the table number from ?table=N and has a category filter + search. A form without product blocks displays an empty state.",
	}, s.setMenuPage)
	// Issue #185: per-workspace currency + BNM rates (MCP parity).
	addTool(s, &mcp.Tool{
		Name:        "get_currency_rates",
		Description: "BNM currency exchange rates (Ringgit per unit of foreign currency), cached daily. Reference data — JomForm does not convert stored amounts; use convert_currency to convert a single amount.",
	}, s.getCurrencyRates)
	// Card t_467614db: the rate store's conversion had no production caller at
	// all, while three surfaces claimed conversion worked. This tool is that
	// caller; it converts one amount on demand and writes nothing.
	addTool(s, &mcp.Tool{
		Name:        "convert_currency",
		Description: "Convert a single amount between currencies using the cached (daily) BNM rates. amount_minor must be an integer in the smallest unit (cents) of the source currency: 10000 = 100.00. Returns converted_minor in the smallest unit of the target currency (e.g. 260 = RM 2.60). For display/reporting only — not for charging: sales settle in the workspace currency and past sales are never converted.",
	}, s.convertCurrency)
	addTool(s, &mcp.Tool{
		Name:        "set_workspace_currency",
		Description: "Set the workspace currency (default MYR). The change applies to new products/sales; past sales keep their original currency (not converted back).",
	}, s.setWorkspaceCurrency)
	// Issue #161: workspace membership + invites (admin only).
	addTool(s, &mcp.Tool{
		Name:        "invite_workspace_member",
		Description: "Invite a member to the workspace (admin only). If the email is already registered, add them directly as a member; if not, create a pending invitation. Role: read_only|manage|admin.",
	}, s.inviteWorkspaceMember)
	addTool(s, &mcp.Tool{
		Name:        "list_workspace_members",
		Description: "List workspace members + roles (admin only).",
	}, s.listWorkspaceMembers)
	addTool(s, &mcp.Tool{
		Name:        "update_workspace_member_role",
		Description: "Change a workspace member's role (admin only). Role: read_only|manage|admin.",
	}, s.updateWorkspaceMemberRole)
	addTool(s, &mcp.Tool{
		Name:        "remove_workspace_member",
		Description: "Remove a member from the workspace (admin only). The owner cannot be removed.",
	}, s.removeWorkspaceMember)
	addTool(s, &mcp.Tool{
		Name:        "list_workspace_invites",
		Description: "List pending invitations for the workspace (admin only).",
	}, s.listWorkspaceInvites)
	addTool(s, &mcp.Tool{
		Name:        "cancel_workspace_invite",
		Description: "Cancel a pending invitation for the workspace (admin only).",
	}, s.cancelWorkspaceInvite)
	// Issue #164: default workspace (multi-workspace login picker) parity.
	addTool(s, &mcp.Tool{
		Name:        "set_default_workspace",
		Description: "Set your default workspace (the workspace you enter directly after logging in, without a prompt). Empty workspace_id = remove the default. The workspace must belong to you.",
	}, s.setDefaultWorkspace)
	addTool(s, &mcp.Tool{
		Name:        "delete_workspace",
		Description: "Send a workspace to the TRASH (owner only — admin members cannot; they leave via remove_workspace_member). Nothing is destroyed: forms, products, sales, CHIP accounts and API keys all remain, and restore_workspace brings it back. TWO conditions: (1) confirm_slug must match the workspace slug; (2) you must have at least one OTHER workspace — the last workspace cannot be removed, because a user must have one workspace. A FULL workspace can go to the trash (the 'must be empty' guard has been removed); PERMANENT delete is only via purge_workspace, and chat can never suggest that tool — it is an MCP tool for the owner, not for chat. Use check_workspace_deletion first to see can_delete.",
	}, s.deleteWorkspaceMCP)
	addTool(s, &mcp.Tool{
		Name:        "check_workspace_deletion",
		Description: "Check whether a workspace can be sent to the trash (read-only, no effect). Returns can_delete, empty, last_workspace, is_owner and blockers[] (each category + count). can_delete = owner AND not the last workspace — content NO LONGER blocks (blockers[] is reported for reference before purge). Use this first so you can state the reason, not fail after clicking.",
	}, s.checkWorkspaceDeletionMCP)
	addTool(s, &mcp.Tool{
		Name:        "list_trashed_workspaces",
		Description: "List the workspaces in your TRASH (deleted_at IS NOT NULL): id, slug, name, deleted_at. Own only — only the owner can restore or permanently delete.",
	}, s.listTrashedWorkspacesMCP)
	addTool(s, &mcp.Tool{
		Name:        "restore_workspace",
		Description: "Restore a workspace from the trash (deleted_at=NULL). Nothing is destroyed — the workspace comes back with its accounts, forms and sales. If its original slug was taken by another workspace while it was in the trash, the restore is REJECTED (no automatic rename). Owner only.",
	}, s.restoreWorkspaceMCP)
	addTool(s, &mcp.Tool{
		Name:        "purge_workspace",
		Description: "PERMANENTLY delete a workspace from the trash (its forms, products, API keys and sales fall with it; the CHIP account CREATED together with that workspace also falls, but an account that ALREADY EXISTED before the workspace — the signup account used when the first workspace was created — is NOT destroyed: it is released back as an account without a workspace, and the accounts_unlinked count reports it). WARNING: cannot be undone. Requires confirm_slug = that workspace's slug. Only a workspace IN THE TRASH — send it to the trash first with delete_workspace. A workspace WITH SALES is rejected (sales are never destroyed or orphaned). NEVER SUGGESTED BY CHAT: in-app chat must not suggest this tool; it is an MCP tool for owners, the same class as purge_form/purge_product/purge_media.",
	}, s.purgeWorkspaceMCP)
	// Issue #181: optional profile phone number parity.
	addTool(s, &mcp.Tool{
		Name:        "update_profile_phone",
		Description: "Set an optional phone number on your profile (empty = remove). Loose format: digits, +, spaces, 8-15 digits.",
	}, s.updateProfilePhone)
	// Issue #191: optional profile timezone parity.
	addTool(s, &mcp.Tool{
		Name:        "update_profile_timezone",
		Description: "Set an optional IANA timezone on your profile (empty = Auto, follow the browser/PC). Examples: Asia/Kuala_Lumpur, UTC.",
	}, s.updateProfileTimezone)
	// Language placement: the dashboard UI language is per-PROFILE.
	addTool(s, &mcp.Tool{
		Name:        "update_profile_language",
		Description: "Set an optional DASHBOARD language on your profile (per-user, follows you to any browser): en, ms, zh, ta or ar (empty = auto, follow that browser's last value, then en). This is the DASHBOARD language only and it is per-profile — public form language is set per-form (config.lang) or per-workspace (default_lang via set_workspace_settings), not here.",
	}, s.updateProfileLanguage)

	// ── Parity: shipping zones + rates (managed rate source, phase 1) ─────────
	addTool(s, &mcp.Tool{
		Name:        "get_shipping_rates",
		Description: "Read the shipping zone set + rates for one account/workspace: every zone (state/district/postcode range, priority, default zone) and its rates (price in SEN, weight range in grams, free-shipping threshold).",
	}, s.getShippingRates)
	addTool(s, &mcp.Tool{
		Name:        "set_shipping_rates",
		Description: "Replace ALL shipping zone sets + rates for one account/workspace. Sets absent from sets are DELETED (rejected if any form still references them). A rate must have a price in SEN; min/max_weight_g in grams (0 = no limit); free_over_sen = free-shipping threshold.",
	}, s.setShippingRates)

	// ── Parity: buyer-borne processing fee (card t_135c3c72) ─────────────────
	// The dashboard sets one rate per CHIP payment method in Settings -> Payment
	// fees; these two tools are the same surface over MCP, and the write carries
	// the same gate: a method may only be ENABLED when the merchant acknowledges
	// the pass-on disclosure.
	addTool(s, &mcp.Tool{
		Name:        "get_payment_fees",
		Description: "Read the processing fee rates per CHIP payment method (fpx, fpx_b2b1, card, ewallet, duitnow_qr). fpx is FPX B2C and fpx_b2b1 is FPX B2B1: two products with their own rate. Each row gives active/not, fixed fee in SEN, percentage fee in BASIS POINTS (100 = 1%), and whether the account owner has given the disclosure acknowledgement. Without that acknowledgement the fee is not charged to the payer even if the rate is stored.",
	}, s.getPaymentFees)
	addTool(s, &mcp.Tool{
		Name:        "set_payment_fees",
		Description: "Replace ALL processing fee rates for one account/workspace. Methods absent from rates are DELETED. Enabling a fee (fee_enabled + rate > 0) REQUIRES acknowledged=true — the account owner's acknowledgement that the fee is passed to the payer; the tool records who and when. The fee is calculated on the buyer's FINAL amount (gross-up), so the merchant does not bear a fee on the fee.",
	}, s.setPaymentFees)

	// ── Parity: courier providers (phase 2) ───────────────────────────────────
	// The dashboard connects a courier in Form Settings -> Shipping; these three
	// tools are the same surface over MCP. They share the connect rules and the
	// readiness test with the dashboard (internal/shipping/connect.go), so an
	// agent cannot connect something the dashboard would refuse.
	addTool(s, &mcp.Tool{
		Name:        "get_shipping_couriers",
		Description: "List shipping providers (lalamove, easyparcel) for one account/workspace: whether server credentials exist, whether the workspace is already connected, the environment (sandbox/live), and WHAT IS STILL MISSING (credentials/connect/grant/origin) before a real quote can be made. No credential values are returned.",
	}, s.getShippingCouriers)
	addTool(s, &mcp.Tool{
		Name:        "connect_shipping_courier",
		Description: "Connect one shipping provider for one account/workspace. Supply the PICKUP ADDRESS (postcode+state for easyparcel; latitude+longitude for lalamove). For easyparcel this tool returns authorize_url — that link is for the ACCOUNT OWNER to open themselves (any device) and grant permission; do not open that link, do not type or ask for a password. Links expire: call again for a new link.",
	}, s.connectShippingCourier)
	addTool(s, &mcp.Tool{
		Name:        "disconnect_shipping_courier",
		Description: "Disconnect one shipping provider for one account/workspace and remove the stored grant. The pickup address is KEPT so reconnecting is easy.",
	}, s.disconnectShippingCourier)

	// ── Parity: per-workspace email templates (issue #107) ────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_email_settings",
		Description: "Read a workspace's email settings: receipt subject/body (placeholders), send receipt, merchant notification, HTML.",
	}, s.getEmailSettings)
	addTool(s, &mcp.Tool{
		Name:        "set_email_settings",
		Description: "Set workspace email settings: receipt_subject, receipt_body ({{ref_code}}, {{amount}}, {{items}}, {{form_url}}, {{buyer_name}}, {{date}}), receipt_enabled, notify_merchant, merchant_notify_email, html. Omitted field = keep existing.",
	}, s.setEmailSettings)

	// ── Parity: per-form notification email (issue #177, overrides #107/#453) ──
	addTool(s, &mcp.Tool{
		Name:        "get_form_email_settings",
		Description: "Read the email settings of ONE form (issue #453): per-form overrides (receipt subject/body, send receipt, merchant notification, HTML), the inherited workspace settings, and the effective values (per-form -> workspace -> default resolution). Empty/null = follow the workspace settings.",
	}, s.getFormEmailSettings)
	addTool(s, &mcp.Tool{
		Name:        "set_form_email_settings",
		Description: "Set email overrides for ONE form (issue #453): receipt_subject, receipt_body, receipt_enabled_override, notify_merchant_override, merchant_notify_email, html_override. Toggles are tri-state — true/false = explicit override, null = follow the workspace settings. Empty subject/body = follow the workspace. Omitted field = keep existing.",
	}, s.setFormEmailSettings)
	addTool(s, &mcp.Tool{
		Name:        "clear_form_email_overrides",
		Description: "Delete ALL per-form email overrides so this form returns to inheriting the workspace email settings (issue #453).",
	}, s.clearFormEmailOverrides)

	// ── Parity: per-form notification rules (issue #193) ────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_form_notifications",
		Description: "Read the list of per-form email notification rules (issue #193): event, recipient, subject/body, conditional logic, active. Every new form ships with one default notification to the merchant: event sale.paid for payment forms, response.submitted for response forms (issue #597).",
	}, s.getFormNotifications)
	addTool(s, &mcp.Tool{
		Name:        "set_form_notifications",
		Description: "Replace ALL per-form email notification rules (issue #193). notifications[] in full: {id?, name, event, to_email, subject, body, condition?, enabled?}. event: sale.paid|sale.failed|response.submitted|subscription.charged. to_email: an email, {admin_email}, or {field_id}. condition: {field_id, op, value}|null (op: equals|not_equals|contains|gt|lt|not_empty). Empty list = no email is sent.",
	}, s.setFormNotifications)
	// ── Parity: per-form confirmations (issue #179) ─────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_form_confirmations",
		Description: "Read form confirmations (issue #179): the list of confirmations (message/redirect) + conditional logic. A default confirmation always exists.",
	}, s.getFormConfirmations)
	addTool(s, &mcp.Tool{
		Name:        "set_form_confirmations",
		Description: "Set form confirmations (issue #179): replace all confirmations (name, type message|redirect, message/redirect_url, condition). The default confirmation is always kept. HTML messages are sanitised (no XSS).",
	}, s.setFormConfirmations)
	// ── Parity: per-form payment-failed message (issue #393) ────────────
	addTool(s, &mcp.Tool{
		Name:        "get_form_failure_message",
		Description: "Read the form's payment-failed message (issue #393): what the buyer sees when payment fails. Empty = use the default message.",
	}, s.getFormFailureMessage)
	addTool(s, &mcp.Tool{
		Name:        "set_form_failure_message",
		Description: "Set the form's payment-failed message (issue #393): the HTML the buyer sees when payment fails (sanitised, no XSS). Empty = use the default message.",
	}, s.setFormFailureMessage)
	// ── Parity: per-form Open Graph image (issue #411) ──────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_form_og_image",
		Description: "Read the form's Open Graph image (og:image) (issue #411): the custom image URL when set, empty = use the generated default /og/{slug}.png.",
	}, s.getFormOGImage)
	addTool(s, &mcp.Tool{
		Name:        "set_form_og_image",
		Description: "Set the form's Open Graph image (og:image) (issue #411): custom image URL (must be https://) for the social preview when the form is shared. Empty = use the generated default.",
	}, s.setFormOGImage)

	// ── Parity: per-workspace hosted legal pages (issue #184) ────────────────
	addTool(s, &mcp.Tool{
		Name:        "generate_legal_page",
		Description: "Generate a draft legal page (privacy|terms|refund) for a workspace via the LLM (deepseek-v4.1-flash:1017) with a template filled with the business details. Returns the draft for review — not saved until set_legal_page is called.",
	}, s.generateLegalPage)
	addTool(s, &mcp.Tool{
		Name:        "get_legal_page",
		Description: "Read one workspace legal page (privacy|terms|refund): mode (auto/manual) + content.",
	}, s.getLegalPage)
	addTool(s, &mcp.Tool{
		Name:        "set_legal_page",
		Description: "Save the workspace's legal page content (privacy|terms|refund) with mode auto|manual. The content is served at {workspace}.jomform.com/privacy|terms|refund.",
	}, s.setLegalPage)

	// ── Parity: event tracking (issue #92) ────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_events",
		Description: "Account audit/behaviour log (newest first). Can filter by event_name, from/to (RFC3339), and page/limit. Read-only. On the dashboard the same log appears on the Activity page — and is workspace-scoped there via ?workspace=<slug>.",
	}, s.listEvents)

	// ── Parity: email delivery status (issue #194) ────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_email_deliveries",
		Description: "Account email delivery status (newest first): delivered/bounce/complaint/open/click, plus enqueue_failed (the email could not be queued at all, so it was never sent) — with email, time, sale_ref and detail. Can filter form_id, sale_ref, event_type, and page/limit. Read-only.",
	}, s.listEmailDeliveries)

	// ── Parity: response-only form submissions (issue #171) ───────────────
	addTool(s, &mcp.Tool{
		Name:        "list_form_submissions",
		Description: "List responses (submissions) for a response-only form (mode 'response', no payment). Returns field data + time. Filter form_id, page/limit.",
	}, s.listFormSubmissions)

	// ── Parity: AI spam review queue (issue #245) ─────────────────────────
	// The merchant's agent can list spam-flagged submissions/sales and
	// un-spam a false positive. A spam-flagged sale that receives a successful
	// payment is auto-un-spammed by the payment callback (no agent action).
	addTool(s, &mcp.Tool{
		Name:        "list_spam_submissions",
		Description: "List submissions flagged as spam by AI (issue #245): field data, spam reason, time. Can filter page/limit. Read-only.",
	}, s.listSpamSubmissions)
	addTool(s, &mcp.Tool{
		Name:        "unspam_submission",
		Description: "Un-flag spam on a submission (false positive). Write tool (manage/admin role).",
	}, s.unspamSubmission)
	addTool(s, &mcp.Tool{
		Name:        "list_spam_sales",
		Description: "List sales flagged as spam by AI (issue #245): ref, status, amount, buyer, spam reason, time. Can filter page/limit. Read-only.",
	}, s.listSpamSales)
	addTool(s, &mcp.Tool{
		Name:        "unspam_sale",
		Description: "Un-flag spam on a sale (false positive). Write tool (manage/admin role).",
	}, s.unspamSale)

	// ── Parity: media library (issue #180) ───────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_media",
		Description: "List media uploaded by the account (media library): url, name, date, size, and thumbnail/medium/full for images whose sizes are generated. Can filter form_id and source (merchant|customer). Read-only.",
	}, s.listMedia)
	// Issue #363: media upload parity — the agent pushes image + code assets
	// + PDFs over MCP, mirroring the dashboard upload. Images serve
	// immediately (png/jpeg/webp get thumbnail/medium/full sizes); code assets
	// (css/js/html/svg/woff2) wait for the #364 scan gate; PDFs serve
	// immediately.
	addTool(s, &mcp.Tool{
		Name:        "upload_media",
		Description: "Upload media/assets to your workspace (issue #363): kind='image' (png/jpeg/webp/gif — optimised, EXIF stripped; opaque images are stored as JPEG (URL ends .jpg), images with a transparent background are stored as PNG (URL ends .png) so the transparency is preserved; an animated GIF becomes a still image (first frame only); thumbnail/medium/full are generated for every image), kind='asset' (css/js/html/svg/woff2 — stored as-is, sniffed before being served; svg MUST be valid XML — text before the root element, for example banner /* */, is rejected because the browser will show a parsererror) or kind='pdf' (stored as-is, immediately usable). Send the file content as base64 in 'data'. Returns a URL to use as a landing image_url/logo_url or a landing page <link>/<script>. 'size' in the response is the TOTAL STORED (base object + image variants / PDF preview), the SAME number list_media returns for that id — not the number of bytes you sent.",
	}, s.uploadMedia)

	// Tong sampah media: padam = hantar ke bin (objek TIDAK dimusnahkan), pulih
	// selagi byte masih ada, dan buang kekal = satunya tempat byte + kuota
	// dilepaskan.
	addTool(s, &mcp.Tool{
		Name:        "delete_media",
		Description: "Send media to the TRASH (deleted_at set). Bytes are NOT destroyed and quota is NOT released — the object is still in storage, so it still counts against the 1GB limit. The same URL keeps working after restore_media. Use purge_media to destroy the bytes permanently.",
	}, s.deleteMediaMCP)
	addTool(s, &mcp.Tool{
		Name:        "list_deleted_media",
		Description: "List media in the TRASH: id, key, url, mime, size, form_id, deleted_at and object_present. object_present=false means the bytes are already gone from storage (deleted by an operator or a lifecycle rule) — restore will be REJECTED.",
	}, s.listDeletedMedia)
	addTool(s, &mcp.Tool{
		Name:        "restore_media",
		Description: "Restore media from the trash (deleted_at=NULL). The SAME URL keeps working. REJECTED (409) when the object is already gone from storage — restoring the row would only hand back a URL that 404s.",
	}, s.restoreMediaMCP)
	addTool(s, &mcp.Tool{
		Name:        "purge_media",
		Description: "Permanently delete media from the trash. WARNING: cannot be undone. This is the only place bytes are deleted from storage AND the 1GB quota is released (size variants + PDF previews are deleted too), and the place where EDGE CDN copies are purged in the same call so those URLs stop being downloadable. The response carries 'edge' (purged | not_configured | no_urls | out_of_zone | failed) and 'edge_urls' only when the CDN accepted at least one purge request; on not_configured, no_urls and out_of_zone that field is absent entirely, and on failed it lists only the accepted ones; when there are no CDN credentials it mentions 'edge_max_age_s' — the maximum period the edge can still serve its copy. Only media in the trash — send it to the bin first with delete_media.",
	}, s.purgeMediaMCP)

	// ── Parity: form template gallery (issue #174) ────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_templates",
		Description: "List system form templates (gallery): slug, title, category, mode (payment/response). Use with use_template for quickstart.",
	}, s.listTemplatesMCP)
	addTool(s, &mcp.Tool{
		Name:        "use_template",
		Description: "Clone a form template into a new draft in your account. Fill slug (from list_templates) for a system template, or template_id (from list_own_templates) for your own template. Returns form_id + slug to open in the builder.",
	}, s.useTemplateMCP)
	// ── Parity: merchant-managed templates (issue #457) ───────────────────
	// Dashboard Templates view: save a form as a reusable template, then
	// list/edit/delete/clone your own templates. Account-scoped.
	addTool(s, &mcp.Tool{
		Name:        "list_own_templates",
		Description: "List your own templates (issue #457): id, slug, title, config, theme. Different from list_templates (system gallery).",
	}, s.listOwnTemplatesMCP)
	addTool(s, &mcp.Tool{
		Name:        "create_template",
		Description: "Save an existing form as a reusable template (issue #457): fill form_id (from list_forms) to copy that form, or title + config for a new template. Templates are owned by your account only.",
	}, s.createTemplateMCP)
	addTool(s, &mcp.Tool{
		Name:        "update_template",
		Description: "Update your own template (issue #457): title, config and/or theme. Omit fields that do not change. template_id from list_own_templates.",
	}, s.updateTemplateMCP)
	addTool(s, &mcp.Tool{
		Name:        "delete_template",
		Description: "Delete (soft-delete) your own template (issue #457). template_id from list_own_templates. Can be created again at any time.",
	}, s.deleteTemplateMCP)

	// ── Parity: unverified-account purge (issue #190) ───────────────────
	// The auto-deletion runs on the hourly sweep; this admin tool surfaces
	// the backlog count (read-only) so agents can observe the purge.
	addTool(s, &mcp.Tool{
		Name:        "purge_unverified_accounts",
		Description: "Report the number of unverified accounts (email) older than the threshold, BROKEN DOWN into how many the sweep will delete and how many are BLOCKED (the account still holds sales or subscriptions, so it will not be deleted). Admin only; read-only — deletes nothing.",
	}, s.purgeUnverifiedAccounts)

	// ── Parity: QA test-account purge (card t_6ae7f586) ─────────────────
	// The marker is written at signup through the QA channel, so a card that
	// fails mid-verification still leaves an account this tool can find.
	addTool(s, &mcp.Tool{
		Name:        "purge_test_accounts",
		Description: "Delete TEST accounts flagged is_test created through the QA signup channel. Super-admin only; audited. The DEFAULT is a DRY RUN (confirm=false): report exactly what WILL be deleted and delete nothing; set confirm=true to actually delete. It REJECTS accounts not flagged as test (including those whose email looks like a test account), and BLOCKS — does not delete — test accounts that still hold sales or subscriptions. Protected accounts are never touched. The response NAMES every account (id + email + counts): removed, removed_emails, removed_count, blocked (reason: sales|subscriptions|fk_restricted), blocked_count, scanned. Idempotent — a second run deletes nothing and reports that. There is no age limit and no creator filter: an account can be deleted as long as it IS FLAGGED.",
	}, s.purgeTestAccounts)

	// ── Super-admin: platform-wide monitoring (issue #386) ─────────────
	// These tools are gated on the super-admin role (#385) and are read-only
	// by default. They let the platform owner's AI agent monitor usage across
	// ALL workspaces once JomForm is public. Destructive admin writes are separate
	// and explicit (none exist yet). Every call is audited.
	addTool(s, &mcp.Tool{
		Name:        "admin_list_workspaces",
		Description: "List ALL workspaces (storefronts) on the platform: id, name, slug, owner email, created date, number of forms + products. Super-admin only; read-only; audited.",
	}, s.adminListWorkspaces)
	addTool(s, &mcp.Tool{
		Name:        "admin_media_usage",
		Description: "Media storage usage per workspace (bytes) against the 1GB quota per workspace. Super-admin only; read-only; audited.",
	}, s.adminMediaUsage)
	addTool(s, &mcp.Tool{
		Name:        "admin_list_sales",
		Description: "List sales across ALL workspaces, filterable (date_from/date_to, workspace_id, status). Includes refunded_sen/outstanding_sen/refund_status (issue #518). Super-admin only; read-only; audited.",
	}, s.adminListSales)
	addTool(s, &mcp.Tool{
		Name:        "admin_spam_queue",
		Description: "List all spam items (submissions + sales) across all workspaces. Super-admin only; read-only; audited.",
	}, s.adminSpamQueue)
	addTool(s, &mcp.Tool{
		Name:        "admin_email_deliveries",
		Description: "Email delivery status across ALL workspaces (delivered/bounce/complaint/open/click), filterable by sale_ref/event_type/form_id. Super-admin only; read-only; audited.",
	}, s.adminEmailDeliveries)
	addTool(s, &mcp.Tool{
		Name:        "admin_dead_letter_events",
		Description: "List inbound callback events parked as dead letters (issue #574): dedup_key, event_type, attempt count, reason, time. After #569 unprocessable events are parked as 'dead' and STOP being retried — before this they were not visible at all (no MCP/REST reads webhook_events). Super-admin only; read-only; audited. On the /admin page the same list is shown with a requeue button per row (GET /v1/admin/dead-letters, issue #579), so an operator can see failed callbacks without needing an MCP client.",
	}, s.adminDeadLetterEvents)
	addTool(s, &mcp.Tool{
		Name:        "admin_requeue_dead_letter_event",
		Description: "Re-enable the dead letter event so the replay sweep tries it again (issue #574), after the cause is fixed. dedup_key from admin_dead_letter_events. The attempt count is reset so it is not re-parked by the same sweep before it gets to run. Super-admin only; audited. From the /admin page the same action can be run (POST /v1/admin/dead-letters/requeue, issue #579).",
	}, s.adminRequeueDeadLetterEvent)
	addTool(s, &mcp.Tool{
		Name:        "admin_system_health",
		Description: "System health: uptime, number of recent MCP errors (24h), email queue depth. Super-admin only; read-only; audited.",
	}, s.adminSystemHealth)
	// Card t_94e5cca3: the phase-1 dormancy report (read-only). Mirrors the /admin
	// dormancy table and GET /v1/admin/dormancy.
	addTool(s, &mcp.Tool{
		Name:        "admin_dormancy_report",
		Description: "Per-account dormancy report (phase 1 of the dormancy policy): account id, workspace slug, owner email, created, last activity, days dormant, dormancy bucket (active|30|60|90|365), forms published, submissions in the last 90 days, sales ever, and - for an account that may NOT be treated as dormant - the exclusion reasons (system_account, has_sales, has_subscription, recent_submissions, workspace_in_trash, no_owner_email). Rows are most-dormant-first, capped by limit (1-500, default 200). READ-ONLY: nothing is deleted, unpublished or flagged; phases 2 (unpublish) and 3 (delete) are separate, unimplemented decisions. The same report is rendered on the /admin page and exported as CSV (GET /v1/admin/dormancy?format=csv). Super-admin only; audited.",
	}, s.adminDormancyReport)

	// ── Super-admin: suppression list (issue #601) ──────────────────────
	// The pre-send suppression list (issue #56) had no surface at all: SES
	// ingestion could add an address, but nothing could read the list back,
	// add one by hand, or remove a false positive without psql. These three
	// tools back the /admin suppression section; the dashboard uses the
	// matching REST endpoints (GET/POST/DELETE /v1/admin/suppressions).
	addTool(s, &mcp.Tool{
		Name:        "admin_list_suppressions",
		Description: "List of addresses blocked from being sent email (issue #601): email, reason (hard_bounce|complaint|manual), source, note, who added it and when. Searchable (q, matches email or note) and paginated (page/limit). Super-admin only; read-only; audited. On the /admin page the same list is displayed with a search box and pager (GET /v1/admin/suppressions, issue #601).",
	}, s.adminListSuppressions)
	addTool(s, &mcp.Tool{
		Name:        "admin_add_suppression",
		Description: "Add an address to the suppression list manually so that no email is sent to it (issue #601). reason: hard_bounce|complaint|manual (default manual), note optional. Idempotent — an address already on the list does not produce an error. Super-admin only; audited. From the /admin page it is added through the manual form (POST /v1/admin/suppressions, issue #601).",
	}, s.adminAddSuppression)
	addTool(s, &mcp.Tool{
		Name:        "admin_remove_suppression",
		Description: "Remove an address from the suppression list so that email delivery to it resumes (issue #601) — for false-positive cases. Super-admin only; audited. From the /admin page it is removed with a button + confirmation (DELETE /v1/admin/suppressions/{email}, issue #601).",
	}, s.adminRemoveSuppression)

	// ── Parity: in-app feedback (system-owned, two-way thread) ──────────
	// The dashboard's feedback form + admin triage, over MCP. Unlike the rest
	// of the registry, these tools are scoped to the authenticated USER, not to
	// a workspace: a feedback thread belongs to the person who filed it and
	// carries no account_id, so naming a workspace cannot widen the read. A
	// super-admin additionally sees every thread (triage) from the same tools.
	addTool(s, &mcp.Tool{
		Name:        "list_feedback",
		Description: "List your own feedback threads (newest first), filterable by status (open|in_progress|resolved|closed) and paginated (page/limit). The scope is the authenticated USER, not a workspace — feedback is not account data. Super-admin sees ALL threads (triage). Read-only.",
	}, s.listFeedback)
	addTool(s, &mcp.Tool{
		Name:        "get_feedback_thread",
		Description: "Read one feedback thread together with all its messages (author_kind: user|admin), earliest first. A thread belonging to another user answers 'not found' — the same as an id that does not exist. Super-admin can read any thread. Read-only.",
	}, s.getFeedbackThread)
	addTool(s, &mcp.Tool{
		Name:        "reply_feedback",
		Description: "Reply in a feedback thread. The author is determined by the caller: super-admin replies as admin (and an 'open' thread moves to in_progress), other users reply as the thread owner. Body minimum 10 characters. User scope — you cannot reply to someone else's thread.",
	}, s.replyFeedback)
	addTool(s, &mcp.Tool{
		Name:        "set_feedback_status",
		Description: "Change the status of a feedback thread (open|in_progress|resolved|closed). A triage decision — super-admin only; audited. Status is per-thread, not per-message.",
	}, s.setFeedbackStatus)
	addTool(s, &mcp.Tool{
		Name:        "retry_feedback_notify",
		Description: "Resend the email notification that FAILED for one feedback thread (the notify_failed_at field on the thread). This is the recovery path for delivery failures: an email that never made it into the queue is marked on the thread so it is visible, and this tool retries through the same channel. Outcome: queued|sent|failed|none|skipped ('none' means no failure was recorded; 'skipped' means no super-admin recipient is configured, so no email was attempted and no job was queued — check the super-admin list). Super-admin only; audited.",
	}, s.retryFeedbackNotify)

	// In-app chat (phase 1): the ONE write the chat has — file a feedback thread
	// from the conversation. Registered here so the dashboard capability has its
	// MCP counterpart (AGENTS.md parity), and it goes through the same
	// feedbacksvc as the /feedback page: same validation, same per-user daily
	// limit, same notification. User-scoped + write-scoped (see the file).
	addTool(s, &mcp.Tool{
		Name:        "submit_feedback",
		Description: "File feedback or a new feature request from a chat conversation. Same as submitting on the /feedback page: stored as a thread you can read and reply to, and the JomForm team is notified. Body minimum 10 characters, maximum 5000; maximum 10 submissions a day per user. Requires a USER token (OAuth/session) and readwrite scope — API keys are rejected because feedback is user personal data.",
	}, s.submitFeedback)

	// ── Parity: analytics (issue #396) ────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_form_analytics",
		Description: "Per-form analytics: number of entries (submissions), paid/confirmed/failed/pending, and NET revenue after refunds. submissions = entries recorded for that form, read from the table the form writes: mode=response → submissions table; payment form → sales (all statuses). mode is included so the label is correct. Money in minor units: revenue_sen is what buyers paid (includes the buyer-borne fee in fees_sen) and net_revenue_sen is what you keep; both are totals across currencies (do not label with a single symbol), broken down by the currency of each sale in revenue_by_currency / fees_by_currency / net_revenue_by_currency and refunded_by_currency (keys are ISO codes, values in minor units). Across all your workspaces. Read-only.",
	}, s.formAnalytics)
	addTool(s, &mcp.Tool{
		Name:        "get_product_analytics",
		Description: "Per-product analytics: units sold (paid), revenue. Money in minor units: revenue_sen (gross value of all sales) and paid_revenue_sen (paid, NET after refunds) are totals across currencies (do not label with a single symbol), broken down by the currency of each sale in revenue_by_currency and paid_revenue_by_currency (keys are ISO codes, values in minor units). Across all your workspaces. Read-only.",
	}, s.productAnalytics)

	// ── Parity: custom domains (issue #396) ──────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_domains",
		Description: "List the account's custom domains: id, domain, provider, verified, auto_cname. Read-only.",
	}, s.listDomains)
	addTool(s, &mcp.Tool{
		Name: "add_domain",
		// Card t_48a93084: the CNAME target is deployment-specific, so the
		// description points at the target the tool RETURNS instead of naming the
		// hosted product's (which is the wrong host on a self-hosted install).
		Description: "Claim a new custom domain (e.g. kedai.com). Then set the CNAME to the target returned by add_domain and call verify_domain.",
	}, s.addDomain)
	addTool(s, &mcp.Tool{
		Name:        "verify_domain",
		Description: "Verify a custom domain (id from list_domains). Only verified when the domain actually serves a JomForm page.",
	}, s.verifyDomain)
	addTool(s, &mcp.Tool{
		Name:        "remove_domain",
		Description: "Remove a custom domain claim (id from list_domains).",
	}, s.removeDomain)
	addTool(s, &mcp.Tool{
		Name:        "auto_cname_domain",
		Description: "Create a Cloudflare CNAME record automatically for the domain (Cloudflare must be linked). Returns auto_cname=created|failed.",
	}, s.autoCNAMEDomain)
	// Issue #397: per-domain TLS config + status.
	addTool(s, &mcp.Tool{
		Name:        "set_domain_tls_mode",
		Description: "Set the per-domain TLS mode: auto (Let's Encrypt, default) or custom (bring-your-own-cert). In custom mode, upload the certificate with set_domain_custom_cert.",
	}, s.setDomainTLSModeMCP)
	addTool(s, &mcp.Tool{
		Name:        "set_domain_custom_cert",
		Description: "Upload your own TLS certificate (bring-your-own-cert) for the domain: cert + key in PEM format. The domain is served this certificate.",
	}, s.setDomainCustomCertMCP)
	// Issue #398: per-domain ACME contact email (get/set).
	addTool(s, &mcp.Tool{
		Name:        "get_domain_tls_email",
		Description: "Read the per-domain ACME/Let's Encrypt email for auto-TLS. Empty = fall back to the account email, then the global ACME_EMAIL. Read-only.",
	}, s.getDomainTLSEmailMCP)
	addTool(s, &mcp.Tool{
		Name:        "set_domain_tls_email",
		Description: "Set the per-domain ACME/Let's Encrypt email for auto-TLS. Empty = fall back to the account email, then the global ACME_EMAIL.",
	}, s.setDomainTLSEmailMCP)

	// ── Parity: tracking (issue #396) ────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_tracking",
		Description: "Read the account's global tracking scripts (Google Analytics, Facebook Pixel, etc.) that are injected into the <head> of public forms. Read-only.",
	}, s.getTracking)
	addTool(s, &mcp.Tool{
		Name:        "set_tracking",
		Description: "Set the account's global tracking scripts. Only <script src=https://...>, <meta> and comments are allowed (sanitized).",
	}, s.setTracking)

	// ── Parity: CHIP account settings (issue #396) ──────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_chip_settings",
		Description: "Read the account's CHIP credential status: configured, verified, mode, brand_id, secret_key_masked, email_fallback, whitelist. The full secret is never returned. Read-only.",
	}, s.getChipSettings)
	addTool(s, &mcp.Tool{
		Name:        "set_chip_settings",
		Description: "Set the account's CHIP credentials (brand_id + secret_key). Empty secret = keep the existing key. Validated against CHIP before saving. Admin only. `mode` is accepted for compatibility with older clients and IGNORED: no payment behaviour depends on it.",
	}, s.setChipSettings)
	addTool(s, &mcp.Tool{
		Name:        "test_chip_connection",
		Description: "Test the account's CHIP credentials against CHIP without saving anything. Empty secret_key tests the stored key; a provided one tests a key you are about to save. Admin only.",
	}, s.testChipConnection)
	addTool(s, &mcp.Tool{
		Name:        "clear_chip_settings",
		Description: "Clear the account's CHIP credentials. Admin only.",
	}, s.clearChipSettings)

	// ── Parity: profile avatar (issue #396) ──────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "update_profile_avatar",
		Description: "Set the profile avatar (the URL must be an image hosted by JomForm from the upload bucket). Empty = remove the avatar.",
	}, s.updateProfileAvatar)

	// ── Parity: passkeys (issue #396) ────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_passkeys",
		Description: "List your account's passkeys (WebAuthn): id, created_at. Read-only.",
	}, s.listPasskeys)
	addTool(s, &mcp.Tool{
		Name:        "delete_passkey",
		Description: "Delete one passkey (id from list_passkeys).",
	}, s.deletePasskey)

	// ── Parity: social accounts (issue #396) ─────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_social_accounts",
		Description: "List linked social accounts (google|azure_ad|github|cloudflare): provider, email, name, picture, linked_at. Read-only.",
	}, s.listSocialAccounts)
	addTool(s, &mcp.Tool{
		Name:        "unlink_social_account",
		Description: "Unlink a social account (provider from list_social_accounts).",
	}, s.unlinkSocialAccount)

	// ── Parity: 2FA (issue #396) ─────────────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_2fa_status",
		Description: "Read your account's 2FA status: enabled (true/false). Read-only.",
	}, s.getTwoFAStatus)
	addTool(s, &mcp.Tool{
		Name:        "setup_2fa",
		Description: "Generate a TOTP secret for 2FA (not yet active). Returns secret + otpauth_url to be scanned by an authenticator app, then call enable_2fa with the code.",
	}, s.setupTwoFA)
	addTool(s, &mcp.Tool{
		Name:        "enable_2fa",
		Description: "Enable 2FA with the 6-digit code from the authenticator app (after setup_2fa).",
	}, s.enableTwoFA)
	addTool(s, disable2FATool(), s.disableTwoFA)

	// ── Parity: trusted devices (issue #454) ─────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "list_trusted_devices",
		Description: "List your account's trusted 2FA devices (label, ip, first_seen, expires_at). Trusted devices skip the 2FA code for 30 days. Read-only.",
	}, s.listTrustedDevices)
	addTool(s, &mcp.Tool{
		Name:        "revoke_trusted_device",
		Description: "Revoke one trusted device (device_id from list_trusted_devices). The next sign-in on that device will ask for a 2FA code again — immediately.",
	}, s.revokeTrustedDevice)
	addTool(s, &mcp.Tool{
		Name:        "revoke_all_trusted_devices",
		Description: "Revoke ALL trusted devices on your account (\"log out everywhere\"). Every device will ask for a 2FA code again.",
	}, s.revokeAllTrustedDevices)

	// ── Parity: system settings (issue #396) ─────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_system_settings",
		Description: "Read all platform system settings (site_*, landing_*). Read-only.",
	}, s.getSystemSettings)
	addTool(s, &mcp.Tool{
		Name:        "set_system_setting",
		Description: "Set one system setting (key must start with site_ or landing_).",
	}, s.setSystemSetting)

	// ── Parity: sales export CSV (issue #396) ────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "export_sales_csv",
		Description: "Export a set of account records as CSV. Without form_id it contains both sales AND responses (a record_type column marks each row, plus the recorded answer columns), matching the list that is shown. Filterable by form_id/status/date_from/date_to/q — the same filters as the list. The refund column exposes partial refunds (issue #518); total is what the buyer paid, fee is the buyer-borne processing fee inside it, and net is total − fee — the money you keep (card t_8064a965). Returns the csv content.",
	}, s.exportSalesCSV)

	// ── Parity: Cloudflare (issue #396) ──────────────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_cloudflare_status",
		Description: "Read the account's Cloudflare link status: linked (this account's Cloudflare credentials can be read) and configured — same meaning as /rest/cf/status: this DEPLOYMENT has a Cloudflare OAuth app, so the auto-CNAME flow can be offered. The OAuth link can only be completed in a browser; from MCP use connect_cloudflare_api_token or connect_cloudflare_global_key. Read-only.",
	}, s.getCloudflareStatus)
	addTool(s, &mcp.Tool{
		Name:        "connect_cloudflare_api_token",
		Description: "Link Cloudflare via API token (verified immediately, stored encrypted).",
	}, s.connectCloudflareAPIToken)
	addTool(s, &mcp.Tool{
		Name:        "connect_cloudflare_global_key",
		Description: "Link Cloudflare via global API key (verified immediately, stored encrypted).",
	}, s.connectCloudflareGlobalKey)
	addTool(s, &mcp.Tool{
		Name:        "disconnect_cloudflare",
		Description: "Unlink Cloudflare from the account.",
	}, s.disconnectCloudflare)

	// ── Parity: webhook signing key (issue #396) ─────────────────────────
	addTool(s, &mcp.Tool{
		Name:        "get_webhook_signing_key",
		Description: "Read the account's Ed25519 signing keys (key_id, public_key, alg) to verify X-JomForm-Signature-Ed25519 webhook signatures. Reading existing keys does not require write scope; GENERATING a new key (the first time, or after it is deleted) requires readwrite.",
	}, s.getWebhookSigningKey)

}

// requireWriteScope enforces the API-key WRITE SCOPE on a mutating tool. It is
// deliberately separate from the role gate (requireWriteRole /
// requireWriteRoleIn): scope and workspace role are two independent axes and a
// write must satisfy both.
//
// WHY IT IS ITS OWN FUNCTION (issue #645). The role gate used to BE the scope
// gate: each write tool called accountAndScope(ctx, true), which tested
// dbstore.ScopeHas(scopes, "readwrite") before doing anything. 5dafe3a (issue
// #162, workspace RBAC) swapped those call sites for requireWriteRole, whose
// chain reaches accountAndScope(ctx, false) — write=false — so the scope test
// was skipped and a readonly key could delete forms, archive products, cancel
// subscriptions and mint itself a readwrite key. Folding the two gates back into
// one function is what caused that: a future RBAC change could drop the scope
// half again with no test noticing. Keeping the scope test in a separately named
// function means every write tool names BOTH gates, and dropping one is a
// visible diff line rather than a changed boolean argument.
//
// OAuth tokens carry the GRANTED scope, not a hardcoded readwrite: a token
// minted from `?scope=read` holds ["read"] (oauthmeta.GrantedScopes resolves
// the grant at authorize time; authenticate in http.go returns t.Scope), so it
// IS refused right here. That refusal is the point of the read scope —
// TestReadScopeOAuthTokenIsRefusedEveryWrite and
// TestRequireWriteScopeRefusesReadScope exercise this exact function. A key
// with the legacy "admin" scope passes (ScopeHasWrite).
func (s *Server) requireWriteScope(ctx context.Context) error {
	id := IdentityFromContext(ctx)
	if id == nil {
		return errors.New("token missing or invalid")
	}
	if !dbstore.ScopeHasWrite(id.Scopes) {
		return errors.New("insufficient scope (needs readwrite)")
	}
	return nil
}

// requireWriteScopeIn is the per-workspace form of requireWriteScope: the scope
// axis is credential-wide, not per-workspace, so accountID is only there to keep
// the call sites symmetrical with requireWriteRoleIn.
func (s *Server) requireWriteScopeIn(ctx context.Context, _ string) error {
	return s.requireWriteScope(ctx)
}

// accountAndScope resolves the account id for a call, enforcing the WRITE SCOPE
// when write is true.
//
// Deprecated for write gates: the write argument is a boolean a caller can get
// wrong (that is exactly the 5dafe3a regression). Write tools must instead name
// requireWriteScope / requireWriteRole explicitly. Kept because read calls use
// it with write=false and two write call sites still pass true.
func (s *Server) accountAndScope(ctx context.Context, write bool) (accountID string, err error) {
	id := IdentityFromContext(ctx)
	if id == nil {
		return "", fmt.Errorf("token missing or invalid")
	}
	if write && !dbstore.ScopeHasWrite(id.Scopes) {
		return "", fmt.Errorf("insufficient scope (needs readwrite)")
	}
	// Per-call scope: if an explicit single account is requested, use it;
	// otherwise for account-and-write tools we resolve to the caller's first
	// accessible account by default (a workspace_id is honoured too).
	if cs := callScopeFrom(ctx); cs != nil {
		if cs.ExplicitAccountID != "" {
			return cs.ExplicitAccountID, nil
		}
		if len(cs.Accounts) > 0 {
			return cs.Accounts[0], nil
		}
		return id.AccountID, nil
	}
	return id.AccountID, nil
}

// accountAndRole resolves the account id + the caller's role in the workspace
// that owns it (issue #162). It honours the per-call scope (issue #307): when
// the call names a workspace/account, the role is that workspace's role; otherwise the
// caller's first accessible account's role is used. role is "" when the caller
// is the owner (admin) or has no linked workspace.
func (s *Server) accountAndRole(ctx context.Context) (accountID, role string, err error) {
	accountID, err = s.accountAndScope(ctx, false)
	if err != nil {
		return "", "", err
	}
	if s.deps.Workspaces == nil || s.deps.WorkspaceMembers == nil {
		return accountID, "", nil
	}
	userID := s.userIDFor(ctx)
	if userID == "" {
		return "", "", fmt.Errorf("token missing or invalid")
	}
	// If the call scoped to an explicit workspace, use that workspace's role (already
	// resolved by resolveScope — issue #307).
	if cs := callScopeFrom(ctx); cs != nil && cs.WorkspaceID != "" {
		return accountID, cs.Role, nil
	}
	role, err = s.deps.Workspaces.RoleByAccount(ctx, accountID, userID)
	if err != nil {
		return "", "", fmt.Errorf("db: %w", err)
	}
	return accountID, role, nil
}

// requireWriteRole enforces BOTH gates a write needs, on the account resolved
// for this call:
//
//   - the API-key WRITE SCOPE (requireWriteScope): a readonly key is refused.
//     This is the axis 5dafe3a dropped (issue #645).
//   - the manage-or-admin ROLE in the workspace owning that account (issue #162
//     MCP parity): a read_only member's agent cannot write.
//
// The two are separate calls on purpose — see requireWriteScope for why folding
// them into one function is what caused the regression.
func (s *Server) requireWriteRole(ctx context.Context) (accountID string, err error) {
	if err := s.requireWriteScope(ctx); err != nil {
		return "", err
	}
	accountID, role, err := s.accountAndRole(ctx)
	if err != nil {
		return "", err
	}
	if role != "" && role != dbstore.RoleManage && role != dbstore.RoleAdmin {
		return "", errors.New(RoleManageRequiredLeadIn)
	}
	return accountID, nil
}

// requireAdminRole enforces the admin-only gate (CHIP credentials + member
// management), on BOTH axes: the API-key write scope and the admin role.
func (s *Server) requireAdminRole(ctx context.Context) (accountID string, err error) {
	if err := s.requireWriteScope(ctx); err != nil {
		return "", err
	}
	accountID, role, err := s.accountAndRole(ctx)
	if err != nil {
		return "", err
	}
	if role != "" && role != dbstore.RoleAdmin {
		return "", fmt.Errorf("workspace admin required")
	}
	return accountID, nil
}

// requireAdminRoleForRead is the ROLE-only form of requireAdminRole, for an
// admin-surface READ.
//
// Issue #645 made the scope axis explicit, which is correct for writes — but it
// also means a tool that only READS an admin view must not be routed through
// requireAdminRole, or a readonly key would be refused on a read (the same
// defect as item 4 of the card, one layer down). Naming the two intents
// separately is what keeps "admin read" and "admin write" from sharing a gate
// whose meaning changed underneath them.
func (s *Server) requireAdminRoleForRead(ctx context.Context) error {
	_, role, err := s.accountAndRole(ctx)
	if err != nil {
		return err
	}
	if role != "" && role != dbstore.RoleAdmin {
		return fmt.Errorf("workspace admin required")
	}
	return nil
}
