// REST endpoints for the AI spam review queue (issue #245).
//
// The merchant sees spam-flagged submissions and sales in a review queue and
// can un-spam a false positive (or confirm spam — confirming is implicit:
// leaving it flagged keeps it in the queue and subject to the 30-day
// retention sweep). A spam-flagged sale that receives a successful payment is
// automatically un-spammed by the payment callback.
package httpapi

import (
	"net/http"
	"time"
)

// restSpamSubmission is the REST JSON shape of one spam-flagged submission.
type restSpamSubmission struct {
	ID         string         `json:"id"`
	FormID     string         `json:"form_id"`
	Data       map[string]any `json:"data"`
	SpamReason string         `json:"spam_reason,omitempty"`
	CreatedAt  string         `json:"created_at"`
}

// restSpamSale is the REST JSON shape of one spam-flagged sale.
//
// Currency is the currency the sale was RECORDED in (card t_d5f359c7): the spam
// queue lists real sales, so their totals are labelled per row like every other
// sale surface — never with the workspace's current setting, which would
// relabel history after a currency switch (issue #185).
type restSpamSale struct {
	RefCode    string `json:"ref_code"`
	Status     string `json:"status"`
	TotalSen   int64  `json:"total_sen"`
	Currency   string `json:"currency"`
	Buyer      string `json:"buyer"`
	BuyerEmail string `json:"buyer_email,omitempty"`
	FormID     string `json:"form_id,omitempty"`
	SpamReason string `json:"spam_reason,omitempty"`
	CreatedAt  string `json:"created_at"`
}

// listSpamSubmissions handles GET /v1/rest/spam/submissions — account-scoped,
// paginated, newest first. Returns { items, page, limit, total }.
func (h *RESTHandlers) listSpamSubmissions(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return
	}
	page, limit := parsePageLimit(r)
	subs, total, err := h.Submissions.ListSpamByAccountsPage(r.Context(), scope.Accounts, page, limit)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	out := make([]restSpamSubmission, 0, len(subs))
	for _, s := range subs {
		reason := ""
		if s.SpamReason != nil {
			reason = *s.SpamReason
		}
		out = append(out, restSpamSubmission{
			ID: s.ID, FormID: s.FormID, Data: s.Data,
			SpamReason: reason, CreatedAt: s.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	respond(w, http.StatusOK, map[string]any{
		"items": out, "page": page, "limit": limit, "total": total,
	})
}

// unspamSubmission handles POST /v1/rest/spam/submissions/{id}/unspam — clears
// the spam flag on a false positive. Account-scoped.
func (h *RESTHandlers) unspamSubmission(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	id := r.PathValue("id")
	sub, err := h.Submissions.ByID(r.Context(), id)
	if err != nil || sub.AccountID != actx.Account.ID {
		writeErr(w, http.StatusNotFound, "submission not found")
		return
	}
	if err := h.Submissions.UnSpam(r.Context(), id); err != nil {
		respondErr(w, r, err)
		return
	}
	respond(w, http.StatusOK, map[string]string{"status": "unspammed", "id": id})
}

// listSpamSales handles GET /v1/rest/spam/sales — account-scoped, paginated,
// newest first. Returns { items, page, limit, total }.
func (h *RESTHandlers) listSpamSales(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return
	}
	page, limit := parsePageLimit(r)
	sales, total, err := h.Sales.ListSpamByAccountsPage(r.Context(), scope.Accounts, page, limit)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	out := make([]restSpamSale, 0, len(sales))
	for _, s := range sales {
		reason := ""
		if s.SpamReason != nil {
			reason = *s.SpamReason
		}
		out = append(out, restSpamSale{
			RefCode: s.RefCode, Status: s.Status, TotalSen: s.TotalSen,
			Currency: s.CurrencyOrMYR(),
			Buyer:    s.Client.FullName, BuyerEmail: s.Client.Email, FormID: s.FormID,
			SpamReason: reason, CreatedAt: s.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	respond(w, http.StatusOK, map[string]any{
		"items": out, "page": page, "limit": limit, "total": total,
	})
}

// unspamSale handles POST /v1/rest/spam/sales/{ref}/unspam — clears the spam
// flag on a false positive. Account-scoped by ref code.
func (h *RESTHandlers) unspamSale(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	ref := r.PathValue("ref")
	sale, err := h.Sales.ByRefCode(r.Context(), ref)
	if err != nil || sale.AccountID != actx.Account.ID {
		writeErr(w, http.StatusNotFound, "sale not found")
		return
	}
	if err := h.Sales.UnSpam(r.Context(), sale.ID); err != nil {
		respondErr(w, r, err)
		return
	}
	respond(w, http.StatusOK, map[string]string{"status": "unspammed", "ref_code": ref})
}
