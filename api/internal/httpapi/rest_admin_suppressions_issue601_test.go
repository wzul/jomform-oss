// Issue #601: the super-admin suppression list REST endpoints.
//
// GET  /v1/admin/suppressions?q=&page=&limit=   list + search + pager
// POST /v1/admin/suppressions                   manual add (reason + note)
// DELETE /v1/admin/suppressions/{email}         remove (mail resumes)
//
// Integration against real Postgres + Valkey (a real Sessions store is needed
// for the session cookie the super-admin gate resolves); skipped without
// JOMFORM_TEST_DATABASE_URL / JOMFORM_TEST_VALKEY_URL.
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// suppressionsFixture wires the admin suppression endpoints over the test pool
// with a real session, and returns the handlers, a session cookie for the given
// user and the suppression store.
func suppressionsFixture(t *testing.T, pool *pgxpool.Pool, userID string, isSuperAdmin bool) (*RESTHandlers, string, *dbstore.SuppressionStore) {
	t.Helper()
	// A real Valkey-backed Sessions store: the super-admin gate resolves the
	// session cookie to a user, so a stub would not exercise it.
	sess := authTestValkey(t)
	token, err := sess.Create(context.Background(), userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	supp := dbstore.NewSuppressionStore(pool)
	h := &RESTHandlers{
		Accounts:     dbstore.NewAccountStore(pool),
		Users:        dbstore.NewUserStore(pool),
		Sessions:     sess,
		SuperAdmin:   func(string) bool { return isSuperAdmin },
		Suppressions: supp,
	}
	return h, token, supp
}

// adminSuppressionsMux builds a mux + cookie for one super-admin request flow,
// so each test reads as setup → request → assert.
func adminSuppressionsMux(t *testing.T, pool *pgxpool.Pool, userID string, isSuperAdmin bool) (*http.ServeMux, string) {
	t.Helper()
	h, token, _ := suppressionsFixture(t, pool, userID, isSuperAdmin)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux, token
}

// TestAdminSuppressionsSuperAdminGated pins the gate: a non-super-admin gets
// 403 and nothing is written, on all three endpoints.
func TestAdminSuppressionsSuperAdminGated(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	testdb.Reset(t, pool, "suppressed_emails")
	_, userID := bundleFixture(t, ctx, pool)

	mux, token := adminSuppressionsMux(t, pool, userID, false)

	addr := fmt.Sprintf("nongate-%d@example.test", time.Now().UnixNano())

	// No allowlist match → 403 on every verb.
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/v1/admin/suppressions", ""},
		{http.MethodPost, "/v1/admin/suppressions", `{"email":"` + addr + `","reason":"manual"}`},
		{http.MethodDelete, "/v1/admin/suppressions/" + addr, ""},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("issue #601: %s %s as a non-super-admin = %d, want 403 (body %s)",
				tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	// And the rejected POST really wrote nothing.
	if ok, err := dbstore.NewSuppressionStore(pool).IsSuppressed(ctx, addr); err != nil {
		t.Fatalf("IsSuppressed: %v", err)
	} else if ok {
		t.Error("issue #601: a 403'd POST still wrote a suppression")
	}
}

// TestAdminSuppressionsAddListRemove is the acceptance path: add an address by
// hand, see it in the list with its reason/source/note/actor, find it by
// search, then remove it and confirm mail to it would resume.
func TestAdminSuppressionsAddListRemove(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	testdb.Reset(t, pool, "suppressed_emails")
	_, userID := bundleFixture(t, ctx, pool)

	var actorEmail string
	if err := pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, userID).Scan(&actorEmail); err != nil {
		t.Fatalf("read actor email: %v", err)
	}

	mux, token := adminSuppressionsMux(t, pool, userID, true)
	supp := dbstore.NewSuppressionStore(pool)

	// The operator types the address with mixed case and a trailing space; it
	// must be stored lower-cased and trimmed (the acceptance criterion is about
	// the DB, not just the app layer).
	typed := fmt.Sprintf("  Mixed-Case-%d@Example.TEST  ", time.Now().UnixNano())
	stored := strings.ToLower(strings.TrimSpace(typed))

	// ── POST ──
	body := fmt.Sprintf(`{"email":%q,"reason":"hard_bounce","note":"asked us to stop"}`, typed)
	rec := doAdminJSON(mux, token, http.MethodPost, "/v1/admin/suppressions", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var addRes struct {
		Status  string `json:"status"`
		Email   string `json:"email"`
		Created bool   `json:"created"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &addRes); err != nil {
		t.Fatalf("POST body: %v (%s)", err, rec.Body.String())
	}
	if addRes.Email != stored {
		t.Errorf("POST echoed email %q, want the normalised %q", addRes.Email, stored)
	}
	if !addRes.Created {
		t.Error("issue #601: the first POST reported created=false")
	}

	// Idempotent: the same address again is 200 with created=false, not an error.
	recAgain := doAdminJSON(mux, token, http.MethodPost, "/v1/admin/suppressions", body)
	if recAgain.Code != http.StatusOK {
		t.Fatalf("duplicate POST = %d, want 200 (body %s)", recAgain.Code, recAgain.Body.String())
	}
	var againRes struct {
		Created bool `json:"created"`
	}
	_ = json.Unmarshal(recAgain.Body.Bytes(), &againRes)
	if againRes.Created {
		t.Error("issue #601: a duplicate POST reported created=true — the address is not unique")
	}

	// ── GET (list) ──
	rec = doAdminJSON(mux, token, http.MethodGet, "/v1/admin/suppressions?page=1&limit=20", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []struct {
			Email          string `json:"email"`
			Reason         string `json:"reason"`
			Source         string `json:"source"`
			Note           string `json:"note"`
			CreatedAt      string `json:"created_at"`
			CreatedBy      string `json:"created_by"`
			CreatedByEmail string `json:"created_by_email"`
		} `json:"items"`
		Page  int   `json:"page"`
		Limit int   `json:"limit"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("GET body: %v (%s)", err, rec.Body.String())
	}
	if list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("list total=%d len=%d, want 1/1 (body %s)", list.Total, len(list.Items), rec.Body.String())
	}
	row := list.Items[0]
	if row.Email != stored {
		t.Errorf("row email = %q, want %q", row.Email, stored)
	}
	// reason + source + created_at per row are an explicit acceptance criterion.
	if row.Reason != "hard_bounce" {
		t.Errorf("row reason = %q, want hard_bounce", row.Reason)
	}
	if row.Source != "manual" {
		t.Errorf("row source = %q, want manual", row.Source)
	}
	if row.Note != "asked us to stop" {
		t.Errorf("row note = %q, want the typed note", row.Note)
	}
	if row.CreatedAt == "" {
		t.Error("issue #601: the row has no created_at — the acceptance criterion needs it visible")
	}
	if row.CreatedByEmail != actorEmail {
		t.Errorf("row created_by_email = %q, want the adding admin's %q", row.CreatedByEmail, actorEmail)
	}
	if list.Page != 1 || list.Limit != 20 {
		t.Errorf("pager echo page=%d limit=%d, want 1/20", list.Page, list.Limit)
	}

	// ── GET (search, mixed case + whitespace in the term) ──
	q := "  " + strings.ToUpper(stored) + " "
	rec = doAdminJSON(mux, token, http.MethodGet, "/v1/admin/suppressions?q="+urlEscape(q), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET search = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var found struct {
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &found); err != nil {
		t.Fatalf("search body: %v", err)
	}
	if found.Total != 1 {
		t.Errorf("issue #601: searching the upper-cased term found %d rows, want 1 — the term is not normalised", found.Total)
	}

	// A search that matches nothing reports zero (not the whole list).
	rec = doAdminJSON(mux, token, http.MethodGet, "/v1/admin/suppressions?q=nothing-matches-this", "")
	var none struct {
		Total int64 `json:"total"`
		Items []any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &none); err != nil {
		t.Fatalf("no-match body: %v", err)
	}
	if none.Total != 0 || len(none.Items) != 0 {
		t.Errorf("no-match search total=%d len=%d, want 0/0", none.Total, len(none.Items))
	}
	if none.Items == nil {
		t.Error("issue #601: items is JSON null rather than an empty array — the dashboard iterates it")
	}

	// ── DELETE ──
	rec = doAdminJSON(mux, token, http.MethodDelete, "/v1/admin/suppressions/"+urlEscape(strings.ToUpper(stored)), "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
	// The decisive assertion for the acceptance criterion "removing an entry
	// actually re-enables sending": the address is no longer suppressed.
	if ok, err := supp.IsSuppressed(ctx, stored); err != nil {
		t.Fatalf("IsSuppressed after DELETE: %v", err)
	} else if ok {
		t.Error("issue #601: the address is still reported suppressed after DELETE — removing it did not re-enable sending")
	}

	// Removing it again is 404, not a silent success.
	rec = doAdminJSON(mux, token, http.MethodDelete, "/v1/admin/suppressions/"+urlEscape(stored), "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("issue #601: second DELETE = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestAdminSuppressionsRejectsBadInput pins the two 400s the spec calls for: a
// malformed address and a reason outside the enum.
func TestAdminSuppressionsRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	testdb.Reset(t, pool, "suppressed_emails")
	_, userID := bundleFixture(t, ctx, pool)
	mux, token := adminSuppressionsMux(t, pool, userID, true)

	for _, tc := range []struct{ name, body string }{
		{"not an email", `{"email":"not-an-email","reason":"manual"}`},
		{"empty email", `{"email":"","reason":"manual"}`},
		{"unknown reason", `{"email":"ok@example.test","reason":"because-i-said-so"}`},
		{"malformed json", `{"email":`},
	} {
		rec := doAdminJSON(mux, token, http.MethodPost, "/v1/admin/suppressions", tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("issue #601: POST %s = %d, want 400 (body %s)", tc.name, rec.Code, rec.Body.String())
		}
	}

	// Nothing above was written.
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM suppressed_emails`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("issue #601: a rejected POST wrote %d rows, want 0", count)
	}
}

// doAdminJSON performs an admin request with the session cookie and returns the
// recorder.
func doAdminJSON(mux *http.ServeMux, token, method, path, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

// urlEscape percent-encodes a query value / path segment (an address contains
// "@", which the DELETE route's path must carry encoded).
func urlEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "@", "%40"), " ", "%20")
}
