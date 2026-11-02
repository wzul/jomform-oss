package httpapi

// Guard: JOMFORM_SELF_HOSTED must reach PublicHandlers.SelfHosted.
//
// The flag is threaded through three pieces, and the middle hop is invisible to
// every other test in this package:
//
//	env JOMFORM_SELF_HOSTED   -> config.Config.SelfHosted  (config.Load, covered)
//	config.SelfHosted         -> PublicHandlers.SelfHosted (main.go — THIS FILE)
//	PublicHandlers.SelfHosted -> ownOriginForm             (selfhost_origin_form_test.go)
//
// config.Load returning the right bool, and the handler refusing to serve when
// the field is false, together prove NOTHING about whether the field is ever
// assigned: SelfHosted is an optional struct field, so the composite literal in
// main.go compiles just as happily without it. Drop the assignment and every
// test here stays green while a one-command self-host install serves no form at
// all — the dashboard prints the form as live and every buyer gets "Borang tidak
// dijumpai". That is an outage, not a red.
//
// So this asserts the hop from the source, the way
// internal/config/ses_replyto_wiring_guard_test.go asserts ReplyTo's.
import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// mainGoForWiring is relative to this package's directory (api/internal/httpapi)
// -> api/cmd/jomform-api/main.go.
const mainGoForWiring = "../../cmd/jomform-api/main.go"

func TestSelfHostedReachesPublicHandlersInMainGo(t *testing.T) {
	assertMainWiresField(t, "PublicHandlers", "SelfHosted")
}

// TestSelfHostedReachesRESTHandlersInMainGo is the same hop for the OTHER handler
// that builds a public form URL (card t_6c0badbe). The dashboard's builder reads
// the URL off the REST payload; if THIS literal keeps the default `false`, a
// self-hosted install's "Your form is live" dialog prints the hosted tenant shape
// again (a dead host) while the PublicHandlers literal — wired correctly — serves
// the form at its own origin. The two must be the same flag.
func TestSelfHostedReachesRESTHandlersInMainGo(t *testing.T) {
	assertMainWiresField(t, "RESTHandlers", "SelfHosted")
}

// TestWorkspacesReachesRESTHandlersInMainGo pins the OTHER hop createForm depends
// on (card t_6c0badbe): the form-create guard refuses an account with no live
// workspace, and it can only answer that question through this store. The field is
// optional, so the literal compiles just as happily without it — and then a
// workspace-free account would create an unreachable form again, silently.
func TestWorkspacesReachesRESTHandlersInMainGo(t *testing.T) {
	assertMainWiresField(t, "RESTHandlers", "Workspaces")
}

// assertMainWiresField requires every composite literal of typeName in main.go to
// set `field:` to some value (a selector, e.g. cfg.SelfHosted / workspacesStore).
func assertMainWiresField(t *testing.T, typeName, field string) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, mainGoForWiring, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", mainGoForWiring, err)
	}

	literals, wired := 0, false
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != typeName {
			return true
		}
		literals++
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != field {
				continue
			}
			if _, ok := kv.Value.(*ast.SelectorExpr); ok {
				wired = true
			}
			// A local variable (e.g. `Workspaces: workspacesStore`) is equally a
			// wired value; only the zero value / an omitted field is not.
			if _, ok := kv.Value.(*ast.Ident); ok {
				wired = true
			}
		}
		return true
	})

	if literals == 0 {
		t.Fatalf("no %s composite literal found in main.go — this guard is looking at code "+
			"that no longer exists and would pass vacuously; fix the guard, not the test", typeName)
	}
	if !wired {
		t.Fatalf("the %s literal(s) in %s never set %s from a config/store value — the deployment "+
			"cannot reach the code path that depends on it (%d literal(s) inspected). Wire the hop; "+
			"do not delete this test", typeName, mainGoForWiring, field, literals)
	}
}
