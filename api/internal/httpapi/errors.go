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
	"github.com/wzul/jomform/api/internal/payments"
)

// errorClass buckets an error into a stable, low-cardinality class used for
// both the structured log field and the Prometheus error counter.
func errorClass(err error) string {
	switch {
	case errors.Is(err, dbstore.ErrFormNotFound),
		errors.Is(err, dbstore.ErrProductNotFound),
		errors.Is(err, dbstore.ErrAPIKeyInvalid),
		errors.Is(err, payments.ErrNotFound):
		return "not_found"
	case errors.Is(err, dbstore.ErrEmailTaken):
		return "conflict"
	case errors.Is(err, dbstore.ErrOutOfStock):
		return "conflict"
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

// respondErr maps an error to a user-facing response. User-actionable errors
// get a clear localized message; everything else is a generic 500 with the
// real cause logged (never leaked to the client).
func respondErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, dbstore.ErrFormNotFound),
		errors.Is(err, dbstore.ErrProductNotFound),
		errors.Is(err, dbstore.ErrAPIKeyInvalid),
		errors.Is(err, payments.ErrNotFound):
		writeErr(w, http.StatusNotFound, "tidak dijumpai")
	case errors.Is(err, dbstore.ErrEmailTaken):
		writeErr(w, http.StatusConflict, "emel sudah didaftarkan")
	case errors.Is(err, dbstore.ErrOutOfStock):
		writeErr(w, http.StatusConflict, "produk telah habis (out of stock)")
	case errors.Is(err, dbstore.ErrTemplateGalleryMetadata):
		// Issue #594: publishing a template into the gallery needs the two
		// fields its card renders. Refused, with the reason, rather than
		// shipping a card with no category and no description.
		writeErr(w, http.StatusBadRequest,
			"template perlu category dan satu blok text (description) sebelum diterbitkan ke galeri")
	case errors.Is(err, payments.ErrNotConfigured):
		writeErr(w, http.StatusBadRequest, "chip belum dikonfigurasi akaun ini")
	default:
		logError(r, "internal_error", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}
