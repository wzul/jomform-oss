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
// The allowlist above is keyed by "repo-relative file (site)", and three shapes
// made that key ambiguous. All three were reproduced with `go build ./...` RC=0
// and this guard GREEN:
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
//	G3. a METHOD on ANOTHER RECEIVER sharing the accessor's name. A method name
//	    is scoped to its receiver TYPE, so `(s *UserStore) ConsumeTOTPStep` and
//	    `(o *Other) ConsumeTOTPStep` are two different methods that coexist in
//	    one file. Keyed by the bare name alone both produced the SAME site
//	    "… (ConsumeTOTPStep)", so a second full implementation inherited the
//	    allowlist entry written for the accessor and read GREEN. G1's `decl`
//	    prefix does not close this: it separates a declaration from a function,
//	    and here BOTH sides are functions of the same name.
//
// All three are closed in enclosingDecl, and the key it returns is INJECTIVE
// across every FuncDecl in a file — the property each of the three needed:
//
//   - a METHOD is keyed by its RECEIVER TYPE and its name
//     (`UserStore.ConsumeTOTPStep`, G3), because a bare name does not name a
//     receiver, and a sibling method on another receiver is a sibling
//     IMPLEMENTATION, not the same site;
//   - a plain FUNCTION is keyed by its bare name — there is no receiver to add,
//     and a package-level function and a method may share a name in Go, so this
//     keys them apart from each other too;
//   - a declaration INSIDE a function body is keyed by that function (G2), so a
//     statement held in a const belongs to the function that holds it, whatever
//     the const is called;
//   - a declaration OUTSIDE any function is keyed `decl <name>` (G1) — a shape no
//     function key can equal, since an identifier cannot contain a space and no
//     method key can equal, since an identifier cannot contain a dot. The three
//     key spaces are disjoint by SHAPE, which is what makes the key injective
//     rather than merely longer.
//
// This is a different axis from E's function-scoped local bindings and does not
// replace them. TestWriteSiteKeyIsInjectiveAcrossFuncDecls pins the injectivity
// directly, on an in-memory file, so the property does not depend on the api
// tree happening to contain a colliding shape at any moment.
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
//
// THE SAME CLASS, ONE DECLARATION FORM OVER: local `const`/`var` (ValueSpec)
//
// A local `const x = "…"` / `var x = "…"` is an *ast.ValueSpec, not an
// *ast.AssignStmt, so scoping the `:=` map left it in the file-wide const map —
// keyed by NAME, last declaration winning, and MERGED ACROSS THE DIRECTORY by
// packageConsts, so a local declaration in one file could decide a statement in
// another. Measured in both directions again, on a tree that builds: a genuine
// second implementation whose column const a sibling function shadowed with a
// local const read GREEN (the guard named nothing), and an innocent
// `UPDATE products SET …` whose column const a sibling's local const shadowed
// with the column name was reported as a TOTP write. Fixed the same way, by
// scope: collectStringConsts now takes PACKAGE-LEVEL declarations only, and a
// function-local declaration is carried by the same scoped map as a `:=`
// binding — both forms collected where they are actually in scope, so the reach
// the ValueSpec map granted is kept (a local const still resolves its own
// function's statement, and a package const named from another function still
// resolves). The card's alternative — "report a name that resolves two ways" —
// was rejected ON MEASUREMENT here: it closes the MISS and entrenches the false
// alarm. Pinned by TestLocalDeclarationsAreScopedToTheirFunction.
//
// THE CONSUMER ALLOWLIST IS KEYED BY SITE, AND WHY THAT IS THE SAME FIX AS G (H)
//
// allowedDirectConsumers WAS keyed by FILE, while
// TestOnlyTheSharedRuleConsumesATOTPStep recorded a call site as {file, fn} and
// checked the FILE against it. The fn it carried reached only the error text. So
// the permission written for ONE named function in api/internal/httpapi/auth.go
// (handle2FAEnable, the pending-secret flow) extended to every other function in
// that file, and TestSharedRuleHasExactlyOneDefinition then waived the thin
// shape a second time:
//
//	func (z *Other) handle2FAEnable(ctx context.Context, userID string, step int64) (bool, error) {
//		return z.Users.ConsumeTOTPStep(ctx, userID, step)
//	}
//
// That is a public entry point which takes a STEP rather than a code — it
// validates nothing — and consumes it. It read GREEN on all four checks on the
// pre-fix tree (`go build ./...` RC=0), because:
//
//   - the consumer check found `allowedDirectConsumers["api/internal/httpapi/auth.go"]`
//     and continued, the probe being in that file;
//   - TestSharedRuleHasExactlyOneDefinition skipped it as a thin
//     delegation — the exemption written for the surface alias
//     checkAndConsumeTOTP, which is sound for an alias the surface CALLS and not
//     for "whatever else is in the file".
//
// Fixed the same way G fixed the write-side key: the consumer allowlist now keys
// the SITE — `repo-relative file (Recv.Name)` for a method, `... (Name)` for a
// function, i.e. the funcSiteKey of G — so a second method of the same name on
// another receiver (G3) is a different site as well. A thin delegation is now
// judged by THAT SAME allowlist through unvettedThinConsumerSite, so both
// consumer checks consult one decision point instead of two keys that can
// disagree; a thin delegation to the store accessor is waived only where someone
// vetted the site, while a delegation of the RULE itself
// (totpconsume.ConsumeCode / the checkAndConsumeTOTP alias) is still not a
// consumer at all and is left to TestBothSurfacesCallTheSharedRule.
//
// A deliberate thin alias on a new receiver stays possible, and costs ONE line —
// its site, with a reason, in allowedDirectConsumers. That is the review, and it
// is the point. A body that validates AND consumes is not a delegation at all:
// TestSharedRuleHasExactlyOneDefinition reports it against its receiver-qualified
// expected list, which is where the non-thin variant of the same probe already
// fails.

import (
	"go/ast"
	"go/parser"
	"go/printer"
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

// allowedStepWrites maps a WRITE SITE ("repo-relative file (site)",
// slash-separated from api/) to why it is allowed to write users.totp_last_step
// itself.
//
// dbstore/user.go (UserStore.ConsumeTOTPStep) is the accessor that DEFINES the
// write — the predicate lives inside its UPDATE, which is what makes a code
// single-use rather than single-use-per-window. The shared rule reaches it by
// CALLING ConsumeTOTPStep, never by writing the column itself, so it is
// deliberately NOT listed here: an entry for it would be stale by construction
// (dbstore never calls the rule) and is rejected by the staleness check below.
//
// KEYED BY SITE, NOT JUST BY FILE — and the site of a METHOD is its RECEIVER
// TYPE plus its name, never its bare name. A second write function added beside
// the accessor in the same file is a second implementation of the rule just as
// much as one in another package, so the allowlist must not cover it by
// accident; and because a method name is scoped to its receiver type, a bare
// name would put a SECOND method of the same name on a different receiver under
// this exact allowlist entry. `UserStore.ConsumeTOTPStep` names the one
// receiver whose accessor is vetted here. A plain function has no receiver and
// is keyed by its bare name (no dot), so it cannot collide with a method key
// either.
//
// A statement declared at PACKAGE level (a const or var outside any function) is
// keyed as `... (decl <name>)` — e.g. `... (decl totpStepUpdateSQL)` — because
// there is no function to name, and a bare name is NOT safe to use here: a
// package-level const and a method may share a name in Go (different
// namespaces), so a bare `file (ConsumeTOTPStep)` would key a const and the
// allowlisted accessor METHOD identically, letting the const's statement inherit
// the method's permission. The three key shapes — `Recv.Name`, `Name`, and
// `decl Name` — cannot collide with one another: an identifier contains neither
// a dot nor a space. A local const/var inside a function body belongs to that
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
	"api/internal/dbstore/user.go (UserStore.ConsumeTOTPStep)": "the store accessor — defines " +
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

// allowedDirectConsumers maps a consume SITE — "repo-relative file (site)",
// slash-separated from api/, where the site is the funcSiteKey of G: `Recv.Name`
// for a method, the bare name for a plain function — to why that site is allowed
// to CALL ConsumeTOTPStep outside the shared rule.
//
// dbstore is deliberately absent: internal/dbstore/user.go DEFINES the method,
// it never calls it, and this guard walks call expressions — so an entry for it
// would be stale by construction and is rejected by the staleness check below.
//
// KEYED BY SITE, NOT BY FILE (H in the header). The entry used to name a FILE,
// and TestOnlyTheSharedRuleConsumesATOTPStep checked only the file, so the one
// function this entry was written for extended permission to every function
// beside it in that file — and TestSharedRuleHasExactlyOneDefinition waived a
// thin second consumer on top of that. The site key is the same injective key
// the write-side allowlist uses (TestWriteSiteKeyIsInjectiveAcrossFuncDecls), so
// a method on another receiver sharing the name is a different site too.
//
// A file alone is NOT a key here, by construction: every entry must name a site
// inside the parentheses. That is what the two consumer checks below rely on,
// and allowedToConsumeStep is the one place both of them ask.
var allowedDirectConsumers = map[string]string{
	"api/internal/totpconsume/consume.go (ConsumeCode)": "the shared rule — the one implementation, " +
		"which both surfaces call",
	"api/internal/httpapi/auth.go (AuthHandlers.handle2FAEnable)": "handle2FAEnable consumes the step of the PENDING " +
		"secret (not the user's own) and maps a DB error to 500 rather than a 401 " +
		"rejection — a different flow, deliberately not folded into the shared rule",
}

// consumeSite names the site a ConsumeTOTPStep call belongs to, in the
// allowlist's key shape: "api/<repo-relative file> (<funcSiteKey>)".
func consumeSite(file string, fn *ast.FuncDecl) string {
	return file + " (" + funcSiteKey(fn) + ")"
}

// allowedToConsumeStep reports whether SOMEONE VETTED this call site. It is the
// one place the consumer allowlist is consulted, so the two consumer checks
// cannot come to different conclusions about the same site — the exact shape
// that let the thin second consumer of (H) through: the call check read the
// allowlist by FILE and continued, the definition check waived the same call as
// a thin delegation, and neither question named the site.
func allowedToConsumeStep(site string) bool {
	_, ok := allowedDirectConsumers[site]
	return ok
}

// TestOnlyTheSharedRuleConsumesATOTPStep fails when a production file outside the
// shared rule consumes a TOTP time-step. That is the drift this card exists to
// prevent: a hand-rolled copy in one surface, invisible to the other surface's
// tests.
//
// The allowlist is keyed by the SITE (file AND function, receiver-qualified — H
// in the header), not by the file: an entry written for one function does not
// cover its neighbours. A stale entry is its own drift and is reported too.
//
// BITE: add `users.ConsumeTOTPStep(ctx, u.ID, step)` to any handler in
// internal/httpapi or internal/mcpsrv (the pre-fix state of PR 686's copy) and
// this test names that file and site and fails.
//
// BITE (H): add it inside a NEW function of the already-allowlisted
// api/internal/httpapi/auth.go — e.g. a thin
// `func (z *Other) handle2FAEnable(ctx context.Context, id string, step int64)
// (bool, error) { return z.Users.ConsumeTOTPStep(ctx, id, step) }` — and this
// test names that site instead of continuing on the file.
func TestOnlyTheSharedRuleConsumesATOTPStep(t *testing.T) {
	root := apiRoot(t)

	// found is the SET of call sites, keyed in the allowlist's own shape
	// ("api/<file> (<Recv.Name>)"), so the check below and the allowlist are
	// speaking about the same thing — and so an entry written for one function
	// cannot cover its neighbour in the same file.
	found := map[string]bool{}

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
				site := consumeSite("api/"+rel, fn)
				found[site] = true
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

	for site := range found {
		if allowedToConsumeStep(site) {
			continue
		}
		t.Errorf("%s consumes a TOTP time-step directly.\n"+
			"The single-use rule is implemented ONCE, in internal/totpconsume; this site is a "+
			"SECOND copy of it (the exact drift this guard exists to stop). Call "+
			"totpconsume.ConsumeCode instead, or — if this really is a different flow — add the "+
			"SITE (%s) to allowedDirectConsumers with the reason, in this test. The entry names "+
			"one function: a neighbour in an already-allowed FILE is not covered.",
			site, site)
	}

	// A stale allowlist is its own drift: an entry for a site that no longer
	// consumes a step would silently permit a future copy to reappear there.
	for site, why := range allowedDirectConsumers {
		if !found[site] {
			t.Errorf("allowedDirectConsumers lists %s (%s) but that site no longer calls "+
				"ConsumeTOTPStep — remove the stale entry so the allowlist cannot hide a future copy",
				site, why)
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

	var impls []string         // a body that consumes a step without delegating
	var thinConsumers []string // a thin delegation to the STORE ACCESSOR, unvetted

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
			// A thin body DELEGATES rather than implements, but WHICH delegation
			// it is decides who judges it (H in the header); waiving every thin
			// body is what let a second consumer of a TOTP step hide in the
			// already-allowlisted auth.go.
			//
			//   - `return totpconsume.ConsumeCode(…)` (or the surface alias
			//     checkAndConsumeTOTP) is the alias shape this exemption was
			//     written for: it contains no rule of its own, and
			//     TestBothSurfacesCallTheSharedRule requires the surface to
			//     INVOKE it, so a dead alias is caught there;
			//   - `return users.ConsumeTOTPStep(…)` is an entry point that
			//     consumes a step under a name of its own — there is no rule in
			//     the body, only the consumption — so it is judged by the SAME
			//     allowlist TestOnlyTheSharedRuleConsumesATOTPStep uses, by site.
			//     A deliberate alias can be admitted there; a method on a new
			//     receiver inside an already-allowed file cannot ride in on its
			//     neighbour's entry.
			//
			// A thin delegation of an UNRELATED name is neither: `return
			// decodeBase64(s)` in a file that also mentions the accessor is not
			// a consumer, and is skipped as it always was.
			if site, bad := unvettedThinConsumerSite("api/"+rel, fn, allowedToConsumeStep); bad {
				thinConsumers = append(thinConsumers, site)
				return true
			}
			if thinDelegationCall(fn) != nil {
				return true
			}
			// A non-delegating function that consumes a step IS an implementation.
			if fnConsumesStep(fn) {
				impls = append(impls, "api/"+rel+" ("+funcSiteKey(fn)+")")
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk api tree: %v", err)
	}
	sort.Strings(impls)
	sort.Strings(thinConsumers)

	if len(impls) == 0 {
		t.Fatal("found no function that consumes a TOTP step — this guard is vacuous, fix it")
	}
	// A thin delegation to the store accessor is not a second DEFINITION, but it
	// is a second CALL SITE, and the exemption is not free: it must be a site
	// someone vetted, in the same allowlist the consumer check consults.
	for _, got := range thinConsumers {
		t.Errorf("unexpected TOTP step consumer %q — a single-return delegation to the store "+
			"accessor contains no rule, but it IS an entry point that consumes a step under a name "+
			"of its own, and the allowlist entry beside it names ONE function rather than the file. "+
			"Route it through totpconsume.ConsumeCode instead, or — if a deliberately thin alias "+
			"really is wanted — add its site to allowedDirectConsumers with the reason.",
			got)
	}
	// Exactly one implementation is expected. The exception in httpapi/auth.go
	// (handle2FAEnable, the pending-secret flow) is a KNOWN, allowed second
	// consumer and is asserted by name so it cannot grow. It is asserted by
	// RECEIVER-QUALIFIED name, like every other site key in this file: a method
	// name is scoped to its receiver type, so a bare `handle2FAEnable` would let
	// a SECOND method of that name on another type in the same file land on the
	// expected list and read GREEN — a second implementation of the pending-secret
	// flow, invisible to this check (reproduced with `go build ./...` RC=0).
	want := []string{
		"api/internal/httpapi/auth.go (AuthHandlers.handle2FAEnable)",
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

	// seen maps an allowlisted write site ("file (site)", where a site is
	// `Recv.Name` for a method, a bare name for a plain function, or
	// `decl <name>` for a declaration outside any function) to itself, so a
	// stale entry (or one that silently stopped matching) is visible by name.
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
		reported := map[string]bool{}
		for _, e := range sqlExpressions(f, funcs) {
			if !statementWritesTOTPStep(f, consts, funcs, e) {
				continue
			}
			site := rel + " (" + e.site + ")"
			if reported[site] {
				// The same site is reachable twice now: once through the literal in
				// the helper's body and once through a call to it. One report, not
				// two — the reader needs the place, not the multiplicity.
				continue
			}
			reported[site] = true
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

// TestBuilderAssembledStatementsAreCaught pins the residual t_599d7ff2 left open and
// t_65e18bd7 closes: a statement assembled ACROSS STATEMENTS inside a helper — the
// ordinary `strings.Builder` shape — used to read GREEN for a full second implementation
// of the single-use rule, while the same statement spelled `+`, `fmt.Sprintf` or a `:=`
// binding was caught.
//
// It also pins the BOUNDARY, because widening this walk is how a guard starts accusing
// legitimate code: a Builder whose parts are literals belonging to another table stays
// innocent, and a Builder fed an unresolvable part yields nothing rather than a fragment.
func TestBuilderAssembledStatementsAreCaught(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		write bool
	}{
		{
			name: "the card's probe: strings.Builder across statements, through a call site",
			src: `package probe
import "strings"
func copyStep() string {
	var b strings.Builder
	b.WriteString("UPDATE users SET ")
	b.WriteString("totp_last_step = $2, updated_at = now() WHERE id = $1 AND totp_last_step < $2")
	return b.String()
}
func use(db interface{}) { db.Exec(copyStep()) }`,
			write: true,
		},
		{
			name: "bytes.Buffer, a resolvable helper among the parts, through a call site",
			src: `package probe
import "bytes"
func totpStepColumn() string { return "totp_last_step" }
func copyStep() string {
	var buf bytes.Buffer
	buf.WriteString("UPDATE users SET ")
	buf.WriteString(totpStepColumn())
	buf.WriteString(" = $2 WHERE id = $1 AND totp_last_step < $2")
	return buf.String()
}
func use(db interface{}) { db.Exec(copyStep()) }`,
			write: true,
		},
		{
			name: "a fragment-only Builder helper reached through the caller's + is still caught",
			src: `package probe
import "strings"
func tail() string {
	var b strings.Builder
	b.WriteString("totp_last_step = $2")
	b.WriteString(" WHERE id = $1 AND totp_last_step < $2")
	return b.String()
}
func use(db interface{}) { db.Exec("UPDATE users SET " + tail()) }`,
			write: true,
		},
		{
			name: "a Builder assembling an UNRELATED update stays innocent",
			src: `package probe
import "strings"
func unrelated() string {
	var b strings.Builder
	b.WriteString("UPDATE products SET ")
	b.WriteString("name = $2 WHERE id = $1")
	return b.String()
}`,
			write: false,
		},
		{
			name: "a Builder with an unresolvable part yields nothing, not a fragment",
			src: `package probe
import "strings"
func unknown() string { return dynamic() }
func copy() string {
	var b strings.Builder
	b.WriteString("UPDATE users SET ")
	b.WriteString(unknown())
	b.WriteString(" WHERE id = $1")
	return b.String()
}`,
			write: false,
		},
		{
			name: "a for-loop append has no single value and is not folded",
			src: `package probe
import "strings"
func copy(cols []string) string {
	var b strings.Builder
	b.WriteString("UPDATE users SET ")
	for _, c := range cols {
		b.WriteString(c)
	}
	return b.String()
}`,
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
					if v := resolveSQLText(ret, consts, nil, nil); v != "" {
						funcs[fn.Name.Name] = v
					}
				}
				if v := builderAssembled(fn, consts, funcs); v != "" {
					funcs[fn.Name.Name] = v
				}
			}
			got := false
			for _, e := range sqlExpressions(f, funcs) {
				if statementWritesTOTPStep(f, consts, funcs, e) {
					got = true
				}
			}
			if got != tc.write {
				t.Errorf("statement resolved as a TOTP write = %v, want %v — the Builder "+
					"spelling is the ordinary way to assemble a statement from more than two "+
					"pieces, so a guard that cannot read it proves nothing about the "+
					"single-use rule (card t_65e18bd7). Source:\n%s", got, tc.write, tc.src)
			}
		})
	}
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
				// A statement assembled across statements in the body (card t_65e18bd7).
				if s := builderAssembled(fn, consts, funcs); s != "" {
					funcs[fn.Name.Name] = s
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

// TestLocalDeclarationsAreScopedToTheirFunction is the same scope rule as the
// test above, for the OTHER declaration form: a local `const x = "…"` /
// `var x = "…"` is an *ast.ValueSpec, not an *ast.AssignStmt, so scoping the
// assignment map (card t_15b5a250) left it in the file-wide const map — keyed by
// NAME, last declaration winning.
//
// WHAT THAT COST, measured on the two cases below before the fix (each on a tree
// that builds, `go build ./...` RC=0, with the probe planted in
// api/internal/httpapi):
//
//   - a GENUINE second implementation whose column const a sibling function
//     shadowed with `const col = "name"` read GREEN — the guard did not name the
//     copy at all (the statement resolved to a products update);
//   - an innocent `UPDATE products SET …` was reported as a TOTP write, because a
//     sibling function's `const col = "totp_last_step"` overwrote the package
//     declaration of col that the innocent statement means.
//
// So the map had to be scoped, not merely made to "report a name that resolves two
// ways": convicting on either reading fixes the MISS and entrenches the false
// alarm, which is the reading this test's second case pins. The reach the
// ValueSpec map granted is preserved by the scoped map instead — a declaration in
// its OWN function still resolves (cases 4 and 5) — and a PACKAGE-LEVEL const
// named from another function is still caught (case 7, the shape dbstore/form.go
// keeps its statements in).
//
// It runs on in-memory sources, so it holds on a future tree where the api tree no
// longer contains the trigger shape, and it goes through statementWritesTOTPStep —
// the same function the tree-wide check uses.
//
// BITE: put the ValueSpec case back into the file-wide map (collect ValueSpecs with
// the whole *ast.File instead of package-level declarations + the scoped map) and
// cases 1, 2, 3, 6 and 7 fail — measured, all five. BITE (reach): drop the ValueSpec
// arm from localStringBindings and cases 4 and 5 fail — measured, both.
func TestLocalDeclarationsAreScopedToTheirFunction(t *testing.T) {
	const preamble = `package probe

func colFn() string { return "totp_last_step" }
`

	cases := []struct {
		name  string
		src   string
		write bool // does the final statement write users.totp_last_step?
	}{
		{
			// The card's probe: the column const is a package-level declaration
			// shadowed by an UNRELATED function's local const. The copy's own
			// function does not shadow it, so the package declaration is the
			// correct reading and the copy must be caught.
			name: "sibling function declares a const of the same name",
			src: preamble + `
const col = "totp_last_step"

func unrelated() string { const col = "name"; return col }

func copy() string { return "UPDATE users SET " + col + " = $2 WHERE col < $2" }
`,
			write: true,
		},
		{
			// The other direction: the local declaration holds the column name
			// and the package declaration is innocent. Reading the innocent
			// statement against the sibling's declaration convicts it.
			name: "sibling function's declaration only, products column",
			src: `package probe

const col = "name"

func unrelated() string { const col = "totp_last_step"; return col }

func innocent() string { return "UPDATE products SET " + col + " = $2 WHERE id = $1" }
`,
			write: false,
		},
		{
			// `var` is the same ValueSpec form and shadows the same way.
			name: "the var form of a sibling declaration",
			src: preamble + `
const col = "totp_last_step"

func unrelated() string { var col = "name"; return col }

func copy() string { return "UPDATE users SET " + col + " = $2 WHERE col < $2" }
`,
			write: true,
		},
		{
			// The declaration is in the copy's OWN function, where it is in scope
			// and is the column: the local reading convicts. Dropping the local
			// (ValueSpec) reading would read the package declaration and lose it.
			name: "the declaration is in the copy's own function",
			src: preamble + `
const col = "name"

func copy() string { const col = "totp_last_step"; return "UPDATE users SET " + col + " = $2 WHERE col < $2" }
`,
			write: true,
		},
		{
			// The reach this map exists for, in the declaration form: a local
			// const is the column, with no package declaration at all.
			name: "a local declaration is the column",
			src: preamble + `
func copy() string { const col = "totp_last_step"; return "UPDATE users SET " + col + " = $2 WHERE col < $2" }
`,
			write: true,
		},
		{
			// A declaration must not reach ACROSS functions: the column is
			// declared in one function and used in another, where it means
			// nothing at all — not a TOTP write, and not a products one either.
			name: "a declaration does not reach into a sibling function",
			src: preamble + `
func one() string { const col = "totp_last_step"; return col }

func two() string { return "UPDATE products SET " + col + " = $2 WHERE id = $1" }
`,
			write: false,
		},
		{
			// The reach bound the card set: a name that resolves to a
			// PACKAGE-LEVEL const (dbstore/form.go keeps its statements in such
			// consts) must keep being caught when a sibling function shadows that
			// name locally. The const holds a FRAGMENT assembled by a statement
			// in another function, so the reading runs through the name and not
			// through the fragment's own literal (which would be caught either
			// way and so would prove nothing here).
			name: "a package-level fragment survives a sibling shadow",
			src: preamble + `
const stepTail = "totp_last_step = $2, updated_at = now() WHERE id = $1 AND totp_last_step < $2"

func unrelated() string { const stepTail = "name"; return stepTail }

func use() string { return "UPDATE users SET " + stepTail }
`,
			write: true,
		},
		{
			// The negative control: nothing anywhere names the column.
			name: "no column name at all",
			src: `package probe

func innocent() string { const q = "name"; return "UPDATE products SET " + q + " = $2 WHERE id = $1" }
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
				// A statement assembled across statements in the body (card t_65e18bd7).
				if s := builderAssembled(fn, consts, funcs); s != "" {
					funcs[fn.Name.Name] = s
				}
			}

			got := false
			for _, e := range sqlExpressions(f, funcs) {
				if statementWritesTOTPStep(f, consts, funcs, e) {
					got = true
				}
			}
			if got != tc.write {
				t.Errorf("resolved as a TOTP write = %v, want %v — a const/var declared inside a "+
					"function is in scope only there, so it must not decide a statement in another "+
					"function (and must still decide one in its own)", got, tc.write)
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

	// Calls to a package-level function whose body resolves to a statement. The
	// statement TEXT of a helper assembled across its own statements (or simply a
	// helper an author chose to route through) is not a literal at the call site,
	// so without this the walk never reaches it: measured on the card's probe,
	// `db.Exec(builder())` with a `strings.Builder` helper read GREEN while the
	// same statement written inline was caught (card t_65e18bd7).
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || inside[call] {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		if _, ok := funcs[id.Name]; !ok {
			return true
		}
		out = append(out, sqlExpr{node: call, site: enclosingDecl(f, call)})
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
// THE KEY IS INJECTIVE ACROSS EVERY FuncDecl IN THE FILE, and each part of the
// shape is there for a shape that read GREEN or FALSE-RED without it (all of
// them reproduced; see section G in the header).
//
// A METHOD IS KEYED BY ITS RECEIVER TYPE AND ITS NAME (`UserStore.ConsumeTOTPStep`),
// because a method name is scoped to its receiver type: a bare name is not
// unique across a file, and
//
//	func (s *UserStore) ConsumeTOTPStep(ctx context.Context, …) (bool, error) { … }
//	func (s *Other)     ConsumeTOTPStep(ctx context.Context, …) (bool, error) { … }
//
// compiles as two different methods. Keyed by the bare name they produced the
// SAME site string, so the second — a full second implementation of the rule —
// inherited the allowlist entry written for the accessor and read GREEN. The
// receiver TYPE (not its variable) is what names the method, so `s`, `o` and an
// unnamed receiver all key alike.
//
// A PLAIN FUNCTION IS KEYED BY ITS BARE NAME. There is no receiver to qualify it
// with, and a function and a method may share a name in Go — different
// namespaces — so this keys them apart from each other as well.
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
// collide with a function key: an identifier cannot contain a space; and it
// cannot collide with a method key either, since an identifier cannot contain a
// dot. The three shapes are disjoint by construction, which is the property that
// makes the key injective rather than merely distinct in the cases we happened
// to try.
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
	fnKey, fnSpan := "", -1
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
				fnKey, fnSpan = funcSiteKey(d), size
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
	if fnKey != "" {
		return fnKey
	}
	if vsName != "" {
		return "decl " + vsName
	}
	return ""
}

// funcSiteKey names a FuncDecl uniquely within its file: `Recv.Name` for a
// method, the bare name for a plain function. A receiver is recorded as its
// TYPE, so the same method reached through an unnamed receiver, `s` or `o` keys
// the same, while two methods that share a name on DIFFERENT types stay apart —
// which is the whole point, since Go scopes a method name to its receiver type.
//
// The receiver is rendered through the go/printer, so a pointer, a qualified
// type (`pkg.T`) and a generic receiver all come out as written; a method whose
// receiver has no type at all (not reachable from valid Go) falls back to the
// bare name rather than inventing one.
func funcSiteKey(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	var buf strings.Builder
	if err := printer.Fprint(&buf, token.NewFileSet(), fn.Recv.List[0].Type); err != nil {
		return fn.Name.Name
	}
	recv := strings.TrimSpace(buf.String())
	recv = strings.TrimPrefix(recv, "*")
	if recv == "" {
		return fn.Name.Name
	}
	// `dbstore.T` names one type; strip a package qualifier so the key stays
	// about the type in THIS package, not the spelling of its import path.
	if i := strings.LastIndexByte(recv, '.'); i >= 0 {
		recv = recv[i+1:]
	}
	return recv + "." + fn.Name.Name
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

// collectStringConsts maps every PACKAGE-LEVEL const/var name in a file to its
// string initialiser, so a statement referring to a name (the `%s` argument of a
// Sprintf, or a const holding a fragment) is resolved rather than ignored.
//
// PACKAGE-LEVEL ONLY, and that bound is load-bearing. This used to walk the whole
// file — ast.Inspect over every *ast.ValueSpec, function bodies included — into
// ONE map keyed by NAME, last declaration winning. A function-local
// `const x = "…"` (or `var x = "…"`) in an unrelated function therefore overwrote
// the package-level `x` for every LATER statement in the file, and — because
// packageConsts merges a directory's files into one map — for statements in every
// file of the package. Measured on the card's probe, in both directions:
//
//   - a genuine second implementation whose column const was shadowed this way
//     read GREEN (the copy was not named at all);
//   - an innocent `UPDATE products SET …` whose column const was shadowed by a
//     local declaration holding the column name was reported as a TOTP write.
//
// A local declaration is in scope only inside its own function, so it does not
// belong in this map at all; localStringBindings carries it, scoped, instead.
func collectStringConsts(f *ast.File) map[string]string {
	consts := map[string]string{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
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
		}
	}
	return consts
}

// localStringBindings maps the name a FUNCTION BODY binds to a resolvable string
// to that string, so a statement assembled after a local binding is resolved
// rather than left as an opaque identifier:
//
//	col := colFn()
//	const col2 = "totp_last_step"   // a local DECLARATION binds the same way
//	q := fmt.Sprintf("UPDATE users SET %s = ...", col)
//
// This is the reach collectStringConsts gives a package-level const, moved to the
// scope the name actually lives in. BOTH declaration forms are collected, because
// Go binds them the same: `x := "..."` is an *ast.AssignStmt, while `var x = "..."`
// / `const x = "..."` is an *ast.ValueSpec. Collecting only the assignment form
// (card t_15b5a250's fix) left the ValueSpec form to the file-wide map, which is
// the shadowing defect this function's scope exists to prevent — measured as a
// MISS on a genuine second implementation whose column const a sibling function
// shadowed with a local const, and as a FALSE ALARM on an innocent update whose
// column const a local const shadowed with the column name (card t_eb8782eb).
//
// ONE STATEMENT AT A TIME, in source order: the map seen at the point of a
// statement is exactly what an author would have had in scope, so a name
// re-assigned to something opaque cannot make an earlier statement read wrong.
// A local declaration is in scope from the end of its own spec, so a ValueSpec
// that ends at or after the statement contributes nothing either.
//
// SCOPED TO THE ENCLOSING FUNCTION, and that scope is load-bearing. An earlier
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
		switch d := n.(type) {
		case *ast.AssignStmt:
			if d.End() >= before {
				// At or after the statement being resolved: not in scope, and
				// visiting deeper would only see a later re-assignment.
				return false
			}
			for i, lhs := range d.Lhs {
				if i >= len(d.Rhs) {
					continue
				}
				id, ok := lhs.(*ast.Ident)
				if !ok {
					continue
				}
				if s := resolveSQLText(d.Rhs[i], consts, funcs, nil); s != "" {
					local[id.Name] = s
				}
			}
			return true
		case *ast.ValueSpec:
			if d.End() >= before {
				return false
			}
			for i, name := range d.Names {
				if i >= len(d.Values) {
					continue
				}
				if s := resolveSQLText(d.Values[i], consts, funcs, nil); s != "" {
					local[name.Name] = s
				}
			}
			return true
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
			if ret != nil {
				if s := resolveSQLText(ret, consts, nil, nil); s != "" {
					funcs[fn.Name.Name] = s
				}
			}
			if s := builderAssembled(fn, consts, funcs); s != "" {
				funcs[fn.Name.Name] = s
			}
		}
	}
	memo[dir] = funcs
	return funcs
}

// builderAssembled returns the SQL text a function assembles across STATEMENTS, when
// its body is the ordinary `var b strings.Builder` / `b.WriteString(…)…` / `return
// b.String()` shape (card t_65e18bd7).
//
// WHY THIS EXISTS. The walk resolved a ONE-EXPRESSION return, a literal concatenation, a
// resolvable call and a `:=` binding — so a statement assembled with `strings.Builder`,
// which is the ordinary Go way to build a query from more than two pieces, read GREEN for
// a full second implementation of the single-use rule. Measured on the card: the same
// statement spelled `"…" + colFn() + "…"`, `fmt.Sprintf`, or `col := colFn()` was CAUGHT;
// the Builder spelling was not. `api/internal/dbstore/media_migrate.go` already builds
// UPDATEs dynamically, so this is not an invented evasion.
//
// THE BOUNDARY IS DELIBERATE, and it is the same one the parent card set: ONE function
// body, in SOURCE ORDER, folding only a literal or an expression `resolveSQLText` can
// already resolve. No call graph, no imported callees, no reordering, no loops (a `for`
// that appends is not folded — it has no single value). A name that is not a builder just
// yields nothing, so a legitimate function that happens to use a Builder is not accused;
// and a Builder whose parts cannot all be resolved yields nothing rather than a partial
// string, which is the direction that cannot produce a false alarm from fragments.
func builderAssembled(fn *ast.FuncDecl, consts, funcs map[string]string) string {
	if fn == nil || fn.Body == nil {
		return ""
	}
	// A part may name a const/var declared in THIS body (`const col = "totp_last_step"`
	// beside the builder). That name is no longer in the package const map — it is in
	// scope here and nowhere else — so it comes from the scoped map; without this the
	// fold would abandon a statement it used to reconstruct, trading away reach the
	// ValueSpec map granted before it was scoped.
	local := localStringBindings(fn, fn.End(), consts, funcs)
	// Which identifiers are builders? Bound by a declaration or assignment whose value is
	// a `strings.Builder` / `bytes.Buffer` composite or conversion.
	builders := map[string]bool{}
	var out []string
	for _, stmt := range fn.Body.List {
		switch st := stmt.(type) {
		case *ast.DeclStmt:
			// `var b strings.Builder` — the DECLARATION spelling, which is the one the
			// card's probe actually used. It is a DeclStmt/GenDecl/ValueSpec, not an
			// AssignStmt, so missing it left the fold inert for the ordinary shape.
			gd, ok := st.Decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i < len(vs.Values) && isBufferExpr(vs.Values[i]) {
						builders[name.Name] = true
						continue
					}
					if vs.Type != nil && selNameIsBuffer(vs.Type) {
						builders[name.Name] = true
					}
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range st.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || i >= len(st.Rhs) {
					continue
				}
				if isBufferExpr(st.Rhs[i]) {
					builders[id.Name] = true
				}
				// `s += "literal"` on a plain local string is the same assembly shape.
				if st.Tok == token.ADD_ASSIGN && id.Name != "_" {
					if rhs, ok := st.Rhs[i].(*ast.BasicLit); ok && rhs.Kind == token.STRING {
						if v, err := strconv.Unquote(rhs.Value); err == nil {
							out = append(out, v)
						}
					}
				}
			}
		case *ast.ExprStmt:
			call, ok := st.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "WriteString" {
				continue
			}
			recv, ok := sel.X.(*ast.Ident)
			if !ok || !builders[recv.Name] {
				continue
			}
			if len(call.Args) != 1 {
				continue
			}
			v := resolveSQLText(call.Args[0], consts, funcs, local)
			if v == "" {
				// An unresolvable part: abandon the whole fold rather than quote a
				// fragment. A partial string cannot be matched as the defect anyway.
				return ""
			}
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return ""
	}
	joined := strings.Join(out, "")
	return joined
}

// isBufferExpr reports whether an expression constructs one of the buffer types a
// statement is assembled into.
func isBufferExpr(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.CompositeLit:
		return selNameIsBuffer(v.Type)
	case *ast.CallExpr:
		return selNameIsBuffer(v.Fun)
	case *ast.UnaryExpr:
		return isBufferExpr(v.X)
	}
	return false
}

func selNameIsBuffer(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return (pkg.Name == "strings" || pkg.Name == "bytes") && (sel.Sel.Name == "Builder" || sel.Sel.Name == "Buffer")
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

// thinDelegationCall returns the expression a function's body returns when the
// body is exactly one `return <call>` — i.e. a delegation rather than an
// implementation — and nil otherwise.
//
// It was a bool returning "thin"; the caller has to know WHICH call the
// delegation makes (the rule, or the store accessor), because those two are
// judged differently (H in the header). Reporting the call rather than a bool is
// what makes that distinction impossible to forget at a call site.
func thinDelegationCall(fn *ast.FuncDecl) *ast.CallExpr {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return nil
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return nil
	}
	call, _ := ret.Results[0].(*ast.CallExpr)
	return call
}

// callsStoreAccessor reports whether a call expression invokes the store
// accessor ConsumeTOTPStep — the one call that consumes a step.
func callsStoreAccessor(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel != nil && sel.Sel.Name == "ConsumeTOTPStep"
}

// unvettedThinConsumerSite reports whether fn is a thin delegation TO THE STORE
// ACCESSOR whose SITE no one vetted, and returns the site to name when it is.
//
// A thin delegation of the RULE (totpconsume.ConsumeCode, or a surface's
// checkAndConsumeTOTP alias) is not a consumer at all — it contains no rule and
// no consumption of its own — so it is left to TestBothSurfacesCallTheSharedRule.
// Neither is a thin delegation of some UNRELATED name (`return decodeBase64(s)`
// in the same file): a file is read whenever it mentions the accessor, and only
// "the returned call IS the accessor" makes a function a consumer. A thin
// delegation of the ACCESSOR does consume a step under a name of its own, and is
// admissible only on a site someone wrote down.
//
// THE FILE IS NOT CONSULTED, deliberately, and that is the point of the helper:
// the allowlist entry that admits a function in api/internal/httpapi/auth.go
// names THAT function. Deciding admission from the file — the shape the pre-fix
// check had — is what let the thin second consumer in. A site is a site whether
// or not its file also holds an allowed one;
// TestThinConsumerIsJudgedBySiteNotFile pins exactly that difference on an
// in-memory file, so the property does not depend on the api tree happening to
// contain a colliding pair.
func unvettedThinConsumerSite(file string, fn *ast.FuncDecl, allowed func(site string) bool) (string, bool) {
	call := thinDelegationCall(fn)
	if call == nil || !callsStoreAccessor(call) {
		return "", false
	}
	site := consumeSite(file, fn)
	if allowed(site) {
		return "", false
	}
	return site, true
}

// TestWriteSiteKeyIsInjectiveAcrossFuncDecls pins the property every shape in
// section G of the header needed: the site key enclosingDecl returns is DIFFERENT
// for every declaration in a file. It runs on an in-memory file, so the property
// is asserted regardless of whether the api tree happens to contain a colliding
// shape today.
//
// WHY THIS EXISTS SEPARATELY. The tree-wide check found G1 and G3 only because
// the api tree happened to contain the right shape at the right time — and G3
// (a method on another receiver sharing the accessor's name) was read GREEN in
// this very package until this test existed. A property that is only true while
// the tree cooperates is not a property; it is a coincidence that expires.
//
// Each case gives the keys the FuncDecls in the file must produce, IN SOURCE
// ORDER, plus the keys its const/var declarations must produce. A case whose
// keys are not pairwise distinct fails on the injectivity check below, which is
// the assertion the three G shapes actually need.
//
// BITE: revert funcSiteKey to `fn.Name.Name` and the G3 and method-vs-function
// cases fail (a bare name collapses them onto one key).
func TestWriteSiteKeyIsInjectiveAcrossFuncDecls(t *testing.T) {
	cases := []struct {
		name string
		src  string
		// funcKeys are the expected keys of the file's FuncDecls, in the order
		// they appear. A method's expected key is `Recv.Name`.
		funcKeys []string
		// declKeys maps a const/var NAME to the key its declaration must
		// produce: `decl <name>` outside any function, the enclosing FUNC's key
		// inside one (G2).
		declKeys map[string]string
	}{
		{
			// G3, the shape this card is about: the accessor and a second
			// implementation of the rule, distinguished only by receiver type.
			name: "G3: two methods share a name on different receivers",
			src: `package probe

import "context"

type UserStore struct{}

type Other struct{}

func (s *UserStore) ConsumeTOTPStep(ctx context.Context, userID string, step int64) (bool, error) {
	return true, nil
}

func (s *Other) ConsumeTOTPStep(ctx context.Context, userID string, step int64) (bool, error) {
	return true, nil
}
`,
			funcKeys: []string{"UserStore.ConsumeTOTPStep", "Other.ConsumeTOTPStep"},
		},
		{
			// The receiver VARIABLE is not part of a method's identity: `s`, `o`
			// and an unnamed receiver all name the same method, and a key that
			// embedded the variable would make the allowlist entry depend on a
			// name nobody vets (renaming the receiver would silently un-allow
			// the one legitimate write site).
			name: "the receiver variable is not part of the key",
			src: `package probe

type UserStore struct{}

func (s *UserStore) ConsumeTOTPStep() bool { return true }

func (_ *UserStore) Another() bool { return true }
`,
			funcKeys: []string{"UserStore.ConsumeTOTPStep", "UserStore.Another"},
		},
		{
			// G1, the axis PR 830 closed: a method and a package-level const may
			// share a name (different Go namespaces).
			name: "G1: a method and a package-level const share a name",
			src: `package probe

type UserStore struct{}

const ConsumeTOTPStep = "UPDATE users SET totp_last_step = $2"

func (s *UserStore) ConsumeTOTPStep() bool { return true }
`,
			funcKeys: []string{"UserStore.ConsumeTOTPStep"},
			declKeys: map[string]string{"ConsumeTOTPStep": "decl ConsumeTOTPStep"},
		},
		{
			name: "a method and a plain function share a name",
			src: `package probe

type UserStore struct{}

func ConsumeTOTPStep() bool { return true }

func (s *UserStore) ConsumeTOTPStep() bool { return true }
`,
			funcKeys: []string{"ConsumeTOTPStep", "UserStore.ConsumeTOTPStep"},
		},
		{
			// G2: a const INSIDE the accessor belongs to the accessor, so it
			// does not produce a second write site for the same statement — and
			// its key is the accessor's key, not its own name.
			name: "G2: a function-local const is keyed by its function",
			src: `package probe

type UserStore struct{}

func (s *UserStore) ConsumeTOTPStep() bool {
	const stepSQL = "UPDATE users SET totp_last_step = $2"
	return stepSQL != ""
}
`,
			funcKeys: []string{"UserStore.ConsumeTOTPStep"},
			declKeys: map[string]string{"stepSQL": "UserStore.ConsumeTOTPStep"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := parser.ParseFile(token.NewFileSet(), "probe.go", tc.src, 0)
			if err != nil {
				t.Fatalf("parse probe source: %v", err)
			}

			var funcs []*ast.FuncDecl
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				funcs = append(funcs, fn)
			}

			// Every declaration in the file, keyed the way the guard keys it. A
			// declaration INSIDE a function body is deliberately folded onto its
			// function's key (G2: the statement belongs to the function that
			// holds it, and a separate key both double-reported it and turned a
			// legitimate local const into an unlisted write site), so only
			// package-level declarations take part in injectivity.
			seen := map[string]string{}
			note := func(key, what string) {
				if prev, dup := seen[key]; dup {
					t.Errorf("site key %q is shared by %s and %s — the key must be injective across "+
						"every declaration in a file, or one inherits the other's allowlist entry",
						key, prev, what)
					return
				}
				seen[key] = what
			}
			for _, fn := range funcs {
				note(enclosingDecl(f, fn.Name), funcSiteKey(fn))
			}
			ast.Inspect(f, func(n ast.Node) bool {
				vs, ok := n.(*ast.ValueSpec)
				if !ok || len(vs.Names) == 0 {
					return true
				}
				if enclosingFuncDecl(f, vs.Names[0]) == nil {
					note(enclosingDecl(f, vs.Names[0]), "decl "+vs.Names[0].Name)
				}
				return true
			})

			var gotFuncs []string
			for _, fn := range funcs {
				gotFuncs = append(gotFuncs, enclosingDecl(f, fn.Name))
			}
			if strings.Join(gotFuncs, ",") != strings.Join(tc.funcKeys, ",") {
				t.Errorf("function keys = %v, want %v (source order)", gotFuncs, tc.funcKeys)
			}

			for name, want := range tc.declKeys {
				found := false
				ast.Inspect(f, func(n ast.Node) bool {
					vs, ok := n.(*ast.ValueSpec)
					if !ok || len(vs.Names) == 0 || vs.Names[0].Name != name {
						return true
					}
					found = true
					if got := enclosingDecl(f, vs.Names[0]); got != want {
						t.Errorf("key for declaration %s = %q, want %q", name, got, want)
					}
					return true
				})
				if !found {
					t.Errorf("no const/var named %q in the probe file", name)
				}
			}
		})
	}
}

// TestThinConsumerIsJudgedBySiteNotFile pins the rule that closes (H): a thin
// delegation to the store accessor is admissible only on a SITE somebody vetted,
// and the file it sits in buys it nothing.
//
// WHY THIS EXISTS SEPARATELY, the same reason TestWriteSiteKeyIsInjectiveAcrossFuncDecls
// exists: the tree-wide check only caught the probe because the api tree happens
// to contain a colliding pair — an allowlisted function in a file that a second
// function could hide behind. In-memory sources pinned here stay pinned when the
// tree no longer cooperates, and a change to the rule is exercised by both.
//
// Each assertion below is on the SITE the guard would name, for a file holding
// an allowed function beside an unvetted one. It runs on in-memory sources, so
// the property does not depend on the api tree containing a colliding pair.
//
// BITE: consult the FILE instead of the site — `if file == allowedFile { return
// false }` in unvettedThinConsumerSite — and the second case fails: the neighbour
// inherits the entry written for an allowed function beside it, which is the
// pre-fix shape exactly.
//
// BITE: drop the `callsStoreAccessor` test and the third case fails — an
// unrelated one-line helper in a file that merely mentions the accessor becomes
// a false red.
func TestThinConsumerIsJudgedBySiteNotFile(t *testing.T) {
	src := `package probe

import "context"

type Other struct{ Users *UserStore }

// The site someone vetted: a thin alias that delegates the accessor.
func (z *Other) handle2FAEnableAllowed(ctx context.Context, userID string, step int64) (bool, error) {
	return z.Users.ConsumeTOTPStep(ctx, userID, step)
}

// A THIN second consumer: same file, a site of its own, vetted by nobody.
func (z *Other) handle2FAEnable(ctx context.Context, userID string, step int64) (bool, error) {
	return z.Users.ConsumeTOTPStep(ctx, userID, step)
}

// Not a consumer at all: the returned call is not the accessor.
func decodeBase64(s string) string {
	return base64Std(s)
}

// Not a consumer either: it delegates the RULE, not the accessor.
func checkAndConsumeTOTP(ctx context.Context, userID string, code string) bool {
	return ConsumeCode(ctx, userID, code)
}
`
	const file = "api/internal/httpapi/auth.go"
	vettedSite := file + " (Other.handle2FAEnableAllowed)"
	neighbourSite := file + " (Other.handle2FAEnable)"

	allowed := func(site string) bool { return site == vettedSite }

	f, err := parser.ParseFile(token.NewFileSet(), "probe.go", src, 0)
	if err != nil {
		t.Fatalf("parse probe source: %v", err)
	}

	// reported maps a funcSiteKey to the site the guard names for it; a function
	// the guard clears is simply ABSENT (an entry with an empty value would make
	// "reported as nothing" indistinguishable from "not reported").
	reported := map[string]string{}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if site, bad := unvettedThinConsumerSite(file, fn, allowed); bad {
			reported[funcSiteKey(fn)] = site
		}
	}

	// The two consumers, judged apart: the vetted one clears, its neighbour does
	// not. A file-level decision cannot produce this pair.
	if got, bad := reported["Other.handle2FAEnableAllowed"]; bad {
		t.Errorf("the allowlisted site was reported as %q — an entry must admit the site it names", got)
	}
	if got := reported["Other.handle2FAEnable"]; got != neighbourSite {
		t.Errorf("the unvetted thin neighbour was judged %q, want %q — a file-level allowlist lets a "+
			"second consumer inherit the entry written for the function beside it", got, neighbourSite)
	}

	// A body whose return is NOT the accessor is not a consumer, whatever file
	// it lives in: reporting one would be a false red that teaches authors to
	// widen the allowlist.
	for _, key := range []string{"decodeBase64", "checkAndConsumeTOTP"} {
		if got, bad := reported[key]; bad {
			t.Errorf("%s was reported as a consumer (%q) — only a body whose return IS the store "+
				"accessor consumes a step", key, got)
		}
	}
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
