// CSV export of account sales (session or API-key authed).
package httpapi

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
)

func (h *RESTHandlers) exportSalesCSV(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	sales, err := h.Sales.ListByAccount(r.Context(), actx.Account.ID, 5000)
	if err != nil {
		respondErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="jomform-sales-%s.csv"`, actx.Account.Slug))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{
		"ref_code", "status", "payment_method", "buyer_name", "buyer_email",
		"total_rm", "created_at", "purchase_id",
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
		})
	}
	cw.Flush()
}
