package biteharness

// Markdown-table pipe guard (card t_1ef82657).
//
// THE DEFECT CLASS. A raw `|` inside a markdown table cell ENDS that cell, even
// inside a backticked code span: GFM spec 4.10, Example 200 — "Include a pipe in
// a cell's content by escaping it, including inside other inline spans". Three
// rows of BITE-HARNESSES.md carry unescaped in-code pipes (a shell `||`, a
// `printf ... | grep -q`) and publish a broken table to GitHub while every gate
// stays green, because the file is markdown and no gate renders it. The inventory
// guard in inventory_test.go splits each row on `|` and reads column 1 only, so
// it passes on those rows too (measured on origin/main a1311c24:
// `go test ./internal/biteharness/ -count=1` -> ok).
//
// This is a CLASS, not three accidents: measured on origin/main a1311c24, three
// rows break the harness table (line 37 = 8 unescaped pipes, line 38 = 7, line
// 53 = 7) against a header of 5, and three separate cards fix them one line each.
// Nothing checked the count, which is why it kept happening.
//
// WHAT THIS ADDS: a source-side count of UNESCAPED pipes per row (a pipe preceded
// by an odd number of backslashes is escaped and is not a separator), required to
// equal the row's OWN table header — derived from the header, never hardcoded, so
// a table with a different column count needs no edit here.
//
// The COUNT is the check that fires. A RENDER-side "cell count != header"
// assertion does NOT: GitHub normalises an over-long row to the header's cell
// count by dropping the overflow (measured — 0 of 33 rendered rows differ from the
// header, before AND after the fixes), so that assertion stays green on a broken
// table. The source-side count fires on all three.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pipeTables are the repo-root, agent-edited markdown tables whose rows this guard
// keeps honest. MCP-PARITY.md is deliberately absent: it is a bullet list of
// `dashboard -> tool` lines, not a `| ... |` table, so it has no cell to split.
var pipeTables = []string{inventoryFile, "GATE-INVENTORY.md"}

// pendingPipeFix pins rows that are KNOWN broken on this tree, each to the exact
// unescaped-pipe count it carries today AND the change that fixes it, so this
// guard can land and bite BEFORE those changes land. It is the same shape, and the
// same measured compromise, as notYetOnMain in inventory_test.go: the alternative —
// refusing the guard until the three sibling rows are fixed — blocks a one-file
// test on three separate open PRs.
//
// The count is pinned, not just the name, so the pin cannot hide a WORSE defect: a
// pinned row whose count changed at all (a fourth pipe added, a partial fix) is no
// longer the known state and fails. A pinned row whose fix has LANDED is INERT, not
// RED (the same choice notYetOnMain documents): the pin stops matching and removing
// it is optional. What is NOT tolerated is a broken row that is not pinned at its
// current count — that is a NEW instance of the class and fails. Measured on
// origin/main a1311c24 these are exactly the three mismatched rows (37: 8, 38: 7,
// 53: 7).
type pinnedRow struct {
	pipes int
	why   string
}

var pendingPipeFix = map[string]pinnedRow{
	"web/scripts/biteproof_dev_hosts.sh":             {8, "fixed by PR #1029 (card t_fefdcaef): row 37, 8 unescaped pipes vs 5"},
	"web/scripts/biteproof_docs_prose_convention.sh": {7, "fixed by card t_6cb58b07 (open): row 38, 7 unescaped pipes vs 5"},
	"scripts/biteproof_preflight_reuse.sh":           {7, "fixed by PR #1037 (card t_a3928797): row 53, 7 unescaped pipes vs 5"},
}

// unescapedPipes counts bare `|` in s: a pipe preceded by an odd number of
// backslashes is escaped (`\|`) and is cell CONTENT, not a separator.
func unescapedPipes(s string) int {
	n, back := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			back++
			continue
		case '|':
			if back%2 == 0 {
				n++
			}
		}
		back = 0
	}
	return n
}

// pipeMismatch is a data row whose unescaped-pipe count differs from its table's
// header. header/line are 1-based line numbers for the message.
type pipeMismatch struct {
	line   int
	text   string
	got    int
	want   int
	header int
}

// scanTablePipes walks markdown lines and, per table whose rows start with `|`,
// compares every data row's unescaped-pipe count to that table's own header. A
// non-`|` line ends the current table, so a file with several tables (the
// inventory carries a harness table and a cost table) is handled per table. The
// separator row (`|---|---|`) has the header's own count by construction and is
// therefore never a mismatch. tables and rows are returned so a truncated or
// mis-shaped parse can be told apart from a clean one instead of passing
// vacuously.
func scanTablePipes(lines []string) (mismatches []pipeMismatch, tables, rows int) {
	target, headerLine := -1, 0
	for i, l := range lines {
		if !strings.HasPrefix(l, "|") {
			target = -1
			continue
		}
		c := unescapedPipes(l)
		if target < 0 {
			target, headerLine = c, i+1
			tables++
			continue
		}
		rows++
		if c != target {
			mismatches = append(mismatches, pipeMismatch{line: i + 1, text: l, got: c, want: target, header: headerLine})
		}
	}
	return mismatches, tables, rows
}

// harnessNameOf returns the first backticked token of a row's first column — the
// harness path the inventory guard also keys on — so a pin names a row and not a
// line number that moves under it.
func harnessNameOf(row string) string {
	parts := strings.Split(strings.TrimSpace(row), "|")
	if len(parts) < 2 {
		return ""
	}
	spans := backtickRe.FindAllStringSubmatch(parts[1], -1)
	if len(spans) == 0 {
		return ""
	}
	return spans[0][1]
}

// toleratedByPin reports whether a mismatch is EXACTLY the known-broken state a pin
// records — same harness name, same unescaped-pipe count. A row that is pinned but
// whose count has moved (a fourth pipe added, a partial fix) is NOT tolerated, so a
// pin cannot hide a worse defect than the one it names.
func toleratedByPin(m pipeMismatch) (pinnedRow, bool) {
	name := harnessNameOf(m.text)
	if name == "" {
		return pinnedRow{}, false
	}
	pin, ok := pendingPipeFix[name]
	if !ok || pin.pipes != m.got {
		return pinnedRow{}, false
	}
	return pin, true
}

// TestMarkdownTableRowsHaveUniformPipeCounts is the guard: every table row of
// pipeTables must carry the same unescaped-pipe count as its own header, except
// the rows pinned in pendingPipeFix at exactly their recorded count.
func TestMarkdownTableRowsHaveUniformPipeCounts(t *testing.T) {
	root := repoRoot(t)
	totalTables, totalRows, tolerated := 0, 0, 0
	for _, rel := range pipeTables {
		path := filepath.Join(root, rel)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v — a guard that reads nothing proves nothing", path, err)
		}
		mismatches, tables, rows := scanTablePipes(strings.Split(string(raw), "\n"))
		if tables == 0 || rows < 10 {
			t.Fatalf("%s: scan found %d table(s) and %d data row(s) — the file (or this parser) "+
				"changed shape and the check would pass vacuously", rel, tables, rows)
		}
		totalTables += tables
		totalRows += rows
		for _, m := range mismatches {
			if pin, ok := toleratedByPin(m); ok {
				t.Logf("%s:%d: known-broken row tolerated at its pinned count (got %d, header line %d is %d): %s",
					rel, m.line, m.got, m.header, m.want, pin.why)
				tolerated++
				continue
			}
			t.Errorf("%s:%d: row has %d unescaped pipes but its header (line %d) has %d — a raw `|` "+
				"inside a backticked code span ENDS the cell (GFM 4.10, Example 200), so GitHub publishes a "+
				"broken table. Escape every in-cell pipe as `\\|` (change nothing else). Row: %.140q",
				rel, m.line, m.got, m.header, m.want, m.text)
		}
	}
	if totalTables == 0 {
		t.Fatalf("no markdown table found in %v", pipeTables)
	}
	t.Logf("%d table(s), %d rows checked for a uniform unescaped-pipe count (%d known-broken rows tolerated)",
		totalTables, totalRows, tolerated)
}

// TestPendingPipeFixEntriesAreStillListed closes the one hole a pin could open: a
// pin that names no row in the table tolerates nothing and hides the fact that the
// row it excused is gone. Mirrors TestNotYetOnMainEntriesAreStillListed.
func TestPendingPipeFixEntriesAreStillListed(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, inventoryFile))
	if err != nil {
		t.Fatalf("read %s: %v — a pin cannot be checked against a file that cannot be read", inventoryFile, err)
	}
	listed := map[string]bool{}
	for _, l := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(l, "|") {
			continue
		}
		if n := harnessNameOf(l); n != "" {
			listed[n] = true
		}
	}
	for name, pin := range pendingPipeFix {
		if !listed[name] {
			t.Errorf("pendingPipeFix pins `%s` (%s) but %s has no row naming it — a pin that names nothing "+
				"excuses nothing; remove it or correct the name", name, pin.why, inventoryFile)
		}
	}
	t.Logf("pending pipe-fix pins all name a listed row: %d", len(pendingPipeFix))
}

// TestTablePipeDetectorBites proves the detector catches the class, on fixtures
// that reproduce the two idioms that actually broke the table — so reverting the
// fix (or adding a fourth unescaped pipe) is caught, and a correctly escaped row
// stays green. It reads no file: the frozen fixtures cannot rot when the real rows
// are fixed.
func TestTablePipeDetectorBites(t *testing.T) {
	header := "| Harness | Proves | Cases | Wall time here |"
	if got := unescapedPipes(header); got != 5 {
		t.Fatalf("fixture header has %d unescaped pipes, want 5 — the fixture is wrong, not the code", got)
	}
	cases := []struct {
		name   string
		row    string
		broken bool
	}{
		{"escaped pipes stay one cell (the fix)",
			"| `api/scripts/biteproof_x.py` | case 7: `printf '%s' \"$out\" \\| grep -q 'x'` | 3 | 1.2s |", false},
		{"raw pipe inside a code span (the pipe form that broke row 37)",
			"| `web/scripts/biteproof_dev_hosts.sh` | case: `printf '%s' \"$out\" | grep -q 'x'` | 4 | 3.2s |", true},
		{"raw `||` inside a code span (the shell-or that broke row 53)",
			"| `scripts/biteproof_preflight_reuse.sh` | case 7: `gr_is_hostport \"$auth\" || comp=\"${comp##*@}\"` | 9 | n/a |", true},
		{"plain row with no in-cell pipe",
			"| `api/scripts/biteproof_z.py` | nothing special | 4 | 3.2s |", false},
		{"ONE backslash escapes the pipe (odd -> cell content)",
			`| ` + "`api/scripts/biteproof_a.py`" + ` | a\| b | 1 | 1s |`, false},
		{"TWO backslashes leave the pipe a SEPARATOR (even -> extra cell)",
			`| ` + "`api/scripts/biteproof_b.py`" + ` | a\\| b | 1 | 1s |`, true},
	}
	for _, c := range cases {
		got, tables, _ := scanTablePipes([]string{header, c.row})
		if tables != 1 {
			t.Errorf("%s: scan found %d tables, want 1", c.name, tables)
			continue
		}
		if c.broken != (len(got) == 1) {
			t.Errorf("%s: %q -> mismatches=%d (broken=%v), want broken=%v",
				c.name, c.row, len(got), len(got) == 1, c.broken)
		}
	}
}

// TestPipePinCannotHideAWorseDefect proves the pinned-count rule: a pinned row
// tolerated at exactly the count recorded in pendingPipeFix, but a pinned row whose
// count has MOVED — one more unescaped pipe, or a partial fix that left another
// cell split — is not tolerated. Without this, a pin would be a name-based blanket
// over a row and could hide a worse defect on it.
func TestPipePinCannotHideAWorseDefect(t *testing.T) {
	const harness = "web/scripts/biteproof_dev_hosts.sh"
	pin, ok := pendingPipeFix[harness]
	if !ok {
		t.Fatalf("fixture assumes %s is pinned; the map moved, fix the fixture", harness)
	}
	cases := []struct {
		name   string
		got    int
		wantOK bool
	}{
		{"exactly the pinned count", pin.pipes, true},
		{"one MORE unescaped pipe", pin.pipes + 1, false},
		{"a partial fix, one fewer", pin.pipes - 1, false},
		{"back to the header count (the fix landed)", 5, false},
	}
	for _, c := range cases {
		m := pipeMismatch{line: 1, got: c.got, want: 5, header: 1, text: "| `" + harness + "` | x | y | z |"}
		if _, ok := toleratedByPin(m); ok != c.wantOK {
			t.Errorf("%s: toleratedByPin(got=%d) = %v, want %v — a pin must match the recorded count exactly",
				c.name, c.got, ok, c.wantOK)
		}
	}
	// A mismatch naming no harness (an un-backticked first cell) is never tolerated:
	// there is no key to match a pin against.
	if _, ok := toleratedByPin(pipeMismatch{text: "| not a harness | a | b | c | d |"}); ok {
		t.Errorf("toleratedByPin tolerated a row naming no harness")
	}
}
