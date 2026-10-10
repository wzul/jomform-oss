// WorkspaceStore.ByMember / access must survive an ACCOUNT-LESS workspace.
//
// WHY THIS IS A DEFECT AND NOT A STYLE CHOICE
//
// Both reads LEFT JOIN `accounts` and used to scan `a.id` straight into a
// `string`. For a workspace with ZERO accounts the join emits NULL, pgx refuses
// the column (`cannot scan NULL into *string`) and the function returns
// (nil, err) — the WHOLE result, not the one row. One account-less workspace
// therefore took the caller's entire workspace list with it:
//
//   - resolveSessionAccount reads the error as "no workspaces" and authed()
//     answers 401 for a signed-in user;
//   - isLastWorkspace fails CLOSED on it, so the Danger zone reports
//     `last_workspace: true` and refuses every delete;
//   - workspaceAccessByID turns it into 403 `bukan workspace anda` for the
//     account-less workspace the caller OWNS;
//   - the MCP account list (userAccessibleAccounts) collapses to nothing.
//
// An account-less workspace is reachable, not hypothetical: 000024_space_accounts
// backfilled an account only for workspaces that lacked one at migration time,
// and the REST delete tests create `workspaces` rows with no `accounts` row
// directly. The fix COALESCEs the id to '' and returns the workspace with an
// empty Accounts slice.
//
// These tests drive the real store against a real Postgres, because the shape
// they pin only exists once such a row is actually in the table.

package dbstore

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/testdb"
)

// mkAccountlessWorkspace inserts a workspace owned by userID with NO accounts
// row, and returns its id and slug. It goes behind WorkspaceStore.Create on
// purpose: Create always links an account, and the defect only exists for a row
// that has none.
func mkAccountlessWorkspace(t *testing.T, pool *pgxpool.Pool, userID, name string) (id, slug string) {
	t.Helper()
	ctx := context.Background()
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, $2, 'accountless-' || substr(md5(random()::text),1,8))
		 RETURNING id, slug`, userID, name).Scan(&id, &slug); err != nil {
		t.Fatalf("create account-less workspace: %v", err)
	}
	// Precondition, asserted rather than assumed: the row really has no account.
	// Without this the test could pass on a fixture that never had the shape.
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM accounts WHERE workspace_id=$1`, id).Scan(&n); err != nil {
		t.Fatalf("count accounts for %s: %v", id, err)
	}
	if n != 0 {
		t.Fatalf("fixture is not the defect shape: workspace %s has %d accounts, want 0", id, n)
	}
	return id, slug
}

func mkWorkspaceTestUser(t *testing.T, pool *pgxpool.Pool, tag string) string {
	t.Helper()
	var userID string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users (email, password_hash)
		 VALUES ($1 || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`, tag).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return userID
}

// TestWorkspaceByMemberKeepsAccountlessWorkspaceBesideAnAccountedOne is the
// guard the defect card asks for: a caller who owns ONE account-less workspace
// AND one normal workspace gets BOTH back, the account-less one with no
// accounts, and err is nil.
//
// Pre-fix this fails at the very first assertion: ByMember returns (nil,
// "cannot scan NULL into *string") because the account-less row poisons the
// whole scan.
func TestWorkspaceByMemberKeepsAccountlessWorkspaceBesideAnAccountedOne(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	sp := NewWorkspaceStore(pool)
	userID := mkWorkspaceTestUser(t, pool, "bm-accountless-")

	// The normal workspace goes through Create, so it owns an account and is the
	// control that proves the listing still carries accounts.
	normal, err := sp.Create(ctx, userID, "Has an account", "bm-normal-"+userID[:6])
	if err != nil {
		t.Fatalf("create normal workspace: %v", err)
	}
	var normalAcc string
	if err := pool.QueryRow(ctx,
		`SELECT id FROM accounts WHERE workspace_id=$1`, normal.ID).Scan(&normalAcc); err != nil {
		t.Fatalf("read normal workspace account: %v", err)
	}
	accountlessID, _ := mkAccountlessWorkspace(t, pool, userID, "Accountless")

	got, err := sp.ByMember(ctx, userID)
	if err != nil {
		t.Fatalf("ByMember err = %v, want nil — ONE account-less workspace must not take the "+
			"caller's entire listing down (the LEFT JOIN emits NULL and the strict scan failed "+
			"the whole query)", err)
	}
	if len(got) != 2 {
		t.Fatalf("ByMember returned %d workspaces, want 2 (the account-less one included): %+v",
			len(got), got)
	}
	byID := map[string]WorkspaceAccess{}
	for _, w := range got {
		byID[w.WorkspaceID] = w
	}
	al, ok := byID[accountlessID]
	if !ok {
		t.Fatalf("the account-less workspace %s is MISSING from ByMember: %+v", accountlessID, got)
	}
	if len(al.Accounts) != 0 {
		t.Errorf("account-less workspace Accounts = %v, want empty", al.Accounts)
	}
	if al.Role != RoleAdmin {
		t.Errorf("account-less workspace Role = %q, want %q (the caller owns it)",
			al.Role, RoleAdmin)
	}
	if al.Name != "Accountless" {
		t.Errorf("account-less workspace Name = %q, want %q", al.Name, "Accountless")
	}
	nw, ok := byID[normal.ID]
	if !ok {
		t.Fatalf("the normal workspace %s is missing from ByMember: %+v", normal.ID, got)
	}
	if len(nw.Accounts) != 1 || nw.Accounts[0] != normalAcc {
		t.Errorf("normal workspace Accounts = %v, want exactly [%s]", nw.Accounts, normalAcc)
	}
}

// TestWorkspaceByMemberReturnsAnAccountlessWorkspaceAlone is the shape the
// defect card measured: the caller's only workspace has no account, so the
// failing query returned n=0 with the NULL-scan error and the caller's list —
// and therefore their session scope — was simply gone.
func TestWorkspaceByMemberReturnsAnAccountlessWorkspaceAlone(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	sp := NewWorkspaceStore(pool)
	userID := mkWorkspaceTestUser(t, pool, "bm-only-accountless-")
	id, _ := mkAccountlessWorkspace(t, pool, userID, "Only workspace")

	got, err := sp.ByMember(ctx, userID)
	if err != nil {
		t.Fatalf("ByMember err = %v, want nil — the caller's ONLY workspace has no account, "+
			"which must read as 'one workspace with no accounts', not as 'no workspaces'", err)
	}
	if len(got) != 1 {
		t.Fatalf("ByMember returned %d workspaces, want the single account-less one: %+v", len(got), got)
	}
	if got[0].WorkspaceID != id {
		t.Errorf("ByMember()[0].WorkspaceID = %q, want %q", got[0].WorkspaceID, id)
	}
	if len(got[0].Accounts) != 0 {
		t.Errorf("Accounts = %v, want empty", got[0].Accounts)
	}
}

// TestWorkspaceAccessSurvivesAnAccountlessWorkspace pins the SECOND site: access()
// backs both AccessByID and AccessBySlug, and workspaceAccessByID turns its error
// into 403 `bukan workspace anda`. Without this half, the account-less workspace
// the caller owns cannot be opened, renamed or deleted even once the listing is
// fixed — the two blockers are independent.
func TestWorkspaceAccessSurvivesAnAccountlessWorkspace(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	sp := NewWorkspaceStore(pool)
	userID := mkWorkspaceTestUser(t, pool, "access-accountless-")
	id, slug := mkAccountlessWorkspace(t, pool, userID, "Accountless for access")

	byID, ok, err := sp.AccessByID(ctx, id, userID)
	if err != nil {
		t.Fatalf("AccessByID(account-less, owner) err = %v, want nil — the strict scan made "+
			"the caller's OWN account-less workspace unresolvable (403 downstream)", err)
	}
	if !ok {
		t.Fatalf("AccessByID(account-less, owner) ok = false, want true: the caller owns it")
	}
	if byID.WorkspaceID != id || byID.Role != RoleAdmin {
		t.Errorf("AccessByID = %+v, want workspace %s with role %s", byID, id, RoleAdmin)
	}
	if len(byID.Accounts) != 0 {
		t.Errorf("AccessByID Accounts = %v, want empty", byID.Accounts)
	}

	bySlug, ok, err := sp.AccessBySlug(ctx, slug, userID)
	if err != nil {
		t.Fatalf("AccessBySlug(account-less, owner) err = %v, want nil", err)
	}
	if !ok {
		t.Fatalf("AccessBySlug(account-less, owner) ok = false, want true")
	}
	if bySlug.WorkspaceID != id || len(bySlug.Accounts) != 0 {
		t.Errorf("AccessBySlug = %+v, want workspace %s and no accounts", bySlug, id)
	}
}
