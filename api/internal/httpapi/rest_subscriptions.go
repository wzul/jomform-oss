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
}

func restSubRow(sw *dbstore.SubscriptionWithProduct) subscriptionRow {
	r := subscriptionRow{
		ID: sw.ID, Customer: sw.ClientEmail, ProductID: sw.ProductID,
		ProductName: sw.ProductName, AmountSen: sw.AmountSen, Period: sw.Period,
		Status: sw.Status, RetryAttempts: sw.RetryAttempts,
		CreatedAt: sw.CreatedAt.UTC().Format(time.RFC3339),
	}
	if !sw.NextChargeAt.IsZero() {
		r.NextChargeAt = sw.NextChargeAt.UTC().Format(time.RFC3339)
	}
	return r
}

// GET /v1/rest/subscriptions — list subscriptions for the resolved account
// (or every account in the selected space), paginated. Optional ?status=
// (active|past_due|cancelled) and the usual ?page/&limit.
func (h *RESTHandlers) listSubscriptions(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return
	}
	page, limit := parsePageLimit(r)
	status := r.URL.Query().Get("status")
	switch status {
	case "", dbstore.SubActive, dbstore.SubPastDue, dbstore.SubCanceled:
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
