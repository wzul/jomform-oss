package httpapi

// Card t_67db0213 — wiring guard for the MCP side of Cloudflare `configured`.
//
// get_cloudflare_status answers from mcpsrv.Deps.CFOAuthConfigured, a func field
// that cmd/jomform-api/main.go must fill with httpapi.CFOAuthConfigured — the
// same rule /v1/rest/cf/status publishes. If that wiring is dropped, reordered
// or replaced by a second env read, the field stays nil and the tool falls back
// to configured:false: the ORIGINAL defect, silently, with every unit test that
// builds its own Deps lit green.
//
// So this test reads main.go's source and asserts the wiring, the way
// config/ses_replyto_wiring_guard_test.go does for SES_REPLY_TO. A missing
// field in a struct literal is not something the type checker can catch — every
// Deps field is optional.
//
// Scope, stated: this proves the ASSIGNMENT exists and names httpapi's rule. It
// does not prove what the rule returns (cf_oauth_test.go) nor that both surfaces
// agree at runtime (cf_status_surface_agreement_test.go).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// mainGoPath resolves to api/cmd/jomform-api/main.go from api/internal/httpapi.
const mainGoPath = "../../cmd/jomform-api/main.go"

func TestMainWiresMCPCloudflareConfiguredToTheRESTRule(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, mainGoPath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", mainGoPath, err)
	}

	var literals []*ast.CompositeLit
	ast.Inspect(f, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := cl.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Deps" {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == "mcpsrv" {
			literals = append(literals, cl)
		}
		return true
	})
	if len(literals) == 0 {
		t.Fatalf("found no &mcpsrv.Deps{…} literal in %s — this guard is not looking at the code it claims to check", mainGoPath)
	}

	for i, cl := range literals {
		pos := fset.Position(cl.Pos())
		wired := false
		for _, el := range cl.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "CFOAuthConfigured" {
				continue
			}
			sel, ok := kv.Value.(*ast.SelectorExpr)
			if !ok {
				t.Errorf("%s: mcpsrv.Deps literal #%d sets CFOAuthConfigured to something other than httpapi.CFOAuthConfigured — a second reading of the rule is how the two surfaces drifted apart", pos, i+1)
				continue
			}
			pkg, _ := sel.X.(*ast.Ident)
			if pkg == nil || pkg.Name != "httpapi" || sel.Sel.Name != "CFOAuthConfigured" {
				t.Errorf("%s: mcpsrv.Deps literal #%d sets CFOAuthConfigured to %s.%s, want httpapi.CFOAuthConfigured (the rule GET /v1/rest/cf/status publishes)", pos, i+1, identName(pkg), sel.Sel.Name)
				continue
			}
			wired = true
		}
		if !wired {
			t.Errorf("%s: mcpsrv.Deps literal #%d does not set CFOAuthConfigured — get_cloudflare_status would answer configured:false on every deployment, disagreeing with /v1/rest/cf/status (card t_67db0213)", pos, i+1)
		}
	}
}
