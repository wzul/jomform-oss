// CLASS GUARD — a test-only client address must be one the suite can RECLAIM.
//
// The login breaker keys `jf:loginfail:ip:<addr>` in a Valkey DB every checkout
// shares, with a 15-minute TTL, and the auth fixture clears only the counters
// under THIS process's own address block (testIPPoolFor, two /24s). An address
// that no process can
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
// so it cannot see an address that arrives through a variable or a table.
//
// The third hole it used to have — "a helper this file does not know about" —
// is closed, and not by a second list that could itself go stale:
// TestEveryAddressPresentingHelperIsPinned walks this package's own funcs and
// fails on any that presents an `ip` parameter to a request without being in
// ipArgHelpers below. That is what makes the list a CLOSED set rather than a
// promise; the map alone could only ever pin its own keys (card t_e9bd13cd).
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
// guard, so the set is pinned in BOTH directions: TestEveryRequestHelperTakesATestIP
// refuses a stale entry or a wrong index, and
// TestEveryAddressPresentingHelperIsPinned refuses a helper that is missing from
// the map altogether — the half that made "a helper this file does not know
// about" a hole rather than a note.
var ipArgHelpers = map[string]int{
	"postJSON":            3, // postJSON(mux, path, body, ip)
	"postJSONCookies":     3, // postJSONCookies(mux, path, body, ip, session)
	"registerAndVerify":   4, // registerAndVerify(t, h, email, password, ip)
	"enable2FAAndSession": 3, // enable2FAAndSession(t, h, email, ip)
	// The unlock-route probes and the passkey ceremony helpers take a client
	// address and hand it to the request themselves, which is what these five
	// were missing from this map for (card t_e9bd13cd): their call sites were
	// outside the literal scan entirely, so `unlockPost(h, host, path,
	// "10.99.0.7", pw)` was a literal the guard could not see.
	"startPasskeyLogin":     2, // startPasskeyLogin(t, h, ip)
	"finishPasskeyLogin":    3, // finishPasskeyLogin(t, h, body, ip)
	"unlockPost":            3, // unlockPost(hnd, host, path, ip, password)
	"concurrentUnlockBurst": 3, // concurrentUnlockBurst(hnd, host, path, ip, password, n)
	"unlockBurstCodes":      3, // unlockBurstCodes(hnd, host, probePath, ip, n)
	// setTestClientAddr puts the address where a directly-connected request
	// carries it — r.RemoteAddr — which is the position ClientIP reads unless the
	// request arrived through a proxy (card t_ac16d273); a literal handed to it
	// is the same defect in a new spelling.
	"setTestClientAddr": 1, // setTestClientAddr(req, ip)
}

// ipHeaderNames are the headers the server reads a client address from, through
// middleware.ClientIP — and only when the request reached it through a trusted
// hop, which is why a test may not rely on one (see setTestClientAddr).
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
			"test Valkey. A literal sits outside every process's address block, so the counter it\n"+
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
		{"a setTestClientAddr argument", `fn() { setTestClientAddr(req, "10.9.9.9") }`},
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
		{"the sanctioned address setter", `fn() { setTestClientAddr(req, ip) }`},
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
//
// This direction covers the entries that ARE in the map: a key whose helper was
// renamed (a stale entry, so the literal scan would be looking for a call that
// cannot exist) and an index that no longer lands on the client-IP parameter.
// The OTHER direction — a helper that presents an address and is absent from the
// map — is a different walk and lives in
// TestEveryAddressPresentingHelperIsPinned below.
func TestEveryRequestHelperTakesATestIP(t *testing.T) {
	for name, idx := range ipArgHelpers {
		decl := findFuncDecl(t, name)
		if decl == nil {
			t.Errorf("ipArgHelpers names %q, which this package does not declare — the guard is scanning for a helper that does not exist", name)
			continue
		}
		names := funcParamNames(decl)
		if len(names) == 0 {
			continue
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

// TestEveryAddressPresentingHelperIsPinned is the other half of the pin, and it
// is the half the guard's own header advertised: a helper that takes a client
// address and is NOT in ipArgHelpers at all.
//
// TestEveryRequestHelperTakesATestIP above iterates the map's OWN keys, so it
// sees a renamed helper and a wrong index but is blind to a helper that was
// never added — and the map is what the literal scan calls through, so a new
// `…(…, ip string)` helper with a hand-written address at its call site passes
// both guards. Measured on the pre-fix tree (7960d09): a helper taking an `ip`
// and setting X-Real-IP from it, called with 10.99.0.7, left
// TestNoLiteralClientIPInTests and TestEveryRequestHelperTakesATestIP green.
// That address sits outside every process's /24, so the jf:loginfail:ip counter
// it writes is one the scoped reclaim can never clear — the cross-process flake
// this whole file exists to remove.
//
// So this walks the package's _test.go funcs (the same walker the literal scan
// uses, so a NEW file is covered because it exists) and fails on every func that
// presents its `ip` parameter to a request without being listed.
func TestEveryAddressPresentingHelperIsPinned(t *testing.T) {
	decls := packageFuncDecls(t)
	if len(decls) < 100 {
		t.Fatalf("scanned only %d func(s) — the file walker is broken, so this guard would pass vacuously", len(decls))
	}

	where := make(map[string]string, len(decls))
	for _, d := range decls {
		where[d.name] = d.file
	}

	var unlisted []string
	for name := range addressPresentingHelpers(decls) {
		if _, listed := ipArgHelpers[name]; !listed {
			unlisted = append(unlisted, where[name]+": "+name)
		}
	}
	sort.Strings(unlisted)
	if len(unlisted) > 0 {
		t.Errorf("%d helper(s) present a client address to a request but are not in ipArgHelpers —\n"+
			"their call sites are outside the literal scan, so a hand-written address handed to one is\n"+
			"exactly the counter the scoped reclaim cannot clear:\n  %s\n\n"+
			"Add each to ipArgHelpers with the zero-based index of its client-IP parameter, and pass\n"+
			"%s(t) at its call sites instead of a literal.",
			len(unlisted), strings.Join(unlisted, "\n  "), testIPHelperName)
	}
}

// TestTheAddressPresenterDetectorDiscriminates is the reverse scan's own control.
// A predicate that fires on nothing would make the pin above vacuous rather than
// green, and the two are indistinguishable from that test's output alone, so the
// detector is pinched between the shapes it must catch and the shapes it must
// leave alone.
func TestTheAddressPresenterDetectorDiscriminates(t *testing.T) {
	fires := []struct {
		what string
		src  string
		want []string
	}{
		{
			"a helper that presents the socket peer",
			`func qaPeer(req *http.Request, ip string) { setTestClientAddr(req, ip) }`,
			[]string{"qaPeer"},
		},
		{
			"a helper that assigns r.RemoteAddr",
			`func qaRemote(req *http.Request, ip string) { req.RemoteAddr = ip }`,
			[]string{"qaRemote"},
		},
		{
			// The shape the card's probe took: a NEW helper whose `ip` lands in
			// a header position the literal scan reads.
			"a helper that sets a client-IP header from its parameter",
			`func qaHeader(req *http.Request, path string, ip string) { req.Header.Set("X-Real-IP", ip) }`,
			[]string{"qaHeader"},
		},
		{
			"a wrapper that forwards to a listed helper",
			`func qaForward(mux *http.ServeMux, ip string) { postJSON(mux, "/x", nil, ip) }`,
			[]string{"qaForward"},
		},
		{
			"a wrapper one hop further from the request",
			"func qaInner(req *http.Request, ip string) { setTestClientAddr(req, ip) }\n" +
				`func qaOuter(req *http.Request, ip string) { qaInner(req, ip) }`,
			[]string{"qaInner", "qaOuter"},
		},
	}
	for _, tc := range fires {
		got := addressPresentingFromSource(t, tc.src)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("the reverse scan saw %v for %s — want %v; a presenter it misses is a helper whose\ncall sites stay outside the literal scan:\n  %s",
				got, tc.what, tc.want, tc.src)
		}
	}

	// Silent: a func that takes an address as DATA, and a request builder whose
	// address never comes from its own `ip` parameter. Both are legitimate, and a
	// false positive here would force an allowlist and hollow the pin out.
	staysSilent := []struct {
		what string
		src  string
	}{
		{
			"a validator that reads the address as data",
			`func qaValidIP(ip, family string) bool { return strings.HasPrefix(ip, family) }`,
		},
		{
			"a helper whose address parameter is not named ip",
			`func qaOtherName(req *http.Request, addr string) { setTestClientAddr(req, addr) }`,
		},
		{
			"a helper that builds a request without its own address",
			`func qaBuild(mux *http.ServeMux, ip string) { req := httptest.NewRequest("POST", "/x", nil); mux.ServeHTTP(httptest.NewRecorder(), req) }`,
		},
	}
	for _, tc := range staysSilent {
		if got := addressPresentingFromSource(t, tc.src); len(got) != 0 {
			t.Errorf("the reverse scan fired on %s — %v; the pin is a closed set, so a false positive\nhere would demand an entry (or an exemption) for a func that never presents an address:\n  %s",
				tc.what, got, tc.src)
		}
	}
}

// addressPresentingFromSource runs the reverse scan over a source snippet, for
// the control above.
func addressPresentingFromSource(t *testing.T, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "control.go", "package p\n"+src+"\n", 0)
	if err != nil {
		t.Fatalf("parse control snippet: %v\n  %s", err, src)
	}
	var decls []testFuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			decls = append(decls, testFuncDecl{name: fd.Name.Name, file: "control.go", decl: fd})
		}
	}

	var out []string
	for name := range addressPresentingHelpers(decls) {
		if _, listed := ipArgHelpers[name]; !listed {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// addressPresentingHelpers returns the funcs that put a client address into a
// request, keyed by name with the index of the `ip` parameter they present.
//
// It is seeded with the helpers the literal scan already knows (by definition
// they ARE presenters) and grown to a fixpoint, because a func that passes its
// own `ip` parameter to one of these presents the address too — which is how
// concurrentUnlockBurst, a wrapper around unlockPost, is seen.
func addressPresentingHelpers(decls []testFuncDecl) map[string]int {
	present := make(map[string]int, len(ipArgHelpers)+len(decls))
	for name, idx := range ipArgHelpers {
		present[name] = idx
	}
	for changed := true; changed; {
		changed = false
		for _, d := range decls {
			if _, known := present[d.name]; known {
				continue
			}
			idx := ipParamIndex(d.decl)
			if idx < 0 {
				continue
			}
			if presentsIPParameter(d.decl, idx, present) {
				present[d.name] = idx
				changed = true
			}
		}
	}
	return present
}

// presentsIPParameter reports whether decl hands the value of parameter idx into
// a request: through a helper that presents one (setTestClientAddr, the unlock
// and passkey helpers, …), a RemoteAddr assignment, or a client-IP header Set —
// the positions the literal scan reads, with the parameter where the literal
// would be.
//
// It is deliberately shallow about the VALUE: `ip` must reach the position
// itself (`setTestClientAddr(req, ip)`), not through a local copy. A helper that
// launders the address through a variable first is still missed, and that is the
// limit the header states.
func presentsIPParameter(decl *ast.FuncDecl, idx int, present map[string]int) bool {
	if decl.Body == nil || idx < 0 {
		return false
	}
	hit := false
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		if hit {
			return false
		}
		switch node := n.(type) {
		case *ast.CallExpr:
			// A call to a presenter, with this func's `ip` where that helper
			// takes its client address.
			if target, ok := present[callName(node)]; ok && target >= 0 && target < len(node.Args) {
				if referencesIPParameter(node.Args[target]) {
					hit = true
					return true
				}
			}
			// req.Header.Set("X-Real-IP", ip).
			if sel, ok := node.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Set" && len(node.Args) == 2 {
				if hdr, ok := stringLiteral(node.Args[0]); ok && ipHeaderNames[hdr] && referencesIPParameter(node.Args[1]) {
					hit = true
				}
			}
		case *ast.AssignStmt:
			// req.RemoteAddr = ip.
			for i, lhs := range node.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "RemoteAddr" || i >= len(node.Rhs) {
					continue
				}
				if referencesIPParameter(node.Rhs[i]) {
					hit = true
				}
			}
		}
		return true
	})
	return hit
}

// referencesIPParameter reports whether e reads the identifier `ip` — the name
// every client-address helper in this package gives its parameter, which is what
// lets the pin find them without a list. A field of that name (cfg.ip) is not
// the parameter, so it does not count.
func referencesIPParameter(e ast.Expr) bool {
	if e == nil {
		return false
	}
	fields := map[token.Pos]bool{}
	ast.Inspect(e, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			fields[sel.Sel.Pos()] = true
		}
		return true
	})
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == "ip" && !fields[id.Pos()] {
			found = true
		}
		return true
	})
	return found
}

// ipParamIndex returns the zero-based position of decl's parameter named "ip",
// or -1 when it declares none.
func ipParamIndex(decl *ast.FuncDecl) int {
	for i, name := range funcParamNames(decl) {
		if name == "ip" {
			return i
		}
	}
	return -1
}

// funcParamNames returns decl's parameter names in order. A field that binds no
// name (`*testing.T`) is one parameter with an empty name, so the index of a
// named parameter after it is its real position.
func funcParamNames(decl *ast.FuncDecl) []string {
	var names []string
	if decl.Type.Params == nil {
		return names
	}
	for _, f := range decl.Type.Params.List {
		if len(f.Names) == 0 {
			names = append(names, "")
			continue
		}
		for _, id := range f.Names {
			names = append(names, id.Name)
		}
	}
	return names
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
//
// Since card t_ac16d273 the address must go to setTestClientAddr (r.RemoteAddr):
// a client-IP header is only read when the request arrived through a proxy, which
// a bare-mux test request does not.
func TestLoginHelpersSetATestClientIP(t *testing.T) {
	for _, name := range []string{"loginOnce", "loginOnceUA"} {
		decl := findFuncDecl(t, name)
		if decl == nil {
			t.Fatalf("%s is not declared in this package — this guard would check nothing", name)
		}
		if !presentsTestIPToTheLayer(decl.Body) {
			t.Errorf("%s does not present its client address through %s(req, %s(t)) — without it every\n"+
				"request it builds carries httptest's default 192.0.2.1, which is outside every process's\n"+
				"reclaimed /24, so any wrong-credential login it drives leaves a counter no run can clear",
				name, testClientAddrHelper, testIPHelperName)
		}
	}
}

// testClientAddrHelper is the one sanctioned way to present a client address to
// a request built by hand (testutil_test.go): it writes r.RemoteAddr.
const testClientAddrHelper = "setTestClientAddr"

// presentsTestIPToTheLayer reports whether body hands the address the layer
// reads — via setTestClientAddr or a RemoteAddr assignment — a value derived
// from testIP(t).
func presentsTestIPToTheLayer(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			if callName(node) == testClientAddrHelper {
				for _, arg := range node.Args {
					if inner, ok := arg.(*ast.CallExpr); ok && callName(inner) == testIPHelperName {
						found = true
					}
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range node.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "RemoteAddr" || i >= len(node.Rhs) {
					continue
				}
				if inner, ok := node.Rhs[i].(*ast.CallExpr); ok && callName(inner) == testIPHelperName {
					found = true
				}
			}
		}
		return true
	})
	return found
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

// testFuncDecl is one func declaration from this package's test files, carrying
// the file it came from so a finding can name it.
type testFuncDecl struct {
	name string
	file string
	decl *ast.FuncDecl
}

// packageFuncDecls parses each of this package's _test.go files once and returns
// the funcs they declare.
func packageFuncDecls(t *testing.T) []testFuncDecl {
	t.Helper()
	var out []testFuncDecl
	for _, path := range packageTestGoFiles(t) {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				out = append(out, testFuncDecl{name: fd.Name.Name, file: filepath.Base(path), decl: fd})
			}
		}
	}
	return out
}

func findFuncDecl(t *testing.T, name string) *ast.FuncDecl {
	t.Helper()
	for _, d := range packageFuncDecls(t) {
		if d.name == name {
			return d.decl
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
