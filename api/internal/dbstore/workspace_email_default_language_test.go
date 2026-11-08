package dbstore

// Card t_281831eb — a new workspace's receipt subject/body must not be born in
// one language.
//
// WHAT THIS PINS
//
//  1. A workspace whose email settings row has just been materialised holds an
//     EMPTY subject and body, i.e. "not customised". Before migration 000083 the
//     column DEFAULT was the Malay rendering of the receipt (000021), so every
//     workspace — English, Chinese, Tamil, Arabic — stored Malay in a field the
//     dashboard renders verbatim, and an English merchant saw Malay in a field
//     he never filled in.
//  2. The 000083 backfill blanks ONLY a half that is byte-for-byte the
//     pre-language literal, per field. A merchant's own subject/body survives
//     verbatim, including the case where one half was edited and the other was
//     left alone (the settings page posts both halves back on save).
//  3. An empty stored half resolves to JomForm's receipt in the BUYER's form
//     language, so an English form gets an English receipt and a Malay form a
//     Malay one, with nothing stored per workspace.
//
// The backfill cases EXECUTE the shipped migration file (read through
// db.MigrationStatements, the same embed the API applies at boot) rather than a
// copy of its SQL: if the literal in the migration stops matching
// emailtext.LegacyDefault by one byte, the "still holds the default" row is not
// rewritten and case 2 goes RED.
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/db"
	"github.com/wzul/jomform/api/internal/emailtext"
)

// emailSettingsFixture creates one workspace and returns its id, so each case
// owns the single workspace_email_settings row it asserts on.
func emailSettingsFixture(t *testing.T, ctx context.Context, p *pgxpool.Pool, tag string) string {
	t.Helper()
	user := testOwnerUser(t, ctx, p)
	w, err := NewWorkspaceStore(p).Create(ctx, user, "Receipt "+tag, "it-email-"+tag+"-"+user[:8])
	if err != nil {
		t.Fatalf("create workspace %q: %v", tag, err)
	}
	return w.ID
}

// readEmailRow returns the stored subject/body for a workspace, asserting the
// row exists (a fixture bug must not read as "empty settings").
func readEmailRow(t *testing.T, ctx context.Context, p *pgxpool.Pool, wsID string) (subject, body string) {
	t.Helper()
	if err := p.QueryRow(ctx,
		`SELECT receipt_subject, receipt_body FROM workspace_email_settings WHERE workspace_id=$1`,
		wsID).Scan(&subject, &body); err != nil {
		t.Fatalf("read workspace_email_settings for %s: %v", wsID, err)
	}
	return subject, body
}

// TestNewWorkspaceStoresNoReceiptTemplate is the card's core: a freshly
// materialised workspace stores NOTHING, so no language is pinned in the row and
// the dashboard has nothing Malay to render on an English page.
func TestNewWorkspaceStoresNoReceiptTemplate(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	wsID := emailSettingsFixture(t, ctx, p, "new")
	ws := NewWorkspaceStore(p)

	// First read: a workspace with no stored row answers the defaults in memory
	// and writes nothing (the read-never-materialises contract; the dedicated
	// guard lives in workspace_email_read_no_write_test.go).
	got, err := ws.EmailSettings(ctx, wsID)
	if err != nil {
		t.Fatalf("EmailSettings: %v", err)
	}
	if got.ReceiptSubject != "" || got.ReceiptBody != "" {
		t.Errorf("a new workspace's stored receipt is not empty: subject=%q body=%q — a column DEFAULT "+
			"is pinning one language into a field the dashboard renders verbatim", got.ReceiptSubject, got.ReceiptBody)
	}
	// A read no longer materialises the row (a GET must never write), so the
	// row is created where a write does: a bare INSERT that takes the column
	// DEFAULTs — the value every workspace is born with, and the one 000083
	// emptied.
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM workspace_email_settings WHERE workspace_id=$1`, wsID); n != 0 {
		t.Fatalf("reading the settings materialised a row: rows=%d, want 0 (a read must not write)", n)
	}
	if _, err := p.Exec(ctx,
		`INSERT INTO workspace_email_settings (workspace_id) VALUES ($1)`, wsID); err != nil {
		t.Fatalf("materialise the email settings row: %v", err)
	}
	// The row really exists now, and a SECOND read (the one that used to return
	// the Malay column DEFAULT) is empty too.
	if n := countRows(t, ctx, p, `SELECT COUNT(*) FROM workspace_email_settings WHERE workspace_id=$1`, wsID); n != 1 {
		t.Fatalf("workspace_email_settings rows for a new workspace = %d, want 1 (row written explicitly)", n)
	}
	again, err := ws.EmailSettings(ctx, wsID)
	if err != nil {
		t.Fatalf("EmailSettings (second read): %v", err)
	}
	if again.ReceiptSubject != "" || again.ReceiptBody != "" {
		t.Errorf("a materialised workspace row holds a stored receipt template: subject=%q body=%q",
			again.ReceiptSubject, again.ReceiptBody)
	}

	// And the column DEFAULT itself is empty: an INSERT that names only the
	// workspace must not inherit a language either.
	var defSubject, defBody string
	if err := p.QueryRow(ctx,
		`INSERT INTO workspace_email_settings (workspace_id) VALUES ($1)
		 ON CONFLICT (workspace_id) DO UPDATE SET updated_at=now()
		 RETURNING receipt_subject, receipt_body`,
		emailSettingsFixture(t, ctx, p, "coldef")).Scan(&defSubject, &defBody); err != nil {
		t.Fatalf("insert with column defaults: %v", err)
	}
	if defSubject != "" || defBody != "" {
		t.Errorf("workspace_email_settings column DEFAULT still carries text: subject=%q body=%q", defSubject, defBody)
	}
}

// TestEmailSettingsBackfillClearsOnlyTheSystemDefault executes the SHIPPED 000083
// migration over rows that simulate a pre-upgrade database.
func TestEmailSettingsBackfillClearsOnlyTheSystemDefault(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	legacySubject, legacyBody := emailtext.LegacyDefault(emailtext.Receipt)
	if legacySubject == "" || legacyBody == "" {
		t.Fatal("test setup: the receipt legacy default is missing from the dictionary")
	}

	const merchantSubject = "Your order is confirmed"
	const merchantBody = "Thanks! Keep this note."

	// A never-customised row (what 000021's DEFAULT wrote into every workspace).
	untouched := emailSettingsFixture(t, ctx, p, "legacy")
	if _, err := p.Exec(ctx,
		`INSERT INTO workspace_email_settings (workspace_id, receipt_subject, receipt_body) VALUES ($1,$2,$3)`,
		untouched, legacySubject, legacyBody); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	// A fully customised row: both halves are the merchant's words.
	custom := emailSettingsFixture(t, ctx, p, "custom")
	if _, err := p.Exec(ctx,
		`INSERT INTO workspace_email_settings (workspace_id, receipt_subject, receipt_body) VALUES ($1,$2,$3)`,
		custom, merchantSubject, merchantBody); err != nil {
		t.Fatalf("seed custom row: %v", err)
	}
	// The reachable half-edited shape: the settings page posts both halves back,
	// so a merchant who edited only the body stores the system subject verbatim.
	half := emailSettingsFixture(t, ctx, p, "half")
	if _, err := p.Exec(ctx,
		`INSERT INTO workspace_email_settings (workspace_id, receipt_subject, receipt_body) VALUES ($1,$2,$3)`,
		half, legacySubject, merchantBody); err != nil {
		t.Fatalf("seed half-edited row: %v", err)
	}

	// Apply migration 000083 exactly as the API would at boot.
	migrations, err := db.MigrationStatements()
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	const want = "000083_email_settings_default_language_neutral.up.sql"
	var sql string
	for _, m := range migrations {
		if m.Name == want {
			sql = m.SQL
			break
		}
	}
	if sql == "" {
		t.Fatalf("%s is not in the embedded migration set — the backfill never runs in production either", want)
	}
	if _, err := p.Exec(ctx, sql); err != nil {
		t.Fatalf("apply %s: %v", want, err)
	}

	// 1. The untouched row is emptied — the system's own text is not content.
	if s, b := readEmailRow(t, ctx, p, untouched); s != "" || b != "" {
		t.Errorf("the backfill left the pre-language default in place: subject=%q body=%q", s, b)
	}
	// 2. A merchant's wording is never rewritten.
	if s, b := readEmailRow(t, ctx, p, custom); s != merchantSubject || b != merchantBody {
		t.Errorf("the backfill rewrote merchant copy: subject=%q body=%q, want %q/%q",
			s, b, merchantSubject, merchantBody)
	}
	// 3. Judged per half: the system subject goes, the merchant body stays.
	if s, b := readEmailRow(t, ctx, p, half); s != "" || b != merchantBody {
		t.Errorf("the backfill did not judge each half on its own: subject=%q body=%q, want \"\"/%q",
			s, b, merchantBody)
	}

	// 4. What a buyer now receives: the empty halves follow the form language,
	// and the merchant's body is still his own words in every language.
	for _, lang := range []string{emailtext.En, emailtext.Ms} {
		st, err := NewWorkspaceStore(p).EmailSettings(ctx, untouched)
		if err != nil {
			t.Fatalf("EmailSettings after backfill: %v", err)
		}
		effective := ResolveFormEmailSettings(nil, st, lang)
		def := emailtext.Text(lang, emailtext.Receipt, nil)
		if effective.ReceiptSubject != def.Subject || effective.ReceiptBody != def.Body {
			t.Errorf("lang=%s: an empty stored receipt did not resolve to the catalogue's receipt: "+
				"subject=%q body=%q", lang, effective.ReceiptSubject, effective.ReceiptBody)
		}
		if lang == emailtext.En && strings.Contains(effective.ReceiptSubject, "Resit pembayaran") {
			t.Errorf("lang=en: the resolved receipt subject is Malay: %q", effective.ReceiptSubject)
		}
		if lang == emailtext.Ms && !strings.Contains(effective.ReceiptSubject, "Resit pembayaran") {
			t.Errorf("lang=ms: the resolved receipt subject is not Malay: %q", effective.ReceiptSubject)
		}
	}
	st, err := NewWorkspaceStore(p).EmailSettings(ctx, custom)
	if err != nil {
		t.Fatalf("EmailSettings (custom row): %v", err)
	}
	for _, lang := range []string{emailtext.En, emailtext.Ms} {
		if got := ResolveFormEmailSettings(nil, st, lang); got.ReceiptSubject != merchantSubject || got.ReceiptBody != merchantBody {
			t.Errorf("lang=%s: merchant copy was translated or dropped: subject=%q body=%q",
				lang, got.ReceiptSubject, got.ReceiptBody)
		}
	}
}
