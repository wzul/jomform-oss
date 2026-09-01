package dbstore

// Integration tests for the forms NOT NULL (SQLSTATE 23502) bug class:
// nil config/theme maps must never reach the DB as NULL.
// Skipped unless JOMFORM_TEST_DATABASE_URL is set; migrations are applied
// first, mirroring the production boot path.

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wzul/jomform/api/internal/db"
)

var (
	pgOnce sync.Once
	pgPool *pgxpool.Pool
	pgErr  error
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("JOMFORM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping dbstore integration test")
	}
	pgOnce.Do(func() {
		pool, err := db.Connect(context.Background(), dsn)
		if err != nil {
			pgErr = err
			return
		}
		if err = db.Migrate(context.Background(), pool, slog.New(slog.DiscardHandler)); err != nil {
			pgErr = err
			return
		}
		pgPool = pool
	})
	if pgErr != nil {
		t.Fatalf("test database unavailable: %v", pgErr)
	}
	return pgPool
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
