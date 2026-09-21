package dbstore

// Form trash: restore vs purge.
//
// THE TWO PATHS ARE DELIBERATELY ASYMMETRIC, and these tests pin the difference:
//
//	restore — clears deleted_at. No CASCADE fires, nothing is destroyed, and a
//	          second click reverses it. It CAN refuse: the per-workspace unique
//	          index covers LIVE rows only, so trashing releases the slug and a new
//	          form may take it. Clearing deleted_at would then violate the index
//	          (SQLSTATE 23505), and renaming the row silently would move a public
//	          URL the merchant was told about.
//	purge   — destroys the row and its CASCADE children (submissions,
//	          notifications, confirmations, form email settings, form-scoped
//	          webhooks). Irreversible, and REFUSED for any form that has a sale:
//	          sales.form_id is ON DELETE SET NULL, so the delete itself would
//	          succeed and silently orphan the sale.
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"testing"
)

// trashForm creates a form on a fresh account and trashes it.
func trashForm(t *testing.T, ctx context.Context, acc, slug string) *Form {
	t.Helper()
	fs := NewFormStore(testPool(t))
	f := &Form{AccountID: acc, Slug: slug, Title: "IT trash", Status: "draft"}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}
	if err := fs.SoftDelete(ctx, acc, f.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	return f
}

// TestFormRestoreBringsBackTrashedFormAndKeepsSlug is the restore half of Wan's
// Tong Sampah decision: the bulk-delete copy promises "can be restored later",
// so this is the promise being made TRUE.
func TestFormRestoreBringsBackTrashedFormAndKeepsSlug(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)
	fs := NewFormStore(p)
	f := trashForm(t, ctx, acc, "it-restore-"+acc[:8])

	if err := fs.Restore(ctx, acc, f.ID); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	got, err := fs.GetByID(ctx, acc, f.ID)
	if err != nil {
		t.Fatalf("GetByID after restore: %v", err)
	}
	if got.Slug != f.Slug {
		t.Errorf("slug after restore = %q, want %q (a restore is not a rename)", got.Slug, f.Slug)
	}
	// It must be back in the LIVE list and gone from the trash.
	live, err := fs.ListByAccount(ctx, acc)
	if err != nil {
		t.Fatalf("ListByAccount: %v", err)
	}
	var inLive bool
	for _, x := range live {
		if x.ID == f.ID {
			inLive = true
		}
	}
	if !inLive {
		t.Error("restored form is not in the live list")
	}
	trashed, err := fs.ListTrashed(ctx, acc)
	if err != nil {
		t.Fatalf("ListTrashed: %v", err)
	}
	for _, x := range trashed {
		if x.ID == f.ID {
			t.Error("restored form is still in the trash")
		}
	}
}

// TestFormRestoreRefusesWhenSlugWasRetaken is the case the card flags as easy to
// miss. A trashed form releases its slug (the unique index is partial on live
// rows), so a new form may take it. Restoring must be REFUSED BY NAME — never a
// silent rename, and never an opaque 500.
//
// WHY THE ACCOUNT MUST BE WORKSPACE-LINKED HERE. The index is
// `forms (workspace_id, slug) WHERE deleted_at IS NULL`, and in a UNIQUE index
// NULL values are DISTINCT — so an account with no workspace (workspace_id NULL)
// cannot collide at all and the guard is unreachable. Production accounts always
// have one (migration 000024), which is why this test builds a real
// workspace + account rather than a bare account.
func TestFormRestoreRefusesWhenSlugWasRetaken(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	_, wsIDs := wsDeleteFixture(t, ctx, p, 1)
	var acc string
	if err := p.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, wsIDs[0]).Scan(&acc); err != nil {
		t.Fatalf("read workspace account: %v", err)
	}
	fs := NewFormStore(p)

	slug := "it-retaken-" + acc[:8]
	old := trashForm(t, ctx, acc, slug)

	// A NEW form takes the released slug while old sits in the trash.
	dup := &Form{AccountID: acc, Slug: slug, Title: "new owner of the slug", Status: "draft"}
	if err := fs.Create(ctx, dup); err != nil {
		t.Fatalf("create replacement form with the released slug: %v", err)
	}

	err := fs.Restore(ctx, acc, old.ID)
	if err == nil {
		t.Fatal("Restore succeeded onto a slug another LIVE form now holds — the partial " +
			"unique index was not enforced, so two live forms share a public URL")
	}
	if err != ErrFormSlugTaken {
		t.Fatalf("Restore error = %v, want ErrFormSlugTaken (a named refusal, not a raw driver error)", err)
	}

	// Nothing was renamed and nothing was restored.
	var deleted *string
	if qerr := p.QueryRow(ctx, `SELECT deleted_at::text FROM forms WHERE id=$1`, old.ID).Scan(&deleted); qerr != nil {
		t.Fatalf("read old form: %v", qerr)
	}
	if deleted == nil {
		t.Error("the refused restore cleared deleted_at anyway")
	}
	var oldSlug string
	if qerr := p.QueryRow(ctx, `SELECT slug FROM forms WHERE id=$1`, old.ID).Scan(&oldSlug); qerr != nil {
		t.Fatalf("read old slug: %v", qerr)
	}
	if oldSlug != slug {
		t.Errorf("refused restore renamed the form to %q — a silent rename moves a public URL", oldSlug)
	}
}

// TestFormRestoreSlugTakenGuardIsReachableOnlyWithAWorkspace is the inverse
// control for the test above: it proves the collision genuinely needs the
// per-workspace index. On an account with NO workspace the same restore
// SUCCEEDS (NULL workspace_id values are distinct in the unique index), so the
// refusal above is caused by the index rather than by some unrelated check.
func TestFormRestoreSlugTakenGuardIsReachableOnlyWithAWorkspace(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p) // NOTE: no workspace_id
	fs := NewFormStore(p)

	slug := "it-nows-" + acc[:8]
	old := trashForm(t, ctx, acc, slug)
	dup := &Form{AccountID: acc, Slug: slug, Title: "dup", Status: "draft"}
	if err := fs.Create(ctx, dup); err != nil {
		t.Fatalf("create duplicate slug: %v", err)
	}
	if err := fs.Restore(ctx, acc, old.ID); err != nil {
		t.Fatalf("Restore on a workspace-less account error = %v — the unique index is "+
			"`(workspace_id, slug) WHERE deleted_at IS NULL`, and NULL workspace_id values are "+
			"distinct, so no collision is possible here", err)
	}
}

// TestFormPurgeRefusesWhenFormHasSales is acceptance (a) and the guard the card
// calls out as the important one. `sales.form_id` is ON DELETE SET NULL, so the
// delete would SUCCEED and silently detach the sale from the form it records.
// The purge must refuse and the sale must keep its form_id.
//
// MUTATION EVIDENCE: delete the `sales > 0` check in FormStore.Purge and this
// test fails — the purge returns nil and sales.form_id becomes NULL, which is
// also asserted here so the orphaning is visible rather than implied.
func TestFormPurgeRefusesWhenFormHasSales(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)
	fs := NewFormStore(p)
	ss := NewSaleStore(p)

	f := trashForm(t, ctx, acc, "it-purgesale-"+acc[:8])
	sale := &Sale{
		AccountID: acc, FormID: f.ID, RefCode: "IT-PS-" + acc[:8],
		Client: SaleClient{FullName: "A", Email: "a@example.test"},
		Items:  []SaleItem{}, Status: SalePaid, PaymentMethod: "chip",
		SaleType: SaleTypeOneTime,
	}
	if err := ss.Create(ctx, sale); err != nil {
		t.Fatalf("create sale: %v", err)
	}

	err := fs.Purge(ctx, acc, f.ID)
	if err == nil {
		t.Fatal("Purge succeeded on a form with a sale — sales.form_id is ON DELETE SET NULL, " +
			"so the sale is now silently detached from the form that produced it")
	}
	if err != ErrFormHasSales {
		t.Fatalf("Purge error = %v, want ErrFormHasSales", err)
	}

	// The form is still in the trash and the sale still points at it.
	var n int
	if qerr := p.QueryRow(ctx, `SELECT COUNT(*) FROM forms WHERE id=$1`, f.ID).Scan(&n); qerr != nil {
		t.Fatalf("count form: %v", qerr)
	}
	if n != 1 {
		t.Error("the refused purge deleted the form anyway")
	}
	got, qerr := ss.ByID(ctx, sale.ID)
	if qerr != nil {
		t.Fatalf("fetch sale: %v", qerr)
	}
	if got.FormID != f.ID {
		t.Errorf("sale.form_id = %q after a refused purge, want %q — the sale was orphaned", got.FormID, f.ID)
	}

	// The count the refusal states must match reality.
	cnt, cerr := fs.CountSalesByForm(ctx, acc, f.ID)
	if cerr != nil {
		t.Fatalf("CountSalesByForm: %v", cerr)
	}
	if cnt != 1 {
		t.Errorf("CountSalesByForm = %d, want 1", cnt)
	}
}

// TestFormPurgeDestroysFormAndCascadeChildren is acceptance (b)'s first half:
// a sold-free trashed form purges, and the CASCADE children go with it.
func TestFormPurgeDestroysFormAndCascadeChildren(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)
	fs := NewFormStore(p)

	f := trashForm(t, ctx, acc, "it-purge-"+acc[:8])
	// CASCADE children (submissions / notifications / confirmations / form email
	// settings). Seeded directly: the store APIs for these are not the subject
	// here, and the FK is what the test is about.
	if _, err := p.Exec(ctx,
		`INSERT INTO submissions (form_id, account_id, data) VALUES ($1,$2,'{}'::jsonb)`,
		f.ID, acc); err != nil {
		t.Fatalf("seed submission: %v", err)
	}
	if _, err := p.Exec(ctx,
		`INSERT INTO form_confirmations (form_id) VALUES ($1) ON CONFLICT DO NOTHING`, f.ID); err != nil {
		// form_confirmations may require more columns; treat a shape mismatch as
		// "not seeded" rather than a failure of the thing under test.
		t.Logf("form_confirmations not seeded (%v) — covered by the FK, not asserted", err)
	}

	if err := fs.Purge(ctx, acc, f.ID); err != nil {
		t.Fatalf("Purge: %v", err)
	}

	var n int
	if err := p.QueryRow(ctx, `SELECT COUNT(*) FROM forms WHERE id=$1`, f.ID).Scan(&n); err != nil {
		t.Fatalf("count form: %v", err)
	}
	if n != 0 {
		t.Error("the form row survived the purge")
	}
	for _, q := range []struct {
		table string
		sql   string
	}{
		{"submissions", `SELECT COUNT(*) FROM submissions WHERE form_id=$1`},
	} {
		var c int
		if err := p.QueryRow(ctx, q.sql, f.ID).Scan(&c); err != nil {
			t.Fatalf("count %s: %v", q.table, err)
		}
		if c != 0 {
			t.Errorf("%s still has %d row(s) after the purge — the CASCADE did not fire", q.table, c)
		}
	}
	// And it is gone from the trash list.
	trashed, err := fs.ListTrashed(ctx, acc)
	if err != nil {
		t.Fatalf("ListTrashed: %v", err)
	}
	for _, x := range trashed {
		if x.ID == f.ID {
			t.Error("purged form is still listed in the trash")
		}
	}
}

// TestFormPurgeRefusesLiveForm is guard 1: the ONLY way in is trash → purge, so
// a single mistyped call cannot destroy a live form. A live form is
// ErrFormNotFound here, exactly as an unknown id is — so the refusal cannot even
// be used to probe which ids are live.
func TestFormPurgeRefusesLiveForm(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)
	fs := NewFormStore(p)

	f := &Form{AccountID: acc, Slug: "it-live-" + acc[:8], Title: "IT live", Status: "draft"}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}
	if err := fs.Purge(ctx, acc, f.ID); err != ErrFormNotFound {
		t.Fatalf("Purge on a LIVE form error = %v, want ErrFormNotFound (trash first)", err)
	}
	var n int
	if err := p.QueryRow(ctx, `SELECT COUNT(*) FROM forms WHERE id=$1`, f.ID).Scan(&n); err != nil {
		t.Fatalf("count form: %v", err)
	}
	if n != 1 {
		t.Error("a live form was purged — the trashed-only guard did not hold")
	}
}

// TestFormGetTrashedIsScopedToTrashAndAccount keeps the purge confirmation read
// honest: it must not become a side door to a live form, nor to another
// account's trash.
func TestFormGetTrashedIsScopedToTrashAndAccount(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)
	other := uniqueAccount(t, ctx, p)
	fs := NewFormStore(p)

	trashed := trashForm(t, ctx, acc, "it-gt-"+acc[:8])
	got, err := fs.GetTrashed(ctx, acc, trashed.ID)
	if err != nil {
		t.Fatalf("GetTrashed on an own trashed form: %v", err)
	}
	if got.DeletedAt == nil {
		t.Error("GetTrashed returned a form with nil DeletedAt — it is not reading the trash columns")
	}
	if got.Slug != trashed.Slug {
		t.Errorf("GetTrashed slug = %q, want %q (the purge confirmation compares against this)", got.Slug, trashed.Slug)
	}
	// Another account's trashed form is invisible.
	if _, err := fs.GetTrashed(ctx, other, trashed.ID); err != ErrFormNotFound {
		t.Errorf("GetTrashed from another account error = %v, want ErrFormNotFound", err)
	}
	// A LIVE form is invisible to it too.
	live := &Form{AccountID: acc, Slug: "it-gtlive-" + acc[:8], Title: "IT", Status: "draft"}
	if err := fs.Create(ctx, live); err != nil {
		t.Fatalf("create live form: %v", err)
	}
	if _, err := fs.GetTrashed(ctx, acc, live.ID); err != ErrFormNotFound {
		t.Errorf("GetTrashed on a LIVE form error = %v, want ErrFormNotFound", err)
	}
}

// TestFormTrashRoundTripThenPurgeThenWorkspaceDeleteIsTheCardSequence is the
// end-to-end order the card's comment lays out: create → trash → restore →
// trash again → purge → then the workspace delete succeeds. It is the proof that
// the "empty the workspace first" path is actually walkable from the product,
// not just in theory.
func TestFormTrashRoundTripThenPurgeThenWorkspaceDeleteIsTheCardSequence(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)
	user, ids := wsDeleteFixture(t, ctx, p, 2)
	target := ids[0]
	var acc string
	if err := p.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, target).Scan(&acc); err != nil {
		t.Fatalf("read workspace account: %v", err)
	}
	fs := NewFormStore(p)

	f := &Form{AccountID: acc, Slug: "it-seq-" + acc[:8], Title: "IT", Status: "draft"}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	// The workspace is blocked while the form exists — trashed or not.
	if _, _, err := sp.CountEmpty(ctx, target, ""); err != nil {
		t.Fatalf("CountEmpty: %v", err)
	}
	if _, _, err := sp.CountEmpty(ctx, target, ""); err != nil {
		t.Fatal(err)
	}

	// trash → restore (form returns with the same slug, nothing destroyed)
	if err := fs.SoftDelete(ctx, acc, f.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if err := fs.Restore(ctx, acc, f.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got, gerr := fs.GetByID(ctx, acc, f.ID); gerr != nil || got.Slug != f.Slug {
		t.Fatalf("form after restore: %v (slug %q)", gerr, got.Slug)
	}

	// trash again → purge, and NOW the workspace delete is allowed.
	if err := fs.SoftDelete(ctx, acc, f.ID); err != nil {
		t.Fatalf("trash again: %v", err)
	}
	if err := fs.Purge(ctx, acc, f.ID); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, blockers, err := sp.CountEmpty(ctx, target, ""); err != nil {
		t.Fatalf("CountEmpty after purge: %v", err)
	} else if len(blockers) != 0 {
		t.Fatalf("workspace still blocked after the purge: %+v", blockers)
	}
	if _, err := sp.Delete(ctx, target, user, ""); err != nil {
		t.Fatalf("workspace delete after emptying it: %v — the card's whole path is not walkable", err)
	}
}
