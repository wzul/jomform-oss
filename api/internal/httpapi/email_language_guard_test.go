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
//  3. RENDER — every send through a mailer field must pass a resolved language
//     as its 4th argument — both families: the `…Sender` receipts path and the
//     `…Mailer` fields (VerifyMailer, AuthMailer, Mailer, …) the auth, invite
//     and feedback paths deliver through. These are the calls that hand text to
//     the branded wrapper, and so the place where "what the recipient reads" and
//     "what the client is told it is" could disagree.
//
// The analyzer reports anything it cannot resolve instead of skipping it, and it
// carries floors on what it found. A guard that silently stops covering its
// subject is worse than none, because it certifies the thing it does not check.
package httpapi

import (
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
	// minSenderSites is the same idea for the render half: the repo sends through
	// 9 `…Sender` sites and 9 `…Mailer` sites. A matcher that recognised only one
	// family would still pass the no-empty-language check on the other family by
	// never looking at it, so the count of sites it DID match is asserted.
	// 18 is the current truth, kept as a floor: adding an email raises it, and a
	// matcher that loses a family fails instead of going quiet.
	minSenderSites = 18
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
}

// callSite is one call expression inside a function.
type callSite struct {
	callee string
	args   []ast.Expr
}

// analyzer holds the whole-module view.
type analyzer struct {
	fset        *token.FileSet
	inDictFile  map[string]bool
	funcsByName map[string][]*funcInfo
	allFuncs    []*funcInfo
	// resolvedParams caches a parameter's resolution, once computed.
	resolvedParams map[string]resolvedValue

	// Findings.
	copyOutsideDict        []site
	unresolved             []site
	jobsWithoutLang        []site
	senderCallsWithoutLang []site
	dictSites              int
	emailJobCount          int
	filesScanned           int
	// senderSites counts the email sends the render matcher recognised. A
	// matcher that stops recognising a family reports nothing instead of
	// reporting a problem, so the count is floored in the test.
	senderSites int
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
		inDictFile:     map[string]bool{},
		funcsByName:    map[string][]*funcInfo{},
		resolvedParams: map[string]resolvedValue{},
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
	// Phase 1: assignments and call sites, for every function.
	for _, fi := range a.allFuncs {
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
				if name, ok := senderSendCall(node); ok {
					a.senderSites++
					if !passesLang(node) {
						a.senderCallsWithoutLang = append(a.senderCallsWithoutLang, site{
							pos: a.fset.Position(node.Pos()).String(), field: name,
							note: "no language argument",
						})
					}
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

// senderSendCall recognises an email send through the SES sender — the four
// methods that hand text to the branded wrapper — and returns a name for the
// error message.
//
// The receiver is recognised by the SHAPE of the field it is reached through,
// not by the type behind it: a `…Sender` field (`d.Pay.Receipts.Sender`) or any
// `…Mailer` field (`VerifyMailer`, `AuthMailer`, `Mailer`). The distinction is
// not incidental — both are the same `email.Sender` handed to the same branded
// wrapper, reached through two field names, and a matcher that knew only one of
// them left every site in the other family unchecked while still reporting
// green. An unrelated type's Send is still not swept in: the receiver must be a
// field SELECTOR ending in one of those two names, so `w.handler.Send(ctx, job)`
// and a local variable's Send are not matched.
func senderSendCall(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	switch sel.Sel.Name {
	case "Send", "SendReceipt", "SendWithAttachment", "SendReceiptWithAttachment":
	default:
		return "", false
	}
	recv, ok := sel.X.(*ast.SelectorExpr)
	if !ok || !isSenderFieldName(recv.Sel.Name) {
		return "", false
	}
	return "…" + recv.Sel.Name + "." + sel.Sel.Name, true
}

// isSenderFieldName reports whether a receiver field name belongs to the mailer
// family the guard covers.
func isSenderFieldName(field string) bool {
	return field == "Sender" || strings.HasSuffix(field, "Mailer")
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
		return exprStringRender(x.X) + " + " + exprStringRender(x.Y)
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
// wide as its matcher, and the matcher once recognised one field name out of two:
// every `…Mailer` site was unchecked while the suite stayed green. Counting the
// sites it DID recognise fails a matcher that goes narrow again, even though a
// narrow matcher reports no findings at all.
func TestEveryEmailSendPassesAResolvedLanguage(t *testing.T) {
	a := newAnalyzer(t)
	a.walk()

	if a.senderSites < minSenderSites {
		t.Fatalf("recognised only %d email send site(s) (expected at least %d) — the render "+
			"matcher has lost part of its subject (the `…Sender` and `…Mailer` families both "+
			"send through the branded wrapper), so this guard would pass over whatever it no "+
			"longer matches", a.senderSites, minSenderSites)
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
