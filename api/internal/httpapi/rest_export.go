// CSV export of account sales (session or API-key authed).
package httpapi

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func (h *RESTHandlers) exportSalesCSV(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	// Optional per-form filter: /v1/rest/sales/export.csv?form_id=<uuid>.
	// Account scoping is unchanged: sales must belong to actx.Account.ID.
	var sales []*dbstore.Sale
	var err error
	if formID := r.URL.Query().Get("form_id"); formID != "" {
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
	_ = cw.Write([]string{
		"ref_code", "status", "payment_method", "buyer_name", "buyer_email",
		"total_rm", "created_at", "purchase_id", "table_number",
	})
	for _, s := range sales {
		_ = cw.Write([]string{
			s.RefCode,
			s.Status,
			s.PaymentMethod,
			s.Client.FullName,
			s.Client.Email,
			strconv.FormatFloat(float64(s.TotalSen)/100, 'f', 2, 64),
			s.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
			s.ChipPurchaseID,
			s.TableNumber,
		})
	}
	cw.Flush()
}
