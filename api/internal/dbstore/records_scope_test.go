package dbstore

import "testing"

// The ONE decision: "which table(s) does a sales read cover?".
//
// It replaced a rule keyed on a single form id (formID == "" → false), which
// could not answer the question an unfiltered read asks — so every unfiltered
// read served sales only and an account's response entries were invisible on
// the list, in the CSV export and to agents. These are the pure cases; the
// database-backed proof that the readers use it lives in
// internal/httpapi/records_mixed_test.go.
func TestResolveRecordScope(t *testing.T) {
	payment := &Form{ID: "pay-1", Config: map[string]any{"mode": "payment"}}
	legacy := &Form{ID: "legacy-1"} // pre-#171: no mode key at all
	response := &Form{ID: "resp-1", Config: map[string]any{"mode": "response"}}
	response2 := &Form{ID: "resp-2", Config: map[string]any{"mode": "response"}}

	cases := []struct {
		name        string
		forms       []*Form
		formID      string
		sales       bool
		subs        bool
		only        bool
		mixed       bool
		responseIDs []string
	}{
		{
			name:  "no forms at all: sales only (the historical answer)",
			forms: nil, sales: true,
		},
		{
			name:  "payment forms only: sales only, never mixed",
			forms: []*Form{payment, legacy}, sales: true,
		},
		{
			// The reported defect: this case used to answer "sales only", so
			// the response entries of the account never appeared in a list
			// without a form filter.
			name:  "an unfiltered read over a scope with a response form spans BOTH tables",
			forms: []*Form{payment, response}, sales: true, subs: true, mixed: true,
			responseIDs: []string{"resp-1"},
		},
		{
			name:  "several response forms are all included in the unfiltered read",
			forms: []*Form{payment, response, response2}, sales: true, subs: true, mixed: true,
			responseIDs: []string{"resp-1", "resp-2"},
		},
		{
			name:  "a response form alone still spans both (sales half may be empty)",
			forms: []*Form{response}, sales: true, subs: true, mixed: true,
			responseIDs: []string{"resp-1"},
		},
		{
			name:  "form filter on a payment form: sales only",
			forms: []*Form{payment, response}, formID: "pay-1", sales: true,
		},
		{
			name:  "form filter on a legacy form (no mode): sales only, the pre-#171 default",
			forms: []*Form{payment, legacy, response}, formID: "legacy-1", sales: true,
		},
		{
			// Issue #604 behaviour, preserved: a single response form's deep
			// link reads submissions only.
			name:  "form filter on a response form: submissions only",
			forms: []*Form{payment, response}, formID: "resp-1", subs: true, only: true,
			responseIDs: []string{"resp-1"},
		},
		{
			// A filter naming an unknown/foreign form must not widen the read
			// to another form's entries.
			name:  "form filter on an unknown id: sales only, scoped to that id",
			forms: []*Form{payment, response}, formID: "no-such-form", sales: true,
		},
		{
			name:  "a nil form in the slice is skipped, not dereferenced",
			forms: []*Form{nil, response}, sales: true, subs: true, mixed: true,
			responseIDs: []string{"resp-1"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveRecordScope(tc.forms, tc.formID)
			if got.IncludeSales != tc.sales {
				t.Errorf("IncludeSales = %v, want %v", got.IncludeSales, tc.sales)
			}
			if got.IncludeSubs != tc.subs {
				t.Errorf("IncludeSubs = %v, want %v", got.IncludeSubs, tc.subs)
			}
			if got.OnlyResponses != tc.only {
				t.Errorf("OnlyResponses = %v, want %v", got.OnlyResponses, tc.only)
			}
			if got.Mixed != tc.mixed {
				t.Errorf("Mixed = %v, want %v", got.Mixed, tc.mixed)
			}
			if got.FilterFormID != tc.formID {
				t.Errorf("FilterFormID = %q, want %q", got.FilterFormID, tc.formID)
			}
			if len(got.ResponseFormIDs) != len(tc.responseIDs) {
				t.Fatalf("ResponseFormIDs = %v, want %v", got.ResponseFormIDs, tc.responseIDs)
			}
			for i, id := range tc.responseIDs {
				if got.ResponseFormIDs[i] != id {
					t.Errorf("ResponseFormIDs[%d] = %q, want %q", i, got.ResponseFormIDs[i], id)
				}
			}
			// A response-only scope must never claim to include sales: the
			// readers use exactly one branch.
			if got.OnlyResponses && got.IncludeSales {
				t.Error("a response-only scope also reports IncludeSales")
			}
			if got.Mixed && (!got.IncludeSales || !got.IncludeSubs) {
				t.Error("Mixed is true but the scope does not include both tables")
			}
		})
	}
}

// The decision must not depend on the form list ORDER: the response-form set is
// built by scanning it, and a set is unordered.
func TestResolveRecordScopeIsOrderIndependent(t *testing.T) {
	a := &Form{ID: "resp-a", Config: map[string]any{"mode": "response"}}
	b := &Form{ID: "resp-b", Config: map[string]any{"mode": "response"}}
	p := &Form{ID: "pay", Config: map[string]any{"mode": "payment"}}

	first := ResolveRecordScope([]*Form{a, p, b}, "")
	second := ResolveRecordScope([]*Form{b, a, p}, "")
	if first.Mixed != second.Mixed || first.IncludeSubs != second.IncludeSubs || first.IncludeSales != second.IncludeSales {
		t.Fatalf("scope flags depend on form order: %+v vs %+v", first, second)
	}
	if len(first.ResponseFormIDs) != 2 || len(second.ResponseFormIDs) != 2 {
		t.Fatalf("response form ids: %v vs %v", first.ResponseFormIDs, second.ResponseFormIDs)
	}
}

// A status filter is a payment-status question (a submission has no status
// column). The merged read must therefore HIDE the responses half rather than
// return zero rows for the whole list — "0 records" would read as "nothing was
// ever recorded", the exact misreading issue #604 fixed for the per-form view.
func TestRecordFilterProjectsStatusOntoBothStores(t *testing.T) {
	f := RecordFilter{FormID: "f1", Status: "paid", DateFrom: "2026-01-01", DateTo: "2026-01-31", Q: "ali"}

	if sf := f.SaleFilter(); sf.Status != "paid" || sf.FormID != "f1" || sf.Q != "ali" ||
		sf.DateFrom != "2026-01-01" || sf.DateTo != "2026-01-31" {
		t.Errorf("SaleFilter lost a field: %+v", sf)
	}
	// A submission filter has no status field at all — that asymmetry is the
	// reason the merged read excludes the responses half when status is set.
	sub := f.SubmissionFilter([]string{"resp-1"})
	if len(sub.FormIDs) != 1 || sub.FormIDs[0] != "resp-1" {
		t.Errorf("SubmissionFilter form set = %v, want [resp-1]", sub.FormIDs)
	}
	if sub.FormID != "" {
		t.Errorf("SubmissionFilter.FormID = %q, want empty (the set form is used)", sub.FormID)
	}
	if sub.Q != "ali" || sub.DateFrom != "2026-01-01" || sub.DateTo != "2026-01-31" {
		t.Errorf("SubmissionFilter lost a filter the list applies: %+v", sub)
	}
}

// SubmissionFilter.formClause must never degrade to "match every row": a read
// without a form (or form set) has nothing to scope to and must match nothing.
func TestSubmissionFormClauseNeverMatchesEverything(t *testing.T) {
	clause, args := SubmissionFilter{}.formClause("form_id", 2)
	if clause != "false" {
		t.Errorf("empty filter clause = %q, want a false predicate (never the whole table)", clause)
	}
	if len(args) != 0 {
		t.Errorf("empty filter clause produced args %v", args)
	}

	clause, args = SubmissionFilter{FormIDs: []string{"a", "b"}}.formClause("form_id", 2)
	if clause != "form_id = ANY($2)" {
		t.Errorf("form-set clause = %q, want the ANY() predicate", clause)
	}
	if len(args) != 1 {
		t.Fatalf("form-set clause args = %v", args)
	}

	clause, args = SubmissionFilter{FormID: "a"}.formClause("form_id", 3)
	if clause != "form_id = $3" {
		t.Errorf("single-form clause = %q", clause)
	}
	if len(args) != 1 {
		t.Fatalf("single-form clause args = %v", args)
	}
}
