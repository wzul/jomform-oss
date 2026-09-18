// CSV export of account sales (session or API-key authed).
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

func (h *RESTHandlers) exportSalesCSV(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	scope, ok := h.scopeOr403(w, r, actx)
	if !ok {
		return
	}
	// Optional per-form filter: /v1/rest/sales/export.csv?form_id=<uuid>.
	formID := r.URL.Query().Get("form_id")
	// Issue #604: the export button lives on the same page as the list, so it has
	// to follow the same table choice — a response-mode form's entries are
	// submissions, and exporting `sales` for one produced a header-only file
	// while the page above it listed entries.
	if _, isResponse := h.responseModeForm(r, scope, formID); isResponse {
		h.exportResponsesCSV(w, r, scope, formID, actx.Account.Slug)
		return
	}
	// Account scoping is unchanged: sales must belong to actx.Account.ID.
	var sales []*dbstore.Sale
	var err error
	if formID != "" {
		sales, err = h.Sales.ListByAccountForm(r.Context(), actx.Account.ID, formID, 5000)
	} else {
		sales, err = h.Sales.ListByAccount(r.Context(), actx.Account.ID, 5000)
	}
	if err != nil {
		respondErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="jomform-sales-%s.csv"`, actx.Account.Slug))
	cw := csv.NewWriter(w)
	// Issue #518: refunded_rm / outstanding_rm / refund_status make a partial
	// refund visible in the export instead of the sale reading as fully paid.
	_ = cw.Write([]string{
		"ref_code", "status", "payment_method", "buyer_name", "buyer_email",
		"total_rm", "created_at", "purchase_id", "table_number",
		"refunded_rm", "outstanding_rm", "refund_status",
	})
	for _, s := range sales {
		_ = cw.Write([]string{
			s.RefCode,
			s.Status,
			s.PaymentMethod,
			csvguard.Cell(s.Client.FullName),
			csvguard.Cell(s.Client.Email),
			strconv.FormatFloat(float64(s.TotalSen)/100, 'f', 2, 64),
			s.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
			s.ChipPurchaseID,
			csvguard.Cell(s.TableNumber),
			strconv.FormatFloat(float64(s.RefundedSen)/100, 'f', 2, 64),
			strconv.FormatFloat(float64(s.OutstandingSen())/100, 'f', 2, 64),
			s.RefundStatus(),
		})
	}
	cw.Flush()
}

// exportResponsesCSV writes a response-mode form's entries as CSV. The columns
// differ from the sales export on purpose: a response has no reference code, no
// payment and no money — the value is the recorded answers, so they are emitted
// as one column per distinct answer key (issue #604).
func (h *RESTHandlers) exportResponsesCSV(w http.ResponseWriter, r *http.Request, scope *scopeResult, formID, accountSlug string) {
	subs, _, err := h.Submissions.ListFilteredPage(r.Context(), scope.Accounts, dbstore.SubmissionFilter{
		FormID: formID, Q: r.URL.Query().Get("q"),
	}, 1, 100)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	if len(subs) == 0 {
		// No rows means no answer keys to head, so emit just the identity
		// columns rather than a bare empty file.
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition",
			fmt.Sprintf(`attachment; filename="jomform-responses-%s.csv"`, accountSlug))
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"submission_id", "created_at"})
		cw.Flush()
		return
	}
	// One column per answer key, in the order the API returns them, so the same
	// form always exports the same columns.
	keys := make([]string, 0, 8)
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

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="jomform-responses-%s.csv"`, accountSlug))
	cw := csv.NewWriter(w)
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
