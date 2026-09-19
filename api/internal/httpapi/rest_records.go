// The /sales read surface as ONE record set (sales + response-mode forms'
// submissions).
//
// Before this file the surface had four readers (paginated list, legacy array,
// CSV export, MCP list_sales) and each re-answered "is this a response form?"
// from a SINGLE form id. With no form filter every one of them answered "no",
// so an account's response entries were invisible on the list, missing from the
// export, absent from the dashboard's recent list and invisible to agents —
// while the per-form counter reported them correctly. Two surfaces, two answers.
//
// This file holds the REST side of the one decision (dbstore.ResolveRecordScope)
// and the row mapping for a mixed list. The mode rule itself and the merged
// query live in dbstore/records.go so REST and MCP cannot drift apart.
package httpapi

import (
	"context"
	"net/http"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// recordScope resolves the ONE decision for this request: which tables the
// sales read must read, given the caller's account scope and ?form_id=.
//
// An error reading the scope's forms is returned rather than swallowed: falling
// back to "sales only" is exactly the silent degradation that hid the response
// entries, and a caller that cannot determine the record set must not be told a
// confidently wrong answer.
func (h *RESTHandlers) recordScope(ctx context.Context, scope *scopeResult, formID string) (dbstore.RecordScope, error) {
	if h.Forms == nil {
		// No form store wired (bare-mode tests): the historical sales-only
		// answer. Never guess "mixed" without the form list to prove it.
		return dbstore.ResolveRecordScope(nil, formID), nil
	}
	forms, err := h.Forms.ListByAccounts(ctx, scope.Accounts)
	if err != nil {
		return dbstore.RecordScope{}, err
	}
	return dbstore.ResolveRecordScope(forms, formID), nil
}

// recordFilter projects the request's query parameters onto the merged read's
// filter set. Kept in one place so the list, the legacy array shape, the export
// and the dashboard bundle all filter identically (the export used to ignore
// every filter the list applied, so a filtered page exported a different data
// set).
func recordFilter(r *http.Request) dbstore.RecordFilter {
	q := r.URL.Query()
	return dbstore.RecordFilter{
		FormID:   q.Get("form_id"),
		Status:   q.Get("status"),
		DateFrom: q.Get("date_from"),
		DateTo:   q.Get("date_to"),
		Q:        q.Get("q"),
	}
}

// recordRows maps a merged record set into the sales-list row shapes: a sale
// becomes a saleRow and a response entry a restSubmissionRow, in the merge
// order the read returned (newest first).
//
// The two row structs are reused rather than unioned into a third: every
// consumer already discriminates on `sale_type`/`submission_id` (the web
// page's rowKind does), and a third shape would be one more thing to keep in
// step with the Go emitters.
func (h *RESTHandlers) recordRows(ctx context.Context, records []*dbstore.Record) []any {
	sales := make([]*dbstore.Sale, 0, len(records))
	for _, rec := range records {
		if rec.Sale != nil {
			sales = append(sales, rec.Sale)
		}
	}
	emailStatus := h.latestEmailStatuses(ctx, sales)

	out := make([]any, 0, len(records))
	for _, rec := range records {
		if rec.Submission != nil {
			out = append(out, submissionsAsRows([]*dbstore.Submission{rec.Submission})[0])
			continue
		}
		if rec.Sale == nil {
			continue
		}
		out = append(out, toSaleRow(rec.Sale, emailStatus[rec.Sale.RefCode]))
	}
	return out
}

// listRecordRows writes one page of the merged record set in the sales-list
// envelope. It is the paginated /sales read for every case the scope decision
// says spans both tables (an unfiltered list, plus any filter that keeps both
// halves).
func (h *RESTHandlers) listRecordRows(w http.ResponseWriter, r *http.Request, scope *scopeResult, sc dbstore.RecordScope, page, limit int) error {
	records, total, err := h.Sales.MergedPage(r.Context(), scope.Accounts, sc, recordFilter(r), page, limit)
	if err != nil {
		return err
	}
	respond(w, http.StatusOK, map[string]any{
		"items": h.recordRows(r.Context(), records),
		"page":  page, "limit": limit, "total": total,
		"mixed": sc.Mixed,
	})
	return nil
}
