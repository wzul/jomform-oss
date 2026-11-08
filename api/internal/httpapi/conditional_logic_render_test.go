package httpapi

// Card t_9e875bca — the builder offers conditional logic on every field block,
// and BOTH public renderers computed the block's CondJSON, but no template
// emitted it: the payment renderer's refresh script watched a [data-condition]
// selector that matched nothing, and the response renderer had no script at all.
// A merchant's rule therefore did nothing on any public form — the field stayed
// visible and its `required` blocked the submit.
//
// The tests below drive the REAL call sites (renderBlocks / renderResponseForm
// and the two submit routes), not the templates by hand, and they pin all three
// halves of the fix:
//
//  1. every conditional block renders inside a wrapper carrying the merchant's
//     own rule (data-condition) on BOTH renderers;
//  2. both renderers emit the SAME conditional script, and that script disables
//     a hidden block's controls (a `required` on a display:none control still
//     blocks the submit in Chrome);
//  3. the server applies the same rule to a submission: a hidden block is not
//     required and its answer is not stored, on a response form and on a
//     payment form.
//
// A hand-made template test cannot stand in for (1): the defect WAS the call
// site never reaching a template that emitted the attribute.

import (
	"context"
	"encoding/json"
	"html"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// conditionalRenderConfig is a stored config in the shape the builder writes:
// one plain text block, and one REQUIRED select shown only while that text block
// has an answer (t2 also carries the per-field description the panel offers).
func conditionalRenderConfig(mode string) map[string]any {
	cfg := map[string]any{
		"blocks": []any{
			map[string]any{"id": "t1", "type": "name", "value": "Nama"},
			map[string]any{
				"id": "t2", "type": "select", "value": "Jenis", "required": true,
				"description": "Hanya jika nama diisi",
				"condition":   map[string]any{"field_id": "t1", "op": "not_empty", "value": ""},
				"choices": []any{
					map[string]any{"label": "A", "value": "a"},
					map[string]any{"label": "B", "value": "b"},
				},
			},
		},
		"products": []any{},
	}
	if mode != "" {
		cfg["mode"] = mode
	}
	return cfg
}

// conditionAttr returns the value of the page's data-condition attribute as a
// browser reads it (the template escapes the quotes of the JSON, exactly as
// html/template does for every attribute value). "" when the page has none.
func conditionAttr(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`data-condition="([^"]*)"`).FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return html.UnescapeString(m[1])
}

// wrappedConditionalBlock returns the page slice from the block wrapper's
// opening tag to its matching close, counting nested <div>s (a description
// div). "" when the page carries no wrapper.
func wrappedConditionalBlock(body string) string {
	open := strings.Index(body, `<div class="jf-block"`)
	if open < 0 {
		return ""
	}
	depth, i := 0, open
	for i < len(body) {
		next := strings.Index(body[i:], "<div")
		closing := strings.Index(body[i:], "</div>")
		if closing < 0 {
			return ""
		}
		if next >= 0 && next < closing {
			depth++
			i += next + len("<div")
			continue
		}
		depth--
		i += closing + len("</div>")
		if depth == 0 {
			return body[open:i]
		}
	}
	return ""
}

// Acceptance #1: a stored condition reaches the page the buyer gets — as a
// wrapper around the whole block, carrying the merchant's rule — on a payment
// form AND on a response form. Before the fix neither page carried the attribute
// at all, so the script had nothing to act on.
func TestConditionAttributeReachesBothRenderers(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		control string
	}{
		{"payment", renderPaymentPage(t, "cond-pay", conditionalRenderConfig("")), `data-id="t2"`},
		{"response", renderResponsePage(t, "cond-resp", conditionalRenderConfig("response")), `<select name="t2" required>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if n := strings.Count(c.body, `<div class="jf-block"`); n != 1 {
				t.Errorf("%d conditional wrappers on the page, want 1 (t1 is unconditional)", n)
			}
			want := `{"field_id":"t1","op":"not_empty","value":""}`
			if got := conditionAttr(t, c.body); got != want {
				t.Errorf("data-condition = %q, want %q", got, want)
			}
			// The wrapper must contain the WHOLE block. A wrapper around the
			// label alone would leave the control — the thing the rule exists to
			// hide — on screen.
			inner := wrappedConditionalBlock(c.body)
			if inner == "" {
				t.Fatalf("no balanced conditional wrapper on the page")
			}
			for _, want := range []string{
				`<label class="lbl">Jenis (required)</label>`,
				c.control,
				`<div class="desc">Hanya jika nama diisi</div>`,
			} {
				if !strings.Contains(inner, want) {
					t.Errorf("the wrapped block is missing %q:\n%s", want, inner)
				}
			}
		})
	}
}

// Acceptance #2 (the client half): both renderers ship the SAME conditional
// script, and it deals with the browser constraint that made a hidden required
// field unsubmittable — a hidden block's controls are DISABLED, not just hidden.
func TestConditionalScriptIsOnBothRenderers(t *testing.T) {
	pages := map[string]string{
		"payment":  renderPaymentPage(t, "cond-pay2", conditionalRenderConfig("")),
		"response": renderResponsePage(t, "cond-resp2", conditionalRenderConfig("response")),
	}
	// The browser constraint that made this half necessary: a control that is
	// only hidden keeps running its constraint validation, and Chrome refuses
	// to submit ("An invalid form control with name=… is not focusable").
	const disables = "el.querySelectorAll('input,select,textarea').forEach(function(x){x.disabled=!show;});"
	for name, body := range pages {
		if n := strings.Count(body, blockConditionJS); n != 1 {
			t.Errorf("%s page carries the shared conditional script %d times, want 1", name, n)
		}
		if !strings.Contains(body, "querySelectorAll('[data-condition]')") {
			t.Errorf("%s page's conditional script does not read [data-condition]", name)
		}
		if !strings.Contains(body, disables) {
			t.Errorf("%s page's conditional script hides the block without disabling its controls, "+
				"so a hidden `required` field still blocks the submit", name)
		}
	}
}

// conditionalSubmitBlocks is the stored config both submit tests use: a plain
// text block and a REQUIRED email shown only while that text block has an answer.
func conditionalSubmitBlocks() []any {
	return []any{
		map[string]any{"id": "t1", "type": "name", "value": "Nama"},
		map[string]any{
			"id": "t2", "type": "email", "value": "Emel", "required": true,
			"condition": map[string]any{"field_id": "t1", "op": "not_empty", "value": ""},
		},
	}
}

// conditionalSubmitFixture seeds a user + workspace + account + one live product
// and two published forms whose whole field contract is config.blocks: a
// response-mode form and a payment (order) form, each carrying the conditional
// pair above.
func conditionalSubmitFixture(t *testing.T) (h *PublicHandlers, host, respSlug, orderSlug, accountID, prodID string) {
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

	respSlug = "it-cond-resp-" + accountID[:8]
	rf := &dbstore.Form{
		AccountID: accountID, Slug: respSlug, Title: "Pendaftaran",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{"mode": "response", "blocks": conditionalSubmitBlocks()},
	}
	if err := h.Forms.Create(ctx, rf); err != nil {
		t.Fatalf("create response form: %v", err)
	}

	orderSlug = "it-cond-order-" + accountID[:8]
	ob := append([]any{
		map[string]any{"id": "p1", "type": "product", "product_id": prod.ID, "value": "Tiket"},
	}, conditionalSubmitBlocks()...)
	of := &dbstore.Form{
		AccountID: accountID, Slug: orderSlug, Title: "Tiket Order",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{"blocks": ob},
	}
	if err := h.Forms.Create(ctx, of); err != nil {
		t.Fatalf("create order form: %v", err)
	}
	return h, host, respSlug, orderSlug, accountID, prodID
}

// storedSubmissionData reads back the answers the server RECORDED for subID.
func storedSubmissionData(t *testing.T, subID string) map[string]any {
	t.Helper()
	sub, err := dbstore.NewSubmissionStore(bundleTestPool(t)).ByID(context.Background(), subID)
	if err != nil {
		t.Fatalf("fetch submission: %v", err)
	}
	return sub.Data
}

// submittedID decodes the {status, id} body a successful submit returns.
func submittedID(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var resp struct {
		Status string `json:"status"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode submit response: %v (body %s)", err, rec.Body.String())
	}
	if resp.Status != "submitted" || resp.ID == "" {
		t.Fatalf("submit response = %+v, want status submitted + an id", resp)
	}
	return resp.ID
}

// Acceptance #2 (the server half): a response form whose required email block is
// hidden by a false condition accepts the submission, and the hidden block's
// answer is not stored — while the required marker still fires when the
// condition HOLDS (the neutralising arm that proves the 201s above are about the
// condition and not about required-checks disappearing).
func TestResponseSubmitAppliesBlockConditions(t *testing.T) {
	h, host, slug, _, _, _ := conditionalSubmitFixture(t)

	// The defect: t1 is empty, so t2 is hidden — the page never showed it, and
	// the submit must not be rejected for the field the buyer could not fill.
	rec := postSlug(t, h, host, slug, `{"fields":{"t1":""}}`)
	if rec.Code != 201 {
		t.Fatalf("hidden required block blocked the submit: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	data := storedSubmissionData(t, submittedID(t, rec))
	if _, ok := data["t2"]; ok {
		t.Errorf("the hidden block's answer was stored: %v", data)
	}
	if v, ok := data["t1"]; !ok || v != "" {
		t.Errorf("the visible block's answer was lost: %v", data)
	}

	// A direct POST that names the hidden field must not smuggle it in: the
	// client is not authoritative, the same rule applies to what is stored.
	rec = postSlug(t, h, host, slug, `{"fields":{"t1":"","t2":"sneak@example.test"}}`)
	if rec.Code != 201 {
		t.Fatalf("hidden block rejected a direct POST: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	data = storedSubmissionData(t, submittedID(t, rec))
	if _, ok := data["t2"]; ok {
		t.Errorf("a hand-crafted POST stored the hidden block's answer: %v", data)
	}

	// Neutralising arm: with t1 filled the condition HOLDS, so t2 is required
	// again and the same shape is refused. Without this the 201s above could
	// come from a server that stopped enforcing required blocks.
	rec = postSlug(t, h, host, slug, `{"fields":{"t1":"Ali"}}`)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "Emel is required") {
		t.Errorf("visible required block: status = %d body %s, want 400 naming Emel",
			rec.Code, rec.Body.String())
	}

	// And the visible pair round-trips.
	rec = postSlug(t, h, host, slug, `{"fields":{"t1":"Ali","t2":"ali@example.test"}}`)
	if rec.Code != 201 {
		t.Fatalf("visible required block filled: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	data = storedSubmissionData(t, submittedID(t, rec))
	if data["t1"] != "Ali" || data["t2"] != "ali@example.test" {
		t.Errorf("stored answers = %v, want both visible blocks", data)
	}
}

// The same rule on a payment form: the order path validates the form's block
// contract, so a hidden required block there must not reject the order either —
// and its answer must not reach sales.fields.
func TestFormOrderAppliesBlockConditions(t *testing.T) {
	h, host, _, orderSlug, _, prodID := conditionalSubmitFixture(t)

	items := `"items":[{"product_id":"` + prodID + `","quantity":1}],"payment_method":"bank_direct"`
	client := `"client":{"email":"buyer@example.test"},`

	rec := postSlug(t, h, host, orderSlug, `{`+client+`"fields":{"t1":""},`+items+`}`)
	if rec.Code != 201 {
		t.Fatalf("hidden required block blocked the order: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if got := storedSaleFields(t, orderRefCode(t, rec)); got["t2"] != nil {
		t.Errorf("the hidden block's answer reached sales.fields: %v", got)
	}

	// Neutralising arm: the condition holds, so the required block is enforced.
	rec = postSlug(t, h, host, orderSlug, `{`+client+`"fields":{"t1":"Ali"},`+items+`}`)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "Emel is required") {
		t.Errorf("visible required block: status = %d body %s, want 400 naming Emel",
			rec.Code, rec.Body.String())
	}

	// A direct POST naming the hidden field must not smuggle it in either.
	rec = postSlug(t, h, host, orderSlug, `{`+client+`"fields":{"t1":"","t2":"sneak@example.test"},`+items+`}`)
	if rec.Code != 201 {
		t.Fatalf("hidden block rejected a direct order POST: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if got := storedSaleFields(t, orderRefCode(t, rec)); got["t2"] != nil {
		t.Errorf("a hand-crafted order POST stored the hidden block's answer: %v", got)
	}
}

// orderRefCode decodes the {ref_code} a successful order returns.
func orderRefCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var resp struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.RefCode == "" {
		t.Fatalf("decode order response: %v (body %s)", err, rec.Body.String())
	}
	return resp.RefCode
}

// storedSaleFields reads the answers the server RECORDED for the sale with this
// ref code — the row itself, so the assertion is about what was stored and not
// about what the handler happened to pass around.
func storedSaleFields(t *testing.T, ref string) map[string]any {
	t.Helper()
	var raw []byte
	if err := bundleTestPool(t).QueryRow(context.Background(),
		`SELECT fields FROM sales WHERE ref_code = $1`, ref).Scan(&raw); err != nil {
		t.Fatalf("read sales.fields for %s: %v", ref, err)
	}
	out := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("decode sales.fields: %v", err)
		}
	}
	return out
}
