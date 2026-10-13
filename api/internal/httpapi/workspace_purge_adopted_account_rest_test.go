// The REST wire half of the purge's ACCOUNT RULE (card t_9fe4c36a).
//
// dbstore's TestWorkspacePurgeDoesNotDestroyAnAdoptedLegacyAccount pins the store
// behaviour. THIS test drives the real route table with a real session and pins
// what the transport does with it: a 200 that reports the two outcomes
// separately, and a row that is still there afterwards.
//
// WHY THE TRANSPORT NEEDS ITS OWN LEG. The count map is the ONLY thing a caller
// (the MCP tool, an operator's curl, an audit script) reads back to learn what a
// purge destroyed. If the store unlinked the adopted account but the response
// kept reporting it as a plain deleted account, every downstream reader would
// still believe a pre-existing account had been destroyed — the claim would be
// wrong at the last mile even though the fix landed.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestRESTWorkspacePurgeUnlinksAnAdoptedLegacyAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DB-backed REST workspace test in -short mode")
	}
	ctx := context.Background()
	pool := testdb.Open(t)
	sess := authTestValkey(t)
	h := trashRESTFixture(t, pool)
	h.Sessions = sess
	h.Users = dbstore.NewUserStore(pool)

	userID := mkWsUser(t, pool, "rest-ws-adopt")
	sp := dbstore.NewWorkspaceStore(pool)

	// The account signup left behind: the caller's one and only, no workspace.
	var adopted string
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1, 'signup', 'rest-adopt-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&adopted); err != nil {
		t.Fatalf("create the signup account: %v", err)
	}
	found, err := sp.SoleEmptyUnscopedAccount(ctx, userID)
	if err != nil {
		t.Fatalf("SoleEmptyUnscopedAccount: %v", err)
	}
	if found != adopted {
		t.Fatalf("SoleEmptyUnscopedAccount = %q, want %q — the fixture is not in the adopted shape",
			found, adopted)
	}
	target, err := sp.CreateForAccount(ctx, userID, "Adopted", "rest-adopt-ws-"+userID[:6], found)
	if err != nil {
		t.Fatalf("CreateForAccount: %v", err)
	}
	// A second workspace, only so the bin is allowed (the last live workspace
	// can never be binned).
	keeper, err := sp.Create(ctx, userID, "Keeper", "rest-keep-ws-"+userID[:6])
	if err != nil {
		t.Fatalf("create the keeper workspace: %v", err)
	}
	var keeperAcc string
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, keeper.ID).
		Scan(&keeperAcc); err != nil {
		t.Fatalf("read the keeper's account: %v", err)
	}

	token, err := sess.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Bin, then purge for good — both through the real route table, both naming
	// the workspace's slug.
	rec := sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+target.ID,
		`{"confirm_slug":"`+target.Slug+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("bin the workspace = %d (%s)", rec.Code, rec.Body.String())
	}
	rec = sessionReq(t, h, token, http.MethodDelete, "/v1/workspaces/"+target.ID+"/purge",
		`{"confirm_slug":"`+target.Slug+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("purge the adopted workspace = %d (%s)", rec.Code, rec.Body.String())
	}

	var body struct {
		Status string           `json:"status"`
		Purged map[string]int64 `json:"purged"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("purge response is not JSON: %v (%s)", err, rec.Body.String())
	}
	if body.Status != "purged" {
		t.Errorf("purge status = %q, want \"purged\"", body.Status)
	}
	// THE WIRE ASSERTION: the caller is told which of the two things happened.
	if body.Purged["accounts"] != 0 {
		t.Errorf("purged[accounts] = %d, want 0 — the adopted account was not the workspace's to "+
			"destroy, so the response must not report it as destroyed", body.Purged["accounts"])
	}
	if body.Purged["accounts_unlinked"] != 1 {
		t.Errorf("purged[accounts_unlinked] = %d, want 1 — the response does not say the "+
			"pre-existing account was UNLINKED instead of destroyed; body=%s",
			body.Purged["accounts_unlinked"], rec.Body.String())
	}
	if body.Purged["workspaces"] != 1 {
		t.Errorf("purged[workspaces] = %d, want 1", body.Purged["workspaces"])
	}

	// And the rows: the workspace is gone, the account is not.
	if n := countRowsREST(t, pool, `SELECT COUNT(*) FROM workspaces WHERE id=$1`, target.ID); n != 0 {
		t.Error("the purged workspace row is still there")
	}
	var wsID *string
	if err := pool.QueryRow(ctx, `SELECT workspace_id FROM accounts WHERE id=$1`, adopted).
		Scan(&wsID); err != nil {
		t.Fatalf("the purge destroyed the adopted account (reading it back): %v — this is the "+
			"defect t_9fe4c36a measured on prod", err)
	}
	if wsID != nil {
		t.Errorf("the adopted account's workspace_id = %q, want NULL — the purge must return it "+
			"to the unlinked state it had before the adoption", *wsID)
	}
	if n := countRowsREST(t, pool, `SELECT COUNT(*) FROM accounts WHERE id=$1`, keeperAcc); n != 1 {
		t.Error("the purge destroyed a DIFFERENT workspace's account")
	}
}
