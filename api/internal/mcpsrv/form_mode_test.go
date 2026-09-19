package mcpsrv

// the MCP form tools must expose the form's collection mode, so an
// agent can tell an order form from a response-only form (an RSVP/contact form)
// before suggesting a sales action — the same distinction the dashboard now
// makes. MCP feature parity requires the dashboard's fields to reach the agent.
//
// This drives the REAL handlers (s.listForms / s.getForm) over a real Postgres
// and decodes the JSON the agent would receive, rather than asserting on the
// struct definition. A struct-tag-only test would pass even if the field were
// never populated.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.
import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestMCPFormToolsExposeTheCollectionMode(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping MCP form-mode test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	// One user + account (API-key-shaped identity: AccountID, no UserID).
	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('mcp-mode-' || md5(random()::text) || '@example.test', 'it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'mcp-mode-' || md5(random()::text), 'mcp-mode-' || md5(random()::text) RETURNING id`,
		userID,
	).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	forms := dbstore.NewFormStore(pool)
	rsvp := &dbstore.Form{
		AccountID: accountID, Slug: "mcp-rsvp-" + accountID[:8], Title: "RSVP Form",
		Config: map[string]any{"mode": "response"}, Status: dbstore.FormStatusPublished,
	}
	order := &dbstore.Form{
		AccountID: accountID, Slug: "mcp-order-" + accountID[:8], Title: "Order Form",
		Config: map[string]any{"mode": "payment"}, Status: dbstore.FormStatusPublished,
	}
	legacy := &dbstore.Form{
		AccountID: accountID, Slug: "mcp-legacy-" + accountID[:8], Title: "Legacy Order Form",
		// No mode key: a pre-#171 form, and a genuine payment form.
		Config: map[string]any{"blocks": []any{}}, Status: dbstore.FormStatusPublished,
	}
	for _, f := range []*dbstore.Form{rsvp, order, legacy} {
		if err := forms.Create(ctx, f); err != nil {
			t.Fatalf("create form %s: %v", f.Slug, err)
		}
	}

	srv := &Server{deps: &Deps{Forms: forms, Accounts: dbstore.NewAccountStore(pool)}}
	// API-key identity: scoped to this account (no UserID → the account itself).
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}})

	// ── list ────────────────────────────────────────────────────────────────
	_, out, err := srv.listForms(callCtx, nil, emptyIn{})
	if err != nil {
		t.Fatalf("listForms: %v", err)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal listForms output: %v", err)
	}
	var rows []struct {
		ID   string `json:"id"`
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("decode listForms JSON: %v (%s)", err, raw)
	}
	got := map[string]string{}
	for _, r := range rows {
		got[r.ID] = r.Mode
	}
	want := map[string]struct {
		mode string
		why  string
	}{
		rsvp.ID:   {dbstore.FormModeResponse, "a response-only form must be distinguishable from a sales form"},
		order.ID:  {dbstore.FormModePayment, "an order form is a payment form"},
		legacy.ID: {dbstore.FormModePayment, "a form with no mode key predates response mode and is a payment form"},
	}
	for id, w := range want {
		m, ok := got[id]
		if !ok {
			t.Errorf("form %s missing from the MCP list output", id)
			continue
		}
		if m != w.mode {
			t.Errorf("MCP list mode for %s = %q, want %q — %s", id, m, w.mode, w.why)
		}
	}

	// ── get ─────────────────────────────────────────────────────────────────
	_, detail, err := srv.getForm(callCtx, nil, formRefIn{FormID: rsvp.ID})
	if err != nil {
		t.Fatalf("getForm: %v", err)
	}
	draw, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal getForm output: %v", err)
	}
	var one struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(draw, &one); err != nil {
		t.Fatalf("decode getForm JSON: %v (%s)", err, draw)
	}
	if one.Mode != dbstore.FormModeResponse {
		t.Errorf("MCP getForm mode = %q, want %q", one.Mode, dbstore.FormModeResponse)
	}
}
