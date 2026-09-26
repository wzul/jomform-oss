package dbstore

// Regression: the public form URL must resolve to a host that actually SERVES
// the form, and there must be exactly ONE such host per workspace.
//
// THE HISTORY. Form URLs were built from WorkspaceSlugByAccount, which joins
// `accounts a JOIN workspaces sp ON sp.id = a.workspace_id`. AccountStore.Create
// — the insert the signup path uses (auth.go register, social.go, the magic-link
// signup) — did not set workspace_id, so that column was NULL for every merchant
// account created by signup and the join returned no row. Every caller swallowed
// the error and answered "": the dashboard's "View" action rendered `href=""`
// (so the click reloaded /forms), MCP list_forms returned an empty url, and a
// buyer's receipt fell back to the apex thank-you page.
//
// THE FIRST FIX was a fallback: the serving path tried the ACCOUNT slug first
// and only then the workspace join, and the URL builder was taught to do the
// same. That made the two agree — by making every workspace serve on TWO
// hostnames. The account slug is synthetic (`<workspace>-<8 hex of the account
// id>`, WorkspaceStore.Create) precisely so it cannot collide with the
// workspace's own slug, and no surface ever displays it: the dashboard shows
// `https://{workspace}.jomform.com` (settings/page.tsx). So a workspace was
// reachable at a second, invisible address.
//
// THE RULE NOW (Wan's decision, choice b): a workspace is served on its
// WORKSPACE subdomain, and nothing else. FormStore.ByWorkspaceSlug /
// PublishedByWorkspaceSlug and WorkspaceStore.PublicHostSlugByAccount resolve a
// WORKSPACE slug only.
//
// WHERE THE WORKSPACE COMES FROM — the half that was measured wrong once
// already, so it is pinned here rather than described. A signup creates an
// account and NO workspace (#718, Wan 2026-09-20: a user with zero workspaces is
// ASKED to create one, which is what keeps /workspace-picker reachable; #743
// re-affirmed the same boundary on 2026-09-24). So an account is linked to its
// workspace by the FIRST WORKSPACE CREATE, not by signup:
// WorkspaceStore.SoleEmptyUnscopedAccount + CreateForAccount, the ask-first path
// rest_workspaces.go and the MCP create_workspace tool both use. Both halves are
// asserted below — the signup account really is unlinked, and after the ask-first
// create its host really serves.
//
// This file pins BOTH the single-host rule and that boundary. The assertions call
// the real lookups rather than comparing strings to a literal, so a slug that
// merely looks plausible cannot pass: the host has to resolve the form through
// the serving path's own query.
//
// Skipped without JOMFORM_TEST_DATABASE_URL.
import (
	"context"
	"testing"
)

// signupAccount creates a user and the account the SIGNUP path creates for it —
// via AccountStore.Create, which links NO workspace (#718) — then links it the
// way the user's first workspace create does (CreateForAccount, the ask-first
// path). It returns the account id, its account slug and its workspace slug.
//
// The account slug is then rewritten to the SYNTHETIC form production carries
// (`<slug>-<8 hex of the account id>`), which is what WorkspaceStore.Create
// gives every new workspace's account and what migration 000024 gave the
// pre-existing ones. That reproduces the shape this rule is about: the account
// slug and the workspace slug DIFFER, so "served on the workspace subdomain"
// and "served on either name" are distinguishable outcomes rather than the same
// string twice.
func signupAccount(t *testing.T, ctx context.Context, slugPrefix string) (accountID, accountSlug, workspaceSlug string) {
	t.Helper()
	pool := testPool(t)
	ws := NewWorkspaceStore(pool)
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-pub-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	acc := &Account{
		OwnerUserID: userID,
		Name:        "it-signup-store",
		Slug:        slugPrefix + "-" + userID[:8],
	}
	if err := NewAccountStore(pool).Create(ctx, acc); err != nil {
		t.Fatalf("create account via the signup path: %v", err)
	}
	// Fixture precondition, asserted rather than assumed: signup links NO
	// workspace. This is #718's ask-first boundary and the reason the
	// NULL-workspace shape still exists at all — a change that starts linking a
	// workspace HERE makes /workspace-picker unreachable and fails on this line
	// instead of in the login-path guards.
	var wid *string
	if err := pool.QueryRow(ctx, `SELECT workspace_id::text FROM accounts WHERE id=$1`, acc.ID).Scan(&wid); err != nil {
		t.Fatalf("read workspace_id: %v", err)
	}
	if wid != nil {
		t.Fatalf("the signup path linked account %s to workspace %s — a signup creates an account and NO "+
			"workspace (#718): a user with zero workspaces must be ASKED to create one, and that prompt is "+
			"what keeps this flow reachable", acc.ID, *wid)
	}

	// The user's first workspace create — the ask-first path, which REUSES the
	// signup account instead of inserting a second one (issue #158).
	wsSlug := slugPrefix + "-ws-" + userID[:8]
	if _, err := ws.CreateForAccount(ctx, userID, "it-signup-ws", wsSlug, acc.ID); err != nil {
		t.Fatalf("link the signup account to its first workspace (ask-first create): %v", err)
	}

	synth := slugPrefix + "-" + userID[:8] + "-" + acc.ID[:8]
	if _, err := pool.Exec(ctx, `UPDATE accounts SET slug=$2 WHERE id=$1`, acc.ID, synth); err != nil {
		t.Fatalf("rewrite account slug to the synthetic production shape: %v", err)
	}
	var gotWS string
	if err := pool.QueryRow(ctx,
		`SELECT sp.slug FROM accounts a JOIN workspaces sp ON sp.id = a.workspace_id WHERE a.id=$1`,
		acc.ID).Scan(&gotWS); err != nil {
		t.Fatalf("read workspace slug: %v", err)
	}
	if gotWS == synth {
		t.Fatalf("fixture is not discriminating: account slug == workspace slug (%q)", synth)
	}
	return acc.ID, synth, gotWS
}

func TestPublicHostSlugResolvesForASignupCreatedAccount(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	ws := NewWorkspaceStore(pool)
	fs := NewFormStore(pool)

	accountID, accountSlug, workspaceSlug := signupAccount(t, ctx, "it-signup")

	// The resolver answers the account's WORKSPACE slug — the host the dashboard
	// displays and the public server serves. It must never answer "" for an
	// account that exists: an empty answer is what rendered href="" on the
	// dashboard and pointed a buyer's receipt at the apex page.
	slug, err := ws.PublicHostSlugByAccount(ctx, accountID)
	if err != nil {
		t.Fatalf("PublicHostSlugByAccount: %v", err)
	}
	if slug == "" {
		t.Fatalf("PublicHostSlugByAccount returned \"\" for account %s — a form URL built from this is "+
			"empty, which renders href=\"\" on the dashboard and points a buyer's receipt at the apex "+
			"thank-you page", accountID)
	}
	if slug != workspaceSlug {
		t.Errorf("PublicHostSlugByAccount = %q, want the WORKSPACE slug %q (this account's slug is the "+
			"synthetic %q, which no surface displays)", slug, workspaceSlug, accountSlug)
	}

	// The decisive property: the slug resolves the form through the SERVING
	// path's own lookup. A slug that merely looks plausible would 404 for the
	// merchant; this is what makes the URL a real one.
	form := &Form{AccountID: accountID, Slug: "it-pub-form-" + accountID[:8], Title: "Signup form", Status: FormStatusPublished}
	if err := fs.Create(ctx, form); err != nil {
		t.Fatalf("create form: %v", err)
	}
	served, err := fs.ByWorkspaceSlug(ctx, slug, form.Slug)
	if err != nil {
		t.Fatalf("ByWorkspaceSlug(%q, %q) — the host this resolver emits does not serve the form: %v",
			slug, form.Slug, err)
	}
	if served.ID != form.ID {
		t.Fatalf("ByWorkspaceSlug resolved form %s, want %s", served.ID, form.ID)
	}

	// And the ACCOUNT slug is not a host any more. This is the rule this card
	// exists to enforce; without it the fallback is quietly back.
	if _, err := fs.ByWorkspaceSlug(ctx, accountSlug, form.Slug); err == nil {
		t.Errorf("ByWorkspaceSlug(%q, %q) resolved the form through the ACCOUNT slug — a workspace must be "+
			"served on its workspace subdomain (%q) and nothing else; the account slug is the namespace a "+
			"verified custom domain resolves through, not a second hostname",
			accountSlug, form.Slug, workspaceSlug)
	}
	if _, err := fs.PublishedByWorkspaceSlug(ctx, accountSlug, form.Slug); err == nil {
		t.Errorf("PublishedByWorkspaceSlug(%q, %q) resolved the form through the ACCOUNT slug — the "+
			"published lookup must be workspace-only too", accountSlug, form.Slug)
	}
}

// TestASignupAccountHasNoPublicHostUntilItIsGivenAWorkspace pins the other half
// of the boundary, and it is the half whose absence made the previous rework
// wrong: while a signup account is UNLINKED (the #718 shape, before the user is
// asked to create a workspace) there is no host for it, and the URL builder says
// so instead of naming a synthetic address the dashboard never shows.
//
// This is asserted rather than left implicit because "every account has a
// workspace" is exactly the invariant a future change is tempted to restore by
// linking one at signup — which is the regression that broke #718.
func TestASignupAccountHasNoPublicHostUntilItIsGivenAWorkspace(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	ws := NewWorkspaceStore(pool)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-pub-nohost-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	acc := &Account{OwnerUserID: userID, Name: "it-nohost-store", Slug: "it-nohost-" + userID[:8]}
	if err := NewAccountStore(pool).Create(ctx, acc); err != nil {
		t.Fatalf("create account via the signup path: %v", err)
	}

	// No workspace yet: the resolver reports that plainly. It must NOT fall back
	// to the account slug — that fallback is the second hostname this rule
	// removed, and naming it is what put the invisible address into a merchant's
	// own dashboard URL.
	slug, _ := ws.PublicHostSlugByAccount(ctx, acc.ID)
	if slug != "" {
		t.Errorf("PublicHostSlugByAccount = %q for an account with no workspace, want \"\" — there is no "+
			"host for it until the user creates one (the ask-first flow), and answering the ACCOUNT slug "+
			"here is the invisible second hostname this rule removes", slug)
	}

	// The ask-first create is what gives it a host, and that host is the one the
	// URL builder then names.
	host := "it-nohost-ws-" + userID[:8]
	if _, err := ws.CreateForAccount(ctx, userID, "it-nohost-ws", host, acc.ID); err != nil {
		t.Fatalf("ask-first create: %v", err)
	}
	got, err := ws.PublicHostSlugByAccount(ctx, acc.ID)
	if err != nil {
		t.Fatalf("PublicHostSlugByAccount after the ask-first create: %v", err)
	}
	if got != host {
		t.Errorf("PublicHostSlugByAccount = %q after the ask-first create, want %q", got, host)
	}
}

// TestPublicHostSlugIsTheWorkspaceSlugAndNotTheAccountSlug covers the explicit
// "new workspace" path (rest_workspaces.go -> WorkspaceStore.Create), where the
// account slug is deliberately synthetic (`it-pub-workspace-<8 hex>`), so the
// two names really do differ.
func TestPublicHostSlugIsTheWorkspaceSlugAndNotTheAccountSlug(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	ws := NewWorkspaceStore(pool)
	fs := NewFormStore(pool)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-pub-ws-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	// WorkspaceStore.Create makes the workspace AND a linked account.
	workspace, err := ws.Create(ctx, userID, "it-pub-workspace", "it-pub-ws-"+userID[:8])
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	acc, err := NewAccountStore(pool).ByWorkspaceID(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("account for workspace: %v", err)
	}
	// Fixture precondition: the two slugs must actually differ, or this test
	// cannot tell "workspace only" from "either one".
	if acc.Slug == workspace.Slug {
		t.Fatalf("fixture is not discriminating: account slug == workspace slug (%q)", acc.Slug)
	}

	slug, err := ws.PublicHostSlugByAccount(ctx, acc.ID)
	if err != nil {
		t.Fatalf("PublicHostSlugByAccount: %v", err)
	}
	if slug != workspace.Slug {
		t.Errorf("PublicHostSlugByAccount = %q, want the WORKSPACE slug %q — a merchant who named a "+
			"workspace keeps that host, and it is the only host the public server serves", slug, workspace.Slug)
	}
	if got, err := ws.WorkspaceSlugByAccount(ctx, acc.ID); err != nil || got != workspace.Slug {
		t.Fatalf("WorkspaceSlugByAccount = %q/%v, want %q", got, err, workspace.Slug)
	}

	form := &Form{AccountID: acc.ID, Slug: "it-pub-ws-form-" + acc.ID[:8], Title: "Workspace form", Status: FormStatusPublished}
	if err := fs.Create(ctx, form); err != nil {
		t.Fatalf("create form: %v", err)
	}
	served, err := fs.ByWorkspaceSlug(ctx, workspace.Slug, form.Slug)
	if err != nil {
		t.Fatalf("ByWorkspaceSlug(%q, %q): %v", workspace.Slug, form.Slug, err)
	}
	if served.ID != form.ID {
		t.Fatalf("ByWorkspaceSlug resolved form %s, want %s", served.ID, form.ID)
	}
	// The account-slug host that used to answer here must be gone.
	if _, err := fs.ByWorkspaceSlug(ctx, acc.Slug, form.Slug); err == nil {
		t.Errorf("ByWorkspaceSlug(%q, %q) resolved the form through the ACCOUNT slug — %q.jomform.com "+
			"must no longer serve this workspace", acc.Slug, form.Slug, acc.Slug)
	}
}
