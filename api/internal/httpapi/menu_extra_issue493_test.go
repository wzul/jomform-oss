package httpapi

// Issue #493 (part 3 of 3): merchant-configurable extra checkout fields for the
// menu cart, with name + email ALWAYS mandatory.
//
// Acceptance criteria pinned here:
//   - the attached form's field blocks render in the menu checkout with the
//     form's own labels and required flags;
//   - the checkout's helper line explains where the fields come from and links
//     into the form builder to edit them;
//   - name + email are always collected and a form that also has a name/email
//     block never duplicates them (they can never be removed);
//   - with no form attached the checkout renders no extra fields and still
//     collects name + email;
//   - the client ships the extra values in fields{} and the server re-validates
//     them against the same contract (integration test, needs the test DB);
//   - every new system string exists in all 5 locales.

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

// extraContract is the field contract a merchant's attached form produces when
// its fields are configured in the normal form builder (no registered_fields).
func extraContract() []formfields.Field {
	return []formfields.Field{
		{FieldID: "f_name", Type: "name", Label: "Nama", Required: true},
		{FieldID: "f_email", Type: "email", Label: "Emel", Required: true},
		{FieldID: "addr", Type: "address", Label: "Alamat penghantaran", Required: true, Placeholder: "No 1, Jalan…"},
		{FieldID: "phone", Type: "phone", Label: "Telefon", Required: false},
		{FieldID: "spice", Type: "select", Label: "Tahap pedas", Required: true,
			Choices: []formfields.Choice{{Label: "Biasa", Value: "biasa"}, {Label: "Pedas", Value: "pedas"}}},
		{FieldID: "pickup", Type: "time", Label: "Masa ambil"},
		{FieldID: "notes", Type: "textarea", Label: "Nota", Placeholder: "cth: kurang gula"},
	}
}

// menu493Fixture is the #485 fixture plus the extra-field checkout config.
func menu493Fixture(t *testing.T) workspaceMenuData {
	t.Helper()
	d := menu492Fixture(t)
	d.ExtraFields = menuExtraFields(extraContract(), formLangs["en"])
	d.ExtraFormSlug = "extra-info"
	d.ExtraFormID = "form-123"
	d.ExtraFormEdit = "/builder?form=form-123"
	return d
}

func renderMenu493(t *testing.T, data workspaceMenuData) string {
	t.Helper()
	var buf bytes.Buffer
	if err := menuPageTmpl.Execute(&buf, data); err != nil {
		t.Fatalf("menuPageTmpl.Execute: %v", err)
	}
	return buf.String()
}

// The attached form's blocks render in the checkout step with the merchant's
// own labels, required flags, placeholders and choices.
func TestIssue493ExtraFieldsRenderInCheckout(t *testing.T) {
	body := renderMenu493(t, menu493Fixture(t))
	for _, want := range []string{
		`class="extrahead"`,                    // helper block
		`name="addr"`,                          // address field
		`name="spice"`,                         // select field
		`name="pickup"`,                        // time field
		`name="notes"`,                         // textarea field
		`Alamat penghantaran`,                  // merchant label (verbatim)
		`placeholder="No 1, Jalan…"`,           // merchant placeholder
		`<option value="biasa">Biasa</option>`, // merchant choice
		`<option value="pedas">Pedas</option>`, // merchant choice
		`<textarea name="notes"`,               // textarea block rendered
		`name="addr" rows="2" required`,        // required address is enforced
		`name="spice" required`,                // required select
		`name="pickup"`,                        // optional time
		`/builder?form=form-123`,               // link to edit the fields
	} {
		if !strings.Contains(body, want) {
			t.Errorf("menu checkout missing %q\nbody:\n%s", want, body)
		}
	}
	// A required field carries the localised "(required)" marker.
	if !strings.Contains(body, "Alamat penghantaran (required)") {
		t.Error("required extra field did not render the localised required marker")
	}
}

// The helper line explains that the fields come from the selected form, and
// links to the builder to edit them.
func TestIssue493HelperAndEditLink(t *testing.T) {
	body := renderMenu493(t, menu493Fixture(t))
	if !strings.Contains(body, `class="extrahelp"`) {
		t.Fatal("checkout has no helper line for the extra fields")
	}
	if !strings.Contains(body, `class="extralink" href="/builder?form=form-123"`) {
		t.Error("checkout helper has no link into the form builder")
	}
	if !strings.Contains(body, "Edit this form") {
		t.Error("checkout helper has no localised edit label")
	}
}

// Name + email are ALWAYS collected, and a form that also declares name/email
// blocks never duplicates them in the extra-fields list.
func TestIssue493NameAndEmailAlwaysMandatoryNeverDuplicated(t *testing.T) {
	d := menu493Fixture(t)
	for _, f := range d.ExtraFields {
		if f.Type == "name" || f.Type == "email" {
			t.Fatalf("extra fields must not contain a %s field (name+email are mandatory and separate)", f.Type)
		}
	}
	body := renderMenu493(t, d)
	// Exactly one name input and one email input.
	if n := strings.Count(body, `name="full_name"`); n != 1 {
		t.Errorf("checkout has %d full_name inputs, want 1", n)
	}
	if n := strings.Count(body, `name="email"`); n != 1 {
		t.Errorf("checkout has %d email inputs, want 1", n)
	}
	if !strings.Contains(body, `name="full_name" type="text" autocomplete="name" required`) {
		t.Error("buyer name is not required")
	}
	if !strings.Contains(body, `name="email" type="email" autocomplete="email" required`) {
		t.Error("buyer email is not required")
	}
	// The mandatory-field message is present and localised.
	if !strings.Contains(body, `data-lbl-required="Please enter your name and email."`) {
		t.Error("checkout missing the localised mandatory name/email message")
	}
}

// With no form attached there are no extra fields and the checkout still works
// (name + email only).
func TestIssue493NoFormMeansNameEmailOnly(t *testing.T) {
	d := menu493Fixture(t)
	d.ExtraFields = nil
	d.ExtraFormSlug = ""
	d.ExtraFormID = ""
	d.ExtraFormEdit = ""
	body := renderMenu493(t, d)
	if strings.Contains(body, `class="extrahead"`) {
		t.Error("no form attached but the extra-fields helper rendered")
	}
	if !strings.Contains(body, `name="full_name"`) || !strings.Contains(body, `name="email"`) {
		t.Error("no form attached: checkout must still collect name + email")
	}
	// The client still posts an empty extra-fields map and the checkout button
	// stays present.
	if !strings.Contains(body, "function extraFields(){") || !strings.Contains(body, `id="menuPay"`) {
		t.Error("no form attached: checkout wiring missing")
	}
}

// The client collects every extra input by its block-id name (the same key the
// form's own submit path records) and ships it in fields{}.
func TestIssue493ClientCollectsExtraFields(t *testing.T) {
	body := renderMenu493(t, menu493Fixture(t))
	for _, want := range []string{
		"function extraFields(){",
		"payForm.querySelectorAll('[name]')",
		"n==='full_name'||n==='email'||n==='website'||n==='jf_ts'",
		"out[n]=el.value",
		"extraFields()",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("checkout script missing %q", want)
		}
	}
}

// The field-contract derivation reads a form's builder blocks, keeps only
// collectable input types, uses the block id as the field id, and prefers an
// explicit registered_fields contract when one is present (#254).
func TestIssue493ContractFromBuilderBlocks(t *testing.T) {
	cfg := map[string]any{
		"blocks": []any{
			map[string]any{"id": "title", "type": "text", "value": "Order"},
			map[string]any{"id": "prod", "type": "product", "product_id": "p1"},
			map[string]any{"id": "addr", "type": "address", "value": "Alamat", "required": true,
				"placeholder": "No 1"},
			map[string]any{"id": "spice", "type": "select", "value": "Pedas", "required": true,
				"choices": []any{map[string]any{"label": "Biasa", "value": "biasa"}}},
			map[string]any{"id": "addr", "type": "address", "value": "dup"}, // duplicate id dropped
			map[string]any{"id": "", "type": "phone"},                       // blank id dropped
		},
	}
	got := formfields.Contract(cfg)
	if len(got) != 2 {
		t.Fatalf("Contract returned %d fields, want 2 (%+v)", len(got), got)
	}
	if got[0].FieldID != "addr" || got[0].Type != "address" || !got[0].Required || got[0].Placeholder != "No 1" {
		t.Errorf("addr field = %+v", got[0])
	}
	if got[1].FieldID != "spice" || len(got[1].Choices) != 1 || got[1].Choices[0].Value != "biasa" {
		t.Errorf("spice field = %+v", got[1])
	}
	// An explicit registered_fields contract wins.
	reg := formfields.Contract(map[string]any{
		"blocks":            []any{map[string]any{"id": "addr", "type": "address", "value": "A"}},
		"registered_fields": []any{map[string]any{"field_id": "notes", "type": "textarea", "label": "Nota", "required": true}},
	})
	if len(reg) != 1 || reg[0].FieldID != "notes" {
		t.Errorf("registered_fields did not win: %+v", reg)
	}
	// Nothing configured → no contract.
	if got := formfields.Contract(map[string]any{}); got != nil {
		t.Errorf("empty config produced a contract: %+v", got)
	}
	if got := formfields.Contract(nil); got != nil {
		t.Errorf("nil config produced a contract: %+v", got)
	}
}

// ValidateFields enforces the derived contract: a missing required field is
// rejected, a bad email is rejected, and a valid submission passes.
func TestIssue493ValidateFieldsEnforcesContract(t *testing.T) {
	regs := formfields.Contract(map[string]any{
		"blocks": []any{
			map[string]any{"id": "addr", "type": "address", "value": "Alamat", "required": true},
			map[string]any{"id": "alt", "type": "email", "value": "Emel lain"},
			map[string]any{"id": "spice", "type": "select", "value": "Pedas", "required": true},
		},
	})
	if err := formfields.ValidateFields(regs, map[string]string{"addr": "x", "spice": "pedas"}); err != nil {
		t.Errorf("valid submission rejected: %v", err)
	}
	if err := formfields.ValidateFields(regs, map[string]string{"spice": "pedas"}); err == nil {
		t.Error("missing required addr was accepted")
	}
	if err := formfields.ValidateFields(regs, map[string]string{"addr": "x", "spice": "pedas", "alt": "not-an-email"}); err == nil {
		t.Error("invalid email was accepted")
	}
	// No contract → nothing enforced.
	if err := formfields.ValidateFields(nil, nil); err != nil {
		t.Errorf("empty contract rejected a submission: %v", err)
	}
}

// Every locale carries the two new system strings — no blank label can leak,
// and a non-English locale really localises the extra-fields helper.
func TestIssue493ExtraStringsInFiveLocales(t *testing.T) {
	fields := map[string]func(langStrings) string{
		"MenuExtraHelp": func(l langStrings) string { return l.MenuExtraHelp },
		"MenuExtraEdit": func(l langStrings) string { return l.MenuExtraEdit },
	}
	if len(formLangs) != 5 {
		t.Fatalf("formLangs has %d entries, want 5", len(formLangs))
	}
	for code, ls := range formLangs {
		for name, get := range fields {
			if get(ls) == "" {
				t.Errorf("lang %q has empty %s", code, name)
			}
		}
	}
	if got := langStringsForCode("ms").MenuExtraEdit; got != "Edit borang ini" {
		t.Errorf("ms MenuExtraEdit = %q, want Edit borang ini", got)
	}
	// An Arabic menu page localises the helper line and stays RTL.
	d := menu493Fixture(t)
	d.Ls = formLangs["ar"]
	d.ExtraFields = menuExtraFields(extraContract(), formLangs["ar"])
	body := renderMenu493(t, d)
	if !strings.Contains(body, `dir="rtl"`) {
		t.Error("ar menu page did not set dir=rtl")
	}
	if !strings.Contains(body, "تعديل هذا النموذج") {
		t.Error("ar menu page did not localise the extra-fields edit link")
	}
}

// menuExtraFields skips name/email blocks and falls back to the field id when a
// block has no label.
func TestIssue493MenuExtraFieldsSkipsNameEmail(t *testing.T) {
	got := menuExtraFields([]formfields.Field{
		{FieldID: "n", Type: "name", Label: "Nama"},
		{FieldID: "e", Type: "email", Label: "Emel"},
		{FieldID: "addr", Type: "address", Label: ""},
	}, formLangs["en"])
	if len(got) != 1 || got[0].ID != "addr" {
		t.Fatalf("menuExtraFields = %+v, want just addr", got)
	}
	if got[0].Label != "addr" {
		t.Errorf("unlabelled field did not fall back to its id: %q", got[0].Label)
	}
	if got[0].RequiredMark == "" {
		t.Error("RequiredMark was not resolved from the dictionary")
	}
}

// menuExtraContract drops name/email blocks so a form that also declares them
// (even as required) can never demand an unrendered field: the checkout's own
// mandatory name + email inputs satisfy that requirement.
func TestIssue493MenuExtraContractDropsNameEmail(t *testing.T) {
	got := menuExtraContract(map[string]any{
		"blocks": []any{
			map[string]any{"id": "nm", "type": "name", "value": "Nama", "required": true},
			map[string]any{"id": "em", "type": "email", "value": "Emel", "required": true},
			map[string]any{"id": "addr", "type": "address", "value": "Alamat", "required": true},
		},
	})
	if len(got) != 1 || got[0].FieldID != "addr" {
		t.Fatalf("menuExtraContract = %+v, want just addr", got)
	}
	// An order that supplies the address therefore validates even though the
	// attached form declared required name + email blocks.
	if err := formfields.ValidateFields(got, map[string]string{"addr": "x"}); err != nil {
		t.Errorf("contract rejected a valid menu order: %v", err)
	}
}

// ── integration: server-side validation of the attached form's blocks ───────
//
// The attached form here has NO registered_fields — its extra fields live in
// the normal builder blocks, which is exactly the merchant flow #493 wires.
func TestIssue493MenuOrderValidatesBuilderBlockFields(t *testing.T) {
	h, workspaceSlug, accountID, prodID, _ := menuOrderFixture(t)
	ctx := context.Background()
	host := workspaceSlug + ".jomform.com"

	// Published form whose fields are ordinary builder blocks — including
	// required name + email blocks, which must NOT become extra fields (the
	// checkout already collects both mandatorily).
	formSlug := "it-493-extra-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "Extra info", Status: "published",
		Config: map[string]any{
			"blocks": []any{
				map[string]any{"id": "nm", "type": "name", "value": "Nama", "required": true},
				map[string]any{"id": "em", "type": "email", "value": "Emel", "required": true},
				map[string]any{"id": "addr", "type": "address", "value": "Alamat penghantaran", "required": true},
				map[string]any{"id": "spice", "type": "select", "value": "Tahap pedas", "required": true,
					"choices": []any{map[string]any{"label": "Biasa", "value": "biasa"}}},
			},
		},
	}
	if err := h.Forms.Create(ctx, f); err != nil {
		t.Fatalf("create extra form: %v", err)
	}

	base := `{"client":{"full_name":"Ali","email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","quantity":1}],` +
		`"payment_method":"bank_direct"`

	// No form attached yet → blocks are not enforced, the order goes through.
	// Issue #497: the menu is a form; the order posts to its own URL and the
	// extra fields come from the menu form's attached order form.
	menuForm := &dbstore.Form{
		AccountID: accountID, Slug: "it-493-menu-" + accountID[:8], Title: "Menu",
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
	menuOrderURL := "/" + menuForm.Slug + "/order"
	postMenu := func(body string) *httptest.ResponseRecorder {
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)
		req := httptest.NewRequest(http.MethodPost, menuOrderURL, bytes.NewBufferString(body))
		req.Host = host
		req.RemoteAddr = uniqueTestIP() + ":1234"
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	rec := postMenu(base + `}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("unattached form: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}

	// Attach the extra form to the MENU FORM's config (JSON blob setting, no
	// migration). Issue #497: the attachment lives on the form, not the
	// workspace landing page.
	menuForm.Config = dbstore.WithMenu(menuForm.Config,
		&dbstore.MenuPage{Enabled: true, OrderFormSlug: formSlug})
	if err := h.Forms.Update(ctx, accountID, menuForm.ID, menuForm.Title, menuForm.Config, nil); err != nil {
		t.Fatalf("attach extra form: %v", err)
	}

	// Missing a required block field → 400.
	rec = postMenu(base + `}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("attached form, missing required block field: status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}

	// Providing both → 201 and the values persist on the sale under the block
	// ids (the same keys the form's own submit path records).
	rec = postMenu(base + `,"fields":{"addr":"No 1 Jalan","spice":"biasa"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("attached form, fields supplied: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
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
	if fields["addr"] != "No 1 Jalan" || fields["spice"] != "biasa" {
		t.Errorf("sale fields = %+v, want addr + spice persisted", fields)
	}
	// The attached form validates only — the sale is attributed to the MENU
	// form, never to the attached extra-info form (issue #497).
	if sale.FormID != menuForm.ID {
		t.Errorf("sale.form_id = %q, want the menu form id %q", sale.FormID, menuForm.ID)
	}
	// The attached form never relaxes the mandatory name + email.
	rec = postMenu(`{"client":{"email":"ali@example.test"},` +
		`"items":[{"product_id":"` + prodID + `","quantity":1}],"payment_method":"bank_direct",` +
		`"fields":{"addr":"x","spice":"biasa"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("name-less order with an attached form: status = %d, want 400", rec.Code)
	}
}
