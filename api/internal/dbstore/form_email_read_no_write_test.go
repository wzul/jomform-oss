package dbstore

// A READ of the per-form email settings must not WRITE the table (card
// t_3c67947d).
//
// THE DEFECT, measured on prod v1.23.7. Opening a form's settings page and
// reading the Notifications panel issued exactly one request —
// `GET /v1/rest/forms/{id}/email-settings` — with no PUT/DELETE anywhere, and
// `form_email_settings` still gained a row of defaults in that same second.
// `FormStore.FormEmailSettings` treated EVERY error from its SELECT as "no row"
// and then ran `INSERT INTO form_email_settings (form_id) VALUES ($1) ON
// CONFLICT DO NOTHING`, so a page load was a write. Two consequences:
//
//   - `updated_at` can no longer answer "did this merchant edit their email
//     settings?" — every page view looks like an edit; and
//   - the error branch is unreachable AS an error: a dropped connection or a
//     renamed column is indistinguishable from "no row" and is answered with
//     the defaults, HTTP 200.
//
// WHAT THIS PINS
//  1. Reading a form with no stored row leaves the row count unchanged and
//     still answers the defaults the editor needs.
//  2. The row is created by the FIRST WRITE (SaveFormEmailSettings, which the
//     PUT handler calls) — exactly one row — and a later read neither adds nor
//     overwrites it.
//  3. A REAL database error is returned as an error, not disguised as "no row
//     yet". Proven against a stub table that is missing the columns the SELECT
//     reads but accepts the old INSERT: before the fix that call answered
//     defaults with a nil error.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when unset.

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/testdb"
)

// formEmailFixture seeds a user + workspace + account + form and returns the
// form id. It deliberately creates NO form_email_settings row: the "never
// visited" state the card is about.
func formEmailFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var userID, wsID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-esread-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-esread-ws', 'it-esread-' || md5(random()::text)) RETURNING id`, userID).Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'it-esread-acc', 'it-esread-' || md5(random()::text), $2) RETURNING id`,
		userID, wsID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	var formID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO forms (account_id, workspace_id, slug, title)
		 VALUES ($1, $2, 'it-esread-' || md5(random()::text), 'Email settings read') RETURNING id`,
		accountID, wsID).Scan(&formID); err != nil {
		t.Fatalf("create form: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM forms WHERE id=$1`, formID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, accountID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspaces WHERE id=$1`, wsID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	return formID
}

func countFormEmailRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, formID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM form_email_settings WHERE form_id=$1`, formID).Scan(&n); err != nil {
		t.Fatalf("count form_email_settings rows: %v", err)
	}
	return n
}

func TestFormEmailSettingsReadDoesNotCreateRow(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	formID := formEmailFixture(t, ctx, pool)
	store := NewFormStore(pool)

	if got := countFormEmailRows(t, ctx, pool, formID); got != 0 {
		t.Fatalf("fixture left %d form_email_settings row(s); the test needs the never-visited state", got)
	}

	// ── the read: same call the GET handler makes ───────────────────────────
	st, err := store.FormEmailSettings(ctx, formID)
	if err != nil {
		t.Fatalf("read settings of a form with no stored row: %v — a form that was never "+
			"configured must still answer the defaults", err)
	}
	if got := countFormEmailRows(t, ctx, pool, formID); got != 0 {
		t.Fatalf("reading the settings CREATED %d form_email_settings row(s) — a page load is a "+
			"write, so updated_at can no longer answer \"did the merchant edit this?\"", got)
	}

	// The answer must still be the defaults the editor renders.
	if st == nil {
		t.Fatal("read returned nil settings; the GET handler dereferences it")
	}
	if st.FormID != formID {
		t.Errorf("FormID = %q, want %q", st.FormID, formID)
	}
	if !st.ReceiptEnabled {
		t.Error("ReceiptEnabled = false for a form with no stored row, want true (the built-in default)")
	}
	if st.ReceiptSubject != "" || st.ReceiptBody != "" || st.MerchantNotifyEmail != "" {
		t.Errorf("defaults = {subject:%q body:%q email:%q}, want all empty so the workspace inherits",
			st.ReceiptSubject, st.ReceiptBody, st.MerchantNotifyEmail)
	}
	if st.ReceiptEnabledOverride != nil || st.NotifyMerchantOverride != nil || st.HTMLOverride != nil {
		t.Error("override toggles are non-nil on a form with no stored row, want nil (= inherit)")
	}

	// ── the write: the first PUT materialises the row, once ─────────────────
	st.ReceiptSubject = "Resit kedai"
	if err := store.SaveFormEmailSettings(ctx, st); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	if got := countFormEmailRows(t, ctx, pool, formID); got != 1 {
		t.Fatalf("first save left %d row(s), want exactly 1", got)
	}

	// A later read must not add a second row nor overwrite the stored value.
	got, err := store.FormEmailSettings(ctx, formID)
	if err != nil {
		t.Fatalf("read settings after save: %v", err)
	}
	if got.ReceiptSubject != "Resit kedai" {
		t.Errorf("read-back receipt_subject = %q, want %q", got.ReceiptSubject, "Resit kedai")
	}
	if n := countFormEmailRows(t, ctx, pool, formID); n != 1 {
		t.Errorf("reading an EXISTING row left %d rows, want 1", n)
	}
}

// TestFormEmailSettingsRealErrorIsNotDefaults pins the second half of the card:
// "not found" is `pgx.ErrNoRows` and nothing else. Every other error must reach
// the caller, because answering the built-in defaults on a broken database is
// how a genuine failure stays invisible.
//
// The proof does not need a broken database: it needs a table that answers
// neither shape. A stub schema holds a `form_email_settings` whose only column
// is form_id, so the SELECT fails with 42703 (undefined column) while the OLD
// INSERT — which names form_id alone — succeeds. Before the fix this call
// returned the defaults and a nil error; after it must return the error.
func TestFormEmailSettingsRealErrorIsNotDefaults(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t) // ensures the DSN is set and the scratch schema exists

	dsn := os.Getenv(testdb.DSNEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping integration test", testdb.DSNEnv)
	}
	schema := fmt.Sprintf("test_esread_err_p%d", os.Getpid())
	id := fmt.Sprintf("%q", schema)
	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS `+id+` CASCADE`); err != nil {
		t.Fatalf("drop stub schema: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE SCHEMA `+id); err != nil {
		t.Fatalf("create stub schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+id+` CASCADE`)
	})
	if _, err := pool.Exec(ctx, `CREATE TABLE `+id+`.form_email_settings (form_id uuid PRIMARY KEY)`); err != nil {
		t.Fatalf("create stub table: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	stubPool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open stub pool: %v", err)
	}
	defer stubPool.Close()

	// A uuid that names no form: the stub has no FK, so the old INSERT succeeds
	// and the old code returns defaults without an error.
	_, err = NewFormStore(stubPool).FormEmailSettings(ctx, "00000000-0000-0000-0000-000000000001")
	if err == nil {
		t.Fatal("a database error in the SELECT was answered as \"no row yet\" (defaults, nil " +
			"error) — a dropped connection or a renamed column would be invisible, and the " +
			"merchant would be shown defaults instead of a failure")
	}
}
