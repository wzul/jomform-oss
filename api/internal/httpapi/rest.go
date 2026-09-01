// REST API v1: session- and API-key-authenticated merchant endpoints.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// RESTHandlers serves /v1/rest/* merchant resources.
type RESTHandlers struct {
	Accounts *dbstore.AccountStore
	Products *dbstore.ProductStore
	Forms    *dbstore.FormStore
	Keys     *dbstore.APIKeyStore
	Sales    *dbstore.SaleStore
	Sessions *auth.Sessions
	Pay      *payments.Service
	Webhooks *dbstore.WebhookStore
	Domains  *dbstore.DomainStore
	// ChipProbe validates credentials against CHIP at save time.
	ChipProbe interface {
		ValidateCredentials(ctx context.Context, secret string) error
	}

	// EncryptKey is the 32-byte AES-GCM key from ENCRYPTION_KEY env.
	SecretsKey []byte
}

// RegisterRoutes wires REST endpoints.
func (h *RESTHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("GET /v1/rest/products", h.authed(h.listProducts))
	mux.Handle("POST /v1/rest/products", h.authed(h.createProduct))
	mux.Handle("DELETE /v1/rest/products/{id}", h.authed(h.deleteProduct))
	mux.Handle("PUT /v1/rest/forms/{id}/chip-override", h.authed(h.setFormChipOverride))
	mux.Handle("DELETE /v1/rest/forms/{id}/chip-override", h.authed(h.clearFormChipOverride))
	mux.Handle("GET /v1/rest/forms", h.authed(h.listForms))
	mux.Handle("POST /v1/rest/forms", h.authed(h.createForm))
	mux.Handle("GET /v1/rest/forms/{id}", h.authed(h.getForm))
	mux.Handle("PATCH /v1/rest/forms/{id}", h.authed(h.updateForm))
	mux.Handle("POST /v1/rest/forms/{id}/publish", h.authed(h.publishForm))
	mux.Handle("GET /v1/rest/keys", h.authed(h.listKeys))
	mux.Handle("POST /v1/rest/keys", h.authed(h.createKey))
	mux.Handle("DELETE /v1/rest/keys/{id}", h.authed(h.revokeKey))
	mux.Handle("GET /v1/rest/summary", h.authed(h.summary))
	mux.Handle("PUT /v1/rest/settings/chip", h.authed(h.setChipCreds))
	mux.Handle("GET /v1/rest/settings/chip", h.authed(h.chipStatus))
	mux.Handle("DELETE /v1/rest/settings/chip", h.authed(h.deleteChipCreds))
	mux.Handle("PUT /v1/rest/settings/custom-domain", h.authed(h.setCustomDomain))
	mux.Handle("GET /v1/rest/settings/custom-domain", h.authed(h.getCustomDomain))
	mux.Handle("PUT /v1/rest/settings/einvoice", h.authed(h.putEinvoicing))
	mux.Handle("GET /v1/rest/settings/einvoice", h.authed(h.getEinvoicing))
	mux.Handle("GET /v1/rest/webhooks", h.authed(h.listWebhooks))
	mux.Handle("POST /v1/rest/webhooks", h.authed(h.createWebhook))
	mux.Handle("DELETE /v1/rest/webhooks/{id}", h.authed(h.deleteWebhook))
	mux.Handle("GET /v1/rest/domains", h.authed(h.listCustomDomains))
	mux.Handle("POST /v1/rest/domains", h.authed(h.addCustomDomain))
	mux.Handle("POST /v1/rest/domains/{id}/verify", h.authed(h.verifyCustomDomainByID))
	mux.Handle("DELETE /v1/rest/domains/{id}", h.authed(h.deleteCustomDomainByID))
	mux.Handle("POST /v1/rest/domains/connect", h.authed(h.connectCFHostedDomain))
	mux.Handle("POST /v1/rest/settings/custom-domain/verify", h.authed(h.verifyCustomDomain))
	mux.Handle("POST /v1/rest/settings/custom-domain/connect", h.authed(h.connectCFHosted))
	mux.Handle("DELETE /v1/rest/settings/custom-domain", h.authed(h.deleteCustomDomain))
	mux.Handle("POST /v1/rest/sales/{ref}/confirm", h.authed(h.confirmSale))
	mux.Handle("POST /v1/rest/sales/{ref}/capture", h.authed(h.captureSale))
	mux.Handle("POST /v1/rest/sales/{ref}/release", h.authed(h.releaseSale))
	mux.Handle("GET /v1/rest/sales", h.authed(h.listSales))
	mux.Handle("GET /v1/rest/analytics/forms", h.authed(h.formAnalytics))
	mux.Handle("GET /v1/rest/analytics/products", h.authed(h.productAnalytics))
	mux.Handle("GET /v1/rest/sales/export.csv", h.authed(h.exportSalesCSV))
}

// Resolve identifies the caller: Bearer API key (preferred) or session
// cookie + default account (owner's first account).
func (h *RESTHandlers) resolve(w http.ResponseWriter, r *http.Request) (*dbstore.Account, bool) {
	// 1. Bearer API key
	if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
		raw := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if raw == "" {
			return nil, false
		}
		key, err := h.Keys.LookupByKeyHash(r.Context(), auth.HashAPIKey(raw))
		if err != nil {
			return nil, false
		}
		acc, err := h.Accounts.ByID(r.Context(), key.AccountID)
		if err != nil {
			return nil, false
		}
		go h.Keys.Touch(context.Background(), key.ID)
		return acc, true
	}

	// 2. Session cookie
	c, err := r.Cookie(sessionCookie)
	if err != nil || h.Sessions == nil {
		return nil, false
	}
	userID, err := h.Sessions.Get(r.Context(), c.Value)
	if err != nil {
		return nil, false
	}
	// First owned account (M5 adds multi-account switching).
	accs, err := h.Accounts.ByOwner(r.Context(), userID)
	if err != nil || len(accs) == 0 {
		return nil, false
	}
	return accs[0], true
}

// AccountCtx carries the resolved account through the request.
type AccountCtx struct {
	Account *dbstore.Account
}

type acctKey struct{}

// AccountFrom returns the resolved account for this request.
func AccountFrom(ctx context.Context) *AccountCtx {
	if v, ok := ctx.Value(acctRequestKey{}).(*AccountCtx); ok {
		return v
	}
	return nil
}

type acctRequestKey struct{}

// authed wraps a handler with auth + account resolution.
func (h *RESTHandlers) authed(next func(http.ResponseWriter, *http.Request, *AccountCtx)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acc, ok := h.resolve(w, r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "auth diperlukan")
			return
		}
		ctx := context.WithValue(r.Context(), acctRequestKey{}, &AccountCtx{Account: acc})
		next(w, r.WithContext(ctx), &AccountCtx{Account: acc})
	})
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	return dec.Decode(v)
}

func respond(w http.ResponseWriter, code int, v any) { writeJSON(w, code, v) }

func respondErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, dbstore.ErrFormNotFound), errors.Is(err, dbstore.ErrAPIKeyInvalid):
		writeErr(w, http.StatusNotFound, "tidak dijumpai")
	default:
		slog.Error("rest_error", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}
