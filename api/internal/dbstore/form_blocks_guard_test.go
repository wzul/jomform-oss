package dbstore

// THE RULE THIS GUARD EXISTS TO KEEP — and the failure it prevents.
// Card t_8fcb546d, the second column of the `sales.items` class (t_d8764334).
//
// `formBlocksArray` (form.go) wraps a `config->'blocks'` expansion so a NON-ARRAY
// value in that JSON key cannot make the extraction RAISE. When it raised, the
// gallery-metadata predicate failed the WHOLE statement — and the statement
// covers an entire id set (`BlockedGalleryTemplates` / `SetStatusMany`, both
// `id = ANY($2)`), so ONE malformed row stopped a bulk publish for every id in
// the batch while the caller saw 500 {"code":"internal"} (measured by the QA card,
// reproduced on pristine origin/main 376a50c9). The helper's own doc comment
// states the rule that keeps the fix holding:
//
//	Keep EVERY expansion of `config->'blocks'` going through this helper — a bare
//	`jsonb_array_elements(config->'blocks')` reintroduces the batch-wide failure
//	it exists to prevent.
//
// Nothing asserted that rule, and the wrong guard (`COALESCE(col,'[]'::jsonb)`,
// which replaces SQL NULL only) had been copied into the 000060 migration pair
// as well as the predicate — four sites, no test.
//
// BITE (run it): change formBlocksArray's CASE to `COALESCE(exactly)` or add a
// bare `jsonb_array_elements(config->'blocks')` to any dbstore query, and this
// test reds naming the file and the fragment.
//
// SCOPE, stated because a guard is judged by what it MISSES: the walk covers this
// package's non-test SOURCES and `internal/db/migrations/*.sql` — the places the
// predicate and its migration copies live. Prose is excluded explicitly (a Go
// match must fall inside a string literal, SQL line comments are stripped): this
// helper's own doc comment names `jsonb_array_elements(config->'blocks')`, and
// judging prose would make the guard red on the explanation of its own rule. An
// expansion of some OTHER JSON key is not judged — `sales.items` has its own guard
// in saleitems_guard_test.go.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	// The expansions the guard judges. `_text` raises the same way.
	blocksExpansionRE = regexp.MustCompile(`(?is)jsonb_array_elements(?:_text)?\s*\(`)
	// What an argument has to mention to be judged.
	blocksArgumentRE = regexp.MustCompile(`(?is)'blocks'`)
	// The two spellings that count as guarded: the helper call (Go) and the
	// literal test the helper emits (SQL).
	blocksGuardRE = regexp.MustCompile(`(?is)formBlocksArray|jsonb_typeof`)
)

func TestConfigBlocksExpansionsAreGuarded(t *testing.T) {
	// PREMISE, and it is asserted in Go rather than by reading text: the helper
	// must still ask what the value IS (a presence check such as COALESCE cannot
	// catch the non-NULL non-array that makes the extraction raise), and the
	// predicate must actually be built from it.
	helper := formBlocksArray("config->'blocks'")
	if !strings.Contains(helper, "jsonb_typeof") {
		t.Fatalf("formBlocksArray no longer asks jsonb_typeof — a presence check (COALESCE, IS NULL) "+
			"cannot catch the non-array value that makes the extraction raise. Helpers that render "+
			"the expansion: %q", helper)
	}
	if !strings.Contains(galleryMetadataMissingSQL, helper) {
		t.Fatalf("galleryMetadataMissingSQL is no longer built from formBlocksArray(%q) — the predicate "+
			"expands `config->'blocks'` by some other spelling, which is exactly the drift this guard "+
			"exists to catch", "config->'blocks'")
	}

	guarded, violations := scanBlocksExpansions(t, ".", []string{".go"}, true)
	sqlGuarded, sqlViolations := scanBlocksExpansions(t, filepath.Join("..", "db", "migrations"), []string{".sql"}, false)

	if guarded == 0 {
		t.Fatalf("no guarded expansion of `config->'blocks'` found in this package — the walk is " +
			"vacuous (the predicate moved, or the argument extraction broke), so it can no longer " +
			"prove anything")
	}
	if sqlGuarded == 0 {
		t.Fatalf("no guarded expansion of `config->'blocks'` found in internal/db/migrations — the " +
			"walk is looking in the wrong place, or the 000060 migration pair lost its guard")
	}
	for _, v := range append(violations, sqlViolations...) {
		t.Errorf("%s", v)
	}
	if len(violations)+len(sqlViolations) > 0 {
		t.Errorf("\nA bare `jsonb_array_elements(<config->'blocks'>)` RAISES on a non-array value "+
			"(SQLSTATE 22023), which fails the WHOLE statement — and the gallery-metadata statements "+
			"cover an entire id set, so one odd row stops a bulk publish for every id in the batch. "+
			"Route the expansion through formBlocksArray(\"config->'blocks'\") (Go) or the literal "+
			"CASE WHEN jsonb_typeof(...) = 'array' (SQL). Guarded sites found: %d in this package, "+
			"%d in the migrations.", guarded, sqlGuarded)
	}
}

// scanBlocksExpansions walks dir for files with the given extensions and returns
// how many expansions of `config->'blocks'` carry the guard, plus one message per
// expansion that does not.
//
// It reads the raw source rather than the AST: the SQL lives in Go string
// literals that are concatenated across a helper CALL, so only the raw text keeps
// the expansion and its guard in one piece. Prose is therefore excluded
// explicitly — for a Go file a match has to fall inside a string literal, and for
// SQL the line comments are stripped first.
func scanBlocksExpansions(t *testing.T, dir string, exts []string, skipTests bool) (int, []string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	guarded := 0
	var violations []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		isGo := strings.HasSuffix(name, ".go")
		matched := false
		for _, ext := range exts {
			if strings.HasSuffix(name, ext) {
				matched = true
			}
		}
		if !matched {
			continue
		}
		if skipTests && strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(dir, name))
		if rerr != nil {
			t.Fatalf("read %s/%s: %v", dir, name, rerr)
		}
		text := string(raw)
		var literals [][2]int
		if isGo {
			literals = goStringLiteralRanges(t, filepath.Join(dir, name), raw)
		} else {
			text = stripSQLLineComments(text)
		}
		for _, loc := range blocksExpansionRE.FindAllStringIndex(text, -1) {
			if isGo && !insideAnyRange(loc[0], literals) {
				// A doc comment naming the expansion is not a SQL site (measured:
				// formBlocksArray's own doc comment names it).
				continue
			}
			arg := sqlArgument(text, loc[1])
			if !blocksArgumentRE.MatchString(arg) {
				// An expansion of some other JSON key: not this rule's business.
				continue
			}
			if blocksGuardRE.MatchString(arg) {
				guarded++
				continue
			}
			violations = append(violations, filepath.Join(dir, name)+": "+
				"unguarded `jsonb_array_elements(<config->'blocks'>)`: "+truncate(oneLine(arg), 90))
		}
	}
	sort.Strings(violations)
	return guarded, violations
}

// goStringLiteralRanges returns the byte range of every string literal in a Go
// source, so a match can be required to fall inside one — a comment mention is
// then never mistaken for a SQL site.
func goStringLiteralRanges(t *testing.T, path string, src []byte) [][2]int {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var out [][2]int
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		out = append(out, [2]int{fset.Position(lit.Pos()).Offset, fset.Position(lit.End()).Offset})
		return true
	})
	return out
}

// stripSQLLineComments blanks `-- …` to the end of the line, leaving single-quoted
// literals alone (a `--` inside one is data, not a comment). Newlines are kept, so
// the offsets of everything else are unchanged.
func stripSQLLineComments(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	inQuote := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c == '\'':
			inQuote = !inQuote
			b.WriteByte(c)
		case !inQuote && c == '-' && i+1 < len(text) && text[i+1] == '-':
			for i < len(text) && text[i] != '\n' {
				i++
			}
			if i < len(text) {
				b.WriteByte('\n')
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// insideAnyRange reports whether off falls inside one of the ranges.
func insideAnyRange(off int, ranges [][2]int) bool {
	for _, r := range ranges {
		if off >= r[0] && off < r[1] {
			return true
		}
	}
	return false
}

// sqlArgument returns the text of the expansion's argument, up to its closing
// paren (paren-balanced, so a nested call does not end it early).
//
// `oneLine` and `truncate` (used for the message) come from
// saleitems_guard_test.go — the sibling guard for the same class on
// `sales.items`, which is where this package keeps those two helpers.
func sqlArgument(text string, start int) string {
	depth := 0
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return strings.TrimSpace(text[start:i])
			}
			depth--
		}
	}
	return strings.TrimSpace(text[start:])
}
