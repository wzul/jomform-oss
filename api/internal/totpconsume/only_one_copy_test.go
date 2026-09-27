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
//
// A SECOND IMPLEMENTATION IS NOT A SECOND CALL BY THAT NAME
//
// The three checks above key on the NAME ConsumeTOTPStep, so two shapes of a
// second implementation read GREEN here (both were reproduced before this
// section existed):
//
//	A. a function that runs the UPDATE itself instead of calling the store
//	   accessor — it never mentions ConsumeTOTPStep, so every check above passed
//	   it. This is the drift that would silently reopen TOTP replay on the
//	   2FA-disable path, and it is the shape a future author reaches for when the
//	   accessor's signature does not quite fit.
//	B. a delegating wrapper in a NEW package — its body is a single return of a
//	   call, so TestSharedRuleHasExactlyOneDefinition skipped it as a thin alias,
//	   and TestBothSurfacesCallTheSharedRule only ever looks inside httpapi and
//	   mcpsrv.
//
// Both are asserted directly below: TestOnlyTheStoreWritesTheTOTPStepColumn
// catches (A) by SQL, and TestOnlyKnownPackagesCallTheSharedRule catches (B) by
// pinning the full set of packages that may call the rule, so a new package is a
// deliberate, reviewed addition rather than a silent pass.
//
// Three spellings escaped the first version of those two checks, and all three
// were reproduced before this section was widened:
//
//	C. the statement is not a lone literal in a function body — it is a
//	   package-level const (`const totpStepUpdateSQL = "UPDATE users SET
//	   totp_last_step = ..."`), or it is split across literals, or it is built by
//	   fmt.Sprintf from the column name. The first version matched the regex
//	   against ONE literal at a time and only inside an *ast.FuncDecl, so a
//	   GenDecl value spec was never visited and no single piece of a split
//	   statement carried both halves of the signature.
//	D. the caller check only recorded an *ast.SelectorExpr whose X was the
//	   package alias, so binding the rule to a name first
//	   (`var consume = totpconsume.ConsumeCode`) and calling the NAME was
//	   invisible — a different spelling of the same dodge as the dot-import it
//	   already refuses.
//
// None of these is an invented evasion: dbstore/form.go declares
// `const setStatusSQL` and builds it by literal concatenation, and
// dbstore/media_migrate.go builds UPDATEs with fmt.Sprintf. An author copying
// the accessor's statement into a const is copying the house style.
//
// Covered now by matching the WHOLE expression an author wrote (not one literal
// at a time), by visiting every string literal in the file including a
// package-level declaration, and by counting a call through a name the rule was
// BOUND to.
//
// INDIRECTION THROUGH A CALL OR A LOCAL BINDING (E)
//
// The same walk used to stop at any *ast.CallExpr that was not a Sprintf, and at
// an identifier bound by `:=`. So a statement an author assembled through a
// helper — `col := colFn()` then Sprintf, `"…" + colFn() + "…"`, or the whole
// statement returned by `stmtFn()` and passed straight to Exec — left no
// resolvable literal anywhere the walk looked, and the column check read GREEN
// for a full second implementation:
//
//	q := "UPDATE users SET " + colFn() + " = $2 … AND " + colFn() + " < $2"
//	q := fmt.Sprintf("UPDATE users SET %s = $2 …", colFn(), colFn())
//	col := colFn(); q := fmt.Sprintf("… %s …", col)
//	pool.Exec(ctx, stmtFn(), u.ID, step)     // literal is stmtFn's own return
//
// All four were reproduced on a1cf703 (`go build ./...` RC=0, guard green) and
// bite now. The first three are closed by treating a call to a package-level
// function of THIS package as a string piece resolved to that function's return
// expression (E1), and by resolving a name bound by a preceding `:=`/`=` in the
// file (E3); the fourth is caught by the SAME standalone-literal walk as C, since
// the helper's own `return` literal is a literal in the tree — do not read it as
// a new bite.
//
// ONE HOP, AND THE BOUND IS DELIBERATE. A callee is resolved only when it is an
// Ident naming a package-level function of the same package whose body is a
// single `return <resolvable expression>`; a Selector callee (`pkg.Fn()`) is left
// opaque, because resolving that name against this package's function table would
// attribute the wrong body to the call. A helper that assembles its statement
// across statements (`strings.Builder`, a loop) is therefore still GREEN — that
// is a known boundary, not an oversight: there is no single expression to quote,
// and guessing one there is worse than leaving it. Widening past one hop would
// need real data-flow analysis.
//
// ONE KEY PER WRITE SITE, AND WHY THE KEY IS SHAPED THE WAY IT IS (G)
//
// The allowlist above is keyed by "repo-relative file (site)", and two shapes
// made that key ambiguous. Both were reproduced on the previous cut of this
// guard (`go build ./...` RC=0, guard green):
//
//	G1. a PACKAGE-LEVEL const named exactly like the accessor —
//	    `const ConsumeTOTPStep = "UPDATE users SET totp_last_step = ..."` — is a
//	    different Go namespace from the METHOD of that name, so it compiles, and
//	    it produced a key BYTE-IDENTICAL to the allowlisted entry written for the
//	    method. A full second implementation of the rule read GREEN. The same
//	    collapse hid a genuinely STALE entry: with the accessor neutered so it no
//	    longer writes the column, the const's own write satisfied the staleness
//	    check on the method's behalf, and neither half of the pair was reported.
//	G2. a function-LOCAL const gave a FALSE RED. Moving the accessor's own
//	    statement into `const stepSQL = "..."` inside its body reported the same
//	    statement twice — once as the function, once as `(stepSQL)`, which is not
//	    allowlisted — and then failed for there being no production write site at
//	    all. The emitted advice is to remove the allowlist entry, i.e. to delete
//	    the only protection there is, for a change that renamed no behaviour.
//
// Both are closed in enclosingDecl, by key: a declaration INSIDE a function body
// is keyed by that function (G2), and a declaration outside any function is
// keyed `decl <name>` (G1) — a shape no function key can equal, since an
// identifier cannot contain a space. This is a different axis from E's
// function-scoped local bindings and does not replace them.
//
// SCOPE OF A NAME BOUND BY `:=` (F)
//
// E3 resolved a name bound earlier by an assignment, but the map holding those
// bindings was keyed by NAME ONLY and filled from the whole *ast.File, and an
// Ident was resolved against it BEFORE the package-wide const map. One unrelated
// `x := "…"` anywhere earlier in a file therefore overrode the package declaration
// of `x` for every later statement in that file — which both MISSED a genuine
// second implementation (its column const shadowed by an unrelated same-file
// local read GREEN) and named legitimate non-TOTP code as a TOTP write. The
// earlier comment here claimed the map "cannot fail towards a false alarm"; it
// did, in both directions.
//
// Fixed by scoping rather than by trusting the map less:
//
//   - a binding is collected only inside the function that ENCLOSES the
//     statement being resolved (a package-level declaration has no local scope
//     at all), so a local in one function can never reach another. A name that
//     shadows a package declaration in a SIBLING function is not in scope, which
//     is exactly Go's rule and exactly what the false alarm was;
//   - a statement is reported if it reads as a TOTP write under EITHER reading of
//     a name that could be resolved two ways — the in-scope local binding, or the
//     package-level declaration. Inside a function a local genuinely shadows a
//     package const, so neither reading may be silently dropped: dropping the
//     const reading misses a copy whose column came from a shadowed name, and
//     dropping the local reading misses one built from a helper
//     (`col := colFn()`) whenever the package happens to declare a `col`. An
//     ambiguous name is therefore reported loudly, never resolved silently.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// sharedRuleImport is the import path of the shared rule. It is matched exactly
// (not by suffix) so a same-named package elsewhere in the module graph cannot
// slip past the caller check below.
const sharedRuleImport = "github.com/wzul/jomform/api/internal/totpconsume"

// sqlWritesTOTPStep matches a WRITE to the users.totp_last_step column inside a
// SQL string literal. Requiring the UPDATE (rather than a bare
// `totp_last_step =`) keeps a SELECT that merely compares the column from
// reading as a write, and `\s*` around the `=` means formatting cannot hide one:
// `totp_last_step=$2`, `totp_last_step  =  $2` and the accessor's own
// `totp_last_step = $2` all match.
var sqlWritesTOTPStep = regexp.MustCompile(`(?is)\bupdate\b.*?\btotp_last_step\s*=`)

// allowedStepWrites maps a WRITE SITE ("repo-relative file (function)",
// slash-separated from api/) to why it is allowed to write users.totp_last_step
// itself.
//
// dbstore/user.go (ConsumeTOTPStep) is the accessor that DEFINES the write — the
// predicate lives inside its UPDATE, which is what makes a code single-use rather
// than single-use-per-window. The shared rule reaches it by CALLING
// ConsumeTOTPStep, never by writing the column itself, so it is deliberately NOT
// listed here: an entry for it would be stale by construction (dbstore never
// calls the rule) and is rejected by the staleness check below.
//
// Keyed by function, not just by file: a SECOND write function added beside the
// accessor in the same file is a second implementation of the rule just as much
// as one in another package, so the allowlist must not cover it by accident.
//
// A statement declared at PACKAGE level (a const or var outside any function) is
// keyed as `... (decl <name>)` — e.g. `... (decl totpStepUpdateSQL)` — because
// there is no function to name, and a bare name is NOT safe to use here: a
// package-level const and a method may share a name in Go (different
// namespaces), so `file (ConsumeTOTPStep)` would key a const and the allowlisted
// accessor METHOD identically, letting the const's statement inherit the
// method's permission. A local const/var inside a function body belongs to that
// function and is keyed by the FUNCTION, not by its own name — a function-local
// declaration is part of the function's statement, and keying it separately both
// reported that statement twice and turned a legitimate
// `const stepSQL = "…"` inside the accessor into an unlisted write site plus a
// bogus "no production file writes …" failure.
//
// The list is checked in BOTH directions: an unlisted write site fails, and a
// listed one that no longer writes the column fails, so it cannot rot into a
// blanket permission for a future copy.
var allowedStepWrites = map[string]string{
	"api/internal/dbstore/user.go (ConsumeTOTPStep)": "the store accessor — defines " +
		"the atomic UPDATE whose WHERE clause only ever moves totp_last_step forward",
}

// ruleCallerPackages pins the packages whose PRODUCTION code may call the shared
// rule's entry point (totpconsume.ConsumeCode).
//
// Two surfaces call it today, and each needs its own behavioural coverage of the
// single-use property. A third package appearing here unannounced means a new
// surface is enforcing — or quietly not enforcing — the rule, which is exactly
// the review this list forces. Adding one is a one-line, reviewed change here;
// the point is that it cannot happen silently.
//
// internal/totpconsume is deliberately absent: it OWNS the rule, so a call
// inside it is not a new surface, and a second DEFINITION there is caught by
// TestSharedRuleHasExactlyOneDefinition instead.
var ruleCallerPackages = map[string]string{
	"internal/httpapi": "the REST surface — login 2FA, 2FA enable, magic-session step-up",
	"internal/mcpsrv":  "the MCP surface — disable_2fa",
}

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

// TestOnlyTheStoreWritesTheTOTPStepColumn fails when a production file outside
// the store writes users.totp_last_step itself — the INLINED shape of a second
// implementation.
//
// This is the check the name-based ones cannot make. A function that does
// `pool.Exec(ctx, "UPDATE users SET totp_last_step = $2 ...")` never mentions
// ConsumeTOTPStep, so before this test existed it passed every check in this
// file while being a full second copy of the rule — the exact drift that would
// silently reopen TOTP replay on the 2FA-disable path.
//
// It is deliberately SQL-level rather than call-level: the rule IS the UPDATE
// (the WHERE clause is what makes a code single-use), so copying the statement
// is copying the rule, whatever the function is called.
//
// BITE: add to internal/httpapi a function that runs the UPDATE itself instead
// of calling the store accessor, and this test names that file and function.
//
// The check matches the WHOLE SQL an author wrote, not one literal at a time,
// and it visits package-level declarations as well as function bodies — a const
// at the top of a file is where this repo keeps its statements (see
// dbstore/form.go's setStatusSQL). Concretely, for every string-literal
// expression in a production file it builds the statement the expression
// denotes: string concatenation and fmt.Sprintf are resolved, `%s`-style verbs
// substituted from their arguments, and a const/var the expression names is
// expanded to its own initialiser. A literal nested inside an already-covered
// expression is not also reported, so a split statement names its write site
// once.
//
// BITE (declaration placement): move the statement into a package-level const
// and exec it from a function — the const's own site is named, plus the function
// that uses it.
//
// BITE (split / formatted): `"UPDATE users SET " + column + " = $2 ..."`, or
// fmt.Sprintf("UPDATE users SET %s = $2 ...", column) — named at the statement's
// site.
func TestOnlyTheStoreWritesTheTOTPStepColumn(t *testing.T) {
	root := apiRoot(t)

	// seen maps an allowlisted write site ("file (function)", or
	// "file (decl <name>)" for a declaration outside any function) to itself, so
	// a stale entry (or one that silently stopped matching) is visible by name.
	seen := map[string]bool{}
	constsByDir := map[string]map[string]string{}
	funcsByDir := map[string]map[string]string{}

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
		rel = "api/" + filepath.ToSlash(rel)

		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, src, 0)
		if perr != nil {
			return perr
		}

		consts := packageConsts(filepath.Dir(path), constsByDir)
		funcs := packageStringFuncs(filepath.Dir(path), funcsByDir, constsByDir)
		for _, e := range sqlExpressions(f, funcs) {
			if !statementWritesTOTPStep(f, consts, funcs, e) {
				continue
			}
			site := rel + " (" + e.site + ")"
			if _, allowed := allowedStepWrites[site]; allowed {
				seen[site] = true
			} else {
				t.Errorf("%s (%s) writes users.totp_last_step in a SQL statement.\n"+
					"The single-use rule is implemented ONCE — the UPDATE's WHERE clause IS the "+
					"rule, so an inlined copy is a second implementation and drifts silently "+
					"from the shared one. Call totpconsume.ConsumeCode (or the store accessor "+
					"users.ConsumeTOTPStep) instead, or — if this really is a different flow — "+
					"add the write site to allowedStepWrites with the reason, in this test.",
					rel, e.site)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk api tree: %v", err)
	}

	if len(seen) == 0 {
		t.Fatal("no production file writes users.totp_last_step — either the column was renamed or " +
			"the statement was replaced, and this guard is now vacuous; fix the guard, do not delete it")
	}
	// A stale allowlist entry would silently permit a copy to reappear there.
	for site, why := range allowedStepWrites {
		if !seen[site] {
			t.Errorf("allowedStepWrites lists %s (%s) but that write site no longer writes "+
				"users.totp_last_step — remove the stale entry so the allowlist cannot hide a "+
				"future inlined copy", site, why)
		}
	}
}

// statementWritesTOTPStep decides whether ONE expression an author wrote is a
// write to users.totp_last_step. It is the single place that rule lives, so the
// tree-wide check and the in-memory scope test below cannot drift apart — a
// duplicated rule would let a mutation pass both.
//
// TWO READINGS, AND EITHER MAY CONVICT. A name can mean two things at once:
// what the enclosing function bound it to (which shadows a package-level
// declaration — Go's own rule) and what the package declares. Both are real
// answers, so the statement counts as a TOTP write if EITHER says so:
//
//   - dropping the local reading misses a copy built from a helper
//     (`col := colFn()`) whenever the package happens to declare a `col`;
//   - dropping the package reading misses a copy whose column came from a
//     package const that some unrelated function shadows — the exact MISS this
//     rule was fixed for.
//
// An ambiguous name is therefore reported, never resolved silently. The map is
// scoped by enclosingFuncDecl so a binding in one function cannot reach a
// statement in another; without that scope the package reading would be
// overridden by an unrelated sibling and the copy would read GREEN.
func statementWritesTOTPStep(f *ast.File, consts, funcs map[string]string, e sqlExpr) bool {
	local := localStringBindings(enclosingFuncDecl(f, e.node), e.node.Pos(), consts, funcs)
	return sqlWritesTOTPStep.MatchString(resolveSQLText(e.node, consts, funcs, local)) ||
		sqlWritesTOTPStep.MatchString(resolveSQLText(e.node, consts, funcs, nil))
}

// TestLocalBindingsAreScopedToTheirFunction pins the SCOPE rule the check above
// depends on, independent of the tree it runs over: a name bound by an assignment
// inside one function must not resolve a statement in another function, and must
// not turn legitimate non-TOTP code into a TOTP write.
//
// WHY THIS EXISTS SEPARATELY. The check above found this defect only because the
// api tree happened to contain the right shape at the right time. The scope rule
// is a property of resolveSQLText/localStringBindings, and it has already been
// got wrong once: the first version was handed the whole *ast.File and keyed by
// name only, so `x := "…"` in ANY earlier statement overrode the package
// declaration of x for every later statement in that file. That both MISSED a
// genuine second implementation (its column const shadowed by a sibling function
// read GREEN) and named an innocent `UPDATE products SET name = …` as a TOTP
// write. The comment next to that map claimed it "cannot fail towards a false
// alarm"; it did, in both directions, and no test noticed.
//
// It goes through statementWritesTOTPStep — the same function the tree-wide check
// uses — so a change to the rule is exercised here too. The sources are parsed
// in-memory, so this runs on any tree, including a future one where the api tree
// no longer contains the trigger shape.
//
// BITE (precedence): drop the package reading from statementWritesTOTPStep and
// the sibling-shadow case fails. BITE (scope): hand localStringBindings the whole
// file and both the sibling-shadow and false-alarm cases fail. BITE (local
// reading): drop the local reading and the helper-assembled case fails.
func TestLocalBindingsAreScopedToTheirFunction(t *testing.T) {
	const preamble = `package probe

func colFn() string { return "totp_last_step" }
`

	cases := []struct {
		name  string
		src   string
		write bool // does the final statement write users.totp_last_step?
	}{
		{
			// The defect: the column const is shadowed by an UNRELATED function.
			// Read as the sibling bound it the statement is "name"; read against
			// the package declaration it is "totp_last_step". Inside the copy's
			// own function the const is NOT shadowed, so the package reading is
			// the correct one and the copy must be caught.
			name: "sibling function binds the same name",
			src: preamble + `
const col = "totp_last_step"

func unrelated() string { col := "name"; return col }

func copy() string { return "UPDATE users SET " + col + " = $2 WHERE col < $2" }
`,
			write: true,
		},
		{
			// A function-local binding in the copy's OWN function does shadow the
			// package declaration, so this one really is a products write: the
			// false alarm the file-scoped map raised.
			name: "unrelated function only, products column",
			src: `package probe

const col = "name"

func unrelated() string { col := "totp_last_step"; return col }

func innocent() string { return "UPDATE products SET " + col + " = $2 WHERE id = $1" }
`,
			write: false,
		},
		{
			// The binding is in scope: the copy is assembled from a helper through
			// a local name, which is the reach this map exists for (R3).
			name: "local binding built from a helper",
			src: preamble + `
func copy() string { col := colFn(); return "UPDATE users SET " + col + " = $2 WHERE col < $2" }
`,
			write: true,
		},
		{
			// Both readings must stay live: here the copy's own function shadows
			// the name, but the package const is the column, so dropping the
			// package reading would hide it.
			name: "same function shadows a column const",
			src: preamble + `
const col = "totp_last_step"

func copy() string { col := "surname"; return "UPDATE users SET " + col + " = $2 WHERE col < $2" }
`,
			write: true,
		},
		{
			// The binding must not reach ACROSS functions either: two functions
			// binding the same name, with the column only in the first.
			name: "a binding does not reach into a sibling function",
			src: preamble + `
func one() string { col := "totp_last_step"; return col }

func two() string { col := "name"; return "UPDATE products SET " + col + " = $2 WHERE id = $1" }
`,
			write: false,
		},
		{
			// THE CASE THAT SEPARATES "either reading" FROM "consts first, local
			// as fallback". The package declares a `col` that is NOT the column,
			// and the copy builds its column from a helper into a LOCAL `col`.
			// The local binding is in scope and IS the column, so this is a
			// genuine copy — but a rule that lets the package declaration win
			// whenever the name exists never consults the binding and reads it as
			// the products column. Dropping the local reading is not a safe
			// simplification: it reopens R3's reach.
			name: "package declares the name, the local binding is the column",
			src: preamble + `
const col = "name"

func copy() string { col := colFn(); return "UPDATE users SET " + col + " = $2 WHERE col < $2" }
`,
			write: true,
		},
		{
			// The negative control: nothing anywhere names the column.
			name: "no column name at all",
			src: `package probe

func innocent() string { q := "name"; return "UPDATE products SET " + q + " = $2 WHERE id = $1" }
`,
			write: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "probe.go", tc.src, 0)
			if err != nil {
				t.Fatalf("parse probe source: %v", err)
			}
			consts := collectStringConsts(f)
			funcs := map[string]string{}
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil {
					continue
				}
				if ret := singleReturn(fn); ret != nil {
					if s := resolveSQLText(ret, consts, nil, nil); s != "" {
						funcs[fn.Name.Name] = s
					}
				}
			}

			got := false
			for _, e := range sqlExpressions(f, funcs) {
				if statementWritesTOTPStep(f, consts, funcs, e) {
					got = true
				}
			}
			if got != tc.write {
				t.Errorf("resolved as a TOTP write = %v, want %v — a name bound in one function "+
					"must not decide a statement in another, and a name that resolves two ways "+
					"must be reported rather than silently dropped", got, tc.write)
			}
		})
	}
}

// TestOnlyKnownPackagesCallTheSharedRule pins the set of packages whose
// production code may call the rule's entry point, totpconsume.ConsumeCode.
//
// TestSharedRuleHasExactlyOneDefinition cannot replace this: a delegating
// wrapper in a NEW package has a single-return body, so it is skipped as a thin
// alias rather than reported as a second definition, and
// TestBothSurfacesCallTheSharedRule only ever looks inside httpapi and mcpsrv.
// Before this test existed, a wrapper in, say, internal/qa_mirror passed
// everything — a new surface enforcing the rule with none of the review, and a
// new place for the single-use property to be quietly lost.
//
// The list is checked in BOTH directions, so it cannot rot: an unlisted package
// fails, and a listed package that no longer calls the rule fails.
//
// BITE: add `return totpconsume.ConsumeCode(ctx, users, u, code)` to a function
// in any new package and this test names that package.
func TestOnlyKnownPackagesCallTheSharedRule(t *testing.T) {
	root := apiRoot(t)

	type caller struct {
		file string
		fn   string
	}
	found := map[string][]caller{} // package dir -> call sites
	var dotImports []string

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
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		pkgDir := filepath.ToSlash(filepath.Dir(rel))
		if pkgDir == "internal/totpconsume" {
			return nil // the package that OWNS the rule; a second DEFINITION there is
			// TestSharedRuleHasExactlyOneDefinition's job, not a new surface.
		}

		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, src, 0)
		if perr != nil {
			return perr
		}

		// Resolve the local name the shared rule is imported under, exactly (the
		// import path is compared in full, so a look-alike path cannot pass).
		alias := ""
		for _, spec := range f.Imports {
			p, uerr := strconv.Unquote(spec.Path.Value)
			if uerr != nil || p != sharedRuleImport {
				continue
			}
			if spec.Name != nil {
				if spec.Name.Name == "." {
					// A dot-import makes the call read as a bare ConsumeCode and is
					// the one spelling that could hide an invocation from this check.
					dotImports = append(dotImports, "api/"+rel)
					return nil
				}
				alias = spec.Name.Name
				continue
			}
			alias = "totpconsume"
		}
		if alias == "" {
			return nil
		}

		// A caller can also reach the rule through a NAME it was bound to:
		//
		//	var consume = totpconsume.ConsumeCode
		//	func f(...) bool { return consume(ctx, users, u, code) }
		//
		// The call is then a bare Ident, so the selector below never sees it and
		// a new package binding the rule first is invisible — a different
		// spelling of the same dodge as the dot-import refused further down.
		bindings := ruleBindingsIn(f, alias)

		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			ast.Inspect(fn, func(inner ast.Node) bool {
				call, ok := inner.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel != nil && sel.Sel.Name == "ConsumeCode" {
					id, ok := sel.X.(*ast.Ident)
					if !ok || id.Name != alias {
						return true
					}
					found[pkgDir] = append(found[pkgDir], caller{
						file: "api/" + rel,
						fn:   fn.Name.Name,
					})
					return true
				}
				id, ok := call.Fun.(*ast.Ident)
				if !ok {
					return true
				}
				if _, bound := bindings[id.Name]; bound {
					found[pkgDir] = append(found[pkgDir], caller{
						file: "api/" + rel,
						fn:   fn.Name.Name + " via " + id.Name,
					})
				}
				return true
			})
			return false
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk api tree: %v", err)
	}

	for _, file := range dotImports {
		t.Errorf("%s dot-imports the shared rule (import . %q).\n"+
			"A dot-import makes the call read as a bare ConsumeCode(...) and hides it from every "+
			"name-based check in this file. Import the package by name so the call site is "+
			"auditable.", file, sharedRuleImport)
	}
	if len(found) == 0 {
		t.Fatal("no package calls totpconsume.ConsumeCode — the shared rule's only callers are " +
			"gone, so this guard is vacuous; fix the guard, do not delete it")
	}

	dirs := make([]string, 0, len(found))
	for dir := range found {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		var sites []string
		for _, c := range found[dir] {
			sites = append(sites, c.file+" ("+c.fn+")")
		}
		sort.Strings(sites)
		if _, ok := ruleCallerPackages[dir]; ok {
			t.Logf("%s calls the shared rule from: %s", dir, strings.Join(sites, ", "))
			continue
		}
		t.Errorf("%s calls totpconsume.ConsumeCode, but it is not a reviewed caller of the "+
			"single-use rule.\n"+
			"Call sites: %s\n"+
			"A new package calling the rule is a new SURFACE enforcing it — it needs its own "+
			"behavioural coverage of the single-use property, and it must not be added to this "+
			"list silently. If it is deliberate, add the package to ruleCallerPackages with the "+
			"reason, in this test, in the same change.", dir, strings.Join(sites, ", "))
	}
	for dir, why := range ruleCallerPackages {
		if len(found[dir]) == 0 {
			t.Errorf("ruleCallerPackages lists %s (%s) but that package no longer calls "+
				"totpconsume.ConsumeCode — remove the stale entry, or the surface has lost its "+
				"TOTP single-use check", dir, why)
		}
	}
}

// ruleBindingsIn maps every file-scope name that the shared rule is bound to,
// to the selector expression that bound it — `var consume =
// totpconsume.ConsumeCode` yields {"consume": "totpconsume.ConsumeCode"}.
//
// It exists because a call through such a name is a bare Ident
// (`consume(ctx, users, u, code)`), which the selector check above cannot see:
// the package binding the rule first is then invisible to this guard. Only a
// direct binding to the rule's entry point counts — `var x = somethingElse` is
// not a caller, and resolving arbitrary indirection would be guessing.
func ruleBindingsIn(f *ast.File, alias string) map[string]string {
	bindings := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range vs.Names {
			if i >= len(vs.Values) {
				continue
			}
			sel, ok := vs.Values[i].(*ast.SelectorExpr)
			if !ok || sel.Sel == nil || sel.Sel.Name != "ConsumeCode" {
				continue
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok || id.Name != alias {
				continue
			}
			bindings[name.Name] = alias + ".ConsumeCode"
		}
		return true
	})
	return bindings
}

// sqlExpr is one string-literal expression an author wrote — a statement, in
// the sense this guard cares about — with the write site that authored it.
type sqlExpr struct {
	node ast.Expr
	site string // the enclosing function name, or a package-level declaration's name
}

// sqlExpressions returns every string-literal expression in a file, wherever it
// is declared.
//
// This is deliberately wider than a walk of *ast.FuncDecl: the repo keeps its
// statements in package-level consts (dbstore/form.go's setStatusSQL), and a
// declaration outside any function was never visited at all. A statement split
// across literals, or built by fmt.Sprintf from the column name, is likewise not
// visible one literal at a time — so a concatenation or a format call is
// returned as ONE expression, and the literals inside it are not returned again
// (the statement is named once, at the site that wrote it).
func sqlExpressions(f *ast.File, funcs map[string]string) []sqlExpr {
	var out, composites []sqlExpr
	inside := map[ast.Node]bool{}

	// Composite expressions first, so a literal that is part of one can be
	// excluded below. Pre-order visiting means the OUTER of nested
	// concatenations is seen first, and marking its whole subtree keeps the
	// inner pieces from being reported as statements of their own.
	ast.Inspect(f, func(n ast.Node) bool {
		expr, ok := n.(ast.Expr)
		if !ok || !isCompositeStringExpr(expr, funcs) || inside[expr] {
			return true
		}
		composites = append(composites, sqlExpr{node: expr, site: enclosingDecl(f, expr)})
		ast.Inspect(expr, func(inner ast.Node) bool {
			inside[inner] = true
			return true
		})
		return true
	})

	// Every remaining standalone literal: this is what reaches a package-level
	// const, a var initialiser, and a lone string passed to Exec.
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING || inside[lit] {
			return true
		}
		out = append(out, sqlExpr{node: lit, site: enclosingDecl(f, lit)})
		return true
	})

	return append(composites, out...)
}

// isCompositeStringExpr reports whether an expression builds a string out of
// more than one piece: a `+` concatenation or a Sprintf-style call with a
// literal format.
func isCompositeStringExpr(expr ast.Expr, funcs map[string]string) bool {
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		return e.Op == token.ADD && isStringPiece(e.X, funcs) && isStringPiece(e.Y, funcs)
	case *ast.CallExpr:
		_, ok := formatCall(e)
		return ok
	}
	return false
}

// isStringPiece reports whether an expression can contribute to a string an
// author assembled: a string literal, a NAME (a const holding a fragment or the
// column name formatted in), a parenthesised one of those, a concatenation of
// pieces, or — one hop, same package — a call to a package-level function whose
// body returns a resolvable string expression. Requiring both sides to be
// literals would miss the mixed spelling
// `"UPDATE users SET " + totpStepColumn + " = $2 ..."`, which is exactly the
// shape a statement split across its pieces takes.
//
// The call case is what closes the helper-function gap: `pool.Exec(ctx,
// stmtFn(), ...)` carries no literal of its own, so the statement is only
// visible at the helper's return. Only a callee in funcs is string-valued —
// that map holds the package-level functions of THIS package whose body is a
// resolvable return, so an imported callee stays opaque and contributes nothing
// (widening further would guess).
func isStringPiece(expr ast.Expr, funcs map[string]string) bool {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return e.Kind == token.STRING
	case *ast.Ident:
		return true
	case *ast.ParenExpr:
		return isStringPiece(e.X, funcs)
	case *ast.BinaryExpr:
		return e.Op == token.ADD && isStringPiece(e.X, funcs) && isStringPiece(e.Y, funcs)
	case *ast.CallExpr:
		return callIsStringValue(e, funcs)
	}
	return false
}

// callIsStringValue reports whether a call's value is a string this guard can
// resolve: an Ident callee (a package-level function of this package) present in
// funcs. A Selector callee is deliberately excluded — `pkg.Fn()` may be a
// same-named callee in an unrelated package, and resolving that name against
// THIS package's function table would attribute the wrong body to the call.
func callIsStringValue(call *ast.CallExpr, funcs map[string]string) bool {
	id, ok := call.Fun.(*ast.Ident)
	if !ok {
		return false
	}
	_, ok = funcs[id.Name]
	return ok
}

// formatCall returns the format literal of a Sprintf-style call (and true), so a
// statement assembled from its column name is resolved as one string. Only the
// standard formatting helpers are recognised, and only when the format itself is
// a literal — an opaque format cannot be judged, and guessing would be worse
// than leaving it.
func formatCall(call *ast.CallExpr) (*ast.BasicLit, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || len(call.Args) == 0 {
		return nil, false
	}
	switch sel.Sel.Name {
	case "Sprintf", "Printf", "Fprintf", "Errorf", "Sprint":
	default:
		return nil, false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return nil, false
	}
	return lit, true
}

// enclosingDecl names the write site an expression belongs to: the innermost
// function containing it, or — for a declaration OUTSIDE any function, which is
// what this guard previously could not see — that declaration's own name, in a
// namespace of its own.
//
// WHY THE DECLARATION KEY IS PREFIXED (and not just the bare name)
//
// A function and a package-level declaration may share a name in Go: a const and
// a method are different namespaces, so
//
//	const ConsumeTOTPStep = `UPDATE users SET totp_last_step = $2 …`
//	func (s *UserStore) ConsumeTOTPStep(ctx context.Context, …) (bool, error) { … }
//
// compiles. Keying the const by its bare name therefore produced the very same
// string as the allowlist entry written for the METHOD, and the second
// implementation silently inherited a permission granted to a different
// declaration — while a stale entry for the method was equally invisible,
// because the const's write satisfied `seen` on its behalf. `decl <Name>` cannot
// collide with a function key: an identifier cannot contain a space.
//
// WHY A LOCAL DECLARATION IS KEYED BY ITS FUNCTION:
//
// A const/var INSIDE a function body is part of that function's statement, so it
// is named by the function. Keying it by its own name instead reported the SAME
// statement twice (the function, and the declaration) and — because the function
// key is what the allowlist covers — turned a legitimate
// `const stepSQL = "…" INSIDE the accessor` into an unlisted write site plus a
// bogus "no production file writes …" failure, whose message tells the author to
// delete the allowlist entry that is the only protection there is.
func enclosingDecl(f *ast.File, node ast.Node) string {
	fnName, fnSpan := "", -1
	vsName, vsSpan := "", -1
	ast.Inspect(f, func(n ast.Node) bool {
		var pos, end token.Pos
		switch d := n.(type) {
		case *ast.FuncDecl:
			pos, end = d.Pos(), d.End()
			if node.Pos() < pos || end < node.End() {
				return true
			}
			if size := int(end - pos); fnSpan == -1 || size < fnSpan {
				fnName, fnSpan = d.Name.Name, size
			}
		case *ast.ValueSpec:
			if len(d.Names) == 0 {
				return true
			}
			pos, end = d.Pos(), d.End()
			if node.Pos() < pos || end < node.End() {
				return true
			}
			if size := int(end - pos); vsSpan == -1 || size < vsSpan {
				vsName, vsSpan = d.Names[0].Name, size
			}
		}
		return true
	})
	// Innermost function first: whatever a declaration inside a function body is
	// called, it belongs to that function.
	if fnName != "" {
		return fnName
	}
	if vsName != "" {
		return "decl " + vsName
	}
	return ""
}

// resolveSQLText reconstructs the string an expression denotes: literals are
// unquoted, a name is expanded to a const/var initialiser in the same package,
// `+` concatenation and Sprintf-style formatting are resolved with their
// arguments substituted, and a call to a package-level function of the same
// package (one hop) is resolved to the string its return expression denotes.
// Anything else contributes nothing.
//
// The point is that the WHOLE statement is matched, so splitting it across
// literals, formatting the column name into it, or reaching it through a helper
// function cannot hide the signature.
func resolveSQLText(expr ast.Expr, consts, funcs, local map[string]string) string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return ""
		}
		s, err := strconv.Unquote(e.Value)
		if err != nil {
			return ""
		}
		return s
	case *ast.Ident:
		// A name the enclosing function bound wins over a package-level
		// declaration of the same name — that is Go's own scoping, and the
		// binding map is now scoped to that function so it cannot reach a
		// statement outside it. A name the statement never bound falls back to
		// the package-wide const map.
		if s, ok := local[e.Name]; ok {
			return s
		}
		return consts[e.Name]
	case *ast.ParenExpr:
		return resolveSQLText(e.X, consts, funcs, local)
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return ""
		}
		return resolveSQLText(e.X, consts, funcs, local) + resolveSQLText(e.Y, consts, funcs, local)
	case *ast.CallExpr:
		if callIsStringValue(e, funcs) {
			return funcs[e.Fun.(*ast.Ident).Name]
		}
		if _, ok := formatCall(e); !ok {
			return ""
		}
		format, err := strconv.Unquote(e.Args[0].(*ast.BasicLit).Value)
		if err != nil {
			return ""
		}
		args := make([]string, 0, len(e.Args)-1)
		for _, a := range e.Args[1:] {
			args = append(args, resolveSQLText(a, consts, funcs, local))
		}
		return applyFormat(format, args)
	}
	return ""
}

// applyFormat substitutes args into a format string, covering the verbs this
// repo uses to build SQL (%s, %v, %q). Anything else is left verbatim, which
// can only make the result no MORE likely to match — the guard fails closed
// towards reporting rather than silent.
func applyFormat(format string, args []string) string {
	var b strings.Builder
	next := 0
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			b.WriteByte(format[i])
			continue
		}
		j := i + 1
		if j < len(format) && format[j] == '%' {
			b.WriteByte('%')
			i = j
			continue
		}
		// Skip flags, width and precision to reach the verb.
		for j < len(format) && strings.ContainsRune("+-# 0123456789.", rune(format[j])) {
			j++
		}
		if j >= len(format) {
			b.WriteString(format[i:])
			break
		}
		verb := format[j]
		arg := ""
		if next < len(args) {
			arg = args[next]
			next++
		}
		switch verb {
		case 's', 'v':
			b.WriteString(arg)
		case 'q':
			b.WriteString(strconv.Quote(arg))
		default:
			b.WriteString(format[i : j+1])
		}
		i = j
	}
	return b.String()
}

// collectStringConsts maps every const/var name in a file to its string
// initialiser, so a statement referring to a name (the `%s` argument of a
// Sprintf, or a const holding a fragment) is resolved rather than ignored.
func collectStringConsts(f *ast.File) map[string]string {
	consts := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range vs.Names {
			if i >= len(vs.Values) {
				continue
			}
			lit, ok := vs.Values[i].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			if s, err := strconv.Unquote(lit.Value); err == nil {
				consts[name.Name] = s
			}
		}
		return true
	})
	return consts
}

// localStringBindings maps the name a FUNCTION BODY binds to a resolvable string
// to that string, so a statement assembled after a local assignment is resolved
// rather than left as an opaque identifier:
//
//	col := colFn()
//	q := fmt.Sprintf("UPDATE users SET %s = ...", col)
//
// This is the same reach collectStringConsts gives a const/var declaration, and
// the same one the guard has always given a function-local `var x = "..."`
// (which is resolved via the file-wide const map); `x := "..."` is an
// *ast.AssignStmt instead of a *ast.ValueSpec, so without this it resolved to
// nothing.
//
// ONE STATEMENT AT A TIME, in source order: the map seen at the point of a
// statement is exactly what an author would have had in scope, so a name
// re-assigned to something opaque cannot make an earlier statement read wrong.
//
// SCOPED TO THE ENCLOSING FUNCTION, and that scope is load-bearing. The first
// version was handed the whole *ast.File and keyed by NAME only, so an unrelated
// `x := "..."` anywhere earlier in a file overrode the package declaration of x
// for every later statement in that file — which silently MISSED a genuine
// second implementation whose column const was shadowed by a sibling function,
// and named legitimate non-TOTP code as a TOTP write. A name bound inside a
// function is in scope only there, and a package-level declaration has no local
// bindings at all, so scope nil yields nothing.
//
// A nested function literal is a scope of its own and is not descended into.
func localStringBindings(scope ast.Node, before token.Pos, consts, funcs map[string]string) map[string]string {
	local := map[string]string{}
	if scope == nil {
		return local
	}
	ast.Inspect(scope, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok && n != scope {
			// A nested literal binds into a scope the statement being resolved
			// is not in; its names must not leak out.
			return false
		}
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		if as.End() >= before {
			// At or after the statement being resolved: not in scope, and
			// visiting deeper would only see a later re-assignment.
			return false
		}
		for i, lhs := range as.Lhs {
			if i >= len(as.Rhs) {
				continue
			}
			id, ok := lhs.(*ast.Ident)
			if !ok {
				continue
			}
			if s := resolveSQLText(as.Rhs[i], consts, funcs, nil); s != "" {
				local[id.Name] = s
			}
		}
		return true
	})
	return local
}

// enclosingFuncDecl returns the innermost function body a node belongs to — an
// *ast.FuncDecl's body or a function literal — or nil when the node sits at
// package level, where there is no local scope.
//
// It exists so localStringBindings can be scoped correctly: only the names bound
// inside THIS body are in scope for a statement in it.
func enclosingFuncDecl(f *ast.File, node ast.Node) ast.Node {
	var best ast.Node
	span := -1
	ast.Inspect(f, func(n ast.Node) bool {
		var body *ast.BlockStmt
		switch d := n.(type) {
		case *ast.FuncDecl:
			body = d.Body
		case *ast.FuncLit:
			body = d.Body
		default:
			return true
		}
		if body == nil || node.Pos() < body.Pos() || body.End() < node.End() {
			return true
		}
		// Innermost wins.
		if size := int(body.End() - body.Pos()); span == -1 || size < span {
			best, span = n, size
		}
		return true
	})
	return best
}

// packageConsts is a memoised collectStringConsts over EVERY production file in
// a directory, not just the one being inspected.
// A statement's pieces do not have to live beside it: the column name it
// formats in, or a shared fragment, is exactly the kind of thing that lands in a
// sibling file of the same package (dbstore/form.go's setStatusSQL concatenates
// a const declared elsewhere in the package). Resolving only the current file
// would leave the Sprintf spelling of a copy green whenever its column name was
// declared one file over.
func packageConsts(dir string, memo map[string]map[string]string) map[string]string {
	if c, ok := memo[dir]; ok {
		return c
	}
	consts := map[string]string{}
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	for _, p := range files {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		src, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		fset := token.NewFileSet()
		pf, err := parser.ParseFile(fset, p, src, 0)
		if err != nil {
			continue
		}
		for k, v := range collectStringConsts(pf) {
			consts[k] = v
		}
	}
	memo[dir] = consts
	return consts
}

// packageStringFuncs is a memoised resolution of the package-level FUNCTIONS in
// a directory whose body returns a string this guard can reconstruct — the same
// package-wide reach packageConsts gives a const, one hop of indirection.
//
// WHY: a statement does not have to be written where it is USED. Reaching it
// through a helper —
//
//	func stmtFn() string { return `UPDATE users SET totp_last_step = ...` }
//	pool.Exec(ctx, stmtFn(), u.ID, step)
//
// — leaves no literal at the call site, so an author who copies the accessor's
// statement into a helper (rather than inline) used to be invisible. The body is
// right there in the same package, so one hop is resolvable without guessing.
//
// DELIBERATELY BOUNDED. Only a package-level function (no receiver), only when
// its body is a SINGLE return of a resolvable expression, and the helper's own
// return is resolved against package constants only — never against other
// helpers — so this stays one hop and not a call-graph walk. A function that
// assembles its result across statements has no single expression to quote, and
// picking one would be a guess; that shape is left GREEN deliberately, and is
// the one residual of this guard (it needs real data-flow analysis).
func packageStringFuncs(dir string, memo map[string]map[string]string, constsMemo map[string]map[string]string) map[string]string {
	if f, ok := memo[dir]; ok {
		return f
	}
	funcs := map[string]string{}
	consts := packageConsts(dir, constsMemo)
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	for _, p := range files {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		src, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		fset := token.NewFileSet()
		pf, err := parser.ParseFile(fset, p, src, 0)
		if err != nil {
			continue
		}
		for _, decl := range pf.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			ret := singleReturn(fn)
			if ret == nil {
				continue
			}
			if s := resolveSQLText(ret, consts, nil, nil); s != "" {
				funcs[fn.Name.Name] = s
			}
		}
	}
	memo[dir] = funcs
	return funcs
}

// singleReturn returns the expression a function's body returns, when the body
// is exactly one `return <expr>` — the shape whose value is quotable as a
// string. A multi-statement body returns nil.
func singleReturn(fn *ast.FuncDecl) ast.Expr {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return nil
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return nil
	}
	return ret.Results[0]
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
