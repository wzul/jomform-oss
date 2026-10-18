package dbstore

// Card t_0da81d94 — the built-in default confirmation is a DICTIONARY lookup,
// not prose the server owns, and migration 000076 carries the rows the old
// constant left behind to that shape.
//
// Why a migration and not only a code change: the BM sentence was PERSISTED. Every
// form's confirmation set gains a default row on first read, written from
// DefaultConfirmationMessage, and the submit / thank-you paths served that stored
// message to the buyer — so the renderer's localized fallback (Ls.ResponseReceived)
// was dead code for every form that had ever been read. Clearing the constant alone
// would have fixed only forms created after it; the prose already in the table keeps
// being served until something moves it.
//
// These tests execute the SHIPPED migration files (the embedded up file through
// db.MigrationStatements, the down file read verbatim from disk, as a human running a
// rollback would) against rows seeded in the shapes a live system holds, and assert
// what each one may and may not touch.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/db"
)

const (
	migrationUp0DA81D94   = "000076_form_confirmation_default_language.up.sql"
	migrationDown0DA81D94 = "000076_form_confirmation_default_language.down.sql"
)

// legacyDefaultConfirmationMessage is the sentence the shipped constant carried.
// Spelled out here (not read from the code) so this file fails if that prose is
// reachable again — as a constant, a fallback or a literal.
const legacyDefaultConfirmationMessage = "Terima kasih! Maklum balas anda telah diterima."

// legacyDefaultFailureMessage is the same ship for a failed payment.
const legacyDefaultFailureMessage = "Pembayaran gagal. Sila cuba lagi."

// TestDefaultConfirmationCarriesNoServerProse pins the constants themselves: the
// built-in default is "not merchant-authored" (empty) and resolved per locale, so
// nothing may write a sentence into these.
func TestDefaultConfirmationCarriesNoServerProse(t *testing.T) {
	if DefaultConfirmationMessage != "" {
		t.Errorf("DefaultConfirmationMessage = %q, want empty — the built-in default is a "+
			"dictionary lookup on the buyer's page (Ls.ResponseReceived), and this value is "+
			"PERSISTED into every form's default confirmation row, so prose here reaches every "+
			"locale in one language", DefaultConfirmationMessage)
	}
	if DefaultFailureMessage != "" {
		t.Errorf("DefaultFailureMessage = %q, want empty — the built-in failure body is "+
			"Ls.PaymentFailedBody, resolved on the thank-you page in the buyer's language",
			DefaultFailureMessage)
	}
	if def := DefaultConfirmation("00000000-0000-0000-0000-000000000000"); def.Message != "" {
		t.Errorf("the default confirmation carries message %q; it must carry no prose", def.Message)
	}
}

// TestDefaultConfirmationLanguageMigrationNormalisesLegacyRows executes 000076
// against a table holding every shape it can meet: the legacy built-in default
// (must be emptied), a merchant-authored default and a merchant-authored NAMED
// confirmation carrying that same sentence (both must survive), and a default row
// that is already empty (a no-op). It then re-applies the up file (idempotence)
// and runs the down file, which must restore only what the up file changed.
func TestDefaultConfirmationLanguageMigrationNormalisesLegacyRows(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)
	formID := seedConfirmationForm(t, ctx, p, acc)

	legacyDefault := seedConfirmation(t, ctx, p, formID, "Default Confirmation", true, legacyDefaultConfirmationMessage, 0)
	merchantDefault := seedConfirmation(t, ctx, p, formID, "Default Confirmation", true, "Resit akan dihantar dalam 3 hari.", 1)
	merchantNamed := seedConfirmation(t, ctx, p, formID, "Terima kasih khas", false, legacyDefaultConfirmationMessage, 2)
	alreadyEmpty := seedConfirmation(t, ctx, p, formID, "Default Confirmation", true, "", 3)

	// PREMISE: the defect is in the table before the migration — the server's BM
	// sentence is stored on the built-in default. Without this, a green result
	// below could mean the migration was never needed.
	if got := confirmationMessage(t, ctx, p, legacyDefault); got != legacyDefaultConfirmationMessage {
		t.Fatalf("premise broken: the legacy default row holds %q before the migration, so this "+
			"test proves nothing about moving it", got)
	}

	upSQL := embeddedMigrationSQL(t, migrationUp0DA81D94)
	if _, err := p.Exec(ctx, upSQL); err != nil {
		t.Fatalf("apply %s: %v", migrationUp0DA81D94, err)
	}

	if got := confirmationMessage(t, ctx, p, legacyDefault); got != "" {
		t.Errorf("the built-in default still holds %q after %s — the server's BM sentence stays "+
			"in the table and keeps being served to every locale",
			got, migrationUp0DA81D94)
	}
	if got := confirmationMessage(t, ctx, p, merchantDefault); got != "Resit akan dihantar dalam 3 hari." {
		t.Errorf("a merchant-authored DEFAULT message became %q — the migration may only touch the "+
			"shipped sentence, never a merchant's own words", got)
	}
	if got := confirmationMessage(t, ctx, p, merchantNamed); got != legacyDefaultConfirmationMessage {
		t.Errorf("a merchant-authored NAMED confirmation carrying that same sentence became %q — "+
			"is_default is what marks the built-in row, and a named one is the merchant's text", got)
	}
	if got := confirmationMessage(t, ctx, p, alreadyEmpty); got != "" {
		t.Errorf("an already-empty default became %q", got)
	}

	// Idempotent: a second pass (what a re-run, or a lane that re-applies the file
	// by hand, would do) changes nothing.
	if _, err := p.Exec(ctx, upSQL); err != nil {
		t.Fatalf("re-apply %s must be safe: %v", migrationUp0DA81D94, err)
	}
	if got := confirmationMessage(t, ctx, p, merchantDefault); got != "Resit akan dihantar dalam 3 hari." {
		t.Errorf("the second pass changed a merchant message to %q", got)
	}

	// The DOWN file, executed verbatim as a rollback would be. It may only undo
	// what the up file did — and it is the stated rollback contract, so it is
	// checked rather than trusted (down files are never executed by the
	// forward-only runner).
	downSQL := shippedMigrationFile(t, migrationDown0DA81D94)
	if _, err := p.Exec(ctx, downSQL); err != nil {
		t.Fatalf("apply %s: %v", migrationDown0DA81D94, err)
	}
	if got := confirmationMessage(t, ctx, p, legacyDefault); got != legacyDefaultConfirmationMessage {
		t.Errorf("after the down migration the built-in default holds %q, want the pre-fix sentence "+
			"back %q", got, legacyDefaultConfirmationMessage)
	}
	if got := confirmationMessage(t, ctx, p, merchantDefault); got != "Resit akan dihantar dalam 3 hari." {
		t.Errorf("the down migration overwrote a merchant's own message with %q", got)
	}
	if got := confirmationMessage(t, ctx, p, merchantNamed); got != legacyDefaultConfirmationMessage {
		t.Errorf("the down migration changed a named confirmation to %q", got)
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

// embeddedMigrationSQL returns the SQL of an UP migration from the embedded set —
// the same bytes the runner applies at boot.
func embeddedMigrationSQL(t *testing.T, name string) string {
	t.Helper()
	migrations, err := db.MigrationStatements()
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	for _, m := range migrations {
		if m.Name == name {
			return m.SQL
		}
	}
	t.Fatalf("migration %s not found in the embedded set", name)
	return ""
}

// shippedMigrationFile reads a migration file verbatim from disk. DOWN files are
// not part of the embedded up-only set (the runner is forward-only), so this is
// the only way to execute the shipped rollback rather than a copy of it.
func shippedMigrationFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "db", "migrations", name))
	if err != nil {
		t.Fatalf("read ../db/migrations/%s: %v", name, err)
	}
	return string(b)
}

func seedConfirmationForm(t *testing.T, ctx context.Context, p *pgxpool.Pool, accountID string) string {
	t.Helper()
	var id string
	if err := p.QueryRow(ctx,
		`INSERT INTO forms (account_id, slug, title, config, status)
		 VALUES ($1, 'it-conf-' || md5(random()::text), 'IT confirmation form',
		         '{"mode":"response"}'::jsonb, 'published')
		 RETURNING id`, accountID).Scan(&id); err != nil {
		t.Fatalf("create form: %v", err)
	}
	t.Cleanup(func() {
		_, _ = p.Exec(context.Background(), `DELETE FROM forms WHERE id=$1`, id)
	})
	return id
}

func seedConfirmation(t *testing.T, ctx context.Context, p *pgxpool.Pool,
	formID, name string, isDefault bool, message string, position int,
) string {
	t.Helper()
	var id string
	if err := p.QueryRow(ctx,
		`INSERT INTO form_confirmations (form_id, name, type, message, is_default, position)
		 VALUES ($1, $2, 'message', $3, $4, $5) RETURNING id`,
		formID, name, message, isDefault, position).Scan(&id); err != nil {
		t.Fatalf("seed confirmation %q: %v", name, err)
	}
	return id
}

func confirmationMessage(t *testing.T, ctx context.Context, p *pgxpool.Pool, id string) string {
	t.Helper()
	var msg string
	if err := p.QueryRow(ctx,
		`SELECT message FROM form_confirmations WHERE id=$1`, id).Scan(&msg); err != nil {
		t.Fatalf("read confirmation %s: %v", id, err)
	}
	return msg
}
