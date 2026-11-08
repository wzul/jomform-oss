// The Affiliates tab (Settings → Affiliates) reads code / name / commission_pct
// / referral_url out of GET /v1/workspaces/{id}/affiliates. The handler used to
// marshal the dbstore struct straight out: PascalCase keys and no referral_url,
// so every row rendered as an empty "%" and the referral link — the whole point
// of the tab — was never shown. This pins the dashboard's shape.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL) + Valkey
// (JOMFORM_TEST_VALKEY_URL); skipped when either is unset.
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListAffiliatesReturnsDashboardShape(t *testing.T) {
	st := affiliateTenancyFixture(t)

	mux := http.NewServeMux()
	st.h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/workspaces/"+st.wsB.ID+"/affiliates", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: st.token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET affiliates: status %d, body %s", rec.Code, rec.Body.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
	}
	var row map[string]any
	for _, r := range rows {
		if r["code"] == "BBB" {
			row = r
			break
		}
	}
	if row == nil {
		t.Fatalf("affiliate BBB missing from %s — snake_case keys are what the dashboard reads", rec.Body.String())
	}

	// The row must carry what settings/page.tsx renders: code, name, commission
	// and the referral link.
	if got, _ := row["name"].(string); got != "Tenant B affiliate" {
		t.Errorf("name = %q, want %q", got, "Tenant B affiliate")
	}
	if got, _ := row["commission_pct"].(float64); got != 10 {
		t.Errorf("commission_pct = %v, want 10", row["commission_pct"])
	}
	wantURL := "https://" + st.wsB.Slug + ".jomform.com/?aff=BBB"
	if got, _ := row["referral_url"].(string); got != wantURL {
		t.Errorf("referral_url = %q, want %q — without it the tab cannot show the link", got, wantURL)
	}
	// The old shape: PascalCase keys must be gone, not merely joined.
	for _, k := range []string{"Code", "Name", "CommissionPct", "WorkspaceID"} {
		if _, ok := row[k]; ok {
			t.Errorf("response still carries the raw dbstore key %q (body %s)", k, rec.Body.String())
		}
	}
}
