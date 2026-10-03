package mcpsrv

// Guard for the workspace purge's reachability CLAIM (card t_c295f96e; tree
// coverage tightened by card t_5a89de2c).
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
// ("humans only"). The docs/workspaces page carried a sixth in the same class
// ("offered to people only").
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
//   - No source file in `api/`, `web/src` OR the root `MCP-PARITY.md` may carry
//     any of the wordings that assert more than the repo enforces, as a
//     statement about the purge (falsePurgeClaims), matched case-insensitively.
//     The parity file was outside the first cut's walk entirely, and the same
//     list is matched against the purge tool's registered description.
//   - The corrected statements themselves must still be present, at every site
//     that carries one (correctedClaimSites), so the fix cannot be silently
//     undone in either direction.
//   - No web source may call DELETE /v1/workspaces/{id}/purge. That is what makes
//     "reached from the API or MCP, not by clicking" a fact rather than a hope;
//     wiring a dashboard control reds here, which is the point.
//   - TestTheClaimScanBites puts each removed wording back INTO the real file's
//     content, in memory (nothing on disk is written), and requires the scan to go
//     RED naming file:line. A guard whose bite proof lives only in a shell harness
//     can rot with every gate green; this one bites inside the gate that runs it.
//
// WHY THE EXEMPTION IS A QUOTATION RULE, NOT A FILE- OR REGION-WIDE ONE (card
// t_5a89de2c, leg 1)
//
// The first cut of this guard exempted whole FILES from the scan (falseClaimExempt
// keyed by path, findInTree returning early), and that was measured to be exactly
// as weak as it sounds: a fresh live claim appended to rest_workspaces.go — the
// file that CARRIED the original defect — left the guard green, because the file's
// one corrective sentence bought silence for every other line in it.
//
// So a file may now only QUOTE the strings it declares, and a match is excused
// only when that occurrence sits INSIDE DOUBLE QUOTES on its line: the corrective
// comment names the claim it replaced ("there is deliberately NO MCP tool in
// front of it", which was false), and naming it is precisely what the quotes do.
// An unquoted claim is live even when it sits in an exempt file and even when it
// sits in the same comment block — which is what makes the card's own probe (a
// fresh claim appended to rest_workspaces.go) and its five reverts all fail here
// instead of passing.
//
// The residual, stated rather than implied: the whole-file form survives for
// exactly ONE path — this guard's own header, which is the catalogue of the
// removed wordings and cannot be written without printing them. Every other path
// with a whole-file entry fails TestFalseClaimExemptionsStayHonest, a declared
// quote that leaves the file fails it too, and TestClaimExemptionQuotesDoNotBlind-
// AFile pins both directions of the quotation rule against the real files. That
// combination is what keeps an exemption from rotting into a place to park a claim.
//
// KNOWN OPEN, DELIBERATELY NOT FIXED HERE (card t_2a686ada): web/src/components/
// i18nDict.ts still tells the merchant the permanent delete of a workspace "lives
// in the Trash view" / "is a separate, human-only step in the Trash view", which is
// a CAPABILITY the dashboard does not have (the Trash section offers Restore
// only, deliberately — see WorkspaceTab.tsx's header). That one needs a product
// decision, not a wording fix, so it is not asserted either way here; the docs
// sentence it contradicts IS pinned below.
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

// doubleQuotedSpan captures a double-quoted span on a line — how a corrective
// comment NAMES the claim it replaced.
var doubleQuotedSpan = regexp.MustCompile(`"([^"]*)"`)

// purgeVerbFragments is the class this guard is about: registered tools whose NAME
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

// falsePurgeClaims are the wordings that assert more about the purge than the
// repo enforces (or than the code does at all), matched CASE-INSENSITIVELY: the
// defect shipped in more than one casing (rest_workspaces.go's shouting "NO MCP
// tool in front of it" beside rest.go's lowercase copy), and a scan that only saw
// the shouting version was a measured hole (card t_5a89de2c, leg 3).
//
// This is the FIVE SITES' OWN WORDINGS, not three of them: the pre-fix wording
// differed per site, and pinning only the three that happened to be caught left
// four of the five reverting GREEN. Each entry names where it was found, so a new
// offender can be told apart from a reworded old one.
//
// What is deliberately NOT in the list: "chat tidak pernah boleh mencadangkan"
// on its own. That was the TAIL of purge_workspace's pre-fix description, but the
// corrected delete_workspace note (mcpsrv/workspaces.go, the tool's own result
// text) uses the same words to state the rule correctly — the false half of that
// site was the "ALAT MANUSIA" framing, which is pinned, so adding the tail would
// flag a sentence that is right. For the same reason the plain-English form is
// pinned as "only a person can (ever) PURGE" rather than "only a person can …":
// the ROUTE really is session-only, so "only a person can drive this route" is a
// true sentence this scan must leave alone.
var falsePurgeClaims = []string{
	"no MCP tool in front of it",     // rest_workspaces.go + rest.go — the route comment
	"human-only, by construction",    // rest_workspaces.go — the comment's original opener
	"human-only, enforced elsewhere", // dbstore/workspace.go — the store's original opener
	"alat manusia",                   // purge_workspace's description + its /docs/mcp row
	"tindakan manusia sahaja",        // delete_workspace's success note
	"dan itu manusia sahaja",         // delete_workspace's description, pre-fix tail
	"humans only",                    // MCP-PARITY.md
	"offered to people only",         // web/src/app/docs/workspaces/page.tsx
	"only a person can purge",        // the plain-English claim (dbstore/workspace.go)
	"only a person can ever purge",   // the plain-English claim (rest_workspaces.go)
}

// purgeClaimTrees are the trees the claim scan walks. MCP-PARITY.md is a FILE,
// not a tree, and it is named here because it carried one of the wordings and the
// first cut of this guard never reached the repo root at all — its walk was `api`
// and `web/src` only, so the root parity file was unscanned (card t_5a89de2c,
// leg 2).
var purgeClaimTrees = []string{"api", filepath.Join("web", "src"), "MCP-PARITY.md"}

// claimExemption is why ONE file may contain a banned wording, and how.
type claimExemption struct {
	reason string
	// quotes are the banned wordings this file is allowed to QUOTE — it must
	// still contain each of them (TestFalseClaimExemptionsStayHonest), and a
	// match is excused only when it is declared here AND enclosed in double
	// quotes on its line. nil means "the whole file is a quotation", reserved for
	// exactly one path (this guard's own header) and refused anywhere else.
	quotes []string
}

// falseClaimExempt names the files allowed to CONTAIN a banned wording, with the
// reason and the declared quotations. The only legitimate reason is a CORRECTIVE
// one: the comment that fixes a false statement has to be able to NAME the
// statement it fixes, otherwise the next reader cannot tell which sentence
// changed or why.
var falseClaimExempt = map[string]claimExemption{
	"api/internal/httpapi/rest_workspaces.go": {
		reason: "the corrected comment quotes the claim it replaced (\"there is " +
			"deliberately NO MCP tool in front of it\", which was false) and the " +
			"plain-English reading of it, so a reader can see what changed; the " +
			"correction itself is asserted in correctedClaimSites",
		quotes: []string{"no MCP tool in front of it", "only a person can ever purge"},
	},
	"api/internal/dbstore/workspace.go": {
		reason: "the corrected block quotes the opener it replaced (\"HUMAN-ONLY, " +
			"ENFORCED ELSEWHERE\") and reads it back as \"only a person can purge\", " +
			"which is the claim that is NOT enforced",
		quotes: []string{"human-only, enforced elsewhere", "only a person can purge"},
	},
	"api/internal/mcpsrv/purge_reachability_claim_test.go": {
		reason: "this guard's own header is the catalogue of the wordings that were " +
			"removed — a list of what the repo may not say, which cannot be written " +
			"without printing it",
	},
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
			"reads files across api/, web/src and the repo root, so it must fail rather than check "+
			"a subtree and pass", root, err)
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
			"change. A tool MISSING from the list means the extractor stopped seeing its literal.\n"+
			"BITE ANCHOR 3 (card t_5a89de2c): this is what a sixth purge-verb tool fires.",
			names, expectedPurgeTools)
	}

	// 2. The rule that IS enforced, asserted for every member: never proposable to
	//    the in-app chat. This is the guarantee the corrected comments name.
	for _, n := range names {
		if ChatToolIsProposable(n) {
			t.Errorf("ChatToolIsProposable(%q) = true — a permanent-destruction tool must never be "+
				"proposable to the chat. The comments now say the tool is reachable by an owner's MCP "+
				"client and NOT proposable by the chat; making the first half true does not license "+
				"weakening the second.\n"+
				"BITE ANCHOR 1 (card t_5a89de2c): this is what putting a purge-verb tool into "+
				"chatProposableTools fires.", n)
		}
	}

	// 3. purge_workspace specifically: registered, and its description states the
	//    chat half of the rule rather than a humans-only claim.
	wsDesc := purgeWorkspaceDescription(t)
	if !containsFold(wsDesc, "chat") {
		t.Errorf("purge_workspace's description no longer says anything about the chat lane:\n  %q\n"+
			"An auditor has to be able to read WHICH half of the rule applies to this tool off the "+
			"artifact. The chat half is the real one.", wsDesc)
	}
	for _, bad := range falsePurgeClaims {
		if containsFold(wsDesc, bad) {
			t.Errorf("purge_workspace's description carries %q again — that asserts a reachability the "+
				"repo does not enforce (an owner's write-scoped API key reaches this tool).", bad)
		}
	}

	// 4. THE WHOLE TREE. The over-strong sites were spread over api/, web/src and
	//    the repo root; a correction that lands in one file and not the others is
	//    the defect this guard exists to stop, so the scan is a walk, not a list.
	for _, subtree := range purgeClaimTrees {
		for _, hit := range findInTree(t, root, subtree) {
			t.Errorf("%s carries the corrected claim %q.\n"+
				"purge_workspace IS a registered MCP tool an owner's write-scoped key can call; what "+
				"holds is that the chat may never PROPOSE a permanent delete and that the REST route "+
				"is session-only. State that, or the next reader believes a rule the code does not "+
				"implement — which is the defect card t_c295f96e reported.", hit.String(), hit.needle)
		}
	}

	// 5. And the corrected statements must still be there, at every site that
	//    carries one, so the fix cannot be quietly reverted in either direction.
	//    Each want is short and lives on ONE line of the file, so a rewrap is a
	//    deliberate edit rather than a silent red.
	for _, site := range correctedClaimSites {
		src := readRepoFile(t, root, site.file)
		for _, want := range site.wants {
			if !strings.Contains(src, want) {
				t.Errorf("%s no longer contains %q.\n%s\n"+
					"If the sentence was reworded, pin the new wording here in the same change — do NOT "+
					"delete this assertion to keep a run green.", site.file, want, site.why)
			}
		}
	}

	// 6. "Reached from the API or MCP, not by clicking" has to stay a fact. No web
	//    source may call the workspace purge route; wiring a dashboard control is a
	//    deliberate change that must edit this guard (and the docs sentence that
	//    tells a merchant how the permanent step is reached).
	for _, hit := range webWorkspacePurgeCallers(t, root) {
		t.Errorf("%s calls the workspace purge route from the web tree.\n"+
			"It did not, and the docs now say the permanent step is reached through the REST API or an "+
			"MCP client acting for the owner — never from a dashboard control. If a control is being "+
			"added, that is a deliberate decision: update the docs sentence, the copy that still "+
			"promises a Trash-view step (card t_2a686ada) and this guard together.", hit)
	}
}

// correctedClaimSites are the artifacts that must state the corrected, three-part
// rule, with the short strings that prove each one still does. Naming the site
// here (rather than only asserting the absence of the old wording) is what makes
// a silent DELETION of the correction a red rather than a pass.
var correctedClaimSites = []struct {
	file  string
	wants []string
	why   string
}{
	{
		file:  "api/internal/httpapi/rest_workspaces.go",
		wants: []string{"session-only", "NOT the case that no MCP tool exists"},
		why: "The corrected comment's whole value is that it separates the half that holds (the route " +
			"is session-only; the chat can never propose the purge) from the half that does not (an " +
			"owner's MCP client does reach the tool).",
	},
	{
		file:  "api/internal/httpapi/rest.go",
		wants: []string{"never proposable to the in-app chat", "IS a registered MCP tool"},
		why: "The route table's own comment is the first thing a reader of the REST surface sees; if it " +
			"drops the distinction, the route's long comment cannot carry it alone.",
	},
	{
		file:  "api/internal/dbstore/workspace.go",
		wants: []string{"NOT PROPOSABLE TO THE IN-APP CHAT", "REACHABLE BY MCP"},
		why: "The store half is where an API key actually lands, so the claim most needs to be right " +
			"beside Purge().",
	},
	{
		file:  "api/internal/mcpsrv/tools.go",
		wants: []string{"TIDAK PERNAH DICADANGKAN OLEH CHAT", "ia alat MCP untuk pemilik"},
		why: "purge_workspace's registered description is what an MCP client's model reads; it must " +
			"tell the owner the tool is for them and that the in-app chat may never propose it.",
	},
	{
		file:  "web/src/app/docs/mcp/page.tsx",
		wants: []string{"TIDAK PERNAH DICADANGKAN OLEH CHAT"},
		why:   "The tool reference is the merchant-facing copy of the same description.",
	},
	{
		file: "web/src/app/docs/workspaces/page.tsx",
		wants: []string{
			"never by clicking a dashboard control",
			"can never propose",
		},
		why: "The docs sentence is what makes \"reached from the API or MCP\" a stated fact; it is also " +
			"the sentence that goes stale if a Trash-view control is ever built (card t_2a686ada).",
	},
	{
		file:  "MCP-PARITY.md",
		wants: []string{"never proposable to chat", "write-scoped API key can call"},
		why: "The root parity file is one of the five sites' neighbours and had no guard at all until " +
			"card t_5a89de2c added the repo root to the walk.",
	},
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

// claimMatch is one line of one file that carries a banned wording.
type claimMatch struct {
	file   string // repo-relative, slash-separated
	line   int    // 1-based
	text   string // the line, trimmed
	needle string // which entry of falsePurgeClaims matched
}

func (m claimMatch) String() string {
	return m.file + ":" + itoa(m.line) + ": " + m.text
}

// findInTree walks root/subtree, applies the claim rules to every source file it
// finds, and returns the matches sorted by path. `subtree` may be a FILE (the
// repo-root MCP-PARITY.md), which is why the walk is not a directory walk.
//
// A walk rather than a file list on purpose: the claim this guard is about was
// spread over files in three places, and a list would have to be updated by the
// very change that introduces a fourth.
func findInTree(t *testing.T, root, subtree string) []claimMatch {
	t.Helper()
	dir := filepath.Join(root, subtree)
	var out []claimMatch
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
		if !scannedSourceExt(filepath.Ext(path)) {
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
		out = append(out, scanText(filepath.ToSlash(rel), string(raw))...)
		return nil
	})
	if werr != nil {
		t.Fatalf("walk %s: %v", dir, werr)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// scannedSourceExt is the extension set the claim scan reads: sources, the docs
// pages, the sql and the markdown the claims shipped in.
func scannedSourceExt(ext string) bool {
	switch ext {
	case ".go", ".ts", ".tsx", ".md", ".js", ".sql":
		return true
	}
	return false
}

// normText is a file's COMMENT prose, with consecutive comment lines joined by a
// single space. A claim the author WRAPPED across two comment lines is one phrase
// here instead of two half-phrases — which is what the card's own probe is, and
// what a line-at-a-time scan silently missed (measured: an earlier cut of this fix
// went green on exactly the probe the card was filed for).
//
// Each normalised line keeps its physical line number and the offset its content
// starts at inside that physical line, so a match is reported as file:line and the
// quotation rule can still look at the real line it came from. Non-comment lines
// are dropped rather than folded, so two unrelated code lines cannot be glued into
// a sentence nobody wrote; the every-line pass (scanText) covers those.
type normText struct {
	text       string
	starts     []int // normalised offset where each normalised line's content begins
	physLine   []int // 1-based physical line number of that content
	physOffset []int // byte offset of that content within its physical line
}

// foldableMarkers are the leading tokens that make a line COMMENT prose.
// `//` and `*` cover the Go/TS/TSX comments the defect shipped in.
var foldableMarkers = []string{"//", "*"}

// mdStructural are the markdown prefixes that START something rather than
// continue a paragraph, so they must not be folded into the line above.
var mdStructural = []string{"#", "-", "*", "+", "|", ">", "```", "    "}

// foldable reports whether a trimmed line is prose that may be folded with the
// prose above it: a comment line (or a markdown paragraph continuation).
func foldable(ext, trimmed string) bool {
	if trimmed == "" {
		return false // a blank line ends the run
	}
	if ext == ".md" {
		for _, m := range mdStructural {
			if strings.HasPrefix(trimmed, m) {
				return false
			}
		}
		// An ordered-list item ("1. ...") starts something too.
		if len(trimmed) > 2 && trimmed[0] >= '0' && trimmed[0] <= '9' {
			if i := strings.Index(trimmed, "."); i > 0 && i <= 3 {
				return false
			}
		}
		return true
	}
	for _, m := range foldableMarkers {
		if strings.HasPrefix(trimmed, m) {
			return true
		}
	}
	return false
}

// stripMarkers removes the leading comment markers from an already-trimmed line,
// so the folded text is the prose and not `// //`.
func stripMarkers(trimmed string) string {
	for {
		changed := false
		for _, m := range []string{"//", "*"} {
			if strings.HasPrefix(trimmed, m) {
				trimmed = strings.TrimSpace(trimmed[len(m):])
				changed = true
			}
		}
		if !changed {
			return trimmed
		}
	}
}

// newNormText builds the folded comment-prose view of one file.
func newNormText(ext, raw string) normText {
	var nt normText
	var b strings.Builder
	prevFold := false
	for i, ln := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(ln)
		if !foldable(ext, trimmed) {
			prevFold = false
			continue
		}
		if prevFold {
			b.WriteString(" ")
		} else if b.Len() > 0 {
			b.WriteString("\n")
		}
		content := stripMarkers(trimmed)
		nt.starts = append(nt.starts, b.Len())
		nt.physLine = append(nt.physLine, i+1)
		off := strings.Index(ln, content)
		if off < 0 {
			off = 0
		}
		nt.physOffset = append(nt.physOffset, off)
		b.WriteString(content)
		prevFold = true
	}
	nt.text = b.String()
	return nt
}

// locate maps a normalised offset back to the physical line it came from and the
// byte offset inside that physical line, so the quotation rule can be applied to
// the REAL line and a failure can name file:line.
func (nt normText) locate(normOff int) (physLine, physOff int, ok bool) {
	i := sort.Search(len(nt.starts), func(k int) bool { return nt.starts[k] > normOff }) - 1
	if i < 0 {
		return 0, 0, false
	}
	return nt.physLine[i], nt.physOffset[i] + (normOff - nt.starts[i]), true
}

// scanText applies the claim rules to ONE file's content, keyed by its
// repo-relative path (which is what the exemption map is keyed by).
//
// Two passes, UNIONed, because neither alone is enough:
//
//   - EVERY LINE, so a claim inside a string literal is seen (the tool
//     descriptions in tools.go are single long lines, not comments — a
//     comment-only scan missed them entirely when this was tried);
//   - the FOLDED COMMENT PROSE, so a claim the author wrapped across two comment
//     lines is one phrase instead of two half-phrases (rest_workspaces.go's own
//     probe, and the card's measured hole).
//
// It takes content rather than a path so TestTheClaimScanBites can hand it the
// REAL file's body with one wording put back, or with a fresh claim appended —
// the real file's exemption rules included — and check the scan goes red without
// any file on disk being touched. That is what keeps this guard's bite proof
// inside the gate that runs it, instead of in a shell harness nobody re-runs.
func scanText(rel, raw string) []claimMatch {
	ex, exempt := falseClaimExempt[rel]
	var out []claimMatch
	seen := map[string]bool{}

	record := func(line, off int, needle string) {
		key := needle + "|" + itoa(line)
		if seen[key] {
			return
		}
		if exempt && claimExcused(ex, raw, line, off, needle) {
			return
		}
		seen[key] = true
		out = append(out, claimMatch{
			file:   rel,
			line:   line,
			text:   lineText(raw, line),
			needle: needle,
		})
	}

	// Pass 1: every physical line, wherever the wording sits on it.
	for i, line := range strings.Split(raw, "\n") {
		low := strings.ToLower(line)
		for _, bad := range falsePurgeClaims {
			if idx := strings.Index(low, strings.ToLower(bad)); idx >= 0 {
				record(i+1, idx, bad)
			}
		}
	}

	// Pass 2: the folded comment prose, for a claim wrapped over a line break.
	nt := newNormText(filepath.Ext(rel), raw)
	lower := strings.ToLower(nt.text)
	for _, bad := range falsePurgeClaims {
		needle := strings.ToLower(bad)
		at := 0
		for {
			idx := strings.Index(lower[at:], needle)
			if idx < 0 {
				break
			}
			start := at + idx
			at = start + len(needle)
			if line, off, ok := nt.locate(start); ok {
				record(line, off, bad)
			}
		}
	}
	return out
}

// claimExcused reports whether a banned wording found on physical line `line` at
// byte offset `off` within it is a QUOTATION rather than a live claim.
//
// Three conditions, all needed: the file is exempt at all; the wording is
// DECLARED by that file's exemption (so a new quoted wording is a red until
// somebody writes down why); and the occurrence sits inside double quotes —
// which is the whole mechanism, because naming the claim you replaced is the only
// reason a correction needs to print it.
//
// ONLY the quoted span matters, and only a match that STARTS inside it counts: a
// quotation of one wording must not excuse a different banned wording sitting on
// the same line or elsewhere in the same comment block, which a containment test
// on the line (or on the whole text) would do.
func claimExcused(ex claimExemption, raw string, line, off int, needle string) bool {
	if ex.quotes == nil {
		return true // the whole-file form; exactly one path is allowed to hold it
	}
	declared := false
	for _, q := range ex.quotes {
		if strings.EqualFold(q, needle) {
			declared = true
			break
		}
	}
	if !declared {
		return false
	}
	lines := strings.Split(raw, "\n")
	if line < 1 || line > len(lines) {
		return false
	}
	// THE OFFSET IS A FOLDED OFFSET, so the quotation test must read a FOLDED line.
	//
	// Both passes search a case-folded haystack and hand the folded offset here. Go's
	// simple case mapping is NOT length-preserving for every rune: `\u0130` folds to a
	// SHORTER string, so a folded offset points EARLIER than the same position in the
	// raw line by one byte per such rune. Testing the raw line with it can land a match
	// INSIDE a quoted span that does not contain it, so a live unquoted claim after a
	// quotation is EXCUSED. Measured, before this fix: on
	// `// \u0130\u0130\u0130 "no MCP tool in front of it" only a person can ever purge`,
	// the live `only a person can ever purge` was not reported at all, though the header
	// states "a match is excused only when that occurrence sits INSIDE DOUBLE QUOTES on
	// its line" (card t_89c4d70b). Folding here keeps the offset in the coordinates it
	// was computed in; doubleQuotedSpan only looks for `"`, whose folded positions are
	// unchanged, so the rule it applies is the same — and the raw line is still kept for
	// failure MESSAGES, which need the reader's text rather than the folded form.
	text := strings.ToLower(lines[line-1])
	if off < 0 || off >= len(text) {
		return false
	}
	for _, m := range doubleQuotedSpan.FindAllStringSubmatchIndex(text, -1) {
		// m[0], m[1] are the span including the quotes; m[2], m[3] the inside.
		if off >= m[0] && off < m[1] {
			return true
		}
	}
	return false
}

// lineText returns the trimmed text of a 1-based line, for failure messages.
func lineText(raw string, line int) string {
	lines := strings.Split(raw, "\n")
	if line < 1 || line > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[line-1])
}

// containsFold is strings.Contains for callers that do not care about casing —
// the claim defect shipped in more than one casing.
func containsFold(hay, needle string) bool {
	return strings.Contains(strings.ToLower(hay), strings.ToLower(needle))
}

// TestFalseClaimExemptionsStayHonest keeps the exemption map from rotting: every
// entry must be a real, non-empty file whose exemption is still needed, a
// declared quotation must still be in the file (the exemption's whole basis), and
// the whole-file form is allowed for exactly one path.
func TestFalseClaimExemptionsStayHonest(t *testing.T) {
	root := repoRootFromMcpsrv(t)
	for rel, ex := range falseClaimExempt {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("%s is exempted with the reason %q, but the file cannot be read: %v — "+
				"delete the entry or fix the path", rel, ex.reason, err)
			continue
		}
		if len(raw) == 0 {
			t.Errorf("%s is exempted but is empty — an exemption on an empty file excuses nothing", rel)
			continue
		}
		if ex.quotes == nil {
			if rel != "api/internal/mcpsrv/purge_reachability_claim_test.go" {
				t.Errorf("%s carries a WHOLE-FILE exemption, which is reserved for this guard's own "+
					"header (the catalogue of the removed wordings). A whole-file exemption on a source "+
					"file is the exact hole card t_5a89de2c closed: it bought silence for every line the "+
					"corrective sentence did not occupy. Declare the strings the file may QUOTE instead.", rel)
				continue
			}
			still := false
			for _, bad := range falsePurgeClaims {
				if containsFold(string(raw), bad) {
					still = true
					break
				}
			}
			if !still {
				t.Errorf("%s no longer contains any of the corrected claims, so its exemption is stale "+
					"(reason: %q). Remove the entry — an exemption kept after the claim is gone is how a "+
					"live claim later hides behind a dead exception.", rel, ex.reason)
			}
			continue
		}
		for _, q := range ex.quotes {
			if !containsFold(string(raw), q) {
				t.Errorf("%s is exempted for the quoted string %q, but no longer contains it. The "+
					"quotation is the exemption's whole basis — with it gone the entry excuses nothing, "+
					"and a live claim could later sit behind it. Either restore the corrective quotation "+
					"or remove the entry.", rel, q)
			}
		}
	}
}

// TestClaimExemptionQuotesDoNotBlindAFile is the FOLLOW-UP guard (card
// t_5a89de2c, leg 1). It proves the exemption MECHANISM — not just its prose —
// keeps an exempt file's other lines, and its unquoted lines, in the scan.
//
// Two directions, both over the real files, both read-only:
//
//  1. a fresh live claim appended anywhere in an exempt file must be SEEN. That is
//     the card's own measured probe: appending it to rest_workspaces.go left the
//     first cut green.
//  2. the quotation marks are what buy the exemption, so REMOVING them from the
//     corrective line makes that claim live and it must be reported. Without this
//     direction, a rule of "this file is quiet near a quote" would pass direction
//     1 and still hide a live claim sitting in the corrective comment block.
func TestClaimExemptionQuotesDoNotBlindAFile(t *testing.T) {
	root := repoRootFromMcpsrv(t)

	// The card's probe, verbatim: a fresh claim, unquoted, in the file that
	// carried the original defect.
	const freshClaim = "// PURGE IS HUMAN-ONLY: this route is session-only and there is deliberately " +
		"NO MCP tool in front of it, so only a person can ever destroy a workspace."

	checked := 0
	for rel, ex := range falseClaimExempt {
		if ex.quotes == nil {
			continue // the whole-file form is asserted in TestFalseClaimExemptionsStayHonest
		}
		checked++
		raw := readRepoFile(t, root, rel)

		// 1. appended, unquoted, far from the corrective block.
		injected := raw + "\n" + freshClaim + "\n"
		hits := scanText(rel, injected)
		if len(hits) == 0 {
			t.Errorf("%s: a fresh unquoted claim appended to the file was NOT reported.\n"+
				"The file-wide exemption this card removed did exactly that; the quotation rule must "+
				"keep every line outside a quotation in the scan.", rel)
		} else {
			last := len(strings.Split(raw, "\n"))
			if !namesTheLine(hits, last+1) {
				t.Errorf("%s: the appended claim was reported at the wrong place (%v) — the scan must "+
					"name the line the wording is on", rel, hits)
			}
		}

		// 2. the SAME block with its quotation marks stripped: now live.
		for _, q := range ex.quotes {
			line := correctiveLine(raw, q)
			if line == "" {
				t.Errorf("%s: %q is declared as a quotation but no line carries it — the exemption's "+
					"basis cannot be located", rel, q)
				continue
			}
			unquoted := strings.Replace(raw, line, strings.ReplaceAll(line, "\"", ""), 1)
			if strings.Contains(unquoted, line) {
				t.Errorf("%s: could not strip the quotation marks from the corrective line for %q", rel, q)
				continue
			}
			if len(scanText(rel, unquoted)) == 0 {
				t.Errorf("%s: with the quotation marks removed from the line that names %q the scan "+
					"reported NOTHING. Quoting is the only thing that lets this file print the wording — "+
					"the same sentence unquoted is a live over-strong claim and must be reported.", rel, q)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no file has a declared-quotation exemption, so this guard proves nothing — the " +
			"whole-file exemption this card removed would be back the moment the map empties")
	}
}

// correctiveLine returns the first line of raw that contains q, or "".
func correctiveLine(raw, q string) string {
	for _, line := range strings.Split(raw, "\n") {
		if containsFold(line, q) && strings.Contains(line, "\"") {
			return line
		}
	}
	return ""
}

// namesTheLine reports whether any match is on the given 1-based line.
func namesTheLine(hits []claimMatch, line int) bool {
	for _, h := range hits {
		if h.line == line {
			return true
		}
	}
	return false
}

// TestTheClaimScanBites is the guard's own bite proof, run inside the gate that
// already runs this file.
//
// WHY IT LIVES HERE. The other bite proofs in this repo are shell harnesses that
// mutate a file, run a guard and restore it — deliberately on demand, because each
// case costs a full package test. This guard's subject is TEXT, so every case here
// is read-only: the REAL file's body is loaded, the removed wording is put back IN
// MEMORY at the line it shipped on, and the scan is required to go RED naming
// file:line. Nothing on disk is written, so this costs milliseconds and runs on
// every merge instead of waiting for somebody to re-run a harness.
//
// The wordings are the five sites' own pre-fix text, lifted from the revisions
// this card was filed against (ca110813 / 7d0489b3), so a case going quiet means
// THIS GUARD went blind — not that the wording changed.
func TestTheClaimScanBites(t *testing.T) {
	root := repoRootFromMcpsrv(t)

	// --- the real sites, each with its own pre-fix wording -------------------
	type siteProbe struct {
		label  string
		file   string
		now    string // the corrected sentence as it stands today (the anchor)
		before string // the wording that shipped before the correction
	}
	sites := []siteProbe{
		{
			label: "rest_workspaces.go — a fresh claim appended (the card's own probe)",
			file:  "api/internal/httpapi/rest_workspaces.go",
			now:   "// A LIVE workspace is a 404 here: the only way in is trash → purge, enforced in",
			before: "// A LIVE workspace is a 404 here: the only way in is trash → purge, enforced in\n" +
				"// PURGE IS HUMAN-ONLY: this route is session-only and there is deliberately NO MCP\n" +
				"// tool in front of it, so only a person can ever destroy a workspace.",
		},
		{
			label: "rest_workspaces.go — the comment's original opener",
			file:  "api/internal/httpapi/rest_workspaces.go",
			now:   "// WHAT IS HUMAN-ONLY HERE, PRECISELY (card t_c295f96e). This comment used to",
			before: "// HUMAN-ONLY, BY CONSTRUCTION. The route is session-only (sessionOwnerID, an\n" +
				"// API key has no user) and there is deliberately NO MCP tool in front of it:",
		},
		{
			label: "dbstore/workspace.go — the store's original opener",
			file:  "api/internal/dbstore/workspace.go",
			now:   "// WHAT IS HUMAN-ONLY, PRECISELY (card t_c295f96e). This block used to open",
			before: "// HUMAN-ONLY, ENFORCED ELSEWHERE. `purge` is deliberately NOT reachable from the\n" +
				"// dashboard UI or from a proposable chat tool, so only a person can purge.",
		},
		{
			label: "rest.go — the route table's comment (the lowercase copy)",
			file:  "api/internal/httpapi/rest.go",
			now:   "// session-only (an API key has no user, so a key cannot drive it), and the",
			before: "// session-only with no MCP tool in front of it: the assistant may bin, never\n" +
				"// destroy.",
		},
		{
			label:  "tools.go — purge_workspace's description",
			file:   "api/internal/mcpsrv/tools.go",
			now:    "TIDAK PERNAH DICADANGKAN OLEH CHAT: chat dalam aplikasi tidak boleh mencadangkan",
			before: "ALAT MANUSIA: chat tidak pernah boleh mencadangkannya.",
		},
		{
			label:  "tools.go — delete_workspace's description",
			file:   "api/internal/mcpsrv/tools.go",
			now:    "ia alat MCP untuk pemilik, bukan untuk chat.",
			before: "padam KEKAL hanya melalui purge_workspace, dan itu manusia sahaja.",
		},
		{
			label:  "docs/workspaces/page.tsx — the docs sentence",
			file:   "web/src/app/docs/workspaces/page.tsx",
			now:    "never by clicking a dashboard control.",
			before: "which is offered to people only, never proposed by the assistant.",
		},
		{
			label:  "MCP-PARITY.md — the root parity file (never reached by the first cut's walk)",
			file:   "MCP-PARITY.md",
			now:    "workspace that has sales, and is never proposable to chat.",
			before: "workspace that has sales, and is never proposable to chat (humans only).",
		},
	}

	for _, s := range sites {
		src := readRepoFile(t, root, s.file)
		if !strings.Contains(src, s.now) {
			t.Errorf("%s: the corrected sentence %q is not in the file, so this case cannot prove the "+
				"scan bites on it — re-anchor the case (or the correction was reverted, which test 5 "+
				"of TestWorkspacePurgeClaimMatchesWhatTheRepoEnforces also reports).", s.file, s.now)
			continue
		}
		hits := scanText(s.file, strings.Replace(src, s.now, s.before, 1))
		if len(hits) == 0 {
			t.Errorf("BITE PROOF FAILED: %s\n"+
				"  put back: %q\n"+
				"  the scan reported NOTHING — this guard has gone blind to a wording that shipped and "+
				"was corrected (cards t_c295f96e / t_5a89de2c).", s.label, s.before)
			continue
		}
		named := false
		for _, h := range hits {
			if h.file == s.file {
				named = true
				break
			}
		}
		if !named {
			t.Errorf("BITE PROOF FAILED: %s — the scan went red on %v, none of which names %s",
				s.label, hits, s.file)
			continue
		}
		t.Logf("bites: %s -> RED at %s", s.label, hits[0].String())
	}

	// --- the healthy tree is QUIET (a guard that always fails proves nothing) --
	for _, subtree := range purgeClaimTrees {
		if hits := findInTree(t, root, subtree); len(hits) > 0 {
			t.Errorf("the tree at HEAD is not clean under this scan: %v — a red from the bite proof "+
				"above would then be this tree being broken rather than the mutation", hits)
		}
	}

	// --- the exemption is PER FILE, not global --------------------------------
	// A quoted wording in a file that is not exempt is still a live claim: quoting
	// what you replaced is a licence the artefacts that actually corrected the
	// claim are given, not a general escape from this scan.
	const quotedClaim = `// the docs used to say "ALAT MANUSIA" beside this route`
	if hits := scanText("api/internal/httpapi/rest.go", quotedClaim); len(hits) == 0 {
		t.Error("BITE PROOF FAILED: a QUOTED banned wording in a non-exempt file was not reported — " +
			"the exemption has leaked past the files that are allowed to quote")
	}

	// --- the regression anchors the card says must keep biting ----------------
	// Anchors 1 and 3 are asserted over the REAL registry in tests 1 and 2 above.
	// Proved here against the shapes they must see, so this file cannot pass
	// vacuously if the extractor or the route check drifts.
	const sixthTool = `&mcp.Tool{Name: "hard_delete_everything", Description: "destroys it all"}`
	if len(mcpToolLiteral.FindAllStringSubmatch(sixthTool, -1)) == 0 {
		t.Error("BITE PROOF FAILED: the tool-literal extractor does not recognise a well-formed " +
			"registration, so test 1's class sweep would pass by seeing no literals at all")
	}
	// Anchor 2 (card t_5a89de2c): a call to the workspace purge route from the web
	// tree, with a negative control so the check is about THIS route and not "any
	// purge".
	const wiredCall = `await fetch("/api/workspaces/" + w.id + "/purge", { method: "DELETE" })`
	if hits := webPurgeCallsIn("web/src/components/WorkspaceTab.tsx", wiredCall); len(hits) == 0 {
		t.Errorf("BITE PROOF FAILED: a fetch of the workspace purge route from the web tree was not "+
			"seen — the guard that keeps \"not by clicking\" a fact has gone blind (%v)", hits)
	}
	const otherPurge = `await fetch("/api/forms/" + f.id + "/purge", { method: "DELETE" })`
	if hits := webPurgeCallsIn("web/src/components/FormsTab.tsx", otherPurge); len(hits) != 0 {
		t.Errorf("the web purge-route check flags a non-workspace purge route (%v) — it must be scoped "+
			"to the workspace purge, or every bin in the dashboard reds it", hits)
	}
}

// webPurgeCallsIn finds lines that CALL the workspace purge route in one file's
// content. A function of content (not a path) so the bite proof can drive it with
// a synthetic call site.
func webPurgeCallsIn(rel, raw string) []claimMatch {
	var out []claimMatch
	for i, line := range strings.Split(raw, "\n") {
		if !strings.Contains(line, "/purge") || !strings.Contains(line, "workspaces/") {
			continue
		}
		out = append(out, claimMatch{
			file: rel, line: i + 1, text: strings.TrimSpace(line),
			needle: "workspaces/{id}/purge",
		})
	}
	return out
}

// webWorkspacePurgeCallers walks web/src and returns every line that calls the
// workspace purge route.
func webWorkspacePurgeCallers(t *testing.T, root string) []string {
	t.Helper()
	dir := filepath.Join(root, "web", "src")
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
		switch filepath.Ext(path) {
		case ".ts", ".tsx", ".js":
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
		for _, h := range webPurgeCallsIn(filepath.ToSlash(rel), string(raw)) {
			out = append(out, h.String())
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

// TestClaimExcusedUsesFoldedOffsets pins the coordinate system of the quotation rule
// (card t_89c4d70b).
//
// Both scan passes search a case-folded haystack and hand the folded offset to
// claimExcused. Go's simple case mapping is NOT length-preserving for every rune —
// `\u0130` folds to a SHORTER string — so a folded offset points EARLIER than the same
// position in the raw line. With the quotation test reading the RAW line, the live
// unquoted claim sitting just after a quotation landed INSIDE the quoted span and was
// excused: the header promises "a match is excused only when that occurrence sits
// INSIDE DOUBLE QUOTES on its line" and the code did not hold it. Measured before the
// fix, the three-rune case below reported ZERO hits; after it, the live claim is
// reported and the quoted one still is not.
func TestClaimExcusedUsesFoldedOffsets(t *testing.T) {
	const rel = "api/internal/httpapi/rest_workspaces.go"

	// One folded offset per \u0130, so the shift has to be absorbed for the live claim
	// to be seen. The quoted, DECLARED wording is excused; the unquoted one is not.
	text := "// \u0130\u0130\u0130 \"no MCP tool in front of it\" only a person can ever purge\n"
	hits := scanText(rel, text)

	var quoted, live int
	for _, h := range hits {
		switch h.needle {
		case "no MCP tool in front of it":
			quoted++
		case "only a person can ever purge":
			live++
		}
	}
	if quoted != 0 {
		t.Errorf("the quoted, declared wording was reported %d time(s) — a quotation is what lets this file print it", quoted)
	}
	if live == 0 {
		t.Errorf("the LIVE unquoted claim was NOT reported: the folded offset shifted onto the quotation and excused it. "+
			"claimExcused must read the folded line, in the coordinates the offset was computed in. Text: %q", text)
	}

	// The rule must still hold on a plain line: quoting is what excuses, nothing else.
	plain := "// only a person can ever purge\n"
	if len(scanText(rel, plain)) == 0 {
		t.Errorf("an unquoted claim on a plain line must be reported")
	}
	quotedOnly := "// \"only a person can ever purge\" is not what this repo enforces\n"
	if n := len(scanText(rel, quotedOnly)); n != 0 {
		t.Errorf("a quoted, declared claim must be excused, got %d hit(s)", n)
	}
}
