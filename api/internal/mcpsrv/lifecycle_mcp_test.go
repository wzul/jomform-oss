package mcpsrv

// The lifecycle table on the MCP surface (card t_52429d6f).
//
// The two properties that matter:
//
//  1. NO SECOND COPY. The tool and the resource must answer with
//     lifecycle.SalesMachine.Describe(), field for field. A surface that kept its
//     own table would pass its own tests and drift from the machine Apply
//     enforces — the exact failure this card exists to remove.
//  2. REACHABLE. The tool is registered and callable over the real protocol
//     (tools/list, tools/call), and the resource template is listed and
//     readable (resources/templates/list, resources/read) — an unregistered
//     handler is invisible to every agent.

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/lifecycle"
)

// connectLifecycleTestServer builds the real server and connects an in-memory
// client to it, with the identity a transport would have put on the context.
func connectLifecycleTestServer(t *testing.T) (*Server, *mcp.ClientSession) {
	t.Helper()
	srv := New(&Deps{}, nil, nil, nil)
	client := mcp.NewClient(&mcp.Implementation{Name: "lifecycle-test", Version: "test"}, nil)
	cTransport, sTransport := mcp.NewInMemoryTransports()
	callCtx := WithAuthIdentity(context.Background(), &AuthIdentity{
		// UserID makes this the OAuth shape: the scope resolver then does not
		// need an account store. An empty account list is enough for the table
		// itself, which is not account data.
		UserID: "u-lifecycle-test", AccountID: "acc-lifecycle-test", Scopes: []string{"readwrite"},
	})
	serverSession, err := srv.mcp.Connect(callCtx, sTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	clientSession, err := client.Connect(context.Background(), cTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	return srv, clientSession
}

// tableFromResource reads the resource and decodes the table it serves.
func tableFromResource(t *testing.T, session *mcp.ClientSession, uri string) lifecycle.Table {
	t.Helper()
	res, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("resources/read %s: %v", uri, err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("resources/read %s returned %d contents, want 1", uri, len(res.Contents))
	}
	c := res.Contents[0]
	if c.MIMEType != "application/json" {
		t.Errorf("resource mimeType = %q, want application/json", c.MIMEType)
	}
	var table lifecycle.Table
	if err := json.Unmarshal([]byte(c.Text), &table); err != nil {
		t.Fatalf("resource body is not the table: %v (body: %s)", err, c.Text)
	}
	return table
}

// tableFromTool calls describe_lifecycle without an id and decodes the answer.
func tableFromTool(t *testing.T, session *mcp.ClientSession) lifecycle.Table {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "describe_lifecycle",
		Arguments: map[string]any{"aggregate": "sales"},
	})
	if err != nil {
		t.Fatalf("tools/call describe_lifecycle: %v", err)
	}
	if res.IsError {
		t.Fatalf("describe_lifecycle answered an error: %+v", res.Content)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal tool structured content: %v", err)
	}
	var table lifecycle.Table
	if err := json.Unmarshal(raw, &table); err != nil {
		t.Fatalf("describe_lifecycle output is not the table: %v (%s)", err, raw)
	}
	return table
}

// TestLifecycleToolAndResourceAreTheGoTable is the "no second copy" test: both
// surfaces must equal Machine.Describe(), including every edge and its guard
// flag. If a surface grows its own table, this fails on the first divergence.
func TestLifecycleToolAndResourceAreTheGoTable(t *testing.T) {
	_, session := connectLifecycleTestServer(t)
	want := lifecycle.SalesMachine.Describe()

	fromResource := tableFromResource(t, session, "jomform://lifecycle/sales")
	if !reflect.DeepEqual(fromResource, want) {
		gotJSON, _ := json.MarshalIndent(fromResource, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Errorf("the MCP resource does not serve the Go table.\nresource:\n%s\ngo:\n%s", gotJSON, wantJSON)
	}

	fromTool := tableFromTool(t, session)
	if !reflect.DeepEqual(fromTool, want) {
		gotJSON, _ := json.MarshalIndent(fromTool, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Errorf("describe_lifecycle does not serve the Go table.\ntool:\n%s\ngo:\n%s", gotJSON, wantJSON)
	}
}

// TestLifecycleSurfacesAreRegistered proves the surfaces exist over the
// protocol, not merely as methods: a handler nobody can list is unreachable to
// every agent.
func TestLifecycleSurfacesAreRegistered(t *testing.T) {
	_, session := connectLifecycleTestServer(t)

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	found := false
	for _, tool := range tools.Tools {
		if tool.Name == "describe_lifecycle" {
			found = true
			if !strings.Contains(tool.Description, "available_events") {
				t.Errorf("describe_lifecycle's description does not mention available_events, so an agent is not told what it gets back")
			}
			if tool.InputSchema == nil {
				t.Errorf("describe_lifecycle has no input schema — an agent cannot know to pass aggregate/id")
			}
			break
		}
	}
	if !found {
		t.Fatal("describe_lifecycle is not registered (tools/list does not list it)")
	}

	templates, err := session.ListResourceTemplates(context.Background(), nil)
	if err != nil {
		t.Fatalf("resources/templates/list: %v", err)
	}
	uriFound := false
	for _, rt := range templates.ResourceTemplates {
		if rt.URITemplate == "jomform://lifecycle/{aggregate}" {
			uriFound = true
			break
		}
	}
	if !uriFound {
		t.Fatalf("the jomform://lifecycle/{aggregate} resource template is not registered: %+v", templates.ResourceTemplates)
	}
}

// TestDescribeLifecycleUnknownAggregateNamesTheKnownOnes: a typo must not look
// like "this aggregate accepts nothing".
func TestDescribeLifecycleUnknownAggregateNamesTheKnownOnes(t *testing.T) {
	srv := New(&Deps{}, nil, nil, nil)
	_, _, err := srv.describeLifecycle(context.Background(), nil, describeLifecycleIn{Aggregate: "sale"})
	if err == nil {
		t.Fatal("describe_lifecycle accepted an unknown aggregate")
	}
	if !strings.Contains(err.Error(), "sales") {
		t.Errorf("the refusal %q does not name the known aggregate, so an agent cannot retry", err)
	}
}

// TestDescribeLifecycleWithSaleIDIsTheRecordsOwnAnswer: with an id the tool
// answers for THAT sale — the state and the events it accepts right now, guards
// evaluated. The list must equal Machine.Available for the same row.
func TestDescribeLifecycleWithSaleIDIsTheRecordsOwnAnswer(t *testing.T) {
	st := issue518MCPFixture(t, 5000)

	_, out, err := st.srv.describeLifecycle(st.ctx, nil, describeLifecycleIn{Aggregate: "sales", ID: st.refCode})
	if err != nil {
		t.Fatalf("describe_lifecycle with a sale id: %v", err)
	}
	if out.State != dbstore.SalePaid {
		t.Errorf("state = %q, want paid", out.State)
	}
	sale, err := st.srv.deps.Sales.ByRefCode(st.ctx, st.refCode)
	if err != nil {
		t.Fatalf("re-read the sale: %v", err)
	}
	want := lifecycle.SalesMachine.Available(sale)
	if !reflect.DeepEqual(out.AvailableEvents, want) {
		t.Errorf("available_events = %+v, want %+v (the table's own answer)", out.AvailableEvents, want)
	}
	// A paid sale with no captured authorization cannot be captured: the tool
	// must say so WITH the reason, because that is what the caller would hit.
	refused := false
	for _, ev := range out.AvailableEvents {
		if ev.Event == lifecycle.SaleEventCapture {
			refused = !ev.Enabled && ev.Reason != ""
		}
	}
	if !refused {
		t.Errorf("capture is not reported as disabled-with-reason on %+v", out.AvailableEvents)
	}
}

// TestDescribeLifecycleRefusesAnotherAccountsSale keeps the read scoped: an id
// belonging to an account the call cannot touch is "not found", never a leak.
func TestDescribeLifecycleRefusesAnotherAccountsSale(t *testing.T) {
	st := issue518MCPFixture(t, 5000)
	other := WithAuthIdentity(context.Background(), &AuthIdentity{
		AccountID: "acc-someone-else", Scopes: []string{"readwrite"},
	})
	if _, _, err := st.srv.describeLifecycle(other, nil, describeLifecycleIn{Aggregate: "sales", ID: st.refCode}); err == nil {
		t.Fatal("describe_lifecycle answered for a sale in another account")
	}
}

// TestListSalesRowsCarryAvailableEvents pins the chat half: the in-app chat reads
// list_sales, so the server's own answer must travel with every row. Without it
// the model is back to guessing from `status`.
func TestListSalesRowsCarryAvailableEvents(t *testing.T) {
	paid := &dbstore.Sale{
		RefCode: "JF-AVAIL-1", Status: dbstore.SalePaid, TotalSen: 1000,
		SaleType: dbstore.SaleTypeOneTime, PaymentMethod: "chip",
	}
	row := saleOutFromSale(paid)
	if len(row.AvailableEvents) == 0 {
		t.Fatal("a paid sale row carries no available_events — the chat would guess from status again")
	}
	if !reflect.DeepEqual(row.AvailableEvents, lifecycle.SalesMachine.Available(paid)) {
		t.Errorf("row available_events = %+v, want the table's answer", row.AvailableEvents)
	}

	cancelled := &dbstore.Sale{RefCode: "JF-AVAIL-2", Status: dbstore.SaleCancelled}
	row = saleOutFromSale(cancelled)
	if row.AvailableEvents == nil {
		t.Fatal("a terminal sale row serialises available_events as null; it must be an empty array")
	}
	if len(row.AvailableEvents) != 0 {
		t.Errorf("a cancelled sale row offers %+v", row.AvailableEvents)
	}
}
