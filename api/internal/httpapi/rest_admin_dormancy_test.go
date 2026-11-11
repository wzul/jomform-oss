// Card t_94e5cca3: GET /v1/admin/dormancy — the phase-1 dormancy report.
//
// Integration against a real Postgres + Valkey; skipped without
// JOMFORM_TEST_DATABASE_URL / JOMFORM_TEST_VALKEY_URL.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// dormancyFixture wires RESTHandlers with the dormancy store and returns the
// handlers plus a session cookie for userID.
func dormancyFixture(t *testing.T, pool *pgxpool.Pool, userID string, superAdmin bool) (*RESTHandlers, string) {
	t.Helper()
	valkeyURL := os.Getenv("JOMFORM_TEST_VALKEY_URL")
	if valkeyURL == "" {
		t.Skip("JOMFORM_TEST_VALKEY_URL not set; skipping dormancy integration test")
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
		Accounts:   dbstore.NewAccountStore(pool),
		Users:      dbstore.NewUserStore(pool),
		Sessions:   sess,
		Dormancy:   dbstore.NewDormancyStore(pool),
		SuperAdmin: func(string) bool { return superAdmin },
	}
	return h, token
}

// TestAdminDormancyReportShowsACandidateAndAnExcludedLiveFlow is the end-to-end
// version of the card's two headline rows: an account that is simply dormant (a
// candidate) and one that is dormant while buyers are still submitting (EXCLUDED,
// with the reason). It also pins the read_only promise in the payload.
func TestAdminDormancyReportShowsACandidateAndAnExcludedLiveFlow(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	_, adminUser := bundleFixture(t, ctx, pool)

	// The super-admin's own account is an ordinary, freshly created account: it
	// will show up as active. That is correct — it is not the system account.

	// A dormant candidate: created 400 days ago, nothing since.
	var candAcc string
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (owner_user_id, name, slug, created_at)
		SELECT u.id,'cand','it-dorm-cand', now() - interval '400 days'
		FROM users u WHERE u.id = $1 RETURNING id`, adminUser).Scan(&candAcc); err != nil {
		t.Fatalf("seed candidate: %v", err)
	}

	// A dormant merchant with a live buyer flow: a submission 3 days ago.
	var liveAcc string
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (owner_user_id, name, slug, created_at)
		SELECT u.id,'live','it-dorm-live', now() - interval '400 days'
		FROM users u WHERE u.id = $1 RETURNING id`, adminUser).Scan(&liveAcc); err != nil {
		t.Fatalf("seed live-flow account: %v", err)
	}
	var formID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO forms (account_id, slug, title, status)
		VALUES ($1::uuid,'it-dorm-live-form','f','published') RETURNING id`, liveAcc).Scan(&formID); err != nil {
		t.Fatalf("seed form: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (form_id, account_id, created_at)
		VALUES ($1::uuid,$2::uuid, now() - interval '3 days')`, formID, liveAcc); err != nil {
		t.Fatalf("seed submission: %v", err)
	}

	h, token := dormancyFixture(t, pool, adminUser, true)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/v1/admin/dormancy?limit=500", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	var body struct {
		ReadOnly bool   `json:"read_only"`
		Policy   string `json:"policy"`
		Rows     []struct {
			AccountID        string   `json:"account_id"`
			Bucket           string   `json:"bucket"`
			Excluded         bool     `json:"excluded"`
			ExclusionReasons []string `json:"exclusion_reasons"`
			Submissions90d   int64    `json:"submissions_90d"`
			FormsPublished   int64    `json:"forms_published"`
			NudgeEligible    bool     `json:"nudge_eligible"`
			WorkspaceSlug    string   `json:"workspace_slug"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad json: %v (%s)", err, rec.Body.String())
	}
	if !body.ReadOnly {
		t.Error("the payload does not declare read_only — an agent could mistake the report for an instruction")
	}
	if !strings.Contains(body.Policy, "No account is deleted") {
		t.Errorf("policy sentence = %q", body.Policy)
	}

	var cand, live bool
	for _, r := range body.Rows {
		switch r.AccountID {
		case candAcc:
			cand = true
			if r.Excluded || r.Bucket != "365" {
				t.Errorf("the plain dormant account is bucket=%q excluded=%v, want 365/false", r.Bucket, r.Excluded)
			}
			if !r.NudgeEligible {
				t.Error("the plain 400-day account is not nudge-eligible")
			}
		case liveAcc:
			live = true
			if !r.Excluded || r.Bucket != "excluded" {
				t.Errorf("the account with a 3-day-old submission is bucket=%q excluded=%v, want excluded/true", r.Bucket, r.Excluded)
			}
			if r.Submissions90d != 1 || r.FormsPublished != 1 {
				t.Errorf("submissions_90d=%d forms_published=%d, want 1/1", r.Submissions90d, r.FormsPublished)
			}
			found := false
			for _, why := range r.ExclusionReasons {
				if why == "recent_submissions" {
					found = true
				}
			}
			if !found {
				t.Errorf("reasons = %v, want recent_submissions", r.ExclusionReasons)
			}
			if r.NudgeEligible {
				t.Error("an excluded account is nudge-eligible")
			}
		}
	}
	if !cand || !live {
		t.Fatalf("the report did not return both fixture accounts (cand=%v live=%v) — it returned %d rows",
			cand, live, len(body.Rows))
	}
}

// TestAdminDormancyReportIsSuperAdminOnly pins the gate.
func TestAdminDormancyReportIsSuperAdminOnly(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	_, userID := bundleFixture(t, ctx, pool)

	h, token := dormancyFixture(t, pool, userID, false)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest("GET", "/v1/admin/dormancy", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-super-admin: got %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestAdminDormancyCSVExportIsOneLinePerAccount pins the operator export the card
// asks for ("a JSON/CSV line per account").
func TestAdminDormancyCSVExportIsOneLinePerAccount(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	_, adminUser := bundleFixture(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (owner_user_id, name, slug, created_at)
		SELECT u.id,'csv','it-dorm-csv', now() - interval '500 days'
		FROM users u WHERE u.id = $1`, adminUser); err != nil {
		t.Fatalf("seed: %v", err)
	}

	h, token := dormancyFixture(t, pool, adminUser, true)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest("GET", "/v1/admin/dormancy?format=csv", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("content-type = %q, want text/csv", ct)
	}
	lines := strings.Split(strings.TrimRight(rec.Body.String(), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("CSV has %d line(s), want a header plus at least one account:\n%s", len(lines), rec.Body.String())
	}
	if !strings.HasPrefix(lines[0], "account_id,workspace_slug,owner_email") {
		t.Errorf("header = %q", lines[0])
	}
	// The 500-day account is the most dormant row, so it is the first data line.
	// (Workspace slug is empty here: these fixtures are accounts with no workspace,
	// which is what `bundleFixture` creates.)
	if !strings.Contains(lines[1], ",500,365,false,") {
		t.Errorf("first data line is not the seeded 500-day account: %q", lines[1])
	}
	// One CSV line per account, plus the header: the report returned len(lines)-1 rows.
	var jsonBody struct {
		Rows []struct {
			AccountID string `json:"account_id"`
		} `json:"rows"`
	}
	reqJSON := httptest.NewRequest("GET", "/v1/admin/dormancy", nil)
	reqJSON.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	recJSON := httptest.NewRecorder()
	mux.ServeHTTP(recJSON, reqJSON)
	if err := json.Unmarshal(recJSON.Body.Bytes(), &jsonBody); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if want := len(jsonBody.Rows) + 1; len(lines) != want {
		t.Errorf("CSV has %d lines; the JSON report has %d rows, so one line per account means %d",
			len(lines), len(jsonBody.Rows), want)
	}

	// A format the endpoint does not render is refused rather than silently
	// answered with JSON.
	req2 := httptest.NewRequest("GET", "/v1/admin/dormancy?format=xml", nil)
	req2.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("format=xml: got %d, want 400", rec2.Code)
	}
}
