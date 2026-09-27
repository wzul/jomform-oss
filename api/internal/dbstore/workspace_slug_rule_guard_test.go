package dbstore

// Guard for the workspace-slug rule's architecture (card t_3541fb75, the rework
// of PR 691 onto current main).
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

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// slugShapeRule is the length leg of the rule, as it is written in the ONE
// implementation. Matching the exact expression rather than a looser shape is
// deliberate: a re-implementation cannot hide behind a renamed variable.
const slugShapeRule = "len(slug) < 3 || len(slug) > 40"

// slugRuleFile is the ONE place the workspace-slug rule is defined.
const slugRuleFile = "internal/dbstore/workspace_slug.go"

// slugRuleCallers are the production files that must DELEGATE to it. They are
// the write paths that can set or MOVE a tenant's public host:
//
//	httpapi/rest_workspaces.go — POST /v1/workspaces, PATCH /v1/workspaces/{id}
//	mcpsrv/workspaces.go       — create_workspace, update_workspace
//
// dbstore/workspace.go's ProvisionLegacyWorkspaceForOwner also calls it, but its
// call is asserted below in the same file as the rule, so it is covered by the
// "the one implementation is where this file says it is" check.
var slugRuleCallers = []string{
	"internal/httpapi/rest_workspaces.go",
	"internal/mcpsrv/workspaces.go",
}

// TestWorkspaceSlugRuleHasExactlyOneDefinition is the core assertion: the shape
// rule appears in ONE non-test file, and every file that needs it delegates.
//
// The scan WALKS the whole api/ tree rather than reading a fixed list, because
// the defect this guard is about is a FOURTH writer reimplementing the rule. A
// fixed list would report "the copies are gone" the moment the rule was pasted
// somewhere new — the opposite of what it must say.
//
// The assertion is stated as a SET of paths, not a count, so a copy moving
// between files is reported by name rather than as an off-by-one.
func TestWorkspaceSlugRuleHasExactlyOneDefinition(t *testing.T) {
	found := scanAPITreeFor(t, slugShapeRule)

	// The rule must be there, or the guard is reading the wrong tree and would
	// pass vacuously.
	if !found[slugRuleFile] {
		t.Fatalf("the slug shape rule (%q) is not in %s — the guard's anchor moved and it would "+
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
		t.Fatalf("the slug shape rule (%q) is defined in %v as well as %s. That is a SECOND "+
			"implementation — the drift this rule exists to prevent. Make the extra file(s) delegate "+
			"to ValidateWorkspaceSlug instead.", slugShapeRule, extra, slugRuleFile)
	}

	// And every write path that can set or move a slug must really call it.
	for _, rel := range slugRuleCallers {
		src := readAPIFile(t, rel)
		if !strings.Contains(src, "ValidateWorkspaceSlug(") {
			t.Errorf("%s does not call ValidateWorkspaceSlug — it either stopped validating the "+
				"slug or grew its own copy of the rule, and the rule is then no longer one rule", rel)
		}
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
func TestTheReservedLegLivesInTheOneRule(t *testing.T) {
	src := readAPIFile(t, slugRuleFile)
	if !strings.Contains(src, "urlguard.ReservedSubdomain(") {
		t.Fatalf("%s does not consult urlguard.ReservedSubdomain — the reserved-name refusal is "+
			"enforced somewhere else, so a caller that does not restate it accepts a name JomForm "+
			"owns", slugRuleFile)
	}
	// The refusal must be the SHARED wording, not a second sentence.
	if !strings.Contains(src, "urlguard.ReservedSubdomainMessage(") {
		t.Errorf("%s refuses a reserved label without urlguard.ReservedSubdomainMessage — a second "+
			"wording is a second rule, and the transports return whatever the rule says", slugRuleFile)
	}
	// The list itself must stay private: two things to keep in step is the
	// defect, not the fix.
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
func TestTheReservedLegRunsBeforeTheShapeRule(t *testing.T) {
	src := readAPIFile(t, slugRuleFile)
	start := strings.Index(src, "func ValidateWorkspaceSlug(")
	if start < 0 {
		t.Fatalf("ValidateWorkspaceSlug is not in %s — the guard's anchor moved", slugRuleFile)
	}
	body := src[start:]
	end := strings.Index(body, "\n}\n")
	if end < 0 {
		t.Fatalf("could not find the end of ValidateWorkspaceSlug in %s", slugRuleFile)
	}
	body = body[:end]

	reservedAt := strings.Index(body, "urlguard.ReservedSubdomain(")
	shapeAt := strings.Index(body, slugShapeRule)
	if reservedAt < 0 || shapeAt < 0 {
		t.Fatalf("ValidateWorkspaceSlug no longer contains both legs (reserved at %d, shape at %d) "+
			"— this guard would otherwise pass on a function that checks neither", reservedAt, shapeAt)
	}
	if reservedAt > shapeAt {
		t.Errorf("ValidateWorkspaceSlug checks the shape rule before the reserved list. A reserved "+
			"label shorter than the minimum (\"s3\") would then be reported as malformed input, and the "+
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
	if len(found) == 0 {
		t.Fatalf("no file under api/ contains %q — the walk is not covering the tree", needle)
	}
	return found
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
