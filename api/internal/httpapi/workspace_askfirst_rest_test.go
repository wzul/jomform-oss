package httpapi

// Ask-first onboarding on the REST transport, with the credential the card's
// reproduction actually holds.
//
// The dashboard's own ask-first create (POST /v1/workspaces) reuses the signup
// account instead of letting Workspaces.Create insert a second one (issue #158).
// `SoleEmptyUnscopedAccount` used to exclude any account carrying an `api_keys`
// row, so the reuse only worked for a user who had never minted a key. A fresh
// user who creates one — through POST /v1/rest/keys, or an agent calling the
// documented create_api_key tool — then created their first workspace and ended
// up owning TWO accounts.
//
// This drives the real REST surface end to end: a session cookie, a real API key
// row, POST /v1/workspaces, then a read-back of the ACCOUNTS TABLE plus a real
// GET /v1/rest/keys?workspace=<slug> — the list the merchant actually looks at.
// Counting rows is not enough on its own: the second half is the user-visible
// harm (a key left on a workspace-less account cannot be seen or revoked from
// inside the workspace).
//
// Skipped without JOMFORM_TEST_DATABASE_URL (and JOMFORM_TEST_VALKEY_URL, for
// the session).
import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestRESTCreateWorkspaceReusesAKeyHoldingSignupAccount(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	h := newBundleHandlers(pool)
	h.Sessions = authTestValkey(t)

	// The post-signup state: one account, no workspace — PLUS the credential a
	// user (or their agent) mints before they can use the API at all.
	accountID, userID := bundleFixture(t, ctx, pool)
	raw, hash, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}
	if _, err := dbstore.NewAPIKeyStore(pool).Create(ctx, accountID, hash, "agent-key", []string{dbstore.ScopeReadWrite}); err != nil {
		t.Fatalf("store api key: %v", err)
	}
	_ = raw
	tok := rbacSessionCookie(t, h, userID)

	slug := "restaf-" + accountID[:8]
	rec := rbacDo(t, h, "POST", "/v1/workspaces", tok, map[string]any{"name": "Ask First", "slug": slug})
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /v1/workspaces = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var created map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v (body %s)", err, rec.Body.String())
	}

	// ONE account, and it is the signup one, linked to the new workspace.
	var total int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM accounts WHERE owner_user_id=$1`, userID).Scan(&total); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if total != 1 {
		t.Fatalf("creating the first workspace left the user owning %d accounts, want 1 — "+
			"the key-holding signup account was not reused and a second one was inserted", total)
	}
	var linked string
	var wsID *string
	if err := pool.QueryRow(ctx,
		`SELECT id, workspace_id FROM accounts WHERE owner_user_id=$1`, userID).Scan(&linked, &wsID); err != nil {
		t.Fatalf("read account: %v", err)
	}
	if linked != accountID {
		t.Fatalf("surviving account = %q, want the signup account %q", linked, accountID)
	}
	if wsID == nil || *wsID != created["id"] {
		t.Fatalf("account workspace_id = %v, want the created workspace %q", wsID, created["id"])
	}

	// USER-VISIBLE: the merchant must be able to see the key their agent holds
	// from inside the workspace they just created. A key on the stranded account
	// is invisible here, which is what the card measured as an empty list.
	rec = rbacDo(t, h, "GET", "/v1/rest/keys?workspace="+slug, tok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/rest/keys?workspace=%s = %d, want 200 (body %s)", slug, rec.Code, rec.Body.String())
	}
	var keys []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &keys); err != nil {
		t.Fatalf("decode keys: %v (body %s)", err, rec.Body.String())
	}
	if len(keys) != 1 || keys[0].Name != "agent-key" {
		t.Fatalf("GET /v1/rest/keys?workspace=%s returned %s, want the agent-key the caller minted — "+
			"the credential is stranded on the workspace-less account, so the merchant can neither "+
			"see nor revoke it", slug, rec.Body.String())
	}
}
