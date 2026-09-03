// REST API v1: session- and API-key-authenticated merchant endpoints.
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/currency"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
	"github.com/wzul/jomform/api/internal/llm"
	"github.com/wzul/jomform/api/internal/payments"
	"github.com/wzul/jomform/api/internal/tlsmgr"
)

// RESTHandlers serves /v1/rest/* merchant resources.
type RESTHandlers struct {
	Accounts          *dbstore.AccountStore
	Products          *dbstore.ProductStore
	ProductVariations *dbstore.ProductVariationStore
	Forms             *dbstore.FormStore
	Keys           *dbstore.APIKeyStore
	Sales          *dbstore.SaleStore
	Subs           *dbstore.SubscriptionStore
	Sessions       *auth.Sessions
	Pay            *payments.Service
	Webhooks       *dbstore.WebhookStore
	SystemSettings *dbstore.SystemSettingStore
	Spaces         *dbstore.SpaceStore
	// Issue #161: space membership + invites.
	SpaceMembers   *dbstore.SpaceMemberStore
	Passkeys       *dbstore.PasskeyStore
	// Issue #161: Mailer delivers the invite email (nil in bare-mode tests).
	Mailer VerifyMailer
	// Issue #199: when non-nil, the invite email is enqueued and sent by a
	// background worker instead of synchronously in the request handler. Nil
	// (bare-mode tests) → synchronous send via Mailer.
	EmailQueue *emailqueue.Queue
	// BaseURL is the app origin used to build invite signup links.
	BaseURL string
	Users          *dbstore.UserStore
	Domains        *dbstore.DomainStore
	// Issue #212: auto-TLS at origin. Nil when ACME is not configured.
	// Verified custom domains are registered here so the origin provisions
	// + renews their Let's Encrypt certs (SNI-based cert selection).
	TLS *tlsmgr.Manager
	// CookieDomain is the shared Domain attribute for the session cookie
	// (e.g. ".jomform.com") so passkey/social sessions remain valid on product
	// subdomains such as mcp.jomform.com (issue #140). Empty → host-only.
	CookieDomain string
	// ChipProbe validates credentials against CHIP at save time.
	ChipProbe interface {
		ValidateCredentials(ctx context.Context, secret string) error
	}

	// EncryptKey is the 32-byte AES-GCM key from ENCRYPTION_KEY env.
	SecretsKey []byte

	// Store is the S3/MinIO object store for image-block uploads (issue #61).
	// Nil when media storage is not configured (upload returns 503).
	Store *ObjectStore

	// Events is the audit/behaviour log (issue #92). Nil in bare-mode tests;
	// Record calls are no-ops when nil.
	Events *dbstore.EventStore

	// EmailDeliveryEvents is the SES delivery-event log (issue #92/#106). Nil
	// in bare-mode tests; the per-sale email status view returns empty.
	EmailDeliveryEvents *dbstore.EmailDeliveryEventStore

	// Issue #171: response-only form submissions (list + count endpoints).
	Submissions *dbstore.SubmissionStore

	// Issue #180: media library metadata store (list/filter/soft-delete).
	// Nil in bare-mode tests; the media endpoints return empty/503.
	Media *dbstore.MediaStore

	// Issue #184: LLM client for auto-generating per-space legal pages.
	// Nil in bare-mode tests; the generate endpoint returns 503.
	LLM interface {
		GenerateLegalPage(ctx context.Context, pageType string, d llm.BusinessDetails) (string, error)
	}
	// Issue #185: BNM exchange-rate cache (Valkey). Nil in bare-mode tests;
	// the currency-rates endpoint returns empty.
	Currency *currency.Store
}

// RegisterRoutes wires REST endpoints.
func (h *RESTHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("GET /v1/rest/products", h.authed(h.listProducts))
	mux.Handle("POST /v1/rest/products", h.authed(h.createProduct))
	mux.Handle("POST /v1/rest/products/bulk", h.authed(h.bulkProducts))
	mux.Handle("PUT /v1/rest/products/{id}", h.authed(h.updateProduct))
	mux.Handle("GET /v1/rest/products/{id}", h.authed(h.getProduct))
	mux.Handle("DELETE /v1/rest/products/{id}", h.authed(h.deleteProduct))
	mux.Handle("PUT /v1/rest/forms/{id}/chip-override", h.authed(h.setFormChipOverride))
	mux.Handle("DELETE /v1/rest/forms/{id}/chip-override", h.authed(h.clearFormChipOverride))
	mux.Handle("GET /v1/rest/forms", h.authed(h.listForms))
	mux.Handle("POST /v1/rest/forms", h.authed(h.createForm))
	mux.Handle("POST /v1/rest/forms/bulk", h.authed(h.bulkForms))
	mux.Handle("GET /v1/rest/forms/{id}", h.authed(h.getForm))
	mux.Handle("PATCH /v1/rest/forms/{id}", h.authed(h.updateForm))
	mux.Handle("DELETE /v1/rest/forms/{id}", h.authed(h.deleteForm))
	// Issue #171: response-only form submissions (list + count).
	mux.Handle("GET /v1/rest/forms/{id}/submissions", h.authed(h.listSubmissions))
	mux.Handle("GET /v1/rest/forms/{id}/submissions/count", h.authed(h.countSubmissions))
	mux.Handle("POST /v1/rest/forms/{id}/publish", h.authed(h.publishForm))
	mux.Handle("POST /v1/rest/forms/{id}/status", h.authed(h.setFormStatus))
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
	mux.Handle("PUT /v1/rest/forms/{id}/restrictions", h.authed(h.putFormRestrictions))
	mux.HandleFunc("GET /v1/templates", h.listTemplates)
	mux.HandleFunc("GET /v1/system-settings/public", h.publicSystemSettings)
	mux.Handle("GET /v1/system-settings", h.authed(h.listSystemSettings))
	mux.Handle("PUT /v1/system-settings", h.authed(h.setSystemSetting))
	mux.Handle("POST /v1/templates/use", h.authed(h.useTemplate))
	mux.Handle("GET /v1/cf/start", h.authed(h.cfStart))
	mux.HandleFunc("GET /v1/cf/callback", h.cfCallback)
	mux.Handle("GET /v1/cf/status", h.authed(h.cfStatus))
	mux.Handle("POST /v1/cf/disconnect", h.authed(h.cfDisconnect))
	mux.Handle("POST /v1/cf/api-token", middleware.RateLimit(h.authed(h.cfConnectAPIToken)))
	mux.Handle("POST /v1/cf/global-key", middleware.RateLimit(h.authed(h.cfConnectGlobalKey)))
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
	mux.Handle("GET /v1/spaces/{id}/email-settings", h.authed(h.getSpaceEmailSettings))
	mux.Handle("PUT /v1/spaces/{id}/email-settings", h.authed(h.saveSpaceEmailSettings))
	// Issue #184: per-space hosted legal pages (privacy/terms/refund).
	mux.Handle("GET /v1/spaces/{id}/legal", h.authed(h.getSpaceLegalPages))
	mux.Handle("POST /v1/spaces/{id}/legal/generate", h.authed(h.generateSpaceLegalPage))
	mux.Handle("PUT /v1/spaces/{id}/legal", h.authed(h.saveSpaceLegalPage))
	// Issue #161: space membership + invites (admin only).
	mux.Handle("POST /v1/spaces/{id}/invites", h.authed(h.createInvite))
	mux.Handle("GET /v1/spaces/{id}/invites", h.authed(h.listInvites))
	mux.Handle("DELETE /v1/spaces/{id}/invites/{inviteId}", h.authed(h.deleteInvite))
	mux.Handle("GET /v1/spaces/{id}/members", h.authed(h.listMembers))
	mux.Handle("PATCH /v1/spaces/{id}/members/{userId}", h.authed(h.updateMemberRole))
	mux.Handle("DELETE /v1/spaces/{id}/members/{userId}", h.authed(h.removeMember))
	mux.Handle("GET /v1/rest/settings/einvoice", h.authed(h.getEinvoicing))
	mux.Handle("GET /v1/rest/currency/rates", h.authed(h.getCurrencyRates))
	mux.Handle("GET /v1/rest/webhooks", h.authed(h.listWebhooks))
	mux.Handle("GET /v1/rest/settings/webhook-key", h.authed(h.getWebhookKey))
	mux.Handle("POST /v1/rest/webhooks", h.authed(h.createWebhook))
	mux.Handle("PUT /v1/rest/webhooks/{id}", h.authed(h.updateWebhook))
	mux.Handle("GET /v1/rest/webhooks/{id}/deliveries", h.authed(h.listWebhookDeliveries))
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
	mux.Handle("GET /v1/rest/sales/{ref}/email-status", h.authed(h.saleEmailStatus))
	// Issue #194: email delivery status view — account-scoped, paginated,
	// filterable by form/sale/status.
	mux.Handle("GET /v1/rest/email-deliveries", h.authed(h.listEmailDeliveries))
	// Issue #131: subscription management for merchants.
	mux.Handle("GET /v1/rest/subscriptions", h.authed(h.listSubscriptions))
	mux.Handle("POST /v1/rest/subscriptions/{id}/cancel", h.authed(h.cancelSubscription))
	// Issue #154: update the card on an existing subscription (re-tokenize).
	mux.Handle("POST /v1/rest/subscriptions/{id}/update-card", h.authed(h.updateSubscriptionCard))
	mux.Handle("GET /v1/rest/dashboard-bundle", h.authed(h.dashboardBundle))
	mux.Handle("GET /v1/rest/analytics/forms", h.authed(h.formAnalytics))
	mux.Handle("GET /v1/rest/analytics/products", h.authed(h.productAnalytics))
	// Issue #92: event-tracking read endpoints (audit log + behaviour analytics).
	mux.Handle("GET /v1/rest/events", h.authed(h.listEvents))
	mux.Handle("GET /v1/rest/events/count", h.authed(h.countEvents))
	mux.Handle("GET /v1/rest/events/breakdown", h.authed(h.breakdownEvents))
	mux.Handle("GET /v1/rest/sales/export.csv", h.authed(h.exportSalesCSV))
	// Issue #61: image-block direct upload. Rate-limited (owner decision)
	// to curb abuse; auth is account-scoped via h.authed.
	mux.Handle("POST /v1/rest/uploads/image",
		middleware.RateLimitWith(2, 6)(h.authed(h.uploadImage)))
	// Issue #133: avatar upload — downscales to a small profile image.
	mux.Handle("POST /v1/rest/uploads/avatar",
		middleware.RateLimitWith(2, 6)(h.authed(h.uploadAvatar)))
	// Issue #133: set/clear the session user's avatar_url. Validated to be a
	// JomForm-hosted media URL (from the upload bucket) to avoid storing
	// arbitrary external URLs.
	mux.Handle("PUT /v1/rest/profile/avatar", h.authed(h.updateProfileAvatar))
	// Issue #145: public media route — streams uploaded objects from the
	// media bucket to browsers. Unauthenticated (public media); the key is
	// validated to a safe `u/...` path (no traversal).
	mux.HandleFunc("GET /media/{key...}", h.handleMedia)
	// Issue #180: media library — list the merchant's uploaded media
	// (account-scoped, paginated, filterable by form/source) and soft-delete
	// one (also removes the object from MinIO).
	mux.Handle("GET /v1/rest/media", h.authed(h.listMedia))
	mux.Handle("DELETE /v1/rest/media/{id}", h.authed(h.deleteMedia))
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
	userID, issued, err := h.Sessions.GetCreated(r.Context(), c.Value)
	if err != nil {
		return nil, false
	}
	// Password reset invalidation (issue #62): if the owner reset their
	// password after this session was issued, the session is dead.
	if u, e := h.Users.ByID(r.Context(), userID); e == nil && u != nil && u.PwResetAt != nil && !auth.ValidAfter(issued, *u.PwResetAt) {
		_ = h.Sessions.Destroy(r.Context(), c.Value)
		invalidateSessionAcctCache(c.Value)
		return nil, false
	}
	// Resolve the account. Issue #158: when a ?space=<slug> scope is present
	// and belongs to the session user, resolve the account from that space
	// (each space owns its own account) instead of the user's first account.
	// This is what makes switching spaces actually switch the account context
	// — CHIP keys, domains, webhooks, products, revenue all follow the space.
	// Without a space scope we fall back to the first owned account (legacy
	// unscoped callers / API-key path).
	acc := h.resolveSessionAccount(r, userID, c.Value)
	if acc == nil {
		// Cache held IDs but none fetched: stale mapping (account deleted or
		// owner listing would be empty). Drop the entry so the next request
		// re-resolves via ByOwner, then reject.
		invalidateSessionAcctCache(c.Value)
		return nil, false
	}
	return acc, true
}

// resolveSessionAccount resolves the account for a session-authenticated
// request. When the request carries a ?space=<slug> scope that belongs to the
// session user (owner OR member, issue #162), the account is resolved from
// that space (issue #158) so all account-level data is space-scoped.
// Otherwise the user's first owned account is used (legacy unscoped callers).
func (h *RESTHandlers) resolveSessionAccount(r *http.Request, userID, token string) *dbstore.Account {
	// Space-scoped resolution (issue #158): each space owns its own account.
	if slug := r.URL.Query().Get("space"); slug != "" && h.Spaces != nil {
		acc, ok, err := h.Spaces.AccessBySlug(r.Context(), slug, userID)
		if err == nil && ok {
			for _, id := range acc.Accounts {
				if a, err := h.Accounts.ByID(r.Context(), id); err == nil {
					return a
				}
			}
			// Accessible space with zero accounts (defensive; shouldn't happen
			// after the migration): fall back to the first owned account.
			// The space is empty so nothing leaks.
			accs, err := h.Accounts.ByOwner(r.Context(), userID)
			if err == nil && len(accs) > 0 {
				return accs[0]
			}
			return nil
		}
		// Space not accessible / unknown: fall back to the first account the
		// user can access (owner OR member, issue #162) so authed() succeeds;
		// the list/settings endpoint's scopeOr403 then returns 403 for the
		// foreign space. Returning nil here would surface a 401 instead of the
		// intended 403.
		if accs, err := h.Accounts.ByOwner(r.Context(), userID); err == nil && len(accs) > 0 {
			return accs[0]
		}
		if h.Spaces != nil {
			if members, err := h.Spaces.ByMember(r.Context(), userID); err == nil {
				for _, m := range members {
					for _, id := range m.Accounts {
						if a, err := h.Accounts.ByID(r.Context(), id); err == nil {
							return a
						}
					}
				}
			}
		}
		return nil
	}
	// Unscoped: first owned account (existing behaviour). Account IDs are
	// TTL-cached per token, so repeat requests skip the Accounts.ByOwner
	// Postgres listing; a hit still re-fetches the account by primary key
	// (one cheap query) so account/CHIP-credential changes are never stale.
	if ids, ok := cachedSessionAcctIDs(token); ok {
		for _, id := range ids {
			if a, err := h.Accounts.ByID(r.Context(), id); err == nil {
				return a
			}
		}
		return nil
	}
	accs, err := h.Accounts.ByOwner(r.Context(), userID)
	if err != nil || len(accs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(accs))
	for _, a := range accs {
		ids = append(ids, a.ID)
	}
	storeSessionAcctIDs(token, ids)
	return accs[0]
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
		ctx = middleware.WithAccountID(ctx, acc.ID)
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

// sessionOwnerID resolves the current session's user id ("" when anon).
func (h *RESTHandlers) sessionOwnerID(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" || h.Sessions == nil {
		return ""
	}
	uid, issued, gerr := h.Sessions.GetCreated(r.Context(), c.Value)
	if gerr != nil {
		return ""
	}
	// Password reset invalidation (issue #62).
	if u, e := h.Users.ByID(r.Context(), uid); e == nil && u != nil && u.PwResetAt != nil && !auth.ValidAfter(issued, *u.PwResetAt) {
		_ = h.Sessions.Destroy(r.Context(), c.Value)
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

// startSessionFor issues a new session cookie for a user id. Passkey/social
// logins issue a 14-day persistent (remember) session, so the persisted
// remember flag is set to true — a later 2FA-enable rotation (#69) will then
// inherit the 14d persistence instead of silently downgrading to 24h.
func (h *RESTHandlers) startSessionFor(w http.ResponseWriter, r *http.Request, userID string) error {
	ttl := rememberLongTTL
	maxAge := int(rememberLongTTL.Seconds())
	token, err := h.Sessions.CreateRememberedWithTTL(r.Context(), userID, ttl, true)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		Domain:   h.CookieDomain,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
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
