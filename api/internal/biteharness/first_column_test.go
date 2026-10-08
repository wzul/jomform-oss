package biteharness

// firstColumn guard (card t_9bdbf216).
//
// WHY THIS EXISTS. firstColumn (inventory_test.go) returns a markdown table
// row's first cell and is read by two callers: documentedHarnesses (the
// inventory walk) and harnessNameOf (table_pipes_test.go, the pin matcher).
// PR #1049 taught it that GFM 4.10 lets a row OMIT its leading pipe
// ("use of leading and trailing pipes may be inconsistent", Example 199), so the
// first cell is text up to the first `|` — parts[1] with a leading pipe,
// parts[0] without.
//
// THE HOLE THIS CLOSES. The shipped suite could not tell that fix apart from the
// pre-fix shape. Measured at tag v1.20.82 (tree 295fd6a6): reverting firstColumn's
// guard to the pre-fix
//
//	if len(parts) >= 2 { return parts[1] }
//
// left the WHOLE package green — `ok ... 0.038s` — because every no-leading-pipe
// row in the real BITE-HARNESSES.md begins with the backticked harness path, so
// parts[0] and parts[1] BOTH carry a resolving backtick and the downstream regex
// answers the same either way. The only shape that discriminates is a
// no-leading-pipe row whose FIRST cell carries the backticked path AND whose
// SECOND cell does not resolve to one: under the fix firstColumn reads
// "`path` ", under the pre-fix shape it reads the SECOND cell.
//
// WHAT THIS PINS (value-level, so a revert reds a NAMED assertion instead of
// crashing at a downstream row-shape guard):
//
//  1. firstColumn reads the FIRST cell of a no-leading-pipe row.
//  2. harnessNameOf (the pin matcher) names that row's harness from that cell.
//  3. documentedHarnesses — the REAL inventory reader — returns that harness from
//     a real fixture file, so the helper and the reader are shown to agree.
//  4. a no-leading-pipe row whose FIRST cell is backtick-free still reads as
//     "names no harness", and documentedHarnesses FATALS on it naming that reason
//     — observed on the real reader in a subprocess, not inferred from the
//     predicate. That is what makes the branch under test load-bearing: under the
//     pre-fix shape the reader silently steals a path from a LATER column, so a
//     row that names no harness in its first column passes.
//
// SCOPE: this pins firstColumn's two branches and the one downstream fatal they
// decide. It does not re-prove the table-pipe cell count (table_pipes_test.go)
// and does not execute any harness.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// noLeadBackticked is a table row pasted WITHOUT its leading pipe whose FIRST
// cell carries the backticked harness path. Its SECOND cell holds a single
// (incomplete) backtick, so the pre-fix shape — which reads the second cell —
// resolves to NO harness and the fix — which reads the first — resolves to the
// path. That asymmetry is what makes the row discriminate; a row whose second
// cell also carried a complete backticked path would answer the same both ways.
const noLeadBackticked = "`api/scripts/biteproof_w.py` | probe `a | b` | 2s |"

// noLeadBacktickFree is a no-leading-pipe row whose FIRST cell carries NO
// backtick at all, while a LATER cell does. Under the fix its first column names
// no harness (documentedHarnesses fatals); under the pre-fix shape the reader
// would silently answer with the later cell's path.
const noLeadBacktickFree = "not a harness | `api/scripts/biteproof_y.py` | 1 | 2s |"

// fixtureHeader/fixtureDelim make a fixture file that documentedHarnesses reads
// as a harness table (its first column reads `Harness`) with the delimiter row
// anchoring it.
const (
	fixtureHeader = "| Harness | Proves | Cases | Wall time |"
	fixtureDelim  = "|---|---|---|---|"
)

// writeFixture writes a one-row harness table (header + delimiter + row) to a
// temp file and returns its path.
func writeFixture(t *testing.T, row string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, inventoryFile)
	body := fixtureHeader + "\n" + fixtureDelim + "\n" + row + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
	return path
}

// TestFirstColumnReadsANoLeadingPipeRowsFirstCell is the load-bearing value-level
// assertion: reverting firstColumn to the pre-fix shape makes BOTH rows below
// answer differently, so the guard reds a named assertion.
func TestFirstColumnReadsANoLeadingPipeRowsFirstCell(t *testing.T) {
	// The exact shape the fix must read by its FIRST cell.
	if got, want := firstColumn(noLeadBackticked), "`api/scripts/biteproof_w.py` "; got != want {
		t.Errorf("firstColumn(%q) = %q, want the FIRST cell %q — the pre-fix shape reads the "+
			"SECOND cell instead, which is the whole defect", noLeadBackticked, got, want)
	}
	if got, want := harnessNameOf(noLeadBackticked), "api/scripts/biteproof_w.py"; got != want {
		t.Errorf("harnessNameOf(%q) = %q, want %q — the reader must name the harness from the "+
			"row's FIRST column", noLeadBackticked, got, want)
	}

	// A backtick-free FIRST cell must read as "names no harness" (harnessNameOf
	// == ""), and must NOT pick up the later cell's path — which is exactly what
	// the pre-fix shape does.
	if got := firstColumn(noLeadBacktickFree); strings.Contains(got, "`") {
		t.Errorf("firstColumn(%q) = %q, want a first cell with no backtick — the first column "+
			"names no harness", noLeadBacktickFree, got)
	}
	if got := harnessNameOf(noLeadBacktickFree); got != "" {
		t.Errorf("harnessNameOf(%q) = %q, want \"\" (names no harness) — the pre-fix shape answers "+
			"with a LATER cell's path, so a row that names no harness in its first column would pass",
			noLeadBacktickFree, got)
	}

	// Control: the leading-pipe branch is unchanged by the fix, so a leading-pipe
	// row still reads its first cell. Without this a "fix" that broke the common
	// case would be invisible.
	const leadRow = "| `api/scripts/biteproof_x.py` | proves | 1 | 2s |"
	if got, want := firstColumn(leadRow), " `api/scripts/biteproof_x.py` "; got != want {
		t.Errorf("firstColumn(%q) = %q, want %q — the leading-pipe branch must be unchanged",
			leadRow, got, want)
	}
	if got, want := harnessNameOf(leadRow), "api/scripts/biteproof_x.py"; got != want {
		t.Errorf("harnessNameOf(%q) = %q, want %q — the leading-pipe branch must be unchanged",
			leadRow, got, want)
	}
}

// TestDocumentedHarnessesReadsANoLeadingPipeRowsFirstColumn drives the REAL reader
// on a real fixture file, so the helper's answer above is shown to be the one the
// inventory walk uses (not merely a parallel implementation that agrees).
func TestDocumentedHarnessesReadsANoLeadingPipeRowsFirstColumn(t *testing.T) {
	path := writeFixture(t, noLeadBackticked)
	docs := documentedHarnesses(t, path)
	found := false
	for _, d := range docs {
		if d == "api/scripts/biteproof_w.py" {
			found = true
		}
	}
	if !found {
		t.Errorf("documentedHarnesses(%s) = %v, want it to contain %q — a no-leading-pipe row "+
			"whose first cell carries the path must be read by that cell",
			filepath.Base(path), docs, "api/scripts/biteproof_w.py")
	}
}

// TestDocumentedHarnessesFatalsWhenTheFirstCellNamesNoHarness proves the fatal
// path the branch decides, by RUNNING it: the child is this test binary, told to
// drive documentedHarnesses on a fixture whose only row has a backtick-free first
// cell. Under the fix the real reader fatals naming "names no harness"; under the
// pre-fix shape it would instead silently answer with the later cell's path and
// the child would pass — so the parent's assertions below red on a revert.
func TestDocumentedHarnessesFatalsWhenTheFirstCellNamesNoHarness(t *testing.T) {
	path := writeFixture(t, noLeadBacktickFree)

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve this test binary: %v — the fatal path cannot be observed without it", err)
	}
	cmd := exec.Command(self, "-test.run=^TestBiteharnessFixtureReaderHelper$", "-test.v")
	cmd.Env = append(os.Environ(), fixturePathEnv+"="+path)
	out, runErr := cmd.CombinedOutput()
	text := string(out)

	if runErr == nil {
		t.Errorf("documentedHarnesses did NOT fatal on %q (child exit 0): the reader accepted a row "+
			"whose first column names no harness. Under the fix it fatals; a pass means the first "+
			"column was read as a LATER cell's path. Child output:\n%s", noLeadBacktickFree, text)
		return
	}
	if !strings.Contains(text, "names no harness") {
		t.Errorf("documentedHarnesses failed on %q for the WRONG reason — want its own message "+
			"`names no harness`, got:\n%s", noLeadBacktickFree, text)
	}
}

// fixturePathEnv carries the fixture path from the parent test to the child.
const fixturePathEnv = "JF_BITEHARNESS_FIXTURE"

// TestBiteharnessFixtureReaderHelper is the child half of the test above. It does
// nothing unless the parent set the fixture env, so its ordinary run in the
// package is a no-op — the classic helper-process shape for observing a t.Fatalf
// that would otherwise take the parent's goroutine down.
func TestBiteharnessFixtureReaderHelper(t *testing.T) {
	path := os.Getenv(fixturePathEnv)
	if path == "" {
		return // helper process: driven only while the parent sets fixturePathEnv
	}
	docs := documentedHarnesses(t, path)
	t.Logf("documentedHarnesses(%s) = %v", path, docs)
}
