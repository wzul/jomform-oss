// Form restrictions (issue #207): schedule (start/end datetime) + entry limit.
// Gravity-Forms-style — a form can be scheduled to open/close and capped at a
// number of entries. The public page shows a pending/expired/limit message
// instead of the form, and the submit handlers refuse a submission for the same
// window and the same limit through the same ONE derived predicate
// (submitRestriction ← lifecycle.FormOrders), so a page and a POST cannot
// disagree about whether this form is taking a response right now (card
// t_305be5b1).
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

// submitRestriction answers the submit side's question — "may this POST store a
// response right now" — with the SAME ONE derived predicate the public page
// asked when it rendered (lifecycle.FormOrders). It returns the restriction
// state to refuse with, and false when the submission may proceed.
//
// Card t_305be5b1: before this the submit path checked the entry limit and
// NOTHING else, so a form whose window had closed (or had not opened yet) still
// stored a submission through a direct POST while the page, the REST read and
// the MCP read all refused it. The window is the arm this adds; the entry limit
// keeps its answer, now from the same predicate instead of a second count.
//
// The two PRODUCT codes are deliberately NOT refused here. A payment form with
// nothing sellable is refused by the order core's own cart validation, and this
// gate must not take that rule over — the card that added the state
// (t_7d4c20c8) is about the page, not about re-wording an empty-cart 400 into a
// 409. "not published" and "trashed" are not here either: a draft answers 404
// and a trashed row never resolves, both before this runs.
func (h *PublicHandlers) submitRestriction(ctx context.Context, f *dbstore.Form) (restrictionState, bool) {
	order := h.formOrder(ctx, f)
	if lifecycle.FormOrderIsProductCode(order.Code) {
		return restrictionOpen, false
	}
	return restrictionStateForOrder(order.Code)
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
// place. The submit side asks that same predicate too (submitRestriction), which
// is why the last helper that judged the window on its own — scheduleState, whose
// only caller was the deleted render check — is gone as well (card t_305be5b1).
// There is no window rule in this package any more: there is one predicate, and
// every reader asks it about the instant it cares about.
