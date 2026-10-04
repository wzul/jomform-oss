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
//		{"error":"workspace tidak dijumpai","code":"workspace.notFound"}
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
	// "server menolak: %s"). The web page branched on the bare status, so a
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

	// The product bin's two refusals, the same class as the two above: a purge
	// that would destroy sales history, and one a live subscription still needs
	// (the FK is ON DELETE RESTRICT). Keyed on the exact wire message.
	CodeProductHasSales         = "product.hasSales"
	CodeProductHasSubscriptions = "product.hasSubscriptions"

	// Caller-supplied URL scheme (urlguard) and outbound webhook URLs.
	CodeURLNotHTTPS       = "url.notHttps"
	CodeWebhookURLInvalid = "webhook.urlInvalid"

	// Uploads / media.
	CodeUploadStorageQuota    = "upload.storageQuota"
	CodeUploadTypeUnsupported = "upload.typeUnsupported"

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
// Two different messages may share one code: "email tidak sah" and "emel tidak
// sah" are the same user-facing complaint.
var errCodeByMessage = map[string]string{
	// Tenancy / workspace scope — the reported case is the first entry.
	"workspace tidak dijumpai": CodeWorkspaceNotFound,
	"bukan workspace anda":     CodeWorkspaceNotFound,

	"slug sudah digunakan":        CodeWorkspaceSlugTaken,
	"slug 3-40 aksara":            CodeWorkspaceSlugBad,
	"slug: a-z, 0-9, dash sahaja": CodeWorkspaceSlugBad,
	"slug tidak sah":              CodeWorkspaceSlugBad,

	// Roles / key scope.
	"perlu peranan manage atau admin":         CodeRoleManageRequired,
	"perlu admin workspace":                   CodeRoleAdminRequired,
	"scope tidak mencukupi (perlu readwrite)": CodeScopeReadonly,

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
	"skop workspace diperlukan — pilih workspace dahulu": CodeWorkspaceScopeRequired,

	// The chat's "this card cannot be carried out" refusal (t_623676d1). Keyed on
	// the exact literal rest_chat.go's chatConfirmMessageFor writes (see
	// CodeProposalArgsInvalid for why it is not the 409 class code). Without this
	// row the page would see `request.conflict` and print "expired or was already
	// used" for a proposal that was neither — and, worse, would not learn that the
	// token is still live.
	"cadangan itu tidak boleh dilaksanakan — minta pembantu menyediakan kad baharu": CodeProposalArgsInvalid,

	// Payments / CHIP / subscriptions.
	"chip belum dikonfigurasi akaun ini":                 CodePaymentNotConfigured,
	"pembayaran tidak aktif":                             CodePaymentInactive,
	"kredensial tidak sah — semak Brand ID / Secret Key": CodeChipCredentialsBad,
	"langganan telah dibatalkan":                         CodeSubscriptionCancelled,

	// Form / product / template validation.
	"status mesti draft|private|published":                                                   CodeFormStatusBad,
	"password mesti sekurang-kurangnya 4 aksara":                                             CodeFormPasswordTooShort,
	"produk telah habis (out of stock)":                                                      CodeProductOutOfStock,
	"template perlu category dan satu blok text (description) sebelum diterbitkan ke galeri": CodeTemplateNeedMetadata,

	// Trash refusals. These two are assembled around a literal, so they are keyed
	// by the exact wire message the handler writes.
	"tidak boleh dipulihkan: slug borang ini telah diambil borang lain — " +
		"tukar slug borang itu dahulu, kemudian cuba pulihkan semula": CodeFormSlugTaken,
	"borang ini mempunyai jualan — sejarah jualan tidak boleh dimusnahkan " +
		"dengan memadam borang. Ia kekal dalam tong sampah.": CodeFormHasSales,

	// Form-slug RENAME refusals (the two a PATCH /forms/{id} slug write can
	// answer). A collision and a malformed value are both refusals the merchant
	// can act on, and both are written here by the handler that maps the store's
	// sentinel error, so the key is the exact wire message.
	"slug sudah digunakan oleh borang lain dalam workspace ini": CodeFormSlugInUse,
	// dbstore.ErrInvalidSlug's own text — the store's sentinel IS the rule, so the
	// wire message states it rather than inventing a second wording. The literal
	// is pinned equal to the sentinel by TestFormSlugInvalidWireMessageMatchesTheStore.
	"form slug must be 1-40 characters of a-z, 0-9 or -": CodeFormSlugInvalid,

	// The product bin's refusals, same shape as the form's above.
	"produk ini mempunyai jualan — sejarah jualan tidak boleh dimusnahkan dengan memadam produk. Ia kekal dalam tong sampah.": CodeProductHasSales,
	"produk ini masih ada langganan — batalkan langganan itu dahulu. Ia kekal dalam tong sampah.":                             CodeProductHasSubscriptions,

	// Outbound webhook URL (ssrfguard refusal).
	"url webhook tidak sah (menunjuk ke host peribadi/loopback)": CodeWebhookURLInvalid,

	// Custom-domain TLS material.
	"cert/key tidak sah — semak format PEM dan padanan kunci": CodeDomainCertInvalid,
	"cert tidak sah": CodeDomainCertInvalid,

	// Uploads / media.
	"had storan workspace dicapai (1GB)":                                             CodeUploadStorageQuota,
	"jenis fail tidak disokong (png/jpeg/webp/gif/css/js/html/svg/woff2/pdf sahaja)": CodeUploadTypeUnsupported,
	"jenis imej tidak disokong (png/jpeg/webp/gif sahaja)":                           CodeUploadTypeUnsupported,
	"jenis fail tidak disokong (css/js/html/svg/woff2 sahaja)":                       CodeUploadTypeUnsupported,

	// Throttling.
	"terlalu banyak cubaan. Cuba lagi kemudian.": CodeRateLimited,

	// Account / auth (login, register, profile).
	"email sudah didaftarkan":         CodeEmailTaken,
	"emel sudah didaftarkan":          CodeEmailTaken,
	"email sudah digunakan":           CodeEmailTaken,
	"email tidak sah":                 CodeEmailInvalid,
	"emel tidak sah":                  CodeEmailInvalid,
	"domain emel tidak menerima emel": CodeEmailDomainNoMX,
	"sila sahkan email anda sebelum log masuk (semak peti masuk untuk pautan pengesahan)": CodeEmailUnverified,
	"kata laluan mesti sekurang-kurangnya 10 aksara":                                      CodePasswordTooShort,
	"kata laluan baharu mesti sekurang-kurangnya 10 aksara":                               CodePasswordTooShort,
	"kata laluan salah":            CodePasswordInvalid,
	"maklumat log masuk tidak sah": CodeLoginInvalid,

	// Social connect ownership link (social.go). Note "akaun tidak dijumpai" is
	// deliberately ABSENT: that literal is shared with the email-verify and
	// password-recovery handlers (verify.go, auth_ux.go, auth_recovery.go), so
	// naming a code for it would also change those endpoints' wire contract — a
	// different surface with its own pages.
	"pautan penghubung tidak sah":                CodeConnectInvalid,
	"pautan penghubung telah tamat tempoh":       CodeConnectExpired,
	"identiti sudah dipautkan ke akaun lain":     CodeConnectOtherLinked,
	"akaun untuk alamat ini tidak dijumpai lagi": CodeConnectNoAccount,

	// "No token in the request", one entry per surface (issue #6177 follow-up).
	// These four were ONE shared literal ("token diperlukan") until each handler
	// got its own wording — the point of the split is that a shared wire message
	// could not identify the surface, so no single code could give the right
	// sentence. errcode_test.go's dead-key guard fails if a key here stops
	// appearing as a literal in a handler, so the split cannot silently revert.
	"token pengesahan diperlukan": CodeVerifyTokenMissing,
	"token set semula diperlukan": CodeResetTokenMissing,
	"token log masuk diperlukan":  CodeMagicTokenMissing,
	"token penghubung diperlukan": CodeConnectTokenMissing,
}

// errCodeByPrefix handles messages the handler assembles around a literal, so
// the literal itself is only a prefix. The longest matching prefix wins.
var errCodeByPrefix = []struct {
	prefix string
	code   string
}{
	// urlguard.InvalidMessage(field) → "<field> mesti bermula dengan https:// — …"
	{" mesti bermula dengan https://", CodeURLNotHTTPS},
	// payments.CaptureSale/ReleaseSale/Refund refusals are surfaced verbatim.
	{"jualan bukan dalam status boleh-refund", CodeRefundNotAllowed},
	// The chat's 429 bodies are BUILT with fmt.Sprintf around the bucket that
	// enforces them (rest_chat.go), so no exact key can ever match them: the
	// wire message is "had chat dicapai (60 mesej sejam). …" and the number in
	// it follows chatRatePerHour. The prefix keeps the code stable across a
	// limit change, and the number itself is deliberately NOT written here —
	// a second copy of it in this table would be one more thing to drift.
	{"had chat dicapai (", CodeRateLimited},
	{"had pengesahan dicapai (", CodeRateLimited},
	// The workspace NAME cap, forwarded from dbstore.ErrWorkspaceNameLength. A
	// prefix rather than an exact key because the store owns the wording and
	// states the cap in it ("nama workspace maksimum 120 aksara"); matching the
	// lead-in means a future re-wording that keeps the sentence's shape keeps its
	// code, and #6177's rule (the code decides, not the status) still holds.
	{"nama workspace maksimum ", CodeWorkspaceNameTooLong},
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
func codeFor(status int, msg string) string {
	if c := ErrCodeFor(msg); c != "" {
		return c
	}
	return classCode(status)
}
