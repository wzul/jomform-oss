package httpapi

// Card t_453489b9 — the buyer picks the payment method on JomForm and CHIP does
// not ask again.
//
// These are the ROUTE-level guards. The URL/whitelist arithmetic is unit-tested
// in payments/paymethod_test.go; what is asserted here is that the two buyer
// surfaces actually reach it, on a real sale and a real CHIP request body:
//
//  1. the chosen group NARROWS the CHIP purchase's payment_method_whitelist (so
//     CHIP has nothing else to offer) and the buyer is redirected to CHIP's
//     checkout page with ?preferred=<identifier>;
//  2. a card-only purchase — which is when CHIP answers with a direct_post_url —
//     NEVER redirects the buyer to that URL and never appends a query to it
//     (PITFALL 1: it is a POST target for CHIP's hosted card fields);
//  3. a method this checkout cannot honour is REFUSED (400) with a sentence, and
//     no purchase is created: a silent fallback would pay with a method the
//     buyer never picked;
//  4. an order that names no method (an older page, an embed) is byte-for-byte
//     the pre-feature request — an unchanged whitelist and CHIP's URL untouched.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// pickChip is a CHIP stand-in whose RESPONSE is configurable (the shared
// fakeChip in payment_fee_t_135c3c72_test.go is pinned to one body and is used
// by the fee tests; this one exists because PITFALL 1 is a property of the
// RESPONSE — a card-only purchase comes back with a direct_post_url).
type pickChip struct {
	mu     sync.Mutex
	bodies []map[string]any
	// respond is the JSON body returned by POST /purchases/.
	respond func() map[string]any
	// available is what GET /payment_methods/ answers with. nil means 404 —
	// "CHIP could not be asked" — which is the pre-t_1d0198e3 behaviour of this
	// fake and what the other tests in the package rely on.
	available []string
	// methodCalls / methodAmounts record the listing calls, so a test can prove
	// the picker asked (and with what amount).
	methodCalls   int
	methodAmounts []string
}

func (pc *pickChip) listCalls() (int, []string) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	return pc.methodCalls, append([]string{}, pc.methodAmounts...)
}

func (pc *pickChip) last() map[string]any {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	if len(pc.bodies) == 0 {
		return nil
	}
	return pc.bodies[len(pc.bodies)-1]
}

func (pc *pickChip) count() int {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	return len(pc.bodies)
}

// pickFixture is one workspace with CHIP credentials that decrypt, a product, a
// published payment form, and a handler wired to pickChip.
type pickFixture struct {
	h        *PublicHandlers
	host     string
	formSlug string
	product  string
	account  string
	ws       string
	fees     *dbstore.PaymentFeeStore
	chip     *pickChip
}

// newPickFixture wires one workspace ... available is what CHIP's
// GET /payment_methods/ answers with (nil = the endpoint 404s).
func newPickFixture(t *testing.T, respond func() map[string]any) pickFixture {
	return newPickFixtureWithAvailability(t, respond, nil)
}

func newPickFixtureWithAvailability(t *testing.T, respond func() map[string]any, available []string) pickFixture {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)
	key := []byte(feeTestEncKey)

	sealed, err := cryptoutil.Encrypt(key, []byte("test-chip-secret"))
	if err != nil {
		t.Fatalf("encrypt chip secret: %v", err)
	}
	var userID, wsID, wsSlug, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'pick-ws-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING id, slug`, userID).Scan(&wsID, &wsSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id, chip_brand_id, chip_secret_key_enc)
		 VALUES ($1, 'pick-acc-' || md5(random()::text), 'it-' || md5(random()::text), $2, 'brand_test', $3)
		 RETURNING id`, userID, wsID, sealed).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	ps := dbstore.NewProductStore(pool)
	prod := &dbstore.Product{AccountID: accountID, Name: "Produk Ujian", PriceSen: 100000, Type: dbstore.SaleTypeOneTime, Active: true}
	if err := ps.Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	fs := dbstore.NewFormStore(pool)
	formSlug := "it-pick-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "Pick form",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{
			"blocks": []any{
				// product_id is what makes this a LIVE line-item control for the
				// block renderer (blockLayoutPaintsNoLineItemControl): without it a
				// payment form is served as "not accepting orders", so a test that
				// needs the REAL page (the picker) would assert against that page.
				map[string]any{"id": "b1", "type": "product", "product_id": prod.ID, "products": []any{
					map[string]any{"id": prod.ID, "name": prod.Name, "price_sen": prod.PriceSen},
				}},
			},
		},
	}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	pc := &pickChip{respond: respond}
	pc.available = available
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/payment_methods/" {
			pc.mu.Lock()
			pc.methodCalls++
			pc.methodAmounts = append(pc.methodAmounts, r.URL.Query().Get("amount"))
			list := pc.available
			pc.mu.Unlock()
			if list == nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"available_payment_methods": list})
			return
		}
		if r.URL.Path != "/purchases/" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		pc.mu.Lock()
		pc.bodies = append(pc.bodies, body)
		pc.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pc.respond())
	}))
	t.Cleanup(srv.Close)

	sales := dbstore.NewSaleStore(pool)
	accounts := dbstore.NewAccountStore(pool)
	pay := payments.New(chip.New(srv.URL), accounts, sales, dbstore.NewSubscriptionStore(pool), key, "http://localhost:8080")
	pay.Workspaces = dbstore.NewWorkspaceStore(pool)
	pay.Forms = fs
	h := &PublicHandlers{
		Forms:       fs,
		Sales:       sales,
		Accounts:    accounts,
		Products:    ps,
		Workspaces:  dbstore.NewWorkspaceStore(pool),
		BaseURL:     "http://localhost:8080",
		Pay:         pay,
		PaymentFees: dbstore.NewPaymentFeeStore(pool),
	}
	return pickFixture{h: h, host: wsSlug + ".jomform.com", formSlug: formSlug, product: prod.ID, account: accountID, ws: wsID, fees: dbstore.NewPaymentFeeStore(pool), chip: pc}
}

// setWhitelist stores the account's CHIP method whitelist (what the merchant
// ticks in Settings > CHIP), which is what the picker and the order validate
// against.
func (fx pickFixture) setWhitelist(t *testing.T, methods ...string) {
	t.Helper()
	if _, err := bundleTestPool(t).Exec(context.Background(),
		`UPDATE accounts SET chip_method_whitelist = $1 WHERE id = $2`, methods, fx.account); err != nil {
		t.Fatalf("set whitelist: %v", err)
	}
}

// order posts one item with an optional pay_method.
func (fx pickFixture) order(t *testing.T, payMethod string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + fx.product + `","quantity":1}],` +
		`"payment_method":"chip","fields":{}`
	if payMethod != "" {
		body += `,"pay_method":"` + payMethod + `"`
	}
	body += `}`
	return issue461Serve(t, fx.h, fx.host, http.MethodPost, "/"+fx.formSlug+"/order", body)
}

func checkoutURL(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var out struct {
		CheckoutURL string `json:"checkout_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode order response: %v (body %s)", err, rec.Body.String())
	}
	return out.CheckoutURL
}

func sentWhitelist(t *testing.T, sent map[string]any) []string {
	t.Helper()
	raw, _ := sent["payment_method_whitelist"].([]any)
	out := make([]string, 0, len(raw))
	for _, m := range raw {
		out = append(out, m.(string))
	}
	return out
}

// TestPayMethodNarrowsWhitelistAndPrefersFPX: the buyer picked FPX B2C, so CHIP
// gets a whitelist holding ONLY `fpx` — not the FPX family (card t_1d0198e3:
// fpx_b2b1 is a separate product with its own fee, and a whitelist that still
// offers it lets CHIP ask the buyer B2C-or-B2B1 again) — and the buyer lands on
// CHIP's page with FPX pre-selected.
func TestPayMethodNarrowsWhitelistAndPrefersFPX(t *testing.T) {
	fx := newPickFixture(t, func() map[string]any {
		return map[string]any{
			"id": "pur_fpx_1", "status": "created",
			"checkout_url": "https://pay.example.test/pur_fpx_1?lang=ms",
			"total":        100000, "currency": "MYR",
		}
	})

	rec := fx.order(t, payments.PayMethodFPX)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	sent := fx.chip.last()
	if sent == nil {
		t.Fatalf("no CHIP purchase was created")
	}
	if got := strings.Join(sentWhitelist(t, sent), ","); got != "fpx" {
		t.Fatalf("payment_method_whitelist = %q, want only `fpx` — anything else is a method CHIP can still offer", got)
	}
	want := "https://pay.example.test/pur_fpx_1?lang=ms&preferred=fpx"
	if got := checkoutURL(t, rec); got != want {
		t.Fatalf("checkout_url = %q, want %q (the existing query must survive and FPX be pre-selected)", got, want)
	}
}

// TestPayMethodCardNeverRedirectsToDirectPostURL is PITFALL 1 at the route: a
// card-only whitelist makes CHIP answer with a direct_post_url. The buyer must
// never be sent there, and no query may be appended to it.
func TestPayMethodCardNeverRedirectsToDirectPostURL(t *testing.T) {
	const (
		checkout = "https://pay.example.test/pur_card_1"
		direct   = "https://pay.example.test/direct_post/pur_card_1/"
	)
	fx := newPickFixture(t, func() map[string]any {
		return map[string]any{
			"id": "pur_card_1", "status": "created",
			"checkout_url": checkout, "direct_post_url": direct,
			"total": 100000, "currency": "MYR",
		}
	})

	rec := fx.order(t, payments.PayMethodCard)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if got := strings.Join(sentWhitelist(t, fx.chip.last()), ","); got != "visa,mastercard,maestro" {
		t.Fatalf("payment_method_whitelist = %q, want the card networks only", got)
	}
	got := checkoutURL(t, rec)
	if strings.Contains(got, "direct_post") {
		t.Fatalf("the buyer was redirected to a direct_post_url: %q — that URL is a POST target for CHIP's card fields, not a page", got)
	}
	if got != checkout+"?preferred=card" {
		t.Fatalf("checkout_url = %q, want %q", got, checkout+"?preferred=card")
	}
}

// TestPayMethodCardLeavesBareDirectPostURLUntouched: if the only URL CHIP returns
// IS the direct-post URL (both fields hold it), it is passed through exactly — a
// mangled URL is worse than an unusable one, because the buyer cannot tell.
func TestPayMethodCardLeavesBareDirectPostURLUntouched(t *testing.T) {
	const direct = "https://pay.example.test/direct_post/pur_only_1/"
	fx := newPickFixture(t, func() map[string]any {
		return map[string]any{
			"id": "pur_only_1", "status": "created",
			"checkout_url": direct, "direct_post_url": direct,
			"total": 100000, "currency": "MYR",
		}
	})
	rec := fx.order(t, payments.PayMethodCard)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if got := checkoutURL(t, rec); got != direct {
		t.Fatalf("checkout_url = %q, want the untouched %q (no ?preferred= appended to a direct-post URL)", got, direct)
	}
}

// TestPayMethodUnavailableIsRefusedWith400: a method the workspace does not
// enable is refused BEFORE a purchase exists, with a sentence — never silently
// downgraded to whatever the merchant's whitelist allows.
func TestPayMethodUnavailableIsRefusedWith400(t *testing.T) {
	fx := newPickFixture(t, func() map[string]any {
		return map[string]any{"id": "pur_never", "status": "created",
			"checkout_url": "https://pay.example.test/never", "total": 100000, "currency": "MYR"}
	})
	fx.setWhitelist(t, "fpx")

	rec := fx.order(t, payments.PayMethodCard)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("submit status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if !strings.Contains(strings.ToLower(out.Message), "not available") {
		t.Fatalf("refusal message = %q, want the buyer-facing 'not available' sentence", out.Message)
	}
	if n := fx.chip.count(); n != 0 {
		t.Fatalf("%d CHIP purchase(s) were created for a refused method — the buyer would have been charged with a method they did not pick", n)
	}

	// The same workspace's enabled method still works, so the refusal is about
	// the choice and not about the checkout being broken.
	if rec := fx.order(t, payments.PayMethodFPX); rec.Code != http.StatusCreated {
		t.Fatalf("fpx status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestNoPayMethodKeepsPreviousBehaviour pins that the feature is additive: an
// order from a page with no picker sends no pay_method, keeps the merchant's
// whitelist untouched and CHIP's URL is passed through byte-identically.
func TestNoPayMethodKeepsPreviousBehaviour(t *testing.T) {
	const checkout = "https://pay.example.test/pur_plain_1"
	fx := newPickFixture(t, func() map[string]any {
		return map[string]any{"id": "pur_plain_1", "status": "created",
			"checkout_url": checkout, "total": 100000, "currency": "MYR"}
	})
	fx.setWhitelist(t, "fpx", "card")

	rec := fx.order(t, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	// ResolveWhitelist expands 'card' to the networks; nothing else changes.
	if got := strings.Join(sentWhitelist(t, fx.chip.last()), ","); got != "fpx,visa,mastercard,maestro" {
		t.Fatalf("payment_method_whitelist = %q, want the merchant's own list unchanged", got)
	}
	if got := checkoutURL(t, rec); got != checkout {
		t.Fatalf("checkout_url = %q, want the untouched %q", got, checkout)
	}
}
