package dbstore

// A READ of the per-workspace email settings must not WRITE the table (card
// t_9f2140d5).
//
// THE DEFECT. `WorkspaceStore.EmailSettings` treated EVERY error from its
// SELECT as "no row yet" and then ran
// `INSERT INTO workspace_email_settings (workspace_id) VALUES ($1) ON CONFLICT
// DO NOTHING`. It is reached by READS — `GET /v1/workspaces/{id}/email-settings`
// and, through `formWorkspaceEmailSettings`, `GET
// /v1/rest/forms/{id}/email-settings` — so opening a settings page with no save
// created a row of defaults and owned its `updated_at`. Measured on the form
// route while fixing the identical defect one file over (card t_3c67947d):
// `workspace_email_settings` went from 0 to 1 row across a single GET with no
// PUT/POST/DELETE issued.
//
// Two consequences, the same as the form-level card:
//   - `updated_at` can no longer answer "did this merchant edit their email
//     settings?" — every page view looks like an edit; and
//   - the error branch is unreachable AS an error: a dropped connection or a
//     renamed column is indistinguishable from "no row" and is answered with
//     the defaults, HTTP 200.
//
// WHAT THIS PINS
//  1. Reading a workspace with no stored row leaves the row count unchanged and
//     still answers the defaults the settings page needs.
//  2. The row is created by the FIRST WRITE (SaveEmailSettings, which the PUT
//     handler calls) — exactly one row — and a later read neither adds nor
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

// workspaceEmailFixture seeds a user + workspace and returns the workspace id.
// It deliberately creates NO workspace_email_settings row: the "never
// configured" state the card is about.
func workspaceEmailFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var userID, wsID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-wsesread-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-wsesread-ws', 'it-wsesread-' || md5(random()::text)) RETURNING id`, userID).Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspaces WHERE id=$1`, wsID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	return wsID
}

func countWorkspaceEmailRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, wsID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM workspace_email_settings WHERE workspace_id=$1`, wsID).Scan(&n); err != nil {
		t.Fatalf("count workspace_email_settings rows: %v", err)
	}
	return n
}

func TestWorkspaceEmailSettingsReadDoesNotCreateRow(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	wsID := workspaceEmailFixture(t, ctx, pool)
	store := NewWorkspaceStore(pool)

	if got := countWorkspaceEmailRows(t, ctx, pool, wsID); got != 0 {
		t.Fatalf("fixture left %d workspace_email_settings row(s); the test needs the never-configured state", got)
	}

	// ── the read: same call both GET handlers make ──────────────────────────
	st, err := store.EmailSettings(ctx, wsID)
	if err != nil {
		t.Fatalf("read settings of a workspace with no stored row: %v — a workspace that was "+
			"never configured must still answer the defaults", err)
	}
	if got := countWorkspaceEmailRows(t, ctx, pool, wsID); got != 0 {
		t.Fatalf("reading the settings CREATED %d workspace_email_settings row(s) — a page load "+
			"is a write, so updated_at can no longer answer \"did the merchant edit this?\"", got)
	}

	// The answer must still be the defaults the settings page renders.
	if st == nil {
		t.Fatal("read returned nil settings; both GET handlers dereference it")
	}
	if st.WorkspaceID != wsID {
		t.Errorf("WorkspaceID = %q, want %q", st.WorkspaceID, wsID)
	}
	if !st.ReceiptEnabled {
		t.Error("ReceiptEnabled = false for a workspace with no stored row, want true (the built-in default)")
	}
	if st.ReceiptSubject != "" || st.ReceiptBody != "" || st.MerchantNotifyEmail != "" {
		t.Errorf("defaults = {subject:%q body:%q email:%q}, want all empty",
			st.ReceiptSubject, st.ReceiptBody, st.MerchantNotifyEmail)
	}
	if st.NotifyMerchant || st.HTML {
		t.Errorf("toggles = {notify_merchant:%v html:%v}, want both false", st.NotifyMerchant, st.HTML)
	}

	// ── the write: the first PUT materialises the row, once ─────────────────
	st.ReceiptSubject = "Resit kedai"
	if err := store.SaveEmailSettings(ctx, st); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	if got := countWorkspaceEmailRows(t, ctx, pool, wsID); got != 1 {
		t.Fatalf("first save left %d row(s), want exactly 1", got)
	}

	// A later read must not add a second row nor overwrite the stored value.
	got, err := store.EmailSettings(ctx, wsID)
	if err != nil {
		t.Fatalf("read settings after save: %v", err)
	}
	if got.ReceiptSubject != "Resit kedai" {
		t.Errorf("read-back receipt_subject = %q, want %q", got.ReceiptSubject, "Resit kedai")
	}
	if n := countWorkspaceEmailRows(t, ctx, pool, wsID); n != 1 {
		t.Errorf("reading an EXISTING row left %d rows, want 1", n)
	}
}

// TestWorkspaceEmailSettingsRealErrorIsNotDefaults pins the second half of the
// card: "not found" is `pgx.ErrNoRows` and nothing else. Every other error must
// reach the caller, because answering the built-in defaults on a broken database
// is how a genuine failure stays invisible.
//
// The proof does not need a broken database: it needs a table that answers
// neither shape. A stub schema holds a `workspace_email_settings` whose only
// column is workspace_id, so the SELECT fails with 42703 (undefined column)
// while the OLD INSERT — which names workspace_id alone — succeeds. Before the
// fix this call returned the defaults and a nil error; after it must return the
// error.
func TestWorkspaceEmailSettingsRealErrorIsNotDefaults(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t) // ensures the DSN is set and the scratch schema exists

	dsn := os.Getenv(testdb.DSNEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping integration test", testdb.DSNEnv)
	}
	schema := fmt.Sprintf("test_wsesread_err_p%d", os.Getpid())
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
	if _, err := pool.Exec(ctx, `CREATE TABLE `+id+`.workspace_email_settings (workspace_id uuid PRIMARY KEY)`); err != nil {
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

	// A workspace id that has no row: the stub has no FK, so the old INSERT
	// succeeds and the old code returns defaults without an error.
	_, err = NewWorkspaceStore(stubPool).EmailSettings(ctx, "00000000-0000-0000-0000-000000000001")
	if err == nil {
		t.Fatal("a database error in the SELECT was answered as \"no row yet\" (defaults, nil " +
			"error) — a dropped connection or a renamed column would be invisible, and the " +
			"merchant would be shown defaults instead of a failure")
	}
}
