package biteharness

// Bite-harness inventory guard (issue: the api/scripts + web/scripts + backup
// bite proofs were outside every gate, so a green preflight proved the guards
// RUN but never that they still BITE).
//
// THE PROBLEM THIS CLOSES. A guard's whole value is that it fails when the
// behaviour it pins is removed. Nothing in this repo proved that: the bite
// harnesses (api/scripts/biteproof_*.py, web/scripts/operator-bite.sh,
// web/scripts/biteproof_next_managed_files.sh, deploy/backup/jomform-backup_bite.sh)
// are on-demand instruments, so an edit to the source they sabotage could leave
// them silently broken with every gate still green. The card that created
// BITE-HARNESSES.md proved the gap by grep: `biteproof` appeared nowhere in
// scripts/preflight.sh, and GATE-INVENTORY.md had no row for any of them.
//
// THE DECISION (recorded in BITE-HARNESSES.md, measured, not assumed): they stay
// on demand. One case costs a full `go test` (or `npm run test:unit`), the three
// api harnesses measure 2m02s-5m08s each on this host, and wiring them would
// roughly double a full gate the merge lane already serialises on. What was
// actually wrong was not that they are manual — it was that NOTHING SAID SO, and
// nothing noticed when the set changed. So BITE-HARNESSES.md is the inventory and
// this file is what keeps it honest. It adds NO gate: it runs inside the `api
// test` gate that already exists.
//
// WHAT IT PROVES (both directions, plus two honesty properties):
//
//  1. Every bite harness on disk has a row in BITE-HARNESSES.md. A new
//     `*bite*` file with no row fails the build, so the inventory cannot fall
//     behind the tree the way the gate table fell five gates behind the runner.
//  2. Every row names a file that exists AND is discovered by the same walk. A
//     row pointing at a file that was renamed away, or at something that is not
//     a harness, fails — so the table cannot rot into a claim about instruments
//     that no longer exist.
//  3. The documented claim "none of these is a per-merge gate" is CHECKED, not
//     trusted: no harness name may appear in an ACTIVE line of
//     scripts/preflight.sh. Wiring one as a gate therefore forces the doc and
//     GATE-INVENTORY.md to be corrected in the same change.
//  4. The table is non-empty and every row is a full row. A guard that reads
//     nothing proves nothing — an emptied table would otherwise pass vacuously.
//
// THE NAMING CONVENTION IS LOAD-BEARING, AND BITE-HARNESSES.md STATES IT under
// "Adding a harness" — that sentence is the one card t_e6c6d214 added, because this
// comment used to claim the doc stated it while the doc did not, so the rule the gate
// enforces was discoverable only by reading THIS file. The rule: a harness is a file
// whose BASENAME contains "bite" (case-insensitive), excluding Go test files and the
// inventory file itself. Directory names never trigger it, which is why this package can
// be called `biteharness` without matching itself.
//
// SCOPE LIMITS, stated so this guard is not over-claimed: it proves the
// inventory is complete and its files exist. It does NOT prove any harness
// passes, does NOT prove a harness's cases still fail on their guard's own
// assertion (that residual is filed against one harness's verdict logic), and
// does NOT prove its stated wall times are current. It is a register, not a
// re-run.
//
// THE `Cases` CELL IS CHECKED FOR ONE MARKER FAMILY, AND THAT BOUND IS THE POINT
// (card t_dc646914). This file reads column 1 only, so before that card a row could
// publish a case count its own harness contradicted. Measured on origin/main
// e19abcb4: the `scripts/biteproof_preflight_reuse.sh` row's Cases cell said 12 while
// `grep -c '^# --- case [0-9]' scripts/biteproof_preflight_reuse.sh` returned 13, and
// this package's tests were green. The cell had been one short since PR #1089 added
// cases 10 AND 11 in one change and bumped the cell once; the change after it bumped
// 11 -> 12 while the markers went 12 -> 13 and inherited the offset.
// cases_cell_test.go now reads the cell, but ONLY where the count is derivable: a
// harness whose `# --- case` markers are exactly 0..N, no gap and no duplicate (four
// rows on this tree: scripts/biteproof_api_test_gate_timeout.sh 6/6,
// scripts/biteproof_web_image_gate.sh 3/3, scripts/biteproof_preflight_reuse.sh 13/13,
// web/scripts/biteproof_workspace_danger_refusal.sh 3/3) — plus every narrative line
// that states `case count is **N**`, checked against its row's cell.
//
// The REST of the column is NOT derivable from one convention, measured on c36b5e22:
// web/scripts/biteproof_npm_audit_gate.sh prints five `== case N` echo headers
// (case 0 a control) while its cell of 4 counts the four mutations its `run_case`
// helper drives, so a marker-counting rule would demand 5 there and red a correct
// row; web/scripts/biteproof_next_managed_files.sh labels them in prose
// (`# Case 0:`, and ONE `# --- case 6:` heading against a cell of 6, which is why a
// "one marker means covered" rule would red a correct row),
// deploy/biteproof_build_and_ship_gate.sh uses `# case A`..`# case D` plus a fifth
// leg, and the api/scripts/*.py harnesses carry no `# --- case` marker at all. For
// those rows the `Cases` cell remains a CLAIM, not a check, and it must not be
// described as enforced anywhere.

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// inventoryFile is the register this guard keeps in step with the tree.
const inventoryFile = "BITE-HARNESSES.md"

// preflightScript is the gate runner. Property 3 reads it: a harness name in an
// ACTIVE line means one of them was wired as a gate, which contradicts the doc.
const preflightScript = "scripts/preflight.sh"

// skipDirs are trees that cannot hold a source harness and are expensive or
// misleading to walk: VCS metadata, per-card linked worktrees (a full checkout
// each), installed dependencies, and build output.
var skipDirs = map[string]bool{
	".git":              true,
	".worktrees":        true,
	"node_modules":      true,
	".next":             true,
	"out":               true,
	"dist":              true,
	"vendor":            true,
	"coverage":          true,
	"test-results":      true,
	"playwright-report": true,
}

// harnessMarkRe is the naming convention: "bite" anywhere in the basename,
// case-insensitively (operator-bite.sh, jomform-backup_bite.sh,
// biteproof_*.py, biteproof_*.sh).
var harnessMarkRe = regexp.MustCompile(`(?i)bite`)

// backtickRe captures a backticked span in the table's FIRST column.
var backtickRe = regexp.MustCompile("`([^`]+)`")

// tableDelimRe matches a markdown table DELIMITER row: nothing but pipes, dashes,
// colons and spaces, and at least one dash (`|---|---|`, `|:--|--:|`). GFM 4.10
// uses this row — not a leading `|` — to decide where a table BEGINS: "The table
// is broken at the first empty line, or beginning of another block-level
// structure" and the header is "the line immediately preceding" the delimiter.
// Keying table detection on a leading pipe (the pre-fix shape) hid every table
// row whose leading pipe was omitted.
var tableDelimRe = regexp.MustCompile(`^[\s|:-]*-[\s|:-]*$`)

// isDelimRow reports whether a line is a table delimiter row. It also requires a
// pipe, so a bare `---` (a setext heading underline or a thematic break) is NOT
// a table row.
func isDelimRow(line string) bool {
	t := strings.TrimSpace(line)
	return strings.Contains(t, "|") && tableDelimRe.MatchString(t)
}

// firstColumn returns a markdown table row's first cell. GFM 4.10 allows a row
// whose leading pipe is OMITTED — Example 199's data row `bar | baz` renders as
// `<td>bar</td><td>baz</td>` — so the first cell is text up to the first `|`:
// parts[1] when the leading pipe is present, parts[0] when it is absent. Keying
// on `cols[1]` unconditionally (the pre-fix shape) read a no-leading-pipe row's
// SECOND column as its first.
func firstColumn(row string) string {
	t := strings.TrimSpace(row)
	parts := strings.Split(t, "|")
	if strings.HasPrefix(t, "|") && len(parts) >= 2 {
		return parts[1]
	}
	return parts[0]
}

// notYetOnMain are harnesses that are REGISTERED here before the PR that lands
// them has merged. They are allowed to be absent from the tree (that is the
// whole point of an entry) and their row is checked normally the moment the file
// appears — so landing the PR needs no edit here.
//
// The alternative — refusing the row until the file exists — was measured and
// rejected: it blocks the OTHER PR's merge for a one-line doc row, which is how a
// register turns into a landmine (PR 772 ships `biteproof_ses_replyto.py` and was
// written before this register existed).
//
// WHAT STOPS THIS BEING A HOLE: an entry is only consulted for a file that is
// ABSENT (see TestInventoryRowsPointAtRealHarnesses), and
// TestNotYetOnMainEntriesAreStillListed refuses an entry the table does not
// carry — so a name cannot be parked here to excuse a harness that is missing
// from BOTH the tree and the table. The cost of that choice, stated plainly: a
// stale entry for a file that has since landed is inert rather than RED, so this
// list can accumulate a dead path. It is cheap to re-check by hand and it is
// re-read as part of the row it excuses; a force-expiry rule would have put the
// block back on the PR that lands the file.
var notYetOnMain = map[string]string{
	"api/scripts/biteproof_ses_replyto.py": "ships with the SES Reply-To guard's own PR, which is being re-cut onto main; registered here so its landing needs no doc edit",
}

// repoRoot locates the repository root from this file's directory
// (api/internal/biteharness -> ../../../).
func repoRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return abs
}

// isHarness reports whether a basename is a bite harness by the documented
// convention. Test files and the inventory itself are excluded explicitly rather
// than by luck.
func isHarness(name string) bool {
	if !harnessMarkRe.MatchString(name) {
		return false
	}
	if strings.HasSuffix(name, "_test.go") {
		return false
	}
	// A COMPILED python artefact is not a harness, and counting one is a false red
	// (card t_65a6f9dd). Running any api/scripts/biteproof_*.py WRITES
	// `api/scripts/__pycache__/biteproof_x.cpython-3xx.pyc`, whose basename still contains
	// "bite" — so the next `go test ./internal/biteharness/` reported an unregistered
	// harness named after a build artefact, and the only way to clear it was to delete a
	// directory the run had just created. The naming rule is about SOURCE harnesses; a
	// .pyc is output. (The harnesses themselves now also set sys.dont_write_bytecode, which
	// is the other half of the same fix — this side makes the walk robust to any tool that
	// compiles python under the tree.)
	if strings.HasSuffix(name, ".pyc") || strings.HasSuffix(name, ".pyo") {
		return false
	}
	if strings.EqualFold(name, inventoryFile) {
		return false
	}
	return true
}

// discoverHarnesses walks the repo and returns every harness, repo-relative,
// slash-separated and sorted. The scanned count is returned too so an empty or
// truncated walk can be told apart from a clean one instead of passing vacuously.
func discoverHarnesses(t *testing.T, root string) ([]string, int) {
	t.Helper()
	var found []string
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		scanned++
		if !isHarness(d.Name()) {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		found = append(found, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v — this guard cannot run without it", root, err)
	}
	sort.Strings(found)
	return found, scanned
}

// documentedHarnesses parses the FIRST backticked token of the first column of
// each row of the harness table in the inventory — the table whose header's
// first column is `Harness`. The scan STARTS at that header and stops at the
// first line that is not a table row, so a later table in the same file (the
// document also carries a cost table) cannot be mistaken for harness rows, and
// the parse cannot wander into prose.
//
// "Not a table row" for the ROW WALK is any line with no pipe (a blank line, a
// heading, a prose line): such a line closes the table. A row that simply OMITS
// its leading pipe is still a row — GFM 4.10 permits it (Example 199) — so only
// the ROW walk keys on `HasPrefix(line, "|")`. The HEADER must still carry the
// leading pipe and read `Harness` in its first column, which keeps header
// detection unambiguous among the prose that also carries pipes.
//
// Prose above and below the table is ignored, and so is every other column — a
// backticked command in "Proves" is not a row.
//
// A row whose first column has no backticked token is a FAILURE, not a skip: a
// row that parses to nothing is exactly how a table looks complete while naming
// no harness at all.
func documentedHarnesses(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — the inventory is the only thing this guard keeps honest, "+
			"and a guard that reads nothing proves nothing", path, err)
	}
	if len(raw) == 0 {
		t.Fatalf("%s is empty — an empty inventory would report green while naming no harness", path)
	}
	lines := strings.Split(string(raw), "\n")

	// Locate the harness table by its header. A missing header is a failure: it
	// means this guard would read the wrong table (or none) and pass vacuously.
	header := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cols := strings.Split(trimmed, "|")
		if len(cols) >= 2 && strings.TrimSpace(cols[1]) == "Harness" {
			header = i
			break
		}
	}
	if header < 0 {
		t.Fatalf("%s has no table whose first column is `Harness` — this guard parses the FIRST "+
			"backticked path in that table's first column, so without the header it would read "+
			"nothing and report green", path)
	}

	var docs []string
	for i := header + 1; i < len(lines); i++ {
		line := lines[i]
		if !strings.Contains(line, "|") {
			break // the table ended (a blank line, a heading, a prose line)
		}
		trimmed := strings.TrimSpace(line)
		if isDelimRow(trimmed) {
			continue
		}
		first := firstColumn(trimmed)
		spans := backtickRe.FindAllStringSubmatch(first, -1)
		if len(spans) == 0 {
			t.Fatalf("%s:%d: table row %q names no harness (the first column must be "+
				"backticked, e.g. | `api/scripts/biteproof_x.py` | ...) — a row that parses "+
				"to nothing is how the table reads complete while covering nothing", path, i+1, trimmed)
		}
		name := spans[0][1]
		if !strings.Contains(name, "/") {
			t.Fatalf("%s:%d: row %q names %q, which is not a repo-relative path with a "+
				"directory in it", path, i+1, trimmed, name)
		}
		// The column count is checked on the row WITH its leading pipe restored,
		// so a row that merely omitted that pipe cannot pass the full-row shape
		// by counting one column fewer.
		cols := strings.Split("|"+trimmed, "|")
		if len(cols) < 5 {
			t.Fatalf("%s:%d: row %q has %d columns, want the full "+
				"| Harness | Proves | Cases | Wall time | row — a placeholder row states "+
				"nothing and must not be able to sit here", path, i+1, trimmed, len(cols))
		}
		docs = append(docs, name)
	}
	sort.Strings(docs)
	return docs
}

// TestEveryBiteHarnessIsRegisteredInTheInventory is the disk -> table direction.
func TestEveryBiteHarnessIsRegisteredInTheInventory(t *testing.T) {
	root := repoRoot(t)
	found, scanned := discoverHarnesses(t, root)
	if scanned < 100 {
		t.Fatalf("walked only %d files under %s — the walk (or its skip list) is broken, so "+
			"this guard would report no harnesses on any tree and pass vacuously", scanned, root)
	}
	if len(found) == 0 {
		t.Fatalf("no bite harness found on disk under %s (scanned %d files) — either the last "+
			"harness was deleted (then delete its row and say why) or the naming convention "+
			"changed and this register is now blind", root, scanned)
	}

	doc := filepath.Join(root, inventoryFile)
	docs := documentedHarnesses(t, doc)

	inDocs := map[string]bool{}
	for _, d := range docs {
		inDocs[d] = true
	}
	var missing []string
	for _, f := range found {
		if !inDocs[f] {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%s does not name %d bite harness(es) that exist on disk: %v — every harness "+
			"must have a row (path first, backticked) so a reader can tell a deliberately-manual "+
			"instrument from one nobody wired up. Adding the file and its row is one change.",
			inventoryFile, len(missing), missing)
	}
	t.Logf("bite harnesses on disk: %d, all registered in %s (scanned %d files)",
		len(found), inventoryFile, scanned)
}

// TestInventoryRowsPointAtRealHarnesses is the table -> disk direction. A row
// naming a file that does not exist, or one the walk does not classify as a
// harness, is a claim about an instrument nobody can run.
func TestInventoryRowsPointAtRealHarnesses(t *testing.T) {
	root := repoRoot(t)
	found, _ := discoverHarnesses(t, root)
	onDisk := map[string]bool{}
	for _, f := range found {
		onDisk[f] = true
	}

	docs := documentedHarnesses(t, filepath.Join(root, inventoryFile))
	if len(docs) == 0 {
		t.Fatalf("%s has a table but no harness could be parsed from column 1 — a register that "+
			"lists nothing proves nothing", inventoryFile)
	}

	for _, d := range docs {
		abs := filepath.Join(root, filepath.FromSlash(d))
		info, err := os.Stat(abs)
		if err != nil {
			if why, ok := notYetOnMain[d]; ok {
				t.Logf("%s lists `%s`, which is not on this tree yet: %s", inventoryFile, d, why)
				continue
			}
			t.Errorf("%s lists `%s`, which does not exist (%v) — a row naming a renamed-away or "+
				"never-committed harness is a claim nobody can check. Remove the row, or land the "+
				"file with it.", inventoryFile, d, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s lists `%s`, which is empty — an empty harness proves nothing", inventoryFile, d)
		}
		if !onDisk[d] {
			t.Errorf("%s lists `%s`, which the harness walk does not classify as a bite harness "+
				"(basename must contain \"bite\") — either it is not a harness and does not belong "+
				"in this register, or the walk and this row disagree about what a harness is",
				inventoryFile, d)
		}
	}
	t.Logf("inventory rows verified: %d (%d registered ahead of their PR)",
		len(docs), len(notYetOnMain))
}

// TestNotYetOnMainEntriesAreStillListed closes the one hole the
// registered-before-landing allowance could open: a name parked in notYetOnMain
// must still have its ROW in the table. Without this, an entry could excuse a
// harness that is missing from both the tree and the register — which is exactly
// the silent gap this whole file exists to remove.
func TestNotYetOnMainEntriesAreStillListed(t *testing.T) {
	root := repoRoot(t)
	docs := documentedHarnesses(t, filepath.Join(root, inventoryFile))
	listed := map[string]bool{}
	for _, d := range docs {
		listed[d] = true
	}
	for name, why := range notYetOnMain {
		if !listed[name] {
			t.Errorf("notYetOnMain carries %q (%s) but %s has no row for it — an allowance for a "+
				"harness the register does not even name excuses nothing; either add the row or "+
				"drop the entry", name, why, inventoryFile)
		}
	}
	t.Logf("registered-ahead entries all have a row: %d", len(notYetOnMain))
}

// TestNoBiteHarnessIsWiredAsAGate pins the documented claim that these are
// on-demand instruments. If wiring one as a gate is ever the right call, the doc
// (and GATE-INVENTORY.md, which the selftest gate parses) must be corrected in
// the same change — this is what makes that mandatory rather than remembered.
//
// Only ACTIVE lines count: the runner's comments legitimately name the backup
// bite proof to explain why it is NOT a gate, and a strip-comments-first read is
// the same rule scripts/preflight_test.sh check 10 applies to `|| true`.
// stripCommentAt drops a trailing comment — but only one a `#` actually INTRODUCES.
//
// The old shape was `strings.Index(code, "#")`, which truncates at ANY `#`, including
// one inside `${...}`. A runner line like `X="${X#y}"; bash api/scripts/biteproof_x.py`
// was reduced to `X="${X`, so a harness wired behind the parameter expansion was
// INVISIBLE and the guard read GREEN (card t_6b291041). The shell's own rule: a `#`
// starts a comment at the start of a word — i.e. at the line start or after
// whitespace — not mid-token. Same fix, and same reason, as check 10 in
// scripts/preflight_test.sh (card t_9c1c1518).
func stripCommentAt(line string) string {
	for i := 0; i < len(line); i++ {
		if line[i] != '#' {
			continue
		}
		if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' {
			return line[:i]
		}
	}
	return line
}

func TestStripCommentAtKeepsHashesInsideTokens(t *testing.T) {
	cases := []struct {
		line, want, why string
	}{
		{`X="${X#y}"; bash api/scripts/biteproof_menu_variant_gaps.py`,
			`X="${X#y}"; bash api/scripts/biteproof_menu_variant_gaps.py`,
			"a `#` inside ${...} is part of the token; truncating here hid a wired harness"},
		{`X="${X#y}"; bash api/scripts/biteproof_x.py  # a real comment`,
			`X="${X#y}"; bash api/scripts/biteproof_x.py  `,
			"a whitespace-preceded `#` DOES start a comment"},
		{`# a comment line`, ``, "a line-leading `#` starts a comment"},
		{`url=https://x/#frag`, `url=https://x/#frag`,
			"a `#` mid-token (a URL fragment) is not a comment"},
		{`c=${#FAILED[@]}; run monitor`, `c=${#FAILED[@]}; run monitor`,
			"`${#arr[@]}` must survive so the `run` call site behind it is still read"},
		{``, ``, "empty line"},
	}
	for _, c := range cases {
		if got := stripCommentAt(c.line); got != c.want {
			t.Errorf("stripCommentAt(%q) = %q, want %q — %s", c.line, got, c.want, c.why)
		}
	}
}

func TestNoBiteHarnessIsWiredAsAGate(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, preflightScript)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — property 3 of this guard cannot be checked without the runner", path, err)
	}

	found, _ := discoverHarnesses(t, root)
	if len(found) == 0 {
		t.Fatalf("no harness discovered, so this check would pass vacuously")
	}

	var active []string
	for _, line := range strings.Split(string(raw), "\n") {
		code := stripCommentAt(line)
		if strings.TrimSpace(code) == "" {
			continue
		}
		active = append(active, code)
	}
	if len(active) < 50 {
		t.Fatalf("only %d active lines read out of %s — the comment strip is eating the file, "+
			"so this check would report a clean runner whatever it contains", len(active), preflightScript)
	}
	activeText := strings.Join(active, "\n")

	var wired []string
	for _, h := range found {
		base := filepath.Base(h)
		if strings.Contains(activeText, h) || strings.Contains(activeText, base) {
			wired = append(wired, h)
		}
	}
	if len(wired) > 0 {
		t.Fatalf("%s invokes %d bite harness(es) in an ACTIVE line: %v. %s states these are "+
			"on-demand, NOT per-merge gates (measured: one case costs a full `go test`, 2m02s-5m08s "+
			"per api harness on this host). Either un-wire them, or correct %s and add the row to "+
			"GATE-INVENTORY.md in the same change — the selftest gate parses that table in both "+
			"directions.", preflightScript, len(wired), wired, inventoryFile, inventoryFile)
	}
	t.Logf("no bite harness is wired into %s (%d active lines checked, %d harnesses known)",
		preflightScript, len(active), len(found))
}
