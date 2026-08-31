// e-Invoice (LHDN) account settings + outbound webhook CRUD + event dispatcher.
package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
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

// DispatchWebhooks fans an event out to all active webhook endpoints of the
// account, HMAC-signed with each endpoint's secret. Async + log-only failure.
func DispatchWebhooks(ctx context.Context, store *dbstore.WebhookStore, accountID, event string, payload map[string]any) {
	hooks, err := store.ActiveForEvent(ctx, accountID, event)
	if err != nil || len(hooks) == 0 {
		return
	}
	body, _ := json.Marshal(map[string]any{
		"event":     event,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data":      payload,
	})
	for _, wh := range hooks {
		go func(url, secret string) {
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write(body)
			sig := hex.EncodeToString(mac.Sum(nil))
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-JomForm-Event", event)
			req.Header.Set("X-JomForm-Signature", "sha256="+sig)
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				slog.Warn("webhook_delivery_failed", "url", url, "err", err)
				return
			}
			defer resp.Body.Close()
			slog.Info("webhook_delivered", "url", url, "event", event, "status", resp.StatusCode)
		}(wh.URL, wh.Secret)
	}
}
