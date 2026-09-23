// Issue #601: the suppression-list admin migration (000061) must run cleanly
// UP and DOWN, and its backfill must preserve the provenance it rewrites.
//
// The existing fresh-DB guard (TestMigrationsApplyFromZero) applies every up
// migration to an EMPTY database, where 000061's backfill UPDATE matches
// nothing — so it cannot prove the part of this migration that only matters on
// a database that already holds rows (production has held a row since
// 2026-09-11). These tests cover exactly that gap:
//
//  1. UP with legacy data: the free-text reason the SNS ingestion path used to
//     write is mapped to a canonical code, the original text survives in
//     `note`, and the two CHECK constraints then hold.
//  2. DOWN: the migration reverses — index, constraints and both columns go —
//     and it is a no-op when run against a database that never had them.
//
// Skipped unless JOMFORM_TEST_DATABASE_URL is set (same convention as the rest
// of the package).
package db

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// migrationFile reads one migration file off disk by name. The downs are not in
// the embed FS (the runtime runner never needs them), so a rollback test has to
// read the file the same way TestEveryMigrationHasDownFile does.
func migrationFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("migrations", name))
	if err != nil {
		t.Fatalf("read migrations/%s: %v", name, err)
	}
	return string(b)
}

// TestSuppressionAdminMigrationBackfillsLegacyRows applies the whole migration
// set to a scratch database, rewrites the 000061 changes away to reproduce the
// pre-000061 schema, seeds the row shape the old ingestion path wrote, and then
// re-applies 000061 on top — asserting the backfill and the CHECKs.
func TestSuppressionAdminMigrationBackfillsLegacyRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dsn := os.Getenv("JOMFORM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping suppression migration test")
	}

	name := createScratchDB(t, ctx, dsn)
	pool, err := Connect(ctx, dsnFor(t, dsn, name))
	if err != nil {
		t.Fatalf("connect to scratch database %s: %v", name, err)
	}
	defer pool.Close()

	up := migrationFile(t, "000061_suppression_list_admin.up.sql")
	down := migrationFile(t, "000061_suppression_list_admin.down.sql")

	// Bring the scratch database to the current schema through the same runner
	// the API uses at startup, so suppressed_emails (000018) and every other
	// table exist exactly as in production.
	if err := Migrate(ctx, pool, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("apply migrations to scratch database: %v", err)
	}

	// Reproduce the pre-000061 schema so the legacy row can be seeded: 000018
	// created the table with four columns and no CHECK.
	if _, err := pool.Exec(ctx, down); err != nil {
		t.Fatalf("000061 down did not apply: %v", err)
	}
	// The rollback must leave the table usable and back at its 000018 shape.
	for _, col := range []string{"note", "created_by"} {
		var present bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM information_schema.columns
			               WHERE table_name = 'suppressed_emails' AND column_name = $1)`, col,
		).Scan(&present); err != nil {
			t.Fatalf("probe column %s: %v", col, err)
		}
		if present {
			t.Errorf("issue #601: down left column %q in place — the rollback is incomplete", col)
		}
	}
	var idxPresent bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM pg_indexes
		               WHERE schemaname = current_schema()
		                 AND tablename = 'suppressed_emails' AND indexname = 'idx_suppressed_emails_created_at')`,
	).Scan(&idxPresent); err != nil {
		t.Fatalf("probe index: %v", err)
	}
	if idxPresent {
		t.Error("issue #601: down left the ordering index in place")
	}
	// Running the down twice must be a no-op, not an error (a rollback that
	// cannot be retried is a rollback nobody dares run).
	if _, err := pool.Exec(ctx, down); err != nil {
		t.Fatalf("issue #601: running the down migration a second time failed: %v", err)
	}

	// Seed the row the pre-#601 ingestion path wrote: FREE-TEXT reason, no note,
	// no created_by. This is the production row shape from 2026-09-11.
	legacyAddr := "legacy-bounce@simulator.amazonses.com"
	if _, err := pool.Exec(ctx, `
		INSERT INTO suppressed_emails (email, reason, source, created_at)
		VALUES ($1, 'permanent bounce (Permanent)', 'bounce', now())`, legacyAddr); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	// Plus one already-canonical row, which the backfill must leave alone.
	canonicalAddr := "already-canonical@example.test"
	if _, err := pool.Exec(ctx, `
		INSERT INTO suppressed_emails (email, reason, source, created_at)
		VALUES ($1, 'hard_bounce', 'bounce', now())`, canonicalAddr); err != nil {
		t.Fatalf("seed canonical row: %v", err)
	}

	// ── UP, on a database that has data ──
	if _, err := pool.Exec(ctx, up); err != nil {
		t.Fatalf("issue #601: 000061 failed on a database that already holds rows: %v\n"+
			"the backfill must run BEFORE the CHECK constraint is added", err)
	}

	var reason, note, source string
	if err := pool.QueryRow(ctx,
		`SELECT reason, note, source FROM suppressed_emails WHERE email = $1`, legacyAddr).
		Scan(&reason, &note, &source); err != nil {
		t.Fatalf("read legacy row: %v", err)
	}
	if reason != "hard_bounce" {
		t.Errorf("legacy reason = %q, want hard_bounce — the free text was not normalised", reason)
	}
	if note != "permanent bounce (Permanent)" {
		t.Errorf("legacy note = %q, want the original free text preserved — the backfill erased the provenance", note)
	}

	// The already-canonical row is untouched.
	if err := pool.QueryRow(ctx,
		`SELECT reason, note FROM suppressed_emails WHERE email = $1`, canonicalAddr).
		Scan(&reason, &note); err != nil {
		t.Fatalf("read canonical row: %v", err)
	}
	if reason != "hard_bounce" || note != "" {
		t.Errorf("issue #601: the backfill rewrote an already-canonical row (reason %q, note %q)", reason, note)
	}

	// ── The CHECKs now hold ──
	if _, err := pool.Exec(ctx, `
		INSERT INTO suppressed_emails (email, reason, source) VALUES ('bad-reason@x.test', 'nonsense', 'bounce')`,
	); err == nil {
		t.Error("issue #601: the reason CHECK does not reject an unknown reason — the enum is not enforced at the DB level")
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO suppressed_emails (email, reason, source) VALUES ('bad-source@x.test', 'manual', 'nonsense')`,
	); err == nil {
		t.Error("issue #601: the source CHECK does not reject an unknown source")
	}

	// ── Uniqueness is enforced case-insensitively by the CITEXT PK, which the
	//    migration must leave as the PK ──
	if _, err := pool.Exec(ctx, `
		INSERT INTO suppressed_emails (email, reason, source) VALUES ($1, 'manual', 'manual')`,
		"LEGACY-BOUNCE@SIMULATOR.AMAZONSES.COM"); err == nil {
		t.Error("issue #601: an upper-case spelling of a stored address was inserted — the address is not unique case-insensitively")
	}

	// And the down reverses a populated table too.
	if _, err := pool.Exec(ctx, down); err != nil {
		t.Fatalf("issue #601: the down migration failed on a populated table: %v", err)
	}
	var stillThere int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM suppressed_emails WHERE email = $1`, legacyAddr).Scan(&stillThere); err != nil {
		t.Fatalf("read after down: %v", err)
	}
	if stillThere != 1 {
		t.Error("issue #601: the down migration lost a row — it must only drop what the up added")
	}
}
