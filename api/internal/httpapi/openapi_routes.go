// Public REST API contract — the machine-readable OpenAPI 3.1 spec for
// /v1/rest/*.
//
// WHY THIS FILE EXISTS
//
//	  /v1/rest/* had 108 working routes and no description of them anywhere. A
//	  merchant could not start, and a third-party developer had nothing to build
//	  against. Worse, a hand-written endpoint list would be a lie within a week —
//	  this repo already lost a docs card to prose that drifted from the code
//	  (t_d768342e).
//
//	  So the list below is not documentation: it is the CONTRACT, and it is pinned
//	  to the code by openapi_parity_test.go. That test parses the real
//	  RegisterRoutes table in rest.go and fails when the two disagree in either
//	  direction — a route with no contract entry, or a contract entry for a route
//	  that does not exist. It also validates the generated document with a real
//	  OpenAPI 3.1 validator, so an invalid spec cannot be published.
//
//	  Every field here is a claim about the code, so every field is checkable:
//
//		Method/Path/Handler  parsed out of rest.go (the parity test)
//		Access               the gate the handler actually calls (requireWriteRole /
//		                     requireAdminRole / session-only), also asserted from the
//		                     handler source
//		Rate                 whether the route is throttled, and by what — asserted
//		                     against the middleware wrap in rest.go and the limiter
//		                     call in the handler, BOTH directions. This matters
//		                     because the honest fact about this API is that almost
//		                     nothing is throttled, and that fact is exactly the one
//		                     that rots silently. Where the cap is a CONSTANT — the
//		                     chat lane (t_da26169c) and the four per-IP sentences
//		                     (t_9cdfa18e) — the sentence is rendered from it rather
//		                     than typed: a literal there is a copy of the number
//		                     that no test can tell from the truth. The per-IP
//		                     sentences are additionally pinned to the numbers their
//		                     own rest.go registration passes, by a guard that reads
//		                     those call sites.
//
//	  Regenerate the published artifacts with:
//
//		go run ./cmd/openapi-gen
//
//	  which rewrites api/openapi/openapi.json (the published contract),
//	  web/public/openapi.json (the same bytes, served to any visitor at
//	  https://jomform.com/openapi.json — the repository is private, so the
//	  github.com copy is unreadable for the third-party developer this contract
//	  exists for) and web/src/lib/openapiSpec.gen.ts (the same document, for the
//	  /docs/api page to render). The parity test re-runs the generator in memory
//	  and fails if any of them differs from the code or from the others.
package httpapi

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Access levels a /v1/rest/* route can require. These are the gates the handlers
// actually apply, not a suggestion:
//
//	AccessRead     authenticated (API key OR session); any scope.
//	AccessWrite    a readwrite API key (readonly keys get 403) and, for a
//	               ?workspace= scope, manage-or-admin role.
//	AccessAdmin    as AccessWrite, plus the admin role in the scoped workspace.
//	               Used by the CHIP credential and per-form CHIP feed routes.
//	AccessSession  session cookie only. An API key is refused with 401 because
//	               these routes act on a PERSON (a chat conversation, a feedback
//	               thread, your own avatar), and a key carries no person.
const (
	AccessRead    = "read"
	AccessWrite   = "write"
	AccessAdmin   = "admin"
	AccessSession = "session"
)

// Rate-source values, so the test can tell where a limit is enforced.
const (
	RateMiddleware = "middleware" // middleware.RateLimit / RateLimitWith, keyed per IP
	RateHandler    = "handler"    // an in-handler limiter, keyed per user
)

// restRouteDoc is one operation's contract.
type restRouteDoc struct {
	Method  string // GET POST PUT PATCH DELETE
	Path    string // /v1/rest/... with {} placeholders, exactly as rest.go declares it
	Handler string // the RESTHandlers method rest.go wires (doubles as operationId)
	Tag     string
	Summary string
	// Access is the strongest gate the handler applies.
	Access string
	// Body reports whether the route reads a JSON request body.
	Body bool
	// Req/Resp name components declared in restComponentSchemas. Resp is a
	// component name, or "" for a body this file only describes in prose.
	Req  string
	Resp string
	// RespContentType is the media type of the success body when it is NOT
	// application/json (a CSV export, a printable HTML document). Empty means
	// application/json — the default this table assumes, so every route that
	// does not say otherwise keeps exactly the entry it had.
	RespContentType string
	// RespDesc is what the 200/201 body actually is, in plain words, for the
	// routes whose payload is too shape-shifty to pin as one component
	// (analytics rollups, the dashboard bundle, CSV).
	RespDesc string
	// Rate is "" when the route is NOT throttled. This is a stated fact of the
	// API, and it is the majority case: an unthrottled route is documented as
	// unthrottled rather than promised a quota that does not exist.
	Rate string
	// RateSource is RateMiddleware / RateHandler. Empty iff Rate is empty.
	RateSource string
	// Desc carries the honest specifics a caller needs and cannot infer:
	// "the raw key is returned exactly once", "409 + requires_ack when a
	// payment form has no products", "this is an SSE stream".
	Desc string
	// ScopeOnly reports that Access=write is applied WITHOUT the ?workspace=
	// role axis: the handler calls requireWriteScope directly and never
	// consults scopeOr403/requireWriteRole (card t_4abd052a).
	//
	// It exists because the docs page's LEGEND is a definition of the LEVEL,
	// rendered once and applied to every write-labelled route — so it promised
	// "manage/admin role inside a ?workspace= scope" for the one route that
	// does not apply that axis. The route's own Desc said so honestly, but a
	// reader who met the legend got the wrong contract. A per-route caveat
	// derived from the contract is the fix; the alternative (adding the role
	// axis to the handler) changes a shipped route's behaviour and was
	// explicitly rejected on the parent card.
	ScopeOnly bool
}

// chatMessageRatePolicy and chatConfirmRatePolicy render the contract's Rate
// sentence from the constants that ENFORCE the cap (rest_chat.go), the same way
// chatLimits() reports them to the client.
//
// WHY BUILT, NOT WRITTEN OUT. A literal here is a copy of the cap that nothing
// can tell from the truth: it stays at 60 while rest_chat.go moves to 90, every
// artifact stays internally consistent, and the published contract tells the
// developer reading it a number the server does not apply (t_da26169c). Built
// from the constant, the same edit moves the document — which is what makes
// TestGeneratedArtifactsAreInSync fail on an artifact that was not regenerated,
// and TestChatRatePoliciesQuoteTheEnforcedCaps fail on a sentence that quotes a
// number the handler does not enforce.
//
// %g, not %.0f: the cap is quoted exactly as written, so a half-hour value is
// visible instead of being rounded to a number the limiter never applies.
func chatMessageRatePolicy() string {
	return fmt.Sprintf("%g messages per hour per user, burst %g", chatRatePerHour, chatBurst)
}

// chatConfirmRatePolicy is the confirm/cancel bucket's sentence — one string for
// both routes because /chat/cancel shares confirm's bucket (see rest_chat.go's
// handleChatCancel), so a caller reading either operation is told the same thing.
func chatConfirmRatePolicy() string {
	return fmt.Sprintf("%g confirmations per hour per user, burst %g", chatConfirmPerHour, chatConfirmBurst)
}

// aiGenerateRatePolicy and uploadRatePolicy are the same treatment for the
// contract's PER-IP sentences: the numbers come from the constants rest.go's
// middleware.RateLimitWith call sites pass (aiGenerateRatePerHour/aiGenerateBurst,
// uploadRatePerSecond/uploadBurst), because those call sites are the thing that
// enforces them.
//
// WHY BUILT, NOT WRITTEN OUT. These four sentences were typed copies of the call
// sites until t_9cdfa18e: moving a registration (RateLimitWith(2, 6) -> 9, 6)
// left the published contract and /docs/api promising 2/s to the caller, and
// TestOpenAPIRateLimitsMatchTheCode only asserted that the route carried a
// middleware.RateLimit WRAP, never the numbers it was given. Built from the
// constant, that edit moves the document, a stale artifact is caught by
// TestGeneratedArtifactsAreInSync, and the sentence itself is checked against
// the numbers its own registration passes by
// TestMiddlewareRatePoliciesQuoteTheRegisteredCaps.
//
// The unit is the unit the constant is named in: aiGenerateRatePerHour is quoted
// per hour while the limiter is handed aiGenerateRatePerHour/3600.0 per second.
func aiGenerateRatePolicy() string {
	return fmt.Sprintf("%g per hour per IP, burst %g", aiGenerateRatePerHour, aiGenerateBurst)
}

func uploadRatePolicy() string {
	return fmt.Sprintf("%g per second per IP, burst %g", uploadRatePerSecond, uploadBurst)
}

// restRouteDocs is the contract for every /v1/rest/* route.
//
// Order follows rest.go's RegisterRoutes so a reader can diff the two by eye;
// the generator sorts by path and method so the published document is stable.
var restRouteDocs = []restRouteDoc{
	// ── Products ─────────────────────────────────────────────────────────────
	{Method: "GET", Path: "/v1/rest/products", Handler: "listProducts", Tag: "Products",
		Summary: "List the workspace's products", Access: AccessRead, Resp: "ProductList",
		RespDesc: "The workspace's live products, each with its variations."},
	{Method: "POST", Path: "/v1/rest/products", Handler: "createProduct", Tag: "Products",
		Summary: "Create a product", Access: AccessWrite, Body: true, Req: "ProductWrite", Resp: "Product",
		Desc: "Money is integer sen. price_sen is required and may not be negative; a missing or null price_sen is refused rather than stored as 0. For a subscription set type=subscription and billing_period=month|year. Image URLs must be https://."},
	{Method: "POST", Path: "/v1/rest/products/bulk", Handler: "bulkProducts", Tag: "Products",
		Summary: "Archive many products at once", Access: AccessWrite, Body: true, Resp: "BulkResult",
		Desc: "Action-based bulk archive (active=false). Up to 500 ids per call."},
	{Method: "PUT", Path: "/v1/rest/products/{id}", Handler: "updateProduct", Tag: "Products",
		Summary: "Update a product", Access: AccessWrite, Body: true, Req: "ProductWrite", Resp: "Product",
		Desc: "Partial update: omitted fields keep their current value. variations is replaced wholesale — send the full set, or [] to turn a variable product back into a simple one."},
	{Method: "GET", Path: "/v1/rest/products/{id}", Handler: "getProduct", Tag: "Products",
		Summary: "Read one product", Access: AccessRead, Resp: "Product"},
	{Method: "DELETE", Path: "/v1/rest/products/{id}", Handler: "deleteProduct", Tag: "Products",
		Summary: "Move a product to the trash", Access: AccessWrite, Resp: "Deleted",
		Desc: "This moves the product to the bin — it is not the same as taking it off sale. To take a product off sale and leave it in the catalogue, PUT {\"active\":false} instead; a trashed product is invisible to every other read until it is restored."},
	{Method: "GET", Path: "/v1/rest/products/trash", Handler: "listTrashedProducts", Tag: "Products",
		Summary: "List the products in the trash", Access: AccessRead, RespDesc: "The workspace's trashed products, each with its sales_count, subscription_count and can_purge, so a caller can tell a restorable product from one that can never be destroyed.",
		Desc: "Each row also carries deleted_at (RFC3339). can_purge is false when the product has a sale or a subscription, which is exactly what DELETE .../purge would refuse with 409."},
	{Method: "POST", Path: "/v1/rest/products/{id}/restore", Handler: "restoreProduct", Tag: "Products",
		Summary: "Restore a product from the trash", Access: AccessWrite, RespDesc: "status=restored and the product id.",
		Desc: "Restoring touches no CASCADE and destroys nothing. Unlike a form restore there is no slug to collide with — products have no unique name — so this only fails when the product is not in the trash (404)."},
	{Method: "DELETE", Path: "/v1/rest/products/{id}/purge", Handler: "purgeProduct", Tag: "Products",
		Summary: "Destroy a trashed product for good", Access: AccessWrite, Body: true, RespDesc: "status=purged, the product id and its name.",
		Desc: "Only a product already in the trash can be destroyed; a live product is a 404. The body must repeat the product's own name as confirm_name; a mismatch is refused with 400. A product that has ever been sold is refused with 409 and sales_count (sales history is never destroyed), and one a subscription still points at is refused with 409 and subscription_count."},

	// ── Forms ────────────────────────────────────────────────────────────────
	{Method: "GET", Path: "/v1/rest/forms", Handler: "listForms", Tag: "Forms",
		Summary: "List forms", Access: AccessRead, Resp: "FormList",
		Desc: "Every form with its slug, status, public URL and mode. mode is payment (default) or response."},
	{Method: "POST", Path: "/v1/rest/forms", Handler: "createForm", Tag: "Forms",
		Summary: "Create a form", Access: AccessWrite, Body: true, Req: "FormWrite", Resp: "Form",
		Desc: "A blank slug is generated from the title and uniquified within the workspace. An explicit slug is used as given, and must be unique among the workspace's live forms: a slug another live form already serves is refused with 409 form.slugInUse (the same code and sentence a slug rename onto it answers). The form is created as a draft; publish it with POST /v1/rest/forms/{id}/publish."},
	{Method: "POST", Path: "/v1/rest/forms/ai-generate", Handler: "aiGenerateForm", Tag: "Forms",
		Summary: "Draft a form config from a prompt (AI)", Access: AccessWrite, Body: true, Resp: "Form",
		RespDesc:   "A draft form config (blocks + theme) to review and then save.",
		Rate:       aiGenerateRatePolicy(),
		RateSource: RateMiddleware,
		Desc:       "This route calls a language model and is charged accordingly, which is why it — and only it among the form routes — is throttled."},
	{Method: "POST", Path: "/v1/rest/forms/bulk", Handler: "bulkForms", Tag: "Forms",
		Summary: "Publish, draft or delete many forms at once", Access: AccessWrite, Body: true, Resp: "BulkResult",
		Desc: "Up to 500 ids. A payment form with no products is skipped on publish and reported in needs_products rather than being published empty. A draft or delete also drops the cached social card of every form that was serving one (see the status route's edge fields)."},
	{Method: "GET", Path: "/v1/rest/forms/{id}", Handler: "getForm", Tag: "Forms",
		Summary: "Read one form", Access: AccessRead, Resp: "Form",
		Desc: "Includes the current config (blocks) and theme, so you can read, mutate the parts you want and send the whole config back."},
	{Method: "PATCH", Path: "/v1/rest/forms/{id}", Handler: "updateForm", Tag: "Forms",
		Summary: "Update a form's title, config, theme or slug", Access: AccessWrite, Body: true, Req: "FormWrite", Resp: "Form",
		Desc: "Partial update: an omitted title, config, theme or slug keeps the stored value, so a body that renames the form does not have to resend its title. An empty title (or slug) also means \"leave unchanged\" — this route never blanks a form's title. A slug another live form already serves is refused with 409 form.slugInUse, and nothing else in the same request is written; a malformed slug is 400 form.slugInvalid. A rename leaves a temporary 301 from the old URL."},
	{Method: "DELETE", Path: "/v1/rest/forms/{id}", Handler: "deleteForm", Tag: "Forms",
		Summary: "Delete a form (soft delete)", Access: AccessWrite, Resp: "Deleted",
		Desc: "The form goes to the trash and can be restored; its public page and its share card stop being served. A card the CDN had cached is dropped in the same request and the reply names the outcome in edge."},
	{Method: "GET", Path: "/v1/rest/forms/trash", Handler: "listTrashedForms", Tag: "Forms",
		Summary: "List the forms in the trash", Access: AccessRead, RespDesc: "The account's trashed forms, each with the number of sales attached to it, so a caller can tell a restorable form from one that can never be destroyed."},
	{Method: "POST", Path: "/v1/rest/forms/{id}/restore", Handler: "restoreForm", Tag: "Forms",
		Summary: "Restore a form from the trash", Access: AccessWrite, RespDesc: "status=restored and the form id.",
		Desc: "Restoring touches no CASCADE. If another form has taken the trashed form's slug in the meantime the restore is refused with 409 — change that form's slug first, then restore."},
	{Method: "DELETE", Path: "/v1/rest/forms/{id}/purge", Handler: "purgeForm", Tag: "Forms",
		Summary: "Destroy a trashed form for good", Access: AccessWrite, Body: true, RespDesc: "status=purged and the form id.",
		Desc: "Only a form already in the trash can be destroyed. The body must repeat the form's own slug as confirm_slug; a mismatch is refused with 400. A form that has ever been sold is refused with 409 and sales_count, because sales history is never destroyed. The destroyed form's share card is dropped from the CDN in the same request and the reply names the outcome in edge."},
	{Method: "POST", Path: "/v1/rest/forms/{id}/publish", Handler: "publishForm", Tag: "Forms",
		Summary: "Publish a form", Access: AccessWrite, Resp: "FormStatus", Body: true,
		Desc: "The form goes live at its URL. A payment form with no products is refused with 409 and requires_ack=true, because it could never take money; send {\"acknowledge\":true} or ?acknowledge=1 to publish it anyway."},
	{Method: "POST", Path: "/v1/rest/forms/{id}/status", Handler: "setFormStatus", Tag: "Forms",
		Summary: "Set a form's status (draft | private | published)", Access: AccessWrite, Body: true, Resp: "FormStatus",
		Desc: "status=private also sets an access password if one is supplied (hashed, never returned). Leaving a publicly served status (published or unlisted) drops the form's cached share card at the CDN in the SAME request: the reply then carries edge (purged | not_configured | out_of_zone | failed) and, when nothing was purged, edge_max_age_s, the freshness window that then bounds how long the old card can still be served. The status change itself never fails because the purge did."},
	{Method: "GET", Path: "/v1/rest/forms/{id}/submissions", Handler: "listSubmissions", Tag: "Submissions",
		Summary: "List a response form's submissions", Access: AccessRead, Resp: "Paged",
		Desc: "Response-mode forms store responses, not sales, so this is how you read them. Supports ?page= and ?limit= (max 100)."},
	{Method: "GET", Path: "/v1/rest/forms/{id}/submissions/count", Handler: "countSubmissions", Tag: "Submissions",
		Summary: "Count a response form's submissions", Access: AccessRead, Resp: "Count"},
	{Method: "PUT", Path: "/v1/rest/forms/{id}/tracking", Handler: "putFormTracking", Tag: "Form settings",
		Summary: "Set per-form analytics/tracking", Access: AccessWrite, Body: true},
	{Method: "PUT", Path: "/v1/rest/forms/{id}/settings", Handler: "putFormSettings", Tag: "Form settings",
		Summary: "Set per-form display settings", Access: AccessWrite, Body: true},
	{Method: "PUT", Path: "/v1/rest/forms/{id}/restrictions", Handler: "putFormRestrictions", Tag: "Form settings",
		Summary: "Set a form's open/close window and entry limit", Access: AccessWrite, Body: true},
	{Method: "GET", Path: "/v1/rest/forms/{id}/menu", Handler: "getFormMenu", Tag: "Form settings",
		Summary: "Read a form's restaurant-menu configuration", Access: AccessRead},
	{Method: "PUT", Path: "/v1/rest/forms/{id}/menu", Handler: "putFormMenu", Tag: "Form settings",
		Summary: "Set a form's restaurant-menu configuration", Access: AccessWrite, Body: true},
	{Method: "GET", Path: "/v1/rest/forms/{id}/email-settings", Handler: "getFormEmailSettings", Tag: "Form settings",
		Summary: "Read a form's email overrides", Access: AccessWrite,
		Desc: "Refused for a readonly key: this route is reached from the email settings editor and is gated as a write even though it only reads."},
	{Method: "PUT", Path: "/v1/rest/forms/{id}/email-settings", Handler: "saveFormEmailSettings", Tag: "Form settings",
		Summary: "Set a form's email overrides", Access: AccessWrite, Body: true},
	{Method: "DELETE", Path: "/v1/rest/forms/{id}/email-settings", Handler: "clearFormEmailOverrides", Tag: "Form settings",
		Summary: "Clear a form's email overrides (inherit the workspace again)", Access: AccessWrite},
	{Method: "GET", Path: "/v1/rest/forms/{id}/notifications", Handler: "listFormNotifications", Tag: "Form settings",
		Summary: "List a form's notification rules", Access: AccessWrite,
		Desc: "Gated as a write, like the email-settings read above."},
	{Method: "POST", Path: "/v1/rest/forms/{id}/notifications", Handler: "createFormNotification", Tag: "Form settings",
		Summary: "Create a notification rule", Access: AccessWrite, Body: true},
	{Method: "PUT", Path: "/v1/rest/forms/{id}/notifications/{nid}", Handler: "updateFormNotification", Tag: "Form settings",
		Summary: "Update a notification rule", Access: AccessWrite, Body: true},
	{Method: "DELETE", Path: "/v1/rest/forms/{id}/notifications/{nid}", Handler: "deleteFormNotification", Tag: "Form settings",
		Summary: "Delete a notification rule", Access: AccessWrite},
	{Method: "GET", Path: "/v1/rest/forms/{id}/confirmations", Handler: "getFormConfirmations", Tag: "Form settings",
		Summary: "Read a form's confirmations", Access: AccessRead},
	{Method: "PUT", Path: "/v1/rest/forms/{id}/confirmations", Handler: "saveFormConfirmations", Tag: "Form settings",
		Summary: "Set a form's confirmations", Access: AccessWrite, Body: true},
	{Method: "GET", Path: "/v1/rest/forms/{id}/qr-stand", Handler: "getFormQRStand", Tag: "Form settings",
		Summary: "Render a form's printable QR stand", Access: AccessRead,
		RespContentType: "text/html; charset=utf-8",
		RespDesc:        "An HTML document: the printable QR stand — the form's title, its public URL encoded as a QR image, and the ?table=N badge when a table was given.",
		Desc:            "Served for a form the caller owns at ANY status, including a draft: this is the merchant's own print artefact, and a form is usually printed before it goes live. It is what the dashboard's \"Print stand\" action opens. The public /qr/{slug}/stand route is the buyer-facing one and follows the page's status rule instead — a draft's stand answers 404 there, because the stand prints the form's title."},

	// ── Per-form CHIP feeds and credentials (admin) ──────────────────────────
	{Method: "GET", Path: "/v1/rest/forms/{id}/chip-feeds", Handler: "getFormChipFeeds", Tag: "Payments (CHIP)",
		Summary: "Read a form's CHIP feeds", Access: AccessRead},
	{Method: "PUT", Path: "/v1/rest/forms/{id}/chip-feeds", Handler: "setFormChipFeeds", Tag: "Payments (CHIP)",
		Summary: "Set a form's CHIP feeds", Access: AccessAdmin, Body: true,
		Desc: "Admin only: these feeds carry payment credentials."},
	{Method: "PUT", Path: "/v1/rest/forms/{id}/chip-override", Handler: "setFormChipOverride", Tag: "Payments (CHIP)",
		Summary: "Override a form's CHIP credentials", Access: AccessAdmin, Body: true},
	{Method: "DELETE", Path: "/v1/rest/forms/{id}/chip-override", Handler: "clearFormChipOverride", Tag: "Payments (CHIP)",
		Summary: "Clear a form's CHIP credential override", Access: AccessAdmin},
	{Method: "PUT", Path: "/v1/rest/settings/chip", Handler: "setChipCreds", Tag: "Payments (CHIP)",
		Summary: "Set the workspace's CHIP credentials", Access: AccessAdmin, Body: true},
	{Method: "GET", Path: "/v1/rest/settings/chip", Handler: "chipStatus", Tag: "Payments (CHIP)",
		Summary: "Read whether CHIP is configured", Access: AccessRead},
	{Method: "DELETE", Path: "/v1/rest/settings/chip", Handler: "deleteChipCreds", Tag: "Payments (CHIP)",
		Summary: "Clear the workspace's CHIP credentials", Access: AccessAdmin},

	// ── API keys ─────────────────────────────────────────────────────────────
	{Method: "GET", Path: "/v1/rest/keys", Handler: "listKeys", Tag: "API keys",
		Summary: "List API keys (never the secret)", Access: AccessRead, Resp: "KeyList"},
	{Method: "POST", Path: "/v1/rest/keys", Handler: "createKey", Tag: "API keys",
		Summary: "Create an API key", Access: AccessWrite, Body: true, Resp: "KeyCreated",
		Desc: "Returns the raw key exactly once — store it then. Scopes default to [\"readonly\"]; the supported values are readonly and readwrite. The same key authenticates MCP."},
	{Method: "DELETE", Path: "/v1/rest/keys/{id}", Handler: "revokeKey", Tag: "API keys",
		Summary: "Revoke an API key", Access: AccessWrite, Resp: "Deleted"},

	// ── Account summary and templates ────────────────────────────────────────
	{Method: "GET", Path: "/v1/rest/summary", Handler: "summary", Tag: "Account",
		Summary: "Workspace totals (sales, revenue, counts)", Access: AccessRead, Resp: "Summary"},
	{Method: "GET", Path: "/v1/rest/dashboard-bundle", Handler: "dashboardBundle", Tag: "Account",
		Summary: "One call for the whole dashboard payload", Access: AccessRead,
		RespDesc: "An object with me, workspaces, chip_status, keys, domains, webhooks, social, einvoice, products, summary, sales, form_analytics and product_analytics."},

	// ── Settings ─────────────────────────────────────────────────────────────
	{Method: "PUT", Path: "/v1/rest/settings/custom-domain", Handler: "setCustomDomain", Tag: "Settings",
		Summary: "Set the workspace's custom domain", Access: AccessWrite, Body: true},
	{Method: "GET", Path: "/v1/rest/settings/custom-domain", Handler: "getCustomDomain", Tag: "Settings",
		Summary: "Read the workspace's custom domain", Access: AccessRead},
	{Method: "DELETE", Path: "/v1/rest/settings/custom-domain", Handler: "deleteCustomDomain", Tag: "Settings",
		Summary: "Clear the workspace's custom domain", Access: AccessWrite},
	{Method: "PUT", Path: "/v1/rest/settings/einvoice", Handler: "putEinvoicing", Tag: "Settings",
		Summary: "Set e-Invoice (LHDN) settings", Access: AccessWrite, Body: true},
	{Method: "GET", Path: "/v1/rest/settings/einvoice", Handler: "getEinvoicing", Tag: "Settings",
		Summary: "Read e-Invoice (LHDN) settings", Access: AccessRead},
	{Method: "PUT", Path: "/v1/rest/settings/tracking", Handler: "putTracking", Tag: "Settings",
		Summary: "Set workspace tracking/analytics", Access: AccessWrite, Body: true},
	{Method: "GET", Path: "/v1/rest/settings/tracking", Handler: "getTracking", Tag: "Settings",
		Summary: "Read workspace tracking/analytics", Access: AccessRead},
	{Method: "GET", Path: "/v1/rest/settings/webhook-key", Handler: "getWebhookKey", Tag: "Settings",
		Summary: "Read the webhook signing key", Access: AccessWrite, ScopeOnly: true,
		Desc: "Refused for a readonly key: the account's Ed25519 keypair is generated on first use, so the FIRST call on a workspace writes and this read is gated as a write. Once a keypair exists the same readonly key is served — the gate is state-dependent, and Access states the strongest one the handler applies. The key scope is the only gate here: the handler does not read a ?workspace= role."},
	{Method: "GET", Path: "/v1/rest/currency/rates", Handler: "getCurrencyRates", Tag: "Settings",
		Summary: "Read cached exchange rates (BNM)", Access: AccessRead},

	// ── Shipping (managed zones + rates) ─────────────────────────────────────
	{Method: "GET", Path: "/v1/rest/shipping", Handler: "getShipping", Tag: "Shipping",
		Summary: "Read the workspace's shipping zone sets and rates", Access: AccessRead,
		Desc: "The managed rate source a form's shipping block uses when its source is \"zones\": zone sets, each zone's match rules (states / districts / postcode range) and its rate rows (price in integer sen, an optional weight band in grams and an optional free-shipping threshold). Scoped to the account the request resolved to (?workspace= resolves that workspace's own account)."},
	{Method: "PUT", Path: "/v1/rest/shipping", Handler: "putShipping", Tag: "Shipping",
		Summary: "Replace the workspace's shipping zone sets and rates", Access: AccessWrite, Body: true,
		Desc: "One transaction: a set sent with its id keeps it (so every form pointing at it keeps resolving), a new set is inserted, and a set the payload OMITS is deleted — refused with 409 when a form still points at that set. Every zone needs at least one rate and every non-fallback zone needs a match rule; a configuration that could not resolve is refused with 400 rather than saved and discovered by a buyer."},
	{Method: "GET", Path: "/v1/rest/shipping/couriers", Handler: "getShippingCouriers", Tag: "Shipping",
		Summary: "Read the workspace's courier connections", Access: AccessRead,
		Desc: "Every courier provider this deployment knows (lalamove, easyparcel) with: whether the SERVER holds usable credentials for it (never the value — only the source), whether THIS workspace has connected it, the pickup address a parcel ships from, the connection's environment (sandbox/live, so a simulated rate is visible as simulated), and what is still missing (credentials / connect / grant / origin) before a real quote can happen. A provider whose production keys do not exist yet is listed as not configured rather than hidden."},
	{Method: "POST", Path: "/v1/rest/shipping/couriers/{provider}/connect", Handler: "postShippingCourierConnect", Tag: "Shipping",
		Summary: "Connect a courier provider for the workspace", Access: AccessWrite, Body: true,
		Desc: "Lalamove authenticates server to server, so connecting means naming the PICKUP point (lat/lng) it will be quoted from and it is immediately ready. EasyParcel is OAuth 2.0 and its consent belongs to the MERCHANT: this returns an authorize URL (with a single-use, 30-minute state) for the account owner to open on any device. No password is ever accepted here. A refusal names its remedy: missing server credentials, a missing pickup point, or an unregistered redirect_uri."},
	{Method: "POST", Path: "/v1/rest/shipping/couriers/{provider}/disconnect", Handler: "postShippingCourierDisconnect", Tag: "Shipping",
		Summary: "Disconnect a courier provider, dropping its stored grant", Access: AccessWrite,
		Desc: "Marks the connection disconnected and deletes the sealed OAuth grant, so a revoked authorization can never be quoted with. The pickup address is KEPT: it is the merchant's configuration and reconnecting must not make them retype it."},

	// ── Webhooks ─────────────────────────────────────────────────────────────
	{Method: "GET", Path: "/v1/rest/webhooks", Handler: "listWebhooks", Tag: "Webhooks",
		Summary: "List outbound webhooks", Access: AccessRead, Resp: "WebhookList"},
	{Method: "POST", Path: "/v1/rest/webhooks", Handler: "createWebhook", Tag: "Webhooks",
		Summary: "Create an outbound webhook", Access: AccessWrite, Body: true, Resp: "Webhook"},
	{Method: "PUT", Path: "/v1/rest/webhooks/{id}", Handler: "updateWebhook", Tag: "Webhooks",
		Summary: "Update an outbound webhook", Access: AccessWrite, Body: true, Resp: "Webhook"},
	{Method: "GET", Path: "/v1/rest/webhooks/{id}/deliveries", Handler: "listWebhookDeliveries", Tag: "Webhooks",
		Summary: "List a webhook's recent deliveries", Access: AccessRead, Resp: "Paged"},
	{Method: "POST", Path: "/v1/rest/webhooks/{id}/deliveries/{deliveryId}/retry", Handler: "retryWebhookDelivery", Tag: "Webhooks",
		Summary: "Retry a failed delivery", Access: AccessWrite},
	{Method: "POST", Path: "/v1/rest/webhooks/{id}/retries/{retryId}/retry", Handler: "retryDeadLettered", Tag: "Webhooks",
		Summary: "Retry a dead-lettered delivery", Access: AccessWrite},
	{Method: "DELETE", Path: "/v1/rest/webhooks/{id}", Handler: "deleteWebhook", Tag: "Webhooks",
		Summary: "Delete an outbound webhook", Access: AccessWrite, Resp: "Deleted"},

	// ── Custom domains ───────────────────────────────────────────────────────
	{Method: "GET", Path: "/v1/rest/domains", Handler: "listCustomDomains", Tag: "Domains",
		Summary: "List custom domains", Access: AccessRead},
	{Method: "POST", Path: "/v1/rest/domains", Handler: "addCustomDomain", Tag: "Domains",
		Summary: "Add a custom domain", Access: AccessWrite, Body: true},
	{Method: "POST", Path: "/v1/rest/domains/{id}/verify", Handler: "verifyCustomDomainByID", Tag: "Domains",
		Summary: "Verify a custom domain's DNS", Access: AccessWrite},
	{Method: "POST", Path: "/v1/rest/domains/{id}/auto-cname", Handler: "autoCreateCNAMEForDomain", Tag: "Domains",
		Summary: "Create the CNAME through the linked Cloudflare account", Access: AccessWrite},
	{Method: "POST", Path: "/v1/rest/domains/{id}/tls-mode", Handler: "setDomainTLSMode", Tag: "Domains",
		Summary: "Set a domain's TLS mode", Access: AccessWrite, Body: true},
	{Method: "POST", Path: "/v1/rest/domains/{id}/tls-email", Handler: "setDomainTLSEmail", Tag: "Domains",
		Summary: "Set the ACME contact email for a domain", Access: AccessWrite, Body: true},
	{Method: "POST", Path: "/v1/rest/domains/{id}/custom-cert", Handler: "uploadCustomCert", Tag: "Domains",
		Summary: "Upload a custom TLS certificate", Access: AccessWrite, Body: true},
	{Method: "DELETE", Path: "/v1/rest/domains/{id}", Handler: "deleteCustomDomainByID", Tag: "Domains",
		Summary: "Delete a custom domain", Access: AccessWrite, Resp: "Deleted"},

	// ── Sales ────────────────────────────────────────────────────────────────
	{Method: "GET", Path: "/v1/rest/sales", Handler: "listSales", Tag: "Sales",
		Summary: "List sales and responses", Access: AccessRead, Resp: "Paged",
		Desc: "Without a form filter this is the whole record set: sales AND response-mode submissions, merged newest first. Filters: ?form_id= ?status= ?from= ?to= ?q= ?page= ?limit=. Omit ?page= for the legacy plain-array shape."},
	{Method: "GET", Path: "/v1/rest/sales/{ref}/email-status", Handler: "saleEmailStatus", Tag: "Sales",
		Summary: "Read a sale's receipt email timeline", Access: AccessRead},
	{Method: "GET", Path: "/v1/rest/sales/export.csv", Handler: "exportSalesCSV", Tag: "Sales",
		Summary: "Export sales/responses as CSV", Access: AccessRead,
		RespDesc: "A text/csv body with the same rows and filters as the list."},
	{Method: "POST", Path: "/v1/rest/sales/{ref}/confirm", Handler: "confirmSale", Tag: "Sales",
		Summary: "Confirm a sale (for example a bank transfer)", Access: AccessWrite},
	{Method: "POST", Path: "/v1/rest/sales/{ref}/capture", Handler: "captureSale", Tag: "Sales",
		Summary: "Capture a skip-capture authorisation", Access: AccessWrite, Body: true,
		Desc: "Omit amount_sen to capture the full authorisation; send it to capture part of it."},
	{Method: "POST", Path: "/v1/rest/sales/{ref}/release", Handler: "releaseSale", Tag: "Sales",
		Summary: "Release (void) a skip-capture authorisation", Access: AccessWrite},
	{Method: "POST", Path: "/v1/rest/sales/{ref}/refund", Handler: "refundSale", Tag: "Sales",
		Summary: "Refund a paid sale, in full or in part", Access: AccessWrite, Body: true,
		Desc: "A partial refund keeps the sale paid with refunded_sen set and outstanding_sen still refundable; only reaching the total marks it refunded. An amount that would over-refund is refused."},
	{Method: "POST", Path: "/v1/rest/sales/bulk-capture", Handler: "bulkCaptureSales", Tag: "Sales",
		Summary: "Capture many skip-capture authorisations", Access: AccessWrite, Body: true, Resp: "BulkResult"},
	{Method: "GET", Path: "/v1/rest/email-deliveries", Handler: "listEmailDeliveries", Tag: "Sales",
		Summary: "List email delivery events", Access: AccessRead, Resp: "Paged"},
	{Method: "GET", Path: "/v1/rest/subscriptions", Handler: "listSubscriptions", Tag: "Subscriptions",
		Summary: "List subscriptions", Access: AccessRead, Resp: "SubscriptionList"},
	{Method: "POST", Path: "/v1/rest/subscriptions/{id}/cancel", Handler: "cancelSubscription", Tag: "Subscriptions",
		Summary: "Cancel a subscription", Access: AccessWrite,
		Desc: "Cancelling does not revoke the customer's card token; the subscription can be revived later."},
	{Method: "POST", Path: "/v1/rest/subscriptions/{id}/update-card", Handler: "updateSubscriptionCard", Tag: "Subscriptions",
		Summary: "Re-tokenise a subscription's card", Access: AccessWrite, Body: true},
	{Method: "POST", Path: "/v1/rest/subscriptions/{id}/retry", Handler: "retrySubscription", Tag: "Subscriptions",
		Summary: "Retry a past-due subscription now", Access: AccessWrite},

	// ── Analytics, events, spam ──────────────────────────────────────────────
	{Method: "GET", Path: "/v1/rest/analytics/forms", Handler: "formAnalytics", Tag: "Analytics",
		Summary: "Per-form entry counts and money", Access: AccessRead, Resp: "FormStatsList",
		Desc: "Each row counts exactly one table — a response form's responses, or a payment form's sales — so the two are never summed."},
	{Method: "GET", Path: "/v1/rest/analytics/products", Handler: "productAnalytics", Tag: "Analytics",
		Summary: "Per-product sales totals", Access: AccessRead},
	{Method: "GET", Path: "/v1/rest/events", Handler: "listEvents", Tag: "Analytics",
		Summary: "List the account's audit/behaviour events", Access: AccessRead, Resp: "Paged"},
	{Method: "GET", Path: "/v1/rest/events/count", Handler: "countEvents", Tag: "Analytics",
		Summary: "Count events", Access: AccessRead, Resp: "Count"},
	{Method: "GET", Path: "/v1/rest/events/breakdown", Handler: "breakdownEvents", Tag: "Analytics",
		Summary: "Break events down by name", Access: AccessRead},
	{Method: "GET", Path: "/v1/rest/events/stream", Handler: "handleEventsStream", Tag: "Analytics",
		Summary: "Stream this workspace's events (SSE)", Access: AccessRead,
		RespDesc: "A text/event-stream of sale.paid, sale.failed, response.submitted and subscription.charged events for the authenticated account.",
		Desc:     "Server-sent events. The stream is account-scoped by the authenticating credential, so it can never carry another tenant's events."},
	{Method: "GET", Path: "/v1/rest/spam/submissions", Handler: "listSpamSubmissions", Tag: "Spam review",
		Summary: "List spam-flagged submissions", Access: AccessRead, Resp: "Paged"},
	{Method: "POST", Path: "/v1/rest/spam/submissions/{id}/unspam", Handler: "unspamSubmission", Tag: "Spam review",
		Summary: "Un-flag a submission (false positive)", Access: AccessWrite},
	{Method: "GET", Path: "/v1/rest/spam/sales", Handler: "listSpamSales", Tag: "Spam review",
		Summary: "List spam-flagged sales", Access: AccessRead, Resp: "Paged"},
	{Method: "POST", Path: "/v1/rest/spam/sales/{ref}/unspam", Handler: "unspamSale", Tag: "Spam review",
		Summary: "Un-flag a sale (false positive)", Access: AccessWrite},

	// ── Templates ────────────────────────────────────────────────────────────
	{Method: "GET", Path: "/v1/rest/templates", Handler: "listOwnTemplates", Tag: "Templates",
		Summary: "List the workspace's own templates", Access: AccessRead},
	{Method: "POST", Path: "/v1/rest/templates", Handler: "createOwnTemplate", Tag: "Templates",
		Summary: "Create a template", Access: AccessWrite, Body: true},
	{Method: "GET", Path: "/v1/rest/templates/{id}", Handler: "getOwnTemplate", Tag: "Templates",
		Summary: "Read a template", Access: AccessRead},
	{Method: "PUT", Path: "/v1/rest/templates/{id}", Handler: "updateOwnTemplate", Tag: "Templates",
		Summary: "Update a template", Access: AccessWrite, Body: true},
	{Method: "DELETE", Path: "/v1/rest/templates/{id}", Handler: "deleteOwnTemplate", Tag: "Templates",
		Summary: "Delete a template", Access: AccessWrite, Resp: "Deleted"},
	{Method: "POST", Path: "/v1/rest/templates/{id}/use", Handler: "useOwnTemplate", Tag: "Templates",
		Summary: "Clone a template into a new draft form", Access: AccessWrite, Resp: "Form"},

	// ── Media and uploads ────────────────────────────────────────────────────
	{Method: "GET", Path: "/v1/rest/media", Handler: "listMedia", Tag: "Media",
		Summary: "List uploaded media", Access: AccessRead, Resp: "MediaList",
		Desc: "Newest first and paginated (?page/?limit; the limit caps at 100), with optional ?form=<id> and ?source=merchant|customer filters. Pass ?url=<a media URL this workspace owns> instead of ?page to ask for that ONE row: the URL is resolved to its object key, so a row older than the newest 100 rows — or a URL whose advertised host has changed since it was stored — is still found. A URL this workspace does not own answers an EMPTY items list with 200, not 404: 'not in this library' is an ordinary answer, not a failed request."},
	{Method: "DELETE", Path: "/v1/rest/media/{id}", Handler: "deleteMedia", Tag: "Media",
		Summary: "Move a media item to the trash", Access: AccessWrite, Resp: "Deleted",
		Desc: "This marks the row trashed. The object itself is NOT removed from storage and the bytes keep counting against the workspace's 1GB quota: the bin holds real storage, so it is not a free tier. Destroy the object for good with DELETE /v1/rest/media/{id}/purge, which is also where the quota is released."},
	{Method: "GET", Path: "/v1/rest/media/trash", Handler: "listTrashedMedia", Tag: "Media",
		Summary: "List the media in the trash", Access: AccessRead,
		RespDesc: "The workspace's trashed media, each with deleted_at (RFC3339) and object_present.",
		Desc:     "object_present reports whether the bytes are still in the store. A row can be in the bin while the object is already gone (removed by an operator or a bucket lifecycle rule), which is why a restore checks rather than assumes."},
	{Method: "POST", Path: "/v1/rest/media/{id}/restore", Handler: "restoreMedia", Tag: "Media",
		Summary: "Restore a media item from the trash", Access: AccessWrite, RespDesc: "status=restored, the media id and the public URL that serves it (a code asset's URL is on the workspace's own host; an image's is the media host).",
		Desc: "Clears deleted_at, so the same URL works again — the bytes were never removed. If the object is no longer in the store the restore is refused with 409 and nothing is changed, because restoring would put a broken URL back in the library. Quota is untouched: the bytes were never released on soft delete."},
	{Method: "DELETE", Path: "/v1/rest/media/{id}/purge", Handler: "purgeMedia", Tag: "Media",
		Summary: "Destroy a trashed media item for good", Access: AccessWrite, RespDesc: "status=purged, the media id and the object key that was removed.",
		Desc: "Only a media row already in the trash can be destroyed; a live row is a 404. The object and its derived variants (sized images, PDF preview) are removed from the store and the bytes are released from the 1GB quota — this is the only path that releases them."},
	{Method: "POST", Path: "/v1/rest/uploads/image", Handler: "uploadImage", Tag: "Media",
		Summary: "Upload an image (multipart)", Access: AccessWrite,
		RespDesc:   "The stored media item with its public https URL.",
		Rate:       uploadRatePolicy(),
		RateSource: RateMiddleware,
		Desc:       "multipart/form-data with the file in file. Send the bytes, not JSON. An svg must be a well-formed XML document: a body with text before the root element (for example a /* */ banner) is refused with 400 upload.svgInvalid, because a browser parses image/svg+xml as XML and would show a parsererror instead of the image."},
	{Method: "POST", Path: "/v1/rest/uploads/avatar", Handler: "uploadAvatar", Tag: "Media",
		Summary: "Upload a profile avatar (multipart)", Access: AccessWrite,
		RespDesc:   "The stored avatar with its public https URL.",
		Rate:       uploadRatePolicy(),
		RateSource: RateMiddleware},
	{Method: "POST", Path: "/v1/rest/uploads/asset", Handler: "uploadAsset", Tag: "Media",
		Summary: "Upload a code asset (multipart: css/js/html/svg/woff2)", Access: AccessWrite,
		RespDesc:   "The stored asset, which stays unserved until its scan passes.",
		Rate:       uploadRatePolicy(),
		RateSource: RateMiddleware,
		Desc:       "Merchant-only. A code asset is scanned before it is served. An svg must be a well-formed XML document: a body with text before the root element (for example a /* */ banner) is refused with 400 upload.svgInvalid, because a browser parses image/svg+xml as XML and would show a parsererror instead of the image."},
	{Method: "PUT", Path: "/v1/rest/profile/avatar", Handler: "updateProfileAvatar", Tag: "Profile",
		Summary: "Set the signed-in user's avatar URL", Access: AccessSession, Body: true,
		Desc: "The URL must point at JomForm-hosted media; arbitrary external URLs are refused."},

	// ── Feedback and chat (a person, not an account) ─────────────────────────
	{Method: "GET", Path: "/v1/rest/feedback", Handler: "handleListFeedback", Tag: "Feedback",
		Summary: "List your feedback threads", Access: AccessSession},
	{Method: "POST", Path: "/v1/rest/feedback", Handler: "handleCreateFeedback", Tag: "Feedback",
		Summary: "File a feedback thread", Access: AccessSession, Body: true},
	{Method: "GET", Path: "/v1/rest/feedback/{id}", Handler: "handleGetFeedback", Tag: "Feedback",
		Summary: "Read one of your feedback threads", Access: AccessSession},
	{Method: "POST", Path: "/v1/rest/feedback/{id}/messages", Handler: "handleReplyFeedback", Tag: "Feedback",
		Summary: "Reply on one of your feedback threads", Access: AccessSession, Body: true},
	{Method: "GET", Path: "/v1/rest/chat", Handler: "handleChatStatus", Tag: "In-app assistant",
		Summary: "Report whether the in-app assistant is available", Access: AccessSession},
	{Method: "POST", Path: "/v1/rest/chat", Handler: "handleChat", Tag: "In-app assistant",
		Summary: "Ask the in-app assistant one message", Access: AccessSession, Body: true,
		RespDesc:   "The assistant's reply, the tool steps it took, and whether it filed feedback.",
		Rate:       chatMessageRatePolicy(),
		RateSource: RateHandler,
		Desc:       "Session-only, and read-only by construction: the assistant's tool calls run under a readonly identity, so nothing typed into the box can write. The limit is enforced in the handler, keyed on the user rather than the IP."},
	{Method: "POST", Path: "/v1/rest/chat/confirm", Handler: "handleChatConfirm", Tag: "In-app assistant",
		Summary: "Execute a write the assistant proposed", Access: AccessSession, Body: true,
		RespDesc:   "status, the tool that ran, and its result.",
		Rate:       chatConfirmRatePolicy(),
		RateSource: RateHandler,
		Desc:       "The only place a proposed write is executed. The body carries the opaque single-use token the browser holds and the args_hash the card showed; there is no confirm flag and nothing the caller can set to make the write happen, because the token IS the authorisation. A model cannot reach this endpoint — it is never given the token."},
	{Method: "POST", Path: "/v1/rest/chat/cancel", Handler: "handleChatCancel", Tag: "In-app assistant",
		Summary: "Discard a write the assistant proposed", Access: AccessSession, Body: true,
		RespDesc:   "status and the tool whose proposal was discarded.",
		Rate:       chatConfirmRatePolicy(),
		RateSource: RateHandler,
		Desc:       "Same request shape as confirm (token + args_hash), and it shares confirm's bucket. Withdrawing a proposal executes nothing."},
}

// restSpecInfo is the document's info block.
const (
	restSpecTitle   = "JomForm REST API"
	restSpecVersion = "1.0.0"
	restSpecDesc    = "The JomForm REST API. Every route is under `/v1/rest/` and is authenticated with " +
		"`Authorization: Bearer <api key>`; the same key also authenticates the JomForm MCP server. " +
		"Keys are created in Settings → API keys, or with `POST /v1/rest/keys`.\n\n" +
		"**Server to server only.** The API sends no `Access-Control-Allow-*` headers, so a browser " +
		"page on another origin cannot read a response. This is deliberate, not an omission: the " +
		"API is built for server-side calls.\n\n" +
		"**Rate limits are the exception, not the rule.** Within this API only the routes marked " +
		"`x-jomform-rate-limit` are throttled; every other route has no rate limit at all. Do not " +
		"build a client that assumes a uniform quota."
)

// restSpecTags is the tag list, ordered for the published document.
var restSpecTags = []string{
	"Forms", "Form settings", "Submissions", "Products", "Sales", "Subscriptions",
	"Payments (CHIP)", "Webhooks", "Domains", "Media", "Templates", "Analytics",
	"Spam review", "Settings", "API keys", "Account", "Profile", "Feedback",
	"In-app assistant",
}

// restComponentSchemas is the published component set. Only shapes the handlers
// actually return are described here; a route whose payload is a computed rollup
// says so in prose (RespDesc) instead of pretending to a schema.
var restComponentSchemas = map[string]map[string]any{
	"Error": {
		"type":        "object",
		"description": "Every failure body is `{\"error\": \"...\"}`. A few routes add fields: publish adds `code` and `requires_ack` when a payment form has no products.",
		"properties": map[string]any{
			"error":        map[string]any{"type": "string"},
			"code":         map[string]any{"type": "string"},
			"requires_ack": map[string]any{"type": "boolean"},
		},
		"required": []string{"error"},
	},
	"Paged": {
		"type": "object",
		"properties": map[string]any{
			"items": map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"page":  map[string]any{"type": "integer"},
			"limit": map[string]any{"type": "integer"},
			"total": map[string]any{"type": "integer"},
		},
		"required": []string{"items", "page", "limit", "total"},
	},
	"Count": {
		"type":       "object",
		"properties": map[string]any{"count": map[string]any{"type": "integer"}},
		"required":   []string{"count"},
	},
	"Deleted": {
		"type":       "object",
		"properties": map[string]any{"deleted": map[string]any{"type": "boolean"}},
	},
	"BulkResult": {
		"type": "object",
		"properties": map[string]any{
			"updated":        map[string]any{"type": "integer"},
			"deleted":        map[string]any{"type": "integer"},
			"needs_products": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"edge":           map[string]any{"type": "string", "enum": []any{"purged", "not_configured", "out_of_zone", "failed"}, "description": "Present when the action left a publicly served status and a cached share card had to go: what the CDN said."},
			"edge_max_age_s": map[string]any{"type": "integer", "description": "Present with any edge value other than purged: the freshness window that now bounds how long the old cards can still be served."},
		},
	},
	"FormStatus": {
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{"type": "string"},
			"edge": map[string]any{"type": "string", "enum": []any{"purged", "not_configured", "out_of_zone", "failed"},
				"description": "Present when this transition left a publicly served status (published or unlisted) and the form's cached share card /og/{slug}.png had to be dropped: what the CDN said. Absent when nothing was invalidated."},
			"edge_urls": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
				"description": "The card URL prefixes the purge named, one per public host."},
			"edge_max_age_s": map[string]any{"type": "integer",
				"description": "Present with any edge value other than purged: the cache freshness window that then bounds how long the old card can still be served."},
			"edge_error": map[string]any{"type": "string",
				"description": "Present with edge=failed: what the CDN answered."},
		},
	},
	"Variation": {
		"type": "object",
		"properties": map[string]any{
			"id":         map[string]any{"type": "string"},
			"name":       map[string]any{"type": "string"},
			"price_sen":  map[string]any{"type": "integer", "description": "Price in sen (1/100)."},
			"stock":      map[string]any{"type": []string{"integer", "null"}, "description": "null means unlimited."},
			"images":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"sort_order": map[string]any{"type": "integer"},
			// Issue #623: a variation may carry its own cycle; absent = inherit.
			"billing_period":       map[string]any{"type": []string{"string", "null"}, "enum": []any{"month", "year", nil}},
			"trial_days":           map[string]any{"type": []string{"integer", "null"}},
			"subscription_charges": map[string]any{"type": []string{"integer", "null"}},
		},
		"required": []string{"name", "price_sen"},
	},
	"Product": {
		"type": "object",
		"properties": map[string]any{
			"id":             map[string]any{"type": "string"},
			"name":           map[string]any{"type": "string"},
			"description":    map[string]any{"type": "string"},
			"price_sen":      map[string]any{"type": "integer", "description": "Price in sen (1/100)."},
			"images":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"type":           map[string]any{"type": "string", "enum": []any{"one_time", "subscription"}},
			"billing_period": map[string]any{"type": "string", "enum": []any{"month", "year", ""}},
			"trial_days":     map[string]any{"type": "integer"},
			"schedule_mode":  map[string]any{"type": "string", "enum": []any{"from_payment", "catch_up", "skip_missed"}},
			"active":         map[string]any{"type": "boolean"},
			"stock":          map[string]any{"type": []string{"integer", "null"}, "description": "null means unlimited."},
			"variations":     map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/Variation"}},
			"created_at":     map[string]any{"type": "string"},
			"updated_at":     map[string]any{"type": "string"},
			"subscription_charges": map[string]any{"type": []string{"integer", "null"},
				"description": "null means charge for as long as the subscription runs."},
		},
		"required": []string{"id", "name", "price_sen", "type"},
	},
	"ProductList": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/Product"}},
	"ProductWrite": map[string]any{
		"type":        "object",
		"description": "Product fields to create or change. On PUT, omitted fields are unchanged.",
		"properties": map[string]any{
			"name":                 map[string]any{"type": "string"},
			"description":          map[string]any{"type": "string"},
			"price_sen":            map[string]any{"type": "integer", "minimum": 0},
			"images":               map[string]any{"type": "array", "items": map[string]any{"type": "string", "format": "uri"}},
			"type":                 map[string]any{"type": "string", "enum": []any{"one_time", "subscription"}},
			"billing_period":       map[string]any{"type": "string", "enum": []any{"month", "year"}},
			"trial_days":           map[string]any{"type": "integer", "minimum": 0},
			"stock":                map[string]any{"type": []string{"integer", "null"}},
			"active":               map[string]any{"type": "boolean"},
			"variations":           map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/Variation"}},
			"subscription_charges": map[string]any{"type": []string{"integer", "null"}},
			"schedule_mode":        map[string]any{"type": "string", "enum": []any{"from_payment", "catch_up", "skip_missed"}},
		},
	},
	"Form": {
		"type": "object",
		"properties": map[string]any{
			"id":         map[string]any{"type": "string"},
			"slug":       map[string]any{"type": "string"},
			"title":      map[string]any{"type": "string"},
			"status":     map[string]any{"type": "string", "enum": []any{"draft", "private", "published", "unlisted"}},
			"mode":       map[string]any{"type": "string", "enum": []any{"payment", "response"}},
			"url":        map[string]any{"type": "string"},
			"config":     map[string]any{"type": "object", "additionalProperties": true},
			"theme":      map[string]any{"type": "object", "additionalProperties": true},
			"is_private": map[string]any{"type": "boolean"},
			"updated_at": map[string]any{"type": "string"},
		},
		"required": []string{"id", "slug", "title", "status", "mode"},
	},
	"FormList": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/Form"}},
	"FormWrite": map[string]any{
		"type":        "object",
		"description": "Form fields to create or change. `config` is the builder document (blocks); sending it replaces the stored config.",
		"properties": map[string]any{
			"slug":   map[string]any{"type": "string"},
			"title":  map[string]any{"type": "string"},
			"config": map[string]any{"type": "object", "additionalProperties": true},
			"theme":  map[string]any{"type": "object", "additionalProperties": true},
		},
	},
	"KeyList": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/Key"}},
	"Key": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":   map[string]any{"type": "string"},
			"name": map[string]any{"type": "string"},
			"scopes": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []any{"readonly", "readwrite"}},
				"description": "readonly may read; readwrite may also write. A legacy admin scope exists on old keys and behaves as a superset; it is not offered to new keys."},
		},
		"required": []string{"id", "name", "scopes"},
	},
	"KeyCreated": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":     map[string]any{"type": "string"},
			"name":   map[string]any{"type": "string"},
			"scopes": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"key":    map[string]any{"type": "string", "description": "The raw key. Returned in this response only — it is never readable again."},
		},
		"required": []string{"id", "key"},
	},
	"Summary": map[string]any{
		"type":                 "object",
		"description":          "Workspace totals: sale counts by status, revenue net of refunds, and record counts.",
		"additionalProperties": true,
	},
	"Subscription": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":             map[string]any{"type": "string"},
			"customer":       map[string]any{"type": "string"},
			"product_id":     map[string]any{"type": "string"},
			"product_name":   map[string]any{"type": "string"},
			"amount_sen":     map[string]any{"type": "integer", "description": "Amount in sen (1/100)."},
			"period":         map[string]any{"type": "string", "enum": []any{"month", "year"}},
			"status":         map[string]any{"type": "string", "enum": []any{"active", "past_due", "cancelled"}},
			"next_charge_at": map[string]any{"type": "string"},
			"retry_attempts": map[string]any{"type": "integer"},
			"created_at":     map[string]any{"type": "string"},
		},
		"required": []string{"id", "status", "amount_sen"},
	},
	"SubscriptionList": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/Subscription"}},
	"Webhook": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":         map[string]any{"type": "string"},
			"url":        map[string]any{"type": "string", "format": "uri"},
			"events":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"active":     map[string]any{"type": "boolean"},
			"secret":     map[string]any{"type": "string"},
			"created_at": map[string]any{"type": "string"},
		},
	},
	"WebhookList": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/Webhook"}},
	"Media": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":          map[string]any{"type": "string"},
			"form_id":     map[string]any{"type": "string"},
			"key":         map[string]any{"type": "string"},
			"url":         map[string]any{"type": "string"},
			"mime":        map[string]any{"type": "string"},
			"size":        map[string]any{"type": "integer"},
			"source":      map[string]any{"type": "string"},
			"scan_status": map[string]any{"type": "string", "description": "Only `ok` is served for a code asset."},
			"scan_reason": map[string]any{"type": "string"},
			"sizes":       map[string]any{"type": "object", "additionalProperties": true},
		},
		"required": []string{"id", "url"},
	},
	"MediaList": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/Media"}},
	"Event": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":         map[string]any{"type": "string"},
			"name":       map[string]any{"type": "string"},
			"properties": map[string]any{"type": "object", "additionalProperties": true},
			"created_at": map[string]any{"type": "string"},
		},
	},
	"EventList": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/Event"}},
	"FormStats": map[string]any{
		"type":                 "object",
		"description":          "One row per form: the form's own mode, its entry count (responses for a response form, sales for a payment form) and its money figures.",
		"additionalProperties": true,
	},
	"FormStatsList": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/FormStats"}},
}

// sortedRouteDocs returns the registry ordered by path then method, so the
// published document is byte-stable across runs.
func sortedRouteDocs() []restRouteDoc {
	out := make([]restRouteDoc, len(restRouteDocs))
	copy(out, restRouteDocs)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return methodRank(out[i].Method) < methodRank(out[j].Method)
	})
	return out
}

// methodRank orders methods inside one path item the way the spec reads
// (GET, POST, PUT, PATCH, DELETE).
func methodRank(m string) int {
	switch m {
	case "GET":
		return 0
	case "POST":
		return 1
	case "PUT":
		return 2
	case "PATCH":
		return 3
	case "DELETE":
		return 4
	}
	return 5
}

// pathParameters returns the {name} placeholders of a route path, as OpenAPI
// path parameters. Every placeholder is a string in this API (ids, slugs, ref
// codes and email addresses).
func pathParameters(path string) []map[string]any {
	var out []map[string]any
	for _, seg := range strings.Split(path, "/") {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			name := strings.TrimSuffix(strings.TrimPrefix(seg, "{"), "}")
			out = append(out, map[string]any{
				"name":        name,
				"in":          "path",
				"required":    true,
				"description": "Identifier in the URL path.",
				"schema":      map[string]any{"type": "string"},
			})
		}
	}
	return out
}

// operationFor renders one registry entry as an OpenAPI operation object.
func operationFor(d restRouteDoc) map[string]any {
	desc := d.Desc
	if d.RespDesc != "" {
		if desc != "" {
			desc += "\n\n"
		}
		desc += "Response: " + d.RespDesc
	}
	op := map[string]any{
		"operationId": d.Handler,
		"summary":     d.Summary,
		"tags":        []string{d.Tag},
		// The access level is an extension, not prose: the docs page renders it
		// and the parity test asserts it matches the gate the handler calls.
		"x-jomform-access": d.Access,
		"responses":        responsesFor(d),
	}
	if d.ScopeOnly {
		// The caveat the legend needs, published with the operation rather than
		// typed into the page (card t_4abd052a). The docs page renders this on the
		// route's row AND marks its legend entry, so the promise cannot outrun the
		// gate.
		op["x-jomform-scope-only"] = true
	}
	if desc != "" {
		op["description"] = desc
	}
	if d.Rate != "" {
		op["x-jomform-rate-limit"] = map[string]any{
			"scope":  d.RateSource,
			"policy": d.Rate,
		}
		op["description"] = strings.TrimSpace(op["description"].(string) + "\n\nRate limit: " + d.Rate + ".")
	}
	if ps := pathParameters(d.Path); len(ps) > 0 {
		op["parameters"] = ps
	}
	if d.Body {
		schema := map[string]any{"type": "object", "additionalProperties": true}
		if d.Req != "" {
			schema = map[string]any{"$ref": "#/components/schemas/" + d.Req}
		}
		op["requestBody"] = map[string]any{
			"required": true,
			"content": map[string]any{
				"application/json": map[string]any{"schema": schema},
			},
		}
	}
	return op
}

// responsesFor builds the response object: the documented success body, 401 for
// every route, 403 where a scope/role can refuse, 429 where a limit exists.
func responsesFor(d restRouteDoc) map[string]any {
	ok := "200"
	if d.Method == "POST" && d.Path == "/v1/rest/products" {
		ok = "201"
	}
	success := map[string]any{"description": "Success."}
	mediaType := d.RespContentType
	if mediaType == "" {
		mediaType = "application/json"
	}
	if d.Resp != "" {
		success["content"] = map[string]any{
			mediaType: map[string]any{
				"schema": map[string]any{"$ref": "#/components/schemas/" + d.Resp},
			},
		}
	} else {
		// No component: either described in prose (RespDesc) or a small ack
		// body. Either way the honest schema is "an object", not a guess.
		success["content"] = map[string]any{
			mediaType: map[string]any{
				"schema": map[string]any{"type": "object", "additionalProperties": true},
			},
		}
	}
	out := map[string]any{
		ok: success,
		"400": errorResponse("The request body or parameters are invalid. " +
			"Error messages are returned in the API's own words, not a fixed enumeration."),
		"401": errorResponse("Missing, unknown or revoked credential. Body: {\"error\":\"auth diperlukan\"}."),
	}
	if d.Access != AccessSession {
		out["403"] = errorResponse("The credential is not allowed to do this: a readonly key on a write, " +
			"or a ?workspace= scope the caller has no manage/admin role in.")
	}
	if d.Access == AccessSession {
		out["503"] = errorResponse("The backing service for this route is not configured on this deployment.")
	}
	if d.Rate != "" {
		out["429"] = errorResponse("Rate limited. Throttled middleware responses carry `Retry-After: 30`.")
	}
	return out
}

func errorResponse(desc string) map[string]any {
	return map[string]any{
		"description": desc,
		"content": map[string]any{
			"application/json": map[string]any{
				"schema": map[string]any{"$ref": "#/components/schemas/Error"},
			},
		},
	}
}

// OpenAPISpec builds the published document. It is deterministic: the same
// source always produces byte-identical JSON, which is what lets the parity test
// compare the files on disk against a fresh generation.
func OpenAPISpec() map[string]any {
	paths := map[string]any{}
	for _, d := range sortedRouteDocs() {
		item, ok := paths[d.Path].(map[string]any)
		if !ok {
			item = map[string]any{}
			paths[d.Path] = item
		}
		item[strings.ToLower(d.Method)] = operationFor(d)
	}
	schemas := map[string]any{}
	for name, s := range restComponentSchemas {
		schemas[name] = s
	}
	servers := map[string]any{
		"servers": []any{
			map[string]any{
				"url":         "https://api.jomform.com",
				"description": "Production. Served through Cloudflare (requests reach the API through the proxy).",
			},
		},
	}
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       restSpecTitle,
			"version":     restSpecVersion,
			"description": restSpecDesc,
			"license":     map[string]any{"name": "MIT"},
		},
		"servers": servers["servers"],
		"tags":    tagObjects(),
		"paths":   paths,
		"components": map[string]any{
			"schemas": schemas,
		},
		// The document's own contract with its reader: what it covers and what
		// it deliberately does not state.
		"x-jomform-scope": "Every /v1/rest/* route. /v1/auth/*, /v1/admin/*, /v1/cf/* and the public " +
			"form pages are outside this document.",
		"x-jomform-cors": "None. The API sends no Access-Control-Allow-* header and does not answer " +
			"browser preflight requests; it is for server-to-server calls.",
		"x-jomform-versioning": "v1 is the stable surface. A breaking change means a new prefix " +
			"(/v2), not a change under /v1.",
	}
}

func tagObjects() []any {
	out := make([]any, 0, len(restSpecTags))
	for _, t := range restSpecTags {
		out = append(out, map[string]any{"name": t})
	}
	return out
}

// OpenAPISpecJSON renders the document as the published JSON bytes (indented,
// trailing newline).
func OpenAPISpecJSON() ([]byte, error) {
	b, err := json.MarshalIndent(OpenAPISpec(), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// GeneratedArtifactPaths lists every artifact this package publishes, in the
// order the generator writes them. It is the single source for the generator AND
// for the freshness guard, so a new artifact cannot be generated-but-unchecked.
//
// WHY web/public/openapi.json IS A SEPARATE ARTIFACT FROM api/openapi/openapi.json
//
//	They hold byte-identical bytes, and the duplication is deliberate. The
//	contract's audience is a third-party developer, and this repository is
//	PRIVATE: a link to api/openapi/openapi.json on github.com answers 404 for
//	exactly the reader the /docs/api page was written for. web/public/ is served
//	by the web app at https://jomform.com/openapi.json, so the document is
//	readable without a repository account. Both copies are generated from
//	OpenAPISpecJSON() in one run, so they cannot disagree — and
//	TestGeneratedArtifactsAreInSync fails if either one is stale.
func GeneratedArtifactPaths() []string {
	out := []string{
		"api/openapi/openapi.json",
		"web/public/openapi.json",
		"web/src/lib/openapiSpec.gen.ts",
		"web/src/lib/apiExamples.gen.ts",
	}
	sort.Strings(out)
	return out
}

// GeneratedArtifact renders one published artifact from this package's registry.
// Both the generator (cmd/openapi-gen) and the freshness guard
// (openapi_parity_test.go) call THIS function, so "the file on disk matches the
// code" is checked against the one definition of what the file should be.
//
// The web artifact is a typed module rather than raw JSON so the /docs/api page
// can import it without a fetch, and so a TypeScript mistake in a consumer is a
// build failure instead of a runtime blank.
func GeneratedArtifact(rel string) string {
	switch rel {
	case "api/openapi/openapi.json", "web/public/openapi.json":
		b, err := OpenAPISpecJSON()
		if err != nil {
			// Marshalling a plain map of JSON-safe values cannot fail; treat it
			// as a programming error rather than emitting a broken artifact.
			panic("openapi: marshal spec: " + err.Error())
		}
		// Two paths, one document. api/openapi/openapi.json is the copy the repo
		// publishes for a reader with access to the repository;
		// web/public/openapi.json is the copy a browser can fetch from
		// https://jomform.com/openapi.json without one. They are emitted from the
		// same call so they cannot drift from each other.
		return string(b)
	case "web/src/lib/openapiSpec.gen.ts":
		b, err := OpenAPISpecJSON()
		if err != nil {
			panic("openapi: marshal spec: " + err.Error())
		}
		// The object is annotated rather than declared `as const`: a literal type
		// inferred over 200 KB of JSON makes the TypeScript compiler work hard on
		// every build that touches this module, and nothing here needs the literal
		// narrowing. The annotation is declared in this file so it stays
		// self-contained.
		return "// GENERATED FILE — DO NOT EDIT.\n" +
			"//\n" +
			"// The JomForm REST API contract (OpenAPI 3.1), rendered from\n" +
			"// api/internal/httpapi/openapi_routes.go — the same registry the API's own\n" +
			"// route table is pinned to by api/internal/httpapi/openapi_parity_test.go.\n" +
			"//\n" +
			"// The /docs/api page imports THIS module and renders the endpoint reference\n" +
			"// from it, so the page cannot describe an endpoint the API does not have.\n" +
			"//\n" +
			"// Regenerate:  cd api && go run ./cmd/openapi-gen\n" +
			"// A stale copy fails the api test suite (TestGeneratedArtifactsAreInSync).\n" +
			"\n" +
			"export type OpenApiOperation = {\n" +
			"  operationId?: string;\n" +
			"  summary?: string;\n" +
			"  description?: string;\n" +
			"  tags?: string[];\n" +
			"  parameters?: { name: string; in: string; required?: boolean; description?: string; schema?: unknown }[];\n" +
			"  requestBody?: unknown;\n" +
			"  responses?: Record<string, unknown>;\n" +
			"  \"x-jomform-access\"?: string;\n" +
			"  \"x-jomform-rate-limit\"?: { scope?: string; policy?: string };\n" +
			"  // Any further OpenAPI field — deliberately open, see OpenApiSpec.\n" +
			"  [k: string]: unknown;\n" +
			"};\n" +
			"\n" +
			"// The document's own keys, typed only as far as a consumer needs them. The\n" +
			"// generated object is a full OpenAPI document, so everything beyond these\n" +
			"// fields is intentionally left loose: a strict shape here would only mean a\n" +
			"// type fight every time the generator emits one more standard field.\n" +
			"export type OpenApiSpec = {\n" +
			"  openapi: string;\n" +
			"  info: { title: string; version: string; description?: string; license?: { name: string } };\n" +
			"  servers: { url: string; description?: string }[];\n" +
			"  tags: { name: string }[];\n" +
			"  paths: Record<string, Record<string, OpenApiOperation>>;\n" +
			"  components: { schemas: Record<string, unknown> };\n" +
			"  \"x-jomform-scope\"?: string;\n" +
			"  \"x-jomform-cors\"?: string;\n" +
			"  \"x-jomform-versioning\"?: string;\n" +
			"  // Any further OpenAPI field (webhooks, externalDocs, …).\n" +
			"  [k: string]: unknown;\n" +
			"};\n" +
			"\n" +
			"export const OPENAPI_SPEC: OpenApiSpec = " + string(b) + ";\n"
	case "web/src/lib/apiExamples.gen.ts":
		b, err := restAPIExampleJSON()
		if err != nil {
			panic("openapi: marshal examples: " + err.Error())
		}
		return "// GENERATED FILE — DO NOT EDIT.\n" +
			"//\n" +
			"// The worked examples on /docs/api, rendered from\n" +
			"// api/internal/httpapi/openapi_examples.go. Each one is EXECUTED against a\n" +
			"// live server by api/internal/httpapi/openapi_examples_test.go and asserted\n" +
			"// on its status and body, so a copy-pasteable example cannot be wrong\n" +
			"// without the api suite going red. The `curl` strings here are generated\n" +
			"// from the same structured request the test runs.\n" +
			"//\n" +
			"// Regenerate:  cd api && go run ./cmd/openapi-gen\n" +
			"\n" +
			"export type ApiExample = {\n" +
			"  id: string;\n" +
			"  title: string;\n" +
			"  why: string;\n" +
			"  note: string;\n" +
			"  method: string;\n" +
			"  path: string;\n" +
			"  curl: string;\n" +
			"  expectStatus: number;\n" +
			"  expectContains: string;\n" +
			"  needsKey: boolean;\n" +
			"  needsFixture: boolean;\n" +
			"};\n" +
			"\n" +
			"export const API_EXAMPLES: { examples: ApiExample[] } = " + string(b) + ";\n"
	}
	return ""
}
