// Package testdb provides the shared integration-test database helper.
//
// JomForm's integration tests run against a real Postgres (skipped when
// JOMFORM_TEST_DATABASE_URL is unset). Historically every test package used the
// SAME `public` schema of that one database, so the test DB accumulated rows
// across runs and tests became order- and history-dependent: stale
// webhook_retries filled DueRetries(limit), an affiliate workspace referenced a
// missing owner user, a reused CHIP purchase id collided on subscription_charges,
// and global-count assertions (email_delivery_events) saw other runs' rows.
//
// Open gives each test PROCESS a scratch schema that is dropped and re-created
// on every test-binary run, then migrated into. A test therefore starts from a
// schema it alone owns, with only migration seed data present:
//
//   - history-independent — a previous run's rows can never be seen, and
//   - cross-package-safe — `go test ./...` runs packages in parallel against
//     the same database, and each package runs in its own process, so each
//     process owns its own schema and a cleanup in one can no longer delete
//     rows another is using.
//
// The schema name carries the process's pid (`test_<pkg>_p<pid>`), not just the
// package name. Every checkout points at the SAME test database, so a per-package
// name means two concurrent full suites — the normal state of this board, where
// several worktrees gate at once — both DROP and re-create `test_<pkg>`, and
// whichever re-creates it second pulls the tables out from under the other
// mid-test. The failures then look like a regression in code the diff never
// touched: `relation "workspaces" does not exist (SQLSTATE 42P01)`, or a peer's
// rows surviving into a schema that was supposed to be fresh (`duplicate key
// value violates unique constraint "subscription_charges_chip_purchase_id_key"`
// in a test that had just re-created its schema). Schemas left behind by a
// process that has exited are pruned on the next Open.
//
// Tables the migrations seed (system_settings, the template library) are
// populated, exactly as a freshly-migrated database would be. For tests that
// assert on the WHOLE table (global List/COUNT), Reset empties the tables a
// test owns at entry and again in t.Cleanup, so the assertion is deterministic
// on every -count.
package testdb

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/db"
)

// DSNEnv is the environment variable holding the integration-test database URL.
const DSNEnv = "JOMFORM_TEST_DATABASE_URL"

// publicSchema is where the shared extensions (citext, pgcrypto) must live.
const publicSchema = "public"

// extensionLockKey serializes scratch-schema preparation across every test
// package — and every checkout — sharing one database. The value is arbitrary;
// it only has to be a constant, because the state it guards (the location of a
// database-global extension) is shared by all of them.
const extensionLockKey int64 = 0x4A6F6D466F726D // "JomForm"

// sharedExtensions are the extensions the migrations declare, which every
// package needs reachable from `public` (users.email, accounts.custom_domain,
// suppressed_emails.email and the forms/feedback_threads columns are CITEXT;
// gen_random_uuid() comes from pgcrypto). It is db.SharedExtensions, which db
// derives from the migration set itself — so adding an extension to a migration
// cannot leave this repair behind, and there is exactly one list to keep right.
var sharedExtensions = db.SharedExtensions

// extensionProvisioner is the minimum ensureExtensions needs: something that
// can begin a transaction. Both *pgxpool.Pool (what open passes) and *pgx.Conn
// satisfy it, which lets a guard drive the privilege path — ensureExtensions
// running as a role that does not own the extensions — on a single session.
type extensionProvisioner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

var (
	once sync.Once
	pool *pgxpool.Pool
	err  error
)

// Open returns a pool connected to this test process's scratch schema, applying
// migrations on first use (mirroring the production boot path). It skips the
// test when DSNEnv is unset, so unit-only runs stay hermetic.
//
// The scratch schema is named after the test package AND this process's pid
// (`test_<pkg>_p<pid>`), and is dropped and re-created on every call — i.e. once
// per test-binary run — so no row from a previous run survives into this one.
// The pid is what makes the name unique per RUN rather than merely per package:
// every checkout points at the same test database, so two concurrent `go test`
// binaries for the same package would otherwise share one name and each one's
// DROP SCHEMA ... CASCADE would delete the tables the other is mid-test on.
func Open(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv(DSNEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping integration test", DSNEnv)
	}
	once.Do(func() { pool, err = open(context.Background(), dsn, pkgName()) })
	if err != nil {
		t.Fatalf("test database unavailable: %v", err)
	}
	return pool
}

// Connect is Open for tests that use subtests (t.Run) and therefore want the
// package pool without a *testing.T. It is safe to call from any test in the
// package; the pool is created once.
func Connect(t *testing.T) (*pgxpool.Pool, error) {
	t.Helper()
	dsn := os.Getenv(DSNEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping integration test", DSNEnv)
	}
	once.Do(func() { pool, err = open(context.Background(), dsn, pkgName()) })
	return pool, err
}

func open(ctx context.Context, dsn, schema string) (*pgxpool.Pool, error) {
	// Admin connection to (re)create the scratch schema.
	admin, err := db.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	defer admin.Close()

	// The shared extensions are provisioned into `public` by ensureSchema
	// below, BEFORE it drops this package's scratch schema — see
	// ensureExtensions for why that order is load-bearing. Nothing here touches
	// the schema first, so there is no second, earlier chance to repair.
	if err := ensureSchema(ctx, admin, schema); err != nil {
		return nil, err
	}
	pruneDeadSchemas(ctx, admin, schema)

	// Pool whose connections resolve unqualified names to the scratch schema
	// first, then `public` for shared extensions (pgcrypto, citext). This is the
	// same mechanism as `SET search_path` in psql.
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := p.Ping(ctx); err != nil {
		p.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if err := migrate(ctx, p, schema); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

func ensureSchema(ctx context.Context, admin *pgxpool.Pool, schema string) error {
	// Extensions FIRST, before the DROP below. Three reasons, all load-bearing:
	//
	//   - migration 000001 declares them with `CREATE EXTENSION IF NOT EXISTS`,
	//     and each package's migrate() sets search_path to its OWN scratch
	//     schema first. On a fresh database the first package to migrate would
	//     otherwise install pgcrypto/citext INSIDE its scratch schema, and every
	//     other package — whose search_path resolved the name to that schema —
	//     then died. Installing them in `public` first makes the migration a
	//     NOTICE no-op for everyone.
	//   - DROP SCHEMA ... CASCADE takes an extension living inside the schema
	//     being dropped with it, and the cascade reaches further than the
	//     schema: with citext stranded in `s1` and a citext column on a table
	//     in `s2`, the drop reports `drop cascades to extension citext` AND
	//     `drop cascades to column email of table s2.t`. Repairing first is
	//     therefore not cosmetic — it is what keeps the drop from reaching into
	//     a peer's schema.
	//   - the repair call is serialized on an advisory lock, because concurrent
	//     `CREATE EXTENSION IF NOT EXISTS` is a race, not a no-op.
	if err := ensureExtensions(ctx, admin); err != nil {
		return fmt.Errorf("ensure shared extensions: %w", err)
	}
	if _, err := admin.Exec(ctx, `DROP SCHEMA IF EXISTS `+quote(schema)+` CASCADE`); err != nil {
		return fmt.Errorf("drop scratch schema: %w", err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+quote(schema)); err != nil {
		return fmt.Errorf("create scratch schema: %w", err)
	}
	return nil
}

// ensureExtensions puts every shared extension in `public`, creating it there
// if it is missing — the ONE repair this package performs, called from
// ensureSchema (and therefore from open() and ResetSchema, always before the
// scratch schema is dropped).
//
// The defect this closes: a *fresh* database (one where `public` has no citext)
// made 000001's `CREATE EXTENSION IF NOT EXISTS citext` take its schema from
// the session search_path — and every package migrates under
// `SET search_path=<scratch>,public`, so the extension was installed into
// whichever scratch schema migrated first. From then on the extension
// *existed*, so `IF NOT EXISTS` skipped it in every other package, and those
// packages' `<their scratch>,public` could not resolve `citext`: the whole
// suite failed with `type "citext" does not exist (SQLSTATE 42704)` in packages
// the change never touched. It also survived the run, because
// `DROP SCHEMA ... CASCADE` only drops an extension that lives inside the
// schema being dropped — which is exactly why the first package's own re-run
// healed it and everyone else's stayed broken.
//
// Three details make this correct rather than merely tidy:
//
//   - `CREATE EXTENSION ... SCHEMA public` does NOT relocate an extension that
//     already exists elsewhere (`IF NOT EXISTS` turns into a NOTICE and the
//     schema argument is ignored), so a database already poisoned this way
//     needs an explicit `ALTER EXTENSION ... SET SCHEMA public`. Every
//     extension in sharedExtensions is relocatable.
//   - The ALTER is issued ONLY when the extension is not already in `public`.
//     It is a semantic no-op in that case but NOT a privilege no-op: ALTER
//     EXTENSION requires ownership of the extension
//     (https://www.postgresql.org/docs/current/sql-alterextension.html), so the
//     unconditional form turns a correctly-shaped database whose extension is
//     owned by a different role — a template-derived or psql-provisioned
//     database — into `ERROR: must be owner of extension citext (SQLSTATE
//     42501)` on every reset. `CREATE EXTENSION ... IF NOT EXISTS ... SCHEMA
//     public` carries no such requirement (it degrades to a NOTICE), so reading
//     the current schema first is what keeps the repair free for a database that
//     does not need repairing.
//   - Extension creation is not concurrency-safe on its own: `go test ./...`
//     runs every package in parallel, and two packages racing the first
//     `CREATE EXTENSION IF NOT EXISTS` can collide on the database-global
//     `pg_extension_name_index` (duplicate key, SQLSTATE 23505). A transaction
//     advisory lock keyed to the database serializes this across packages and
//     across concurrent checkouts, so exactly one installs and the rest wait
//     and then see it.
//
// The extensions come from sharedExtensions (db.SharedExtensions, derived from
// the migration set), not a hand-written list: adding an extension to a
// migration cannot leave this repair behind.
func ensureExtensions(ctx context.Context, admin extensionProvisioner) error {
	tx, err := admin.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin extension transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, extensionLockKey); err != nil {
		return fmt.Errorf("acquire extension lock: %w", err)
	}
	for _, ext := range sharedExtensions {
		// Where is it now? An extension an earlier run installed into a
		// scratch schema is one that lived in a *different* package's schema
		// (e.g. test_chat) and outlived the package that made it, because
		// DROP SCHEMA ... CASCADE only takes an extension that lives inside
		// the schema being dropped.
		current, installed, err := installedExtensionSchema(ctx, tx, ext)
		if err != nil {
			return err
		}
		if !installed {
			if _, err := tx.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS `+quote(ext)+` SCHEMA `+quote(publicSchema)); err != nil {
				return fmt.Errorf("create extension %s in %s: %w", ext, publicSchema, err)
			}
			continue
		}
		if current == publicSchema {
			// Correctly placed: issuing the ALTER here would demand
			// ownership of an extension this role may not own, and the
			// database is already in the shape we require.
			continue
		}
		if _, err := tx.Exec(ctx, `ALTER EXTENSION `+quote(ext)+` SET SCHEMA `+quote(publicSchema)); err != nil {
			return fmt.Errorf("relocate extension %s from %s to %s: %w", ext, current, publicSchema, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit extension transaction: %w", err)
	}
	return nil
}

// pruneDeadSchemas drops leftover scratch schemas whose owning process is gone.
//
// Once the name carries a pid, a run that was killed (or a test that crashed)
// leaves its schema behind, and without this the test database would accumulate
// one per crashed run forever. Only schemas matching the scratch naming shape are
// considered, and a schema whose pid is still alive — i.e. another test binary
// gating right now — is NEVER touched: dropping a live peer's schema is exactly
// the mid-test race this naming scheme exists to prevent.
//
// A name without a pid suffix predates this scheme and cannot be attributed to a
// process at all, so it is reclaimed only when the database has no OTHER live
// connection. With a peer connected, leaving it is the conservative answer: a
// stray empty schema costs nothing, a wrong drop costs a peer's whole run.
//
// Cleanup is best-effort: a failure here must not fail the test that is about to
// run, so errors are swallowed rather than propagated.
func pruneDeadSchemas(ctx context.Context, admin *pgxpool.Pool, keep string) {
	_, _ = pruneScratch(ctx, keep, pgScratch{admin})
}

// scratchQuerier is the slice of the connection prune needs. It exists so the
// prune logic can be driven against a fake that reports planted schemas and a
// controlled "other connections" count — the live-database test can only ever
// observe the real answers, so the rule's two directions (drop what is
// abandoned, never drop what a peer owns) need a driver that can set them.
type scratchQuerier interface {
	schemas(ctx context.Context) ([]string, error)
	otherConnections(ctx context.Context) (int, error)
	dropSchema(ctx context.Context, name string) error
}

// pgScratch is the live implementation, over the admin connection.
type pgScratch struct{ admin *pgxpool.Pool }

func (q pgScratch) schemas(ctx context.Context) ([]string, error) {
	rows, err := q.admin.Query(ctx,
		`SELECT nspname FROM pg_namespace WHERE nspname LIKE 'test\_%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err == nil {
			names = append(names, n)
		}
	}
	return names, rows.Err()
}

func (q pgScratch) otherConnections(ctx context.Context) (int, error) {
	var others int
	err := q.admin.QueryRow(ctx,
		`SELECT count(*) FROM pg_stat_activity
		  WHERE datname = current_database() AND pid <> pg_backend_pid()`).Scan(&others)
	return others, err
}

func (q pgScratch) dropSchema(ctx context.Context, name string) error {
	_, err := q.admin.Exec(ctx, `DROP SCHEMA IF EXISTS `+quote(name)+` CASCADE`)
	return err
}

// pruneScratch drops every leftover scratch schema the delete rule says is
// reclaimable, and reports the names it dropped. `keep` is this process's own
// schema and is never a candidate. A nil error with an empty slice is the normal
// "nothing to do" case; the caller ignores the error entirely (best-effort
// cleanup must not fail the test about to run).
func pruneScratch(ctx context.Context, keep string, q scratchQuerier) ([]string, error) {
	names, err := q.schemas(ctx)
	if err != nil {
		return nil, err
	}

	// Unattributable (legacy) schemas may be reclaimed only when this is the
	// sole connection to the database.
	solo := true
	if others, err := q.otherConnections(ctx); err == nil && others > 0 {
		solo = false
	}

	var dropped []string
	for _, n := range names {
		if n == keep || !reclaimableScratchSchema(n, solo) {
			continue
		}
		if err := q.dropSchema(ctx, n); err != nil {
			continue
		}
		dropped = append(dropped, n)
	}
	return dropped, nil
}

// reclaimableScratchSchema reports whether a leftover scratch schema may be
// dropped. It is deliberately conservative in BOTH directions:
//
//   - a schema a live process still owns is never reclaimable, because dropping
//     it reproduces the mid-test race this naming scheme exists to prevent, and
//   - nothing is reclaimed while any other connection exists, because
//     `DROP SCHEMA ... CASCADE` is database-wide DDL: it invalidates catalogue
//     entries a peer may be reading, and a peer's `pg_indexes` scan can then die
//     with `could not open relation with OID (SQLSTATE XX000)`. Cleanup is not
//     worth a peer's gate run — a leaked empty schema costs nothing.
func reclaimableScratchSchema(schema string, solo bool) bool {
	if !solo {
		// `DROP SCHEMA ... CASCADE` is database-wide DDL: it invalidates
		// catalogue entries a peer may be reading, so it is never issued while
		// any other connection exists, however abandoned the schema looks.
		return false
	}
	// Alone, a schema may be reclaimed if it predates the pid scheme and so
	// cannot be attributed to a process at all, or if its owning process is gone.
	return legacyScratchSchema(schema) || !liveScratchSchema(schema)
}

// legacyScratchSchema reports whether a scratch schema predates the pid suffix,
// i.e. it cannot be attributed to any process.
func legacyScratchSchema(schema string) bool {
	_, ok := schemaPID(schema)
	return !ok
}

// liveScratchSchema reports whether a scratch schema belongs to a process that
// still exists. A name with no pid suffix cannot be attributed, so it is
// reported live here; pruneDeadSchemas decides separately whether an
// unattributable schema is safe to reclaim.
func liveScratchSchema(schema string) bool {
	pid, ok := schemaPID(schema)
	if !ok {
		return true
	}
	return ownerAliveFromErr(syscall.Kill(pid, 0))
}

// ownerAliveFromErr maps the result of `kill(pid, 0)` to "that process still
// exists". EPERM counts as alive: the process is there but owned by another
// user, and treating it as dead would delete a schema out from under a running
// test — the exact race this naming scheme exists to prevent. Split out from the
// syscall so that arm is testable without needing a second unprivileged user.
func ownerAliveFromErr(err error) bool {
	return err == nil || err == syscall.EPERM
}

// schemaPID extracts the pid encoded by pkgName, e.g. test_httpapi_p1234 → 1234.
func schemaPID(schema string) (int, bool) {
	i := strings.LastIndex(schema, "_p")
	if i < 0 {
		return 0, false
	}
	pid, err := strconv.Atoi(schema[i+2:])
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// installedExtensionSchema reports the schema ext is installed in, and whether
// it is installed at all. A not-installed extension is not an error: the caller
// creates it.
func installedExtensionSchema(ctx context.Context, tx pgx.Tx, ext string) (string, bool, error) {
	var schema string
	err := tx.QueryRow(ctx, `
		SELECT n.nspname
		  FROM pg_extension e
		  JOIN pg_namespace n ON n.oid = e.extnamespace
		 WHERE e.extname = $1`, ext).Scan(&schema)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read installed schema of extension %s: %w", ext, err)
	}
	return schema, true, nil
}

// migrate applies the embedded migrations into schema. Unlike db.Migrate it
// does not consult schema_migrations: the scratch schema was just re-created,
// so every migration must run (seed data included), and a single multi-
// statement Exec keeps the ~64 migrations to one round trip.
//
// The shared extensions are provisioned into `public` by open() BEFORE this
// runs, so the `CREATE EXTENSION IF NOT EXISTS` in 000001 is a NOTICE no-op
// rather than a create inside this scratch schema. That is what keeps a fresh
// database usable by all packages in one run — see open().
func migrate(ctx context.Context, p *pgxpool.Pool, schema string) error {
	migrations, err := db.MigrationStatements()
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("SET search_path=" + quote(schema) + ",public;\n")
	for _, m := range migrations {
		b.WriteString(m.SQL)
		b.WriteString(";\n")
	}
	if _, err := p.Exec(ctx, b.String()); err != nil {
		return fmt.Errorf("migrate scratch schema %s: %w", schema, err)
	}
	return nil
}

// Reset empties the given tables so a test that asserts on global table state
// (a List/COUNT over the whole table) starts deterministic, and schedules the
// same cleanup for after the test. Because each package owns its own schema,
// this only ever affects rows this package created — it can never delete rows
// another package is using concurrently.
//
// Call it before seeding the rows under test:
//
//	pool := testdb.Open(t)
//	testdb.Reset(t, pool, "email_delivery_events")
//
// The reset runs again in t.Cleanup, so a -count=3 run leaves each iteration a
// clean schema. Unknown table names fail the test rather than silently passing.
func Reset(t *testing.T, pool *pgxpool.Pool, tables ...string) {
	t.Helper()
	clean := func() error {
		if len(tables) == 0 {
			return nil
		}
		stmt := "TRUNCATE TABLE " + strings.Join(quoted(tables), ", ") + " RESTART IDENTITY CASCADE"
		_, err := pool.Exec(context.Background(), stmt)
		return err
	}
	if err := clean(); err != nil {
		t.Fatalf("testdb.Reset(%v): %v", tables, err)
	}
	t.Cleanup(func() {
		if err := clean(); err != nil {
			t.Errorf("testdb.Reset cleanup(%v): %v", tables, err)
		}
	})
}

// ResetSchema drops and re-creates this process's scratch schema, wiping every
// row (including migration seed data) and re-applying migrations. Use it only
// for a test that must observe a truly empty database; prefer Reset for
// per-table cleanup.
func ResetSchema(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv(DSNEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping integration test", DSNEnv)
	}
	once.Do(func() { pool, err = open(context.Background(), dsn, pkgName()) })
	if err != nil {
		t.Fatalf("test database unavailable: %v", err)
	}
	schema := pkgName()
	admin, cerr := db.Connect(context.Background(), dsn)
	if cerr != nil {
		t.Fatalf("test database unavailable: %v", cerr)
	}
	defer admin.Close()
	// ensureSchema repairs the extension placement BEFORE dropping the scratch
	// schema, so a stranded extension cannot be taken by the DROP — and the
	// extensions live in public, never in this package's schema.
	if cerr := ensureSchema(context.Background(), admin, schema); cerr != nil {
		t.Fatalf("reset scratch schema: %v", cerr)
	}
	if cerr := migrate(context.Background(), pool, schema); cerr != nil {
		t.Fatalf("re-migrate scratch schema: %v", cerr)
	}
	return pool
}

// NewLogger is the discard logger used for migration output.
func NewLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// pkgName derives a schema name from the calling test package AND this process's
// pid (e.g. internal/dbstore → test_dbstore_p1234).
//
//   - the package part is what separates the packages `go test ./...` runs in
//     parallel against the one shared database, and
//   - the pid part is what separates concurrent RUNS of the same package from
//     different checkouts. Every checkout points at the same database, so a
//     name without it is shared by every lane gating at once, and each lane's
//     `DROP SCHEMA ... CASCADE` deletes the tables the others are mid-test on.
//
// Do not "simplify" the pid away — see the package doc comment.
func pkgName() string {
	return "test_" + sanitize(pkgFromFrames()) + "_p" + strconv.Itoa(os.Getpid())
}

func sanitize(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "pkg"
	}
	return b.String()
}

func quote(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func quoted(idents []string) []string {
	out := make([]string, len(idents))
	for i, id := range idents {
		out[i] = quote(id)
	}
	return out
}
