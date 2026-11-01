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
	"os"
	"testing"
)

// mainGoForWiring is relative to this package's directory (api/internal/httpapi)
// -> api/cmd/jomform-api/main.go.
const mainGoForWiring = "../../cmd/jomform-api/main.go"

func TestSelfHostedReachesPublicHandlersInMainGo(t *testing.T) {
	src, err := os.ReadFile(mainGoForWiring)
	if err != nil {
		t.Fatalf("read %s: %v", mainGoForWiring, err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, mainGoForWiring, src, 0)
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
		if !ok || sel.Sel == nil || sel.Sel.Name != "PublicHandlers" {
			return true
		}
		literals++
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "SelfHosted" {
				continue
			}
			if val, ok := kv.Value.(*ast.SelectorExpr); ok && val.Sel != nil && val.Sel.Name == "SelfHosted" {
				wired = true
			}
		}
		return true
	})

	if literals == 0 {
		t.Fatal("no PublicHandlers composite literal found in main.go — this guard is looking at code " +
			"that no longer exists and would pass vacuously; fix the guard, not the test")
	}
	if !wired {
		t.Fatalf("the PublicHandlers literal(s) in %s never set SelfHosted: cfg.SelfHosted — "+
			"JOMFORM_SELF_HOSTED cannot reach the serving path, so a self-hosted install serves no "+
			"form at its own origin (%d literal(s) inspected). Wire the hop; do not delete this test",
			mainGoForWiring, literals)
	}
}
