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
	// A workspace DELETE refused because something still lives in it. The refusal
	// names the obstacles (the body's `blockers` array), which is exactly the
	// class of message the page's own generic sentence cannot say.
	CodeWorkspaceNotEmpty = "workspace.notEmpty"

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
	CodeFormSlugInUse = "form.slugInUse"
	CodeFormSlugInvalid = "form.slugInvalid"

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
	"terlalu banyak cubaan. Cuba lagi kemudian.":                  CodeRateLimited,
	"had chat dicapai (60 mesej sejam). Sila cuba sebentar lagi.": CodeRateLimited,

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
