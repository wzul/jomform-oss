package httpapi

// CLASS GUARD — the defect this PR fixes had a shape, not just a site.
//
// Five readers independently answered "is this a response form?" from a SINGLE
// form id (three Go copies plus an inline SQL CASE plus a fourth in mcpsrv), and
// every one of them answered "no" when there was no form filter. The fix is one
// shared decision (dbstore.ResolveRecordScope) keyed on the form SET.
//
// These guards exist so a FIFTH reader cannot quietly reintroduce the sixth copy.
// They are SOURCE scans, not behavioural tests, and that is deliberate: a
// behaviour test can only cover the readers that exist today, so it cannot refuse
// the next one. The trade-off is that a scan is satisfied by the SPELLING of the
// read, so every assertion below is anchored on something that is part of the
// API being used (a filter TYPE, a store VALUE, a consumed RecordScope field)
// rather than on a name a caller may choose — and every scan WALKS THE TREE
// instead of naming files, so a new file is scanned because it exists, not
// because someone remembered to add it to a list. The only lists here are
// allowlists of files that are already justified, so an unlisted new file fails
// by default. (The guard used to hold a hardcoded four-file list: a compiling
// fifth reader planted beside them shipped green. The table-choice scan then
// used to key on the receiver IDENTIFIER `Sales.`/`Submissions.` and on a
// MENTION of the shared decision: a locally-aliased store, a helper that
// returned the store, and a discarded ResolveRecordScope call all defeated it.
// See the measurement block above the table-choice scan for what each of those
// proved and how it is closed now.)
//
// Coverage limits, stated honestly: it scans Go source under api/internal for the
// enumerable shapes below, and it is a source scan, so it cannot see:
//
//   - a reader that decides the table from something other than a form mode (a
//     new column, a per-form cache, a caller-supplied bool). MEASURED: such a
//     reader passes (see TestTheTableChoiceScanDiscriminates' "coverage limit"
//     allow-cases);
//   - a mode derived through a NEW WRAPPER NAME (`if h.isResponseOnly(f)`),
//     because the scan does not chase aliases of the decision itself. MEASURED:
//     passes. TestTheModeRuleHasOneDefinition only refuses the literal
//     `== "response"` and a bare assignment, so a delegating wrapper is
//     invisible to it too — a stated limit of the pair, not a silent gap;
//   - non-Go callers, and behaviour (what the query actually reads at runtime).

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// internalGoFiles returns every non-test Go file under internal/, relative to the
// httpapi package directory. Every tree-walking guard below is driven by it.
func internalGoFiles(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..", "internal")
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info == nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		out = append(out, filepath.ToSlash(path))
		return nil
	})
	if err != nil {
		t.Fatalf("class guard could not walk internal/: %v", err)
	}
	if len(out) < 50 {
		t.Fatalf("class guard walked only %d non-test Go files under internal/ — the scan is not "+
			"looking at the tree, so it would pass vacuously", len(out))
	}
	return out
}

// relInternal renders a walk result as the path the allowlists are keyed on
// ("dbstore/form_mode.go", "httpapi/public.go"): relative to internal/, not to
// this package directory. A mismatch here silently kills every allowlist entry,
// so it is asserted by TestTheAllowlistKeysMatchWalkedPaths below.
func relInternal(path string) string {
	root := filepath.Join("..", "..", "internal")
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// readWalked reads one file the walk returned (its path is already relative to
// this package directory). It fails the test rather than returning empty source.
func readWalked(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("class guard cannot read %s: %v (the guard cannot pass vacuously)", path, err)
	}
	return string(b)
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("class guard cannot read %s: %v (the guard cannot pass vacuously)", rel, err)
	}
	return string(b)
}

// topLevelFunc splits a source file into top-level function bodies.
var topLevelFunc = regexp.MustCompile(`(?m)^func `)

// goFunctions splits src at top-level `func` declarations. The shape scan below
// is per FUNCTION, not per file, and that matters: `formID == ""` in one function
// and a form read in another are not the shape. The dbstore accessors and
// public.go each hold an empty-form-id branch and a form read in DIFFERENT
// functions and are innocent; the pre-fix reader held both in ONE function, which
// is what this refuses.
func goFunctions(src string) []string {
	idx := topLevelFunc.FindAllStringIndex(src, -1)
	if len(idx) == 0 {
		return nil
	}
	out := make([]string, 0, len(idx))
	for i, loc := range idx {
		end := len(src)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		out = append(out, src[loc[0]:end])
	}
	return out
}

// The pre-fix shape, as conjuncts inside ONE function:
//
//  1. a branch keyed on an EMPTY form id (`formID == ""`),
//  2. a lookup of that ONE form beside it (a Go accessor, or a single-form SQL
//     read), and
//  3. a mode decision or a record-set read derived from it.
//
// Together they are "decide which table to read from a single form id". Any one
// of them alone is ordinary code, which is why the guard is a conjunction: it
// refuses the shape while leaving `formID == ""` where it belongs (a store
// accessor validating its own argument) alone.
var (
	emptyFormIDBranch = regexp.MustCompile(`formID\s*==\s*""`)
	oneFormLookup     = regexp.MustCompile(`\bForms\.(Get|By|List)\w*|(?i)FROM\s+forms\b|(?i)JOIN\s+forms\b`)
	oneFormDecision   = regexp.MustCompile(`IsResponseForm\(|FormMode\(|formMode\(|FormModeResponse`)
	recordSetRead     = regexp.MustCompile(`\b(Sales|Submissions)\.(List|Merged|Count|ByID)\w*|(?i)FROM\s+(sales|submissions)\b`)
)

// TestTheShapeScanDiscriminates is the positive control for the shape scan: it
// must refuse the pre-fix reader (both the Go-accessor flavour and the
// hand-rolled-SQL flavour, in a file that is NOT on any list) and must leave
// ordinary code alone.
func TestTheShapeScanDiscriminates(t *testing.T) {
	refuses := []struct{ name, src string }{
		{"pre-fix reader, Go accessor", `func (h *RESTHandlers) oldStyle(w http.ResponseWriter, r *http.Request, sc *scopeResult) {
	if h.Forms == nil || formID == "" {
		return h.Sales.ListByAccounts(ctx, sc.Accounts, 50)
	}
	form, err := h.Forms.GetByIDInAccounts(ctx, sc.Accounts, formID)
	if err != nil || form == nil {
		return h.Sales.ListByAccounts(ctx, sc.Accounts, 50)
	}
	if dbstore.IsResponseForm(form) {
		return nil, nil
	}
	return h.Sales.ListByAccounts(ctx, sc.Accounts, 50)
}`},
		{"pre-fix reader, hand-rolled single-form SQL", `func (h *RESTHandlers) sqlStyle(ctx context.Context, formID string) {
	if formID == "" {
		q := "SELECT id FROM sales WHERE account_id = ANY($1)"
	} else {
		q := "SELECT config FROM forms WHERE id = $1"
	}
}`},
		{"reader keyed on an empty id with a literal mode compare", `func (h *RESTHandlers) literalStyle(ctx context.Context, formID string) {
	form, _ := h.Forms.GetByID(ctx, accountID, formID)
	if formID == "" || formMode(form) == "response" {
		return
	}
	_ = form
}`},
	}
	for _, c := range refuses {
		if !refusesTheShape(c.src) {
			t.Errorf("the shape scan does NOT refuse %q — the guard would ship the pre-fix shape green", c.name)
		}
	}
	allows := []struct{ name, src string }{
		{"a store accessor validating its own argument", `func (s *SaleStore) ListByAccountsFormPage(ctx context.Context, accountIDs []string, formID string, page, limit int) ([]*Sale, int64, error) {
	if len(accountIDs) == 0 || formID == "" {
		return []*Sale{}, 0, nil
	}
	rows, err := s.pool.Query(ctx, "SELECT 1 FROM sales WHERE form_id::text = $2", accountIDs, formID)
	return nil, 0, err
}`},
		{"public.go's token helper (no form read, no record read)", `func verifyFormAccessToken(key []byte, token string) (string, bool) {
	formID, expiryStr, mac := parts[0], parts[1], parts[2]
	if formID == "" {
		return "", false
	}
	return formID, true
}`},
		{"a per-form read with no empty-id branch", `func (h *RESTHandlers) perForm(ctx context.Context, formID string) {
	rows, err := h.Submissions.ListByAccountsFormPage(ctx, accs, formID, 1, 20)
	_ = rows
	_ = err
}`},
	}
	for _, c := range allows {
		if hits := shapeScanHits(c.src); hits != "" {
			t.Errorf("the shape scan refuses %q (%s) — the guard would block ordinary code", c.name, hits)
		}
	}
}

// shapeScanHits reports which conjuncts matched, or "" when the function is not
// the pre-fix shape: an empty-form-id branch AND a one-form lookup, plus at least
// one of a mode decision / a record-set read.
func shapeScanHits(fn string) string {
	if !emptyFormIDBranch.MatchString(fn) || !oneFormLookup.MatchString(fn) {
		return ""
	}
	var hits []string
	if oneFormDecision.MatchString(fn) {
		hits = append(hits, "mode decision")
	}
	if recordSetRead.MatchString(fn) {
		hits = append(hits, "record-set read")
	}
	if len(hits) == 0 {
		return ""
	}
	return `formID == "", one-form lookup, ` + strings.Join(hits, ", ")
}

// refusesTheShape is the boolean form of the scan, used by the control below.
func refusesTheShape(fn string) bool { return shapeScanHits(fn) != "" }

// ---------------------------------------------------------------------------
// The NEXT reader: "did this function pick the table from the mode?"
//
// The shape scan above encodes the PRE-FIX BUG, so it can only refuse code that
// still carries the `formID == ""` short-circuit. A reader can reintroduce the
// same defect without that literal at all: read one form (or none), ask
// IsResponseForm about it, and pick the store from the answer.
//
// Measured on main (ad03c16): a compiling fifth reader of exactly that shape,
// planted in httpapi with the current API, left EVERY test above GREEN — so the
// class guard did not yet cover the class it names.
//
// This scan encodes the INVARIANT rather than the bug, which is what makes it
// reach the next reader: the table choice for a record list belongs to
// dbstore.ResolveRecordScope, so a function that derives a form's MODE and then
// reads sales/submissions ROWS (or hands a record store to someone else) without
// USING the one decision is refused — whatever its control flow looks like.
//
// # What the first version of this scan missed (all measured, all compiling)
//
// The first strengthening (d9edb4e, PR #643) bound the row read to the receiver
// NAME and the escape to a MENTION. Three readers that reintroduce the very
// defect it names each left EVERY guard test green on that revision:
//
//  1. the store reached through a LOCAL ALIAS (`salesStore := h.Sales`) — the
//     predicate wanted the literal identifier `Sales.`/`Submissions.`, so one
//     rename defeated it and the ENTIRE internal/httpapi package stayed green
//     (go test ./internal/httpapi/ -count=1 -> ok 60.705s, rc=0, 0 FAIL);
//  2. a HELPER that returns the store — mode derivation in one function and the
//     row read in another, so no single function carried the whole shape;
//  3. a function that MENTIONS the one decision (`dbstore.ResolveRecordScope`)
//     and DISCARDS the result — the escape was a mention, not a use.
//
// All three are closed below, by binding the row read to the table's own FILTER
// TYPE (part of its API, unlike a local variable name), by treating a handed-out
// or aliased store as the table pick it is, and by requiring the decision to be
// USED — its RecordScope fields consumed, or the merged read called.
//
// # The exclusions, and what they cost
//
// There is no allowlist: the exemption is the predicate itself (a scalar Count*
// is not in it at all, and a per-form read with no mode derivation is not in it
// either), so a new file cannot be grandfathered into it. The cost of that is
// pinned by TestTheTableChoiceScanDiscriminates below, which asserts it in BOTH
// directions.
//
// # Measured on the healthy tree (so the exclusions are evidence, not taste)
//
// Numbers below are per-FUNCTION, which is how this scan is applied; the
// per-FILE reading of the same predicate is given too because it differs:
//
//	walked files        228 non-test Go files under internal/
//	top-level functions 1757 split by goFunctions
//	mode-deriving funcs 14, in 7 files (dbstore/form_mode.go, dbstore/
//	                    form_notification.go, dbstore/records.go, httpapi/
//	                    public.go, httpapi/rest_handlers.go, httpapi/
//	                    restrictions.go, mcpsrv/forms.go)
//	PRE-EXCLUSION hits  1 per-FUNCTION, 2 per-FILE
//	POST-EXCLUSION hits 0 — the guard is green on this tree
//
// The single per-function hit is dbstore.ResolveRecordScope itself, which is
// exempted by its own DECLARATION line — an exemption a CALL SITE cannot reach.
// The per-FILE count is higher (2) because a file that holds a mode derivation
// and a table read ANYWHERE is flagged at file scope even when no single
// function does: dbstore/records.go (the decision + the merged readers over the
// same table names) and httpapi/rest_handlers.go (a `formMode` read in the form
// handlers + the sales row reads elsewhere in the file). The shipped scan is
// per-FUNCTION, so neither file is reported — the per-file number is given here
// only so a later reader does not re-derive it and conclude the guard is
// vacuous.
//
// Two things this scan deliberately does NOT flag, both previously counted as
// exclusions and now simply outside the predicate:
//
//   - `<Store>.Count*` is a scalar about ONE form the caller already holds
//     (httpapi/restrictions.go entryLimitReached compares a form's entry count
//     against its cap), not a table choice for a list;
//   - a submissions read with a literal form id and NO mode derivation is the
//     per-form route, correct by design (#604). It answers "read this form's
//     entries", not "which table is this list".
//
// # The remaining coverage limit, measured rather than claimed
//
// A reader that derives the mode through a NEW WRAPPER NAME
// (`if h.isResponseOnly(f) { ... }`) still passes, because this scan does not
// chase aliases of the decision itself. That is stated in the file header and
// asserted as an ALLOW-case in the control below, so a future tightening that
// closes it will fail loudly instead of passing for the wrong reason.
var (
	// modeDerivation is the same rule TestTheModeRuleHasOneDefinition protects;
	// how the mode was derived is that guard's business, and this scan only needs
	// to know that a mode derivation happened.
	modeDerivation = regexp.MustCompile(`IsResponseForm\(|FormMode\(|formMode\(|FormModeResponse`)
	// recordRowRead is a read of sales/submissions ROWS. The first alternative
	// binds the call to the FILTER TYPE the table defines, so an aliased store is
	// caught as well as a named one; a local variable name is not part of the
	// table's API, the filter struct is. The call's arguments may span lines, but
	// only CONTINUATION lines (each starting with whitespace) are allowed through
	// — a top-level `func ` or a column-0 comment cannot be crossed, so this can
	// never match a call in one function with a filter literal in the next one.
	// Then the named store, a raw SELECT from either table, or a RecordScope the
	// function builds itself.
	//
	// `<Store>.Count*` is deliberately absent: a Count is a scalar about ONE form
	// the caller already holds (httpapi/restrictions.go entryLimitReached compares
	// a form's entry count against its cap), not a table choice for a list.
	recordRowRead = regexp.MustCompile(`(?i)\.(List|Merged|Page|ByID)\w*\s*\([^\n]*(?:\n[ 	]+[^\n]*){0,8}?\b(Sale|Submission)Filter\s*\{` +
		`|(?i)\b(Sales|Submissions)\.(List|Merged|Page|ByID)\w*` +
		`|(?i)\bFROM\s+(sales|submissions)\b` +
		`|RecordScope\s*\{`)
	// storeHandout is the other half of "pick the table": a function whose ANSWER
	// to "which store?" is a record store — returned to its caller, or bound to a
	// local the rest of the function reads through. This is the value, not the
	// receiver spelling, so a rename cannot hide it. The line-anchored tail keeps
	// it off an ordinary call through the store (`err = h.Sales.Count...`), which
	// is a read, not a choice of table.
	storeHandout = regexp.MustCompile(`(?m)\breturn\s+(?:[A-Za-z_]\w*\.)*(?:Sales|Submissions)\b\s*(?:,|$|;|\})` +
		`|\b\w+\s*:=\s*(?:[A-Za-z_]\w*\.)*(?:Sales|Submissions)\b\s*(?:$|;|//)`)
	// reachesTheOneDecision is how a reader is SUPPOSED to answer the question,
	// and it demands a USE, not a mention: consuming a RecordScope's fields, or
	// calling the merged read. A call whose result is thrown away
	// (`_ = dbstore.ResolveRecordScope(...)`) deliberately does NOT match — that
	// spelling satisfied the first predicate while the function still picked its
	// table from the mode.
	//
	// The decision's own DECLARATION is the one exemption, and it is anchored on
	// the declaration line so a call site cannot reach it. It lives in this
	// predicate, not in a separate one, so that this scan has exactly ONE escape
	// to neutralise and the in-suite control can still prove it bites in both
	// directions (see TestTheTableChoiceScanDiscriminates).
	reachesTheOneDecision = regexp.MustCompile(`(?m)^func\s+(?:\([^)]*\)\s+)?ResolveRecordScope\s*\(` +
		`|\b\w+\.(IncludeSales|IncludeSubs|OnlyResponses|ResponseFormIDs|Mixed|FilterFormID)\b` +
		`|(?i)\bMerged(Page|All|Count)\s*\(`)
)

// tableChoiceHits reports why a function picks the record table from a mode
// instead of using the one decision, or "" when it does not do so.
func tableChoiceHits(fn string) string {
	if !modeDerivation.MatchString(fn) {
		return ""
	}
	var why string
	switch {
	case recordRowRead.MatchString(fn):
		why = "reads sales/submissions rows"
	case storeHandout.MatchString(fn):
		why = "hands a sales/submissions store to its caller or to a local alias"
	default:
		return ""
	}
	if reachesTheOneDecision.MatchString(fn) {
		return ""
	}
	return "derives a form's mode, then " + why + ", without USING the one decision " +
		"(dbstore.ResolveRecordScope: consume its RecordScope fields, or call the merged read)"
}

// TestTheTableChoiceScanDiscriminates is the control for the invariant scan. It
// must refuse the readers that defeat the OLD shape scan, the three escapes that
// defeated the FIRST version of THIS scan (aliased store, a helper that returns
// the store, a mention of the decision that is discarded), and must leave the
// measured exclusions and ordinary code alone — a scan that flags clean code
// fails on a healthy tree, which is how the false positive in the mode-literal
// pattern turned main red.
func TestTheTableChoiceScanDiscriminates(t *testing.T) {
	refuses := []struct{ name, src string }{
		{"fifth reader: one form read by id, mode picks the store", `func (h *RESTHandlers) fifthReader(w http.ResponseWriter, r *http.Request, scope *scopeResult) {
	formID := r.URL.Query().Get("form_id")
	var form *dbstore.Form
	if formID != "" {
		f, err := h.Forms.GetByIDInAccounts(r.Context(), scope.Accounts, formID)
		if err != nil {
			return
		}
		form = f
	}
	if dbstore.IsResponseForm(form) {
		subs, _, err := h.Submissions.ListFilteredPage(ctx, scope.Accounts, dbstore.SubmissionFilter{FormID: formID}, 1, 50)
		_ = subs
		_ = err
		return
	}
	sales, _, err := h.Sales.ListFilteredPage(ctx, scope.Accounts, dbstore.SaleFilter{FormID: formID}, 1, 50)
	_ = sales
	_ = err
}`},
		{"reader that names the table from the mode with no form read at all", `func (h *RESTHandlers) modeList(ctx context.Context, f *dbstore.Form, accs []string) {
	if dbstore.IsResponseForm(f) {
		rows, _, _ := h.Submissions.ListFilteredPage(ctx, accs, dbstore.SubmissionFilter{}, 1, 20)
		_ = rows
		return
	}
	rows, _, _ := h.Sales.ListFilteredPage(ctx, accs, dbstore.SaleFilter{}, 1, 20)
	_ = rows
}`},
		{"hand-rolled SQL picked from the mode", `func (h *RESTHandlers) sqlMode(ctx context.Context, f *dbstore.Form) string {
	if formMode(f) == dbstore.FormModeResponse {
		return "SELECT id FROM submissions WHERE account_id = ANY($1)"
	}
	return "SELECT id FROM sales WHERE account_id = ANY($1)"
}`},
		{"a reader that builds its own RecordScope from the mode", `func (h *RESTHandlers) ownScope(f *dbstore.Form) dbstore.RecordScope {
	if IsResponseForm(f) {
		return dbstore.RecordScope{IncludeSubs: true, OnlyResponses: true}
	}
	rows, _, _ := h.Sales.ListFilteredPage(ctx, accs, dbstore.SaleFilter{}, 1, 20)
	_ = rows
	return dbstore.RecordScope{}
}`},
	}
	for _, c := range refuses {
		if tableChoiceHits(c.src) == "" {
			t.Errorf("the invariant scan does NOT refuse %q — a fifth reader of that shape would ship green", c.name)
		}
	}

	// The three escapes the FIRST version of this scan let through. Each is
	// asserted through the SAME per-function walk the guard performs, because two
	// of them only look like the defect once the function is split out (the
	// alias in one function, the row read in another).
	escapes := []struct{ name, src string }{
		{"P1 escape: the store reached through a LOCAL ALIAS", `func (h *RESTHandlers) aliasedReader(w http.ResponseWriter, r *http.Request, f *dbstore.Form, formID string) {
	salesStore := h.Sales
	subsStore := h.Submissions
	if dbstore.IsResponseForm(f) {
		rows, _, _ := subsStore.ListFilteredPage(ctx, accs, dbstore.SubmissionFilter{FormID: formID}, 1, 20)
		_ = rows
		return
	}
	rows, _, _ := salesStore.ListFilteredPage(ctx, accs, dbstore.SaleFilter{FormID: formID}, 1, 20)
	_ = rows
}`},
		{"P4 escape: a HELPER that answers with the store, the read in another function", `func (h *RESTHandlers) qaPicker(f *dbstore.Form) any {
	if dbstore.IsResponseForm(f) {
		return h.Submissions
	}
	return h.Sales
}

func (h *RESTHandlers) qaRead(ctx context.Context, f *dbstore.Form, accs []string) int {
	switch st := h.qaPicker(f).(type) {
	case *dbstore.SubmissionStore:
		rows, _, _ := st.ListFilteredPage(ctx, accs, dbstore.SubmissionFilter{}, 1, 20)
		return len(rows)
	}
	rows, _, _ := h.Sales.ListFilteredPage(ctx, accs, dbstore.SaleFilter{}, 1, 20)
	return len(rows)
}`},
		{"P6 escape: MENTIONS the one decision and DISCARDS it", `func (h *RESTHandlers) mentionsTheDecision(ctx context.Context, f *dbstore.Form, accs []string) int {
	forms, _ := h.Forms.ListByAccounts(ctx, accs)
	_ = dbstore.ResolveRecordScope(forms, "")
	if dbstore.IsResponseForm(f) {
		rows, _, _ := h.Submissions.ListFilteredPage(ctx, accs, dbstore.SubmissionFilter{}, 1, 20)
		return len(rows)
	}
	rows, _, _ := h.Sales.ListFilteredPage(ctx, accs, dbstore.SaleFilter{}, 1, 20)
	return len(rows)
}`},
	}
	for _, c := range escapes {
		if !refusesAnyFunction(c.src) {
			t.Errorf("the invariant scan does NOT refuse %q — that reader reintroduces the defect it names "+
				"and ships green, exactly as the FIRST version of this scan did on d9edb4e", c.name)
		}
	}

	allows := []struct{ name, src string }{
		{"the entry-limit counter (Count of a form already in hand)", `func (h *PublicHandlers) entryLimitReached(ctx context.Context, f *dbstore.Form) (bool, error) {
	if f.EntryLimit == nil {
		return false, nil
	}
	if formMode(f) == dbstore.FormModeResponse {
		count, err := h.Submissions.CountByAccountsForm(ctx, []string{f.AccountID}, f.ID)
		return count > 0, err
	}
	count, err := h.Sales.CountByAccountsForm(ctx, []string{f.AccountID}, f.ID)
	return count > 0, err
}`},
		{"the per-form submissions route (#604, no mode derivation)", `func (h *RESTHandlers) perForm(ctx context.Context, formID string, accs []string) {
	subs, _, err := h.Submissions.ListFilteredPage(ctx, accs, dbstore.SubmissionFilter{FormID: formID}, 1, 20)
	_ = subs
	_ = err
}`},
		{"the one decision itself (exempt by its own DECLARATION line)", `func ResolveRecordScope(forms []*Form, formID string) RecordScope {
	if formID != "" {
		for _, f := range forms {
			if IsResponseForm(f) {
				return RecordScope{FilterFormID: formID, IncludeSubs: true, OnlyResponses: true}
			}
		}
	}
	return RecordScope{IncludeSales: true}
}`},
		{"a caller that uses the one decision (consumes its fields)", `func (h *RESTHandlers) listRecordRows(w http.ResponseWriter, r *http.Request, scope *scopeResult, sc dbstore.RecordScope) error {
	if !sc.IncludeSubs {
		return nil
	}
	records, _, err := h.Sales.MergedPage(r.Context(), scope.Accounts, sc, recordFilter(r), 1, 20)
	_ = records
	return err
}`},
		{"a reader that ALIASES the store but USES the one decision", `func (h *RESTHandlers) aliasedButCorrect(f *dbstore.Form, sc dbstore.RecordScope) (int, error) {
	salesStore := h.Sales
	_ = dbstore.IsResponseForm(f)
	if !sc.IncludeSales {
		return 0, nil
	}
	rows, _, err := salesStore.MergedPage(ctx, accs, sc, dbstore.RecordFilter{}, 1, 20)
	return len(rows), err
}`},
		{"the delegating mode wrapper itself (a name, not a decision)", `func (h *RESTHandlers) isResponseOnly(f *dbstore.Form) bool {
	return dbstore.IsResponseForm(f)
}`},
		{"STATED COVERAGE LIMIT: the mode derived through a NEW WRAPPER NAME", `func (h *RESTHandlers) p5Reader(f *dbstore.Form, accs []string) int {
	if h.isResponseOnly(f) {
		rows, _, _ := h.Submissions.ListFilteredPage(ctx, accs, dbstore.SubmissionFilter{}, 1, 20)
		return len(rows)
	}
	rows, _, _ := h.Sales.ListFilteredPage(ctx, accs, dbstore.SaleFilter{}, 1, 20)
	return len(rows)
}`},
		{"STATED COVERAGE LIMIT: the table chosen from a caller-supplied bool, not a mode", `func (h *RESTHandlers) p7Reader(accs []string, isResponse bool) int {
	if isResponse {
		rows, _, _ := h.Submissions.ListFilteredPage(ctx, accs, dbstore.SubmissionFilter{}, 1, 20)
		return len(rows)
	}
	rows, _, _ := h.Sales.ListFilteredPage(ctx, accs, dbstore.SaleFilter{}, 1, 20)
	return len(rows)
}`},
		{"a MULTI-LINE call whose filter literal is in the NEXT function (must not over-reach)", `func (h *RESTHandlers) windowA(f *dbstore.Form, st storeIface) int {
	if dbstore.IsResponseForm(f) {
		rows, _, _ := st.ListFilteredPage(
			ctx,
			accs,
			1, 20,
		)
		return len(rows)
	}
	return 0
}

func (h *RESTHandlers) windowB() {
	_ = dbstore.SubmissionFilter{}
}`},
	}
	for _, c := range allows {
		if hits := tableChoiceHits(c.src); hits != "" {
			t.Errorf("the invariant scan refuses %q (%s) — the guard would block ordinary code", c.name, hits)
		}
	}

	// The call window must still reach a genuine MULTI-LINE call: the previous
	// allow-case only proves it does not OVER-reach, and a window of zero would
	// pass that while catching nothing.
	multiLine := `func (h *RESTHandlers) multiLineReader(f *dbstore.Form, st storeIface) int {
	if dbstore.IsResponseForm(f) {
		rows, _, _ := st.ListFilteredPage(
			ctx,
			accs,
			dbstore.SubmissionFilter{FormID: "x"},
			1, 20,
		)
		return len(rows)
	}
	return 0
}`
	if tableChoiceHits(multiLine) == "" {
		t.Error("the invariant scan does NOT refuse a multi-line ListFilteredPage call — the argument window is " +
			"too narrow, so the filter-type binding only works for single-line calls")
	}
}

// refusesAnyFunction reports whether the guard's per-function walk refuses any
// single function of src — the exact predicate TestNoReaderPicksTheTableFromAMode
// applies to the tree, reused so the control cannot drift from the guard.
func refusesAnyFunction(src string) bool {
	for _, fn := range goFunctions(src) {
		if tableChoiceHits(fn) != "" {
			return true
		}
	}
	return false
}

// TestNoReaderPicksTheTableFromAMode is the CLASS GUARD for the next reader: no
// function under api/internal may derive a form's mode and then read
// sales/submissions rows, or hand out a record store, without USING
// dbstore.ResolveRecordScope.
//
// It walks the tree, so a reader in a file that does not exist yet is refused
// because its file exists, not because someone remembered to allowlist it.
func TestNoReaderPicksTheTableFromAMode(t *testing.T) {
	scanned, modeDerived := 0, 0
	for _, path := range internalGoFiles(t) {
		src := readWalked(t, path)
		for _, fn := range goFunctions(src) {
			scanned++
			if modeDerivation.MatchString(fn) {
				modeDerived++
			}
			if hits := tableChoiceHits(fn); hits != "" {
				t.Errorf("%s: a function %s. That is the shape that made an unfiltered read fall "+
					"back to sales-only — it can be reached with no `formID == \"\"` at all, which is why "+
					"the pre-fix shape scan alone does not cover it, and it survives a receiver rename, "+
					"which is why the scan binds the read to the filter type instead of the store's "+
					"identifier. Use the one decision (RESTHandlers.recordScope → "+
					"dbstore.ResolveRecordScope, then consume its fields or call the merged read).\n"+
					"offending function:\n%s", relInternal(path), hits, firstLines(fn, 16))
			}
		}
	}
	// Anti-vacuity floor: a splitter or a matcher that stopped seeing the tree
	// would otherwise report a clean scan. The measured tree holds 1757 top-level
	// functions of which 14 derive a mode; the floors are well below that so
	// ordinary growth cannot trip them, and a broken scan cannot pass them.
	if scanned < 500 {
		t.Fatalf("the table-choice scan split only %d top-level functions out of internal/ — the "+
			"scan is not looking at the tree, so it would pass vacuously", scanned)
	}
	if modeDerived < 5 {
		t.Fatalf("the table-choice scan found only %d mode-deriving functions — the mode predicate is "+
			"not matching, so no reader could ever be refused", modeDerived)
	}
}

// TestSalesReadersUseTheOneDecision fails when any function under internal/
// re-implements the table choice from a single form id. It enumerates the tree,
// so a reader in a file that does not exist yet is refused without being named
// here — the escape hatch this test used to have (a hardcoded four-file list that
// a fifth file simply was not in) is closed by construction.
func TestSalesReadersUseTheOneDecision(t *testing.T) {
	for _, path := range internalGoFiles(t) {
		src := readWalked(t, path)
		for _, fn := range goFunctions(src) {
			if hits := shapeScanHits(fn); hits != "" {
				t.Errorf("%s: a function keys a branch on an empty form id, looks up that ONE form and "+
					"then decides the table(s) from it (%s) — that is the pre-fix shape that made an "+
					"unfiltered read fall back to sales-only. The table choice belongs in "+
					"dbstore.ResolveRecordScope (keyed on the form SET), reached via RESTHandlers.recordScope "+
					"/ Server.recordScope.\noffending function:\n%s", relInternal(path), hits, firstLines(fn, 14))
			}
		}
	}
}

// firstLines keeps an error message readable.
func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = append(lines[:n], "\t…")
	}
	return strings.Join(lines, "\n")
}

// TestSalesReadEntryPointsReachTheOneDecision is the FLOOR under the scan above:
// the known sales-read entry points must still speak to the one decision, so a
// refactor that quietly drops it from one of them fails even though the shape
// scan (which looks for the pre-fix pattern) would not notice. This list is not
// the guard — TestSalesReadersUseTheOneDecision above is, and it needs no list —
// it is an assertion about these four.
func TestSalesReadEntryPointsReachTheOneDecision(t *testing.T) {
	salesReadFiles := []string{
		"rest_handlers.go",
		"rest_records.go",
		"rest_export.go",
		"rest_dashboard.go",
	}
	for _, name := range salesReadFiles {
		src := readRepoFile(t, filepath.Join("internal", "httpapi", name))
		// The readers must actually consult the one decision. rest_handlers.go
		// reaches it through h.recordScope (declared in rest_records.go); the
		// others call recordScope or the merged read directly.
		if !strings.Contains(src, "recordScope") && !strings.Contains(src, "Merged") {
			t.Errorf("%s does not reach the ONE scope decision (recordScope/Merged*) — a sales "+
				"reader that decides its own tables is how the fifth copy of the rule appears", name)
		}
	}
}

// TestMCPReaderUsesTheOneDecision is the same guard for the MCP lane, which had
// its own copy of the rule (responseModeFormID, re-parsing config["mode"] by hand
// instead of calling dbstore.FormMode). It walks the package rather than reading
// one file, so a new mcpsrv file cannot carry a copy unnoticed.
func TestMCPReaderUsesTheOneDecision(t *testing.T) {
	const mcpsrv = "../../internal/mcpsrv"
	entries, err := os.ReadDir(mcpsrv)
	if err != nil {
		t.Fatalf("class guard cannot read internal/mcpsrv: %v", err)
	}
	scanned, readPath := 0, 0
	// A file participates in the sales READ if it reaches the one decision or
	// reads the sales/submissions stores. The hand-rolled-mode check applies to
	// those files, determined from their own content rather than from a list of
	// names — so the next reader file is covered the moment it exists.
	readPathShape := regexp.MustCompile(`ResolveRecordScope|recordScope|Merged(Page|All|Count)|\b(Sales|Submissions)\.(List|Merged|Count|ByID)`)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		scanned++
		src := readWalked(t, filepath.Join(mcpsrv, e.Name()))
		if strings.Contains(src, "responseModeFormID") {
			t.Errorf("internal/mcpsrv/%s re-implements the response-form decision (responseModeFormID) — "+
				"it must call the shared dbstore.ResolveRecordScope via Server.recordScope", e.Name())
		}
		if !readPathShape.MatchString(src) {
			continue
		}
		readPath++
		// The mode RULE itself has one definition (dbstore.FormMode); a reader
		// that reads config["mode"] by hand is a second copy.
		if regexp.MustCompile(`Config\["mode"\]`).MatchString(src) {
			t.Errorf("internal/mcpsrv/%s is on the sales read path and reads config[\"mode\"] by hand — use "+
				"dbstore.FormMode/IsResponseForm, the single definition of the mode rule", e.Name())
		}
	}
	if scanned < 20 {
		t.Fatalf("class guard scanned only %d files in internal/mcpsrv — it is not looking at the package", scanned)
	}
	if readPath == 0 {
		t.Fatal("class guard found no sales-read file in internal/mcpsrv — the read-path predicate is not " +
			"matching anything, so this test would pass vacuously")
	}
	handlers := readRepoFile(t, filepath.Join("internal", "mcpsrv", "handlers.go"))
	if !strings.Contains(handlers, "dbstore.ResolveRecordScope") {
		t.Error("mcpsrv/handlers.go no longer consults dbstore.ResolveRecordScope — the MCP sales " +
			"read would drift from the REST one again")
	}
}

// modeLiteralShape is the defect shape the mode-rule guard looks for: comparing a
// form mode to the "response" literal, or writing the shared constant as a BARE
// assignment (a second definition of the rule).
//
// The second alternative must NOT match an `==` against the shared constant:
// `== FormModeResponse` literally contains `= FormModeResponse`, so a pattern of
// `=\s*FormModeResponse` flagged every caller that asks the rule exactly the way
// the guard's own message recommends. A preceding operator character is
// therefore excluded — RE2 has no lookbehind, so the character is consumed by the
// negated class instead. That narrowing is load-bearing: the pattern must stay
// discriminating in BOTH directions.
var modeLiteralShape = regexp.MustCompile(`==\s*"response"|[^=<>!&|+\-*/%]\s*=\s*FormModeResponse`)

// TestTheModeLiteralPatternDiscriminates is the case table the pattern was
// narrowed against. It is a control INSIDE the suite: it fails if the pattern is
// re-broadened (which re-introduces the false positive that made main red) or
// narrowed too far (which would stop refusing a real second definition).
func TestTheModeLiteralPatternDiscriminates(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		match bool
	}{
		{"comparing to the shared constant (asking the rule)", `if mode == FormModeResponse {`, false},
		{"IsResponseForm's own body", `func IsResponseForm(f *Form) bool { return FormMode(f) == FormModeResponse }`, false},
		{"the httpapi alias call site", `if formMode(form) == dbstore.FormModeResponse {`, false},
		{"not-equal against the shared constant", `if m != FormModeResponse {`, false},
		{"the recommended call", `if IsResponseForm(f) {`, false},
		{"comparing to the literal (the defect)", `if FormMode(f) == "response" {`, true},
		{"a bare assignment (a second definition)", `mode = FormModeResponse`, true},
		{"a short declaration (a second definition)", `m := FormModeResponse`, true},
		{"an assignment with no spaces", `mode=FormModeResponse`, true},
		{"a multi-line bare assignment", "mode =\n\tFormModeResponse", true},
	}
	for _, c := range cases {
		if got := modeLiteralShape.MatchString(c.src); got != c.match {
			t.Errorf("%s: pattern match = %v, want %v (source: %s)", c.name, got, c.match, c.src)
		}
	}
}

// TestTheModeRuleHasOneDefinition guards the shared rule itself. Every place that
// needs the answer must call dbstore.FormMode / dbstore.IsResponseForm; a
// hand-rolled string comparison against "response" is a second definition that
// can drift (the pre-fix mcpsrv copy did exactly this).
func TestTheModeRuleHasOneDefinition(t *testing.T) {
	// Only two files hold the literal legitimately: the rule's own file, and the
	// SQL CASE (which cannot call a Go function and has a behavioural test of its
	// own).
	//
	// records.go is deliberately NOT exempt: it is the decision's home and it
	// calls IsResponseForm like any other caller — exempting it let a mutation
	// that hand-rolled the comparison there pass the guard.
	//
	// httpapi/public.go is deliberately NOT exempt either. It used to be, as "the
	// httpapi alias of the rule", and an allowlisted file is an unscanned file: a
	// hand-rolled `v == "response"` planted in it stayed GREEN while the alias
	// name kept the call sites honest. It is clean on its own merits now — its
	// comparisons are against dbstore.FormModeResponse — and the alias itself is
	// pinned by TestTheHTTPAPIAliasIsOneDelegatingLine instead.
	allowed := map[string]bool{
		filepath.Join("dbstore", "form_mode.go"): true,
		filepath.Join("dbstore", "analytics.go"): true, // the SQL CASE, pinned by its own test
	}
	for _, path := range internalGoFiles(t) {
		rel := relInternal(path)
		if allowed[rel] {
			continue
		}
		// The assertion is per FILE and does not get the benefit of the doubt for
		// a file that also calls the helper: records.go does call IsResponseForm
		// in its other branch, and an escape hatch on that presence let a
		// hand-rolled comparison in the SAME file pass.
		if modeLiteralShape.MatchString(readWalked(t, path)) {
			t.Errorf("%s compares a form mode to a literal — the mode rule must be asked, not "+
				"re-derived (dbstore.FormMode / IsResponseForm / formMode), and the literal belongs "+
				"only in an allowlisted file. This is how the mode rule grew a second definition before", rel)
		}
	}
}

// TestTheHTTPAPIAliasIsOneDelegatingLine pins what public.go's old allowance was
// standing in for. public.go may hold the NAME formMode, but only as a delegation
// to dbstore.FormMode: a second implementation of the rule cannot hide in the
// alias body while the call sites still look right.
func TestTheHTTPAPIAliasIsOneDelegatingLine(t *testing.T) {
	src := readRepoFile(t, filepath.Join("internal", "httpapi", "public.go"))
	alias := regexp.MustCompile(`(?s)func formMode\(f \*dbstore\.Form\) string \{\s*return dbstore\.FormMode\(f\)\s*\}`)
	if !alias.MatchString(src) {
		t.Error("httpapi/public.go no longer defines formMode as one delegating line " +
			"(`func formMode(f *dbstore.Form) string { return dbstore.FormMode(f) }`) — the alias must " +
			"not become a second implementation of the mode rule")
	}
}

// TestTheAllowlistKeysMatchWalkedPaths keeps the allowlists honest. They are keyed
// on paths relative to internal/ ("dbstore/form_mode.go"), while the walk returns
// paths relative to this package directory ("../../internal/dbstore/form_mode.go").
// When the two disagree the allowlists silently stop matching, and the files that
// legitimately hold the literal are reported as violations — the guard then fails
// on clean code, and a key bug looks exactly like a real finding. Asserting each
// key exists in the walk is what turns that into a loud, named failure.
func TestTheAllowlistKeysMatchWalkedPaths(t *testing.T) {
	walked := map[string]bool{}
	sample := make([]string, 0, 3)
	for _, p := range internalGoFiles(t) {
		walked[relInternal(p)] = true
		if len(sample) < 3 {
			sample = append(sample, relInternal(p))
		}
	}
	keys := []string{
		filepath.Join("dbstore", "form_mode.go"),
		filepath.Join("dbstore", "analytics.go"),
		filepath.Join("dbstore", "records.go"),
		filepath.Join("dbstore", "admin_overview.go"),
	}
	for _, k := range keys {
		if !walked[filepath.ToSlash(k)] {
			t.Errorf("allowlist key %q matches no path the walk returns — the entry is dead, so the file it "+
				"meant to exempt is scanned (and clean code fails). Walked paths look like: %v", k, sample)
		}
	}
}

// TestMergedReadGoesThroughTheOneBuilder keeps the merged SQL honest, and keeps it
// SINGULAR: the two tables may only be merged in dbstore/records.go.
func TestMergedReadGoesThroughTheOneBuilder(t *testing.T) {
	// A new file that hand-rolls the merge is a second copy of the record-set
	// read, which is the shape this whole guard exists for — and it is refused
	// because the file exists, not because it is named. Two shapes count as a
	// hand-rolled merge: an explicit UNION ALL, and a file that queries BOTH
	// tables. The latter has two justified exceptions, both counters rather than
	// row reads: analytics.go (the SQL CASE, pinned by its own behavioural test)
	// and admin_overview.go (platform-wide COUNTs for super-admins).
	mergedOwner := filepath.Join("dbstore", "records.go")
	bothTablesAllowed := map[string]bool{
		mergedOwner:                                   true,
		filepath.Join("dbstore", "analytics.go"):      true,
		filepath.Join("dbstore", "admin_overview.go"): true,
	}
	salesTable := regexp.MustCompile(`(?i)FROM\s+sales\b`)
	subsTable := regexp.MustCompile(`(?i)FROM\s+submissions\b`)
	for _, path := range internalGoFiles(t) {
		rel := relInternal(path)
		src := readWalked(t, path)
		if strings.Contains(src, "UNION ALL") && rel != mergedOwner {
			t.Errorf("%s merges the sales and submissions tables (UNION ALL) — the merged read belongs "+
				"in dbstore/records.go, through dbstore.ResolveRecordScope, or the record set has a second definition", rel)
		}
		if salesTable.MatchString(src) && subsTable.MatchString(src) && !bothTablesAllowed[rel] {
			t.Errorf("%s reads both the sales and the submissions table by hand — a file that unions the "+
				"two record sets itself is the shape this guard exists for. Use dbstore.ResolveRecordScope + "+
				"the merged read (SaleStore.MergedPage/MergedAll/MergedCount).", rel)
		}
	}

	// records.go must keep building its WHERE clauses from the per-store
	// builders, so the list and the export cannot drift apart on filter
	// semantics (the export used to ignore every filter the list applied).
	src := readRepoFile(t, filepath.Join("internal", "dbstore", "records.go"))
	for _, want := range []string{"saleWhere(", "submissionWhere("} {
		if !strings.Contains(src, want) {
			t.Errorf("records.go no longer builds its WHERE clause with %s — a second copy of the "+
				"filter semantics is how the list and the export disagreed", want)
		}
	}
	// The merged read must select from both tables through the scope decision,
	// never from a hardcoded table pair.
	if !strings.Contains(src, "sc.IncludeSales") || !strings.Contains(src, "sc.IncludeSubs") {
		t.Error("records.go no longer branches on the RecordScope flags — the tables it reads must " +
			"come from the one decision, not be fixed")
	}
}

// TestThePerFormSubmissionReadsStayPerForm pins the surfaces that are CORRECT
// today: a {id} route always reads the submissions table, and the spam queues
// name their own table. Making one of these mode-aware would be a regression.
func TestThePerFormSubmissionReadsStayPerForm(t *testing.T) {
	perForm := readRepoFile(t, filepath.Join("internal", "httpapi", "rest_submissions.go"))
	if !strings.Contains(perForm, "Submissions.") {
		t.Error("rest_submissions.go no longer reads the submissions store — the per-form route " +
			"must always read submissions (it is per-form by design)")
	}
	spam := readRepoFile(t, filepath.Join("internal", "httpapi", "rest_spam.go"))
	if !strings.Contains(spam, "Submissions.") || !strings.Contains(spam, "Sales.") {
		t.Error("rest_spam.go no longer reads both tables — the spam queues name their own table and " +
			"must stay symmetric with the lists")
	}
}
