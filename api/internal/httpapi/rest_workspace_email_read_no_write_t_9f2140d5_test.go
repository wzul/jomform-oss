package httpapi

// Reading a workspace's email settings must not write workspace_email_settings
// (card t_9f2140d5).
//
// Measured while fixing the identical defect on the FORM settings (card
// t_3c67947d): one `GET /v1/rest/forms/{id}/email-settings` — a page load with no
// PUT/POST/DELETE — took `workspace_email_settings` from 0 to 1 row, because
// `WorkspaceStore.EmailSettings` created a default row from its SELECT's error
// branch. The same store call backs the workspace settings route itself, so both
// reads were writes.
//
// THIS DRIVES THE REAL ROUTES (RegisterRoutes, real bearer key / session
// cookie), not the store, so a guard that only fixed the SQL would still fail
// here if a handler called something that writes.
//
// WHAT THIS PINS
//  1. `GET /v1/workspaces/{id}/email-settings` answers 200 with the defaults for
//     a workspace that has never been configured and leaves the table empty;
//     the first PUT creates the row exactly once, and a following GET keeps it.
//  2. `GET /v1/rest/forms/{id}/email-settings`, which resolves the inherited
//     workspace settings, leaves workspace_email_settings untouched while its
//     `workspace` object still carries the defaults.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); the session route
// needs JOMFORM_TEST_VALKEY_URL and skips without it, exactly like every other
// session-authenticated workspace route test.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func workspaceEmailRowCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, wsID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM workspace_email_settings WHERE workspace_id=$1`, wsID).Scan(&n); err != nil {
		t.Fatalf("count workspace_email_settings rows: %v", err)
	}
	return n
}

// TestGetWorkspaceEmailSettingsDoesNotWrite is the settings page itself:
// GET .../email-settings, nothing else.
func TestGetWorkspaceEmailSettingsDoesNotWrite(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	h := rbacHandlers(t, pool) // session path: workspace routes resolve a USER

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-wsesget-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var slug string
	if err := pool.QueryRow(ctx, `SELECT 'it-wsesget-' || md5(random()::text)`).Scan(&slug); err != nil {
		t.Fatalf("build slug: %v", err)
	}
	ws, err := h.Workspaces.Create(ctx, userID, "it-wsesget", slug)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspaces WHERE id=$1`, ws.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	token := rbacSessionCookie(t, h, userID)
	path := "/v1/workspaces/" + ws.ID + "/email-settings"

	if got := workspaceEmailRowCount(t, ctx, pool, ws.ID); got != 0 {
		t.Fatalf("fixture left %d workspace_email_settings row(s); the test needs the "+
			"never-configured state", got)
	}

	// ── the page load: one GET, nothing else ────────────────────────────────
	rec := sessionReq(t, h, token, http.MethodGet, path, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d (%s), want 200 — the settings page cannot load", path, rec.Code, rec.Body.String())
	}
	var st dbstore.EmailSettings
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("GET %s: bad json: %v (%s)", path, err, rec.Body.String())
	}
	if st.WorkspaceID != ws.ID {
		t.Errorf("workspace_id = %q, want %q", st.WorkspaceID, ws.ID)
	}
	if !st.ReceiptEnabled {
		t.Error("receipt_enabled = false for a workspace with no stored row, want true (the built-in default)")
	}
	if st.ReceiptSubject != "" || st.ReceiptBody != "" || st.MerchantNotifyEmail != "" {
		t.Errorf("defaults = {subject:%q body:%q email:%q}, want all empty",
			st.ReceiptSubject, st.ReceiptBody, st.MerchantNotifyEmail)
	}
	if st.NotifyMerchant || st.HTML {
		t.Errorf("toggles = {notify_merchant:%v html:%v}, want both false", st.NotifyMerchant, st.HTML)
	}
	if got := workspaceEmailRowCount(t, ctx, pool, ws.ID); got != 0 {
		t.Fatalf("GET %s created %d workspace_email_settings row(s) — a page load wrote the "+
			"table, so updated_at can no longer answer \"did this merchant edit their email "+
			"settings?\"", path, got)
	}

	// ── the write: PUT materialises the row, exactly once ───────────────────
	rec = sessionReq(t, h, token, http.MethodPut, path,
		`{"receipt_subject":"Resit kedai","receipt_body":"Badan resit","receipt_enabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT %s = %d (%s), want 200", path, rec.Code, rec.Body.String())
	}
	if got := workspaceEmailRowCount(t, ctx, pool, ws.ID); got != 1 {
		t.Fatalf("first PUT left %d workspace_email_settings row(s), want exactly 1", got)
	}

	// And a following page load neither adds a row nor loses the value.
	rec = sessionReq(t, h, token, http.MethodGet, path, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s after save = %d (%s), want 200", path, rec.Code, rec.Body.String())
	}
	st = dbstore.EmailSettings{}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("GET %s after save: bad json: %v (%s)", path, err, rec.Body.String())
	}
	if st.ReceiptSubject != "Resit kedai" {
		t.Errorf("receipt_subject after save = %q, want %q", st.ReceiptSubject, "Resit kedai")
	}
	if got := workspaceEmailRowCount(t, ctx, pool, ws.ID); got != 1 {
		t.Errorf("GET after save left %d workspace_email_settings row(s), want 1", got)
	}
}

// TestGetFormEmailSettingsDoesNotWriteWorkspaceRow is the measured reproduction:
// the FORM settings GET resolves the workspace settings it inherits from, so the
// workspace read has to be clean too.
//
// form_email_settings is deliberately NOT asserted here — that half is card
// t_3c67947d's fix and is pinned by its own test; on a branch without it this
// route still creates a form row, which must not be allowed to mask this fix.
func TestGetFormEmailSettingsDoesNotWriteWorkspaceRow(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	accountID, _ := bundleFixture(t, ctx, pool)
	wsID := bundleLinkWorkspace(t, ctx, pool, accountID)
	var formID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO forms (account_id, workspace_id, slug, title)
		 VALUES ($1, $2, 'it-wsesfget-' || md5(random()::text), 'Workspace email settings GET') RETURNING id`,
		accountID, wsID).Scan(&formID); err != nil {
		t.Fatalf("create form: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM forms WHERE id=$1`, formID)
	})

	h := newBundleHandlers(pool)
	key := bundleAPIKey(t, ctx, pool, accountID)
	path := "/v1/rest/forms/" + formID + "/email-settings"

	if got := workspaceEmailRowCount(t, ctx, pool, wsID); got != 0 {
		t.Fatalf("fixture left %d workspace_email_settings row(s); the test needs the "+
			"never-configured state", got)
	}

	// ── the page load: one GET, nothing else ────────────────────────────────
	code, body := bundleGet(t, h, key, path)
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d (body %v), want 200 — the settings page cannot load", path, code, body)
	}
	wsBody, ok := body["workspace"].(map[string]any)
	if !ok {
		t.Fatalf("response has no `workspace` object (body %v) — the page shows what the form "+
			"inherits from", body)
	}
	if got, _ := wsBody["workspace_id"].(string); got != wsID {
		t.Errorf("workspace.workspace_id = %v, want %q", wsBody["workspace_id"], wsID)
	}
	if got, ok := wsBody["receipt_enabled"].(bool); !ok || !got {
		t.Errorf("workspace.receipt_enabled = %v, want true (the built-in default)", wsBody["receipt_enabled"])
	}
	if got, _ := wsBody["receipt_subject"].(string); got != "" {
		t.Errorf("workspace.receipt_subject = %q for a never-configured workspace, want \"\"", got)
	}
	if got := workspaceEmailRowCount(t, ctx, pool, wsID); got != 0 {
		t.Fatalf("GET %s created %d workspace_email_settings row(s) — the read wrote the "+
			"settings it inherits from", path, got)
	}

	// Once the merchant saves the workspace settings, the same GET reports them
	// — proving the fix removed the WRITE, not the read.
	if err := h.Workspaces.SaveEmailSettings(ctx, &dbstore.EmailSettings{
		WorkspaceID: wsID, ReceiptSubject: "Resit kedai", ReceiptEnabled: true,
	}); err != nil {
		t.Fatalf("save workspace email settings: %v", err)
	}
	if got := workspaceEmailRowCount(t, ctx, pool, wsID); got != 1 {
		t.Fatalf("save left %d workspace_email_settings row(s), want exactly 1", got)
	}
	code, body = bundleGet(t, h, key, path)
	if code != http.StatusOK {
		t.Fatalf("GET %s after save = %d (body %v), want 200", path, code, body)
	}
	wsBody, _ = body["workspace"].(map[string]any)
	if got, _ := wsBody["receipt_subject"].(string); got != "Resit kedai" {
		t.Errorf("workspace.receipt_subject after save = %q, want %q", got, "Resit kedai")
	}
	if got := workspaceEmailRowCount(t, ctx, pool, wsID); got != 1 {
		t.Errorf("GET after save left %d workspace_email_settings row(s), want 1", got)
	}
}
