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
// the next one. The trade-off is that a scan can be satisfied by a rename, so
// every assertion is anchored on a shape that encodes the bug — and every scan
// WALKS THE TREE instead of naming files, so a new file is scanned because it
// exists, not because someone remembered to add it to a list. The only lists here
// are allowlists of files that are already justified, so an unlisted new file
// fails by default. (The guard used to hold a hardcoded four-file list: a
// compiling fifth reader planted beside them shipped green.)
//
// Coverage limits, stated honestly: it scans Go source under api/internal for the
// enumerable shapes below. It cannot see a reader that decides the table from
// something else entirely (a new column, a per-form cache), and it does not scan
// non-Go callers.

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
// negated class instead. That fix is the whole point of this card: it must stay
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
