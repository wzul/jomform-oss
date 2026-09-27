package totpconsume

// Issue #528 / the mirrored-rule defect: this file is the guard that makes TOTP
// single-use DRIFT RED without a human remembering to edit two places.
//
// WHY A SOURCE GUARD AND NOT A COMMENT
//
// The rule needs auth (validate a code, learn which time-step matched) and
// dbstore (consume that step atomically), and those two packages may not import
// each other. So it started as one copy in internal/httpapi and, when the MCP
// surface needed it (PR 686), as a SECOND hand-rolled copy in internal/mcpsrv —
// with a doc comment asking the next reader to please keep the two in step.
//
// That is not a guard. On the 2FA-disable path, drift between the copies means a
// code seen once can strip a second factor twice. The rule now lives once, here,
// and both surfaces CALL it. What can still go wrong is a future change that
// hand-rolls a third copy in one surface — so this test parses the source and
// fails the moment any package outside this one consumes a TOTP step of its own
// accord.
//
// WHAT THIS GUARD PROVES, AND WHAT IT DOES NOT
//
// Proves: the production tree contains exactly one implementation of
// "validate a code against the user's own secret, then consume the matched
// step", and both the REST and MCP surfaces route through it.
//
// Does NOT prove: that the rule's behaviour is correct — that is
// consume_test.go's job (a real Postgres, plus a stub that fails). The two files
// are deliberately complementary: this one catches a second COPY, the behavioural
// one catches the single copy being neutered. A mutation that deletes the
// ConsumeTOTPStep call from the shared rule compiles here and goes RED in
// consume_test.go, without the mutator touching any other package.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// ruleImplementationDirs documents where the rule is allowed to live, for a
// reader of this guard. It is not consulted at runtime — the checks below are
// exact (file + function), which a directory listing could not express.
//
//   - internal/totpconsume: the shared rule itself — the only place it may be
//     written.
//   - internal/httpapi:     ONE deliberate exception, asserted by name below.
//     The /v1/auth/2fa/enable handler consumes a step for the PENDING secret
//     (the secret being confirmed, which is not yet on the user row) and answers
//     a database failure with a 500 rather than a 401 code rejection. It is a
//     different flow with a different input and a different failure contract,
//     not a copy of this rule; folding it in would change behaviour (#528's
//     contract is verified and must stay as-is).
var _ = [...]string{
	"internal/totpconsume: the shared rule (this package)",
	"internal/httpapi: handle2FAEnable only (pending-secret flow, asserted by name)",
}

// allowedDirectConsumers maps a source file (repo-relative, slash-separated from
// api/) to why it is allowed to CALL ConsumeTOTPStep outside the shared rule.
//
// dbstore is deliberately absent: internal/dbstore/user.go DEFINES the method,
// it never calls it, and this guard walks call expressions — so an entry for it
// would be stale by construction and is rejected by the staleness check below.
var allowedDirectConsumers = map[string]string{
	"api/internal/totpconsume/consume.go": "the shared rule — the one implementation, " +
		"which both surfaces call",
	"api/internal/httpapi/auth.go": "handle2FAEnable consumes the step of the PENDING " +
		"secret (not the user's own) and maps a DB error to 500 rather than a 401 " +
		"rejection — a different flow, deliberately not folded into the shared rule",
}

// TestOnlyTheSharedRuleConsumesATOTPStep fails when a production file outside the
// shared rule consumes a TOTP time-step. That is the drift this card exists to
// prevent: a hand-rolled copy in one surface, invisible to the other surface's
// tests.
//
// BITE: add `users.ConsumeTOTPStep(ctx, u.ID, step)` to any handler in
// internal/httpapi or internal/mcpsrv (the pre-fix state of PR 686's copy) and
// this test names that file and fails.
func TestOnlyTheSharedRuleConsumesATOTPStep(t *testing.T) {
	root := apiRoot(t)

	type site struct {
		file string // repo-relative from api/
		fn   string
	}
	var found []site

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil // tests may drive the store directly; production code may not
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)

		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, src, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok {
				return true
			}
			ast.Inspect(fn, func(inner ast.Node) bool {
				call, ok := inner.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel == nil || sel.Sel.Name != "ConsumeTOTPStep" {
					return true
				}
				found = append(found, site{file: "api/" + rel, fn: fn.Name.Name})
				return false
			})
			return false
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk api tree: %v", err)
	}

	if len(found) == 0 {
		t.Fatal("no ConsumeTOTPStep call site found anywhere in the api tree — the store method " +
			"was renamed or removed and this guard is now vacuous; fix the guard, do not delete it")
	}

	seen := map[string]bool{}
	for _, s := range found {
		seen[s.file] = true
		if _, ok := allowedDirectConsumers[s.file]; ok {
			continue
		}
		t.Errorf("%s (%s) consumes a TOTP time-step directly.\n"+
			"The single-use rule is implemented ONCE, in internal/totpconsume; this file is a "+
			"SECOND copy of it (the exact drift this guard exists to stop). Call "+
			"totpconsume.ConsumeCode instead, or — if this really is a different flow — add the "+
			"file to allowedDirectConsumers with the reason, in this test.", s.file, s.fn)
	}

	// A stale allowlist is its own drift: an entry for a file that no longer
	// consumes a step would silently permit a future copy to reappear there.
	for file, why := range allowedDirectConsumers {
		if !seen[file] {
			t.Errorf("allowedDirectConsumers lists %s (%s) but that file no longer calls "+
				"ConsumeTOTPStep — remove the stale entry so the allowlist cannot hide a future copy",
				file, why)
		}
	}
}

// TestBothSurfacesCallTheSharedRule fails when either API surface stops routing
// its TOTP proof through the shared rule. A surface that quietly drops the call
// is the same defect as a copy: its 2FA path loses single-use while the other
// surface's tests stay green.
//
// TWO shapes are checked, because there are two ways to lose the check and only
// checking one of them is a guard that reads green while the surface is dead:
//
//  1. the surface no longer references the shared rule at all (the copy came
//     back, or the alias was deleted);
//  2. the surface still contains the call but no longer MAKES it from its
//     handlers — a dead alias. `totpconsume.ConsumeCode` written in a function
//     nobody calls proves nothing, and the surface's own behavioural tests may
//     still pass if they exercise a different path.
//
// BITE: M3/M4 (re-copied rule) fail shape 1; dropping the handler's call while
// leaving the alias behind fails shape 2.
func TestBothSurfacesCallTheSharedRule(t *testing.T) {
	root := apiRoot(t)

	surfaces := map[string]string{
		"internal/httpapi": "the REST surface (login 2FA, 2FA enable, magic-session step-up)",
		"internal/mcpsrv":  "the MCP surface (disable_2fa)",
	}

	for dir, what := range surfaces {
		files, err := filepath.Glob(filepath.Join(root, dir, "*.go"))
		if err != nil {
			t.Fatalf("glob %s: %v", dir, err)
		}

		mentionsShared := false
		var invokers []string

		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			src, rerr := os.ReadFile(path)
			if rerr != nil {
				t.Fatalf("read %s: %v", path, rerr)
			}
			if strings.Contains(string(src), "totpconsume.ConsumeCode(") {
				mentionsShared = true
			}
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, path, src, 0)
			if perr != nil {
				t.Fatalf("parse %s: %v", path, perr)
			}
			base := filepath.Base(path)
			ast.Inspect(f, func(n ast.Node) bool {
				fn, ok := n.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					return true
				}
				// Calls made from inside the alias ARE the delegation, not an
				// invocation of it — counting them would let a dead alias pass.
				if fn.Name.Name == "checkAndConsumeTOTP" {
					return true
				}
				ast.Inspect(fn, func(inner ast.Node) bool {
					call, ok := inner.(*ast.CallExpr)
					if !ok {
						return true
					}
					if isRuleCall(call) {
						invokers = append(invokers, base+" ("+fn.Name.Name+")")
					}
					return true
				})
				return false
			})
		}

		if !mentionsShared {
			t.Errorf("%s does not reference totpconsume anywhere in production code.\n"+
				"%s must route its TOTP proof through the shared rule — otherwise this surface "+
				"has silently lost single-use while the other surface's tests stay green.",
				dir, what)
		}
		if len(invokers) == 0 {
			t.Errorf("%s never INVOKES the shared rule from a handler — the only reference left is "+
				"the alias's own delegation.\n"+
				"(%s) A surface whose alias is dead code reads as 'calls the shared rule' to a text "+
				"search while refusing nothing. Restore the call from the handler, or delete the "+
				"alias so the breakage is visible.", dir, what)
			continue
		}
		sort.Strings(invokers)
		t.Logf("%s invokes the shared rule from: %s", dir, strings.Join(invokers, ", "))
	}
}

// isRuleCall reports whether a call expression is an invocation of the TOTP
// single-use rule — either through a surface's local alias
// (checkAndConsumeTOTP) or directly (totpconsume.ConsumeCode).
func isRuleCall(call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name == "checkAndConsumeTOTP"
	case *ast.SelectorExpr:
		if fn.Sel == nil {
			return false
		}
		if fn.Sel.Name == "ConsumeCode" {
			return true
		}
		return fn.Sel.Name == "checkAndConsumeTOTP"
	}
	return false
}

// TestSharedRuleHasExactlyOneDefinition fails when the rule's entry point is
// defined in more than one package — the state this card was filed against
// (httpapi.checkAndConsumeTOTP and mcpsrv.checkAndConsumeTOTP were two full
// copies).
//
// The local aliases in each surface are thin (one delegating call), so they are
// not a second definition; they are counted here by requiring that any function
// named checkAndConsumeTOTP is a single-statement delegation.
//
// BITE: paste the rule's body back into either surface's alias and this test
// fails on that file.
func TestSharedRuleHasExactlyOneDefinition(t *testing.T) {
	root := apiRoot(t)

	var impls []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		// The rule's implementation is recognised by the call it must make: the
		// atomic step consumption. Only the shared package may contain it inside
		// a function that is not a pure delegation.
		if !strings.Contains(string(src), "ConsumeTOTPStep(") && !strings.Contains(string(src), "totpconsume.ConsumeCode(") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)

		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, src, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			if isThinDelegation(fn) {
				return true
			}
			// A non-delegating function that consumes a step IS an implementation.
			if fnConsumesStep(fn) {
				impls = append(impls, "api/"+rel+" ("+fn.Name.Name+")")
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk api tree: %v", err)
	}
	sort.Strings(impls)

	if len(impls) == 0 {
		t.Fatal("found no function that consumes a TOTP step — this guard is vacuous, fix it")
	}
	// Exactly one implementation is expected. The exception in httpapi/auth.go
	// (handle2FAEnable, the pending-secret flow) is a KNOWN, allowed second
	// consumer and is asserted by name so it cannot grow.
	want := []string{
		"api/internal/httpapi/auth.go (handle2FAEnable)",
		"api/internal/totpconsume/consume.go (ConsumeCode)",
	}
	for _, got := range impls {
		if !contains(want, got) {
			t.Errorf("unexpected TOTP step consumer %q — the single-use rule must be implemented "+
				"ONCE (internal/totpconsume). If this is genuinely a different flow, add it to the "+
				"expected list here AND to allowedDirectConsumers, with its reason.", got)
		}
	}
	for _, w := range want {
		if !contains(impls, w) {
			t.Errorf("expected TOTP step consumer %q is missing — either the rule moved (update the "+
				"guard, do not weaken it) or a surface stopped consuming its step", w)
		}
	}
}

// fnConsumesStep reports whether a function body calls anything named
// ConsumeTOTPStep.
func fnConsumesStep(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel != nil && sel.Sel.Name == "ConsumeTOTPStep" {
			found = true
			return false
		}
		return true
	})
	return found
}

// isThinDelegation reports whether a function's body is a single return of a
// call — i.e. an alias that delegates the rule rather than implementing it.
func isThinDelegation(fn *ast.FuncDecl) bool {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return false
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}
	_, ok = ret.Results[0].(*ast.CallExpr)
	return ok
}

// apiRoot locates the api/ directory from this test file's own location, so the
// guard works from any worktree and from any working directory.
func apiRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate the api tree")
	}
	// .../api/internal/totpconsume/only_one_copy_test.go -> .../api
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("api root %s does not contain go.mod: %v", root, err)
	}
	return root
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
