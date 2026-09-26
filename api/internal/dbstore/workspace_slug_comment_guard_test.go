package dbstore

// Guard for the workspace-slug rule's doc comment (card t_03010340).
//
// WHY THIS FILE EXISTS
//
// api/internal/dbstore/workspace_slug.go used to open with a claim that was
// false in shipped source:
//
//	// ... which is where "one more copy" stops being acceptable:
//	// both writers now delegate, and the legacy provision reuses the
//	// same validator so it cannot mint a slug the API would itself reject.
//
// Only ONE of the two other writers delegated. `grep -n 'ValidateWorkspaceSlug'
// api/ | grep -v _test` returns the definition, the comment, and a single call
// site (ProvisionLegacyWorkspaceForOwner, same package). The REST create handler
// and mcpsrv.validateWorkspaceSlug each still carry their own inline copy of the
// length / charset / "-jomform" rules.
//
// The same PR fixed the same shape elsewhere (auth.go's "this is the one function
// every login path funnels through"), and the reason it matters is mechanical:
// a doc comment that asserts a de-duplication which has not happened is a claim
// nothing can falsify, so the next reader believes the rule lives in one place
// and changes only that one. Correcting the prose without binding it to the code
// reproduces the defect in a nicer font.
//
// WHAT IS PINNED
//
// The corrected comment states a number: the slug SHAPE rule (3-40 / a-z 0-9 dash
// / must not end in "jomform") still has inline copies in the two write paths, so
// it is NOT yet one rule. That number is asserted here against the sources. Two
// consequences, both intended:
//
//   - Delete a copy without updating the comment -> red. ("both writers now
//     delegate" has become true, and the comment must say so.)
//   - Add a copy -> red. (A NEW writer reimplemented the rule, which is the
//     drift the comment is about.)
//
// This test reads SOURCE ONLY and needs no database, so it runs in the plain
// `go test ./...` pass, not only in the integration suite.
//
// The other direction — the claim that ValidateWorkspaceSlug has exactly one
// PRODUCTION caller — is asserted by the same counts plus the call-site test
// below, so the sentence cannot go stale in either half.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// slugShapeRule is the ONE expression each copy of the rule uses for its
// length leg. Matching on it rather than on a looser shape is deliberate: it is
// the line every copy shares verbatim, so a copy that is present cannot hide.
const slugShapeRule = "len(slug) < 3 || len(slug) > 40"

// slugRuleDefiners names the files whose copy of the shape rule is EXPECTED and
// accounted for in workspace_slug.go's comment: the definition itself, and the
// two write paths that still carry their own inline copy.
var slugRuleDefiners = []string{
	"internal/dbstore/workspace_slug.go",
	"internal/httpapi/rest_workspaces.go",
	"internal/mcpsrv/workspaces.go",
}

// TestWorkspaceSlugShapeRuleIsNotYetOneRule asserts the corrected comment's own
// claim: the shape rule still has copies in the two write paths, and the comment
// says exactly that.
//
// The scan WALKS the whole api/ tree rather than reading a fixed list, because
// the defect this guard is about is a FOURTH writer reimplementing the rule. A
// fixed list would report "the copies are gone" the moment the rule was pasted
// somewhere new — the opposite of what it must say.
//
// The assertion is stated as a SET of paths, not a count, so a copy moving
// between files is reported by name rather than as an off-by-one.
func TestWorkspaceSlugShapeRuleIsNotYetOneRule(t *testing.T) {
	found := scanAPITreeFor(t, slugShapeRule)

	// The definition must be there, or this guard is reading the wrong tree.
	if !found["internal/dbstore/workspace_slug.go"] {
		t.Fatalf("ValidateWorkspaceSlug's own length rule (%q) is not in internal/dbstore/workspace_slug.go — "+
			"the guard's anchor moved and it would pass vacuously", slugShapeRule)
	}

	// Everything that defines the rule, minus the ones the comment accounts for.
	var unaccounted []string
	for rel := range found {
		if !containsString(slugRuleDefiners, rel) {
			unaccounted = append(unaccounted, rel)
		}
	}
	sort.Strings(unaccounted)

	// The write paths the comment says are still carrying their own copy.
	var delegating []string
	for _, rel := range []string{
		"internal/httpapi/rest_workspaces.go",
		"internal/mcpsrv/workspaces.go",
	} {
		if !found[rel] {
			delegating = append(delegating, rel)
		}
	}
	if len(unaccounted) > 0 {
		t.Fatalf("the slug shape rule (%q) also appears in %v, which workspace_slug.go's comment does not "+
			"account for. That is a NEW writer reimplementing the rule — exactly the drift the comment is "+
			"about. Either make it delegate to ValidateWorkspaceSlug, or add it to the comment and to "+
			"slugRuleDefiners together", slugShapeRule, unaccounted)
	}

	comment := readAPIFile(t, "internal/dbstore/workspace_slug.go")
	// "still each carry their own inline copy" is the corrected comment's claim
	// that the de-duplication has NOT happened yet.
	claimsCopiesRemain := strings.Contains(comment, "still each carry their own inline copy")
	copiesRemain := len(delegating) == 0

	switch {
	case copiesRemain && claimsCopiesRemain:
		// The documented state: the copies are still there and the comment says
		// so. Nothing to do.
		return
	case !copiesRemain && !claimsCopiesRemain:
		// The intended end state: both writers delegate (PR 691 lands here) and
		// the comment states it. The rule really is one rule.
		return
	case copiesRemain && !claimsCopiesRemain:
		// The ORIGINAL defect, reproduced: the comment claims the writers
		// delegate while both still carry their own copy.
		t.Fatalf("workspace_slug.go's comment no longer claims the write paths carry their own copy, but %v "+
			"still do. That is the false claim this card removed — the comment now asserts a de-duplication "+
			"that has not happened", delegating)
	default: // !copiesRemain && claimsCopiesRemain
		t.Fatalf("%v no longer carry the inline slug shape rule (%q), but workspace_slug.go's comment still "+
			"claims they do. Either the delegation landed (then say so in the comment), or they were deleted "+
			"without delegating and the write paths no longer validate at all — work out which before "+
			"editing the prose", delegating, slugShapeRule)
	}
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// scanAPITreeFor returns every non-test .go file under api/ whose source
// contains needle, keyed by its api/-relative path. Test files are excluded so
// this guard cannot be satisfied by a fixture that happens to spell the rule.
func scanAPITreeFor(t *testing.T, needle string) map[string]bool {
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
		if strings.Contains(string(raw), needle) {
			rel, _ := filepath.Rel(root, path)
			found[filepath.ToSlash(rel)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk api/ for %q: %v — the guard cannot run without it", needle, err)
	}
	return found
}

// TestValidateWorkspaceSlugHasExactlyOneProductionCaller pins the other half of
// the comment: outside tests, the validator is called from ONE place
// (ProvisionLegacyWorkspaceForOwner in workspace.go), and NOT from the REST
// handler or the MCP tool.
//
// This is what makes "the legacy provision reuses the same validator so it
// cannot mint a slug the API would itself reject" a measured statement rather
// than a wish: the caller has to exist, in that file, and the two write paths
// must not quietly acquire the validator in a way that leaves the comment
// describing the wrong architecture.
func TestValidateWorkspaceSlugHasExactlyOneProductionCaller(t *testing.T) {
	// Every production file that names the validator, whether as a definition,
	// a call, or prose about it. workspace.go is here because it holds the one
	// call site the comment claims.
	scanned := []string{
		"internal/dbstore/workspace_slug.go",
		"internal/dbstore/workspace.go",
		"internal/httpapi/rest_workspaces.go",
		"internal/mcpsrv/workspaces.go",
	}

	callRE := regexp.MustCompile(`(?m)^[^\n]*\bValidateWorkspaceSlug\s*\(`)
	var callers []string
	for _, rel := range scanned {
		for _, line := range callRE.FindAllString(readAPIFile(t, rel), -1) {
			trimmed := strings.TrimSpace(line)
			// The definition's own signature is not a call site...
			if strings.Contains(line, "func ValidateWorkspaceSlug") {
				continue
			}
			// ...and neither is a doc-comment mention of the function.
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			callers = append(callers, rel)
		}
	}

	want := []string{"internal/dbstore/workspace.go"}
	if len(callers) != 1 || callers[0] != want[0] {
		t.Fatalf("ValidateWorkspaceSlug's non-test call sites are %v, want exactly %v "+
			"(ProvisionLegacyWorkspaceForOwner). workspace_slug.go's comment states that this validator has "+
			"ONE production caller and that the REST/MCP write paths still carry their own copies — if the "+
			"architecture changed, the comment must change with it", callers, want)
	}

	// The caller must really be the legacy provision's guard, not merely a call
	// that happens to sit in the right file.
	ws := readAPIFile(t, "internal/dbstore/workspace.go")
	if !strings.Contains(ws, "if err := ValidateWorkspaceSlug(slug); err != nil {") {
		t.Fatalf("internal/dbstore/workspace.go no longer validates the derived slug through " +
			"ValidateWorkspaceSlug — the comment's whole justification (\"the legacy provision reuses the " +
			"same validator so it cannot mint a slug the API would itself reject\") is then untrue")
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
