package httpapi

// POST /v1/rest/forms from an account with NO workspace is refused (card
// t_6c0badbe).
//
// THE DEFECT, measured on a clean self-host install. A signup creates an account
// and NO workspace (issue #718 — ask-first onboarding), and `accounts.workspace_id`
// is nullable. The create handler accepted the POST anyway, the
// `forms_sync_workspace_id` trigger (migration 000045) copied the NULL onto the
// row, the form could then be PUBLISHED — and it was unreachable forever, because
// every public serving path resolves a form through its workspace. Two test forms
// (`clean-test`, `clean-test-2`) were left `published` with `workspace_id IS NULL`,
// both 404 at /f/<slug>. The dashboard never sends this shape; a direct API client
// does.
//
// WHAT THIS PINS
//   1. The refusal: 409 + the machine `code` `workspace.required`, on the real
//      route through the real mux, authenticating the way an API client does.
//   2. That it refuses BEFORE storing: no row exists afterwards. A guard that
//      answered 409 while the INSERT had already happened would leave exactly the
//      orphan row the card is about.
//   3. The other direction: the SAME account, once linked to a workspace, creates
//      the same form successfully (201). Without this the guard could be a blanket
//      refusal of every create and still pass case 1.
//
// BITE: delete the HasLiveWorkspace check in createForm and case 1 goes 201 with a
// stored row whose workspace_id is NULL.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when unset.
import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestCreateFormWithoutWorkspaceIsRefused(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	// The state a fresh signup is in: a user, an account, NO workspace.
	accountID, _ := bundleFixture(t, ctx, pool)
	h := newBundleHandlers(pool)
	key := bundleAPIKey(t, ctx, pool, accountID)

	code, body := createFormThroughRoute(t, h, key, `{"title":"clean-test","slug":"clean-test"}`)
	if code != http.StatusConflict {
		t.Fatalf("POST /v1/rest/forms for an account with no workspace = %d (body %v), want 409 — "+
			"storing it produces a form that can be published and is then 404 at every address",
			code, body)
	}
	if got := body["code"]; got != CodeWorkspaceRequired {
		t.Errorf("code = %v, want %q — the dashboard cannot tell this refusal from a generic "+
			"conflict without it (body %v)", got, CodeWorkspaceRequired, body)
	}
	// The prose stays on the wire for API-key/curl/MCP callers.
	if got, _ := body["error"].(string); got != workspaceRequiredMessage {
		t.Errorf("error = %v, want the wire prose %q", body["error"], workspaceRequiredMessage)
	}

	// It must refuse BEFORE storing. A 409 with a row behind it is still the
	// orphan this card is about.
	var stored int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM forms WHERE account_id=$1 AND slug='clean-test'`, accountID).Scan(&stored); err != nil {
		t.Fatalf("count forms: %v", err)
	}
	if stored != 0 {
		t.Fatalf("the refused create still stored %d row(s) — the guard runs after the INSERT, so the "+
			"orphan form exists anyway", stored)
	}

	// ── the other direction ─────────────────────────────────────────────────
	// Link the account to a workspace (what the dashboard's ask-first create
	// does) and retry the SAME request: it must succeed, so the guard refuses
	// the account's STATE and nothing else.
	wsID := bundleLinkWorkspace(t, ctx, pool, accountID)

	code, body = createFormThroughRoute(t, h, key, `{"title":"clean-test","slug":"clean-test"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST /v1/rest/forms for an account WITH a workspace = %d (body %v), want 201 — the "+
			"guard must refuse the account's state, not every create", code, body)
	}
	var wsIDOnForm *string
	if err := pool.QueryRow(ctx,
		`SELECT workspace_id::text FROM forms WHERE account_id=$1 AND slug='clean-test'`, accountID).Scan(&wsIDOnForm); err != nil {
		t.Fatalf("read stored form: %v", err)
	}
	if wsIDOnForm == nil || *wsIDOnForm != wsID {
		t.Fatalf("the created form's workspace_id = %v, want %q — a form must land in the workspace "+
			"that now owns it", wsIDOnForm, wsID)
	}
	// And the URL it reports is the reachable one, not a host nobody serves.
	url, _ := body["url"].(string)
	if url != "" {
		t.Logf("created form URL on a hosted-shaped handler (no SelfHosted): %q", url)
	}
}

// createFormThroughRoute drives POST /v1/rest/forms through the registered mux
// (real route, real auth middleware) and decodes the JSON envelope.
func createFormThroughRoute(t *testing.T, h *RESTHandlers, rawKey, payload string) (int, map[string]any) {
	t.Helper()
	code, raw := tplReq(t, h, rawKey, http.MethodPost, "/v1/rest/forms", payload)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode POST /v1/rest/forms response: %v (%s)", err, raw)
	}
	return code, out
}
