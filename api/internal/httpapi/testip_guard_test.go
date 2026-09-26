// CLASS GUARD — a test-only client address must be one the suite can RECLAIM.
//
// The login breaker keys `jf:loginfail:ip:<addr>` in a Valkey DB every checkout
// shares, with a 15-minute TTL, and the auth fixture clears only the counters
// under THIS process's own /24 (testIPFamilyFor). An address that no process can
// derive is therefore a counter the reclaim can never reach: one run drives it
// to the lockout threshold, the next lane's run inherits it, and a peer asserts
// 429 where it wants 401 — the exact cross-process flake the scoped reclaim was
// written to remove. `jf:2fa_pending:<ip>:<email>` is ip-keyed the same way.
//
// The tests used to inject hand-written literals (10.9.9.9, 10.20.0.2, …) into
// the request helpers, and two helpers injected no address at all (so every
// request inherited httptest's default RemoteAddr 192.0.2.1). Both are the same
// defect, so both are refused here.
//
// This is a SOURCE scan, and deliberately so: a behavioural test can only cover
// the call sites that exist today, so it cannot refuse the next literal someone
// types. The scan is over the AST rather than the text, so a literal in a
// comment or a doc string is not a hit and the scan cannot be satisfied by
// spelling. The trade-off, stated: it refuses a literal in the positions below,
// so it cannot see an address that arrives through a variable, a table, or a
// helper this file does not know about.
package httpapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// literalIPAddress matches the ADDRESS inside a dotted-quad string literal. The
// value may carry a port (the RemoteAddr shape "203.0.113.7:1234") or be the
// bare address; ClientIP accepts either, so the guard must see both.
var literalIPAddress = regexp.MustCompile(`^\d{1,3}(?:\.\d{1,3}){3}(?::\d+)?$`)

// testIPHelperName is the only sanctioned source of a test client address.
const testIPHelperName = "testIP"

// ipArgHelpers maps a request helper to the zero-based index of its client-IP
// parameter. A helper that takes one and is NOT listed here would slip past the
// guard, so TestEveryRequestHelperTakesATestIP pins the set against the helpers
// themselves.
var ipArgHelpers = map[string]int{
	"postJSON":            3, // postJSON(mux, path, body, ip)
	"postJSONCookies":     3, // postJSONCookies(mux, path, body, ip, session)
	"registerAndVerify":   4, // registerAndVerify(t, h, email, password, ip)
	"enable2FAAndSession": 3, // enable2FAAndSession(t, h, email, ip)
}

// ipHeaderNames are the headers the server reads a client address from
// (middleware.ClientIP).
var ipHeaderNames = map[string]bool{"X-Real-IP": true, "CF-Connecting-IP": true}

// TestNoLiteralClientIPInTests is the guard: no test may hand the HTTP layer a
// hand-written client address, in any of the positions the layer reads one.
func TestNoLiteralClientIPInTests(t *testing.T) {
	files := packageTestGoFiles(t)
	if len(files) < 100 {
		t.Fatalf("scanned only %d test file(s) — the file walker is broken, so this guard would pass vacuously", len(files))
	}

	var findings []string
	for _, path := range files {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			for _, off := range literalClientIPOffsets(n) {
				pos := fset.Position(off)
				findings = append(findings, formatFinding(path, pos.Line))
			}
			return true
		})
	}

	// A scan that finds nothing when the package plainly contains literals would
	// be the vacuous pass this guard exists to prevent; the control below pins
	// that the detector works, so an empty result here is a real one.
	if len(findings) > 0 {
		sort.Strings(findings)
		t.Fatalf("%d hand-written client address(es) in this package's tests — a test\n"+
			"address must come from %s(t) so the auth fixture can reclaim its\n"+
			"jf:loginfail:ip:<addr> / jf:2fa_pending:<ip>:<email> counters on the SHARED\n"+
			"test Valkey. A literal sits outside every process's /24, so the counter it\n"+
			"writes outlives the run and the next lane inherits it:\n  %s",
			len(findings), testIPHelperName, strings.Join(findings, "\n  "))
	}
}

// literalClientIPOffsets returns the position of every literal client address in
// n, covering all four positions the HTTP layer reads one from.
func literalClientIPOffsets(n ast.Node) []token.Pos {
	var out []token.Pos

	// 1. The client-IP argument of a request helper.
	if call, ok := n.(*ast.CallExpr); ok {
		if idx, known := ipArgHelpers[callName(call)]; known && idx < len(call.Args) {
			if lit, ok := call.Args[idx].(*ast.BasicLit); ok && isLiteralIP(lit) {
				out = append(out, lit.Pos())
			}
		}
		// 3. A client-IP header value: Set("X-Real-IP", "…") /
		//    Set("CF-Connecting-IP", "…").
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Set" && len(call.Args) == 2 {
			if hdr, ok := stringLiteral(call.Args[0]); ok && ipHeaderNames[hdr] {
				if lit, ok := call.Args[1].(*ast.BasicLit); ok && isLiteralIP(lit) {
					out = append(out, lit.Pos())
				}
			}
		}
	}

	// 2. req.RemoteAddr = "…".
	if assign, ok := n.(*ast.AssignStmt); ok {
		for i, lhs := range assign.Lhs {
			sel, ok := lhs.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "RemoteAddr" || i >= len(assign.Rhs) {
				continue
			}
			if lit, ok := assign.Rhs[i].(*ast.BasicLit); ok && isLiteralIP(lit) {
				out = append(out, lit.Pos())
			}
		}
	}
	return out
}

// TestTheLiteralClientIPDetectorDiscriminates is the detector's own control: it
// must FIRE on the exact shapes that were in this package (a helper argument, a
// RemoteAddr assignment, a client-IP header) and STAY SILENT on an address that
// is not a client address. Without this, the guard above could pass because the
// detector is broken rather than because the package is clean — the two are
// indistinguishable from the guard's output alone.
func TestTheLiteralClientIPDetectorDiscriminates(t *testing.T) {
	fires := []struct {
		what string
		src  string
	}{
		{"a helper's ip argument (the 10.9.9.9 site)", `fn() { postJSON(mux, "/v1/auth/login", body, "10.9.9.9") }`},
		{"postJSONCookies' ip argument (the 10.20.0.2 site)", `fn() { postJSONCookies(mux, p, body, "10.20.0.2", s) }`},
		{"registerAndVerify's ip argument (the 10.20.1.1 site)", `fn() { registerAndVerify(t, h, e, "pw", "10.20.1.1") }`},
		{"enable2FAAndSession's ip argument", `fn() { enable2FAAndSession(t, h, e, "10.33.1.1") }`},
		{"a RemoteAddr assignment", `fn() { req.RemoteAddr = "203.0.113.7:1234" }`},
		{"an X-Real-IP header", `fn() { req.Header.Set("X-Real-IP", "10.7.7.7") }`},
		{"a CF-Connecting-IP header", `fn() { req.Header.Set("CF-Connecting-IP", "10.1.2.3") }`},
	}
	for _, tc := range fires {
		if got := detectorHits(t, tc.src); len(got) != 1 {
			t.Errorf("detector did not fire on %s — the guard would let it back in (hits=%d)\n  %s", tc.what, len(got), tc.src)
		}
	}

	// The sanctioned spelling, and the addresses that are NOT a client address,
	// must stay silent. An SNS payload's IP field and an assertion's expected
	// literal are data, not a client address, and flagging them would make the
	// guard noisy enough to be allowlisted away.
	staysSilent := []struct {
		what string
		src  string
	}{
		{"the sanctioned helper", `fn() { postJSON(mux, "/v1/auth/login", body, ip) }`},
		{"an SNS notification payload field", `fn() { body := []byte("{\"ipAddress\":\"203.0.113.7\"}") }`},
		{"a predicate table in a guard", `fn() { cases = append(cases, struct{ key, want string }{"jf:loginfail:ip:10.99.99.1", ""}) }`},
		{"an unrelated literal", `fn() { email := "2fa-10.9.9.9@example.test" }`},
		{"a literal assigned to something other than RemoteAddr", `fn() { req.Header.Set("X-Other", "10.1.2.3") }`},
	}
	for _, tc := range staysSilent {
		if got := detectorHits(t, tc.src); len(got) != 0 {
			t.Errorf("detector fired on %s — a false positive would force an allowlist and hollow out the guard (hits=%d)\n  %s", tc.what, len(got), tc.src)
		}
	}
}

// TestEveryRequestHelperTakesATestIP pins the helper set the guard knows about
// against the helpers as they are declared. ipArgHelpers is the one list here,
// and a list is only honest if a mismatch is loud: a request helper that takes a
// client address the guard does not know about is exactly how the literals got
// in, so it must fail rather than silently narrow the scan.
func TestEveryRequestHelperTakesATestIP(t *testing.T) {
	for name, idx := range ipArgHelpers {
		decl := findFuncDecl(t, name)
		if decl == nil {
			t.Errorf("ipArgHelpers names %q, which this package does not declare — the guard is scanning for a helper that does not exist", name)
			continue
		}
		params := decl.Type.Params.List
		if len(params) == 0 {
			continue
		}
		// Count parameters (a field may bind several names).
		var names []string
		for _, f := range params {
			for _, id := range f.Names {
				names = append(names, id.Name)
			}
		}
		if idx >= len(names) {
			t.Errorf("%s has %d parameter(s); ipArgHelpers says its client address is argument %d", name, len(names), idx)
			continue
		}
		if names[idx] != "ip" {
			t.Errorf("%s argument %d is %q, but ipArgHelpers says that is its client-IP parameter — the guard would inject-check the wrong argument", name, idx, names[idx])
		}
	}
}

// TestLoginHelpersSetATestClientIP closes the half the literal scan cannot see.
//
// loginOnce / loginOnceUA take NO client-IP parameter: they build the request
// themselves, so before this guard they sent httptest's default RemoteAddr
// ("192.0.2.1:1234") and the login path keyed jf:loginfail:ip:192.0.2.1 and
// jf:2fa_pending:192.0.2.1:<email> on it. Measured on the pre-fix tree: a single
// full-package run left two `jf:2fa_pending:192.0.2.1:` keys behind — a literal
// outside every /24, so the scoped reclaim can never clear them and a peer run
// inherits the counter. The literal scan above cannot refuse this shape, because
// the offending value is not in the test file at all; so the property the
// helpers must satisfy is asserted directly.
func TestLoginHelpersSetATestClientIP(t *testing.T) {
	for _, name := range []string{"loginOnce", "loginOnceUA"} {
		decl := findFuncDecl(t, name)
		if decl == nil {
			t.Fatalf("%s is not declared in this package — this guard would check nothing", name)
		}
		calls := headerSetCalls(decl.Body)
		var found bool
		for _, c := range calls {
			hdr, ok := stringLiteral(c.name)
			if !ok || !ipHeaderNames[hdr] {
				continue
			}
			if call, ok := c.value.(*ast.CallExpr); ok && callName(call) == testIPHelperName {
				found = true
			}
		}
		if !found {
			t.Errorf("%s does not set a client-IP header to %s(t) — without it every request it\n"+
				"builds carries httptest's default 192.0.2.1, which is outside every process's\n"+
				"reclaimed /24, so any wrong-credential login it drives leaves a counter no run can clear",
				name, testIPHelperName)
		}
	}
}

// headerSetCall is a `.Set(<name>, <value>)` call on some receiver.
type headerSetCall struct {
	name  ast.Expr
	value ast.Expr
}

// headerSetCalls returns every `<recv>.Set(name, value)` call in body.
func headerSetCalls(body *ast.BlockStmt) []headerSetCall {
	var out []headerSetCall
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Set" {
			return true
		}
		out = append(out, headerSetCall{name: call.Args[0], value: call.Args[1]})
		return true
	})
	return out
}

// detectorHits parses src as a function body and returns the positions the
// detector fires on.
func detectorHits(t *testing.T, src string) []token.Pos {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "control.go", "package p\nfunc "+src+"\n", 0)
	if err != nil {
		t.Fatalf("parse control snippet %q: %v", src, err)
	}
	var hits []token.Pos
	ast.Inspect(f, func(n ast.Node) bool {
		hits = append(hits, literalClientIPOffsets(n)...)
		return true
	})
	return hits
}

// packageTestGoFiles returns this package's _test.go files, walked rather than
// listed so a NEW file is scanned because it exists.
func packageTestGoFiles(t *testing.T) []string {
	t.Helper()
	entries, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatalf("list package test files: %v", err)
	}
	sort.Strings(entries)
	return entries
}

func findFuncDecl(t *testing.T, name string) *ast.FuncDecl {
	t.Helper()
	for _, path := range packageTestGoFiles(t) {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name {
				return fd
			}
		}
	}
	return nil
}

func callName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

func isLiteralIP(lit *ast.BasicLit) bool {
	s, ok := stringLiteral(lit)
	return ok && literalIPAddress.MatchString(s)
}

func stringLiteral(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

func formatFinding(path string, line int) string {
	return filepath.Base(path) + ":" + strconv.Itoa(line)
}
