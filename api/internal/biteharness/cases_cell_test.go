package biteharness

// The `Cases` column guard (card t_dc646914).
//
// THE HOLE THIS CLOSES. documentedHarnesses (inventory_test.go) reads column 1
// only — the backticked harness path. Nothing read the `Cases` cell, so a row
// could publish a case count its own harness contradicts while every gate stayed
// green. Measured on origin/main e19abcb4, before the doc fix landed, the
// `scripts/biteproof_preflight_reuse.sh` row's Cases cell said 12 while
//
//	grep -c '^# --- case [0-9]' scripts/biteproof_preflight_reuse.sh   # 13
//
// and `go test ./internal/biteharness/ -count=1` reported ok. The cell had been
// one short since PR #1089 added cases 10 AND 11 in one change and bumped the
// cell once; the next change inherited the offset (11 -> 12 against markers
// 12 -> 13). This is the pipe-count hole table_pipes_test.go closed, one column
// over: the parser read the row, and the number in it was never read.
//
// WHAT IS CHECKED. The `Cases` cell is derivable only for the marker family a
// harness labels `# --- case N`: there the count is `grep -c '^# --- case [0-9]'`,
// and BITE-HARNESSES.md's intro states that convention (control `case 0`
// included). This guard applies it to the rows that actually CARRY that family —
// a harness whose `# --- case` numbers are exactly 0..N with no gap and no
// duplicate. Four rows qualify on this tree and all four agree:
//
//	scripts/biteproof_api_test_gate_timeout.sh          6/6
//	scripts/biteproof_web_image_gate.sh                 3/3
//	scripts/biteproof_preflight_reuse.sh               13/13
//	web/scripts/biteproof_workspace_danger_refusal.sh   3/3
//
// WHAT IS NOT CHECKED, and it must not be described as checked. The rest of the
// column is NOT derivable from one convention — measured on c36b5e22:
// web/scripts/biteproof_npm_audit_gate.sh prints FIVE `echo "== case N"` headers
// (case 0 a control) and drives four mutations through its own `run_case` helper,
// so its cell of 4 counts sabotages rather than markers — a marker-counting rule
// would demand 5 there and red a correct row; web/scripts/biteproof_next_managed_files.sh
// labels them in prose (`# Case 0:`, `# --- case 6:` — ONE `# --- case` marker
// against a cell of 6, which is why a bare "any marker means covered" rule would
// have redded a correct row); deploy/biteproof_build_and_ship_gate.sh uses
// `# case A`..`# case D` plus a fifth leg (5, cell 5); and the api/scripts/*.py
// harnesses carry no `# --- case` line at all. So those rows stay claims, not
// checks, and SCOPE LIMITS in inventory_test.go records that with these numbers.
// A guard that claimed the whole column would be a false claim inside the very
// document this card is about.
//
// THE OTHER DIRECTION. A narrative line stating `case count is **N**` must equal
// the `Cases` cell of the harness its PARAGRAPH names. The doc's intro promises
// the narrative and the cell move together; nothing checked it. Attribution is
// paragraph-scoped and REFUSES to guess: a narrative line whose paragraph names no
// registered row, or more than one, is a failure, not a skip.
//
// COVERAGE FLOOR. A guard that quietly stops covering rows passes vacuously, so
// the covered-row count is floored and every run logs which rows it read.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// caseMarkerRe matches the marker line the documented convention counts, anchored
// at column 0 exactly as `grep -c '^# --- case [0-9]'` is. An indented marker and
// the plural `# --- cases 4-5:` heading (biteproof_next_managed_files.sh carries
// one) are deliberately NOT counted: both are comments ABOUT cases, not case
// starts, and counting them would make the guard disagree with the command the
// document tells a reader to run.
var caseMarkerRe = regexp.MustCompile(`(?m)^# --- case ([0-9]+)`)

// narrativeCaseCountRe matches the doc's narrative claim. Both the emphasis and
// the wording are fixed by the document's own convention (BITE-HARNESSES.md's
// intro and the one line under the reuse harness's row), so a claim written a
// third way is invisible here rather than mis-read; the coverage floor on this
// leg is what stops that becoming a silent hole.
var narrativeCaseCountRe = regexp.MustCompile(`case count is\s*\*\*([0-9]+)\*\*`)

// caseMarkerNumbers returns every case number a harness labels with the documented
// `# --- case N` marker, in file order.
func caseMarkerNumbers(text string) []int {
	var nums []int
	for _, m := range caseMarkerRe.FindAllStringSubmatch(text, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue // unreachable: the capture group is [0-9]+
		}
		nums = append(nums, n)
	}
	return nums
}

// carriesCaseMarkerConvention reports whether text labels its cases with the
// documented `# --- case N` marker for EVERY n in 0..N — no gap, no duplicate —
// and returns the marker count (`what grep -c returns`) when it does.
//
// A harness that only mentions the marker in passing is NOT covered rather than
// mis-measured: web/scripts/biteproof_next_managed_files.sh carries a single
// `# --- case 6:` heading over a suite labelled in prose, and a "one marker means
// covered" rule would have demanded the cell say 1 where the harness really runs
// 6 cases — a false RED on a correct row, which is how a guard gets pinned out of
// the way instead of fixed. Numbering that starts anywhere else or skips a number
// is the same: not this convention, so not this check.
func carriesCaseMarkerConvention(text string) (int, bool) {
	nums := caseMarkerNumbers(text)
	if len(nums) == 0 {
		return 0, false
	}
	seen := make(map[int]bool, len(nums))
	highest := -1
	for _, n := range nums {
		if seen[n] {
			return 0, false // the same case numbered twice is not the convention
		}
		seen[n] = true
		if n > highest {
			highest = n
		}
	}
	for n := 0; n <= highest; n++ {
		if !seen[n] {
			return 0, false // a gap: the numbering is not 0..N
		}
	}
	return len(nums), true
}

// casesCellVerdict compares a row's `Cases` cell with the marker count, returning
// a reason when they disagree. Pure, so the fixture test can drive it without a
// file: a cell that is prose ("3 of 8 run", "5 cases (14 assertions)") cannot be
// verified against a count and is reported rather than skipped, because a covered
// row whose cell stopped being a number is exactly how this check would go quiet.
func casesCellVerdict(cell string, count int) (string, bool) {
	trimmed := strings.TrimSpace(cell)
	n, err := strconv.Atoi(trimmed)
	if err != nil {
		return "the `Cases` cell " + strconv.Quote(trimmed) +
			" is not a number, so it cannot be checked against the harness's " +
			strconv.Itoa(count) + " `# --- case N` marker(s) — write the marker count, " +
			"or explain the exception in SCOPE LIMITS (inventory_test.go)", false
	}
	if n != count {
		return "the `Cases` cell says " + strconv.Itoa(n) + " but the harness carries " +
			strconv.Itoa(count) + " `# --- case N` marker(s) (`grep -c '^# --- case [0-9]' <that " +
			"harness>`) — the row publishes a count its own harness contradicts", false
	}
	return "", true
}

// harnessTableRow is one data row of the harness table, with its 1-based source
// line so a failure names the line an editor has to open.
type harnessTableRow struct {
	line  int
	name  string
	cells []string
}

// rowCells splits a markdown table row into TRIMMED cells by GFM 4.10's rule,
// reusing table_pipes_test.go's splitter so an escaped `\|` stays content and the
// column indexes cannot drift from the pipe guard's. One empty boundary cell at
// each end is dropped, which makes the split independent of whether the row wrote
// its leading pipe.
func rowCells(row string) []string {
	parts := splitUnescapedPipes(strings.TrimSpace(row))
	for len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
		parts = parts[1:]
	}
	for len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// harnessTableRows parses the harness table — the one whose header's first column
// is `Harness` — into its header cells and its data rows. The walk mirrors
// documentedHarnesses: it starts at that header, treats a line with no pipe as the
// table's end, skips delimiter rows, and fails on a row whose first column names
// no backticked harness (a row that parses to nothing is how the table reads
// complete while covering nothing). TestCasesCellParserAgreesWithTheRegister
// keeps the two parsers from drifting apart.
func harnessTableRows(t *testing.T, path string) (header []string, rows []harnessTableRow) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — a guard that reads nothing proves nothing", path, err)
	}
	lines := strings.Split(string(raw), "\n")
	headerLine := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := rowCells(trimmed)
		if len(cells) >= 2 && cells[0] == "Harness" {
			header, headerLine = cells, i
			break
		}
	}
	if headerLine < 0 {
		t.Fatalf("%s has no table whose first column is `Harness` — without it this guard "+
			"would read no row and report green", path)
	}
	for i := headerLine + 1; i < len(lines); i++ {
		line := lines[i]
		if !strings.Contains(line, "|") {
			break // the table ended
		}
		trimmed := strings.TrimSpace(line)
		if isDelimRow(trimmed) {
			continue
		}
		spans := backtickRe.FindAllStringSubmatch(firstColumn(trimmed), -1)
		if len(spans) == 0 {
			t.Fatalf("%s:%d: table row %q names no harness — the first column must be "+
				"backticked, or the row cannot be attributed to anything", path, i+1, trimmed)
		}
		rows = append(rows, harnessTableRow{line: i + 1, name: spans[0][1], cells: rowCells(trimmed)})
	}
	return header, rows
}

// casesColumnIndex finds the `Cases` column by name in the table's OWN header,
// never by a hardcoded position: a table reordered under this guard must fail
// loudly here rather than silently compare the Proves prose to a number.
func casesColumnIndex(t *testing.T, rel string, header []string) int {
	t.Helper()
	for i, c := range header {
		if c == "Cases" {
			return i
		}
	}
	t.Fatalf("%s: the harness table's header is %q, which has no `Cases` column — this "+
		"guard reads the cell by name, so it cannot run against a table that does not "+
		"carry one", rel, header)
	return -1
}

// coveredCaseRowsFloor is the number of rows that must carry the `# --- case N`
// convention for this guard to claim anything. It is the MEASURED count, not a
// round number below it: four rows qualify on this tree, so losing one — a
// harness's markers rewritten into prose, which is exactly what the drift looks
// like — fails here instead of quietly shrinking the check. Adding a harness in
// this family raises the count and never trips it; a row leaving the family must
// be justified, with the measurement, in SCOPE LIMITS.
const coveredCaseRowsFloor = 4

// TestCasesCellMatchesTheHarnessCaseMarkerCount is the guard: for every row whose
// harness numbers its cases `# --- case 0..N`, the row's `Cases` cell must BE that
// number.
func TestCasesCellMatchesTheHarnessCaseMarkerCount(t *testing.T) {
	root := repoRoot(t)
	rel := inventoryFile
	doc := filepath.Join(root, rel)
	header, rows := harnessTableRows(t, doc)
	if len(rows) < 10 {
		t.Fatalf("%s parsed to %d data rows — the table or this parser changed shape and "+
			"the check below would prove nothing", rel, len(rows))
	}
	casesIdx := casesColumnIndex(t, rel, header)

	var covered []string
	for _, row := range rows {
		if casesIdx >= len(row.cells) {
			t.Errorf("%s:%d: row %q has %d cells, too few for the `Cases` column (index %d) — "+
				"the pipe guard reports the shape; this guard cannot read the cell",
				rel, row.line, row.name, len(row.cells), casesIdx)
			continue
		}
		harnessPath := filepath.Join(root, filepath.FromSlash(row.name))
		raw, err := os.ReadFile(harnessPath)
		if err != nil {
			// Absent harnesses are TestInventoryRowsPointAtRealHarnesses' business;
			// there is no text here to count, so this leg simply cannot speak.
			t.Logf("%s:%d: `%s` is not readable on this tree (%v) — its `Cases` cell is not "+
				"checked here", rel, row.line, row.name, err)
			continue
		}
		count, ok := carriesCaseMarkerConvention(string(raw))
		if !ok {
			continue // a different marker family: the cell stays a claim
		}
		covered = append(covered, row.name)
		if why, good := casesCellVerdict(row.cells[casesIdx], count); !good {
			t.Errorf("%s:%d: `%s`: %s", rel, row.line, row.name, why)
		}
	}
	sort.Strings(covered)
	if len(covered) < coveredCaseRowsFloor {
		t.Fatalf("%s: only %d row(s) carry the `# --- case 0..N` convention (%v), below the "+
			"floor of %d — this guard would report green over almost nothing. Either the "+
			"marker family was rewritten (then say so in SCOPE LIMITS and move the floor, "+
			"with the measurement) or the parse is broken", rel, len(covered), covered,
			coveredCaseRowsFloor)
	}
	t.Logf("%s: %d of %d rows carry the `# --- case 0..N` convention and had their `Cases` "+
		"cell checked against the marker count: %v (the other %d rows are in a marker "+
		"family this rule cannot derive — SCOPE LIMITS records them as claims)",
		rel, len(covered), len(rows), covered, len(rows)-len(covered))
}

// TestNarrativeCaseCountMatchesItsRowsCasesCell checks the document's OTHER
// statement of the same number: a line saying `case count is **N**` must equal the
// `Cases` cell of the harness named in its own paragraph. That is the promise
// BITE-HARNESSES.md's intro makes ("its narrative line below the table repeats
// that number"), and the reuse row's narrative was nine cases behind the script
// while this register read green.
//
// The paragraph is the attribution scope — nearest blank lines on both sides —
// because the narrative sits under the harness's own section and names it there.
// A paragraph naming no registered row, or two, is a failure: guessing which row a
// number belongs to is how a claim gets checked against the wrong harness.
func TestNarrativeCaseCountMatchesItsRowsCasesCell(t *testing.T) {
	root := repoRoot(t)
	rel := inventoryFile
	doc := filepath.Join(root, rel)
	raw, err := os.ReadFile(doc)
	if err != nil {
		t.Fatalf("read %s: %v — the narrative cannot be checked without the document", doc, err)
	}
	header, rows := harnessTableRows(t, doc)
	casesIdx := casesColumnIndex(t, rel, header)
	byName := make(map[string]harnessTableRow, len(rows))
	for _, row := range rows {
		byName[row.name] = row
	}

	lines := strings.Split(string(raw), "\n")
	found := 0
	for i, line := range lines {
		for _, m := range narrativeCaseCountRe.FindAllStringSubmatch(line, -1) {
			found++
			claimed, cerr := strconv.Atoi(m[1])
			if cerr != nil {
				t.Fatalf("%s:%d: internal: the regex captured %q, which is not a number", rel, i+1, m[1])
			}
			start, end := i, i
			for start > 0 && strings.TrimSpace(lines[start-1]) != "" {
				start--
			}
			for end < len(lines)-1 && strings.TrimSpace(lines[end+1]) != "" {
				end++
			}
			paragraph := strings.Join(lines[start:end+1], "\n")

			var named []string
			for _, row := range rows {
				if strings.Contains(paragraph, row.name) {
					named = append(named, row.name)
				}
			}
			if len(named) == 0 {
				t.Errorf("%s:%d: a narrative line claims the case count is %d but its paragraph "+
					"(lines %d-%d) names no harness row, so the claim can be checked against "+
					"nothing — name the harness in that paragraph", rel, i+1, claimed, start+1, end+1)
				continue
			}
			if len(named) > 1 {
				t.Errorf("%s:%d: a narrative line claims the case count is %d but its paragraph "+
					"(lines %d-%d) names %d harness rows (%v), so it cannot be attributed — "+
					"split the paragraph so each count sits beside its own harness",
					rel, i+1, claimed, start+1, end+1, len(named), named)
				continue
			}
			row := byName[named[0]]
			if casesIdx >= len(row.cells) {
				t.Errorf("%s:%d: `%s` has %d cells, too few for the `Cases` column (index %d)",
					rel, row.line, row.name, len(row.cells), casesIdx)
				continue
			}
			cell := row.cells[casesIdx]
			cellN, aerr := strconv.Atoi(cell)
			if aerr != nil {
				t.Errorf("%s:%d: the narrative says the case count is %d, but `%s`'s `Cases` cell "+
					"(line %d) is %q — not a number, so the two cannot move together",
					rel, i+1, claimed, row.name, row.line, cell)
				continue
			}
			if claimed != cellN {
				t.Errorf("%s:%d: the narrative says the case count is %d but `%s`'s `Cases` cell "+
					"(line %d) says %d — the two are stated in the same document and must move "+
					"together", rel, i+1, claimed, row.name, row.line, cellN)
			}
		}
	}
	if found == 0 {
		t.Fatalf("%s carries no `case count is **N**` line — the intro promises the narrative "+
			"repeats the row's number, so this leg would pass vacuously. Restore the sentence "+
			"or delete this test and say why in SCOPE LIMITS", rel)
	}
	t.Logf("%s: %d narrative case-count claim(s) checked against their row's `Cases` cell", rel, found)
}

// TestCasesCellParserAgreesWithTheRegister keeps this file's table parser and
// inventory_test.go's documentedHarnesses from drifting apart: both read the same
// table for different columns, and a change that makes them disagree about which
// rows exist would let the cell check name a row the register does not, or miss one
// it does.
func TestCasesCellParserAgreesWithTheRegister(t *testing.T) {
	root := repoRoot(t)
	doc := filepath.Join(root, inventoryFile)
	_, rows := harnessTableRows(t, doc)
	var mine []string
	for _, row := range rows {
		mine = append(mine, row.name)
	}
	sort.Strings(mine)
	register := documentedHarnesses(t, doc) // already sorted
	if len(mine) != len(register) {
		t.Fatalf("this file's parser reads %d rows and documentedHarnesses reads %d — the two "+
			"parsers must see the same table, or the `Cases` check and the register disagree "+
			"about what exists", len(mine), len(register))
	}
	for i := range mine {
		if mine[i] != register[i] {
			t.Fatalf("row %d: this file's parser reads %q and documentedHarnesses reads %q — "+
				"the two parsers have drifted", i, mine[i], register[i])
		}
	}
	t.Logf("both parsers read the same %d rows of %s", len(mine), inventoryFile)
}

// TestCasesCellCheckBites drives the predicate on frozen fixtures, so reverting
// the rule (or widening it into the families it cannot derive from) is caught
// without touching a real harness. Every case is the marker text a harness would
// carry plus the `Cases` cell its row would publish.
func TestCasesCellCheckBites(t *testing.T) {
	markers := func(nums ...int) string {
		var b strings.Builder
		for _, n := range nums {
			b.WriteString("# --- case " + strconv.Itoa(n) + ": a case\n...\n")
		}
		return b.String()
	}
	cases := []struct {
		name        string
		text        string
		wantCount   int
		wantCovered bool
	}{
		{"0..12 (the reuse harness today): 13 markers", markers(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12), 13, true},
		{"0..5 (the api-test-gate harness): 6 markers", markers(0, 1, 2, 3, 4, 5), 6, true},
		{"0..2 (the web-image and workspace-danger harnesses): 3 markers", markers(0, 1, 2), 3, true},
		{"a single marker numbered 6 (the next-managed-files shape): NOT covered", markers(6), 0, false},
		{"a gap 0..5 then 9: NOT covered", markers(0, 1, 2, 3, 4, 5, 9), 0, false},
		{"the same case numbered twice: NOT covered", markers(0, 1, 1, 2), 0, false},
		{"numbering that starts at 1: NOT covered", markers(1, 2, 3), 0, false},
		{"no marker at all (the python harnesses): NOT covered", "# case one\n# case two\n", 0, false},
		{"the PLURAL heading `# --- cases 4-5:` is not a marker",
			"# --- cases 4-5: the tracked half\n# --- case 6: the skip\n", 0, false},
		{"an INDENTED marker is not at column 0 and is not counted",
			"  # --- case 7: indented\n", 0, false},
	}
	for _, c := range cases {
		count, covered := carriesCaseMarkerConvention(c.text)
		if covered != c.wantCovered || count != c.wantCount {
			t.Errorf("%s: carriesCaseMarkerConvention = (%d, %v), want (%d, %v)",
				c.name, count, covered, c.wantCount, c.wantCovered)
		}
	}

	verdicts := []struct {
		name  string
		cell  string
		count int
		want  bool
	}{
		{"13 markers, cell 13 (the fixed row)", "13", 13, true},
		{"13 markers, cell 12 (THE CARDED DEFECT — one short since PR #1089)", "12", 13, false},
		{"6 markers, cell 6", "6", 6, true},
		{"6 markers, cell 5", "5", 6, false},
		{"3 markers, cell 3", "3", 3, true},
		{"a prose cell cannot be verified, so it is reported", "3 of 8 run", 3, false},
		{"a cell with a qualifier is not a bare number", "5 cases (14 assertions)", 5, false},
		{"an empty cell is not a number", "", 3, false},
		{"surrounding spaces are tolerated", " 13 ", 13, true},
	}
	for _, v := range verdicts {
		why, ok := casesCellVerdict(v.cell, v.count)
		if ok != v.want {
			t.Errorf("%s: casesCellVerdict(%q, %d) = (%q, %v), want ok=%v",
				v.name, v.cell, v.count, why, ok, v.want)
		}
		if !ok && why == "" {
			t.Errorf("%s: a failing verdict must carry a reason", v.name)
		}
	}

	// The failure message has to name both numbers, or an operator reading a red
	// cannot tell which side to change — the same rule the pipe guard's message
	// follows.
	why, ok := casesCellVerdict("12", 13)
	if ok || !strings.Contains(why, "12") || !strings.Contains(why, "13") {
		t.Errorf("the mismatch message must name both the cell and the marker count; got %q", why)
	}
}
