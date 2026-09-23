// Package testdb — guards that prepareExtensions never issues a statement it
// has no privilege to issue.
//
// The defect these guard against: prepareExtensions used to issue
// `ALTER EXTENSION <ext> SET SCHEMA public` for every installed extension,
// whether or not it was already in `public`. Moving the extension is a semantic
// no-op in that case but NOT a privilege no-op — Postgres requires ownership of
// the extension for ALTER EXTENSION
// (https://www.postgresql.org/docs/current/sql-alterextension.html) — so a
// database whose extensions sit correctly in `public` under a role other than
// the one running ensureSchema failed every scratch-schema preparation with
//
//	ERROR: must be owner of extension citext (SQLSTATE 42501)
//
// where the code without the ALTER passed. Such databases are ordinary:
// `citext` and `pgcrypto` are trusted extensions, so any role holding CREATE on
// a database becomes the owner of whatever it installs (documented on
// https://www.postgresql.org/docs/current/sql-createextension.html), and a
// provisioned or template-derived database can easily be owned by a different
// role than the one that installed them.
//
// The suite cannot see this as superuser — a superuser owns everything — hence
// these guards build the ownership shape explicitly out of throwaway roles, and
// switch identity with `SET SESSION AUTHORIZATION` exactly as this card's
// reproduction does. They need to CREATE ROLE / CREATE DATABASE and to switch
// identity, so they fail with a clear message on a non-superuser connection
// rather than quietly proving nothing.

package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/wzul/jomform/api/internal/db"
)

// ownedDBPrefix names the throwaway databases these guards create; rolePrefix
// names the throwaway roles.
const (
	ownedDBPrefix = "jomform_testdb_own_"
	rolePrefix    = "jf_testdb_own_"
)

// randSuffix returns a short random identifier suffix.
func randSuffix(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("generate suffix: %v", err)
	}
	return hex.EncodeToString(buf)
}

// requireSuperuser fails (not skips) unless the connection may create roles and
// switch identity. Skipping would leave the run green while proving nothing,
// which is the failure mode this whole card is about.
func requireSuperuser(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	var super bool
	if err := conn.QueryRow(ctx, `SELECT rolsuper FROM pg_roles WHERE rolname = current_user`).Scan(&super); err != nil {
		t.Fatalf("read current_user's superuser flag: %v", err)
	}
	if !super {
		t.Fatalf("this guard must build an ownership shape out of throwaway roles, which needs a superuser "+
			"connection (CREATE ROLE + SET SESSION AUTHORIZATION); %s is not one", "current_user")
	}
}

// createThrowawayRole creates a LOGIN role and registers its removal.
func createThrowawayRole(t *testing.T, ctx context.Context, conn *pgx.Conn, dsn, name string) {
	t.Helper()
	if _, err := conn.Exec(ctx, `CREATE ROLE `+quote(name)+` LOGIN`); err != nil {
		t.Fatalf("create throwaway role %s: %v", name, err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c, err := pgx.Connect(dropCtx, dsn)
		if err != nil {
			t.Logf("cleanup: could not connect to drop role %s: %v", name, err)
			return
		}
		defer func() { _ = c.Close(dropCtx) }()
		if _, err := c.Exec(dropCtx, `DROP ROLE IF EXISTS `+quote(name)); err != nil {
			t.Logf("cleanup: could not drop throwaway role %s: %v", name, err)
		}
	})
}

// createOwnedDatabase creates a throwaway database from TEMPLATE template0 and
// owned by owner, then registers its removal.
//
// template0 matters twice over: it inherits no extensions at all, so this guard
// decides their placement itself, and the shape does not depend on whether the
// host's template1 happens to carry them.
func createOwnedDatabase(t *testing.T, ctx context.Context, conn *pgx.Conn, dsn, name, owner string) {
	t.Helper()
	stmt := "CREATE DATABASE " + quote(name) + " TEMPLATE template0 OWNER " + quote(owner)
	if _, err := conn.Exec(ctx, stmt); err != nil {
		t.Fatalf("create throwaway database %s owned by %s: %v", name, owner, err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c, err := pgx.Connect(dropCtx, dsn)
		if err != nil {
			t.Logf("cleanup: could not connect to drop %s: %v", name, err)
			return
		}
		defer func() { _ = c.Close(dropCtx) }()
		q := quote(name)
		if _, err := c.Exec(dropCtx, `DROP DATABASE IF EXISTS `+q+` WITH (FORCE)`); err != nil {
			if _, err2 := c.Exec(dropCtx, `DROP DATABASE IF EXISTS `+q); err2 != nil {
				t.Logf("cleanup: could not drop throwaway database %s: %v", name, err2)
			}
		}
	})
}

// asRole runs fn with the session's identity switched to role, restoring the
// original identity afterwards.
//
// SET SESSION AUTHORIZATION is used instead of a second login on purpose: the
// throwaway roles are created without a password and the host's TCP
// authentication is not trust, so a second connection as that role is not
// available. Switching identity is what the card's own reproduction does, and
// it is what makes the privilege check real — every statement inside fn is
// executed as role, including the extension ownership checks.
func asRole(t *testing.T, ctx context.Context, conn *pgx.Conn, role string, fn func() error) error {
	t.Helper()
	if _, err := conn.Exec(ctx, `SET SESSION AUTHORIZATION `+quote(role)); err != nil {
		t.Fatalf("switch identity to %s: %v", role, err)
	}
	defer func() {
		if _, err := conn.Exec(context.Background(), `RESET SESSION AUTHORIZATION`); err != nil {
			t.Fatalf("restore identity after running as %s: %v", role, err)
		}
	}()
	return fn()
}

// dsnForDatabase returns the connection string with its database name replaced.
func dsnForDatabase(t *testing.T, dsn, database string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse %s: %v", DSNEnv, err)
	}
	u.Path = "/" + database
	return u.String()
}

// schemaOfExtension reports the schema ext is installed in for a connection.
// It is deliberately local to this file: the guard must stand on its own, and
// it is driven on a *pgx.Conn (one session, switching identity) as well as on
// the pool the relocation test uses.
func schemaOfExtension(t *testing.T, ctx context.Context, conn interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, ext string) string {
	t.Helper()
	var schema string
	err := conn.QueryRow(ctx, `
		SELECT n.nspname
		  FROM pg_extension e
		  JOIN pg_namespace n ON n.oid = e.extnamespace
		 WHERE e.extname = $1`, ext).Scan(&schema)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return ""
		}
		t.Fatalf("resolve schema of extension %s: %v", ext, err)
	}
	return schema
}

// TestPrepareExtensionsToleratesAnExtensionItDoesNotOwn is the bite for this
// card, reproducing the measured shape: the database is owned by one role
// (`runner`) and the shared extensions are installed in `public` by a DIFFERENT
// role (`installer`) — legitimate, because citext/pgcrypto are trusted, so any
// role with CREATE on the database owns what it installs.
//
// Pre-fix this fails: prepareExtensions issues `ALTER EXTENSION ... SET SCHEMA
// public` for both extensions, and `runner` does not own them.
//
// The second half is the control that keeps the first half from being vacuous:
// on the SAME database and the SAME connection, an extension that really is
// misplaced must still be dealt with — loudly, naming the extension and both
// schemas — never left behind in silence. A "fix" that simply stopped
// relocating would pass the first half and fail here.
func TestPrepareExtensionsToleratesAnExtensionItDoesNotOwn(t *testing.T) {
	dsn := integrationDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	super, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to %s: %v", DSNEnv, err)
	}
	defer func() { _ = super.Close(ctx) }()
	requireSuperuser(t, ctx, super)

	suffix := randSuffix(t)
	installer := rolePrefix + "in_" + suffix
	runner := rolePrefix + "ru_" + suffix
	// Registered in this order so that LIFO cleanup drops the DATABASE before
	// the roles it depends on.
	createThrowawayRole(t, ctx, super, dsn, installer)
	createThrowawayRole(t, ctx, super, dsn, runner)

	database := ownedDBPrefix + suffix
	createOwnedDatabase(t, ctx, super, dsn, database, runner)
	dbDSN := dsnForDatabase(t, dsn, database)

	admin, err := pgx.Connect(ctx, dbDSN)
	if err != nil {
		t.Fatalf("connect to throwaway database %s: %v", database, err)
	}
	defer func() { _ = admin.Close(ctx) }()

	// The installer needs CREATE on the database (what a trusted extension
	// requires) plus CREATE/USAGE on public, where its objects land.
	for _, role := range []string{installer, runner} {
		for _, stmt := range []string{
			`GRANT CREATE ON DATABASE ` + quote(database) + ` TO ` + quote(role),
			`GRANT CREATE, USAGE ON SCHEMA public TO ` + quote(role),
		} {
			if _, err := admin.Exec(ctx, stmt); err != nil {
				t.Fatalf("grant to %s: %v", role, err)
			}
		}
	}

	// The installer — and only the installer — creates the extensions in public.
	if err := asRole(t, ctx, admin, installer, func() error {
		for _, ext := range publicExtensions {
			if _, err := admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS `+quote(ext)+` SCHEMA `+quote(publicSchema)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("installer %s could not create the extensions in %s: %v", installer, publicSchema, err)
	}

	// The precondition the guard depends on: present, in public, owned by a
	// role other than the runner. If any of that is false the test proves
	// nothing, so it fails rather than proceeding.
	for _, ext := range publicExtensions {
		var owner string
		if err := admin.QueryRow(ctx, `
			SELECT pg_get_userbyid(e.extowner)
			  FROM pg_extension e
			 WHERE e.extname = $1`, ext).Scan(&owner); err != nil {
			t.Fatalf("read owner of extension %s: %v", ext, err)
		}
		if owner != installer {
			t.Fatalf("setup failed: %s is owned by %q, want the installer %q", ext, owner, installer)
		}
		if got := schemaOfExtension(t, ctx, admin, ext); got != publicSchema {
			t.Fatalf("setup failed: %s is in %q, want %q", ext, got, publicSchema)
		}
	}

	// The card's acceptance: a correctly-placed extension owned by someone else
	// must not cost the runner a statement it cannot execute.
	err = asRole(t, ctx, admin, runner, func() error { return prepareExtensions(ctx, admin) })
	if err != nil {
		t.Fatalf("prepareExtensions as the database owner %s, whose extensions are owned by %s: %v\n\n"+
			"This is the unconditional-ALTER defect: SET SCHEMA is a semantic no-op when the extension "+
			"is already in public, but ALTER EXTENSION still requires owning it.",
			runner, installer, err)
	}
	for _, ext := range publicExtensions {
		if got := schemaOfExtension(t, ctx, admin, ext); got != publicSchema {
			t.Errorf("extension %s moved to %q, want %q", ext, got, publicSchema)
		}
	}

	// Control: an extension that really is misplaced on this same database must
	// still be dealt with. The runner cannot legally move it, so the honest
	// outcome is a loud, actionable failure naming the extension and both
	// schemas — never a silent success leaving the database mis-shaped.
	//
	// The misplaced state is planted as the superuser because relocation needs
	// ownership of the extension AND of its contained objects, and a trusted
	// non-superuser install leaves the contained type owned by the bootstrap
	// superuser (measured: `ALTER EXTENSION citext SET SCHEMA x` as the
	// installing non-superuser fails with `must be owner of type citext`).
	// A stranded extension is therefore always planted by a superuser.
	misplaced := "test_misplaced_" + suffix
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+quote(misplaced)); err != nil {
		t.Fatalf("create scratch schema %s: %v", misplaced, err)
	}
	if _, err := admin.Exec(ctx, `ALTER EXTENSION `+quote("citext")+` SET SCHEMA `+quote(misplaced)); err != nil {
		t.Fatalf("plant citext in %s: %v", misplaced, err)
	}

	err = asRole(t, ctx, admin, runner, func() error { return prepareExtensions(ctx, admin) })
	if err == nil {
		t.Errorf("prepareExtensions succeeded while citext was stranded in %s under another owner; "+
			"a repair that quietly does nothing leaves the database mis-shaped", misplaced)
	} else {
		msg := err.Error()
		for _, want := range []string{"citext", misplaced, publicSchema} {
			if !strings.Contains(msg, want) {
				t.Errorf("failure is not actionable: it must name the extension, the schema it is in and the "+
					"schema it should be in; %q is missing from %q", want, msg)
			}
		}
	}
	// The failed move must not have been applied, and the correctly-placed
	// extension must not have been disturbed by the failing run.
	if got := schemaOfExtension(t, ctx, admin, "citext"); got != misplaced {
		t.Errorf("citext is in %q after a failed relocation, want it left in %q", got, misplaced)
	}
	if got := schemaOfExtension(t, ctx, admin, "pgcrypto"); got != publicSchema {
		t.Errorf("pgcrypto is in %q after an unrelated relocation failed, want %q", got, publicSchema)
	}
}

// TestPrepareExtensionsStillRelocatesWhenItOwnsTheExtension is the other half of
// the card's acceptance, kept next to the ownership guard so that a change which
// stops relocating cannot pass both: with the extension in a scratch schema and
// owned by the connection running prepareExtensions, the move must still happen.
func TestPrepareExtensionsStillRelocatesWhenItOwnsTheExtension(t *testing.T) {
	dsn := integrationDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	name := createScratchDB(t, ctx, dsn)
	dbDSN := dsnFor(t, dsn, name)

	admin, err := db.Connect(ctx, dbDSN)
	if err != nil {
		t.Fatalf("connect to throwaway database: %v", err)
	}
	defer admin.Close()

	// Plant the pre-fix state: a scratch schema holding both extensions, with
	// the same role (this connection) owning them, so the relocation is legal.
	scratch := "test_owned_scratch"
	stmt := "CREATE SCHEMA " + quote(scratch) + ";\n" +
		"SET search_path=" + quote(scratch) + ",public;\n" +
		"CREATE EXTENSION IF NOT EXISTS citext;\n" +
		"CREATE EXTENSION IF NOT EXISTS pgcrypto;\n"
	if _, err := admin.Exec(ctx, stmt); err != nil {
		t.Fatalf("plant the stranded state: %v", err)
	}
	for _, ext := range publicExtensions {
		if got := schemaOfExtension(t, ctx, admin, ext); got != scratch {
			t.Fatalf("setup failed: %s is in %q, want %q", ext, got, scratch)
		}
	}

	if err := prepareExtensions(ctx, admin); err != nil {
		t.Fatalf("relocate extensions this connection owns: %v", err)
	}
	for _, ext := range publicExtensions {
		if got := schemaOfExtension(t, ctx, admin, ext); got != publicSchema {
			t.Errorf("extension %s is in %q after preparation, want %q", ext, got, publicSchema)
		}
	}
}
