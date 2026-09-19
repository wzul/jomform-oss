package dbstore

import (
	"context"
	"strconv"
	"time"
)

// The /sales read surface returns ONE record set built from TWO tables:
// `sales` for payment forms and `submissions` for response-mode forms (issue
// #171). Which of those tables a read has to touch is a decision about the SET
// of forms in the caller's scope, not about a single form id.
//
// Before this file every reader re-answered that question itself, keyed on one
// form id, so an unfiltered read ("All") could only ever answer "sales" — the
// response entries of the scope were invisible on the list, in the CSV export,
// on the dashboard and to agents (the reported defect). There were three copies
// of the rule (httpapi, mcpsrv, and a SQL CASE in analytics.go) and each new
// reader repeated the same mistake.
//
// This file is the ONE place that decides and the ONE place that merges:
//
//	ResolveRecordScope — the pure decision, keyed on the forms in scope.
//	SaleStore.MergedCount / MergedPage / MergedAll — the merged read.
//
// Both the REST layer and the MCP layer call these, so a fourth reader cannot
// invent a fifth answer.
//
// The read lives on SaleStore because that store already owns the pool and the
// sales-side column definitions; submission rows are hydrated by a
// SubmissionStore built over the same pool, so each table's *Select constant
// and scan site stays the single definition of its column alignment (the
// migration/scan-drift guard in this package depends on that).

// RecordScope is the answer to "which tables does this read have to read?".
//
// A note on the defaults, which are load-bearing:
//   - No form filter + no response form in scope → sales only. That is the
//     pre-#171 behaviour and it must not change for the overwhelming majority
//     of accounts, which have no response form at all.
//   - No form filter + response forms in scope → BOTH tables. The unfiltered
//     list is the whole record set; hiding the response entries is the defect
//     this decision exists to fix.
//   - A form filter → that form's own mode decides, and only that form's rows
//     are returned. A payment form stays sales-only, a response form stays
//     submissions-only (issue #604, unchanged and already tested).
type RecordScope struct {
	// FilterFormID is the ?form_id= the caller supplied ("" when unfiltered).
	FilterFormID string
	// IncludeSales / IncludeSubs are the tables this read must read.
	IncludeSales bool
	IncludeSubs  bool
	// ResponseFormIDs lists the response-mode forms of the scope whose
	// submissions belong to this read. Empty when IncludeSubs is false.
	ResponseFormIDs []string
	// OnlyResponses is true when the whole read is submissions (a single
	// response-mode form was selected). Readers that render one table per
	// branch keep their existing response-only path for this case.
	OnlyResponses bool
	// Mixed is true when the read spans both tables, i.e. the caller gets a
	// combined list of sales and response entries.
	Mixed bool
}

// ResolveRecordScope is the ONE rule that decides which table(s) a sales read
// must read, given the forms in the caller's account scope and the caller's
// optional form filter.
//
// It is deliberately a pure function over the form list so it can be tested
// without a database, and so REST and MCP cannot drift: both call it.
//
// `forms` must be the non-deleted forms of the caller's account set. A caller
// that cannot resolve them (no form store wired) passes nil and gets the
// historical sales-only answer — it must never guess "mixed" from a missing
// form list.
func ResolveRecordScope(forms []*Form, formID string) RecordScope {
	if formID != "" {
		for _, f := range forms {
			if f == nil || f.ID != formID {
				continue
			}
			if IsResponseForm(f) {
				return RecordScope{
					FilterFormID:    formID,
					IncludeSubs:     true,
					ResponseFormIDs: []string{formID},
					OnlyResponses:   true,
				}
			}
			// A payment form: sales only, and never another form's entries.
			return RecordScope{FilterFormID: formID, IncludeSales: true}
		}
		// The filter names a form that is not in scope (or does not exist).
		// The historical answer is "sales, filtered by that form" — which
		// matches no rows rather than silently widening to another form's
		// entries.
		return RecordScope{FilterFormID: formID, IncludeSales: true}
	}

	sc := RecordScope{IncludeSales: true}
	for _, f := range forms {
		if f == nil || !IsResponseForm(f) {
			continue
		}
		sc.ResponseFormIDs = append(sc.ResponseFormIDs, f.ID)
	}
	if len(sc.ResponseFormIDs) > 0 {
		sc.IncludeSubs = true
		sc.Mixed = true
	}
	return sc
}

// Record is one entry of the merged read: either a sale or a response-mode
// form's submission. Exactly one of Sale/Submission is set, per Kind.
type Record struct {
	Kind       string // RecordKindSale | RecordKindSubmission
	Sale       *Sale
	Submission *Submission
}

// Record kinds, the discriminator the merged read returns and the value the CSV
// export writes into its `record_type` column.
//
// The submission kind is spelled "response" on purpose: the list already marks a
// response row with `sale_type: "response"` (rest_handlers.go submissionsAsRows),
// and the export sits directly under that list. A file whose rows said
// "submission" while the page said "response" would be two names for one concept
// on the same screen.
const (
	RecordKindSale       = "sale"
	RecordKindSubmission = "response"
)

const maxRecordPageLimit = 100

// RecordFilter is the filter set of a merged /sales read: the union of
// SaleFilter and SubmissionFilter's applicable fields.
//
//   - FormID scopes the read to one form (its mode then decides the table).
//   - Status is SALES ONLY: a submission has no status column. A non-empty
//     status therefore HIDES the submission half instead of returning zero rows
//     for the whole list — a merchant who filtered by payment status has asked
//     a question responses cannot answer, and an empty page would read as
//     "nothing was recorded" (the exact misreading #604 fixed).
//   - DateFrom/DateTo/Q apply to both halves, each with its own semantics
//     (sales: ref_code + buyer name; submissions: the recorded answer values).
type RecordFilter struct {
	FormID   string
	Status   string
	DateFrom string
	DateTo   string
	Q        string
}

// SaleFilter projects the shared filter onto the sales store's filter type, so
// the sales WHERE clause stays single-sourced in sale.go.
func (f RecordFilter) SaleFilter() SaleFilter {
	return SaleFilter{
		FormID: f.FormID, Status: f.Status,
		DateFrom: f.DateFrom, DateTo: f.DateTo, Q: f.Q,
	}
}

// SubmissionFilter projects the shared filter onto the submissions store's
// filter type. formIDs is the scope's response-form set.
func (f RecordFilter) SubmissionFilter(formIDs []string) SubmissionFilter {
	return SubmissionFilter{
		FormIDs:  formIDs,
		DateFrom: f.DateFrom, DateTo: f.DateTo, Q: f.Q,
	}
}

// mergedOrderKey keeps the merge-order rationale next to the read it describes.
//
// The order is `created_at DESC, row_id DESC`. created_at is the real merge
// key; the row-id tiebreaker is not cosmetic — Postgres `now()` is the
// *transaction* timestamp, so rows written in one transaction (bulk import,
// migration, seeded fixtures) share created_at exactly, and LIMIT/OFFSET
// without a deterministic tiebreaker can repeat or skip rows across pages.

// mergedParts builds the UNION ALL over the tables the scope selected, plus the
// arguments it needs. Each half's WHERE clause comes from that store's own
// builder (saleWhere / submissionWhere), so the filter semantics have exactly
// one definition each — two copies is how the list and the export drifted.
func mergedParts(accountIDs []string, sc RecordScope, f RecordFilter) (string, []any) {
	parts := []string{}
	args := []any{}

	if sc.IncludeSales {
		clause, sa := saleWhere(accountIDs, f.SaleFilter(), len(args)+1)
		parts = append(parts, `SELECT '`+RecordKindSale+`' AS kind, id::text AS row_id, created_at FROM sales WHERE `+clause)
		args = append(args, sa...)
	}
	// A status filter is a payment-status question; a submission cannot be in
	// one, so the responses half is hidden rather than reported as empty.
	if sc.IncludeSubs && f.Status == "" {
		clause, sa := submissionWhere(accountIDs, f.SubmissionFilter(sc.ResponseFormIDs), len(args)+1)
		parts = append(parts, `SELECT '`+RecordKindSubmission+`' AS kind, id::text AS row_id, created_at FROM submissions WHERE `+clause)
		args = append(args, sa...)
	}
	if len(parts) == 0 {
		// Nothing selected: a false predicate keeps the SQL valid and returns
		// no rows rather than leaking a whole table.
		return `SELECT '' AS kind, '' AS row_id, now() AS created_at WHERE false`, args
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " UNION ALL "
		}
		out += p
	}
	return out, args
}

// MergedCount returns how many rows the merged read matches.
func (s *SaleStore) MergedCount(ctx context.Context, accountIDs []string, sc RecordScope, f RecordFilter) (int64, error) {
	if len(accountIDs) == 0 {
		return 0, nil
	}
	parts, args := mergedParts(accountIDs, sc, f)
	var total int64
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM (`+parts+`) u`, args...).Scan(&total)
	return total, err
}

// MergedPage returns one page of the merged record set plus the total row count
// for the same filter.
//
// `sc` is the scope decision (which tables, which response forms); both halves
// come from the same decision, so the list and the export cannot disagree about
// what the record set is.
func (s *SaleStore) MergedPage(ctx context.Context, accountIDs []string, sc RecordScope, f RecordFilter, page, limit int) ([]*Record, int64, error) {
	if len(accountIDs) == 0 {
		return []*Record{}, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > maxRecordPageLimit {
		limit = maxRecordPageLimit
	}
	offset := (page - 1) * limit

	total, err := s.MergedCount(ctx, accountIDs, sc, f)
	if err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []*Record{}, 0, nil
	}

	parts, args := mergedParts(accountIDs, sc, f)
	rows, err := s.pool.Query(ctx,
		`SELECT kind, row_id, created_at FROM (`+parts+`) u`+
			` ORDER BY created_at DESC, row_id DESC LIMIT $`+strconv.Itoa(len(args)+1)+
			` OFFSET $`+strconv.Itoa(len(args)+2),
		append(append([]any{}, args...), limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	type ref struct{ kind, id string }
	pageRows := []ref{}
	for rows.Next() {
		var r ref
		var createdAt time.Time
		if err := rows.Scan(&r.kind, &r.id, &createdAt); err != nil {
			return nil, 0, err
		}
		pageRows = append(pageRows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	saleIDs := []string{}
	subIDs := []string{}
	for _, r := range pageRows {
		if r.kind == RecordKindSubmission {
			subIDs = append(subIDs, r.id)
			continue
		}
		saleIDs = append(saleIDs, r.id)
	}

	salesByID := map[string]*Sale{}
	if len(saleIDs) > 0 {
		sales, err := s.ByIDs(ctx, accountIDs, saleIDs)
		if err != nil {
			return nil, 0, err
		}
		for _, sl := range sales {
			salesByID[sl.ID] = sl
		}
	}
	subsByID := map[string]*Submission{}
	if len(subIDs) > 0 {
		subs, err := (&SubmissionStore{pool: s.pool}).ByIDs(ctx, accountIDs, subIDs)
		if err != nil {
			return nil, 0, err
		}
		for _, sub := range subs {
			subsByID[sub.ID] = sub
		}
	}

	out := make([]*Record, 0, len(pageRows))
	for _, r := range pageRows {
		if r.kind == RecordKindSubmission {
			if sub := subsByID[r.id]; sub != nil {
				out = append(out, &Record{Kind: RecordKindSubmission, Submission: sub})
			}
			continue
		}
		if sl := salesByID[r.id]; sl != nil {
			out = append(out, &Record{Kind: RecordKindSale, Sale: sl})
		}
	}
	return out, total, nil
}

// MergedAll returns every row matching the filter, paged internally, plus the
// total. It exists for the CSV export: an export that silently stops at a
// store's page cap is not "the same rows as the list" (the sales list clamps
// any limit above 200 down to 50, so an export asking for 5000 rows got 50),
// and the caller must not have to know the batch size to get a complete file.
func (s *SaleStore) MergedAll(ctx context.Context, accountIDs []string, sc RecordScope, f RecordFilter) ([]*Record, int64, error) {
	total, err := s.MergedCount(ctx, accountIDs, sc, f)
	if err != nil || total == 0 {
		return []*Record{}, total, err
	}
	out := make([]*Record, 0, total)
	for page := 1; ; page++ {
		batch, _, err := s.MergedPage(ctx, accountIDs, sc, f, page, maxRecordPageLimit)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, batch...)
		if int64(len(out)) >= total || len(batch) == 0 {
			break
		}
	}
	return out, total, nil
}
