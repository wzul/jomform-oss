package dbstore

// Guard for the workspace-slug rule's architecture (card t_3541fb75, the rework
// of PR 691 onto current main; the caller side re-walked on card t_fec28096).
//
// WHAT REPLACED WHAT. The file this replaces (workspace_slug_comment_guard_test.go)
// pinned the OPPOSITE state, and it was right to at the time: it asserted that
// the shape rule STILL had inline copies in the two write paths, because a
// doc comment on main claimed a de-duplication that had not happened. PR 691 is
// what performs that de-duplication, so the old guard's premise ("the copies
// remain") is now false BY DESIGN — keeping it would have meant keeping the
// drift it described.
//
// The reserved-subdomain rule (t_586ff0c3, merged after PR 691's old base) is
// the second half of the same story: it landed as its own leg in each transport,
// and it belongs in the ONE rule for exactly the same reason the shape rule does.
// This guard pins all of it, in both directions:
//
//   - the shared rule is the only place the shape rule is written;
//   - the shared rule is the only place the reserved list is consulted;
//   - every write path that can SET or MOVE a slug actually delegates to it.
//
// It reads SOURCE ONLY and needs no database, so it runs in the plain
// `go test ./...` pass, not only in the integration suite.
//
// CARD t_fec28096 — WHY THE CALLER SIDE WALKS THE TREE, AGAIN.
//
// The file PR 691 landed to replace the old guard kept the shape half's tree
// walk and dropped the caller half's: it read a fixed pair of file paths
// (slugRuleCallers) and checked each for `strings.Contains(src,
// "ValidateWorkspaceSlug(")`. That is the very defect PR 808 had just fixed one
// file earlier — a hard-coded list is satisfied by the files it names and blind
// to every other one — so a caller added in a NEW file was invisible again.
// Measured on main before this change: a fifth caller in a brand-new
// internal/httpapi/qa_fifth_caller.go compiled (`go build ./...` RC=0) and left
// TestWorkspaceSlugRuleHasExactlyOneDefinition AND the whole dbstore package
// green under -race. The drift this guard claims to catch was the one thing it
// could not see.
//
// The caller half now derives the set by WALKING the api/ tree, and compares it
// with the reviewed set. Three further decisions are deliberate, not incidental:
//
//  1. The walk matches CALLS through the AST (go/parser), not a substring. A
//     definition is not a call expression, so the rule's own `func
//     ValidateWorkspaceSlug(slug string) (string, error)` can never be counted
//     as a call site — which a substring pattern cannot promise: an
//     arity-specific pattern mis-classifies that definition line as a caller
//     (the pre-691 definition returned only `error`, and 691's takes `(string,
//     error)`), so the subtraction is load-bearing there and structural here.
//  2. Comments are not parsed (`parser.ParseFile(..., 0)`), so a file cannot be
//     made a caller by a sentence in a comment, nor stop being one by
//     commenting its call out.
//  3. The shape half pins the BOUNDS, not the exact identifier: it matches
//     `len(<ident>) < 3 || len(<ident>) > 40`. The old exact-string match
//     claimed "a re-implementation cannot hide behind a renamed variable" and
//     did not deliver it — measured on main: `func qaRenamedVarWriter(s string)
//     bool { return len(s) < 3 || len(s) > 40 }` in a new file left the guard
//     green. Its limit, stated rather than implied: a re-implementation that
//     restates the bounds in another arrangement (`len(s) > 40 || len(s) < 3`,
//     `len(s) <= 2`) is still not a match on this line — a source pattern
//     cannot bound every spelling, and the behavioural tests are what catch a
//     rule that is wrong rather than duplicated.
//  4. The RESERVED leg is read the same way as the caller half (card
//     t_b0f76a19). Both of its checks used to be substring reads over the rule
//     file, so a COMMENT satisfied them, inside a file whose own header argues
//     that comments must not be able to satisfy a check. Measured before this
//     change: with the leg disabled and its call text left in a comment
//     (`// PROBE: urlguard.ReservedSubdomain(slug) used to be refused here.`
//     followed by `if false {`) the rule built clean (`go build ./...` RC=0)
//     and TestTheReservedLegLivesInTheOneRule,
//     TestTheReservedLegRunsBeforeTheShapeRule and urlguard's
//     TestTheOneRuleConsumesThisList all stayed GREEN. They now parse the rule
//     with go/parser, comments dropped, and require a `urlguard.ReservedSubdomain`
//     CALL inside ValidateWorkspaceSlug's own body — and, for the order check, a
//     shape condition in the same parsed body — so a sentence can no longer
//     stand in for either leg. The behavioural suite is still what keeps a wrong
//     rule from shipping; this file's job is to keep a SECOND copy, or a
//     silently-missing leg, from being reported green.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// slugShapeRule is the length leg of the rule, as it is written in the ONE
// implementation. It is quoted in this file's messages; the tree-wide scan uses
// slugShapeRuleRe, which matches the same bounds.
const slugShapeRule = "len(slug) < 3 || len(slug) > 40"

// slugShapeRuleRe pins the length leg's BOUNDS — 3 and 40, as one conjunction —
// while leaving the identifier open, so a second implementation cannot escape
// by renaming its parameter. It matched exactly one file in api/ when it was
// introduced (internal/dbstore/workspace_slug.go); anything else matching it is
// a second definition of the same rule and is reported by name.
var slugShapeRuleRe = regexp.MustCompile(
	`len\([A-Za-z_][A-Za-z0-9_]*\)\s*<\s*3\s*\|\|\s*len\([A-Za-z_][A-Za-z0-9_]*\)\s*>\s*40`)

// slugRuleDefinitionRe finds the shared rule's definition whatever its ARITY
// (`func ValidateWorkspaceSlug(slug string) (string, error)` on current main).
// An arity-specific pattern would either miss the definition or read it as a
// call site, which is the mis-classification card t_fec28096 records.
var slugRuleDefinitionRe = regexp.MustCompile(`(?m)^func\s+ValidateWorkspaceSlug\b`)

// slugRuleFile is the ONE place the workspace-slug rule is defined.
const slugRuleFile = "internal/dbstore/workspace_slug.go"

// slugRuleCallers is the REVIEWED SET of production files that call the shared
// rule — the write paths that can set or MOVE a tenant's public host, plus the
// legacy provisioning path in the rule's own package:
//
//	dbstore/workspace.go        — ProvisionLegacyWorkspaceForOwner's derived slug,
//	                              and the reserved-name probe it runs on candidates
//	httpapi/rest_workspaces.go  — POST /v1/workspaces, PATCH /v1/workspaces/{id}
//	mcpsrv/workspaces.go        — create_workspace, update_workspace
//
// It is the thing the derived set is COMPARED against, and the only list in this
// file that is hand-written. It is not the scan: the scan walks the tree, so a
// caller in a file this list never names is reported by path instead of being
// invisible (that was the measured defect — see the header).
//
// A file may hold more than one call site (`dbstore/workspace.go` holds three),
// so the assertion is over the SET OF PATHS and never over a count.
var slugRuleCallers = []string{
	"internal/dbstore/workspace.go",
	"internal/httpapi/rest_workspaces.go",
	"internal/mcpsrv/workspaces.go",
}

// TestWorkspaceSlugRuleHasExactlyOneDefinition is the core assertion: the shape
// rule appears in ONE non-test file, that file really defines the rule, and the
// set of files calling it is exactly the reviewed set.
//
// The scan WALKS the whole api/ tree rather than reading a fixed list, because
// the defect this guard is about is a FOURTH writer reimplementing the rule — or
// a new caller of it. A fixed list reports "the copies are gone" the moment the
// rule was pasted somewhere new — the opposite of what it must say.
//
// The assertion is stated as a SET of paths, not a count, so a copy moving
// between files is reported by name rather than as an off-by-one.
func TestWorkspaceSlugRuleHasExactlyOneDefinition(t *testing.T) {
	found := scanAPITreeMatching(t, fmt.Sprintf("a file matching the shape rule %s", slugShapeRuleRe),
		slugShapeRuleRe.MatchString)

	// The rule must be there, or the guard is reading the wrong tree and would
	// pass vacuously.
	if !found[slugRuleFile] {
		t.Fatalf("the slug shape rule (%s) is not in %s — the guard's anchor moved and it would "+
			"pass vacuously", slugShapeRule, slugRuleFile)
	}

	var extra []string
	for rel := range found {
		if rel != slugRuleFile {
			extra = append(extra, rel)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		t.Fatalf("the slug shape rule (%s, any parameter name) is defined in %v as well as %s. "+
			"That is a SECOND implementation — the drift this rule exists to prevent. Make the "+
			"extra file(s) delegate to ValidateWorkspaceSlug instead.", slugShapeRule, extra,
			slugRuleFile)
	}

	// The definition must be in the file this guard names, whatever its arity —
	// otherwise the caller set below is compared against a rule that lives
	// somewhere else, and the guard has silently changed its subject.
	if !slugRuleDefinitionRe.MatchString(readAPIFile(t, slugRuleFile)) {
		t.Fatalf("no `func ValidateWorkspaceSlug` definition in %s — the guard's anchor moved, and "+
			"a guard checked against the wrong file proves nothing. Fix slugRuleFile in this test "+
			"if the rule deliberately moved.", slugRuleFile)
	}

	// And every file that calls the rule must be a REVIEWED caller. The set is
	// derived from the tree, so a new caller is named by path instead of passing.
	callers := scanAPITreeForCallers(t, "ValidateWorkspaceSlug")
	if len(callers) == 0 {
		t.Fatalf("no file under api/ calls ValidateWorkspaceSlug — the walk is not covering the " +
			"tree, and this half of the guard would then pass while proving nothing")
	}

	got := make([]string, 0, len(callers))
	for rel := range callers {
		got = append(got, rel)
	}
	sort.Strings(got)

	reviewed := map[string]bool{}
	for _, rel := range slugRuleCallers {
		reviewed[rel] = true
	}
	var unexpected []string
	for _, rel := range got {
		if !reviewed[rel] {
			unexpected = append(unexpected, fmt.Sprintf("%s (%s)", rel,
				strings.Join(sortedCopy(callers[rel]), ", ")))
		}
	}
	if len(unexpected) > 0 {
		t.Errorf("these files call ValidateWorkspaceSlug but are not reviewed callers of the "+
			"shared rule:\n\t%s\n"+
			"Call sites found: %v\nReviewed set:     %v\n"+
			"A file calling the rule is a SURFACE that enforces the slug rule, and it inherits the "+
			"reserved-name refusal with it. If the new caller is deliberate, add its path to "+
			"slugRuleCallers in this test WITH its reason in the same change — the list is not the "+
			"scan, it is the thing the scan is compared against.",
			strings.Join(unexpected, "\n\t"), got, slugRuleCallers)
	}
	for _, rel := range slugRuleCallers {
		if len(callers[rel]) == 0 {
			t.Errorf("%s is listed in slugRuleCallers but no longer calls ValidateWorkspaceSlug — "+
				"it either stopped validating the slug or grew its own copy of the rule, and the "+
				"rule is then no longer one rule. Remove the entry only if the surface is really "+
				"gone, never because the guard is inconvenient.", rel)
		}
	}
	for _, rel := range got {
		t.Logf("%s calls the shared rule at %s", rel, strings.Join(sortedCopy(callers[rel]), ", "))
	}
}

// TestTheReservedLegLivesInTheOneRule pins the half of the rule that arrived
// from a different card (t_586ff0c3): the reserved-subdomain refusal must be
// enforced INSIDE the shared rule, not restated in a transport.
//
// WHY THIS DIRECTION. A transport-level check can be forgotten by the next
// caller, and it cannot be reached by the rename path at all unless that path
// re-implements it — which is how a name ends up claimable on one surface and
// refused on another. With the leg in the rule, every caller inherits it, and
// "did we forget the reserved list on this path?" stops being a question.
//
// HOW IT READS (card t_b0f76a19). This used to be a substring read of the rule
// file, so the CALL did not have to exist — a comment spelling it was enough
// (see the header's point 4 for the measurement). It now parses the rule with
// comments dropped and requires a real CallExpr inside ValidateWorkspaceSlug's
// own body, which is the same reason the caller half above uses go/parser. The
// package is resolved from the file's IMPORTS, so renaming the import is not
// drift while some other package's ReservedSubdomain is not accepted here.
func TestTheReservedLegLivesInTheOneRule(t *testing.T) {
	file, _ := parseAPIFile(t, slugRuleFile)
	pkg := urlguardPkgNames(t, file, slugRuleFile)
	rule := funcDeclByName(t, file, slugRuleFile, "ValidateWorkspaceSlug")

	if sites := importedCallSites(rule.Body, pkg, "ReservedSubdomain"); len(sites) == 0 {
		t.Fatalf("%s does not CALL urlguard.ReservedSubdomain inside ValidateWorkspaceSlug "+
			"(parsed with comments dropped, so a comment naming the call does not count) — the "+
			"reserved-name refusal is enforced somewhere else, so a caller that does not restate it "+
			"accepts a name JomForm owns. Call sites found in total: %d.", slugRuleFile,
			len(importedCallSites(file, pkg, "ReservedSubdomain")))
	}
	// The refusal must be the SHARED wording, not a second sentence. This half is
	// FILE-scoped on purpose: the wording call lives in reservedSlugRefusal.Error,
	// not in the rule's body, and the class sentinel the transports match is what
	// the rule returns.
	if len(importedCallSites(file, pkg, "ReservedSubdomainMessage")) == 0 {
		t.Errorf("%s refuses a reserved label without CALLING urlguard.ReservedSubdomainMessage — "+
			"a second wording is a second rule, and the transports return whatever the rule says",
			slugRuleFile)
	}
	// The list itself must stay private: two things to keep in step is the
	// defect, not the fix. Deliberately a RAW read that any mention fails: this is
	// the one check where a comment mentioning the name IS the thing to catch,
	// because a pasted copy starts as one.
	for _, rel := range append([]string{slugRuleFile}, slugRuleCallers...) {
		if strings.Contains(readAPIFile(t, rel), "reservedSubdomains") {
			t.Errorf("%s refers to the package-private reserved list directly — the list must stay "+
				"inside urlguard so there is exactly one copy", rel)
		}
	}
}

// TestTheReservedLegRunsBeforeTheShapeRule pins the ORDER inside the rule. It is
// not cosmetic: "s3" is a reserved label AND shorter than the 3-character
// minimum, so if the length leg ran first the caller would be told their input
// was malformed and would retry a variation of the same sensitive name instead
// of learning the name is JomForm's. Policy refusal beats shape refusal.
//
// Both sides of the comparison come from the PARSED body (card t_b0f76a19), not
// from strings.Index over the raw file: a commented-out copy of either leg used
// to move the comparison without moving the code — and a commented-out reserved
// leg satisfied this test entirely.
func TestTheReservedLegRunsBeforeTheShapeRule(t *testing.T) {
	file, fset := parseAPIFile(t, slugRuleFile)
	pkg := urlguardPkgNames(t, file, slugRuleFile)
	rule := funcDeclByName(t, file, slugRuleFile, "ValidateWorkspaceSlug")

	reserved := importedCallSites(rule.Body, pkg, "ReservedSubdomain")
	shape := slugShapeCondSites(rule.Body)
	if len(reserved) == 0 || len(shape) == 0 {
		t.Fatalf("ValidateWorkspaceSlug no longer contains both legs in its parsed body "+
			"(reserved calls: %d, shape conditions: %d) — this guard would otherwise pass on a "+
			"function that checks neither", len(reserved), len(shape))
	}
	reservedAt := fset.Position(reserved[0]).Offset
	shapeAt := fset.Position(shape[0]).Offset
	if reservedAt > shapeAt {
		t.Errorf("ValidateWorkspaceSlug checks the shape rule before the reserved list. A reserved " +
			"label shorter than the minimum (\"s3\") would then be reported as malformed input, and the " +
			"merchant would retry a variation of a name JomForm owns")
	}
}

// TestLegacyProvisionStillValidatesTheDerivedSlug pins the caller in the rule's
// own package: ProvisionLegacyWorkspaceForOwner must validate the slug it
// derived from the owner's email through the SAME rule, so the auto-provisioning
// path cannot mint a host the API would itself reject.
func TestLegacyProvisionStillValidatesTheDerivedSlug(t *testing.T) {
	ws := readAPIFile(t, "internal/dbstore/workspace.go")
	if !strings.Contains(ws, "if _, err := ValidateWorkspaceSlug(slug); err != nil {") {
		t.Fatalf("internal/dbstore/workspace.go no longer validates the derived slug through " +
			"ValidateWorkspaceSlug — the auto-provisioning path can then mint a slug the API would " +
			"refuse, and the user would own a workspace whose own subdomain is dead")
	}
}

// scanAPITreeMatching returns every non-test .go file under api/ whose source
// satisfies match, keyed by its api/-relative path. Test files are excluded so
// this guard cannot be satisfied by a fixture that happens to spell the rule.
func scanAPITreeMatching(t *testing.T, what string, match func(string) bool) map[string]bool {
	t.Helper()
	root := filepath.Join("..", "..")
	found := map[string]bool{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Vendor and generated trees are not writers of this rule.
			if info.Name() == "vendor" || info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if match(string(raw)) {
			rel, _ := filepath.Rel(root, path)
			found[filepath.ToSlash(rel)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk api/ for %s: %v — the guard cannot run without it", what, err)
	}
	if len(found) == 0 {
		t.Fatalf("no file under api/ is %s — the walk is not covering the tree", what)
	}
	return found
}

// scanAPITreeForCallers returns, for every non-test .go file under api/ that
// CALLS name, the source positions of those calls, keyed by the file's
// api/-relative path.
//
// WHY A WALK AND NOT A LIST. A fixed list is satisfied by the files it names and
// blind to every other one, so the drift it claims to catch is the one drift it
// cannot report — measured on main before this change: a fifth caller in
// internal/httpapi/qa_fifth_caller.go built clean and red-ed nothing in this
// package. The set of files is therefore DERIVED, and slugRuleCallers is only
// the thing it is compared against.
//
// The parse keeps no comments (`parser.ParseFile(..., 0)`), so a sentence in a
// comment can neither satisfy this check nor break it.
func scanAPITreeForCallers(t *testing.T, name string) map[string][]string {
	t.Helper()
	root := filepath.Join("..", "..")
	found := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "testdata", "vendor":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, raw, 0)
		if perr != nil {
			return perr
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		for _, pos := range ruleCallSites(file, name) {
			found[rel] = append(found[rel], fmt.Sprintf("line %d", fset.Position(pos).Line))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk api/ for callers of %s: %v — the guard cannot run without it", name, err)
	}
	return found
}

// ruleCallSites returns the position of every CALL to name inside file.
//
// A call is a CallExpr whose callee is the bare name, a qualified
// `pkg.ValidateWorkspaceSlug`, or a name the rule was BOUND to in this file
// (`v := dbstore.ValidateWorkspaceSlug` and then `v(slug)`, package-level or
// local). The binding is covered because the name-only spellings above cannot
// see it, and a surface that binds the rule first would otherwise be a caller
// this guard does not report.
//
// A DEFINITION is not a call, structurally: the rule's own `func
// ValidateWorkspaceSlug(slug string) (string, error)` is a FuncDecl name, not a
// CallExpr. No arity-, name-, or parameter-specific pattern is needed to
// subtract it — which matters here, because the definition line DOES match the
// string "ValidateWorkspaceSlug(" that a substring scan looks for.
//
// WHAT THIS CANNOT SEE, stated so it is not over-claimed: the rule stored as a
// func value in a container (`map[string]func(string) (string, error){"v":
// dbstore.ValidateWorkspaceSlug}`) and called through that container. A wrapper
// of that shape still reaches the rule by one of the spellings above, and a full
// re-implementation is the shape-rule half's job.
func ruleCallSites(file *ast.File, name string) []token.Pos {
	bound := ruleBoundNames(file, name)
	var sites []token.Pos
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if callsRule(call.Fun, name, bound) {
			sites = append(sites, call.Pos())
		}
		return true
	})
	return sites
}

// callsRule reports whether a CallExpr's callee expression is a call of the
// rule: the bare name, a selector ending in the name (`dbstore.`-qualified or,
// deliberately, a method of the same name — a method is a second implementation,
// and naming its file is the right report), or a name bound to the rule.
func callsRule(fun ast.Expr, name string, bound map[string]bool) bool {
	switch e := fun.(type) {
	case *ast.Ident:
		return e.Name == name || bound[e.Name]
	case *ast.SelectorExpr:
		return e.Sel != nil && e.Sel.Name == name
	case *ast.ParenExpr:
		return callsRule(e.X, name, bound)
	}
	return false
}

// ruleBoundNames returns every name in file that the rule itself is bound to:
// a package-level `var v = dbstore.ValidateWorkspaceSlug` or a local
// `v := dbstore.ValidateWorkspaceSlug`. Only a binding to the rule's entry point
// counts — `v := somethingElse` is not a caller, and resolving arbitrary
// indirection would be guessing.
//
// The map is FILE-scoped rather than scope-precise. That direction is safe HERE
// because the assertion is over the SET OF FILES: over-reporting a site inside a
// file that already calls the rule changes nothing, and the only names that can
// be added are ones assigned the rule itself.
func ruleBoundNames(file *ast.File, name string) map[string]bool {
	bound := map[string]bool{}
	note := func(lhs []ast.Expr, rhs []ast.Expr) {
		if len(lhs) != len(rhs) {
			return
		}
		for i, l := range lhs {
			id, ok := l.(*ast.Ident)
			if !ok || id.Name == "_" {
				continue
			}
			if refersToRule(rhs[i], name) {
				bound[id.Name] = true
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch d := n.(type) {
		case *ast.ValueSpec:
			lhs := make([]ast.Expr, 0, len(d.Names))
			for _, nm := range d.Names {
				lhs = append(lhs, nm)
			}
			note(lhs, d.Values)
		case *ast.AssignStmt:
			note(d.Lhs, d.Rhs)
		}
		return true
	})
	return bound
}

// refersToRule reports whether e names the rule (bare or qualified).
func refersToRule(e ast.Expr, name string) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name == name
	case *ast.SelectorExpr:
		return v.Sel != nil && v.Sel.Name == name
	}
	return false
}

// sortedCopy returns in, sorted, without mutating the caller's slice.
func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// urlguardImportPath is the package whose reserved-name leg the rule must
// consult. It is resolved from the rule file's IMPORTS rather than matched as
// the literal selector `urlguard.…`, so renaming the import is not reported as
// drift while a call to some other package's ReservedSubdomain is not accepted
// as this one.
const urlguardImportPath = "github.com/wzul/jomform/api/internal/urlguard"

// parseAPIFile parses a file by its api/-relative path with NO comments
// (`parser.ParseFile(..., 0)`), so a sentence describing a call can neither
// satisfy a check built on the result nor break one.
func parseAPIFile(t *testing.T, rel string) (*ast.File, *token.FileSet) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, readAPIFile(t, rel), 0)
	if err != nil {
		t.Fatalf("parse %s: %v — this guard cannot run without it", rel, err)
	}
	return file, fset
}

// urlguardPkgNames returns the local names file uses for urlguard — the default
// name unless it is imported under an alias. An empty result is a FAILURE, not
// an empty check: without the import the reserved leg cannot be in this file,
// and a guard that then inspected nothing would pass on a file that lost it.
func urlguardPkgNames(t *testing.T, file *ast.File, rel string) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != urlguardImportPath {
			continue
		}
		if imp.Name != nil {
			names[imp.Name.Name] = true
			continue
		}
		names["urlguard"] = true
	}
	if len(names) == 0 {
		t.Fatalf("%s does not import %s — the reserved-name leg has left the rule, and this guard "+
			"would otherwise be checking nothing", rel, urlguardImportPath)
	}
	return names
}

// funcDeclByName returns the top-level func declaration called name, failing
// when it is absent or bodiless: a guard whose subject moved must say so,
// never pass by looking at the rest of the file.
func funcDeclByName(t *testing.T, file *ast.File, rel, name string) *ast.FuncDecl {
	t.Helper()
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if ok && fn.Name != nil && fn.Name.Name == name && fn.Body != nil {
			return fn
		}
	}
	t.Fatalf("no `func %s` definition in %s — the guard's anchor moved, and a guard checked "+
		"against the wrong file proves nothing. Fix the anchor if the rule deliberately moved.",
		name, rel)
	return nil
}

// importedCallSites returns the position of every CALL to pkg.fn inside node
// (a whole file or a single function body). pkg holds the local names the
// imported package is bound to, so an aliased import still counts. Comments are
// absent from the parsed tree, so text describing a call is never a call.
func importedCallSites(node ast.Node, pkg map[string]bool, fn string) []token.Pos {
	var sites []token.Pos
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != fn {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && pkg[id.Name] {
			sites = append(sites, call.Pos())
		}
		return true
	})
	return sites
}

// slugShapeCondSites returns the position of every expression inside node that
// restates the shape rule's BOUNDS as one conjunction: `len(<ident>) < 3 ||
// len(<ident>) > 40`. It matches the AST, so a commented or quoted copy is not
// a match — the same limit the tree-wide slugShapeRuleRe carries applies to the
// bounds themselves: another ARRANGEMENT (`len(s) > 40 || len(s) < 3`,
// `len(s) <= 2`) is a different expression and is not matched here either.
func slugShapeCondSites(node ast.Node) []token.Pos {
	var sites []token.Pos
	ast.Inspect(node, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok && isSlugShapeCond(e) {
			sites = append(sites, e.Pos())
		}
		return true
	})
	return sites
}

// isSlugShapeCond reports whether e is `len(<ident>) < 3 || len(<ident>) > 40`,
// with parentheses allowed around either comparison.
func isSlugShapeCond(e ast.Expr) bool {
	lor, ok := stripParens(e).(*ast.BinaryExpr)
	if !ok || lor.Op != token.LOR {
		return false
	}
	return isLenBound(lor.X, token.LSS, "3") && isLenBound(lor.Y, token.GTR, "40")
}

// isLenBound reports whether e is `len(<ident>) <op> <n>`.
func isLenBound(e ast.Expr, op token.Token, n string) bool {
	cmp, ok := stripParens(e).(*ast.BinaryExpr)
	if !ok || cmp.Op != op {
		return false
	}
	call, ok := stripParens(cmp.X).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != "len" {
		return false
	}
	if _, ok := stripParens(call.Args[0]).(*ast.Ident); !ok {
		return false
	}
	lit, ok := stripParens(cmp.Y).(*ast.BasicLit)
	return ok && lit.Kind == token.INT && lit.Value == n
}

// unparen strips any number of parentheses from e.
func stripParens(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// readAPIFile reads a file by its api/-relative path, failing (never skipping)
// when it cannot: a guard that silently passes because it read nothing proves
// nothing. Paths are resolved from this file's own directory, so the test is
// independent of the working directory the gate happens to run in.
func readAPIFile(t *testing.T, rel string) string {
	t.Helper()
	path := filepath.Join("..", "..", filepath.FromSlash(rel))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — this guard cannot run without it, and a skip would report green while "+
			"proving nothing", path, err)
	}
	if len(raw) == 0 {
		t.Fatalf("%s is empty", path)
	}
	return string(raw)
}
