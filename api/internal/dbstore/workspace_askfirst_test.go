package dbstore

// Integration tests for ask-first onboarding (Wan's decision, 2026-09-20):
// a signup creates an account and NO workspace, and a user with zero workspaces
// is ASKED to create one before any create surface is reachable. The create path
// must reuse the signup account rather than minting a second one, otherwise every
// new user ends up owning two accounts — one of them workspace-less and
// unreachable, while the dashboard reads the account resolved from the scoped
// workspace.
//
// Skipped unless JOMFORM_TEST_DATABASE_URL is set; migrations are applied first
// by testdb.Open, mirroring the production boot path.

import (
	"context"
	"testing"
)

// TestSoleEmptyUnscopedAccountReusesTheSignupAccount is the no-double-account
// check for the ask-first path: the account a signup leaves behind is the ONE the
// create path links, and the user ends with exactly one account.
func TestSoleEmptyUnscopedAccountReusesTheSignupAccount(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)

	user := testOwnerUser(t, ctx, p)

	// What signup creates (auth.go): an account, no workspace, no content.
	var signupAcc string
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1, 'signup', 'signup-' || md5(random()::text))
		 RETURNING id`, user).Scan(&signupAcc); err != nil {
		t.Fatalf("create signup account: %v", err)
	}

	// The user has NO workspace yet, so the ask-first path applies.
	prior, err := sp.ByMember(ctx, user)
	if err != nil {
		t.Fatalf("ByMember before create: %v", err)
	}
	if len(prior) != 0 {
		t.Fatalf("a fresh signup already has %d workspaces, want 0", len(prior))
	}

	// The create path must FIND the signup account...
	found, err := sp.SoleEmptyUnscopedAccount(ctx, user)
	if err != nil {
		t.Fatalf("SoleEmptyUnscopedAccount: %v", err)
	}
	if found != signupAcc {
		t.Fatalf("SoleEmptyUnscopedAccount = %q, want the signup account %q", found, signupAcc)
	}

	// ...and LINK it rather than inserting a fresh one.
	slug := "askfirst-" + randSuffix()
	ws, err := sp.CreateForAccount(ctx, user, "Ask First", slug, found)
	if err != nil {
		t.Fatalf("CreateForAccount: %v", err)
	}
	if ws.Slug != slug {
		t.Fatalf("workspace slug = %q, want %q", ws.Slug, slug)
	}

	// THE ASSERTION THAT MATTERS: exactly ONE account, linked to the workspace.
	var total int
	if err := p.QueryRow(ctx,
		`SELECT count(*) FROM accounts WHERE owner_user_id=$1`, user).Scan(&total); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if total != 1 {
		t.Fatalf("the user owns %d accounts after creating their first workspace, want 1 — "+
			"the create path minted a second account instead of reusing the signup one", total)
	}
	var linked string
	var wsID *string
	if err := p.QueryRow(ctx,
		`SELECT id, workspace_id FROM accounts WHERE owner_user_id=$1`, user).Scan(&linked, &wsID); err != nil {
		t.Fatalf("read account: %v", err)
	}
	if linked != signupAcc {
		t.Fatalf("the surviving account is %q, want the signup account %q", linked, signupAcc)
	}
	if wsID == nil || *wsID != ws.ID {
		t.Fatalf("account workspace_id = %v, want %q — the signup account was left workspace-less", wsID, ws.ID)
	}

	// The user can now see the workspace, so the ask-first gate is satisfied.
	after, err := sp.ByMember(ctx, user)
	if err != nil {
		t.Fatalf("ByMember after create: %v", err)
	}
	if len(after) != 1 || after[0].WorkspaceID != ws.ID {
		t.Fatalf("workspaces after create = %+v, want the one just created", after)
	}

	// And the reuse must NOT happen twice: a second create cannot relink the same
	// account (it is already in a workspace), and the guard falls back to the
	// ordinary fresh-account path.
	next, err := sp.SoleEmptyUnscopedAccount(ctx, user)
	if err != nil {
		t.Fatalf("SoleEmptyUnscopedAccount after link: %v", err)
	}
	if next != "" {
		t.Fatalf("SoleEmptyUnscopedAccount returned %q after the account was linked — "+
			"a second create could relink an account that is already in a workspace", next)
	}
}

// TestSoleEmptyUnscopedAccountLeavesLegacyAccountsAlone pins the content guard.
// An account that already holds forms is a legacy workspace-less account whose
// public host slug its published forms resolve through (PublicHostSlugByAccount);
// moving it is a data-migration decision, never a side effect of pressing
// "create workspace". The store reports "" so the caller keeps the fresh-account
// behaviour, and the guard must bite for every CONTENT table.
//
// api_keys is deliberately NOT in this list — see
// TestSoleEmptyUnscopedAccountKeepsTheCallersKey.
func TestSoleEmptyUnscopedAccountLeavesLegacyAccountsAlone(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)

	for _, tc := range []struct {
		name string
		seed func(t *testing.T, accID string)
	}{
		{"forms", func(t *testing.T, accID string) {
			if _, err := p.Exec(ctx,
				`INSERT INTO forms (account_id, slug, title, config, theme)
				 VALUES ($1, 'legacy-'||md5(random()::text), 'legacy form', '{}'::jsonb, '{}'::jsonb)`, accID); err != nil {
				t.Fatalf("seed form: %v", err)
			}
		}},
		{"products", func(t *testing.T, accID string) {
			if _, err := p.Exec(ctx,
				`INSERT INTO products (account_id, name, price_sen, images)
				 VALUES ($1, 'legacy product', 1000, '[]'::jsonb)`, accID); err != nil {
				t.Fatalf("seed product: %v", err)
			}
		}},
		{"sales", func(t *testing.T, accID string) {
			if _, err := p.Exec(ctx,
				`INSERT INTO sales (account_id, ref_code, client, items)
				 VALUES ($1, 'JF-LEGACY-'||md5(random()::text), '{}'::jsonb, '[]'::jsonb)`, accID); err != nil {
				t.Fatalf("seed sale: %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := testOwnerUser(t, ctx, p)
			var accID string
			if err := p.QueryRow(ctx,
				`INSERT INTO accounts (owner_user_id, name, slug)
				 VALUES ($1, 'legacy', 'legacy-' || md5(random()::text))
				 RETURNING id`, user).Scan(&accID); err != nil {
				t.Fatalf("create legacy account: %v", err)
			}

			// Before any content: eligible for reuse.
			if got, err := sp.SoleEmptyUnscopedAccount(ctx, user); err != nil {
				t.Fatalf("SoleEmptyUnscopedAccount (empty): %v", err)
			} else if got != accID {
				t.Fatalf("an empty unscoped account was not offered for reuse: got %q want %q", got, accID)
			}

			tc.seed(t, accID)

			got, err := sp.SoleEmptyUnscopedAccount(ctx, user)
			if err != nil {
				t.Fatalf("SoleEmptyUnscopedAccount (with %s): %v", tc.name, err)
			}
			if got != "" {
				t.Fatalf("an account holding %s was offered for reuse (got %q) — linking it "+
					"would move live data as a side effect of creating a workspace", tc.name, got)
			}
		})
	}
}

// TestSoleEmptyUnscopedAccountRefusesToPickAmongSeveral pins the "exactly one
// account" condition: with two workspace-less accounts there is no unambiguous
// answer, so the caller must fall back to the fresh-account path rather than
// silently linking whichever row the planner returned first.
func TestSoleEmptyUnscopedAccountRefusesToPickAmongSeveral(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)

	user := testOwnerUser(t, ctx, p)
	for i := 0; i < 2; i++ {
		if _, err := p.Exec(ctx,
			`INSERT INTO accounts (owner_user_id, name, slug)
			 VALUES ($1, 'multi', 'multi-' || md5(random()::text))`, user); err != nil {
			t.Fatalf("create account %d: %v", i, err)
		}
	}

	got, err := sp.SoleEmptyUnscopedAccount(ctx, user)
	if err != nil {
		t.Fatalf("SoleEmptyUnscopedAccount: %v", err)
	}
	if got != "" {
		t.Fatalf("SoleEmptyUnscopedAccount picked %q among several accounts — "+
			"there is no unambiguous account to reuse, the caller must create a fresh one", got)
	}
}

// TestCreateForAccountIsOwnerChecked pins that the link can never reach somebody
// else's account, and that a second link attempt fails rather than silently
// succeeding (the RowsAffected guard).
func TestCreateForAccountIsOwnerChecked(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)

	owner := testOwnerUser(t, ctx, p)
	stranger := testOwnerUser(t, ctx, p)

	var strangerAcc string
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1, 'stranger', 'stranger-' || md5(random()::text))
		 RETURNING id`, stranger).Scan(&strangerAcc); err != nil {
		t.Fatalf("create stranger account: %v", err)
	}

	if _, err := sp.CreateForAccount(ctx, owner, "Steal", "steal-"+randSuffix(), strangerAcc); err == nil {
		t.Fatal("CreateForAccount linked ANOTHER user's account into the caller's workspace — the link is not owner-checked")
	}

	// The workspace insert is rolled back with the failed link, so the caller's
	// workspace list is unchanged.
	if ws, err := sp.ByMember(ctx, owner); err != nil {
		t.Fatalf("ByMember: %v", err)
	} else if len(ws) != 0 {
		t.Fatalf("a failed CreateForAccount still left %d workspaces behind — the tx is not atomic", len(ws))
	}
	// And the stranger's account is untouched.
	var wsID *string
	if err := p.QueryRow(ctx, `SELECT workspace_id FROM accounts WHERE id=$1`, strangerAcc).Scan(&wsID); err != nil {
		t.Fatalf("read stranger account: %v", err)
	}
	if wsID != nil {
		t.Fatalf("the stranger's account was linked to workspace %q", *wsID)
	}
}

// TestCreateForAccountSyncsDependentRows pins the explicit workspace_id sync.
// Migration 000045's trg_forms_sync_workspace_id fires only on INSERT / UPDATE OF
// account_id on forms, so linking the ACCOUNT does not move the rows hanging off
// it — the precedent is migration 000012, which synced these tables explicitly.
// Under the content guard the rows are normally absent, so this seeds them behind
// the guard to prove the sync itself works for any caller.
func TestCreateForAccountSyncsDependentRows(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)

	user := testOwnerUser(t, ctx, p)
	var accID string
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1, 'sync', 'sync-' || md5(random()::text))
		 RETURNING id`, user).Scan(&accID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	for _, stmt := range []string{
		`INSERT INTO forms (account_id, slug, title, config, theme)
		 VALUES ($1, 'sync-'||md5(random()::text), 'sync form', '{}'::jsonb, '{}'::jsonb)`,
		`INSERT INTO api_keys (account_id, name, key_hash)
		 VALUES ($1, 'sync key', 'h-' || md5(random()::text))`,
	} {
		if _, err := p.Exec(ctx, stmt, accID); err != nil {
			t.Fatalf("seed dependent row: %v", err)
		}
	}

	ws, err := sp.CreateForAccount(ctx, user, "Sync", "sync-"+randSuffix(), accID)
	if err != nil {
		t.Fatalf("CreateForAccount: %v", err)
	}

	for _, table := range []string{"forms", "products", "sales", "api_keys"} {
		var unscoped int
		if err := p.QueryRow(ctx,
			`SELECT count(*) FROM `+table+` WHERE account_id=$1 AND workspace_id IS NULL`, accID).Scan(&unscoped); err != nil {
			t.Fatalf("count unscoped %s: %v", table, err)
		}
		if unscoped != 0 {
			t.Fatalf("%d %s row(s) kept a NULL workspace_id after their account was linked to %q — "+
				"CreateForAccount's explicit sync is missing (the 000045 trigger does not fire on an account update)",
				unscoped, table, ws.Slug)
		}
	}
}

// TestSoleEmptyUnscopedAccountKeepsTheCallersKey pins the OTHER half of the
// boundary: the caller's own API key must NOT disqualify their signup account
// from being reused as the first workspace's account.
//
// WHY. Every /mcp caller authenticates through the api_keys table
// (mcpsrv/http.go → Keys.LookupByKeyHash), and the documented create_api_key
// tool writes that row. So an `api_keys` clause here made the reuse branch
// unreachable on the transport that needs it most: create_workspace fell through
// to Workspaces.Create, the user ended up owning TWO accounts, and the agent's
// credential was stranded on the workspace-less one — the dashboard could
// neither list nor revoke it, because /v1/rest/keys resolves through the scoped
// account. The same clause also broke REST for any key-holding user.
//
// A key is a credential, not content: it has no public host slug and no
// published URL, so there is nothing a link can move, and CreateForAccount
// already syncs api_keys.workspace_id with the account. The key must therefore
// travel WITH its account into the new workspace — which is the second assertion
// here, because "found the account" and "the key is still reachable afterwards"
// are two different claims and only the second one is user-visible.
func TestSoleEmptyUnscopedAccountKeepsTheCallersKey(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewWorkspaceStore(p)

	user := testOwnerUser(t, ctx, p)

	// The post-signup state, PLUS the credential every MCP caller holds.
	var signupAcc, keyID string
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1, 'signup', 'signup-' || md5(random()::text))
		 RETURNING id`, user).Scan(&signupAcc); err != nil {
		t.Fatalf("create signup account: %v", err)
	}
	if err := p.QueryRow(ctx,
		`INSERT INTO api_keys (account_id, name, key_hash)
		 VALUES ($1, 'agent-key', 'h-' || md5(random()::text))
		 RETURNING id`, signupAcc).Scan(&keyID); err != nil {
		t.Fatalf("create api key: %v", err)
	}

	// The account is still the ONE the create path may reuse. Before the fix
	// this returned "" for exactly this shape.
	got, err := sp.SoleEmptyUnscopedAccount(ctx, user)
	if err != nil {
		t.Fatalf("SoleEmptyUnscopedAccount: %v", err)
	}
	if got != signupAcc {
		t.Fatalf("SoleEmptyUnscopedAccount = %q, want the key-holding signup account %q — an "+
			"account that holds only an API key is still the account to reuse; excluding it leaves "+
			"every MCP caller owning two accounts (the key is a credential, not published content)", got, signupAcc)
	}

	ws, err := sp.CreateForAccount(ctx, user, "Ask First", "keeps-key-"+randSuffix(), got)
	if err != nil {
		t.Fatalf("CreateForAccount: %v", err)
	}

	// One account, linked — the no-double-account assertion.
	var total int
	if err := p.QueryRow(ctx,
		`SELECT count(*) FROM accounts WHERE owner_user_id=$1`, user).Scan(&total); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if total != 1 {
		t.Fatalf("the user owns %d accounts after creating their first workspace, want 1", total)
	}

	// And the caller's key must now resolve inside THEIR workspace, not on a
	// stranded account. This is the harm the card measured: the dashboard's
	// key list is scoped to the workspace's accounts, so a key left behind on a
	// workspace-less account is invisible and unrevokable.
	var keyWorkspace *string
	if err := p.QueryRow(ctx,
		`SELECT k.workspace_id FROM api_keys k WHERE k.id=$1`, keyID).Scan(&keyWorkspace); err != nil {
		t.Fatalf("read key back: %v", err)
	}
	if keyWorkspace == nil || *keyWorkspace != ws.ID {
		t.Fatalf("the caller's API key points at workspace %v after the link, want %q — the credential "+
			"was stranded on an account the dashboard cannot reach from inside the workspace",
			keyWorkspace, ws.ID)
	}
}
