package mcpsrv

// Card t_3d5a8d32 — the forms aggregate on the MCP surface.
//
// Two things an agent must be able to ask without guessing:
//
//   - get_form: is this form's buyer page submittable right now
//     (accepting_orders + reason/code), and what can I DO with it
//     (state + available_events)? Before the card MCP answered neither, so a
//     200 read told an agent nothing about a published form whose page refused
//     every buyer.
//   - describe_lifecycle aggregate=forms id=<form id>: the same table the write
//     paths enforce, for a form — including a TRASHED one, whose only remaining
//     events are restore and purge.
//
// This drives the real handlers over a real Postgres and decodes the JSON an
// agent receives, rather than asserting on struct tags (a tag-only test passes
// even when the handler never populates the field).
//
// Skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/lifecycle"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestMCPFormReadAnswersWhatABuyerCanDoAndWhatTheAgentMayDo(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping MCP form lifecycle test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('mcp-life-' || md5(random()::text) || '@example.test', 'it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'mcp-life-' || md5(random()::text), 'mcp-life-' || md5(random()::text) RETURNING id`,
		userID,
	).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	forms := dbstore.NewFormStore(pool)
	dead := &dbstore.Form{
		AccountID: accountID, Slug: "mcp-dead-" + accountID[:8], Title: "Derma",
		Config: map[string]any{"blocks": []any{
			map[string]any{"id": "nm1", "type": "name"},
			map[string]any{"id": "q5uhvoy", "type": "product", "value": ""},
		}},
		Status: dbstore.FormStatusPublished,
	}
	draft := &dbstore.Form{
		AccountID: accountID, Slug: "mcp-draft-" + accountID[:8], Title: "RSVP",
		Config: map[string]any{"mode": "response", "blocks": []any{
			map[string]any{"id": "nm1", "type": "name"},
		}},
		Status: dbstore.FormStatusDraft,
	}
	for _, f := range []*dbstore.Form{dead, draft} {
		if err := forms.Create(ctx, f); err != nil {
			t.Fatalf("create form %s: %v", f.Slug, err)
		}
	}

	srv := &Server{deps: &Deps{Forms: forms, Accounts: dbstore.NewAccountStore(pool)}}
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}})

	// ── get_form: the dead published form ───────────────────────────────────
	_, detail, err := srv.getForm(callCtx, nil, formRefIn{FormID: dead.ID})
	if err != nil {
		t.Fatalf("getForm: %v", err)
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal getForm: %v", err)
	}
	var one struct {
		Status          string                     `json:"status"`
		State           string                     `json:"state"`
		AcceptingOrders *bool                      `json:"accepting_orders"`
		Reason          string                     `json:"reason"`
		Code            string                     `json:"code"`
		NoProductBlock  string                     `json:"no_product_block_id"`
		AvailableEvents []lifecycle.AvailableEvent `json:"available_events"`
	}
	if err := json.Unmarshal(raw, &one); err != nil {
		t.Fatalf("decode getForm JSON: %v (%s)", err, raw)
	}
	if one.AcceptingOrders == nil {
		t.Fatalf("get_form carries no accepting_orders — an agent still has to guess: %s", raw)
	}
	if *one.AcceptingOrders {
		t.Error("a published payment form whose buyer page refuses every buyer reads as accepting")
	}
	if one.Code != lifecycle.FormOrderNoProductSelected {
		t.Errorf("code = %q, want %q", one.Code, lifecycle.FormOrderNoProductSelected)
	}
	if one.NoProductBlock != "q5uhvoy" {
		t.Errorf("no_product_block_id = %q, want q5uhvoy", one.NoProductBlock)
	}
	if one.Reason == "" {
		t.Error("a refused form carries no reason")
	}
	if one.State != dbstore.FormStatusPublished {
		t.Errorf("state = %q, want published", one.State)
	}
	events := map[string]lifecycle.AvailableEvent{}
	for _, ev := range one.AvailableEvents {
		events[ev.Event] = ev
	}
	if _, ok := events[lifecycle.FormEventPublish]; ok {
		t.Error("a published form offers publish on MCP — a no-op must not be offered as an action")
	}
	for _, want := range []string{lifecycle.FormEventUnpublish, lifecycle.FormEventDelete} {
		ev, ok := events[want]
		if !ok || !ev.Enabled {
			t.Errorf("a published form does not offer an enabled %s (got %+v)", want, ev)
		}
	}

	// ── get_form: the draft ─────────────────────────────────────────────────
	_, draftOut, err := srv.getForm(callCtx, nil, formRefIn{FormID: draft.ID})
	if err != nil {
		t.Fatalf("getForm draft: %v", err)
	}
	if draftOut.State != dbstore.FormStatusDraft {
		t.Errorf("draft state = %q, want draft", draftOut.State)
	}
	if draftOut.AcceptingOrders {
		t.Error("a draft reads as accepting orders")
	}
	if draftOut.Code != lifecycle.FormOrderNotPublished {
		t.Errorf("draft code = %q, want %q", draftOut.Code, lifecycle.FormOrderNotPublished)
	}

	// ── describe_lifecycle: the table, then one record ──────────────────────
	_, table, err := srv.describeLifecycle(callCtx, nil, describeLifecycleIn{Aggregate: "forms"})
	if err != nil {
		t.Fatalf("describeLifecycle(forms): %v", err)
	}
	if table.Aggregate != "forms" || len(table.States) == 0 {
		t.Fatalf("the forms table is not served: %+v", table)
	}
	states := map[string]bool{}
	for _, st := range table.States {
		states[st.State] = true
	}
	for _, want := range []string{
		dbstore.FormStatusDraft, dbstore.FormStatusPublished,
		dbstore.FormStateTemplateDraft, dbstore.FormStateTrashed, dbstore.FormStatePurged,
	} {
		if !states[want] {
			t.Errorf("the served forms table omits the state %q", want)
		}
	}

	_, rec, err := srv.describeLifecycle(callCtx, nil, describeLifecycleIn{Aggregate: "forms", ID: dead.ID})
	if err != nil {
		t.Fatalf("describeLifecycle(forms, id): %v", err)
	}
	if rec.State != dbstore.FormStatusPublished {
		t.Errorf("record state = %q, want published", rec.State)
	}
	if rec.AcceptingOrders == nil || *rec.AcceptingOrders {
		t.Error("the record answer's accepting_orders is missing or true for a dead form")
	}
	if len(rec.AvailableEvents) == 0 {
		t.Error("the record answer carries no available_events")
	}

	// ── a TRASHED form is a legitimate subject: restore/purge are the actions
	if err := forms.SoftDelete(ctx, accountID, dead.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	_, trashed, err := srv.describeLifecycle(callCtx, nil, describeLifecycleIn{Aggregate: "forms", ID: dead.ID})
	if err != nil {
		t.Fatalf("describeLifecycle(trashed form): %v", err)
	}
	if trashed.State != dbstore.FormStateTrashed {
		t.Errorf("trashed state = %q, want %q", trashed.State, dbstore.FormStateTrashed)
	}
	tEvents := map[string]bool{}
	for _, ev := range trashed.AvailableEvents {
		tEvents[ev.Event] = true
	}
	if len(tEvents) != 2 || !tEvents[lifecycle.FormEventRestore] || !tEvents[lifecycle.FormEventPurge] {
		t.Errorf("a trashed form offers %v, want restore and purge only", tEvents)
	}
	if trashed.AcceptingOrders == nil || *trashed.AcceptingOrders {
		t.Error("a trashed form reads as accepting orders")
	}
	if trashed.Code != lifecycle.FormOrderTrashed {
		t.Errorf("trashed code = %q, want %q", trashed.Code, lifecycle.FormOrderTrashed)
	}

	// ── purge is guarded by "this form was sold" ────────────────────────────
	// The store refuses to destroy a form that recorded a sale (409
	// form.hasSales), so the table must not tell an agent it may. The count
	// rides the TRASH read (dbstore.Form.SalesCount), which is what makes this
	// answerable from the row instead of a second query at the surface.
	if _, err := pool.Exec(ctx,
		`INSERT INTO sales (account_id, form_id, ref_code, client, items, total_sen)
		 VALUES ($1, $2, $3, '{}', '[]', 0)`,
		accountID, dead.ID, "mcp-life-"+dead.ID[:8],
	); err != nil {
		t.Fatalf("insert sale: %v", err)
	}
	got, err := forms.GetTrashed(ctx, accountID, dead.ID)
	if err != nil {
		t.Fatalf("GetTrashed after a sale: %v", err)
	}
	n, known := got.FormSalesCount()
	if !known || n != 1 {
		t.Fatalf("the trash read carries sales_count=%d (known=%v), want 1 — the purge guard would pass a sold form", n, known)
	}
	_, sold, err := srv.describeLifecycle(callCtx, nil, describeLifecycleIn{Aggregate: "forms", ID: dead.ID})
	if err != nil {
		t.Fatalf("describeLifecycle(sold trashed form): %v", err)
	}
	for _, ev := range sold.AvailableEvents {
		if ev.Event != lifecycle.FormEventPurge {
			continue
		}
		if ev.Enabled {
			t.Fatal("a form with sales is offered an ENABLED purge — the store answers 409 form.hasSales")
		}
		if !strings.Contains(ev.Reason, "sales") {
			t.Errorf("disabled purge reason = %q, want the sales rule", ev.Reason)
		}
	}
}
