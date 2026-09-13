// Package csvguard neutralises spreadsheet formula injection in exported CSV.
//
// Issue #526: sales CSV export (REST GET /v1/rest/sales/export.csv and MCP
// export_sales_csv) writes buyer-supplied text — the order's client.full_name,
// client.email and table_number — straight into cells. encoding/csv performs
// RFC 4180 quoting ONLY: it wraps a field containing a comma/quote/newline in
// double quotes but never neutralises a leading formula character, and Excel,
// Google Sheets and LibreOffice all still EVALUATE a double-quoted field. So a
// buyer could submit a name beginning with `=` (e.g.
// `=HYPERLINK("http://evil.example/x","Klik")`) and have it execute in the
// merchant's spreadsheet when the export is opened.
//
// This is the ONE shared rule for that class (mirroring internal/urlguard):
// every caller-supplied text cell goes through Cell before being handed to
// encoding/csv. Do not hand-roll a second per-call-site copy.
//
// The neutralisation prefixes a single ASCII apostrophe when the value's first
// character could start a formula (`=` `+` `-` `@`) or is a control character
// Excel treats as a formula lead-in (tab, CR, LF). The apostrophe is the
// conventional spreadsheet "treat as literal text" marker: Excel hides it in
// the cell and shows the original text, while the raw file stays unambiguous.
// Server-generated and numeric columns (ref_code, status, timestamps, money
// formatted with %.2f) are NOT passed through here — they cannot begin with a
// formula character and quoting them would corrupt the file for consumers.
package csvguard

import "strings"

// formulaLeads are the leading characters that a spreadsheet may interpret as
// the start of a formula or a formula continuation.
//
//   - '='  a formula
//   - '+'  a formula (Excel accepts +SUM(...) as well as =SUM(...))
//   - '-'  a formula (and a negative number, which is fine to quote — a leading
//     apostrophe on "-2+3" is the safe reading; a real number like "-2.50"
//     is unaffected because it is generated numerically, not passed here)
//   - '@'  a formula/function lead-in
//   - '\t', '\r', '\n' control characters some spreadsheet versions treat as
//     formula lead-ins, and which also break naive CSV readers
var formulaLeads = []byte{'=', '+', '-', '@', '\t', '\r', '\n'}

// Cell returns v neutralised for safe inclusion in a CSV cell. A value that
// cannot start a formula is returned byte-identical (so ordinary names, emails
// and table numbers are untouched). A value whose first byte is a formula lead
// is prefixed with a single apostrophe.
//
// The caller still writes the result through encoding/csv, which handles RFC
// 4180 quoting for commas, quotes and newlines. This function only removes the
// formula semantics; the two concerns are deliberately separate.
func Cell(v string) string {
	if v == "" {
		return v
	}
	for _, lead := range formulaLeads {
		if v[0] == lead {
			return "'" + v
		}
	}
	return v
}

// CellAll neutralises every value in a record. Use it for a whole row when
// every column is caller-supplied; for export rows that mix server-generated
// and caller-supplied columns, call Cell on the caller-supplied fields only.
func CellAll(record []string) []string {
	out := make([]string, len(record))
	for i, v := range record {
		out[i] = Cell(v)
	}
	return out
}

// HasFormulaLead reports whether v would be neutralised by Cell. Exported for
// tests and for callers that need to assert a value was attacker-shaped.
func HasFormulaLead(v string) bool {
	if v == "" {
		return false
	}
	return strings.IndexByte(string(formulaLeads), v[0]) >= 0
}
