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

	// decls is every function declaration in the package, keyed by name. It is
	// what lets a registration's tool argument be RESOLVED when it is not an
	// inline literal: `addTool(s, disable2FATool(), …)` names a function whose
	// body returns the &mcp.Tool{…}, and a parser that cannot follow that call
	// reads a registry smaller than the one the server serves.
	decls map[string][]*ast.FuncDecl

	// methods holds the selector names that are METHODS of this package (a
	// declaration with a receiver). A selector call is only an edge to a
	// method: a package-level function is called UNQUALIFIED in Go, so
	// `errors.New(...)` can never be a call to this package's New. Matching a
	// selector on `decls` alone made exactly that mistake — every handler that
	// formats an error with errors.New "reached" New, and so reached every sink
	// New could reach (measured: 20 read tools reported as store writers the
	// day New gained a registration sink).
	methods map[string]bool

	// regs is EVERY addTool / mcp.AddTool call site the parser saw — including
	// the ones whose tool argument it could not resolve. A registration shape
	// the parser does not understand must fail by name and file:line
	// (TestEveryToolRegistrationYieldsATool) instead of quietly shrinking the
	// set every Go-side tool guard measures against.
	regs []registrationSite

	fset *token.FileSet // for file:line in failure messages
}

// registrationSite is one registration-shaped call site.
type registrationSite struct {
	File   string // "tools.go"
	Line   int    // 1-based line of the call
	Tool   string // resolved wire name; "" when the parser could not resolve it
	Reason string // why it did not resolve — carried verbatim into the failure
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
		edges:   map[string]map[string]bool{},
		sinks:   map[string]map[string]bool{},
		origin:  map[string]string{},
		decls:   map[string][]*ast.FuncDecl{},
		methods: map[string]bool{},
		fset:    fset,
	}
	// Pass 1: collect declaration names so a call can be classified as
	// "reaches another function here" vs "reaches a store method".
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
			pkg.decls[fd.Name.Name] = append(pkg.decls[fd.Name.Name], fd)
			if fd.Recv != nil {
				pkg.methods[fd.Name.Name] = true
			}
			if fd.Body == nil {
				continue
			}
			if _, seen := pkg.origin[fd.Name.Name]; !seen {
				pkg.origin[fd.Name.Name] = e.Name()
			}
		}
	}
	if len(pkg.decls) == 0 {
		t.Fatal("no function declarations parsed — the guard would pass vacuously")
	}
	// Pass 2: build edges (calls into this package) and sinks (calls on the
	// server's dependencies). A call to a name declared in this package is an
	// EDGE; it is never also counted as a sink, which is what keeps the scope
	// gate itself out of the sink set.
	known := func(name string) bool { _, ok := pkg.decls[name]; return ok }
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
					if method := fn.Sel.Name; known(method) && pkg.methods[method] {
						// A METHOD of this package (selected on the server,
						// a store or any other receiver).
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

// collectTools finds the tool registrations in a file. EVERY registration shape
// this package uses is handled:
//
//	addTool(s, &mcp.Tool{Name: "x", …}, s.handler)      — inline literal
//	mcp.AddTool(s.mcp, &mcp.Tool{Name: "x", …}, s.handler)
//	addTool(s, disable2FATool(), s.handler)             — same-package constructor
//
// The third shape is why "an inline literal, or nothing" was not enough:
// disable2FA's definition lives in a constructor function
// (parity_issue396.go), so reading only inline literals made the tool — and the
// guards that sweep the registry to police it — blind to a registered,
// write-capable tool. A site whose argument STILL does not resolve is recorded in
// p.regs, so TestEveryToolRegistrationYieldsATool fails on it by name and
// file:line instead of the tool quietly leaving every guard's view.
func (p *parsedPackage) collectTools(f *ast.File, file string) {
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		if !isRegistration(call.Fun) {
			return true
		}
		site := registrationSite{File: file, Line: p.fset.Position(call.Pos()).Line}

		tool, why := p.resolveToolArg(f, call)
		handler := ""
		if last := call.Args[len(call.Args)-1]; last != nil {
			if sel, ok := last.(*ast.SelectorExpr); ok {
				handler = sel.Sel.Name
			} else if id, ok := last.(*ast.Ident); ok {
				handler = id.Name
			}
		}
		if tool == nil || tool.Name == "" || handler == "" {
			if isRegistrationPrimitiveForwarding(f, call) {
				return true
			}
			switch {
			case tool == nil:
				site.Reason = "the tool argument is a shape this parser cannot resolve (" + why + ")"
			case tool.Name == "":
				site.Reason = "the resolved tool definition carries no `Name:` field, so no guard can key on it"
			default:
				site.Reason = "the tool resolves to `" + tool.Name + "` but the registration names no handler function"
			}
			p.regs = append(p.regs, site)
			return true
		}
		site.Tool = tool.Name
		p.regs = append(p.regs, site)
		p.tools = append(p.tools, toolReg{Name: tool.Name, Description: tool.Description, Handler: handler})
		return true
	})
}

// resolveToolArg resolves a registration call's tool-definition argument. When
// it cannot, the returned string describes the arguments it saw, so the failure
// names the shape instead of only the file:line.
func (p *parsedPackage) resolveToolArg(f *ast.File, call *ast.CallExpr) (*toolReg, string) {
	var shapes []string
	for _, a := range call.Args {
		if reg := p.toolFromExpr(f, call, a); reg != nil {
			return reg, ""
		}
		shapes = append(shapes, describeToolArg(a))
	}
	return nil, strings.Join(shapes, ", ")
}

// toolFromExpr resolves ONE registration argument to a tool definition, or nil
// when the argument is not one. Understood shapes:
//
//	&mcp.Tool{Name: "x", …}   an inline literal
//	disable2FATool()          a function in THIS package returning one
//	t                         a local bound to either, e.g. `t := &mcp.Tool{…}`
//
// A resolved tool may still carry an empty Name; the caller reports that as a
// failure rather than dropping the tool.
func (p *parsedPackage) toolFromExpr(f *ast.File, call *ast.CallExpr, arg ast.Expr) *toolReg {
	if lit := compositeToolLiteral(arg); lit != nil {
		return toolFromLiteral(lit)
	}
	if ce, ok := arg.(*ast.CallExpr); ok {
		name := calleeName(ce.Fun)
		if name == "" {
			return nil
		}
		for _, fd := range p.decls[name] {
			if out := returnedToolLiteral(fd); out != nil {
				return out
			}
		}
		return nil
	}
	if id, ok := arg.(*ast.Ident); ok {
		if fd := enclosingFuncDecl(f, call.Pos()); fd != nil {
			if lit := localToolLiteral(fd.Body, id.Name); lit != nil {
				return toolFromLiteral(lit)
			}
		}
	}
	return nil
}

// returnedToolLiteral reads a constructor's tool definition: the `return
// &mcp.Tool{…}` among its OWN top-level statements, or `t := &mcp.Tool{…}; return t`.
//
// Only top-level returns are read, because a constructor can build helper
// closures that return unrelated literals — disable2FATool defines
// `str := func(…) *jsonschema.Schema { return &jsonschema.Schema{…} }`, and
// accepting that would make the parser invent a nameless tool.
func returnedToolLiteral(fd *ast.FuncDecl) *toolReg {
	if fd == nil || fd.Body == nil {
		return nil
	}
	for _, st := range fd.Body.List {
		ret, ok := st.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			continue
		}
		if lit := compositeToolLiteral(ret.Results[0]); lit != nil {
			return toolFromLiteral(lit)
		}
		if id, ok := ret.Results[0].(*ast.Ident); ok {
			if lit := localToolLiteral(fd.Body, id.Name); lit != nil {
				return toolFromLiteral(lit)
			}
		}
	}
	return nil
}

// localToolLiteral finds the `name := &mcp.Tool{…}` (or `var name = …`) a body
// binds, so a tool held in a local variable is still resolvable.
func localToolLiteral(body *ast.BlockStmt, name string) *ast.CompositeLit {
	if body == nil {
		return nil
	}
	var out *ast.CompositeLit
	ast.Inspect(body, func(n ast.Node) bool {
		if out != nil {
			return false
		}
		switch x := n.(type) {
		case *ast.AssignStmt:
			if len(x.Lhs) != len(x.Rhs) {
				return true
			}
			for i, lhs := range x.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name == name {
					if lit := compositeToolLiteral(x.Rhs[i]); lit != nil {
						out = lit
					}
				}
			}
		case *ast.ValueSpec:
			for i, id := range x.Names {
				if id.Name == name && i < len(x.Values) {
					if lit := compositeToolLiteral(x.Values[i]); lit != nil {
						out = lit
					}
				}
			}
		}
		return true
	})
	return out
}

// compositeToolLiteral unwraps an expression to an `&mcp.Tool{…}` (or a bare
// `mcp.Tool{…}`) composite literal. The literal's TYPE must be named Tool, so an
// unrelated literal — disable2FATool's *jsonschema.Schema helper — is never
// mistaken for a tool definition.
func compositeToolLiteral(e ast.Expr) *ast.CompositeLit {
	if un, ok := e.(*ast.UnaryExpr); ok {
		if un.Op != token.AND {
			return nil
		}
		e = un.X
	}
	lit, ok := e.(*ast.CompositeLit)
	if !ok || !isToolTypeExpr(lit.Type) {
		return nil
	}
	return lit
}

// isToolTypeExpr reports whether a type expression names this SDK's Tool type
// (`mcp.Tool`).
func isToolTypeExpr(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	return ok && sel.Sel != nil && sel.Sel.Name == "Tool"
}

// toolFromLiteral reads the Name/Description fields of a Tool literal.
func toolFromLiteral(lit *ast.CompositeLit) *toolReg {
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

// describeToolArg renders an unresolved argument shape for the failure message.
func describeToolArg(a ast.Expr) string {
	switch x := a.(type) {
	case *ast.Ident:
		return "identifier `" + x.Name + "`"
	case *ast.CallExpr:
		if n := calleeName(x.Fun); n != "" {
			return "call `" + n + "()`"
		}
		return "an unrecognised call"
	case *ast.SelectorExpr:
		if x.Sel != nil {
			return "selector ending in `." + x.Sel.Name + "`"
		}
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return "a `&` expression that is not a Tool literal"
		}
	}
	return "an unrecognised expression"
}

// calleeName names the function a call expression calls: a plain identifier
// (`disable2FATool()`) or a selector (`mcp.AddTool(…)` → "AddTool").
func calleeName(fun ast.Expr) string {
	switch x := fun.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		if x.Sel != nil {
			return x.Sel.Name
		}
	case *ast.ParenExpr:
		return calleeName(x.X)
	}
	return ""
}

// enclosingFuncDecl returns the innermost function declaration of f whose body
// contains pos.
func enclosingFuncDecl(f *ast.File, pos token.Pos) *ast.FuncDecl {
	var best *ast.FuncDecl
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil || fd.Name == nil {
			continue
		}
		if fd.Pos() <= pos && pos <= fd.End() {
			if best == nil || fd.Pos() > best.Pos() {
				best = fd
			}
		}
	}
	return best
}

// isFuncParam reports whether name is a parameter of fd.
func isFuncParam(fd *ast.FuncDecl, name string) bool {
	if fd == nil || fd.Type == nil || fd.Type.Params == nil {
		return false
	}
	for _, fld := range fd.Type.Params.List {
		for _, id := range fld.Names {
			if id.Name == name {
				return true
			}
		}
	}
	return false
}

// isRegistrationPrimitiveForwarding reports whether a registration-shaped call is
// the registration PRIMITIVE forwarding its own argument: addTool (mcp_history.go)
// takes `t *mcp.Tool` and passes it to mcp.AddTool. That call site registers
// nothing by itself — the tools are registered by the primitive's callers, every
// one of which the parser resolves — so it is plumbing, not a shape the parser is
// blind to. Both halves must hold: the enclosing function takes a *mcp.Tool AND
// the argument that did not resolve is one of that function's parameters, so a
// literal-carrying call inside such a helper is still counted as a registration.
func isRegistrationPrimitiveForwarding(f *ast.File, call *ast.CallExpr) bool {
	fd := enclosingFuncDecl(f, call.Pos())
	if fd == nil || fd.Type == nil || fd.Type.Params == nil {
		return false
	}
	hasToolParam := false
	for _, fld := range fd.Type.Params.List {
		if star, ok := fld.Type.(*ast.StarExpr); ok && isToolTypeExpr(star.X) {
			hasToolParam = true
		}
	}
	if !hasToolParam {
		return false
	}
	for _, a := range call.Args {
		if id, ok := a.(*ast.Ident); ok && isFuncParam(fd, id.Name) {
			return true
		}
	}
	return false
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

// TestSelectorEdgesFollowMethodsOnly pins the rule that keeps a name collision
// from forging a call edge.
//
// WHY IT MATTERS. A package-level function is called UNQUALIFIED in Go, so a
// selector call can only ever reach a METHOD of this package. The parser used to
// accept any selector whose name matched a declaration, which made
// `errors.New(...)` an edge to this package's `New`. While New reached no store
// that was invisible; the moment it registered a resource template (a sink by
// its "Add" verb, card t_52429d6f) every handler that formats an error — 20 of
// the chat's READ tools — was reported as a store writer, and two guards failed
// with a list of tools nobody had touched.
//
// The second assertion keeps this test from passing for the wrong reason: if the
// parser stopped following `registerLifecycleResource` to the registrar at all,
// the first assertion would hold while the graph had simply gone blind.
func TestSelectorEdgesFollowMethodsOnly(t *testing.T) {
	pkg := parsePackage(t)
	if fns := pkg.reachableFuncs("getForm"); fns["registerLifecycleResource"] {
		t.Errorf("getForm reaches registerLifecycleResource: a selector call was resolved to a " +
			"package-level function, so `errors.New(...)` is being read as a call to New")
	}
	if isWrite, sinks := pkg.reachesWriteSink("registerLifecycleResource"); !isWrite {
		t.Errorf("registerLifecycleResource reaches no sink (%v) — the assertion above would then "+
			"hold because the call graph lost the edge, not because the edge rule is right", sinks)
	}
}

// TestEveryToolRegistrationYieldsATool is the property the TypeScript guard got
// in PR 821, stated on the Go side: every registration in this package must yield
// a tool with a usable name.
//
// WHY A PROPERTY AND NOT A COUNT. Every parser is blind to SOME source shape, and
// the cost of that blindness is not the missing name — it is that the missing
// tool leaves the view of every guard built on the parser (the scope-gate sweep,
// the chat exposure sweep) while the whole suite stays green. A pinned count
// would catch only the shape that changes the number, and it rots the moment any
// unrelated tool lands (see t_585ab08b on the TS count pin). This asserts the
// cause instead: no registration may resolve to nothing, and a shape the parser
// cannot read fails HERE, by file:line and by the argument it did not understand.
//
// The one call site that legitimately resolves to nothing is the registration
// PRIMITIVE's own forwarding (addTool calling mcp.AddTool with its `t *mcp.Tool`
// parameter); it is excluded structurally, in collectTools, not by an allowlist
// entry naming a file and a line.
func TestEveryToolRegistrationYieldsATool(t *testing.T) {
	pkg := parsePackage(t)
	if len(pkg.regs) < 100 {
		t.Fatalf("found only %d registration call site(s) — this property would pass vacuously", len(pkg.regs))
	}

	var unresolved []string
	resolved := 0
	for _, s := range pkg.regs {
		if s.Tool == "" {
			unresolved = append(unresolved, fmt.Sprintf("%s:%d — %s", s.File, s.Line, s.Reason))
			continue
		}
		resolved++
	}
	if len(unresolved) > 0 {
		sort.Strings(unresolved)
		t.Errorf("%d registration call site(s) did not yield a tool name:\n  %s\n\n"+
			"The tool each one registers is INVISIBLE to every Go-side guard in this package "+
			"(the scope-gate sweep, the chat exposure sweeps), which compare against floors and "+
			"ratios and so stay green while the registry they measure shrinks. Teach the parser "+
			"the shape — do NOT exempt the tool: an allowlist entry would excuse exactly the "+
			"blindness this test exists to name.", len(unresolved), strings.Join(unresolved, "\n  "))
	}
	if resolved != len(pkg.tools) {
		t.Errorf("%d registration(s) resolved to a name but the tool set holds %d — every resolved "+
			"registration must contribute exactly one tool", resolved, len(pkg.tools))
	}

	seen := map[string]string{}
	for _, tool := range pkg.tools {
		if tool.Handler == "" {
			t.Errorf("tool %q resolved with no handler function — the guards cannot follow it", tool.Name)
			continue
		}
		// The handler must be a function THIS package declares, or every
		// write-sink and scope-gate question about the tool gets answered from an
		// empty call graph: a misnamed handler reads as "reaches nothing", which
		// is exactly how a write tool would pass the scope sweep vacuously.
		if _, ok := pkg.decls[tool.Handler]; !ok {
			t.Errorf("tool %q registers handler %q, which is not a function declared in this "+
				"package — the call graph cannot follow it, so the scope-gate sweep answers "+
				"\"reaches nothing\" for this tool and passes vacuously", tool.Name, tool.Handler)
		}
		if prev, dup := seen[tool.Name]; dup {
			t.Errorf("tool %q is registered twice (by %s and %s)", tool.Name, prev, pkg.origin[tool.Handler])
		}
		seen[tool.Name] = pkg.origin[tool.Handler]
	}
	t.Logf("%d registration call sites, %d resolved to a named tool (%d distinct names)",
		len(pkg.regs), resolved, len(seen))
}

// TestParsedRegistryMatchesTheWire pins the parser against the AUTHORITY: the set
// of tools registerTools actually serves over the MCP protocol.
//
// The source parse is a convenience — it gives the guards a call graph and a
// description without a database — but a convenience that disagrees with what the
// server serves is worse than no parse at all, because every conclusion drawn
// from it is then a conclusion about a registry that does not exist. This is the
// assertion that would have caught the `disable2FA` constructor gap on the day it
// landed: the parse said 176, the wire said 177, and the difference was a
// registered, write-capable tool no guard could see (card t_5d89bbdc).
func TestParsedRegistryMatchesTheWire(t *testing.T) {
	pkg := parsePackage(t)
	wire := registeredToolNames(t)

	parsed := map[string]bool{}
	for _, tool := range pkg.tools {
		parsed[tool.Name] = true
	}

	var missing, extra []string
	for name := range wire {
		if !parsed[name] {
			missing = append(missing, name)
		}
	}
	for name := range parsed {
		if !wire[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) > 0 {
		t.Errorf("the source parser does not see %d tool(s) the server SERVES (%d parsed vs %d on the "+
			"wire): %v\nThey are registered, so the tools/list a client receives offers them, yet "+
			"every guard built on this parser treats them as if they did not exist — a write tool "+
			"here is never checked for the write-scope gate. Teach the parser the registration "+
			"shape; do not excuse the tool.", len(missing), len(parsed), len(wire), missing)
	}
	if len(extra) > 0 {
		t.Errorf("the source parser sees %d tool(s) the server does NOT serve: %v\nA registration "+
			"reachable only from dead code, or a shape parsed twice, would put a phantom tool in "+
			"front of every guard.", len(extra), extra)
	}
	t.Logf("parser and wire agree: %d tools", len(wire))
}
