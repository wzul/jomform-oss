package shipping

// Phase-2 courier tests, no network and no database: every adapter is driven
// against an httptest server (the vendor's own contract, byte for byte) and the
// resolver is driven against stub providers.
//
// WHAT THESE PROVE, and why each one exists:
//
//   - TestLalamoveSignatureMatchesTheDocumentedRawString: the HMAC is pinned to a
//     KNOWN ANSWER, because it is the one piece of that API whose failure mode is
//     a 401 that reads like a wrong key ("HMAC signature doesn't match the
//     provided payload"). The raw string is TIMESTAMP\r\nMETHOD\r\nPATH\r\n\r\nBODY.
//   - TestLalamoveQuoteWrapsTheBodyAndParsesTheQuotation: the measured contract —
//     the body MUST be `{"data": …}` (the flat shape is answered 401), the price is
//     a STRING decimal, and the quotation id + expiry are recorded.
//   - TestLalamoveWithoutACoordinateIsCoverage: a point-based courier asked about
//     an address with no point is a FALLBACK, not a fault — and never a quotation
//     between two invented coordinates.
//   - TestResolveCourierFallsBackWhenTheProviderCannotAnswer: the card's rule that
//     the zone table still charges the fee when the courier fails, times out, or
//     is not connected — with the reason class recorded.
//   - TestResolveCourierRefusesWithoutAFallback: with nothing to fall back to the
//     order is REFUSED, never charged 0.
//   - TestQuoteCacheReusesOneQuotationForADuplicateSubmission: the double-submit
//     guard — neither provider has an idempotency key, so an identical request
//     must not create a second quotation.
//   - TestEasyParcelQuoteSendsSubdivisionCodesAndPicksTheCheapest and its token
//     tests: the OAuth + quotation contract (Basic auth token exchange, ISO
//     3166-2:MY subdivision codes, weight in KG).
//   - TestCredentialsAbsenceIsAStateNotAnError: production Lalamove keys and a live
//     EasyParcel account are the operator's to obtain; their absence must be a
//     clear, non-crashing "not configured" state.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ── Lalamove ─────────────────────────────────────────────────────────────────

func TestLalamoveSignatureMatchesTheDocumentedRawString(t *testing.T) {
	// Known answer computed independently (python hmac.new(b"test-secret",
	// b"1700000000000\r\nPOST\r\n/v3/quotations\r\n\r\n" + body, sha256).hexdigest()).
	const want = "8ebecef8febe0743c97b59c68c570ffe9e2389281dabc8505a84ef462f790a5c"
	got := LalamoveSignature("test-secret", "1700000000000", "POST", "/v3/quotations", `{"data":{"a":1}}`)
	if got != want {
		t.Fatalf("signature = %s, want %s", got, want)
	}
	// The separator is the vendor's, not a formatting accident: a single \n in
	// place of the \r\n pair changes the signature (that is the bug this pins).
	if got == LalamoveSignature("test-secret", "1700000000000", "POST", "/v3/quotations", `{"data":{"a":1}}`[:len(`{"data":{"a":1}}`)-1]) {
		t.Fatal("the body is not part of the signature")
	}
}

// lalamoveServer answers /v3/quotations with a recorded sandbox response and
// records what it was sent.
type lalamoveServer struct {
	*httptest.Server
	gotPath    string
	gotBody    string
	gotAuth    string
	gotMarket  string
	gotReqID   string
	calls      int32
	status     int
	respBody   string
	assertions func(t *testing.T)
}

// newLalamoveServer builds the stub and asserts the request shape (the wrapped
// body is the measured contract; the flat body is answered 401 by the real API).
func newLalamoveServer(t *testing.T, status int, respBody string) *lalamoveServer {
	t.Helper()
	ls := &lalamoveServer{status: status, respBody: respBody}
	ls.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&ls.calls, 1)
		ls.gotPath = r.URL.Path
		ls.gotAuth = r.Header.Get("Authorization")
		ls.gotMarket = r.Header.Get("Market")
		ls.gotReqID = r.Header.Get("Request-ID")
		raw, _ := io.ReadAll(r.Body)
		ls.gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(ls.status)
		_, _ = w.Write([]byte(ls.respBody))
	}))
	t.Cleanup(ls.Close)
	return ls
}

func newTestLalamove(t *testing.T, base string) *Lalamove {
	t.Helper()
	l := NewLalamove(LalamoveConfig{
		Key: "pk_test_example", Secret: "secret-example", Base: base, Market: "MY",
	}, nil)
	l.now = func() time.Time { return time.UnixMilli(1700000000000) }
	l.requestID = func() string { return "req-fixed" }
	return l
}

const lalamoveSandboxQuotation = `{"data":{
  "quotationId":"3607053707382538954",
  "scheduleAt":"2026-10-16T02:24:29.00Z",
  "expiresAt":"2026-10-16T02:24:59.00Z",
  "serviceType":"MOTORCYCLE",
  "priceBreakdown":{"base":"4.0","extraMileage":"4.2","total":"8.2","currency":"MYR"},
  "distance":{"value":"10300"}
}}`

func TestLalamoveQuoteWrapsTheBodyAndParsesTheQuotation(t *testing.T) {
	srv := newLalamoveServer(t, http.StatusCreated, lalamoveSandboxQuotation)
	l := newTestLalamove(t, srv.URL)

	q, err := l.Quote(context.Background(), QuoteRequest{
		Origin:      Address{State: "Kuala Lumpur", Postcode: "50000"},
		Destination: Address{State: "Kuala Lumpur", Postcode: "50450"},
		OriginLat:   "3.1578", OriginLng: "101.7120",
		DestinationLat: "3.1390", DestinationLng: "101.6869",
	})
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	// THE WRAPPED BODY: the flat shape is answered 401 by the real API (measured).
	if !strings.HasPrefix(srv.gotBody, `{"data":`) {
		t.Fatalf("body = %s, want it wrapped in `data` (the flat shape is refused by Lalamove)", srv.gotBody)
	}
	if !strings.Contains(srv.gotBody, `"serviceType":"MOTORCYCLE"`) {
		t.Errorf("body missing the default service type: %s", srv.gotBody)
	}
	if !strings.HasPrefix(srv.gotAuth, "hmac pk_test_example:1700000000000:") {
		t.Errorf("Authorization = %q, want `hmac KEY:TIMESTAMP:SIGNATURE`", srv.gotAuth)
	}
	if srv.gotMarket != "MY" {
		t.Errorf("Market = %q, want MY", srv.gotMarket)
	}
	if srv.gotReqID != "req-fixed" {
		t.Errorf("Request-ID = %q, want the injected id", srv.gotReqID)
	}
	if q.PriceSen != 820 {
		t.Errorf("PriceSen = %d, want 820 (the string \"8.2\" is sen, never a float)", q.PriceSen)
	}
	if q.QuoteID != "3607053707382538954" {
		t.Errorf("QuoteID = %q", q.QuoteID)
	}
	if q.Environment != "sandbox" {
		t.Errorf("Environment = %q, want sandbox (pk_test_ prefix)", q.Environment)
	}
	if q.ExpiresAt.IsZero() {
		t.Error("ExpiresAt was not parsed — a quotation that expires must record when")
	}
	if q.Detail["extra_mileage_sen"] != int64(420) {
		t.Errorf("detail extra_mileage_sen = %v, want 420", q.Detail["extra_mileage_sen"])
	}
}

func TestLalamoveUnauthorizedIsUnavailableNotAFault(t *testing.T) {
	// The real answer to a rejected key pair is this, and it must read as "not
	// usable here" (a fallback) rather than as a provider fault.
	srv := newLalamoveServer(t, http.StatusUnauthorized,
		`{"errors":[{"id":"ERR_AUTHENTICATE","message":"HMAC signature doesn't match the provided payload."}]}`)
	l := newTestLalamove(t, srv.URL)
	_, err := l.Quote(context.Background(), QuoteRequest{
		OriginLat: "1", OriginLng: "1", DestinationLat: "2", DestinationLng: "2",
	})
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("err = %v, want ErrProviderUnavailable", err)
	}
	if strings.Contains(err.Error(), "HMAC") {
		t.Errorf("the vendor's message leaked into the error: %v", err)
	}
}

func TestLalamoveCoverageErrorIsCoverage(t *testing.T) {
	srv := newLalamoveServer(t, http.StatusUnprocessableEntity,
		`{"errors":[{"id":"ERR_INSUFFICIENT_STOPS"}]}`)
	l := newTestLalamove(t, srv.URL)
	_, err := l.Quote(context.Background(), QuoteRequest{
		OriginLat: "1", OriginLng: "1", DestinationLat: "2", DestinationLng: "2",
	})
	if !errors.Is(err, ErrProviderNoCoverage) {
		t.Fatalf("err = %v, want ErrProviderNoCoverage", err)
	}
}

func TestLalamoveWithoutACoordinateIsCoverage(t *testing.T) {
	srv := newLalamoveServer(t, http.StatusCreated, lalamoveSandboxQuotation)
	l := newTestLalamove(t, srv.URL)
	_, err := l.Quote(context.Background(), QuoteRequest{
		Origin:      Address{State: "Selangor"},
		Destination: Address{State: "Selangor"},
		// No coordinates: Lalamove prices a route between two points and a
		// postcode is not a point.
	})
	if !errors.Is(err, ErrProviderNoCoverage) {
		t.Fatalf("err = %v, want ErrProviderNoCoverage", err)
	}
	if atomic.LoadInt32(&srv.calls) != 0 {
		t.Error("the provider was called without coordinates — it must refuse locally")
	}
}

func TestLalamoveWithoutCredentialsIsUnavailable(t *testing.T) {
	l := NewLalamove(LalamoveConfig{}, nil)
	if l.Available() {
		t.Fatal("Available() = true without a key pair")
	}
	if _, err := l.Quote(context.Background(), QuoteRequest{}); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("err = %v, want ErrProviderUnavailable", err)
	}
	if l.Environment() != "" {
		t.Errorf("Environment = %q, want empty for an unrecognised/absent key", l.Environment())
	}
}

func TestLalamoveOriginStripsTheVersionSuffix(t *testing.T) {
	for _, base := range []string{
		"https://rest.sandbox.lalamove.com", "https://rest.sandbox.lalamove.com/",
		"https://rest.sandbox.lalamove.com/v3",
	} {
		l := NewLalamove(LalamoveConfig{Key: "pk_test_x", Secret: "s", Base: base, Market: "MY"}, nil)
		if got := l.cfg.origin(); got != "https://rest.sandbox.lalamove.com" {
			t.Errorf("origin(%q) = %q", base, got)
		}
	}
	// No base configured: the key prefix decides, so a prod key never talks to
	// the sandbox host.
	prod := NewLalamove(LalamoveConfig{Key: "pk_prod_x", Secret: "s"}, nil)
	if prod.cfg.origin() != LalamoveProdBase || prod.Environment() != "prod" {
		t.Errorf("prod origin = %q env = %q", prod.cfg.origin(), prod.Environment())
	}
}

// ── money ────────────────────────────────────────────────────────────────────

func TestParseAmountSenTakesTheVendorsDigits(t *testing.T) {
	cases := []struct {
		in   any
		want int64
		bad  bool
	}{
		{"8.2", 820, false},
		{"8.20", 820, false},
		{"RM8.20", 820, false},
		{" 1,234.5 ", 123450, false},
		{12.31, 1231, false}, // the float64 path: 12.31 * 100 is 1230.9999…
		{json.Number("16.55"), 1655, false},
		{"12.345", 1235, false}, // half-up on the third digit
		{"0", 0, false},
		{".5", 50, false},
		{"N/A", 0, true},
		{"", 0, true},
		{nil, 0, true},
		{"abc", 0, true},
	}
	for _, tc := range cases {
		got, err := ParseAmountSen(tc.in)
		if tc.bad {
			if err == nil {
				t.Errorf("ParseAmountSen(%#v) = %d, want an error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseAmountSen(%#v): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseAmountSen(%#v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// ── EasyParcel ───────────────────────────────────────────────────────────────

type easyParcelTokenStore struct {
	loaded    TokenSet
	found     bool
	saved     []TokenSet
	errors    []string
	loadCalls int32
}

func (s *easyParcelTokenStore) Load(context.Context, string, string) (TokenSet, bool, error) {
	atomic.AddInt32(&s.loadCalls, 1)
	return s.loaded, s.found, nil
}
func (s *easyParcelTokenStore) Save(_ context.Context, _, _ string, t TokenSet) error {
	s.saved = append(s.saved, t)
	s.loaded, s.found = t, true
	return nil
}
func (s *easyParcelTokenStore) MarkError(_ context.Context, _, _, class string) error {
	s.errors = append(s.errors, class)
	return nil
}

func testEasyParcelConfig(base string) EasyParcelConfig {
	return EasyParcelConfig{
		ClientID: "client-example", ClientSecret: "secret-example",
		APIBase: base, APIVersion: "2026-09",
		AuthorizeURL:    "https://api.easyparcel.com/oauth/login",
		TokenURL:        base + "/oauth/token",
		RedirectStaging: "https://jomform-staging.wanzul-hosting.com/api/shipping/easyparcel/callback",
		RedirectProd:    "https://jomform.com/api/shipping/easyparcel/callback",
	}
}

func TestEasyParcelAuthorizeURLCarriesTheStateAndTheRegisteredRedirect(t *testing.T) {
	cfg := testEasyParcelConfig("https://api.easyparcel.com")
	link, err := EasyParcelAuthorizeURL(cfg, "state-123",
		"https://jomform-staging.wanzul-hosting.com/api/shipping/easyparcel/callback")
	if err != nil {
		t.Fatalf("authorize url: %v", err)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.Path != "/oauth/login" {
		t.Errorf("path = %q, want /oauth/login", u.Path)
	}
	q := u.Query()
	if q.Get("client_id") != "client-example" || q.Get("state") != "state-123" {
		t.Errorf("query = %v", q)
	}
	// The redirect_uri is the PUBLIC /api/… URL (the web app proxies it), because
	// that is what is registered in the Developer Hub.
	if q.Get("redirect_uri") != cfg.RedirectStaging {
		t.Errorf("redirect_uri = %q, want the staging callback", q.Get("redirect_uri"))
	}
	if strings.Contains(link, cfg.ClientSecret) {
		t.Fatal("the secret is in the authorize URL")
	}
}

func TestEasyParcelTokenExchangeUsesBasicAuthAndFormBody(t *testing.T) {
	var gotAuth, gotBody, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token_type":"Bearer","expires_in":36000,
		  "access_token":"access-example","refresh_token":"refresh-example",
		  "refresh_token_expires_in":31557600,"refresh_token_expires_at":"2027-10-16T02:00:00.000Z"}`))
	}))
	defer srv.Close()

	cfg := testEasyParcelConfig("https://api.easyparcel.com")
	cfg.TokenURL = srv.URL
	ts, err := EasyParcelExchangeCode(context.Background(), nil, cfg, "code-123", cfg.RedirectStaging, "state-123")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Fatalf("Authorization = %q, want HTTP Basic (OAuth 2.0 requires the secret there)", gotAuth)
	}
	if gotCT != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q", gotCT)
	}
	form, _ := url.ParseQuery(gotBody)
	if form.Get("grant_type") != "authorization_code" || form.Get("code") != "code-123" ||
		form.Get("redirect_uri") != cfg.RedirectStaging || form.Get("state") != "state-123" {
		t.Errorf("form = %v", form)
	}
	if ts.AccessToken != "access-example" || ts.RefreshToken != "refresh-example" {
		t.Errorf("token set = %+v", ts)
	}
	if ts.ExpiresAt.IsZero() || ts.RefreshExpiresAt.IsZero() {
		t.Errorf("expiries were not parsed: %+v", ts)
	}
}

func TestEasyParcelTokenErrorDoesNotLeakTheVendorDescription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"code=code-123 was already used"}`))
	}))
	defer srv.Close()
	cfg := testEasyParcelConfig("https://api.easyparcel.com")
	cfg.TokenURL = srv.URL
	_, err := EasyParcelExchangeCode(context.Background(), nil, cfg, "code-123", cfg.RedirectStaging, "s")
	if err == nil {
		t.Fatal("a failed exchange must be an error")
	}
	if strings.Contains(err.Error(), "code-123") {
		t.Errorf("the error quotes our own request: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("the stable vendor code should survive (it classifies the failure): %v", err)
	}
}

// easyparcelServer answers the quotation endpoint and records the request.
type easyParcelServer struct {
	*httptest.Server
	gotAuth string
	gotPath string
	gotBody map[string]any
	calls   int32
}

func newEasyParcelServer(t *testing.T, status int, body string) *easyParcelServer {
	t.Helper()
	s := &easyParcelServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&s.calls, 1)
		s.gotAuth = r.Header.Get("Authorization")
		s.gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &s.gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

const easyParcelQuotations = `{"status_code":200,"message":"1 request success, 0 request error.",
 "data":[{"status":"success","quotations":[
  {"courier":{"service_id":"EP-CS096","service_name":"Aramex (Pick Up)","courier_name":"Aramex"},
   "pricing":{"currency":"MYR","total_amount":12.31,"shipment_price":10.2,"shipment_tax":0.61,
     "total_features_price":0.69,"seller_payable_amount":12.31}},
  {"courier":{"service_id":"EP-CS0KS","service_name":"CJ Century","courier_name":"CJ Century Logistics"},
   "pricing":{"currency":"MYR","total_amount":7.8,"shipment_price":7.8,"shipment_tax":0.0,
     "total_features_price":0.0,"seller_payable_amount":7.8}}
 ]}]}`

func TestEasyParcelQuoteSendsSubdivisionCodesAndPicksTheCheapest(t *testing.T) {
	srv := newEasyParcelServer(t, http.StatusOK, easyParcelQuotations)
	store := &easyParcelTokenStore{
		loaded: TokenSet{AccessToken: "access-example", RefreshToken: "refresh-example",
			ExpiresAt: time.Now().Add(time.Hour)},
		found: true,
	}
	cfg := testEasyParcelConfig(srv.URL)
	ep := NewEasyParcel(cfg, store, nil)

	q, err := ep.Quote(context.Background(), QuoteRequest{
		AccountID:   "acct-1",
		Origin:      Address{State: "Selangor", Postcode: "40150"},
		Destination: Address{State: "Pulau Pinang", Postcode: "11950"},
		WeightG:     1500,
		Country:     "MY",
	})
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if srv.gotAuth != "Bearer access-example" {
		t.Errorf("Authorization = %q, want the bearer grant", srv.gotAuth)
	}
	if srv.gotPath != "/open_api/2026-09/shipment/quotations" {
		t.Errorf("path = %q (the API version is part of the path)", srv.gotPath)
	}
	shipment, ok := srv.gotBody["shipment"].([]any)
	if !ok || len(shipment) != 1 {
		t.Fatalf("body shipment = %#v", srv.gotBody["shipment"])
	}
	item := shipment[0].(map[string]any)
	sender := item["sender"].(map[string]any)
	receiver := item["receiver"].(map[string]any)
	// ISO 3166-2:MY — Selangor is MY-10 and Pulau Pinang is MY-07, whatever
	// spelling the merchant used.
	if sender["subdivision_code"] != "MY-10" || receiver["subdivision_code"] != "MY-07" {
		t.Errorf("subdivision codes = %v / %v", sender["subdivision_code"], receiver["subdivision_code"])
	}
	if sender["postcode"] != "40150" || receiver["postcode"] != "11950" {
		t.Errorf("postcodes = %v / %v", sender["postcode"], receiver["postcode"])
	}
	if fmt.Sprint(item["weight"]) != "1.5" {
		t.Errorf("weight = %v, want 1.5 KG (the gram value, converted without a float)", item["weight"])
	}
	if srv.gotBody["api_rate_on"] != true {
		t.Errorf("api_rate_on = %v, want true", srv.gotBody["api_rate_on"])
	}
	// The cheapest quotation is the fee: a buyer cannot see the courier list, so
	// silently picking an expensive service would be an invisible decision.
	if q.PriceSen != 780 {
		t.Errorf("PriceSen = %d, want 780 (the cheapest)", q.PriceSen)
	}
	if q.ServiceName != "CJ Century" {
		t.Errorf("ServiceName = %q, want the chosen service recorded", q.ServiceName)
	}
	if q.Detail["shipment_price_sen"] != int64(780) {
		t.Errorf("detail = %#v", q.Detail)
	}
}

func TestEasyParcelUnparseablePricesAreSkippedNotTreatedAsFree(t *testing.T) {
	srv := newEasyParcelServer(t, http.StatusOK, `{"data":[{"quotations":[
	  {"courier":{"service_name":"A"},"pricing":{"currency":"MYR","total_amount":"N/A"}},
	  {"courier":{"service_name":"B"},"pricing":{"currency":"MYR","total_amount":9.9}}
	]}]}`)
	store := &easyParcelTokenStore{loaded: TokenSet{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)}, found: true}
	ep := NewEasyParcel(testEasyParcelConfig(srv.URL), store, nil)
	q, err := ep.Quote(context.Background(), QuoteRequest{
		AccountID: "a", Origin: Address{State: "Selangor", Postcode: "40150"},
		Destination: Address{State: "Sabah", Postcode: "88000"}, WeightG: 1000, Country: "MY",
	})
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if q.PriceSen != 990 {
		t.Errorf("PriceSen = %d, want 990 — an unparseable price must never win as a 0", q.PriceSen)
	}
}

func TestEasyParcelEmptyQuotationsIsCoverage(t *testing.T) {
	srv := newEasyParcelServer(t, http.StatusOK, `{"data":[{"quotations":[]}]}`)
	store := &easyParcelTokenStore{loaded: TokenSet{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)}, found: true}
	ep := NewEasyParcel(testEasyParcelConfig(srv.URL), store, nil)
	_, err := ep.Quote(context.Background(), QuoteRequest{
		AccountID: "a", Origin: Address{State: "Selangor", Postcode: "40150"},
		Destination: Address{State: "Sabah", Postcode: "88000"}, WeightG: 1000, Country: "MY",
	})
	if !errors.Is(err, ErrProviderNoCoverage) {
		t.Fatalf("err = %v, want ErrProviderNoCoverage (no courier can take this lane)", err)
	}
}

func TestEasyParcelMissingSubdivisionIsCoverage(t *testing.T) {
	srv := newEasyParcelServer(t, http.StatusOK, easyParcelQuotations)
	store := &easyParcelTokenStore{loaded: TokenSet{AccessToken: "t", ExpiresAt: time.Now().Add(time.Hour)}, found: true}
	ep := NewEasyParcel(testEasyParcelConfig(srv.URL), store, nil)
	_, err := ep.Quote(context.Background(), QuoteRequest{
		AccountID: "a", Origin: Address{State: "Singapore", Postcode: "018989"},
		Destination: Address{State: "Sabah", Postcode: "88000"}, WeightG: 1000, Country: "MY",
	})
	if !errors.Is(err, ErrProviderNoCoverage) {
		t.Fatalf("err = %v, want ErrProviderNoCoverage (a state with no ISO code cannot be priced)", err)
	}
	if atomic.LoadInt32(&srv.calls) != 0 {
		t.Error("the API was called without the required subdivision_code")
	}
}

func TestEasyParcelRejectedGrantIsMarkedAndUnavailable(t *testing.T) {
	srv := newEasyParcelServer(t, http.StatusUnauthorized, `{"message":"unauthorized"}`)
	store := &easyParcelTokenStore{loaded: TokenSet{AccessToken: "stale", ExpiresAt: time.Now().Add(time.Hour)}, found: true}
	ep := NewEasyParcel(testEasyParcelConfig(srv.URL), store, nil)
	_, err := ep.Quote(context.Background(), QuoteRequest{
		AccountID: "acct-1", Origin: Address{State: "Selangor", Postcode: "40150"},
		Destination: Address{State: "Sabah", Postcode: "88000"}, WeightG: 1000, Country: "MY",
	})
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("err = %v, want ErrProviderUnavailable", err)
	}
	if len(store.errors) != 1 || store.errors[0] != "rejected" {
		t.Errorf("recorded error classes = %v, want [rejected]", store.errors)
	}
}

func TestEasyParcelExpiredGrantRefreshesOnceAndSaves(t *testing.T) {
	var refreshed int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			atomic.AddInt32(&refreshed, 1)
			form, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(form), "grant_type=refresh_token") {
				t.Errorf("refresh form = %s", form)
			}
			_, _ = w.Write([]byte(`{"token_type":"Bearer","expires_in":36000,"access_token":"fresh","refresh_token":"refresh2"}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer fresh" {
			t.Errorf("quotation used %q, want the refreshed token", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(easyParcelQuotations))
	}))
	defer srv.Close()

	store := &easyParcelTokenStore{
		loaded: TokenSet{AccessToken: "stale", RefreshToken: "refresh1",
			ExpiresAt: time.Now().Add(-time.Minute)}, // expired
		found: true,
	}
	cfg := testEasyParcelConfig(srv.URL)
	ep := NewEasyParcel(cfg, store, nil)
	if _, err := ep.Quote(context.Background(), QuoteRequest{
		AccountID: "acct-1", Origin: Address{State: "Selangor", Postcode: "40150"},
		Destination: Address{State: "Sabah", Postcode: "88000"}, WeightG: 1000, Country: "MY",
	}); err != nil {
		t.Fatalf("quote: %v", err)
	}
	if atomic.LoadInt32(&refreshed) != 1 {
		t.Errorf("refreshes = %d, want exactly 1", refreshed)
	}
	if len(store.saved) != 1 || store.saved[0].AccessToken != "fresh" {
		t.Errorf("saved = %+v, want the refreshed grant persisted", store.saved)
	}
	if len(store.errors) != 0 {
		t.Errorf("a successful refresh must not record an error class: %v", store.errors)
	}
}

func TestEasyParcelWithoutAGrantIsUnavailableNotAFault(t *testing.T) {
	store := &easyParcelTokenStore{found: false}
	ep := NewEasyParcel(testEasyParcelConfig("https://api.easyparcel.com"), store, nil)
	if !ep.Available() {
		t.Fatal("Available() must be true when the app + store exist (whether THIS workspace consented is a per-account question)")
	}
	_, err := ep.Quote(context.Background(), QuoteRequest{AccountID: "acct-1"})
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("err = %v, want ErrProviderUnavailable for a workspace that never consented", err)
	}
}

func TestEasyParcelEnvironmentIsDecidedByTheAccount(t *testing.T) {
	ep := NewEasyParcel(testEasyParcelConfig("https://api.easyparcel.com"), &easyParcelTokenStore{}, nil)
	if ep.Environment() != EasyParcelEnvironment {
		t.Errorf("Environment = %q, want %q (one app serves sandbox and live; the ACCOUNT decides)",
			ep.Environment(), EasyParcelEnvironment)
	}
	// The redirect URI follows the DEPLOYMENT, not the credentials.
	cfg := testEasyParcelConfig("https://api.easyparcel.com")
	if cfg.RedirectURI("prod") != cfg.RedirectProd || cfg.RedirectURI("dev") != cfg.RedirectStaging {
		t.Error("RedirectURI must pick prod for prod and staging otherwise")
	}
}

func TestMalaysiaSubdivisionCoversEverySpelling(t *testing.T) {
	cases := map[string]string{
		"Penang": "MY-07", "Pulau Pinang": "MY-07", "pinang": "MY-07",
		"KL": "MY-14", "Kuala Lumpur": "MY-14", "WP Kuala Lumpur": "MY-14",
		"Selangor": "MY-10", "Sabah": "MY-12", "Sarawak": "MY-13", "Labuan": "MY-15",
		"Johor Bahru": "MY-01", "Melaka": "MY-04", "Malacca": "MY-04",
		"Putrajaya": "MY-16", "Terengganu": "MY-11", "Perlis": "MY-09",
		"Negeri Sembilan": "MY-05", "Kedah": "MY-02", "Kelantan": "MY-03", "Pahang": "MY-06",
		"Perak": "MY-08",
	}
	for in, want := range cases {
		if got := MalaysiaSubdivision(in); got != want {
			t.Errorf("MalaysiaSubdivision(%q) = %q, want %q", in, got, want)
		}
	}
	if got := MalaysiaSubdivision("Singapore"); got != "" {
		t.Errorf("MalaysiaSubdivision(Singapore) = %q, want empty", got)
	}
}

// ── the resolver: quote wins, zone table falls back ──────────────────────────

// stubProvider is a controllable Provider.
type stubProvider struct {
	name      string
	env       string
	available bool
	quote     ProviderQuote
	err       error
	calls     int32
	lastReq   QuoteRequest
	delay     time.Duration
}

func (s *stubProvider) Name() string        { return s.name }
func (s *stubProvider) Environment() string { return s.env }
func (s *stubProvider) Available() bool     { return s.available }
func (s *stubProvider) Quote(ctx context.Context, req QuoteRequest) (ProviderQuote, error) {
	atomic.AddInt32(&s.calls, 1)
	s.lastReq = req
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return ProviderQuote{}, ctx.Err()
		}
	}
	if s.err != nil {
		return ProviderQuote{}, s.err
	}
	return s.quote, nil
}

// zoneFixtureSet is a workspace's own zone table: Peninsular RM8 free over
// RM150, East Malaysia RM15, with the East zone carrying a POINT (which is what a
// point-based courier is quoted to).
func zoneFixtureSet(withPoints bool) ZoneSet {
	east := Zone{
		ID: "z-east", Name: "East Malaysia", States: []string{"east_malaysia"},
		Rates: []Rate{{ID: "r-east", Label: "Standard", PriceSen: 1500}},
	}
	if withPoints {
		east.Lat, east.Lng = "5.9804", "116.0735"
	}
	return ZoneSet{ID: "set-1", Name: "Malaysia", Default: true, Zones: []Zone{
		{ID: "z-west", Name: "West Malaysia", States: []string{"peninsular"},
			Lat: "3.1390", Lng: "101.6869",
			Rates: []Rate{{ID: "r-west", Label: "Standard", PriceSen: 800, FreeOverSen: 15000}}},
		east,
	}}
}

func TestResolveCourierPrefersTheQuoteAndRecordsTheEstimate(t *testing.T) {
	set := zoneFixtureSet(true)
	p := &stubProvider{
		name: ProviderLalamove, env: "sandbox", available: true,
		quote: ProviderQuote{Provider: ProviderLalamove, Environment: "sandbox", PriceSen: 820,
			Currency: "MYR", QuoteID: "q-1", ExpiresAt: time.Now().Add(time.Minute)},
	}
	reg := NewRegistry(p)
	q, err := ResolveCourier(context.Background(), reg, CourierOptions{
		Provider:    ProviderLalamove,
		Destination: Address{State: "Sabah", Postcode: "88000"},
		OriginLat:   "3.1578", OriginLng: "101.7120",
		Fallback: &set, SubtotalSen: 1000,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if q.Source != FeeSourceCourier {
		t.Errorf("Source = %q, want courier", q.Source)
	}
	if q.PriceSen != 820 {
		t.Errorf("PriceSen = %d, want the quoted 820", q.PriceSen)
	}
	// The buyer's form showed the zone rate; the quotation is what was charged.
	if q.EstimatePriceSen != 1500 {
		t.Errorf("EstimatePriceSen = %d, want the zone estimate 1500", q.EstimatePriceSen)
	}
	if q.ProviderQuoteID != "q-1" || q.ExpiresAt == nil || q.Environment != "sandbox" {
		t.Errorf("quote bookkeeping = %+v", q)
	}
	// The drop-off point came from the MATCHED zone, so the courier is quoted to
	// the place the buyer's address actually landed in.
	if p.lastReq.DestinationLat != "5.9804" || p.lastReq.DestinationLng != "116.0735" {
		t.Errorf("destination point = %q,%q, want the East Malaysia zone's point",
			p.lastReq.DestinationLat, p.lastReq.DestinationLng)
	}
	if q.Zone == nil || q.Zone.ZoneName != "East Malaysia" || q.Zone.PriceSen != 1500 {
		t.Errorf("the zone record must keep the estimate/fallback intact: %+v", q.Zone)
	}
}

func TestResolveCourierFallsBackWhenTheProviderCannotAnswer(t *testing.T) {
	set := zoneFixtureSet(true)
	cases := []struct {
		name       string
		provider   Provider
		registry   *Registry
		wantReason string
	}{
		{
			name:       "not connected / no credentials",
			provider:   &stubProvider{name: ProviderLalamove, available: false},
			wantReason: FallbackUnavailable,
		},
		{
			name:       "provider error",
			provider:   &stubProvider{name: ProviderLalamove, available: true, err: ErrProviderFault},
			wantReason: FallbackError,
		},
		{
			name:       "provider cannot cover the destination",
			provider:   &stubProvider{name: ProviderLalamove, available: true, err: ErrProviderNoCoverage},
			wantReason: FallbackError,
		},
		{
			name:       "unknown provider name",
			provider:   &stubProvider{name: ProviderLalamove, available: true},
			registry:   NewRegistry(), // nothing registered
			wantReason: FallbackUnavailable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := tc.registry
			if reg == nil {
				reg = NewRegistry(tc.provider)
			}
			q, err := ResolveCourier(context.Background(), reg, CourierOptions{
				Provider:    ProviderLalamove,
				Destination: Address{State: "Sabah", Postcode: "88000"},
				OriginLat:   "3.1578", OriginLng: "101.7120",
				Fallback: &set, SubtotalSen: 1000,
			})
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if q.Source != FeeSourceZones || q.PriceSen != 1500 {
				t.Fatalf("source = %q price = %d, want the zone fallback at 1500", q.Source, q.PriceSen)
			}
			if q.FallbackReason != tc.wantReason {
				t.Errorf("FallbackReason = %q, want %q", q.FallbackReason, tc.wantReason)
			}
			if q.Provider != ProviderLalamove {
				t.Errorf("Provider = %q, want the provider that was ASKED recorded even when it lost", q.Provider)
			}
			if q.EstimatePriceSen != 1500 {
				t.Errorf("EstimatePriceSen = %d, want 1500", q.EstimatePriceSen)
			}
		})
	}
}

func TestResolveCourierTimesOutIntoTheFallback(t *testing.T) {
	set := zoneFixtureSet(true)
	p := &stubProvider{name: ProviderLalamove, available: true, delay: 200 * time.Millisecond,
		quote: ProviderQuote{PriceSen: 820}}
	q, err := ResolveCourier(context.Background(), NewRegistry(p), CourierOptions{
		Provider:    ProviderLalamove,
		Destination: Address{State: "Sabah", Postcode: "88000"},
		OriginLat:   "3.1578", OriginLng: "101.7120",
		Fallback: &set, SubtotalSen: 1000,
		Timeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if q.Source != FeeSourceZones || q.FallbackReason != FallbackTimeout {
		t.Fatalf("source = %q reason = %q, want the zone fallback with the timeout class",
			q.Source, q.FallbackReason)
	}
}

func TestResolveCourierRefusesWithoutAFallback(t *testing.T) {
	p := &stubProvider{name: ProviderLalamove, available: false}
	_, err := ResolveCourier(context.Background(), NewRegistry(p), CourierOptions{
		Provider:    ProviderLalamove,
		Destination: Address{State: "Sabah", Postcode: "88000"},
	})
	if !errors.Is(err, ErrCourierNoFallback) {
		t.Fatalf("err = %v, want ErrCourierNoFallback — never a silent 0", err)
	}
}

func TestResolveCourierRefusesAnEmptyDestination(t *testing.T) {
	set := zoneFixtureSet(true)
	p := &stubProvider{name: ProviderLalamove, available: true, quote: ProviderQuote{PriceSen: 100}}
	_, err := ResolveCourier(context.Background(), NewRegistry(p), CourierOptions{
		Provider: ProviderLalamove, Fallback: &set,
	})
	if !errors.Is(err, ErrCourierAddress) {
		t.Fatalf("err = %v, want ErrCourierAddress", err)
	}
	if atomic.LoadInt32(&p.calls) != 0 {
		t.Error("a provider must not be asked to price \"anywhere\"")
	}
}

func TestResolveCourierFreeShippingThresholdStillApplies(t *testing.T) {
	set := zoneFixtureSet(true)
	p := &stubProvider{name: ProviderLalamove, available: false}
	q, err := ResolveCourier(context.Background(), NewRegistry(p), CourierOptions{
		Provider:    ProviderLalamove,
		Destination: Address{State: "Selangor", Postcode: "40150"},
		Fallback:    &set, SubtotalSen: 20000,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if q.PriceSen != 0 || q.Zone == nil || !q.Zone.Free {
		t.Errorf("the zone fallback must keep the free-shipping rule: %+v", q.Zone)
	}
}

// ── the double-submission guard ──────────────────────────────────────────────

func TestQuoteCacheReusesOneQuotationForADuplicateSubmission(t *testing.T) {
	set := zoneFixtureSet(true)
	p := &stubProvider{name: ProviderLalamove, available: true,
		quote: ProviderQuote{Provider: ProviderLalamove, PriceSen: 820, QuoteID: "q-1",
			ExpiresAt: time.Now().Add(2 * time.Minute)}}
	cache := NewQuoteCache()
	reg := NewRegistry(p)
	opts := CourierOptions{
		Provider: ProviderLalamove, Cache: cache,
		Destination: Address{State: "Sabah", Postcode: "88000"},
		OriginLat:   "3.1578", OriginLng: "101.7120",
		Fallback: &set, SubtotalSen: 1000,
	}
	first, err := ResolveCourier(context.Background(), reg, opts)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := ResolveCourier(context.Background(), reg, opts)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if got := atomic.LoadInt32(&p.calls); got != 1 {
		t.Fatalf("provider calls = %d, want 1 — a duplicate submission must not create a second quotation", got)
	}
	if first.PriceSen != second.PriceSen || first.ProviderQuoteID != second.ProviderQuoteID {
		t.Errorf("a reused quotation must be identical: %+v vs %+v", first, second)
	}
	// A DIFFERENT cart is a different question and must be asked.
	opts.SubtotalSen = 5000
	if _, err := ResolveCourier(context.Background(), reg, opts); err != nil {
		t.Fatalf("third: %v", err)
	}
	// (the fingerprint covers the stops, the parcel and the provider — not the
	// subtotal, which does not change a courier's price)
	if got := atomic.LoadInt32(&p.calls); got != 1 {
		t.Errorf("provider calls = %d, want 1: the subtotal is not part of a courier price", got)
	}
	opts.Destination = Address{State: "Sarawak", Postcode: "93000"}
	// A DIFFERENT destination IS a different question.
	if _, err := ResolveCourier(context.Background(), reg, opts); err != nil {
		t.Fatalf("fourth: %v", err)
	}
	if got := atomic.LoadInt32(&p.calls); got != 2 {
		t.Errorf("provider calls = %d, want 2 after a different stop", got)
	}
}

func TestQuoteCacheDoesNotHandBackADeadQuotation(t *testing.T) {
	cache := NewQuoteCache()
	now := time.Now()
	cache.now = func() time.Time { return now }
	req := QuoteRequest{AccountID: "a", Destination: Address{State: "Sabah"}}
	cache.Put(req, ProviderQuote{PriceSen: 100, QuoteID: "q", ExpiresAt: now.Add(time.Second)})
	if _, ok := cache.Get(req); !ok {
		t.Fatal("a live quotation must be reused")
	}
	now = now.Add(2 * time.Second)
	if _, ok := cache.Get(req); ok {
		t.Fatal("an expired quotation must not be handed to the next submission")
	}
	if cache.Len() != 0 {
		t.Errorf("an expired entry should be dropped, len = %d", cache.Len())
	}
}

// ── credentials ──────────────────────────────────────────────────────────────

func TestCredentialsAbsenceIsAStateNotAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.env")
	creds := LoadCourierCredentials(missing, missing)
	if creds.Lalamove.Configured() || creds.EasyParcel.Configured() {
		t.Fatal("a missing file must not produce credentials")
	}
	if creds.LalamoveSource != "none" || creds.EasyParcelSource != "none" {
		t.Errorf("sources = %q / %q, want none", creds.LalamoveSource, creds.EasyParcelSource)
	}
	if creds.LalamovePath != missing {
		t.Errorf("the path is configuration and is kept for the operator: %q", creds.LalamovePath)
	}
	// And the adapters built from it are simply unavailable — no panic, no error.
	reg := NewRegistry(NewLalamove(creds.Lalamove, nil), NewEasyParcel(creds.EasyParcel, nil, nil))
	for _, st := range ProviderStatuses(reg) {
		if st.CredentialsPresent {
			t.Errorf("%s reports credentials it does not have", st.Name)
		}
	}
}

func TestCredentialsAreReadFromTheFilesAndNeverSerialized(t *testing.T) {
	dir := t.TempDir()
	lala := filepath.Join(dir, "lalamove.env")
	if err := os.WriteFile(lala, []byte(
		"# a comment\nLALAMOVE_SANDBOX_API_KEY=pk_test_from_file\n"+
			"LALAMOVE_SANDBOX_API_SECRET=secret_from_file\n"+
			"LALAMOVE_SANDBOX_BASE=https://rest.sandbox.lalamove.com\n"+
			"LALAMOVE_MARKET=MY\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	ep := filepath.Join(dir, "easyparcel.env")
	if err := os.WriteFile(ep, []byte(
		"EASYPARCEL_CLIENT_ID=client_from_file\nEASYPARCEL_CLIENT_SECRET=secret_from_file\n"+
			"EASYPARCEL_REDIRECT_STAGING=\"https://staging.example/api/shipping/easyparcel/callback\"\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	creds := LoadCourierCredentials(lala, ep)
	if creds.Lalamove.Environment() != "sandbox" || creds.Lalamove.Market != "MY" {
		t.Errorf("lalamove config = %+v (env %q)", creds.Lalamove, creds.Lalamove.Environment())
	}
	if creds.LalamoveSource != "file" || creds.EasyParcelSource != "file" {
		t.Errorf("sources = %q / %q, want file", creds.LalamoveSource, creds.EasyParcelSource)
	}
	// The quotes around the redirect URI are stripped (an env file written by
	// hand carries them) and the value is not otherwise echoed anywhere.
	if creds.EasyParcel.RedirectURI("dev") != "https://staging.example/api/shipping/easyparcel/callback" {
		t.Errorf("redirect = %q", creds.EasyParcel.RedirectURI("dev"))
	}
	// THE CREDENTIALS ARE NOT SERIALIZABLE: no JSON tag, no String() that prints
	// them, and the status structs carry only presence + source.
	blob, err := json.Marshal(ProviderStatuses(NewRegistry(NewLalamove(creds.Lalamove, nil),
		NewEasyParcel(creds.EasyParcel, nil, nil))))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, secret := range []string{"pk_test_from_file", "secret_from_file", "client_from_file"} {
		if strings.Contains(string(blob), secret) {
			t.Fatalf("a credential value reached the JSON: %s", blob)
		}
	}
}

func TestCourierStatesReportWhatIsMissing(t *testing.T) {
	reg := NewRegistry(NewLalamove(LalamoveConfig{Key: "pk_test_x", Secret: "s", Market: "MY"}, nil))
	withCreds := CourierCredentials{Lalamove: LalamoveConfig{Key: "pk_test_x", Secret: "s"}, LalamoveSource: "file"}

	// Nothing configured.
	states := CourierStates(reg, nil, withCreds)
	lala := findState(t, states, ProviderLalamove)
	if lala.Ready || strings.Join(lala.Missing, ",") != "connect" {
		t.Errorf("unconnected lalamove = %+v, want missing just `connect`", lala)
	}

	// Connected, but with no pickup point: a point-based courier cannot quote.
	states = CourierStates(reg, []Connection{{
		Provider: ProviderLalamove, Status: StatusConnected, Environment: EnvSandbox,
	}}, withCreds)
	lala = findState(t, states, ProviderLalamove)
	if lala.Ready || strings.Join(lala.Missing, ",") != "origin" {
		t.Errorf("pointless lalamove = %+v, want missing just `origin`", lala)
	}

	// Complete.
	states = CourierStates(reg, []Connection{{
		Provider: ProviderLalamove, Status: StatusConnected, Environment: EnvSandbox,
		OriginLat: "3.1", OriginLng: "101.7",
	}}, withCreds)
	lala = findState(t, states, ProviderLalamove)
	if !lala.Ready {
		t.Errorf("connected lalamove with a point = %+v, want ready", lala)
	}

	// No credentials at all: the provider is listed, and says why it is not ready.
	states = CourierStates(NewRegistry(NewLalamove(LalamoveConfig{}, nil)), nil, CourierCredentials{})
	lala = findState(t, states, ProviderLalamove)
	if lala.Ready || strings.Join(lala.Missing, ",") != "credentials,connect" {
		t.Errorf("credential-less lalamove = %+v, want credentials+connect", lala)
	}
}

func findState(t *testing.T, states []ProviderState, name string) ProviderState {
	t.Helper()
	for _, st := range states {
		if st.Info.Name == name {
			return st
		}
	}
	t.Fatalf("provider %s not reported", name)
	return ProviderState{}
}

func TestConnectRefusesWhatItCannotDo(t *testing.T) {
	store := newMemCourierStore()
	reg := NewRegistry(NewLalamove(LalamoveConfig{Key: "pk_test_x", Secret: "s", Market: "MY"}, nil))

	// No credentials on the deployment.
	c := &Connector{Store: store, Registry: reg, Creds: CourierCredentials{}}
	if _, err := c.Connect(context.Background(), "acct", ProviderLalamove,
		ConnectRequest{OriginLat: "3.1", OriginLng: "101.7"}); !errors.Is(err, ErrConnectNoCredentials) {
		t.Errorf("err = %v, want ErrConnectNoCredentials", err)
	}

	// Credentials present, but no pickup point.
	c.Creds = CourierCredentials{Lalamove: LalamoveConfig{Key: "pk_test_x", Secret: "s"}}
	if _, err := c.Connect(context.Background(), "acct", ProviderLalamove,
		ConnectRequest{}); !errors.Is(err, ErrConnectOriginRequired) {
		t.Errorf("err = %v, want ErrConnectOriginRequired", err)
	}

	// A good connect.
	out, err := c.Connect(context.Background(), "acct", ProviderLalamove,
		ConnectRequest{OriginLat: "3.1578", OriginLng: "101.7120", ServiceType: "car"})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if !out.Connected || out.AuthorizeURL != "" {
		t.Errorf("outcome = %+v, want connected with no browser step", out)
	}
	conn, ok := store.connections["acct|"+ProviderLalamove]
	if !ok {
		t.Fatal("the connection was not saved")
	}
	// The ENVIRONMENT comes from the key prefix, not from the merchant.
	if conn.Environment != EnvSandbox || conn.ServiceType != "CAR" {
		t.Errorf("saved connection = %+v", conn)
	}

	// An unknown provider name is refused before anything is written.
	if _, err := c.Connect(context.Background(), "acct", "unicorn", ConnectRequest{}); !errors.Is(err, ErrProviderUnknown) {
		t.Errorf("err = %v, want ErrProviderUnknown", err)
	}
}

func TestConnectEasyParcelIssuesALinkAndNeverAsksForAPassword(t *testing.T) {
	store := newMemCourierStore()
	reg := NewRegistry(NewEasyParcel(testEasyParcelConfig("https://api.easyparcel.com"), store, nil))
	c := &Connector{
		Store: store, Registry: reg, Env: "dev",
		Creds: CourierCredentials{EasyParcel: testEasyParcelConfig("https://api.easyparcel.com")},
	}
	out, err := c.Connect(context.Background(), "acct", ProviderEasyParcel, ConnectRequest{
		OriginPostcode: "40150", OriginState: "Selangor",
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if out.Connected {
		t.Error("an OAuth provider is not quotable until the MERCHANT consents")
	}
	if out.AuthorizeURL == "" || !strings.Contains(out.AuthorizeURL, "state=") {
		t.Fatalf("authorize url = %q", out.AuthorizeURL)
	}
	if out.RedirectURI != store.lastState.redirectURI {
		t.Errorf("the link's redirect_uri must be the one the state was issued for (%q vs %q)",
			out.RedirectURI, store.lastState.redirectURI)
	}
	// The consent is consumed exactly once.
	if _, ok, _ := store.ConsumeState(context.Background(), store.lastState.state, time.Minute); !ok {
		t.Fatal("the issued state must be consumable")
	}
	if _, ok, _ := store.ConsumeState(context.Background(), store.lastState.state, time.Minute); ok {
		t.Fatal("a state must not be replayable")
	}
}

// memCourierStore is an in-memory shipping.CourierStore for the tests that do not
// need Postgres (the HTTP-level tests drive the real one).
type memCourierStore struct {
	connections map[string]Connection
	grants      map[string]TokenSet
	errors      map[string]string
	states      map[string]StateRef
	lastState   struct{ state, redirectURI string }
}

func newMemCourierStore() *memCourierStore {
	return &memCourierStore{
		connections: map[string]Connection{}, grants: map[string]TokenSet{},
		errors: map[string]string{}, states: map[string]StateRef{},
	}
}

func (s *memCourierStore) key(accountID, provider string) string { return accountID + "|" + provider }

func (s *memCourierStore) Connection(_ context.Context, accountID, provider string) (Connection, bool, error) {
	c, ok := s.connections[s.key(accountID, provider)]
	return c, ok, nil
}

func (s *memCourierStore) Connections(context.Context, string) ([]Connection, error) {
	out := make([]Connection, 0, len(s.connections))
	for _, c := range s.connections {
		out = append(out, c)
	}
	return out, nil
}

func (s *memCourierStore) SaveConnection(_ context.Context, accountID string, c Connection) error {
	s.connections[s.key(accountID, c.Provider)] = c
	return nil
}

func (s *memCourierStore) Disconnect(_ context.Context, accountID, provider string) error {
	k := s.key(accountID, provider)
	c := s.connections[k]
	c.Status, c.HasGrant = StatusDisconnected, false
	s.connections[k] = c
	delete(s.grants, k)
	return nil
}

func (s *memCourierStore) IssueState(_ context.Context, accountID, provider, redirectURI, createdBy string) (string, error) {
	state := fmt.Sprintf("state-%d", len(s.states)+1)
	s.states[state] = StateRef{AccountID: accountID, Provider: provider, RedirectURI: redirectURI, CreatedBy: createdBy}
	s.lastState.state, s.lastState.redirectURI = state, redirectURI
	return state, nil
}

func (s *memCourierStore) ConsumeState(_ context.Context, state string, ttl time.Duration) (StateRef, bool, error) {
	ref, ok := s.states[state]
	if !ok {
		return StateRef{}, false, nil
	}
	delete(s.states, state)
	return ref, true, nil
}

func (s *memCourierStore) Load(_ context.Context, accountID, provider string) (TokenSet, bool, error) {
	t, ok := s.grants[s.key(accountID, provider)]
	return t, ok, nil
}

func (s *memCourierStore) Save(_ context.Context, accountID, provider string, t TokenSet) error {
	s.grants[s.key(accountID, provider)] = t
	c := s.connections[s.key(accountID, provider)]
	c.Status, c.HasGrant = StatusConnected, true
	if c.Environment == "" {
		c.Environment = EnvSandbox
	}
	s.connections[s.key(accountID, provider)] = c
	return nil
}

func (s *memCourierStore) MarkError(_ context.Context, accountID, provider, class string) error {
	s.errors[s.key(accountID, provider)] = class
	return nil
}
