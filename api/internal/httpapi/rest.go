// REST API v1: session- and API-key-authenticated merchant endpoints.
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/chat"
	"github.com/wzul/jomform/api/internal/currency"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
	"github.com/wzul/jomform/api/internal/llm"
	"github.com/wzul/jomform/api/internal/payments"
	"github.com/wzul/jomform/api/internal/tlsmgr"
)

// The per-IP caps this file's registrations apply to /v1/rest routes through a
// middleware.RateLimitWith wrap.
//
// WHY THEY ARE NAMED (t_9cdfa18e). These four numbers used to live only in the
// call sites below, and the published contract's per-IP Rate sentences were
// hand-typed copies of them in openapi_routes.go. A copy has no owner: the
// call site could move to 9/s while api/openapi/openapi.json — and /docs/api,
// which renders the same sentences — went on promising 2/s, with every guard
// green. Named here, the registration and the sentence read ONE number
// (aiGenerateRatePolicy / uploadRatePolicy in openapi_routes.go render from
// these), and openapi_parity_test.go reads the number back out of these very
// call sites to pin the sentence to what the server really passes.
//
// The unit in the name is the unit the CONTRACT quotes. The limiter itself
// always takes tokens per second, so the hourly cap is divided by 3600 at the
// call site — the same shape rest_chat.go uses for chatRatePerHour.
const (
	// aiGenerateRatePerHour / aiGenerateBurst bound POST /v1/rest/forms/ai-generate.
	// An LLM call costs money, so this is the tightest per-IP cap in the API.
	aiGenerateRatePerHour = 5.0
	aiGenerateBurst       = 3.0
	// uploadRatePerSecond / uploadBurst bound the three upload routes
	// (image / avatar / asset) — one bucket each, same numbers for all three.
	uploadRatePerSecond = 2.0
	uploadBurst         = 6.0
)

// RESTHandlers serves /v1/rest/* merchant resources.
type RESTHandlers struct {
	Accounts          *dbstore.AccountStore
	Products          *dbstore.ProductStore
	ProductVariations *dbstore.ProductVariationStore
	Forms             *dbstore.FormStore
	Keys              *dbstore.APIKeyStore
	Sales             *dbstore.SaleStore
	Subs              *dbstore.SubscriptionStore
	Sessions          *auth.Sessions
	Pay               *payments.Service
	Webhooks          *dbstore.WebhookStore
	SystemSettings    *dbstore.SystemSettingStore
	Workspaces        *dbstore.WorkspaceStore
	// Issue #161: workspace membership + invites.
	WorkspaceMembers *dbstore.WorkspaceMemberStore
	// Issue #444: affiliate marketing.
	Affiliates *dbstore.AffiliateStore
	Passkeys   *dbstore.PasskeyStore
	// Issue #161: Mailer delivers the invite email (nil in bare-mode tests).
	Mailer VerifyMailer
	// Issue #199: when non-nil, the invite email is enqueued and sent by a
	// background worker instead of synchronously in the request handler. Nil
	// (bare-mode tests) → synchronous send via Mailer.
	EmailQueue *emailqueue.Queue
	// BaseURL is the app origin used to build invite signup links, and — on a
	// self-hosted install — the workspace/form address every payload prints.
	BaseURL string
	// SelfHosted declares that this deployment IS the origin its buyers load
	// (JOMFORM_SELF_HOSTED): ONE hostname for every workspace, no tenant
	// subdomain. It decides the shape of the public form URL this API reports
	// (card t_6c0badbe): on a self-hosted install {BaseURL}/f/{slug} is the only
	// address that serves the form, and the hosted product's tenant-subdomain
	// shape is a 404. Wired here from config.Config.SelfHosted by main.go; the
	// same flag PublicHandlers uses to SERVE the form from its own origin (issue
	// #77 / PublicHandlers.ownOriginForm), so the URL the dashboard prints and
	// the host that answers cannot disagree. The SAME field decides the WORKSPACE
	// address every REST payload prints (card t_48a93084), and it is decided by
	// the flag, never by comparing hosts: the hosted product's APP_BASE_URL is
	// its own apex (issue #77).
	SelfHosted bool
	Users      *dbstore.UserStore
	Domains    *dbstore.DomainStore
	// Issue #212: auto-TLS at origin. Nil when ACME is not configured.
	// Verified custom domains are registered here so the origin provisions
	// + renews their Let's Encrypt certs (SNI-based cert selection).
	TLS *tlsmgr.Manager
	// CookieDomain is the shared Domain attribute for the session cookie
	// (e.g. ".jomform.com") so passkey/social sessions remain valid on product
	// subdomains such as mcp.jomform.com (issue #140). Empty → host-only.
	CookieDomain string
	// ChipProbe validates credentials against CHIP at save time.
	ChipProbe interface {
		ValidateCredentials(ctx context.Context, secret string) error
	}

	// EncryptKey is the 32-byte AES-GCM key from ENCRYPTION_KEY env.
	SecretsKey []byte

	// Store is the S3/MinIO object store for image-block uploads (issue #61).
	// Nil when media storage is not configured (upload returns 503).
	Store ObjectStoreIface

	// Events is the audit/behaviour log (issue #92). Nil in bare-mode tests;
	// Record calls are no-ops when nil.
	Events *dbstore.EventStore

	// EmailDeliveryEvents is the SES delivery-event log (issue #92/#106). Nil
	// in bare-mode tests; the per-sale email status view returns empty.
	EmailDeliveryEvents *dbstore.EmailDeliveryEventStore
	// WebhookEvents is the inbound callback event store (issue #579). Nil → the
	// admin dead-letter endpoints report the store as unavailable.
	WebhookEvents *dbstore.WebhookEventStore
	// Suppressions is the SES suppression list (issue #56). Nil → the admin
	// suppression endpoints report the store as unavailable. Backs the
	// super-admin screen added in issue #601.
	Suppressions *dbstore.SuppressionStore

	// Dormancy is the read-only phase-1 dormancy report store (card t_94e5cca3).
	// Nil → GET /v1/admin/dormancy reports the store as unavailable. It owns no
	// table: the report is computed from accounts/events/forms/sales/submissions/
	// subscriptions in one statement.
	Dormancy *dbstore.DormancyStore

	// Issue #171: response-only form submissions (list + count endpoints).
	Submissions *dbstore.SubmissionStore

	// Issue #193: per-form notification rules.
	Notifications *dbstore.FormNotificationStore

	// Issue #180: media library metadata store (list/filter/soft-delete).
	// Nil in bare-mode tests; the media endpoints return empty/503.
	Media *dbstore.MediaStore

	// Shipping is the managed zone/rate store (rate source "zones"). Nil in
	// bare-mode tests: a form whose shipping block asks for a zone rate then
	// reports that the zone rate source is not configured rather than charging 0.
	Shipping *dbstore.ShippingStore

	// Couriers is the phase-2 courier wiring (registry + connections + the
	// EasyParcel OAuth app config). It backs the dashboard's connect/disconnect
	// surface, and the PUBLIC callback that completes an OAuth consent. Nil in
	// bare-mode tests: the connect surface reports "not configured" and the
	// public callback refuses.
	Couriers *Couriers

	// Issue #364: background asset-scan queue. Same Valkey-backed queue as
	// email (#199) / spam judge (#245). Code-asset uploads enqueue an
	// asset_scan job here; a nil queue (dev, no Valkey) means the asset stays
	// 'pending' and is not served until a scan flips it (fail-closed).
	AssetQueue *emailqueue.Queue

	// Issue #184: LLM client for auto-generating per-workspace legal pages.
	// Nil in bare-mode tests; the generate endpoint returns 503.
	LLM interface {
		GenerateLegalPage(ctx context.Context, pageType string, d llm.BusinessDetails) (string, error)
		// Issue #256: AI form builder — draft a form config from a prompt.
		GenerateFormConfig(ctx context.Context, prompt string) (string, error)
	}
	// Issue #185: BNM exchange-rate cache (Valkey). Nil in bare-mode tests;
	// the currency-rates endpoint returns empty.
	Currency *currency.Store

	// SuperAdmin (issue #385/#387): reports whether an email is on the site
	// super-admin allowlist (SUPER_ADMIN_EMAILS). Nil → no super-admins.
	// Gates the /v1/admin/overview endpoint (issue #387) so platform-wide
	// usage metrics are only visible to the platform owner.
	SuperAdmin func(email string) bool

	// Realtime (issue #447): in-memory pub/sub hub backing the SSE stream
	// (GET /v1/rest/events/stream). Nil in bare-mode tests → the stream
	// endpoint returns 503. The payments OnPaid + response.submitted paths
	// publish sale/response events here so connected dashboards update live.
	Realtime *RealtimeHub

	// PaymentFees is the per-workspace processing-fee store (card t_135c3c72):
	// one rate per CHIP payment method, plus the merchant's pass-on
	// acknowledgement. Nil in bare-mode tests → the fee is simply inactive,
	// which is the pre-feature behaviour.
	PaymentFees *dbstore.PaymentFeeStore

	// Feedback is the in-app feedback thread store (system-owned, two-way
	// conversation). Nil → the feedback endpoints report the store as
	// unavailable. It is NOT account-scoped: a thread carries no account_id,
	// so these routes never resolve a workspace (see rest_feedback.go).
	Feedback *dbstore.FeedbackThreadStore
	// SuperAdminEmails is the same allowlist backing SuperAdmin, kept as a list
	// so the feedback notification can address the platform team through the
	// existing email lane. One list, one place to change who is notified.
	SuperAdminEmails []string

	// Chat is the in-app assistant: phase 1 reads the caller's own data and files
	// feedback; phase 2 PROPOSES writes for the user to confirm. Nil when no LLM
	// is configured → the endpoint reports the assistant as unavailable rather than
	// offering a chat box that errors. Its message path runs its tool calls under a
	// READONLY identity (see rest_chat.go), so a message cannot write; only
	// Confirm — with a server-issued token a human holds — runs a tool with the
	// write scope.
	Chat ChatRunner
	// ChatLimiter is the per-user chat message bucket (LLM calls cost money).
	// Nil in bare-mode tests.
	ChatLimiter *middleware.RateLimiter
	// ChatConfirmLimiter is the per-user PROPOSAL CONFIRMATION bucket (phase 2).
	// Separate from ChatLimiter because a confirmation costs no model call: the
	// thing being bounded is how fast a person can be walked through confirmation
	// cards, not spend. Nil in bare-mode tests.
	ChatConfirmLimiter *middleware.RateLimiter
	// ChatSessions is the stored chat history (see rest_chat_sessions.go). Nil
	// when unwired: the history routes then answer 503 rather than an empty
	// list, which the sidebar would render as "no conversations" — a wrong
	// answer that looks like a true one.
	ChatSessions *dbstore.ChatSessionStore
}

// ChatRunner answers one chat message. The production value is *chat.Runner;
// it is an interface only so the REST edge can be tested with a fake.
type ChatRunner interface {
	Run(ctx context.Context, history []chat.Turn, message string) (*chat.Result, error)
	// Ready reports whether this runner can answer at all: a model is wired AND
	// it has a credential. GET /v1/rest/chat reports it as `available`, which
	// the UI uses to decide whether to offer the box at all.
	//
	// It is part of the interface on purpose. Availability used to be inferred
	// from `Chat != nil`, which answers "was the object constructed", not "can
	// this deployment answer a message" — a deployment with no model key got a
	// box whose every message died. Making it a method means every
	// implementation must answer the question, and the compiler refuses one
	// that would rather not.
	Ready() bool
	// Confirm executes a proposal a HUMAN confirmed on the card (phase 2). It
	// takes the opaque token and the argument hash the card carried, and it
	// executes the STORED arguments under writeCtx (the human's identity with the
	// write scope). It is the only method on this interface that can write, and it
	// is unreachable without a token this server issued to this user.
	Confirm(ctx context.Context, writeCtx context.Context, userID, token, argsHash string) (*chat.Confirmed, error)
	// Cancel spends a proposal WITHOUT executing it — the user's "no". It is on
	// this interface because the UI must be able to kill a card server-side: a
	// cancel that only hid the card in the browser would leave a live
	// authorisation behind.
	Cancel(ctx context.Context, userID, token string) error
}

// RegisterRoutes wires REST endpoints.
func (h *RESTHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("GET /v1/rest/products", h.authed(h.listProducts))
	mux.Handle("POST /v1/rest/products", h.authed(h.createProduct))
	mux.Handle("POST /v1/rest/products/bulk", h.authed(h.bulkProducts))
	// Tong sampah (products): the bin's three routes. `trash` is a literal
	// segment and `{id}` is a wildcard, and net/http picks the MOST SPECIFIC
	// pattern — a literal matches a strict subset of what the wildcard matches —
	// so the bin read is not shadowed by GET /{id}. The rule is order-
	// independent (go.dev/blog/routing-enhancements, "the most specific pattern
	// wins"), so this registration order is not what decides it; the literal
	// itself is. TestRESTProductTrashRouteIsNotShadowedByProductID asserts the
	// outcome in both directions, because nothing in this file can.
	mux.Handle("GET /v1/rest/products/trash", h.authed(h.listTrashedProducts))
	mux.Handle("PUT /v1/rest/products/{id}", h.authed(h.updateProduct))
	mux.Handle("GET /v1/rest/products/{id}", h.authed(h.getProduct))
	mux.Handle("DELETE /v1/rest/products/{id}", h.authed(h.deleteProduct))
	mux.Handle("POST /v1/rest/products/{id}/restore", h.authed(h.restoreProduct))
	mux.Handle("DELETE /v1/rest/products/{id}/purge", h.authed(h.purgeProduct))
	mux.Handle("PUT /v1/rest/forms/{id}/chip-override", h.authed(h.setFormChipOverride))
	mux.Handle("DELETE /v1/rest/forms/{id}/chip-override", h.authed(h.clearFormChipOverride))
	// Issue #208: multiple per-form CHIP feeds with conditional logic.
	mux.Handle("GET /v1/rest/forms/{id}/chip-feeds", h.authed(h.getFormChipFeeds))
	mux.Handle("PUT /v1/rest/forms/{id}/chip-feeds", h.authed(h.setFormChipFeeds))
	mux.Handle("GET /v1/rest/forms", h.authed(h.listForms))
	// Trash (issue: workspace must be emptyable). A literal segment beats
	// `{id}`, so this is not shadowed by GET /v1/rest/forms/{id}.
	mux.Handle("GET /v1/rest/forms/trash", h.authed(h.listTrashedForms))
	mux.Handle("POST /v1/rest/forms", h.authed(h.createForm))
	// Issue #256: AI form builder — draft a form config from a prompt. Tight
	// per-IP rate limit because LLM calls cost money (aiGenerateRatePerHour,
	// burst aiGenerateBurst — the contract quotes these same constants).
	mux.Handle("POST /v1/rest/forms/ai-generate", middleware.RateLimitWith(aiGenerateRatePerHour/3600.0, aiGenerateBurst)(h.authed(h.aiGenerateForm)))
	mux.Handle("POST /v1/rest/forms/bulk", h.authed(h.bulkForms))
	mux.Handle("GET /v1/rest/forms/{id}", h.authed(h.getForm))
	mux.Handle("PATCH /v1/rest/forms/{id}", h.authed(h.updateForm))
	mux.Handle("DELETE /v1/rest/forms/{id}", h.authed(h.deleteForm))
	// Tong sampah (Wan's decision): a trashed form can come back, and can also
	// be destroyed for good. Restore touches no CASCADE; purge does, so purge
	// additionally refuses a form that has ever been sold.
	mux.Handle("POST /v1/rest/forms/{id}/restore", h.authed(h.restoreForm))
	mux.Handle("DELETE /v1/rest/forms/{id}/purge", h.authed(h.purgeForm))
	// Issue #171: response-only form submissions (list + count).
	mux.Handle("GET /v1/rest/forms/{id}/submissions", h.authed(h.listSubmissions))
	mux.Handle("GET /v1/rest/forms/{id}/submissions/count", h.authed(h.countSubmissions))
	mux.Handle("POST /v1/rest/forms/{id}/publish", h.authed(h.publishForm))
	mux.Handle("POST /v1/rest/forms/{id}/status", h.authed(h.setFormStatus))
	mux.Handle("GET /v1/rest/keys", h.authed(h.listKeys))
	mux.Handle("POST /v1/rest/keys", h.authed(h.createKey))
	mux.Handle("DELETE /v1/rest/keys/{id}", h.authed(h.revokeKey))
	mux.Handle("GET /v1/rest/summary", h.authed(h.summary))
	mux.Handle("PUT /v1/rest/settings/chip", h.authed(h.setChipCreds))
	mux.Handle("GET /v1/rest/settings/chip", h.authed(h.chipStatus))
	mux.Handle("DELETE /v1/rest/settings/chip", h.authed(h.deleteChipCreds))
	mux.Handle("PUT /v1/rest/settings/custom-domain", h.authed(h.setCustomDomain))
	mux.Handle("GET /v1/rest/settings/custom-domain", h.authed(h.getCustomDomain))
	mux.Handle("PUT /v1/rest/settings/einvoice", h.authed(h.putEinvoicing))
	// Card t_135c3c72: per-method processing-fee rates + the merchant's
	// pass-on disclosure acknowledgement, and the per-form override.
	mux.Handle("GET /v1/rest/settings/payment-fees", h.authed(h.getFeeSettings))
	mux.Handle("PUT /v1/rest/settings/payment-fees", h.authed(h.putFeeSettings))
	mux.Handle("GET /v1/rest/forms/{id}/fee-override", h.authed(h.getFormFeeOverride))
	mux.Handle("PUT /v1/rest/forms/{id}/fee-override", h.authed(h.putFormFeeOverride))
	mux.Handle("PUT /v1/rest/settings/tracking", h.authed(h.putTracking))
	mux.Handle("GET /v1/rest/settings/tracking", h.authed(h.getTracking))
	mux.Handle("PUT /v1/rest/forms/{id}/tracking", h.authed(h.putFormTracking))
	mux.Handle("PUT /v1/rest/forms/{id}/settings", h.authed(h.putFormSettings))
	mux.Handle("PUT /v1/rest/forms/{id}/restrictions", h.authed(h.putFormRestrictions))
	// Issue #497: per-form restaurant-menu config (the menu is a form setting,
	// served at the form's own URL).
	mux.Handle("GET /v1/rest/forms/{id}/menu", h.authed(h.getFormMenu))
	mux.Handle("PUT /v1/rest/forms/{id}/menu", h.authed(h.putFormMenu))
	// The merchant's own printable QR stand (issue #438). The PUBLIC route
	// /qr/{slug}/stand applies the form page's status gate — a draft's stand
	// 404s there because it prints the form's title — so the dashboard's
	// "Print stand" action opens this authed route instead, which serves the
	// same document for a form the caller owns, at any status. See
	// rest_qr_stand.go.
	mux.Handle("GET /v1/rest/forms/{id}/qr-stand", h.authed(h.getFormQRStand))
	// Issue #177: per-form notification email settings (Gravity-Forms-style).
	mux.Handle("GET /v1/rest/forms/{id}/email-settings", h.authed(h.getFormEmailSettings))
	mux.Handle("PUT /v1/rest/forms/{id}/email-settings", h.authed(h.saveFormEmailSettings))
	// Issue #453: clear every per-form email override (inherit workspace again).
	mux.Handle("DELETE /v1/rest/forms/{id}/email-settings", h.authed(h.clearFormEmailOverrides))
	// Issue #193: per-form notification rules (list/create/update/delete).
	mux.Handle("GET /v1/rest/forms/{id}/notifications", h.authed(h.listFormNotifications))
	mux.Handle("POST /v1/rest/forms/{id}/notifications", h.authed(h.createFormNotification))
	mux.Handle("PUT /v1/rest/forms/{id}/notifications/{nid}", h.authed(h.updateFormNotification))
	mux.Handle("DELETE /v1/rest/forms/{id}/notifications/{nid}", h.authed(h.deleteFormNotification))
	// Issue #179: per-form confirmations (Gravity-Forms-style).
	mux.Handle("GET /v1/rest/forms/{id}/confirmations", h.authed(h.getFormConfirmations))
	mux.Handle("PUT /v1/rest/forms/{id}/confirmations", h.authed(h.saveFormConfirmations))
	mux.HandleFunc("GET /v1/templates", h.listTemplates)
	// Card t_48a93084: the deployment's own facts, so the dashboard can print a
	// workspace address that EXISTS on this install (see meta.go).
	mux.HandleFunc("GET /v1/meta", h.deploymentMeta)
	mux.HandleFunc("GET /v1/system-settings/public", h.publicSystemSettings)
	mux.Handle("GET /v1/system-settings", h.authed(h.listSystemSettings))
	mux.Handle("PUT /v1/system-settings", h.authed(h.setSystemSetting))
	mux.Handle("POST /v1/templates/use", h.authed(h.useTemplate))
	// Issue #457: merchant-managed templates (owner-scoped CRUD + clone).
	mux.Handle("GET /v1/rest/templates", h.authed(h.listOwnTemplates))
	mux.Handle("POST /v1/rest/templates", h.authed(h.createOwnTemplate))
	mux.Handle("GET /v1/rest/templates/{id}", h.authed(h.getOwnTemplate))
	mux.Handle("PUT /v1/rest/templates/{id}", h.authed(h.updateOwnTemplate))
	mux.Handle("DELETE /v1/rest/templates/{id}", h.authed(h.deleteOwnTemplate))
	mux.Handle("POST /v1/rest/templates/{id}/use", h.authed(h.useOwnTemplate))
	mux.Handle("GET /v1/cf/start", h.authed(h.cfStart))
	mux.HandleFunc("GET /v1/cf/callback", h.cfCallback)
	mux.Handle("GET /v1/cf/status", h.authed(h.cfStatus))
	mux.Handle("POST /v1/cf/disconnect", h.authed(h.cfDisconnect))
	mux.Handle("POST /v1/cf/api-token", middleware.RateLimit(h.authed(h.cfConnectAPIToken)))
	mux.Handle("POST /v1/cf/global-key", middleware.RateLimit(h.authed(h.cfConnectGlobalKey)))
	mux.Handle("POST /v1/auth/passkeys/register/start", h.authed(h.passkeyRegisterStart))
	mux.Handle("POST /v1/auth/passkeys/register/finish", h.authed(h.passkeyRegisterFinish))
	mux.Handle("POST /v1/auth/passkeys/login/start", middleware.RateLimit(http.HandlerFunc(h.passkeyLoginStart)))
	mux.Handle("POST /v1/auth/passkeys/login/finish", middleware.RateLimit(http.HandlerFunc(h.passkeyLoginFinish)))
	mux.Handle("GET /v1/auth/passkeys", h.authed(h.passkeyList))
	mux.Handle("DELETE /v1/auth/passkeys/{id}", h.authed(h.passkeyDelete))
	mux.Handle("GET /v1/workspaces", h.authed(h.listWorkspaces))
	mux.Handle("POST /v1/workspaces", h.authed(h.createWorkspace))
	// Rename a workspace's name and/or slug. A name needs manage-or-admin role;
	// the SLUG additionally needs OWNERSHIP, because the slug is the tenant's
	// public host — printed on QR stands and shared links. The old slug is kept
	// as an alias (migration 000068) and keeps resolving to the same workspace,
	// so the change never breaks a URL already in the wild.
	mux.Handle("PATCH /v1/workspaces/{id}", h.authed(h.renameWorkspace))
	// The bin, read half. Registered BEFORE the {id} patterns so the literal
	// `trash` segment wins — a workspace id could never be "trash", but net/http
	// resolves the literal first anyway, and registering it here makes that
	// explicit rather than incidental.
	mux.Handle("GET /v1/workspaces/trash", h.authed(h.listTrashedWorkspaces))
	// Delete a workspace: it goes to the BIN (deleted_at), it is NOT destroyed.
	// Owner-only, and refused only when it would leave the owner with no LIVE
	// workspace. Every row it holds stays on disk until a human purges.
	mux.Handle("DELETE /v1/workspaces/{id}", h.authed(h.deleteWorkspace))
	// Bring a binned workspace back. No confirmation — it destroys nothing.
	// 409 when the slug was re-claimed while it sat in the bin.
	mux.Handle("POST /v1/workspaces/{id}/restore", h.authed(h.restoreWorkspace))
	// Destroy a binned workspace FOR GOOD. Confirmed by typing its slug. This is
	// the only irreversible call in the workspace surface. The ROUTE is
	// session-only (an API key has no user, so a key cannot drive it), and the
	// tool in front of the store is never proposable to the in-app chat — but it
	// IS a registered MCP tool an owner's write-scoped key can call, exactly like
	// purge_form / purge_product / purge_media. See purgeWorkspace.
	mux.Handle("DELETE /v1/workspaces/{id}/purge", h.authed(h.purgeWorkspace))
	// Pre-flight for the delete button: the same emptiness definition, as a
	// read, so the UI can list blockers before the destructive call.
	mux.Handle("GET /v1/workspaces/{id}/deletion-check", h.authed(h.workspaceDeletionCheck))
	mux.Handle("GET /v1/workspaces/{id}/settings", h.authed(h.getWorkspaceSettings))
	mux.Handle("PUT /v1/workspaces/{id}/settings", h.authed(h.saveWorkspaceSettings))
	mux.Handle("GET /v1/workspaces/{id}/email-settings", h.authed(h.getWorkspaceEmailSettings))
	mux.Handle("PUT /v1/workspaces/{id}/email-settings", h.authed(h.saveWorkspaceEmailSettings))
	// Issue #184: per-workspace hosted legal pages (privacy/terms/refund).
	mux.Handle("GET /v1/workspaces/{id}/legal", h.authed(h.getWorkspaceLegalPages))
	mux.Handle("POST /v1/workspaces/{id}/legal/generate", h.authed(h.generateWorkspaceLegalPage))
	mux.Handle("PUT /v1/workspaces/{id}/legal", h.authed(h.saveWorkspaceLegalPage))
	// Issue #444: affiliate marketing.
	mux.Handle("GET /v1/workspaces/{id}/affiliates", h.authed(h.listAffiliates))
	mux.Handle("POST /v1/workspaces/{id}/affiliates", h.authed(h.createAffiliate))
	mux.Handle("PUT /v1/workspaces/{id}/affiliates/{affid}/commission", h.authed(h.setAffiliateCommission))
	mux.Handle("GET /v1/workspaces/{id}/affiliates/{affid}/sales", h.authed(h.getAffiliateSales))
	// Issue #161: workspace membership + invites (admin only).
	mux.Handle("POST /v1/workspaces/{id}/invites", h.authed(h.createInvite))
	mux.Handle("GET /v1/workspaces/{id}/invites", h.authed(h.listInvites))
	mux.Handle("DELETE /v1/workspaces/{id}/invites/{inviteId}", h.authed(h.deleteInvite))
	mux.Handle("GET /v1/workspaces/{id}/members", h.authed(h.listMembers))
	mux.Handle("PATCH /v1/workspaces/{id}/members/{userId}", h.authed(h.updateMemberRole))
	mux.Handle("DELETE /v1/workspaces/{id}/members/{userId}", h.authed(h.removeMember))
	mux.Handle("GET /v1/rest/settings/einvoice", h.authed(h.getEinvoicing))
	// Managed shipping zones + rates (the shipping block's "zones" rate source).
	mux.Handle("GET /v1/rest/shipping", h.authed(h.getShipping))
	mux.Handle("PUT /v1/rest/shipping", h.authed(h.putShipping))
	// Couriers (phase 2): which provider a workspace has connected, where the
	// parcel ships from, and the connect/disconnect of each. The OAuth CALLBACK
	// is a public route (the merchant's browser arrives from EasyParcel), so it
	// lives on the public handler next to /v1/payments/chip/callback.
	mux.Handle("GET /v1/rest/shipping/couriers", h.authed(h.getShippingCouriers))
	mux.Handle("POST /v1/rest/shipping/couriers/{provider}/connect", h.authed(h.postShippingCourierConnect))
	mux.Handle("POST /v1/rest/shipping/couriers/{provider}/disconnect", h.authed(h.postShippingCourierDisconnect))
	mux.Handle("GET /v1/rest/currency/rates", h.authed(h.getCurrencyRates))
	mux.Handle("GET /v1/rest/webhooks", h.authed(h.listWebhooks))
	mux.Handle("GET /v1/rest/settings/webhook-key", h.authed(h.getWebhookKey))
	mux.Handle("POST /v1/rest/webhooks", h.authed(h.createWebhook))
	mux.Handle("PUT /v1/rest/webhooks/{id}", h.authed(h.updateWebhook))
	mux.Handle("GET /v1/rest/webhooks/{id}/deliveries", h.authed(h.listWebhookDeliveries))
	// Issue #197: manual retry of a failed delivery (re-enqueues immediately).
	mux.Handle("POST /v1/rest/webhooks/{id}/deliveries/{deliveryId}/retry", h.authed(h.retryWebhookDelivery))
	// Issue #197: manual retry of a dead-lettered retry.
	mux.Handle("POST /v1/rest/webhooks/{id}/retries/{retryId}/retry", h.authed(h.retryDeadLettered))
	mux.Handle("DELETE /v1/rest/webhooks/{id}", h.authed(h.deleteWebhook))
	mux.Handle("GET /v1/rest/domains", h.authed(h.listCustomDomains))
	mux.Handle("POST /v1/rest/domains", h.authed(h.addCustomDomain))
	mux.Handle("POST /v1/rest/domains/{id}/verify", h.authed(h.verifyCustomDomainByID))
	mux.Handle("POST /v1/rest/domains/{id}/auto-cname", h.authed(h.autoCreateCNAMEForDomain))
	mux.Handle("POST /v1/rest/domains/{id}/tls-mode", h.authed(h.setDomainTLSMode))
	mux.Handle("POST /v1/rest/domains/{id}/tls-email", h.authed(h.setDomainTLSEmail))
	mux.Handle("POST /v1/rest/domains/{id}/custom-cert", h.authed(h.uploadCustomCert))
	mux.Handle("DELETE /v1/rest/domains/{id}", h.authed(h.deleteCustomDomainByID))
	mux.Handle("DELETE /v1/rest/settings/custom-domain", h.authed(h.deleteCustomDomain))
	mux.Handle("POST /v1/rest/sales/{ref}/confirm", h.authed(h.confirmSale))
	mux.Handle("POST /v1/rest/sales/{ref}/capture", h.authed(h.captureSale))
	mux.Handle("POST /v1/rest/sales/{ref}/release", h.authed(h.releaseSale))
	mux.Handle("POST /v1/rest/sales/{ref}/refund", h.authed(h.refundSale))
	mux.Handle("POST /v1/rest/sales/bulk-capture", h.authed(h.bulkCaptureSales))
	mux.Handle("GET /v1/rest/sales", h.authed(h.listSales))
	mux.Handle("GET /v1/rest/sales/{ref}", h.authed(h.getSale))
	mux.Handle("GET /v1/rest/sales/{ref}/email-status", h.authed(h.saleEmailStatus))
	// Issue #194: email delivery status view — account-scoped, paginated,
	// filterable by form/sale/status.
	mux.Handle("GET /v1/rest/email-deliveries", h.authed(h.listEmailDeliveries))
	// Issue #131: subscription management for merchants.
	mux.Handle("GET /v1/rest/subscriptions", h.authed(h.listSubscriptions))
	mux.Handle("POST /v1/rest/subscriptions/{id}/cancel", h.authed(h.cancelSubscription))
	// Issue #154: update the card on an existing subscription (re-tokenize).
	mux.Handle("POST /v1/rest/subscriptions/{id}/update-card", h.authed(h.updateSubscriptionCard))
	// round 13c: revive a past_due subscription so the engine retries it.
	mux.Handle("POST /v1/rest/subscriptions/{id}/retry", h.authed(h.retrySubscription))
	mux.Handle("GET /v1/rest/dashboard-bundle", h.authed(h.dashboardBundle))
	// Issue #387: super-admin platform-wide usage overview. Gated on the
	// super-admin role (#385); non-admins get 403. Read-only + audited.
	mux.Handle("GET /v1/admin/overview", h.authed(h.handleAdminOverview))
	// Issue #579: dead-letter visibility for the admin dashboard, mirroring the
	// admin_dead_letter_events / admin_requeue_dead_letter_event MCP tools.
	mux.Handle("GET /v1/admin/dead-letters", h.authed(h.handleAdminDeadLetters))
	mux.Handle("POST /v1/admin/dead-letters/requeue", h.authed(h.handleAdminRequeueDeadLetter))
	// Card t_94e5cca3: the phase-1 dormancy report (read-only; ?format=csv for the
	// operator export). Mirrors the admin_dormancy_report MCP tool.
	mux.Handle("GET /v1/admin/dormancy", h.authed(h.handleAdminDormancy))
	// Issue #601: super-admin suppression list — list/search, manual add, and
	// remove so mail to an address resumes. Mirrors the admin_list_suppressions
	// / admin_add_suppression / admin_remove_suppression MCP tools.
	mux.Handle("GET /v1/admin/suppressions", h.authed(h.handleAdminListSuppressions))
	mux.Handle("POST /v1/admin/suppressions", h.authed(h.handleAdminAddSuppression))
	mux.Handle("DELETE /v1/admin/suppressions/{email}", h.authed(h.handleAdminRemoveSuppression))
	// In-app feedback: the user's own threads (system-owned form, so these are
	// NOT account-scoped) and the super-admin triage surface. Mirrors the
	// list_feedback / get_feedback_thread / reply_feedback / set_feedback_status
	// MCP tools.
	mux.Handle("GET /v1/rest/feedback", h.authed(h.handleListFeedback))
	mux.Handle("POST /v1/rest/feedback", h.authed(h.handleCreateFeedback))
	mux.Handle("GET /v1/rest/feedback/{id}", h.authed(h.handleGetFeedback))
	mux.Handle("POST /v1/rest/feedback/{id}/messages", h.authed(h.handleReplyFeedback))
	mux.Handle("GET /v1/admin/feedback", h.authed(h.handleAdminListFeedback))
	mux.Handle("GET /v1/admin/feedback/{id}", h.authed(h.handleAdminGetFeedback))
	mux.Handle("POST /v1/admin/feedback/{id}/messages", h.authed(h.handleAdminReplyFeedback))
	mux.Handle("POST /v1/admin/feedback/{id}/status", h.authed(h.handleAdminSetFeedbackStatus))
	// The release path for a feedback notification that failed: the marker on
	// the thread is only useful if an operator can act on it (the #547/#579
	// visible-state-plus-a-way-to-act pair). Mirrors the retry_feedback_notify
	// MCP tool.
	mux.Handle("POST /v1/admin/feedback/{id}/notify", h.authed(h.handleAdminRetryFeedbackNotify))
	// In-app chat (phase 1): answers questions about the caller's own data and
	// can file feedback — nothing else. Session-only and per-user rate limited;
	// its tool calls run under a READONLY identity (rest_chat.go), so no sentence
	// typed into the box can reach a write.
	mux.Handle("GET /v1/rest/chat", h.authed(h.handleChatStatus))
	mux.Handle("POST /v1/rest/chat", h.authed(h.handleChat))
	// In-app chat (phase 2): the confirmation lane. A proposal the model made is
	// executed ONLY here, and only with the opaque single-use token the browser
	// holds — the model never sees it. Session-only, per-user limited, and audited.
	mux.Handle("POST /v1/rest/chat/confirm", h.authed(h.handleChatConfirm))
	mux.Handle("POST /v1/rest/chat/cancel", h.authed(h.handleChatCancel))
	// Chat history: the conversations a person has had with the assistant,
	// stored server-side so a logout, a new device or a reload does not destroy
	// them. Same lane rules as the message routes — session-only and
	// workspace-scoped — and every read is scoped to (user, workspace) in the
	// store, so one person's conversation is not another's, nor another
	// workspace's.
	mux.Handle("GET /v1/rest/chat/sessions", h.authed(h.handleChatSessionsList))
	mux.Handle("POST /v1/rest/chat/sessions", h.authed(h.handleChatSessionsCreate))
	mux.Handle("GET /v1/rest/chat/sessions/{id}", h.authed(h.handleChatSessionGet))
	mux.Handle("PATCH /v1/rest/chat/sessions/{id}", h.authed(h.handleChatSessionPatch))
	mux.Handle("DELETE /v1/rest/chat/sessions/{id}", h.authed(h.handleChatSessionDelete))
	mux.Handle("GET /v1/rest/analytics/forms", h.authed(h.formAnalytics))
	mux.Handle("GET /v1/rest/analytics/products", h.authed(h.productAnalytics))
	// Issue #92: event-tracking read endpoints (audit log + behaviour analytics).
	mux.Handle("GET /v1/rest/events", h.authed(h.listEvents))
	mux.Handle("GET /v1/rest/events/count", h.authed(h.countEvents))
	mux.Handle("GET /v1/rest/events/breakdown", h.authed(h.breakdownEvents))
	// Issue #447: realtime notifications — an authenticated SSE stream that
	// pushes sale.paid / sale.failed / response.submitted / subscription.charged
	// events to the merchant's connected clients (account-scoped, no
	// cross-tenant leak). The Sales page subscribes via EventSource.
	mux.Handle("GET /v1/rest/events/stream", h.authed(h.handleEventsStream))
	mux.Handle("GET /v1/rest/sales/export.csv", h.authed(h.exportSalesCSV))
	// Issue #245: AI spam review queue — spam-flagged submissions/sales the
	// merchant can un-spam (false positive). A spam-flagged sale that gets a
	// successful payment is auto-un-spammed by the payment callback.
	mux.Handle("GET /v1/rest/spam/submissions", h.authed(h.listSpamSubmissions))
	mux.Handle("POST /v1/rest/spam/submissions/{id}/unspam", h.authed(h.unspamSubmission))
	mux.Handle("GET /v1/rest/spam/sales", h.authed(h.listSpamSales))
	mux.Handle("POST /v1/rest/spam/sales/{ref}/unspam", h.authed(h.unspamSale))
	// Issue #61: image-block direct upload. Rate-limited (owner decision)
	// to curb abuse; auth is account-scoped via h.authed.
	mux.Handle("POST /v1/rest/uploads/image",
		middleware.RateLimitWith(uploadRatePerSecond, uploadBurst)(h.authed(h.uploadImage)))
	// Issue #133: avatar upload — downscales to a small profile image.
	mux.Handle("POST /v1/rest/uploads/avatar",
		middleware.RateLimitWith(uploadRatePerSecond, uploadBurst)(h.authed(h.uploadAvatar)))
	// Issue #361: merchant code-asset upload (CSS/JS/HTML/SVG/woff2) for
	// customising a landing/workspace page. Stored tenant-scoped and served as
	// hardened static content on the workspace/custom domain. Customer uploads
	// stay images-only; these are merchant-only.
	mux.Handle("POST /v1/rest/uploads/asset",
		middleware.RateLimitWith(uploadRatePerSecond, uploadBurst)(h.authed(h.uploadAsset)))
	// Issue #133: set/clear the session user's avatar_url. Validated to be a
	// JomForm-hosted media URL (from the upload bucket) to avoid storing
	// arbitrary external URLs.
	mux.Handle("PUT /v1/rest/profile/avatar", h.authed(h.updateProfileAvatar))
	// Issue #145: public media route — streams uploaded objects from the
	// media bucket to browsers. Unauthenticated (public media); the key is
	// validated to a safe `u/...` path (no traversal).
	mux.HandleFunc("GET /media/{key...}", h.handleMedia)
	// Issue #180: media library — list the merchant's uploaded media
	// (account-scoped, paginated, filterable by form/source) and soft-delete
	// one (also removes the object from MinIO).
	mux.Handle("GET /v1/rest/media", h.authed(h.listMedia))
	// Tong sampah (media): the bin read, then restore/purge. Same shape as the
	// products and forms bins. The bytes stop being deleted at soft-delete time
	// (see deleteMedia) and are removed by the purge instead.
	mux.Handle("GET /v1/rest/media/trash", h.authed(h.listTrashedMedia))
	mux.Handle("DELETE /v1/rest/media/{id}", h.authed(h.deleteMedia))
	mux.Handle("POST /v1/rest/media/{id}/restore", h.authed(h.restoreMedia))
	mux.Handle("DELETE /v1/rest/media/{id}/purge", h.authed(h.purgeMedia))
}

// Resolve identifies the caller: Bearer API key (preferred) or session
// cookie + default account (owner's first account). It returns the resolved
// account plus the credential context (an API key's scopes — issue #645).
func (h *RESTHandlers) resolve(w http.ResponseWriter, r *http.Request) (*AccountCtx, bool) {
	// 1. Bearer API key
	if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
		raw := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if raw == "" {
			return nil, false
		}
		key, err := h.Keys.LookupByKeyHash(r.Context(), auth.HashAPIKey(raw))
		if err != nil {
			return nil, false
		}
		acc, err := h.Accounts.ByID(r.Context(), key.AccountID)
		if err != nil {
			return nil, false
		}
		go h.Keys.Touch(context.Background(), key.ID)
		// Issue #645: carry the key's scopes so the write gate can enforce
		// them. Without this the scope was read from nowhere on REST and a
		// readonly key could write every account endpoint.
		return &AccountCtx{
			Account: acc, ViaAPIKey: true,
			KeyScopes: key.Scopes, KeyID: key.ID,
		}, true
	}

	// 2. Session cookie
	c, err := r.Cookie(sessionCookie)
	if err != nil || h.Sessions == nil {
		return nil, false
	}
	userID, issued, err := h.Sessions.GetCreated(r.Context(), c.Value)
	if err != nil {
		return nil, false
	}
	// Password reset invalidation (issue #62): if the owner reset their
	// password after this session was issued, the session is dead.
	if u, e := h.Users.ByID(r.Context(), userID); e == nil && u != nil && u.PwResetAt != nil && !auth.ValidAfter(issued, *u.PwResetAt) {
		_ = h.Sessions.Destroy(r.Context(), c.Value)
		invalidateSessionAcctCache(c.Value)
		return nil, false
	}
	// Resolve the account. Issue #158: when a ?workspace=<slug> scope is present
	// and belongs to the session user, resolve the account from that workspace
	// (each workspace owns its own account) instead of the user's first account.
	// This is what makes switching workspaces actually switch the account context
	// — CHIP keys, domains, webhooks, products, revenue all follow the workspace.
	// Without a workspace scope we fall back to the first owned account (legacy
	// unscoped callers / API-key path).
	acc := h.resolveSessionAccount(r, userID, c.Value)
	if acc == nil {
		// Cache held IDs but none fetched: stale mapping (account deleted or
		// owner listing would be empty). Drop the entry so the next request
		// re-resolves via ByOwner, then reject.
		invalidateSessionAcctCache(c.Value)
		return nil, false
	}
	// A session caller has no API key, so no scope restriction applies
	// (ViaAPIKey false): the session user's own role remains the write gate.
	return &AccountCtx{Account: acc}, true
}

// resolveSessionAccount resolves the account for a session-authenticated
// request. When the request carries a ?workspace=<slug> scope that belongs to the
// session user (owner OR member, issue #162), the account is resolved from
// that workspace (issue #158) so all account-level data is workspace-scoped.
// Otherwise the user's first owned account is used (legacy unscoped callers).
func (h *RESTHandlers) resolveSessionAccount(r *http.Request, userID, token string) *dbstore.Account {
	// Workspace-scoped resolution (issue #158): each workspace owns its own account.
	if slug := r.URL.Query().Get("workspace"); slug != "" && h.Workspaces != nil {
		acc, ok, err := h.Workspaces.AccessBySlug(r.Context(), slug, userID)
		if err == nil && ok {
			for _, id := range acc.Accounts {
				if a, err := h.Accounts.ByID(r.Context(), id); err == nil {
					return a
				}
			}
			// Accessible workspace with zero accounts (defensive; shouldn't happen
			// after the migration): fall back to the first owned account.
			// The workspace is empty so nothing leaks.
			accs, err := h.Accounts.ByOwner(r.Context(), userID)
			if err == nil && len(accs) > 0 {
				return accs[0]
			}
			return nil
		}
		// Workspace not accessible / unknown: fall back to the first account the
		// user can access (owner OR member, issue #162) so authed() succeeds;
		// the list/settings endpoint's scopeOr403 then returns 403 for the
		// foreign workspace. Returning nil here would surface a 401 instead of the
		// intended 403.
		if accs, err := h.Accounts.ByOwner(r.Context(), userID); err == nil && len(accs) > 0 {
			return accs[0]
		}
		if h.Workspaces != nil {
			if members, err := h.Workspaces.ByMember(r.Context(), userID); err == nil {
				for _, m := range members {
					for _, id := range m.Accounts {
						if a, err := h.Accounts.ByID(r.Context(), id); err == nil {
							return a
						}
					}
				}
			}
		}
		return nil
	}
	// Unscoped: first owned account (existing behaviour). Account IDs are
	// TTL-cached per token, so repeat requests skip the Accounts.ByOwner
	// Postgres listing; a hit still re-fetches the account by primary key
	// (one cheap query) so account/CHIP-credential changes are never stale.
	if ids, ok := cachedSessionAcctIDs(token); ok {
		for _, id := range ids {
			if a, err := h.Accounts.ByID(r.Context(), id); err == nil {
				return a
			}
		}
		return nil
	}
	accs, err := h.Accounts.ByOwner(r.Context(), userID)
	if err != nil || len(accs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(accs))
	for _, a := range accs {
		ids = append(ids, a.ID)
	}
	storeSessionAcctIDs(token, ids)
	return accs[0]
}

// AccountCtx carries the resolved account through the request, plus — when the
// request was authenticated with a Bearer API key — that key's scopes.
//
// The scopes ride on the context because requireWriteRole needs BOTH axes
// (scope and workspace role) and scope is a property of the credential, not of
// the account or of the ?workspace= scope. Before this, resolve() read
// key.AccountID and never key.Scopes, so the scope was not enforced on ANY REST
// write path (issue #645). A session-authenticated request has no key and
// therefore no scope restriction (ViaAPIKey false).
type AccountCtx struct {
	Account *dbstore.Account
	// ViaAPIKey is true when the request authenticated with a Bearer API key
	// (not a session cookie). Scope is enforced only on this path.
	ViaAPIKey bool
	// KeyScopes holds the authenticating API key's scopes (nil for sessions).
	KeyScopes []string
	// KeyID is the authenticating key's id, for audit/self-revoke checks.
	KeyID string
}

type acctKey struct{}

// AccountFrom returns the resolved account for this request.
func AccountFrom(ctx context.Context) *AccountCtx {
	if v, ok := ctx.Value(acctRequestKey{}).(*AccountCtx); ok {
		return v
	}
	return nil
}

type acctRequestKey struct{}

// authed wraps a handler with auth + account resolution.
func (h *RESTHandlers) authed(next func(http.ResponseWriter, *http.Request, *AccountCtx)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actx, ok := h.resolve(w, r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "authentication required")
			return
		}
		ctx := context.WithValue(r.Context(), acctRequestKey{}, actx)
		ctx = middleware.WithAccountID(ctx, actx.Account.ID)
		next(w, r.WithContext(ctx), actx)
	})
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	return dec.Decode(v)
}

func respond(w http.ResponseWriter, code int, v any) { writeJSON(w, code, v) }

// sessionOwnerID resolves the current session's user id ("" when anon).
func (h *RESTHandlers) sessionOwnerID(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" || h.Sessions == nil {
		return ""
	}
	uid, issued, gerr := h.Sessions.GetCreated(r.Context(), c.Value)
	if gerr != nil {
		return ""
	}
	// Password reset invalidation (issue #62).
	if u, e := h.Users.ByID(r.Context(), uid); e == nil && u != nil && u.PwResetAt != nil && !auth.ValidAfter(issued, *u.PwResetAt) {
		_ = h.Sessions.Destroy(r.Context(), c.Value)
		return ""
	}
	return uid
}

// profileUser loads the current session user (nil when anonymous).
func (h *RESTHandlers) profileUser(r *http.Request) (*dbstore.User, error) {
	uid := h.sessionOwnerID(r)
	if uid == "" {
		return nil, nil
	}
	return h.Users.ByID(r.Context(), uid)
}

// startSessionFor issues a new session cookie for a user id. Passkey/social
// logins issue a 14-day persistent (remember) session, so the persisted
// remember flag is set to true — a later 2FA-enable rotation (#69) will then
// inherit the 14d persistence instead of silently downgrading to 24h.
//
// It also applies the legacy-onboarding rule before the cookie is written, for
// the same reason AuthHandlers.startSession does: this is the other function on
// the code path that turns a request into a logged-in user, and the passkey
// login reaches the session through here rather than through startSession. See
// provisionLegacyWorkspace.
func (h *RESTHandlers) startSessionFor(w http.ResponseWriter, r *http.Request, userID string) error {
	provisionLegacyWorkspace(r.Context(), h.Workspaces, userID)

	ttl := rememberLongTTL
	maxAge := int(rememberLongTTL.Seconds())
	token, err := h.Sessions.CreateRememberedWithTTL(r.Context(), userID, ttl, true)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		Domain:   h.CookieDomain,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
	return nil
}

func base64URL(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func base64URLDecode(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// passkeyLookupUser finds a user by id for discoverable login.
func (h *RESTHandlers) passkeyLookupUser(ctx context.Context, userID string) (*dbstore.PasskeyUser, error) {
	u, err := h.Users.ByID(ctx, userID)
	if err != nil || u == nil {
		return nil, err
	}
	return h.Passkeys.UserFor(ctx, userID, u.Email)
}
