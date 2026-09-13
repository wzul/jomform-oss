package csvguard

import (
	"encoding/csv"
	"strings"
	"testing"
)

// TestRestExportRowIsNeutralised_RegressionProof exercises the exact row shape
// rest_export.go writes, with Cell wired in. It is the sibling of
// TestExportedCSVCarriesNoLiveFormula and additionally asserts the two
// server-generated numeric columns are NOT quoted (so the fix cannot be
// "escape everything", which would corrupt money/date columns for consumers).
func TestRestExportRowIsNeutralised_RegressionProof(t *testing.T) {
	// A buyer submits this through the public order route (no auth):
	// full_name, email and table_number are all attacker-controlled.
	buyerName := `=HYPERLINK("http://evil.example/x","Klik untuk resit")`
	buyerEmail := "@SUM(1+9)*cmd|'/C calc'!A0"
	tableNumber := "+1+1"

	var sb strings.Builder
	cw := csv.NewWriter(&sb)
	_ = cw.Write([]string{"ref_code", "buyer_name", "buyer_email", "total_rm",
		"created_at", "table_number", "outstanding_rm"})
	_ = cw.Write([]string{
		"JF-TEST01",
		Cell(buyerName), Cell(buyerEmail),
		"10.00",               // server-generated, must stay raw
		"2026-09-13 10:00:00", // server-generated, must stay raw
		Cell(tableNumber),
		"0.00", // server-generated, must stay raw
	})
	cw.Flush()

	r := csv.NewReader(strings.NewReader(sb.String()))
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	row := rows[1]

	// 1. No cell may begin with a formula lead.
	for i, cell := range row {
		if HasFormulaLead(cell) {
			t.Fatalf("column %q still begins with a live formula lead: %q", rows[0][i], cell)
		}
	}
	// 2. Server-generated columns stay byte-identical (no blanket escaping).
	if row[3] != "10.00" {
		t.Fatalf("total_rm was altered: %q", row[3])
	}
	if row[6] != "0.00" {
		t.Fatalf("outstanding_rm was altered: %q", row[6])
	}
	// 3. The buyer's text survives (readable, just inert).
	if !strings.Contains(row[1], "evil.example") {
		t.Fatalf("buyer name text lost: %q", row[1])
	}
}
