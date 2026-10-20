// Language-consistency guard: no transactional-email copy outside the shared
// dictionary, and every email declares the language it was rendered in.
//
// Why this exists. The pre-change repo had seven transactional emails with
// hardcoded Malay subjects and three that consulted no language at all, plus a
// branded wrapper that declared lang="ms" for every message. That is a bug
// CLASS, not a one-off: the next transactional email added would regress it
// silently, and the symptom (a Tamil user receiving English, or an English user
// receiving Malay) only shows up when a human reads a real inbox.
//
// So the rule is enforced structurally rather than remembered. Everything below
// is DERIVED FROM THE MODULE SOURCE with go/ast — no hand-kept list of senders
// that drifts as soon as someone adds one:
//
//  1. CONTENT — a `Subject:` / `Body:` value in an `emailqueue.Job` literal must
//     trace back to the shared dictionary. "Trace" is real: the analyzer follows
//     a local variable, a package-level const, and a function PARAMETER (resolved
//     across its call sites module-wide), because `subj := "Sahkan emel";
//     Subject: subj` is exactly how a new hardcoded email gets written, and a
//     check that only looked at inline literals would wave it through.
//  2. TRANSPORT — every email-carrying Job literal must carry a `Lang:`. Without
//     it the message is branded in the wrong language even when its text is
//     right, because the branded wrapper takes its <html lang="…"> from there.
//  3. RENDER — every send through a mailer must pass a resolved language as its
//     4th argument. The mailers themselves are recognised by TYPE, not by the
//     name of the field they are reached through: `email.Sender` (the receipts
//     path) and the `VerifyMailer` interface (the auth, invite and feedback
//     paths) are one branded wrapper, and the fields holding them happen to be
//     called `Sender`, `Mailer`, `VerifyMailer`, `AuthMailer`. A new field
//     NAME, a receiver reached through a local or a PACKAGE-LEVEL variable, or
//     a receiver that is itself a CALL (`Make().Send(…)`) must not fall out of
//     the guard — so the matcher keys on the declared type and on the email
//     send SIGNATURE, and a differently-named field holding a mailer is
//     examined exactly like the four that exist today. These are the calls that
//     hand text to the branded wrapper, and so the place where "what the
//     recipient reads" and "what the client is told it is" could disagree.
//  4. CENSUS — a matcher's SILENCE is not evidence. A receiver shape it has no
//     rule for is skipped without a word, so the guard would keep reporting
//     green over a send it never looked at; that is how three mailer-carrying
//     receiver shapes (a METHOD call, a slice/map INDEX and a TYPE ASSERTION)
//     stayed invisible while this file said all twenty sites pass. So every
//     send-shaped call the walk meets has its receiver SHAPE classified, and a
//     shape the matcher cannot decide — no rule for it, or a rule whose inputs
//     do not resolve — FAILS the guard and names the shape and the call site.
//     A new blind spot of this class is then a red run, not a green one.
//
// The analyzer reports anything it cannot resolve instead of skipping it, and it
// carries floors on what it found. A guard that silently stops covering its
// subject is worse than none, because it certifies the thing it does not check.
package httpapi

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// dictionaryPkgPath is the ONE package allowed to hold transactional-email copy.
const dictionaryPkgPath = "internal/emailtext"

// nonEmailJobTypes are the queue job types that are NOT email deliveries: they
// carry a JSON payload on Body for a background judge, not a message to a
// person. They are named explicitly, so a new non-email type is a deliberate
// addition rather than a silent exemption.
var nonEmailJobTypes = map[string]bool{
	// assetgate.JobType and spamjudge.JobType — both payload jobs whose Body is a
	// serialized request, never a message.
	"JobType": true,
}

// rendererFuncs are the functions that build a message out of a template the
// dictionary owns. Trusting one is only sound because its INPUT is a dictionary
// template (renderReceipt/renderReceiptBody are handed one by the receiver, and
// magicLinkEmailText/inviteEmailSubject/inviteEmailBody are thin wrappers over
// emailtext.Text — see their definitions). A general string-building helper is
// deliberately NOT trusted: doing so would let a sender hide its copy behind a
// function and defeat the guard entirely.
var rendererFuncs = map[string]bool{
	"renderReceipt":      true,
	"renderReceiptBody":  true,
	"renderNotification": true,
	"magicLinkEmailText": true,
	"inviteEmailSubject": true,
	"inviteEmailBody":    true,
}

// Floors. A scanner that matches nothing must fail loudly rather than report a
// clean module it never really examined.
const (
	minJobLiterals   = 8   // the repo builds 11 email-carrying Job literals
	minScannedFiles  = 100 // the api module has well over 200 non-test .go files
	minResolvedSites = 10  // email content sites that DO resolve through the dictionary
	// minSenderSites is the same idea for the render half: the repo sends
	// through 18 sites reached through a named field (`…Sender`, `…Mailer`,
	// `…VerifyMailer`, `…AuthMailer`) plus a receiver reached through a LOCAL
	// or a package-level variable, and 2 whose receiver is a bare identifier —
	// the sender's own internal delegations, `s.SendReceipt(…)` and
	// `s.SendReceiptWithAttachment(…)` in internal/email/ses.go, where the
	// receiver is the method receiver and carries no field name at all.
	// 20 is the current truth, kept as a floor: adding an email raises it, and a
	// matcher that loses part of its subject fails instead of going quiet.
	//
	// The 2 identifier-reached sites are load-bearing, and the floor has to
	// include them. The matcher this file replaced keyed on the FIELD NAME, so
	// it recognised exactly the 18 field-reached ones — which is what this floor
	// used to be, and a reversion to that matcher therefore scored 18 against a
	// floor of 18 and passed GREEN, certifying two sites it never looked at. A
	// floor is only a floor if it is the whole count the matcher recognises
	// today, not the count the narrowest matcher it replaced produced.
	minSenderSites = 20
)

// resolution is how an expression's value was produced.
type resolution int

const (
	resUnknown resolution = iota // not attributable — reported, never assumed fine
	resDict                      // rendered by the shared dictionary
	resLiteral                   // a string literal (or concatenation of them)
)

// resolvedValue pairs a resolution with the literal text, when there is one.
type resolvedValue struct {
	res   resolution
	value string
}

// site is one email-content location, for an error message.
type site struct {
	pos   string
	field string
	value string
	note  string
}

// senderSite is one recognised email send: the file it is written in (module
// relative) and the send method it calls. It exists so a particular site can be
// pinned BY NAME rather than only through the count — a line number would rot on
// any edit above it — and so a floor failure can say what WAS seen.
type senderSite struct {
	file   string
	method string
}

// funcInfo is one function's contribution to the analysis.
type funcInfo struct {
	decl   *ast.FuncDecl
	file   string
	params []string
	// locals maps a local variable name to how its current value was produced.
	// A later assignment overwrites the judgement, so a name reused for a
	// literal after a dictionary call is reported as a literal.
	locals map[string]resolvedValue
	// emailParams are the parameters used as a Job Subject/Body.
	emailParams map[string]bool
	// calls are the calls made from this function, for cross-function resolution.
	calls []callSite
	// mailerIdents are the identifiers in scope for this function whose current
	// value is a mailer: a parameter or method receiver DECLARED as a mailer
	// type, a local assigned from a mailer-valued expression, and a
	// PACKAGE-LEVEL variable of a mailer type (which is in scope for every
	// function in its package). It is what lets the matcher see
	// `m := h.Mailer; m.Send(…)` and `pkgMailer.Send(…)`, where the receiver is
	// an ident and carries no field name at all.
	mailerIdents map[string]bool
	// identTypes are the identifiers in scope for this function whose DECLARED
	// type the module can name: the method receiver and parameters, a local
	// declared or assigned from a typed expression, and the package-level
	// variables of the package. It is what lets a METHOD-call receiver
	// (`o.make().Send(…)`) be matched: `canonicalFuncName` cannot name `o.make`
	// — there is no package in it — so the method has to be found from the
	// receiving TYPE, and that type is only knowable from `o`'s declaration.
	identTypes map[string]typedExpr
}

// typedExpr is a type expression together with the file that declares it, so a
// type written `Mailer` in one file and `email.Sender` in another canonicalises
// to one name. Carrying the declaring file (rather than just the rendered name)
// is what lets a CONTAINER type be taken apart: the element of `[]Mailer` is
// only nameable once you know which file to resolve `Mailer` against.
type typedExpr struct {
	expr ast.Expr
	pf   *parsedFile
}

// canonical renders the type as `<package>.<Type>`, resolving that file's
// import aliases.
func (t typedExpr) canonical() (string, bool) {
	if t.pf == nil {
		return "", false
	}
	return canonicalTypeName(t.expr, t.pf)
}

// callSite is one call expression inside a function.
type callSite struct {
	callee string
	args   []ast.Expr
}

// analyzer holds the whole-module view.
type analyzer struct {
	fset *token.FileSet
	// root is the api module root, so a finding can name a file the way a
	// reader sees it in the repo.
	root        string
	inDictFile  map[string]bool
	funcsByName map[string][]*funcInfo
	allFuncs    []*funcInfo
	// resolvedParams caches a parameter's resolution, once computed.
	resolvedParams map[string]resolvedValue
	// parsed is every file the walker read. The type-based pass needs the whole
	// module at once (a field's declared type may be declared in another
	// package), so the ASTs are kept rather than streamed.
	parsed []*parsedFile
	byPath map[string]*parsedFile
	// mailers is the module-wide type picture: which TYPES carry the email-send
	// API, which struct fields hold one of them, and which calls produce one,
	// whatever those fields and functions are called. See buildMailerIndex.
	mailers *mailerIndex
	// pkgMailerVars are the package-scope variables whose value is a mailer,
	// keyed by the directory of the file that declares them (a package's
	// variables are in scope for every file of that package and for no file of
	// another). A package-scope `var m Mailer` is not a local, so the
	// per-function body walk never sees it — see markPkgMailerVars.
	pkgMailerVars map[string]map[string]bool
	// pkgVarTypes are the DECLARED types of the package-scope variables, keyed
	// the same way, so a method-call receiver reached through one
	// (`pkgOwner.owner.make().Send(…)`) can have its receiving type resolved.
	pkgVarTypes map[string]map[string]typedExpr

	// Findings.
	copyOutsideDict        []site
	unresolved             []site
	jobsWithoutLang        []site
	senderCallsWithoutLang []site
	// receiverCensus counts the send-shaped calls the walk met per receiver
	// SHAPE, and undecided are the ones the matcher had no rule for (or a rule
	// whose inputs did not resolve). The census exists because a matcher that
	// does not understand a shape says NOTHING: the count of recognised sites
	// does not move, no finding is filed, and the guard certifies a send it
	// never looked at. TestEverySendReceiverShapeIsDecided fails on these.
	receiverCensus map[recvShape]int
	undecided      []site
	dictSites      int
	emailJobCount  int
	filesScanned   int
	// senderSites counts the email sends the render matcher recognised. A
	// matcher that stops recognising a family reports nothing instead of
	// reporting a problem, so the count is floored in the test.
	senderSites int
	// senderSiteList is WHICH sends it recognised, as (module-relative file,
	// method). A count cannot tell "the receiver-reached sites were lost" from
	// "one was lost and a field-reached one was gained elsewhere", and it is the
	// first that has to fail — so the two sites the field-name-keyed matcher
	// could not see at all are asserted by name in the test as well.
	senderSiteList []senderSite
}

// isDictionaryFile reports whether a file belongs to the dictionary package, the
// one place allowed to hold email copy.
func isDictionaryFile(path string) bool {
	return strings.Contains(filepath.ToSlash(path), "/"+dictionaryPkgPath+"/")
}

// ---------------------------------------------------------------------------
// Pass 1: parse and index.
// ---------------------------------------------------------------------------

// newAnalyzer parses every non-test .go file in the api module and indexes its
// functions.
func newAnalyzer(t *testing.T) *analyzer {
	t.Helper()

	root, err := filepath.Abs(apiModuleDir)
	if err != nil {
		t.Fatalf("resolve %s: %v", apiModuleDir, err)
	}

	a := &analyzer{
		fset:           token.NewFileSet(),
		root:           root,
		inDictFile:     map[string]bool{},
		funcsByName:    map[string][]*funcInfo{},
		resolvedParams: map[string]resolvedValue{},
		byPath:         map[string]*parsedFile{},
		receiverCensus: map[recvShape]int{},
	}

	werr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == ".git" || name == "vendor" || name == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(a.fset, path, nil, 0)
		if perr != nil {
			// An unparsed file makes the whole analysis unsound.
			return perr
		}
		a.filesScanned++
		a.inDictFile[path] = isDictionaryFile(path)
		pf := newParsedFile(path, file)
		a.parsed = append(a.parsed, pf)
		a.byPath[path] = pf
		a.indexFile(path, file)
		return nil
	})
	if werr != nil {
		t.Fatalf("walk the api module: %v", werr)
	}
	return a
}

// indexFile records every function in a file.
func (a *analyzer) indexFile(path string, file *ast.File) {
	// Package-level string constants, so a const-valued body is resolvable.
	consts := map[string]resolvedValue{}
	for _, decl := range file.Decls {
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
				if i >= len(vs.Values) {
					continue
				}
				if v, ok := concatLiterals(vs.Values[i]); ok {
					if !a.inDictFile[path] && isEmailContentConst(name.Name, v) {
						a.copyOutsideDict = append(a.copyOutsideDict, site{
							pos: a.fset.Position(vs.Pos()).String(), field: "const " + name.Name,
							value: v, note: "package-level constant",
						})
					} else {
						a.dictSites++
					}
				}
			}
		}
	}

	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		fi := &funcInfo{
			decl:        fd,
			file:        path,
			locals:      map[string]resolvedValue{},
			emailParams: map[string]bool{},
		}
		// Seed the function's OWN locals with the file's package-level constants.
		// A fresh map per function is load-bearing: sharing one map across a
		// file's functions lets one function's assignment overwrite another's
		// judgement, so a sender would be judged on a same-named variable it
		// never touched.
		for k, v := range consts {
			fi.locals[k] = v
		}
		if fd.Type.Params != nil {
			for _, f := range fd.Type.Params.List {
				for _, n := range f.Names {
					fi.params = append(fi.params, n.Name)
				}
			}
		}
		a.allFuncs = append(a.allFuncs, fi)
		if fd.Name != nil {
			a.funcsByName[fd.Name.Name] = append(a.funcsByName[fd.Name.Name], fi)
		}
	}
}

// ---------------------------------------------------------------------------
// Pass 2: walk each function body.
// ---------------------------------------------------------------------------

// walk resolves every function body, then collects the findings.
//
// The two phases are separate because parameter resolution reads the CALLER's
// locals: if a function were fully processed before its callers' assignments were
// recorded, a parameter passed a dictionary value would look unresolvable purely
// because of iteration order. Phase 1 makes every function's locals and call
// sites final; phase 2 then resolves against that finished picture.
func (a *analyzer) walk() {
	// Phase 0: the module-wide TYPE picture. It has to be complete before any
	// function is judged, because whether a receiver is a mailer depends on a
	// type declaration that may live in another package.
	a.buildMailerIndex()
	// And the package-scope variables holding a mailer, because an ident
	// receiver is looked up in the scope of the function that reaches it.
	a.markPkgMailerVars()

	// Phase 1: assignments and call sites, for every function.
	for _, fi := range a.allFuncs {
		a.markIdentTypes(fi)
		a.markMailerIdents(fi)
		inDict := a.inDictFile[fi.file]
		ast.Inspect(fi.decl.Body, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.AssignStmt:
				a.recordAssign(fi, node, inDict)
			case *ast.ValueSpec:
				a.recordValueSpec(fi, node, inDict)
			case *ast.CallExpr:
				if callee := calledFunctionName(node); callee != "" {
					fi.calls = append(fi.calls, callSite{callee: callee, args: node.Args})
				}
			}
			return true
		})
	}

	// Phase 2: Job literals and email sends.
	for _, fi := range a.allFuncs {
		inDict := a.inDictFile[fi.file]
		ast.Inspect(fi.decl.Body, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CompositeLit:
				if isEmailQueueJobLitLocal(node) {
					a.checkJobLiteral(fi, node, inDict)
				}
			case *ast.CallExpr:
				verdict, isSend := a.classifySend(fi, node)
				if !isSend {
					return true
				}
				// EVERY send-shaped call is censused, matched or not: a
				// receiver shape the matcher has no rule for is a site it
				// silently skips, and skipping it is exactly the failure this
				// census turns loud.
				a.noteReceiverShape(fi, node, verdict)
				if !verdict.matched {
					return true
				}
				a.senderSites++
				a.recordSenderSite(fi, node)
				if !passesLang(node) {
					a.senderCallsWithoutLang = append(a.senderCallsWithoutLang, site{
						pos: a.fset.Position(node.Pos()).String(), field: verdict.name,
						note: "no language argument",
					})
				}
			}
			return true
		})
	}
}

// recordAssign resolves a `x := expr` / `x = expr` statement.
func (a *analyzer) recordAssign(fi *funcInfo, stmt *ast.AssignStmt, inDict bool) {
	vals := make([]resolvedValue, 0, len(stmt.Rhs))
	for _, rhs := range stmt.Rhs {
		vals = append(vals, a.resolve(fi, rhs, inDict, 0))
	}
	// A single right-hand side feeding several names (a multi-return call) gives
	// each name the same judgement; anything else pairs positionally.
	for i, lhs := range stmt.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}
		v := vals[0]
		if len(vals) > 1 && i < len(vals) {
			v = vals[i]
		}
		fi.locals[id.Name] = v
	}
}

// recordValueSpec resolves a `var x = expr` declaration.
func (a *analyzer) recordValueSpec(fi *funcInfo, spec *ast.ValueSpec, inDict bool) {
	if len(spec.Values) == 0 {
		return
	}
	for _, name := range spec.Names {
		fi.locals[name.Name] = a.resolve(fi, spec.Values[0], inDict, 0)
	}
}

// resolve evaluates an expression to a resolution. depth bounds the recursion
// through parameters, so a recursive chain cannot spin.
func (a *analyzer) resolve(fi *funcInfo, expr ast.Expr, inDict bool, depth int) resolvedValue {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return resolvedValue{res: resUnknown}
		}
		if inDict {
			return resolvedValue{res: resDict}
		}
		v, err := strconv.Unquote(e.Value)
		if err != nil {
			return resolvedValue{res: resUnknown}
		}
		return resolvedValue{res: resLiteral, value: v}

	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return resolvedValue{res: resUnknown}
		}
		l := a.resolve(fi, e.X, inDict, depth)
		r := a.resolve(fi, e.Y, inDict, depth)
		// A concatenation is as literal as its most literal part — the
		// `emailtext.Phrase(lang, …) + subj` shape in the merchant copy is
		// dictionary on both sides, and a literal on either side is a failure.
		if l.res == resLiteral {
			return l
		}
		if r.res == resLiteral {
			return r
		}
		if l.res == resDict && r.res == resDict {
			return resolvedValue{res: resDict}
		}
		return resolvedValue{res: resUnknown}

	case *ast.CallExpr:
		if dictCallName(e) != "" {
			return resolvedValue{res: resDict}
		}
		if name := calledFunctionName(e); name != "" && rendererFuncs[name] {
			return resolvedValue{res: resDict}
		}
		return resolvedValue{res: resUnknown}

	case *ast.Ident:
		if v, ok := fi.locals[e.Name]; ok {
			return v
		}
		// A parameter: resolve it across every call site of this function.
		if a.isParam(fi, e.Name) {
			return a.resolveParam(fi.decl.Name.Name, e.Name, depth)
		}
		return resolvedValue{res: resUnknown}

	case *ast.SelectorExpr:
		// `msg.Subject` — the field of a value the file assigned from the
		// dictionary. Both fields of a dictionary Message are dictionary text.
		if id, ok := e.X.(*ast.Ident); ok {
			if v, found := fi.locals[id.Name]; found {
				return v
			}
		}
		return resolvedValue{res: resUnknown}
	}
	return resolvedValue{res: resUnknown}
}

// isParam reports whether name is one of a function's parameters.
func (a *analyzer) isParam(fi *funcInfo, name string) bool {
	for _, p := range fi.params {
		if p == name {
			return true
		}
	}
	return false
}

// resolveParam resolves a function parameter across every call site of that
// function in the module. It returns resLiteral if ANY caller passes a literal
// (one literal call site is enough to make the parameter a source of hardcoded
// copy), resDict only when every call site is dictionary-derived, and resUnknown
// when a call site cannot be resolved or none was found — so an unanalysable
// shape is reported rather than assumed clean.
func (a *analyzer) resolveParam(funcName, paramName string, depth int) resolvedValue {
	if depth > 4 {
		return resolvedValue{res: resUnknown}
	}
	key := funcName + "." + paramName
	if r, ok := a.resolvedParams[key]; ok {
		return r
	}
	// Mark in progress so a recursive chain terminates.
	a.resolvedParams[key] = resolvedValue{res: resUnknown}

	idx := -1
	var target *funcInfo
	for _, fi := range a.funcsByName[funcName] {
		for i, p := range fi.params {
			if p == paramName {
				idx, target = i, fi
				break
			}
		}
		if target != nil {
			break
		}
	}
	if target == nil {
		return resolvedValue{res: resUnknown}
	}

	seen := 0
	anyUnknown := false
	for _, caller := range a.allFuncs {
		if a.inDictFile[caller.file] {
			continue
		}
		for _, call := range caller.calls {
			if call.callee != funcName || len(call.args) <= idx {
				continue
			}
			seen++
			v := a.resolve(caller, call.args[idx], a.inDictFile[caller.file], depth+1)
			switch v.res {
			case resLiteral:
				// One literal call site is enough: the parameter carries
				// hardcoded copy. Report the literal so the failure names it.
				a.resolvedParams[key] = v
				return v
			case resUnknown:
				anyUnknown = true
			}
		}
	}
	switch {
	case seen == 0 || anyUnknown:
		a.resolvedParams[key] = resolvedValue{res: resUnknown}
	default:
		a.resolvedParams[key] = resolvedValue{res: resDict}
	}
	return a.resolvedParams[key]
}

// checkJobLiteral inspects one emailqueue.Job literal.
func (a *analyzer) checkJobLiteral(fi *funcInfo, lit *ast.CompositeLit, inDict bool) {
	pos := a.fset.Position(lit.Pos())
	jobType := jobTypeAt(lit)
	payloadJob := nonEmailJobTypes[jobType]

	hasEmail, hasLang := false, false
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Subject", "Body":
			if payloadJob {
				// A judge/gate payload is not a message to a person.
				continue
			}
			hasEmail = true
			a.checkJobValue(fi, pos, key.Name, kv.Value, jobType, inDict)
		case "Lang":
			hasLang = true
		}
	}
	if !hasEmail || payloadJob {
		return
	}
	a.emailJobCount++
	if !hasLang {
		a.jobsWithoutLang = append(a.jobsWithoutLang, site{
			pos: pos.String(), field: "Lang", note: "Type: " + jobType,
		})
	}
}

// checkJobValue resolves one Subject/Body expression and files the finding.
func (a *analyzer) checkJobValue(
	fi *funcInfo, pos token.Position, field string, expr ast.Expr, jobType string, inDict bool,
) {
	v := a.resolve(fi, expr, inDict, 0)
	switch v.res {
	case resDict:
		a.dictSites++
	case resLiteral:
		a.copyOutsideDict = append(a.copyOutsideDict, site{
			pos: pos.String(), field: field, value: v.value,
			note: "Job literal (Type: " + jobType + ")",
		})
	default:
		a.unresolved = append(a.unresolved, site{
			pos: pos.String(), field: field, value: exprStringRender(expr),
			note: "Job literal (Type: " + jobType + ")",
		})
	}
}

// ---------------------------------------------------------------------------
// Shared AST helpers.
// ---------------------------------------------------------------------------

// isEmailQueueJobLitLocal accepts `emailqueue.Job{…}` plus the bare `Job{…}` the
// queue package itself writes.
func isEmailQueueJobLitLocal(lit *ast.CompositeLit) bool {
	if isEmailQueueJobLit(lit) {
		return true
	}
	if id, ok := lit.Type.(*ast.Ident); ok {
		return id.Name == "Job"
	}
	return false
}

// jobTypeAt reports a Job literal's Type expression for an error message.
func jobTypeAt(lit *ast.CompositeLit) string {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Type" {
			return exprStringRender(kv.Value)
		}
	}
	return "?"
}

// concatLiterals folds a string literal or a +-concatenation of them.
func concatLiterals(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", false
		}
		return v, true
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		l, lok := concatLiterals(e.X)
		r, rok := concatLiterals(e.Y)
		if !lok || !rok {
			return "", false
		}
		return l + r, true
	}
	return "", false
}

// dictCallName returns the callee for a call whose result comes from the shared
// dictionary, or "" when the call is not one.
func dictCallName(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "emailtext" {
		return ""
	}
	switch sel.Sel.Name {
	case "Text", "Phrase", "LegacyDefault":
		return "emailtext." + sel.Sel.Name
	}
	return ""
}

// calledFunctionName returns the bare name a call goes by, so a site can be
// matched against rendererFuncs and a function's own declaration.
func calledFunctionName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

// ---------------------------------------------------------------------------
// Which receivers are mailers — decided by TYPE, never by field name.
// ---------------------------------------------------------------------------

// sendMethodNames are the methods that hand rendered text to the branded
// wrapper: the entry points to the mailer API, not a naming rule for whatever
// the mailer happens to be reached through.
var sendMethodNames = map[string]bool{
	"Send":                      true,
	"SendReceipt":               true,
	"SendWithAttachment":        true,
	"SendReceiptWithAttachment": true,
}

// parsedFile is one parsed source file plus the facts the type pass needs in
// order to name a type the same way from every file: its package clause and the
// local alias of each import.
type parsedFile struct {
	path        string
	file        *ast.File
	pkgName     string
	importNames map[string]string
}

// newParsedFile records a file's package name and import aliases, so a type
// written `em.Sender` in one file and `email.Sender` in another still
// canonicalises to ONE name.
func newParsedFile(path string, f *ast.File) *parsedFile {
	pf := &parsedFile{
		path:        path,
		file:        f,
		pkgName:     f.Name.Name,
		importNames: map[string]string{},
	}
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		canonical := p
		if i := strings.LastIndex(canonical, "/"); i >= 0 {
			canonical = canonical[i+1:]
		}
		alias := canonical
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		if alias == "_" || alias == "." || alias == "" {
			continue
		}
		pf.importNames[alias] = canonical
	}
	return pf
}

// canonicalTypeName renders a type expression as `<package>.<Type>`, resolving
// the file's import aliases. A type declared in the file's OWN package is
// qualified with that package's name, which is what makes a field's declared
// type comparable with the type's declaration in a different file.
func canonicalTypeName(expr ast.Expr, pf *parsedFile) (string, bool) {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return canonicalTypeName(e.X, pf)
	case *ast.ParenExpr:
		return canonicalTypeName(e.X, pf)
	case *ast.IndexExpr:
		return canonicalTypeName(e.X, pf)
	case *ast.SelectorExpr:
		id, ok := e.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		name := pf.importNames[id.Name]
		if name == "" {
			name = id.Name
		}
		return name + "." + e.Sel.Name, true
	case *ast.Ident:
		return pf.pkgName + "." + e.Name, true
	}
	return "", false
}

// mailerIndex is the module-wide TYPE picture the render matcher works from.
type mailerIndex struct {
	// types are the canonical names of the types that CARRY the mailer API:
	// `email.Sender` and the `VerifyMailer` interface. Nothing here mentions a
	// field name.
	types map[string]bool
	// declared are the canonical names of EVERY type the module declares, so a
	// receiver type that is not in here is out of scope by construction (an
	// imported type's methods are not the module's to guard) rather than
	// merely unfound.
	declared map[string]bool
	// pkgs are the packages declaring one of those types, so a constructor in
	// such a package (`email.New`) is recognised as producing a mailer.
	pkgs map[string]bool
	// fieldNames are the struct field names whose DECLARED TYPE is one of those
	// types — `Sender`, `Mailer`, `VerifyMailer`, `AuthMailer` today, and
	// whatever a newly added one is called tomorrow.
	fieldNames map[string]bool
	// fieldTypes are the struct field names with the DECLARED type of every
	// field of that name in the module, so the type of `h.Anything` can be
	// resolved and a method called on the value it holds (`h.Anything.make()`)
	// can be found. A name declared with two different types is ambiguous and
	// resolves to nothing, which the census reports rather than guesses at.
	fieldTypes map[string][]typedExpr
	// funcResults maps a canonically-named FUNCTION (`httpapi.Make`,
	// `email.New`) to the types the module DECLARES it returns. A call used as
	// the receiver of a send — `Make().Send(…)` — carries no field name and no
	// local's identity, so the callee's declared result type is the only thing
	// that can identify it.
	funcResults map[string][]typedExpr
	// methodResults is the same for a METHOD, keyed
	// `<ReceivingType>.<Method>` (`httpapi.qaProbeOwner.make`). A method-call
	// receiver (`o.make().Send(…)`) has no package in its callee — `o.make` is
	// never a key in funcResults, whose keys are `<package>.<Name>` — so the
	// method has to be found from the receiving type.
	methodResults map[string][]typedExpr
}

// mailerType reports whether a declared type name is one of the module's own
// mailer types.
func (a *analyzer) mailerType(t typedExpr) bool {
	key, ok := t.canonical()
	return ok && a.mailers.types[key]
}

// resultsCarryMailer reports whether any of a callee's DECLARED results is a
// mailer type — the test a call receiver (`Make().Send(…)`) is matched on.
func (a *analyzer) resultsCarryMailer(results []typedExpr) bool {
	for _, res := range results {
		if a.mailerType(res) {
			return true
		}
	}
	return false
}

// singleResult is a callee's declared result when it has exactly one, so a
// local assigned from a call (`o := Make()`, `list := Mailers()`) has a type
// the module can name.
func singleResult(results []typedExpr) (typedExpr, bool) {
	if len(results) != 1 {
		return typedExpr{}, false
	}
	return results[0], true
}

// buildMailerIndex derives the mailer types, their packages, and every struct
// field holding one, from the module source.
func (a *analyzer) buildMailerIndex() {
	a.mailers = &mailerIndex{
		types:         map[string]bool{},
		declared:      map[string]bool{},
		pkgs:          map[string]bool{},
		fieldNames:    map[string]bool{},
		fieldTypes:    map[string][]typedExpr{},
		funcResults:   map[string][]typedExpr{},
		methodResults: map[string][]typedExpr{},
	}
	for _, pf := range a.parsed {
		for _, decl := range pf.file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Name == nil {
					continue
				}
				// What this declaration's RESULT names, so a call used as the
				// receiver of a send (`Make().Send(…)`) can be recognised even
				// though it carries no field name and no local's identity. The
				// key carries the declaring PACKAGE (or, for a method, the
				// RECEIVING TYPE), because a bare `New` means `payments.New` in
				// one file and `email.New` in another.
				if d.Type.Results != nil {
					var results []typedExpr
					for _, res := range d.Type.Results.List {
						if _, ok := canonicalTypeName(res.Type, pf); !ok {
							continue
						}
						results = append(results, typedExpr{expr: res.Type, pf: pf})
					}
					if len(results) > 0 {
						if d.Recv != nil && len(d.Recv.List) > 0 {
							// A METHOD: reachable only through a receiving type,
							// so it is keyed on that type and never on a package
							// (`o.make` is not `<pkg>.make`).
							if recvType, ok := canonicalTypeName(d.Recv.List[0].Type, pf); ok {
								key := recvType + "." + d.Name.Name
								a.mailers.methodResults[key] = append(a.mailers.methodResults[key], results...)
							}
						} else {
							key := pf.pkgName + "." + d.Name.Name
							a.mailers.funcResults[key] = append(a.mailers.funcResults[key], results...)
						}
					}
				}
				// A method on a named type that declares an email-shaped send.
				if d.Recv == nil || len(d.Recv.List) == 0 ||
					!sendMethodNames[d.Name.Name] || !emailSendShape(d.Type.Params) {
					continue
				}
				if key, ok := canonicalTypeName(d.Recv.List[0].Type, pf); ok {
					a.mailers.types[key] = true
				}
			case *ast.GenDecl:
				// An interface that requires an email-shaped send, and — for
				// every type the module declares — the canonical name, so a
				// receiver type not in `declared` is known to be imported.
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					a.mailers.declared[pf.pkgName+"."+ts.Name.Name] = true
					it, ok := ts.Type.(*ast.InterfaceType)
					if !ok || it.Methods == nil || !interfaceSendsEmail(it) {
						continue
					}
					a.mailers.types[pf.pkgName+"."+ts.Name.Name] = true
				}
			}
		}
	}
	for key := range a.mailers.types {
		if i := strings.Index(key, "."); i > 0 {
			a.mailers.pkgs[key[:i]] = true
		}
	}
	// And now the FIELDS, keyed on the type they are declared with: a field is
	// a mailer field however it is named, as long as it holds a mailer. Every
	// field's type is recorded (not only the mailers') because a method called
	// on a field's value (`h.Anything.make()`) is looked up from the type that
	// field declares.
	for _, pf := range a.parsed {
		ast.Inspect(pf.file, func(n ast.Node) bool {
			st, ok := n.(*ast.StructType)
			if !ok || st.Fields == nil {
				return true
			}
			for _, f := range st.Fields.List {
				for _, name := range f.Names {
					if name.Name == "_" {
						continue
					}
					a.mailers.fieldTypes[name.Name] = append(
						a.mailers.fieldTypes[name.Name], typedExpr{expr: f.Type, pf: pf})
					if key, ok := canonicalTypeName(f.Type, pf); ok && a.mailers.types[key] {
						a.mailers.fieldNames[name.Name] = true
					}
				}
			}
			return true
		})
	}
}

// emailSendShape reports whether a method's parameter list is an email send: a
// recipient, a subject and a body, all three strings. This — a signature, not a
// receiver's name — is what keeps an unrelated `Send(ctx, job)` out of the
// guard.
func emailSendShape(params *ast.FieldList) bool {
	if params == nil {
		return false
	}
	var types []string
	for _, f := range params.List {
		n := len(f.Names)
		if n == 0 {
			n = 1 // an unnamed parameter still occupies a position
		}
		for i := 0; i < n; i++ {
			types = append(types, exprStringRender(f.Type))
		}
	}
	return len(types) >= 3 && types[0] == "string" && types[1] == "string" && types[2] == "string"
}

// interfaceSendsEmail reports whether an interface requires an email-shaped
// send method.
func interfaceSendsEmail(it *ast.InterfaceType) bool {
	for _, m := range it.Methods.List {
		ft, ok := m.Type.(*ast.FuncType)
		if !ok {
			continue
		}
		for _, name := range m.Names {
			if sendMethodNames[name.Name] && emailSendShape(ft.Params) {
				return true
			}
		}
	}
	return false
}

// markMailerIdents records the identifiers inside one function whose value is a
// mailer: a parameter or method receiver DECLARED as a mailer type, a
// package-scope variable of a mailer type (which is in scope here), and a local
// assigned from a mailer-valued expression. It resolves to a fixpoint, so
// `a := b; b := h.Mailer` and the same pair in the other order agree.
func (a *analyzer) markMailerIdents(fi *funcInfo) {
	fi.mailerIdents = map[string]bool{}
	pf := a.byPath[fi.file]
	if pf == nil {
		return
	}
	// The package's own mailers first: a package-scope `var m Mailer` is not a
	// local, so the body walk below would never see it, and `m.Send(…)` — an
	// ident receiver with no field name to key on — would go unexamined.
	for name := range a.pkgMailerVars[filepath.Dir(fi.file)] {
		fi.mailerIdents[name] = true
	}
	mark := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			key, ok := canonicalTypeName(f.Type, pf)
			if !ok || !a.mailers.types[key] {
				continue
			}
			for _, n := range f.Names {
				fi.mailerIdents[n.Name] = true
			}
		}
	}
	mark(fi.decl.Recv)
	mark(fi.decl.Type.Params)

	for round := 0; round < 4; round++ {
		changed := false
		ast.Inspect(fi.decl.Body, func(n ast.Node) bool {
			switch stmt := n.(type) {
			case *ast.AssignStmt:
				for i, lhs := range stmt.Lhs {
					id, ok := lhs.(*ast.Ident)
					if !ok || id.Name == "_" || fi.mailerIdents[id.Name] || len(stmt.Rhs) == 0 {
						continue
					}
					rhs := stmt.Rhs[0]
					if len(stmt.Rhs) > 1 && i < len(stmt.Rhs) {
						rhs = stmt.Rhs[i]
					}
					if a.isMailerExpr(fi, pf, rhs) {
						fi.mailerIdents[id.Name] = true
						changed = true
					}
				}
			case *ast.ValueSpec:
				for i, name := range stmt.Names {
					if name.Name == "_" || fi.mailerIdents[name.Name] {
						continue
					}
					mailer := false
					switch {
					case len(stmt.Values) > i:
						mailer = a.isMailerExpr(fi, pf, stmt.Values[i])
					case stmt.Type != nil:
						key, ok := canonicalTypeName(stmt.Type, pf)
						mailer = ok && a.mailers.types[key]
					}
					if mailer {
						fi.mailerIdents[name.Name] = true
						changed = true
					}
				}
			}
			return true
		})
		if !changed {
			break
		}
	}
}

// markPkgMailerVars records the PACKAGE-SCOPE variables whose value is a
// mailer, per declaring directory. A package-scope `var m Mailer` (or `var m =
// Make()`) is not a local, so markMailerIdents — which walks function bodies —
// never sees it, and `m.Send(…)` would fall out of the guard while the guard
// still reported green. It resolves to a fixpoint, so `var a = b; var b = Make()`
// and the same pair in the other order agree.
func (a *analyzer) markPkgMailerVars() {
	a.pkgMailerVars = map[string]map[string]bool{}
	a.pkgVarTypes = map[string]map[string]typedExpr{}
	for round := 0; round < 4; round++ {
		changed := false
		for _, pf := range a.parsed {
			dir := filepath.Dir(pf.path)
			// A declaration is judged against the package's own variables as
			// they stand (a package-scope initializer can only name another
			// package-scope value), which is what the fixpoint converges on.
			scope := a.pkgMailerScope(dir, pf)
			for _, decl := range pf.file.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.VAR {
					continue
				}
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, name := range vs.Names {
						if name.Name == "_" {
							continue
						}
						// The DECLARED type, whichever round this is: a
						// package-scope `var owner qaProbeOwner` is what a
						// method-call receiver reached through it has to be
						// resolved against, mailer or not.
						if t, ok := a.pkgVarDeclType(scope, pf, vs, i); ok {
							if a.pkgVarTypes[dir] == nil {
								a.pkgVarTypes[dir] = map[string]typedExpr{}
							}
							if _, seen := a.pkgVarTypes[dir][name.Name]; !seen {
								a.pkgVarTypes[dir][name.Name] = t
							}
						}
						if a.pkgMailerVars[dir][name.Name] {
							continue
						}
						mailer := false
						switch {
						case len(vs.Values) > i:
							mailer = a.isMailerExpr(scope, pf, vs.Values[i])
						case vs.Type != nil:
							key, ok := canonicalTypeName(vs.Type, pf)
							mailer = ok && a.mailers.types[key]
						}
						if !mailer {
							continue
						}
						if a.pkgMailerVars[dir] == nil {
							a.pkgMailerVars[dir] = map[string]bool{}
						}
						a.pkgMailerVars[dir][name.Name] = true
						scope.mailerIdents[name.Name] = true
						changed = true
					}
				}
			}
		}
		if !changed {
			break
		}
	}
}

// pkgMailerScope is a funcInfo-shaped view of what is already known to be a
// mailer in one package, so isMailerExpr can judge a package-scope initializer
// that names another package-scope variable.
func (a *analyzer) pkgMailerScope(dir string, pf *parsedFile) *funcInfo {
	fi := &funcInfo{file: pf.path, mailerIdents: map[string]bool{}}
	for name := range a.pkgMailerVars[dir] {
		fi.mailerIdents[name] = true
	}
	return fi
}

// isMailerExpr reports whether an expression's value is a mailer: a field whose
// declared type is one, an identifier already known to hold one, a composite
// literal or conversion of the type itself, a call whose declared result type is
// one, or a constructor of the package that declares it.
func (a *analyzer) isMailerExpr(fi *funcInfo, pf *parsedFile, expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		if _, ok := e.X.(*ast.Ident); ok && a.mailers.fieldNames[e.Sel.Name] {
			return true
		}
		key, ok := canonicalTypeName(e, pf) // a qualified type: email.Sender(x)
		return ok && a.mailers.types[key]
	case *ast.Ident:
		return fi.mailerIdents[e.Name]
	case *ast.CompositeLit:
		key, ok := canonicalTypeName(e.Type, pf)
		return ok && a.mailers.types[key]
	case *ast.CallExpr:
		if a.callProducesMailer(pf, e) {
			return true // a function whose declared result IS a mailer
		}
		if key, ok := canonicalTypeName(e.Fun, pf); ok && a.mailers.types[key] {
			return true // a conversion
		}
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && a.mailers.pkgs[pf.importNames[id.Name]] {
				return true // a constructor in the mailer's own package
			}
		}
		return false
	case *ast.UnaryExpr:
		return a.isMailerExpr(fi, pf, e.X)
	case *ast.ParenExpr:
		return a.isMailerExpr(fi, pf, e.X)
	}
	return false
}

// canonicalFuncName renders a call's callee the way canonicalTypeName renders a
// type — `<package>.<Name>`, with the call site's import aliases resolved — so
// the declaration's result type can be looked up from any file. A callee that is
// not a plain name (a method on a value, say) has no package to key on and is
// reported unresolved rather than guessed at.
func canonicalFuncName(pf *parsedFile, fun ast.Expr) (string, bool) {
	if pf == nil {
		return "", false
	}
	switch f := fun.(type) {
	case *ast.Ident:
		return pf.pkgName + "." + f.Name, true
	case *ast.SelectorExpr:
		id, ok := f.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		name := pf.importNames[id.Name]
		if name == "" {
			name = id.Name
		}
		return name + "." + f.Sel.Name, true
	}
	return "", false
}

// callProducesMailer reports whether a call's value is a mailer, so that a
// receiver that IS a call (`Make().Send(…)`) can be matched: such a receiver
// carries neither a field name nor a local's identity, and the callee's DECLARED
// result type is the only thing that identifies it. The result type is matched
// against the types the module declares as mailers, so `SomeOther().Send(ctx,
// job)` — a Send that is not an email send — still does not match.
func (a *analyzer) callProducesMailer(pf *parsedFile, call *ast.CallExpr) bool {
	key, ok := canonicalFuncName(pf, call.Fun)
	if !ok {
		return false
	}
	return a.resultsCarryMailer(a.mailers.funcResults[key])
}

// pkgVarDeclType is the declared type of one package-scope variable, when the
// declaration names one — explicitly (`var owner qaProbeOwner`) or through a
// composite literal / single-result call initialiser.
func (a *analyzer) pkgVarDeclType(
	scope *funcInfo, pf *parsedFile, vs *ast.ValueSpec, i int,
) (typedExpr, bool) {
	if vs.Type != nil {
		if _, ok := canonicalTypeName(vs.Type, pf); !ok {
			return typedExpr{}, false
		}
		return typedExpr{expr: vs.Type, pf: pf}, true
	}
	if len(vs.Values) <= i {
		return typedExpr{}, false
	}
	return a.exprType(scope, pf, vs.Values[i], 0)
}

// markIdentTypes records the DECLARED type of the identifiers in one function
// whose type the module can name: the method receiver and parameters, a local
// declared or assigned from a typed expression, and the package-level variables
// of the package (which are in scope here).
//
// It exists for one purpose: a METHOD-call receiver (`o.make().Send(…)`) is not
// nameable from its callee — `o.make` has no package in it — so the method has
// to be found from the receiving TYPE, and that type is only knowable from `o`'s
// declaration. One forward pass is enough, because a Go function body declares a
// name before it uses it.
func (a *analyzer) markIdentTypes(fi *funcInfo) {
	fi.identTypes = map[string]typedExpr{}
	pf := a.byPath[fi.file]
	if pf == nil {
		return
	}
	for name, t := range a.pkgVarTypes[filepath.Dir(fi.file)] {
		fi.identTypes[name] = t
	}
	mark := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			for _, n := range f.Names {
				if n.Name != "_" {
					fi.identTypes[n.Name] = typedExpr{expr: f.Type, pf: pf}
				}
			}
		}
	}
	mark(fi.decl.Recv)
	mark(fi.decl.Type.Params)

	ast.Inspect(fi.decl.Body, func(n ast.Node) bool {
		switch stmt := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range stmt.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || id.Name == "_" || len(stmt.Rhs) == 0 {
					continue
				}
				rhs := stmt.Rhs[0]
				if len(stmt.Rhs) > 1 {
					if i >= len(stmt.Rhs) {
						continue
					}
					rhs = stmt.Rhs[i]
				}
				if t, ok := a.exprType(fi, pf, rhs, 0); ok {
					fi.identTypes[id.Name] = t
				}
			}
		case *ast.ValueSpec:
			for i, name := range stmt.Names {
				switch {
				case stmt.Type != nil:
					fi.identTypes[name.Name] = typedExpr{expr: stmt.Type, pf: pf}
				case len(stmt.Values) > i:
					if t, ok := a.exprType(fi, pf, stmt.Values[i], 0); ok {
						fi.identTypes[name.Name] = t
					}
				}
			}
		}
		return true
	})
}

// exprType names the type of a VALUE expression, when the module can: an
// identifier with a declared type, a container INDEX (the element of a slice,
// array or map), a TYPE ASSERTION (the asserted type), a composite literal of a
// named type, a call with exactly one declared result, and a struct FIELD.
//
// It returns false rather than guessing. Every caller turns a false into an
// UNDECIDED receiver shape, which the census fails on — so an unnamed type is a
// red run that asks for a rule, never a silently skipped site.
func (a *analyzer) exprType(
	fi *funcInfo, pf *parsedFile, expr ast.Expr, depth int,
) (typedExpr, bool) {
	if pf == nil || depth > 4 {
		return typedExpr{}, false
	}
	switch e := unparen(expr).(type) {
	case *ast.Ident:
		t, ok := fi.identTypes[e.Name]
		return t, ok
	case *ast.CompositeLit:
		if _, ok := canonicalTypeName(e.Type, pf); !ok {
			return typedExpr{}, false
		}
		return typedExpr{expr: e.Type, pf: pf}, true
	case *ast.TypeAssertExpr:
		if e.Type == nil {
			return typedExpr{}, false
		}
		if _, ok := canonicalTypeName(e.Type, pf); !ok {
			return typedExpr{}, false
		}
		return typedExpr{expr: e.Type, pf: pf}, true
	case *ast.IndexExpr:
		container, ok := a.exprType(fi, pf, e.X, depth+1)
		if !ok {
			return typedExpr{}, false
		}
		if element, ok := elementType(container); ok {
			return element, true
		}
		// A generic instantiation used as a value (`Box[T]{}`): the named type
		// is what canonicalTypeName already folds an IndexExpr to.
		if _, ok := canonicalTypeName(e, pf); !ok {
			return typedExpr{}, false
		}
		return typedExpr{expr: e, pf: pf}, true
	case *ast.SelectorExpr:
		return a.fieldType(e.Sel.Name)
	case *ast.CallExpr:
		key, ok := canonicalFuncName(pf, e.Fun)
		if !ok {
			return typedExpr{}, false
		}
		return singleResult(a.mailers.funcResults[key])
	}
	return typedExpr{}, false
}

// elementType takes a CONTAINER type apart: the element of a slice or array, or
// the value of a map. That is the declared type of `list[0]` and `m[k]`.
func elementType(t typedExpr) (typedExpr, bool) {
	switch e := t.expr.(type) {
	case *ast.ArrayType:
		return typedExpr{expr: e.Elt, pf: t.pf}, true
	case *ast.MapType:
		return typedExpr{expr: e.Value, pf: t.pf}, true
	}
	return typedExpr{}, false
}

// fieldType returns a struct field's declared type when EVERY field of that name
// in the module declares the SAME type. A name declared with two different types
// is ambiguous and resolves to nothing — the census then reports the call rather
// than the guard guessing which struct was meant.
func (a *analyzer) fieldType(name string) (typedExpr, bool) {
	var (
		first typedExpr
		seen  string
		n     int
	)
	for _, t := range a.mailers.fieldTypes[name] {
		key, ok := t.canonical()
		if !ok {
			continue
		}
		if n == 0 {
			first, seen = t, key
		} else if key != seen {
			return typedExpr{}, false
		}
		n++
	}
	if n == 0 {
		return typedExpr{}, false
	}
	return first, true
}

// recordSenderSite notes WHICH send was recognised — its file and the method it
// calls — so a failure can say what the matcher did see and a test can pin a
// particular site by name.
func (a *analyzer) recordSenderSite(fi *funcInfo, call *ast.CallExpr) {
	method := ""
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		method = sel.Sel.Name
	}
	a.senderSiteList = append(a.senderSiteList, senderSite{
		file:   a.moduleRelative(fi.file),
		method: method,
	})
}

// sawSenderSite reports whether one particular send was recognised.
func (a *analyzer) sawSenderSite(want senderSite) bool {
	for _, s := range a.senderSiteList {
		if s == want {
			return true
		}
	}
	return false
}

// moduleRelative names a file the way a reader sees it in the repo, so a finding
// can be pasted into a shell.
func (a *analyzer) moduleRelative(path string) string {
	if a.root == "" {
		return filepath.ToSlash(path)
	}
	rel, err := filepath.Rel(a.root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// recvShape names the SYNTACTIC form of a send's receiver. It exists so the
// census can say WHICH shape the matcher had no rule for: "nothing matched" is
// not a diagnosis, and a matcher's silence is not evidence.
type recvShape string

const (
	shapeIdent      recvShape = "Ident (a variable)"
	shapeField      recvShape = "SelectorExpr (a field)"
	shapePkgCall    recvShape = "CallExpr (a package-level function)"
	shapeMethodCall recvShape = "CallExpr (a method)"
	shapeIndex      recvShape = "IndexExpr (slice/array/map element)"
	shapeTypeAssert recvShape = "TypeAssertExpr"
	shapeOther      recvShape = "unsupported receiver expression"
)

// sendVerdict is what the matcher concluded about one call whose selector name
// is a send method.
//
// `decided` is the load-bearing field, and it is separate from `matched` on
// purpose. A matcher with no rule for a shape — or with a rule whose inputs the
// module cannot name — knows NOTHING about that call, and the guard has to say
// so rather than quietly count it as "not an email send". Conflating the two is
// what let three mailer-carrying receiver shapes sit outside the guard while it
// reported every site passing.
type sendVerdict struct {
	method  string
	shape   recvShape
	decided bool
	matched bool
	name    string // "…Receiver.Method", for a finding
	blind   string // why a shape could not be decided
}

// unparen strips redundant parentheses, so `(o).make()` classifies like
// `o.make()`.
func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// classifySend recognises an email send through a mailer — the four methods
// that hand text to the branded wrapper — and reports WHICH receiver shape it
// saw, whether it could decide it, and whether the receiver is a mailer.
//
// The receiver is recognised by its TYPE, not by the name of the field it is
// reached through. `email.Sender` (the receipts path) and the `VerifyMailer`
// interface (the auth, invite and feedback paths) are the same branded wrapper,
// and the fields holding them are called `Sender`, `Mailer`, `VerifyMailer` and
// `AuthMailer` — but a matcher that keyed on those names would wave through a
// field called `InviteSender`, or a receiver reached through a local variable
// (which carries no field name at all), while still reporting green.
//
// So every shape a mailer can be reached through has a rule here, and the rule
// is applied to the DECLARED type rather than to any name:
//
//   - a field (`h.Mailer`) — the field's declared type;
//   - an identifier (`m`, `pkgMailer`, the method receiver `s`) — whether the
//     function's scope marks it as holding a mailer;
//   - a call (`Make().Send(…)`, `o.make().Send(…)`) — the callee's declared
//     result type, found from the package for a FUNCTION and from the receiving
//     type for a METHOD (`o.make` carries no package, so it can never be a key
//     in funcResults);
//   - an index (`list[0].Send(…)`, `m[k].Send(…)`) — the container's declared
//     ELEMENT type;
//   - a type assertion (`held.(Mailer).Send(…)`) — the asserted type.
//
// An unrelated type's Send is still not swept in: `w.handler.Send(ctx, job)`
// fails the email signature (its first parameter is not a string), so `Handler`
// is not a mailer type and `handler` is not a mailer field.
func (a *analyzer) classifySend(fi *funcInfo, call *ast.CallExpr) (sendVerdict, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !sendMethodNames[sel.Sel.Name] {
		return sendVerdict{}, false
	}
	v := sendVerdict{method: sel.Sel.Name}
	pf := a.byPath[fi.file]
	if pf == nil {
		v.shape = shapeOther
		v.blind = "the file is not in the index"
		return v, true
	}

	switch recv := unparen(sel.X).(type) {
	case *ast.Ident:
		if _, isPkg := pf.importNames[recv.Name]; isPkg {
			// `pkg.Send(…)` is a package-level FUNCTION, not a method on a
			// value: there is no receiver type holding the mailer API, so this
			// guard's subject (a send THROUGH a mailer) does not apply. It is
			// decided, and it is not a mailer send.
			v.shape = shapePkgCall
			v.decided = true
			v.name = "…" + recv.Name + "." + sel.Sel.Name
			return v, true
		}
		v.shape = shapeIdent
		v.decided = true
		v.matched = fi.mailerIdents[recv.Name]
		v.name = "…" + recv.Name + "." + sel.Sel.Name
		return v, true

	case *ast.SelectorExpr:
		v.shape = shapeField
		v.decided = true
		v.matched = a.mailers.fieldNames[recv.Sel.Name]
		v.name = "…" + recv.Sel.Name + "." + sel.Sel.Name
		return v, true

	case *ast.CallExpr:
		return a.classifyCallReceiver(fi, pf, recv, sel, v), true

	case *ast.IndexExpr:
		// `list[0].Send(…)` / `m[k].Send(…)`: the receiver is an ELEMENT, so
		// the mailer type lives in the container's declared element type.
		v.shape = shapeIndex
		v.name = "…" + exprStringRender(recv) + "." + sel.Sel.Name
		container, ok := a.exprType(fi, pf, recv.X, 0)
		if !ok {
			v.blind = "the type of " + exprStringRender(recv.X) + " is not nameable here, so the " +
				"ELEMENT type it indexes cannot be checked"
			return v, true
		}
		element, ok := elementType(container)
		if !ok {
			v.blind = exprStringRender(recv.X) + " is not declared as a slice, array or map, so it " +
				"has no element type to check"
			return v, true
		}
		key, ok := element.canonical()
		if !ok {
			v.blind = "the element type of " + exprStringRender(recv.X) + " is not a nameable type"
			return v, true
		}
		v.decided = true
		v.matched = a.mailers.types[key]
		return v, true

	case *ast.TypeAssertExpr:
		// `held.(Mailer).Send(…)`: the asserted type IS the receiver's type.
		v.shape = shapeTypeAssert
		v.name = "…" + exprStringRender(recv) + "." + sel.Sel.Name
		if recv.Type == nil {
			v.blind = "a type-switch guard (.(type)) has no single asserted type"
			return v, true
		}
		key, ok := canonicalTypeName(recv.Type, pf)
		if !ok {
			v.blind = "the asserted type " + exprStringRender(recv.Type) + " is not a nameable type"
			return v, true
		}
		v.decided = true
		v.matched = a.mailers.types[key]
		return v, true
	}

	v.shape = shapeOther
	v.blind = "no rule for a " + exprStringRender(unparen(sel.X)) + " receiver"
	return v, true
}

// classifyCallReceiver decides a call used as a receiver: `Make().Send(…)`,
// `o.make().Send(…)`, `email.New(x).Send(…)`.
func (a *analyzer) classifyCallReceiver(
	fi *funcInfo, pf *parsedFile, callee *ast.CallExpr, sel *ast.SelectorExpr, v sendVerdict,
) sendVerdict {
	v.name = "…" + callReceiverName(callee) + "." + sel.Sel.Name

	// `F(…)` / `pkg.F(…)` — a package-level FUNCTION (the bare form is this
	// file's own package). Its declared result type is what says whether it
	// produces a mailer, and it is decided by the same isMailerExpr test a local
	// assignment is, so a receiver shape cannot be recognised by one half of the
	// guard and missed by the other.
	switch fun := callee.Fun.(type) {
	case *ast.Ident:
		v.shape = shapePkgCall
		v.decided = true
		v.matched = a.isMailerExpr(fi, pf, callee)
		return v
	case *ast.SelectorExpr:
		if id, ok := fun.X.(*ast.Ident); ok {
			if _, isPkg := pf.importNames[id.Name]; isPkg {
				v.shape = shapePkgCall
				v.decided = true
				v.matched = a.isMailerExpr(fi, pf, callee)
				return v
			}
		}
	default:
		v.shape = shapeOther
		v.blind = "no rule for a call whose callee is " + exprStringRender(callee.Fun)
		return v
	}
	fun := callee.Fun.(*ast.SelectorExpr)

	// `value.method(…)` — a METHOD call. Its callee carries no package, so the
	// method has to be found from the RECEIVING TYPE: `o.make` is never a key
	// in funcResults, whose keys are `<package>.<Name>`.
	v.shape = shapeMethodCall
	recvType, ok := a.exprType(fi, pf, fun.X, 0)
	if !ok {
		v.blind = "the receiving type of the method " + exprStringRender(fun) + " is not nameable here"
		return v
	}
	recvKey, ok := recvType.canonical()
	if !ok {
		v.blind = "the receiving type of the method " + exprStringRender(fun) + " is not a nameable type"
		return v
	}
	if !a.mailers.declared[recvKey] {
		// The method belongs to an IMPORTED type. A mailer is only ever a type
		// the module declares (that is how every mailer here is found), so this
		// is decided — and it is not a mailer.
		v.decided = true
		return v
	}
	results, ok := a.mailers.methodResults[recvKey+"."+fun.Sel.Name]
	if !ok {
		// Declared in the module but no such method: a PROMOTED method through
		// an embedded field, which this index does not follow. Undecided, so it
		// is reported rather than assumed harmless.
		v.blind = "the module declares no method " + recvKey + "." + fun.Sel.Name + " (a promoted method?)"
		return v
	}
	v.decided = true
	v.matched = a.resultsCarryMailer(results)
	return v
}

// noteReceiverShape censuses one send-shaped call: it counts the receiver SHAPE
// it met, and files the ones it could not decide. Called for EVERY send-shaped
// call, matched or not — a site the matcher did not match must still be a site
// it UNDERSTOOD, or the guard is certifying more than it checked.
func (a *analyzer) noteReceiverShape(fi *funcInfo, call *ast.CallExpr, v sendVerdict) {
	if a.receiverCensus == nil {
		a.receiverCensus = map[recvShape]int{}
	}
	a.receiverCensus[v.shape]++
	if v.decided {
		return
	}
	recv := "?"
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		recv = exprStringRender(sel.X)
	}
	a.undecided = append(a.undecided, site{
		pos:   a.fset.Position(call.Pos()).String(),
		field: string(v.shape) + " receiver " + recv + "." + v.method,
		note:  v.blind,
	})
}

// callReceiverName renders a call used as a RECEIVER for an error message, e.g.
// `qaProbeMake()` in `…qaProbeMake().Send`. The arguments are elided because
// the finding already names the file and line.
func callReceiverName(call *ast.CallExpr) string {
	return exprStringRender(call.Fun) + "()"
}

// passesLang reports whether a sender call supplies a language. All four senders
// take it as the 4th argument (to, subject, body, lang), so a missing argument or
// an empty literal is what this looks for.
func passesLang(call *ast.CallExpr) bool {
	const langArg = 3 // 0-based: to=0, subject=1, body=2, lang=3
	if len(call.Args) <= langArg {
		return false
	}
	if lit, ok := call.Args[langArg].(*ast.BasicLit); ok && lit.Kind == token.STRING {
		v, err := strconv.Unquote(lit.Value)
		if err == nil && strings.TrimSpace(v) == "" {
			return false
		}
	}
	return true
}

// exprStringRender is this file's expression renderer, named apart from the
// job-type guard's exprString to keep the two scanners independent.
//
// A receiver a finding names has to be recognisable in the source, so the
// shapes the matcher decides are rendered rather than elided: `list[0].Send` and
// `held.(Mailer).Send` say which send was missed, where "?" says nothing.
func exprStringRender(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.BasicLit:
		return x.Value
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprStringRender(x.X) + "." + x.Sel.Name
	case *ast.CallExpr:
		return exprStringRender(x.Fun) + "(…)"
	case *ast.BinaryExpr:
		return exprStringRender(x.X) + " " + x.Op.String() + " " + exprStringRender(x.Y)
	case *ast.IndexExpr:
		return exprStringRender(x.X) + "[" + exprStringRender(x.Index) + "]"
	case *ast.TypeAssertExpr:
		if x.Type == nil {
			return exprStringRender(x.X) + ".(type)"
		}
		return exprStringRender(x.X) + ".(" + exprStringRender(x.Type) + ")"
	case *ast.ParenExpr:
		return "(" + exprStringRender(x.X) + ")"
	case *ast.StarExpr:
		return "*" + exprStringRender(x.X)
	case *ast.ArrayType:
		if x.Len == nil {
			return "[]" + exprStringRender(x.Elt)
		}
		return "[" + exprStringRender(x.Len) + "]" + exprStringRender(x.Elt)
	case *ast.MapType:
		return "map[" + exprStringRender(x.Key) + "]" + exprStringRender(x.Value)
	case *ast.CompositeLit:
		return exprStringRender(x.Type) + "{…}"
	}
	return "?"
}

// isEmailContentConst reports whether a package-level const carries email copy
// rather than a dashboard label, slug, event name or URL.
//
// The name must say subject or body. A const called `…NotificationName` is the
// human-readable LABEL of a notification rule in the dashboard's rule list — it
// is never mailed — so matching on "notification" alone would flag a label as
// untranslated email copy.
func isEmailContentConst(name, val string) bool {
	lower := strings.ToLower(name)
	if !strings.Contains(lower, "subject") && !strings.Contains(lower, "body") {
		return false
	}
	// A body carrying {{placeholders}} is a receipt/notification template by
	// construction: only those are rendered by the placeholder engine.
	if strings.Contains(val, "{{") {
		return true
	}
	// A subject is prose: it has whitespace, a non-ASCII letter (a translation),
	// or is long enough not to be an identifier.
	return strings.ContainsAny(val, " \t") || hasNonASCII(val) || len([]rune(val)) >= 16
}

// hasNonASCII reports whether s contains a non-ASCII byte — i.e. a translated
// string, since the codebase's own identifiers are ASCII.
func hasNonASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// The guard itself.
// ---------------------------------------------------------------------------

// TestNoTransactionalEmailCopyOutsideTheDictionary is the rule: every piece of
// transactional-email content the module builds must come from the shared
// dictionary, so adding an email with its own hardcoded strings — inline, behind
// a local variable, behind a parameter, or in a package const — fails here
// instead of shipping.
func TestNoTransactionalEmailCopyOutsideTheDictionary(t *testing.T) {
	a := newAnalyzer(t)
	a.walk()

	if a.filesScanned < minScannedFiles {
		t.Fatalf("scanned only %d files (expected at least %d) — the walker is broken and this "+
			"guard would pass vacuously", a.filesScanned, minScannedFiles)
	}
	if a.emailJobCount < minJobLiterals {
		t.Fatalf("found only %d email-carrying emailqueue.Job literals (expected at least %d) — "+
			"the analyzer is broken, so this guard would pass vacuously",
			a.emailJobCount, minJobLiterals)
	}
	if a.dictSites < minResolvedSites {
		t.Fatalf("only %d email content site(s) resolved through the dictionary (expected at "+
			"least %d) — the resolver is broken", a.dictSites, minResolvedSites)
	}
	if len(a.unresolved) > 0 {
		t.Fatalf("the analyzer could not resolve %d email-content expression(s), so this guard "+
			"does not cover them. Teach analyzer.resolve the new shape:\n%s",
			len(a.unresolved), renderSites(a.unresolved))
	}
	if len(a.copyOutsideDict) > 0 {
		t.Fatalf("%d transactional-email string(s) are built outside %s.\n"+
			"Every transactional email must render through the shared dictionary "+
			"(emailtext.Text / emailtext.Phrase), so the language is decided in ONE place and "+
			"every locale is translated there:\n%s",
			len(a.copyOutsideDict), dictionaryPkgPath, renderSites(a.copyOutsideDict))
	}
	t.Logf("scanned %d files: %d email jobs, %d email content sites — all from %s",
		a.filesScanned, a.emailJobCount, a.dictSites, dictionaryPkgPath)
}

// TestEveryQueuedEmailDeclaresItsLanguage is the transport half. A queued email
// whose Lang is unset is branded with the wrong <html lang="…"> even when its
// text is right — the client is told the message is in a language it is not.
// "Every email carries the language it was rendered in" is only a checkable
// property if the field is actually populated, so it is asserted here.
func TestEveryQueuedEmailDeclaresItsLanguage(t *testing.T) {
	a := newAnalyzer(t)
	a.walk()

	if a.emailJobCount < minJobLiterals {
		t.Fatalf("found only %d email-carrying Job literals — the analyzer is broken", a.emailJobCount)
	}
	if len(a.jobsWithoutLang) > 0 {
		t.Fatalf("%d queued email(s) do not declare the language they were rendered in, so the "+
			"branded wrapper cannot emit a matching <html lang=\"…\">:\n%s",
			len(a.jobsWithoutLang), renderSites(a.jobsWithoutLang))
	}
	t.Logf("all %d email-carrying queued jobs declare Lang", a.emailJobCount)
}

// TestEveryEmailSendPassesAResolvedLanguage is the render half: the calls that
// hand rendered text to the branded wrapper must say which language it is in, or
// the wrapper defaults and the message declares the wrong language to the client.
//
// The floor below is what keeps this honest. An empty-language check is only as
// wide as its matcher, and the matcher once recognised one field name out of
// two: every `…Mailer` site was unchecked while the suite stayed green — and it
// then recognised only by NAME, so a mailer reached through a differently-named
// field, or through a local variable, was invisible. Counting the sites it DID
// recognise fails a matcher that goes narrow again — provided the floor is the
// WHOLE count the matcher recognises today, which is why it is 20 and not the 18
// the field-name-keyed matcher scored — even though a narrow matcher reports no
// findings at all. The floor cannot see a shape the matcher never considered
// (the count simply does not move for a site it does not understand), so the
// receiver shapes it must understand are asserted directly in
// TestEmailSendMatcherCoversEveryReceiverShape.
func TestEveryEmailSendPassesAResolvedLanguage(t *testing.T) {
	a := newAnalyzer(t)
	a.walk()

	if a.senderSites < minSenderSites {
		t.Fatalf("recognised only %d email send site(s) (expected at least %d) — the render "+
			"matcher has lost part of its subject (an `email.Sender` or a `VerifyMailer` "+
			"reached through ANY field name, through a local variable, or through the "+
			"sender's own method receiver, sends through the branded wrapper), so this guard "+
			"would pass over whatever it no longer matches. It recognised:\n%s",
			a.senderSites, minSenderSites, renderSenderSites(a.senderSiteList))
	}
	// The two receiver-reached sites: the sends whose receiver is a bare
	// identifier (the method receiver `s`) rather than a named field. They are
	// what the type-based matcher added to this guard's subject, and exactly what
	// the field-name-keyed matcher could not see at all, so they are pinned BY
	// NAME as well as through the count — the count alone cannot tell "both
	// receiver-reached sites were lost" from "one was lost and a field-reached
	// site was gained elsewhere", and it is the first that has to fail. The file
	// and the method are pinned, never the line: a line number moves on any edit
	// above it, and a guard that reddens for that reason stops being believed.
	pinned := []senderSite{
		{file: "internal/email/ses.go", method: "SendReceipt"},
		{file: "internal/email/ses.go", method: "SendReceiptWithAttachment"},
	}
	for _, want := range pinned {
		if !a.sawSenderSite(want) {
			t.Errorf("the render matcher no longer recognises %s %s(…) — that call's receiver "+
				"is the bare identifier `s`, the shape a field-name-keyed matcher cannot see, "+
				"and one of the two sites this guard would then certify as checked without "+
				"checking. Either the matcher went narrow again, or the delegation moved; if "+
				"it moved, move this expectation with it. It recognised:\n%s",
				want.file, want.method, renderSenderSites(a.senderSiteList))
		}
	}
	if len(a.senderCallsWithoutLang) > 0 {
		t.Fatalf("%d email send(s) pass no language to the branded wrapper:\n%s",
			len(a.senderCallsWithoutLang), renderSites(a.senderCallsWithoutLang))
	}
	t.Logf("all %d email send site(s) pass a language to the branded wrapper", a.senderSites)
}

// TestDictionaryOwnsAllNonASCIIEmailContent is a blunt, independent check on the
// same property the analyzer enforces: a non-ASCII letter in email content
// outside the dictionary is a TRANSLATED string that escaped it. It is a separate
// test so a resolution bug in the analyzer cannot hide it.
func TestDictionaryOwnsAllNonASCIIEmailContent(t *testing.T) {
	a := newAnalyzer(t)
	a.walk()

	var escaped []site
	for _, s := range a.copyOutsideDict {
		if hasNonASCII(s.value) {
			escaped = append(escaped, s)
		}
	}
	if len(escaped) > 0 {
		t.Fatalf("%d translated email string(s) live outside %s:\n%s",
			len(escaped), dictionaryPkgPath, renderSites(escaped))
	}
}

// ---------------------------------------------------------------------------
// The matcher's own coverage: one assertion per RECEIVER SHAPE.
// ---------------------------------------------------------------------------

// wantVerdict is one fixture call's expected classification: the receiver shape
// the matcher must report, whether it must be able to decide it, and whether the
// receiver must be recognised as a mailer.
type wantVerdict struct {
	shape   recvShape
	decided bool
	matched bool
}

// matcherFixture is source the matcher is pointed at directly. The module has no
// `Make().Send(…)`, no method-call, index or type-assertion receiver today, and a
// hole in a matcher is invisible exactly while it is unpopulated — which is how
// three of the shapes below went unnoticed while the render guard reported green
// on the real tree. So every shape it must understand is exercised here, on
// source whose only purpose is to be looked at, together with a receiver shape it
// must NOT understand yet: that one is what the census turns into a loud failure
// instead of a silently skipped site.
//
// The declaration ORDER is deliberate: qaProbePkgAlias is declared before the
// variable it aliases, so a matcher that only looked one step ahead would mark
// the alias only by accident.
const matcherFixture = `package httpapi

type qaProbeMailer struct{}

func (qaProbeMailer) Send(to, subject, body, lang string) error { return nil }

func qaProbeMake() qaProbeMailer { return qaProbeMailer{} }

var qaProbePkgAlias = qaProbePkgMailer

var qaProbePkgMailer qaProbeMailer

type qaProbeHolder struct {
	Anything qaProbeMailer
}

type qaProbeOwner struct {
	m qaProbeMailer
}

func (o qaProbeOwner) make() qaProbeMailer { return o.m }

func (o qaProbeOwner) other() qaProbeNotMailer { return qaProbeNotMailer{} }

var qaProbeOwnerVar qaProbeOwner

type qaProbeNotMailer struct{}

func (qaProbeNotMailer) Send(any, job int) error { return nil }

func qaProbeOther(ctx int) qaProbeNotMailer { return qaProbeNotMailer{} }

var qaProbeRunner qaProbeNotMailer

func qaProbeCalls(
	ctx int, o qaProbeOwner, list []qaProbeMailer, byName map[string]qaProbeMailer,
	plain []qaProbeNotMailer, held any,
) {
	_ = qaProbeMake().Send("a@example.test", "s", "b", "en")
	_ = qaProbePkgMailer.Send("a@example.test", "s", "b", "en")
	_ = qaProbePkgAlias.Send("a@example.test", "s", "b", "en")
	m := qaProbePkgMailer
	_ = m.Send("a@example.test", "s", "b", "en")
	k := qaProbeMake()
	_ = k.Send("a@example.test", "s", "b", "en")
	h := qaProbeHolder{}
	_ = h.Anything.Send("a@example.test", "s", "b", "en")
	_ = o.make().Send("a@example.test", "s", "b", "en")
	_ = qaProbeOwnerVar.make().Send("a@example.test", "s", "b", "en")
	_ = list[0].Send("a@example.test", "s", "b", "en")
	_ = byName["k"].Send("a@example.test", "s", "b", "en")
	_ = held.(qaProbeMailer).Send("a@example.test", "s", "b", "en")
	_ = h.Anything.make().Send("a@example.test", "s", "b", "en")
	_ = qaProbeRunner.Send(ctx, 1)
	_ = qaProbeOther(ctx).Send(ctx, 1)
	_ = o.other().Send(ctx, 1)
	_ = plain[0].Send(ctx, 1)
	_ = held.(qaProbeNotMailer).Send(ctx, 1)
}
`

// TestEverySendReceiverShapeIsDecided is the CENSUS. The render matcher above
// can only report on the shapes it has a rule for, and a shape it has no rule
// for produces no finding at all — the site simply is not counted, and the guard
// reports green over a mailer send it never examined. That is not hypothetical:
// a METHOD-call receiver, a slice/map INDEX and a TYPE ASSERTION all carried the
// mailer API and all fell through the switch, and this file certified twenty
// sites while the twenty-first was invisible.
//
// So every send-shaped call the walk meets is classified, and a shape that
// cannot be DECIDED fails here: either the matcher has no rule for it, or the
// rule needs a type the module cannot name. The floor keeps the census itself
// honest — a census that classified nothing would pass vacuously.
func TestEverySendReceiverShapeIsDecided(t *testing.T) {
	a := newAnalyzer(t)
	a.walk()

	total := 0
	for _, n := range a.receiverCensus {
		total += n
	}
	if total < minSenderSites {
		t.Fatalf("the receiver census classified only %d send-shaped call(s) (expected at "+
			"least %d) — the census is broken, so this guard would pass vacuously over "+
			"every receiver shape it never classified", total, minSenderSites)
	}
	if len(a.undecided) > 0 {
		t.Fatalf("%d email send(s) have a receiver shape the render matcher cannot decide. "+
			"That is not a site it checked and cleared — it is a site it does not see, and "+
			"this guard would certify it as passing. Teach analyzer.classifySend the shape "+
			"(a case that answers decided=true and matched=…), or make the type it needs "+
			"nameable via exprType:\n%s",
			len(a.undecided), renderSites(a.undecided))
	}
	var shapes []string
	for shape, n := range a.receiverCensus {
		shapes = append(shapes, fmt.Sprintf("%s=%d", shape, n))
	}
	sort.Strings(shapes)
	t.Logf("the census decided all %d send-shaped call(s): %s", total, strings.Join(shapes, ", "))
}

// TestEmailSendMatcherCoversEveryReceiverShape is the guard on the guard. Every
// way a mailer can be reached — a field of any name, a local, a package-level
// variable, a package-function call, a METHOD call, a slice/map element, a type
// assertion — must be recognised as a send, and each shape must stay unmatched
// when the value it reaches is NOT an email mailer:
//
//   - a call whose declared result type has a `Send`, but not an EMAIL one
//     (`qaProbeOther(ctx).Send(ctx, 1)` — the `Dispatch`/`Runner` negative
//     control, in package-function form);
//   - the same in METHOD form (`o.other().Send(ctx, 1)`), through a container
//     (`plain[0].Send(ctx, 1)`) and through a type assertion
//     (`held.(qaProbeNotMailer).Send(ctx, 1)`);
//   - a package-level variable of that same non-email type.
//
// One shape is DELIBERATELY undecidable — `h.Anything.make()`, a method that
// `Anything`'s declared type does not declare — because that is the state the
// census has to turn into a loud failure rather than a silent skip. The test
// asserts it comes back decided=false AND that the census names it, so the
// tripwire cannot rot into a comment.
//
// The expectation map is keyed on the call as the scanner renders it, and it is
// asserted in BOTH directions: a shape the matcher stops recognising fails, and
// so does a fixture call nobody declared an expectation for.
func TestEmailSendMatcherCoversEveryReceiverShape(t *testing.T) {
	want := map[string]wantVerdict{
		// Reached through a call / a field / an identifier.
		"qaProbeMake(…).Send":   {shapePkgCall, true, true}, // a package-function call receiver
		"qaProbePkgMailer.Send": {shapeIdent, true, true},   // a package-level variable
		"qaProbePkgAlias.Send":  {shapeIdent, true, true},   // a package var aliasing another
		"m.Send":                {shapeIdent, true, true},   // a local from a package var
		"k.Send":                {shapeIdent, true, true},   // a local from a call
		"h.Anything.Send":       {shapeField, true, true},   // a field, whatever it is called
		// Reached through a METHOD call, an INDEX and a TYPE ASSERTION — the
		// three shapes that were invisible before this card.
		"o.make(…).Send":               {shapeMethodCall, true, true}, // a method on a parameter
		"qaProbeOwnerVar.make(…).Send": {shapeMethodCall, true, true}, // a method on a package var
		"list[0].Send":                 {shapeIndex, true, true},      // a slice element
		`byName["k"].Send`:             {shapeIndex, true, true},      // a map value
		"held.(qaProbeMailer).Send":    {shapeTypeAssert, true, true}, // a type assertion
		// The undecidable shape: no method `make` on Anything's declared type.
		"h.Anything.make(…).Send": {shapeMethodCall, false, false},
		// Negative controls: a Send that is not an EMAIL send must not match,
		// in every shape the matcher now decides.
		"qaProbeRunner.Send":           {shapeIdent, true, false},
		"qaProbeOther(…).Send":         {shapePkgCall, true, false},
		"o.other(…).Send":              {shapeMethodCall, true, false},
		"plain[0].Send":                {shapeIndex, true, false},
		"held.(qaProbeNotMailer).Send": {shapeTypeAssert, true, false},
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "qa_matcher_fixture.go", matcherFixture, 0)
	if err != nil {
		t.Fatalf("parse the matcher fixture: %v", err)
	}
	pf := newParsedFile("qa_matcher_fixture.go", file)
	a := &analyzer{
		fset:           fset,
		inDictFile:     map[string]bool{},
		funcsByName:    map[string][]*funcInfo{},
		resolvedParams: map[string]resolvedValue{},
		byPath:         map[string]*parsedFile{pf.path: pf},
		parsed:         []*parsedFile{pf},
		receiverCensus: map[recvShape]int{},
	}
	a.buildMailerIndex()
	a.markPkgMailerVars()

	var infos []*funcInfo
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		fi := &funcInfo{
			decl:        fd,
			file:        pf.path,
			locals:      map[string]resolvedValue{},
			emailParams: map[string]bool{},
		}
		a.markIdentTypes(fi)
		a.markMailerIdents(fi)
		infos = append(infos, fi)
	}

	// call is keyed the way exprStringRender renders a callee.
	got := map[string]sendVerdict{}
	for _, fi := range infos {
		ast.Inspect(fi.decl.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			verdict, isSend := a.classifySend(fi, call)
			if !isSend {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				t.Errorf("a send-shaped call whose callee is not a selector: %s",
					exprStringRender(call.Fun))
				return true
			}
			key := exprStringRender(sel.X) + "." + sel.Sel.Name
			got[key] = verdict
			// The census is the tripwire for a shape the matcher cannot decide,
			// so it is exercised on the same verdict the walk would have filed.
			a.noteReceiverShape(fi, call, verdict)
			return true
		})
	}

	for key, want := range want {
		verdict, ok := got[key]
		if !ok {
			t.Errorf("the fixture call %s was not collected at all — the fixture and this "+
				"test have drifted apart", key)
			continue
		}
		if verdict.shape != want.shape {
			t.Errorf("%s: the matcher classifies the receiver as %q, want %q",
				key, verdict.shape, want.shape)
		}
		if verdict.decided != want.decided {
			t.Errorf("%s: the matcher reports decided=%v, want %v (%s)",
				key, verdict.decided, want.decided, verdict.blind)
		}
		if verdict.matched != want.matched {
			t.Errorf("%s: the matcher reports matched=%v (%q), want %v",
				key, verdict.matched, verdict.name, want.matched)
		}
		if want.matched && verdict.name == "" {
			t.Errorf("%s: matched but named nothing — a finding has to say WHICH send it is", key)
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("the fixture call %s has no expectation in this test — a new receiver "+
				"shape must be declared, not assumed covered", key)
		}
	}

	// The census must be able to SAY what it could not decide: one undecided
	// call, named with its receiver shape and the call site.
	if len(a.undecided) != 1 {
		t.Fatalf("the census filed %d undecided receiver shape(s), want exactly 1:\n%s",
			len(a.undecided), renderSites(a.undecided))
	}
	msg := renderSites(a.undecided)
	for _, sub := range []string{string(shapeMethodCall), "h.Anything.make", "promoted method"} {
		if !strings.Contains(msg, sub) {
			t.Errorf("the census finding does not mention %q, so it would not tell a reader "+
				"what to teach the matcher:\n%s", sub, msg)
		}
	}
	if a.receiverCensus[shapeMethodCall] != 4 {
		t.Errorf("the census counted %d METHOD-call receivers, want 4 (o.make, "+
			"qaProbeOwnerVar.make, h.Anything.make, o.other) — the census is what makes a "+
			"lost shape visible, so it has to count every shape it meets",
			a.receiverCensus[shapeMethodCall])
	}
}

// renderSenderSites formats the sends the matcher recognised for a failure
// message, so a floor failure says what WAS seen rather than only the number.
func renderSenderSites(sites []senderSite) string {
	lines := make([]string, 0, len(sites))
	for _, s := range sites {
		lines = append(lines, "  "+s.file+"  "+s.method)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// renderSites formats findings deterministically for an error message.
func renderSites(sites []site) string {
	lines := make([]string, 0, len(sites))
	for _, s := range sites {
		line := "  " + s.pos + "  " + s.field
		if s.value != "" {
			line += " = " + strconv.Quote(s.value)
		}
		if s.note != "" {
			line += "   [" + s.note + "]"
		}
		lines = append(lines, line)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
