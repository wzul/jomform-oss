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
	"github.com/wzul/jomform/api/internal/lifecycle"
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
//
// It is kept for the submit-side checks, which ask about the WINDOW alone; the
// RENDER side no longer decides with it — serveForm asks lifecycle.FormOrders,
// whose answer folds the window in with the trash, the entry limit and the
// product rule, so the page and every read surface agree (card t_3d5a8d32).
func scheduleState(f *dbstore.Form, now time.Time) restrictionState {
	if f.StartAt != nil && now.Before(*f.StartAt) {
		return restrictionPending
	}
	if f.EndAt != nil && now.After(*f.EndAt) {
		return restrictionExpired
	}
	return restrictionOpen
}

// formEntryCount returns how many orders (payment form) or submissions
// (response form) the form has recorded, or -1 when the store for that mode is
// not wired — which lifecycle.FormOrders reads as "unknown", never as zero.
func (h *PublicHandlers) formEntryCount(ctx context.Context, f *dbstore.Form) int64 {
	accounts := []string{f.AccountID}
	if formMode(f) == dbstore.FormModeResponse {
		if h.Submissions == nil {
			return -1
		}
		n, err := h.Submissions.CountByAccountsForm(ctx, accounts, f.ID)
		if err != nil {
			return -1
		}
		return n
	}
	if h.Sales == nil {
		return -1
	}
	n, err := h.Sales.CountByAccountsForm(ctx, accounts, f.ID)
	if err != nil {
		return -1
	}
	return n
}

// formOrder is the public page's ask of the ONE derived buyer predicate. It
// assembles the facts the row cannot carry — the account's product ids (the
// purged-product arm) and, only when the form HAS an entry limit, the recorded
// count — and hands them to lifecycle.FormOrders.
func (h *PublicHandlers) formOrder(ctx context.Context, f *dbstore.Form) lifecycle.FormOrderState {
	env := lifecycle.FormOrderEnv{Entries: -1, Now: time.Now()}
	if h.Products != nil && f.AccountID != "" {
		if ids, err := h.Products.ProductIDs(ctx, f.AccountID); err == nil {
			env.LiveProductIDs = ids
		}
	}
	if f.EntryLimit != nil && *f.EntryLimit > 0 {
		env.Entries = h.formEntryCount(ctx, f)
	}
	return lifecycle.FormOrders(f, env)
}

// restrictionStateForOrder maps the derived predicate's code onto the message
// page the buyer is served. Every refusal the predicate can report has a state
// here; "not published" and "trashed" cannot reach the render path at all (the
// status checks above answer 404 first), which is why they are not mapped — a
// caller that DOES reach them gets restrictFormGone, and serveForm answers 404.
func restrictionStateForOrder(code string) (restrictionState, bool) {
	switch code {
	case lifecycle.FormOrderScheduled:
		return restrictionPending, true
	case lifecycle.FormOrderClosed:
		return restrictionExpired, true
	case lifecycle.FormOrderEntryLimitReached:
		return restrictionLimitReached, true
	case lifecycle.FormOrderNoProducts, lifecycle.FormOrderNoProductSelected:
		return restrictionNoProducts, true
	}
	return restrictionOpen, false
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

// checkRestrictionsRender answered "should this form render or show a message"
// before card t_3d5a8d32. It is GONE rather than kept alongside the new rule:
// the render decision now comes from lifecycle.FormOrders (see serveForm), and a
// second implementation is how the page and the reads drifted apart in the first
// place. The submit-side checks keep their own helpers — a submission asks about
// the window and the limit at the instant it is posted, which is a different
// question from "what does this form's page show right now".
