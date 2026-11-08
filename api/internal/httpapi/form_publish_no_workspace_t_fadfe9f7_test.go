package httpapi

// The REST doors into the "published, but unreachable forever" defect (card
// t_fadfe9f7) — every route the dashboard and an API client can use to make a
// form live.
//
// THE DEFECT. An account with no workspace (issue #718, ask-first onboarding)
// owns forms whose `workspace_id` is NULL (the forms_sync_workspace_id trigger
// copies accounts.workspace_id onto the row). Since t_6c0badbe POST
// /v1/rest/forms refuses to MINT one, but the rows that predate that guard are
// still one call away from `status='published'` with no address: every public
// serving path resolves a form through its workspace, and the apex /f/<slug> is
// refused by design (issue #77). The response was 200 {"status":"published"},
// which is a false statement about a page that is 404 everywhere forever.
//
// WHAT THIS PINS
//   1. POST /v1/rest/forms/{id}/publish refuses with 409 `workspace.required`
//      and the same sentence the create door uses; the stored status is
//      unchanged.
//   2. POST /v1/rest/forms/{id}/status refuses entering every LIVE status
//      (published/unlisted/private), because those forms are served too — and
//      the dashboard's status control reaches this route, so guarding only
//      /publish would leave the defect one header away.
//   3. POST /v1/rest/forms/bulk action=publish refuses the whole call before
//      writing anything (updated would otherwise count unreachable rows).
//   4. Not widened: back to draft still answers 200 (the remedy), and a missing
//      id still answers 404 (the workspace check runs after the form read, so
//      a lookup miss is never reported as the account's state).
//   5. The exit: once the account is linked to a workspace, the SAME publish
//      answers 200 and the row carries that workspace_id.
//
// BITE: delete the refuseWithoutLiveWorkspace calls in publishForm,
// setFormStatus and bulkForms and cases 1-3 answer 200 while the row is
// published with workspace_id NULL (measured on the un-fixed tree).
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when unset.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestRESTPublishRefusesAFormWithNoWorkspace(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping REST publish/no-workspace guard")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, userID := bundleFixture(t, ctx, pool)
	key := bundleAPIKey(t, ctx, pool, accountID)
	h := newBundleHandlers(pool)

	fs := dbstore.NewFormStore(pool)
	flat := strings.ReplaceAll(accountID, "-", "")
	orphan := &dbstore.Form{
		AccountID: accountID,
		Slug:      "rest-pubnows-" + flat[:10],
		Title:     "Legacy orphan",
		Config:    map[string]any{"mode": "response", "blocks": []any{}},
		Theme:     map[string]any{},
		Status:    dbstore.FormStatusDraft,
	}
	if err := fs.Create(ctx, orphan); err != nil {
		t.Fatalf("seed orphan form: %v", err)
	}
	var wsOfForm *string
	if err := pool.QueryRow(ctx, `SELECT workspace_id::text FROM forms WHERE id=$1`, orphan.ID).Scan(&wsOfForm); err != nil {
		t.Fatalf("read orphan workspace_id: %v", err)
	}
	if wsOfForm != nil {
		t.Fatalf("fixture is wrong: the seeded form already carries workspace_id %v", *wsOfForm)
	}

	statusOf := func() string {
		t.Helper()
		var st string
		if err := pool.QueryRow(ctx, `SELECT status FROM forms WHERE id=$1`, orphan.ID).Scan(&st); err != nil {
			t.Fatalf("read stored status: %v", err)
		}
		return st
	}
	assertRefused := func(code int, raw json.RawMessage, what string) {
		t.Helper()
		if code != http.StatusConflict {
			t.Fatalf("%s = %d, want 409 (body=%s)", what, code, raw)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("%s: decode refusal: %v (%s)", what, err, raw)
		}
		if body["code"] != CodeWorkspaceRequired {
			t.Fatalf("%s: refusal code = %v, want %q (body=%s)", what, body["code"], CodeWorkspaceRequired, raw)
		}
		if body["error"] != workspaceRequiredMessage {
			t.Fatalf("%s: refusal = %v, want the create door's sentence %q", what, body["error"], workspaceRequiredMessage)
		}
		if got := statusOf(); got != dbstore.FormStatusDraft {
			t.Fatalf("%s: stored status = %q, want draft — the refusal ran AFTER the write", what, got)
		}
	}

	// ── 1. The publish route ────────────────────────────────────────────────
	code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+orphan.ID+"/publish", "")
	assertRefused(code, raw, "POST /forms/{id}/publish")

	// ── 2. The status route, for every status that is actually served ───────
	for _, live := range []string{dbstore.FormStatusPublished, dbstore.FormStatusUnlisted} {
		code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+orphan.ID+"/status",
			`{"status":"`+live+`"}`)
		assertRefused(code, raw, "POST /forms/{id}/status "+live)
	}
	code, raw = tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+orphan.ID+"/status",
		`{"status":"private","password":"rahasia123"}`)
	assertRefused(code, raw, "POST /forms/{id}/status private")

	// ── 3. The bulk route ───────────────────────────────────────────────────
	code, raw = tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/bulk",
		`{"action":"publish","ids":["`+orphan.ID+`"]}`)
	assertRefused(code, raw, "POST /forms/bulk publish")

	// ── 4. Not widened, and not re-ordered ─────────────────────────────────
	// Back to draft stays allowed: that is how an existing orphan is taken out
	// of a live state.
	if code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+orphan.ID+"/status",
		`{"status":"draft"}`); code != http.StatusOK {
		t.Fatalf("POST /forms/{id}/status draft on an orphan = %d, want 200 (body=%s) — the guard must "+
			"gate ENTERING a live status, not every write to the account", code, raw)
	}
	// A missing id is still 404, not the account's state: the workspace check
	// runs after the form read (id_lookup_status_test pins the same precedence).
	if code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+uuid.NewString()+"/publish", ""); code != http.StatusNotFound {
		t.Fatalf("POST /forms/{missing}/publish = %d, want 404 (body=%s) — the workspace refusal must "+
			"not mask a lookup miss", code, raw)
	}

	// ── 5. The exit: link the account, then the same publish lands ─────────
	ws, werr := dbstore.NewWorkspaceStore(pool).CreateForAccount(ctx, userID,
		"WS rest-pubnows", "rest-pubnows-"+flat[:8], accountID)
	if werr != nil {
		t.Fatalf("link account to a workspace: %v", werr)
	}
	if code, raw := tplReq(t, h, key, http.MethodPost, "/v1/rest/forms/"+orphan.ID+"/publish", ""); code != http.StatusOK {
		t.Fatalf("publish after the account got a workspace = %d, want 200 (body=%s)", code, raw)
	}
	if got := statusOf(); got != dbstore.FormStatusPublished {
		t.Fatalf("stored status after the linked publish = %q, want published", got)
	}
	var wsAfter *string
	if err := pool.QueryRow(ctx, `SELECT workspace_id::text FROM forms WHERE id=$1`, orphan.ID).Scan(&wsAfter); err != nil {
		t.Fatalf("read workspace_id after the publish: %v", err)
	}
	if wsAfter == nil || *wsAfter != ws.ID {
		t.Fatalf("published form workspace_id = %v, want %q — the published page would still have no address",
			wsAfter, ws.ID)
	}
}
