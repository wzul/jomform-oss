package httpapi

// Card t_f8a59664: `<embed>` is a void element and sat in buildSanitizer's
// SkipElementsContent list, so the sanitizer dropped every token after it —
// `<embed src="x"><input name="email">` came out "".
//
// The customform package's tests pin SanitizeHTML itself. This one pins the
// CALL SITE: PublicHandlers.renderCustomForm is what a buyer's browser loads,
// and the merchant's body only reaches the frame through the JSON string
// literal the bridge concatenates into `frame.srcdoc`. A sanitizer that keeps
// the control is worthless if the page that serves it stops writing it there,
// and the pre-fix failure mode is exactly that: srcdoc's body half is empty.

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// customEmbedBody is the card's shape with a realistic merchant body around it:
// an embed, then the registered control, then a second control.
const customEmbedBody = `<embed src="x" type="video/mp4"><label for="email">Email</label>` +
	`<input type="email" name="email" required>` +
	`<label for="nama">Nama</label><input type="text" name="nama">` +
	`<button type="submit" name="go" value="1">Hantar</button>`

// srcdocBodyFromCustomPage returns the exact sanitized HTML the rendered page
// hands to the frame. The bridge writes it as
// `frame.srcdoc='<!doctype html>…</head><body>'+<JSON string>+'</body></html>'`,
// so the JSON literal between those two markers IS the served form body.
func srcdocBodyFromCustomPage(t *testing.T, page string) string {
	t.Helper()
	const open = `'</head><body>'+`
	i := strings.Index(page, open)
	if i < 0 {
		t.Fatalf("the rendered page does not build its srcdoc the way the bridge does "+
			"(no %q); this test cannot read the served body", open)
	}
	rest := page[i+len(open):]
	const close = `+'</body></html>'`
	j := strings.Index(rest, close)
	if j < 0 {
		t.Fatalf("the srcdoc assignment has no closing %q", close)
	}
	var body string
	if err := json.Unmarshal([]byte(rest[:j]), &body); err != nil {
		t.Fatalf("the srcdoc body literal is not a JSON string: %v (%.120s)", err, rest[:j])
	}
	return body
}

func TestCustomFormRenderKeepsControlAfterEmbed(t *testing.T) {
	cfg := map[string]any{
		"mode": "response",
		"custom_html": map[string]any{
			"html": customEmbedBody,
			"fields": []any{
				map[string]any{"field_id": "email", "type": "email", "label": "Email", "required": true},
				map[string]any{"field_id": "nama", "type": "text", "label": "Nama"},
			},
		},
	}
	h := &PublicHandlers{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://demo.jomform.com/checkout", nil)
	if !h.renderCustomForm(rec, req, themedForm("checkout", "#5e6ad2", cfg)) {
		t.Fatal("renderCustomForm declined a form with enabled custom_html — this is no longer the surface under test")
	}
	if rec.Code != 200 {
		t.Fatalf("rendered page status = %d, want 200", rec.Code)
	}

	served := srcdocBodyFromCustomPage(t, rec.Body.String())

	// Everything after the <embed> must still be in the frame's body. Before
	// the fix `served` was "" — the whole body gone, controls and all.
	for _, want := range []string{
		`<label for="email">Email</label>`,
		`name="email"`,
		`<label for="nama">Nama</label>`,
		`name="nama"`,
		`name="go" value="1"`,
	} {
		if !strings.Contains(served, want) {
			t.Errorf("%s missing from the srcdoc body the page serves: %q", want, served)
		}
	}
	if strings.Contains(strings.ToLower(served), "embed") {
		t.Errorf("the embed element survived into the served body: %q", served)
	}
}
