package dbstore

// THE RULE THIS GUARD EXISTS TO KEEP — and the failure it prevents.
// Card t_c5832f47, the THIRD column of the `jsonb_array_elements` class
// (`sales.items`, card t_d8764334 / PR 820; `config->'blocks'`, card t_8fcb546d).
//
// `productImagesArray` (media_migrate.go) wraps a `products.images` expansion so a
// NON-ARRAY value in that JSONB column cannot make the extraction RAISE. When it
// raised, `MigrateMediaURLs` returned the error — and its caller is the API's OWN
// startup (cmd/jomform-api/main.go), which answers a non-nil error with
// `osExit(1)`. Measured on the pre-fix statement (see
// product_images_shape_test.go): `migrate products.images: ERROR: cannot extract
// elements from an object (SQLSTATE 22023)`. So ONE product row whose `images` is
// not an array AND whose text carries the old internal prefix did not fail one
// request — it stopped the process from booting at all. The helper's own doc
// comment states the rule that keeps the fix holding:
//
//	Keep EVERY expansion of `products.images` going through this helper — a bare
//	`jsonb_array_elements_text(<col>)` on that column reintroduces the
//	startup-time failure it exists to prevent.
//
// Nothing asserted that rule, and the wrong guard (`COALESCE(col,'[]'::jsonb)`,
// which replaces SQL NULL only) is the one every sibling site had started from.
//
// BITE (run it): change productImagesArray's CASE to a `COALESCE`, drop the
// statement's `jsonb_typeof(images) = 'array'` WHERE gate, or add a bare
// `jsonb_array_elements_text(images)` anywhere in this package's non-test
// sources, and this test reds.
//
// SCOPE, stated because a guard is judged by what it MISSES:
//
//   - the PRIMARY check is not a text scan at all: it calls
//     `mediaImagesUpdateSQL` — the exact statement `migrateProductImages` hands
//     to `pool.Exec` — and asserts the guard is in it. A text walk cannot see
//     this site otherwise, because the statement's column arrives through the
//     helper rather than as a literal `images` inside the expansion.
//   - the text walk is the SECOND net, for a bare site written with a literal
//     column: this package's non-test sources and `internal/db/migrations/*.sql`.
//     Prose is excluded explicitly (a Go match must fall inside a string literal,
//     SQL line comments are stripped), because the helper's own doc comment names
//     the bare spelling it forbids.
//   - a future site that expands a column held in a VARIABLE is outside the text
//     walk. That is the boundary, and it is why the primary check exists.
//   - `internal/db/migrations` is walked for violations only. No migration
//     expands `products.images` today — `product_variations.images` is the same
//     class but is never expanded anywhere — so a `sqlGuarded == 0` fatal would
//     make this guard red on correct code the day it lands.
//   - an expansion of some OTHER column is not judged: `sales.items` and
//     `config->'blocks'` have their own guards in this package.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	// The expansions the guard judges. `_text` raises the same way.
	imagesExpansionRE = regexp.MustCompile(`(?is)jsonb_array_elements(?:_text)?\s*\(`)
	// What an argument has to mention to be judged: the column, either literally
	// or through the helper that is named after it.
	imagesArgumentRE = regexp.MustCompile(`(?is)\bimages\b|productImagesArray`)
	// The two spellings that count as guarded: the helper call (Go) and the
	// literal test the helper emits (SQL).
	imagesGuardRE = regexp.MustCompile(`(?is)productImagesArray|jsonb_typeof`)
)

func TestProductImagesExpansionsAreGuarded(t *testing.T) {
	// PREMISE 1 — asserted in Go rather than by reading text: the helper must
	// still ask what the value IS. A presence check (COALESCE, IS NULL) cannot
	// catch the non-NULL non-array that makes the extraction raise.
	helper := productImagesArray("images")
	if !strings.Contains(helper, "jsonb_typeof") {
		t.Fatalf("productImagesArray no longer asks jsonb_typeof — a presence check (COALESCE, IS NULL) "+
			"cannot catch the non-array value that makes the extraction raise, and that value is what "+
			"stops cmd/jomform-api from booting. Helper that renders the expansion: %q", helper)
	}

	// PREMISE 2 — the statement the migration actually runs is built from that
	// helper. This is the assertion a text walk cannot make: the column reaches
	// the expansion through the helper, so nothing in the raw source says
	// `jsonb_array_elements_text(images)`.
	if !strings.Contains(mediaImagesUpdateSQL, helper) {
		t.Fatalf("mediaImagesUpdateSQL is no longer built from productImagesArray(%q) — the statement the "+
			"startup migration runs expands `products.images` by some other spelling, which is exactly the "+
			"drift this guard exists to catch. Statement:\n%s", "images", mediaImagesUpdateSQL)
	}

	// PREMISE 3 — and it still SKIPS a row whose value is not an array, instead
	// of rewriting it to `[]`. Both readings boot; only this one keeps a value
	// the migration cannot interpret. product_images_shape_test.go pins the
	// behaviour on a real Postgres; this pins the statement, so the two cannot
	// drift apart in silence.
	if !strings.Contains(mediaImagesUpdateSQL, "AND jsonb_typeof(images) = 'array'") {
		t.Fatalf("mediaImagesUpdateSQL no longer gates on `jsonb_typeof(images) = 'array'` — without it a "+
			"row whose images is not an array would be REWRITTEN to '[]' (the CASE alone does that), "+
			"destroying a value the migration cannot interpret. Statement:\n%s", mediaImagesUpdateSQL)
	}

	guarded, violations := scanImagesExpansions(t, ".", []string{".go"}, true)
	_, sqlViolations := scanImagesExpansions(t, filepath.Join("..", "db", "migrations"), []string{".sql"}, false)

	if guarded == 0 {
		t.Fatalf("no guarded expansion of `products.images` found in this package — the walk is vacuous " +
			"(the statement moved, or the argument extraction broke), so it can no longer prove anything")
	}
	t.Logf("products.images expansion walk: %d guarded site(s) in this package, %d violation(s); "+
		"migrations walked for violations only (%d found)", guarded, len(violations), len(sqlViolations))
	for _, v := range append(violations, sqlViolations...) {
		t.Errorf("%s", v)
	}
	if len(violations)+len(sqlViolations) > 0 {
		t.Errorf("\nA bare `jsonb_array_elements_text(<products.images>)` RAISES on a non-array value "+
			"(SQLSTATE 22023), and the caller of this migration is the API's OWN startup: a non-nil error "+
			"there is osExit(1), so the process does not boot at all. Route the expansion through "+
			"productImagesArray(\"images\") (Go) or a literal CASE WHEN jsonb_typeof(...) = 'array' (SQL). "+
			"Guarded sites found in this package: %d.", guarded)
	}
}

// scanImagesExpansions walks dir for files with the given extensions and returns
// how many expansions of an `images` column carry the guard, plus one message per
// expansion that does not.
//
// It reads the raw source rather than the AST: the SQL lives in Go string
// literals that are concatenated across a helper CALL, so only the raw text keeps
// the expansion and its guard in one piece. Prose is therefore excluded
// explicitly — for a Go file a match has to fall inside a string literal, and for
// SQL the line comments are stripped first.
//
// `sqlArgument`, `goStringLiteralRanges`, `insideAnyRange`, `stripSQLLineComments`
// and `oneLine` come from the sibling guards of the same class
// (form_blocks_guard_test.go, saleitems_guard_test.go): one implementation of
// each, not a third copy.
func scanImagesExpansions(t *testing.T, dir string, exts []string, skipTests bool) (int, []string) {
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
		if skipTests && strings.HasSuffix(name, "_test.go") {
			continue
		}
		matched := false
		for _, ext := range exts {
			if strings.HasSuffix(name, ext) {
				matched = true
			}
		}
		if !matched {
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
		for _, loc := range imagesExpansionRE.FindAllStringIndex(text, -1) {
			if isGo && !insideAnyRange(loc[0], literals) {
				// A doc comment naming the expansion is not a SQL site
				// (productImagesArray's own doc comment names it).
				continue
			}
			arg := sqlArgument(text, loc[1])
			if !imagesArgumentRE.MatchString(arg) {
				// An expansion of some other column: not this rule's business.
				continue
			}
			if imagesGuardRE.MatchString(arg) {
				guarded++
				continue
			}
			violations = append(violations, filepath.Join(dir, name)+": "+
				"unguarded `jsonb_array_elements(<products.images>)`: "+truncate(oneLine(arg), 90))
		}
	}
	sort.Strings(violations)
	return guarded, violations
}
