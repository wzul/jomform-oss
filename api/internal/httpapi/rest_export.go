// CSV export of the /sales record set (session or API-key authed).
//
// The export button lives on the Sales page, directly above the list, so the
// file it produces has to BE that list. Four separate defects broke that:
//
//  1. It read ONE account (`actx.Account.ID`) while the list above it read the
//     whole workspace account SET — a multi-account workspace exported a subset
//     of what the page displayed.
//  2. It ignored every filter except form_id, so a page filtered to
//     `status=failed` exported every sale.
//  3. It asked for 5000 rows and got 50: the sales list clamps any limit above
//     200 down to 50, so a "complete" export silently stopped at the newest 50
//     rows. Latent at production's row count, wrong by construction.
//  4. It only followed the form's mode when a form_id was given, so exporting
//     "All" dropped every response entry.
//
// All four are one class of bug — a reader that answered a different question
// than the list it sits on — so they are fixed together, from the one scope
// decision (dbstore.ResolveRecordScope) and the same filter set.
package httpapi

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"github.com/wzul/jomform/api/internal/csvguard"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// exportSalesCSV writes GET /v1/rest/sales/export.csv.
//
// The same scope rule the list uses decides the columns:
//
//   - a single response-mode form selected → the answers-columns export (issue
//     #604; a response has no money, so the sales columns would all be empty);
//   - anything that includes the scope's response forms → the sales columns for
//     sale rows, extended with the union of the response forms' answer keys, one
//     row per record;
//   - sales only → the sales columns.
//
// Every filter the list applies is applied here, and the read is
// paging-complete, so the file and the page hold the same rows.
func (h *RESTHandlers) exportSalesCSV(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return
	}
	formID := r.URL.Query().Get("form_id")
	filter := recordFilter(r)

	// Issue #604: the export button lives on the same page as the list, so it
	// has to follow the same table choice — a response-mode form's entries are
	// submissions, and exporting `sales` for one produced a header-only file
	// while the page above it listed entries.
	sc, err := h.recordScope(r.Context(), scope, formID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	// Account scoping is the workspace account SET, exactly as the list uses
	// (the export used to read one account and silently omit the rest).
	records, _, err := h.Sales.MergedAll(r.Context(), scope.Accounts, sc, filter)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	if sc.OnlyResponses {
		subs := make([]*dbstore.Submission, 0, len(records))
		for _, rec := range records {
			if rec.Submission != nil {
				subs = append(subs, rec.Submission)
			}
		}
		writeResponsesCSV(w, subs, actx.Account.Slug)
		return
	}
	writeRecordsCSV(w, records, actx.Account.Slug)
}

// writeRecordsCSV writes the merged export: the sales columns, extended with
// one column per distinct response answer key when the record set contains
// response rows. The columns are the SAME for every row, so a spreadsheet can
// sort and filter the file as one table; a response row leaves the money
// columns at 0.00 and a sale row leaves the answer columns empty.
func writeRecordsCSV(w http.ResponseWriter, records []*dbstore.Record, accountSlug string) {
	keys := []string{}
	seen := map[string]bool{}
	haveSubs := false
	for _, rec := range records {
		if rec.Submission == nil {
			continue
		}
		haveSubs = true
		for k := range rec.Submission.Data {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	// One column per answer key, in a stable order, so the same form always
	// exports the same columns.
	sort.Strings(keys)

	// Issue #518: refunded / outstanding / refund_status make a partial refund
	// visible in the export instead of the sale reading as fully paid.
	//
	// Card t_8064a965: `fee` and `net` sit beside `total`, because the total a
	// buyer paid is NOT what the merchant earned — the buyer-borne processing
	// fee is inside it and is never paid out. `net` is total − fee in the sale's
	// own currency (the payment's net; a refund is its own column and is not
	// folded in here, exactly as the /sales row's "You receive" reads). Without
	// these two columns a merchant reconciling from this file counted the fee as
	// income.
	//
	// Issue #185: the money columns are in the SALE's own currency, so the
	// old `total_rm` / `refunded_rm` / `outstanding_rm` names labelled a USD sale
	// as RM, and a workspace whose history spans currencies gave a reader no way
	// to tell two 24.90 values apart. The columns are now `total`, `refunded`,
	// `outstanding` (still decimal major units), with an explicit `currency`
	// column beside them. The rename is deliberate: keeping the `_rm` suffix
	// would keep stating the wrong money, and this export is produced by the
	// merchant's own download button, not a versioned API contract.
	header := []string{"ref_code", "status", "payment_method", "buyer_name", "buyer_email",
		"currency", "total", "fee", "net", "created_at", "purchase_id", "table_number",
		"refunded", "outstanding", "refund_status"}
	if haveSubs {
		// record_type + submission_id only when the set is actually mixed: a
		// pure sales export keeps the columns its consumers already parse.
		header = append([]string{"record_type", "submission_id"}, header...)
		header = append(header, keys...)
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="jomform-sales-%s.csv"`, accountSlug))
	cw := csv.NewWriter(w)
	_ = cw.Write(header)

	for _, rec := range records {
		var row []string
		if rec.Sale != nil {
			s := rec.Sale
			row = []string{
				s.RefCode, s.Status, s.PaymentMethod,
				csvguard.Cell(s.Client.FullName), csvguard.Cell(s.Client.Email),
				s.CurrencyOrMYR(),
				strconv.FormatFloat(float64(s.TotalSen)/100, 'f', 2, 64),
				strconv.FormatFloat(float64(s.FeeSen)/100, 'f', 2, 64),
				// net is what the merchant keeps: the buyer's total minus the
				// buyer-borne fee, in integer sen before the decimal render.
				strconv.FormatFloat(float64(s.TotalSen-s.FeeSen)/100, 'f', 2, 64),
				s.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
				s.ChipPurchaseID, csvguard.Cell(s.TableNumber),
				strconv.FormatFloat(float64(s.RefundedSen)/100, 'f', 2, 64),
				strconv.FormatFloat(float64(s.OutstandingSen())/100, 'f', 2, 64),
				s.RefundStatus(),
			}
			if haveSubs {
				row = append([]string{dbstore.RecordKindSale, ""}, row...)
				for range keys {
					row = append(row, "")
				}
			}
		} else if rec.Submission != nil {
			sub := rec.Submission
			buyer, email := subBuyer(sub)
			row = []string{
				sub.ID, "response", "",
				csvguard.Cell(buyer), csvguard.Cell(email),
				"", // a response has no money and therefore no currency
				"0.00",
				// fee and net: a response has no payment, so both are zero —
				// the columns stay present so every row of the file has the
				// same shape.
				"0.00", "0.00",
				sub.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
				"", "",
				"0.00", "0.00", dbstore.RefundNone,
			}
			if haveSubs {
				row = append([]string{dbstore.RecordKindSubmission, sub.ID}, row...)
			}
			for _, k := range keys {
				row = append(row, csvguard.Cell(anyToCSVText(sub.Data[k])))
			}
		} else {
			continue
		}
		_ = cw.Write(row)
	}
	cw.Flush()
}

// subBuyer reads the buyer identity the submit handler folds into a response's
// recorded answers (public.go handleResponseSubmit), so a response row carries
// a name/email in the export exactly as it does in the list.
func subBuyer(sub *dbstore.Submission) (name, email string) {
	if sub == nil || sub.Data == nil {
		return "", ""
	}
	name, _ = sub.Data["full_name"].(string)
	email, _ = sub.Data["email"].(string)
	return name, email
}

// writeResponsesCSV emits a response-only export: one column per distinct
// answer key (issue #604). The columns differ from the sales export on purpose
// — a response has no reference code, no payment and no money, so the value is
// the recorded answers, emitted as one column each.
func writeResponsesCSV(w http.ResponseWriter, subs []*dbstore.Submission, accountSlug string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="jomform-responses-%s.csv"`, accountSlug))
	cw := csv.NewWriter(w)
	if len(subs) == 0 {
		// No rows means no answer keys to head, so emit just the identity
		// columns rather than a bare empty file.
		_ = cw.Write([]string{"submission_id", "created_at"})
		cw.Flush()
		return
	}
	keys := []string{}
	seen := map[string]bool{}
	for _, sub := range subs {
		for k := range sub.Data {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	sort.Strings(keys)

	_ = cw.Write(append([]string{"submission_id", "created_at"}, keys...))
	for _, sub := range subs {
		row := []string{
			sub.ID,
			sub.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
		}
		for _, k := range keys {
			row = append(row, csvguard.Cell(anyToCSVText(sub.Data[k])))
		}
		_ = cw.Write(row)
	}
	cw.Flush()
}

// anyToCSVText renders one recorded answer as CSV text. Non-string values (a
// number, a boolean, a repeater/list answer) are emitted as their JSON text so
// the cell is meaningful rather than a Go-formatting artifact.
func anyToCSVText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		// JSON numbers decode as float64; FormatFloat('f', -1) renders an
		// integral value without a trailing ".0" (a form's "3" must not export
		// as "3.0") and keeps a decimal as typed.
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
		return ""
	}
}
