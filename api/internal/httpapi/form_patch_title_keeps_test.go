package httpapi

// Regression guard for the carded follow-up t_0b7980f5: a PARTIAL PATCH of a
// form must never destroy data it did not name.
//
// THE BUG, measured on prod v1.18.0 (PATCH /v1/rest/forms/{id} with a slug and
// NO title returned 200 and emptied the form's title):
//
//	PATCH {"title":"New form"}          -> 200   title="New form"
//	PATCH {"slug":"new-form-faf5"} only -> 200   title=<EMPTY>
//
// Two independent causes, and this file pins both:
//
//  1. `FormStore.Update` wrote `title=$3` RAW. The handler's request struct
//     types title as a plain string, so an OMITTED title decodes to "" — the
//     zero value — and the statement stored it, while the fields beside it
//     (config/theme) are COALESCEd and keep the stored value. The comment above
//     the slug write even STATES that contract ("an omitted slug keeps the
//     stored value — the same 'leave unchanged' contract config/theme have"),
//     so the field next to it was documented as doing something it did not.
//
//  2. The endpoint wrote title/config/theme FIRST and then performed the slug
//     rename, which is the only write here that can be REFUSED (409 on a
//     collision, 400 on a malformed slug). A refused rename therefore returned
//     its error with the earlier field write already committed: the dashboard's
//     own save (form-settings always sends a title) changed the title and then
//     told the merchant the save had failed.
//
// WHAT THIS FILE PINS (each assertion is one of the card's acceptance criteria)
//
//  1. A slug-only PATCH — the card's exact repro, naming the slug the form
//     already has — returns 200 and leaves forms.title untouched, asserted
//     against the DATABASE rather than the response (the defect was a lying 200).
//  2. The same holds across a rename that really moves, and for an explicitly
//     EMPTY title: an empty title means "leave unchanged" exactly as an empty
//     slug does.
//  3. The CONTROL: a PATCH that carries a title still writes it. Without this a
//     "fix" that simply ignored the title column entirely would pass the two
//     assertions above.
//  4. A refused rename (409, code=form.slugInUse) leaves the title as it was —
//     the half-applied save the browser observed, where the page showed a
//     refusal while the row had already changed.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when unset.
// Real DB + real mux on purpose: a fake store would let the guard pass while the
// statement still wrote the zero value, which is precisely the failure here.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// storedTitle reads a form's title straight out of the database. Every
// assertion about the title goes through here rather than through the PATCH
// response, because a 200 that lied was half of the defect.
func (fx renameFixture) storedTitle(t *testing.T) string {
	t.Helper()
	var title string
	if err := fx.pool.QueryRow(fx.ctx,
		`SELECT title FROM forms WHERE id=$1 AND account_id=$2`, fx.formID, fx.accountID).Scan(&title); err != nil {
		t.Fatalf("read stored title: %v", err)
	}
	return title
}

// TestFormPatchOmittingTitleKeepsTheStoredTitle is the card's reproduction,
// plus the controls that make it a guard rather than a tautology.
func TestFormPatchOmittingTitleKeepsTheStoredTitle(t *testing.T) {
	fx := newRenameFixture(t, "it-title-keep-"+randHex(4))
	const seeded = "Western menu" // newRenameFixture's title
	if got := fx.storedTitle(t); got != seeded {
		t.Fatalf("pre-condition: fixture title is %q, want %q", got, seeded)
	}

	// ── 1. The card's own repro: a body naming the slug the form already has.
	// At the PATCH level that is the no-op rename branch, and it used to return
	// 200 with the title emptied.
	code, raw := fx.patch(t, `{"slug":"`+fx.slug+`"}`)
	if code != http.StatusOK {
		t.Fatalf("slug-only PATCH = %d, want 200 (body %s)", code, raw)
	}
	if got := fx.storedTitle(t); got != seeded {
		t.Fatalf("forms.title = %q after PATCH {\"slug\":...} with no title, want the stored %q.\n"+
			"That is the carded defect: the handler's request struct decodes an omitted title to \"\" "+
			"and FormStore.Update wrote it raw, so a partial PATCH destroyed data it never named. "+
			"config and theme keep their stored value for an omitted key; title must do the same.",
			got, seeded)
	}
	if got := fx.storedSlug(t); got != fx.slug {
		t.Errorf("forms.slug = %q after a same-slug PATCH, want it untouched (%q)", got, fx.slug)
	}

	// ── 2. The same contract across a rename that really moves: a renamer must
	// not have to resend the title to keep it.
	newSlug := "it-title-keep-new-" + randHex(4)
	code, raw = fx.patch(t, `{"slug":"`+newSlug+`"}`)
	if code != http.StatusOK {
		t.Fatalf("slug-only rename PATCH = %d, want 200 (body %s)", code, raw)
	}
	if got := fx.storedSlug(t); got != newSlug {
		t.Fatalf("forms.slug = %q after a rename, want %q", got, newSlug)
	}
	if got := fx.storedTitle(t); got != seeded {
		t.Errorf("forms.title = %q after a rename that omitted the title, want %q", got, seeded)
	}

	// ── 3. An explicitly empty title means the same thing (the title analogue
	// of an empty slug, which the handler already skips).
	if code, raw := fx.patch(t, `{"title":"   "}`); code != http.StatusOK {
		t.Fatalf("blank-title PATCH = %d, want 200 (body %s)", code, raw)
	}
	if got := fx.storedTitle(t); got != seeded {
		t.Errorf("forms.title = %q after PATCH {\"title\":\"   \"}, want the stored %q — "+
			"an empty title is not a request to blank a live form's title", got, seeded)
	}

	// ── 4. THE CONTROL: a real title still lands. Without this, a change that
	// dropped the title column entirely would satisfy everything above.
	if code, raw := fx.patch(t, `{"title":"Renamed title"}`); code != http.StatusOK {
		t.Fatalf("title-only PATCH = %d, want 200 (body %s)", code, raw)
	}
	if got := fx.storedTitle(t); got != "Renamed title" {
		t.Errorf("forms.title = %q after PATCH {\"title\":\"Renamed title\"}, want the new title — "+
			"the keep-existing rule must not stop a title from being changed", got)
	}
	// And the seed stays visible: a title-only PATCH must not move the slug.
	if got := fx.storedSlug(t); got != newSlug {
		t.Errorf("forms.slug = %q after a title-only PATCH, want %q", got, newSlug)
	}
}

// TestFormPatchRefusedSlugRenameLeavesTheTitleUnwritten covers the second half
// of the defect: the field write used to commit BEFORE the rename that can be
// refused, so a collision answered 409 with the title already changed.
func TestFormPatchRefusedSlugRenameLeavesTheTitleUnwritten(t *testing.T) {
	fx := newRenameFixture(t, "it-title-refuse-"+randHex(4))
	const seeded = "Western menu"

	// Another LIVE form in the same account owns the slug the save will ask
	// for. RenameSlug's collision check (SlugExistsNotID) is per account, so
	// this refusal is reachable from the PATCH route.
	taken := "it-title-refuse-taken-" + randHex(4)
	other := &dbstore.Form{
		AccountID: fx.accountID, Slug: taken, Title: "Other form",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{"blocks": []any{}}, Theme: map[string]any{},
	}
	fs := dbstore.NewFormStore(fx.pool)
	if err := fs.Create(fx.ctx, other); err != nil {
		t.Fatalf("seed the form owning the slug: %v", err)
	}

	// The dashboard's own save shape: a new title AND a slug another live form
	// already serves. Before the fix this returned the 409 with forms.title
	// already set to the new value.
	code, raw := fx.patch(t, `{"title":"QA title probe refused save","slug":"`+taken+`"}`)
	if code != http.StatusConflict {
		t.Fatalf("PATCH onto a taken slug = %d, want 409 (body %s)", code, raw)
	}
	var body struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode refusal body: %v (%s)", err, raw)
	}
	if body.Code != CodeFormSlugInUse {
		t.Errorf("refusal code = %q, want %q", body.Code, CodeFormSlugInUse)
	}

	if got := fx.storedTitle(t); got != seeded {
		t.Fatalf("forms.title = %q after a save REFUSED for its slug, want the stored %q.\n"+
			"The save is half-applied: the title/theme write ran before the rename that was refused, "+
			"so the merchant was told the save failed while their form had already changed.",
			got, seeded)
	}
	if got := fx.storedSlug(t); got != fx.slug {
		t.Errorf("forms.slug = %q after a refused rename, want the stored %q", got, fx.slug)
	}
	// The form that owns the slug is untouched too — a refused write must not
	// reach either row.
	var otherTitle, otherSlug string
	if err := fx.pool.QueryRow(fx.ctx,
		`SELECT slug, title FROM forms WHERE id=$1`, other.ID).Scan(&otherSlug, &otherTitle); err != nil {
		t.Fatalf("read the other form: %v", err)
	}
	if otherSlug != taken || otherTitle != "Other form" {
		t.Errorf("the form owning the slug is now slug=%q title=%q, want it untouched (%q/%q)",
			otherSlug, otherTitle, taken, "Other form")
	}
}
