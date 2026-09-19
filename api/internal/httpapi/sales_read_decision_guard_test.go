package httpapi

// CLASS GUARD — the defect this PR fixes had a shape, not just a site.
//
// Five readers independently answered "is this a response form?" from a SINGLE
// form id (three Go copies plus an inline SQL CASE plus a fourth in mcpsrv), and
// every one of them answered "no" when there was no form filter. The fix is one
// shared decision (dbstore.ResolveRecordScope) keyed on the form SET. This guard
// exists so a FOURTH reader cannot quietly reintroduce the fifth copy: it fails
// the build when a sales read under internal/ re-implements the rule from a form
// id, or when the merged read's SQL stops going through the one builder.
//
// It is a SOURCE scan, not a behavioural test, and that is deliberate: a
// behaviour test can only cover the readers that exist today, so it cannot
// refuse the next one. The trade-off is that it can be satisfied by a rename, so
// the assertions below are anchored on the shapes that actually encode the bug
// (a `formID == ""` early-return beside a form read; a hand-rolled
// `form_id::text = $` outside sale.go/submission.go) rather than on a mere
// symbol name.
//
// Coverage limits, stated honestly: it scans Go source under api/internal for
// the enumerable shapes below. It cannot see a reader that decides the table
// from something else entirely (a new column, a per-form cache), and it does not
// scan non-Go callers.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// salesReadFiles are the files that implement the /sales read surface. The
// decision must be reached through dbstore.ResolveRecordScope, never re-derived.
var salesReadFiles = []string{
	"rest_handlers.go",
	"rest_records.go",
	"rest_export.go",
	"rest_dashboard.go",
}

// formIDKeyedGuard matches the pre-fix shape: an early return / branch keyed on
// an EMPTY form id guarding a form lookup or the table choice. That exact
// condition is the bug — an unfiltered read must be able to answer "which of my
// forms are response forms", not "is this id a response form".
var formIDKeyedGuard = regexp.MustCompile(`(?m)formID\s*==\s*""`)

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("class guard cannot read %s: %v (the guard cannot pass vacuously)", rel, err)
	}
	return string(b)
}

// TestSalesReadersUseTheOneDecision fails when a sales reader re-implements the
// table choice from a form id.
func TestSalesReadersUseTheOneDecision(t *testing.T) {
	for _, name := range salesReadFiles {
		src := readRepoFile(t, filepath.Join("internal", "httpapi", name))
		if m := formIDKeyedGuard.FindString(src); m != "" {
			t.Errorf("%s contains a `formID == \"\"` guard (%q) — that is the pre-fix shape "+
				"that made an unfiltered read fall back to sales-only. The table choice belongs in "+
				"dbstore.ResolveRecordScope (keyed on the form SET), reached via "+
				"RESTHandlers.recordScope.", name, m)
		}
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
// its own copy of the rule (responseModeFormID, re-parsing config["mode"] by
// hand instead of calling dbstore.FormMode).
func TestMCPReaderUsesTheOneDecision(t *testing.T) {
	src := readRepoFile(t, filepath.Join("internal", "mcpsrv", "handlers.go"))
	if strings.Contains(src, "responseModeFormID") {
		t.Error("mcpsrv/handlers.go re-implements the response-form decision (responseModeFormID) — " +
			"it must call the shared dbstore.ResolveRecordScope via Server.recordScope")
	}
	if !strings.Contains(src, "dbstore.ResolveRecordScope") {
		t.Error("mcpsrv/handlers.go no longer consults dbstore.ResolveRecordScope — the MCP sales " +
			"read would drift from the REST one again")
	}
	// The mode RULE itself has one definition (dbstore.FormMode); a reader that
	// reads config["mode"] by hand is a second copy.
	if regexp.MustCompile(`Config\["mode"\]`).MatchString(src) {
		t.Error("mcpsrv/handlers.go reads config[\"mode\"] by hand — use dbstore.FormMode/IsResponseForm, " +
			"the single definition of the mode rule")
	}
}

// TestTheModeRuleHasOneDefinition guards the shared rule itself. Every place that
// needs the answer must call dbstore.FormMode / dbstore.IsResponseForm; a
// hand-rolled string comparison against "response" is a second definition that
// can drift (the pre-fix mcpsrv copy did exactly this).
func TestTheModeRuleHasOneDefinition(t *testing.T) {
	// The rule's own file is where the literal belongs, plus the SQL CASE (which
	// cannot call a Go function and has a behavioural test of its own), plus the
	// httpapi alias.
	//
	// records.go is deliberately NOT exempt: it is the decision's home and it
	// calls IsResponseForm like any other caller — exempting it let a mutation
	// that hand-rolled the comparison there pass the guard.
	allowed := map[string]bool{
		filepath.Join("dbstore", "form_mode.go"): true,
		filepath.Join("dbstore", "analytics.go"): true, // the SQL CASE, pinned by its own test
		filepath.Join("httpapi", "public.go"):    true, // formMode() is the httpapi alias of the rule
	}
	handRolled := regexp.MustCompile(`==\s*"response"|=\s*FormModeResponse`)

	root := filepath.Join("..", "..", "internal")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil // tests may state the literal value to assert it
		}
		rel, _ := filepath.Rel(root, path)
		if allowed[rel] {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		// The assertion is per FILE and does not get the benefit of the doubt
		// for a file that also calls the helper: records.go does call
		// IsResponseForm in its other branch, and an escape hatch on that
		// presence let a hand-rolled comparison in the SAME file pass.
		if handRolled.MatchString(string(b)) {
			t.Errorf("%s compares a form mode to a literal — the mode rule must be asked, not "+
				"re-derived (dbstore.FormMode / IsResponseForm / formMode), and the literal belongs "+
				"only in an allowlisted file. This is how the mode rule grew a second definition before", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("class guard could not walk internal/: %v", err)
	}
}

// TestMergedReadGoesThroughTheOneBuilder keeps the merged SQL honest: the WHERE
// clauses must come from the per-store builders, so the list and the export
// cannot drift apart on filter semantics (the export used to ignore every filter
// the list applied).
func TestMergedReadGoesThroughTheOneBuilder(t *testing.T) {
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
