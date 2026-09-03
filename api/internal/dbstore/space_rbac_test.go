package dbstore

// Integration tests for space RBAC access resolution (issue #162): members
// (not just owners) must resolve to a space's accounts with their role.
// Skipped unless JOMFORM_TEST_DATABASE_URL is set; migrations are applied
// first, mirroring the production boot path.

import (
	"context"
	"testing"
)

// TestSpaceAccessBySlugOwnerAndMember verifies AccessBySlug resolves the
// account set + role for both the owner (admin) and a member (their role).
func TestSpaceAccessBySlugOwnerAndMember(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewSpaceStore(p)
	sm := NewSpaceMemberStore(p)

	owner := testOwnerUser(t, ctx, p)
	space, err := sp.Create(ctx, owner, "RBAC Space", "rbac-"+randSuffix())
	if err != nil {
		t.Fatalf("create space: %v", err)
	}

	// Owner resolves as admin with the space's account.
	acc, ok, err := sp.AccessBySlug(ctx, space.Slug, owner)
	if err != nil {
		t.Fatalf("access by slug (owner): %v", err)
	}
	if !ok {
		t.Fatal("owner should have access")
	}
	if acc.Role != RoleAdmin {
		t.Fatalf("owner role = %q, want admin", acc.Role)
	}
	if len(acc.Accounts) != 1 {
		t.Fatalf("owner accounts = %d, want 1", len(acc.Accounts))
	}

	// A member with read_only resolves with that role + the same account.
	member := testOwnerUser(t, ctx, p)
	if err := sm.AddMember(ctx, space.ID, member, RoleReadOnly); err != nil {
		t.Fatalf("add member: %v", err)
	}
	macc, ok, err := sp.AccessBySlug(ctx, space.Slug, member)
	if err != nil {
		t.Fatalf("access by slug (member): %v", err)
	}
	if !ok {
		t.Fatal("member should have access")
	}
	if macc.Role != RoleReadOnly {
		t.Fatalf("member role = %q, want read_only", macc.Role)
	}
	if len(macc.Accounts) != 1 || macc.Accounts[0] != acc.Accounts[0] {
		t.Fatalf("member accounts = %v, want %v", macc.Accounts, acc.Accounts)
	}

	// A stranger has no access.
	stranger := testOwnerUser(t, ctx, p)
	if _, ok, err := sp.AccessBySlug(ctx, space.Slug, stranger); err != nil {
		t.Fatalf("access by slug (stranger): %v", err)
	} else if ok {
		t.Fatal("stranger should not have access")
	}
}

// TestSpaceByMemberListsOwnedAndJoined verifies ByMember returns spaces the
// user owns OR is a member of, with the correct role in each.
func TestSpaceByMemberListsOwnedAndJoined(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewSpaceStore(p)
	sm := NewSpaceMemberStore(p)

	owner := testOwnerUser(t, ctx, p)
	space, err := sp.Create(ctx, owner, "ByMember Space", "bymem-"+randSuffix())
	if err != nil {
		t.Fatalf("create space: %v", err)
	}

	// Owner sees the space as admin.
	owned, err := sp.ByMember(ctx, owner)
	if err != nil {
		t.Fatalf("by member (owner): %v", err)
	}
	if len(owned) != 1 || owned[0].SpaceID != space.ID || owned[0].Role != RoleAdmin {
		t.Fatalf("owner spaces = %+v, want 1 admin", owned)
	}

	// A member sees the space with their role.
	member := testOwnerUser(t, ctx, p)
	if err := sm.AddMember(ctx, space.ID, member, RoleManage); err != nil {
		t.Fatalf("add member: %v", err)
	}
	joined, err := sp.ByMember(ctx, member)
	if err != nil {
		t.Fatalf("by member (member): %v", err)
	}
	if len(joined) != 1 || joined[0].SpaceID != space.ID || joined[0].Role != RoleManage {
		t.Fatalf("member spaces = %+v, want 1 manage", joined)
	}

	// A stranger sees nothing.
	stranger := testOwnerUser(t, ctx, p)
	none, err := sp.ByMember(ctx, stranger)
	if err != nil {
		t.Fatalf("by member (stranger): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("stranger spaces = %d, want 0", len(none))
	}
}

// TestSpaceRoleByAccount verifies RoleByAccount resolves the caller's role in
// the space that owns an account (owner → admin, member → their role).
func TestSpaceRoleByAccount(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	sp := NewSpaceStore(p)
	sm := NewSpaceMemberStore(p)

	owner := testOwnerUser(t, ctx, p)
	space, err := sp.Create(ctx, owner, "RoleByAccount Space", "rba-"+randSuffix())
	if err != nil {
		t.Fatalf("create space: %v", err)
	}
	// The space's account id is the one linked at creation.
	var accountID string
	if err := p.QueryRow(ctx, `SELECT id FROM accounts WHERE space_id=$1`, space.ID).Scan(&accountID); err != nil {
		t.Fatalf("get space account: %v", err)
	}

	role, err := sp.RoleByAccount(ctx, accountID, owner)
	if err != nil {
		t.Fatalf("role by account (owner): %v", err)
	}
	if role != RoleAdmin {
		t.Fatalf("owner role = %q, want admin", role)
	}

	member := testOwnerUser(t, ctx, p)
	if err := sm.AddMember(ctx, space.ID, member, RoleReadOnly); err != nil {
		t.Fatalf("add member: %v", err)
	}
	role, err = sp.RoleByAccount(ctx, accountID, member)
	if err != nil {
		t.Fatalf("role by account (member): %v", err)
	}
	if role != RoleReadOnly {
		t.Fatalf("member role = %q, want read_only", role)
	}

	// A stranger has no role.
	stranger := testOwnerUser(t, ctx, p)
	role, err = sp.RoleByAccount(ctx, accountID, stranger)
	if err != nil {
		t.Fatalf("role by account (stranger): %v", err)
	}
	if role != "" {
		t.Fatalf("stranger role = %q, want empty", role)
	}
}
