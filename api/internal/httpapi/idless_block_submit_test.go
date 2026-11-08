package httpapi

// Card t_e854b64c — a required builder block with NO id was still unenforced
// server-side (measured on prod v1.21.167: 6 required id-less blocks on 4
// published forms; POSTing no field values got PAST the contract and died
// later on "kaedah pembayaran tidak sah").
//
// formfields.Contract skipped every block whose id was empty, so such a form
// carried NO required-field enforcement on its own submit routes, while both
// public renderers still painted the block's control with key "" — the #254
// bypass class, for the older block shape. The fix derives one key per block in
// formfields.SubmitKeys ("_block_<index>" when the block has no id) and uses it
// in the contract AND in both renderers, so the key the server validates is the
// key the page submits.
//
// These tests measure the card's two acceptance halves on the REAL routes and
// the REAL renderers:
//
//   - a published form whose only required markers are id-less blocks refuses
//     POST {} / {"fields":{}} with 400 "<label> is required" and
//     stores nothing (response route) / no sale (order route);
//   - the page emits a control under exactly the key the contract validates, and
//     a payload built from the page's own keys is still accepted (201) — a
//     fallback key the page did not send would be a mass over-refusal instead;
//   - the neutralising arm repeats both with every `required` flag false and
//     asserts the pre-fix acceptance, so the refusals are shown to hinge on the
//     required markers.
//
// Integration tests; skipped unless JOMFORM_TEST_DATABASE_URL is set.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/formfields"
)

// formfieldsContractIDs lists the field ids the derived contract carries, in
// order — the keys the submit routes validate.
func formfieldsContractIDs(t *testing.T, cfg map[string]any) []string {
	t.Helper()
	var out []string
	for _, f := range formfields.Contract(cfg) {
		out = append(out, f.FieldID)
	}
	return out
}

// idlessBlocks is the legacy shape the card measured: a display block and two
// INPUT blocks that carry no id at all. Positions are load-bearing — the
// synthesized keys are _block_1 (name) and _block_2 (email).
func idlessBlocks(required bool) []any {
	return []any{
		map[string]any{"type": "section", "value": "Daftar"},
		map[string]any{"type": "name", "value": "Nama Penuh", "required": required},
		map[string]any{"type": "email", "value": "Emel", "required": required},
	}
}

// idlessBlockFixture seeds a user + workspace + account + one live product and
// two published forms whose whole field contract is id-less builder blocks: a
// response-mode form and a payment (order) form. required=false builds the same
// forms with the required flags off — the pre-fix behaviour.
func idlessBlockFixture(t *testing.T, required bool) (h *PublicHandlers, host, respSlug, orderSlug, respFormID, orderFormID, accountID, prodID string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var workspaceSlug string
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-ws-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING slug`, userID).Scan(&workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $2
		 RETURNING id`, userID, workspaceSlug).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	prod := &dbstore.Product{
		AccountID: accountID, Name: "Tiket", PriceSen: 1500,
		Type: dbstore.SaleTypeOneTime, Active: true,
	}
	if err := dbstore.NewProductStore(pool).Create(ctx, prod); err != nil {
		t.Fatalf("create product: %v", err)
	}
	prodID = prod.ID

	h = &PublicHandlers{
		Forms:             dbstore.NewFormStore(pool),
		Sales:             dbstore.NewSaleStore(pool),
		Accounts:          dbstore.NewAccountStore(pool),
		Products:          dbstore.NewProductStore(pool),
		ProductVariations: dbstore.NewProductVariationStore(pool),
		Workspaces:        dbstore.NewWorkspaceStore(pool),
		Submissions:       dbstore.NewSubmissionStore(pool),
		BaseURL:           "http://localhost:8080",
	}
	host = workspaceSlug + ".jomform.com"

	respSlug = "it-noid-resp-" + accountID[:8]
	rf := &dbstore.Form{
		AccountID: accountID, Slug: respSlug, Title: "Idless registration",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{"mode": "response", "blocks": idlessBlocks(required)},
	}
	if err := h.Forms.Create(ctx, rf); err != nil {
		t.Fatalf("create response form: %v", err)
	}
	respFormID = rf.ID

	orderSlug = "it-noid-order-" + accountID[:8]
	orderBlocks := append([]any{
		map[string]any{"id": "p1", "type": "product", "product_id": prod.ID, "value": "Tiket"},
	}, idlessBlocks(required)...)
	of := &dbstore.Form{
		AccountID: accountID, Slug: orderSlug, Title: "Idless order",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{"blocks": orderBlocks},
	}
	if err := h.Forms.Create(ctx, of); err != nil {
		t.Fatalf("create order form: %v", err)
	}
	orderFormID = of.ID
	return h, host, respSlug, orderSlug, respFormID, orderFormID, accountID, prodID
}

// TestIdlessRequiredBlockIsEnforcedOnTheResponseRoute is the card's main arm:
// the response page's keys and the contract's keys are the same value, the
// empty and partial POSTs are refused with the merchant's label, nothing is
// stored, and the payload the page itself produces is still accepted.
func TestIdlessRequiredBlockIsEnforcedOnTheResponseRoute(t *testing.T) {
	h, host, respSlug, _, respFormID, _, accountID, _ := idlessBlockFixture(t, true)
	ctx := context.Background()

	// The served page must key the two id-less INPUT blocks by the synthesized
	// key, and must still render the section block (display, not collected).
	body := renderResponsePage(t, respSlug, map[string]any{"mode": "response", "blocks": idlessBlocks(true)})
	for _, key := range []string{"_block_1", "_block_2"} {
		if tag := controlKeyedBy(body, key); tag == "" {
			t.Fatalf("the page emits no control keyed %q — the contract would require a key the page never submits (page: %s)", key, body)
		}
	}
	if !strings.Contains(body, "Nama Penuh") {
		t.Errorf("the page does not carry the name block's label")
	}

	cases := []struct {
		name string
		body string
		want string
	}{
		{"empty object", `{}`, "Nama Penuh is required"},
		{"empty fields map", `{"fields":{}}`, "Nama Penuh is required"},
		{"only the first id-less block", `{"fields":{"_block_1":"Ali"}}`, "Emel is required"},
		// The reported shape on prod: a direct POST with no field values at all.
		{"client only, no fields", `{"client":{"full_name":"Ali","email":"ali@example.test"}}`, "Nama Penuh is required"},
	}
	for _, c := range cases {
		rec := postSlug(t, h, host, respSlug, c.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body %s)", c.name, rec.Code, rec.Body.String())
			continue
		}
		if !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: body %s, want it to contain %q", c.name, rec.Body.String(), c.want)
		}
		if n := submissionCount(t, h, accountID, respFormID); n != 0 {
			t.Fatalf("%s: %d submissions stored for a refused POST, want 0", c.name, n)
		}
	}

	// What the page's own script submits for a filled form: the two keys the
	// controls carry. Accepted, and stored under those keys.
	bodyJSON := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"fields":{"_block_1":"Ali","_block_2":"ali@example.test"}}`
	rec := postSlug(t, h, host, respSlug, bodyJSON)
	if rec.Code != http.StatusCreated {
		t.Fatalf("filled submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if n := submissionCount(t, h, accountID, respFormID); n != 1 {
		t.Fatalf("submissions stored = %d, want 1", n)
	}
	sub, err := h.Submissions.ByID(ctx, resp.ID)
	if err != nil {
		t.Fatalf("fetch submission: %v", err)
	}
	if sub.Data["_block_1"] != "Ali" {
		t.Errorf("stored data[_block_1] = %v, want the answer the page submitted under that key", sub.Data["_block_1"])
	}
}

// TestIdlessRequiredBlockNeutralised is the neutralising arm: the same form with
// every required flag false accepts a bare {} — the pre-fix behaviour, which is
// what makes the refusals above a test of the fix rather than of some other
// rejection.
func TestIdlessRequiredBlockNeutralised(t *testing.T) {
	h, host, respSlug, _, respFormID, _, accountID, _ := idlessBlockFixture(t, false)

	if rec := postSlug(t, h, host, respSlug, `{}`); rec.Code != http.StatusCreated {
		t.Fatalf("no block is required: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if n := submissionCount(t, h, accountID, respFormID); n != 1 {
		t.Fatalf("submissions stored = %d, want 1", n)
	}
}

// TestIdlessRequiredBlockIsEnforcedOnTheFormOrderRoute: the same hole existed on
// the form's own payment route, where a direct POST could create a sale with no
// field values at all (the prod probe's "kaedah pembayaran tidak sah" was the
// check AFTER the empty contract).
func TestIdlessRequiredBlockIsEnforcedOnTheFormOrderRoute(t *testing.T) {
	h, host, _, orderSlug, _, _, accountID, prodID := idlessBlockFixture(t, true)
	ctx := context.Background()

	items := `"items":[{"product_id":"` + prodID + `","quantity":1}],"payment_method":"bank_direct"`
	client := `"client":{"full_name":"Ali","email":"ali@example.test"},`

	// The order form's blocks: the product block first, so the two id-less
	// INPUT blocks sit at indices 2 and 3. Their keys come from the same
	// derivation the route validates against — asserted positionally below.
	orderCfg := map[string]any{"blocks": append([]any{
		map[string]any{"id": "p1", "type": "product", "product_id": prodID, "value": "Tiket"},
	}, idlessBlocks(true)...)}
	keys := formfieldsContractIDs(t, orderCfg)
	if len(keys) != 2 || keys[0] != "_block_2" || keys[1] != "_block_3" {
		t.Fatalf("contract ids = %v, want the id-less input blocks keyed _block_2/_block_3", keys)
	}
	nameKey, emailKey := keys[0], keys[1]

	// The payment page keys the id-less blocks the same way (it collects
	// fields[el.dataset.id]).
	page := renderPaymentPage(t, orderSlug, orderCfg)
	for _, key := range keys {
		if tag := controlKeyedBy(page, key); tag == "" {
			t.Fatalf("the payment page emits no control keyed %q", key)
		}
	}

	rec := postSlug(t, h, host, orderSlug, `{`+client+items+`}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("order without fields: status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Nama Penuh is required") {
		t.Errorf("order without fields: body %s, want the id-less block's required message", rec.Body.String())
	}
	sales, err := h.Sales.ListByAccount(ctx, accountID, 10)
	if err != nil {
		t.Fatalf("list sales: %v", err)
	}
	if len(sales) != 0 {
		t.Fatalf("a refused order recorded %d sales, want 0", len(sales))
	}

	// One id-less answer given, the other missing — still refused, for the
	// block that is actually empty.
	rec = postSlug(t, h, host, orderSlug, `{`+client+`"fields":{"`+nameKey+`":"Ali"},`+items+`}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Emel is required") {
		t.Fatalf("order with one answer: status = %d body %s, want 400 for the second id-less block", rec.Code, rec.Body.String())
	}

	// Both answered under the page's own keys: accepted.
	rec = postSlug(t, h, host, orderSlug, `{`+client+`"fields":{"`+nameKey+`":"Ali","`+emailKey+`":"ali@example.test"},`+items+`}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("filled order status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if sales, err = h.Sales.ListByAccount(ctx, accountID, 10); err != nil {
		t.Fatalf("list sales: %v", err)
	} else if len(sales) != 1 {
		t.Fatalf("sales recorded = %d, want 1", len(sales))
	}
}

// TestIdlessRequiredBlockOrderNeutralised: the pre-fix behaviour of the order
// route (nothing required → an order with no fields is accepted).
func TestIdlessRequiredBlockOrderNeutralised(t *testing.T) {
	h, host, _, orderSlug, _, _, accountID, prodID := idlessBlockFixture(t, false)
	ctx := context.Background()

	body := `{"client":{"email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","quantity":1}],"payment_method":"bank_direct"}`
	if rec := postSlug(t, h, host, orderSlug, body); rec.Code != http.StatusCreated {
		t.Fatalf("no block is required: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	sales, err := h.Sales.ListByAccount(ctx, accountID, 10)
	if err != nil {
		t.Fatalf("list sales: %v", err)
	}
	if len(sales) != 1 {
		t.Fatalf("sales recorded = %d, want 1", len(sales))
	}
}

// TestContractAndBothRenderersKeyAnIdlessBlockIdentically is the "one rule, not
// two" assertion: for a config mixing an id-carrying block, an id-less one, a
// display block and a block whose real id LOOKS synthesized, the contract's
// field id and the control each renderer paints for that block are the same
// key.
func TestContractAndBothRenderersKeyAnIdlessBlockIdentically(t *testing.T) {
	cfg := map[string]any{"mode": "response", "blocks": []any{
		map[string]any{"id": "b1", "type": "text", "value": "Salam"},
		map[string]any{"type": "name", "value": "Nama", "required": true},
		map[string]any{"id": "_block_1", "type": "text", "value": "Sengaja"}, // real id that collides with the synthetic shape
		map[string]any{"type": "email", "value": "Emel", "required": true},
	}}

	contract := formfieldsContractIDs(t, cfg)
	want := []string{"b1", "_block_1_2", "_block_1", "_block_3"}
	if strings.Join(contract, ",") != strings.Join(want, ",") {
		t.Fatalf("contract ids = %v, want %v", contract, want)
	}

	response := renderResponsePage(t, "idless-keys", cfg)
	payment := renderPaymentPage(t, "idless-keys", cfg)
	for _, id := range contract {
		if controlKeyedBy(response, id) == "" {
			t.Errorf("the response page paints no control keyed %q (contract requires it)", id)
		}
		if controlKeyedBy(payment, id) == "" {
			t.Errorf("the payment page paints no control keyed %q (contract requires it)", id)
		}
	}
}
