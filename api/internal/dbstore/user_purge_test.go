package dbstore

// Integration test for the unverified-account purge (issue #190):
// DeleteUnverifiedOlderThan removes only unverified users older than the
// cutoff, cleans up their auto-provisioned account/space, and never touches
// verified accounts. Skipped unless JOMFORM_TEST_DATABASE_URL is set;
// migrations are applied first, mirroring the production boot path.

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
