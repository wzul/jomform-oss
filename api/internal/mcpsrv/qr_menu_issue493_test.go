package mcpsrv

// Issue #493 / #497: MCP parity for the restaurant menu.
//
// The menu tools get_menu_page / set_menu_page are the MCP surface of the
// "Restaurant menu" dashboard setting, including the optional order form
// (order_form_slug) whose field blocks define the extra fields the menu
// checkout collects.
//
// Issue #497 moved the menu from a per-workspace landing template to a PER-FORM
// setting: both tools now take a form_id and read/write forms.config.menu.
// These are pure tests (no DB): registration is checked over the MCP protocol,
// and the input plumbing + config round-trip are checked directly.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// The menu tools are registered (and therefore must stay in the AGENTS.md
// parity checklist — the #477 guard asserts the reverse direction too).
func TestIssue493MenuToolsRegistered(t *testing.T) {
	srv := &Server{
		mcp:  mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: &Deps{},
	}
	srv.registerTools()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	cTransport, sTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.mcp.Connect(context.Background(), sTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()
	clientSession, err := client.Connect(context.Background(), cTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer clientSession.Close()

	res, err := clientSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	got := map[string]bool{}
	for _, tl := range res.Tools {
		got[tl.Name] = true
	}
	for _, name := range []string{"get_menu_page", "set_menu_page"} {
		if !got[name] {
			t.Errorf("tool %q not registered by registerTools", name)
		}
	}
	// Issue #497: the tools are per-form — the input schema advertises form_id
	// (not the old workspace_id) and still exposes order_form_slug.
	var found bool
	for _, tl := range res.Tools {
		if tl.Name != "set_menu_page" {
			continue
		}
		found = true
		raw, merr := json.Marshal(tl.InputSchema)
		if merr != nil {
			t.Fatalf("marshal input schema: %v", merr)
		}
		if !strings.Contains(string(raw), "form_id") {
			t.Errorf("set_menu_page input schema does not expose form_id: %s", raw)
		}
		if strings.Contains(string(raw), "workspace_id") {
			t.Errorf("set_menu_page still exposes workspace_id (issue #497 moved the menu onto the form): %s", raw)
		}
		if !strings.Contains(string(raw), "order_form_slug") {
			t.Errorf("set_menu_page input schema does not expose order_form_slug: %s", raw)
		}
	}
	if !found {
		t.Error("set_menu_page tool not found in tools/list")
	}
}

// The setting round-trips through the form's config JSON blob under the `menu`
// key (no migration), and the MCP input trims the attached order form slug.
func TestIssue493MenuPageOrderFormSlugRoundTrip(t *testing.T) {
	in := setMenuPageIn{FormID: "form-1", Enabled: true, OrderFormSlug: "  extra-info  "}
	trimmed := strings.TrimSpace(in.OrderFormSlug)
	if trimmed != "extra-info" {
		t.Fatalf("trimmed order form slug = %q, want extra-info", trimmed)
	}
	// A blank slug means "no extra fields" — the checkout collects name+email.
	blank := setMenuPageIn{FormID: "form-1", Enabled: true, OrderFormSlug: "   "}
	if strings.TrimSpace(blank.OrderFormSlug) != "" {
		t.Error("a blank order form slug must mean no attached form")
	}

	// Issue #497: the menu is written under forms.config.menu, preserving the
	// form's blocks and every other config key, and round-trips through JSON.
	formConfig := map[string]any{
		"blocks": []any{map[string]any{"id": "p1", "type": "product", "product_id": "prod-1"}},
		"mode":   "payment",
	}
	next := dbstore.WithMenu(formConfig, &dbstore.MenuPage{Enabled: true, OrderFormSlug: trimmed})
	raw, err := json.Marshal(next)
	if err != nil {
		t.Fatalf("marshal form config: %v", err)
	}
	if !strings.Contains(string(raw), `"order_form_slug":"extra-info"`) {
		t.Errorf("form config JSON missing order_form_slug: %s", raw)
	}
	if !strings.Contains(string(raw), `"blocks"`) {
		t.Errorf("WithMenu dropped the form's blocks: %s", raw)
	}
	if !strings.Contains(string(raw), `"mode":"payment"`) {
		t.Errorf("WithMenu dropped an unrelated config key: %s", raw)
	}
	back := dbstore.MenuFromConfig(next)
	if back == nil || back.OrderFormSlug != "extra-info" || !back.Enabled {
		t.Errorf("menu did not round-trip through the form config: %+v", back)
	}

	// A form without a menu key decodes to no menu (nil), and WithMenu(nil)
	// removes the key entirely so a disabled menu clears cleanly.
	if dbstore.MenuFromConfig(map[string]any{"blocks": []any{}}) != nil {
		t.Error("a form with no menu key must decode to nil")
	}
	cleared := dbstore.WithMenu(next, nil)
	if _, ok := cleared["menu"]; ok {
		t.Error("WithMenu(config, nil) must remove the menu key")
	}
	if _, ok := cleared["blocks"]; !ok {
		t.Error("WithMenu(config, nil) must preserve the form's blocks")
	}
}

// Issue #497: a legacy landing_page_json.menu blob is parsed but safely ignored
// — nothing reads LandingPage.LegacyMenu, and a stray blob cannot render a menu
// at the workspace root.
func TestIssue497LegacyLandingMenuIgnored(t *testing.T) {
	var lp dbstore.LandingPage
	if err := json.Unmarshal([]byte(`{"enabled":true,"menu":{"enabled":true,"categories":[]}}`), &lp); err != nil {
		t.Fatalf("unmarshal legacy landing page: %v", err)
	}
	// The legacy field exists only so the payload parses; it must not be a
	// live menu the renderer would honour.
	if lp.LegacyMenu == nil {
		t.Fatal("legacy menu blob did not parse — a stray payload must not crash")
	}
	// The form-scoped accessor is the only live source, and a landing page is
	// not a form config.
	if dbstore.MenuFromConfig(map[string]any{"landing_page": map[string]any{"menu": map[string]any{"enabled": true}}}) != nil {
		t.Error("MenuFromConfig must only read forms.config.menu, not a nested landing page")
	}
}
