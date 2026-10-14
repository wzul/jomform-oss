package httpapi

// Card t_4086b8f0 — the six palette types that were measured rendering NOTHING
// on a published response form (text, section, html, image, divider, file),
// re-measured end to end against the handler that serves the page.
//
// The measurement this card was opened with: every palette type was created via
// POST /v1/rest/forms, saved via PATCH, published, and the PUBLIC page fetched
// with the workspace Host header. 18 types reached the buyer, six did not — the
// page carried only the honeypot and the Submit button. `t_99c3a08b` fixed five
// of the six (the switch in renderResponseForm dropped them ALL, and
// responseTmpl had no arm for any); this file is the complete-list guard the
// follow-up card asked for, plus the sixth type's decision.
//
// THE SIXTH TYPE, `file`. Decision: REFUSED AT SAVE TIME, naming the type —
// not rendered as a picker. No upload path exists for an anonymous buyer (every
// upload endpoint is behind an authenticated account with a write role and
// charges its media quota; public.go's mux has no upload route), and the two
// field-type authorities already exclude it (formfields.ValidTypes, customform's
// validType). A picker whose file is discarded would tell the merchant an
// attachment exists when it does not — the payment renderer's old behaviour,
// also corrected here. See formfields.UnsupportedBlock.
//
// So the assertions are, per type:
//   - the servable types: the block really appears on the served page;
//   - `file`: the write paths refuse a config that carries it (400 + a message
//     naming the type) AND a form stored before that refusal is not dropped
//     silently on its own live page.
//
// Removing a renderer arm, or the write-path guard, reds one of these.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// paletteBlockConfig is every block type the builder palette offers that a
// RESPONSE form can serve, in the merchant's own order, with one distinctive
// marker per block so the page can be asserted block by block.
func paletteBlockConfig() map[string]any {
	return map[string]any{
		"mode": "response",
		"blocks": []any{
			map[string]any{"id": "tx1", "type": "text", "value": "Nama"},
			map[string]any{"id": "sc1", "type": "section", "value": "Bahagian Satu"},
			map[string]any{"id": "ht1", "type": "html", "value": "<b>Intro</b>"},
			map[string]any{"id": "im1", "type": "image", "src": "https://cdn.example/p.png"},
			map[string]any{"id": "dv1", "type": "divider"},
			map[string]any{"id": "ta1", "type": "textarea", "value": "Catatan"},
			map[string]any{"id": "nu1", "type": "number", "value": "Umur"},
			map[string]any{"id": "se1", "type": "select", "value": "Negeri",
				"choices": []any{map[string]any{"label": "Selangor", "value": "sgr"}}},
			map[string]any{"id": "ra1", "type": "radio", "value": "Jantina",
				"choices": []any{map[string]any{"label": "Lelaki", "value": "L"}}},
			map[string]any{"id": "cb1", "type": "checkbox", "value": "Setuju"},
			map[string]any{"id": "nm1", "type": "name", "value": "Nama Penuh"},
			map[string]any{"id": "em1", "type": "email", "value": "Emel"},
			map[string]any{"id": "ph1", "type": "phone", "value": "Telefon"},
			map[string]any{"id": "dt1", "type": "date", "value": "Tarikh"},
			map[string]any{"id": "tm1", "type": "time", "value": "Masa"},
			map[string]any{"id": "wb1", "type": "website", "value": "Laman"},
			map[string]any{"id": "ad1", "type": "address", "value": "Alamat"},
			map[string]any{"id": "cn1", "type": "consent", "value": "Saya setuju"},
			map[string]any{"id": "hd1", "type": "hidden", "default_value": "src"},
			map[string]any{"id": "ls1", "type": "list", "value": "Senarai"},
			map[string]any{"id": "rp1", "type": "repeater", "value": "Berulang"},
			map[string]any{"id": "ic1", "type": "image-choice", "value": "Pilih",
				"choices": []any{map[string]any{"label": "A", "value": "a"}}},
			map[string]any{"id": "cp1", "type": "captcha", "value": "Captcha"},
		},
		"products": []any{},
	}
}

// TestResponseFormPaletteBlocksReachTheBuyer is the sweep's claim, asserted as
// one page: every block the merchant placed is on the served HTML, with the
// shape that type means (input / label / image / heading / rule). A type that
// renders nothing fails on its own line, so a future arm removal names itself.
func TestResponseFormPaletteBlocksReachTheBuyer(t *testing.T) {
	body := renderResponsePage(t, "palette", paletteBlockConfig())

	for _, tc := range []struct {
		typ  string
		want []string
	}{
		{"text", []string{`<label class="lbl">Nama</label>`, `<input type="text" name="tx1" data-id="tx1">`}},
		{"section", []string{`<div class="section">Bahagian Satu</div>`}},
		{"html", []string{`<div class="text">&lt;b&gt;Intro&lt;/b&gt;</div>`}},
		{"image", []string{`<img class="img" src="https://cdn.example/p.png" alt="">`}},
		{"divider", []string{`<hr>`}},
		{"textarea", []string{`<textarea name="ta1" rows="3">`}},
		{"number", []string{`<input type="number" name="nu1">`}},
		{"select", []string{`<select name="se1">`, `<option value="sgr">Selangor</option>`}},
		{"radio", []string{`<input type="radio" name="ra1" value="L"> Lelaki`}},
		{"checkbox", []string{`<input type="checkbox" name="cb1" value="1"> Setuju`}},
		{"name", []string{`<input type="text" name="nm1">`}},
		{"email", []string{`<input type="email" name="em1">`}},
		{"phone", []string{`<input type="tel" name="ph1">`}},
		{"date", []string{`<input type="date" name="dt1">`}},
		{"time", []string{`<input type="time" name="tm1">`}},
		{"website", []string{`<input type="url" name="wb1">`}},
		{"address", []string{`<textarea name="ad1" rows="2">`}},
		{"consent", []string{`<input type="checkbox" name="cn1" value="1"> Saya setuju`}},
		{"hidden", []string{`<input type="hidden" name="hd1" value="src">`}},
		{"list", []string{`<textarea name="ls1" rows="3">`}},
		{"repeater", []string{`<textarea name="rp1" rows="3">`}},
		{"image-choice", []string{`<input type="radio" name="ic1" value="a"> A`}},
		{"captcha", []string{`<input type="text" name="cp1">`}},
	} {
		for _, w := range tc.want {
			if !strings.Contains(body, w) {
				t.Errorf("block type %q renders nothing on the public page: missing %q", tc.typ, w)
			}
		}
	}
	// The page must not be offering a file control at all — see the decision
	// above; a stray `type="file"` here would be the payment renderer's old lie.
	if strings.Contains(body, `type="file"`) {
		t.Errorf("the response page carries a file picker, which cannot deliver anything: %s", body)
	}
}

// TestResponseFormStoredFileBlockIsNotDroppedSilently pins the legacy path: a
// form saved BEFORE the write-side refusal still exists, and its page must not
// drop the merchant's block on the floor (the defect this card measured). It
// renders the label and says the attachment cannot be collected — never a
// picker, because the picked file could go nowhere.
func TestResponseFormStoredFileBlockIsNotDroppedSilently(t *testing.T) {
	cfg := map[string]any{"mode": "response", "blocks": []any{
		map[string]any{"id": "fl1", "type": "file", "value": "Resit bank", "required": true},
	}}
	body := renderResponsePage(t, "legacy-file", cfg)

	if !strings.Contains(body, "Resit bank") {
		t.Errorf("a stored file block's own label is missing from the page — the block was dropped silently")
	}
	if !strings.Contains(body, `<div class="desc">Attachments cannot be uploaded on this form.</div>`) {
		t.Errorf("the page does not say the attachment cannot be collected (page: %s)", body)
	}
	if strings.Contains(body, `type="file"`) {
		t.Errorf("a stored file block rendered a picker whose file would be discarded")
	}
	if in := firstInputNamed(body, "fl1"); in != "" {
		t.Errorf("a stored file block rendered an input, so the submit JS would record a filename: %s", in)
	}
}

// TestResponseFormFileBlockRefusedAtSaveTime is the card's acceptance #1/#2 for
// the sixth type: the merchant cannot SAVE a response form carrying it, the
// refusal names the type, and the refusal leaves the stored form untouched.
func TestResponseFormFileBlockRefusedAtSaveTime(t *testing.T) {
	h, raw, accountID, pool := issue501Fixture(t)
	ctx := context.Background()

	fileCfg := `{"mode":"response","blocks":[{"id":"fl1","type":"file","value":"Resit bank"}]}`

	// 1. CREATE is refused, with a message naming the block type.
	rec := issue501Do(t, h, raw, http.MethodPost, "/v1/rest/forms",
		`{"title":"With attachment","config":`+fileCfg+`}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST form with a file block = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if msg := rec.Body.String(); !strings.Contains(msg, "file") {
		t.Errorf("the refusal does not name the block type: %s", msg)
	}
	var stored int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM forms WHERE account_id=$1 AND title='With attachment'`,
		accountID).Scan(&stored); err != nil {
		t.Fatalf("count forms: %v", err)
	}
	if stored != 0 {
		t.Errorf("the refused create still stored %d form(s)", stored)
	}

	// 2. UPDATE is refused on a form that exists, and the stored config does not
	//    move. (This is also how a merchant clears the block: delete it and save.)
	good := issue501Do(t, h, raw, http.MethodPost, "/v1/rest/forms",
		`{"title":"Clean","config":{"mode":"response","blocks":[{"id":"tx1","type":"text","value":"Nama"}]}}`)
	if good.Code != http.StatusCreated {
		t.Fatalf("POST a clean form = %d, want 201 (body %s)", good.Code, good.Body.String())
	}
	id, _ := issue501JSON(t, good)["id"].(string)
	before, err := h.Forms.GetByID(ctx, accountID, id)
	if err != nil {
		t.Fatalf("read the clean form: %v", err)
	}

	rec = issue501Do(t, h, raw, http.MethodPatch, "/v1/rest/forms/"+id, `{"config":`+fileCfg+`}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PATCH a file block onto a form = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if msg := rec.Body.String(); !strings.Contains(msg, "file") {
		t.Errorf("the update refusal does not name the block type: %s", msg)
	}
	after, err := h.Forms.GetByID(ctx, accountID, id)
	if err != nil {
		t.Fatalf("re-read the clean form: %v", err)
	}
	blocks, _ := after.Config["blocks"].([]any)
	if len(blocks) != 1 {
		t.Fatalf("the refused update moved the stored blocks: %v", after.Config["blocks"])
	}
	if b0, _ := blocks[0].(map[string]any); b0["type"] != "text" {
		t.Errorf("stored block after the refused update = %v, want the original text block", b0)
	}
	if before.Config["mode"] != after.Config["mode"] {
		t.Errorf("the refused update changed the stored mode: %v -> %v",
			before.Config["mode"], after.Config["mode"])
	}
}

// The refusal must not catch a form that is perfectly servable — the four
// servable static types, an omitted blocks key and an explicit null still save.
func TestResponseFormBlocksWithoutFileStillSave(t *testing.T) {
	h, raw, _, _ := issue501Fixture(t)

	for _, tc := range []struct {
		name string
		body string
	}{
		{"static", `{"title":"Static","config":{"mode":"response","blocks":[` +
			`{"id":"sc1","type":"section","value":"A"},` +
			`{"id":"ht1","type":"html","value":"B"},` +
			`{"id":"im1","type":"image","src":"https://cdn.example/p.png"},` +
			`{"id":"dv1","type":"divider"}]}}`},
		{"no blocks key", `{"title":"No blocks","config":{"mode":"response"}}`},
		{"blocks null", `{"title":"Null blocks","config":{"mode":"response","blocks":null}}`},
	} {
		if rec := issue501Do(t, h, raw, http.MethodPost, "/v1/rest/forms", tc.body); rec.Code != http.StatusCreated {
			t.Errorf("%s: POST = %d, want 201 — the guard must refuse only the block type it owns (body %s)",
				tc.name, rec.Code, rec.Body.String())
		}
	}
}

// TestPaymentFormStoredFileBlockIsNotOfferedAsAPicker is the sibling call site
// of the same class: the PAYMENT renderer painted a real
// `<input type="file">` whose bytes the submit JS never sent ("files not sent
// as text"), so the buyer believed an attachment had gone and the merchant got
// nothing. A payment form stored before the write-side refusal must no longer
// offer a control that cannot deliver.
func TestPaymentFormStoredFileBlockIsNotOfferedAsAPicker(t *testing.T) {
	h := &PublicHandlers{}
	cfg := map[string]any{"blocks": []any{
		map[string]any{"id": "fl1", "type": "file", "value": "Resit bank", "required": true},
	}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://demo.jomform.com/pay", nil)
	if !h.renderBlocks(rec, req, themedForm("pay", "#5e6ad2", cfg)) {
		t.Fatal("renderBlocks declined a config that carries blocks")
	}
	body := rec.Body.String()
	if strings.Contains(body, `type="file"`) {
		t.Errorf("the payment page still offers a file picker whose bytes are discarded on submit")
	}
	if !strings.Contains(body, "Resit bank") {
		t.Errorf("the block's label is missing from the payment page")
	}
	if !strings.Contains(body, "Attachments cannot be uploaded on this form.") {
		t.Errorf("the payment page does not say the attachment cannot be collected (page: %s)", body)
	}
}

// TestResponseFormPaletteBlockOrderIsTheMerchants keeps the fix from becoming a
// reordering: the page paints the blocks in the order the merchant placed them,
// static and input interleaved, which is what a form's layout means.
func TestResponseFormPaletteBlockOrderIsTheMerchants(t *testing.T) {
	body := renderResponsePage(t, "order", map[string]any{"mode": "response", "blocks": []any{
		map[string]any{"id": "sc1", "type": "section", "value": "PERTAMA"},
		map[string]any{"id": "tx1", "type": "text", "value": "KEDUA"},
		map[string]any{"id": "dv1", "type": "divider"},
		map[string]any{"id": "tx2", "type": "text", "value": "KETIGA"},
	}})
	first := strings.Index(body, "PERTAMA")
	second := strings.Index(body, "KEDUA")
	third := strings.Index(body, "KETIGA")
	if first < 0 || second < 0 || third < 0 || !(first < second && second < third) {
		t.Errorf("block order on the page = (%d, %d, %d), want the merchant's order", first, second, third)
	}
}
