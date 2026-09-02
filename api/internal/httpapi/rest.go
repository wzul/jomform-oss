// REST API v1: session- and API-key-authenticated merchant endpoints.
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
	"github.com/wzul/jomform/api/internal/payments"
)

// RESTHandlers serves /v1/rest/* merchant resources.
type RESTHandlers struct {
	Accounts       *dbstore.AccountStore
	Products       *dbstore.ProductStore
	Forms          *dbstore.FormStore
	Keys           *dbstore.APIKeyStore
	Sales          *dbstore.SaleStore
	Sessions       *auth.Sessions
	Pay            *payments.Service
	Webhooks       *dbstore.WebhookStore
	SystemSettings *dbstore.SystemSettingStore
	Spaces         *dbstore.SpaceStore
	Passkeys       *dbstore.PasskeyStore
	Users          *dbstore.UserStore
	Domains        *dbstore.DomainStore
	// ChipProbe validates credentials against CHIP at save time.
	ChipProbe interface {
		ValidateCredentials(ctx context.Context, secret string) error
	}

	// EncryptKey is the 32-byte AES-GCM key from ENCRYPTION_KEY env.
	SecretsKey []byte

	// Store is the S3/MinIO object store for image-block uploads (issue #61).
	// Nil when media storage is not configured (upload returns 503).
	Store *ObjectStore
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
	mux.Handle("POST /v1/rest/forms/bulk", h.authed(h.bulkForms))
	mux.Handle("GET /v1/rest/forms/{id}", h.authed(h.getForm))
	mux.Handle("PATCH /v1/rest/forms/{id}", h.authed(h.updateForm))
	mux.Handle("DELETE /v1/rest/forms/{id}", h.authed(h.deleteForm))
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
	mux.Handle("PUT /v1/rest/settings/tracking", h.authed(h.putTracking))
	mux.Handle("GET /v1/rest/settings/tracking", h.authed(h.getTracking))
	mux.Handle("PUT /v1/rest/forms/{id}/tracking", h.authed(h.putFormTracking))
	mux.Handle("PUT /v1/rest/forms/{id}/settings", h.authed(h.putFormSettings))
	mux.HandleFunc("GET /v1/templates", h.listTemplates)
	mux.HandleFunc("GET /v1/system-settings/public", h.publicSystemSettings)
	mux.Handle("GET /v1/system-settings", h.authed(h.listSystemSettings))
	mux.Handle("PUT /v1/system-settings", h.authed(h.setSystemSetting))
	mux.Handle("POST /v1/templates/use", h.authed(h.useTemplate))
	mux.Handle("GET /v1/cf/start", h.authed(h.cfStart))
	mux.HandleFunc("GET /v1/cf/callback", h.cfCallback)
	mux.Handle("GET /v1/cf/status", h.authed(h.cfStatus))
	mux.Handle("POST /v1/cf/disconnect", h.authed(h.cfDisconnect))
	mux.Handle("POST /v1/auth/passkeys/register/start", h.authed(h.passkeyRegisterStart))
	mux.Handle("POST /v1/auth/passkeys/register/finish", h.authed(h.passkeyRegisterFinish))
	mux.Handle("POST /v1/auth/passkeys/login/start", middleware.RateLimit(http.HandlerFunc(h.passkeyLoginStart)))
	mux.Handle("POST /v1/auth/passkeys/login/finish", middleware.RateLimit(http.HandlerFunc(h.passkeyLoginFinish)))
	mux.Handle("GET /v1/auth/passkeys", h.authed(h.passkeyList))
	mux.Handle("DELETE /v1/auth/passkeys/{id}", h.authed(h.passkeyDelete))
	mux.Handle("GET /v1/spaces", h.authed(h.listSpaces))
	mux.Handle("POST /v1/spaces", h.authed(h.createSpace))
	mux.Handle("GET /v1/spaces/{id}/settings", h.authed(h.getSpaceSettings))
	mux.Handle("PUT /v1/spaces/{id}/settings", h.authed(h.saveSpaceSettings))
	mux.Handle("GET /v1/rest/settings/einvoice", h.authed(h.getEinvoicing))
	mux.Handle("GET /v1/rest/webhooks", h.authed(h.listWebhooks))
	mux.Handle("GET /v1/rest/settings/webhook-key", h.authed(h.getWebhookKey))
	mux.Handle("POST /v1/rest/webhooks", h.authed(h.createWebhook))
	mux.Handle("DELETE /v1/rest/webhooks/{id}", h.authed(h.deleteWebhook))
	mux.Handle("GET /v1/rest/domains", h.authed(h.listCustomDomains))
	mux.Handle("POST /v1/rest/domains", h.authed(h.addCustomDomain))
	mux.Handle("POST /v1/rest/domains/{id}/verify", h.authed(h.verifyCustomDomainByID))
	mux.Handle("DELETE /v1/rest/domains/{id}", h.authed(h.deleteCustomDomainByID))
	mux.Handle("DELETE /v1/rest/settings/custom-domain", h.authed(h.deleteCustomDomain))
	mux.Handle("POST /v1/rest/sales/{ref}/confirm", h.authed(h.confirmSale))
	mux.Handle("POST /v1/rest/sales/{ref}/capture", h.authed(h.captureSale))
	mux.Handle("POST /v1/rest/sales/{ref}/release", h.authed(h.releaseSale))
	mux.Handle("POST /v1/rest/sales/bulk-capture", h.authed(h.bulkCaptureSales))
	mux.Handle("GET /v1/rest/sales", h.authed(h.listSales))
	mux.Handle("GET /v1/rest/dashboard-bundle", h.authed(h.dashboardBundle))
	mux.Handle("GET /v1/rest/analytics/forms", h.authed(h.formAnalytics))
	mux.Handle("GET /v1/rest/analytics/products", h.authed(h.productAnalytics))
	mux.Handle("GET /v1/rest/sales/export.csv", h.authed(h.exportSalesCSV))
	// Issue #61: image-block direct upload. Rate-limited (owner decision)
	// to curb abuse; auth is account-scoped via h.authed.
	mux.Handle("POST /v1/rest/uploads/image",
		middleware.RateLimitWith(2, 6)(h.authed(h.uploadImage)))
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
	// First owned account (M5 adds multi-account switching). Account IDs are
	// TTL-cached per token, so repeat requests skip the Accounts.ByOwner
	// Postgres listing; a hit still re-fetches the account by primary key
	// (one cheap query) so account/CHIP-credential changes are never stale.
	var acc *dbstore.Account
	if ids, ok := cachedSessionAcctIDs(c.Value); ok {
		for _, id := range ids {
			if a, err := h.Accounts.ByID(r.Context(), id); err == nil {
				acc = a
				break
			}
		}
	} else {
		accs, err := h.Accounts.ByOwner(r.Context(), userID)
		if err != nil || len(accs) == 0 {
			return nil, false
		}
		ids := make([]string, 0, len(accs))
		for _, a := range accs {
			ids = append(ids, a.ID)
		}
		storeSessionAcctIDs(c.Value, ids)
		acc = accs[0]
	}
	if acc == nil {
		// Cache held IDs but none fetched: stale mapping (account deleted or
		// owner listing would be empty). Drop the entry so the next request
		// re-resolves via ByOwner, then reject.
		invalidateSessionAcctCache(c.Value)
		return nil, false
	}
	return acc, true
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

// sessionOwnerID resolves the current session's user id ("" when anon).
func (h *RESTHandlers) sessionOwnerID(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" || h.Sessions == nil {
		return ""
	}
	uid, gerr := h.Sessions.Get(r.Context(), c.Value)
	if gerr != nil {
		return ""
	}
	return uid
}

// profileUser loads the current session user (nil when anonymous).
func (h *RESTHandlers) profileUser(r *http.Request) (*dbstore.User, error) {
	uid := h.sessionOwnerID(r)
	if uid == "" {
		return nil, nil
	}
	return h.Users.ByID(r.Context(), uid)
}

// startSessionFor issues a new session cookie for a user id.
func (h *RESTHandlers) startSessionFor(w http.ResponseWriter, r *http.Request, userID string) error {
	token, err := h.Sessions.Create(r.Context(), userID)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(authSessionTTL.Seconds()),
	})
	return nil
}

func base64URL(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func base64URLDecode(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// passkeyLookupUser finds a user by id for discoverable login.
func (h *RESTHandlers) passkeyLookupUser(ctx context.Context, userID string) (*dbstore.PasskeyUser, error) {
	u, err := h.Users.ByID(ctx, userID)
	if err != nil || u == nil {
		return nil, err
	}
	return h.Passkeys.UserFor(ctx, userID, u.Email)
}
