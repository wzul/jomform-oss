package mcpsrv

// MCP parity for the publish-readiness guard: an agent must not be able to
// publish a payment form with nothing to sell silently, and must be told the
// same thing the dashboard is told when it tries.
//
// THE GAP (drop-off, not a crash). Three outside signups built a form, saved it
// as a draft and never published; the two draft payment forms had no products at
// all, so publishing them would have produced a form that could never record a
// sale. The dashboard now refuses that publish once and asks for an
// acknowledgement; the agent surface must apply the SAME rule (it is the same
// dbstore function, not a second copy) or MCP parity is a lie.
//
// Drives the real handlers over a real Postgres and reads the JSON/error an
// agent would receive. Skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"os"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestMCPPublishFormRefusesAPaymentFormWithNoProductsUntilAcknowledged(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping MCP publish-readiness test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('mcp-pub-' || md5(random()::text) || '@example.test', 'it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'mcp-pub-' || md5(random()::text), 'mcp-pub-' || md5(random()::text) RETURNING id`,
		userID,
	).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	forms := dbstore.NewFormStore(pool)
	// The production shape: payment mode, ordinary blocks, no `products` key.
	paid := &dbstore.Form{
		AccountID: accountID, Slug: "mcp-paid-" + accountID[:8], Title: "Contest Entry",
		Config: map[string]any{"mode": "payment", "blocks": []any{
			map[string]any{"id": "b1", "type": "email", "value": "Email"},
			map[string]any{"id": "b2", "type": "name", "value": "Name"},
		}},
		Theme: map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	// The control: a response form with no products is legitimate.
	rsvp := &dbstore.Form{
		AccountID: accountID, Slug: "mcp-rsvp-" + accountID[:8], Title: "RSVP",
		Config: map[string]any{"mode": "response", "blocks": []any{
			map[string]any{"id": "b1", "type": "email", "value": "Email"},
		}},
		Theme: map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	for _, f := range []*dbstore.Form{paid, rsvp} {
		if err := forms.Create(ctx, f); err != nil {
			t.Fatalf("create form %s: %v", f.Slug, err)
		}
	}

	srv := &Server{deps: &Deps{Forms: forms, Accounts: dbstore.NewAccountStore(pool)}}
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}})

	// 1. publish_form on the product-free payment form is refused, and the
	// refusal tells the agent the same thing the dashboard is told.
	if _, _, err := srv.publishFormMCP(callCtx, nil, publishFormIn{FormID: paid.ID}); err == nil {
		t.Fatal("publish_form published a payment form with no products without an acknowledgement")
	} else if err.Error() != errPublishNeedsAck {
		t.Errorf("refusal = %q, want the shared message %q", err.Error(), errPublishNeedsAck)
	}
	still, err := forms.GetByID(ctx, accountID, paid.ID)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if still.Status != dbstore.FormStatusDraft {
		t.Errorf("status after refusal = %q, want draft (a refused publish must change nothing)", still.Status)
	}

	// 2. The same call with acknowledge=true goes through.
	if _, _, err := srv.publishFormMCP(callCtx, nil,
		publishFormIn{FormID: paid.ID, Acknowledge: true}); err != nil {
		t.Fatalf("acknowledged publish_form: %v", err)
	}
	live, err := forms.GetByID(ctx, accountID, paid.ID)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if live.Status != dbstore.FormStatusPublished {
		t.Errorf("status after acknowledged publish = %q, want published", live.Status)
	}

	// 3. Re-publishing an already-live form never asks again.
	if _, _, err := srv.publishFormMCP(callCtx, nil, publishFormIn{FormID: paid.ID}); err != nil {
		t.Errorf("re-publishing an already-live form: %v", err)
	}

	// 4. A response form with no products publishes bare — never over-blocked.
	if _, _, err := srv.publishFormMCP(callCtx, nil, publishFormIn{FormID: rsvp.ID}); err != nil {
		t.Fatalf("publish_form on a response form with no products was refused (%v) — a product-free "+
			"response form is legitimate and must stay publishable", err)
	}
	got, err := forms.GetByID(ctx, accountID, rsvp.ID)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if got.Status != dbstore.FormStatusPublished {
		t.Errorf("RSVP status = %q, want published", got.Status)
	}
}

func TestMCPBulkFormsWithholdsProductFreePaymentForms(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping MCP publish-readiness test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('mcp-bulk-' || md5(random()::text) || '@example.test', 'it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'mcp-bulk-' || md5(random()::text), 'mcp-bulk-' || md5(random()::text) RETURNING id`,
		userID,
	).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	forms := dbstore.NewFormStore(pool)
	empty := &dbstore.Form{
		AccountID: accountID, Slug: "mcp-bulk-empty-" + accountID[:8], Title: "No Products",
		Config: map[string]any{"mode": "payment", "blocks": []any{
			map[string]any{"id": "b1", "type": "text", "value": "Notes"},
		}},
		Theme: map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	selling := &dbstore.Form{
		AccountID: accountID, Slug: "mcp-bulk-ok-" + accountID[:8], Title: "Tee Order",
		Config: map[string]any{"mode": "payment", "blocks": []any{
			map[string]any{"id": "b1", "type": "product", "product_id": "3f1e5c58-1111-2222-3333-444455556666"},
		}},
		Theme: map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	for _, f := range []*dbstore.Form{empty, selling} {
		if err := forms.Create(ctx, f); err != nil {
			t.Fatalf("create form %s: %v", f.Slug, err)
		}
	}

	srv := &Server{deps: &Deps{Forms: forms, Accounts: dbstore.NewAccountStore(pool)}}
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}})

	ids := []string{selling.ID, empty.ID}
	_, out, err := srv.bulkFormsMCP(callCtx, nil, bulkFormsIn{Action: "publish", IDs: ids})
	if err != nil {
		t.Fatalf("bulkFormsMCP: %v", err)
	}
	// The product-free id is withheld and reported: a bulk publish must not be
	// a way around the single-publish guard.
	if out["updated"] != int64(1) {
		t.Errorf("updated = %v, want 1 (only the form with a product)", out["updated"])
	}
	reported, _ := out["needs_products"].([]string)
	if len(reported) != 1 || reported[0] != empty.ID {
		t.Errorf("needs_products = %v, want exactly [%s]", out["needs_products"], empty.ID)
	}
	got, err := forms.GetByID(ctx, accountID, empty.ID)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if got.Status != dbstore.FormStatusDraft {
		t.Errorf("withheld form status = %q, want draft", got.Status)
	}

	// Acknowledged, both end up published — but only the withheld one CHANGES in
	// this call: the pass above already published `selling`, so re-sending it is
	// a no-op and is not counted. `updated` is the number of rows the call
	// changed (bulk_forms description: "updated hanya mengira baris yang
	// benar-benar berubah"), not the number of ids.
	_, out, err = srv.bulkFormsMCP(callCtx, nil,
		bulkFormsIn{Action: "publish", IDs: ids, Acknowledge: true})
	if err != nil {
		t.Fatalf("acknowledged bulkFormsMCP: %v", err)
	}
	if out["updated"] != int64(1) {
		t.Errorf("updated after ack = %v, want 1 (only the product-free form changed; %s was already published)",
			out["updated"], selling.Slug)
	}
	got, err = forms.GetByID(ctx, accountID, empty.ID)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if got.Status != dbstore.FormStatusPublished {
		t.Errorf("status after ack = %q, want published", got.Status)
	}
}
