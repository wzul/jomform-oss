// Package db — fresh-database migration guard (issue #473).
//
// Incident this guards against: migration 000047_affiliates declared
// affiliates.workspace_id as TEXT with a FK to workspaces(id), which is uuid.
// Postgres rejected the constraint (SQLSTATE 42804, "foreign key constraint
// cannot be implemented") and the API restart-looped in production until a
// hotfix shipped. Nothing in the test suite ever applied the migration set to
// an empty database, so the failure was first seen by users.
//
// TestMigrationsApplyFromZero closes that gap: it creates a throwaway database
// with a unique name, runs EVERY embedded *.up.sql through the same runner the
// API uses at startup (Migrate), and fails on the first error. That catches
// type mismatches, bad/absent FKs, ordering bugs and duplicate objects — the
// whole 000047 bug class — before the code is merged.
//
// TestEveryMigrationHasDownFile is the companion static check: every up
// migration must ship a rollback file, so the set stays reversible. Two
// pre-existing gaps are pinned in the allowlist below rather than silently
// tolerated; a full reverse-rollback integration test cannot be asserted while
// those gaps exist (rolling back stops at 000001_init.down.sql, which cannot
// drop users while the spaces table created by 000012 is still present).
//
// Both tests skip unless JOMFORM_TEST_DATABASE_URL is set, matching the
// dbstore integration-test convention. The scratch database is dropped on
// cleanup, so the test is safe to run repeatedly and never touches the
// database named in the DSN.

package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// scratchPrefix names the throwaway databases this test creates. Kept short so
// the generated name stays well inside Postgres' 63-byte identifier limit.
const scratchPrefix = "jomform_migtest_"

// migrationsWithoutDown lists migrations that predate the rollback rule and
// have no .down.sql. Keyed by version, value explains why it is pinned. These
// are pre-existing gaps, not endorsements: new migrations must always ship a
// matching .down.sql or TestEveryMigrationHasDownFile fails.
var migrationsWithoutDown = map[string]string{
	"000005_form_overrides": "predates the down-file rule; tracked as a rollback gap",
	"000012_spaces":         "predates the down-file rule; tracked as a rollback gap",
}

// migrationUpFiles returns every embedded *.up.sql in the same order the
// startup runner applies them (filename order).
func migrationUpFiles(t *testing.T) []string {
	t.Helper()
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("no embedded *.up.sql migrations found — the go:embed directive is broken")
	}
	return files
}

// currentDatabase reports the database the pool is actually connected to.
func currentDatabase(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	var name string
	err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&name)
	return name, err
}

// dsnFor returns the connection string with its database name replaced.
//
// Do NOT go back to pgx.ParseConfig(dsn) + cfg.Database = database +
// cfg.ConnString(): pgx's ConnString() returns the ORIGINAL string and discards
// a mutated Database, so the caller silently keeps connecting to the DSN's own
// database. That defect (issue #532) made this guard connect to the shared test
// database instead of the empty scratch one, find every migration already
// applied, apply none, and pass vacuously — the exact 000047 class it exists to
// catch could have shipped green.
//
// Building the URL with url.URL keeps it explicit and correct, and
// TestMigrationsApplyFromZero additionally asserts (via current_database) that
// the pool really landed on the scratch database, so a regression here fails
// loudly instead of silently disabling the guard.
func dsnFor(t *testing.T, dsn, database string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse JOMFORM_TEST_DATABASE_URL: %v", err)
	}
	u.Path = "/" + database
	return u.String()
}

// randomSuffix returns a short random hex string for a throwaway database name,
// so two concurrent runs (or two guards in one run) never collide.
func randomSuffix(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("generate random database suffix: %v", err)
	}
	return hex.EncodeToString(buf)
}

// createScratchDB creates a uniquely named empty database and registers its
// removal with t.Cleanup. It connects through the DSN's own database so no
// separate "postgres" maintenance database needs to exist.
//
// It passes no TEMPLATE clause, i.e. it copies the server's default template
// (template1) — the same database shape the DSN's own database has. Use
// createScratchDBFromTemplate when the fixture needs a starting state it
// controls rather than one the server happens to have.
func createScratchDB(t *testing.T, ctx context.Context, dsn string) string {
	t.Helper()
	return createScratchDBFromTemplate(t, ctx, dsn, "")
}

// createScratchDBFromTemplate is createScratchDB with an explicit template: an
// empty string means "no TEMPLATE clause" (Postgres' default, template1), any
// other value is used as `CREATE DATABASE … TEMPLATE <template>`.
//
// A fixture that plants a pre-existing state must copy template0. A
// template1-derived database inherits whatever the server's template1 carries —
// on a provisioned server that already includes pgcrypto and citext in
// `public` — which silently changes what the fixture proves: an unqualified
// `CREATE EXTENSION` resolved through a `search_path` that ends in `public`
// then finds the inherited extension and dies with SQLSTATE 42710 ("extension
// … already exists") instead of planting the state the guard is about. Copying
// template0 makes the starting state deterministic on every server, provisioned
// or not.
func createScratchDBFromTemplate(t *testing.T, ctx context.Context, dsn, template string) string {
	t.Helper()
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("generate scratch database name: %v", err)
	}
	// The name ends with the CREATING pid (`…_p<pid>`), the same convention
	// pkgName uses for scratch schemas. It is what lets the database prune
	// (testdb/scratchdb.go) attribute a leftover database to a process and
	// reclaim it the moment that process is gone, instead of having to guess
	// from the clock whether the database might still be in use. testdb's
	// registry guard requires this shape of every throwaway-database creator.
	name := scratchPrefix + hex.EncodeToString(buf) + "_p" + strconv.Itoa(os.Getpid())

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test database to create scratch db: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	stmt := "CREATE DATABASE " + pgx.Identifier{name}.Sanitize()
	if template != "" {
		stmt += " TEMPLATE " + pgx.Identifier{template}.Sanitize()
	}
	if _, err := conn.Exec(ctx, stmt); err != nil {
		t.Fatalf("create scratch database %s (needs CREATEDB): %v", name, err)
	}

	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c, err := pgx.Connect(dropCtx, dsn)
		if err != nil {
			t.Logf("cleanup: could not connect to drop scratch database %s: %v", name, err)
			return
		}
		defer func() { _ = c.Close(dropCtx) }()
		quoted := pgx.Identifier{name}.Sanitize()
		if _, err := c.Exec(dropCtx, "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)"); err != nil {
			// WITH (FORCE) needs Postgres 13+; fall back to a plain drop.
			if _, err2 := c.Exec(dropCtx, "DROP DATABASE IF EXISTS "+quoted); err2 != nil {
				t.Logf("cleanup: could not drop scratch database %s: %v", name, err2)
			}
		}
	})

	return name
}

// TestMigrationsApplyFromZero applies the full embedded migration set to a
// brand-new database and fails on any error — the guard for the 000047 class
// of production crash-loop.
func TestMigrationsApplyFromZero(t *testing.T) {
	dsn := os.Getenv("JOMFORM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping fresh-DB migration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	ups := migrationUpFiles(t)

	name := createScratchDB(t, ctx, dsn)
	pool, err := Connect(ctx, dsnFor(t, dsn, name))
	if err != nil {
		t.Fatalf("connect to scratch database %s: %v", name, err)
	}
	defer pool.Close()

	// Issue #532: prove we are actually on the throwaway database, and that it
	// starts EMPTY. Without this the guard can silently degrade into applying
	// the migrations to the shared test database (where they are already
	// applied), finding every migration present, applying none, and passing
	// vacuously — i.e. the 000047 class could ship green. Asserting here means
	// any regression fails loudly instead.
	if cur, err := currentDatabase(ctx, pool); err != nil {
		t.Fatalf("resolve current_database for the scratch pool: %v", err)
	} else if cur != name {
		t.Fatalf("fresh-DB guard is connected to %q but the scratch database is %q — "+
			"the migrations would be applied to the wrong database and the guard would "+
			"pass vacuously (see issue #532)", cur, name)
	}
	var preExisting int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.tables
		 WHERE table_schema = 'public' AND table_name = 'schema_migrations'`,
	).Scan(&preExisting); err != nil {
		t.Fatalf("probe scratch database for a pre-existing schema_migrations: %v", err)
	}
	if preExisting != 0 {
		t.Fatalf("scratch database %s is NOT empty: schema_migrations already exists — "+
			"the from-zero guard cannot prove anything against a pre-migrated database", name)
	}

	log := slog.New(slog.DiscardHandler)
	if err := Migrate(ctx, pool, log); err != nil {
		t.Fatalf("applying %d migrations to an empty database failed: %v\n\n"+
			"This is exactly the 000047 class of bug (a migration that only fails "+
			"on a database where the object does not already exist). It MUST be "+
			"fixed before merge — see AGENTS.md 'Adding a migration safely'.", len(ups), err)
	}

	// Every up file must be recorded as applied. A migration that was silently
	// skipped (or a version-key collision) shows up here.
	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			t.Fatalf("scan schema_migrations: %v", err)
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate schema_migrations: %v", err)
	}

	for _, f := range ups {
		if !applied[f] {
			t.Errorf("migration %s was not recorded in schema_migrations after Migrate()", f)
		}
	}
	if len(applied) != len(ups) {
		t.Errorf("schema_migrations has %d rows, expected %d (one per *.up.sql)", len(applied), len(ups))
	}

	// A restart must be a no-op: the production boot path runs Migrate on every
	// start, so a migration that is not idempotent would crash-loop too.
	if err := Migrate(ctx, pool, log); err != nil {
		t.Fatalf("second Migrate() run (restart path) failed: %v", err)
	}
}

// TestEveryMigrationHasDownFile enforces that each up migration ships a
// rollback file. The two allowlisted gaps predate the rule and are pinned so
// they cannot grow silently.
//
// This reads the migrations directory from disk, not migrationFS: the embed
// directive only pulls in *.up.sql (the runtime runner never needs the downs).
func TestEveryMigrationHasDownFile(t *testing.T) {
	entries, err := os.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations directory: %v", err)
	}
	have := make(map[string]bool)
	for _, e := range entries {
		have[e.Name()] = true
	}

	for _, up := range migrationUpFiles(t) {
		version := strings.TrimSuffix(up, ".up.sql")
		if have[version+".down.sql"] {
			continue
		}
		if _, pinned := migrationsWithoutDown[version]; pinned {
			continue
		}
		t.Errorf("migration %s has no %s.down.sql — every new migration must ship a rollback (see AGENTS.md)", up, version)
	}

	// Fail loudly if a pinned gap is fixed, so the allowlist cannot rot.
	for version, reason := range migrationsWithoutDown {
		if have[version+".down.sql"] {
			t.Errorf("%s now has a .down.sql (%s) — remove it from migrationsWithoutDown", version, reason)
		}
	}
}

// migrationFileRE is the naming convention every migration must follow: a
// six-digit zero-padded number, an underscore, a slug, and one direction
// suffix. A file that does not match carries no number at all, so the
// uniqueness guard below cannot see it — which is why a non-conforming name is
// a failure in its own right rather than a skipped entry.
var migrationFileRE = regexp.MustCompile(`^[0-9]{6}_[a-z0-9_]+\.(up|down)\.sql$`)

// duplicatedMigrationNumbers pins the number collisions that ALREADY exist on
// main. They are pinned, not endorsed: 27 files across 10 numbers share a
// prefix, every one of them is applied on every live database, and
// schema_migrations keys on the FULL FILENAME (migrate.go:58, :89) so a
// renamed file is RE-APPLIED — renumbering an applied migration is therefore a
// live-data operation, not a tidy-up. Keyed by number, value is every version
// sharing it (sorted), so the allowlist cannot grow silently: a file added to a
// pinned group, or one renumbered away from it, both fail.
//
// Why pin instead of enforce-from-zero: a NEW collision is what the guard is
// for (three parallel PRs picked 000067 at once, measured). The 10 pre-existing
// groups break only the convention, not the runner, so they are recorded here
// with the same "cannot grow, cannot rot" discipline migrationsWithoutDown uses
// for the two missing down-files.
//
// Every group here is independent DDL (different tables/columns); none of them
// depends on another member's object. A collision where one migration DEPENDS on
// another's column would apply in slug-alphabetical order, not authoring order —
// a latent ordering bug this guard prevents from starting, not one it can detect
// after the fact.
var duplicatedMigrationNumbers = map[string][]string{
	"000010": {"000010_signkey", "000010_system_settings"},
	"000018": {"000018_pw_reset_at", "000018_space_default_lang", "000018_suppressed_emails"},
	"000021": {"000021_sale_ref_delivery_events", "000021_space_email_settings"},
	"000023": {"000023_product_variations", "000023_subscription_charges", "000023_subscription_update_card"},
	"000025": {"000025_space_members", "000025_user_default_space"},
	"000028": {"000028_form_private_status", "000028_media", "000028_user_phone"},
	"000029": {"000029_space_currency", "000029_space_footer", "000029_space_legal_pages", "000029_user_timezone"},
	"000032": {"000032_form_confirmations", "000032_form_notifications", "000032_form_webhooks"},
	"000042": {"000042_form_failure_message", "000042_form_unlisted_status"},
	"000062": {"000062_response_form_default_notification", "000062_variation_subscription_cycle", "000062_workspace_spam_judge_enabled"},
}

// duplicateNumbers groups versions by their six-digit number prefix and returns
// only the numbers shared by more than one version.
func duplicateNumbers(versions []string) map[string][]string {
	byNumber := make(map[string][]string)
	for _, v := range versions {
		num := v[:strings.IndexByte(v, '_')]
		byNumber[num] = append(byNumber[num], v)
	}
	dups := make(map[string][]string)
	for num, members := range byNumber {
		if len(members) > 1 {
			sort.Strings(members)
			dups[num] = members
		}
	}
	return dups
}

// TestMigrationNumbersAreUnique enforces one number per migration. This is the
// guard the two migrations_fresh_db guards cannot provide: a same-numbered pair
// is INVISIBLE to them. schema_migrations.version is the full filename, so two
// files sharing a number are two keys and both apply (filename order) — the
// fresh-DB guard's `len(applied) == len(ups)` assertion therefore stays green
// with a collision planted. Measured on main: three parallel PRs picked 000067
// at once, and 10 numbers / 27 files are already duplicated, with every gate
// green — which is exactly why the collision class went unnoticed, not proof
// that it is harmless.
//
// Both directions are checked: a duplicate number is a collision whether it is
// two *.up.sql or two *.down.sql files.
func TestMigrationNumbersAreUnique(t *testing.T) {
	entries, err := os.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations directory: %v", err)
	}

	var ups, downs []string
	for _, e := range entries {
		name := e.Name()
		if !migrationFileRE.MatchString(name) {
			t.Errorf("migration %s does not follow NNNNNN_slug.(up|down).sql — every migration must carry a "+
				"six-digit number so the uniqueness guard can see it (see AGENTS.md 'Adding a migration safely')", name)
			continue
		}
		switch {
		case strings.HasSuffix(name, ".up.sql"):
			ups = append(ups, strings.TrimSuffix(name, ".up.sql"))
		case strings.HasSuffix(name, ".down.sql"):
			downs = append(downs, strings.TrimSuffix(name, ".down.sql"))
		}
	}
	if len(ups) == 0 {
		t.Fatal("no embedded *.up.sql migrations found — the go:embed directive is broken")
	}
	sort.Strings(ups)
	sort.Strings(downs)

	for _, dir := range []struct {
		name     string
		versions []string
	}{{"up", ups}, {"down", downs}} {
		dups := duplicateNumbers(dir.versions)
		for num, members := range dups {
			pinned, ok := duplicatedMigrationNumbers[num]
			if !ok {
				t.Errorf("migration number %s is shared by %d %s files (%s) — give the new one its own number "+
					"(one number per migration; see AGENTS.md). A duplicate number applies in filename order, not "+
					"authoring order, so a pair where one depends on the other's column breaks silently.",
					num, len(members), dir.name, strings.Join(members, ", "))
				continue
			}
			if !slices.Equal(members, slices.Sorted(slices.Values(pinned))) {
				t.Errorf("migration number %s is pinned in duplicatedMigrationNumbers as (%s) but the %s files are "+
					"now (%s) — a pinned collision must not change membership; give the new/changed file its own number.",
					num, strings.Join(pinned, ", "), dir.name, strings.Join(members, ", "))
			}
		}
		// Fail loudly if a pinned collision is fixed, so the allowlist cannot rot.
		for num, pinned := range duplicatedMigrationNumbers {
			if _, still := dups[num]; !still {
				t.Errorf("duplicatedMigrationNumbers pins %s (%s) but no two %s files share that number any more — "+
					"remove the pin", num, strings.Join(pinned, ", "), dir.name)
			}
		}
	}
}
