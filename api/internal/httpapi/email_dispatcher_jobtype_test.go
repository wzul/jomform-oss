// Job-type guard + the form_notification / connect routing tests.
//
// Why the guard exists: (*EmailDispatcher).Send used to answer an unrecognised
// type with a logged warning and a nil return. A job type that was enqueued but
// never routed was therefore discarded with no error, no retry and no
// dead-letter entry — and no metric moved. That is exactly how
// "form_notification" (enqueued by payments/notifications.go) and "connect"
// (enqueued by httpapi/social.go) came to be silently dropped while both call
// sites looked live and their synchronous siblings worked.
//
// The guard has two halves, and both are load-bearing:
//
//  1. STATIC — scan every non-test .go file in the api module for
//     `emailqueue.Job{...}` literals and collect the `Type:` value: a string
//     literal, a same-package const, or a `pkg.JobType` const; and scan
//     (*EmailDispatcher).Send for the values in its switch `case` list. The two
//     sets must match. This half works even when `default:` returns nil, which
//     is the pre-fix shape — a behavioural-only check would be blind there,
//     because the nil return means nothing can be observed from outside. A Type
//     expression the scanner cannot evaluate is REPORTED, not skipped, so a new
//     construction shape surfaces instead of quietly shrinking the guard. A
//     floor on the extracted count stops a broken scanner from turning the whole
//     thing into a no-op that always passes.
//  2. BEHAVIOURAL — feed each collected type to the REAL dispatcher (wired with
//     no senders, where every handled case is nil-safe) and require that it does
//     not answer errUnknownJobType. This is the runtime back-stop for a case
//     label whose body still discards the job.
//
// So deleting a case from the switch fails this test with the enqueuing file
// named, and adding an enqueue site without a matching case fails it too.
//
// Known limit: a job whose Type is computed at runtime (not a literal or a
// const) is invisible to the static half's enqueue scan. No such site exists
// today; the unresolved-expression report is what makes a new one visible.
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/payments"
)

// apiModuleDir is the Go module root, relative to this package's directory
// (Go runs tests with the package dir as the working directory).
const apiModuleDir = "../.."

// minEnqueuedJobTypes is a floor on how many distinct types extraction must
// find. The repo enqueues 12; 10 is deliberately loose so an ordinary removal
// does not trip it, but a scanner that matches nothing (and would therefore
// pass vacuously) does.
const minEnqueuedJobTypes = 10

// jobTypeScan is the result of the static half.
type jobTypeScan struct {
	// types maps an enqueued job type to the files that enqueue it.
	types map[string][]string
	// handled is the set of switch-case values in (*EmailDispatcher).Send.
	handled map[string]bool
	// unresolved records `Type:` expressions the scanner could not evaluate,
	// so an unsupported construction shape fails loudly rather than being
	// silently dropped from the guard's coverage.
	unresolved []string
}

// parsedGoFile is one parsed source file, kept so the second pass can resolve
// identifiers against the file's own package.
type parsedGoFile struct {
	path string
	pkg  string
	file *ast.File
}

// scanEnqueuedJobTypes walks the api module and collects every job type passed
// to an emailqueue.Job composite literal in non-test code.
func scanEnqueuedJobTypes(t *testing.T) *jobTypeScan {
	t.Helper()

	root, err := filepath.Abs(apiModuleDir)
	if err != nil {
		t.Fatalf("resolve %s: %v", apiModuleDir, err)
	}

	var files []parsedGoFile
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "vendor", ".git", "deploy":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", path, perr)
		}
		files = append(files, parsedGoFile{path: path, pkg: f.Name.Name, file: f})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(files) == 0 {
		t.Fatalf("parsed no Go files under %s — the scanner is broken", root)
	}

	// Pass 1: top-level string consts, keyed by package name, so a `pkg.JobType`
	// or bare `JobType` in a Type field can be resolved to its value.
	consts := map[string]map[string]string{}
	for _, pf := range files {
		if consts[pf.pkg] == nil {
			consts[pf.pkg] = map[string]string{}
		}
		for _, decl := range pf.file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
					continue
				}
				lit, ok := vs.Values[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				val, verr := strconv.Unquote(lit.Value)
				if verr != nil {
					continue
				}
				consts[pf.pkg][vs.Names[0].Name] = val
			}
		}
	}

	// Pass 2: the job literals themselves.
	scan := &jobTypeScan{types: map[string][]string{}}
	for _, pf := range files {
		rel := pf.path
		if r, rerr := filepath.Rel(root, pf.path); rerr == nil {
			rel = filepath.ToSlash(r)
		}
		ast.Inspect(pf.file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || !isEmailQueueJobLit(lit) {
				return true
			}
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok || key.Name != "Type" {
					continue
				}
				val, ok := jobTypeValue(kv.Value, pf.pkg, consts)
				if !ok {
					scan.unresolved = append(scan.unresolved, fmt.Sprintf(
						"%s: Type: <%s>", rel, exprString(kv.Value)))
					continue
				}
				scan.types[val] = append(scan.types[val], rel)
			}
			return true
		})
	}
	for typ, srcs := range scan.types {
		scan.types[typ] = dedupePreserveOrder(srcs)
	}
	// Pass 3: the switch cases in (*EmailDispatcher).Send, so the guard compares
	// the handled set against the enqueued set directly. This is the half that
	// still bites when `default:` returns nil (the pre-fix shape).
	handled := map[string]bool{}
	for _, pf := range files {
		if filepath.Base(pf.path) != "email_dispatcher.go" {
			continue
		}
		for _, decl := range pf.file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || fd.Name.Name != "Send" || fd.Body == nil {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				cc, ok := n.(*ast.CaseClause)
				if !ok {
					return true
				}
				for _, e := range cc.List { // a nil List is `default:`
					// jobTypeValue resolves both a bare const in this package
					// and a `pkg.JobType` const from another one.
					val, ok := jobTypeValue(e, pf.pkg, consts)
					if !ok {
						scan.unresolved = append(scan.unresolved, fmt.Sprintf(
							"email_dispatcher.go: case <%s>", exprString(e)))
						continue
					}
					handled[val] = true
				}
				return true
			})
		}
	}
	scan.handled = handled
	return scan
}

// isEmailQueueJobLit reports whether a composite literal is `emailqueue.Job{...}`.
func isEmailQueueJobLit(lit *ast.CompositeLit) bool {
	sel, ok := lit.Type.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Job" {
		return false
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	return ok && pkgIdent.Name == "emailqueue"
}

// jobTypeValue evaluates a `Type:` expression to its string value.
func jobTypeValue(expr ast.Expr, pkg string, consts map[string]map[string]string) (string, bool) {
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
	case *ast.Ident:
		if v, ok := consts[pkg][e.Name]; ok {
			return v, true
		}
		return "", false
	case *ast.SelectorExpr:
		qual, ok := e.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		if v, ok := consts[qual.Name][e.Sel.Name]; ok {
			return v, true
		}
		return "", false
	}
	return "", false
}

// exprString renders an expression for an error message.
func exprString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.BasicLit:
		return v.Value
	case *ast.SelectorExpr:
		return exprString(v.X) + "." + v.Sel.Name
	}
	return fmt.Sprintf("%T", e)
}

func dedupePreserveOrder(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// TestEmailDispatcherHandlesEveryEnqueuedJobType is the class guard: every job
// type the repo enqueues must be routed by the dispatcher. A type with no case
// would be mail the worker discards.
func TestEmailDispatcherHandlesEveryEnqueuedJobType(t *testing.T) {
	scan := scanEnqueuedJobTypes(t)

	if len(scan.unresolved) > 0 {
		t.Fatalf("the scanner could not evaluate %d job-type expression(s), so this guard "+
			"does not cover them. Teach jobTypeValue the new shape:\n  %s",
			len(scan.unresolved), strings.Join(scan.unresolved, "\n  "))
	}
	if len(scan.types) < minEnqueuedJobTypes {
		t.Fatalf("extracted only %d enqueued job types from %s (expected at least %d) — "+
			"the scanner is broken, so this guard would pass vacuously",
			len(scan.types), apiModuleDir, minEnqueuedJobTypes)
	}
	if len(scan.handled) == 0 {
		t.Fatal("extracted no switch cases from (*EmailDispatcher).Send — the scanner is " +
			"broken, so the comparison below would be vacuous")
	}
	t.Logf("extracted %d enqueued job types and %d dispatcher cases",
		len(scan.types), len(scan.handled))

	types := make([]string, 0, len(scan.types))
	for typ := range scan.types {
		types = append(types, typ)
	}
	sort.Strings(types)

	// Half 1 (static): the switch must name every enqueued type. This works even
	// when `default:` returns nil, which is why it is the primary check.
	var missing []string
	for _, typ := range types {
		if scan.handled[typ] {
			continue
		}
		missing = append(missing, fmt.Sprintf("%s  [enqueued by: %s]",
			typ, strings.Join(scan.types[typ], ", ")))
	}
	if len(missing) > 0 {
		t.Fatalf("%d enqueued job type(s) have no case in (*EmailDispatcher).Send, so the "+
			"worker drops that mail with no error, no retry and no dead letter:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}

	// Half 2 (behavioural): the real dispatcher, wired with no senders, must
	// actually HANDLE each type — every handled case is nil-safe there, while an
	// unhandled one returns errUnknownJobType. Catches a case label whose body
	// still throws the job away.
	d := &EmailDispatcher{}
	var swallowed []string
	for _, typ := range types {
		err := d.Send(context.Background(), emailqueue.Job{Type: typ, To: "guard@example.test"})
		if errors.Is(err, errUnknownJobType) {
			swallowed = append(swallowed, typ)
		}
	}
	if len(swallowed) > 0 {
		t.Fatalf("%d enqueued job type(s) reach the dispatcher's unknown-type branch: %s",
			len(swallowed), strings.Join(swallowed, ", "))
	}
	t.Logf("all %d enqueued job types are routed", len(types))
}

// TestEmailDispatcherUnknownTypeFailsLoudly pins the default branch itself: an
// unrouted type must return an error (so the worker retries and eventually
// dead-letters it) and must be logged. Returning nil here is the silent-loss bug
// this change removes.
func TestEmailDispatcherUnknownTypeFailsLoudly(t *testing.T) {
	logs := capturingLogs(t)
	d := &EmailDispatcher{}

	err := d.Send(context.Background(), emailqueue.Job{
		Type: "no_such_job_type", To: "x@example.test",
	})
	if !errors.Is(err, errUnknownJobType) {
		t.Fatalf("Send(unknown type) err = %v, want errUnknownJobType — a nil return "+
			"discards the job with no retry and no dead letter", err)
	}
	// The class must still appear in the log line operators grep for.
	if !strings.Contains(logs.String(), "emailq_unknown_type") {
		t.Fatalf("unknown type was not logged as emailq_unknown_type; log:\n%s", logs.String())
	}
}

// ---------------------------------------------------------------------------
// form_notification: enqueued by payments/notifications.go, delivered through
// the payments receipt sender — the same sender the synchronous branch uses, so
// the message passes the pre-send suppression choke point.
// ---------------------------------------------------------------------------

// countingSuppressor records how often the pre-send guard consulted the list.
type countingSuppressor struct {
	suppressed bool
	calls      int
}

func (s *countingSuppressor) IsSuppressed(_ context.Context, _ string) (bool, error) {
	s.calls++
	return s.suppressed, nil
}

// dispatcherFakeSES starts a local SESv2 endpoint and counts SendEmail requests.
// The SDK reads AWS_ENDPOINT_URL_SESV2 when it builds the client, so a counted
// request is a real observation that the message left the process.
func dispatcherFakeSES(t *testing.T) *atomic.Int64 {
	t.Helper()
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"MessageId":"fake-ses-message-id"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("AWS_ENDPOINT_URL_SESV2", srv.URL)
	return &calls
}

// dispatcherSender builds a Sender that talks to the fake SES endpoint, with
// static credentials so the client never probes instance metadata.
func dispatcherSender(sup email.Suppressor) *email.Sender {
	return email.NewWithSuppressor(email.SMTPConfig{
		From: "noreply@jomform.com", Region: "ap-southeast-1",
		Username: "AKIAEXAMPLE", Password: "secretexample",
	}, sup)
}

// TestEmailDispatcherFormNotificationIsDeliveredAndSuppressionChecked drives the
// whole chain: dispatcher → payments receipt sender → the pre-send choke point →
// SES. Before the fix this job logged emailq_unknown_type and no mail was sent.
func TestEmailDispatcherFormNotificationIsDeliveredAndSuppressionChecked(t *testing.T) {
	calls := dispatcherFakeSES(t)
	sup := &countingSuppressor{}
	d := &EmailDispatcher{Pay: &payments.Service{
		Receipts: &payments.Receipts{Sender: dispatcherSender(sup)},
	}}
	logs := capturingLogs(t)

	err := d.Send(context.Background(), emailqueue.Job{
		Type: "form_notification", To: "admin@example.test",
		Subject: "Jualan baharu — JF-1", Body: "Badan emel", SaleRef: "JF-1",
	})
	if err != nil {
		t.Fatalf("Send(form_notification) err = %v, want nil", err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("SES received %d requests, want 1 — a form notification must actually be delivered", n)
	}
	if sup.calls != 1 {
		t.Fatalf("the pre-send suppression check ran %d times, want 1 — a queued form "+
			"notification must reach the choke point, not route around it", sup.calls)
	}
	logged := logs.String()
	if !strings.Contains(logged, "emailq_form_notification_sent") {
		t.Fatalf("delivery was not logged as emailq_form_notification_sent; log:\n%s", logged)
	}
	if strings.Contains(logged, "emailq_unknown_type") {
		t.Fatalf("form_notification still fell through to the unknown-type branch; log:\n%s", logged)
	}
}

// TestEmailDispatcherFormNotificationSuppressedReturnsNil: a suppressed
// recipient is a handled skip — logged, nil, never a retry and never a
// dead-letter entry, and no SES request.
func TestEmailDispatcherFormNotificationSuppressedReturnsNil(t *testing.T) {
	calls := dispatcherFakeSES(t)
	sup := &countingSuppressor{suppressed: true}
	d := &EmailDispatcher{Pay: &payments.Service{
		Receipts: &payments.Receipts{Sender: dispatcherSender(sup)},
	}}
	logs := capturingLogs(t)

	err := d.Send(context.Background(), emailqueue.Job{
		Type: "form_notification", To: "bounced@example.test",
		Subject: "Jualan baharu", Body: "Badan", SaleRef: "JF-2",
	})
	if err != nil {
		t.Fatalf("Send(form_notification) err = %v, want nil — a suppressed job must not retry", err)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("SES received %d requests, want 0 for a suppressed recipient", n)
	}
	if sup.calls != 1 {
		t.Fatalf("suppression lookups = %d, want 1", sup.calls)
	}
	logged := logs.String()
	if !strings.Contains(logged, "emailq_skip_suppressed") {
		t.Fatalf("skip was not logged as emailq_skip_suppressed; log:\n%s", logged)
	}
	if strings.Contains(logged, "emailq_form_notification_failed") {
		t.Fatalf("a suppressed form notification was logged as a send FAILURE; log:\n%s", logged)
	}
	if strings.Contains(logged, "emailq_form_notification_sent") {
		t.Fatalf("a suppressed form notification was logged as sent; log:\n%s", logged)
	}
}

// TestEmailDispatcherFormNotificationWithoutSenderIsSafe: the nil-sender branch
// must be a logged drop, not a panic.
func TestEmailDispatcherFormNotificationWithoutSenderIsSafe(t *testing.T) {
	logs := capturingLogs(t)
	d := &EmailDispatcher{}

	if err := d.Send(context.Background(), emailqueue.Job{
		Type: "form_notification", To: "admin@example.test", SaleRef: "JF-3",
	}); err != nil {
		t.Fatalf("Send without a sender err = %v, want nil", err)
	}
	if !strings.Contains(logs.String(), "emailq_form_notification_no_sender") {
		t.Fatalf("a missing sender was not logged; log:\n%s", logs.String())
	}
}

// ---------------------------------------------------------------------------
// connect: enqueued by httpapi/social.go, delivered through the shared auth
// mailer — the same mailer its synchronous branch uses.
// ---------------------------------------------------------------------------

// TestEmailDispatcherConnectGoesThroughAuthMailer: before the fix this type
// logged emailq_unknown_type and the email-ownership link never arrived.
func TestEmailDispatcherConnectGoesThroughAuthMailer(t *testing.T) {
	logs := capturingLogs(t)
	mailer := &recorderMailer{}
	d := &EmailDispatcher{AuthMailer: mailer}

	err := d.Send(context.Background(), emailqueue.Job{
		Type: "connect", To: "user@example.test",
		Subject: "Pautan penghubung akaun JomForm anda", Body: "Badan",
	})
	if err != nil {
		t.Fatalf("Send(connect) err = %v, want nil", err)
	}
	if !mailer.wasCalled() {
		t.Fatal("the connect mailer was never called — the email-ownership link is dropped")
	}
	if strings.Contains(logs.String(), "emailq_unknown_type") {
		t.Fatalf("connect still fell through to the unknown-type branch; log:\n%s", logs.String())
	}
}

// TestEmailDispatcherConnectSuppressedReturnsNil: same handled-skip contract as
// every other auth-routed type.
func TestEmailDispatcherConnectSuppressedReturnsNil(t *testing.T) {
	logs := capturingLogs(t)
	mailer := &suppressedMailer{}
	d := &EmailDispatcher{AuthMailer: mailer}

	err := d.Send(context.Background(), emailqueue.Job{
		Type: "connect", To: "bounced@example.test", Subject: "S", Body: "B",
	})
	if err != nil {
		t.Fatalf("Send(connect) err = %v, want nil — a suppressed job must not retry", err)
	}
	if mailer.calls != 1 {
		t.Fatalf("mailer calls = %d, want 1 (the dispatcher must attempt the send and "+
			"let the sender's guard decide)", mailer.calls)
	}
	logged := logs.String()
	if !strings.Contains(logged, "emailq_skip_suppressed") {
		t.Fatalf("connect skip was not logged as emailq_skip_suppressed; log:\n%s", logged)
	}
	// The skip line must name the type so an operator can tell which flow was dropped.
	if !strings.Contains(logged, "connect") {
		t.Fatalf("skip log must carry the job type; log:\n%s", logged)
	}
}
