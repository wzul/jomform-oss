// Issue #131: subscription management for merchants — list subscriptions
// (account-scoped, paginated, status filter) and cancel one by id. Cancelling
// mirrors the MCP cancel_subscription semantics and NEVER revokes the CHIP
// card token (Wan rule), so a subscription can be reactivated later.
package httpapi

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/wzul/jomform/api/internal/dbstore"
)

type subscriptionRow struct {
	ID            string `json:"id"`
	Customer      string `json:"customer"` // client email
	ProductID     string `json:"product_id"`
	ProductName   string `json:"product_name"`
	AmountSen     int64  `json:"amount_sen"`
	Period        string `json:"period"` // month | year
	Status        string `json:"status"` // active | past_due | cancelled
	NextChargeAt  string `json:"next_charge_at"`
	RetryAttempts int64  `json:"retry_attempts"`
	CreatedAt     string `json:"created_at"`
	// Issue #154: true when a charge failed and the card needs updating
	// (re-tokenize). While set, the overdue auto-cancel is paused.
	CardUpdatePending bool `json:"card_update_pending"`
}

func restSubRow(sw *dbstore.SubscriptionWithProduct) subscriptionRow {
	r := subscriptionRow{
		ID: sw.ID, Customer: sw.ClientEmail, ProductID: sw.ProductID,
		ProductName: sw.ProductName, AmountSen: sw.AmountSen, Period: sw.Period,
		Status: sw.Status, RetryAttempts: sw.RetryAttempts,
		CreatedAt:         sw.CreatedAt.UTC().Format(time.RFC3339),
		CardUpdatePending: sw.CardUpdatePending,
	}
	if !sw.NextChargeAt.IsZero() {
		r.NextChargeAt = sw.NextChargeAt.UTC().Format(time.RFC3339)
	}
	return r
}

// GET /v1/rest/subscriptions — list subscriptions for the resolved account
// (or every account in the selected workspace), paginated. Optional ?status=
// (active|past_due|cancelled) and the usual ?page/&limit.
func (h *RESTHandlers) listSubscriptions(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return
	}
	page, limit := parsePageLimit(r)
	status := r.URL.Query().Get("status")
	switch status {
	case "", dbstore.SubActive, dbstore.SubPastDue, dbstore.SubCanceled, dbstore.SubCompleted:
	default:
		writeErr(w, http.StatusBadRequest, "status tidak sah")
		return
	}
	rows, total, err := h.Subs.ListByAccountsPage(r.Context(), scope.Accounts, status, page, limit)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	items := make([]subscriptionRow, 0, len(rows))
	for _, sw := range rows {
		items = append(items, restSubRow(sw))
	}
	respond(w, http.StatusOK, map[string]any{
		"items": items, "page": page, "limit": limit, "total": total,
	})
}

// POST /v1/rest/subscriptions/{id}/cancel — cancel one subscription by id.
// Account-scoped: a subscription id that does not belong to the resolved
// account 404s. Mirrors MCP cancel_subscription (same store method), so the
// card token is retained (Wan rule) and the subscription stays reactivatable.
func (h *RESTHandlers) cancelSubscription(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id diperlukan")
		return
	}
	sub, err := h.Subs.ByIDForAccount(r.Context(), actx.Account.ID, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeErr(w, http.StatusNotFound, "langganan tidak dijumpai")
			return
		}
		respondErr(w, r, err)
		return
	}
	// Never revoke the CHIP token (Wan rule) — only mark the row cancelled.
	if err := h.Subs.Cancel(r.Context(), sub.ID); err != nil {
		respondErr(w, r, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"id": sub.ID, "status": dbstore.SubCanceled})
}

// POST /v1/rest/subscriptions/{id}/update-card — merchant-initiated card
// update on an existing subscription (issue #154). Creates a new CHIP token
// purchase so the customer can authorize a new card; on payment the
// subscription's recurring token is atomically replaced. The old token is
// left in CHIP (Wan rule) but no longer used. Account-scoped: a subscription
// id that does not belong to the resolved account 404s.
func (h *RESTHandlers) updateSubscriptionCard(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id diperlukan")
		return
	}
	sub, err := h.Subs.ByIDForAccount(r.Context(), actx.Account.ID, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeErr(w, http.StatusNotFound, "langganan tidak dijumpai")
			return
		}
		respondErr(w, r, err)
		return
	}
	if sub.Status == dbstore.SubCanceled {
		writeErr(w, http.StatusConflict, "langganan telah dibatalkan")
		return
	}
	if h.Pay == nil {
		writeErr(w, http.StatusServiceUnavailable, "pembayaran tidak aktif")
		return
	}
	p, err := h.Pay.CreateUpdateCardPurchase(r.Context(), sub)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{
		"id": sub.ID, "status": sub.Status,
		"checkout_url": p.CheckoutURL, "purchase_id": p.ID,
	})
}

// POST /v1/rest/subscriptions/{id}/retry — merchant/admin action to revive a
// past_due subscription so the engine charges it again (round 13c).
//
// Why this exists: after 3 failed attempts a subscription became past_due, and
// the due-charge scan only ever selects 'active' — so a past_due subscription
// was never retried again. The only way out was a completed customer card
// update, which meant a merchant could not charge a customer who had topped up
// their account or whose card was temporarily blocked.
//
// This returns the subscription to 'active' and clears card_update_pending. It
// does NOT charge immediately: the next scheduled sweep picks it up, which keeps
// the money movement in one place (the engine) rather than a second path that
// could double-charge. Cancelled and completed subscriptions are refused.
func (h *RESTHandlers) retrySubscription(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id diperlukan")
		return
	}
	sub, err := h.Subs.ByIDForAccount(r.Context(), actx.Account.ID, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeErr(w, http.StatusNotFound, "langganan tidak dijumpai")
			return
		}
		respondErr(w, r, err)
		return
	}
	if err := h.Subs.Reactivate(r.Context(), actx.Account.ID, sub.ID); err != nil {
		if err == dbstore.ErrNotPastDue {
			writeErr(w, http.StatusConflict,
				"hanya langganan past_due boleh dicuba semula (status: "+sub.Status+")")
			return
		}
		respondErr(w, r, err)
		return
	}
	// Make it due now so the next sweep charges it without waiting a full
	// billing period, while a successful charge still corrects the date via the
	// product's schedule mode.
	if err := h.Subs.ScheduleNextCharge(r.Context(), sub.ID, time.Now().Add(-time.Minute)); err != nil {
		respondErr(w, r, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{
		"id": sub.ID, "status": dbstore.SubActive, "retry_queued": true,
	})
}
