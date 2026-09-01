// e-Invoice (LHDN) account settings + outbound webhook CRUD + event dispatcher.
package httpapi

import (
	"net/http"
	"strings"
)

// ─── e-Invoice settings ─────────────────────────────────────────────────────

func (h *RESTHandlers) putEinvoicing(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	var in struct {
		TIN  string `json:"tin"`
		MSIC string `json:"msic"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	if err := h.Accounts.SetEinvoice(r.Context(), actx.Account.ID, strings.TrimSpace(in.TIN), strings.TrimSpace(in.MSIC)); err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (h *RESTHandlers) getEinvoicing(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	tin, msic, err := h.Accounts.Einvoice(r.Context(), actx.Account.ID)
	if err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"tin": tin, "msic": msic})
}

// ─── Outbound webhook CRUD ──────────────────────────────────────────────────

func (h *RESTHandlers) listWebhooks(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	hooks, err := h.Webhooks.ListByAccount(r.Context(), actx.Account.ID)
	if err != nil {
		respondErr(w, err)
		return
	}
	type row struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	out := make([]row, 0, len(hooks))
	for _, wh := range hooks {
		out = append(out, row{ID: wh.ID, URL: wh.URL})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *RESTHandlers) createWebhook(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	var in struct {
		URL    string   `json:"url"`
		Secret string   `json:"secret"`
		Events []string `json:"events"`
	}
	if err := decodeJSON(r, &in); err != nil || !strings.HasPrefix(in.URL, "http") || len(in.Secret) < 8 {
		writeErr(w, http.StatusBadRequest, "url http(s) dan secret (min 8 char) wajib")
		return
	}
	if len(in.Events) == 0 {
		in.Events = []string{"sale.paid"}
	}
	wh, err := h.Webhooks.Create(r.Context(), actx.Account.ID, in.URL, in.Secret, in.Events)
	if err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": wh.ID, "url": wh.URL})
}

func (h *RESTHandlers) deleteWebhook(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if err := h.Webhooks.Delete(r.Context(), actx.Account.ID, r.PathValue("id")); err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ─── Dispatcher: called on payment transitions ──────────────────────────────
