package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestSocialEnsureAccountForNewUser is an integration test for issue #298:
// after Google (social) login, a brand-new user must be auto-provisioned a
// default account so the dashboard's authed() routes (/api/workspaces,
// /api/rest/dashboard-bundle) resolve an account instead of 401ing. The
// session cookie itself was always set correctly — the gap was the account.
//
// Skips unless JOMFORM_TEST_DATABASE_URL + JOMFORM_TEST_VALKEY_URL are set.
func TestSocialEnsureAccountForNewUser(t *testing.T) {
	dbURL := getenv("JOMFORM_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping social account integration test")
	}
	if getenv("JOMFORM_TEST_VALKEY_URL") == "" {
		t.Skip("JOMFORM_TEST_VALKEY_URL not set; skipping social account integration test")
	}

	pool := bundleTestPool(t)
	ctx := context.Background()

	users := dbstore.NewUserStore(pool)
	accounts := dbstore.NewAccountStore(pool)
	workspaces := dbstore.NewWorkspaceStore(pool)
	sess := authTestValkey(t)

	a := &AuthHandlers{
		Sessions: sess,
		Users:    users,
		Accounts: accounts,
		Workspaces:   workspaces,
		BaseURL:  "http://localhost:8080",
	}

	// Simulate a brand-new social signup (only a users row, like
	// CreateFromSocial) with zero accounts.
	email := "social-" + randSuffix() + "@example.test"
	uid, err := users.CreateFromSocial(ctx, email, "Social Tester", "google", "gid-"+randSuffix(), "")
	if err != nil {
		t.Fatalf("CreateFromSocial: %v", err)
	}

	// Before the fix: no account exists → authed() resolution returns nil.
	before, berr := accounts.ByOwner(ctx, uid)
	if berr != nil || len(before) != 0 {
		t.Fatalf("pre-condition: expected 0 accounts for new social user, got %d (err %v)", len(before), berr)
	}

	// The callback's post-login path calls ensureAccountFor.
	req := httptest.NewRequest(http.MethodGet, "/post-login", nil)
	a.ensureAccountFor(req, uid, email)

	after, aerr := accounts.ByOwner(ctx, uid)
	if aerr != nil || len(after) != 1 {
		t.Fatalf("ensureAccountFor: expected exactly 1 account, got %d (err %v)", len(after), aerr)
	}
	if after[0].Name == "" || after[0].Slug == "" {
		t.Fatalf("provisioned account missing name/slug: %+v", after[0])
	}
	if after[0].Name == " Store" {
		t.Fatalf("account name fell back to bare ' Store': %q", after[0].Name)
	}

	// Idempotency: a second call must NOT create a duplicate.
	a.ensureAccountFor(req, uid, email)
	final, ferr := accounts.ByOwner(ctx, uid)
	if ferr != nil || len(final) != 1 {
		t.Fatalf("ensureAccountFor not idempotent: %d accounts (err %v)", len(final), ferr)
	}
}

func getenv(k string) string { return os.Getenv(k) }
