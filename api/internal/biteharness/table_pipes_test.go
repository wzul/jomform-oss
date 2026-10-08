package biteharness

// Markdown-table pipe guard (card t_1ef82657; leading-pipe hole closed by t_44dedf7c).
//
// THE DEFECT CLASS. A raw `|` inside a markdown table cell ENDS that cell, even
// inside a backticked code span: GFM spec 4.10, Example 200 — "Include a pipe in
// a cell's content by escaping it, including inside other inline spans". Three
// rows of BITE-HARNESSES.md carried unescaped in-code pipes (a shell `||`, a
// `printf ... | grep -q`) and published a broken table to GitHub while every gate
// stayed green, because the file is markdown and no gate renders it. The inventory
// guard in inventory_test.go splits each row on `|` and reads column 1 only, so
// it passed on those rows too.
//
// This is a CLASS, not three accidents: measured on origin/main a1311c24, three
// rows broke the harness table against a 4-column header, and three separate cards
// fixed them one line each. Nothing checked the cell count, which is why it kept
// happening.
//
// WHAT THIS ADDS: a source-side CELL count per row (cells are separated by
// UNESCAPED pipes — a pipe preceded by an odd number of backslashes is escaped and
// is content), required to equal the row's OWN table header — derived from the
// header, never hardcoded, so a table with a different column count needs no edit.
//
// THE METRIC IS CELLS, NOT RAW PIPES (card t_44dedf7c). GFM 4.10 lets a row OMIT
// its leading and trailing pipe ("use of leading and trailing pipes may be
// inconsistent", Example 199: the data row `bar | baz` is a table row). The
// pre-fix guard keyed table detection on `strings.HasPrefix(line, "|")`, so such a
// row was invisible — never even counted. Comparing a raw unescaped-pipe count
// cannot fix this: omitting the leading pipe removes one pipe, so a broken 6-pipe
// row becomes a 5-pipe row that equals a 5-pipe header and stays green. The cell
// count is leading-pipe-independent (`| a | b |` and `a | b` are both 2 cells) and
// is the quantity the defect actually changes, so the guard compares THAT.
//
// The COUNT is the check that fires. A RENDER-side "cell count != header"
// assertion does NOT: GitHub normalises an over-long row to the header's cell
// count by dropping the overflow (measured — 0 of 33 rendered rows differ from the
// header, before AND after the fixes), so that assertion stays green on a broken
// table. The source-side count fires.

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

// A pinned row is a harness name plus the exact CELL count it currently carries.
type pinnedRow struct {
	pipes int
	why   string
}

// pendingPipeFix pins rows that are KNOWN broken on this tree, each to the exact
// CELL count it carries today AND the change that fixes it, so this guard can land
// and bite BEFORE those changes land. It is the same shape, and the same measured
// compromise, as notYetOnMain in inventory_test.go: the alternative — refusing the
// guard until a sibling row is fixed — blocks a one-file test on another open PR.
// The count is pinned, not just the name, so the pin cannot hide a WORSE defect: a
// pinned row whose count changed at all (another in-cell pipe, a partial fix) is no
// longer the known state and fails. A pinned row whose fix has LANDED is INERT, not
// RED (the same choice notYetOnMain documents): the pin stops matching and removing
// it is optional. What is NOT tolerated is a broken row that is not pinned at its
// current count — that is a NEW instance of the class and fails.
//
// Measured on origin/main cfbaa439 with the cell metric: exactly one row is
// mismatched — `web/scripts/biteproof_docs_prose_convention.sh` at 6 cells against
// the harness table's 4-cell header. The two pins this map carried before
// (`biteproof_dev_hosts.sh`, `biteproof_preflight_reuse.sh`) are gone because their
// fixes LANDED (#1029, #1037); their rows now carry the header's count.
var pendingPipeFix = map[string]pinnedRow{
	"web/scripts/biteproof_docs_prose_convention.sh": {6, "fixed by card t_6cb58b07 (open): 6 cells — two in-cell pipes each split a cell — against the 4-cell header"},
}

// pipeMismatch is a data row whose CELL count differs from its table's header.
// header/line are 1-based line numbers for the message.
type pipeMismatch struct {
	line   int
	text   string
	got    int
	want   int
	header int
}

// splitUnescapedPipes splits s on UNESCAPED pipes: a pipe preceded by an odd
// number of backslashes is escaped (`\|`) and stays in its cell, so a backticked
// shell `|` written as `\|` is content rather than a cell boundary.
func splitUnescapedPipes(s string) []string {
	var parts []string
	var b strings.Builder
	back := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			back++
			b.WriteByte(s[i])
			continue
		case '|':
			if back%2 == 0 {
				parts = append(parts, b.String())
				b.Reset()
			} else {
				b.WriteByte(s[i])
			}
		default:
			b.WriteByte(s[i])
		}
		back = 0
	}
	parts = append(parts, b.String())
	return parts
}

// cellCount splits a markdown table row into cells by GFM 4.10's rule: cells are
// separated by pipes, and ONE EMPTY boundary cell at each end — present only when
// the row carries that pipe — is dropped. That is what makes a row's cell count
// independent of whether its leading pipe was written, which GFM 4.10 explicitly
// allows ("use of leading and trailing pipes may be inconsistent", Example 199).
// Measured against the spec's own examples: header `| abc | defghi |` -> 2; the
// leading-pipe-less delimiter `:-: | -----------:` -> 2; `| bar | baz |` -> 2 and
// `bar | baz` -> 2.
//
// It is a CELL count, not the raw unescaped-pipe count the guard used before.
// Dropping a leading pipe removes one pipe, so a raw count is not comparable
// across the conventions GFM allows — the pre-fix no-leading-pipe row the card
// names (6 unescaped pipes -> 5 once its leading pipe is gone) would compare
// EQUAL to a 5-pipe header and stay green, which is exactly the hole being
// closed. The cell count is the quantity the defect actually changes.
func cellCount(row string) int {
	parts := splitUnescapedPipes(row)
	for len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
		parts = parts[1:]
	}
	for len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}
	return len(parts)
}

// isBlockStart reports whether a line begins a block-level structure that ENDS an
// open table even when it carries a pipe (GFM 4.10: "The table is broken at the
// first empty line, or beginning of another block-level structure"). Only the
// shapes that plausibly follow one of these tables are covered — an ATX heading,
// a block quote, a fenced code block. A line with no pipe at all closes the table
// by the row test itself, so it needs no entry here.
func isBlockStart(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	switch t[0] {
	case '#', '>':
		return true
	}
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// scanTablePipes walks markdown lines and, per table, compares every DATA row's
// cell count to that table's own header. Tables are located by their DELIMITER
// row — GFM 4.10 defines a table as a header row, a delimiter row and data rows —
// so the header is the line immediately above the delimiter, and detection does
// NOT depend on the header's leading pipe.
//
// A ROW, while a table is open, is any line that carries a pipe and is not blank
// and not a block start. That is the GFM 4.10 rule for a data row, and it makes
// the leading pipe OPTIONAL (Example 199's `bar | baz` is a row) — the pre-fix
// guard keyed on `HasPrefix(line, "|")` instead, so a row pasted without its
// leading pipe was never even counted. The table ends at the first blank line, a
// line with no pipe, or a block start (a heading, a quote, a fence), which is how
// a table that runs off its end is separated from the prose below it — the two
// pipe-carrying prose lines BITE-HARNESSES.md keeps below its harness table are
// behind a blank line, not inside it.
//
// The row walk deliberately keys on `Contains(line, "|")` rather than on the cell
// count: a pipe-less line is treated as the table's end even though GFM would read
// it as a one-cell row (Example 202), because a guard that walked pipe-less prose
// as rows would report phantom cells from ordinary paragraphs.
//
// tables and rows are returned so a truncated or mis-shaped parse can be told
// apart from a clean one instead of passing vacuously.
func scanTablePipes(lines []string) (mismatches []pipeMismatch, tables, rows int) {
	target, headerLine := -1, 0
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if target < 0 {
			// A delimiter row with no open table opens one whose header is the
			// line above it. Without a header that is itself a table row this is
			// not a table (a bare `---` is a thematic break, not a table).
			if isDelimRow(t) && i > 0 && cellCount(lines[i-1]) >= 2 {
				target, headerLine = cellCount(lines[i-1]), i
				tables++
			}
			continue
		}
		if t == "" || !strings.Contains(t, "|") || isBlockStart(t) {
			target = -1 // the table ended
			continue
		}
		c := cellCount(t)
		rows++
		if c != target {
			mismatches = append(mismatches, pipeMismatch{line: i + 1, text: l, got: c, want: target, header: headerLine})
		}
	}
	return mismatches, tables, rows
}

// harnessNameOf returns the first backticked token of a row's first column — the
// harness path the inventory guard also keys on — so a pin names a row and not a
// line number that moves under it. The first column is read through firstColumn,
// so a row that omits its leading pipe still names its harness.
func harnessNameOf(row string) string {
	spans := backtickRe.FindAllStringSubmatch(firstColumn(row), -1)
	if len(spans) == 0 {
		return ""
	}
	return spans[0][1]
}

// toleratedByPin reports whether a mismatch is EXACTLY the known-broken state a pin
// records — same harness name, same CELL count. A row that is pinned but whose
// count has moved (another in-cell pipe, a partial fix) is NOT tolerated, so a pin
// cannot hide a worse defect than the one it names.
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
// pipeTables must carry the same cell count as its own header, except the rows
// pinned in pendingPipeFix at exactly their recorded count.
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
				t.Logf("%s:%d: known-broken row tolerated at its pinned count (got %d cells, header line %d is %d): %s",
					rel, m.line, m.got, m.header, m.want, pin.why)
				tolerated++
				continue
			}
			t.Errorf("%s:%d: row splits into %d cells but its header (line %d) has %d — a raw `|` "+
				"inside a backticked code span ENDS the cell (GFM 4.10, Example 200), so GitHub publishes a "+
				"broken table. Escape every in-cell pipe as `\\|` (change nothing else). A leading pipe is "+
				"NOT required (GFM 4.10, Example 199) and this row's count is measured without assuming one. "+
				"Row: %.140q",
				rel, m.line, m.got, m.header, m.want, m.text)
		}
	}
	if totalTables == 0 {
		t.Fatalf("no markdown table found in %v", pipeTables)
	}
	t.Logf("%d table(s), %d rows checked for a uniform cell count (%d known-broken rows tolerated)",
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
		if !strings.Contains(l, "|") {
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
// fix (or adding another in-cell pipe) is caught, and a correctly escaped row
// stays green. It reads no file: the frozen fixtures cannot rot when the real rows
// are fixed.
//
// Every case is a table of header + delimiter + one data row, so the delimiter
// anchors the table (the fix's detection rule). The last two cases are the SAME
// row with and without its leading pipe and with the same over-count, proving the
// cell metric is leading-pipe-independent: both must be reported, or the guard is
// again blind to the no-leading-pipe convention GFM 4.10 allows.
func TestTablePipeDetectorBites(t *testing.T) {
	header := "| Harness | Proves | Cases | Wall time here |"
	if got := cellCount(header); got != 4 {
		t.Fatalf("fixture header splits into %d cells, want 4 — the fixture is wrong, not the code", got)
	}
	delim := "|---|---|---|---|"
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
		{"an over-count row WITHOUT a leading pipe is still a table row and is caught (the hole)",
			"`api/scripts/biteproof_nolead_b.py` | probe `a | b` | also `c | d` | 2s |", true},
		{"the SAME over-count WITH a leading pipe (the control it must match)",
			"| `api/scripts/biteproof_nolead_a.py` | probe `a | b` | also `c | d` | 2s |", true},
		{"a no-leading-pipe row at the header count stays green",
			"`api/scripts/biteproof_ok.py` | probe `a` | nothing | 2s |", false},
	}
	// The two no-leading-pipe rows above must measure the SAME cell count as their
	// leading-pipe twins, or the fixture does not prove independence.
	if a, b := cellCount("`api/scripts/biteproof_nolead_b.py` | probe `a | b` | also `c | d` | 2s |"),
		cellCount("| `api/scripts/biteproof_nolead_a.py` | probe `a | b` | also `c | d` | 2s |"); a != b || a != 6 {
		t.Fatalf("fixture rows measure %d and %d cells, want both 6 — the fixture does not isolate the leading pipe", a, b)
	}
	for _, c := range cases {
		got, tables, rows := scanTablePipes([]string{header, delim, c.row})
		if tables != 1 {
			t.Errorf("%s: scan found %d tables, want 1", c.name, tables)
			continue
		}
		if rows != 1 {
			t.Errorf("%s: scan counted %d data rows, want 1 — the delimiter row must not be counted as data", c.name, rows)
		}
		if c.broken != (len(got) == 1) {
			t.Errorf("%s: %q -> mismatches=%d (broken=%v), want broken=%v",
				c.name, c.row, len(got), len(got) == 1, c.broken)
		}
	}
}

// TestTablePipesNeedsADelimiterAndClosesOnABlock pins the two directions the fix
// must NOT weaken: a table is not opened without its delimiter row, and an open
// table still CLOSES at a blank line, a block start, or a pipe-less line — so a
// pipe-carrying PROSE line below a table cannot be walked as a phantom row. Each
// case is the harness table followed by the line under test, and asserts on the
// row COUNT so a closed table is told apart from a mismatch that merely vanished.
func TestTablePipesNeedsADelimiterAndClosesOnABlock(t *testing.T) {
	header := "| Harness | Proves | Cases | Wall time here |"
	delim := "|---|---|---|---|"
	row := "| `api/scripts/biteproof_x.py` | x | 1 | 1s |"
	cases := []struct {
		name      string
		lines     []string
		wantTable int
		wantRows  int
	}{
		{"two header-shaped lines with NO delimiter are not a table",
			[]string{header, row}, 0, 0},
		{"a table opens on its delimiter and counts only the data rows",
			[]string{header, delim, row}, 1, 1},
		{"a BLANK line closes the table; a pipe-carrying prose line below is not a row",
			[]string{header, delim, row, "", "legs: `a | b` then `c | d` prose"}, 1, 1},
		{"a HEADING closes the table even when it carries a pipe",
			[]string{header, delim, row, "## a | b heading"}, 1, 1},
		{"a pipe-less line closes the table",
			[]string{header, delim, row, "bar"}, 1, 1},
		{"a fenced code block closes the table",
			[]string{header, delim, row, "```", "x | y", "```"}, 1, 1},
	}
	for _, c := range cases {
		got, tables, rows := scanTablePipes(c.lines)
		if tables != c.wantTable || rows != c.wantRows || len(got) != 0 {
			t.Errorf("%s: tables=%d rows=%d mismatches=%d, want tables=%d rows=%d mismatches=0",
				c.name, tables, rows, len(got), c.wantTable, c.wantRows)
		}
	}
}

// TestPipePinCannotHideAWorseDefect proves the pinned-count rule: a pinned row
// tolerated at exactly the count recorded in pendingPipeFix, but a pinned row whose
// count has MOVED — one more in-cell pipe, or a partial fix that left another cell
// split — is not tolerated. Without this, a pin would be a name-based blanket over a
// row and could hide a worse defect on it.
func TestPipePinCannotHideAWorseDefect(t *testing.T) {
	const harness = "web/scripts/biteproof_docs_prose_convention.sh"
	const headerCells = 4 // the harness table's header carries 4 columns
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
		{"one MORE in-cell pipe", pin.pipes + 1, false},
		{"a partial fix, one fewer", pin.pipes - 1, false},
		{"back to the header count (the fix landed)", headerCells, false},
	}
	for _, c := range cases {
		m := pipeMismatch{line: 1, got: c.got, want: headerCells, header: 1, text: "| `" + harness + "` | x | y | z |"}
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
