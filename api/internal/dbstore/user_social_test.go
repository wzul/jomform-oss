package dbstore

// Integration test for issue #282: CreateFromSocial must never insert a NULL
// name (users.name is NOT NULL). When the social provider returns an empty
// name, the user is still created with a fallback (email local part, then
// "User"). Skipped unless JOMFORM_TEST_DATABASE_URL is set.

import (
	"context"
	"testing"
)

func TestCreateFromSocialEmptyName(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	store := NewUserStore(p)

	email := "social-empty-" + md5Rand() + "@example.test"
	uid, err := store.CreateFromSocial(ctx, email, "", "google", "uid-"+md5Rand(), "", true)
	if err != nil {
		t.Fatalf("CreateFromSocial with empty name: %v", err)
	}
	defer cleanupUsers(t, ctx, p, uid)

	// The user must exist and name must be non-NULL, falling back to the
	// email's local part.
	var name string
	if err := p.QueryRow(ctx, `SELECT name FROM users WHERE id=$1`, uid).Scan(&name); err != nil {
		t.Fatalf("select created user: %v", err)
	}
	if name == "" {
		t.Errorf("name is empty/NULL after social signup with empty provider name; want a fallback")
	}
}

func TestCreateFromSocialEmptyNameNoLocalPart(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	store := NewUserStore(p)

	// Email with no local part (e.g. "@example.test") and empty name → the
	// generic "User" fallback must be used so name is never NULL.
	email := "@" + md5Rand() + ".test"
	uid, err := store.CreateFromSocial(ctx, email, "", "google", "uid-"+md5Rand(), "", true)
	if err != nil {
		t.Fatalf("CreateFromSocial with empty name and no local part: %v", err)
	}
	defer cleanupUsers(t, ctx, p, uid)

	var name string
	if err := p.QueryRow(ctx, `SELECT name FROM users WHERE id=$1`, uid).Scan(&name); err != nil {
		t.Fatalf("select created user: %v", err)
	}
	if name != "User" {
		t.Errorf("name = %q, want %q (generic fallback)", name, "User")
	}
}
