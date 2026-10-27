// The dashboard's courier connect/disconnect surface, and the PUBLIC callback the
// EasyParcel OAuth consent returns to.
//
// TWO PROVIDERS, TWO CONNECT SHAPES, and the RULES live in internal/shipping
// (connect.go) so the dashboard and the MCP tools cannot disagree about them. What
// is here is the HTTP half: JSON shapes, the write-role gate, and the OAuth return.
//
//   - Lalamove authenticates SERVER TO SERVER (an HMAC key pair in a runtime env
//     file). Nothing to authorize in a browser: connecting means naming WHERE THE
//     PARCEL SHIPS FROM (a point, because Lalamove quotes a route) and marking the
//     workspace connected. The key pair never reaches the dashboard.
//   - EasyParcel is OAuth 2.0 and the consent step is the MERCHANT'S. This surface
//     does not open a browser, does not ask for a password and does not see one: it
//     ISSUES the authorize link (with a single-use state) for the merchant to open
//     on any device, and receives the code at the callback. An expired link is
//     normal and is re-issued — never replayed.
//
// THE CALLBACK URL. EasyParcel validates redirect_uri against the value registered
// in its Developer Hub, and the public URL is the WEB app's /api/* proxy (the API
// implements /v1/*; on staging only /api/* reaches it). So the registered value is
// https://<host>/api/shipping/easyparcel/callback and the API implements
// GET /v1/shipping/easyparcel/callback — the same split the existing inbound
// /v1/payments/chip/callback uses.
//
// NOTHING HERE RETURNS A CREDENTIAL. A status row carries whether credentials
// exist and WHERE they were read from (a path is configuration, not a secret), the
// connection's environment and label, and the pickup point. The grant stays sealed
// in the database and is only ever opened by the adapter.
package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/shipping"
)

// courierStatusJSON is one provider's row on the dashboard.
type courierStatusJSON struct {
	Provider string `json:"provider"`
	// CredentialsPresent says the DEPLOYMENT has a usable credential set for this
	// provider. It is false for production Lalamove keys until Wan obtains them,
	// and the UI says so instead of hiding the provider.
	CredentialsPresent bool `json:"credentials_present"`
	// CredentialsHint names where the value is read from (never the value).
	CredentialsHint string `json:"credentials_hint,omitempty"`
	// CredentialsSource is "file" / "env" / "file+env" / "none".
	CredentialsSource string `json:"credentials_source,omitempty"`
	// NeedsBrowser is true when connecting requires the MERCHANT to complete an
	// OAuth consent in a browser (EasyParcel).
	NeedsBrowser bool `json:"needs_browser"`
	// PointBased is true when the provider needs coordinates for both stops
	// (Lalamove): the workspace must give a pickup point and each zone its own.
	PointBased bool `json:"point_based"`
	// DocURL is where the merchant reads about connecting.
	DocURL string `json:"doc_url,omitempty"`
	// Environment is sandbox/live/unknown. Shown because a sandbox rate is
	// SIMULATED and must never be mistaken for a real one.
	Environment string `json:"environment,omitempty"`
	// Connected is whether THIS workspace may be quoted with this provider.
	Connected    bool   `json:"connected"`
	Status       string `json:"status,omitempty"`
	AccountLabel string `json:"account_label,omitempty"`
	// Origin is the pickup address (merchant configuration).
	OriginPostcode string `json:"origin_postcode,omitempty"`
	OriginState    string `json:"origin_state,omitempty"`
	OriginCountry  string `json:"origin_country,omitempty"`
	OriginLat      string `json:"origin_lat,omitempty"`
	OriginLng      string `json:"origin_lng,omitempty"`
	ServiceType    string `json:"service_type,omitempty"`
	// HasGrant says a sealed OAuth grant exists (never the grant).
	HasGrant bool `json:"has_grant,omitempty"`
	// GrantExpiresAt is when the grant's access token expires (so the UI can say
	// "connected, renews automatically" vs "needs reconnecting").
	GrantExpiresAt string `json:"grant_expires_at,omitempty"`
	// LastError is a CLASS ("expired", "rejected"), never a vendor payload.
	LastError string `json:"last_error,omitempty"`
	// ConnectedAt / DisconnectedAt are audit timestamps.
	ConnectedAt    string `json:"connected_at,omitempty"`
	DisconnectedAt string `json:"disconnected_at,omitempty"`
	// Ready is the one bit the merchant acts on: this workspace can be QUOTED by
	// this provider right now.
	Ready bool `json:"ready"`
	// Missing names what is still needed ("credentials", "connect", "grant",
	// "origin"), so an empty state is actionable.
	Missing []string `json:"missing,omitempty"`
}

// courierListResp is the GET response: every provider this build knows, whether or
// not the workspace has connected it.
type courierListResp struct {
	AccountID string              `json:"account_id"`
	Providers []courierStatusJSON `json:"providers"`
	// Env is the deployment environment the callback URL is derived from.
	Env          string `json:"env"`
	CallbackPath string `json:"callback_path"`
	// RedirectURI is the callback URL THIS deployment must have registered with
	// the provider ("" when none is configured), so the dashboard can show the
	// exact value to paste into the Developer Hub.
	RedirectURI string `json:"redirect_uri,omitempty"`
}

// courierStatuses builds the status rows for one workspace from the SHARED state
// model, so a provider cannot be "ready" here and "missing a grant" in the MCP
// tool.
func (h *RESTHandlers) courierStatuses(ctx context.Context, accountID string) []courierStatusJSON {
	var conns []shipping.Connection
	if h.Couriers != nil && h.Couriers.Store != nil {
		if list, err := h.Couriers.Store.Connections(ctx, accountID); err == nil {
			conns = list
		}
	}
	states := shipping.CourierStates(h.courierRegistry(), conns, h.courierCreds())
	out := make([]courierStatusJSON, 0, len(states))
	for _, st := range states {
		row := courierStatusJSON{
			Provider:           st.Info.Name,
			CredentialsPresent: st.Info.CredentialsPresent,
			CredentialsHint:    st.Info.CredentialsHint,
			CredentialsSource:  st.CredentialsSource,
			NeedsBrowser:       st.Info.NeedsBrowser,
			PointBased:         st.Info.PointBased,
			DocURL:             st.Info.DocURL,
			Environment:        st.Info.Environment,
			Missing:            st.Missing,
			Ready:              st.Ready,
		}
		if c := st.Connection; c != nil {
			row.Status = c.Status
			if c.Environment != "" {
				row.Environment = c.Environment
			}
			row.AccountLabel = c.AccountLabel
			row.OriginPostcode = c.Origin.Postcode
			row.OriginState = c.Origin.State
			row.OriginCountry = c.OriginCountry
			row.OriginLat = c.OriginLat
			row.OriginLng = c.OriginLng
			row.ServiceType = c.ServiceType
			row.HasGrant = c.HasGrant
			row.LastError = c.LastError
			row.GrantExpiresAt = rfc3339OrEmpty(c.GrantExpiresAt)
			row.ConnectedAt = rfc3339OrEmpty(c.ConnectedAt)
			row.DisconnectedAt = rfc3339OrEmpty(c.DisconnectedAt)
			row.Connected = c.Usable()
		}
		out = append(out, row)
	}
	return out
}

// rfc3339OrEmpty renders a time for the wire, or "" for the zero time.
func rfc3339OrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// courierCreds is the nil-safe credential accessor.
func (h *RESTHandlers) courierCreds() shipping.CourierCredentials {
	if h.Couriers == nil {
		return shipping.CourierCredentials{}
	}
	return h.Couriers.Creds
}

// courierRegistry is the nil-safe registry accessor.
func (h *RESTHandlers) courierRegistry() *shipping.Registry {
	if h.Couriers == nil {
		return nil
	}
	return h.Couriers.Registry
}

// courierConnector builds the SHARED connect policy for this deployment.
func (h *RESTHandlers) courierConnector() *shipping.Connector {
	return &shipping.Connector{
		Store:    h.couriers().Store,
		Creds:    h.couriers().Creds,
		Registry: h.courierRegistry(),
		Env:      h.courierEnv(),
	}
}

// couriers is a nil-safe accessor for the wiring bundle.
func (h *RESTHandlers) couriers() *Couriers {
	if h.Couriers == nil {
		return &Couriers{}
	}
	return h.Couriers
}

// courierEnv is the deployment environment (prod picks the production redirect).
func (h *RESTHandlers) courierEnv() string {
	if h.Couriers == nil {
		return ""
	}
	return strings.TrimSpace(h.Couriers.Env)
}

// courierRedirectURI is the registered callback URL for this deployment.
func (h *RESTHandlers) courierRedirectURI() string {
	if h.Couriers == nil {
		return ""
	}
	return h.Couriers.Creds.EasyParcel.RedirectURI(h.Couriers.Env)
}

// courierList assembles the list response.
func (h *RESTHandlers) courierList(ctx context.Context, accountID string) courierListResp {
	return courierListResp{
		AccountID:    accountID,
		Providers:    h.courierStatuses(ctx, accountID),
		Env:          h.courierEnv(),
		CallbackPath: "/api/shipping/easyparcel/callback",
		RedirectURI:  h.courierRedirectURI(),
	}
}

// getShippingCouriers lists every provider with this workspace's connection state.
// Route: GET /v1/rest/shipping/couriers
func (h *RESTHandlers) getShippingCouriers(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	// A READ: any member of the workspace may see which provider is active (the
	// form's own fee depends on it), so no write role is required.
	respond(w, http.StatusOK, h.courierList(r.Context(), actx.Account.ID))
}

// connectShippingCourierRequest is the connect payload: WHERE THE PARCEL SHIPS
// FROM, plus the provider's own service choice. No credential is ever accepted
// here — a credential that arrives over an API call is a credential in a log.
type connectShippingCourierRequest struct {
	// OriginPostcode / OriginState are required by EasyParcel (with the ISO
	// subdivision code the adapter derives from the state name).
	OriginPostcode string `json:"origin_postcode"`
	OriginState    string `json:"origin_state"`
	OriginCountry  string `json:"origin_country"`
	// OriginLat / OriginLng are required by a point-based provider (Lalamove).
	OriginLat string `json:"origin_lat"`
	OriginLng string `json:"origin_lng"`
	// ServiceType is the provider's own service code (Lalamove), optional.
	ServiceType string `json:"service_type"`
	// Environment is what the merchant IS authorizing ("sandbox" for a demo
	// account, "live" for a real one). EasyParcel decides the environment by the
	// authorized account, and recording the merchant's intent is what makes a
	// simulated rate visible as simulated.
	Environment string `json:"environment"`
}

// connectShippingCourierResponse answers a connect attempt: either the workspace
// is connected (Lalamove) or a link the MERCHANT must open (EasyParcel).
type connectShippingCourierResponse struct {
	Provider string `json:"provider"`
	// Connected is true when the provider is immediately quotable.
	Connected bool `json:"connected"`
	// AuthorizeURL is present only when a HUMAN must complete the consent step.
	// It is a link, deliberately: the merchant opens it on any device at a time of
	// his choosing, and this server never sees the password.
	AuthorizeURL string `json:"authorize_url,omitempty"`
	// RedirectURI is the exact value that must be registered in the provider's
	// developer hub for that link to come back.
	RedirectURI string `json:"redirect_uri,omitempty"`
	// StateExpiresAt is when this link stops being valid (a fresh one is issued
	// after that — an expired link is not an error).
	StateExpiresAt string `json:"state_expires_at,omitempty"`
	// Status is the resulting provider row, so the UI renders one shape.
	Status courierStatusJSON `json:"status"`
}

// postShippingCourierConnect connects one provider for the workspace, or hands back
// the authorize link when a human must consent.
// Route: POST /v1/rest/shipping/couriers/{provider}/connect
func (h *RESTHandlers) postShippingCourierConnect(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if h.Couriers == nil || h.Couriers.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, shipping.ErrConnectUnavailable.Error())
		return
	}
	// Issue #645: a write needs the API-key write scope AND the manage-or-admin
	// role in the ?workspace= scope.
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in connectShippingCourierRequest
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "butiran tidak lengkap")
		return
	}
	provider := shipping.Normalize(r.PathValue("provider"))
	outcome, err := h.courierConnector().Connect(r.Context(), actx.Account.ID, provider, shipping.ConnectRequest{
		OriginPostcode: in.OriginPostcode,
		OriginState:    in.OriginState,
		OriginCountry:  in.OriginCountry,
		OriginLat:      in.OriginLat,
		OriginLng:      in.OriginLng,
		ServiceType:    in.ServiceType,
		Environment:    in.Environment,
	})
	if err != nil {
		// Every refusal names its own remedy (no credentials / needs a pickup
		// point / no registered redirect), which is what the dashboard shows.
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	resp := connectShippingCourierResponse{
		Provider:     outcome.Provider,
		Connected:    outcome.Connected,
		AuthorizeURL: outcome.AuthorizeURL,
		RedirectURI:  outcome.RedirectURI,
	}
	if !outcome.StateExpiresAt.IsZero() {
		resp.StateExpiresAt = outcome.StateExpiresAt.UTC().Format(time.RFC3339)
	}
	for _, st := range h.courierStatuses(r.Context(), actx.Account.ID) {
		if st.Provider == provider {
			resp.Status = st
			break
		}
	}
	respond(w, http.StatusOK, resp)
}

// postShippingCourierDisconnect drops the connection (and its sealed grant) while
// KEEPING the merchant's pickup address, so reconnecting is one click.
// Route: POST /v1/rest/shipping/couriers/{provider}/disconnect
func (h *RESTHandlers) postShippingCourierDisconnect(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if h.Couriers == nil || h.Couriers.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, shipping.ErrConnectUnavailable.Error())
		return
	}
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	provider := shipping.Normalize(r.PathValue("provider"))
	if err := h.courierConnector().Disconnect(r.Context(), actx.Account.ID, provider); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	respond(w, http.StatusOK, h.courierList(r.Context(), actx.Account.ID))
}

// easyParcelCallback is the PUBLIC OAuth return: the merchant's browser arrives
// with ?code=&state= after consenting.
//
// Route: GET /v1/shipping/easyparcel/callback   (registered publicly in the
// EasyParcel Developer Hub as https://<host>/api/shipping/easyparcel/callback —
// the web app proxies /api/* to the API's /v1/*, and on staging only /api/*
// reaches the API).
//
// WHAT IT TRUSTS. The single-use `state` it issued, and nothing else: this
// endpoint is public, so the state is its only defence against a forged consent.
// The code is exchanged server-side with the client secret; neither the code nor
// the token is logged, echoed or put in a URL.
func (h *PublicHandlers) easyParcelCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if err := h.completeEasyParcelConnect(r, q.Get("state"), q.Get("code"), q.Get("error")); err != nil {
		// The redirect carries a CLASS, never the vendor's own message (which can
		// quote our request) and never the code.
		h.redirectAfterConnect(w, r, "", connectOutcome(err))
		return
	}
	h.redirectAfterConnect(w, r, "connected", "")
}

// completeEasyParcelConnect performs the server-side half of the consent.
func (h *PublicHandlers) completeEasyParcelConnect(r *http.Request, state, code, oauthErr string) error {
	if h.Couriers == nil || h.Couriers.Store == nil {
		return errCourierCallbackUnavailable
	}
	if strings.TrimSpace(oauthErr) != "" {
		// The merchant declined, or the provider refused BEFORE issuing a code.
		// The state is still consumed so it cannot be replayed.
		_, _, _ = h.Couriers.Store.ConsumeState(r.Context(), state, shipping.DefaultStateTTL)
		return errCourierConsentDeclined
	}
	ref, ok, err := h.Couriers.Store.ConsumeState(r.Context(), state, shipping.DefaultStateTTL)
	if err != nil {
		return errCourierCallbackConsume
	}
	if !ok {
		// Unknown, already used, or past its TTL. A fresh link is issued by
		// connecting again; a replayed one never re-binds a grant.
		return errCourierStateInvalid
	}
	if strings.TrimSpace(code) == "" {
		return errCourierCodeMissing
	}
	cfg := h.Couriers.Creds.EasyParcel
	if !cfg.Configured() {
		return errCourierCallbackUnavailable
	}
	redirectURI := strings.TrimSpace(ref.RedirectURI)
	if redirectURI == "" {
		redirectURI = cfg.RedirectURI(h.Couriers.Env)
	}
	tokens, err := shipping.EasyParcelExchangeCode(r.Context(), nil, cfg, strings.TrimSpace(code), redirectURI, state)
	if err != nil {
		return errCourierTokenExchange
	}
	if err := h.Couriers.Store.Save(r.Context(), ref.AccountID, shipping.ProviderEasyParcel, tokens); err != nil {
		if errors.Is(err, dbstore.ErrCourierNoKey) {
			return errCourierNoSealKey
		}
		return errCourierTokenStore
	}
	return nil
}

// redirectAfterConnect sends the merchant's browser back to the dashboard with an
// OUTCOME class. The dashboard shows it as its own sentence; nothing the provider
// said travels in the URL.
func (h *PublicHandlers) redirectAfterConnect(w http.ResponseWriter, r *http.Request, outcome, problem string) {
	target := h.connectReturnURL(r)
	if target == "" {
		// No dashboard base configured: answer in plain text rather than
		// redirecting somewhere invented.
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("EasyParcel: " + firstNonEmpty(outcome, problem) + "\n"))
		return
	}
	u, err := url.Parse(target)
	if err != nil {
		http.Error(w, "redirect tidak sah", http.StatusInternalServerError)
		return
	}
	q := u.Query()
	if outcome != "" {
		q.Set("easyparcel", outcome)
	}
	if problem != "" {
		q.Set("easyparcel_error", problem)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// connectReturnURL is where the merchant lands after consent: the form settings
// page of the APP base URL (the dashboard host, not the API's).
func (h *PublicHandlers) connectReturnURL(r *http.Request) string {
	base := strings.TrimSpace(h.BaseURL)
	if base == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + "/form-settings?tab=shipping"
}

// The callback's failure classes. They are stable, bounded tokens the dashboard
// renders in the merchant's own language; none of them carries a vendor message.
var (
	errCourierCallbackUnavailable = errors.New("courier.callbackUnavailable")
	errCourierConsentDeclined     = errors.New("courier.consentDeclined")
	errCourierStateInvalid        = errors.New("courier.stateInvalid")
	errCourierCodeMissing         = errors.New("courier.codeMissing")
	errCourierTokenExchange       = errors.New("courier.tokenExchangeFailed")
	errCourierTokenStore          = errors.New("courier.tokenStoreFailed")
	errCourierNoSealKey           = errors.New("courier.noSealKey")
	errCourierCallbackConsume     = errors.New("courier.callbackFailed")
)

// connectOutcome maps a failure to the class shown to the merchant.
func connectOutcome(err error) string {
	for _, known := range []error{
		errCourierCallbackUnavailable, errCourierConsentDeclined, errCourierStateInvalid,
		errCourierCodeMissing, errCourierTokenExchange, errCourierTokenStore,
		errCourierNoSealKey, errCourierCallbackConsume,
	} {
		if errors.Is(err, known) {
			return strings.TrimPrefix(known.Error(), "courier.")
		}
	}
	return "failed"
}
