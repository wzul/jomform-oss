package mcpsrv

// Guard for the workspace purge's reachability CLAIM (card t_c295f96e).
//
// WHY THIS FILE EXISTS
//
// api/internal/httpapi/rest_workspaces.go said, beside purgeWorkspace:
//
//	// HUMAN-ONLY, BY CONSTRUCTION. The route is session-only (sessionOwnerID, an
//	// API key has no user) and there is deliberately NO MCP tool in front of it:
//	// ... assistant may move things to the bin and may never destroy them.
//
// Half of that was true and half of it was false. purge_workspace IS a
// registered MCP tool (tools.go), and a write-scoped API key on the owner's
// account reaches it: requireWriteScope + workspaceOwner resolve that caller to
// the account owner, and the store then enforces trashed-only + owner +
// confirm_slug + never-a-workspace-with-sales. Nothing on that path is a person.
// The same over-strong wording was repeated in four other places — the tool's
// own description ("ALAT MANUSIA"), delete_workspace's description ("itu manusia
// sahaja"), the delete tool's success note ("tindakan manusia sahaja"), the
// REST route comment, and — closest to the truth but still wrong — MCP-PARITY.md
// ("humans only").
//
// The guarantee the repo ACTUALLY enforces is narrower, and it is the one Wan's
// rule turns on: no permanent-destruction tool is ever PROPOSABLE to the in-app
// chat. chat_tools_guard_test.go (TestChatNeverProposesAPermanentDelete) pins
// that over every registered tool. So the honest statement is three-part, not
// one-part:
//
//	1. the ROUTE is session-only (an API key has no user, so a key cannot drive
//	   the REST route) — true before and after this change;
//	2. the TOOL is never proposable to the in-app chat — the real rule;
//	3. the tool IS reachable by an owner's MCP client — the same class
//	   purge_form / purge_product / purge_media have always been in.
//
// WHAT IS PINNED HERE, and why each assertion can fail
//
//   - The permanent-verb tool CLASS is enumerated from the package's own
//     registration literals (not a hand-written list), so a sixth purge tool has
//     to be seen by this guard, and a parse that stops seeing the literals fails
//     loudly instead of passing vacuously.
//   - None of those tools may be proposable to chat (the real rule), asserted
//     both ways: the predicate, and the registered-name sweep.
//   - No source file anywhere in api/ or web/src may carry the claim that was
//     corrected: "NO MCP tool in front of it" / "ALAT MANUSIA" / "tindakan
//     manusia sahaja" as a statement about the purge. Correcting the prose
//     without this half reproduces the defect in a nicer font — the next reader
//     believes the rule lives somewhere it does not.
//   - The corrected statements themselves must still be present, so the fix
//     cannot be silently undone in either direction.
//   - No web source may call DELETE /v1/workspaces/{id}/purge. That is what makes
//     "reached from the API or MCP, not by clicking" a fact rather than a hope;
//     wiring a dashboard control reds here, which is the point (see the carded
//     copy defect in web/src/components/i18nDict.ts, which still promises a
//     Trash-view step that does not exist — that one needs a product decision,
//     not a wording fix).
//
// This test reads SOURCE ONLY and needs no database, so it runs in the plain
// `go test ./...` pass.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// mcpToolLiteral pulls a registration's name and declared description out of the
// package source. Anchored on `&mcp.Tool{` rather than on `Name:` alone, so a
// Name/Description pair belonging to something other than a tool cannot be
// mistaken for one.
var mcpToolLiteral = regexp.MustCompile(
	`&mcp\.Tool\{\s*Name:\s*"([a-z0-9_]+)",\s*Description:\s*"((?:[^"\\]|\\.)*)"`)

// purgeVerbTools is the class this guard is about: registered tools whose NAME
// carries a permanent-destruction verb. The same fragment list
// chat_tools_guard_test.go sweeps with, so the two guards cannot disagree about
// what "permanent" means.
var purgeVerbFragments = []string{"purge", "destroy", "permanent", "erase", "wipe", "hard_delete"}

// expectedPurgeTools is the class as it stands today. Pinned exactly, not as a
// floor: the card that corrected the claim did so on the grounds that
// purge_workspace is one of an EXISTING class, so a membership change is what
// this guard must make loud. `purge_unverified_accounts` is the sweep, the other
// four are the bins.
var expectedPurgeTools = []string{
	"purge_form",
	"purge_media",
	"purge_product",
	"purge_unverified_accounts",
	"purge_workspace",
}

// falsePurgeClaims are the wordings that assert more than the repo enforces (or
// than the code does at all). Each was shipped, and the first three are the ones
// this card removed. A new one anywhere in api/ or web/src fails here.
var falsePurgeClaims = []string{
	"NO MCP tool in front of it", // the REST route comment (rest_workspaces.go)
	"ALAT MANUSIA",               // the tool description + its docs row
	"tindakan manusia sahaja",    // the delete tool's success note
}

// repoRootFromMcpsrv locates the repository root (api/internal/mcpsrv -> ../../..).
func repoRootFromMcpsrv(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "MCP-PARITY.md")); err != nil {
		t.Fatalf("expected the repo root at %s (MCP-PARITY.md not found there): %v — this guard "+
			"reads files across api/ and web/, so it must fail rather than check a subtree and pass", root, err)
	}
	return root
}

// purgeToolsFromRegistry reads every non-test .go file in this package and
// returns the registered tools whose name carries a permanent verb, with the
// file each literal came from.
func purgeToolsFromRegistry(t *testing.T) map[string]string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package dir %s: %v", dir, err)
	}
	found := map[string]string{}
	literals := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			t.Fatalf("read %s: %v", e.Name(), rerr)
		}
		for _, m := range mcpToolLiteral.FindAllStringSubmatch(string(raw), -1) {
			literals++
			name := m[1]
			lower := strings.ToLower(name)
			for _, frag := range purgeVerbFragments {
				if strings.Contains(lower, frag) {
					found[name] = e.Name()
					break
				}
			}
		}
	}
	// A floor on the PARSE, not on the answer: the registry is ~180 tools and the
	// registration shape is not about to change to something this regex cannot
	// see — if it does, the purge tools would silently drop out of `found` and the
	// sweep below would pass by having nothing to check.
	if literals < 100 {
		t.Fatalf("parsed %d `&mcp.Tool{` literal(s) from the non-test package files — the extractor "+
			"has drifted from the registration shape, so the purge sweep proves nothing", literals)
	}
	return found
}

// TestWorkspacePurgeClaimMatchesWhatTheRepoEnforces is the card's guard: the
// artifacts an auditor reads must state the rule the code actually enforces.
func TestWorkspacePurgeClaimMatchesWhatTheRepoEnforces(t *testing.T) {
	root := repoRootFromMcpsrv(t)
	purgeTools := purgeToolsFromRegistry(t)

	// 1. The class is the one the card reasoned about. Stated as a SET so a tool
	//    moving between files is not an off-by-one, and so a new member has to be
	//    acknowledged here — which is where the "is this still consistent with
	//    purge_form?" question gets asked.
	var names []string
	for n := range purgeTools {
		names = append(names, n)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != strings.Join(expectedPurgeTools, ",") {
		t.Fatalf("permanent-verb MCP tools = %v, want %v.\n"+
			"A tool added here is a new way to destroy data through MCP: decide deliberately, the same "+
			"way purge_workspace was decided, and correct this list (and MCP-PARITY.md) in the same "+
			"change. A tool MISSING from the list means the extractor stopped seeing its literal.",
			names, expectedPurgeTools)
	}

	// 2. The rule that IS enforced, asserted for every member: never proposable to
	//    the in-app chat. This is the guarantee the corrected comments name.
	for _, n := range names {
		if ChatToolIsProposable(n) {
			t.Errorf("ChatToolIsProposable(%q) = true — a permanent-destruction tool must never be "+
				"proposable to the chat. The comments now say the tool is reachable by an owner's MCP "+
				"client and NOT proposable by the chat; making the first half true does not license "+
				"weakening the second.", n)
		}
	}

	// 3. purge_workspace specifically: registered, and its description states the
	//    chat half of the rule rather than a humans-only claim.
	wsDesc := purgeWorkspaceDescription(t)
	if !strings.Contains(wsDesc, "chat") {
		t.Errorf("purge_workspace's description no longer says anything about the chat lane:\n  %q\n"+
			"An auditor has to be able to read WHICH half of the rule applies to this tool off the "+
			"artifact. The chat half is the real one.", wsDesc)
	}
	for _, bad := range falsePurgeClaims {
		if strings.Contains(wsDesc, bad) {
			t.Errorf("purge_workspace's description carries %q again — that asserts a reachability the "+
				"repo does not enforce (an owner's write-scoped API key reaches this tool).", bad)
		}
	}

	// 4. THE WHOLE TREE. The five over-strong sites were spread over api/ and
	//    web/src; a correction that lands in one file and not the others is the
	//    defect this guard exists to stop, so the scan is a walk, not a list.
	for _, bad := range falsePurgeClaims {
		for _, subtree := range []string{"api", filepath.Join("web", "src")} {
			for _, hit := range findInTree(t, root, subtree, bad) {
				t.Errorf("%s carries the corrected claim %q.\n"+
					"purge_workspace IS a registered MCP tool an owner's write-scoped key can call; what "+
					"holds is that the chat may never PROPOSE a permanent delete and that the REST route "+
					"is session-only. State that, or the next reader believes a rule the code does not "+
					"implement — which is the defect card t_c295f96e reported.", hit, bad)
			}
		}
	}

	// 5. And the corrected statements must still be there, so the fix cannot be
	//    quietly reverted: the route's comment has to carry both halves.
	restSrc := readRepoFile(t, root, "api/internal/httpapi/rest_workspaces.go")
	for _, want := range []string{"session-only", "NOT the case that no MCP tool exists"} {
		if !strings.Contains(restSrc, want) {
			t.Errorf("api/internal/httpapi/rest_workspaces.go's purge comment no longer contains %q.\n"+
				"The corrected comment's whole value is that it separates the half that holds (the route "+
				"is session-only; the chat can never propose the purge) from the half that does not (an "+
				"owner's MCP client does reach the tool). If that distinction is dropped, restore it — "+
				"do not delete this assertion to keep a run green.", want)
		}
	}

	// 6. "Reached from the API or MCP, not by clicking" has to stay a fact. No web
	//    source may call the workspace purge route; wiring a dashboard control is a
	//    deliberate change that must edit this guard (and the docs sentence that
	//    tells a merchant how the permanent step is reached).
	for _, hit := range findInTree(t, root, filepath.Join("web", "src"), "/purge") {
		if !strings.Contains(hit, "workspaces/") {
			continue
		}
		t.Errorf("%s calls the workspace purge route from the web tree.\n"+
			"It did not, and the docs now say the permanent step is reached through the REST API or an "+
			"MCP client acting for the owner — never from a dashboard control. If a control is being "+
			"added, that is a deliberate decision: update the docs sentence and this guard together.", hit)
	}
}

// purgeWorkspaceDescription returns the registered description of
// purge_workspace, failing if the literal cannot be found (a description read
// from nowhere would make the assertions above pass vacuously).
func purgeWorkspaceDescription(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			t.Fatalf("read %s: %v", e.Name(), rerr)
		}
		for _, m := range mcpToolLiteral.FindAllStringSubmatch(string(raw), -1) {
			if m[1] == "purge_workspace" {
				return m[2]
			}
		}
	}
	t.Fatal("purge_workspace's registration literal was not found in the non-test package sources — " +
		"the tool this guard is about is gone or its shape changed, so every description assertion " +
		"here would pass vacuously")
	return ""
}

// skippedScanDirs are trees a source walk must not descend into: build output,
// dependencies, and the git metadata of the checkout.
var skippedScanDirs = map[string]bool{
	".git": true, "node_modules": true, ".next": true, ".worktrees": true,
}

// falseClaimExempt names the files allowed to CONTAIN a false claim, with the
// reason. The only legitimate reason is a CORRECTIVE one: the comment that fixes
// a false statement has to be able to name the statement it fixes, otherwise the
// next reader cannot tell which sentence changed or why. (The guard's own header
// quotes the old comment for exactly that reason, and the repo's earlier
// doc-comment guard does the same.)
//
// This is not a blanket exemption: TestFalseClaimExemptionsStayHonest fails if an
// entry stops being needed, so it cannot rot into a place to park a live claim.
var falseClaimExempt = map[string]string{
	"api/internal/httpapi/rest_workspaces.go": "the corrected comment names the claim it replaced " +
		"(\"there is deliberately NO MCP tool in front of it\", which was false) so a reader can see " +
		"what changed; the correction itself is asserted separately below",
	"api/internal/mcpsrv/purge_reachability_claim_test.go": "this guard's own header quotes the " +
		"removed comment, which is the defect it documents",
}

// TestFalseClaimExemptionsStayHonest keeps the exemption map from rotting: every
// entry must be a real file that still needs its exemption. Deleting a corrective
// comment therefore forces its entry out, and an entry cannot be parked on a file
// that no longer contains the claim.
func TestFalseClaimExemptionsStayHonest(t *testing.T) {
	root := repoRootFromMcpsrv(t)
	for rel, reason := range falseClaimExempt {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("%s is exempted with the reason %q, but the file cannot be read: %v — "+
				"delete the entry or fix the path", rel, reason, err)
			continue
		}
		still := false
		for _, bad := range falsePurgeClaims {
			if strings.Contains(string(raw), bad) {
				still = true
				break
			}
		}
		if !still {
			t.Errorf("%s no longer contains any of the corrected claims, so its exemption is stale "+
				"(reason: %q). Remove the entry — an exemption kept after the claim is gone is how a "+
				"live claim later hides behind a dead exception.", rel, reason)
		}
	}
}

// findInTree walks root/subtree and returns one entry per matching line, as
// "relpath:line: <content>" — the content is included because callers need to
// look at WHAT matched (the purge-route check asserts on the URL in the line) and
// because a failure message that shows the sentence is worth more than a line
// number. `relpath` is RELATIVE TO THE REPO ROOT, which is what falseClaimExempt
// is keyed by.
//
// A walk rather than a file list on purpose: the claim this guard is about was
// spread over five files in two trees, and a list would have to be updated by
// the very change that introduces a sixth.
func findInTree(t *testing.T, root, subtree, needle string) []string {
	t.Helper()
	dir := filepath.Join(root, subtree)
	var out []string
	werr := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skippedScanDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		switch ext {
		case ".go", ".ts", ".tsx", ".md", ".js", ".sql":
		default:
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		if _, exempt := falseClaimExempt[rel]; exempt {
			return nil
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if strings.Contains(line, needle) {
				out = append(out, rel+":"+itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if werr != nil {
		t.Fatalf("walk %s: %v", dir, werr)
	}
	sort.Strings(out)
	return out
}

// itoa is the package's own helper (form_notifications_mcp.go); this test is
// in-package, so the line numbers in failure messages use it rather than pulling
// in a second copy of the same three-line function.

// readRepoFile reads a path relative to the repo root, failing loudly rather
// than returning "" — an empty read would make a containment assertion pass for
// the wrong reason.
func readRepoFile(t *testing.T, root, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read %s: %v — this guard cannot check a file it cannot read", rel, err)
	}
	return string(raw)
}
