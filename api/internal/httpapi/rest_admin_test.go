// Issue #387: super-admin platform-wide usage overview endpoint
// (GET /v1/admin/overview). Integration against a real Postgres + Valkey;
// skipped without JOMFORM_TEST_DATABASE_URL / JOMFORM_TEST_VALKEY_URL.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// adminOverviewFixture wires a RESTHandlers over the test pool with a real
// Sessions store (Valkey) and a super-admin allowlist. It returns the
// handlers plus a session cookie for the given user id.
func adminOverviewFixture(t *testing.T, pool *pgxpool.Pool, userID string, allowlist func(string) bool) (*RESTHandlers, string) {
	t.Helper()
	valkeyURL := os.Getenv("JOMFORM_TEST_VALKEY_URL")
	if valkeyURL == "" {
		t.Skip("JOMFORM_TEST_VALKEY_URL not set; skipping admin overview integration test")
	}
	sess, err := auth.NewSessions(valkeyURL, testSessionOptions()...)
	if err != nil {
		t.Fatalf("valkey sessions: %v", err)
	}
	token, err := sess.Create(context.Background(), userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	h := &RESTHandlers{
		Accounts:            dbstore.NewAccountStore(pool),
		Users:               dbstore.NewUserStore(pool),
		Workspaces:          dbstore.NewWorkspaceStore(pool),
		Sales:               dbstore.NewSaleStore(pool),
		Submissions:         dbstore.NewSubmissionStore(pool),
		EmailDeliveryEvents: dbstore.NewEmailDeliveryEventStore(pool),
		Media:               dbstore.NewMediaStore(pool),
		Sessions:            sess,
		SuperAdmin:          allowlist,
	}
	return h, token
}

// TestAdminOverviewSuperAdminGated verifies the /v1/admin/overview endpoint
// returns 200 for a super-admin and 403 for a non-super-admin (issue #387
// acceptance: non-admins get 403).
func TestAdminOverviewSuperAdminGated(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	_, userID := bundleFixture(t, ctx, pool)

	// The fixture user's email is random; capture it so the allowlist can
	// match it exactly.
	var email string
	if err := pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, userID).Scan(&email); err != nil {
		t.Fatalf("read user email: %v", err)
	}

	// Super-admin allowlist matches this user.
	h, token := adminOverviewFixture(t, pool, userID, func(e string) bool { return e == email })

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Super-admin → 200 with totals.
	req := httptest.NewRequest("GET", "/v1/admin/overview", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("super-admin overview: got %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if _, ok := body["totals"]; !ok {
		t.Errorf("overview missing totals: %v", body)
	}

	// Non-super-admin (allowlist rejects everyone) → 403.
	h2, token2 := adminOverviewFixture(t, pool, userID, func(string) bool { return false })
	mux2 := http.NewServeMux()
	h2.RegisterRoutes(mux2)
	req2 := httptest.NewRequest("GET", "/v1/admin/overview", nil)
	req2.AddCookie(&http.Cookie{Name: sessionCookie, Value: token2})
	rec2 := httptest.NewRecorder()
	mux2.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("non-super-admin overview: got %d, want 403 (body %s)", rec2.Code, rec2.Body.String())
	}
}
