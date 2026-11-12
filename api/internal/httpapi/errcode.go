// Machine-readable error codes for the API error envelope.
//
// THE BUG THIS EXISTS FOR. The API answered every failure with Malay prose —
// `{"error":"workspace tidak dijumpai"}` — and the dashboard rendered that
// string verbatim (`setErr(d.error || t("tpl.useFail"))`). The prose branch is
// the one that actually fires on a real failure, so a user whose dashboard
// language is English read a Malay sentence. Translating the Go literals is NOT
// a fix: the API does not know the caller's language (it is a per-profile
// preference the server never receives on a JSON call), so a server string can
// never follow `users.lang` for a browser-rendered error.
//
// THE CONTRACT. Every error response carries a stable machine `code`:
//
//		{"error":"workspace not found","code":"workspace.notFound"}
//
//	  - The `error` prose is KEPT. It is the wire message for API-key callers,
//	    curl, MCP and the public buyer-form JavaScript, none of which can consume
//	    an i18n key. Removing it would break live callers; the dashboard simply
//	    stops rendering it.
//	  - The dashboard reads `code`, looks it up in its own dictionary and renders
//	    ITS message in the user's language. A code the client does not know falls
//	    back to the page's own localized message, never to this prose.
//
// WHICH CODES ARE SPECIFIC. A code is worth naming when the page's own fallback
// sentence would be wrong or useless ("Failed to use template." hides "that
// workspace is not yours") — i.e. the user-actionable classes: tenancy and role
// refusals, payment/CHIP state, form/product validation, uploads, webhooks,
// domains, rate limits, account/auth. Everything else gets a HTTP-class code,
// which the client deliberately does not translate: the page's own localized
// message is a better sentence than any generic one.
//
// Codes are a PUBLIC CONTRACT (they ship in JSON to API-key callers and are
// mapped by name in web/src/lib/apiErr.ts). Renaming one is a breaking change.
package httpapi

import (
	"net/http"
	"strings"

	"github.com/wzul/jomform/api/internal/customform"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/lifecycle"
	"github.com/wzul/jomform/api/internal/mcpsrv"
	"github.com/wzul/jomform/api/internal/objstore"
)

// Specific codes: the user-actionable classes the dashboard translates.
const (
	// Tenancy / workspace scope.
	CodeWorkspaceNotFound  = "workspace.notFound"
	CodeWorkspaceSlugTaken = "workspace.slugTaken"
	CodeWorkspaceSlugBad   = "workspace.slugInvalid"
	// A workspace NAME over the character cap. Named separately from the slug
	// refusals because the remedy is a different field: answering slugInvalid
	// sends the merchant to fix their ADDRESS for a problem with their NAME
	// (reachable by paste and by assistive technology even though the field is
	// capped at 120).
	CodeWorkspaceNameTooLong = "workspace.nameTooLong"
	// A workspace DELETE refused because something still lives in it. The refusal
	// names the obstacles (the body's `blockers` array), which is exactly the
	// class of message the page's own generic sentence cannot say.
	//
	// RETIRED WITH ITS GUARD (workspace trash). The DELETE no longer refuses on
	// content at all — it moves the workspace to the bin — so no handler returns
	// this code and the "every declared code is reachable" guard would (correctly)
	// report it as dead. Do not re-add it without the guard it names.
	//
	// CodeWorkspaceNotEmpty = "workspace.notEmpty"
	//
	// A permanent workspace PURGE refused because the workspace was ever sold.
	// Sales are the merchant's record of money received and a purge destroys
	// them, so the refusal names the count the same way the form purge does.
	CodeWorkspaceHasSales = "workspace.hasSales"

	// The chat lane's "your request names no workspace" refusal (t_fb393abe).
	//
	// WHY IT EARNS ITS OWN CODE rather than riding the HTTP class. A 400 from
	// POST /chat/confirm is NOT necessarily this condition: chatConfirmStatusFor
	// maps everything it does not recognise to 400, and one reachable class is a
	// tool the server refused AFTER the token was spent (chat/proposal.go's
	// "server refused: %s"). The web page branched on the bare status, so a
	// read_only member whose confirmed write was refused was told to "choose a
	// workspace first" — advice they cannot act on, because they already had one
	// selected (the request carried ?workspace=), and the refusal they needed was
	// the role, not the scope. The code is what lets the page tell the two apart.
	//
	// The 400 stays: the scope refusal is a MISSING PARAMETER and not a
	// permission denial, and wsfetch.ts treats 401/403 as "the session is gone"
	// and redirects to /login (see the argument on scopeAmbiguous in
	// rest_scope.go). Only the CODE is new; the status is unchanged, so no client
	// that branches on 400 is perturbed.
	CodeWorkspaceScopeRequired = "workspace.scopeRequired"

	// The form-create refusal for an account with NO workspace (card
	// t_6c0badbe).
	//
	// WHY IT IS NOT CodeWorkspaceScopeRequired. That code answers "your request
	// did not say which workspace", and the remedy is to send ?workspace=<slug>.
	// This one answers "this account has no workspace AT ALL", and the remedy is
	// to create one — advice the scope code's sentence would not give, and a
	// merchant cannot act on the wrong one: a form stored with
	// `workspace_id IS NULL` can be published and is then 404 at every address
	// forever, because every public serving path resolves a form through its
	// workspace (measured on a clean self-host install: two published forms,
	// both unreachable).
	//
	// 409 rather than 400: the request is well formed, it is the account's state
	// that cannot carry it out, and the same request succeeds unchanged once a
	// workspace exists.
	CodeWorkspaceRequired = "workspace.required"

	// The chat lane's "this card cannot be carried out at all" refusal
	// (t_623676d1).
	//
	// A proposal whose arguments the target tool would never accept — the model
	// keyed the object `id` while the write tool requires `product_id`, which is
	// the rename the shipped read/write schemas invite — is refused BEFORE the
	// token is claimed, so the token is still live and the SAME card can never
	// succeed. Riding 409's class code would let the page print its spent/expired
	// sentence ("expired or was already used") for a proposal that neither
	// expired nor was used; the remedy is the same (ask for a fresh card) but the
	// reason is not, and the reason is what tells the user whether their click was
	// even attempted. The status is 409 like its siblings — the request was well
	// formed and it is the PROPOSAL that is unfit — so no client branching on
	// status is perturbed; only the CODE is new.
	CodeProposalArgsInvalid = "chat.proposalArgsInvalid"

	// Roles and API-key scope.
	CodeRoleManageRequired = "role.manageRequired"
	CodeRoleAdminRequired  = "role.adminRequired"
	CodeScopeReadonly      = "scope.readonly"

	// The device consent card's access-level refusal (card t_cf45b84c). A 400
	// with only the class code cannot be told apart from a malformed body, and
	// the sentence the page shows for "this code is not valid" would be wrong:
	// the code IS valid and the person may simply pick again.
	CodeDeviceScopeInvalid = "device.scopeInvalid"

	// Payments / CHIP / subscriptions.
	CodePaymentNotConfigured  = "payment.notConfigured"
	CodePaymentInactive       = "payment.inactive"
	CodeChipCredentialsBad    = "chip.credentialsInvalid"
	CodeRefundNotAllowed      = "refund.notAllowed"
	CodeSubscriptionCancelled = "subscription.cancelled"

	// Form / product / template validation.
	CodeFormStatusBad        = "form.statusInvalid"
	CodeFormPasswordTooShort = "form.passwordTooShort"
	CodeProductOutOfStock    = "product.outOfStock"
	CodeTemplateNeedMetadata = "template.needMetadata"

	// Trash refusals: a restore that cannot reuse the form's old slug, and a
	// permanent delete of a form that was sold. Both name the obstacle the
	// merchant has to clear, so the dashboard must be able to say it in the
	// merchant's language rather than echo the wire prose.
	CodeFormSlugTaken = "form.slugTaken"
	CodeFormHasSales  = "form.hasSales"

	// The two refusals a form-slug RENAME can answer with. They are named beside
	// CodeFormSlugTaken because a rename colliding with a live URL is the same
	// CONDITION as a restore colliding with one, and it is deliberately a
	// different CODE: the two refusals are different user ACTIONS with different
	// remedies, and the dashboard renders one sentence per code. A restore is
	// unblocked by freeing the slug (change the OTHER form's slug, then restore);
	// a rename is unblocked by choosing a different slug for THIS form. Sharing
	// one code would show one of the two the other's instruction — the exact
	// class of unusable message this file exists to prevent.
	CodeFormSlugInUse   = "form.slugInUse"
	CodeFormSlugInvalid = "form.slugInvalid"

	// A power-user `theme.custom_js` the deterministic #388 scan refuses.
	//
	// WHY IT NEEDS ITS OWN CODE (card t_2717ba30). The refusal's wire prose is
	// assembled around the scan's reason ("custom_js rejected: active code
	// execution marker: eval("), so no exact key can match it, and it fell
	// through to the class code `request.badRequest` — which the dashboard
	// deliberately does not translate. Measured on prod v1.21.296: pasting
	// `eval('2+2');` into /form-settings' "Custom JS" field and clicking Save
	// rendered the bare word "Failed", naming neither the field nor the reason,
	// so the merchant could not tell which control the server had refused. The
	// refusal itself is correct; only its NAME on the wire was missing.
	//
	// ONE code for every door that writes that sentence — REST form
	// create/update, REST template create/update/use, and the MCP form/template
	// tools (whose text is the same, and which therefore resolve to the same
	// code wherever a message→code lookup is applied). The dashboard then needs
	// one sentence, not one per route, and cannot miss a door.
	CodeFormCustomJSRejected = "form.customJsRejected"

	// The product bin's two refusals, the same class as the two above: a purge
	// that would destroy sales history, and one a live subscription still needs
	// (the FK is ON DELETE RESTRICT). Keyed on the exact wire message.
	CodeProductHasSales         = "product.hasSales"
	CodeProductHasSubscriptions = "product.hasSubscriptions"

	// A shipping-zone-set save that would DELETE a set a form's shipping block
	// still points at. The refusal is ASSEMBLED around the set name and the
	// form titles, so the page's own generic sentence ("Failed to save shipping
	// rates") was all a merchant saw for a refusal whose whole point is that
	// they must re-point that form first. Keyed by PREFIX in errCodeByPrefix,
	// on the constant the store builds its sentence from.
	CodeShippingSetInUse = "shipping.setInUse"

	// A shipping-zone-set save whose SHAPE cannot resolve (card t_417fbacb):
	// a set with no name, a set with no zone, an unnamed zone, a zone with no
	// rate, a negative price, a non-numeric postcode or an inverted weight band.
	//
	// WHY IT NEEDS ITS OWN CODE. ValidateShippingSets assembles each refusal
	// around the field at fault ("zon "West" belum ada kadar"), and the REST edge
	// wrote them with only the 400 class code — which the dashboard deliberately
	// does not translate, so the merchant read "Failed to save shipping rates"
	// and nothing else: no field, no reason. The remedy is always the same (fix
	// the zone or its rate and save again), so ONE code and one sentence serve
	// every shape, and the sentence names the three things a save requires.
	//
	// Keyed by PREFIX in errCodeByPrefix on dbstore.ShippingInvalidLeadIn — the
	// constant every refusal in that function is wrapped with — so a re-wording
	// cannot detach the code.
	CodeShippingInvalid = "shipping.invalid"

	// The TYPED-CONFIRMATION refusals: the four sites that make the caller name
	// back what it is about to destroy (a form's slug, a workspace's slug, a
	// product's name) and refuse when the answer does not match.
	//
	// WHY THEY NEED THEIR OWN CODES. These messages are ASSEMBLED around the
	// value they name — "taip nama produk (Kopi Ais) untuk sahkan pemadaman
	// kekal" — so no key in errCodeByMessage can ever match one, and the 400
	// fell through to the HTTP class code `request.badRequest`. A dashboard that
	// renders by code (the whole point of this file, #6177) could then not tell
	// "you typed the wrong name" from any other malformed request, and printed
	// the server's Malay prose instead of its own sentence in the user's
	// language. They are keyed by PREFIX in errCodeByPrefix instead — the fixed
	// lead-in before the interpolated value — so the code is stable whatever the
	// caller mistyped and is not a second copy of the value.
	//
	// ONE CODE FOR THE TWO WORKSPACE MESSAGES. The bin and the permanent delete
	// write different sentences ("untuk sahkan pemadaman" vs "untuk sahkan
	// pemadaman kekal"), but it is the same complaint with the same remedy —
	// type the workspace's own slug — so one code and one sentence serve both.
	// Two codes would be two translations to keep in step and no different
	// instruction to give the merchant.
	CodeFormConfirmSlugMismatch      = "form.confirmSlugMismatch"
	CodeWorkspaceConfirmSlugMismatch = "workspace.confirmSlugMismatch"
	CodeProductConfirmNameMismatch   = "product.confirmNameMismatch"

	// Caller-supplied URL scheme (urlguard) and outbound webhook URLs.
	CodeURLNotHTTPS       = "url.notHttps"
	CodeWebhookURLInvalid = "webhook.urlInvalid"

	// Uploads / media.
	CodeUploadStorageQuota    = "upload.storageQuota"
	CodeUploadTypeUnsupported = "upload.typeUnsupported"
	// An SVG whose bytes are not a well-formed XML document (t_20af3915). The
	// type classification is right — it IS an SVG — but the browser parses an
	// `image/svg+xml` response as XML, and text before the root element (the
	// `/* ... */` banner on a hand-written file) is a fatal parse error, so the
	// asset would be stored and never render. The page's generic "Upload failed"
	// sentence cannot tell the merchant that their file needs fixing, and the
	// API's prose is never rendered (issue #6177), so this class needs its own
	// code and its own localized sentence.
	CodeUploadSVGInvalid = "upload.svgInvalid"

	// Custom-domain TLS material.
	CodeDomainCertInvalid = "domain.certInvalid"

	// Throttling.
	CodeRateLimited = "rate.limited"

	// Account / auth, surfaced on login, register and profile.
	CodeEmailTaken       = "email.taken"
	CodeEmailInvalid     = "email.invalid"
	CodeEmailDomainNoMX  = "email.domainUnreachable"
	CodeEmailUnverified  = "email.unverified"
	CodePasswordTooShort = "password.tooShort"
	CodePasswordInvalid  = "password.invalid"
	CodeLoginInvalid     = "login.invalid"
	CodeStepUpRequired   = "auth.stepUpRequired"

	// Social "connect" ownership link (issue #302). The dashboard /connect page
	// is the ONLY surface that renders these, and it has no page context to
	// fall back on — the link IS the whole request — so each refusal needs its
	// own sentence rather than a shared "unable to connect".
	CodeConnectInvalid     = "connect.invalid"
	CodeConnectExpired     = "connect.expired"
	CodeConnectNoAccount   = "connect.accountMissing"
	CodeConnectOtherLinked = "connect.identityTaken"

	// "No token in the request" for the four token-consuming endpoints, one code
	// each (issue #6177 follow-up). These were ONE shared literal
	// ("token diperlukan", written by social.go, verify.go, auth_ux.go and
	// auth_recovery.go), which meant one wire message could not say WHICH link
	// the caller has to reopen. Each handler now answers its own literal, so
	// each can carry its own code and its own sentence on the dashboard.
	// See the sibling codes on the same surfaces for the bad-token case.
	CodeVerifyTokenMissing  = "verify.tokenMissing"
	CodeResetTokenMissing   = "reset.tokenMissing"
	CodeMagicTokenMissing   = "magicLink.tokenMissing"
	CodeConnectTokenMissing = "connect.tokenMissing"
)

// Class codes: one per HTTP status the error path uses, for every message
// without a specific code. Stable and low-cardinality, so a client can bucket
// on them; the dashboard renders its own page message for these.
const (
	ClassBadRequest      = "request.badRequest"
	ClassAuthRequired    = "auth.required"
	ClassForbidden       = "request.forbidden"
	ClassNotFound        = "request.notFound"
	ClassConflict        = "request.conflict"
	ClassPayloadTooLarge = "request.payloadTooLarge"
	ClassTooManyRequests = "request.tooManyRequests"
	ClassUnprocessable   = "request.unprocessable"
	ClassNotImplemented  = "service.notImplemented"
	ClassUpstreamFailed  = "service.upstreamFailed"
	ClassUnavailable     = "service.unavailable"
	ClassInternal        = "internal"
)

// classCode maps an HTTP status to its fallback code. Every status writeErr is
// called with must answer something a client can branch on.
func classCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return ClassBadRequest
	case http.StatusUnauthorized:
		return ClassAuthRequired
	case http.StatusForbidden:
		return ClassForbidden
	case http.StatusNotFound:
		return ClassNotFound
	case http.StatusConflict:
		return ClassConflict
	case http.StatusRequestEntityTooLarge:
		return ClassPayloadTooLarge
	case http.StatusTooManyRequests:
		return ClassTooManyRequests
	case http.StatusUnprocessableEntity:
		return ClassUnprocessable
	case http.StatusNotImplemented:
		return ClassNotImplemented
	case http.StatusBadGateway:
		return ClassUpstreamFailed
	case http.StatusServiceUnavailable:
		return ClassUnavailable
	default:
		return ClassInternal
	}
}

// errCodeByMessage maps a wire message to its specific code. The key is the
// EXACT literal (or literal prefix — see errCodeByPrefix) the handler passes to
// writeErr. errcode_test.go fails when a key no longer appears as a Go string
// literal anywhere in this package, so a reworded message cannot silently leave
// a stale code behind, and a table entry cannot be dead weight.
//
// Two different messages may share one code — several handlers answer the same
// user-facing complaint with their own wording, and the code table is keyed on
// the text, so each distinct wording needs its own row.
var errCodeByMessage = map[string]string{
	// Tenancy / workspace scope — the reported case is the first entry.
	"workspace not found": CodeWorkspaceNotFound,
	"not your workspace":  CodeWorkspaceNotFound,

	"slug already in use":       CodeWorkspaceSlugTaken,
	"slug 3-40 characters":      CodeWorkspaceSlugBad,
	"slug: a-z, 0-9, dash only": CodeWorkspaceSlugBad,
	"invalid slug":              CodeWorkspaceSlugBad,

	// Roles / key scope.
	"you need the manage or admin role":       CodeRoleManageRequired,
	"you need the workspace admin role":       CodeRoleAdminRequired,
	"insufficient scope (readwrite required)": CodeScopeReadonly,

	// The device consent card's access-level refusal (card t_cf45b84c). Keyed on
	// the constant oauth_device.go writes, so the wording and this row cannot
	// drift apart silently.
	deviceScopeInvalidMessage: CodeDeviceScopeInvalid,

	// The chat lane's ambiguous-scope refusal (t_fb393abe). Keyed on the exact
	// literal rest_scope.go writes for scopeAmbiguous. Without this entry the
	// body carried only the class code `request.badRequest`, which the dashboard
	// deliberately does not translate — so the page could not tell this refusal
	// from any other 400 and rendered the scope copy for all of them.
	//
	// The literal is declared as a named const in rest_scope.go and referenced
	// from here by its name's VALUE: errcode_test.go requires every key to appear
	// as a Go string literal in a non-errcode.go file of this package, which the
	// const's own declaration site satisfies exactly once — so the wire wording
	// and this table cannot drift apart silently.
	"a workspace scope is required — pick a workspace first": CodeWorkspaceScopeRequired,

	// The form-create refusal for an account with NO workspace (card
	// t_6c0badbe). Keyed on the constant rest_handlers.go writes, so the code
	// and the sentence cannot drift apart.
	workspaceRequiredMessage: CodeWorkspaceRequired,

	// The chat's "this card cannot be carried out" refusal (t_623676d1). Keyed on
	// the exact literal rest_chat.go's chatConfirmMessageFor writes (see
	// CodeProposalArgsInvalid for why it is not the 409 class code). Without this
	// row the page would see `request.conflict` and print "expired or was already
	// used" for a proposal that was neither — and, worse, would not learn that the
	// token is still live.
	"that proposal cannot be carried out — ask the assistant to prepare a new card": CodeProposalArgsInvalid,

	// Payments / CHIP / subscriptions.
	"CHIP is not configured for this account":               CodePaymentNotConfigured,
	"payment is not active":                                 CodePaymentInactive,
	"invalid credentials — check the Brand ID / Secret Key": CodeChipCredentialsBad,
	"the subscription has been cancelled":                   CodeSubscriptionCancelled,

	// Form / product / template validation.
	"status must be draft|private|published":     CodeFormStatusBad,
	"the password must be at least 4 characters": CodeFormPasswordTooShort,
	"product is sold out (out of stock)":         CodeProductOutOfStock,
	"a template needs a category and one description block (Section or text) before it can be published to the gallery": CodeTemplateNeedMetadata,

	// Trash refusals. These two are assembled around a literal, so they are keyed
	// by the exact wire message the handler writes.
	"cannot be restored: this form's slug has been taken by another form — " +
		"change that form's slug first, then try restoring again": CodeFormSlugTaken,
	"this form has sales — sales history cannot be destroyed " +
		"by deleting the form. It stays in the bin.": CodeFormHasSales,

	// Form-slug RENAME refusals (the two a PATCH /forms/{id} slug write can
	// answer). A collision and a malformed value are both refusals the merchant
	// can act on, and both are written here by the handler that maps the store's
	// sentinel error, so the key is the exact wire message.
	"the slug is already used by another form in this workspace": CodeFormSlugInUse,
	// dbstore.ErrInvalidSlug's own text — the store's sentinel IS the rule, so the
	// wire message states it rather than inventing a second wording. The literal
	// is pinned equal to the sentinel by TestFormSlugInvalidWireMessageMatchesTheStore.
	"form slug must be 1-40 characters of a-z, 0-9 or -": CodeFormSlugInvalid,

	// The product bin's refusals, same shape as the form's above.
	"this product has sales — sales history cannot be destroyed by deleting the product. It stays in the bin.": CodeProductHasSales,
	"this product still has subscriptions — cancel that subscription first. It stays in the bin.":              CodeProductHasSubscriptions,

	// Outbound webhook URL (ssrfguard refusal).
	"invalid webhook url (points to a private/loopback host)": CodeWebhookURLInvalid,

	// Custom-domain TLS material.
	"invalid cert/key — check the PEM format and that the key matches": CodeDomainCertInvalid,
	"invalid certificate": CodeDomainCertInvalid,

	// Uploads / media.
	"workspace storage limit reached (1GB)":                                    CodeUploadStorageQuota,
	"unsupported file type (png/jpeg/webp/gif/css/js/html/svg/woff2/pdf only)": CodeUploadTypeUnsupported,
	"unsupported image type (png/jpeg/webp/gif only)":                          CodeUploadTypeUnsupported,
	"unsupported file type (css/js/html/svg/woff2 only)":                       CodeUploadTypeUnsupported,

	// Throttling has no exact-key entry any more: the buyer 429
	// ("... terlalu banyak cubaan ...") is a refusal from the catalogue, so it
	// answers its own code (order.rateLimited) instead of a class code, and the
	// dashboard's 429s are the assembled chat/confirm shapes below, which match
	// by prefix.

	// Account / auth (login, register, profile).
	"email already registered":               CodeEmailTaken,
	"email already in use":                   CodeEmailTaken,
	"invalid email":                          CodeEmailInvalid,
	"the email domain does not accept email": CodeEmailDomainNoMX,
	"please verify your email before signing in (check your inbox for the verification link)": CodeEmailUnverified,
	"the password must be at least 10 characters":                                             CodePasswordTooShort,
	"the new password must be at least 10 characters":                                         CodePasswordTooShort,
	"wrong password":        CodePasswordInvalid,
	"invalid login details": CodeLoginInvalid,

	// Social connect ownership link (social.go). Note "akaun tidak dijumpai" is
	// deliberately ABSENT: that literal is shared with the email-verify and
	// password-recovery handlers (verify.go, auth_ux.go, auth_recovery.go), so
	// naming a code for it would also change those endpoints' wire contract — a
	// different surface with its own pages.
	"invalid connect link":                          CodeConnectInvalid,
	"the connect link has expired":                  CodeConnectExpired,
	"identity is already linked to another account": CodeConnectOtherLinked,
	"an account for this address no longer exists":  CodeConnectNoAccount,

	// "No token in the request", one entry per surface (issue #6177 follow-up).
	// These four were ONE shared literal ("token diperlukan") until each handler
	// got its own wording — the point of the split is that a shared wire message
	// could not identify the surface, so no single code could give the right
	// sentence. errcode_test.go's dead-key guard fails if a key here stops
	// appearing as a literal in a handler, so the split cannot silently revert.
	"verification token is required": CodeVerifyTokenMissing,
	"reset token is required":        CodeResetTokenMissing,
	"sign-in token is required":      CodeMagicTokenMissing,
	"connect token is required":      CodeConnectTokenMissing,
}

// errCodeByPrefix handles messages the handler assembles around a literal, so
// the literal itself is only a prefix. The longest matching prefix wins.
var errCodeByPrefix = []struct {
	prefix string
	code   string
}{
	// urlguard.InvalidMessage(field) → "<field> mesti bermula dengan https:// — …"
	{" must start with https://", CodeURLNotHTTPS},
	// The shipping zone-set delete refusal (card t_e11bf975): assembled around
	// the set name and the form titles that still reference it, so the lead-in
	// is the only fixed part. Keyed on dbstore's own constant rather than a
	// hand-typed copy of it, so a re-wording cannot detach the code again.
	{prefix: dbstore.ShippingSetInUseLeadIn, code: CodeShippingSetInUse},
	// The shipping-zone-set SHAPE refusals (card t_417fbacb). ValidateShippingSets
	// assembles each sentence around the set/zone at fault, and wraps every one
	// with this lead-in, so no exact key can match. Keyed on dbstore's constant
	// for the same reason as the row above: a re-wording is then a compile-time
	// reference, not a silently lost code.
	{prefix: dbstore.ShippingInvalidLeadIn, code: CodeShippingInvalid},
	// payments.CaptureSale/ReleaseSale/Refund refusals are surfaced verbatim.
	{"the sale is not in a refundable status", CodeRefundNotAllowed},
	// The workspace-ROLE refusal, keyed on the LEAD-IN rather than as one more
	// exact literal (card t_1900adef).
	//
	// WHY IT IS NOT AN EXACT KEY. This refusal has TWO spellings in the codebase
	// and the exact key above only ever matched one of them: the REST edge writes
	// "perlu peranan manage atau admin" (rest_scope.go, rest_workspaces.go,
	// rest_legal.go) while the MCP write gates write the same sentence one word
	// longer for the workspace-scoped case ("… dalam workspace ini",
	// mcpsrv.requireWriteRoleIn) — which the CHAT CONFIRM path is the only route
	// to surface. Measured on live prod (v1.19.1): a read_only member's confirmed
	// write was refused by that gate, the message missed the exact key, ErrCodeFor
	// answered "", and the 400 shipped as `request.badRequest` — a code the
	// dashboard deliberately does not translate, so the page could not name the
	// cause and printed its generic failure copy instead. #6177's rule failing in
	// the one place it was fixed.
	//
	// The prefix is the value of the constant the GATES themselves build their
	// sentence from, so the pairing is a compile-time reference and a re-wording
	// cannot silently detach the code again. Only messages that are a role refusal
	// contain the phrase, and the longest-match rule above cannot let it claim a
	// message keyed more precisely elsewhere.
	{prefix: mcpsrv.RoleManageRequiredLeadIn, code: CodeRoleManageRequired},
	// The chat's 429 bodies are BUILT with fmt.Sprintf around the bucket that
	// enforces them (rest_chat.go), so no exact key can ever match them: the
	// wire message is "had chat dicapai (60 mesej sejam). …" and the number in
	// it follows chatRatePerHour. The prefix keeps the code stable across a
	// limit change, and the number itself is deliberately NOT written here —
	// a second copy of it in this table would be one more thing to drift.
	{"chat limit reached (", CodeRateLimited},
	{"verification limit reached (", CodeRateLimited},
	// The workspace NAME cap, forwarded from dbstore.ErrWorkspaceNameLength. A
	// prefix rather than an exact key because the store owns the wording and
	// states the cap in it ("workspace name maximum 120 characters"); matching
	// the lead-in means a future re-wording that keeps the sentence's shape keeps
	// its code, and #6177's rule (the code decides, not the status) still holds.
	{"workspace name maximum ", CodeWorkspaceNameTooLong},
	// The TYPED-CONFIRMATION refusals (the three codes above). Each prefix is
	// EXACTLY the first string fragment the handler writes, before the value it
	// interpolates — `"taip slug borang ("` in rest_handlers.go's purgeForm is
	// literally `"taip slug borang ("+f.Slug+")…"` — so the code follows the
	// sentence's LEAD-IN and is unaffected by the value the caller mistyped: a
	// name of "Kopi (Ais) — 500ml" cannot change which code comes back, and no
	// second copy of the value lives in this table to drift.
	//
	// Longest-match keeps the three apart (they share no lead-in), and the two
	// workspace messages collapse onto one prefix on purpose — see
	// CodeWorkspaceConfirmSlugMismatch.
	{"type the form slug (", CodeFormConfirmSlugMismatch},
	{"type the workspace slug (", CodeWorkspaceConfirmSlugMismatch},
	{"type the product name (", CodeProductConfirmNameMismatch},
	// The custom_js scan refusal (card t_2717ba30). A PREFIX because the
	// sentence is assembled around the scan's own reason — "custom_js rejected:
	// active code execution marker: eval(", "… obfuscation/beacon marker: atob(",
	// "… reference to external URL from custom JS" — so no exact key can ever
	// match one. The prefix is the constant EVERY write site builds the sentence
	// from: rest_handlers.go's createForm/updateForm and rest_templates.go's
	// refuseBlockedCustomJS (REST form create/update, template create/update/use)
	// and the MCP form/template tools, whose results are plain text and therefore
	// pair on this same wording. Referencing the constant is what makes a
	// re-wording a compile-time-visible change rather than a silent loss of the
	// code. The reason text is NOT reproduced here: it belongs to the scan, and a
	// second copy would drift.
	{prefix: customform.RejectedLeadIn, code: CodeFormCustomJSRejected},
	// The SVG well-formedness refusal (t_20af3915). A prefix because the message
	// carries the decoder's own diagnostic after the lead-in ("…: teks sebelum
	// elemen akar" / "…: XML syntax error on line 1: …"), which names the line
	// the merchant has to fix and is therefore not a fixed literal. The lead-in
	// is the constant BOTH the REST handlers and the MCP tool build the sentence
	// from, so the wire message and this code cannot drift apart.
	{prefix: objstore.SVGNotXMLMessageLeadIn, code: CodeUploadSVGInvalid},
}

// ErrCodeFor returns the machine code for a wire message, or "" when the
// message has no specific code (the caller then uses its HTTP class code).
// Exported so tests and the parity tooling can ask the same question the error
// path answers.
func ErrCodeFor(msg string) string {
	if c, ok := errCodeByMessage[msg]; ok {
		return c
	}
	bestLen, best := 0, ""
	for _, p := range errCodeByPrefix {
		if len(p.prefix) > bestLen && strings.Contains(msg, p.prefix) {
			bestLen, best = len(p.prefix), p.code
		}
	}
	return best
}

// codeFor is the error path's own lookup: the specific code when one exists,
// otherwise the class code for the status being written.
//
// A lifecycle refusal names its own stable code at the end of the message
// ("… (lifecycle.illegalTransition)"), because that code belongs to the state
// machine rather than to this door: REST, MCP and any future surface refuse the
// same move, and the dashboard translates by code. Reading it off the message
// keeps one source (the typed error in internal/lifecycle) instead of a second
// table entry that could drift from it.
func codeFor(status int, msg string) string {
	if c := ErrCodeFor(msg); c != "" {
		return c
	}
	for _, machine := range []string{lifecycle.CodeIllegalTransition, lifecycle.CodeTransitionRefused} {
		if strings.Contains(msg, "("+machine+")") {
			return machine
		}
	}
	return classCode(status)
}
