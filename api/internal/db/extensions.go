// Package db — provisioning of the extensions shared by every schema.
package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
)

// sharedExtensionLockKey is the advisory-lock key that serializes creation of
// the shared extensions. Every creator must take it, because
// `CREATE EXTENSION IF NOT EXISTS` is NOT race-safe despite its name: the
// IF NOT EXISTS check is a snapshot read of pg_extension, while the insert
// afterwards is guarded by the unique index pg_extension_name_index. Two
// transactions that both miss the check therefore both insert, and the loser
// dies with `duplicate key value violates unique constraint
// "pg_extension_name_index"` (SQLSTATE 23505). Verified on this repo's Postgres
// 16: six concurrent statements produced five errors, one winner.
const sharedExtensionLockKey = "jomform:shared-extensions"

// publicSchema is where the shared extensions belong. Named as a constant so
// every message and assertion refers to the same place.
const publicSchema = "public"

// SharedExtensions are the extensions the migrations create and that every
// schema in a JomForm database then uses UNQUALIFIED: CITEXT columns
// (users.email, accounts.slug, …) and gen_random_uuid() defaults. They are
// installed into `public` once per database rather than per schema, so that a
// schema-level DROP can never take them with them and so that every schema
// (the scratch test schemas included) resolves them through search_path.
//
// The list is DERIVED from the migration set (DeclaredExtensions), not
// hand-written. A hand-written copy is a second source of truth that drifts:
// an extension added to a migration but missing from the list would be created
// inside the first scratch schema again and reintroduce the 23505 race, and
// nothing in the build would say so. Deriving it makes that structurally
// impossible, and mustDeriveSharedExtensions fails loudly if the derivation
// comes back empty (a broken parser or embed directive) instead of silently
// provisioning nothing.
var SharedExtensions = mustDeriveSharedExtensions()

// mustDeriveSharedExtensions reads the extensions the embedded migrations
// declare, and fails loudly rather than returning an empty list: an empty list
// would make EnsureSharedExtensions a no-op that still reports success, which is
// the one failure mode that would look green while the suite it protects died
// with SQLSTATE 42704.
//
// Panicking is deliberate — the only error DeclaredExtensions can return is a
// broken go:embed read, which cannot be recovered from at runtime and must not
// be allowed to pass as "no extensions to provision".
func mustDeriveSharedExtensions() []string {
	names, err := DeclaredExtensions()
	if err != nil {
		panic("db: read the extensions declared by the embedded migrations: " + err.Error())
	}
	if len(names) == 0 {
		panic("db: the embedded migrations declare no extensions — the parser in DeclaredExtensions or the go:embed directive is broken, and provisioning would silently do nothing")
	}
	return names
}

// ExtensionProvisioner is the minimum a caller must offer to provision the
// shared extensions: something that can begin a transaction. Both
// *pgxpool.Pool and *pgx.Conn satisfy it.
type ExtensionProvisioner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// EnsureSharedExtensions creates the shared extensions in `public` if they are
// not present, is safe to call concurrently from any number of processes
// against the same database, and — on return with a nil error — guarantees
// that every extension in SharedExtensions is installed in `public`.
//
// Three properties matter, and each is load-bearing:
//
//  1. Serialized. The advisory lock above is taken for the whole transaction,
//     because `CREATE EXTENSION IF NOT EXISTS` races on
//     pg_extension_name_index (see sharedExtensionLockKey).
//  2. Explicitly in `public`. The schema is named (`WITH SCHEMA public`) and
//     deliberately NOT left to search_path: a caller whose search_path starts
//     with a scratch schema would otherwise install the extension inside that
//     schema, where dropping the schema deletes it and where every other
//     schema's concurrent create then races on the same pg_extension row. That
//     is the defect this helper exists to prevent — on a fresh database the
//     first test package to migrate used to claim both extensions into its own
//     scratch schema and every other package then died with 23505.
//  3. Self-healing. A database created before this helper existed may already
//     hold an extension inside a scratch schema. `ALTER EXTENSION ... SET
//     SCHEMA` moves it (both extensions are relocatable), which is what makes
//     property "nil error implies installed in public" true rather than
//     "true unless the database is old". Dependent objects are unaffected:
//     they reference the type by OID, verified end to end on Postgres 16 —
//     tables using CITEXT kept reading and inserting across the move.
//
// log may be nil.
func EnsureSharedExtensions(ctx context.Context, conn ExtensionProvisioner, log *slog.Logger) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin shared-extension provisioning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, sharedExtensionLockKey); err != nil {
		return fmt.Errorf("lock shared extensions: %w", err)
	}

	for _, ext := range SharedExtensions {
		if err := relocateSharedExtension(ctx, tx, ext); err != nil {
			return err
		}
		stmt := `CREATE EXTENSION IF NOT EXISTS ` + pgx.Identifier{ext}.Sanitize() + ` WITH SCHEMA ` + publicSchema
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("ensure extension %s in %s: %w", ext, publicSchema, err)
		}
	}

	// Turn the guarantee into an assertion: everything below is committed only
	// if the invariant actually holds. A future shared extension that cannot be
	// relocated fails loudly here instead of silently changing where it lives.
	ownership, misplaced, err := sharedExtensionPlacement(ctx, tx)
	if err != nil {
		return err
	}
	if len(misplaced) > 0 {
		return fmt.Errorf("shared extensions are not all in %s after provisioning: %s",
			publicSchema, strings.Join(misplaced, ", "))
	}
	if missing := missingSharedExtensions(ownership); len(missing) > 0 {
		return fmt.Errorf("shared extensions missing after provisioning: %s — installed: %s",
			strings.Join(missing, ", "), strings.Join(ownership, ", "))
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit shared-extension provisioning: %w", err)
	}
	if log != nil {
		log.Info("shared_extensions_ready", "schema_ownership", strings.Join(ownership, ","))
	}
	return nil
}

// relocateSharedExtension moves ext into public when it is installed somewhere
// else. A no-op when the extension is absent (CREATE EXTENSION will place it in
// public) or already in public.
func relocateSharedExtension(ctx context.Context, tx pgx.Tx, ext string) error {
	var current string
	err := tx.QueryRow(ctx, `
		SELECT n.nspname
		  FROM pg_extension e
		  JOIN pg_namespace n ON n.oid = e.extnamespace
		 WHERE e.extname = $1`, ext).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read current schema of extension %s: %w", ext, err)
	}
	if current == publicSchema {
		return nil
	}
	stmt := `ALTER EXTENSION ` + pgx.Identifier{ext}.Sanitize() + ` SET SCHEMA ` + publicSchema
	if _, err := tx.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("relocate extension %s from %s to %s: %w", ext, current, publicSchema, err)
	}
	return nil
}

// sharedExtensionPlacement reports "<extname> in <schema>" for each installed
// shared extension, plus the same strings for any that are installed outside
// public.
func sharedExtensionPlacement(ctx context.Context, tx pgx.Tx) (all, misplaced []string, err error) {
	rows, err := tx.Query(ctx, `
		SELECT e.extname, n.nspname
		  FROM pg_extension e
		  JOIN pg_namespace n ON n.oid = e.extnamespace
		 WHERE e.extname = ANY($1)
		 ORDER BY 1`, SharedExtensions)
	if err != nil {
		return nil, nil, fmt.Errorf("read shared-extension placement: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ext, schema string
		if err := rows.Scan(&ext, &schema); err != nil {
			return nil, nil, fmt.Errorf("scan shared-extension placement: %w", err)
		}
		all = append(all, ext+" in "+schema)
		if schema != publicSchema {
			misplaced = append(misplaced, ext+" in "+schema)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate shared-extension placement: %w", err)
	}
	return all, misplaced, nil
}

// missingSharedExtensions returns the shared extensions absent from installed,
// where installed is the "<extname> in <schema>" form returned above.
func missingSharedExtensions(installed []string) []string {
	var missing []string
	for _, ext := range SharedExtensions {
		found := false
		for _, line := range installed {
			if strings.HasPrefix(line, ext+" in ") {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, ext)
		}
	}
	return missing
}
