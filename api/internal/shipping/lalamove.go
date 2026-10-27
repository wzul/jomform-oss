// Lalamove adapter (v3 API, HMAC-signed).
//
// MEASURED AGAINST THE SANDBOX, not guessed:
//
//   - the request body MUST be wrapped in `{"data": …}`. The flat body (the shape
//     Lalamove's own older examples show) is answered with
//     `401 ERR_AUTHENTICATE "HMAC signature doesn't match the provided payload"`,
//     which reads like a signing bug and is not: the same key/secret signs the
//     wrapped body successfully. This cost a probe to find, so it is pinned by a
//     test (TestLalamoveRequestIsWrappedInData).
//   - `Market: MY` is required and IS the market header (a `X-LLM-Market` header
//     is answered with `422 ERR_INVALID_MARKET`).
//   - a real sandbox quotation answers
//     `{"data":{"quotationId":"…","priceBreakdown":{"total":"8.2","currency":"MYR"},
//     "expiresAt":"…"}}` — total is a STRING decimal and expiresAt carried 30
//     SECONDS of validity in the sandbox (the docs promise 5 minutes).
//
// NO BROWSER, NO ORDER. Quoting is server-to-server and this adapter never
// submits an order: placing one spends real money on a production key and books a
// driver on a sandbox one, and phase 2 is explicitly quote-only. There is also no
// idempotency key on this API, so a quotation must be used at most once; the
// order path asks for it once per submission and reuses it for a duplicate.
//
// MALAYSIA IS INTRA-CITY. A Lalamove quotation needs coordinates for BOTH stops;
// the workspace's connection supplies the pickup point and the matched zone
// supplies the drop-off point. Without either, this adapter refuses with
// ErrProviderNoCoverage and the caller falls back to the zone rate — it never
// invents a coordinate.
package shipping

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Lalamove API roots. The sandbox root serves the simulated market (no money,
// no driver); the production root is only reachable with pk_prod_ keys Wan has
// not obtained yet.
const (
	LalamoveSandboxBase = "https://rest.sandbox.lalamove.com"
	LalamoveProdBase    = "https://rest.lalamove.com"
	// LalamoveAPIVersion is the path prefix the signature covers.
	LalamoveAPIVersion = "/v3"
	// LalamoveDefaultMarket is the market header for Malaysia.
	LalamoveDefaultMarket = "MY"
	// LalamoveDefaultServiceType prices a motorcycle (the cheapest Malaysian
	// service); a block may name MOTORCYCLE / CAR / VAN / 4X4.
	LalamoveDefaultServiceType = "MOTORCYCLE"
	// LalamoveMaxTimeout caps what we will wait for a quotation. A quotation is
	// in the buyer's checkout path.
	LalamoveMaxTimeout = 15 * time.Second
)

// LalamoveConfig is the runtime credential set (see credentials.go: never logged,
// never returned).
type LalamoveConfig struct {
	Key    string
	Secret string
	// Base is the API root, with or without the /v3 suffix ("https://rest.sandbox.lalamove.com").
	Base string
	// Market is the market header (MY).
	Market string
}

// Configured reports whether both halves of the key pair are present.
func (c LalamoveConfig) Configured() bool {
	return strings.TrimSpace(c.Key) != "" && strings.TrimSpace(c.Secret) != ""
}

// Environment reports "sandbox" or "prod" from the key prefix — the prefix is
// the only environment marker this API has. An unrecognised prefix is "" (we do
// not guess), and the connection row then says "unknown".
func (c LalamoveConfig) Environment() string {
	switch {
	case strings.HasPrefix(c.Key, "pk_test_"):
		return "sandbox"
	case strings.HasPrefix(c.Key, "pk_prod_"):
		return "prod"
	default:
		return ""
	}
}

// origin returns the API origin (scheme + host), with any /v3 suffix removed so
// the signed path is always exactly /v3/....
func (c LalamoveConfig) origin() string {
	base := strings.TrimRight(strings.TrimSpace(c.Base), "/")
	if base == "" {
		if c.Environment() == "prod" {
			return LalamoveProdBase
		}
		return LalamoveSandboxBase
	}
	return strings.TrimSuffix(base, LalamoveAPIVersion)
}

// market returns the market header, defaulting to MY.
func (c LalamoveConfig) market() string {
	if m := strings.ToUpper(strings.TrimSpace(c.Market)); m != "" {
		return m
	}
	return LalamoveDefaultMarket
}

// LalamoveSignature is the v3 HMAC: lowercase hex HMAC-SHA256 over
//
//	TIMESTAMP \r\n METHOD \r\n PATH \r\n \r\n BODY
//
// signed with the API secret, sent as `Authorization: hmac KEY:TIMESTAMP:SIG`.
// Exported (and unit-tested against a fixed vector) because it is the one piece
// of this API that is silently wrong in every other implementation.
func LalamoveSignature(secret, timestamp, method, path, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "\r\n" + method + "\r\n" + path + "\r\n\r\n" + body))
	return hex.EncodeToString(mac.Sum(nil))
}

// Lalamove is the Provider implementation.
type Lalamove struct {
	cfg LalamoveConfig
	hc  *http.Client
	now func() time.Time
	// requestID makes the Request-ID header injectable for tests; nil uses a
	// random one per call.
	requestID func() string
}

// NewLalamove builds the adapter. A client may be nil (a bounded default is used).
func NewLalamove(cfg LalamoveConfig, hc *http.Client) *Lalamove {
	if hc == nil {
		hc = &http.Client{Timeout: LalamoveMaxTimeout}
	}
	return &Lalamove{cfg: cfg, hc: hc, now: time.Now}
}

// Name is the registry key.
func (l *Lalamove) Name() string { return ProviderLalamove }

// Environment reports the credential environment (sandbox/prod/"").
func (l *Lalamove) Environment() string { return l.cfg.Environment() }

// Available is false without a usable key pair, which is the state a deployment
// with no production keys is in. The caller falls back to the zone table.
func (l *Lalamove) Available() bool { return l != nil && l.cfg.Configured() }

// lalamoveQuotationRequest is the wire body. It is nested in `data` by
// Quote (see the file comment: the flat shape is rejected).
type lalamoveQuotationRequest struct {
	ServiceType string            `json:"serviceType"`
	Language    string            `json:"language"`
	Stops       []lalamoveStop    `json:"stops"`
	Special     []json.RawMessage `json:"specialRequests"`
}

type lalamoveStop struct {
	Coordinates lalamoveLatLng `json:"coordinates"`
	Address     string         `json:"address"`
}

type lalamoveLatLng struct {
	Lat string `json:"lat"`
	Lng string `json:"lng"`
}

// lalamoveQuotationResponse is the part of the answer this adapter reads.
type lalamoveQuotationResponse struct {
	Data struct {
		QuotationID    string `json:"quotationId"`
		ServiceType    string `json:"serviceType"`
		ExpiresAt      string `json:"expiresAt"`
		PriceBreakdown struct {
			Base      string `json:"base"`
			Total     string `json:"total"`
			Currency  string `json:"currency"`
			ExtraMile string `json:"extraMileage"`
		} `json:"priceBreakdown"`
		Distance struct {
			Value string `json:"value"`
		} `json:"distance"`
	} `json:"data"`
	Errors []lalamoveError `json:"errors"`
}

// lalamoveError is one vendor error. Only the ID is ever kept (a stable token
// like ERR_INSUFFICIENT_STOPS); the message is never stored or shown.
type lalamoveError struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

// Quote asks Lalamove for a quotation. Both stops must carry coordinates.
func (l *Lalamove) Quote(ctx context.Context, req QuoteRequest) (ProviderQuote, error) {
	if !l.Available() {
		return ProviderQuote{}, ErrProviderUnavailable
	}
	if !validCoord(req.OriginLat, req.OriginLng) || !validCoord(req.DestinationLat, req.DestinationLng) {
		// Lalamove prices a route between two points; a postcode is not a point.
		// Refusing here is what turns "the zone has no coordinates" into a clean
		// fallback instead of a quotation between two made-up places.
		return ProviderQuote{}, ErrProviderNoCoverage
	}
	service := strings.ToUpper(strings.TrimSpace(req.ServiceType))
	if service == "" {
		service = LalamoveDefaultServiceType
	}
	body := lalamoveQuotationRequest{
		ServiceType: service,
		Language:    "en_MY",
		Special:     []json.RawMessage{},
		Stops: []lalamoveStop{
			{Coordinates: lalamoveLatLng{Lat: strings.TrimSpace(req.OriginLat), Lng: strings.TrimSpace(req.OriginLng)}, Address: stopAddress(req.Origin, "Penghantaran")},
			{Coordinates: lalamoveLatLng{Lat: strings.TrimSpace(req.DestinationLat), Lng: strings.TrimSpace(req.DestinationLng)}, Address: stopAddress(req.Destination, "Penerima")},
		},
	}
	inner, err := json.Marshal(body)
	if err != nil {
		return ProviderQuote{}, ErrProviderFault
	}
	// The wrapper is load-bearing (see the file comment).
	payload := append([]byte(`{"data":`), inner...)
	payload = append(payload, '}')

	path := LalamoveAPIVersion + "/quotations"
	raw, status, vendorErr, err := l.do(ctx, "POST", path, payload)
	if err != nil {
		return ProviderQuote{}, err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		// A rejected key pair is "not usable here", which is a fallback, not a
		// fault of the buyer's order.
		return ProviderQuote{}, ErrProviderUnavailable
	}
	if status < 200 || status > 299 {
		if isCoverageError(vendorErr) {
			return ProviderQuote{}, ErrProviderNoCoverage
		}
		return ProviderQuote{}, fmt.Errorf("%w: %s", ErrProviderFault, vendorErr)
	}
	var out lalamoveQuotationResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return ProviderQuote{}, ErrProviderFault
	}
	if id := strings.TrimSpace(out.Data.QuotationID); id == "" {
		if isCoverageError(errID(out.Errors)) {
			return ProviderQuote{}, ErrProviderNoCoverage
		}
		return ProviderQuote{}, ErrProviderFault
	}
	sen, err := ParseAmountSen(out.Data.PriceBreakdown.Total)
	if err != nil {
		return ProviderQuote{}, ErrProviderFault
	}
	currency := strings.TrimSpace(out.Data.PriceBreakdown.Currency)
	if currency == "" {
		currency = "MYR"
	}
	detail := map[string]any{
		"service_type": out.Data.ServiceType,
		"total_sen":    sen,
	}
	if base, err := ParseAmountSen(out.Data.PriceBreakdown.Base); err == nil {
		detail["base_sen"] = base
	}
	if extra, err := ParseAmountSen(out.Data.PriceBreakdown.ExtraMile); err == nil {
		detail["extra_mileage_sen"] = extra
	}
	if dist := strings.TrimSpace(out.Data.Distance.Value); dist != "" {
		detail["distance_m"] = dist
	}
	var expires time.Time
	if s := strings.TrimSpace(out.Data.ExpiresAt); s != "" {
		if t, perr := time.Parse(time.RFC3339, s); perr == nil {
			expires = t.UTC()
		}
	}
	return ProviderQuote{
		Provider:    ProviderLalamove,
		Environment: l.cfg.Environment(),
		PriceSen:    sen,
		Currency:    currency,
		QuoteID:     strings.TrimSpace(out.Data.QuotationID),
		ExpiresAt:   expires,
		ServiceName: strings.TrimSpace(out.Data.ServiceType),
		CourierName: "Lalamove",
		Detail:      detail,
	}, nil
}

// do signs and performs one request, returning the body, the status and the
// vendor's error id (all safe to keep: no credential is ever in them).
func (l *Lalamove) do(ctx context.Context, method, path string, payload []byte) ([]byte, int, string, error) {
	ts := fmt.Sprintf("%d", l.now().UnixMilli())
	sig := LalamoveSignature(l.cfg.Secret, ts, method, path, string(payload))
	httpReq, err := http.NewRequestWithContext(ctx, method, l.cfg.origin()+path, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, "", ErrProviderFault
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Market", l.cfg.market())
	httpReq.Header.Set("Request-ID", l.newRequestID())
	// The key is part of the auth header by design of this API; the SECRET
	// never leaves the signature.
	httpReq.Header.Set("Authorization", "hmac "+l.cfg.Key+":"+ts+":"+sig)

	resp, err := l.hc.Do(httpReq)
	if err != nil {
		return nil, 0, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return raw, resp.StatusCode, vendorErrorID(raw), nil
}

// newRequestID returns a random request id (Lalamove accepts and echoes one).
func (l *Lalamove) newRequestID() string {
	if l.requestID != nil {
		return l.requestID()
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("jf-%d", time.Now().UnixNano())
	}
	return "jf-" + hex.EncodeToString(b[:])
}

// vendorErrorID pulls the first `errors[].id` out of a vendor error body.
// Only the ID is kept: it is a stable token ("ERR_INSUFFICIENT_STOPS"), unlike
// the message, and it cannot contain credential material.
func vendorErrorID(raw []byte) string {
	var shape struct {
		Errors []lalamoveError `json:"errors"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		return ""
	}
	if len(shape.Errors) == 0 {
		return ""
	}
	return shape.Errors[0].ID
}

func errID(errs []lalamoveError) string {
	if len(errs) == 0 {
		return ""
	}
	return errs[0].ID
}

// isCoverageError reports whether a vendor error means "this courier cannot take
// this job" (which is a fallback) rather than "the call was wrong".
func isCoverageError(id string) bool {
	switch strings.ToUpper(strings.TrimSpace(id)) {
	case "ERR_INSUFFICIENT_STOPS", "ERR_OUT_OF_SERVICE_AREA", "ERR_UNSERVICEABLE",
		"ERR_NO_DRIVER", "ERR_UNSUPPORTED_SERVICE_TYPE", "ERR_INVALID_COORDINATES":
		return true
	default:
		return false
	}
}

// validCoord reports whether a coordinate is a usable decimal number. It does
// not range-check (Lalamove answers that itself); it rejects the empty string
// and anything that is not a number, which is what "the zone has no point" looks
// like.
func validCoord(lat, lng string) bool {
	return isDecimal(lat) && isDecimal(lng)
}

// isDecimal reports whether s parses as a plain decimal (optional sign, digits,
// optional fraction). No exponent, no whitespace, no units.
func isDecimal(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if s[0] == '-' || s[0] == '+' {
		s = s[1:]
	}
	if s == "" {
		return false
	}
	seenDigit := false
	seenDot := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			seenDigit = true
		case r == '.':
			if seenDot {
				return false
			}
			seenDot = true
		default:
			return false
		}
	}
	return seenDigit
}

// stopAddress renders a stop's human address. Lalamove shows it to the driver,
// so it is the buyer's own words where we have them and a neutral label when the
// address was never submitted.
func stopAddress(a Address, fallback string) string {
	parts := make([]string, 0, 3)
	for _, p := range []string{a.District, a.State, a.Postcode} {
		if s := strings.TrimSpace(p); s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return fallback
	}
	return strings.Join(parts, ", ")
}
