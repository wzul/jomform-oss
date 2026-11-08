// Error masking + structured logging helpers (issue #100).
//
// The rule: user-actionable errors (validation, not-found, conflict,
// rate-limit) get a clear, localized message; internal errors get a generic
// message to the client but full structured detail is logged (error class,
// request id, account id, stack). Internals (SQL, secrets, stack traces) are
// NEVER sent to the client.
package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
	"github.com/wzul/jomform/api/internal/llm"
	"github.com/wzul/jomform/api/internal/payments"
	"github.com/wzul/jomform/api/internal/urlguard"
)

// invalidSlugMessage is the wire message for a rejected form slug. It is
// dbstore.ErrInvalidSlug's own text — the store's sentinel IS the rule (1-40
// characters of a-z, 0-9, dash), so the API states that rule rather than
// inventing a second wording. It is spelled out as a literal (not an
// `.Error()` call) because errcode.go's message→code table requires every key
// to appear as a string literal in a non-errcode.go file of this package;
// TestFormSlugInvalidWireMessageMatchesTheStore pins the two equal so the wire
// wording cannot drift from the store's sentinel.
const invalidSlugMessage = "form slug must be 1-40 characters of a-z, 0-9 or -"

// templateNeedMetadataWire is the wire message for a template publish refused
// for missing gallery metadata (issue #594, t_6a23c70d). It is a named literal
// for the same reason invalidSlugMessage is: errcode.go's message→code table
// keys on the exact text, and it must name BOTH block types the description
// may live in — a Section (or raw-HTML) block, or a Text block written under
// the old content model. A message that said "one text block" would tell the
// merchant to add a field LABEL and publish a card reading "Nama anda".
const templateNeedMetadataWire = "a template needs a category and one description block (Section or text) before it can be published to the gallery"

// The workspace slug rule's wire messages, declared HERE as literals for the
// same reason invalidSlugMessage is (above): errcode.go's message→code table
// requires every key to appear as a string literal in a non-errcode.go file of
// this package, so a handler that forwards dbstore's sentinel text verbatim
// would leave the three slug codes unreachable — every shape refusal would ship
// with a class code and the dashboard would render its generic sentence instead
// of "Slug must be 3-40 chars, a-z, 0-9, dash.".
//
// Each string IS the text of the matching dbstore sentinel (the store's
// sentinel is the rule's wording), and TestWorkspaceSlugWireMessagesAreTheStores
// pins the two equal so the wire wording cannot drift from the store's.
const (
	workspaceSlugLengthMessage = "slug 3-40 characters"
	workspaceSlugCharsMessage  = "slug: a-z, 0-9, dash only"
	workspaceSlugTailMessage   = "invalid slug"
)

// errorClass buckets an error into a stable, low-cardinality class used for
// both the structured log field and the Prometheus error counter.
func errorClass(err error) string {
	switch {
	case errors.Is(err, dbstore.ErrFormNotFound),
		errors.Is(err, dbstore.ErrProductNotFound),
		errors.Is(err, dbstore.ErrAPIKeyInvalid),
		errors.Is(err, dbstore.ErrWebhookNotFound),
		errors.Is(err, dbstore.ErrWorkspaceNotFound),
		errors.Is(err, payments.ErrNotFound):
		return "not_found"
	case errors.Is(err, dbstore.ErrEmailTaken):
		return "conflict"
	case errors.Is(err, dbstore.ErrOutOfStock):
		return "conflict"
	case errors.Is(err, dbstore.ErrSlugTaken):
		// A form's URL slug is unique per workspace: a rename onto a slug
		// another live form already serves is a conflict the caller can fix
		// (pick another slug), not an internal fault.
		return "conflict"
	case errors.Is(err, dbstore.ErrInvalidSlug):
		return "validation"
	case errors.Is(err, dbstore.ErrFormSlugReserved):
		// A reserved form slug is a policy refusal on caller input, not a fault:
		// the same class the workspace reserved-subdomain refusal lands in.
		return "validation"
	case errors.Is(err, dbstore.ErrTemplateGalleryMetadata):
		// Issue #594: refused for a reason the caller can fix, so it is a
		// validation error, not an internal one.
		return "validation"
	case errors.Is(err, payments.ErrNotConfigured):
		return "not_configured"
	default:
		return "internal"
	}
}

// logError logs an internal error with structured, correlatable fields. It
// never returns anything to the client — callers decide the user-facing
// message. The stack is captured so ops can trace the failure without the
// client ever seeing it.
func logError(r *http.Request, event string, err error) {
	class := errorClass(err)
	slog.Error(event,
		"err", err,
		"error_class", class,
		"request_id", middleware.GetRequestID(r.Context()),
		"account_id", middleware.GetAccountID(r.Context()),
		"path", r.URL.Path,
		"stack", string(debug.Stack()),
	)
	if globalMetrics != nil {
		globalMetrics.IncError(class)
	}
}

// respondAIErr maps a failure from an AI (LLM) call to the caller.
//
// WHY A NAMED MAPPING. A model credential the provider REJECTED (rotated,
// revoked, expired) is not an internal bug — it is a deployment state, and every
// AI surface in this package already answers "unavailable" with 503 when the
// client is absent. Without this, a rejected key turns the same feature into a
// bare 500 on every attempt: the UI's unavailable branch never fires and the
// operator sees a generic internal error instead of "the key is bad". The
// credential case therefore joins the not-configured case, and every other
// failure keeps the standard mapping (500, detail logged, never leaked).
//
// unavailableMsg is the surface's own copy for "the AI is not available", which
// each endpoint already has: one rule, the surface's own words.
func respondAIErr(w http.ResponseWriter, r *http.Request, err error, unavailableMsg string) {
	if errors.Is(err, llm.ErrCredential) {
		logError(r, "llm_credential_rejected", err)
		writeErr(w, http.StatusServiceUnavailable, unavailableMsg)
		return
	}
	if errors.Is(err, llm.ErrModelUnavailable) {
		// Same ANSWER as a rejected key — the feature is unavailable, not broken — but a
		// DIFFERENT reason, so the operator is sent to the right half. A 404/410 is a
		// configuration fault ("the model id is wrong or retired"), never a key problem:
		// logging llm_credential_rejected here would be false and would have an operator
		// rotating a key that works.
		logError(r, "llm_model_unavailable", err)
		writeErr(w, http.StatusServiceUnavailable, unavailableMsg)
		return
	}
	respondErr(w, r, err)
}

// paymentInternalText is the generic sentence a payment-path INTERNAL fault says
// on the wire — the same one respondErr's default branch writes. It exists for
// the bulk-capture results, which are 200-body fields with no status of their own.
const paymentInternalText = "internal error"

// paymentNotConfiguredText is the sentence respondErr writes for
// payments.ErrNotConfigured. errcode.go's table is keyed on this exact literal,
// so the refusal keeps its payment.notConfigured code; it is one constant so the
// door and the bulk-capture per-item text cannot drift.
const paymentNotConfiguredText = "CHIP is not configured for this account"

// writePaymentErr writes a payment operation's failure (capture, release,
// refund, single or bulk).
//
// Every refusal a caller can ACT on keeps its 400 and its own sentence: the
// sale-state guards and CHIP's own rejection text are written to be read by a
// merchant. Two classes do not go through the generic 400: a stored CHIP key
// that will not decrypt (payments.ErrChipKeyUnreadable) is a server-side fault,
// so it takes the generic masked 500 and a logged stack; and the not-configured
// refusal is the door's own sentence, which is what errcode.go keys
// payment.notConfigured on. Before this (card t_0eb36e44) an unconfigured
// account's refund answered 400 "ciphertext too short": an internal detail, and
// no remedy for the merchant.
func writePaymentErr(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, payments.ErrChipKeyUnreadable) || errors.Is(err, payments.ErrNotConfigured) {
		respondErr(w, r, err)
		return
	}
	writeErr(w, http.StatusBadRequest, err.Error())
}

// paymentErrText is writePaymentErr's text-only form, for a per-item result that
// has no status code to set (bulk capture's results[].error).
func paymentErrText(err error) string {
	if errors.Is(err, payments.ErrChipKeyUnreadable) {
		return paymentInternalText
	}
	if errors.Is(err, payments.ErrNotConfigured) {
		return paymentNotConfiguredText
	}
	return err.Error()
}

// respondErr maps an error to a user-facing response. User-actionable errors
// get a clear localized message; everything else is a generic 500 with the
// real cause logged (never leaked to the client).
func respondErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, dbstore.ErrFormNotFound),
		errors.Is(err, dbstore.ErrProductNotFound),
		errors.Is(err, dbstore.ErrAPIKeyInvalid),
		errors.Is(err, dbstore.ErrWebhookNotFound),
		errors.Is(err, dbstore.ErrWorkspaceNotFound),
		errors.Is(err, payments.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	case errors.Is(err, dbstore.ErrEmailTaken):
		writeErr(w, http.StatusConflict, "email already registered")
	case errors.Is(err, dbstore.ErrOutOfStock):
		writeErr(w, http.StatusConflict, "product is sold out (out of stock)")
	case errors.Is(err, dbstore.ErrSlugTaken):
		// A form's URL slug is unique per workspace: a rename onto a slug
		// another live form already serves is refused with the reason, rather
		// than reporting a success that changed nothing (the carded defect).
		//
		// writeErr is the SHARED error path (errcode.go): it attaches the
		// machine `code` the dashboard translates and keeps the prose for
		// API-key/curl/MCP callers. There is deliberately no local
		// writeCodedErr here — one mechanism, so the two surfaces cannot drift.
		writeErr(w, http.StatusConflict, "the slug is already used by another form in this workspace")
	case errors.Is(err, dbstore.ErrInvalidSlug):
		// The slug is not a usable URL segment. The store's sentinel text IS the
		// rule (1-40 chars of a-z, 0-9, dash), so it is the honest wire message;
		// writeErr attaches the code the console translates.
		//
		// The message is written as a LITERAL rather than ErrInvalidSlug.Error()
		// so errcode.go's table can key on it: that table's guard requires every
		// key to appear as a string literal in a non-errcode.go file of this
		// package, which an `.Error()` call does not satisfy. The two are pinned
		// equal by TestFormSlugInvalidWireMessageMatchesTheStore, so the wire
		// wording cannot drift from the store's rule.
		writeErr(w, http.StatusBadRequest, invalidSlugMessage)
	case errors.Is(err, dbstore.ErrFormSlugReserved):
		// Card t_f76f81eb: the slug is well formed but names something JomForm
		// owns or already serves on the same host (/privacy, /login, /api, …).
		// A POLICY refusal, not a shape one, so it says WHY (urlguard's wording,
		// which names the refused label) and carries urlguard's stable
		// `form.slugReserved` token the dashboard renders its own sentence for —
		// the same shape writeWorkspaceSlugReserved uses, and for the same
		// reason: errcode.go's table is keyed on exact message literals, and this
		// message carries the label.
		writeFormSlugReserved(w, err)
	case errors.Is(err, dbstore.ErrTemplateGalleryMetadata):
		// Issue #594: publishing a template into the gallery needs the two
		// fields its card renders. Refused, with the reason, rather than
		// shipping a card with no category and no description.
		//
		// The wording names BOTH block types the description may live in,
		// because that is the rule the store enforces (t_6a23c70d): the intro
		// line is a Section (or raw-HTML) block, and a Text block still counts
		// for a template written before `text` became an input. Telling a
		// merchant "one text block" would send them to add a field LABEL and
		// publish a card describing their form as "Nama anda".
		writeErr(w, http.StatusBadRequest,
			templateNeedMetadataWire)
	case errors.Is(err, payments.ErrNotConfigured):
		writeErr(w, http.StatusBadRequest, paymentNotConfiguredText)
	default:
		logError(r, "internal_error", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}

// writeFormSlugReserved answers the reserved-form-slug refusal (card
// t_f76f81eb): the caller asked for a slug JomForm owns or already serves on the
// same host. It is a POLICY answer, not a shape one, so the wire body says WHY —
// urlguard's wording, which names the refused label — and carries the stable
// `form.slugReserved` token the dashboard matches on instead of the generic
// slug rule code.
//
// ONE writer for both REST write paths (POST /forms, PATCH /forms/{id}); both
// reach it through respondErr, so the two halves of the surface cannot answer
// the same bad input differently. Mirrors writeWorkspaceSlugReserved
// (rest_workspaces.go), which exists for the same reason on the host namespace.
//
// The `error` value is the rule's own text (it carries the label), never a
// second sentence written here.
func writeFormSlugReserved(w http.ResponseWriter, verr error) {
	writeJSON(w, http.StatusBadRequest, map[string]any{
		"error": verr.Error(),
		"code":  urlguard.FormSlugReservedCode,
	})
}
