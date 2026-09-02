package dbstore

// Issue #77: slug scoping — a public payment link must be reachable only via
// its registered space subdomain. Two spaces may share the same form slug;
// uniqueness is per-space, not global. Skipped unless JOMFORM_TEST_DATABASE_URL
// is set (migrations applied first, mirroring production boot).

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSlugReuseAcrossSpaces(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	fs := NewFormStore(p)
	ss := NewSpaceStore(p)

	// Two independent spaces, each with its own account.
	accA, spaceA := newSpaceAccount(t, ctx, p)
	accB, spaceB := newSpaceAccount(t, ctx, p)
	if spaceA == spaceB {
		t.Fatalf("spaces must differ, got %q", spaceA)
	}

	const sharedSlug = "abcdef"

	// Same slug in both spaces — must both succeed (per-space uniqueness).
	fa := &Form{AccountID: accA, Slug: sharedSlug, Title: "A", Status: "published"}
	if err := fs.Create(ctx, fa); err != nil {
		t.Fatalf("create form in space A: %v", err)
	}
	fb := &Form{AccountID: accB, Slug: sharedSlug, Title: "B", Status: "published"}
	if err := fs.Create(ctx, fb); err != nil {
		t.Fatalf("create form in space B: %v", err)
	}

	// Each space resolves its own form by the shared slug.
	gotA, err := fs.PublishedBySpaceSlug(ctx, spaceA, sharedSlug)
	if err != nil {
		t.Fatalf("PublishedBySpaceSlug(space A): %v", err)
	}
	if gotA.ID != fa.ID {
		t.Fatalf("space A resolved form %s, want %s", gotA.ID, fa.ID)
	}
	gotB, err := fs.PublishedBySpaceSlug(ctx, spaceB, sharedSlug)
	if err != nil {
		t.Fatalf("PublishedBySpaceSlug(space B): %v", err)
	}
	if gotB.ID != fb.ID {
		t.Fatalf("space B resolved form %s, want %s", gotB.ID, fb.ID)
	}

	// SpaceSlugByAccount resolves each account's space slug for URL building.
	slugA, err := ss.SpaceSlugByAccount(ctx, accA)
	if err != nil {
		t.Fatalf("SpaceSlugByAccount(A): %v", err)
	}
	if slugA != spaceA {
		t.Fatalf("space slug for A = %q, want %q", slugA, spaceA)
	}
	slugB, err := ss.SpaceSlugByAccount(ctx, accB)
	if err != nil {
		t.Fatalf("SpaceSlugByAccount(B): %v", err)
	}
	if slugB != spaceB {
		t.Fatalf("space slug for B = %q, want %q", slugB, spaceB)
	}
}

// newSpaceAccount creates a user, a space, and an account linked to that
// space, returning the account id and the space slug.
func newSpaceAccount(t *testing.T, ctx context.Context, p *pgxpool.Pool) (accountID, spaceSlug string) {
	t.Helper()
	var userID string
	if err := p.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := p.QueryRow(ctx,
		`INSERT INTO spaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-space-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING slug`, userID).Scan(&spaceSlug); err != nil {
		t.Fatalf("create space: %v", err)
	}
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, space_id)
		 SELECT $1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text), id
		 FROM spaces WHERE slug = $2
		 RETURNING id`, userID, spaceSlug).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	return accountID, spaceSlug
}
