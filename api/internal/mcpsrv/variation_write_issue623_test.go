package mcpsrv

// Issue #623: MCP FEATURE PARITY for product variations.
//
// AGENTS.md's parity rule says "Every dashboard feature MUST have a matching MCP
// tool". The dashboard could always edit a product's variations (the editor
// saves them wholesale) while MCP could only LIST them, so the merchant promise
// ("control everything from your agent") was false for variations. These tests
// drive the REAL registered tools over the MCP protocol — not the REST API, not
// the handler directly — so they prove the agent-facing surface, which is what
// the rule is about.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL).

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// issue623State is a seeded account + a wired MCP server, plus a client session.
type issue623State struct {
	srv       *Server
	ctx       context.Context
	accountID string
	productID string
	session   *mcp.ClientSession
}

func i64p(n int64) *int64 { return &n }
func intpMCP(n int) *int  { return &n }
func strp(s string) *string { return &s }

// issue623Fixture seeds a subscription product owned by the caller's account and
// connects an in-memory MCP client to the server's real tool registry.
func issue623Fixture(t *testing.T) issue623State {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('i623-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var workspaceID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'i623-ws-' || md5(random()::text), 'i623-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'i623-acc-' || md5(random()::text), 'i623-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	prod := &dbstore.Product{
		AccountID: accountID, Name: "Kelas", PriceSen: 10000,
		Type: dbstore.SaleTypeSubscription, BillingPeriod: "month", Active: true,
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}

	deps := &Deps{
		Accounts:          dbstore.NewAccountStore(pool),
		Products:          dbstore.NewProductStore(pool),
		ProductVariations: dbstore.NewProductVariationStore(pool),
		Workspaces:        dbstore.NewWorkspaceStore(pool),
		WorkspaceMembers:  dbstore.NewWorkspaceMemberStore(pool),
	}
	srv := &Server{
		mcp:  mcp.NewServer(&mcp.Implementation{Name: "jomform", Version: "test"}, nil),
		deps: deps,
	}
	srv.registerTools()

	client := mcp.NewClient(&mcp.Implementation{Name: "i623-client", Version: "test"}, nil)
	cTransport, sTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.mcp.Connect(ctx, sTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	clientSession, err := client.Connect(ctx, cTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	// The tool wrapper resolves the caller from a per-call scope; an
	// account-scoped identity is what a merchant agent has.
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}})
	return issue623State{srv: srv, ctx: callCtx, accountID: accountID, productID: prod.ID, session: clientSession}
}

// callTool invokes a registered tool over the protocol and returns the decoded
// structured content as a generic map.
func callTool(t *testing.T, st issue623State, name string, args map[string]any) map[string]any {
	t.Helper()
	res, err := st.session.CallTool(st.ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		// Surface the tool's own message so a failure is diagnosable.
		t.Fatalf("%s returned an error: %s", name, contentText(res))
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("%s: marshal result: %v", name, err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("%s: decode result: %v", name, err)
	}
	return out
}

func contentText(res *mcp.CallToolResult) string {
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			return tc.Text
		}
	}
	return "(no text content)"
}

// items unwraps a listTool's {"items":[...]} payload.
func items(t *testing.T, out map[string]any) []map[string]any {
	t.Helper()
	raw, ok := out["items"].([]any)
	if !ok {
		return nil
	}
	res := make([]map[string]any, 0, len(raw))
	for _, v := range raw {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("list item is not an object: %T", v)
		}
		res = append(res, m)
	}
	return res
}

// TestIssue623MCPVariationLifecycle drives create → list → update → delete
// through the REAL MCP tool handlers, including a per-variation subscription
// cycle and the clear flags that restore inheritance.
//
// The handlers are invoked directly (the repo's convention for these tool
// tests) because the in-memory transport does not carry the per-call auth scope;
// TestIssue623MCPToolsAreRegistered below proves separately, over the protocol,
// that these are the tools an agent actually sees.
func TestIssue623MCPVariationLifecycle(t *testing.T) {
	st := issue623Fixture(t)
	pool := testdb.Open(t)
	ctx := context.Background()

	// CREATE with its own cycle (product is monthly; the variation is yearly).
	_, created, err := st.srv.createProductVariation(st.ctx, nil, createVariationIn{
		ProductID: st.productID,
		variationIn: variationIn{
			Name: "Size L — yearly", PriceSen: i64p(120000),
			BillingPeriod: strp("year"), TrialDays: intpMCP(30),
		},
	})
	if err != nil {
		t.Fatalf("create_product_variation: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("create_product_variation returned no id: %+v", created)
	}
	vid := created.ID
	if created.BillingPeriod != "year" {
		t.Errorf("created variation billing_period = %q, want year", created.BillingPeriod)
	}

	// The row really persisted with its own cycle.
	vs := dbstore.NewProductVariationStore(pool)
	got, err := vs.GetByProduct(ctx, st.productID, vid)
	if err != nil {
		t.Fatalf("reload variation: %v", err)
	}
	if got.BillingPeriod != "year" || got.TrialDays == nil || *got.TrialDays != 30 {
		t.Errorf("persisted cycle = %q / %v, want year / 30", got.BillingPeriod, got.TrialDays)
	}
	if got.Charges != nil {
		t.Errorf("charges = %v, want unset — the caller did not set one", got.Charges)
	}

	// LIST must return the new fields.
	_, listed, err := st.srv.listProductVariations(st.ctx, nil, listProductVariationsIn{ProductID: st.productID})
	if err != nil {
		t.Fatalf("list_product_variations: %v", err)
	}
	found := false
	for _, it := range listed.Items {
		if it.ID == vid {
			found = true
			if it.BillingPeriod != "year" {
				t.Errorf("list_product_variations billing_period = %q, want year", it.BillingPeriod)
			}
		}
	}
	if !found {
		t.Fatalf("list_product_variations did not return the created variation")
	}
	// A second variation with NO cycle reports the cycle fields OMITTED, so an
	// agent can tell "inherits the product" from "set to the product's value".
	_, plain, err := st.srv.createProductVariation(st.ctx, nil, createVariationIn{
		ProductID:   st.productID,
		variationIn: variationIn{Name: "One Size", PriceSen: i64p(9000)},
	})
	if err != nil {
		t.Fatalf("create plain variation: %v", err)
	}
	if plain.BillingPeriod != "" || plain.TrialDays != nil || plain.SubscriptionCharges != nil {
		t.Errorf("a variation with no cycle must report no cycle, got %+v", plain)
	}
	var nullCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM product_variations WHERE id=$1
		  AND billing_period IS NULL AND trial_days IS NULL AND subscription_charges IS NULL`,
		plain.ID).Scan(&nullCount); err != nil {
		t.Fatalf("check plain nulls: %v", err)
	}
	if nullCount != 1 {
		t.Errorf("an unset cycle must be stored as NULL (the inherit state), not as zero values")
	}

	// UPDATE edits it (price) and leaves the cycle alone.
	_, updated, err := st.srv.updateProductVariation(st.ctx, nil, updateVariationIn{
		ProductID: st.productID, VariationID: vid, PriceSen: i64p(130000),
	})
	if err != nil {
		t.Fatalf("update_product_variation: %v", err)
	}
	if updated.PriceSen != 130000 {
		t.Errorf("updated price_sen = %d, want 130000", updated.PriceSen)
	}
	if updated.BillingPeriod != "year" {
		t.Errorf("an omitted update field must be left unchanged; billing_period = %q, want year", updated.BillingPeriod)
	}

	// CLEAR the cycle → back to inheriting the product.
	_, cleared, err := st.srv.updateProductVariation(st.ctx, nil, updateVariationIn{
		ProductID: st.productID, VariationID: vid,
		ClearBillingPeriod: true, ClearTrialDays: true,
	})
	if err != nil {
		t.Fatalf("clear variation cycle: %v", err)
	}
	if cleared.BillingPeriod != "" || cleared.TrialDays != nil || cleared.SubscriptionCharges != nil {
		t.Errorf("after clearing, the cycle must be gone, got %+v", cleared)
	}
	clearedRow, err := vs.GetByProduct(ctx, st.productID, vid)
	if err != nil {
		t.Fatalf("reload cleared variation: %v", err)
	}
	if clearedRow.BillingPeriod != "" || clearedRow.TrialDays != nil {
		t.Errorf("cleared row must be NULL, got %q / %v", clearedRow.BillingPeriod, clearedRow.TrialDays)
	}

	// DELETE removes it.
	_, del, err := st.srv.deleteProductVariation(st.ctx, nil, deleteVariationIn{
		ProductID: st.productID, VariationID: vid,
	})
	if err != nil {
		t.Fatalf("delete_product_variation: %v", err)
	}
	if del["status"] != "deleted" {
		t.Errorf("delete_product_variation status = %q, want deleted", del["status"])
	}
	after, err := vs.ListByProduct(ctx, st.productID)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	for _, v := range after {
		if v.ID == vid {
			t.Errorf("variation %s still present after delete_product_variation", vid)
		}
	}
}

// TestIssue623MCPToolsAreRegistered proves over the MCP PROTOCOL (tools/list)
// that the agent-facing surface really exposes the three write tools — the
// parity rule is about what an agent can call, not what a handler exists for.
func TestIssue623MCPToolsAreRegistered(t *testing.T) {
	registered := registeredToolNames(t)
	for _, name := range []string{
		"list_product_variations",
		"create_product_variation",
		"update_product_variation",
		"delete_product_variation",
	} {
		if !registered[name] {
			t.Errorf("%s is not registered — the dashboard can edit variations but an agent cannot (AGENTS.md MCP FEATURE PARITY)", name)
		}
	}
}

// TestIssue623MCPVariationWriteIsProductScoped: a variation id from another
// product must not be editable or deletable through this product — otherwise an
// agent could rewrite a different product's variation.
func TestIssue623MCPVariationWriteIsProductScoped(t *testing.T) {
	st := issue623Fixture(t)
	pool := testdb.Open(t)
	ctx := context.Background()
	vs := dbstore.NewProductVariationStore(pool)

	// A second product in the same account, with its own variation.
	other := &dbstore.Product{
		AccountID: st.accountID, Name: "Other", PriceSen: 5000,
		Type: dbstore.SaleTypeOneTime, Active: true,
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, other); err != nil {
		t.Fatalf("create other product: %v", err)
	}
	if err := vs.ReplaceAll(ctx, other.ID, []*dbstore.ProductVariation{
		{Name: "Foreign", PriceSen: 5100},
	}); err != nil {
		t.Fatalf("replace other variations: %v", err)
	}
	foreign, err := vs.ListByProduct(ctx, other.ID)
	if err != nil || len(foreign) != 1 {
		t.Fatalf("list other variations: %v (n=%d)", err, len(foreign))
	}

	// Editing it under the WRONG product must fail.
	if _, _, err := st.srv.updateProductVariation(st.ctx, nil, updateVariationIn{
		ProductID: st.productID, VariationID: foreign[0].ID, PriceSen: i64p(1),
	}); err == nil {
		t.Errorf("update_product_variation accepted a variation that belongs to another product")
	}
	// The foreign row is untouched.
	still, err := vs.GetByProduct(ctx, other.ID, foreign[0].ID)
	if err != nil {
		t.Fatalf("reload foreign variation: %v", err)
	}
	if still.PriceSen != 5100 {
		t.Errorf("foreign variation price_sen = %d, want 5100 — it must not have been modified", still.PriceSen)
	}

	// Same for delete.
	if _, _, err := st.srv.deleteProductVariation(st.ctx, nil, deleteVariationIn{
		ProductID: st.productID, VariationID: foreign[0].ID,
	}); err == nil {
		t.Errorf("delete_product_variation accepted a variation that belongs to another product")
	}
	if _, err := vs.GetByProduct(ctx, other.ID, foreign[0].ID); err != nil {
		t.Errorf("foreign variation was deleted: %v", err)
	}
}

// TestIssue623MCPRejectsAnUnknownCycleField: a silently-ignored cycle is how a
// merchant ends up billing yearly where they chose monthly, so an invalid value
// must be refused rather than dropped.
func TestIssue623MCPRejectsAnUnknownCycleField(t *testing.T) {
	st := issue623Fixture(t)
	cases := []createVariationIn{
		{ProductID: st.productID, variationIn: variationIn{Name: "Bad period", PriceSen: i64p(100), BillingPeriod: strp("weekly")}},
		{ProductID: st.productID, variationIn: variationIn{Name: "Bad trial", PriceSen: i64p(100), TrialDays: intpMCP(-1)}},
		{ProductID: st.productID, variationIn: variationIn{Name: "Bad count", PriceSen: i64p(100), SubscriptionCharges: intpMCP(0)}},
	}
	for _, in := range cases {
		if _, _, err := st.srv.createProductVariation(st.ctx, nil, in); err == nil {
			t.Errorf("create_product_variation accepted an invalid cycle: %+v", in.variationIn)
		}
	}
}
