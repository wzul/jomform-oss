package httpapi

// Guard: JOMFORM_SELF_HOSTED must reach the WORKSPACE-URL writers (card
// t_48a93084), on both transports.
//
// The hop this file pins is invisible to every other test in the repo, for the
// same reason selfhost_flag_wiring_test.go exists for PublicHandlers:
//
//	env JOMFORM_SELF_HOSTED    -> config.Config.SelfHosted   (config.Load, covered)
//	config.SelfHosted          -> RESTHandlers.SelfHosted    (main.go — THIS FILE)
//	config.SelfHosted          -> mcpsrv.Deps.SelfHosted     (main.go — THIS FILE)
//	cfg.AppBaseURL             -> mcpsrv.Deps.AppBaseURL     (main.go — THIS FILE)
//	RESTHandlers.SelfHosted    -> workspace payloads         (workspace_url_t_48a93084_test.go)
//
// SelfHosted is an OPTIONAL struct field: the composite literals in main.go
// compile exactly as happily without it, and every unit test above stays green
// while a self-hosted install keeps answering
// `"url":"https://<slug>.jomform.com"` — a host that does not exist there. That
// is the defect this card closes, so the assignment itself is asserted, from the
// source, the way internal/config/ses_replyto_wiring_guard_test.go asserts
// ReplyTo's.
import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"testing"
)

// assertLiteralWires is the shared shape of the checks below: find every
// composite literal of <qualifier>.<typeName> in main.go (failing loudly when
// there is none, because a guard looking at deleted code passes vacuously) and
// require each to set `key: <expr ending in wantSel>`.
//
// The packet qualifier is part of the match on purpose: main.go also builds an
// empty `httpapi.Deps{}`, and a guard matching on the bare type name would
// demand the workspace fields on THAT literal too.
func assertLiteralWires(t *testing.T, src []byte, packet, typeName string, pairs map[string]string) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, mainGoForWiring, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", mainGoForWiring, err)
	}

	literals := 0
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != typeName {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != packet {
			return true
		}
		literals++
		for key, wantSel := range pairs {
			wired := false
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				id, ok := kv.Key.(*ast.Ident)
				if !ok || id.Name != key {
					continue
				}
				if v, ok := kv.Value.(*ast.SelectorExpr); ok && v.Sel != nil && v.Sel.Name == wantSel {
					wired = true
				}
			}
			if !wired {
				t.Errorf("the %s literal(s) in %s never set %s: cfg.%s — the deployment flag/origin "+
					"cannot reach the address this transport prints, so a self-hosted install keeps "+
					"answering <slug>.jomform.com (%d literal(s) inspected). Wire the hop; do not "+
					"delete this test.", typeName, mainGoForWiring, key, wantSel, literals)
			}
		}
		return true
	})

	if literals == 0 {
		t.Fatalf("no %s composite literal found in main.go — this guard is looking at code that no "+
			"longer exists and would pass vacuously; fix the guard, not the test", typeName)
	}
}

func TestSelfHostedReachesTheRESTWorkspacePayloads(t *testing.T) {
	src, err := os.ReadFile(mainGoForWiring)
	if err != nil {
		t.Fatalf("read %s: %v", mainGoForWiring, err)
	}
	assertLiteralWires(t, src, "httpapi", "RESTHandlers", map[string]string{"SelfHosted": "SelfHosted"})
}

func TestSelfHostedReachesTheMCPWorkspaceTools(t *testing.T) {
	src, err := os.ReadFile(mainGoForWiring)
	if err != nil {
		t.Fatalf("read %s: %v", mainGoForWiring, err)
	}
	assertLiteralWires(t, src, "mcpsrv", "Deps", map[string]string{
		"SelfHosted": "SelfHosted",
		"AppBaseURL": "AppBaseURL",
	})
}
