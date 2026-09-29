package dbstore

// Workspace trash — the bin, the restore, and the purge that is still a hard
// delete.
//
// WHAT THIS PINS
//
//  1. "Satu user wajib ada 1 workspace" — the LAST LIVE workspace cannot be
//     binned. The rule is expressed as a count of the owner's remaining live
//     memberships, so it protects whichever workspace is last rather than
//     pinning an "original" one — and a workspace ALREADY IN THE BIN does not
//     count as one the user still has.
//  2. Delete MARKS the workspace. Every row it holds (accounts, forms,
//     products, sales, api_keys, settings, members, invites, legal pages,
//     email settings) is still present afterwards — asserted by count BEFORE
//     and AFTER, which is the only way to tell a trash from a delete.
//  3. The binned workspace leaves the live list (ByOwner / ByMember / BySlug /
//     ByID) and appears in ListTrashed, newest first. Restore brings it back
//     whole.
//  4. A binned workspace RELEASES its slug (the unique index is partial over
//     live rows), so a new workspace may take the name; restoring the original
//     afterwards is REFUSED with ErrWorkspaceSlugTaken rather than colliding
//     silently (the form pattern: TestFormRestoreRefusesWhenSlugWasRetaken).
//  5. Purge is the hard delete, and it keeps the ORDER the old delete had:
//     sales → forms → products → api_keys → accounts → workspaces, because
//     those FKs are NO ACTION and a bare `DELETE FROM workspaces` fails
//     (SQLSTATE 23503) on any workspace holding an account. Purge refuses a
//     LIVE row (trash is the only way in) and refuses a workspace that has
//     ever been SOLD.
//  6. users.default_workspace_id pointing at a binned workspace becomes NULL:
//     the old FK's ON DELETE SET NULL fired on the hard delete, and a mark
//     fires no FK at all, so the store has to clear it or the next login lands
//     on a workspace nobody can see.
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// wsDeleteFixture creates a user plus N empty workspaces (each with the account
// WorkspaceStore.Create makes), returning the user id and the workspace ids in
// creation order.
func wsDeleteFixture(t *testing.T, ctx context.Context, p *pgxpool.Pool, n int) (string, []string) {
	t.Helper()
	sp := NewWorkspaceStore(p)
	user := testOwnerUser(t, ctx, p)
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		slug := "it-wsdel-" + user[:8] + "-" + string(rune('a'+i))
		w, err := sp.Create(ctx, user, "Delete me", slug)
		if err != nil {
			t.Fatalf("create workspace %d: %v", i, err)
		}
		ids = append(ids, w.ID)
	}
	return user, ids
}

// TestWorkspaceDeleteRefusesLastWorkspace is Wan's rule: with exactly one
// workspace, the delete is refused AND the workspace is still there.
//
// MUTATION EVIDENCE: neutralise the `remaining == 0` guard in
// WorkspaceStore.Trash and this test fails on both assertions (the row is
// marked and err is nil).
func TestWorkspaceDeleteRefusesLastWorkspace(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)
	user, ids := wsDeleteFixture(t, ctx, p, 1)
	only := ids[0]

	_, err := sp.Delete(ctx, only, user, "")
	if err == nil {
		t.Fatal("Delete on the owner's ONLY workspace returned nil error — the " +
			"\"at least one workspace\" invariant is not enforced")
	}
	if !IsLastWorkspaceError(err) {
		t.Fatalf("Delete error = %v, want the last_workspace refusal", err)
	}

	// The workspace must still exist, live, with its account.
	var n int
	if err := p.QueryRow(ctx, `SELECT COUNT(*) FROM workspaces WHERE id=$1`, only).Scan(&n); err != nil {
		t.Fatalf("count workspace: %v", err)
	}
	if n != 1 {
		t.Errorf("workspace rows after a refused delete = %d, want 1 (the refusal must not delete)", n)
	}
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM workspaces WHERE id=$1 AND deleted_at IS NULL`, only); n != 1 {
		t.Error("the refused workspace was marked anyway — a refusal must leave the row LIVE")
	}
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM accounts WHERE workspace_id=$1`, only); n != 1 {
		t.Errorf("account rows after a refused delete = %d, want 1", n)
	}
}

// TestWorkspaceDeleteLastWorkspaceAllowsSecondWorkspace is the other half of the
// rule: with TWO workspaces, one may be binned. Without this, a guard that
// refused every delete would pass the test above and lock every merchant in.
//
// The last assertion is the one that makes `deleted_at IS NULL` on the census
// load-bearing: once the first workspace is IN THE BIN it must not count as
// somewhere the user can still go, so binning the second one is now refused.
func TestWorkspaceDeleteLastWorkspaceAllowsSecondWorkspace(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)
	user, ids := wsDeleteFixture(t, ctx, p, 2)

	trashed, err := sp.Delete(ctx, ids[0], user, "")
	if err != nil {
		t.Fatalf("Delete with a second workspace remaining: %v", err)
	}
	if trashed["workspaces"] != 1 {
		t.Errorf("deleted[workspaces] = %d, want 1", trashed["workspaces"])
	}

	// The binned workspace is off the LIVE list and on the trash list.
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM workspaces WHERE owner_user_id=$1 AND deleted_at IS NULL`, user); n != 1 {
		t.Errorf("live workspaces left = %d, want exactly 1", n)
	}
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM workspaces WHERE id=$1 AND deleted_at IS NOT NULL`, ids[0]); n != 1 {
		t.Error("the deleted workspace was not marked — deleted_at is still NULL")
	}
	if live, err := sp.ByOwner(ctx, user); err != nil {
		t.Fatalf("ByOwner: %v", err)
	} else if len(live) != 1 || live[0].ID != ids[1] {
		t.Errorf("ByOwner = %+v, want only the un-binned workspace %s", live, ids[1])
	}
	if _, err := sp.ByID(ctx, ids[0]); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("ByID on a binned workspace error = %v, want a miss — a trashed workspace "+
			"must be invisible to the live reads or the switcher offers it", err)
	}
	bin, err := sp.ListTrashed(ctx, user)
	if err != nil {
		t.Fatalf("ListTrashed: %v", err)
	}
	if len(bin) != 1 || bin[0].ID != ids[0] {
		t.Fatalf("ListTrashed = %+v, want the one binned workspace %s", bin, ids[0])
	}
	if bin[0].DeletedAt == nil {
		t.Error("the trash row has a nil DeletedAt — the trash view cannot show when it was binned")
	}

	// The workspace that remains is binnable in principle — but it is the LAST
	// LIVE one now, and a binned workspace is not somewhere to land.
	if _, err := sp.Delete(ctx, ids[1], user, ""); !IsLastWorkspaceError(err) {
		t.Errorf("deleting the now-last live workspace error = %v, want last_workspace refusal "+
			"(a workspace already in the bin must not count as one the user still has)", err)
	}
}

// TestWorkspaceTrashKeepsEveryRow is acceptance 1 and the heart of the card:
// the delete is a MARK, so every row the old hard delete would have destroyed is
// still on disk afterwards. Counted BEFORE and AFTER, so a store that quietly
// deleted one table passes no part of this.
func TestWorkspaceTrashKeepsEveryRow(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)
	user, ids := wsDeleteFixture(t, ctx, p, 2)
	target, keep := ids[0], ids[1]

	var acc string
	if err := p.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, target).Scan(&acc); err != nil {
		t.Fatalf("read workspace account: %v", err)
	}

	// Content in every table the old delete cleared, including the RESTRICT
	// ones the not_empty guard used to refuse: a workspace FULL of content is
	// exactly what the card says must now be binnable.
	fs := NewFormStore(p)
	form := &Form{AccountID: acc, Slug: "it-wstrash-" + acc[:8], Title: "IT", Status: FormStatusDraft}
	if err := fs.Create(ctx, form); err != nil {
		t.Fatalf("seed form: %v", err)
	}
	if err := NewProductStore(p).Create(ctx, &Product{
		AccountID: acc, Name: "IT", PriceSen: 100, Type: "one_time",
	}); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	ss := NewSaleStore(p)
	if err := ss.Create(ctx, &Sale{
		AccountID: acc, RefCode: "IT-WSTRASH-" + acc[:8],
		Client: SaleClient{FullName: "A", Email: "a@example.test"},
		Items:  []SaleItem{}, Status: SalePaid, PaymentMethod: "chip",
		SaleType: SaleTypeOneTime,
	}); err != nil {
		t.Fatalf("seed sale: %v", err)
	}
	if _, err := p.Exec(ctx,
		`INSERT INTO api_keys (account_id, name, key_hash, scopes, workspace_id)
		 VALUES ($1,'it','it-trash-'||$2,'{admin}',$3)`, acc, acc[:8], target); err != nil {
		t.Fatalf("seed live api key: %v", err)
	}
	if _, err := sp.Settings(ctx, target); err != nil {
		t.Fatalf("materialise settings: %v", err)
	}
	if _, err := p.Exec(ctx,
		`INSERT INTO workspace_invites (workspace_id, email, role, token) VALUES ($1,$2,'read_only','it-trash-inv-'||$3)`,
		target, "invite-"+acc[:8]+"@example.test", acc[:8]); err != nil {
		t.Fatalf("seed invite: %v", err)
	}
	if _, err := p.Exec(ctx,
		`INSERT INTO workspace_legal_pages (workspace_id, page_type) VALUES ($1,'privacy')`, target); err != nil {
		t.Fatalf("seed legal page: %v", err)
	}
	if _, err := p.Exec(ctx,
		`INSERT INTO workspace_email_settings (workspace_id) VALUES ($1)`, target); err != nil {
		t.Fatalf("seed email settings: %v", err)
	}

	// The counts the trash must not disturb — measured on a workspace that now
	// holds something in EVERY category, which the old not_empty guard refused
	// outright.
	rows := []struct {
		name string
		sql  string
		args []any
	}{
		{"accounts", `SELECT COUNT(*) FROM accounts WHERE workspace_id=$1`, []any{target}},
		{"forms", `SELECT COUNT(*) FROM forms WHERE account_id=$1`, []any{acc}},
		{"products", `SELECT COUNT(*) FROM products WHERE account_id=$1`, []any{acc}},
		{"sales", `SELECT COUNT(*) FROM sales WHERE account_id=$1`, []any{acc}},
		{"api_keys", `SELECT COUNT(*) FROM api_keys WHERE workspace_id=$1`, []any{target}},
		{"workspace_settings", `SELECT COUNT(*) FROM workspace_settings WHERE workspace_id=$1`, []any{target}},
		{"workspace_invites", `SELECT COUNT(*) FROM workspace_invites WHERE workspace_id=$1`, []any{target}},
		{"workspace_legal_pages", `SELECT COUNT(*) FROM workspace_legal_pages WHERE workspace_id=$1`, []any{target}},
		{"workspace_email_settings", `SELECT COUNT(*) FROM workspace_email_settings WHERE workspace_id=$1`, []any{target}},
	}
	before := map[string]int{}
	for _, r := range rows {
		before[r.name] = countRows(t, ctx, p, r.sql, r.args...)
		if before[r.name] == 0 {
			t.Fatalf("fixture bug: %s was seeded but counts 0 before the trash", r.name)
		}
	}

	deleted, err := sp.Delete(ctx, target, user, "")
	if err != nil {
		t.Fatalf("Delete a workspace FULL of content: %v — the not_empty guard must be gone, "+
			"a full workspace is exactly what has to be binnable", err)
	}
	if deleted["workspaces"] != 1 {
		t.Errorf("deleted[workspaces] = %d, want 1", deleted["workspaces"])
	}

	for _, r := range rows {
		if got := countRows(t, ctx, p, r.sql, r.args...); got != before[r.name] {
			t.Errorf("%s rows after the trash = %d, want %d — the trash DESTROYED a row; "+
				"it must only mark the workspace", r.name, got, before[r.name])
		}
	}
	// And the mark is really there.
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM workspaces WHERE id=$1 AND deleted_at IS NOT NULL`, target); n != 1 {
		t.Error("the workspace is not marked after the trash")
	}
	_ = keep
}

// TestWorkspaceRestoreBringsEverythingBack is acceptance 2: restore clears the
// mark and the workspace returns to every live read with its account, its forms
// and its sales intact — nothing to rebuild, because nothing was removed.
//
// MUTATION EVIDENCE: drop the `deleted_at=NULL` from Restore and this fails on
// the live-read assertions (the row stays binned).
func TestWorkspaceRestoreBringsEverythingBack(t *testing.T) {
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
	form := &Form{AccountID: acc, Slug: "it-wsrestore-" + acc[:8], Title: "IT", Status: FormStatusDraft}
	if err := fs.Create(ctx, form); err != nil {
		t.Fatalf("seed form: %v", err)
	}
	ss := NewSaleStore(p)
	if err := ss.Create(ctx, &Sale{
		AccountID: acc, RefCode: "IT-WSRESTORE-" + acc[:8],
		Client: SaleClient{FullName: "A", Email: "a@example.test"},
		Items:  []SaleItem{}, Status: SalePaid, PaymentMethod: "chip",
		SaleType: SaleTypeOneTime,
	}); err != nil {
		t.Fatalf("seed sale: %v", err)
	}

	if _, err := sp.Delete(ctx, target, user, ""); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := sp.ByID(ctx, target); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("ByID while binned error = %v, want a miss", err)
	}

	if err := sp.Restore(ctx, target, user); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	got, err := sp.ByID(ctx, target)
	if err != nil {
		t.Fatalf("ByID after restore: %v", err)
	}
	if got.DeletedAt != nil {
		t.Errorf("restored workspace DeletedAt = %v, want nil", got.DeletedAt)
	}
	if got.Slug == "" {
		t.Error("the slug was lost across the restore")
	}
	// The content is back with it — because the trash never touched it.
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM forms WHERE id=$1 AND deleted_at IS NULL`, form.ID); n != 1 {
		t.Error("the form did not come back with the restored workspace")
	}
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM sales WHERE account_id=$1`, acc); n != 1 {
		t.Error("the sale did not come back with the restored workspace")
	}
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM accounts WHERE workspace_id=$1`, target); n != 1 {
		t.Error("the CHIP account did not come back with the restored workspace")
	}
	// And the trash list is empty again.
	if bin, err := sp.ListTrashed(ctx, user); err != nil {
		t.Fatalf("ListTrashed: %v", err)
	} else if len(bin) != 0 {
		t.Errorf("ListTrashed after restore = %+v, want empty", bin)
	}
	// Restoring something that is not in the bin is a miss, not a no-op success.
	if err := sp.Restore(ctx, target, user); !errors.Is(err, ErrWorkspaceNotFound) {
		t.Errorf("Restore of a LIVE workspace error = %v, want ErrWorkspaceNotFound", err)
	}
	// Neither is restoring a workspace you do not own.
	other := testOwnerUser(t, ctx, p)
	if err := sp.Restore(ctx, target, other); !errors.Is(err, ErrWorkspaceNotFound) {
		t.Errorf("Restore by a non-owner error = %v, want ErrWorkspaceNotFound", err)
	}
}

// TestWorkspaceTrashReleasesTheSlugAndRestoreRefusesReuse is acceptance 3 and the
// exact form pattern (TestFormRestoreRefusesWhenSlugWasRetaken): the partial
// unique index covers LIVE rows, so binning a workspace frees its name for
// reuse — and the original cannot then be restored over the taker.
//
// MUTATION EVIDENCE: make the index cover the whole table (drop
// `WHERE deleted_at IS NULL`) and the "taker" Create below fails, so the test
// reports the truncation of the trash promise rather than passing vacuously.
// Remove the isUniqueViolation branch in Restore and the final assertion gets a
// raw 23505 instead of ErrWorkspaceSlugTaken.
func TestWorkspaceTrashReleasesTheSlugAndRestoreRefusesReuse(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)
	user, ids := wsDeleteFixture(t, ctx, p, 2)
	target := ids[0]

	original, err := sp.ByID(ctx, target)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if _, err := sp.Delete(ctx, target, user, ""); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// The binned workspace released its slug: a LIVE workspace may take it.
	// This is what `workspaces_slug_live_key` (migration 000073) buys, and it is
	// the whole reason the trash is reversible AND cheap.
	taker, err := sp.Create(ctx, user, "Taker", original.Slug)
	if err != nil {
		t.Fatalf("a new workspace could not take the binned workspace's slug %q: %v — "+
			"the slug is held forever, so the name can never be reused", original.Slug, err)
	}
	if taker.Slug != original.Slug {
		t.Fatalf("taker slug = %q, want %q", taker.Slug, original.Slug)
	}

	// Restoring the original must now be REFUSED, with the named error, rather
	// than colliding with the live taker.
	if err := sp.Restore(ctx, target, user); !errors.Is(err, ErrWorkspaceSlugTaken) {
		t.Fatalf("Restore over a re-claimed slug error = %v, want ErrWorkspaceSlugTaken — "+
			"a restore must never silently collide with the workspace holding the name now", err)
	}
	// And the refusal must not have half-restored it.
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM workspaces WHERE id=$1 AND deleted_at IS NOT NULL`, target); n != 1 {
		t.Error("the refused restore brought the workspace back anyway")
	}
	// The taker is untouched and still live.
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM workspaces WHERE id=$1 AND deleted_at IS NULL`, taker.ID); n != 1 {
		t.Error("the refused restore disturbed the workspace holding the slug")
	}
}

// TestWorkspacePurgeIsTheHardDelete is acceptance 5: the purge really removes
// the rows, in the order the old delete used.
//
// The seeded REVOKED key is load-bearing, exactly as in the old cascade test: it
// is not a blocker (dead configuration), but its `workspace_id` FK is NO ACTION,
// so a naive `DELETE FROM workspaces` fails and only the ordered purge works.
func TestWorkspacePurgeIsTheHardDelete(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)
	user, ids := wsDeleteFixture(t, ctx, p, 2)
	target := ids[0]

	var acc string
	if err := p.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, target).Scan(&acc); err != nil {
		t.Fatalf("read workspace account: %v", err)
	}
	if _, err := p.Exec(ctx,
		`INSERT INTO api_keys (account_id, name, key_hash, scopes, workspace_id, revoked_at)
		 VALUES ($1,'it','it-purge-'||$3,'{admin}',$2, now())`, acc, target, acc[:8]); err != nil {
		t.Fatalf("seed revoked api key: %v", err)
	}
	if _, err := sp.Settings(ctx, target); err != nil {
		t.Fatalf("materialise settings: %v", err)
	}
	if _, err := p.Exec(ctx,
		`INSERT INTO workspace_invites (workspace_id, email, role, token) VALUES ($1,$2,'read_only','it-purge-inv-'||$3)`,
		target, "purge-"+acc[:8]+"@example.test", acc[:8]); err != nil {
		t.Fatalf("seed invite: %v", err)
	}
	if _, err := p.Exec(ctx,
		`INSERT INTO workspace_legal_pages (workspace_id, page_type) VALUES ($1,'privacy')`, target); err != nil {
		t.Fatalf("seed legal page: %v", err)
	}
	if _, err := p.Exec(ctx,
		`INSERT INTO workspace_email_settings (workspace_id) VALUES ($1)`, target); err != nil {
		t.Fatalf("seed email settings: %v", err)
	}

	// Purge a LIVE workspace → refused. Trash is the only way in.
	if _, err := sp.Purge(ctx, target, user); !errors.Is(err, ErrWorkspaceNotFound) {
		t.Fatalf("Purge of a LIVE workspace error = %v, want ErrWorkspaceNotFound — a purge "+
			"must only ever act on something already in the bin", err)
	}
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM workspaces WHERE id=$1`, target); n != 1 {
		t.Fatal("the refused purge destroyed the live workspace")
	}

	// Trash it, then purge it.
	if _, err := sp.Delete(ctx, target, user, ""); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	purged, err := sp.Purge(ctx, target, user)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if purged["workspaces"] != 1 {
		t.Errorf("purged[workspaces] = %d, want 1", purged["workspaces"])
	}

	for _, q := range []struct {
		table string
		sql   string
		args  []any
	}{
		{"workspaces", `SELECT COUNT(*) FROM workspaces WHERE id=$1`, []any{target}},
		{"accounts", `SELECT COUNT(*) FROM accounts WHERE workspace_id=$1`, []any{target}},
		{"api_keys", `SELECT COUNT(*) FROM api_keys WHERE workspace_id=$1`, []any{target}},
		{"forms", `SELECT COUNT(*) FROM forms WHERE account_id=$1`, []any{acc}},
		{"products", `SELECT COUNT(*) FROM products WHERE account_id=$1`, []any{acc}},
		{"workspace_settings", `SELECT COUNT(*) FROM workspace_settings WHERE workspace_id=$1`, []any{target}},
		{"workspace_invites", `SELECT COUNT(*) FROM workspace_invites WHERE workspace_id=$1`, []any{target}},
		{"workspace_legal_pages", `SELECT COUNT(*) FROM workspace_legal_pages WHERE workspace_id=$1`, []any{target}},
		{"workspace_email_settings", `SELECT COUNT(*) FROM workspace_email_settings WHERE workspace_id=$1`, []any{target}},
	} {
		if n := countRows(t, ctx, p, q.sql, q.args...); n != 0 {
			t.Errorf("%s still has %d row(s) after the purge — the ordered delete did not reach it", q.table, n)
		}
	}
}

// TestWorkspacePurgeRefusesAWorkspaceThatWasSold is the sales guard: a purge
// destroys sales rows outright, and sales are the merchant's record of money
// received. The refusal must leave every row in place.
func TestWorkspacePurgeRefusesAWorkspaceThatWasSold(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)
	user, ids := wsDeleteFixture(t, ctx, p, 2)
	target := ids[0]

	var acc string
	if err := p.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, target).Scan(&acc); err != nil {
		t.Fatalf("read workspace account: %v", err)
	}
	ss := NewSaleStore(p)
	if err := ss.Create(ctx, &Sale{
		AccountID: acc, RefCode: "IT-WSPURGE-" + acc[:8],
		Client: SaleClient{FullName: "A", Email: "a@example.test"},
		Items:  []SaleItem{}, Status: SalePaid, PaymentMethod: "chip",
		SaleType: SaleTypeOneTime,
	}); err != nil {
		t.Fatalf("seed sale: %v", err)
	}
	if _, err := sp.Delete(ctx, target, user, ""); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := sp.Purge(ctx, target, user); !errors.Is(err, ErrWorkspaceHasSales) {
		t.Fatalf("Purge of a sold workspace error = %v, want ErrWorkspaceHasSales — a purge "+
			"destroys sales, and sales are the record of money received", err)
	}
	// Nothing may have been destroyed by the refusal.
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM workspaces WHERE id=$1`, target); n != 1 {
		t.Error("the refused purge destroyed the workspace anyway")
	}
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM sales WHERE account_id=$1`, acc); n != 1 {
		t.Error("the refused purge destroyed the sale anyway")
	}
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM accounts WHERE workspace_id=$1`, target); n != 1 {
		t.Error("the refused purge destroyed the CHIP account anyway")
	}
	// Purge by someone who does not own it is refused too.
	other := testOwnerUser(t, ctx, p)
	if _, err := sp.Purge(ctx, target, other); !errors.Is(err, &WorkspaceDeleteError{}) {
		if err == nil {
			t.Error("a non-owner purged someone else's workspace")
		}
	}
}

// TestWorkspaceTrashClearsDefaultWorkspace pins the side effect the FK used to
// provide for free (acceptance in the card's cascade note):
// users.default_workspace_id pointing at the binned workspace becomes NULL, so
// the next login cannot land on a workspace nobody can see.
//
// MUTATION EVIDENCE: delete the `UPDATE users SET default_workspace_id=NULL`
// statement in Trash and this fails — the default survives, because a MARK fires
// no ON DELETE SET NULL.
func TestWorkspaceTrashClearsDefaultWorkspace(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)
	user, ids := wsDeleteFixture(t, ctx, p, 2)
	target := ids[0]

	if err := NewUserStore(p).SetDefaultWorkspace(ctx, user, target); err != nil {
		t.Fatalf("set default workspace: %v", err)
	}
	if _, err := sp.Delete(ctx, target, user, ""); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var def *string
	if err := p.QueryRow(ctx, `SELECT default_workspace_id FROM users WHERE id=$1`, user).Scan(&def); err != nil {
		t.Fatalf("read default_workspace_id: %v", err)
	}
	if def != nil {
		t.Errorf("default_workspace_id = %q after binning that workspace, want NULL — a dangling "+
			"default lands the next login on a workspace that is in the bin", *def)
	}
}

// TestWorkspaceDeleteUnknownIDIsNotFound keeps the refusal taxonomy honest: an
// unknown id is not silently a no-op success.
func TestWorkspaceDeleteUnknownIDIsNotFound(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)
	user, _ := wsDeleteFixture(t, ctx, p, 1)

	_, err := sp.Delete(ctx, "00000000-0000-0000-0000-000000000000", user, "")
	if err != ErrWorkspaceNotFound {
		t.Fatalf("Delete(unknown id) error = %v, want ErrWorkspaceNotFound", err)
	}
	// Binning an ALREADY binned workspace is also a miss: trashing twice is not
	// two operations and must not move deleted_at forward.
	_, ids := wsDeleteFixture(t, ctx, p, 2)
	if _, err := sp.Delete(ctx, ids[0], user, ""); err != nil {
		t.Fatalf("first Delete: %v", err)
	}
	// A different owner with two workspaces, so the last_workspace guard cannot
	// be the thing that answers.
	other := testOwnerUser(t, ctx, p)
	_, oids := wsDeleteFixture(t, ctx, p, 2)
	_ = other
	if _, err := sp.Delete(ctx, ids[0], user, ""); !errors.Is(err, ErrWorkspaceNotFound) {
		t.Errorf("second Delete of the same workspace error = %v, want ErrWorkspaceNotFound", err)
	}
	_ = oids
}

// TestWorkspaceTrashedByIDNotFoundIsTheStoreSentinel is the store half of card
// t_a71ee62b. TrashedByID is the read the purge route's confirmation compares
// against, and its miss must arrive as the STORE's sentinel
// (ErrWorkspaceNotFound) — not as the raw driver error.
//
// WHY THIS IS A DEFECT AND NOT A STYLE CHOICE. scanTrashedWorkspace maps a
// missing row and SQLSTATE 22P02 to pgx.ErrNoRows, and pgx.ErrNoRows is not
// ErrWorkspaceNotFound. The REST handler tests only
// errors.Is(err, ErrWorkspaceNotFound), so the raw shape fell through to
// respondErr's default branch and every not-found purge — including a REAL,
// live workspace id — answered 500 {"code":"internal"} with
// error_class=internal and a stack trace, on live v1.19.1. The sibling
// accessors in this file already answer the sentinel: Restore refuses with
// ErrWorkspaceNotFound via RowsAffected==0, Rename/Trash/Purge map isNoSuchRow
// to it. TrashedByID was the one member of the family that did not.
//
// BOTH MISS SHAPES ARE ASSERTED because they are two different code paths out
// of Postgres — a uuid that names no row (ErrNoRows) and an id that cannot be a
// uuid at all (22P02, raised before any row is considered). A fix that mapped
// only the first would leave the non-uuid 500 in place, which is one of the
// three shapes the card measured.
//
// MUTATION EVIDENCE: restore `return nil, pgx.ErrNoRows` in scanTrashedWorkspace
// and this fails on all four assertions (and the REST guard beside it goes red
// too).
func TestWorkspaceTrashedByIDNotFoundIsTheStoreSentinel(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)
	_, ids := wsDeleteFixture(t, ctx, p, 2)

	// A LIVE workspace is a miss to the trash read — that is what makes
	// trash → purge the only way in, and it is the shape the card measured on
	// a real workspace id.
	live := ids[1]
	if _, err := sp.TrashedByID(ctx, live); !errors.Is(err, ErrWorkspaceNotFound) {
		t.Errorf("TrashedByID(live) err = %v, want ErrWorkspaceNotFound — a live id is a "+
			"not-found here, and the raw pgx.ErrNoRows reaching the handler is exactly the "+
			"500 this card is about", err)
	}
	// An unknown uuid.
	if _, err := sp.TrashedByID(ctx, "00000000-0000-0000-0000-0000000000ff"); !errors.Is(err, ErrWorkspaceNotFound) {
		t.Errorf("TrashedByID(unknown uuid) err = %v, want ErrWorkspaceNotFound", err)
	}
	// A non-uuid: Postgres raises SQLSTATE 22P02 before any row is considered,
	// and it must be the same answer to the caller.
	if _, err := sp.TrashedByID(ctx, "not-a-uuid"); !errors.Is(err, ErrWorkspaceNotFound) {
		t.Errorf("TrashedByID(non-uuid) err = %v, want ErrWorkspaceNotFound — an id that "+
			"cannot be a uuid is an untrusted-input miss, not a server fault", err)
	}
	// And the sentinel must not have been bought by turning every read into a
	// miss: a TRASHED workspace still reads back.
	if _, err := sp.Delete(ctx, ids[0], mustOwnerOf(t, ctx, p, ids[0]), ""); err != nil {
		t.Fatalf("bin the workspace: %v", err)
	}
	got, err := sp.TrashedByID(ctx, ids[0])
	if err != nil {
		t.Fatalf("TrashedByID(trashed) err = %v, want the binned row", err)
	}
	if got == nil || got.ID != ids[0] {
		t.Errorf("TrashedByID(trashed) = %+v, want the binned workspace %s", got, ids[0])
	}

	// Restore shares the same class (the card asked for it explicitly): its
	// guard is `RowsAffected()==0`, but a non-uuid raises SQLSTATE 22P02 out of
	// Exec BEFORE that, so Restore(not-a-uuid) returned the raw driver error and
	// the route answered 500 where the purge route now answers 404. Both misses
	// must be the one sentinel.
	for _, id := range []string{"not-a-uuid", "00000000-0000-0000-0000-0000000000ff"} {
		if err := sp.Restore(ctx, id, mustOwnerOf(t, ctx, p, ids[1])); !errors.Is(err, ErrWorkspaceNotFound) {
			t.Errorf("Restore(%s) err = %v, want ErrWorkspaceNotFound — a non-uuid is the same "+
				"untrusted-input miss as an unknown uuid, and the raw driver error reaching "+
				"respondErr is a 500 on an id-taking route", id, err)
		}
	}
	// The live row must still be live: a refused restore must never have
	// half-applied (the SQLSTATE fires before any row is considered).
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM workspaces WHERE id=$1 AND deleted_at IS NULL`, ids[1]); n != 1 {
		t.Error("a refused Restore disturbed the live workspace")
	}
}

// mustOwnerOf returns the owner user id of a workspace, so a test can bin it
// without re-deriving the fixture's user.
func mustOwnerOf(t *testing.T, ctx context.Context, p *pgxpool.Pool, workspaceID string) string {
	t.Helper()
	var owner string
	if err := p.QueryRow(ctx, `SELECT owner_user_id FROM workspaces WHERE id=$1`, workspaceID).Scan(&owner); err != nil {
		t.Fatalf("read owner of %s: %v", workspaceID, err)
	}
	return owner
}

// TestWorkspaceContentCountsSurvivesTheGuardRemoval is the anti-drift guard on
// the CENSUS (the survivors of the old emptiness definition): the READ half the
// UI uses still has to agree with the rows on disk, even though nothing refuses
// on it any more.
func TestWorkspaceContentCountsSurvivesTheGuardRemoval(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)
	_, ids := wsDeleteFixture(t, ctx, p, 2)
	target := ids[0]

	var acc string
	if err := p.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, target).Scan(&acc); err != nil {
		t.Fatalf("read account: %v", err)
	}
	counts, err := sp.ContentCounts(ctx, target, "")
	if err != nil {
		t.Fatalf("ContentCounts: %v", err)
	}
	if counts["accounts"] != 1 {
		t.Errorf("ContentCounts[accounts] = %d, want 1", counts["accounts"])
	}
	if len(counts) != 1 {
		t.Errorf("ContentCounts = %+v, want only the accounts entry on a fresh workspace", counts)
	}

	// A product shows up in the census as its own category.
	if err := NewProductStore(p).Create(ctx, &Product{
		AccountID: acc, Name: "IT", PriceSen: 100, Type: "one_time",
	}); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	counts, err = sp.ContentCounts(ctx, target, "")
	if err != nil {
		t.Fatalf("ContentCounts after seeding: %v", err)
	}
	if counts["products"] != 1 {
		t.Errorf("ContentCounts[products] = %d, want 1 (%+v)", counts["products"], counts)
	}
	// The census must NOT be a refusal any more: a workspace with content bins.
	// wsDeleteFixture returns the owner it created, so use a fresh fixture pair
	// rather than re-deriving it.
	owner, oids := wsDeleteFixture(t, ctx, p, 2)
	var oacc string
	if err := p.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, oids[0]).Scan(&oacc); err != nil {
		t.Fatalf("read account: %v", err)
	}
	if err := NewProductStore(p).Create(ctx, &Product{
		AccountID: oacc, Name: "IT", PriceSen: 100, Type: "one_time",
	}); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	if _, err := sp.Delete(ctx, oids[0], owner, ""); err != nil {
		t.Fatalf("a workspace with content could not be binned: %v", err)
	}
}
