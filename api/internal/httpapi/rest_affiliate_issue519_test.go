// Issue #519: the affiliate commission update must be scoped to the workspace
// the caller was authorised against. The handler used to authorise the {id}
// workspace in the path and then write an affiliate id taken straight from the
// request, so an admin/manage member of one workspace could change the
// commission rate of an affiliate in another workspace.
//
// This is the cross-tenant regression test: workspace A (owned by user1) has an
// affiliate; user2 is the admin of workspace B only. Setting A's affiliate
// commission through B must be refused with 404 (the same not-found the sibling
// getAffiliateSales returns — no hint that the id exists elsewhere) and must
// leave A's row untouched. Setting B's own affiliate still works.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL) + Valkey
// (JOMFORM_TEST_VALKEY_URL); skipped when either is unset.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// affiliateTenancyFixture seeds TWO independent tenants:
//
//	user1 owns workspace A + affiliate affA
//	user2 owns workspace B + affiliate affB
//
// It returns handlers, the pool, user2's session token (the caller), and both
// workspaces + affiliates.
type affiliateTenancyState struct {
	h     *RESTHandlers
	pool  *pgxpool.Pool
	token string // user2 (workspace B's owner)

	wsA   *dbstore.Workspace
	wsB   *dbstore.Workspace
	affA  *dbstore.Affiliate
	affB  *dbstore.Affiliate
	affSv *dbstore.AffiliateStore
}

func affiliateTenancyFixture(t *testing.T) affiliateTenancyState {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("JOMFORM_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping affiliate cross-tenant test")
	}
	pool := bundleTestPool(t)
	sess := authTestValkey(t) // skips when JOMFORM_TEST_VALKEY_URL unset
	ctx := context.Background()

	newUser := func() string {
		var id string
		if err := pool.QueryRow(ctx,
			`INSERT INTO users (email, password_hash) VALUES ('i519-' || md5(random()::text) || '@example.test', 'it')
			 RETURNING id`).Scan(&id); err != nil {
			t.Fatalf("create user: %v", err)
		}
		return id
	}
	user1, user2 := newUser(), newUser()

	sp := dbstore.NewWorkspaceStore(pool)
	wsA, err := sp.Create(ctx, user1, "Issue519 A", "i519-a-"+uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("create workspace A: %v", err)
	}
	wsB, err := sp.Create(ctx, user2, "Issue519 B", "i519-b-"+uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("create workspace B: %v", err)
	}

	affSv := dbstore.NewAffiliateStore(pool)
	affA := &dbstore.Affiliate{ID: uuid.NewString(), WorkspaceID: wsA.ID, Code: "AAA", Name: "Tenant A affiliate", CommissionPct: 10}
	affB := &dbstore.Affiliate{ID: uuid.NewString(), WorkspaceID: wsB.ID, Code: "BBB", Name: "Tenant B affiliate", CommissionPct: 10}
	if err := affSv.Create(ctx, affA); err != nil {
		t.Fatalf("create affiliate A: %v", err)
	}
	if err := affSv.Create(ctx, affB); err != nil {
		t.Fatalf("create affiliate B: %v", err)
	}

	h := newBundleHandlers(pool)
	h.Sessions = sess
	h.Affiliates = affSv

	// The caller is user2 — an admin of workspace B, with no access to A.
	token, err := sess.Create(ctx, user2)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return affiliateTenancyState{h: h, pool: pool, token: token, wsA: wsA, wsB: wsB, affA: affA, affB: affB, affSv: affSv}
}

// commissionOf re-reads an affiliate row from the database (not from a cached
// struct) so the assertion is on what was actually persisted.
func (st affiliateTenancyState) commissionOf(t *testing.T, id string) float64 {
	t.Helper()
	got, err := st.affSv.ByID(context.Background(), id)
	if err != nil || got == nil {
		t.Fatalf("reload affiliate %s: %v", id, err)
	}
	return got.CommissionPct
}

// putCommission issues a session-authed PUT to the affiliate commission route.
func (st affiliateTenancyState) putCommission(t *testing.T, workspaceID, affiliateID string, pct float64) (int, string) {
	t.Helper()
	mux := http.NewServeMux()
	st.h.RegisterRoutes(mux)
	body, _ := json.Marshal(map[string]float64{"commission_pct": pct})
	req := httptest.NewRequest(http.MethodPut,
		"/v1/workspaces/"+workspaceID+"/affiliates/"+affiliateID+"/commission",
		strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: st.token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// TestIssue519RESTCrossTenantCommissionRefused is the security regression test:
// the caller administers workspace B and must NOT be able to move workspace A's
// affiliate commission rate.
func TestIssue519RESTCrossTenantCommissionRefused(t *testing.T) {
	st := affiliateTenancyFixture(t)

	before := st.commissionOf(t, st.affA.ID)

	code, body := st.putCommission(t, st.wsB.ID, st.affA.ID, 99)
	if code != http.StatusNotFound {
		t.Fatalf("cross-tenant commission set = %d (%s), want 404", code, body)
	}
	// No information leak: the refusal must read exactly like an unknown id in
	// one's own workspace, not like "exists but forbidden".
	if strings.Contains(body, "bukan") || strings.Contains(body, "peranan") {
		t.Errorf("refusal body leaks a distinction between unknown and other-tenant: %s", body)
	}
	if !strings.Contains(body, "affiliate not found") {
		t.Errorf("refusal body = %s, want the not-found message", body)
	}
	if after := st.commissionOf(t, st.affA.ID); after != before {
		t.Fatalf("workspace A affiliate commission changed %v -> %v by a workspace B caller", before, after)
	}

	// A made-up id must be refused the same way (same status + body), so a
	// guesser cannot tell an existing other-tenant id from a nonexistent one.
	codeMiss, bodyMiss := st.putCommission(t, st.wsB.ID, uuid.NewString(), 99)
	if codeMiss != http.StatusNotFound || bodyMiss != body {
		t.Errorf("unknown id = %d (%s), want the same refusal as the cross-tenant id (%d %s)", codeMiss, bodyMiss, code, body)
	}
}

// TestIssue519RESTOwnWorkspaceCommissionStillWorks is the regression half: the
// happy path inside the caller's own workspace must keep working.
func TestIssue519RESTOwnWorkspaceCommissionStillWorks(t *testing.T) {
	st := affiliateTenancyFixture(t)

	code, body := st.putCommission(t, st.wsB.ID, st.affB.ID, 25)
	if code != http.StatusOK {
		t.Fatalf("own-workspace commission set = %d (%s), want 200", code, body)
	}
	if got := st.commissionOf(t, st.affB.ID); got != 25 {
		t.Fatalf("own-workspace commission = %v, want 25", got)
	}
	// And the sibling tenant's row is untouched by the happy path too.
	if got := st.commissionOf(t, st.affA.ID); got != 10 {
		t.Fatalf("workspace A commission = %v, want 10 (untouched)", got)
	}
}
