package httpapi

// Card t_589dc006 — a response form's required markers live in
// config.blocks[].required (the shape every builder-made form has, and the
// shape of the live event-registration form). The submit routes used to
// validate ONLY against config.registered_fields, so such a form had no
// server-side required-field enforcement at all: a direct POST of {} stored an
// empty submission and fired the merchant's notification email (live-observed
// on prod 2026-10-10T12:43:34Z).
//
// The fix makes the form's OWN submit routes use the same blocks-derived
// contract the menu checkout already used (formfields.Contract — an explicit
// registered_fields contract still wins). These tests pin it:
//
//   - a blocks-only response form rejects {} / {"fields":{}} / a partially
//     filled payload with 400 and stores NOTHING;
//   - a blocks-only form ORDER rejects an order that supplies no fields;
//   - the fully filled payload is still accepted (201) on both routes;
//   - a NEUTRALISING arm repeats each case with every `required` flag flipped
//     to false and asserts the pre-fix acceptance, so the assertions are shown
//     to hinge on the required flags and not on some other rejection.
//
// Integration tests; skipped unless JOMFORM_TEST_DATABASE_URL is set.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// requiredBlocks is the field set both fixtures carry: name, email and date
// required, one optional textarea — no registered_fields anywhere.
func requiredBlocks(required bool) []any {
	return []any{
		map[string]any{"id": "nm", "type": "name", "value": "Nama", "required": required},
		map[string]any{"id": "em", "type": "email", "value": "Emel", "required": required},
		map[string]any{"id": "dt", "type": "date", "value": "Tarikh", "required": required},
		map[string]any{"id": "note", "type": "textarea", "value": "Nota"},
	}
}

// requiredBlocksFixture seeds a user + workspace + account + one live product
// and two published forms whose whole field contract is config.blocks: a
// response-mode form and a payment (order) form. required=false builds the same
// two forms with every required flag flipped — the pre-fix behaviour.
func requiredBlocksFixture(t *testing.T, required bool) (h *PublicHandlers, host, respSlug, orderSlug, respFormID, orderFormID, accountID, prodID string) {
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

	respSlug = "it-req-resp-" + accountID[:8]
	rf := &dbstore.Form{
		AccountID: accountID, Slug: respSlug, Title: "Event Registration",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{"mode": "response", "blocks": requiredBlocks(required)},
	}
	if err := h.Forms.Create(ctx, rf); err != nil {
		t.Fatalf("create response form: %v", err)
	}
	respFormID = rf.ID

	orderSlug = "it-req-order-" + accountID[:8]
	orderBlocks := append([]any{
		map[string]any{"id": "p1", "type": "product", "product_id": prod.ID, "value": "Tiket"},
	}, requiredBlocks(required)...)
	of := &dbstore.Form{
		AccountID: accountID, Slug: orderSlug, Title: "Tiket Order",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{"blocks": orderBlocks},
	}
	if err := h.Forms.Create(ctx, of); err != nil {
		t.Fatalf("create order form: %v", err)
	}
	orderFormID = of.ID
	return h, host, respSlug, orderSlug, respFormID, orderFormID, accountID, prodID
}

// postSlug posts body to /{slug}/order on host and returns the recorder.
func postSlug(t *testing.T, h *PublicHandlers, host, slug, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/"+slug+"/order", bytes.NewBufferString(body))
	req.Host = host
	setTestClientAddr(req, testIP(t))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func submissionCount(t *testing.T, h *PublicHandlers, accountID, formID string) int64 {
	t.Helper()
	n, err := h.Submissions.CountByAccountsForm(context.Background(), []string{accountID}, formID)
	if err != nil {
		t.Fatalf("count submissions: %v", err)
	}
	return n
}

// TestResponseSubmitEnforcesRequiredBlocks: an empty or partial POST to a
// blocks-only response form is refused with 400 and stores nothing — the
// merchant-inbox invariant (no row → no notification email; the notify call
// only runs after a successful create).
func TestResponseSubmitEnforcesRequiredBlocks(t *testing.T) {
	h, host, respSlug, _, respFormID, _, accountID, _ := requiredBlocksFixture(t, true)
	ctx := context.Background()

	cases := []struct {
		name string
		body string
		want string
	}{
		{"empty object", `{}`, "medan wajib tidak diisi: Nama"},
		{"empty fields map", `{"fields":{}}`, "medan wajib tidak diisi: Nama"},
		{"only the first required block", `{"fields":{"nm":"Ali"}}`, "medan wajib tidak diisi: Emel"},
		{"blank required value", `{"fields":{"nm":"   ","em":"ali@example.test","dt":"2026-10-10"}}`, "medan wajib tidak diisi: Nama"},
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

	// A payload that fills every required block is still accepted, exactly as
	// before the fix, and round-trips the values.
	body := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"fields":{"nm":"Ali","em":"ali@example.test","dt":"2026-10-10","note":"Ok"}}`
	rec := postSlug(t, h, host, respSlug, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("complete submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp.Status != "submitted" || resp.ID == "" {
		t.Fatalf("complete submit = %+v, want status submitted with an id", resp)
	}
	if n := submissionCount(t, h, accountID, respFormID); n != 1 {
		t.Fatalf("submissions stored = %d, want 1", n)
	}
	sub, err := h.Submissions.ByID(ctx, resp.ID)
	if err != nil {
		t.Fatalf("fetch submission: %v", err)
	}
	if sub.Data["dt"] != "2026-10-10" || sub.Data["note"] != "Ok" {
		t.Errorf("stored data = %v, want the submitted values", sub.Data)
	}
}

// TestResponseSubmitRequiredBlocksNeutralised is the neutralising arm: the same
// form with every required flag false accepts a bare {} — the pre-fix
// behaviour, which is what makes the assertions above a test of the fix rather
// than of some unrelated rejection.
func TestResponseSubmitRequiredBlocksNeutralised(t *testing.T) {
	h, host, respSlug, _, respFormID, _, accountID, _ := requiredBlocksFixture(t, false)

	if rec := postSlug(t, h, host, respSlug, `{}`); rec.Code != http.StatusCreated {
		t.Fatalf("no block is required: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if n := submissionCount(t, h, accountID, respFormID); n != 1 {
		t.Fatalf("submissions stored = %d, want 1", n)
	}
}

// TestFormOrderEnforcesRequiredBlocks: the same gap existed on a form's own
// payment route (order.go), so a direct POST could create a sale with no field
// values. It must be refused, with no sale row.
func TestFormOrderEnforcesRequiredBlocks(t *testing.T) {
	h, host, _, orderSlug, _, _, accountID, prodID := requiredBlocksFixture(t, true)
	ctx := context.Background()

	items := `"items":[{"product_id":"` + prodID + `","quantity":1}],"payment_method":"bank_direct"`
	client := `"client":{"full_name":"Ali","email":"ali@example.test"},`

	rec := postSlug(t, h, host, orderSlug, `{`+client+items+`}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("order without fields: status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "medan wajib tidak diisi: Nama") {
		t.Errorf("order without fields: body %s, want the required-block message", rec.Body.String())
	}
	sales, err := h.Sales.ListByAccount(ctx, accountID, 10)
	if err != nil {
		t.Fatalf("list sales: %v", err)
	}
	if len(sales) != 0 {
		t.Fatalf("a refused order recorded %d sales, want 0", len(sales))
	}

	full := `{` + client +
		`"fields":{"nm":"Ali","em":"ali@example.test","dt":"2026-10-10"},` + items + `}`
	rec = postSlug(t, h, host, orderSlug, full)
	if rec.Code != http.StatusCreated {
		t.Fatalf("complete order status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	sales, err = h.Sales.ListByAccount(ctx, accountID, 10)
	if err != nil {
		t.Fatalf("list sales: %v", err)
	}
	if len(sales) != 1 {
		t.Fatalf("sales recorded = %d, want 1", len(sales))
	}
}

// TestFormOrderRequiredBlocksNeutralised: the pre-fix behaviour of the order
// route (no block required → an order with no fields is accepted).
func TestFormOrderRequiredBlocksNeutralised(t *testing.T) {
	h, host, _, orderSlug, _, _, accountID, prodID := requiredBlocksFixture(t, false)
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
