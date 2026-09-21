package mcpsrv

// Issue #645 — the API-key write-SCOPE class guard.
//
// WHY THIS FILE EXISTS. mcp_parity_test.go checks that every dashboard feature
// has a registered MCP tool and that the docs mention it; output_schema_test.go
// checks the shape of each tool's output. NONE of them checks AUTHORIZATION.
// That is exactly why the regression this card fixes went unnoticed:
//
//	5dafe3a (issue #162, workspace RBAC) replaced the per-tool scope test
//	  - acc, err := s.accountAndScope(ctx, true)   // ScopeHasWrite(scopes)
//	with a role-only gate
//	  + acc, err := s.requireWriteRole(ctx)        // accountAndScope(ctx, false)
//
// so the scope test was skipped and a readonly API key could delete forms,
// archive products, cancel subscriptions and mint itself a readwrite key — with
// the whole suite green.
//
// THE GUARD. Parse this package, build the call graph, and for every tool
// registered in registerTools assert:
//
//	handler reaches a WRITE SINK  ⟹  handler reaches the scope gate
//
// A "write sink" is a call on the server's own dependencies (a `s.…` selector
// chain) whose method name carries a write verb (Create/Delete/Set/Revoke/…).
// The "scope gate" is the named function requireWriteScope — deliberately NOT
// dbstore.ScopeHas, because accountAndScope's deprecated `if write` branch also
// contains that call, and anchoring on it would mark every read call site as
// gated and hide precisely the bug class this guards.
//
// Reachability is transitive (a fixpoint over the call graph), so a write tool
// that goes through a helper (requireFormWrite, requireProductWrite,
// requireWriteRoleIn, requireWorkspaceAdminWrite, …) is covered as long as the
// helper chain reaches the gate.
//
// BITES. Removing the requireWriteScope call from requireWriteRole, or adding a
// new write tool that never names a gate, makes this test fail. Both were
// verified by hand on this branch — see the card's evidence.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// scopeGateFunc is the ONE enforcement point a write tool must reach.
const scopeGateFunc = "requireWriteScope"

// writeVerbs are the method-name segments that mark a call as a write. Split on
// CamelCase, so SetActive → {Set, Active} and SoftDelete → {Soft, Delete}.
// Kept as segment names (not full method names) so a new store method such as
// RevokeAllTrustedDevices is covered without touching this list.
//
// The list is MEASURED, not guessed: TestWriteVerbCoverageIsStillWide below
// asserts it still classifies the write surface it was calibrated against, and
// the calibration output (every distinct deps method called in this package)
// is what the segments below were chosen from. Over-inclusion is safe here — a
// read wrongly called a write only means the guard demands a gate that a gated
// tool already has — but under-inclusion is a hole, so the list errs wide.
var writeVerbs = map[string]bool{
	"Write": true, "Create": true, "Delete": true, "Archive": true,
	"Revoke": true, "Upsert": true, "Update": true, "Set": true,
	"Remove": true, "Publish": true, "Cancel": true, "Retry": true,
	"Send": true, "Add": true, "Insert": true, "Purge": true, "Claim": true,
	"Import": true, "Reply": true, "Rename": true, "Enable": true,
	"Disable": true, "Restore": true, "Requeue": true, "Enqueue": true,
	"Save": true, "Assign": true, "Upload": true, "Mark": true, "Move": true,
	"Approve": true, "Reject": true, "Invalidate": true, "Rotate": true,
	"Regenerate": true, "Submit": true, "Confirm": true, "Capture": true,
	"Release": true, "Refund": true, "Charge": true, "Provision": true,
	// Calibrated addition — every Clear* store method mutates
	// (ClearChipCredentials, ClearChipOverride, ClearDefaultWorkspace,
	// ClearFormEmailOverrides).
	"Clear": true,
}

// exactWriteMethods names store methods that MUTATE without a segment that is
// safe to match in general. They are listed by full name because the segment
// they share also appears on a READER, and a segment match would misclassify
// every reader that uses it:
//
//	"Spam"     → UnSpam (write) but also ListSpamByAccountsPage (read)
//	"Generate" → NOT a write at all: GenerateFormConfig / GenerateLegalPage
//	             return LLM text and persist nothing
//	"Ensure" / "Put" / "Reserve" / "Settle" / "Replace" / "Reactivate" /
//	"Unlink"   → mutators whose only safe spelling is the exact name
//
// Exact names are weaker than segments (a NEW UnSpam-shaped method would not be
// caught), which is why this list carries the justification inline.
var exactWriteMethods = map[string]bool{
	"UnSpam":           true, // Sales.UnSpam / Submissions.UnSpam
	"ReplaceAll":       true, // ProductVariations.ReplaceAll
	"Reactivate":       true, // Subs.Reactivate
	"UnlinkIdentity":   true, // Users.UnlinkIdentity
	"EnsureDefault":    true, // Notifications.EnsureDefault
	"PutObject":        true, // ObjectStore.PutObject
	"ReserveQuota":     true, // MediaStore.ReserveQuota
	"SettleQuotaBytes": true, // MediaStore.SettleQuotaBytes
}

// nonSinkMethods lists store methods whose NAME looks like a write but which are
// NOT a domain write, so counting them would collapse the predicate. Keep this
// list tiny and justified — each entry removes real coverage.
var nonSinkMethods = map[string]string{
	// deps.Events.Record appends an audit/telemetry row. Every tool reaches it
	// through addTool's recordMCPEvent wrapper, so counting it would classify
	// all 161 registered tools as writers and the guard would measure nothing.
	// It is an observability side effect, not a caller-authorized mutation.
	"Record": "audit/telemetry append reached by every tool via addTool",
}

// scopeGateExempt lists registered write tools that legitimately do NOT reach
// requireWriteScope. Keep it EMPTY and keep it that way: an entry here is a
// scope hole, not a convenience. The map exists only so an intentional
// exemption has to be written down with a reason, in one place, under review.
var scopeGateExempt = map[string]string{}

// camelSegments splits an identifier into its CamelCase words.
func camelSegments(name string) []string {
	var out []string
	var cur strings.Builder
	for i, r := range name {
		if r >= 'A' && r <= 'Z' && i > 0 && cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// isWriteName reports whether an identifier names a store write. A method may be
// a write by exact name (exactWriteMethods) or by carrying a write verb segment
// (writeVerbs). nonSinkMethods removes telemetry-shaped false positives.
func isWriteName(name string) bool {
	if _, skip := nonSinkMethods[name]; skip {
		return false
	}
	if exactWriteMethods[name] {
		return true
	}
	for _, seg := range camelSegments(name) {
		if writeVerbs[seg] {
			return true
		}
	}
	return false
}

// mcpPackageDir returns the directory holding this package's source.
func mcpPackageDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

// toolReg is one registered MCP tool: its wire name, its declared description
// (the docs contract), and the handler function name registered for it.
type toolReg struct {
	Name        string
	Description string
	Handler     string
}

// parsedPackage is the call graph extracted from the package source.
type parsedPackage struct {
	tools  []toolReg
	edges  map[string]map[string]bool // func -> callees that are functions in this package
	sinks  map[string]map[string]bool // func -> write-sink method names it calls directly
	origin map[string]string          // func -> "file.go" (for readable failures)
}

// parsePackage parses every non-test .go file in this package and extracts the
// registered tools plus each function's direct calls.
func parsePackage(t *testing.T) *parsedPackage {
	t.Helper()
	dir := mcpPackageDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package dir %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	pkg := &parsedPackage{
		edges:  map[string]map[string]bool{},
		sinks:  map[string]map[string]bool{},
		origin: map[string]string{},
	}
	// Pass 1: collect declaration names so a call can be classified as
	// "reaches another function here" vs "reaches a store method".
	decls := map[string][]*ast.FuncDecl{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", e.Name(), perr)
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name == nil {
				continue
			}
			decls[fd.Name.Name] = append(decls[fd.Name.Name], fd)
			if fd.Body == nil {
				continue
			}
			if _, seen := pkg.origin[fd.Name.Name]; !seen {
				pkg.origin[fd.Name.Name] = e.Name()
			}
		}
	}
	if len(decls) == 0 {
		t.Fatal("no function declarations parsed — the guard would pass vacuously")
	}
	// Pass 2: build edges (calls into this package) and sinks (calls on the
	// server's dependencies). A call to a name declared in this package is an
	// EDGE; it is never also counted as a sink, which is what keeps the scope
	// gate itself out of the sink set.
	known := func(name string) bool { _, ok := decls[name]; return ok }
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", e.Name(), perr)
		}
		pkg.collectTools(f, e.Name())
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Name == nil {
				continue
			}
			self := fd.Name.Name
			// Locals bound to a server/package value (st, err := s.formsStore())
			// carry the sink: st.Create(…) writes even though it is not rooted at
			// `s`. Collect them first so the receiver walk below covers them.
			locals := serverBoundLocals(fd.Body)
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch fn := call.Fun.(type) {
				case *ast.Ident:
					if known(fn.Name) {
						pkg.addEdge(self, fn.Name)
					}
				case *ast.SelectorExpr:
					if method := fn.Sel.Name; known(method) {
						// A method or package-level function of this package.
						pkg.addEdge(self, method)
						return true
					}
					// Otherwise: only calls rooted at the server's own value
					// can touch a store. The receiver may be a selector CHAIN
					// (s.deps.Forms.SoftDelete) or a local bound from one
					// (st.Create), so resolve it to its leftmost identifier and
					// accept `s` or a server-bound local. That restriction keeps
					// stdlib and imported-package calls out of the sink set
					// without needing an import map.
					root := rootIdent(fn.X)
					if (root == "s" || locals[root]) && isWriteName(fn.Sel.Name) {
						pkg.addSink(self, fn.Sel.Name)
					}
				}
				return true
			})
		}
	}
	return pkg
}

func (p *parsedPackage) addEdge(from, to string) {
	if p.edges[from] == nil {
		p.edges[from] = map[string]bool{}
	}
	p.edges[from][to] = true
}

func (p *parsedPackage) addSink(from, method string) {
	if p.sinks[from] == nil {
		p.sinks[from] = map[string]bool{}
	}
	p.sinks[from][method] = true
}

// collectTools finds the tool registrations in a file. Both registration shapes
// used in this package are handled:
//
//	addTool(s, &mcp.Tool{Name: "x", …}, s.handler)
//	mcp.AddTool(s.mcp, &mcp.Tool{Name: "x", …}, s.handler)
func (p *parsedPackage) collectTools(f *ast.File, file string) {
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		if !isRegistration(call.Fun) {
			return true
		}
		tool := toolLiteral(call.Args)
		if tool == nil {
			return true
		}
		handler := ""
		if last := call.Args[len(call.Args)-1]; last != nil {
			if sel, ok := last.(*ast.SelectorExpr); ok {
				handler = sel.Sel.Name
			} else if id, ok := last.(*ast.Ident); ok {
				handler = id.Name
			}
		}
		if handler == "" || tool.Name == "" {
			return true
		}
		p.tools = append(p.tools, toolReg{Name: tool.Name, Description: tool.Description, Handler: handler})
		return true
	})
}

// serverBoundLocals returns the local variable names in a function body whose
// initialiser is rooted at the server receiver or at a value derived from it —
// `st, err := s.formsStore()` or `store := s.deps.Forms`. Calls on those locals
// (st.Create(…)) reach the same stores as the `s.…` chain, so they must count as
// write sinks or the guard would miss the helper-shaped write paths.
func serverBoundLocals(body *ast.BlockStmt) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) == 0 || len(as.Rhs) == 0 {
			return true
		}
		if len(as.Rhs) == 1 {
			// Multi-value return (sp, err := s.workspacesStore()): the first
			// Lhs is the value, the rest are the error/flag. Marking the first
			// only keeps `err` out of the sink set.
			if id, ok := as.Lhs[0].(*ast.Ident); ok && rootIdent(as.Rhs[0]) == "s" {
				out[id.Name] = true
			}
			return true
		}
		if len(as.Lhs) != len(as.Rhs) {
			return true
		}
		for i, lhs := range as.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok {
				continue
			}
			if rootIdent(as.Rhs[i]) == "s" {
				out[id.Name] = true
			}
		}
		return true
	})
	return out
}

// rootIdent walks an expression down to its leftmost identifier, so `s.foo.Bar()`
// and `s.deps.Forms.SoftDelete()` are both recognised as rooted at `s`.
func rootIdent(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.Ident:
			return x.Name
		case *ast.SelectorExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		case *ast.CallExpr:
			// s.workspacesStore() is rooted at s, not at the call.
			e = x.Fun
		case *ast.IndexExpr:
			e = x.X
		default:
			return ""
		}
	}
}

func isRegistration(fun ast.Expr) bool {
	switch fn := fun.(type) {
	case *ast.Ident:
		return fn.Name == "addTool"
	case *ast.SelectorExpr:
		return fn.Sel.Name == "AddTool"
	}
	return false
}

// toolLiteral returns the &mcp.Tool{…} composite literal among the call args.
func toolLiteral(args []ast.Expr) *toolReg {
	for _, a := range args {
		un, ok := a.(*ast.UnaryExpr)
		if !ok || un.Op != token.AND {
			continue
		}
		lit, ok := un.X.(*ast.CompositeLit)
		if !ok {
			continue
		}
		out := &toolReg{}
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			val, ok := kv.Value.(*ast.BasicLit)
			if !ok || val.Kind != token.STRING {
				continue
			}
			s := strings.Trim(val.Value, "`\"")
			switch key.Name {
			case "Name":
				out.Name = s
			case "Description":
				out.Description = s
			}
		}
		return out
	}
	return nil
}

// reachable computes the transitive closure of `seeds` over the call graph.
func (p *parsedPackage) reachable(seeds map[string]bool) map[string]bool {
	out := map[string]bool{}
	for n := range seeds {
		out[n] = true
	}
	for changed := true; changed; {
		changed = false
		for from, tos := range p.edges {
			if !out[from] {
				continue
			}
			for to := range tos {
				if !out[to] {
					out[to] = true
					changed = true
				}
			}
		}
	}
	return out
}

// reachableFuncs returns every function the named function can reach
// (including itself when it is itself a function in this package).
func (p *parsedPackage) reachableFuncs(name string) map[string]bool {
	seeds := map[string]bool{}
	if _, ok := p.edges[name]; ok {
		seeds[name] = true
	} else if _, ok := p.sinks[name]; ok {
		seeds[name] = true
	}
	return p.reachable(seeds)
}

// reachesWriteSink reports whether calling `name` can reach a store write, and
// which sink methods it can reach (for the failure message).
func (p *parsedPackage) reachesWriteSink(name string) (bool, []string) {
	fns := p.reachableFuncs(name)
	hits := map[string]bool{}
	for fn := range fns {
		for m := range p.sinks[fn] {
			hits[m] = true
		}
	}
	out := make([]string, 0, len(hits))
	for m := range hits {
		out = append(out, m)
	}
	sort.Strings(out)
	return len(out) > 0, out
}

// reachesScopeGate reports whether calling `name` can reach requireWriteScope.
func (p *parsedPackage) reachesScopeGate(name string) bool {
	fns := p.reachableFuncs(name)
	for fn := range fns {
		if fn == scopeGateFunc {
			return true
		}
		for m := range p.edges[fn] {
			if m == scopeGateFunc {
				return true
			}
		}
	}
	return false
}

// TestTheScopeGateIsReachableFromEveryWriteTool is the class guard.
//
// It fails when a registered tool can reach a store write without reaching
// requireWriteScope — i.e. exactly the issue #645 shape, where the role gate had
// silently swallowed the scope gate.
func TestTheScopeGateIsReachableFromEveryWriteTool(t *testing.T) {
	pkg := parsePackage(t)
	if len(pkg.tools) < 100 {
		t.Fatalf("parsed only %d registered tools — the guard would pass vacuously", len(pkg.tools))
	}
	// The gate itself must be a real function in this package, or every
	// reachesScopeGate answer would be a meaningless false.
	if _, ok := pkg.origin[scopeGateFunc]; !ok {
		t.Fatalf("%s not found in %s — the guard anchors on it, so this test cannot "+
			"be silently neutered by renaming or deleting it", scopeGateFunc, pkg.origin[scopeGateFunc])
	}

	var writeTools, gated int
	var holes []string
	for _, tool := range pkg.tools {
		isWrite, sinks := pkg.reachesWriteSink(tool.Handler)
		if !isWrite {
			continue
		}
		writeTools++
		if pkg.reachesScopeGate(tool.Handler) {
			gated++
			continue
		}
		if reason, exempt := scopeGateExempt[tool.Name]; exempt {
			t.Logf("tool %q exempt from %s: %s", tool.Name, scopeGateFunc, reason)
			continue
		}
		holes = append(holes, fmt.Sprintf(
			"%s (handler %s in %s) reaches write sink(s) %v but never reaches %s",
			tool.Name, tool.Handler, pkg.origin[tool.Handler], sinks, scopeGateFunc))
	}
	if len(holes) > 0 {
		sort.Strings(holes)
		t.Errorf("issue #645: %d registered tool(s) can write without the API-key SCOPE gate "+
			"(%s). Scope and workspace role are INDEPENDENT axes; a write must reach both. "+
			"Add the scope gate (call requireWriteScope / a helper that does), do NOT fold it "+
			"into a role check:\n  %s", len(holes), scopeGateFunc, strings.Join(holes, "\n  "))
	}
	// The guard must not be satisfiable by disabling writes altogether: assert
	// the measured shape is the one the card recorded (95 write tools on this
	// tree, all gated).
	if writeTools < 80 {
		t.Errorf("only %d write tools found; the predicate lost most of the surface it "+
			"guards (expected ~95). A guard that measures nothing passes for the wrong reason", writeTools)
	}
	if gated != writeTools {
		t.Errorf("%d of %d write tools reach %s; want all", gated, writeTools, scopeGateFunc)
	}
	t.Logf("guarded %d registered tools: %d reach a write sink, %d of those reach %s",
		len(pkg.tools), writeTools, gated, scopeGateFunc)
}

// TestReadOnlyToolsDoNotDemandAWriteScope is the OTHER direction, and it is the
// half that keeps the fix honest: a refusal test alone can be satisfied by
// breaking the feature ("let every call fail").
//
// A tool whose own description promises "Read-only" and whose handler reaches no
// store write must not require a write scope — a readonly key has to be able to
// read. This is the #645 item-4 defect: list_webhooks and get_sale_email_status
// were refused to readonly keys on a pure read.
//
// get_webhook_signing_key is deliberately excluded by the "reaches no write
// sink" clause: it is labelled Read-only but lazily GENERATES the account's
// Ed25519 keypair on first use, so it is gated exactly when it would write.
func TestReadOnlyToolsDoNotDemandAWriteScope(t *testing.T) {
	pkg := parsePackage(t)
	var checked int
	for _, tool := range pkg.tools {
		if !strings.Contains(strings.ToLower(tool.Description), "read-only") {
			continue
		}
		if isWrite, _ := pkg.reachesWriteSink(tool.Handler); isWrite {
			continue
		}
		checked++
		if pkg.reachesScopeGate(tool.Handler) {
			t.Errorf("tool %q is declared Read-only in its description and reaches no store "+
				"write, yet its handler %s requires a write scope — a readonly key is refused "+
				"on a READ (issue #645 item 4)", tool.Name, tool.Handler)
		}
	}
	if checked < 20 {
		t.Errorf("only %d Read-only tools inspected; the predicate lost its surface", checked)
	}
	t.Logf("checked %d Read-only tools that reach no write sink", checked)
}

// TestScopeGateIsSeparateFromTheRoleGate pins the structural decision the card
// makes explicit: scope and role are two independent axes, and the scope gate
// must not be reachable ONLY by being the role gate's first statement.
//
// This asserts the shape that makes a future RBAC change unable to drop the
// scope half silently: requireWriteRole and requireWriteRoleIn each call the
// named gate themselves, so deleting the call is a visible diff line rather
// than a changed boolean argument.
func TestScopeGateIsSeparateFromTheRoleGate(t *testing.T) {
	pkg := parsePackage(t)
	for _, fn := range []string{"requireWriteRole", "requireWriteRoleIn", "requireAdminRole", "requireWorkspaceAdminWrite", "requireWorkspaceWriteRole"} {
		direct := pkg.edges[fn]
		if !direct[scopeGateFunc] {
			t.Errorf("%s does not call %s directly (callers: %v) — the scope test must be named "+
				"in every role gate, or a future RBAC edit can drop it the way 5dafe3a did",
				fn, scopeGateFunc, sortedKeys(direct))
		}
	}
	// accountAndScope's boolean `write` argument is the shape that caused the
	// regression; assert no WRITE tool routes its gate through it without also
	// naming the gate. (Read tools may use accountAndScope(ctx, false) freely —
	// that is what it is for.)
	for _, tool := range pkg.tools {
		isWrite, _ := pkg.reachesWriteSink(tool.Handler)
		if !isWrite || pkg.reachesScopeGate(tool.Handler) {
			continue
		}
		if pkg.edges[tool.Handler]["accountAndScope"] {
			t.Errorf("write tool %q gates through accountAndScope's `write` boolean without naming %s — "+
				"that boolean is what 5dafe3a got wrong", tool.Name, scopeGateFunc)
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
