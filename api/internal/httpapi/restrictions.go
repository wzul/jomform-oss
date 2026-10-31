// Form restrictions (issue #207): schedule (start/end datetime) + entry limit.
// Gravity-Forms-style — a form can be scheduled to open/close and capped at a
// number of entries. The public renderer shows a pending/expired/limit message
// instead of the form; the submit handlers reject submissions past the limit.
// Card t_7d4c20c8 added a fourth, non-time state (a payment form with no
// product to sell) because it needs exactly what these already render: a
// standalone message and no submit control.
package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// restrictionState classifies a form's current availability.
type restrictionState int

const (
	restrictionOpen         restrictionState = iota
	restrictionPending                       // before start_at
	restrictionExpired                       // after end_at
	restrictionLimitReached                  // entry limit reached
	// Card t_7d4c20c8: a published PAYMENT form with no product to sell. Not a
	// time/limit state — the form is unsubmittable until the merchant adds a
	// product, because neither payment layout has a submit path without one:
	// the legacy one's product checkboxes, and (card t_bfdcafbc) the block
	// layout's `if(!items.length){alert(...);return;}` gate, which is why the
	// dispatch asks this state BEFORE the block renderer too. Served instead of
	// the payment page, so the buyer is told the truth instead of being handed
	// a button whose only outcome is "Please select at least one product".
	restrictionNoProducts
)

// scheduleState returns pending/expired/open based on the form's schedule
// window. A form with no start_at/end_at is always open.
func scheduleState(f *dbstore.Form, now time.Time) restrictionState {
	if f.StartAt != nil && now.Before(*f.StartAt) {
		return restrictionPending
	}
	if f.EndAt != nil && now.After(*f.EndAt) {
		return restrictionExpired
	}
	return restrictionOpen
}

// entryLimitReached reports whether the form's entry limit has been reached.
// For payment forms the count is sales; for response forms it is submissions.
func (h *PublicHandlers) entryLimitReached(ctx context.Context, f *dbstore.Form) (bool, error) {
	if f.EntryLimit == nil || *f.EntryLimit <= 0 {
		return false, nil
	}
	var count int64
	var err error
	if formMode(f) == dbstore.FormModeResponse {
		if h.Submissions == nil {
			return false, nil
		}
		count, err = h.Submissions.CountByAccountsForm(ctx, []string{f.AccountID}, f.ID)
	} else {
		if h.Sales == nil {
			return false, nil
		}
		count, err = h.Sales.CountByAccountsForm(ctx, []string{f.AccountID}, f.ID)
	}
	if err != nil {
		return false, err
	}
	return count >= int64(*f.EntryLimit), nil
}

// restrictionMessage resolves the buyer-facing message for a restriction
// state. A per-form override wins; otherwise the language default is used.
func (h *PublicHandlers) restrictionMessage(r *http.Request, f *dbstore.Form, st restrictionState) string {
	ls := h.effectiveLangStrings(r.Context(), f)
	switch st {
	case restrictionPending:
		if f.PendingMessage != nil && *f.PendingMessage != "" {
			return *f.PendingMessage
		}
		return ls.PendingMessage
	case restrictionExpired:
		if f.ExpiredMessage != nil && *f.ExpiredMessage != "" {
			return *f.ExpiredMessage
		}
		return ls.ExpiredMessage
	case restrictionLimitReached:
		if f.LimitMessage != nil && *f.LimitMessage != "" {
			return *f.LimitMessage
		}
		return ls.LimitMessage
	case restrictionNoProducts:
		// No per-form override: the merchant never wrote this message — the
		// form simply has nothing to sell, and the sentence says so.
		return ls.NoProductsMessage
	}
	return ""
}

// renderRestriction renders a standalone message page in place of the form
// (schedule pending/expired, entry-limit-reached, or — card t_7d4c20c8 — a
// payment form with no product to sell). No submit control is painted on it,
// which is the point: every state here means the form cannot be submitted.
func (h *PublicHandlers) renderRestriction(w http.ResponseWriter, r *http.Request, f *dbstore.Form, st restrictionState) {
	msg := h.restrictionMessage(r, f, st)
	title := f.Title
	ls := h.effectiveLangStrings(r.Context(), f)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Issue #537: f.Title is merchant-controlled TEXT, so it MUST be escaped —
	// this is a raw fmt.Fprintf (no html/template autoescaping), and an
	// unescaped title let a `</title><script>` break out and execute on the
	// buyer's page. msg/lang come from server-side maps, but they ride through
	// the same helper so no future edit can reintroduce a raw sink.
	fmt.Fprintf(w, `<!doctype html><html lang="%s"%s><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title>
<style>body{background:#f7f8f8;color:#17181c;font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;padding:24px}
.c{background:#ffffff;border:1px solid #e6e8ec;border-radius:16px;padding:40px;text-align:center;max-width:440px;width:100%%}
h1{font-size:20px;margin:0 0 8px}.m{color:#6b7280;font-size:14px;line-height:1.6;margin:0}
.f{color:#9ca3af;font-size:12px;margin-top:20px}</style></head><body><div class="c">
<h1>%s</h1><p class="m">%s</p>
<p class="f">%s</p></div></body></html>`,
		htmlEsc(ls.Lang), dirAttr(ls.Lang), htmlEsc(title), htmlEsc(title), htmlEsc(msg),
		htmlEsc(ls.PoweredBy))
}

// checkRestrictionsRender returns the restriction state that should block the
// form from rendering (restrictionOpen when the form may render normally).
// Schedule is checked first, then the entry limit.
func (h *PublicHandlers) checkRestrictionsRender(r *http.Request, f *dbstore.Form) restrictionState {
	if st := scheduleState(f, time.Now()); st != restrictionOpen {
		return st
	}
	if reached, err := h.entryLimitReached(r.Context(), f); err == nil && reached {
		return restrictionLimitReached
	}
	return restrictionOpen
}
