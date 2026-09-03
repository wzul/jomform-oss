// Issue #29: the dashboard-bundle must carry the dashboard landing data
// (products, summary, recent sales, analytics) next to me+spaces+settings,
// so the heaviest navigation becomes ONE request. Integration against a
// real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/db"
	"github.com/wzul/jomform/api/internal/dbstore"
)

var (
	bundleOnce sync.Once
	bundlePool *pgxpool.Pool
	bundleErr  error
)

func bundleTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("JOMFORM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping dashboard-bundle integration test")
	}
	bundleOnce.Do(func() {
		pool, err := db.Connect(context.Background(), dsn)
		if err != nil {
			bundleErr = err
			return
		}
		if err = db.Migrate(context.Background(), pool, testLogger()); err != nil {
			bundleErr = err
			return
		}
		bundlePool = pool
	})
	if bundleErr != nil {
		t.Fatalf("test database unavailable: %v", bundleErr)
	}
	return bundlePool
}

// bundleFixture seeds one user + account (+ space) and returns the account
// and user ids. Every call creates fresh rows so tests never collide.
func bundleFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (accountID, userID string) {
	t.Helper()
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text)
		 RETURNING id`, userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	return accountID, userID
}

// newBundleHandlers wires a RESTHandlers over the test pool with a real
// SecretsKey so chip_status decryption works.
func newBundleHandlers(pool *pgxpool.Pool) *RESTHandlers {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return &RESTHandlers{
		Accounts:          dbstore.NewAccountStore(pool),
		Products:          dbstore.NewProductStore(pool),
		ProductVariations: dbstore.NewProductVariationStore(pool),
		Forms:             dbstore.NewFormStore(pool),
		Keys:       dbstore.NewAPIKeyStore(pool),
		Sales:      dbstore.NewSaleStore(pool),
		Subs:       dbstore.NewSubscriptionStore(pool),
		Domains:    dbstore.NewDomainStore(pool),
		Webhooks:   dbstore.NewWebhookStore(pool),
		Spaces:     dbstore.NewSpaceStore(pool),
		Users:      dbstore.NewUserStore(pool),
		SecretsKey: key,
	}
}

// bundleAPIKey creates an API key for the account and returns the raw key
// (session-free auth path, same as production MCP clients use).
func bundleAPIKey(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accountID string) string {
	t.Helper()
	raw, hash, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}
	if _, err := dbstore.NewAPIKeyStore(pool).Create(ctx, accountID, hash, "it-bundle", []string{"admin"}); err != nil {
		t.Fatalf("store api key: %v", err)
	}
	return raw
}

func bundleGet(t *testing.T, h *RESTHandlers, rawKey, path string) (int, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("bundle %s: bad json: %v (%s)", path, err, rec.Body.String())
	}
	return rec.Code, body
}

func TestDashboardBundleCarriesLandingData(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)

	products := dbstore.NewProductStore(pool)
	if err := products.Create(ctx, &dbstore.Product{
		AccountID: accountID, Name: "Bundle Tee", PriceSen: 2500, Type: "one_time",
	}); err != nil {
		t.Fatalf("create product: %v", err)
	}

	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	code, body := bundleGet(t, h, raw, "/v1/rest/dashboard-bundle")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	for _, field := range []string{"me", "spaces", "products", "summary", "sales", "form_analytics", "product_analytics"} {
		if _, ok := body[field]; !ok {
			t.Errorf("bundle missing %q field", field)
		}
	}
	if me, ok := body["me"].(map[string]any); !ok || me["email"] == nil {
		t.Errorf("bundle me missing or wrong shape: %v", body["me"])
	}
	if prods, ok := body["products"].([]any); !ok || len(prods) != 1 {
		t.Errorf("products = %v, want exactly the one seeded", body["products"])
	}
	if sum, ok := body["summary"].(map[string]any); !ok || sum["counts"] == nil {
		t.Errorf("summary missing counts: %v", body["summary"])
	}
	if sales, ok := body["sales"].(map[string]any); !ok || sales["items"] == nil || sales["total"] == nil {
		t.Errorf("sales page shape wrong: %v", body["sales"])
	}
	if fs, ok := body["form_analytics"].([]any); !ok || len(fs) != 0 {
		// empty slice is the correct zero-state for a fresh account
		t.Logf("form_analytics = %v (empty ok for fresh account)", body["form_analytics"])
	}
	_ = strings.TrimSpace // keep strings import used if assertions change
}

func TestDashboardBundleSummaryMatchesSummaryEndpoint(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)

	forms := dbstore.NewFormStore(pool)
	f := &dbstore.Form{AccountID: accountID, Slug: "it-bundle-" + accountID[:8], Title: "Bundle form", Status: "published"}
	if err := forms.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}
	sales := dbstore.NewSaleStore(pool)
	if err := sales.Create(ctx, &dbstore.Sale{
		AccountID: accountID, FormID: f.ID, RefCode: "IT-" + accountID[:8] + "-1",
		Client:   dbstore.SaleClient{FullName: "Bundle Buyer", Email: "buyer@example.test"},
		Items:    []dbstore.SaleItem{{ProductID: "it-p1", Name: "Tee", PriceSen: 1000, Qty: 2}},
		TotalSen: 2000, SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip", Status: dbstore.SaleConfirmed,
	}); err != nil {
		t.Fatalf("create sale: %v", err)
	}

	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	_, bundle := bundleGet(t, h, raw, "/v1/rest/dashboard-bundle")
	code, direct := bundleGet(t, h, raw, "/v1/rest/summary")
	if code != http.StatusOK {
		t.Fatalf("summary status = %d", code)
	}
	b, _ := json.Marshal(bundle["summary"])
	d, _ := json.Marshal(direct)
	if string(b) != string(d) {
		t.Errorf("bundle summary != /rest/summary:\n bundle: %s\n direct: %s", b, d)
	}

	// The bundle's sales page must mirror GET /v1/rest/sales?page=1&limit=10.
	_, directSales := bundleGet(t, h, raw, "/v1/rest/sales?page=1&limit=10")
	if bs, ok := bundle["sales"].(map[string]any); ok {
		if ds, ok := directSales["items"].([]any); ok {
			if items, ok := bs["items"].([]any); ok && len(items) != len(ds) {
				t.Errorf("bundle sales items = %d, direct = %d", len(items), len(ds))
			}
		}
		if bs["total"].(float64) != directSales["total"].(float64) {
			t.Errorf("bundle sales total = %v, direct = %v", bs["total"], directSales["total"])
		}
	} else {
		t.Errorf("bundle sales missing: %v", bundle["sales"])
	}

	// Analytics must be present and reflect the one confirmed sale.
	fs, _ := bundle["form_analytics"].([]any)
	if len(fs) != 1 {
		t.Fatalf("form_analytics = %v, want 1 row", bundle["form_analytics"])
	}
	row := fs[0].(map[string]any)
	if row["revenue_sen"].(float64) != 2000 {
		t.Errorf("form_analytics revenue_sen = %v, want 2000", row["revenue_sen"])
	}
}

func TestDashboardBundleSpaceScopeHonoured(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, userID := bundleFixture(t, ctx, pool)

	sp := dbstore.NewSpaceStore(pool)
	space, err := sp.Create(ctx, userID, "Bundle Space", "it-space-"+accountID[:8])
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	// Link the account to the space so ?space= resolves to it.
	if _, err := pool.Exec(ctx, `UPDATE accounts SET space_id=$1 WHERE id=$2`, space.ID, accountID); err != nil {
		t.Fatalf("link account to space: %v", err)
	}

	h := newBundleHandlers(pool)
	// Session-free checks: the bundle itself must never 403 on scope errors.
	raw := bundleAPIKey(t, ctx, pool, accountID)

	code, body := bundleGet(t, h, raw, "/v1/rest/dashboard-bundle")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	// Products section must be non-null (unscoped fetch succeeded).
	if _, ok := body["products"].([]any); !ok {
		t.Errorf("products not a list: %v", body["products"])
	}
	// A foreign space slug must leave landing sections null (never 403).
	// (With an API key there is no session user, so the scope cannot resolve;
	//  the sections stay null and the bundle still returns 200.)
	code, body = bundleGet(t, h, raw, "/v1/rest/dashboard-bundle?space=no-such-space")
	if code != http.StatusOK {
		t.Fatalf("foreign-space status = %d, want 200", code)
	}
	if body["products"] != nil {
		t.Errorf("products should be null on scope error, got %v", body["products"])
	}
}

func TestDashboardBundleChipSecretDecrypts(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	sealed, err := cryptoutil.Encrypt(key, []byte("sk_test_bundle"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := dbstore.NewAccountStore(pool).SetChipCredentials(ctx, accountID, "br_bundle", sealed, "test", "", nil); err != nil {
		t.Fatalf("set chip creds: %v", err)
	}

	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	_, body := bundleGet(t, h, raw, "/v1/rest/dashboard-bundle")
	cs, ok := body["chip_status"].(map[string]any)
	if !ok {
		t.Fatalf("chip_status missing: %v", body["chip_status"])
	}
	if cs["secret_key"] != "sk_test_bundle" {
		t.Errorf("chip secret = %v, want sk_test_bundle", cs["secret_key"])
	}
}
