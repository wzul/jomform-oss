package dbstore

// Issue #102 — migration / scan-drift guard.
//
// AGENTS.md warns that a migration which adds columns without updating the
// matching `*Select` constant AND every Scan call site (ByID, ByOwner, ByEmail,
// ...) causes silent column-aligned scan regressions (e.g. 401 storms on
// accounts). This file is the automated guard:
//
//  1. TestSelectScanColumnAlignment — parses the dbstore package source and
//     asserts that every `*Select` constant's column count equals the number
//     of exported fields on the struct it scans into, and that at least one
//     Scan call site actually scans that many destinations. A migration that
//     adds a column without updating the select, or a select updated without
//     updating a scan site, fails the build.
//
//  2. TestMigrationColumnsAppearInSelect — parses every migration *.up.sql and
//     asserts that any `ALTER TABLE ... ADD COLUMN` on a table that has a
//     `*Select` constant is reflected in that constant (unless the column is
//     deliberately read via a dedicated accessor query — see the allowlist).
//     This is the convention-enforcement check: a migration that adds a column
//     without updating the corresponding select constant is caught in the same
//     PR.
//
// Both are pure static checks — they run in CI with no database required.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// selectConsts maps a *Select constant name to the struct it scans into.
// Keep this in sync with the store files; the column/field counts themselves
// are derived from source so drift is still caught.
var selectConsts = map[string]string{
	"accountSelect": "Account",
	"formSelect":    "Form",
	"productSelect": "Product",
	"saleSelect":    "Sale",
	"spaceSelect":   "Space",
	"subSelect":     "Subscription",
}

// selectTables maps a *Select constant name to the table it selects from.
var selectTables = map[string]string{
	"accountSelect": "accounts",
	"formSelect":    "forms",
	"productSelect": "products",
	"saleSelect":    "sales",
	"spaceSelect":   "spaces",
	"subSelect":     "subscriptions",
}

// selectScanFuncs maps a *Select constant to the receiver-qualified
// function(s) that perform the full-struct Scan of that select's columns.
// These are the call sites that MUST stay column-aligned with the select
// constant. A migration that adds a column to the select (and struct) without
// updating every one of these scan functions is caught by
// TestSelectScanColumnAlignment. Format: "ReceiverType.Method".
var selectScanFuncs = map[string][]string{
	"accountSelect": {"AccountStore.ByID", "AccountStore.ByOwner"},
	"formSelect":    {"FormStore.scanOne"},
	"productSelect": {"ProductStore.ListActiveByAccount", "ProductStore.ListActiveByAccounts", "ProductStore.Get"},
	"saleSelect":    {"scanSale"},
	"spaceSelect":   {"scanSpace"},
	"subSelect":     {"scanSub"},
}

// intentionallyUnselected lists columns that are deliberately NOT part of the
// main *Select constant because they are read via a dedicated accessor query
// (e.g. Tracking, SigningKeys, CFToken, ChipOverrideFor, SpaceIDByAccount).
// A migration adding one of these must NOT be flagged. Keyed by table name.
var intentionallyUnselected = map[string]map[string]bool{
	"accounts": {
		"tracking_head":    true, // AccountStore.Tracking
		"wh_sign_pub":      true, // AccountStore.SigningKeys
		"wh_sign_priv":     true, // AccountStore.SigningKeys
		"cf_token_enc":     true, // AccountStore.CFToken
		"space_id":         true, // SpaceStore.SpaceIDByAccount
		"slug_changed_from": true, // legacy 301 source, not scanned
	},
	"forms": {
		"space_id":              true, // denormalised, SpaceStore.SpaceIDByAccount
		"tracking_head":         true, // FormStore.SetTracking / read via dedicated query
		"chip_brand_id":         true, // FormStore.ChipOverrideFor
		"chip_secret_key_enc":   true, // FormStore.ChipOverrideFor
		"chip_email_fallback":   true, // FormStore.ChipOverrideFor
		"chip_method_whitelist": true, // FormStore.ChipOverrideFor
	},
	"products": {
		"space_id": true, // denormalised
	},
	"sales": {
		"space_id": true, // denormalised
	},
	"spaces": {
		"created_at": true, // not scanned by spaceSelect
	},
}

// packageDir returns the directory containing this test file.
func packageDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

// selectColumnCount counts the top-level columns in a `SELECT ... FROM`
// constant body. It is robust to nested parens (COALESCE(...), casts) and
// multi-line constants.
func selectColumnCount(sel string) int {
	start := strings.Index(sel, "SELECT")
	if start < 0 {
		return -1
	}
	from := strings.Index(sel[start:], "FROM")
	if from < 0 {
		return -1
	}
	body := sel[start+len("SELECT") : start+from]
	depth := 0
	count := 1
	for _, r := range body {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				count++
			}
		}
	}
	return count
}

// structFieldCount counts the exported fields on a struct type in the package.
func structFieldCount(t *testing.T, dir, typeName string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Name.Name != typeName {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				n := 0
				for _, fld := range st.Fields.List {
					if len(fld.Names) > 0 && fld.Names[0].IsExported() {
						n++
					}
				}
				return n
			}
		}
	}
	t.Fatalf("struct %s not found in package", typeName)
	return -1
}

// collectSelectConsts parses the package source and returns the value of each
// *Select constant (the raw string literal).
func collectSelectConsts(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if _, isSel := selectConsts[name.Name]; !isSel {
						continue
					}
					if i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					val, err := strconvUnquote(lit.Value)
					if err != nil {
						t.Fatalf("unquote %s: %v", name.Name, err)
					}
					out[name.Name] = val
				}
			}
		}
	}
	return out
}

// scanDestinationsInFunc returns the destination counts of every `.Scan(...)`
// call in the named function. `qualified` is either "ReceiverType.Method" for a
// method or a bare name for a package-level function.
func scanDestinationsInFunc(t *testing.T, dir, qualified string) []int {
	t.Helper()
	parts := strings.SplitN(qualified, ".", 2)
	recvType, methodName := "", qualified
	if len(parts) == 2 {
		recvType, methodName = parts[0], parts[1]
	}
	var counts []int
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != methodName || fn.Body == nil {
				continue
			}
			// If a receiver type is expected, match it; otherwise the function
			// must be package-level (no receiver).
			if recvType != "" {
				if fn.Recv == nil {
					continue
				}
				recv := fn.Recv.List[0].Type
				var recvName string
				switch rt := recv.(type) {
				case *ast.StarExpr:
					if id, ok := rt.X.(*ast.Ident); ok {
						recvName = id.Name
					}
				case *ast.Ident:
					recvName = rt.Name
				}
				if recvName != recvType {
					continue
				}
			} else if fn.Recv != nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Scan" {
					counts = append(counts, len(call.Args))
				}
				return true
			})
		}
	}
	return counts
}

// TestSelectScanColumnAlignment asserts every *Select constant's column count
// equals the exported-field count of the struct it scans into, and that every
// registered scan function scans exactly that many destinations.
func TestSelectScanColumnAlignment(t *testing.T) {
	dir := packageDir()
	consts := collectSelectConsts(t, dir)
	if len(consts) != len(selectConsts) {
		t.Fatalf("found %d *Select constants, want %d (a new select constant needs registering in selectConsts)", len(consts), len(selectConsts))
	}

	for name, structName := range selectConsts {
		sel, ok := consts[name]
		if !ok {
			t.Errorf("%s: select constant not found in source", name)
			continue
		}
		cols := selectColumnCount(sel)
		if cols < 0 {
			t.Errorf("%s: could not parse SELECT column list", name)
			continue
		}
		fields := structFieldCount(t, dir, structName)
		if cols != fields {
			t.Errorf("%s selects %d columns but %s has %d exported fields — column/struct drift (update the select AND all Scan call sites together)", name, cols, structName, fields)
		}
		// Every registered scan function must scan exactly `cols` destinations.
		funcs, ok := selectScanFuncs[name]
		if !ok {
			t.Errorf("%s: no scan functions registered in selectScanFuncs", name)
			continue
		}
		for _, fn := range funcs {
			counts := scanDestinationsInFunc(t, dir, fn)
			if len(counts) == 0 {
				t.Errorf("%s: scan function %s not found or has no Scan call — a migration likely added a column without updating the scan sites", name, fn)
				continue
			}
			for _, c := range counts {
				if c != cols {
					t.Errorf("%s used by %s: Scan has %d destinations but select has %d columns — column-aligned scan regression (update the select AND all scan sites together)", name, fn, c, cols)
				}
			}
		}
	}
}

// TestMigrationColumnsAppearInSelect parses every migration *.up.sql and
// asserts that any ALTER TABLE ... ADD COLUMN on a table with a *Select
// constant is reflected in that constant (unless deliberately unselected).
func TestMigrationColumnsAppearInSelect(t *testing.T) {
	dir := packageDir()
	migDir := filepath.Join(dir, "..", "db", "migrations")
	entries, err := os.ReadDir(migDir)
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	consts := collectSelectConsts(t, dir)

	var migFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			migFiles = append(migFiles, e.Name())
		}
	}
	sort.Strings(migFiles)

	for _, name := range migFiles {
		raw, err := os.ReadFile(filepath.Join(migDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		content := string(raw)
		lower := strings.ToLower(content)
		idx := 0
		for {
			pos := strings.Index(lower[idx:], "add column")
			if pos < 0 {
				break
			}
			pos += idx
			table := tableForAlter(content, pos)
			col := columnName(content, pos)
			idx = pos + len("add column")
			if table == "" || col == "" {
				continue
			}
			// Only enforce for tables that have a *Select constant.
			selName := selectConstForTable(table)
			if selName == "" {
				continue
			}
			// Skip columns deliberately read via dedicated accessor queries.
			if intentionallyUnselected[table][col] {
				continue
			}
			sel, ok := consts[selName]
			if !ok {
				t.Errorf("%s: table %q has select constant %s but it was not parsed", name, table, selName)
				continue
			}
			if !strings.Contains(strings.ToLower(sel), strings.ToLower(col)) {
				t.Errorf("%s: adds column %q to table %q but %s does not select it — update the select constant AND all Scan call sites in the same PR", name, col, table, selName)
			}
		}
	}
}

// selectConstForTable maps a table name to its *Select constant.
func selectConstForTable(table string) string {
	for name, tbl := range selectTables {
		if tbl == table {
			return name
		}
	}
	return ""
}

// tableForAlter finds the table name of the ALTER TABLE statement that
// contains the byte offset `pos`.
func tableForAlter(content string, pos int) string {
	head := content[:pos]
	last := strings.LastIndex(strings.ToLower(head), "alter table")
	if last < 0 {
		return ""
	}
	rest := strings.TrimSpace(head[last+len("alter table"):])
	end := strings.IndexAny(rest, " \t\n")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}

// columnName extracts the column name from an "ADD COLUMN <col> ..." at pos,
// skipping an optional "IF NOT EXISTS" clause.
func columnName(content string, pos int) string {
	rest := strings.TrimSpace(content[pos+len("add column"):])
	// Skip "IF NOT EXISTS" (case-insensitive).
	lower := strings.ToLower(rest)
	if strings.HasPrefix(lower, "if not exists") {
		rest = strings.TrimSpace(rest[len("if not exists"):])
	}
	end := strings.IndexAny(rest, " \t\n,")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}

// strconvUnquote unquotes a Go string literal (handles backticks and quotes).
func strconvUnquote(s string) (string, error) {
	if len(s) >= 2 && s[0] == '`' && s[len(s)-1] == '`' {
		return s[1 : len(s)-1], nil
	}
	return strconv.Unquote(s)
}
