package dbstore

// The CREATE half of the per-workspace slug collision (card t_c9c7b104).
//
// ONE USER-VISIBLE SITUATION, TWO ANSWERS. A slug already taken in a workspace
// answered 500 `internal` when it was reached through an INSERT and 409
// `form.slugInUse` when it was reached through a rename. Measured from outside
// on live prod v1.19.1:
//
//	POST  /v1/rest/forms    {"slug":"taken"} -> 500 {"code":"internal"}
//	PATCH /v1/rest/forms/{id} {"slug":"taken"} -> 409 {"code":"form.slugInUse"}
//
// and the prod log names the cause: `duplicate key value violates unique
// constraint "forms_workspace_slug_key" (SQLSTATE 23505)` escaping
// FormStore.Create into respondErr's default branch with error_class=internal.
//
// WHAT THIS FILE PINS
//
//	1. Create on a taken slug is dbstore.ErrSlugTaken — a NAMED refusal, not the
//	   raw driver error. This is the mapping every caller inherits (REST
//	   createForm, the template-use routes, MCP upsert_form's create path).
//	2. Nothing was inserted: the collision does not leave a partial row.
//	3. A CONTROL that the fixture can actually SEE the collision. The unique
//	   index is `forms (workspace_id, slug) WHERE deleted_at IS NULL`, and NULL
//	   values are distinct in a unique index — so an account with no workspace
//	   CANNOT collide and any test built on one proves nothing. The control
//	   asserts the difference in both directions instead of assuming it.
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"testing"
)

// createCollideFixture seeds a workspace + a workspace-linked account, and
// fails loudly if the link is missing — the whole guard is unreachable without
// it (see the NULL-workspace control below).
func createCollideFixture(t *testing.T, ctx context.Context) (string, string) {
	t.Helper()
	p := testPool(t)
	_, wsIDs := wsDeleteFixture(t, ctx, p, 1)
	var acc string
	if err := p.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, wsIDs[0]).Scan(&acc); err != nil {
		t.Fatalf("read workspace account: %v", err)
	}
	var ws *string
	if err := p.QueryRow(ctx, `SELECT workspace_id::text FROM accounts WHERE id=$1`, acc).Scan(&ws); err != nil {
		t.Fatalf("read account workspace: %v", err)
	}
	if ws == nil {
		t.Fatal("fixture account has no workspace_id, so the per-workspace slug index " +
			"cannot be reached at all (in a UNIQUE index NULL values are DISTINCT) — " +
			"this guard would pass vacuously")
	}
	return acc, *ws
}

// TestFormCreateOnTakenSlugIsNotA500 is the card's acceptance criterion at the
// store level: the INSERT collision is a named refusal the API can map to 409.
//
// BITE: remove the `if isUniqueViolation(err) { return ErrSlugTaken }` branch in
// FormStore.Create and this test fails on the sentinel assertion with the raw
// `ERROR: duplicate key value violates unique constraint "forms_workspace_slug_key"`
// — i.e. the exact error that reached respondErr and became the reported 500.
func TestFormCreateOnTakenSlugIsNotA500(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc, _ := createCollideFixture(t, ctx)
	fs := NewFormStore(p)

	slug := "it-create-taken-" + acc[:8]
	first := &Form{AccountID: acc, Slug: slug, Title: "first owner of the slug", Status: FormStatusDraft}
	if err := fs.Create(ctx, first); err != nil {
		t.Fatalf("create the form that owns the slug: %v", err)
	}

	second := &Form{AccountID: acc, Slug: slug, Title: "second, colliding", Status: FormStatusDraft}
	err := fs.Create(ctx, second)
	if err == nil {
		t.Fatal("Create succeeded on a slug another live form of the workspace already serves — " +
			"two live forms now share one public URL")
	}
	if err != ErrSlugTaken {
		t.Fatalf("Create on a taken slug returned %v, want dbstore.ErrSlugTaken.\n"+
			"A raw driver error here is the carded defect: the API maps only the store's "+
			"sentinel, so respondErr's default branch logged error_class=internal and "+
			"answered 500 `internal error` with no code — the caller cannot tell \"slug taken\" "+
			"from \"the server broke\", while the rename path onto the same slug already "+
			"answers 409 form.slugInUse.", err)
	}

	// The refused insert left no partial row: still exactly one form on the slug.
	var n int
	if err := p.QueryRow(ctx,
		`SELECT COUNT(*) FROM forms WHERE account_id=$1 AND slug=$2 AND deleted_at IS NULL`,
		acc, slug).Scan(&n); err != nil {
		t.Fatalf("count forms on the slug: %v", err)
	}
	if n != 1 {
		t.Errorf("live forms on slug %q after a refused create = %d, want 1", slug, n)
	}
}

// TestFormCreateSlugIndexIsWorkspaceScoped is the CONTROL the guard above needs:
// it proves this fixture can tell a workspace-scoped collision from no
// collision at all, so the assertions in TestFormCreateOnTakenSlugIsNotA500 are
// about the real index rather than about a table where nothing can collide.
//
// It is the card's own warning, made executable: "the NULL-workspace case
// cannot collide, so a test that creates without a workspace proves nothing".
// An account with no workspace yields forms with workspace_id NULL, and in a
// UNIQUE index NULL values are DISTINCT — so the same slug twice is ACCEPTED
// there. Asserted only while accounts.workspace_id is still nullable; if a
// migration makes it NOT NULL the control says so instead of failing for a
// reason that has nothing to do with the slug rule.
func TestFormCreateSlugIndexIsWorkspaceScoped(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	fs := NewFormStore(p)

	var nullable string
	if err := p.QueryRow(ctx,
		`SELECT is_nullable FROM information_schema.columns
		 WHERE table_name='accounts' AND column_name='workspace_id'`).Scan(&nullable); err != nil {
		t.Fatalf("read accounts.workspace_id nullability: %v", err)
	}
	if nullable != "YES" {
		t.Skipf("accounts.workspace_id is NOT NULL (%s): an unlinked account no longer exists, "+
			"so the NULL-workspace control has no subject", nullable)
	}

	// A bare account, deliberately with no workspace (uniqueAccount, as the
	// other dbstore fixtures build one).
	bare := uniqueAccount(t, ctx, p)
	var ws *string
	if err := p.QueryRow(ctx, `SELECT workspace_id::text FROM accounts WHERE id=$1`, bare).Scan(&ws); err != nil {
		t.Fatalf("read bare account workspace: %v", err)
	}
	if ws != nil {
		t.Fatalf("the bare account got workspace_id %s; uniqueAccount is no longer workspace-free "+
			"and this control is measuring the wrong thing", *ws)
	}

	slug := "it-nullws-" + bare[:8]
	a := &Form{AccountID: bare, Slug: slug, Title: "no workspace, first", Status: FormStatusDraft}
	if err := fs.Create(ctx, a); err != nil {
		t.Fatalf("create on the workspace-free account: %v", err)
	}
	b := &Form{AccountID: bare, Slug: slug, Title: "no workspace, second", Status: FormStatusDraft}
	if err := fs.Create(ctx, b); err != nil {
		t.Fatalf("the same slug twice on a workspace-free account was refused (%v), so this control "+
			"no longer demonstrates the NULL-workspace case — which means it can no longer prove "+
			"that TestFormCreateOnTakenSlugIsNotA500's account is the reason it collides", err)
	}

	// And the linked account DOES collide on the same slug — the other half of
	// the difference, so the control proves both directions.
	linked, _ := createCollideFixture(t, ctx)
	l1 := &Form{AccountID: linked, Slug: slug, Title: "linked, first", Status: FormStatusDraft}
	if err := fs.Create(ctx, l1); err != nil {
		t.Fatalf("create on the workspace-linked account: %v", err)
	}
	l2 := &Form{AccountID: linked, Slug: slug, Title: "linked, second", Status: FormStatusDraft}
	if err := fs.Create(ctx, l2); err != ErrSlugTaken {
		t.Fatalf("the same slug twice on a WORKSPACE-LINKED account returned %v, want ErrSlugTaken — "+
			"without this the per-workspace unique index is not enforcing anything and the guard "+
			"above is vacuous", err)
	}
}
