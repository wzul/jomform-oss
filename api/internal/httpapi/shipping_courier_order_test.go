package httpapi

// The phase-2 courier source, end to end against a real Postgres: a form's
// shipping block with `source = "courier"` is priced by a real (stub) provider at
// submit time, the quoted fee lands in sales.shipping_sen + sales.total_sen, and
// the sale RECORDS which source produced it.
//
// WHAT THESE PROVE, and why each one exists:
//
//   - TestCourierShippingChargesTheQuotedFee: the card's headline for the courier
//     side — the fee is the QUOTATION, not the zone rate, and the sale says so
//     (source=courier, the provider's quotation id, its environment, and the zone
//     estimate the buyer's form showed).
//   - TestCourierShippingFallsBackToTheZoneRate: the card's fallback rule — a
//     provider that fails / is not connected / has no credentials still leaves the
//     order chargeable at the workspace's own rate, with the reason class recorded
//     and the sale completed.
//   - TestCourierShippingWithoutAFallbackIsRefused: with no courier AND no zone
//     set the order is a 400, never a silent 0.
//   - TestCourierShippingReusesOneQuotationForADuplicateSubmission: neither
//     provider has an idempotency key, so a double-submitted checkout must not
//     create two quotations (the sale records the SAME quotation id).
//   - TestCourierShippingRendersTheEstimateAndSaysItIsQuotedAtCheckout: the buyer's
//     page shows the fallback zone rates AS AN ESTIMATE and says the final fee is
//     quoted at checkout — the fee is never a surprise.
//
// The providers are STUBS: these tests never touch a vendor API. The adapters'
// own contracts (HMAC signing, the wrapped body, OAuth, subdivision codes) are
// pinned by internal/shipping/courier_test.go, and the sandbox quotation this
// design was measured against is recorded there.
//
// Skipped unless JOMFORM_TEST_DATABASE_URL is set (the configuration is real rows).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/shipping"
)

// stubCourierProvider is a controllable shipping.Provider.
type stubCourierProvider struct {
	name      string
	env       string
	available bool
	priceSen  int64
	quoteID   string
	err       error
	calls     int32
}

func (s *stubCourierProvider) Name() string        { return s.name }
func (s *stubCourierProvider) Environment() string { return s.env }
func (s *stubCourierProvider) Available() bool     { return s.available }
func (s *stubCourierProvider) Quote(_ context.Context, req shipping.QuoteRequest) (shipping.ProviderQuote, error) {
	atomic.AddInt32(&s.calls, 1)
	if s.err != nil {
		return shipping.ProviderQuote{}, s.err
	}
	// A point-based courier cannot price a route without both points; emulating
	// that here is what makes the "the zone has no point" fallback arm real
	// rather than a claim (the adapter's own rule is pinned in
	// internal/shipping/courier_test.go).
	if req.OriginLat == "" || req.OriginLng == "" || req.DestinationLat == "" || req.DestinationLng == "" {
		return shipping.ProviderQuote{}, shipping.ErrProviderNoCoverage
	}
	return shipping.ProviderQuote{
		Provider: s.name, Environment: s.env,
		PriceSen: s.priceSen, Currency: "MYR",
		QuoteID: s.quoteID, ServiceName: "MOTORCYCLE", CourierName: "Lalamove",
		Detail: map[string]any{"total_sen": s.priceSen},
	}, nil
}

// courierTestCreds is a credential SET that is "present" without holding a real
// secret: the tests never call a vendor, and the values here are placeholders.
func courierTestCreds() shipping.CourierCredentials {
	return shipping.CourierCredentials{
		Lalamove:       shipping.LalamoveConfig{Key: "pk_test_example", Secret: "secret-example", Market: "MY"},
		LalamoveSource: "file",
		EasyParcel: shipping.EasyParcelConfig{
			ClientID:        "client-example",
			ClientSecret:    "secret-example",
			RedirectStaging: "https://staging.example/api/shipping/easyparcel/callback",
			RedirectProd:    "https://jomform.com/api/shipping/easyparcel/callback",
		},
		EasyParcelSource: "file",
	}
}

// courierBundle wires the courier layer for a test: the real (sealing) store, the
// given provider, and a fresh quotation cache.
func courierBundle(pool *pgxpool.Pool, providers ...shipping.Provider) *shipping.Couriers {
	return &shipping.Couriers{
		Registry: shipping.NewRegistry(providers...),
		Store:    dbstore.NewCourierStore(pool, testSecretsKey()),
		Creds:    courierTestCreds(),
		Env:      "dev",
		Quotes:   shipping.NewQuoteCache(),
	}
}

// testSecretsKey is the deterministic 32-byte AES key the bundle fixtures use.
func testSecretsKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

// courierFixtureOpts shapes the fixture per test.
type courierFixtureOpts struct {
	// provider is the stub the registry will hold.
	provider shipping.Provider
	// connected saves a Lalamove connection (with a pickup point) for the
	// workspace; false exercises the "not connected" fallback.
	connected bool
	// withZoneSet gives the block a fallback zone set; false exercises the
	// "nothing to fall back to" refusal.
	withZoneSet bool
	// zonePoints sets a point on each zone (what a point-based courier is quoted
	// to). false exercises the "no point → not quotable" coverage path.
	zonePoints bool
	// onNoMatch is the block's on_no_match policy.
	onNoMatch string
	// creds overrides the credential set (a nil pointer keeps the test defaults).
	creds *shipping.CourierCredentials
}

// courierShippingFixture seeds a user + workspace + account + product, a managed
// zone set (Peninsular RM8 free over RM150, East Malaysia RM15, both with points
// so a point-based courier can be quoted), and a form whose shipping block names
// the COURIER source. Returns the handlers, the workspace host, the form slug, the
// set id, the account id and the product id.
func courierShippingFixture(t *testing.T, o courierFixtureOpts) (
	h *PublicHandlers, host, formSlug, setID, accountID, prodID string,
) {
	t.Helper()
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping courier order test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, workspaceID, workspaceSlug string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-cour-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-cour-ws-' || md5(random()::text), 'it-cour-' || md5(random()::text))
		 RETURNING id, slug`, userID).Scan(&workspaceID, &workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'it-cour-acc-' || md5(random()::text), 'it-cour-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	prod := &dbstore.Product{AccountID: accountID, Name: "Widget", PriceSen: 1000,
		Type: dbstore.SaleTypeOneTime, Active: true}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	prodID = prod.ID

	west := shipping.Zone{Name: "West Malaysia", States: []string{"peninsular"},
		Rates: []shipping.Rate{{Label: "Standard", PriceSen: 800, FreeOverSen: 15000}}}
	east := shipping.Zone{Name: "East Malaysia", States: []string{"east_malaysia"},
		Rates: []shipping.Rate{{Label: "Standard", PriceSen: 1500}}}
	if o.zonePoints {
		// Putrajaya-ish for the Peninsular zone and Kota Kinabalu for the East
		// zone: the points a Lalamove quotation routes between.
		west.Lat, west.Lng = "2.9264", "101.6964"
		east.Lat, east.Lng = "5.9804", "116.0735"
	}
	shipStore := dbstore.NewShippingStore(pool)
	if err := shipStore.Replace(ctx, accountID, []shipping.ZoneSet{{
		Name: "Malaysia", Default: true, Zones: []shipping.Zone{west, east},
	}}); err != nil {
		t.Fatalf("seed shipping config: %v", err)
	}
	all, err := shipStore.Sets(ctx, []string{accountID})
	if err != nil || len(all) != 1 {
		t.Fatalf("read back shipping config: %v (sets=%d)", err, len(all))
	}
	setID = all[0].ID

	creds := courierTestCreds()
	if o.creds != nil {
		creds = *o.creds
	}
	store := dbstore.NewCourierStore(pool, testSecretsKey())
	bundle := &shipping.Couriers{
		Registry: shipping.NewRegistry(o.provider),
		Store:    store,
		Creds:    creds,
		Env:      "dev",
		Quotes:   shipping.NewQuoteCache(),
	}
	if o.connected {
		if err := store.SaveConnection(ctx, accountID, shipping.Connection{
			Provider: shipping.ProviderLalamove, Status: shipping.StatusConnected,
			Environment: shipping.EnvSandbox, AccountLabel: "MY",
			Origin:        shipping.Address{State: "Selangor", Postcode: "40150"},
			OriginCountry: "MY", OriginLat: "3.1578", OriginLng: "101.7120",
			ServiceType: "MOTORCYCLE",
		}); err != nil {
			t.Fatalf("save courier connection: %v", err)
		}
	}

	h = &PublicHandlers{
		Forms:             dbstore.NewFormStore(pool),
		Sales:             dbstore.NewSaleStore(pool),
		Accounts:          dbstore.NewAccountStore(pool),
		Products:          dbstore.NewProductStore(pool),
		ProductVariations: dbstore.NewProductVariationStore(pool),
		Workspaces:        dbstore.NewWorkspaceStore(pool),
		Shipping:          shipStore,
		Couriers:          bundle,
		BaseURL:           "http://localhost:8080",
	}

	block := map[string]any{
		"id": "ship1", "type": "shipping", "value": "Postage", "product_id": "ship1",
		"source": "courier", "courier_provider": shipping.ProviderLalamove,
		"zone_state_field": "b_state", "on_no_match": o.onNoMatch,
	}
	if o.withZoneSet {
		block["zone_set_id"] = setID
	}
	formSlug = "it-cour-form-" + accountID[:8]
	if err := h.Forms.Create(ctx, &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "Courier shipping", Status: "published",
		Config: map[string]any{
			"blocks": []any{
				map[string]any{"id": "b_state", "type": "text", "value": "State"},
				block,
			},
			"products": []any{map[string]any{"id": prodID, "name": "Widget", "price_sen": 1000}},
		},
	}); err != nil {
		t.Fatalf("create form: %v", err)
	}
	return h, workspaceSlug + ".jomform.com", formSlug, setID, accountID, prodID
}

// courierDetail decodes a sale's shipping_detail into a shape the tests assert on.
type courierDetail struct {
	Source    string `json:"source"`
	Fallbacks int    `json:"fallbacks"`
	Blocks    []struct {
		BlockID          string         `json:"block_id"`
		Source           string         `json:"source"`
		Provider         string         `json:"provider"`
		Environment      string         `json:"environment"`
		ProviderQuoteID  string         `json:"provider_quote_id"`
		ServiceName      string         `json:"service_name"`
		PriceSen         int64          `json:"price_sen"`
		EstimatePriceSen int64          `json:"estimate_price_sen"`
		FallbackReason   string         `json:"fallback_reason"`
		ZoneName         string         `json:"zone_name"`
		MatchedBy        string         `json:"matched_by"`
		Free             bool           `json:"free"`
		ProviderDetail   map[string]any `json:"provider_detail"`
	} `json:"blocks"`
}

func decodeCourierDetail(t *testing.T, sale *dbstore.Sale) courierDetail {
	t.Helper()
	var out courierDetail
	if err := json.Unmarshal(sale.ShippingDetail, &out); err != nil {
		t.Fatalf("decode shipping_detail %s: %v", sale.ShippingDetail, err)
	}
	if len(out.Blocks) == 0 {
		t.Fatalf("shipping_detail carries no blocks: %s", sale.ShippingDetail)
	}
	return out
}

// The headline: the fee is the QUOTATION, and the sale records where it came from.
func TestCourierShippingChargesTheQuotedFee(t *testing.T) {
	provider := &stubCourierProvider{
		name: shipping.ProviderLalamove, env: shipping.EnvSandbox, available: true,
		priceSen: 820, quoteID: "quotation-820",
	}
	h, host, slug, _, _, prodID := courierShippingFixture(t, courierFixtureOpts{
		provider: provider, connected: true, withZoneSet: true, zonePoints: true,
		onNoMatch: shipping.OnNoMatchReject,
	})

	sale := saleFor(t, h, host, slug, zoneOrderBody(prodID, "Sabah", "", 1))
	if sale.ShippingSen != 820 {
		t.Fatalf("shipping_sen = %d, want the quoted 820", sale.ShippingSen)
	}
	if sale.SubtotalSen != 1000 {
		t.Errorf("subtotal_sen = %d, want 1000 (the shipping item is not a line item)", sale.SubtotalSen)
	}
	if sale.TotalSen != 1820 {
		t.Errorf("total_sen = %d, want subtotal+shipping = 1820", sale.TotalSen)
	}
	detail := decodeCourierDetail(t, sale)
	if detail.Source != shipping.FeeSourceCourier {
		t.Errorf("detail source = %q, want courier", detail.Source)
	}
	b := detail.Blocks[0]
	if b.Source != shipping.FeeSourceCourier || b.PriceSen != 820 {
		t.Errorf("block = %+v, want source=courier price_sen=820", b)
	}
	if b.ProviderQuoteID != "quotation-820" {
		t.Errorf("the provider's quotation id must be recorded (a disputed fee is only explainable with it): %+v", b)
	}
	if b.Environment != shipping.EnvSandbox {
		t.Errorf("environment = %q, want sandbox recorded so a SIMULATED fee is never mistaken for a real one", b.Environment)
	}
	if b.EstimatePriceSen != 1500 {
		t.Errorf("estimate_price_sen = %d, want the zone rate the form showed (1500)", b.EstimatePriceSen)
	}
	if b.ZoneName != "East Malaysia" || b.MatchedBy != "state" {
		t.Errorf("the zone the estimate came from must stay recorded: %+v", b)
	}
	if b.FallbackReason != "" {
		t.Errorf("fallback_reason = %q on a successful quote", b.FallbackReason)
	}
	if got := atomic.LoadInt32(&provider.calls); got != 1 {
		t.Errorf("provider calls = %d, want 1", got)
	}
}

// The provider is asked to price the zone the buyer's ADDRESS matched, so the
// courier is never quoted to a place nobody named.
func TestCourierShippingQuotesToTheMatchedZonesPoint(t *testing.T) {
	var gotLat, gotLng string
	provider := &stubCourierProvider{name: shipping.ProviderLalamove, env: shipping.EnvSandbox,
		available: true, priceSen: 900, quoteID: "q-1"}
	recording := &recordingProvider{inner: provider, lat: &gotLat, lng: &gotLng}
	h, host, slug, _, _, prodID := courierShippingFixture(t, courierFixtureOpts{
		provider: recording, connected: true, withZoneSet: true, zonePoints: true,
		onNoMatch: shipping.OnNoMatchReject,
	})
	saleFor(t, h, host, slug, zoneOrderBody(prodID, "Sabah", "", 1))
	// Kota Kinabalu is the East Malaysia zone's own point.
	if gotLat != "5.9804" || gotLng != "116.0735" {
		t.Errorf("quoted to %q,%q, want the matched zone's point", gotLat, gotLng)
	}
}

// A courier that fails, is not connected, or is not even configured must still
// leave the order chargeable — at the workspace's own zone rate, with the reason
// recorded.
func TestCourierShippingFallsBackToTheZoneRate(t *testing.T) {
	cases := []struct {
		name       string
		opts       courierFixtureOpts
		wantReason string
		wantCalls  int32
	}{
		{
			name: "provider answered with an error",
			opts: courierFixtureOpts{
				provider: &stubCourierProvider{name: shipping.ProviderLalamove, available: true,
					err: errors.New("vendor down")},
				connected: true, withZoneSet: true, zonePoints: true,
			},
			wantReason: shipping.FallbackError, wantCalls: 1,
		},
		{
			name: "provider cannot cover the destination",
			opts: courierFixtureOpts{
				provider: &stubCourierProvider{name: shipping.ProviderLalamove, available: true,
					err: shipping.ErrProviderNoCoverage},
				connected: true, withZoneSet: true, zonePoints: true,
			},
			wantReason: shipping.FallbackError, wantCalls: 1,
		},
		{
			name: "the workspace never connected the courier",
			opts: courierFixtureOpts{
				provider: &stubCourierProvider{name: shipping.ProviderLalamove, available: true,
					priceSen: 820},
				connected: false, withZoneSet: true, zonePoints: true,
			},
			wantReason: shipping.FallbackUnavailable, wantCalls: 0,
		},
		{
			name: "the zone has no point to quote to",
			opts: courierFixtureOpts{
				provider: &stubCourierProvider{name: shipping.ProviderLalamove, available: true,
					priceSen: 820},
				connected: true, withZoneSet: true, zonePoints: false,
			},
			wantReason: shipping.FallbackError, wantCalls: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, host, slug, _, _, prodID := courierShippingFixture(t, courierFixtureOpts{
				provider: tc.opts.provider, connected: tc.opts.connected,
				withZoneSet: tc.opts.withZoneSet, zonePoints: tc.opts.zonePoints,
				onNoMatch: shipping.OnNoMatchReject,
			})
			// The order COMPLETES: the zone table charges for Sabah.
			sale := saleFor(t, h, host, slug, zoneOrderBody(prodID, "Sabah", "", 1))
			if sale.ShippingSen != 1500 {
				t.Fatalf("shipping_sen = %d, want the zone fallback 1500", sale.ShippingSen)
			}
			if sale.TotalSen != 2500 {
				t.Errorf("total_sen = %d, want 2500", sale.TotalSen)
			}
			detail := decodeCourierDetail(t, sale)
			if detail.Source != shipping.FeeSourceZones {
				t.Errorf("detail source = %q, want zones (the sale must say the fee is NOT a live rate)", detail.Source)
			}
			if detail.Fallbacks != 1 {
				t.Errorf("fallbacks = %d, want 1 (a fallback must be visible at a glance)", detail.Fallbacks)
			}
			b := detail.Blocks[0]
			if b.FallbackReason != tc.wantReason {
				t.Errorf("fallback_reason = %q, want %q", b.FallbackReason, tc.wantReason)
			}
			if b.Provider != shipping.ProviderLalamove {
				t.Errorf("the provider that was asked must be recorded: %+v", b)
			}
			if b.PriceSen != 1500 || b.ZoneName != "East Malaysia" {
				t.Errorf("the zone that charged must be recorded: %+v", b)
			}
			// The zone's own point is where the courier WOULD have been quoted.
			if got := atomic.LoadInt32(providerCalls(tc.opts.provider)); got != tc.wantCalls {
				t.Errorf("provider calls = %d, want %d", got, tc.wantCalls)
			}
		})
	}
}

// providerCalls reads the call counter off a stub, regardless of its concrete type.
func providerCalls(p shipping.Provider) *int32 {
	switch v := p.(type) {
	case *stubCourierProvider:
		return &v.calls
	default:
		var zero int32
		return &zero
	}
}

// With nothing to fall back to, a failed courier is a REFUSAL — never 0.
func TestCourierShippingWithoutAFallbackIsRefused(t *testing.T) {
	h, host, slug, _, _, prodID := courierShippingFixture(t, courierFixtureOpts{
		provider:  &stubCourierProvider{name: shipping.ProviderLalamove, available: true, priceSen: 820},
		connected: false, withZoneSet: false, zonePoints: false,
		onNoMatch: shipping.OnNoMatchReject,
	})
	rec := zoneOrderPost(t, h, host, slug, zoneOrderBody(prodID, "Sabah", "", 1))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("order = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), shipping.ErrCourierNoFallback.Error()) {
		t.Errorf("refusal = %s, want it to name the missing fallback", rec.Body.String())
	}
}

// Neither provider has an idempotency key, so a double-submitted checkout must
// reuse ONE quotation (and the sale records which).
func TestCourierShippingReusesOneQuotationForADuplicateSubmission(t *testing.T) {
	provider := &stubCourierProvider{name: shipping.ProviderLalamove, env: shipping.EnvSandbox,
		available: true, priceSen: 820, quoteID: "quotation-shared"}
	h, host, slug, _, _, prodID := courierShippingFixture(t, courierFixtureOpts{
		provider: provider, connected: true, withZoneSet: true, zonePoints: true,
		onNoMatch: shipping.OnNoMatchReject,
	})
	body := zoneOrderBody(prodID, "Sabah", "", 1)
	first := saleFor(t, h, host, slug, body)
	second := saleFor(t, h, host, slug, body)

	if got := atomic.LoadInt32(&provider.calls); got != 1 {
		t.Fatalf("provider calls = %d, want 1 — two submissions of the same checkout must not create two quotations", got)
	}
	one, two := decodeCourierDetail(t, first), decodeCourierDetail(t, second)
	if one.Blocks[0].ProviderQuoteID != two.Blocks[0].ProviderQuoteID {
		t.Errorf("quotation ids differ (%q vs %q); the double-submit guard did not hold",
			one.Blocks[0].ProviderQuoteID, two.Blocks[0].ProviderQuoteID)
	}
	if first.ShippingSen != second.ShippingSen || first.ShippingSen != 820 {
		t.Errorf("fees = %d / %d, want the same 820", first.ShippingSen, second.ShippingSen)
	}
}

// The buyer's page shows the fallback zone rates as an ESTIMATE and says the final
// fee is quoted at checkout.
func TestCourierShippingRendersTheEstimateAndSaysItIsQuotedAtCheckout(t *testing.T) {
	h, host, slug, _, _, _ := courierShippingFixture(t, courierFixtureOpts{
		provider:  &stubCourierProvider{name: shipping.ProviderLalamove, available: true, priceSen: 820},
		connected: true, withZoneSet: true, zonePoints: true,
		onNoMatch: shipping.OnNoMatchReject,
	})
	code, body := getForm(t, h, host, slug)
	if code != http.StatusOK {
		t.Fatalf("render = %d, want 200", code)
	}
	for _, want := range []string{
		// The estimate itself: the fallback zone options and their prices.
		`<select class="ship" data-id="ship1" data-courier="lalamove" required>`,
		"West Malaysia", "East Malaysia", "RM8.00", "RM15.00",
		// And the sentence that makes the figure honest.
		"Estimate", "quoted at checkout",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("courier-sourced shipping block missing %q", want)
		}
	}
}

// A courier block whose provider is NOT configured still renders (the estimate is
// the merchant's own zone rates), so a merchant can set the block up before the
// courier is connected.
func TestCourierShippingRendersWithNoCredentials(t *testing.T) {
	creds := courierTestCreds()
	creds.Lalamove = shipping.LalamoveConfig{} // no key pair at all
	creds.LalamoveSource = "none"
	h, host, slug, _, _, _ := courierShippingFixture(t, courierFixtureOpts{
		provider:  shipping.NewLalamove(shipping.LalamoveConfig{}, nil),
		connected: true, withZoneSet: true, zonePoints: true,
		onNoMatch: shipping.OnNoMatchReject, creds: &creds,
	})
	code, body := getForm(t, h, host, slug)
	if code != http.StatusOK {
		t.Fatalf("render = %d, want 200", code)
	}
	if !strings.Contains(body, "West Malaysia") {
		t.Errorf("the estimate must still render without credentials")
	}
}

// ── the connect/disconnect surface (REST) ────────────────────────────────────

// The dashboard's courier surface: the read is account-scoped, connect reports
// what is still missing, and Lalamove (server to server) needs a pickup point.
func TestCourierConnectSurface(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping courier REST test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	accA, _ := bundleFixture(t, ctx, pool)
	accB, _ := bundleFixture(t, ctx, pool)
	keyA := bundleAPIKey(t, ctx, pool, accA)
	keyB := bundleAPIKey(t, ctx, pool, accB)

	provider := &stubCourierProvider{name: shipping.ProviderLalamove, env: shipping.EnvSandbox,
		available: true, priceSen: 820, quoteID: "q"}
	easy := &stubCourierProvider{name: shipping.ProviderEasyParcel,
		env: shipping.EasyParcelEnvironment, available: true, priceSen: 780, quoteID: "ep-q"}
	h := newBundleHandlers(pool)
	h.Couriers = courierBundle(pool, provider, easy)

	code, raw := tplReq(t, h, keyA, http.MethodGet, "/v1/rest/shipping/couriers", "")
	if code != http.StatusOK {
		t.Fatalf("GET couriers = %d (%s)", code, raw)
	}
	var list struct {
		AccountID string `json:"account_id"`
		Providers []struct {
			Provider           string   `json:"provider"`
			CredentialsPresent bool     `json:"credentials_present"`
			CredentialsSource  string   `json:"credentials_source"`
			NeedsBrowser       bool     `json:"needs_browser"`
			PointBased         bool     `json:"point_based"`
			Connected          bool     `json:"connected"`
			Ready              bool     `json:"ready"`
			Missing            []string `json:"missing"`
		} `json:"providers"`
		CallbackPath string `json:"callback_path"`
		RedirectURI  string `json:"redirect_uri"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("decode couriers: %v (%s)", err, raw)
	}
	if list.AccountID != accA || len(list.Providers) != 2 {
		t.Fatalf("couriers = %s, want account %s and both providers", raw, accA)
	}
	if list.CallbackPath != "/api/shipping/easyparcel/callback" {
		t.Errorf("callback_path = %q, want the PUBLIC path (the web proxy reaches the API there)", list.CallbackPath)
	}
	if strings.Contains(string(raw), "secret-example") {
		t.Fatalf("a credential value reached the response: %s", raw)
	}
	lala := list.Providers[0]
	if lala.Provider != shipping.ProviderLalamove || !lala.CredentialsPresent || !lala.PointBased {
		t.Errorf("lalamove row = %+v", lala)
	}
	if lala.Connected || lala.Ready || strings.Join(lala.Missing, ",") != "connect" {
		t.Errorf("unconnected lalamove = %+v, want missing just `connect`", lala)
	}
	pe := list.Providers[1]
	if pe.Provider != shipping.ProviderEasyParcel || !pe.NeedsBrowser {
		t.Errorf("easyparcel row = %+v, want needs_browser", pe)
	}
	if list.RedirectURI != courierTestCreds().EasyParcel.RedirectStaging {
		t.Errorf("redirect_uri = %q, want the staging callback this deployment must register", list.RedirectURI)
	}

	// Connecting Lalamove without a point is refused, naming the remedy.
	code, raw = tplReq(t, h, keyA, http.MethodPost,
		"/v1/rest/shipping/couriers/lalamove/connect", `{"origin_postcode":"40150","origin_state":"Selangor"}`)
	if code != http.StatusBadRequest || !strings.Contains(string(raw), "latitud") {
		t.Fatalf("pointless connect = %d (%s), want a 400 naming the missing pickup point", code, raw)
	}

	// A good connect.
	code, raw = tplReq(t, h, keyA, http.MethodPost, "/v1/rest/shipping/couriers/lalamove/connect",
		`{"origin_postcode":"40150","origin_state":"Selangor","origin_lat":"3.1578","origin_lng":"101.7120"}`)
	if code != http.StatusOK {
		t.Fatalf("connect = %d (%s)", code, raw)
	}
	var conn struct {
		Connected bool `json:"connected"`
		Status    struct {
			Ready          bool   `json:"ready"`
			Connected      bool   `json:"connected"`
			Environment    string `json:"environment"`
			OriginLat      string `json:"origin_lat"`
			CredentialsSrc string `json:"credentials_source"`
		} `json:"status"`
	}
	if err := json.Unmarshal(raw, &conn); err != nil {
		t.Fatalf("decode connect: %v (%s)", err, raw)
	}
	if !conn.Connected || !conn.Status.Ready || !conn.Status.Connected {
		t.Fatalf("connect = %s, want a ready connection", raw)
	}
	if conn.Status.Environment != shipping.EnvSandbox {
		t.Errorf("environment = %q, want the KEY PREFIX's environment (sandbox), not a merchant choice", conn.Status.Environment)
	}

	// The connect is account-scoped: another account's read still shows nothing.
	code, raw = tplReq(t, h, keyB, http.MethodGet, "/v1/rest/shipping/couriers", "")
	if code != http.StatusOK {
		t.Fatalf("foreign GET = %d (%s)", code, raw)
	}
	if strings.Contains(string(raw), "3.1578") {
		t.Errorf("another account's pickup point leaked into the read: %s", raw)
	}

	// EasyParcel's connect returns a LINK and never a token.
	code, raw = tplReq(t, h, keyA, http.MethodPost, "/v1/rest/shipping/couriers/easyparcel/connect",
		`{"origin_postcode":"40150","origin_state":"Selangor","environment":"sandbox"}`)
	if code != http.StatusOK {
		t.Fatalf("easyparcel connect = %d (%s)", code, raw)
	}
	var ep struct {
		Connected    bool   `json:"connected"`
		AuthorizeURL string `json:"authorize_url"`
		RedirectURI  string `json:"redirect_uri"`
	}
	if err := json.Unmarshal(raw, &ep); err != nil {
		t.Fatalf("decode easyparcel connect: %v (%s)", err, raw)
	}
	if ep.Connected {
		t.Error("an OAuth provider is not connected until the MERCHANT consents")
	}
	if ep.AuthorizeURL == "" || !strings.Contains(ep.AuthorizeURL, "state=") {
		t.Errorf("authorize_url = %q, want the consent link", ep.AuthorizeURL)
	}
	if ep.RedirectURI != courierTestCreds().EasyParcel.RedirectStaging {
		t.Errorf("redirect_uri = %q, want the registered callback", ep.RedirectURI)
	}
	if strings.Contains(string(raw), "secret-example") {
		t.Errorf("a credential value reached the connect response: %s", raw)
	}

	// Disconnect keeps the pickup address (reconnecting must be one click) and
	// drops the connection.
	code, raw = tplReq(t, h, keyA, http.MethodPost, "/v1/rest/shipping/couriers/lalamove/disconnect", "")
	if code != http.StatusOK {
		t.Fatalf("disconnect = %d (%s)", code, raw)
	}
	if !strings.Contains(string(raw), "3.1578") {
		t.Errorf("the pickup point must survive a disconnect: %s", raw)
	}
	var after struct {
		Providers []struct {
			Provider  string `json:"provider"`
			Connected bool   `json:"connected"`
			Status    string `json:"status"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(raw, &after); err != nil {
		t.Fatalf("decode disconnect: %v (%s)", err, raw)
	}
	for _, p := range after.Providers {
		if p.Provider == shipping.ProviderLalamove && (p.Connected || p.Status != shipping.StatusDisconnected) {
			t.Errorf("lalamove after disconnect = %+v", p)
		}
	}
}

// The PUBLIC callback trusts only the single-use state it issued: a forged or
// replayed consent must not bind a grant.
func TestEasyParcelCallbackRequiresAnIssuedState(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping courier callback test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, _ := bundleFixture(t, ctx, pool)

	h := &PublicHandlers{
		Accounts: dbstore.NewAccountStore(pool),
		Couriers: courierBundle(pool, &stubCourierProvider{name: shipping.ProviderEasyParcel,
			env: shipping.EasyParcelEnvironment, available: true}),
		BaseURL: "https://jomform-staging.wanzul-hosting.com",
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	post := func(query string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/v1/shipping/easyparcel/callback"+query, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// No state at all.
	rec := post("?code=forged")
	if rec.Code != http.StatusFound {
		t.Fatalf("callback = %d, want a redirect (body %s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); !strings.Contains(got, "easyparcel_error=stateInvalid") {
		t.Errorf("Location = %q, want the stateInvalid class", got)
	}
	if strings.Contains(rec.Header().Get("Location"), "forged") {
		t.Errorf("the code leaked into the redirect: %s", rec.Header().Get("Location"))
	}

	// A state the server never issued is refused the same way.
	rec = post("?state=never-issued&code=x")
	if got := rec.Header().Get("Location"); !strings.Contains(got, "easyparcel_error=stateInvalid") {
		t.Errorf("Location = %q, want stateInvalid for an unknown state", got)
	}

	// A state that IS ours, consumed once, cannot be replayed — and the exchange
	// itself fails against the placeholder credentials, which must be a CLASS
	// (tokenExchangeFailed), never a vendor message and never a code.
	store := dbstore.NewCourierStore(pool, testSecretsKey())
	state, err := store.IssueState(ctx, acc, shipping.ProviderEasyParcel,
		"https://jomform-staging.wanzul-hosting.com/api/shipping/easyparcel/callback", "")
	if err != nil {
		t.Fatalf("issue state: %v", err)
	}
	rec = post("?state=" + state + "&code=one-shot")
	if got := rec.Header().Get("Location"); strings.Contains(got, "one-shot") {
		t.Errorf("the code leaked into the redirect: %s", got)
	}
	if rec := post("?state=" + state + "&code=one-shot"); !strings.Contains(rec.Header().Get("Location"), "stateInvalid") {
		t.Errorf("a replayed state must be refused, got %s", rec.Header().Get("Location"))
	}
}

// recordingProvider forwards to an inner provider while capturing the destination
// point it was asked about.
type recordingProvider struct {
	inner shipping.Provider
	lat   *string
	lng   *string
}

func (r *recordingProvider) Name() string        { return r.inner.Name() }
func (r *recordingProvider) Environment() string { return r.inner.Environment() }
func (r *recordingProvider) Available() bool     { return r.inner.Available() }
func (r *recordingProvider) Quote(ctx context.Context, req shipping.QuoteRequest) (shipping.ProviderQuote, error) {
	*r.lat, *r.lng = req.DestinationLat, req.DestinationLng
	return r.inner.Quote(ctx, req)
}

// The drawer's line, pinned: the sale ROW the dashboard reads says which source
// priced the delivery fee, so a disputed charge is answerable from the sale
// itself rather than from a database dive. Before this the row carried only an
// amount and the record lived in shipping_detail alone.
func TestCourierSaleRowCarriesTheFeeSource(t *testing.T) {
	t.Run("a live quote names the courier", func(t *testing.T) {
		h, host, slug, _, _, prodID := courierShippingFixture(t, courierFixtureOpts{
			provider: &stubCourierProvider{name: shipping.ProviderLalamove, env: shipping.EnvSandbox,
				available: true, priceSen: 820, quoteID: "quotation-820"},
			connected: true, withZoneSet: true, zonePoints: true,
			onNoMatch: shipping.OnNoMatchReject,
		})
		sale := saleFor(t, h, host, slug, zoneOrderBody(prodID, "Sabah", "", 1))
		row := toSaleRow(sale, "")
		if row.ShippingSource != shipping.FeeSourceCourier {
			t.Errorf("shipping_source = %q, want %q", row.ShippingSource, shipping.FeeSourceCourier)
		}
		if row.ShippingSourceDetail != shipping.ProviderLalamove {
			t.Errorf("shipping_source_detail = %q, want the provider %q", row.ShippingSourceDetail, shipping.ProviderLalamove)
		}
		if row.ShippingFallbackReason != "" {
			t.Errorf("shipping_fallback_reason = %q on a sale the courier did price", row.ShippingFallbackReason)
		}
	})

	t.Run("a fallback names the zone AND the failure class", func(t *testing.T) {
		h, host, slug, _, _, prodID := courierShippingFixture(t, courierFixtureOpts{
			provider: &stubCourierProvider{name: shipping.ProviderLalamove, available: true,
				err: errors.New("vendor down")},
			connected: true, withZoneSet: true, zonePoints: true,
			onNoMatch: shipping.OnNoMatchReject,
		})
		sale := saleFor(t, h, host, slug, zoneOrderBody(prodID, "Sabah", "", 1))
		row := toSaleRow(sale, "")
		if row.ShippingSource != shipping.FeeSourceZones {
			t.Errorf("shipping_source = %q, want %q (the fee is NOT a live rate)", row.ShippingSource, shipping.FeeSourceZones)
		}
		if row.ShippingSourceDetail != "East Malaysia" {
			t.Errorf("shipping_source_detail = %q, want the matched zone", row.ShippingSourceDetail)
		}
		if row.ShippingFallbackReason != shipping.FallbackError {
			t.Errorf("shipping_fallback_reason = %q, want %q", row.ShippingFallbackReason, shipping.FallbackError)
		}
	})

	t.Run("a pre-existing static sale says nothing", func(t *testing.T) {
		// A sale with no shipping fee at all must not gain a provenance line: the
		// drawer renders nothing when shipping_source is empty.
		row := toSaleRow(&dbstore.Sale{RefCode: "JF-old", ShippingDetail: json.RawMessage("{}")}, "")
		if row.ShippingSource != "" || row.ShippingSourceDetail != "" || row.ShippingFallbackReason != "" {
			t.Errorf("a sale with no shipping_detail gained provenance: %+v", row)
		}
	})
}
