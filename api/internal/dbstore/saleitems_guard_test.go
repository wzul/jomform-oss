package dbstore

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// THE RULE THIS GUARD EXISTS TO KEEP — and the failure it prevents. Card t_d8764334.
//
// `saleItemsArray` (product.go) wraps a `sales.items` expansion so a NON-ARRAY value in
// that JSONB column cannot make the extraction RAISE. When it raised, the account-wide
// product bin answered 500 `{"code":"internal"}` and the merchant saw an empty bin
// (#820, merge 99c6aa79). The helper's own doc comment states the rule that keeps the
// fix holding:
//
//	Keep EVERY expansion of `sales.items` going through this helper — a bare
//	`jsonb_array_elements(<col>)` on that column reintroduces the account-wide
//	failure it exists to prevent.
//
// Nothing asserted that rule. Measured at 15337da0, all four expansions were guarded and
// no test mentioned the helper at all — so a FIFTH site, added tomorrow as
// `jsonb_array_elements(s.items)`, was invisible to every gate and the tests stayed green
// until a merchant with one odd row lost the whole product bin.
//
// BITE (run it): add a bare `jsonb_array_elements(s.items)` to SalesCountsByProduct and
// this test reds naming the file and the fragment.
//
// BOUNDARY, stated because a guard is judged by what it MISSES: an expansion's argument is
// judged by whether it MENTIONS the `items` column (`items`, `s.items`, `alias.items`),
// because that is the widest rule that cannot fire on an unrelated table's expansion. A
// sales row reached through a name that never says `items` is outside this rule.

var (
	// The expansions the guard judges. `_text` is included: it raises the same way.
	saleItemsExpansion = regexp.MustCompile(`(?is)jsonb_array_elements(?:_text)?\s*\(`)
	// The helper name, spelled here so a rename makes the PREMISE below fail loudly
	// instead of silently matching nothing.
	saleItemsHelperName = "saleItemsArray"
	// What an argument has to mention to be judged.
	saleItemsArgument = regexp.MustCompile(`(?is)\bitems\b`)
	// Stand-ins used while flattening a concatenation, so an argument's position can be
	// judged without resolving Go values.
	guardedPlaceholder = "\x00GUARDED\x00"
	opaquePlaceholder  = "\x00OPAQUE\x00"
)

// TestSalesItemsExpansionsGoThroughTheHelper is the drift guard. It parses this package's
// non-test sources — the same shape as `migration_scan_drift_test.go` — and fails when an
// expansion of `sales.items` is not routed through the helper.
func TestSalesItemsExpansionsGoThroughTheHelper(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	// PREMISE, stated so this guard cannot go vacuous silently.
	helperSeen := false
	// COUNTS, not a set: two call sites can carry identical text (two functions run the
	// same query), and a set would undercount them — measured here, four guarded
	// expansions looked like three.
	guardedSites := 0
	violations := map[string]bool{}

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, rerr := os.ReadFile(filepath.Join(".", name))
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, name, src, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}

		// The helper itself must keep asking what the value IS. A presence check
		// (COALESCE, IS NULL) cannot catch a NON-NULL non-array — the column is NOT
		// NULL, so COALESCE never fires, and a non-array is exactly what raises.
		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Name.Name != saleItemsHelperName || fn.Body == nil {
				return true
			}
			helperSeen = true
			if !strings.Contains(literalsOf(fn.Body), "jsonb_typeof") {
				t.Errorf("%s no longer asks jsonb_typeof — the helper is supposed to guard on what "+
					"the value IS; a presence check (COALESCE, IS NULL) cannot catch the non-array "+
					"value that makes the extraction raise", saleItemsHelperName)
			}
			return true
		})

		for _, text := range flattenedStrings(f) {
			for _, loc := range saleItemsExpansion.FindAllStringIndex(text, -1) {
				arg := argumentText(text, loc[1])
				if strings.Contains(arg, guardedPlaceholder) {
					guardedSites++
					continue
				}
				if !saleItemsArgument.MatchString(arg) {
					// An expansion of some OTHER column: the helper is about
					// `sales.items` only, and accusing every expansion would make this
					// guard fire on innocent code.
					continue
				}
				violations[name+": "+oneLine(truncate(readable(text[loc[0]:]), 90))] = true
			}
		}
	}

	if !helperSeen {
		t.Fatalf("%s was not found in this package — either it was renamed or moved, and this "+
			"guard now matches nothing. Fix the guard (update saleItemsHelperName), do not delete "+
			"it: the bare expansion it forbids still reintroduces the #820 bin outage.",
			saleItemsHelperName)
	}
	if guardedSites == 0 {
		t.Fatalf("no guarded expansion of `sales.items` found in this package at all — the walk "+
			"is vacuous (the queries moved, or the flattening broke), so it can no longer prove "+
			"anything. Premise: at least one call to %s() must be reachable here.",
			saleItemsHelperName)
	}
	if len(violations) > 0 {
		names := make([]string, 0, len(violations))
		for v := range violations {
			names = append(names, v)
		}
		// Deterministic order: a map iteration would shuffle the message run to run.
		sortStrings(names)
		t.Errorf("%d expansion(s) of `sales.items` do NOT go through %s():\n  %s\n\n"+
			"A bare `jsonb_array_elements(<col>)` on that column RAISES on a non-array value, "+
			"which fails the whole account-wide query: measured as 500 {\"code\":\"internal\"} "+
			"and an empty product bin (#820). Route the expansion through %s(<col>) — the CASE "+
			"jsonb_typeof(...) guard is what keeps the bin answering.",
			len(violations), saleItemsHelperName, strings.Join(names, "\n  "), saleItemsHelperName)
	}
}

// flattenedStrings returns each maximal `+` concatenation in the file, flattened to one
// string, plus every standalone literal. A chain is flattened ONCE and its subtree marked,
// so its own fragments are not reported as statements of their own — that is what would
// otherwise turn an inner literal ending at `jsonb_array_elements(` into a false violation.
// Parts are SUBSTITUTED, never dropped:
//
//	saleItemsArray(…)  -> guardedPlaceholder
//	anything else      -> opaquePlaceholder
//
// The opaque stand-in is what keeps this honest. The real queries concatenate a package
// const in the middle (`+ paidSet +`), so a flattener that gave up on an unresolvable part
// left the expansion's own fragment unjudged — measured here, a bare fifth site written in
// exactly that shape stayed INVISIBLE.
func flattenedStrings(f *ast.File) []string {
	inside := map[ast.Node]bool{}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		b, ok := n.(*ast.BinaryExpr)
		if !ok || b.Op != token.ADD || inside[b] {
			return true
		}
		out = append(out, flatten(b))
		ast.Inspect(b, func(inner ast.Node) bool {
			inside[inner] = true
			return true
		})
		return true
	})
	// Standalone literals — an expansion written without concatenation.
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING || inside[lit] {
			return true
		}
		if v, err := strconv.Unquote(lit.Value); err == nil {
			out = append(out, v)
		}
		return true
	})
	return out
}

// flatten renders one expression as text with the substitutions above. It never fails: a
// part it cannot resolve becomes the opaque stand-in, so the pieces that ARE literal stay in
// their true positions relative to each other.
func flatten(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return opaquePlaceholder
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return opaquePlaceholder
		}
		return s
	case *ast.ParenExpr:
		return flatten(v.X)
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return opaquePlaceholder
		}
		return flatten(v.X) + flatten(v.Y)
	case *ast.CallExpr:
		if id, ok := v.Fun.(*ast.Ident); ok && id.Name == saleItemsHelperName {
			return guardedPlaceholder
		}
		return flattenCall(v)
	}
	return opaquePlaceholder
}

// flattenCall resolves the standard formatting helpers when their format is a literal, and
// otherwise keeps the call's own literal arguments in place — a statement assembled by
// `fmt.Sprintf` is a spelling an author really uses and must not read as a blank.
func flattenCall(v *ast.CallExpr) string {
	sel, ok := v.Fun.(*ast.SelectorExpr)
	if ok && sel.Sel != nil && sel.Sel.Name == "Sprintf" && len(v.Args) > 0 {
		if lit, ok := v.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if format, err := strconv.Unquote(lit.Value); err == nil {
				args := make([]string, 0, len(v.Args)-1)
				for _, a := range v.Args[1:] {
					args = append(args, flatten(a))
				}
				return applySimpleFormat(format, args)
			}
		}
	}
	// A shape this guard does not resolve: keep whatever literal pieces it carries, joined
	// by the opaque stand-in so their relative positions survive.
	parts := []string{}
	for _, a := range v.Args {
		if s := flatten(a); s != opaquePlaceholder {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return opaquePlaceholder
	}
	return strings.Join(parts, opaquePlaceholder)
}

// applySimpleFormat substitutes `%s`/`%v`/`%d` in order (drops a width/precision, which
// cannot change whether the text says `items`).
func applySimpleFormat(format string, args []string) string {
	var b strings.Builder
	next := 0
	for i := 0; i < len(format); i++ {
		if format[i] != '%' || i+1 >= len(format) {
			b.WriteByte(format[i])
			continue
		}
		j := i + 1
		for j < len(format) && (format[j] == '-' || format[j] == '+' || (format[j] >= '0' && format[j] <= '9') || format[j] == '.') {
			j++
		}
		if j >= len(format) {
			b.WriteString(format[i:])
			break
		}
		if format[j] != 's' && format[j] != 'v' && format[j] != 'd' {
			b.WriteString(format[i : j+1])
			i = j
			continue
		}
		if next < len(args) {
			b.WriteString(args[next])
			next++
		} else {
			b.WriteString(opaquePlaceholder)
		}
		i = j
	}
	return b.String()
}

// argumentText returns the text of the expansion's argument, up to its closing paren.
func argumentText(text string, start int) string {
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

// literalsOf concatenates every string literal in a node — used to read the helper's body
// without resolving it.
func literalsOf(n ast.Node) string {
	var b strings.Builder
	ast.Inspect(n, func(inner ast.Node) bool {
		if lit, ok := inner.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if v, err := strconv.Unquote(lit.Value); err == nil {
				b.WriteString(v)
			}
		}
		return true
	})
	return b.String()
}

// readable spells the flattening stand-ins out. They are control characters, so an error
// message that carried them raw read as a binary blob in a terminal (measured) and told the
// reader nothing.
//
// It must run BEFORE any truncation: the stand-ins are NUL-delimited, so slicing a fragment
// at a fixed width can cut the closing NUL and leave the substitution with nothing to match
// (that is exactly how a message kept its `\x00OPAQUE` — measured).
func readable(s string) string {
	s = strings.ReplaceAll(s, guardedPlaceholder, "<saleItemsArray…>")
	s = strings.ReplaceAll(s, opaquePlaceholder, "…")
	return s
}

// truncate shortens a fragment to n RUNES, so a multi-byte character is never split.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return strings.Join(strings.Fields(s), " ")
}

// sortStrings is a tiny insertion sort: the message must not depend on map iteration order,
// and the guard should not depend on a helper the package might not have.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
