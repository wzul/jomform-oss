package dbstore

// Issue #539: social signup must record an EXPLICIT verified state instead of
// inheriting the schema default (000017 sets users.verified DEFAULT true), so a
// provider that will not assert the address cannot produce a verified account.

import (
	"context"
	"testing"
	"time"
)

// TestIssue539SocialSignupHonoursProviderClaim is the core guard.
func TestIssue539SocialSignupHonoursProviderClaim(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	us := NewUserStore(p)

	// The provider did NOT assert the address (GitHub /user never returns
	// email_verified; it decodes as false).
	email := "i539-unver-" + time.Now().Format("150405.000000") + "@example.test"
	id, err := us.CreateFromSocial(ctx, email, "Unverified Social", "github", "uid-"+email, "", false)
	if err != nil {
		t.Fatalf("CreateFromSocial(false): %v", err)
	}

	var verified bool
	if err := p.QueryRow(ctx, `SELECT verified FROM users WHERE id=$1`, id).Scan(&verified); err != nil {
		t.Fatalf("read verified: %v", err)
	}
	if verified {
		t.Fatalf("issue #539: an unverified provider claim produced verified=true — the row " +
			"inherited the schema DEFAULT true instead of the explicit value, bypassing the " +
			"email-verification gate and exempting the account from the unverified purge sweep")
	}
}

// TestIssue539SocialSignupVerifiedClaimStillWorks pins the happy path so the fix
// does not break normal social login.
func TestIssue539SocialSignupVerifiedClaimStillWorks(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	us := NewUserStore(p)

	email := "i539-ver-" + time.Now().Format("150405.000000") + "@example.test"
	id, err := us.CreateFromSocial(ctx, email, "Verified Social", "google", "uid-"+email, "", true)
	if err != nil {
		t.Fatalf("CreateFromSocial(true): %v", err)
	}

	var verified bool
	if err := p.QueryRow(ctx, `SELECT verified FROM users WHERE id=$1`, id).Scan(&verified); err != nil {
		t.Fatalf("read verified: %v", err)
	}
	if !verified {
		t.Fatal("a provider asserting email_verified=true must still produce a verified account")
	}
}

// TestIssue539UnverifiedSocialUserIsPurgeable proves the data-lifecycle fix: the
// account is now reachable by the unverified purge sweep, which previously could
// only ever see verified=false rows.
func TestIssue539UnverifiedSocialUserIsPurgeable(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	us := NewUserStore(p)

	email := "i539-purge-" + time.Now().Format("150405.000000") + "@example.test"
	id, err := us.CreateFromSocial(ctx, email, "Purge Social", "github", "uid-"+email, "", false)
	if err != nil {
		t.Fatalf("CreateFromSocial: %v", err)
	}
	if _, err := p.Exec(ctx,
		`UPDATE users SET created_at = now() - interval '400 days' WHERE id=$1`, id); err != nil {
		t.Fatalf("age the row: %v", err)
	}

	// The count is global, so assert the row is included by checking the guard
	// predicate directly rather than relying on a global number.
	var purgeable bool
	if err := p.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND verified=false
		                AND created_at < now() - interval '30 days')`, id).Scan(&purgeable); err != nil {
		t.Fatalf("purge predicate: %v", err)
	}
	if !purgeable {
		t.Fatal("issue #539: an unverified social account must be reachable by the unverified purge sweep")
	}
}
