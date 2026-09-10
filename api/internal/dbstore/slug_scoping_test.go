package dbstore

// Issue #77: slug scoping — a public payment link must be reachable only via
// its registered workspace subdomain. Two workspaces may share the same form slug;
// uniqueness is per-workspace, not global. Skipped unless JOMFORM_TEST_DATABASE_URL
// is set (migrations applied first, mirroring production boot).

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSlugReuseAcrossWorkspaces(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	fs := NewFormStore(p)
	ss := NewWorkspaceStore(p)

	// Two independent workspaces, each with its own account.
	accA, workspaceA := newWorkspaceAccount(t, ctx, p)
	accB, workspaceB := newWorkspaceAccount(t, ctx, p)
	if workspaceA == workspaceB {
		t.Fatalf("workspaces must differ, got %q", workspaceA)
	}

	const sharedSlug = "abcdef"

	// Same slug in both workspaces — must both succeed (per-workspace uniqueness).
	fa := &Form{AccountID: accA, Slug: sharedSlug, Title: "A", Status: "published"}
	if err := fs.Create(ctx, fa); err != nil {
		t.Fatalf("create form in workspace A: %v", err)
	}
	fb := &Form{AccountID: accB, Slug: sharedSlug, Title: "B", Status: "published"}
	if err := fs.Create(ctx, fb); err != nil {
		t.Fatalf("create form in workspace B: %v", err)
	}

	// Each workspace resolves its own form by the shared slug.
	gotA, err := fs.PublishedByWorkspaceSlug(ctx, workspaceA, sharedSlug)
	if err != nil {
		t.Fatalf("PublishedByWorkspaceSlug(workspace A): %v", err)
	}
	if gotA.ID != fa.ID {
		t.Fatalf("workspace A resolved form %s, want %s", gotA.ID, fa.ID)
	}
	gotB, err := fs.PublishedByWorkspaceSlug(ctx, workspaceB, sharedSlug)
	if err != nil {
		t.Fatalf("PublishedByWorkspaceSlug(workspace B): %v", err)
	}
	if gotB.ID != fb.ID {
		t.Fatalf("workspace B resolved form %s, want %s", gotB.ID, fb.ID)
	}

	// WorkspaceSlugByAccount resolves each account's workspace slug for URL building.
	slugA, err := ss.WorkspaceSlugByAccount(ctx, accA)
	if err != nil {
		t.Fatalf("WorkspaceSlugByAccount(A): %v", err)
	}
	if slugA != workspaceA {
		t.Fatalf("workspace slug for A = %q, want %q", slugA, workspaceA)
	}
	slugB, err := ss.WorkspaceSlugByAccount(ctx, accB)
	if err != nil {
		t.Fatalf("WorkspaceSlugByAccount(B): %v", err)
	}
	if slugB != workspaceB {
		t.Fatalf("workspace slug for B = %q, want %q", slugB, workspaceB)
	}
}

// newWorkspaceAccount creates a user, a workspace, and an account linked to that
// workspace, returning the account id and the workspace slug.
func newWorkspaceAccount(t *testing.T, ctx context.Context, p *pgxpool.Pool) (accountID, workspaceSlug string) {
	t.Helper()
	var userID string
	if err := p.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := p.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-workspace-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING slug`, userID).Scan(&workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $2
		 RETURNING id`, userID, workspaceSlug).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	return accountID, workspaceSlug
}
