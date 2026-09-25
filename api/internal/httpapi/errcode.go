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
