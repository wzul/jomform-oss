package httpapi

// Card t_efd99d75 — the menu checkout rendered an attached form's
// `image-choice` block as a free-text box.
//
// `image-choice` is a collectable type, so the block reaches the checkout's
// derived contract carrying its `choices`, but menuPageTmpl's "menuField" table
// had no arm for it: it fell through to the `{{else}}` text input. The buyer was
// shown a box instead of the merchant's options and could type anything, and
// nothing server-side compared the answer to the choices either.
//
// Both halves are pinned here:
//   - the checkout paints one radio per declared choice, keyed by the block id
//     (the key the checkout script submits and ValidateFields enforces);
//   - the page's own collection script submits the checked radio's value under
//     that id and submits nothing when no radio is checked;
//   - the order route refuses an answer outside the choices and stores a
//     declared one.
//
// The neighbouring types this card left out were fixed by t_fda24726: `list`
// and `repeater` are painted as a <textarea rows="3"> by both public renderers,
// so the checkout paints them the same way (menu_list_repeater_t_fda24726_test.go,
// plus the control-kind guard in contract_renderer_parity_test.go).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/formfields"
)

// imageChoiceConfig is the block shape the card measured, run through the same
// derivation the menu page runs.
func imageChoiceConfig() map[string]any {
	return map[string]any{"mode": "response", "blocks": []any{
		map[string]any{"id": "ic1", "type": "image-choice", "value": "Pilih saiz", "required": true,
			"choices": []any{
				map[string]any{"label": "Kecil", "value": "k"},
				map[string]any{"label": "Besar", "value": "b"},
			}},
	}}
}

func imageChoiceMenuFixture(t *testing.T) (workspaceMenuData, []formfields.Field) {
	t.Helper()
	contract := menuExtraContract(imageChoiceConfig())
	if len(contract) != 1 || contract[0].Type != "image-choice" || len(contract[0].Choices) != 2 {
		t.Fatalf("fixture defect: menuExtraContract = %+v, want the image-choice block with both choices", contract)
	}
	d := menu492Fixture(t)
	d.ExtraFields = menuExtraFields(contract, formLangs["en"])
	return d, contract
}

// The checkout paints the merchant's choices as a radio group keyed by the
// block id — not a free-text input.
func TestMenuCheckoutRendersImageChoiceAsRadios(t *testing.T) {
	d, _ := imageChoiceMenuFixture(t)
	body := renderMenu492(t, d)

	tag := controlKeyedBy(body, "ic1")
	if tag == "" {
		t.Fatalf("the checkout renders no control for the image-choice block ic1")
	}
	if !strings.HasPrefix(tag, `<input type="radio"`) {
		t.Errorf("the checkout's control for an image-choice block is %s, want a radio — "+
			"the buyer cannot see the merchant's choices behind a text box", tag)
	}
	if !strings.Contains(tag, `value="k"`) {
		t.Errorf("the first radio does not carry the merchant's choice value: %s", tag)
	}
	if n := strings.Count(body, `name="ic1"`); n != 2 {
		t.Errorf("the checkout renders %d controls named ic1, want one radio per declared choice (2)", n)
	}
	for _, want := range []string{
		`name="ic1" value="b"`,                               // the second declared choice
		`<input type="radio" name="ic1" value="k" required>`, // required on the group
		"Pilih saiz (required)",                              // the merchant's label + the localised marker
		"Kecil",                                              // the merchant's choice labels, verbatim
		"Besar",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("menu checkout missing %q\nbody:\n%s", want, body)
		}
	}
	// The defect itself: the fallback text input must be gone.
	if strings.Contains(body, `<input type="text" name="ic1"`) {
		t.Error("the image-choice block still renders as the fallback free-text input")
	}
}

// The page's own script is what turns the radios into the `fields{}` payload the
// order route validates: the checked radio's value must arrive under the block
// id, and an unanswered group must submit nothing at all (that absence is what
// the server's required check sees).
func TestMenuCheckoutCollectsTheChosenImageChoice(t *testing.T) {
	d, _ := imageChoiceMenuFixture(t)
	dom, _ := newMenuDOMPage(t, d)
	if _, err := dom.vm.RunString(`
		var __r = document.querySelector('input[type=radio][name="ic1"][value="b"]');
		if (!__r) throw new Error('no radio for the merchant choice b');
		__r.checked = true;`); err != nil {
		t.Fatalf("select the merchant's choice in the served page: %v", err)
	}
	v, err := dom.vm.RunString(`JSON.stringify(jfMenuExtraFields())`)
	if err != nil {
		t.Fatalf("run the page's jfMenuExtraFields(): %v", err)
	}
	var fields map[string]string
	if err := json.Unmarshal([]byte(v.String()), &fields); err != nil {
		t.Fatalf("decode jfMenuExtraFields() = %q: %v", v.String(), err)
	}
	if fields["ic1"] != "b" {
		t.Errorf("the checkout submitted fields[ic1] = %q, want the checked choice \"b\" (payload %q)",
			fields["ic1"], v.String())
	}

	// Nothing checked → no answer, so the required arm refuses the order instead
	// of recording the last radio's value.
	d2, _ := imageChoiceMenuFixture(t)
	dom2, _ := newMenuDOMPage(t, d2)
	v2, err := dom2.vm.RunString(`JSON.stringify(jfMenuExtraFields())`)
	if err != nil {
		t.Fatalf("run jfMenuExtraFields() on an unanswered page: %v", err)
	}
	var none map[string]string
	if err := json.Unmarshal([]byte(v2.String()), &none); err != nil {
		t.Fatalf("decode jfMenuExtraFields() = %q: %v", v2.String(), err)
	}
	if _, ok := none["ic1"]; ok {
		t.Errorf("an unanswered image-choice group submitted ic1 = %q; the buyer chose nothing", none["ic1"])
	}
}

// A block that declares NO choices has nothing to draw a radio per choice for.
// Painting an empty radio group would leave a required field nobody can answer,
// which blocks every order on that menu — so the arm falls back to the text
// input the checkout used before, and the server (which only compares against
// declared choices) accepts the answer. Pinned so a future "always radios" edit
// cannot silently make that menu unorderable.
func TestMenuCheckoutKeepsAnEmptyImageChoiceAnswerable(t *testing.T) {
	cfg := map[string]any{"mode": "response", "blocks": []any{
		map[string]any{"id": "ic0", "type": "image-choice", "value": "Pilih saiz", "required": true},
	}}
	contract := menuExtraContract(cfg)
	if len(contract) != 1 || len(contract[0].Choices) != 0 {
		t.Fatalf("fixture defect: menuExtraContract = %+v, want the choice-less block", contract)
	}
	d := menu492Fixture(t)
	d.ExtraFields = menuExtraFields(contract, formLangs["en"])
	body := renderMenu492(t, d)

	if !strings.Contains(body, `name="ic0" value=""`) {
		t.Fatalf("a required image-choice with no choices paints no answerable control\nbody:\n%s", body)
	}
	if strings.Contains(body, `<input type="radio" name="ic0"`) {
		t.Error("a choice-less image-choice painted a radio group with no choices in it")
	}
	if err := formfields.ValidateFields(contract, map[string]string{"ic0": "apa-apa"}); err != nil {
		t.Errorf("a choice-less image-choice refused an answer: %v", err)
	}
	// And the control must be INSIDE its label — a stray closing tag would leave
	// it outside, so the buyer would see no accessible name and axe would flag an
	// unlabelled input. Driven through the page's own document, not the source.
	dom, _ := newMenuDOMPage(t, d)
	v, err := dom.vm.RunString(`(() => {
		var el = document.querySelector('[name=ic0]');
		var l = el && el.closest('label');
		return JSON.stringify(l ? l.textContent.trim() : null);
	})()`)
	if err != nil {
		t.Fatalf("read the choice-less control's label from the page: %v", err)
	}
	if got := strings.Trim(v.String(), `"`); got != "Pilih saiz (required)" {
		t.Errorf("the choice-less control's label = %s, want the merchant label inside its own <label>", v.String())
	}
}

// End to end through the order route: a declared choice is accepted and stored
// under the block id, a value the merchant never offered is refused.
func TestMenuOrderRefusesAnImageChoiceTheMerchantNeverOffered(t *testing.T) {
	h, workspaceSlug, accountID, prodID, _ := menuOrderFixture(t)
	ctx := context.Background()
	host := workspaceSlug + ".jomform.com"

	formSlug := "it-efd99d75-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "Extra info", Status: "published",
		Config: imageChoiceConfig(),
	}
	if err := h.Forms.Create(ctx, f); err != nil {
		t.Fatalf("create extra form: %v", err)
	}

	menuForm := &dbstore.Form{
		AccountID: accountID, Slug: "it-efd99d75-menu-" + accountID[:8], Title: "Menu",
		Status: "published",
		Config: map[string]any{
			"blocks": []any{
				map[string]any{"id": "p1", "type": "product", "product_id": prodID, "value": "Nasi Lemak"},
			},
			"menu": map[string]any{"enabled": true, "categories": []any{}},
		},
	}
	if err := h.Forms.Create(ctx, menuForm); err != nil {
		t.Fatalf("create menu form: %v", err)
	}
	menuForm.Config = dbstore.WithMenu(menuForm.Config,
		&dbstore.MenuPage{Enabled: true, OrderFormSlug: formSlug})
	if err := h.Forms.Update(ctx, accountID, menuForm.ID, menuForm.Title, menuForm.Config, nil); err != nil {
		t.Fatalf("attach extra form: %v", err)
	}

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/"+menuForm.Slug+"/order", bytes.NewBufferString(body))
		req.Host = host
		req.RemoteAddr = uniqueTestIP() + ":1234"
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	base := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","quantity":1}],` +
		`"payment_method":"bank_direct"`

	// A typed answer the merchant never offered — the exact bug: before the fix
	// this created the order and recorded the string.
	rec := post(base + `,"fields":{"ic1":"anything at all"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an undeclared image-choice answer: status = %d, want 400 (body %s)",
			rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "Pilih saiz") {
		t.Errorf("refusal does not name the merchant's field: %s", body)
	}

	// No answer at all → still the required refusal.
	if rec := post(base + `}`); rec.Code != http.StatusBadRequest {
		t.Errorf("an unanswered required image-choice: status = %d, want 400", rec.Code)
	}

	// A declared choice → created, and the answer persisted under the block id.
	rec = post(base + `,"fields":{"ic1":"b"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("a declared image-choice answer: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		RefCode string `json:"ref_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	sale, err := h.Sales.ByRefCode(ctx, resp.RefCode)
	if err != nil {
		t.Fatalf("fetch sale: %v", err)
	}
	fields, err := h.Sales.SaleFields(ctx, sale.ID)
	if err != nil {
		t.Fatalf("sale fields: %v", err)
	}
	if fields["ic1"] != "b" {
		t.Errorf("sale fields = %+v, want ic1 = the chosen value b", fields)
	}
}

// Card t_7c931910 — the page's own required guard could not see an unanswered
// radio group.
//
// The guard read `re.value` for every non-checkbox control. For a radio that is
// the value of the FIRST radio in querySelectorAll order, not the checked one,
// so a required group whose first choice carries any value always passed: the
// order left the page and only the server's 400 stopped it (measured on prod,
// v1.22.1). Driven through the served page — the guard must refuse locally with
// no POST, and must submit the CHECKED choice once one is picked.
func TestMenuCheckoutGuardRefusesAnUnansweredRadioGroup(t *testing.T) {
	d, _ := imageChoiceMenuFixture(t)
	dom, _ := newMenuDOMPage(t, d)
	if dom.buttonFor("p1") == nil {
		t.Fatal("fixture defect: the menu has no card button for p1")
	}
	dom.dispatch("click", dom.buttonFor("p1"))
	if n := len(dom.cart()); n != 1 {
		t.Fatalf("adding the dish put %d line(s) in the basket, want 1", n)
	}
	setMenuAttr(dom.byID["menuName"], "value", "Ali")
	setMenuAttr(dom.byID["menuEmail"], "value", "ali@example.test")
	form := dom.byID["menuCheckout"]
	orderPosts := func() []string {
		var out []string
		for _, u := range dom.fetches {
			if strings.HasSuffix(u, "/order") {
				out = append(out, u)
			}
		}
		return out
	}

	// (a) Nothing in the group is checked — the page must refuse before the
	// order is sent, the way it does for a required text field.
	dom.dispatch("submit", form)
	dom.flushTimers()
	if posts := orderPosts(); len(posts) != 0 {
		t.Fatalf("an unanswered required radio group still POSTed %v — the page's own guard let it "+
			"through and only the server refused (the buyer sees a round trip instead of the local error)", posts)
	}
	if body, ok := dom.storage["__posted_body"]; ok && body != "" {
		t.Errorf("an unanswered required radio group was submitted: %s", body)
	}
	errEl := dom.byID["menuErr"]
	if errEl == nil {
		t.Fatal("the page has no #menuErr surface")
	}
	if menuHasAttr(errEl, "hidden") {
		t.Error("the guard refused the order but showed the buyer no error")
	}
	if want := menuAttr(dom.byID["menuSheet"], "data-lbl-required"); nodeText(errEl) != want {
		t.Errorf("#menuErr = %q, want the page's own required message %q", nodeText(errEl), want)
	}

	// (b) The group IS answered — the LAST radio, so passing cannot be the first
	// radio's value leaking through.
	if _, err := dom.vm.RunString(`document.querySelector('input[type=radio][name="ic1"][value="b"]').checked = true;`); err != nil {
		t.Fatalf("select the merchant's second choice in the served page: %v", err)
	}
	dom.dispatch("submit", form)
	dom.flushTimers()
	posted, ok := dom.storage["__posted_body"]
	if !ok || posted == "" {
		t.Fatalf("a checked radio group posted nothing (requests: %v)", dom.fetches)
	}
	var order struct {
		Fields map[string]string `json:"fields"`
	}
	if err := json.Unmarshal([]byte(posted), &order); err != nil {
		t.Fatalf("decode the posted order body %q: %v", posted, err)
	}
	if order.Fields["ic1"] != "b" {
		t.Errorf("posted fields[ic1] = %q, want the checked choice \"b\" (body %s)", order.Fields["ic1"], posted)
	}
}

// The same guard must still refuse a blank required TEXT field after the group
// passes: the radio fix sits in the loop every required control goes through, so
// the non-radio arm is pinned next to it rather than assumed untouched.
func TestMenuCheckoutGuardStillRefusesABlankRequiredTextField(t *testing.T) {
	cfg := map[string]any{"mode": "response", "blocks": []any{
		map[string]any{"id": "ic1", "type": "image-choice", "value": "Pilih saiz", "required": true,
			"choices": []any{
				map[string]any{"label": "Kecil", "value": "k"},
				map[string]any{"label": "Besar", "value": "b"},
			}},
		map[string]any{"id": "tx1", "type": "text", "value": "Nama syarikat", "required": true},
	}}
	contract := menuExtraContract(cfg)
	if len(contract) != 2 || contract[0].Type != "image-choice" || contract[1].Type != "text" {
		t.Fatalf("fixture defect: menuExtraContract = %+v, want the image-choice then the text block", contract)
	}
	d := menu492Fixture(t)
	d.ExtraFields = menuExtraFields(contract, formLangs["en"])
	dom, _ := newMenuDOMPage(t, d)
	dom.dispatch("click", dom.buttonFor("p1"))
	setMenuAttr(dom.byID["menuName"], "value", "Ali")
	setMenuAttr(dom.byID["menuEmail"], "value", "ali@example.test")
	if _, err := dom.vm.RunString(`document.querySelector('input[type=radio][name="ic1"][value="k"]').checked = true;`); err != nil {
		t.Fatalf("select a choice in the served page: %v", err)
	}
	dom.dispatch("submit", dom.byID["menuCheckout"])
	dom.flushTimers()
	for _, u := range dom.fetches {
		if strings.HasSuffix(u, "/order") {
			t.Fatalf("a blank required text field still POSTed %s — the guard's non-radio arm regressed", u)
		}
	}
	if menuHasAttr(dom.byID["menuErr"], "hidden") {
		t.Error("the guard refused the blank text field but showed the buyer no error")
	}
}

// Card t_7c931910 — the radio group had no programmatic name.
//
// Both radio-drawing arms published the group heading as a SIBLING <label> with
// no `for` and no labelable descendant, which labels nothing, so the group was
// unnamed for assistive tech. They now wrap the group in <fieldset><legend>,
// the native technique the W3C APG names a radio group with
// (https://www.w3.org/WAI/ARIA/apg/practices/names-and-descriptions/). Asserted
// through the served page's DOM, not the template source, and for BOTH arms —
// `radio` had the defect before `image-choice` inherited it.
func TestMenuCheckoutRadioGroupsCarryAnAccessibleName(t *testing.T) {
	for _, tc := range []struct{ typ, id string }{
		{"image-choice", "ic1"},
		{"radio", "rd1"},
	} {
		t.Run(tc.typ, func(t *testing.T) {
			cfg := map[string]any{"mode": "response", "blocks": []any{
				map[string]any{"id": tc.id, "type": tc.typ, "value": "Pilih saiz", "required": true,
					"choices": []any{
						map[string]any{"label": "Kecil", "value": "k"},
						map[string]any{"label": "Besar", "value": "b"},
					}},
			}}
			contract := menuExtraContract(cfg)
			if len(contract) != 1 || contract[0].Type != tc.typ || len(contract[0].Choices) != 2 {
				t.Fatalf("fixture defect: menuExtraContract = %+v, want the %s block with both choices", contract, tc.typ)
			}
			d := menu492Fixture(t)
			d.ExtraFields = menuExtraFields(contract, formLangs["en"])
			dom, body := newMenuDOMPage(t, d)

			v, err := dom.vm.RunString(`(() => {
				var rs = document.querySelectorAll('input[type=radio][name="` + tc.id + `"]');
				if (rs.length !== 2) return JSON.stringify({radios: rs.length});
				var fs = rs[0].closest('fieldset');
				var lg = fs ? fs.querySelector('legend') : null;
				return JSON.stringify({
					radios: rs.length,
					grouped: !!fs,
					bothInside: !!fs && rs[1].closest('fieldset') === fs,
					legend: lg ? lg.textContent.trim() : null
				});
			})()`)
			if err != nil {
				t.Fatalf("read the radio group's structure from the page: %v", err)
			}
			var got struct {
				Radios     int     `json:"radios"`
				Grouped    bool    `json:"grouped"`
				BothInside bool    `json:"bothInside"`
				Legend     *string `json:"legend"`
			}
			if err := json.Unmarshal([]byte(v.String()), &got); err != nil {
				t.Fatalf("decode the group structure %q: %v", v.String(), err)
			}
			if got.Radios != 2 {
				t.Fatalf("the %s block rendered %d radios, want one per declared choice (2)\nbody:\n%s",
					tc.typ, got.Radios, body)
			}
			if !got.Grouped || !got.BothInside {
				t.Fatalf("the %s radios are not inside one fieldset group — the group heading labels "+
					"nothing, so assistive tech has no name for the question", tc.typ)
			}
			if got.Legend == nil || *got.Legend != "Pilih saiz (required)" {
				t.Errorf("the %s group's legend = %v, want the merchant label and the required marker "+
					"\"Pilih saiz (required)\" — the visible text must be unchanged", tc.typ, got.Legend)
			}
			// The defect shape: a heading <label> with no `for` and no control.
			if strings.Contains(body, `<label>Pilih saiz`) {
				t.Errorf("the %s group still publishes its name as a bare sibling <label>", tc.typ)
			}
		})
	}
}
