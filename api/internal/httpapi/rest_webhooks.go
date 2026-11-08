// e-Invoice (LHDN) account settings + outbound webhook CRUD + event dispatcher.
package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/wzul/jomform/api/internal/ssrfguard"
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
		writeErr(w, http.StatusBadRequest, "invalid payload")
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
	// Issue #196: optional ?form_id= narrows to that form's webhooks; absent
	// (or empty) returns the workspace/account-wide webhooks.
	formID := r.URL.Query().Get("form_id")
	hooks, err := h.Webhooks.ListByAccount(r.Context(), actx.Account.ID, formID)
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
		// Issue #196: optional form_id scopes the webhook to one form.
		FormID string `json:"form_id"`
	}
	if err := decodeJSON(r, &in); err != nil || !strings.HasPrefix(in.URL, "http") || len(in.Secret) < 8 {
		writeErr(w, http.StatusBadRequest, "url http(s) and secret (min 8 char) are required")
		return
	}
	// Issue #366 (SSRF): reject webhook URLs that resolve to private,
	// link-local, loopback or cloud-metadata addresses at create time, so a
	// merchant cannot register an endpoint that makes the server reach into
	// its own network. (Delivery-time is guarded again in the dispatcher.)
	if _, err := ssrfguard.ValidateURL(r.Context(), in.URL); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid webhook url (points to a private/loopback host)")
		return
	}
	if len(in.Events) == 0 {
		in.Events = []string{"sale.paid"}
	}
	// Issue #196: a form-scoped webhook must reference a form the account owns.
	if in.FormID != "" {
		if _, err := h.Forms.GetByID(r.Context(), actx.Account.ID, in.FormID); err != nil {
			writeErr(w, http.StatusBadRequest, "form not found")
			return
		}
	}
	wh, err := h.Webhooks.Create(r.Context(), actx.Account.ID, in.FormID, in.URL, in.Secret, in.Events)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit webhook creation.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.webhook_created", map[string]any{"webhook_id": wh.ID, "events": in.Events, "form_id": in.FormID})
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
		writeErr(w, http.StatusBadRequest, "url http(s) is required")
		return
	}
	// Issue #366 (SSRF): same create-time guard on update so an endpoint
	// cannot be re-pointed at a private/loopback/metadata address.
	if _, err := ssrfguard.ValidateURL(r.Context(), in.URL); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid webhook url (points to a private/loopback host)")
		return
	}
	// Secret is optional on update: a blank value keeps the existing secret;
	// a supplied non-blank secret must meet the 8-char minimum.
	if in.Secret != "" && len(in.Secret) < 8 {
		writeErr(w, http.StatusBadRequest, "secret (min 8 char) is required")
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
	// Issue #197: surface dead-lettered retries alongside the delivery log so
	// merchants can see (and manually retry) deliveries that were dropped.
	dead, err := h.Webhooks.ListDeadLettered(r.Context(), actx.Account.ID, r.PathValue("id"), limit)
	if err == nil && len(dead) > 0 {
		deliveries = append(deliveries, dead...)
	}
	writeJSON(w, http.StatusOK, deliveries)
}

// retryWebhookDelivery re-enqueues a failed delivery immediately (issue #197).
// The delivery is looked up account-scoped; a new retry row is inserted with
// next_retry_at = now so the sweep re-delivers it on the next tick. Returns
// 404 when the delivery is not found or not owned.
//
// Issue #522 (cross-tenant signing bypass): the webhook id comes from the URL
// path and is NOT authorised by looking up the delivery alone. The sweep
// resolves the webhook id with an any-account lookup and delivers with that
// webhook's own URL and signing secret, so enqueuing against a foreign webhook
// id would inject attacker-controlled payload into another tenant's endpoint,
// validly signed with the victim's secret. Authorise {id} first: the delivery's
// owning webhook must be exactly the webhook in the path. A mismatch is a 404
// ("tidak dijumpai") so existence is not leaked, and it creates no row.
func (h *RESTHandlers) retryWebhookDelivery(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	webhookID := r.PathValue("id")
	delivery, err := h.Webhooks.GetDelivery(r.Context(), actx.Account.ID, r.PathValue("deliveryId"))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "delivery not found")
			return
		}
		respondErr(w, r, err)
		return
	}
	// The delivery must belong to the webhook named in the path — otherwise the
	// caller is trying to retry its own delivery against someone else's webhook.
	if owned, _ := delivery["webhook_id"].(string); owned != webhookID {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	event, _ := delivery["event"].(string)
	payload, _ := delivery["payload"].(string)
	if err := h.Webhooks.EnqueueRetry(r.Context(), actx.Account.ID, webhookID, event, payload, 1, time.Now()); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit the manual retry.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.webhook_retried", map[string]any{"webhook_id": webhookID, "delivery_id": r.PathValue("deliveryId")})
	writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
}

// retryDeadLettered un-dead-letters a dead-lettered retry so the sweep
// re-delivers it on the next tick (issue #197). Returns 404 when the retry is
// not found or not owned.
func (h *RESTHandlers) retryDeadLettered(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if err := h.Webhooks.RetryNow(r.Context(), actx.Account.ID, r.PathValue("retryId")); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "retry not found")
			return
		}
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit the manual retry.
	h.recordEvent(r, actx, h.sessionOwnerID(r), "settings.webhook_retried", map[string]any{"webhook_id": r.PathValue("id"), "retry_id": r.PathValue("retryId")})
	writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
}
