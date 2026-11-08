package httpapi

// Card t_5eb85ec8 — a HIDDEN conditional identity block's answer must not reach
// the submission or the sale as client.full_name / client.email / client.phone.
//
// THE DEFECT (measured on prod, v1.21.202): a response form with a text gate and
// an email block whose rule is equals(gate,"go"). QA typed "go", filled the
// email, typed "stop" (the block hides: display:none, controls disabled,
// form.checkValidity() true) and submitted → 201, stored
//
//	{"email":"<the address>","f_gate":"stop","full_name":"stop",…}
//
// The hidden BLOCK's own key (f_mail) was correctly absent — VisibleAnswers had
// pruned it — but the page's client{full_name,email} mapping read the disabled
// control (a disabled control still holds the value typed while it was visible)
// and the response route re-adds client.email to the stored answers after the
// prune. So the answer the buyer's rule had withdrawn still reached the
// merchant, under the derived key the Buyer column / CSV / notification read.
//
// THE TWO HALVES, and why each test exists:
//  1. the PAGE must not derive an identity from a disabled control (both public
//     renderers: the response page's client{} mapping, and the payment page's
//     twin). Driven here by running the page's OWN scripts in the goja+cascadia
//     harness: the conditional script is executed, the gate is typed, the
//     identity blocks are filled WHILE VISIBLE, the gate is then set to the
//     hiding answer, and the form is submitted — the order the bug needs.
//  2. the SERVER must not re-add a hidden block's answer as the identity, because
//     a client is not authoritative: a hand-crafted POST can send client.email
//     whatever the page does. The order route must also stop demanding a
//     client.email its own page cannot fill (the hidden email block disables the
//     only control that could supply it), which used to make such a payment form
//     unorderable — 400 "Incomplete details" for a field never shown.
//
// BITE: re-introduce either half and the named test reds while the shipped
// 201-with-the-block-id-absent behaviour (card t_9e875bca, TestResponseSubmitAppliesBlockConditions)
// stays green — see the mutation table in the PR body.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// The gate both fixtures use. It is a COLLECTABLE type (textarea): a rule on a
// block type outside the contract is honoured by the page but invisible to
// VisibleAnswers (card t_7fb80479), which would make the server half of this
// card untestable for a reason that is not this card's.
const (
	identityGateField = "g1"
	identityGateShow  = "go"
	identityGateHide  = "stop"

	hiddenNameAnswer  = "QANAME-5eb85e"
	hiddenEmailAnswer = "qaname+5eb85e@example.test"
	hiddenPhoneAnswer = "60123456789"
	// What a hand-crafted POST (or a page that still read the disabled control)
	// would carry after the block was hidden.
	identityStaleEmail = "stale-5eb85e@example.test"
	identityStaleName  = "Ghost 5eb85e"
)

// conditionalIdentityBlocks is the stored config: one textarea gate, a
// name/email/phone block all gated on the SAME rule, and one always-visible
// textarea so the form is never empty and the gate's own answer is stored.
func conditionalIdentityBlocks(productBlock map[string]any) []any {
	gated := func(id, typ, label string) map[string]any {
		return map[string]any{
			"id": id, "type": typ, "value": label,
			"condition": map[string]any{"field_id": identityGateField, "op": "equals", "value": identityGateShow},
		}
	}
	blocks := []any{}
	if productBlock != nil {
		blocks = append(blocks, productBlock)
	}
	return append(blocks,
		map[string]any{"id": identityGateField, "type": "textarea", "value": "Pintu"},
		gated("nm1", "name", "Nama"),
		gated("em1", "email", "Emel"),
		gated("ph1", "phone", "Telefon"),
		map[string]any{"id": "note1", "type": "textarea", "value": "Nota"},
	)
}

// identityControl returns the control of a block. The two renderers key their
// controls differently — the response renderer names each one by its block id
// (and does not tag the identity arms with data-id), the payment renderer names
// the name/email/phone arms full_name/email/phone and tags every arm with
// data-id — so this accepts either, which is exactly the pair of lookups the
// pages' own conditional script uses.
func identityControl(t *testing.T, d *menuDOM, blockID string) *html.Node {
	t.Helper()
	n := d.find(func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return false
		}
		switch n.Data {
		case "input", "textarea", "select":
		default:
			return false
		}
		return menuAttr(n, "data-id") == blockID || menuAttr(n, "name") == blockID
	})
	if n == nil {
		t.Fatalf("the served page has no control for block %q — the block did not render", blockID)
	}
	return n
}

// driveGatedIdentitySubmit renders a page through the handler that serves it,
// runs the page's OWN two scripts in the shared DOM harness, and returns the
// body the page built for `gate` (the answer the gate is left holding).
//
// The identity answers are typed while the blocks are VISIBLE and the gate is
// then moved to the hiding answer: that is the sequence the defect needs, and a
// test that filled the fields while they were hidden would prove nothing about
// a disabled control's stale value.
func driveGatedIdentitySubmit(t *testing.T, body, submitScriptMarker, gate string) string {
	t.Helper()
	d := newPageDOM(t, body)
	d.orderResponse = map[string]any{"status": "submitted", "message": "ok", "checkout_url": "https://example.test/pay"}
	d.runPageScript(body, "document.querySelectorAll('[data-condition]')")

	gateCtl := identityControl(t, d, identityGateField)
	setMenuAttr(gateCtl, "value", identityGateShow)
	d.dispatch("input", gateCtl)

	for _, id := range []string{"nm1", "em1", "ph1"} {
		ctl := identityControl(t, d, id)
		if menuHasAttr(ctl, "disabled") {
			t.Fatalf("precondition failed: block %q is still disabled with the gate at %q — the page's "+
				"conditional script did not run, so this test would not exercise a hidden block", id, identityGateShow)
		}
		switch id {
		case "nm1":
			setMenuAttr(ctl, "value", hiddenNameAnswer)
		case "em1":
			setMenuAttr(ctl, "value", hiddenEmailAnswer)
		case "ph1":
			setMenuAttr(ctl, "value", hiddenPhoneAnswer)
		}
	}

	setMenuAttr(gateCtl, "value", gate)
	d.dispatch("input", gateCtl)
	if gate == identityGateHide {
		for _, id := range []string{"nm1", "em1", "ph1"} {
			if !menuHasAttr(identityControl(t, d, id), "disabled") {
				t.Fatalf("precondition failed: block %q is not disabled while its rule is false — "+
					"the hidden-block state this test is about was never reached", id)
			}
		}
	}

	// A payment page needs a line item or its script stops at the product alert.
	box := d.find(func(n *html.Node) bool {
		return n.Data == "input" && menuHasClass(n, "p")
	})
	if box != nil {
		setMenuAttr(box, "checked", "")
		d.dispatch("change", box)
	}

	d.runPageScript(body, submitScriptMarker)
	form := d.byID["of"]
	if form == nil {
		t.Fatalf("the served page has no #of form")
	}
	d.dispatch("submit", form)
	posted := d.storage["__posted_body"]
	if strings.TrimSpace(posted) == "" {
		t.Fatalf("the served page's own script posted no body (requests: %v)", d.fetches)
	}
	return posted
}

// Acceptance #1 (response page): once the rule is false, the page must not
// derive client.full_name/email/phone from the disabled controls, and the
// hidden blocks' own answers must stay out of the fields map — while the same
// page, submitted with the rule TRUE, still carries all of them. The second
// direction is what keeps this from being a test that "nothing is ever sent".
func TestResponsePageSendsNoHiddenIdentityAnswer(t *testing.T) {
	cfg := map[string]any{
		"mode":     "response",
		"blocks":   conditionalIdentityBlocks(nil),
		"products": []any{},
	}

	hidden := decodeServedSubmitBody(t, driveGatedIdentitySubmit(t, renderResponsePage(t, "identity-hidden", cfg), "body.client.full_name", identityGateHide))
	for _, key := range []string{"nm1", "em1", "ph1"} {
		if _, ok := hidden.Fields[key]; ok {
			t.Errorf("a hidden block's answer is in fields[%q]: %v", key, hidden.Fields)
		}
	}
	if hidden.Client.FullName != "" || hidden.Client.Email != "" || hidden.Client.Phone != "" {
		t.Errorf("the page derived an identity from HIDDEN blocks: %+v. A disabled control still holds the "+
			"answer typed while the block was visible, so reading it puts an answer the buyer's own rule "+
			"withdrew into client{} — where the server stores it as data[\"email\"]/%q/%q",
			hidden.Client, "full_name", "phone")
	}
	if hidden.Fields[identityGateField] != identityGateHide {
		t.Errorf("fields[%q] = %q, want the gate's own answer %q — the visible block must still submit",
			identityGateField, hidden.Fields[identityGateField], identityGateHide)
	}

	shown := decodeServedSubmitBody(t, driveGatedIdentitySubmit(t, renderResponsePage(t, "identity-shown", cfg), "body.client.full_name", identityGateShow))
	if got := shown.Client.FullName; got != hiddenNameAnswer {
		t.Errorf("control arm: client.full_name = %q, want %q (the rule is TRUE, the block is visible)", got, hiddenNameAnswer)
	}
	if got := shown.Client.Email; got != hiddenEmailAnswer {
		t.Errorf("control arm: client.email = %q, want %q", got, hiddenEmailAnswer)
	}
	if got := shown.Client.Phone; got != hiddenPhoneAnswer {
		t.Errorf("control arm: client.phone = %q, want %q", got, hiddenPhoneAnswer)
	}
	if shown.Fields["em1"] != hiddenEmailAnswer || shown.Fields["nm1"] != hiddenNameAnswer {
		t.Errorf("control arm: the visible blocks' answers are missing from fields: %v", shown.Fields)
	}
}

// Acceptance #1 (payment page): the twin mapping reads the email block's
// control by name and had no disabled check either, so a stale address travelled
// as client.email on a payment form too.
func TestPaymentPageSendsNoHiddenIdentityAnswer(t *testing.T) {
	cfg := map[string]any{
		"mode":     "payment",
		"blocks":   conditionalIdentityBlocks(map[string]any{"id": "p1", "type": "product", "product_id": "11111111-1111-1111-1111-111111111111", "value": "Tiket", "price_sen": 1500}),
		"products": []any{},
	}

	hidden := decodeServedSubmitBody(t, driveGatedIdentitySubmit(t, renderPaymentPage(t, "identity-pay-hidden", cfg), "live('[name=full_name]')", identityGateHide))
	if hidden.Client.Email != "" || hidden.Client.FullName != "" || hidden.Client.Phone != "" {
		t.Errorf("the payment page derived an identity from HIDDEN blocks: %+v — a disabled control still "+
			"holds the answer typed while the block was visible", hidden.Client)
	}
	if _, ok := hidden.Fields["em1"]; ok {
		t.Errorf("the hidden email block's answer is in fields: %v", hidden.Fields)
	}

	shown := decodeServedSubmitBody(t, driveGatedIdentitySubmit(t, renderPaymentPage(t, "identity-pay-shown", cfg), "live('[name=full_name]')", identityGateShow))
	if got := shown.Client.Email; got != hiddenEmailAnswer {
		t.Errorf("control arm: client.email = %q, want %q (the rule is TRUE, the block is visible)", got, hiddenEmailAnswer)
	}
}

// hiddenIdentityFixture seeds a user + workspace + account + one live product and
// two published forms whose whole contract is conditionalIdentityBlocks: a
// response form and a payment (order) form.
func hiddenIdentityFixture(t *testing.T) (h *PublicHandlers, host, respSlug, orderSlug, prodID string) {
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
	var accountID string
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

	respSlug = "it-ident-resp-" + accountID[:8]
	rf := &dbstore.Form{
		AccountID: accountID, Slug: respSlug, Title: "Pendaftaran",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{"mode": "response", "blocks": conditionalIdentityBlocks(nil)},
	}
	if err := h.Forms.Create(ctx, rf); err != nil {
		t.Fatalf("create response form: %v", err)
	}

	orderSlug = "it-ident-order-" + accountID[:8]
	of := &dbstore.Form{
		AccountID: accountID, Slug: orderSlug, Title: "Tiket Order",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{"blocks": conditionalIdentityBlocks(map[string]any{
			"id": "p1", "type": "product", "product_id": prod.ID, "value": "Tiket",
		})},
	}
	if err := h.Forms.Create(ctx, of); err != nil {
		t.Fatalf("create order form: %v", err)
	}
	return h, host, respSlug, orderSlug, prodID
}

// Acceptance #2 (response route): a hand-crafted POST that names the hidden
// blocks AND carries them as client.full_name/email/phone must store none of
// it — the client is not authoritative — while the same POST with the rule TRUE
// stores both the block answers and the derived identity.
func TestResponseSubmitDropsTheIdentityOfAHiddenBlock(t *testing.T) {
	h, host, respSlug, _, _ := hiddenIdentityFixture(t)

	body := `{"fields":{"` + identityGateField + `":"` + identityGateHide + `","note1":"hi"},` +
		`"client":{"full_name":"` + identityStaleName + `","email":"` + identityStaleEmail + `","phone":"` + hiddenPhoneAnswer + `"}}`
	rec := postSlug(t, h, host, respSlug, body)
	if rec.Code != 201 {
		t.Fatalf("hidden identity blocks rejected the submission: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	data := storedSubmissionData(t, submittedID(t, rec))
	for _, key := range []string{"nm1", "em1", "ph1", "full_name", "email", "phone"} {
		if v, ok := data[key]; ok {
			t.Errorf("stored data[%q] = %v — the answer of a block whose rule is FALSE reached the merchant, "+
				"even though its own key should have been pruned too (data: %v)", key, v, data)
		}
	}
	if data[identityGateField] != identityGateHide || data["note1"] != "hi" {
		t.Errorf("the visible answers were lost: %v", data)
	}

	// Control arm: the rule holds, so the blocks ARE the buyer's answers and the
	// identity must be recorded as it always was.
	body = `{"fields":{"` + identityGateField + `":"` + identityGateShow + `","note1":"hi",` +
		`"nm1":"` + hiddenNameAnswer + `","em1":"` + hiddenEmailAnswer + `","ph1":"` + hiddenPhoneAnswer + `"},` +
		`"client":{"full_name":"` + hiddenNameAnswer + `","email":"` + hiddenEmailAnswer + `","phone":"` + hiddenPhoneAnswer + `"}}`
	rec = postSlug(t, h, host, respSlug, body)
	if rec.Code != 201 {
		t.Fatalf("visible identity blocks rejected the submission: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	data = storedSubmissionData(t, submittedID(t, rec))
	for key, want := range map[string]string{
		"nm1": hiddenNameAnswer, "em1": hiddenEmailAnswer, "ph1": hiddenPhoneAnswer,
		"full_name": hiddenNameAnswer, "email": hiddenEmailAnswer, "phone": hiddenPhoneAnswer,
	} {
		if got := data[key]; got != want {
			t.Errorf("control arm: stored data[%q] = %v, want %q — a VISIBLE block's identity must still be "+
				"recorded (data: %v)", key, got, want, data)
		}
	}
}

// Acceptance #3 (order route): a payment form whose email block is hidden for
// this submission cannot be ordered today — the page's only control for
// client.email is disabled, so client.email is empty and the order route refuses
// the order with 400 "Incomplete details" for a field the buyer was never
// shown. The requirement must be skipped for exactly that submission, and the
// identity rule still applies to what is stored.
func TestFormOrderAcceptsAHiddenConditionalEmailBlock(t *testing.T) {
	h, host, _, orderSlug, prodID := hiddenIdentityFixture(t)
	items := `"items":[{"product_id":"` + prodID + `","quantity":1}],"payment_method":"bank_direct"`

	// The page the buyer gets: the gate is not "go", so the email block is
	// hidden and client.email is empty. This must order.
	rec := postSlug(t, h, host, orderSlug, `{"fields":{"`+identityGateField+`":"`+identityGateHide+`"},`+items+`}`)
	if rec.Code != 201 {
		t.Fatalf("a payment form whose email block is hidden for this submission could not be ordered: "+
			"status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	ref := orderRefCode(t, rec)
	if got := storedSaleClient(t, ref).Email; got != "" {
		t.Errorf("sale.client.email = %q, want empty — the form's own page cannot fill it", got)
	}
	if got := storedSaleFields(t, ref); got["em1"] != nil {
		t.Errorf("the hidden email block's answer reached sales.fields: %v", got)
	}

	// A hand-crafted POST may send a stale address anyway (typed while the block
	// was visible). The order route must not record it as the buyer.
	rec = postSlug(t, h, host, orderSlug, `{"client":{"email":"`+identityStaleEmail+`"},`+
		`"fields":{"`+identityGateField+`":"`+identityGateHide+`"},`+items+`}`)
	if rec.Code != 201 {
		t.Fatalf("hidden block + a client.email: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if got := storedSaleClient(t, orderRefCode(t, rec)).Email; got != "" {
		t.Errorf("sale.client.email = %q — a hidden block's answer was promoted to the buyer's identity by "+
			"the order route (the block's own key is pruned, the derived key must be too)", got)
	}

	// Neutralising arm: with the rule TRUE the email block is visible, the page
	// fills it, and the requirement still applies to a POST that omits it.
	rec = postSlug(t, h, host, orderSlug, `{"fields":{"`+identityGateField+`":"`+identityGateShow+`"},`+items+`}`)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "Incomplete details") {
		t.Errorf("a VISIBLE email block must still be required: status = %d body %s, want 400 Incomplete details",
			rec.Code, rec.Body.String())
	}

	// …and when it is supplied the order goes through and records it.
	rec = postSlug(t, h, host, orderSlug, `{"client":{"email":"`+hiddenEmailAnswer+`"},`+
		`"fields":{"`+identityGateField+`":"`+identityGateShow+`","em1":"`+hiddenEmailAnswer+`"},`+items+`}`)
	if rec.Code != 201 {
		t.Fatalf("visible email block supplied: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if got := storedSaleClient(t, orderRefCode(t, rec)).Email; got != hiddenEmailAnswer {
		t.Errorf("control arm: sale.client.email = %q, want %q", got, hiddenEmailAnswer)
	}
}

// storedSaleClient reads the buyer blob the server RECORDED for a sale.
func storedSaleClient(t *testing.T, ref string) dbstore.SaleClient {
	t.Helper()
	var raw []byte
	if err := bundleTestPool(t).QueryRow(context.Background(),
		`SELECT client FROM sales WHERE ref_code = $1`, ref).Scan(&raw); err != nil {
		t.Fatalf("read sales.client for %s: %v", ref, err)
	}
	var out dbstore.SaleClient
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("decode sales.client: %v (raw %s)", err, raw)
		}
	}
	return out
}
