package mcpsrv

// The MCP half of card `t_a58848bd`: an agent that switches a form's mode
// through `upsert_form` must get the same reconciled default notification the
// dashboard gets through PATCH /v1/rest/forms/{id}.
//
// WHY THIS IS NOT DUPLICATION OF THE REST TEST. AGENTS.md's MCP-parity rule says
// every dashboard capability has a matching MCP tool, and this repo guards the
// pair per defect rather than assuming the store seam covers both
// (`upsert_form_slug_test.go` is the same shape: "the MCP half of the carded
// defect"). `upsert_form` is the tool the docs tell an agent to use for "semula
// config penuh", so it is the path an agent actually takes to flip mode — and it
// reaches the reconcile through a different call site
// (mcpsrv/parity.go:609 → Forms.Update), which is exactly the layer a store-only
// guard cannot see.
//
// The observation is the rule the SEND path uses
// (`FormNotificationStore.ListByFormEvent`), not the tool's own response, so a
// tool that reported success while leaving a dead rule cannot pass.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when
// unset.

import (
	"context"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestUpsertFormModeChangeRePointsTheDefaultNotification(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, workspaceID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('i-mn-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'i-mn-ws-' || md5(random()::text), 'i-mn-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'i-mn-acc-' || md5(random()::text), 'i-mn-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	notif := dbstore.NewFormNotificationStore(pool)
	forms := dbstore.NewFormStore(pool)
	srv := &Server{deps: &Deps{
		Accounts:      dbstore.NewAccountStore(pool),
		Forms:         forms,
		Workspaces:    dbstore.NewWorkspaceStore(pool),
		Notifications: notif,
	}}
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}})

	// A payment form created the way the create path creates one, with the
	// default rule EnsureDefault installs.
	f := &dbstore.Form{
		AccountID: accountID, Slug: "i-mn-" + randHex(4), Title: "Order form",
		Config: map[string]any{"mode": "payment", "blocks": []any{}},
		Theme:  map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	if err := forms.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}
	if err := notif.EnsureDefault(ctx, f); err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}

	// The agent switches it to response mode through upsert_form — the tool the
	// docs tell it to use for a config rewrite.
	if _, _, err := srv.upsertForm(callCtx, nil, upsertFormIn{
		FormID: f.ID, Title: f.Title,
		Config: map[string]any{"mode": "response", "blocks": []any{}},
	}); err != nil {
		t.Fatalf("upsert_form switch to response mode: %v", err)
	}

	stored, err := forms.GetByID(ctx, accountID, f.ID)
	if err != nil {
		t.Fatalf("read back form: %v", err)
	}
	if dbstore.FormMode(stored) != dbstore.FormModeResponse {
		t.Fatalf("stored mode = %q, want response — upsert_form did not move the mode, so nothing "+
			"below is measurable", dbstore.FormMode(stored))
	}
	live, err := notif.ListByFormEvent(ctx, f.ID, dbstore.NotifyEventResponseSubmitted)
	if err != nil {
		t.Fatalf("list response.submitted rules: %v", err)
	}
	if len(live) != 1 {
		all, _ := notif.ListByForm(ctx, f.ID)
		t.Errorf("upsert_form moved the form to response mode but 0 of its rules fire "+
			"response.submitted — an agent that flips the mode would leave the merchant un-notified. "+
			"Rules now: %+v (issue #597's defect via the MCP lane, card t_a58848bd)", all)
	}
	all, err := notif.ListByForm(ctx, f.ID)
	if err != nil {
		t.Fatalf("list all rules: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("upsert_form's mode change left %d notification rules, want exactly 1 (%+v)", len(all), all)
	}
}
