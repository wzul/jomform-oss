package httpapi

// Card t_99c3a08b: a response-mode form whose only block is `type: "text"` —
// the builder's "Text / label" block — rendered as a heading, a Submit button
// and NOTHING else. Two things dropped it: renderResponseForm's switch filtered
// the block out before the template ever saw it, and responseTmpl had no `text`
// arm. The merchant's one field therefore produced neither an input nor static
// text; the page measured zero `class="lbl"`, zero `class="text"` and zero
// visible inputs (only the honeypot `website` and `jf_ts`).
//
// These tests drive the REAL call site (renderResponseForm), not the template by
// hand. That distinction is the point: a template-level test stays GREEN with
// the switch's `text` arm removed, which is exactly the defect — the template
// was never reached. Removing the arm reds these tests.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// measuredTextConfig is the stored config of the form Wan reported (form
// 7de60577-638e-4063-9ae1-3b2a4e1f467d, slug new-form-ddb3): ONE text block.
func measuredTextConfig() map[string]any {
	return map[string]any{
		"mode": "response",
		"blocks": []any{map[string]any{
			"id": "n9mplk3", "type": "text", "value": "Cuba. Nama",
			"max_length": 300, "description": "Nama penuh",
		}},
		"products":     []any{},
		"description":  "Cuba. Nama",
		"chip_mapping": map[string]any{},
	}
}

// renderResponsePage renders a response-mode form through the handler that
// serves it and returns the page.
func renderResponsePage(t *testing.T, slug string, cfg map[string]any) string {
	t.Helper()
	h := &PublicHandlers{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://demo.jomform.com/"+slug, nil)
	if !h.renderResponseForm(rec, req, themedForm(slug, "#5e6ad2", cfg)) {
		t.Fatalf("renderResponseForm declined a response form with blocks: %v", cfg["blocks"])
	}
	return rec.Body.String()
}

// firstInputNamed returns the <input …> tag whose name is id, or "".
func firstInputNamed(body, id string) string {
	re := regexp.MustCompile(`<input[^>]*name="` + regexp.QuoteMeta(id) + `"[^>]*>`)
	return re.FindString(body)
}

// A visitor must be able to fill the block in and submit it: the page carries a
// real text input named by the block id, labelled with the block's value.
func TestResponseFormTextBlockRendersInput(t *testing.T) {
	body := renderResponsePage(t, "new-form-ddb3", measuredTextConfig())

	if !strings.Contains(body, `<label class="lbl">Cuba. Nama</label>`) {
		t.Errorf("the block's value is not rendered as the field's label")
	}
	in := firstInputNamed(body, "n9mplk3")
	if in == "" {
		t.Fatalf("no input named n9mplk3 — the block renders nothing (page: %s)", body)
	}
	for _, want := range []string{`type="text"`, `data-id="n9mplk3"`, `maxlength="300"`} {
		if !strings.Contains(in, want) {
			t.Errorf("input is missing %s: %s", want, in)
		}
	}
	// The honeypot (`website`) and `jf_ts` are hidden inputs on every page; the
	// block's own id must name exactly ONE control.
	if n := strings.Count(body, `name="n9mplk3"`); n != 1 {
		t.Errorf(`name="n9mplk3" appears %d times, want 1`, n)
	}
}

// placeholder / default_value / required pass through like the sibling types.
func TestResponseFormTextBlockHonoursFieldSettings(t *testing.T) {
	cfg := map[string]any{"mode": "response", "blocks": []any{map[string]any{
		"id": "q1", "type": "text", "value": "Nama penuh", "required": true,
		"placeholder": "Ali bin Abu", "default_value": "Ali", "pattern": "[A-Za-z ]{1,40}",
	}}}
	in := firstInputNamed(renderResponsePage(t, "settings", cfg), "q1")
	if in == "" {
		t.Fatal("no input named q1")
	}
	for _, want := range []string{
		`placeholder="Ali bin Abu"`, `value="Ali"`, `pattern="[A-Za-z ]{1,40}"`, `required`,
	} {
		if !strings.Contains(in, want) {
			t.Errorf("input is missing %s: %s", want, in)
		}
	}
}

// The other four blocks the same switch dropped are STATIC content, and they
// have exactly one meaning — the one the payment renderer (blockTmpl) already
// paints. A form whose only block is a heading, an intro, an image or a rule
// must not render a blank page either.
func TestResponseFormRendersStaticBlocks(t *testing.T) {
	cfg := map[string]any{"mode": "response", "blocks": []any{
		map[string]any{"id": "s1", "type": "section", "value": "Bahagian A"},
		map[string]any{"id": "h1", "type": "html", "value": "<b>Tebal</b>"},
		map[string]any{"id": "i1", "type": "image", "src": "https://cdn.example/p.png"},
		map[string]any{"id": "d1", "type": "divider", "value": ""},
	}}
	body := renderResponsePage(t, "static", cfg)
	// `html` is escaped like the payment renderer escapes it — the type is a
	// misnomer there, not a raw-HTML hole, and this fix does not widen it.
	for _, want := range []string{
		`<div class="section">Bahagian A</div>`,
		`<div class="text">&lt;b&gt;Tebal&lt;/b&gt;</div>`,
		`<img class="img" src="https://cdn.example/p.png" alt="">`,
		`<hr>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in the rendered response form", want)
		}
	}
	// None of the static blocks may collect an answer.
	for _, id := range []string{"s1", "h1", "i1", "d1"} {
		if in := firstInputNamed(body, id); in != "" {
			t.Errorf("static block %s rendered an input: %s", id, in)
		}
	}
}

// Acceptance #2: the collected value must reach the stored submission — the
// Sales page renders a response form's answers off submission data. Integration
// against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.
func TestResponseFormTextBlockSubmitRoundTrips(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, workspaceSlug, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-workspace-' || md5(random()::text), 'it-' || md5(random()::text))
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

	fs := dbstore.NewFormStore(pool)
	formSlug := "it-text-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "Cuba. Nama",
		Status: "published", Config: measuredTextConfig(),
		Theme: map[string]any{"primary": "#5e6ad2"},
	}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	h := &PublicHandlers{
		Forms:       fs,
		Sales:       dbstore.NewSaleStore(pool),
		Accounts:    dbstore.NewAccountStore(pool),
		Products:    dbstore.NewProductStore(pool),
		Workspaces:  dbstore.NewWorkspaceStore(pool),
		Submissions: dbstore.NewSubmissionStore(pool),
		BaseURL:     "http://localhost:8080",
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"client":{"full_name":"Ali"},"fields":{"n9mplk3":"Ali"}}`
	req := httptest.NewRequest(http.MethodPost, "/"+formSlug+"/order", bytes.NewBufferString(body))
	req.Host = workspaceSlug + ".jomform.com"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode submit response: %v", err)
	}
	if resp.Status != "submitted" || resp.ID == "" {
		t.Fatalf("submit response = %+v, want status submitted + an id", resp)
	}
	sub, err := h.Submissions.ByID(ctx, resp.ID)
	if err != nil {
		t.Fatalf("fetch submission: %v", err)
	}
	if sub.Data["n9mplk3"] != "Ali" {
		t.Errorf("stored submission data[n9mplk3] = %v, want Ali (the text block's answer)", sub.Data["n9mplk3"])
	}
}

// Card t_4823943a — THE RENDERED HALF of the builder's "Treat as intro line".
//
// A `text` block is a short-answer INPUT, so its value is the field's LABEL.
// Forms written under the old content model stored their intro SENTENCE there,
// and the live page reads exactly what the card measured:
//
//	<label class="lbl">Register for our event — cuma rsvp ja</label>
//	<input type="text" name="qoubfpj" data-id="qoubfpj">
//
// The builder cannot migrate those rows (the same value may be a real label such
// as "Abc"), so it warns and offers a per-block action that retypes `text` →
// `section`, keeping the block id and the value bytes untouched. This test pins
// what that retype produces on the page the buyer gets: the same sentence as an
// INTRO LINE, and no label/control pair for that id.
//
// Both pages are rendered through the real response handler, so the assertion is
// about the renderer's decision, not about a config shape:
//   - the BEFORE page must carry the defect markup (otherwise the after-reading
//     proves nothing);
//   - the AFTER page must carry <div class="section">…</div> with the same bytes,
//     must not carry the label, and must render no control named qoubfpj.
func TestConvertedIntroBlockRendersAsProseNotAnInputLabel(t *testing.T) {
	const prose = "Register for our event — cuma rsvp ja"
	labels := func(cfg map[string]any) map[string]any {
		return map[string]any{"mode": "response", "blocks": []any{
			map[string]any{"id": "qoubfpj", "type": cfg["type"], "value": prose},
			map[string]any{"id": "nm", "type": "name", "value": "Nama anda"},
		}}
	}

	before := renderResponsePage(t, "prose-before", labels(map[string]any{"type": "text"}))
	after := renderResponsePage(t, "prose-after", labels(map[string]any{"type": "section"}))

	// BEFORE: the defect, as measured on prod.
	if !strings.Contains(before, `<label class="lbl">`+prose+`</label>`) {
		t.Fatalf("the pre-conversion page does not carry the label this card is about — the AFTER reading would prove nothing\n%s", before)
	}
	if in := firstInputNamed(before, "qoubfpj"); in == "" {
		t.Fatalf("the pre-conversion page renders no control named qoubfpj")
	}

	// AFTER: the sentence is prose. Its bytes are unchanged; its role is not.
	if strings.Contains(after, `<label class="lbl">`+prose+`</label>`) {
		t.Errorf("the converted block is still rendered as an input's label: the merchant's sentence is still nailed to an empty box")
	}
	if !strings.Contains(after, `<div class="section">`+prose+`</div>`) {
		t.Errorf("the converted block is not rendered as an intro line; the page must show %q as display prose", prose)
	}
	if in := firstInputNamed(after, "qoubfpj"); in != "" {
		t.Errorf("the converted block still collects an answer: %s", in)
	}
	// The id is kept, so a notification recipient or a stored contract that names
	// qoubfpj does not lose its block — it simply no longer names a CONTROL. The
	// sibling field is untouched either way.
	if !strings.Contains(after, `name="nm"`) {
		t.Errorf("the sibling name field disappeared from the converted page")
	}
}
