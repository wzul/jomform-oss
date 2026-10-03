package dbstore

// Integration test for the unverified-account purge (issue #190):
// DeleteUnverifiedOlderThan removes only unverified users older than the
// cutoff, cleans up their auto-provisioned account/workspace, and never touches
// verified accounts. Skipped unless JOMFORM_TEST_DATABASE_URL is set;
// migrations are applied first, mirroring the production boot path.
//
// Transaction scope (the sweep's failure mode): the unit of atomicity is one
// USER. One candidate that can never be deleted — an unverified user whose
// account holds sales, which the guards must leave alone — used to abort the
// shared transaction and roll back EVERY other candidate's deletion, silently
// turning the whole sweep into a no-op. The fixtures below therefore place a
// blocked user in the SAME sweep as a deletable one: the deletable one going is
// what a rollback-everything regression breaks.

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// createUnverifiedUser inserts a user with verified=false and an
// auto-provisioned account (mirroring handleRegister), returning the user id.
func createUnverifiedUser(t *testing.T, ctx context.Context, p *pgxpool.Pool, email string) string {
	t.Helper()
	var uid string
	if err := p.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, verified) VALUES ($1, 'it', false) RETURNING id`,
		email).Scan(&uid); err != nil {
		t.Fatalf("create unverified user: %v", err)
	}
	if _, err := p.Exec(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug) VALUES ($1, 'it-store', 'it-' || md5(random()::text))`,
		uid); err != nil {
		t.Fatalf("create auto-provisioned account: %v", err)
	}
	return uid
}

// md5Rand returns a random hex token for unique test emails.
func md5Rand() string {
	return newUUIDString()
}

// cleanupUsers removes the given users and their accounts (RESTRICT FK on
// accounts.owner_user_id requires deleting accounts first).
func cleanupUsers(t *testing.T, ctx context.Context, p *pgxpool.Pool, ids ...string) {
	t.Helper()
	if len(ids) == 0 {
		return
	}
	if _, err := p.Exec(ctx, `DELETE FROM accounts WHERE owner_user_id = ANY($1::uuid[])`, ids); err != nil {
		t.Fatalf("cleanup accounts: %v", err)
	}
	if _, err := p.Exec(ctx, `DELETE FROM users WHERE id = ANY($1::uuid[])`, ids); err != nil {
		t.Fatalf("cleanup users: %v", err)
	}
}

// cleanupUsersWithSales removes fixtures that own sales. sales.account_id is ON
// DELETE RESTRICT and accounts.owner_user_id is ON DELETE RESTRICT, so the
// deletes must run in dependency order: sales → accounts → users.
func cleanupUsersWithSales(t *testing.T, ctx context.Context, p *pgxpool.Pool, ids ...string) {
	t.Helper()
	if len(ids) == 0 {
		return
	}
	if _, err := p.Exec(ctx,
		`DELETE FROM sales WHERE account_id IN (SELECT id FROM accounts WHERE owner_user_id = ANY($1::uuid[]))`,
		ids); err != nil {
		t.Fatalf("cleanup sales: %v", err)
	}
	cleanupUsers(t, ctx, p, ids...)
}

// purgeFixture is an unverified user the sweep can NEVER delete, shaped like the
// production row that wedged the sweep: it owns one account with a sale (skipped
// by the sweep's RESTRICT guard, so accounts.owner_user_id keeps the user alive),
// a second account with no sale at all (deletable on its own), and a workspace
// with no linked account. The sale-free account and the workspace are deleted by
// earlier statements of the same per-user transaction, so their survival after a
// sweep is direct evidence that the transaction rolled back as a unit.
type purgeFixture struct {
	userID       string
	salesAccount string
	freeAccount  string
	workspaceID  string
}

func createBlockedUserWithSale(t *testing.T, ctx context.Context, p *pgxpool.Pool, email string) purgeFixture {
	t.Helper()
	var f purgeFixture
	if err := p.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, verified) VALUES ($1, 'it', false) RETURNING id`,
		email).Scan(&f.userID); err != nil {
		t.Fatalf("create blocked user: %v", err)
	}
	// Account holding a sale → the sweep's sales guard skips it, and
	// accounts.owner_user_id (ON DELETE RESTRICT) then rejects the users delete.
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1, 'it-blocked', 'it-' || md5(random()::text)) RETURNING id`,
		f.userID).Scan(&f.salesAccount); err != nil {
		t.Fatalf("create sales account: %v", err)
	}
	if _, err := p.Exec(ctx,
		`INSERT INTO sales (account_id, ref_code, client, items, subtotal_sen, total_sen)
		 VALUES ($1, 'it-' || md5(random()::text), '{}'::jsonb, '[]'::jsonb, 100, 100)`,
		f.salesAccount); err != nil {
		t.Fatalf("create sale: %v", err)
	}
	// Second, sale-free account (nothing but the blocked user's transaction keeps it).
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1, 'it-free', 'it-' || md5(random()::text)) RETURNING id`,
		f.userID).Scan(&f.freeAccount); err != nil {
		t.Fatalf("create sale-free account: %v", err)
	}
	// Workspace owned by the user with no account linked to it.
	if err := p.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-ws', 'it-' || md5(random()::text)) RETURNING id`,
		f.userID).Scan(&f.workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	// Old enough to be a candidate.
	if _, err := p.Exec(ctx,
		`UPDATE users SET created_at = now() - interval '10 days' WHERE id = $1`, f.userID); err != nil {
		t.Fatalf("backdate blocked user: %v", err)
	}
	return f
}

// countRows returns the number of rows the query returns.
func countRows(t *testing.T, ctx context.Context, p *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := p.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("count (%s): %v", sql, err)
	}
	return n
}

func TestDeleteUnverifiedOlderThan(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	store := NewUserStore(p)

	// Old unverified user (created_at backdated past the cutoff).
	oldUnverified := createUnverifiedUser(t, ctx, p, "old-unverified-"+md5Rand()+"@example.test")
	if _, err := p.Exec(ctx,
		`UPDATE users SET created_at = now() - interval '10 days' WHERE id = $1`, oldUnverified); err != nil {
		t.Fatalf("backdate old unverified: %v", err)
	}

	// Recent unverified user (created_at now — must NOT be purged).
	recentUnverified := createUnverifiedUser(t, ctx, p, "recent-unverified-"+md5Rand()+"@example.test")

	// Old verified user (must NEVER be touched).
	oldVerified := testOwnerUser(t, ctx, p)
	if _, err := p.Exec(ctx,
		`UPDATE users SET verified = true, created_at = now() - interval '10 days' WHERE id = $1`, oldVerified); err != nil {
		t.Fatalf("set old verified: %v", err)
	}
	defer cleanupUsers(t, ctx, p, oldUnverified, recentUnverified, oldVerified)

	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	if _, err := store.DeleteUnverifiedOlderThan(ctx, cutoff); err != nil {
		t.Fatalf("DeleteUnverifiedOlderThan: %v", err)
	}

	// Old unverified user + its auto-provisioned account are gone.
	var cnt int
	if err := p.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = $1`, oldUnverified).Scan(&cnt); err != nil {
		t.Fatalf("count old unverified: %v", err)
	}
	if cnt != 0 {
		t.Errorf("old unverified user still present (count=%d), want 0", cnt)
	}
	if err := p.QueryRow(ctx,
		`SELECT count(*) FROM accounts WHERE owner_user_id = $1`, oldUnverified).Scan(&cnt); err != nil {
		t.Fatalf("count orphan account: %v", err)
	}
	if cnt != 0 {
		t.Errorf("auto-provisioned account for purged user still present (count=%d), want 0", cnt)
	}

	// Recent unverified user survives (not yet past cutoff).
	if err := p.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = $1`, recentUnverified).Scan(&cnt); err != nil {
		t.Fatalf("count recent unverified: %v", err)
	}
	if cnt != 1 {
		t.Errorf("recent unverified user purged (count=%d), want 1", cnt)
	}

	// Verified user survives.
	if err := p.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = $1`, oldVerified).Scan(&cnt); err != nil {
		t.Fatalf("count verified: %v", err)
	}
	if cnt != 1 {
		t.Errorf("verified user purged (count=%d), want 1", cnt)
	}
}

// TestDeleteUnverifiedOlderThanSkipsBlockedUser is the guard for the sweep's
// real failure mode: a candidate that can never be deleted must cost only
// ITSELF, not the whole batch.
//
// The deletable candidate is asserted to be GONE. Under a shared transaction
// that assertion is RED: the blocked user's RESTRICT violation rolls the shared
// transaction back, the function returns an error, and the deletable user
// survives — which is how one wedged row disabled the purge for everybody.
func TestDeleteUnverifiedOlderThanSkipsBlockedUser(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	store := NewUserStore(p)

	// Drain candidates left behind by another test in this package (e.g. a
	// backdated unverified user) so the counts below describe this sweep's own
	// fixtures exactly.
	if _, err := store.DeleteUnverifiedOlderThan(ctx, time.Now()); err != nil {
		t.Fatalf("pre-sweep drain: %v", err)
	}

	blocked := createBlockedUserWithSale(t, ctx, p, "blocked-"+md5Rand()+"@example.test")
	defer cleanupUsersWithSales(t, ctx, p, blocked.userID)

	deletable := createUnverifiedUser(t, ctx, p, "deletable-"+md5Rand()+"@example.test")
	if _, err := p.Exec(ctx,
		`UPDATE users SET created_at = now() - interval '10 days' WHERE id = $1`, deletable); err != nil {
		t.Fatalf("backdate deletable user: %v", err)
	}

	recent := createUnverifiedUser(t, ctx, p, "recent-"+md5Rand()+"@example.test")

	verified := testOwnerUser(t, ctx, p)
	if _, err := p.Exec(ctx,
		`UPDATE users SET verified = true, created_at = now() - interval '10 days' WHERE id = $1`, verified); err != nil {
		t.Fatalf("set verified user: %v", err)
	}
	defer cleanupUsers(t, ctx, p, deletable, recent, verified)

	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	res, err := store.DeleteUnverifiedOlderThan(ctx, cutoff)
	if err != nil {
		// Reported, not fatal: under a shared transaction the sweep returns the
		// blocked user's RESTRICT error — and the assertions below then show what
		// that error cost, which is the point of this test.
		t.Errorf("DeleteUnverifiedOlderThan: %v", err)
	}

	// The deletable candidate is deleted — this is the assertion a
	// rollback-everything regression breaks.
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, deletable); n != 0 {
		t.Errorf("the deletable candidate survived (count=%d, want 0): a blocked candidate in the "+
			"same sweep must not roll back the users it can delete", n)
	}
	if n := countRows(t, ctx, p,
		`SELECT count(*) FROM accounts WHERE owner_user_id = $1`, deletable); n != 0 {
		t.Errorf("the deletable candidate's account survived (count=%d, want 0)", n)
	}

	// The blocked candidate survives, with everything it owns — the sweep leaves
	// protected data alone rather than forcing the delete.
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, blocked.userID); n != 1 {
		t.Errorf("the blocked user was deleted (count=%d, want 1): its account still holds a sale, "+
			"so the RESTRICT FK must keep the user instead of cascading the loss", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM accounts WHERE id = $1`, blocked.salesAccount); n != 1 {
		t.Errorf("the sales-holding account was deleted (count=%d, want 1)", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM sales WHERE account_id = $1`, blocked.salesAccount); n != 1 {
		t.Errorf("the sale was deleted (count=%d, want 1): merchant data must never be removed by the purge", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM accounts WHERE id = $1`, blocked.freeAccount); n != 1 {
		t.Errorf("the blocked user's sale-free account was deleted (count=%d, want 1): the blocked "+
			"user's whole transaction must roll back, leaving the user exactly as it was", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM workspaces WHERE id = $1`, blocked.workspaceID); n != 1 {
		t.Errorf("the blocked user's workspace was deleted (count=%d, want 1): the earlier deletes of "+
			"that user's transaction must have been rolled back with it", n)
	}

	// Neither of the untouched-by-policy candidates is affected.
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, recent); n != 1 {
		t.Errorf("recent unverified user purged (count=%d, want 1)", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, verified); n != 1 {
		t.Errorf("verified user purged (count=%d, want 1)", n)
	}

	// The sweep reports both outcomes, so "deleted nothing" and "blocked by
	// protected data" are distinguishable from the log alone.
	if res.Deleted != 1 {
		t.Errorf("res.Deleted = %d, want 1 (exactly the deletable candidate)", res.Deleted)
	}
	if res.Blocked != 1 {
		t.Errorf("res.Blocked = %d, want 1 (exactly the sale-holding candidate)", res.Blocked)
	}
}

// TestPurgeUserSkipsUserVerifiedMidSweep pins the fix for card t_6577205b.
//
// The candidate list is read OUTSIDE the per-user transaction, and only the last
// of that transaction's three deletes carried a `verified = false` predicate. A
// user who confirmed their email after the sweep listed them but before the
// commit therefore had the two earlier deletes run and COMMIT while the users
// delete was refused: they kept their row and LOST their workspace and
// auto-provisioned account, with `(false, nil)` returned — no error, no Blocked
// count, no log line. Measured on the PR-748 tree: `users=1 accounts=0
// workspaces=0`.
//
// Expected: such a user is left EXACTLY as they were — user row, account(s) and
// workspace(s). The control below proves an ordinary unverified candidate still
// loses all three.
func TestPurgeUserSkipsUserVerifiedMidSweep(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	store := NewUserStore(p)

	uid := createUnverifiedUser(t, ctx, p, "qa-race-"+md5Rand()+"@example.test")
	var wsID string
	if err := p.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-race-ws', 'it-' || md5(random()::text)) RETURNING id`, uid).Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	defer cleanupUsers(t, ctx, p, uid)

	// The user confirms their email after the sweep listed them.
	if _, err := p.Exec(ctx, `UPDATE users SET verified = true WHERE id = $1`, uid); err != nil {
		t.Fatalf("verify: %v", err)
	}

	deleted, err := store.purgeUser(ctx, uid)
	if err != nil {
		t.Fatalf("purgeUser on a verified candidate: %v", err)
	}
	if deleted {
		t.Errorf("purgeUser reported deleted=true for a user who is verified — the sweep must never " +
			"remove a confirmed account")
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, uid); n != 1 {
		t.Errorf("the verified user's row was deleted (count=%d, want 1)", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM accounts WHERE owner_user_id = $1`, uid); n != 1 {
		t.Errorf("the verified user LOST their account (count=%d, want 1) — the deletes before the "+
			"users guard must not commit when the user turned out to be verified", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM workspaces WHERE id = $1`, wsID); n != 1 {
		t.Errorf("the verified user LOST their workspace (count=%d, want 1) — a user verified at "+
			"commit time must be left exactly as they were", n)
	}
}

// TestPurgeUserRemovesOrdinaryUnverifiedCandidate is the control for the test
// above: the fix must not have made the sweep timid.
func TestPurgeUserRemovesOrdinaryUnverifiedCandidate(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	store := NewUserStore(p)

	uid := createUnverifiedUser(t, ctx, p, "qa-plain-"+md5Rand()+"@example.test")
	var wsID string
	if err := p.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-plain-ws', 'it-' || md5(random()::text)) RETURNING id`, uid).Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	defer cleanupUsers(t, ctx, p, uid)

	deleted, err := store.purgeUser(ctx, uid)
	if err != nil {
		t.Fatalf("purgeUser: %v", err)
	}
	if !deleted {
		t.Fatalf("purgeUser reported deleted=false for an ordinary unverified candidate — the " +
			"in-transaction verified check must not refuse a genuine candidate")
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, uid); n != 0 {
		t.Errorf("the unverified user's row survived (count=%d, want 0)", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM accounts WHERE owner_user_id = $1`, uid); n != 0 {
		t.Errorf("the unverified user's account survived (count=%d, want 0)", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM workspaces WHERE id = $1`, wsID); n != 0 {
		t.Errorf("the unverified user's workspace survived (count=%d, want 0)", n)
	}
}

// TestCountUnverifiedOlderThanClassified pins the fix for card t_b8446b61.
//
// `CountUnverifiedOlderThan` counts EVERY older-than-cutoff unverified row,
// including the ones the sweep can only answer with Blocked — so the tool's
// `count` and the sweep's outcome disagree with no way for the caller to see
// why. On the production backlog that was `count: 1` for a user whose account
// holds 36 sales: it reads as "the sweep will clear this", while the truth is
// that this candidate is exactly what wedged the sweep.
//
// The classified read must therefore (a) reproduce the sweep's own verdict for
// the SAME fixtures, and (b) commit nothing.
func TestCountUnverifiedOlderThanClassified(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	store := NewUserStore(p)

	// Drain candidates left behind by another test in this package so the counts
	// below describe this test's own fixtures exactly.
	if _, err := store.DeleteUnverifiedOlderThan(ctx, time.Now()); err != nil {
		t.Fatalf("pre-sweep drain: %v", err)
	}

	blocked := createBlockedUserWithSale(t, ctx, p, "cls-blocked-"+md5Rand()+"@example.test")
	defer cleanupUsersWithSales(t, ctx, p, blocked.userID)

	deletable := createUnverifiedUser(t, ctx, p, "cls-deletable-"+md5Rand()+"@example.test")
	if _, err := p.Exec(ctx,
		`UPDATE users SET created_at = now() - interval '10 days' WHERE id = $1`, deletable); err != nil {
		t.Fatalf("backdate deletable user: %v", err)
	}
	defer cleanupUsers(t, ctx, p, deletable)

	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	res, err := store.CountUnverifiedOlderThanClassified(ctx, cutoff)
	if err != nil {
		t.Fatalf("CountUnverifiedOlderThanClassified: %v", err)
	}

	// The classification is the sweep's: one purgable, one blocked.
	if res.Deleted != 1 {
		t.Errorf("res.Deleted = %d, want 1 — the classified read must say how many candidates the "+
			"sweep CAN purge; if this is 0 the classification has stopped agreeing with the sweep",
			res.Deleted)
	}
	if res.Blocked != 1 {
		t.Errorf("res.Blocked = %d, want 1 — the sale-holding candidate is un-purgeable and must be "+
			"reported as such rather than folded into a bare count", res.Blocked)
	}

	// AND NOTHING WAS DELETED. This is the property that makes the tool's
	// read-only contract true: the classification runs the sweep's own delete
	// inside a transaction that is always rolled back.
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, deletable); n != 1 {
		t.Errorf("the deletable candidate was DELETED by a read (count=%d, want 1) — the classified "+
			"read must roll back, or the read-only tool becomes a purge", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, blocked.userID); n != 1 {
		t.Errorf("the blocked candidate was deleted (count=%d, want 1)", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM sales WHERE account_id = $1`, blocked.salesAccount); n != 1 {
		t.Errorf("the sale was deleted by a read (count=%d, want 1)", n)
	}
	// The blocked user's sale-free account and workspace must still be there too:
	// its transaction rolled back in full, exactly as the sweep's does.
	if n := countRows(t, ctx, p, `SELECT count(*) FROM accounts WHERE owner_user_id = $1`, deletable); n != 1 {
		t.Errorf("the deletable candidate's account was deleted by a read (count=%d, want 1)", n)
	}

	// The plain count still answers the backlog size, which is what it is for —
	// and it is LARGER than the purgable number, which is the whole point.
	plain, err := store.CountUnverifiedOlderThan(ctx, cutoff)
	if err != nil {
		t.Fatalf("CountUnverifiedOlderThan: %v", err)
	}
	if plain != res.Deleted+res.Blocked {
		t.Errorf("CountUnverifiedOlderThan = %d but the classified read found %d purgable + %d "+
			"blocked = %d — the two reads disagree about the same backlog, so one of them is "+
			"counting a different set", plain, res.Deleted, res.Blocked, res.Deleted+res.Blocked)
	}
}

// TestDeleteUnverifiedOlderThanBlockedOnlySweep pins the other half of the
// report: a sweep whose every candidate is blocked deletes nothing, succeeds,
// and says so — instead of surfacing an error (or nothing at all) the way the
// shared transaction did.
func TestDeleteUnverifiedOlderThanBlockedOnlySweep(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	store := NewUserStore(p)

	if _, err := store.DeleteUnverifiedOlderThan(ctx, time.Now()); err != nil {
		t.Fatalf("pre-sweep drain: %v", err)
	}

	blocked := createBlockedUserWithSale(t, ctx, p, "blocked-only-"+md5Rand()+"@example.test")
	defer cleanupUsersWithSales(t, ctx, p, blocked.userID)

	res, err := store.DeleteUnverifiedOlderThan(ctx, time.Now().Add(-7*24*time.Hour))
	if err != nil {
		t.Fatalf("DeleteUnverifiedOlderThan: %v", err)
	}
	if res.Deleted != 0 {
		t.Errorf("res.Deleted = %d, want 0", res.Deleted)
	}
	if res.Blocked != 1 {
		t.Errorf("res.Blocked = %d, want 1", res.Blocked)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, blocked.userID); n != 1 {
		t.Errorf("the blocked user was deleted (count=%d, want 1)", n)
	}
}

func TestCountUnverifiedOlderThan(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	store := NewUserStore(p)

	old := createUnverifiedUser(t, ctx, p, "count-old-"+md5Rand()+"@example.test")
	if _, err := p.Exec(ctx,
		`UPDATE users SET created_at = now() - interval '10 days' WHERE id = $1`, old); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	recent := createUnverifiedUser(t, ctx, p, "count-recent-"+md5Rand()+"@example.test")
	defer cleanupUsers(t, ctx, p, old, recent)

	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	n, err := store.CountUnverifiedOlderThan(ctx, cutoff)
	if err != nil {
		t.Fatalf("CountUnverifiedOlderThan: %v", err)
	}
	if n < 1 {
		t.Errorf("count = %d, want >= 1 (the backdated unverified user)", n)
	}
}

// TestPurgeUserWaitsForAConcurrentVerify proves the FOR UPDATE lock by forcing
// the exact interleaving the card measured, deterministically:
//
//  1. tx1 takes the users row and sets `verified = true` WITHOUT committing, so
//     the verify exists but is not yet visible;
//  2. `purgeUser` runs — the sweep's per-user transaction;
//  3. tx1 commits;
//  4. the user must be left EXACTLY as they were.
//
// With the lock and the in-transaction re-read, purgeUser waits at its
// `SELECT … FOR UPDATE`, then sees the committed `verified = true` and returns
// before deleting anything. Without the lock it reads `verified = false` while
// tx1 is still open, its workspace and account deletes run and commit, and only
// the users delete is refused by the backstop — the silent data loss of
// t_6577205b (`users=1 accounts=0 workspaces=0`).
//
// This test therefore fails if EITHER half of the fix is removed, which is why
// it is worth the sequencing.
func TestPurgeUserWaitsForAConcurrentVerify(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	store := NewUserStore(p)

	uid := createUnverifiedUser(t, ctx, p, "qa-lock-"+md5Rand()+"@example.test")
	var wsID string
	if err := p.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-lock-ws', 'it-' || md5(random()::text)) RETURNING id`, uid).Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	defer cleanupUsers(t, ctx, p, uid)

	tx1, err := p.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx1: %v", err)
	}
	defer tx1.Rollback(ctx)
	var locked bool
	if err := tx1.QueryRow(ctx,
		`SELECT verified = false FROM users WHERE id = $1 FOR UPDATE`, uid).Scan(&locked); err != nil {
		t.Fatalf("lock users row: %v", err)
	}
	if _, err := tx1.Exec(ctx, `UPDATE users SET verified = true WHERE id = $1`, uid); err != nil {
		t.Fatalf("verify inside tx1: %v", err)
	}

	type result struct {
		deleted bool
		err     error
	}
	done := make(chan result, 1)
	go func() {
		deleted, err := store.purgeUser(context.Background(), uid)
		done <- result{deleted, err}
	}()

	// purgeUser must be BLOCKED on the row the sweep's own lock covers, so it
	// cannot have finished while tx1 is still open.
	select {
	case r := <-done:
		t.Fatalf("purgeUser finished while the verify transaction still held the users row "+
			"(deleted=%v, err=%v) — it read `verified` from a state the verify had already "+
			"superseded, which is exactly the window t_6577205b is about", r.deleted, r.err)
	case <-time.After(500 * time.Millisecond):
	}

	if err := tx1.Commit(ctx); err != nil {
		t.Fatalf("commit tx1: %v", err)
	}

	r := <-done
	if r.err != nil {
		t.Fatalf("purgeUser: %v", r.err)
	}
	if r.deleted {
		t.Errorf("purgeUser reported deleted=true for a user verified at commit time")
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM users WHERE id = $1`, uid); n != 1 {
		t.Errorf("the now-verified user's row was deleted (count=%d, want 1)", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM accounts WHERE owner_user_id = $1`, uid); n != 1 {
		t.Errorf("the now-verified user LOST their account (count=%d, want 1) — the deletes that "+
			"ran before the users guard must not commit", n)
	}
	if n := countRows(t, ctx, p, `SELECT count(*) FROM workspaces WHERE id = $1`, wsID); n != 1 {
		t.Errorf("the now-verified user LOST their workspace (count=%d, want 1)", n)
	}
}
