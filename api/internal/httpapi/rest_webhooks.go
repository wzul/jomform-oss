// e-Invoice (LHDN) account settings + outbound webhook CRUD + event dispatcher.
package httpapi

import (
	"net/http"
	"strconv"
	"strings"
)

// ─── e-Invoice settings ─────────────────────────────────────────────────────

func (h *RESTHandlers) putEinvoicing(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		TIN  string `json:"tin"`
		MSIC string `json:"msic"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	if err := h.Accounts.SetEinvoice(r.Context(), actx.Account.ID, strings.TrimSpace(in.TIN), strings.TrimSpace(in.MSIC)); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (h *RESTHandlers) getEinvoicing(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	tin, msic, err := h.Accounts.Einvoice(r.Context(), actx.Account.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"tin": tin, "msic": msic})
}

// ─── Outbound webhook CRUD ──────────────────────────────────────────────────

func (h *RESTHandlers) listWebhooks(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	hooks, err := h.Webhooks.ListByAccount(r.Context(), actx.Account.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	type row struct {
		ID     string   `json:"id"`
		URL    string   `json:"url"`
		Events []string `json:"events"`
	}
	out := make([]row, 0, len(hooks))
	for _, wh := range hooks {
		out = append(out, row{ID: wh.ID, URL: wh.URL, Events: wh.Events})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *RESTHandlers) createWebhook(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
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
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit webhook creation.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.webhook_created", map[string]any{"webhook_id": wh.ID, "events": in.Events})
	writeJSON(w, http.StatusCreated, map[string]string{"id": wh.ID, "url": wh.URL})
}

func (h *RESTHandlers) deleteWebhook(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if err := h.Webhooks.Delete(r.Context(), actx.Account.ID, r.PathValue("id")); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit webhook deletion.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.webhook_deleted", map[string]any{"webhook_id": r.PathValue("id")})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *RESTHandlers) updateWebhook(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		URL    string   `json:"url"`
		Secret string   `json:"secret"`
		Events []string `json:"events"`
	}
	if err := decodeJSON(r, &in); err != nil || !strings.HasPrefix(in.URL, "http") {
		writeErr(w, http.StatusBadRequest, "url http(s) wajib")
		return
	}
	// Secret is optional on update: a blank value keeps the existing secret;
	// a supplied non-blank secret must meet the 8-char minimum.
	if in.Secret != "" && len(in.Secret) < 8 {
		writeErr(w, http.StatusBadRequest, "secret (min 8 char) wajib")
		return
	}
	if len(in.Events) == 0 {
		in.Events = []string{"sale.paid"}
	}
	if err := h.Webhooks.Update(r.Context(), actx.Account.ID, r.PathValue("id"), in.URL, in.Secret, in.Events); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit webhook update.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.webhook_updated", map[string]any{"webhook_id": r.PathValue("id"), "events": in.Events})
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// webhookDeliveryLimitMax bounds the latest-N window surfaced to the UI.
// Mirrors the MCP webhook_deliveries cap so both surfaces stay in step.
const webhookDeliveryLimitMax = 100

// listWebhookDeliveries returns the latest deliveries for one webhook
// (account-scoped via the joined ownership check in ListDeliveries). The
// optional limit query param defaults to 10 and is clamped to 1..100.
func (h *RESTHandlers) listWebhookDeliveries(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	limit := 10
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > webhookDeliveryLimitMax {
		limit = webhookDeliveryLimitMax
	}
	deliveries, err := h.Webhooks.ListDeliveries(r.Context(), actx.Account.ID, r.PathValue("id"), limit)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, deliveries)
}

