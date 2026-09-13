package httpapi

// Issue #526: CSV formula injection. The sales CSV export (REST
// GET /v1/rest/sales/export.csv) wrote buyer-controlled text — the order's
// client.full_name, client.email and table_number — straight into cells.
// encoding/csv does RFC 4180 quoting ONLY and Excel/Sheets still evaluate a
// double-quoted field, so a buyer submitting `=HYPERLINK(...)` through the
// PUBLIC order route (no auth) had it execute in the merchant's spreadsheet.
//
// This is a real end-to-end guard: it submits the malicious order through the
// same public handler a buyer hits, then reads the export produced by the real
// exportSalesCSV handler and asserts no cell carries a live formula lead.
//
// It BITES: reverting the csvguard.Cell() wiring in rest_export.go makes it
// fail (the formula survives into the file). A unit test over the guard alone
// cannot catch a wiring revert, which is why this test drives the handler.
//
// Integration test; skipped unless JOMFORM_TEST_DATABASE_URL is set.

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/csvguard"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestIssue526ExportNeutralisesBuyerFormula is the regression guard.
func TestIssue526ExportNeutralisesBuyerFormula(t *testing.T) {
	h, workspaceSlug, formSlug, prodID := tableNumberFixture(t)
	host := workspaceSlug + ".jomform.com"
	path := "/" + formSlug + "/order"

	// Attacker-shaped buyer input, all three vectors. Sent through the PUBLIC
	// order route exactly as a buyer would.
	evilName := `=HYPERLINK("http://evil.example/x","Klik untuk resit")`
	evilEmail := "+1+1@evil.example"
	evilTable := "-2+3"

	body := `{"client":{"full_name":` + jsonString(evilName) +
		`,"email":` + jsonString(evilEmail) + `},` +
		`"items":[{"product_id":"` + prodID + `","quantity":1}],` +
		`"payment_method":"bank_direct","table_number":` + jsonString(evilTable) + `}`

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	sale, err := h.Sales.ByRefCode(context.Background(), resp.RefCode)
	if err != nil {
		t.Fatalf("fetch sale: %v", err)
	}

	// The malicious values really did persist on the sale (so the export has
	// something dangerous to neutralise — otherwise this test would pass
	// vacuously).
	if sale.Client.FullName != evilName {
		t.Fatalf("sale buyer name = %q, want the attacker value stored verbatim", sale.Client.FullName)
	}
	if sale.TableNumber != evilTable {
		t.Fatalf("sale table_number = %q, want %q", sale.TableNumber, evilTable)
	}

	// Now export through the REAL handler.
	rest := &RESTHandlers{Sales: h.Sales}
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/v1/rest/sales/export.csv", nil)
	rest.exportSalesCSV(rec2, req2, &AccountCtx{Account: &dbstore.Account{ID: sale.AccountID, Slug: "it"}})

	rows, err := csv.NewReader(strings.NewReader(rec2.Body.String())).ReadAll()
	if err != nil || len(rows) < 2 {
		t.Fatalf("CSV export unreadable (%d rows, err %v):\n%s", len(rows), err, rec2.Body.String())
	}
	header := rows[0]
	// Find our sale's row (the fixture creates other sales in the same account).
	var row []string
	for _, r := range rows[1:] {
		for i, name := range header {
			if name == "ref_code" && i < len(r) && r[i] == sale.RefCode {
				row = r
			}
		}
	}
	if row == nil {
		t.Fatalf("ref_code %s not found in the export:\n%s", sale.RefCode, rec2.Body.String())
	}

	// THE ASSERTION: no exported cell may begin with a live formula lead.
	for i, cell := range row {
		if csvguard.HasFormulaLead(cell) {
			t.Fatalf("issue #526: exported column %q begins with a live formula lead: %q\n"+
				"(the buyer's %s is executing in the merchant's spreadsheet)\nfull row: %q",
				header[i], cell, header[i], row)
		}
	}

	// And the buyer text must still be readable (neutralised, not destroyed).
	idx := func(name string) int {
		for i, n := range header {
			if n == name {
				return i
			}
		}
		return -1
	}
	if i := idx("buyer_name"); i < 0 || !strings.Contains(row[i], "evil.example") {
		t.Fatalf("buyer_name lost the text: %q", row)
	}
	// Server-generated columns must NOT be altered by the guard.
	if i := idx("total_rm"); i >= 0 && row[i] != "8.00" {
		t.Fatalf("total_rm was altered by the guard: %q (want 8.00)", row[i])
	}
}

// jsonString encodes s as a JSON string literal.
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
