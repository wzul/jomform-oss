package dbstore

// Integration tests for the forms NOT NULL (SQLSTATE 23502) bug class:
// nil config/theme maps must never reach the DB as NULL.
// Skipped unless JOMFORM_TEST_DATABASE_URL is set; migrations are applied
// first, mirroring the production boot path.
//
// Isolation (issue #474): testdb.Open gives this package its own scratch
// schema, re-created on every run, so rows from a previous run are never
// visible and this package can never delete rows another package is using.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/testdb"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return testdb.Open(t)
}

func uniqueAccount(t *testing.T, ctx context.Context, p *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text)
		 RETURNING id`,
		testOwnerUser(t, ctx, p)).Scan(&id); err != nil {
		t.Fatalf("create account: %v", err)
	}
	return id
}

func testOwnerUser(t *testing.T, ctx context.Context, p *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := p.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return id
}
