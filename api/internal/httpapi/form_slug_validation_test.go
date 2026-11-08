package httpapi

// Card t_f76f81eb on the wire: a form slug had no validator on the create path
// (only shape on rename), so prod v1.25.0 answered
//
//	POST  /v1/rest/forms      {"title":"qa slug probe","slug":"bad slug!!/x"}
//	  -> 201 {"slug":"bad slug!!/x", "url":"https://…/bad slug!!/x"}
//	PATCH /v1/rest/forms/{id} {"slug":"login"} -> 200 {"slug":"login"}
//
// The first is a public address with raw spaces and an extra path segment no
// router can serve; the second puts the merchant's form on a label JomForm owns
// (and on /privacy it would be shadowed by the hosted legal page).
//
// WHAT THIS FILE PINS (each from the real route, never from the store):
//
//  1. create refuses a malformed slug with 400 form.slugInvalid and stores
//     nothing.
//  2. create refuses a RESERVED slug with 400 form.slugReserved, an English
//     `error` that names the label, and stores nothing.
//  3. a create with a BLANK slug whose title slugifies onto a reserved label
//     ("Login") succeeds and lands on a different, servable slug — the generator
//     cannot mint a value the rule refuses.
//  4. a PATCH onto a reserved slug is refused, leaves forms.slug AND the title
//     untouched (the slug write runs first), and leaves no redirect row.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL) through the same
// fixture the rename guards use; skipped when unset.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/urlguard"
)

// assertRefusal decodes an error body and asserts the status, code and that the
// prose is English (the API never knows the caller's language — #6177).
func assertRefusal(t *testing.T, what string, code int, body map[string]any, wantStatus int, wantCode, mustName string) string {
	t.Helper()
	if code != wantStatus {
		t.Fatalf("%s = %d, want %d (body %v)", what, code, wantStatus, body)
	}
	gotCode, _ := body["code"].(string)
	if gotCode != wantCode {
		t.Fatalf("%s code = %q, want %q (body %v) — the dashboard translates the code, so a wrong/absent "+
			"one shows the page's generic sentence", what, gotCode, wantCode, body)
	}
	msg, _ := body["error"].(string)
	if msg == "" {
		t.Fatalf("%s carried no `error` prose (body %v)", what, body)
	}
	if mustName != "" && !strings.Contains(msg, mustName) {
		t.Errorf("%s error = %q, want it to name %q — a merchant told only \"invalid\" retries a variation "+
			"of the same reserved name", what, msg, mustName)
	}
	for _, malay := range []string{"tidak", "sudah", "borang", "sila", "pilih"} {
		if strings.Contains(strings.ToLower(msg), malay) {
			t.Errorf("%s error carries a Malay literal (%q): %q — the REST prose must be English", what, malay, msg)
		}
	}
	return msg
}

// liveFormsWithTitle counts the caller's rows carrying an exact title — used to
// prove a refused create stored nothing without depending on a slug a normalising
// write may have changed.
func liveFormsWithTitle(t *testing.T, fx renameFixture, title string) int {
	t.Helper()
	var n int
	if err := fx.pool.QueryRow(fx.ctx,
		`SELECT COUNT(*) FROM forms WHERE account_id=$1 AND title=$2 AND deleted_at IS NULL`,
		fx.accountID, title).Scan(&n); err != nil {
		t.Fatalf("count forms by title: %v", err)
	}
	return n
}

func TestCreateFormRefusesAMalformedSlug(t *testing.T) {
	fx := newRenameFixture(t, "it-malformed-"+randHex(4))

	code, out := createFormBody(t, fx, `{"title":"qa slug probe","slug":"bad slug!!/x","config":{"blocks":[]}}`)
	assertRefusal(t, "POST /v1/rest/forms with \"bad slug!!/x\"", code, out,
		http.StatusBadRequest, "form.slugInvalid", "")

	if n := liveFormsWithTitle(t, fx, "qa slug probe"); n != 0 {
		t.Errorf("%d form row(s) stored for a REFUSED create — the refusal must not half-write, and a "+
			"stored row is a public URL (prod served it as %q)", n, "https://…/bad slug!!/x")
	}
}

func TestCreateFormRefusesAReservedSlug(t *testing.T) {
	fx := newRenameFixture(t, "it-reserved-"+randHex(4))

	code, out := createFormBody(t, fx, `{"title":"qa reserved probe","slug":"Login","config":{"blocks":[]}}`)
	msg := assertRefusal(t, "POST /v1/rest/forms with \"Login\"", code, out,
		http.StatusBadRequest, urlguard.FormSlugReservedCode, "login")
	if !strings.Contains(strings.ToLower(msg), "reserved") {
		t.Errorf("the refusal %q does not say the name is reserved", msg)
	}
	if n := liveFormsWithTitle(t, fx, "qa reserved probe"); n != 0 {
		t.Errorf("%d form row(s) stored for a REFUSED create of a reserved slug", n)
	}

	// Same answer for a route the API serves on the same host: a form at
	// /privacy would be shadowed by the hosted legal page and never reached.
	code, out = createFormBody(t, fx, `{"title":"qa privacy probe","slug":"privacy","config":{"blocks":[]}}`)
	assertRefusal(t, "POST /v1/rest/forms with \"privacy\"", code, out,
		http.StatusBadRequest, urlguard.FormSlugReservedCode, "privacy")
}

func TestCreateFormGeneratedSlugAvoidsReservedLabels(t *testing.T) {
	fx := newRenameFixture(t, "it-gen-"+randHex(4))

	// No slug: the generator derives one from the title. "Login" slugifies onto
	// a reserved label, so the generator must suffix it rather than hand the
	// create path a value its own validator refuses.
	code, out := createFormBody(t, fx, `{"title":"Login","config":{"blocks":[]}}`)
	if code != http.StatusCreated {
		t.Fatalf("POST /v1/rest/forms with a blank slug and a reserved-derived title = %d, want 201 "+
			"(body %v)", code, out)
	}
	slug, _ := out["slug"].(string)
	if slug == "" {
		t.Fatalf("the created form reports no slug: %v", out)
	}
	if err := func() error { _, e := dbstore.ValidateFormSlug(slug); return e }(); err != nil {
		t.Fatalf("generated slug %q fails the rule the create path enforces: %v", slug, err)
	}
	if slug == "login" {
		t.Fatalf("generated slug is the reserved label \"login\" — a form was minted on JomForm's own name")
	}

	// And it is actually servable: the address the response reports answers once
	// the form is published (a create lands as a draft, which the public path
	// does not serve by design).
	id, _ := out["id"].(string)
	if err := fx.h.Forms.SetStatus(fx.ctx, fx.accountID, id, dbstore.FormStatusPublished); err != nil {
		t.Fatalf("publish the created form: %v", err)
	}
	rec := publicServe(t, fx.pub, fx.host, "/"+slug)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /%s (the slug the create reported) = %d, want 200 (body %s)", slug, rec.Code, rec.Body.String())
	}
}

// TestPatchFormKeepsAPreRuleReservedSlugEditable is the compatibility half on
// the wire: prod answered 200 for a PATCH onto "login", so a row like that can
// exist. The rule must not brick it — the dashboard re-sends the slug it read on
// every settings save, so a PATCH carrying the SAME reserved slug has to succeed
// and write the other fields. (Only a NEW claim is refused; see the test above.)
func TestPatchFormKeepsAPreRuleReservedSlugEditable(t *testing.T) {
	fx := newRenameFixture(t, "it-legacy-"+randHex(4))
	// Reach the state the defect produced: a reserved slug already stored.
	if _, err := fx.pool.Exec(fx.ctx, `UPDATE forms SET slug='privacy' WHERE id=$1`, fx.formID); err != nil {
		t.Fatalf("simulate a pre-rule row: %v", err)
	}

	code, raw := fx.patch(t, `{"slug":"privacy","title":"Edited after the rule"}`)
	if code != http.StatusOK {
		t.Fatalf("PATCH re-sending the slug the form already has = %d, want 200 — otherwise a form stored "+
			"before the rule can never have its settings saved again (body %s)", code, raw)
	}
	var title string
	if err := fx.pool.QueryRow(fx.ctx, `SELECT title FROM forms WHERE id=$1`, fx.formID).Scan(&title); err != nil {
		t.Fatalf("read stored title: %v", err)
	}
	if title != "Edited after the rule" {
		t.Errorf("forms.title = %q after an unchanged-slug save, want the submitted title", title)
	}
	if got := fx.storedSlug(t); got != "privacy" {
		t.Errorf("forms.slug = %q, want it left at %q", got, "privacy")
	}
}

func TestPatchFormRefusesAReservedSlugAndWritesNothing(t *testing.T) {
	fx := newRenameFixture(t, "it-patch-reserved-"+randHex(4))
	before := fx.storedSlug(t)

	code, raw := fx.patch(t, `{"slug":"login","title":"Renamed by a refused save"}`)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode PATCH response: %v (%s)", err, raw)
	}
	assertRefusal(t, "PATCH /v1/rest/forms/{id} with \"login\"", code, out,
		http.StatusBadRequest, urlguard.FormSlugReservedCode, "login")

	if got := fx.storedSlug(t); got != before {
		t.Errorf("forms.slug = %q after a REFUSED rename, want %q untouched", got, before)
	}
	// The rename runs before the field write, so the refused save must not have
	// changed the title either (card t_0b7980f5's ordering rule).
	var title string
	if err := fx.pool.QueryRow(fx.ctx, `SELECT title FROM forms WHERE id=$1`, fx.formID).Scan(&title); err != nil {
		t.Fatalf("read stored title: %v", err)
	}
	if title != "Western menu" {
		t.Errorf("forms.title = %q after a REFUSED rename, want the seeded \"Western menu\" — the save wrote "+
			"the title and then failed on the slug", title)
	}
	var redirects int
	if err := fx.pool.QueryRow(fx.ctx,
		`SELECT COUNT(*) FROM form_slug_redirects WHERE form_id=$1`, fx.formID).Scan(&redirects); err != nil {
		t.Fatalf("count redirects: %v", err)
	}
	if redirects != 0 {
		t.Errorf("a refused rename left %d redirect row(s)", redirects)
	}
}
