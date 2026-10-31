// CLASS GUARD — every test store that can MINT a session must own its keyspace.
//
// The test Valkey is shared by every checkout on the host, and a session key
// minted through a store built without a namespace carries the PRODUCTION name
// (`jf:session:<hash>`) and the production TTL. Nothing removes it when the test
// process exits, so the key outlives the run and no later run can tell it apart
// from another's — the leak t_e4c8ea65 closed for the fixtures it converted, by
// giving this package a per-process namespace (`testSessionOptions`) and a sweep
// in TestMain.
//
// It left one fixture behind: auth_totp_test.go opened
//
//	auth.NewSessions("valkey://localhost:6379/0")
//
// — unscoped, and pinned to db 0, which is also config.go's production default
// index. totpRegister drives register → verify → login, so the TOTP tests really
// minted sessions: measured on the pre-fix tree, `go test ./internal/httpapi
// -run Test2FA` moved db 0 from 846 to 857 `jf:session:*` keys (+11, TTL 86400s)
// while the namespaced fixtures left db 9 untouched. The comment above
// testSessionOptions asserted the opposite ("Every auth.NewSessions call in this
// package's tests goes through it"), which is why the gap survived review.
//
// A behavioural test cannot catch the NEXT fixture: a store that never mints in
// the tests that run today still mints in the one someone adds tomorrow. So this
// is a SOURCE scan over the AST, in the shape TestNoLiteralClientIPInTests
// already uses for client addresses: every `auth.NewSessions(...)` in this
// package's _test.go files must either pass testSessionOptions in its own
// argument list, or be a named exception with a reason. Allowlist entries are
// checked in BOTH directions — an entry that no longer matches a call fails — so
// the list cannot rot into a blanket exemption.
//
// The limit, stated: the scan matches the qualifier `auth.NewSessions` literally,
// so a test file that imported the package under another name would evade it
// (none does; `go vet` would not catch that either). It also cannot follow a
// store built in a helper and passed around — which is why the rule is about the
// call site, the one place a keyspace is decided.
package httpapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/auth"
)

// sessionStoreAllowlist names the auth.NewSessions call sites in this package's
// tests that may build a store WITHOUT testSessionOptions. Keyed by
// "file:func" so it survives line moves; one line of reason each. Every entry
// must match a call that exists, or the guard fails as stale.
var sessionStoreAllowlist = map[string]string{
	"auth_hardening_isolation_test.go:TestReclaimLeavesPeersLoginCountersAlone":                           "opens an unscoped store only to reach .Client() for seeding and deleting its own peer counter/sentinel — it mints no session",
	"test_session_valkey_t_e4c8ea65_test.go:sweepTestSessionKeys":                                         "is the exit sweep itself, built with auth.WithKeyNamespace(testSessionKeyNamespace) — the namespace it deletes, not the shared options",
	"test_session_valkey_t_e4c8ea65_test.go:TestTestSessionsLiveInTheProcessNamespaceAndTheSweepIsScoped": "has to build a store on a PEER namespace to prove the sweep is scoped, which testSessionOptions cannot express",
}

// sessionStoreCall is one auth.NewSessions call in a test file.
type sessionStoreCall struct {
	file   string
	fn     string // enclosing func; "" when the call is not inside one
	line   int
	scoped bool // passes testSessionOptions in its own argument list
}

// TestEverySessionStoreOwnsItsKeyspace is the guard: no test in this package may
// build a session store that writes the production key layout.
func TestEverySessionStoreOwnsItsKeyspace(t *testing.T) {
	files := packageTestGoFiles(t)
	if len(files) < 100 {
		t.Fatalf("scanned only %d test file(s) — the file walker is broken, so this guard would pass vacuously", len(files))
	}

	var findings []string
	used := make(map[string]bool, len(sessionStoreAllowlist))
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, call := range scanSessionStores(t, filepath.Base(path), string(src)) {
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

	if len(findings) > 0 {
		sort.Strings(findings)
		t.Fatalf("%d test session store(s) built without testSessionOptions — a store without the\n"+
			"per-process namespace writes the PRODUCTION `jf:session:<hash>` key (TTL %v) onto the\n"+
			"shared test Valkey, where TestMain's sweep cannot see it and it outlives the run:\n  %s\n\n"+
			"Build it as auth.NewSessions(url, testSessionOptions()...) — or, if the store is a raw\n"+
			"client or a peer-namespace probe that cannot be, add it to sessionStoreAllowlist with\n"+
			"the one-line reason.",
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
		t.Errorf("%d sessionStoreAllowlist entr(ies) match no unscoped auth.NewSessions call — either the\n"+
			"call was fixed (drop the entry) or a func was renamed and the exemption now silently\n"+
			"follows whatever that name means next:\n  %s", len(stale), strings.Join(stale, "\n  "))
	}
}

// scanSessionStores returns every auth.NewSessions call in one test file's
// source, with the func that encloses it. Calls are found by walking the source
// once, so a call that no func encloses is still reported (and cannot hide).
func scanSessionStores(t *testing.T, file, src string) []sessionStoreCall {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}

	var out []sessionStoreCall
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isAuthNewSessions(call) {
				return true
			}
			out = append(out, sessionStoreCall{
				file:   file,
				fn:     fd.Name.Name,
				line:   fset.Position(call.Pos()).Line,
				scoped: passesTestSessionOptions(call),
			})
			return true
		})
	}

	// Everything the func walk saw, plus anything it could not see (a
	// package-level var initialiser), so the scan is a closed set over the file
	// rather than over the declarations we happened to visit.
	total := 0
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && isAuthNewSessions(call) {
			total++
		}
		return true
	})
	for ; total > len(out); total-- {
		out = append(out, sessionStoreCall{file: file, line: 0})
	}
	return out
}

// isAuthNewSessions reports whether call is `auth.NewSessions(...)`.
func isAuthNewSessions(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "NewSessions" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "auth"
}

// passesTestSessionOptions reports whether the call itself (its own argument
// list, i.e. the same statement) hands the store the package's keyspace options.
func passesTestSessionOptions(call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		inner, ok := arg.(*ast.CallExpr)
		if !ok {
			continue
		}
		if callName(inner) == "testSessionOptions" {
			return true
		}
	}
	return false
}

// TestTheSessionStoreScanDiscriminates is the detector's own control: it must
// fire on the shapes that mint production-layout keys and stay silent on the
// sanctioned ones, and the allowlist must absorb exactly the call it names.
// Without this, the guard above could be green because the scan is blind.
func TestTheSessionStoreScanDiscriminates(t *testing.T) {
	fires := []struct {
		what string
		src  string
	}{
		{
			"the hardcoded production-URL store (the auth_totp_test.go gap)",
			`func qaTotp(t *testing.T) { s, err := auth.NewSessions("valkey://localhost:6379/0"); _ = s; _ = err }`,
		},
		{
			"the test Valkey URL without the namespace options",
			`func qaEnv(t *testing.T) { s, err := auth.NewSessions(os.Getenv(testdb.ValkeyEnv)); _ = s; _ = err }`,
		},
	}
	for _, tc := range fires {
		got := scanSessionStores(t, "qa_control_test.go", "package httpapi\n"+tc.src+"\n")
		if len(got) != 1 || got[0].scoped || got[0].fn == "" {
			t.Errorf("the scan did not flag %s — a store built this way mints the production key layout (got %+v)", tc.what, got)
		}
	}

	staysSilent := []struct {
		what      string
		src       string
		wantCalls int
	}{
		{
			"the sanctioned spelling",
			`func qaScoped(t *testing.T) { s, err := auth.NewSessions(url, testSessionOptions()...); _ = s; _ = err }`,
			1,
		},
		{
			"the sanctioned spelling with a fixture option",
			`func qaScoped(t *testing.T) { s, err := auth.NewSessions(url, testSessionOptions(auth.WithEmailCodeKey(k))...); _ = s; _ = err }`,
			1,
		},
		{
			"an unrelated constructor",
			`func qaOther(t *testing.T) { l := auth.NewLoginLimiter(client); _ = l }`,
			0,
		},
	}
	for _, tc := range staysSilent {
		got := scanSessionStores(t, "qa_control_test.go", "package httpapi\n"+tc.src+"\n")
		if len(got) != tc.wantCalls {
			t.Errorf("the scan saw %d auth.NewSessions call(s) in %s, want %d (got %+v)", len(got), tc.what, tc.wantCalls, got)
			continue
		}
		if len(got) == 1 && !got[0].scoped {
			t.Errorf("the scan flagged %s — an allowlist entry for a correct fixture would be noise that hollows the guard out (got %+v)", tc.what, got)
		}
	}

	// The allowlist must absorb the call it names — and only that: the same
	// source under a different func name is a finding, which is what makes a
	// renamed exempt func loud instead of silently still-exempt.
	allowedSrc := `package httpapi
func TestReclaimLeavesPeersLoginCountersAlone(t *testing.T) { s, err := auth.NewSessions(url); _ = s; _ = err }
`
	calls := scanSessionStores(t, "auth_hardening_isolation_test.go", allowedSrc)
	if _, allowed := sessionStoreAllowlist[calls[0].file+":"+calls[0].fn]; !allowed || calls[0].scoped {
		t.Fatalf("the allowlist does not name the raw-client call site it is supposed to exempt (call %+v)", calls[0])
	}
	renamed := scanSessionStores(t, "auth_hardening_isolation_test.go",
		"package httpapi\nfunc TestSomethingElse(t *testing.T) { s, err := auth.NewSessions(url); _ = s; _ = err }\n")
	if _, allowed := sessionStoreAllowlist[renamed[0].file+":"+renamed[0].fn]; allowed {
		t.Errorf("a renamed func inherited the raw-client exemption — the allowlist is keyed by file:func so it must not")
	}
}
