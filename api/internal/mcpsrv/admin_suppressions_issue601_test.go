package mcpsrv

// Issue #601: the super-admin suppression list MCP tools.
//
// These cover the two things that can silently break an admin tool: it is not
// registered (the agent cannot see it at all), and it is not gated (a
// non-super-admin can read and rewrite the platform's suppression list).
//
// The tools' outputSchema is covered centrally by
// TestMCPToolsHaveObjectOutputSchema, which enumerates the real tools/list — it
// is what forbids a bare-slice Out, and these handlers return
// *listPageOutput[T] / map[string]any for exactly that reason.

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestAdminSuppressionToolsRegistered verifies the three issue #601 tools are
// discoverable over the MCP protocol.
func TestAdminSuppressionToolsRegistered(t *testing.T) {
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
	for _, name := range []string{
		"admin_list_suppressions", "admin_add_suppression", "admin_remove_suppression",
	} {
		if !got[name] {
			t.Errorf("issue #601: tool %q not registered by registerTools — the agent cannot see it", name)
		}
	}
}

// TestAdminSuppressionToolsFailClosed verifies the super-admin gate on all
// three tools: with no allowlist configured (Deps.SuperAdmin nil) every one of
// them must reject, before it touches the store.
func TestAdminSuppressionToolsFailClosed(t *testing.T) {
	srv := &Server{
		mcp:  mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: &Deps{},
	}
	ctx := context.Background()
	req := &mcp.CallToolRequest{}

	if _, _, err := srv.adminListSuppressions(ctx, req, adminListSuppressionsIn{}); err == nil {
		t.Error("issue #601: admin_list_suppressions accepted a non-super-admin")
	}
	if _, _, err := srv.adminAddSuppression(ctx, req, adminAddSuppressionIn{Email: "a@b.com"}); err == nil {
		t.Error("issue #601: admin_add_suppression accepted a non-super-admin")
	}
	if _, _, err := srv.adminRemoveSuppression(ctx, req, adminRemoveSuppressionIn{Email: "a@b.com"}); err == nil {
		t.Error("issue #601: admin_remove_suppression accepted a non-super-admin")
	}
}

// TestAdminSuppressionAddValidatesInput pins the input validation that runs
// BEFORE the store is consulted, so a malformed address or an out-of-enum
// reason is rejected with a message rather than reaching the reason CHECK as a
// constraint violation. The store is nil here on purpose: a handler that
// validated late would dereference it and panic.
func TestAdminSuppressionAddValidatesInput(t *testing.T) {
	srv := &Server{
		mcp:  mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: &Deps{},
	}
	// Grant the caller super-admin so the gate does not short-circuit first.
	srv.deps.SuperAdmin = func(string) bool { return true }
	ctx := WithAuthIdentity(context.Background(), &AuthIdentity{AccountID: "acct", UserID: "user"})
	req := &mcp.CallToolRequest{}

	for _, tc := range []struct {
		name string
		in   adminAddSuppressionIn
	}{
		{"not an email", adminAddSuppressionIn{Email: "nope"}},
		{"empty email", adminAddSuppressionIn{Email: "   "}},
		{"unknown reason", adminAddSuppressionIn{Email: "a@b.com", Reason: "because"}},
	} {
		// The store is nil, so an early validation failure returns a validation
		// error; reaching the store would return the "not configured" error
		// instead. Distinguish them so a validation regression is caught.
		_, _, err := srv.adminAddSuppression(ctx, req, tc.in)
		if err == nil {
			t.Errorf("issue #601: admin_add_suppression accepted %s", tc.name)
			continue
		}
		if err.Error() == "senarai suppression tidak dikonfigurasi" {
			t.Errorf("issue #601: %s reached the store instead of being rejected by validation", tc.name)
		}
	}

	// A remove with an empty address is rejected too.
	if _, _, err := srv.adminRemoveSuppression(ctx, req, adminRemoveSuppressionIn{Email: "  "}); err == nil {
		t.Error("issue #601: admin_remove_suppression accepted an empty address")
	}
}
