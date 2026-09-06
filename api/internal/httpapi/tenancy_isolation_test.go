// Issue #158: proper space tenancy — each space owns its own account, and
// resolve() resolves the account from the selected space (?space=) rather
// than the user's first account. This is the security regression test: a
// merchant switching to a second space must NEVER see the first space's CHIP
// credentials, products, sales, domains or webhooks.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL) + Valkey
// (JOMFORM_TEST_VALKEY_URL); skipped in CI (which sets neither).
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// tenancyFixture seeds a user with TWO spaces, each owning its own account
// (via Spaces.Create, which now creates a fresh account per space). It sets
// CHIP credentials on the FIRST space's account only, then returns the
// handlers, the session cookie, and both space slugs.
func tenancyFixture(t *testing.T) (*RESTHandlers, *pgxpool.Pool, string, string, string) {
	t.Helper()
	dsn := os.Getenv("JOMFORM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping tenancy-isolation test")
	}
	pool := bundleTestPool(t)
	sess := authTestValkey(t) // skips when JOMFORM_TEST_VALKEY_URL unset

	ctx := context.Background()
	// User
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('ten-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}

	sp := dbstore.NewSpaceStore(pool)
	// First space — Spaces.Create now creates a fresh account linked to it.
	space1, err := sp.Create(ctx, userID, "Tenancy One", "ten-one-"+userID[:8])
	if err != nil {
		t.Fatalf("create space1: %v", err)
	}
	// Second space — must get its OWN fresh account, not inherit space1's.
	space2, err := sp.Create(ctx, userID, "Tenancy Two", "ten-two-"+userID[:8])
	if err != nil {
		t.Fatalf("create space2: %v", err)
	}

	// Resolve each space's account id.
	acc1 := accountIDForSpace(t, ctx, pool, space1.ID)
	acc2 := accountIDForSpace(t, ctx, pool, space2.ID)
	if acc1 == "" || acc2 == "" {
		t.Fatalf("spaces must each own an account (acc1=%q acc2=%q)", acc1, acc2)
	}
	if acc1 == acc2 {
		t.Fatalf("space1 and space2 must NOT share an account (both %q)", acc1)
	}

	// Set CHIP credentials on space1's account ONLY.
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	sealed, err := cryptoutil.Encrypt(key, []byte("sk_test_tenancy_one"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := dbstore.NewAccountStore(pool).SetChipCredentials(ctx, acc1, "br_ten_one", sealed, "test", "", nil, true); err != nil {
		t.Fatalf("set chip creds on space1: %v", err)
	}

	// Wire RESTHandlers with a real session store.
	h := newBundleHandlers(pool)
	h.Sessions = sess
	h.SecretsKey = key

	// Create a session for the user.
	token, err := sess.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return h, pool, token, space1.Slug, space2.Slug
}

func accountIDForSpace(t *testing.T, ctx context.Context, pool *pgxpool.Pool, spaceID string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE space_id=$1`, spaceID).Scan(&id)
	if err != nil {
		return ""
	}
	return id
}

// tenancyGet issues a session-authed GET and returns the decoded body.
func tenancyGet(t *testing.T, h *RESTHandlers, token, path string) (int, map[string]any) {
	t.Helper()
	code, raw := tenancyGetRaw(t, h, token, path)
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("GET %s: bad json: %v (%s)", path, err, string(raw))
	}
	return code, body
}

// tenancyGetRaw issues a session-authed GET and returns the raw body bytes.
func tenancyGetRaw(t *testing.T, h *RESTHandlers, token, path string) (int, []byte) {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// TestTenancySpaceScopedChipIsolation: switching to a second space must show
// that space's (empty) CHIP status, never the first space's credentials.
func TestTenancySpaceScopedChipIsolation(t *testing.T) {
	h, _, token, slug1, slug2 := tenancyFixture(t)

	// Scoped to space1 → its CHIP creds are configured.
	code, b1 := tenancyGet(t, h, token, "/v1/rest/settings/chip?space="+slug1)
	if code != http.StatusOK {
		t.Fatalf("space1 chip status = %d", code)
	}
	if cs, ok := b1["configured"].(bool); !ok || !cs {
		t.Fatalf("space1 chip should be configured, got %v", b1)
	}

	// Scoped to space2 → its own account has NO CHIP creds (clean slate).
	code, b2 := tenancyGet(t, h, token, "/v1/rest/settings/chip?space="+slug2)
	if code != http.StatusOK {
		t.Fatalf("space2 chip status = %d", code)
	}
	if cs, ok := b2["configured"].(bool); !ok || cs {
		t.Fatalf("space2 chip must be unconfigured (no cross-space leakage), got %v", b2)
	}
	if sk, _ := b2["secret_key"].(string); sk != "" {
		t.Fatalf("space2 must not expose space1's secret key, got %q", sk)
	}
}

// TestTenancySpaceScopedProductsIsolation: products created in space1 must
// not appear when scoped to space2.
func TestTenancySpaceScopedProductsIsolation(t *testing.T) {
	h, pool, token, slug1, slug2 := tenancyFixture(t)
	ctx := context.Background()

	sp1, err := h.Spaces.BySlug(ctx, slug1)
	if err != nil || sp1 == nil {
		t.Fatalf("resolve space1: %v", err)
	}
	acc1 := accountIDForSpace(t, ctx, pool, sp1.ID)
	if err := dbstore.NewProductStore(pool).Create(ctx, &dbstore.Product{
		AccountID: acc1, Name: "Space1 Only Product", PriceSen: 1000, Type: "one_time",
	}); err != nil {
		t.Fatalf("create product in space1: %v", err)
	}

	// /v1/rest/products returns a plain JSON array.
	_, raw1 := tenancyGetRaw(t, h, token, "/v1/rest/products?space="+slug1)
	var prods1 []any
	if err := json.Unmarshal(raw1, &prods1); err != nil || len(prods1) != 1 {
		t.Fatalf("space1 products = %s, want 1", string(raw1))
	}

	_, raw2 := tenancyGetRaw(t, h, token, "/v1/rest/products?space="+slug2)
	var prods2 []any
	if err := json.Unmarshal(raw2, &prods2); err != nil || len(prods2) != 0 {
		t.Fatalf("space2 products = %s, want 0 (no cross-space leakage)", string(raw2))
	}
}
