package csvguard

import (
	"encoding/csv"
	"strings"
	"testing"
)

// TestCellNeutralisesFormulaLeads is the core guard: every leading character a
// spreadsheet may evaluate must come back prefixed.
func TestCellNeutralisesFormulaLeads(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"equals function", `=HYPERLINK("http://evil.example/x","Klik")`, `'=HYPERLINK("http://evil.example/x","Klik")`},
		{"equals sum", "=1+1", "'=1+1"},
		{"plus", "+1+1@evil.example", "'+1+1@evil.example"},
		{"minus formula", "-2+3", "'-2+3"},
		{"minus cmd", "-2+3|cmd", "'-2+3|cmd"},
		{"at function", "@SUM(A1)", "'@SUM(A1)"},
		{"leading tab", "\t=1+1", "'\t=1+1"},
		{"leading cr", "\r=1+1", "'\r=1+1"},
		{"leading newline", "\n=1+1", "'\n=1+1"},
		// Values that must pass through byte-identical.
		{"ordinary name", "Ahmad bin Ali", "Ahmad bin Ali"},
		{"ordinary email", "ahmad@example.com", "ahmad@example.com"},
		{"email with plus tag", "a+b@example.com", "a+b@example.com"},
		{"numeric table", "8", "8"},
		{"empty", "", ""},
		{"unicode name", "Siti Nurhaliza 李", "Siti Nurhaliza 李"},
		{"leading space then equals is inert", " =1+1", " =1+1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Cell(tc.in); got != tc.want {
				t.Fatalf("Cell(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestCellAllNeutralisesEveryColumn covers whole-row use.
func TestCellAllNeutralisesEveryColumn(t *testing.T) {
	got := CellAll([]string{"=a", "b", "+c", "@d", ""})
	want := []string{"'=a", "b", "'+c", "'@d", ""}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("index %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestExportedCSVCarriesNoLiveFormula is the end-to-end shape of the bug: write
// a row exactly the way rest_export.go / parity_issue396.go do (encoding/csv)
// and assert no cell in the produced file begins with a formula lead once the
// caller-supplied fields have gone through Cell.
//
// This mirrors the manual proof recorded on issue #526, where the raw
// cw.Write call emitted:
//
//	"=HYPERLINK(""http://evil.example/x"",""Klik untuk resit"")",+1+1@evil.example,-2+3
//
// i.e. the formula survived RFC 4180 quoting intact.
func TestExportedCSVCarriesNoLiveFormula(t *testing.T) {
	buyerName := `=HYPERLINK("http://evil.example/x","Klik untuk resit")`
	buyerEmail := "+1+1@evil.example"
	tableNumber := "-2+3"

	var sb strings.Builder
	cw := csv.NewWriter(&sb)
	_ = cw.Write([]string{
		"ref_code", "status", "payment_method", "buyer_name", "buyer_email",
		"total_rm", "created_at", "purchase_id", "table_number",
	})
	_ = cw.Write([]string{
		"JF-TEST01", "paid", "chip",
		Cell(buyerName), Cell(buyerEmail),
		"10.00", "2026-09-13 10:00:00", "pur_test",
		Cell(tableNumber),
	})
	cw.Flush()
	out := sb.String()

	// Re-parse the emitted CSV and assert no field begins with a formula lead.
	r := csv.NewReader(strings.NewReader(out))
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("re-parsing exported CSV: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	for col, cell := range rows[1] {
		if HasFormulaLead(cell) {
			t.Fatalf("column %d (%q) in the exported CSV begins with a live formula lead: %q",
				col, rows[0][col], cell)
		}
	}

	// The original text must still be recoverable (apostrophe is the literal
	// marker, not a mutilation of the data).
	if !strings.Contains(out, "HYPERLINK") {
		t.Fatalf("buyer text was lost from the export:\n%s", out)
	}
}
