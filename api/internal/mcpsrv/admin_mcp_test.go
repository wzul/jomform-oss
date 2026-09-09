package mcpsrv

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestAdminToolsRegistered verifies the issue #386 super-admin MCP tools are
// registered and discoverable over the MCP protocol.
func TestAdminToolsRegistered(t *testing.T) {
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
		"admin_list_spaces", "admin_media_usage", "admin_list_sales",
		"admin_spam_queue", "admin_email_deliveries", "admin_system_health",
	} {
		if !got[name] {
			t.Errorf("tool %q not registered by registerTools", name)
		}
	}
}

// TestAdminToolsFailClosed verifies the super-admin gate (issue #385) rejects
// a non-super-admin caller on every admin tool — a missing SUPER_ADMIN_EMAILS
// allowlist must never grant platform-wide access (issue #386 acceptance).
func TestAdminToolsFailClosed(t *testing.T) {
	srv := &Server{
		mcp:  mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: &Deps{},
	}
	ctx := context.Background()
	req := &mcp.CallToolRequest{}

	// No allowlist configured → every admin tool must reject.
	if _, _, err := srv.adminListSpaces(ctx, req, emptyIn{}); err == nil {
		t.Error("admin_list_spaces should reject a non-super-admin")
	}
	if _, _, err := srv.adminMediaUsage(ctx, req, emptyIn{}); err == nil {
		t.Error("admin_media_usage should reject a non-super-admin")
	}
	if _, _, err := srv.adminListSales(ctx, req, adminListSalesIn{}); err == nil {
		t.Error("admin_list_sales should reject a non-super-admin")
	}
	if _, _, err := srv.adminSpamQueue(ctx, req, adminSpamQueueIn{}); err == nil {
		t.Error("admin_spam_queue should reject a non-super-admin")
	}
	if _, _, err := srv.adminEmailDeliveries(ctx, req, adminEmailDeliveriesIn{}); err == nil {
		t.Error("admin_email_deliveries should reject a non-super-admin")
	}
	if _, _, err := srv.adminSystemHealth(ctx, req, emptyIn{}); err == nil {
		t.Error("admin_system_health should reject a non-super-admin")
	}
}
