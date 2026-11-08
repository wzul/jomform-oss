// EasyParcel adapter: OAuth 2.0 connect + live quotation.
//
// THE ENVIRONMENT IS NOT IN THE URL. One EasyParcel OAuth application serves
// BOTH sandbox and live; which one you are talking to is decided by WHICH ACCOUNT
// the merchant authorized (the consent screen offers "Demo" or a live account).
// So this adapter cannot report "sandbox" or "prod" from its credentials — it
// reports the environment as decided per-connection, and the connection row
// records what the merchant picked. Nothing here ever assumes a demo account.
//
// THE CONNECT STEP IS A HUMAN'S. The authorize URL must be opened by the merchant
// on their own device and their password never touches this server: the lane's
// part ends at issuing the URL, receiving the code at the callback, and doing the
// server-side token exchange (see EasyParcelExchangeCode). The state is single
// use and short lived; an expired link is normal and is re-issued, never treated
// as a failure.
//
// MEASURED/CONTRACT DETAILS (docs.easyparcel.com, OpenAPI reference):
//
//   - authorize:  GET  https://api.easyparcel.com/oauth/login?client_id&redirect_uri&state
//   - token:      POST https://api.easyparcel.com/oauth/token  (HTTP Basic
//     client_id:client_secret) with grant_type=authorization_code,
//     redirect_uri, code, state → access_token, expires_in (~10h),
//     refresh_token, refresh_token_expires_at (~1 year).
//   - quotation:  POST {base}/open_api/{version}/shipment/quotations with
//     `Authorization: Bearer …` and
//     {"api_rate_on":true,"shipment":[{"sender":{postcode,subdivision_code,country},
//     "receiver":{…},"parcel_value","weight"(KG),"width","length","height"}]}
//     → {"data":[{"quotations":[{"courier":{…},"pricing":{"currency","total_amount",…}}]}]}
//     The version segment is part of the path (2026-09 today) and is
//     configurable, so a version bump is a config change, not a code change.
//   - a quotation is FREE and does not book anything; this adapter never submits
//     an order (that is money, and phase 2 is quote-only).
//
// SUFFIXES: the subdivision code is ISO 3166-2 (MY-07 = Pulau Pinang), derived
// from the state name through the SAME alias table the zone resolver uses — so
// "Penang", "Pulau Pinang" and "pinang" all produce MY-07 and a merchant typing
// one of them cannot silently lose the courier.
package shipping

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// EasyParcel endpoints + defaults.
const (
	EasyParcelDefaultAPIBase      = "https://api.easyparcel.com"
	EasyParcelDefaultAPIVersion   = "2026-09"
	EasyParcelDefaultAuthorizeURL = "https://api.easyparcel.com/oauth/login"
	EasyParcelDefaultTokenURL     = "https://api.easyparcel.com/oauth/token"
	// EasyParcelMaxTimeout caps a quotation call (it is in the checkout path).
	EasyParcelMaxTimeout = 12 * time.Second
	// EasyParcelEnvironment is what this adapter reports as its environment: it
	// is decided by the authorized ACCOUNT, not by a URL or a key prefix.
	EasyParcelEnvironment = "account"
)

// EasyParcelConfig is the OAuth application credential set (never logged).
type EasyParcelConfig struct {
	ClientID     string
	ClientSecret string
	AuthorizeURL string
	TokenURL     string
	// RedirectStaging / RedirectProd are the registered callback URLs. The
	// environment is picked from the running deployment's own base URL, not from
	// the credentials.
	RedirectStaging string
	RedirectProd    string
	APIBase         string
	APIVersion      string
}

// Configured reports whether the OAuth app credentials are present. The app is
// not environment-scoped, so this single pair serves sandbox and live.
func (c EasyParcelConfig) Configured() bool {
	return strings.TrimSpace(c.ClientID) != "" && strings.TrimSpace(c.ClientSecret) != ""
}

// AuthorizeEndpoint returns the consent URL's origin.
func (c EasyParcelConfig) AuthorizeEndpoint() string {
	if s := strings.TrimSpace(c.AuthorizeURL); s != "" {
		return s
	}
	return EasyParcelDefaultAuthorizeURL
}

// TokenEndpoint returns the token exchange URL.
func (c EasyParcelConfig) TokenEndpoint() string {
	if s := strings.TrimSpace(c.TokenURL); s != "" {
		return s
	}
	return EasyParcelDefaultTokenURL
}

// APIBaseURL returns the API root (no trailing slash).
func (c EasyParcelConfig) APIBaseURL() string {
	if s := strings.TrimSpace(c.APIBase); s != "" {
		return strings.TrimRight(s, "/")
	}
	return EasyParcelDefaultAPIBase
}

// Version returns the API version path segment.
func (c EasyParcelConfig) Version() string {
	if s := strings.TrimSpace(c.APIVersion); s != "" {
		return strings.Trim(s, "/")
	}
	return EasyParcelDefaultAPIVersion
}

// RedirectURI picks the registered callback for the deployment. `env` is the
// deployment environment ("prod" uses the production URL); anything else uses the
// staging one. A deployment that registered neither (a local dev box) gets "" and
// the connect attempt refuses loudly rather than sending a wrong redirect_uri —
// EasyParcel validates redirect_uri against the registered value, so a wrong one
// is an opaque consent-page error.
func (c EasyParcelConfig) RedirectURI(env string) string {
	if strings.EqualFold(strings.TrimSpace(env), "prod") {
		return strings.TrimSpace(c.RedirectProd)
	}
	return strings.TrimSpace(c.RedirectStaging)
}

// AuthorizeURL builds the consent link for one connect attempt. state is the
// single-use CSRF/attempt token the callback must match.
//
// The URL is handed to a HUMAN (a link the merchant opens on any device). It
// contains no credential: the client id is public by OAuth design, and the secret
// is only used in the token exchange.
func EasyParcelAuthorizeURL(cfg EasyParcelConfig, state, redirectURI string) (string, error) {
	if !cfg.Configured() {
		return "", ErrProviderUnavailable
	}
	if strings.TrimSpace(state) == "" || strings.TrimSpace(redirectURI) == "" {
		return "", errors.New("redirect_uri and state are required for the authorization link")
	}
	u, err := url.Parse(cfg.AuthorizeEndpoint())
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("client_id", strings.TrimSpace(cfg.ClientID))
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// TokenSet is one OAuth grant. It is the value that gets SEALED before storage;
// it never appears in a log, a response body or a sale record.
type TokenSet struct {
	AccessToken      string
	RefreshToken     string
	TokenType        string
	ExpiresAt        time.Time
	RefreshExpiresAt time.Time
}

// Usable reports whether the access token is present and not (nearly) expired.
// `skew` is how far ahead of expiry to treat it as unusable, so a token that
// expires mid-request is refreshed up front.
func (t TokenSet) Usable(now time.Time, skew time.Duration) bool {
	if strings.TrimSpace(t.AccessToken) == "" {
		return false
	}
	if t.ExpiresAt.IsZero() {
		return true
	}
	return now.Add(skew).Before(t.ExpiresAt)
}

// TokenStore persists one workspace's OAuth grant. The implementation SEALS
// the value at rest (dbstore seals it with the deployment's encryption key); this
// interface exists so the adapter can refresh a token without knowing how it is
// stored, and so the tests can run with an in-memory store.
//
// It is declared in connection.go (together with the Connection it belongs to) —
// see that file for why the interface lives on the consumer side.

// easyParcelTokenResponse is the token endpoint's answer.
type easyParcelTokenResponse struct {
	TokenType            string   `json:"token_type"`
	ExpiresIn            int64    `json:"expires_in"`
	AccessToken          string   `json:"access_token"`
	RefreshToken         string   `json:"refresh_token"`
	RefreshExpiresIn     int64    `json:"refresh_token_expires_in"`
	RefreshExpiresAt     string   `json:"refresh_token_expires_at"`
	Error                string   `json:"error"`
	ErrorDescription     string   `json:"error_description"`
	Scope                string   `json:"scope"`
	AdditionalProperties struct{} `json:"-"`
}

// EasyParcelExchangeCode swaps a consent code for a token set. It is the ONLY
// part of the connect flow that uses the client secret, and it runs server-side
// after the merchant's browser returns.
func EasyParcelExchangeCode(ctx context.Context, hc *http.Client, cfg EasyParcelConfig, code, redirectURI, state string) (TokenSet, error) {
	if !cfg.Configured() {
		return TokenSet{}, ErrProviderUnavailable
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", redirectURI)
	form.Set("code", code)
	if s := strings.TrimSpace(state); s != "" {
		form.Set("state", s)
	}
	return easyParcelToken(ctx, hc, cfg, form)
}

// EasyParcelRefresh renews an expired access token with the stored refresh token.
func EasyParcelRefresh(ctx context.Context, hc *http.Client, cfg EasyParcelConfig, refreshToken string) (TokenSet, error) {
	if !cfg.Configured() {
		return TokenSet{}, ErrProviderUnavailable
	}
	if strings.TrimSpace(refreshToken) == "" {
		return TokenSet{}, ErrProviderUnavailable
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	return easyParcelToken(ctx, hc, cfg, form)
}

// easyParcelToken performs the token call (Basic auth + form body) and maps the
// answer onto a TokenSet.
func easyParcelToken(ctx context.Context, hc *http.Client, cfg EasyParcelConfig, form url.Values) (TokenSet, error) {
	if hc == nil {
		hc = &http.Client{Timeout: EasyParcelMaxTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenEndpoint(),
		strings.NewReader(form.Encode()))
	if err != nil {
		return TokenSet{}, ErrProviderFault
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	// OAuth 2.0 requires the secret to reach the token endpoint; it is sent as
	// HTTP Basic and never written to a log, a comment or a stored value.
	req.SetBasicAuth(strings.TrimSpace(cfg.ClientID), strings.TrimSpace(cfg.ClientSecret))
	resp, err := hc.Do(req)
	if err != nil {
		return TokenSet{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out easyParcelTokenResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return TokenSet{}, fmt.Errorf("%w: token exchange: HTTP %d", ErrProviderFault, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 || strings.TrimSpace(out.AccessToken) == "" {
		// The vendor's OAuth error code is a stable token ("invalid_grant"); its
		// description can quote our own request, so it is not kept.
		return TokenSet{}, fmt.Errorf("%w: token exchange: HTTP %d %s", ErrProviderFault, resp.StatusCode, strings.TrimSpace(out.Error))
	}
	now := time.Now().UTC()
	ts := TokenSet{
		AccessToken:  strings.TrimSpace(out.AccessToken),
		RefreshToken: strings.TrimSpace(out.RefreshToken),
		TokenType:    strings.TrimSpace(out.TokenType),
	}
	if out.ExpiresIn > 0 {
		ts.ExpiresAt = now.Add(time.Duration(out.ExpiresIn) * time.Second)
	}
	if s := strings.TrimSpace(out.RefreshExpiresAt); s != "" {
		if t, perr := time.Parse(time.RFC3339, s); perr == nil {
			ts.RefreshExpiresAt = t.UTC()
		}
	} else if out.RefreshExpiresIn > 0 {
		ts.RefreshExpiresAt = now.Add(time.Duration(out.RefreshExpiresIn) * time.Second)
	}
	return ts, nil
}

// EasyParcel is the Provider implementation.
type EasyParcel struct {
	cfg     EasyParcelConfig
	tokens  TokenStore
	hc      *http.Client
	now     func() time.Time
	refresh func(ctx context.Context, refreshToken string) (TokenSet, error)
}

// NewEasyParcel builds the adapter. A nil store means "this deployment cannot
// hold a grant", which reports Available() == false: the connect surface still
// shows the provider so an operator sees WHY nothing is quoted.
func NewEasyParcel(cfg EasyParcelConfig, store TokenStore, hc *http.Client) *EasyParcel {
	if hc == nil {
		hc = &http.Client{Timeout: EasyParcelMaxTimeout}
	}
	e := &EasyParcel{cfg: cfg, tokens: store, hc: hc, now: time.Now}
	e.refresh = func(ctx context.Context, refreshToken string) (TokenSet, error) {
		return EasyParcelRefresh(ctx, hc, cfg, refreshToken)
	}
	return e
}

// Name is the registry key.
func (e *EasyParcel) Name() string { return ProviderEasyParcel }

// Environment is EasyParcelEnvironment: the environment is decided by the
// authorized account, not by anything this adapter holds.
func (e *EasyParcel) Environment() string { return EasyParcelEnvironment }

// Available needs the OAuth app AND a store to hold a grant. Whether a given
// WORKSPACE is connected is a per-account question the caller asks the store.
func (e *EasyParcel) Available() bool {
	return e != nil && e.cfg.Configured() && e.tokens != nil
}

// accessToken returns a usable bearer token, refreshing once when the stored one
// is expired. A missing grant is ErrProviderUnavailable (a fallback), never a
// 500: a workspace that has not connected EasyParcel must not break its form.
func (e *EasyParcel) accessToken(ctx context.Context, accountID string) (string, error) {
	if !e.Available() {
		return "", ErrProviderUnavailable
	}
	ts, found, err := e.tokens.Load(ctx, accountID, e.Name())
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrProviderFault, err)
	}
	if !found || !ts.Usable(e.now(), 60*time.Second) {
		if !found || strings.TrimSpace(ts.RefreshToken) == "" {
			if found {
				_ = e.tokens.MarkError(ctx, accountID, e.Name(), "expired")
			}
			return "", ErrProviderUnavailable
		}
		refreshed, rerr := e.refresh(ctx, ts.RefreshToken)
		if rerr != nil {
			_ = e.tokens.MarkError(ctx, accountID, e.Name(), "refresh_failed")
			if errors.Is(rerr, ErrProviderUnavailable) {
				return "", ErrProviderUnavailable
			}
			return "", rerr
		}
		if strings.TrimSpace(refreshed.RefreshToken) == "" {
			refreshed.RefreshToken = ts.RefreshToken
		}
		if refreshed.RefreshExpiresAt.IsZero() {
			refreshed.RefreshExpiresAt = ts.RefreshExpiresAt
		}
		if serr := e.tokens.Save(ctx, accountID, e.Name(), refreshed); serr != nil {
			return "", fmt.Errorf("%w: %v", ErrProviderFault, serr)
		}
		return refreshed.AccessToken, nil
	}
	return ts.AccessToken, nil
}

// easyParcelQuoteRequest is the quotation wire body. Weight is KG (the vendor's
// unit) and is emitted from the gram value as a bare JSON number, so no float
// arithmetic touches it; parcel_value is money and is emitted in the account
// currency from integer sen.
type easyParcelQuoteRequest struct {
	APIRateOn bool `json:"api_rate_on"`
	Shipment  []struct {
		Sender      easyParcelStop `json:"sender"`
		Receiver    easyParcelStop `json:"receiver"`
		ParcelValue json.Number    `json:"parcel_value,omitempty"`
		Weight      json.Number    `json:"weight"`
		Width       json.Number    `json:"width,omitempty"`
		Length      json.Number    `json:"length,omitempty"`
		Height      json.Number    `json:"height,omitempty"`
	} `json:"shipment"`
}

type easyParcelStop struct {
	Postcode        string `json:"postcode"`
	SubdivisionCode string `json:"subdivision_code"`
	Country         string `json:"country"`
}

// easyParcelQuoteResponse is the part of the answer this adapter reads.
type easyParcelQuoteResponse struct {
	StatusCode int    `json:"status_code"`
	Message    string `json:"message"`
	Data       []struct {
		Status     string                `json:"status"`
		Quotations []easyParcelQuotation `json:"quotations"`
	} `json:"data"`
}

// easyParcelQuotation is one rate in the answer (courier + pricing).
type easyParcelQuotation struct {
	Courier struct {
		ServiceID   string `json:"service_id"`
		ServiceName string `json:"service_name"`
		CourierName string `json:"courier_name"`
	} `json:"courier"`
	Pricing struct {
		Currency string `json:"currency"`
		// The pricing members are `any` rather than json.Number: the vendor has
		// been seen to answer a string ("N/A") for a component, and a
		// json.Number field makes that a hard unmarshal FAILURE — which would
		// turn one bad component into "no rate at all". ParseAmountSen handles
		// both shapes and an unparseable value is skipped, never treated as a
		// free shipment.
		TotalAmount        any `json:"total_amount"`
		ShipmentPrice      any `json:"shipment_price"`
		ShipmentTax        any `json:"shipment_tax"`
		TotalFeaturesPrice any `json:"total_features_price"`
		SellerPayable      any `json:"seller_payable_amount"`
	} `json:"pricing"`
}

// Quote asks EasyParcel for rates and returns the CHEAPEST one.
//
// The cheapest is chosen deliberately: this fee is charged to a buyer who cannot
// see the courier list, so silently picking an expensive service would be a
// different (and invisible) decision. The service that was picked, and its
// courier, are both recorded on the sale so the merchant can see which one sold.
func (e *EasyParcel) Quote(ctx context.Context, req QuoteRequest) (ProviderQuote, error) {
	token, err := e.accessToken(ctx, req.AccountID)
	if err != nil {
		return ProviderQuote{}, err
	}
	country := strings.ToUpper(strings.TrimSpace(req.Country))
	if country == "" {
		country = "MY"
	}
	if country != "MY" {
		// The registered EasyParcel account is Malaysian; a non-MY stop cannot
		// be priced by it.
		return ProviderQuote{}, ErrProviderNoCoverage
	}
	originSub := MalaysiaSubdivision(req.Origin.State)
	destSub := MalaysiaSubdivision(req.Destination.State)
	originPostcode := strings.TrimSpace(req.Origin.Postcode)
	destPostcode := strings.TrimSpace(req.Destination.Postcode)
	if originSub == "" || destSub == "" || originPostcode == "" || destPostcode == "" {
		// subdivision_code and postcode are REQUIRED by the API: without them
		// EasyParcel cannot price the lane, and inventing a code would price a
		// different one.
		return ProviderQuote{}, ErrProviderNoCoverage
	}
	weight := json.Number(kgFromGrams(req.WeightG))
	if req.WeightG <= 0 {
		// The API requires a weight. A block that declares none is priced as a
		// 1 kg parcel, which is stated in the recorded detail so the assumption
		// is visible on the sale rather than buried here.
		weight = json.Number("1")
	}
	body := easyParcelQuoteRequest{APIRateOn: true}
	var item struct {
		Sender      easyParcelStop `json:"sender"`
		Receiver    easyParcelStop `json:"receiver"`
		ParcelValue json.Number    `json:"parcel_value,omitempty"`
		Weight      json.Number    `json:"weight"`
		Width       json.Number    `json:"width,omitempty"`
		Length      json.Number    `json:"length,omitempty"`
		Height      json.Number    `json:"height,omitempty"`
	}
	item.Sender = easyParcelStop{Postcode: originPostcode, SubdivisionCode: originSub, Country: country}
	item.Receiver = easyParcelStop{Postcode: destPostcode, SubdivisionCode: destSub, Country: country}
	item.Weight = weight
	if req.ParcelValueSen > 0 {
		item.ParcelValue = json.Number(FormatSen(req.ParcelValueSen))
	}
	if req.Dimensions.WidthCM > 0 {
		item.Width = json.Number(trimFloat(req.Dimensions.WidthCM))
	}
	if req.Dimensions.LengthCM > 0 {
		item.Length = json.Number(trimFloat(req.Dimensions.LengthCM))
	}
	if req.Dimensions.HeightCM > 0 {
		item.Height = json.Number(trimFloat(req.Dimensions.HeightCM))
	}
	body.Shipment = append(body.Shipment, item)

	payload, merr := json.Marshal(body)
	if merr != nil {
		return ProviderQuote{}, ErrProviderFault
	}
	path := fmt.Sprintf("/open_api/%s/shipment/quotations", e.cfg.Version())
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.cfg.APIBaseURL()+path,
		strings.NewReader(string(payload)))
	if err != nil {
		return ProviderQuote{}, ErrProviderFault
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	resp, err := e.hc.Do(httpReq)
	if err != nil {
		return ProviderQuote{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		// The grant is no longer accepted. Recording the CLASS is what lets the
		// dashboard tell the merchant to reconnect instead of showing a rate that
		// silently is not live.
		_ = e.tokens.MarkError(ctx, req.AccountID, e.Name(), "rejected")
		return ProviderQuote{}, ErrProviderUnavailable
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return ProviderQuote{}, fmt.Errorf("%w: quotations: HTTP %d", ErrProviderFault, resp.StatusCode)
	}
	var out easyParcelQuoteResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return ProviderQuote{}, ErrProviderFault
	}
	best, ok := cheapestEasyParcel(out)
	if !ok {
		// "no courier can take this lane" is coverage, and coverage falls back.
		return ProviderQuote{}, ErrProviderNoCoverage
	}
	sen, aerr := ParseAmountSen(best.Pricing.TotalAmount)
	if aerr != nil {
		return ProviderQuote{}, ErrProviderFault
	}
	currency := strings.TrimSpace(best.Pricing.Currency)
	if currency == "" {
		currency = "MYR"
	}
	detail := map[string]any{
		"service_name": best.Courier.ServiceName,
		"courier_name": best.Courier.CourierName,
		"service_id":   best.Courier.ServiceID,
		"weight_g":     req.WeightG,
		"api_version":  e.cfg.Version(),
	}
	for key, raw := range map[string]any{
		"shipment_price_sen": best.Pricing.ShipmentPrice,
		"shipment_tax_sen":   best.Pricing.ShipmentTax,
		"features_price_sen": best.Pricing.TotalFeaturesPrice,
		"seller_payable_sen": best.Pricing.SellerPayable,
	} {
		if v, perr := ParseAmountSen(raw); perr == nil {
			detail[key] = v
		}
	}
	return ProviderQuote{
		Provider:    ProviderEasyParcel,
		Environment: EasyParcelEnvironment,
		PriceSen:    sen,
		Currency:    currency,
		ServiceName: strings.TrimSpace(best.Courier.ServiceName),
		CourierName: strings.TrimSpace(best.Courier.CourierName),
		Detail:      detail,
	}, nil
}

// cheapestEasyParcel picks the lowest-priced quotation in sen. An unparseable
// price is skipped rather than treated as free (a 0 that is really "N/A" would
// sell shipping for nothing).
func cheapestEasyParcel(out easyParcelQuoteResponse) (easyParcelQuotation, bool) {
	var best easyParcelQuotation
	var bestSen int64
	found := false
	for _, d := range out.Data {
		for _, q := range d.Quotations {
			sen, err := ParseAmountSen(q.Pricing.TotalAmount)
			if err != nil || sen < 0 {
				continue
			}
			if !found || sen < bestSen {
				best, bestSen, found = q, sen, true
			}
		}
	}
	return best, found
}

// kgFromGrams renders a gram weight as the vendor's KG decimal ("1500" -> "1.5").
// Integer arithmetic: no float ever touches a weight that will be billed.
func kgFromGrams(g int64) string {
	if g <= 0 {
		return "1"
	}
	whole := g / 1000
	frac := g % 1000
	if frac == 0 {
		return strconv.FormatInt(whole, 10)
	}
	s := strconv.FormatInt(frac+1000, 10)
	// s is 4 digits ("1500"); the fraction is its last three.
	return strconv.FormatInt(whole, 10) + "." + strings.TrimRight(s[1:], "0")
}

// trimFloat renders a dimension without a trailing ".0" for whole centimetres.
func trimFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	return s
}

// malaysiaSubdivisions maps a canonical state key (see stateKey) to its
// ISO 3166-2:MY code. EasyParcel requires the subdivision code, not the name, and
// a wrong code prices a different lane, so the map is exhaustive over the
// resolver's alias table.
var malaysiaSubdivisions = map[string]string{
	"johor":           "MY-01",
	"kedah":           "MY-02",
	"kelantan":        "MY-03",
	"melaka":          "MY-04",
	"negeri sembilan": "MY-05",
	"pahang":          "MY-06",
	"penang":          "MY-07",
	"perak":           "MY-08",
	"perlis":          "MY-09",
	"selangor":        "MY-10",
	"terengganu":      "MY-11",
	"sabah":           "MY-12",
	"sarawak":         "MY-13",
	"kuala lumpur":    "MY-14",
	"labuan":          "MY-15",
	"putrajaya":       "MY-16",
}

// MalaysiaSubdivision returns the ISO 3166-2:MY code for a Malaysian state name
// in any accepted spelling ("Penang", "Pulau Pinang", "penang" → "MY-07"), or ""
// when the name is not a Malaysian state.
func MalaysiaSubdivision(state string) string {
	return malaysiaSubdivisions[stateKey(state)]
}
