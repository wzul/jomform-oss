package httpapi

// Regression: GET /v1/rest/domains (the dashboard's custom-domain list) went
// through dbstore.DomainStore.ListDomains, which scanned the nullable
// account_domains columns into plain strings. A domain claimed through the
// production write path (POST /v1/rest/domains → AddDomain, which inserts only
// account_id/domain/provider) has all five NULL, so the scan failed and the
// handler answered an error instead of the merchant's list — and
// dashboardBundle's best-effort section swallowed it into an absent `domains`
// key.
//
// This drives the REAL REST mux (API-key auth, the same auth path MCP clients
// and the dashboard's API use) against a real Postgres.
//
// Integration test: skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// restDomainGet drives an authed GET on the real mux and returns the status +
// decoded body. The body is decoded loosely so a non-JSON error page is a
// visible failure rather than a silent nil.
func restDomainGet(t *testing.T, h *RESTHandlers, rawKey, path string) (int, any) {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var body any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("GET %s: body is not JSON: %v (%s)", path, err, rec.Body.String())
	}
	return rec.Code, body
}

// TestRESTDomainListReturnsClaimedRow: claim a domain through the production
// HTTP path, then read it back from the list endpoint. Before the fix this
// returned an error body (the same NULL-scan failure the MCP tool hit).
func TestRESTDomainListReturnsClaimedRow(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)

	ds := dbstore.NewDomainStore(pool)
	domain := "rest-claim-" + runKey() + ".example.test"
	if _, err := ds.AddDomain(ctx, accountID, domain, "cname"); err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	// Reproduce the reported state before trusting any green.
	assertDomainColumnsNull(t, ctx, accountID, domain)

	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	code, body := restDomainGet(t, h, raw, "/v1/rest/domains")
	if code != http.StatusOK {
		t.Fatalf("GET /v1/rest/domains = %d, want 200 (body %v)", code, body)
	}
	rows, ok := body.([]any)
	if !ok {
		t.Fatalf("GET /v1/rest/domains returned %T, want a JSON array (%v)", body, body)
	}
	if len(rows) != 1 {
		t.Fatalf("GET /v1/rest/domains returned %d rows, want 1 (%v)", len(rows), rows)
	}
	row, ok := rows[0].(map[string]any)
	if !ok {
		t.Fatalf("row = %T, want an object (%v)", rows[0], rows[0])
	}
	if row["domain"] != domain || row["provider"] != "cname" {
		t.Errorf("row = %v, want domain=%s provider=cname", row, domain)
	}
	if row["auto_cname"] != "" {
		t.Errorf("row auto_cname = %v, want an empty string", row["auto_cname"])
	}
	if row["tls_mode"] != "auto" {
		t.Errorf("row tls_mode = %v, want auto", row["tls_mode"])
	}
	if row["verified"] != false {
		t.Errorf("row verified = %v, want false", row["verified"])
	}
}

// TestDashboardBundleCarriesClaimedDomain: the same defect made the bundle's
// custom-domain section disappear — rest_dashboard.go treats a ListDomains
// error as "no domains", so the merchant saw an empty Domains tab with no
// error anywhere. The bundle must now carry the claim.
func TestDashboardBundleCarriesClaimedDomain(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)

	domain := "bundle-claim-" + runKey() + ".example.test"
	if _, err := dbstore.NewDomainStore(pool).AddDomain(ctx, accountID, domain, "cname"); err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	assertDomainColumnsNull(t, ctx, accountID, domain)

	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	code, body := bundleGet(t, h, raw, "/v1/rest/dashboard-bundle")
	if code != http.StatusOK {
		t.Fatalf("dashboard-bundle = %d, want 200", code)
	}
	domains, ok := body["domains"].([]any)
	if !ok {
		t.Fatalf("bundle domains = %T (%v), want an array carrying the claim", body["domains"], body["domains"])
	}
	if len(domains) != 1 {
		t.Fatalf("bundle carries %d domains, want 1 (%v)", len(domains), domains)
	}
	first, _ := domains[0].(map[string]any)
	if first["domain"] != domain {
		t.Errorf("bundle domain = %v, want %s", first["domain"], domain)
	}
}

// assertDomainColumnsNull fails if the fixture stopped producing the reported
// state — the whole point is that a production claim leaves the five nullable
// columns NULL.
func assertDomainColumnsNull(t *testing.T, ctx context.Context, accountID, domain string) {
	t.Helper()
	pool := bundleTestPool(t)
	var autoCNAME, tlsError, tlsCert, tlsKey, tlsEmail *string
	if err := pool.QueryRow(ctx,
		`SELECT auto_cname, tls_error, tls_cert_enc, tls_key_enc, tls_email
		   FROM account_domains WHERE account_id=$1 AND domain=$2`, accountID, domain).
		Scan(&autoCNAME, &tlsError, &tlsCert, &tlsKey, &tlsEmail); err != nil {
		t.Fatalf("read raw claim columns: %v", err)
	}
	if autoCNAME != nil || tlsError != nil || tlsCert != nil || tlsKey != nil || tlsEmail != nil {
		t.Fatalf("fixture stopped reproducing the reported state: auto_cname=%v tls_error=%v tls_cert_enc=%v tls_key_enc=%v tls_email=%v",
			autoCNAME, tlsError, tlsCert, tlsKey, tlsEmail)
	}
}
