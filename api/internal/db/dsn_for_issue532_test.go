package db

// Issue #532: the fresh-DB migration guard was passing vacuously.
//
// dsnFor() built the scratch-database connection string with
// pgx.ParseConfig(dsn) → cfg.Database = scratchName → cfg.ConnString(). pgx's
// ConnString() returns the ORIGINAL connection string and DISCARDS a mutated
// Database field, so the guard actually connected to the shared test database.
// There, every migration was already applied, so Migrate() applied NONE of them
// and TestMigrationsApplyFromZero passed without ever exercising a single
// migration from zero — silently disabling the guard for the 000047 class of
// production crash-loop it exists to catch.
//
// These tests pin the property directly, so a future "simplification" back to
// the pgx form fails here rather than quietly disarming production safety.

import (
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestIssue532DsnForTargetsTheGivenDatabase is the core regression: the
// produced DSN must actually name the scratch database.
func TestIssue532DsnForTargetsTheGivenDatabase(t *testing.T) {
	const dsn = "postgres://jomform:pw@localhost:5432/jomform_test?sslmode=disable"
	const scratch = "jomform_migtest_abc123"

	got := dsnFor(t, dsn, scratch)

	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("dsnFor produced an unparseable DSN %q: %v", got, err)
	}
	if db := strings.TrimPrefix(u.Path, "/"); db != scratch {
		t.Fatalf("issue #532: dsnFor(dsn, %q) targets database %q — the guard would run "+
			"against the wrong database and pass vacuously", scratch, db)
	}
	// The original database must NOT survive anywhere in the string.
	if strings.Contains(got, "jomform_test") {
		t.Fatalf("issue #532: dsnFor output still references the source database: %q", got)
	}
	// Credentials/params must be preserved, or the scratch connection fails.
	if u.User == nil || u.User.Username() != "jomform" {
		t.Fatalf("dsnFor dropped the credentials: %q", got)
	}
	if !strings.Contains(got, "sslmode=disable") {
		t.Fatalf("dsnFor dropped query parameters: %q", got)
	}
	if u.Host != "localhost:5432" {
		t.Fatalf("dsnFor changed the host: %q", got)
	}
}

// TestIssue532PgxConnStringDiscardsDatabaseMutation documents WHY dsnFor must
// not use the pgx form, so the reason survives in the codebase rather than only
// in a PR description. If a future pgx release starts reflecting the mutation,
// this test tells us the constraint lifted.
func TestIssue532PgxConnStringDiscardsDatabaseMutation(t *testing.T) {
	const orig = "postgres://u:p@host:5432/jomform_test?sslmode=disable"

	cfg, err := pgx.ParseConfig(orig)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cfg.Database = "jomform_migtest_zzz"

	if cfg.ConnString() == orig {
		t.Logf("confirmed: pgx ConnString() ignores the mutated Database — " +
			"this is why dsnFor builds the URL itself (issue #532)")
	} else {
		t.Logf("note: pgx now reflects a mutated Database; the dsnFor workaround " +
			"is still correct but no longer load-bearing")
	}
}
