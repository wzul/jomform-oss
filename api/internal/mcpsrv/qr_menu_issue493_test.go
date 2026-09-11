package mcpsrv

// Issue #493: MCP parity for the restaurant-menu extra checkout fields.
//
// The menu tools get_menu_page / set_menu_page are the MCP surface of the
// "Restaurant menu" dashboard setting, including the optional order form
// (order_form_slug) whose field blocks define the extra fields the menu
// checkout collects. These are pure tests (no DB): registration is checked over
// the MCP protocol, and the input plumbing is checked directly.

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
	// The order_form_slug input is advertised to the agent.
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
		if !strings.Contains(string(raw), "order_form_slug") {
			t.Errorf("set_menu_page input schema does not expose order_form_slug: %s", raw)
		}
	}
	if !found {
		t.Error("set_menu_page tool not found in tools/list")
	}
}

// The setting round-trips through the workspace_settings JSON blob under the
// order_form_slug key (no migration), and the MCP input trims it.
func TestIssue493MenuPageOrderFormSlugRoundTrip(t *testing.T) {
	in := setMenuPageIn{WorkspaceID: "ws-1", Enabled: true, OrderFormSlug: "  extra-info  "}
	trimmed := strings.TrimSpace(in.OrderFormSlug)
	if trimmed != "extra-info" {
		t.Fatalf("trimmed order form slug = %q, want extra-info", trimmed)
	}
	// A blank slug means "no extra fields" — the checkout collects name+email.
	blank := setMenuPageIn{WorkspaceID: "ws-1", Enabled: true, OrderFormSlug: "   "}
	if strings.TrimSpace(blank.OrderFormSlug) != "" {
		t.Error("a blank order form slug must mean no attached form")
	}

	// The JSON key the dashboard and MCP share is order_form_slug, and the
	// value survives a marshal/unmarshal through the landing_page_json blob.
	raw, err := json.Marshal(&dbstore.LandingPage{
		Enabled: true,
		Menu:    &dbstore.MenuPage{Enabled: true, OrderFormSlug: trimmed},
	})
	if err != nil {
		t.Fatalf("marshal landing page: %v", err)
	}
	if !strings.Contains(string(raw), `"order_form_slug":"extra-info"`) {
		t.Errorf("landing page JSON missing order_form_slug: %s", raw)
	}
	var back dbstore.LandingPage
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal landing page: %v", err)
	}
	if back.Menu == nil || back.Menu.OrderFormSlug != "extra-info" {
		t.Errorf("order_form_slug did not round-trip: %+v", back.Menu)
	}
	// A menu without the key decodes to an empty slug (no attached form).
	var legacy dbstore.LandingPage
	if err := json.Unmarshal([]byte(`{"enabled":true,"menu":{"enabled":true,"categories":[]}}`), &legacy); err != nil {
		t.Fatalf("unmarshal legacy menu: %v", err)
	}
	if legacy.Menu == nil || legacy.Menu.OrderFormSlug != "" {
		t.Errorf("legacy menu decoded a non-empty order form slug: %+v", legacy.Menu)
	}
}
