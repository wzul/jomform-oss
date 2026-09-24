package httpapi

// Legacy onboarding at LOGIN: an existing user who already owns forms and has no
// workspace gets one provisioned, with his forms attached, before his first page
// load.
//
// These drive the REAL login handler over real Postgres + Valkey, so what is
// asserted is the wiring — that the provisioning actually runs on the login path
// — rather than that some function could be called. A store-level test (see
// dbstore/workspace_legacy_test.go) cannot catch the interesting failure here,
// which is the call site being absent: the rule exists, its tests pass, and the
// user still logs in to an empty workspace list.
//
// The two halves are the boundary Wan drew (2026-09-20): *has forms, no
// workspace* → provision; *no forms, no workspace* → ASK. So both directions are
// asserted, and the second one (a fresh user must NOT be provisioned) is the half
// that a one-sided test would let rot into "everyone gets a workspace".
//
// Skipped without JOMFORM_TEST_DATABASE_URL / JOMFORM_TEST_VALKEY_URL.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// authFixtureWithWorkspaces is authTestFixture plus the Workspaces store the
// login-path provisioning needs. Kept as its own helper so the existing
// auth-hardening tests (which assert against a fixture that deliberately has no
// Workspaces wired, i.e. the store-absent path) are untouched.
func authFixtureWithWorkspaces(t *testing.T) *AuthHandlers {
	t.Helper()
	h, _, _ := authTestFixture(t)
	h.Workspaces = dbstore.NewWorkspaceStore(bundleTestPool(t))
	return h
}

// legacyLoginFixture creates a real, verified, password-loginable user through the
// registration + verification flow (so the login below is a genuine one and not
// a hand-built session), then attaches `forms` published forms to the account the
// signup created — the exact shape measured on prod: an account,
// accounts.workspace_id IS NULL, and forms hanging off it.
func legacyLoginFixture(t *testing.T, h *AuthHandlers, forms int) (user, account, email string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	// A unique IP per test: the login lockout counter lives in the shared test
	// Valkey with a 15-minute TTL (issue #474).
	ip := uniqueTestIP()
	email = fmt.Sprintf("legacy-login-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", ip)

	if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE email=$1`, email).Scan(&user); err != nil {
		t.Fatalf("resolve registered user: %v", err)
	}
	// The account registration created — NOT a hand-made one, so the fixture
	// cannot accidentally test a shape no signup produces.
	if err := pool.QueryRow(ctx,
		`SELECT id FROM accounts WHERE owner_user_id=$1 ORDER BY created_at LIMIT 1`, user).
		Scan(&account); err != nil {
		t.Fatalf("resolve the signup account: %v", err)
	}

	for i := 0; i < forms; i++ {
		if _, err := pool.Exec(ctx,
			`INSERT INTO forms (account_id, slug, title, config, theme, status)
			 VALUES ($1, 'legacy-login-' || md5(random()::text), 'legacy', '{}'::jsonb, '{}'::jsonb, 'published')`,
			account); err != nil {
			t.Fatalf("seed form %d: %v", i, err)
		}
	}
	return user, account, email
}

// TestLoginProvisionsLegacyWorkspaceForAnExistingUser is the guard that bites for
// the card's criterion 1/2: an existing user with forms but no workspace can
// reach his forms after a login.
//
// Neutralise the fix by deleting the `a.provisionLegacyWorkspace(r, userID)` call
// from startSession (or by making the store's LegacyUnscopedAccount return ""),
// and this test goes red on the workspace count and on the unscoped-form count
// while every store-level test still passes.
func TestLoginProvisionsLegacyWorkspaceForAnExistingUser(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	h := authFixtureWithWorkspaces(t)

	user, account, email := legacyLoginFixture(t, h, 2)

	// Before the login: the prod shape — a form-owning account with no workspace.
	var preWorkspaces int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE owner_user_id=$1`, user).Scan(&preWorkspaces); err != nil {
		t.Fatalf("count workspaces before: %v", err)
	}
	if preWorkspaces != 0 {
		t.Fatalf("fixture is wrong: the user already owns %d workspaces", preWorkspaces)
	}

	rec := postJSON(loginMux(h), "/v1/auth/login",
		map[string]any{"email": email, "password": "password123"}, uniqueTestIP())
	if rec.Code != 200 {
		t.Fatalf("login status = %d (body %s)", rec.Code, rec.Body.String())
	}

	// The user can now REACH a workspace — the point of the whole card.
	list, err := dbstore.NewWorkspaceStore(pool).ByMember(ctx, user)
	if err != nil {
		t.Fatalf("ByMember: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("after login the user sees %d workspaces, want exactly 1 — a user with forms and "+
			"no workspace must get one provisioned at login (Wan, 2026-09-20)", len(list))
	}

	// And his forms came with it: no form may keep a NULL workspace_id. This is
	// the assertion that goes red for the silent failure mode, because linking
	// only the ACCOUNT leaves every form unscoped with nothing erroring.
	var unscoped int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM forms WHERE account_id=$1 AND workspace_id IS NULL`, account).
		Scan(&unscoped); err != nil {
		t.Fatalf("count unscoped forms: %v", err)
	}
	if unscoped != 0 {
		t.Fatalf("%d form(s) kept workspace_id IS NULL after the login provision — the account was "+
			"linked but its forms were not moved (trg_forms_sync_workspace_id does not fire on an "+
			"account UPDATE)", unscoped)
	}

	// Exactly one workspace and one account: a provision that went through
	// Workspaces.Create would leave the user owning two accounts (issue #158).
	var workspaces, accounts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE owner_user_id=$1`, user).Scan(&workspaces); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE owner_user_id=$1`, user).Scan(&accounts); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if workspaces != 1 || accounts != 1 {
		t.Fatalf("after provisioning: %d workspaces / %d accounts, want 1 / 1", workspaces, accounts)
	}

	// Idempotent: a SECOND login provisions nothing further.
	rec2 := postJSON(loginMux(h), "/v1/auth/login",
		map[string]any{"email": email, "password": "password123"}, uniqueTestIP())
	if rec2.Code != 200 {
		t.Fatalf("second login status = %d (body %s)", rec2.Code, rec2.Body.String())
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE owner_user_id=$1`, user).Scan(&workspaces); err != nil {
		t.Fatalf("count workspaces after re-login: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE owner_user_id=$1`, user).Scan(&accounts); err != nil {
		t.Fatalf("count accounts after re-login: %v", err)
	}
	if workspaces != 1 || accounts != 1 {
		t.Fatalf("a second login changed the user to %d workspaces / %d accounts — provisioning must "+
			"be idempotent", workspaces, accounts)
	}
}

// TestLoginDoesNotProvisionForAFreshUser pins the OTHER half of the boundary, so
// the ask-first prompt cannot be silently turned into dead code: a user with no
// forms is ASKED to create a workspace, never given one.
func TestLoginDoesNotProvisionForAFreshUser(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	h := authFixtureWithWorkspaces(t)

	user, account, email := legacyLoginFixture(t, h, 0)

	rec := postJSON(loginMux(h), "/v1/auth/login",
		map[string]any{"email": email, "password": "password123"}, uniqueTestIP())
	if rec.Code != 200 {
		t.Fatalf("login status = %d (body %s)", rec.Code, rec.Body.String())
	}

	var workspaces int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE owner_user_id=$1`, user).Scan(&workspaces); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if workspaces != 0 {
		t.Fatalf("a user with NO forms was provisioned %d workspace(s) at login — he must be ASKED to "+
			"create one (the /workspace-picker first-run flow), and provisioning him here makes that "+
			"prompt unreachable", workspaces)
	}
	var wsID *string
	if err := pool.QueryRow(ctx, `SELECT workspace_id FROM accounts WHERE id=$1`, account).Scan(&wsID); err != nil {
		t.Fatalf("read account: %v", err)
	}
	if wsID != nil {
		t.Fatalf("the fresh user's account was linked to workspace %q", *wsID)
	}
}
