package db

// The passkey AAGUID down migration (000075) must not throw a stored AAGUID away.
//
// WHY THIS FILE EXISTS. Down files are never executed by the forward-only runner
// (migrate.go), so nothing in the suite reads one, and a down file can destroy
// data while every gate stays green. This one did: it converted the column with
// encode(aaguid,'escape') — an exactly reversible spelling of a bytea — and
// carried an unconditional
//
//     UPDATE passkeys SET aaguid = '\x'::bytea;
//
// above it, on the stated grounds that a value whose bytes are not valid UTF-8
// "cannot be expressed as text at all". The reason is false (the very expression
// the next line uses is reversible) and the UPDATE was the damage: it emptied
// EVERY row, the all-ASCII and all-zero ones included. Measured against a real
// Postgres before the fix: 0 of 4 rows preserved — i.e. a rollback wiped the
// authenticator identity of every registered passkey, and no roll-forward could
// reconstruct it.
//
// WHAT IS ASSERTED. The real runner builds the schema in a scratch schema this
// process owns; rows are seeded with the shapes a live system actually holds (a
// browser's AAGUID carrying bytes >= 0x80, an all-ASCII one, the spec's all-zero
// none-attestation one, one made of backslashes, and a pre-bytea legacy value);
// then the SHIPPED down file is executed verbatim — comments and all, exactly as
// a human running a rollback would. Every row must survive, and its bytes must
// come back byte-exact through decode(aaguid,'escape'). Re-adding the UPDATE
// takes that to 0 of 5, so this test is what keeps it out.
//
// The second phase pins the rest of the rollback CONTRACT the down file states:
// a later roll-forward is byte-exact only for rows whose text is already their
// own byte spelling. That is asserted as the general predicate
// `restored == (text == raw bytes)` — not as a count — so the file's comment
// cannot rot into a claim nobody checks.

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPasskeyAAGUIDDownMigrationKeepsEveryStoredAAGUID is the guard described above.
func TestPasskeyAAGUIDDownMigrationKeepsEveryStoredAAGUID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dsn := os.Getenv("JOMFORM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping passkey AAGUID down-migration test")
	}

	pool := scratchSchemaPool(t, ctx, dsn)
	// The real boot path: every migration, including 000075 up.
	if err := Migrate(ctx, pool, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	userID := seedUser(t, ctx, pool)
	// id -> the exact bytes the AAGUID column holds. The five shapes matter: the
	// first two are what a real authenticator and a virtual one send, the third
	// is the spec's none-attestation value, the fourth is the one byte that the
	// escape spelling has to encode, and the fifth is a legacy pre-bytea value
	// (all a TEXT column could ever hold).
	seeded := []struct {
		credID []byte
		aaguid string // hex
	}{
		{[]byte{0x11}, "adce000235bcc60a648b0b25f1f05503"}, // Chrome's real AAGUID, byte 0xad first
		{[]byte{0x12}, "01020304050607080102030405060708"}, // all-ASCII
		{[]byte{0x13}, "00000000000000000000000000000000"}, // spec: none-attestation
		{[]byte{0x14}, "5c5c5c5c5c5c5c5c5c5c5c5c5c5c5c5c"}, // backslashes
		{[]byte{0x15}, hex.EncodeToString([]byte("01020304050607080102030405060708"))},
	}
	for _, s := range seeded {
		raw, err := hex.DecodeString(s.aaguid)
		if err != nil {
			t.Fatalf("bad fixture %q: %v", s.aaguid, err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO passkeys (id, user_id, public_key, aaguid) VALUES ($1,$2,$3,$4)`,
			s.credID, userID, []byte{0x01}, raw); err != nil {
			t.Fatalf("seed passkey %s: %v", s.aaguid, err)
		}
	}

	before := readPasskeysHex(t, ctx, pool, `encode(aaguid,'hex')`)
	if len(before) != len(seeded) {
		t.Fatalf("premise broken: %d passkey rows visible, want %d — the assertions below "+
			"would not be about the rows this test seeded", len(before), len(seeded))
	}

	// ── the down file, verbatim ──────────────────────────────────────────────
	if _, err := pool.Exec(ctx, migrationFile(t, "000075_passkey_aaguid_bytea.down.sql")); err != nil {
		t.Fatalf("the shipped down file did not apply: %v", err)
	}

	// It really did revert the type (and the default 000013 had).
	var dataType, colDefault string
	if err := pool.QueryRow(ctx, `
		SELECT data_type, column_default FROM information_schema.columns
		 WHERE table_schema = current_schema() AND table_name = 'passkeys'
		   AND column_name = 'aaguid'`).Scan(&dataType, &colDefault); err != nil {
		t.Fatalf("read aaguid column after the down file: %v", err)
	}
	if dataType != "text" {
		t.Errorf("aaguid column is %s after the down file, want text", dataType)
	}
	if colDefault != "''::text" {
		t.Errorf("aaguid default is %q after the down file, want ''::text", colDefault)
	}

	// Every row survived, and every row's bytes are recoverable.
	recovered := readPasskeysHex(t, ctx, pool, `encode(decode(aaguid,'escape'),'hex')`)
	downText := readPasskeysHex(t, ctx, pool, `encode(convert_to(aaguid,'UTF8'),'hex')`)
	preserved := 0
	for id, want := range before {
		got, ok := recovered[id]
		if !ok {
			t.Errorf("passkey %s is GONE after the down migration: a rollback discarded a "+
				"registered authenticator's identity", id)
			continue
		}
		if got != want {
			t.Errorf("passkey %s: AAGUID came back as %s, want %s (text now holds %q)",
				id, got, want, decodeHexToText(t, downText[id]))
			continue
		}
		preserved++
	}
	if preserved != len(before) {
		t.Fatalf("the down migration preserved %d of %d AAGUIDs; it must preserve all of "+
			"them — dropping the `UPDATE passkeys SET aaguid = '\\x'` line is what makes "+
			"this pass", preserved, len(before))
	}

	// ── the rest of the contract: what a later roll-forward restores ─────────
	if _, err := pool.Exec(ctx, migrationFile(t, "000075_passkey_aaguid_bytea.up.sql")); err != nil {
		t.Fatalf("re-applying the up file after a rollback must be safe: %v", err)
	}
	afterUp := readPasskeysHex(t, ctx, pool, `encode(aaguid,'hex')`)

	rawSpelling, escaped := 0, 0
	for id, want := range before {
		restored := afterUp[id] == want
		// The up file converts with convert_to(aaguid,'UTF8'), which is exact
		// precisely when the text column already held the row's own byte spelling.
		wantRestored := downText[id] == want
		if restored != wantRestored {
			t.Errorf("passkey %s: after a roll-forward the AAGUID was restored=%v, but its text "+
				"was the raw byte spelling=%v — the down file's rollback contract is wrong",
				id, restored, wantRestored)
		}
		if wantRestored {
			rawSpelling++
		} else {
			escaped++
		}
	}
	if rawSpelling == 0 || escaped == 0 {
		t.Fatalf("fixture does not discriminate: %d raw-spelling rows and %d escaped rows; the "+
			"contract assertion above proves nothing unless both shapes are present",
			rawSpelling, escaped)
	}
}

// scratchSchemaPool returns a pool whose unqualified names resolve to a scratch
// schema this test process owns inside the shared test database, with that schema
// dropped on cleanup. It is internal/testdb's mechanism (a schema per pid, so
// concurrent `go test` binaries cannot tear each other down) rather than a
// per-card database — this package cannot import testdb, which imports it.
func scratchSchemaPool(t *testing.T, ctx context.Context, dsn string) *pgxpool.Pool {
	t.Helper()
	schema := fmt.Sprintf("jf_aaguid_down_p%d", os.Getpid())

	admin, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	if _, err := admin.Exec(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE`); err != nil {
		t.Fatalf("drop scratch schema %s: %v", schema, err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create scratch schema %s: %v", schema, err)
	}
	admin.Close()

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse test database url: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping test database: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		dropCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cleaner, err := Connect(dropCtx, dsn)
		if err != nil {
			t.Logf("cleanup: could not connect to drop scratch schema %s: %v", schema, err)
			return
		}
		defer cleaner.Close()
		if _, err := cleaner.Exec(dropCtx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE`); err != nil {
			t.Logf("cleanup: could not drop scratch schema %s: %v", schema, err)
		}
	})
	return pool
}

// readPasskeysHex returns the passkeys table as credential-id -> the hex of expr,
// where expr is evaluated against the aaguid column in whatever type it holds.
func readPasskeysHex(t *testing.T, ctx context.Context, pool *pgxpool.Pool, expr string) map[string]string {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT encode(id,'hex'), `+expr+` FROM passkeys`)
	if err != nil {
		t.Fatalf("read passkeys (%s): %v", expr, err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, value string
		if err := rows.Scan(&id, &value); err != nil {
			t.Fatalf("scan passkeys (%s): %v", expr, err)
		}
		out[id] = value
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read passkeys (%s): %v", expr, err)
	}
	return out
}

// decodeHexToText renders a hex string for a failure message, so a mismatch shows
// what the text column actually holds.
func decodeHexToText(t *testing.T, hexValue string) string {
	t.Helper()
	b, err := hex.DecodeString(hexValue)
	if err != nil {
		return hexValue
	}
	return string(b)
}
