package httpapi

// The MODE-CHANGE half of #597, driven through the REAL REST route (card
// `t_a58848bd`, found by QA on `t_f37d710d`).
//
// WHY THIS TEST EXISTS ON TOP OF THE STORE-LEVEL GUARDS. Those pin
// `FormStore.Update` and `ReconcileDefault`; they would still pass if the route
// stopped handing the config to the store — and the route is where the defect
// was observed. QA's reproduction is the live one: a form created as a payment
// form, switched to response mode in the builder, and its one default rule still
// reading `sale.paid`. So this drives the same bytes the builder sends
// (`{"title":…,"config":{…}}` on PATCH /v1/rest/forms/{id}) and then asserts the
// rule the SEND path actually looks for.
//
// The builder's payload shape is not a guess: web/src/app/builder/page.tsx sends
// `{ title, config }` for an existing form (the theme belongs to Form Settings),
// and updateForm passes `in.Config` straight through. If that ever changes, the
// reconcile stops running and this test is what goes red.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestRESTModeChangeRePointsTheDefaultNotification(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping REST mode-change test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, _ := bundleFixture(t, ctx, pool)
	key := bundleAPIKey(t, ctx, pool, acc)

	store := dbstore.NewFormNotificationStore(pool)
	h := newBundleHandlers(pool)
	h.Notifications = store

	// ── created as a PAYMENT form through the real create route ─────────────
	slug := "mc-rest-" + acc[:8]
	createBody, _ := json.Marshal(map[string]any{
		"slug": slug, "title": "Order Form",
		"config": map[string]any{"mode": "payment", "blocks": []any{}},
	})
	code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms", string(createBody))
	if code != http.StatusCreated {
		t.Fatalf("create payment form status = %d, body=%s", code, raw)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode created form: %v (%s)", err, raw)
	}
	// The pre-condition the defect is built on: the create route installed the
	// sale.paid default. If it had not, this test would be measuring a
	// different failure and its red below would be uninterpretable.
	before, err := store.ListByForm(ctx, created.ID)
	if err != nil {
		t.Fatalf("list rules after create: %v", err)
	}
	if len(before) != 1 || before[0].Event != dbstore.NotifyEventSalePaid {
		t.Fatalf("pre-condition: the create route installed %+v, want exactly one sale.paid default", before)
	}

	// ── the merchant switches it to response mode — the builder's own PATCH ─
	patchBody, _ := json.Marshal(map[string]any{
		"title":  "Order Form",
		"config": map[string]any{"mode": "response", "blocks": []any{}},
	})
	code, raw = tplReq(t, h, key, http.MethodPatch, "/v1/rest/forms/"+created.ID, string(patchBody))
	if code != http.StatusOK {
		t.Fatalf("switch to response mode via PATCH status = %d, body=%s", code, raw)
	}

	// The form really is a response form in the DATABASE (not merely in the
	// response payload), so the mode the reconcile read is the stored one.
	stored, err := dbstore.NewFormStore(pool).GetByID(ctx, acc, created.ID)
	if err != nil {
		t.Fatalf("read back form: %v", err)
	}
	if dbstore.FormMode(stored) != dbstore.FormModeResponse {
		t.Fatalf("stored mode = %q, want response — the PATCH did not move the mode, so the rest of "+
			"this test would prove nothing", dbstore.FormMode(stored))
	}

	// ── THE ASSERTION: the rule the SEND path looks for must exist ──────────
	// ListByFormEvent is the same call FireFormNotifications makes, so this can
	// never agree with the store while disagreeing with the sender.
	live, err := store.ListByFormEvent(ctx, created.ID, dbstore.NotifyEventResponseSubmitted)
	if err != nil {
		t.Fatalf("list response.submitted rules: %v", err)
	}
	if len(live) != 1 {
		all, _ := store.ListByForm(ctx, created.ID)
		t.Errorf("after switching form %s to response mode, 0 of its rules fire "+
			"response.submitted — the merchant is never told anyone responded. Rules now: %+v "+
			"(issue #597's defect, surviving on the mode-change path)", slug, all)
	} else {
		if live[0].Event != dbstore.NotifyEventResponseSubmitted {
			t.Errorf("live rule event = %q, want %q", live[0].Event, dbstore.NotifyEventResponseSubmitted)
		}
		_, _, wantSubject, wantBody := dbstore.DefaultNotification(dbstore.FormModeResponse)
		if live[0].Subject != wantSubject || live[0].Body != wantBody {
			t.Errorf("the re-pointed rule does not carry the response default template:\n"+
				"subject=%q\nwant:   %q", live[0].Subject, wantSubject)
		}
	}

	// Exactly one rule in total: the reconcile MOVES the default rather than
	// adding a second rule beside it (a duplicate would mail the merchant twice
	// for one response).
	all, err := store.ListByForm(ctx, created.ID)
	if err != nil {
		t.Fatalf("list all rules: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("response form %s has %d notification rules, want exactly 1 (%+v)", slug, len(all), all)
	}
}

// TestRESTConfigSaveThatKeepsTheModeLeavesTheDefaultAlone is the control on the
// route: the builder PATCHes its config on EVERY save, and the overwhelming
// majority of those saves do not move the mode. A reconcile that ran on every
// config write would rewrite the stored template on a save that changed
// nothing — and would thereby move `updated_at` and, worse, overwrite a
// merchant-edited rule whose edit happened to arrive through the same route.
func TestRESTConfigSaveThatKeepsTheModeLeavesTheDefaultAlone(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping REST mode-change control")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, _ := bundleFixture(t, ctx, pool)
	key := bundleAPIKey(t, ctx, pool, acc)

	store := dbstore.NewFormNotificationStore(pool)
	h := newBundleHandlers(pool)
	h.Notifications = store

	createBody, _ := json.Marshal(map[string]any{
		"slug": "mc-ctrl-" + acc[:8], "title": "RSVP",
		"config": map[string]any{"mode": "response", "blocks": []any{}},
	})
	code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms", string(createBody))
	if code != http.StatusCreated {
		t.Fatalf("create response form status = %d, body=%s", code, raw)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode created form: %v (%s)", err, raw)
	}
	before, err := store.ListByForm(ctx, created.ID)
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	if len(before) != 1 || before[0].Event != dbstore.NotifyEventResponseSubmitted {
		t.Fatalf("pre-condition: rules = %+v, want one response.submitted default", before)
	}

	// A save that adds a block but does not move the mode.
	patchBody, _ := json.Marshal(map[string]any{
		"title": "RSVP",
		"config": map[string]any{"mode": "response", "blocks": []any{
			map[string]any{"id": "b1", "type": "text", "value": "Welcome"},
		}},
	})
	code, raw = tplReq(t, h, key, http.MethodPatch, "/v1/rest/forms/"+created.ID, string(patchBody))
	if code != http.StatusOK {
		t.Fatalf("config save status = %d, body=%s", code, raw)
	}
	after, err := store.ListByForm(ctx, created.ID)
	if err != nil {
		t.Fatalf("list rules after: %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("rules = %+v, want the single untouched default", after)
	}
	if !after[0].UpdatedAt.Equal(before[0].UpdatedAt) {
		t.Errorf("a config save that did not change the mode rewrote the default rule "+
			"(updated_at %v -> %v)", before[0].UpdatedAt, after[0].UpdatedAt)
	}
	if after[0].ID != before[0].ID {
		t.Errorf("the default rule was replaced (id %s -> %s)", before[0].ID, after[0].ID)
	}
}
