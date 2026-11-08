package httpapi

// GET /v1/rest/forms/{id}/email-settings must not write (card t_3c67947d).
//
// Measured on prod v1.23.7: the merchant opened the form's settings page, read
// the Notifications panel and pressed Cancel; the API log showed a single
// `GET .../email-settings` and `form_email_settings` gained a row of defaults in
// the same second. The store created the row from its error branch, so the page
// load — not the merchant — owned `updated_at`.
//
// THIS DRIVES THE REAL ROUTE (RegisterRoutes + bearer auth), not the store, so a
// guard that only fixed the SQL would still fail here if the handler called
// something that writes.
//
// WHAT THIS PINS
//  1. The GET answers 200 with the editor's shape (`form_id`, `workspace`,
//     `effective`) for a form that has never been configured.
//  2. It leaves form_email_settings exactly as it found it — zero rows.
//  3. The first PUT is what creates the row, and a following GET keeps it at
//     one row with the saved value.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when unset.

import (
	"context"
	"net/http"
	"testing"
)

func TestGetFormEmailSettingsDoesNotWrite(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	accountID, _ := bundleFixture(t, ctx, pool)
	wsID := bundleLinkWorkspace(t, ctx, pool, accountID)
	var formID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO forms (account_id, workspace_id, slug, title)
		 VALUES ($1, $2, 'it-esget-' || md5(random()::text), 'Email settings GET') RETURNING id`,
		accountID, wsID).Scan(&formID); err != nil {
		t.Fatalf("create form: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM forms WHERE id=$1`, formID)
	})

	h := newBundleHandlers(pool)
	key := bundleAPIKey(t, ctx, pool, accountID)
	path := "/v1/rest/forms/" + formID + "/email-settings"

	count := func() int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM form_email_settings WHERE form_id=$1`, formID).Scan(&n); err != nil {
			t.Fatalf("count form_email_settings rows: %v", err)
		}
		return n
	}

	var wsCountBefore int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM workspace_email_settings WHERE workspace_id=$1`, wsID).Scan(&wsCountBefore); err != nil {
		t.Fatalf("count workspace_email_settings rows: %v", err)
	}
	if got := count(); got != 0 {
		t.Fatalf("fixture left %d form_email_settings row(s); the test needs the never-visited state", got)
	}

	// ── the page load: one GET, nothing else ────────────────────────────────
	code, body := bundleGet(t, h, key, path)
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d (body %v), want 200 — the settings editor cannot load", path, code, body)
	}
	if got, _ := body["form_id"].(string); got != formID {
		t.Errorf("form_id = %v, want %q", body["form_id"], formID)
	}
	if _, ok := body["effective"]; !ok {
		t.Errorf("response has no `effective` object (body %v) — the editor renders the resolved "+
			"settings, so a missing one is an empty panel", body)
	}
	if _, ok := body["workspace"]; !ok {
		t.Errorf("response has no `workspace` object (body %v) — the panel shows what the form "+
			"inherits from, and the workspace has settings", body)
	}
	if got := count(); got != 0 {
		t.Fatalf("GET %s created %d form_email_settings row(s) — a page load wrote the table, so "+
			"updated_at can no longer answer \"did this merchant edit their email settings?\"", path, got)
	}

	// A separate, pre-existing write of the same class on the inherited side:
	// the GET resolves workspace_email_settings through WorkspaceStore.
	// EmailSettings, which has the identical INSERT-on-any-error shape. Out of
	// this card's scope (file t_3c67947d's sibling card), but recorded here as
	// the observation it is — never asserted, so it cannot mask this fix.
	var wsCountAfter int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM workspace_email_settings WHERE workspace_id=$1`, wsID).Scan(&wsCountAfter); err != nil {
		t.Fatalf("count workspace_email_settings rows after GET: %v", err)
	}
	if wsCountAfter != wsCountBefore {
		t.Logf("OBSERVED (separate defect): the same GET created %d workspace_email_settings "+
			"row(s) via WorkspaceStore.EmailSettings", wsCountAfter-wsCountBefore)
	}

	// ── the write: PUT materialises the row, exactly once ───────────────────
	code, raw := tplReq(t, h, key, http.MethodPut, path, `{"receipt_subject":"Resit kedai"}`)
	if code != http.StatusOK {
		t.Fatalf("PUT %s = %d (%s), want 200", path, code, raw)
	}
	if got := count(); got != 1 {
		t.Fatalf("first PUT left %d form_email_settings row(s), want exactly 1", got)
	}

	// And a following page load neither adds a row nor loses the value.
	code, body = bundleGet(t, h, key, path)
	if code != http.StatusOK {
		t.Fatalf("GET %s after save = %d (body %v), want 200", path, code, body)
	}
	if got, _ := body["receipt_subject"].(string); got != "Resit kedai" {
		t.Errorf("receipt_subject after save = %v, want %q", body["receipt_subject"], "Resit kedai")
	}
	if got := count(); got != 1 {
		t.Errorf("GET after save left %d form_email_settings row(s), want 1", got)
	}
}
