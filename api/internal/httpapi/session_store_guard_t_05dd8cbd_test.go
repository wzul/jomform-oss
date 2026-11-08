// CLASS GUARD — every test store that can MINT a session must own its keyspace.
//
// The test Valkey is shared by every checkout on the host, and a session key
// minted through a store built without a namespace carries the PRODUCTION name
// (`jf:session:<hash>`) and the production TTL. Nothing removes it when the test
// process exits, so the key outlives the run and no later run can tell it apart
// from another's — the leak t_e4c8ea65 closed for the fixtures it converted, by
// giving each package a per-process namespace (`testSessionOptions`,
// `testValkeyOptions`, `valkeyTestOptions`) and a sweep in TestMain.
//
// This guard used to scan only THIS package, and that limit left two writers
// behind, both in internal/auth (card t_d08b36c2): its own Valkey tests opened
// `NewSessions($JOMFORM_TEST_VALKEY_URL)` unscoped, so every run wrote
// production-layout keys, and one seed (`rdb.Set(..., 0)`) was IMMORTAL — a run
// killed before cleanup left a TTL -1 key nothing could ever remove. Measured on
// the pre-fix tree: SIGKILL during that seed left `jf:session:adbb…` with PTTL
// -1 on the shared DB. A package-local guard cannot see that.
//
// So this is now a SOURCE scan over the AST of EVERY `_test.go` file under the
// api module, in the shape TestNoLiteralClientIPInTests already uses for client
// addresses: every session-store constructor call (`auth.NewSessions(...)`, or a
// bare `NewSessions(...)` inside package auth) must either be scoped in its own
// argument list — `WithKeyNamespace(...)` directly, or a package-local options
// helper that passes it (`...()...`) — or be a named exception with a reason.
// Allowlist entries are checked in BOTH directions — an entry that no longer
// matches a call fails — so the list cannot rot into a blanket exemption.
//
// The limit, stated: the scan matches the constructor by name (`auth.NewSessions`
// with the `auth` qualifier, or the bare spelling inside internal/auth), so a
// test file that imported the package under another name would evade it (none
// does; `go vet` would not catch that either). It also cannot follow a name that
// is not a function declaration in the same package — which is why the rule is
// about the call site, the one place a keyspace is decided.
package httpapi

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/wzul/jomform/api/internal/auth"
)

// sessionStoreAllowlist names the session-store call sites in the module's tests
// that may build a store WITHOUT a key namespace. Keyed by
// "<dir-relative-to-api>/<file>:<func>" so it survives line moves; one line of
// reason each. Every entry must match a call that exists, or the guard fails as
// stale.
var sessionStoreAllowlist = map[string]string{
	"internal/httpapi/auth_hardening_isolation_test.go:TestReclaimLeavesPeersLoginCountersAlone":                             "opens an unscoped store only to reach .Client() for seeding and deleting its own peer counter/sentinel — it mints no session",
	"internal/auth/session_namespace_t_e4c8ea65_test.go:TestKeyNamespaceIsValidated":                                         "builds the production-layout store to read keyPrefix() only — it mints nothing, and that unchanged layout is what the test pins",
	"internal/auth/session_derived_namespace_t_6004c826_test.go:TestDerivedStoresInheritTheNamespaceAndOneSweepReclaimsThem": "deliberate production-layout probe: it must emit today's unscoped `jf:session:<hash>` / `jf:magic:<hash>` names to prove an empty namespace is byte-identical, so it cannot be namespaced; its session TTL is bounded to 2m and every minted key is deleted by exact name in cleanup",
}

// sessionStoreCall is one session-store constructor call in a test file.
type sessionStoreCall struct {
	file   string // dir-relative path, e.g. internal/auth/session_keyspace_test.go
	fn     string // enclosing func; "" when the call is not inside one
	line   int
	scoped bool // the call's own argument list names a key namespace
}

// moduleTestFile is one parsed `_test.go` file under the api module.
type moduleTestFile struct {
	rel  string // slash path relative to the api module root
	dir  string // slash dir relative to the api module root, e.g. internal/auth
	file *ast.File
	fset *token.FileSet
}

// TestEverySessionStoreOwnsItsKeyspace is the guard: no test in ANY package may
// build a session store that writes the production key layout.
func TestEverySessionStoreOwnsItsKeyspace(t *testing.T) {
	files := moduleTestFiles(t)
	if len(files) < 100 {
		t.Fatalf("scanned only %d test file(s) — the file walker is broken, so this guard would pass vacuously", len(files))
	}
	helpers := scopingHelpers(files)

	sawPackage := map[string]bool{}
	var findings []string
	used := make(map[string]bool, len(sessionStoreAllowlist))
	for _, f := range files {
		sawPackage[f.dir] = true
		for _, call := range scanSessionStores(t, f, helpers) {
			if call.scoped {
				continue
			}
			key := call.file + ":" + call.fn
			if _, allowed := sessionStoreAllowlist[key]; allowed && call.fn != "" {
				used[key] = true
				continue
			}
			switch {
			case call.fn == "":
				findings = append(findings, key+":"+strconv.Itoa(call.line)+" (not inside a func, so it cannot be allowlisted)")
			default:
				findings = append(findings, key+":"+strconv.Itoa(call.line))
			}
		}
	}

	// The scan has to have READ the packages this guard exists for, or "no
	// findings" means "no files" — the exact shape the old package-local guard
	// had for internal/auth.
	for _, pkg := range []string{"internal/auth", "internal/httpapi", "internal/mcpsrv"} {
		if !sawPackage[pkg] {
			t.Fatalf("the scan never read package %s — a guard that cannot see a package cannot cover it", pkg)
		}
	}

	if len(findings) > 0 {
		sort.Strings(findings)
		t.Fatalf("%d test session store(s) built without a key namespace — a store without the\n"+
			"per-process namespace writes the PRODUCTION `jf:session:<hash>` key (TTL %v) onto the\n"+
			"shared test Valkey, where the package's TestMain sweep cannot see it and it outlives the run:\n  %s\n\n"+
			"Build it with the package's options helper (testSessionOptions() / testValkeyOptions() /\n"+
			"valkeyTestOptions()) or with auth.WithKeyNamespace(...) — or, if the store is a raw client\n"+
			"or a deliberate production-layout probe that cannot be, add it to sessionStoreAllowlist\n"+
			"with the one-line reason.",
			len(findings), auth.SessionTTL, strings.Join(findings, "\n  "))
	}

	var stale []string
	for key := range sessionStoreAllowlist {
		if !used[key] {
			stale = append(stale, key)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("%d sessionStoreAllowlist entr(ies) match no unscoped session-store call — either the\n"+
			"call was fixed (drop the entry) or a func was renamed and the exemption now silently\n"+
			"follows whatever that name means next:\n  %s", len(stale), strings.Join(stale, "\n  "))
	}
}

// moduleTestSource is the PARSE of every `_test.go` file under the api module,
// taken once per test process: the tree does not change during a run, and the
// walk plus the go/parser pass over ~660 files is the expensive part of these
// guards. The guards still assert on what it holds — including the anti-vacuity
// floor on the number of files.
var moduleTestSource struct {
	once  sync.Once
	files []*moduleTestFile
	err   error
}

// moduleTestFiles parses every `_test.go` file under the api module (the parent
// of this package), so a NEW package or file is scanned because it exists.
func moduleTestFiles(t *testing.T) []*moduleTestFile {
	t.Helper()
	c := &moduleTestSource
	c.once.Do(func() { c.files, c.err = parseModuleTestFiles() })
	if c.err != nil {
		t.Fatal(c.err)
	}
	return c.files
}

func parseModuleTestFiles() ([]*moduleTestFile, error) {
	root, err := filepath.Abs("../..")
	if err != nil {
		return nil, fmt.Errorf("resolve the api module root: %w", err)
	}
	var out []*moduleTestFile
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "testdata", "vendor", ".git":
				return filepath.SkipDir
			}
			if strings.HasPrefix(d.Name(), ".") && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			return perr
		}
		out = append(out, &moduleTestFile{rel: rel, dir: path.Dir(rel), file: f, fset: fset})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk the api module's test files: %w", err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out, nil
}

// scopingHelpers returns, per package dir, the names of functions whose bodies
// build a key namespace (`WithKeyNamespace`). Passing such a helper's result to a
// store constructor is how every package in this repo spells a scoped store:
// `auth.NewSessions(url, testSessionOptions()...)`.
func scopingHelpers(files []*moduleTestFile) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, f := range files {
		for _, decl := range f.file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			found := false
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok && callName(call) == "WithKeyNamespace" {
					found = true
					return false
				}
				return true
			})
			if !found {
				continue
			}
			if out[f.dir] == nil {
				out[f.dir] = map[string]bool{}
			}
			out[f.dir][fd.Name.Name] = true
		}
	}
	return out
}

// scanSessionStores returns every session-store constructor call in one parsed
// test file, with the func that encloses it. Calls are found by walking the
// source once, so a call that no func encloses is still reported (and cannot
// hide).
func scanSessionStores(t *testing.T, f *moduleTestFile, helpers map[string]map[string]bool) []sessionStoreCall {
	t.Helper()
	isCall := func(n ast.Node) (*ast.CallExpr, bool) {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isSessionStoreCall(call, f.dir) {
			return nil, false
		}
		return call, true
	}

	var out []sessionStoreCall
	for _, decl := range f.file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := isCall(n)
			if !ok {
				return true
			}
			out = append(out, sessionStoreCall{
				file:   f.rel,
				fn:     fd.Name.Name,
				line:   f.fset.Position(call.Pos()).Line,
				scoped: callIsScoped(call, helpers[f.dir]),
			})
			return true
		})
	}

	// Everything the func walk saw, plus anything it could not see (a
	// package-level var initialiser), so the scan is a closed set over the file
	// rather than over the declarations we happened to visit.
	total := 0
	ast.Inspect(f.file, func(n ast.Node) bool {
		if _, ok := isCall(n); ok {
			total++
		}
		return true
	})
	for ; total > len(out); total-- {
		out = append(out, sessionStoreCall{file: f.rel, line: 0})
	}
	return out
}

// isSessionStoreCall reports whether call builds a session store: `auth.NewSessions`
// anywhere, or the bare `NewSessions` inside package auth (where the constructor
// is a package-level ident).
func isSessionStoreCall(call *ast.CallExpr, dir string) bool {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if sel.Sel.Name != "NewSessions" {
			return false
		}
		id, ok := sel.X.(*ast.Ident)
		return ok && id.Name == "auth"
	}
	id, ok := call.Fun.(*ast.Ident)
	return ok && id.Name == "NewSessions" && dir == "internal/auth"
}

// callIsScoped reports whether the call itself (its own argument list) hands the
// store a key namespace: directly, or through a package-local options helper that
// passes one.
func callIsScoped(call *ast.CallExpr, helpers map[string]bool) bool {
	scoped := false
	for _, arg := range call.Args {
		ast.Inspect(arg, func(n ast.Node) bool {
			inner, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := callName(inner)
			if name == "WithKeyNamespace" || helpers[name] {
				scoped = true
				return false
			}
			return true
		})
		if scoped {
			return true
		}
	}
	return false
}

// TestTheSessionStoreScanDiscriminates is the detector's own control: it must
// fire on the shapes that mint production-layout keys and stay silent on the
// sanctioned ones, including the helper spelling and the bare constructor inside
// package auth, and the allowlist must absorb exactly the call it names.
// Without this, the guard above could be green because the scan is blind.
func TestTheSessionStoreScanDiscriminates(t *testing.T) {
	dir := "internal/httpapi"
	const src = `package httpapi
func testSessionOptions(opts ...auth.SessionOption) []auth.SessionOption {
	return append(opts, auth.WithKeyNamespace("jftest:px"))
}
func scoped(t *testing.T) { s, err := auth.NewSessions(url, testSessionOptions()...); _ = s; _ = err }
func direct(t *testing.T) { s, err := auth.NewSessions(url, auth.WithKeyNamespace("jftest:px")); _ = s; _ = err }
func unscoped(t *testing.T) { s, err := auth.NewSessions(url); _ = s; _ = err }
func hardcoded(t *testing.T) { s, err := auth.NewSessions("valkey://localhost:6379/0"); _ = s; _ = err }
func unrelated(t *testing.T) { l := auth.NewLoginLimiter(client); _ = l }
`
	f := parseFixture(t, dir, "qa_control_test.go", src)
	helpers := scopingHelpers([]*moduleTestFile{f})
	if !helpers[dir]["testSessionOptions"] {
		t.Fatalf("the helper derivation missed testSessionOptions — a scoped store passed through it would be flagged (helpers %v)", helpers[dir])
	}
	calls := scanSessionStores(t, f, helpers)
	if len(calls) != 4 {
		t.Fatalf("the scan saw %d call(s) in the control source, want 4 (got %+v)", len(calls), calls)
	}
	got := map[string]bool{}
	for _, c := range calls {
		got[c.fn] = c.scoped
	}
	for _, want := range []struct {
		fn     string
		scoped bool
		why    string
	}{
		{"scoped", true, "passes the package's options helper"},
		{"direct", true, "passes auth.WithKeyNamespace directly"},
		{"unscoped", false, "the test Valkey URL without a namespace"},
		{"hardcoded", false, "a hardcoded production URL"},
	} {
		if s, ok := got[want.fn]; !ok || s != want.scoped {
			t.Errorf("scan said %s scoped=%v (present=%v), want %v — %s", want.fn, s, ok, want.scoped, want.why)
		}
	}

	// The bare spelling is the one package auth uses, where `auth.` is absent. It
	// must be recognised there — and NOT in another package, where a bare
	// NewSessions belongs to something else.
	bare := `package auth
func valkeyTestOptions() []SessionOption { return []SessionOption{WithKeyNamespace("jftest:px")} }
func scoped(t *testing.T) { s, err := NewSessions(url, valkeyTestOptions()...); _ = s; _ = err }
func unscoped(t *testing.T) { s, err := NewSessions(url); _ = s; _ = err }
`
	authDir := "internal/auth"
	af := parseFixture(t, authDir, "qa_auth_control_test.go", bare)
	ah := scopingHelpers([]*moduleTestFile{af})
	authCalls := scanSessionStores(t, af, ah)
	if len(authCalls) != 2 {
		t.Fatalf("the scan saw %d bare NewSessions call(s) in package auth, want 2 (got %+v)", len(authCalls), authCalls)
	}
	for _, c := range authCalls {
		wantScoped := c.fn == "scoped"
		if c.scoped != wantScoped {
			t.Errorf("package auth call %s scoped=%v, want %v", c.fn, c.scoped, wantScoped)
		}
	}
	otherPkg := parseFixture(t, "internal/mcpsrv", "qa_mcp_control_test.go", bare)
	if calls := scanSessionStores(t, otherPkg, scopingHelpers([]*moduleTestFile{otherPkg})); len(calls) != 0 {
		t.Errorf("a bare NewSessions outside package auth was treated as the auth constructor: %+v", calls)
	}

	// The allowlist must absorb the call it names — and only that: the same
	// source under a different func name is a finding, which is what makes a
	// renamed exempt func loud instead of silently still-exempt.
	allowedSrc := `package httpapi
func TestReclaimLeavesPeersLoginCountersAlone(t *testing.T) { s, err := auth.NewSessions(url); _ = s; _ = err }
`
	allowed := scanSessionStores(t, parseFixture(t, dir, "auth_hardening_isolation_test.go", allowedSrc), helpers)
	if len(allowed) != 1 {
		t.Fatalf("the allowlist control source produced %d call(s), want 1", len(allowed))
	}
	if _, ok := sessionStoreAllowlist[allowed[0].file+":"+allowed[0].fn]; !ok || allowed[0].scoped {
		t.Fatalf("the allowlist does not name the raw-client call site it is supposed to exempt (call %+v)", allowed[0])
	}
	renamed := scanSessionStores(t, parseFixture(t, dir, "auth_hardening_isolation_test.go",
		"package httpapi\nfunc TestSomethingElse(t *testing.T) { s, err := auth.NewSessions(url); _ = s; _ = err }\n"), helpers)
	if _, ok := sessionStoreAllowlist[renamed[0].file+":"+renamed[0].fn]; ok {
		t.Errorf("a renamed func inherited the raw-client exemption — the allowlist is keyed by file:func so it must not")
	}
}

// parseFixture parses an in-memory control source as if it lived at
// <dir>/<file> in the module.
func parseFixture(t *testing.T, dir, file, src string) *moduleTestFile {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		t.Fatalf("parse control source %s: %v", file, err)
	}
	return &moduleTestFile{rel: dir + "/" + file, dir: dir, file: f, fset: fset}
}
