package httpapi

// The REST half of card t_c9c7b104: POST /v1/rest/forms with an already-taken
// slug must answer 409 form.slugInUse, exactly as the rename path onto the same
// slug does — never 500.
//
// THE DEFECT, measured from outside on live prod v1.19.1 (72092730) in a
// throwaway workspace:
//
//	POST  /v1/rest/forms?workspace=qa847ws {"title":"QA 847 ws probe 2","slug":"qa847-ws-collide"}
//	  -> 500 {"code":"internal","error":"internal error"}
//	PATCH /v1/rest/forms/{id}?workspace=qa847ws {"slug":"qa847-ws-collide"}
//	  -> 409 {"code":"form.slugInUse","error":"slug sudah digunakan oleh borang lain dalam workspace ini"}
//
// with the prod api log naming the frame and the cause:
//
//	ERROR internal_error err="ERROR: duplicate key value violates unique constraint
//	  \"forms_workspace_slug_key\" (SQLSTATE 23505)" error_class=internal
//	  path=/v1/rest/forms ... httpapi.(*RESTHandlers).createForm rest_handlers.go:899
//
// WHY IT IS NOT ONLY A STORE TEST. The store half (form_create_slug_test.go)
// pins the mapping; this half drives the REAL route through the REAL mux with a
// real API key, reads the status and the machine code out of the wire body, and
// asserts the two paths agree with each other. A store-only guard would still
// pass if createForm grew its own unrelated error handling, and a handler-only
// guard built on a fake store would pass while the real INSERT mapping was
// absent — which is precisely the shipped defect.
//
// THE WORKSPACE-LINKED ACCOUNT IS LOAD-BEARING, like the store test's: the
// index is `forms (workspace_id, slug) WHERE deleted_at IS NULL` and NULL
// values are distinct in a unique index, so an account with no workspace cannot
// collide. buildRenameFixture (form_slug_rename_test.go) is reused verbatim
// because it already builds workspace -> account -> form with the link in place
// and verifies it through the public serving path.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when unset.

import (
	"encoding/json"
	"net/http"
	"testing"
)

// createFormBody POSTs a form through the real REST route and returns the
// status plus the decoded envelope (the error shape and the success shape share
// the keys this test reads).
func createFormBody(t *testing.T, fx renameFixture, body string) (int, map[string]any) {
	t.Helper()
	code, raw := tplReq(t, fx.h, fx.key, http.MethodPost, "/v1/rest/forms", body)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode POST /v1/rest/forms response: %v (%s)", err, raw)
	}
	return code, out
}

// TestCreateFormOnTakenSlugIs409NotA500 is the card's acceptance criterion on
// the exact surface the defect was measured on.
//
// BITE: revert the ErrSlugTaken mapping in FormStore.Create and the second POST
// answers 500 with code `internal` — this test fails on the status assertion and
// names the defect.
func TestCreateFormOnTakenSlugIs409NotA500(t *testing.T) {
	fx := newRenameFixture(t, "it-create-ok-"+randHex(4))

	// The slug is taken by a live form of the same workspace: fx.formID already
	// serves fx.slug. So the FIRST create below claims a NEW slug and the
	// SECOND collides on it, reproducing the prod probe's shape exactly.
	taken := "it-create-taken-" + randHex(4)
	code, out := createFormBody(t, fx, `{"title":"first owner of the slug","slug":"`+taken+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("first POST with a free slug = %d, want 201 (body %v)", code, out)
	}
	firstID, _ := out["id"].(string)
	if firstID == "" {
		t.Fatalf("the created form has no id in its response: %v", out)
	}

	code, out = createFormBody(t, fx, `{"title":"second, colliding","slug":"`+taken+`"}`)
	if code != http.StatusConflict {
		t.Fatalf("POST /v1/rest/forms with the already-taken slug %q = %d (body %v), want 409.\n"+
			"A 500 here IS the carded defect: the raw SQLSTATE 23505 on forms_workspace_slug_key "+
			"escaped FormStore.Create into respondErr's default branch, which logs "+
			"error_class=internal and answers `internal error` with no code — so a REST API-key "+
			"caller or an MCP upsert_form cannot tell \"slug taken\" from \"the server broke\". "+
			"Measured on live prod v1.19.1: POST -> 500 {\"code\":\"internal\"}.", taken, code, out)
	}
	if got, _ := out["code"].(string); got != CodeFormSlugInUse {
		t.Errorf("collision envelope code = %q, want %q — the same code the rename path onto this "+
			"slug already answers, so one user-visible situation has one answer and the dashboard "+
			"renders one sentence for both", got, CodeFormSlugInUse)
	}
	if msg, _ := out["error"].(string); msg == "" {
		t.Error("the collision envelope dropped its prose — API-key, curl and MCP callers read it")
	}
	if msg, _ := out["error"].(string); msg == "internal error" {
		t.Errorf("the collision envelope still carries the generic internal prose (%q)", msg)
	}

	// The refused create inserted nothing: exactly one live form on the slug.
	var n int
	if err := fx.pool.QueryRow(fx.ctx,
		`SELECT COUNT(*) FROM forms WHERE account_id=$1 AND slug=$2 AND deleted_at IS NULL`,
		fx.accountID, taken).Scan(&n); err != nil {
		t.Fatalf("count forms on the slug: %v", err)
	}
	if n != 1 {
		t.Errorf("live forms on slug %q after a refused create = %d, want 1 — the collision must "+
			"not leave a row behind", taken, n)
	}

	// The form that owns the slug is untouched, and its URL still resolves.
	var owner string
	if err := fx.pool.QueryRow(fx.ctx, `SELECT slug FROM forms WHERE id=$1`, firstID).Scan(&owner); err != nil {
		t.Fatalf("read the owning form's slug: %v", err)
	}
	if owner != taken {
		t.Errorf("the form owning slug %q now has %q — a refused create must not touch it", taken, owner)
	}
}

// TestCreateAndRenameAnswerTheSameForATakenSlug is the card's own framing made
// executable: it drives BOTH paths onto the SAME taken slug in the SAME
// workspace and asserts they agree. This is the assertion a fix that only
// special-cased createForm (or only the store) could still fail.
func TestCreateAndRenameAnswerTheSameForATakenSlug(t *testing.T) {
	fx := newRenameFixture(t, "it-both-ok-"+randHex(4))

	taken := "it-both-taken-" + randHex(4)
	if code, out := createFormBody(t, fx, `{"title":"owner","slug":"`+taken+`"}`); code != http.StatusCreated {
		t.Fatalf("seed the taken slug = %d, want 201 (body %v)", code, out)
	}

	createCode, createOut := createFormBody(t, fx, `{"title":"create collides","slug":"`+taken+`"}`)
	renameCode, renameRaw := fx.patch(t, `{"slug":"`+taken+`","title":"Western menu"}`)
	var renameOut map[string]any
	if err := json.Unmarshal(renameRaw, &renameOut); err != nil {
		t.Fatalf("decode PATCH response: %v (%s)", err, renameRaw)
	}

	if createCode != renameCode {
		t.Errorf("POST onto a taken slug = %d but PATCH onto the SAME slug = %d.\n"+
			"One user-visible situation answered two ways is the carded defect; the rename path "+
			"(FormStore.RenameSlug -> ErrSlugTaken -> 409) is the correct answer and the create "+
			"path must match it.", createCode, renameCode)
	}
	if createCode != http.StatusConflict || renameCode != http.StatusConflict {
		t.Fatalf("both paths must answer 409 for a taken slug; got create=%d rename=%d",
			createCode, renameCode)
	}
	cCode, _ := createOut["code"].(string)
	rCode, _ := renameOut["code"].(string)
	if cCode != rCode {
		t.Errorf("POST code = %q but PATCH code = %q — the dashboard translates ONE machine code "+
			"per sentence, so a divergence means one of the two refusals renders something else",
			cCode, rCode)
	}
	if cCode != CodeFormSlugInUse {
		t.Errorf("both paths should carry %q; got %q", CodeFormSlugInUse, cCode)
	}
	// And the rename was refused too: the form being renamed kept its own slug.
	if got := fx.storedSlug(t); got != fx.slug {
		t.Errorf("the renamed form's slug changed to %q on a refused rename, want %q untouched",
			got, fx.slug)
	}
}
