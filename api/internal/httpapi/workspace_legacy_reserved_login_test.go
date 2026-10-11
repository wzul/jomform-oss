package httpapi

// The LOGIN-path half of card t_ada797d1: the auto-provision that runs on a
// successful sign-in must not mint a workspace on a name JomForm reserves, and
// must not leave the user without the workspace his forms need.
//
// WHY IT IS HERE AND NOT ONLY IN dbstore. The store-level guard drives
// ProvisionLegacyWorkspaceForOwner directly; it cannot see the wiring — that a
// real POST /v1/auth/login runs the provisioning, answers 200, and leaves the
// user owning exactly one workspace whose slug is servable. That call site is
// the whole subject of the card (auth.go:807).
//
// The derived name is JomForm's own guess from the user's email: the derivation
// takes the first five characters of the local part, so `docs-…@example.test`
// reduces to "docs", a label urlguard reserves (a routed JomForm surface). The
// user never chose it, so it must not be handed to him and must not be shown to
// him as a failure either.
//
// Skipped without JOMFORM_TEST_DATABASE_URL / JOMFORM_TEST_VALKEY_URL.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/urlguard"
)

// legacyReservedDerivedLoginFixture is legacyLoginFixture with an email local
// part that DERIVES a reserved name: "docs-<nanos>" is five characters wide, so
// the derivation window sees "docs-", trims the trailing dash and yields "docs".
// The nanos keep the address unique across runs, as the other fixtures do.
//
// The rest is deliberately the same shape as legacyLoginFixture: a real user
// created through registration + verification (so the login below is genuine),
// the account registration created for him, and published forms hanging off it
// with accounts.workspace_id still NULL.
func legacyReservedDerivedLoginFixture(t *testing.T, h *AuthHandlers, forms int) (user, account, email string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	email = fmt.Sprintf("docs-%d@example.test", time.Now().UnixNano())
	registerAndVerify(t, h, email, "password123", uniqueTestIP())

	if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE email=$1`, email).Scan(&user); err != nil {
		t.Fatalf("resolve registered user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT id FROM accounts WHERE owner_user_id=$1 ORDER BY created_at LIMIT 1`, user).
		Scan(&account); err != nil {
		t.Fatalf("resolve the signup account: %v", err)
	}
	for i := 0; i < forms; i++ {
		if _, err := pool.Exec(ctx,
			`INSERT INTO forms (account_id, slug, title, config, theme, status)
			 VALUES ($1, 'legacy-reserved-' || md5(random()::text), 'legacy', '{}'::jsonb, '{}'::jsonb, 'published')`,
			account); err != nil {
			t.Fatalf("seed form %d: %v", i, err)
		}
	}

	// The fixture must actually exercise the case, or the test proves nothing:
	// the address reduces to a reserved label.
	if derived := dbstore.SlugFromEmailLocalPart(email, user); derived != "docs" {
		t.Fatalf("SlugFromEmailLocalPart(%q) = %q, want %q — this fixture no longer derives a "+
			"reserved name", email, derived, "docs")
	}
	if !urlguard.ReservedSubdomain("docs") {
		t.Fatal("\"docs\" is not on the reserved list any more — this test can no longer prove the " +
			"login path refuses to mint it")
	}
	return user, account, email
}

// TestLoginProvisionSkipsAReservedDerivedName drives the real login handler for a
// user whose derived workspace name is reserved.
//
// Neutralise either half of the fix and it goes red: with the reserved leg
// dropped from the slug rule the login mints "docs"; with the provisioner
// refusing the reserved name outright the user ends the login with ZERO
// workspaces and every form still unscoped — he is stranded, because the login
// path only logs that error.
func TestLoginProvisionSkipsAReservedDerivedName(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	h := authFixtureWithWorkspaces(t)

	user, account, email := legacyReservedDerivedLoginFixture(t, h, 2)

	rec := postJSON(loginMux(h), "/v1/auth/login",
		map[string]any{"email": email, "password": "password123"}, uniqueTestIP())
	if rec.Code != 200 {
		t.Fatalf("login status = %d (body %s)", rec.Code, rec.Body.String())
	}

	// The user reaches exactly one workspace, and its slug is one the wildcard
	// host can serve — read back from the row, not from a returned struct.
	var slug string
	if err := pool.QueryRow(ctx,
		`SELECT slug FROM workspaces WHERE owner_user_id=$1`, user).Scan(&slug); err != nil {
		t.Fatalf("read the provisioned workspace: %v — a form-owning user must still end the login "+
			"with a workspace", err)
	}
	if urlguard.ReservedSubdomain(slug) {
		t.Fatalf("the login provisioned the workspace on the RESERVED host %q.jomform.com for %q",
			slug, email)
	}
	if _, err := dbstore.ValidateWorkspaceSlug(slug); err != nil {
		t.Fatalf("the provisioned slug %q is refused by the slug rule itself: %v", slug, err)
	}
	if !strings.HasPrefix(slug, "docs-") {
		t.Fatalf("slug = %q, want the ladder's next candidate under the derived name \"docs-\"", slug)
	}
	var workspaces int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE owner_user_id=$1`, user).
		Scan(&workspaces); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if workspaces != 1 {
		t.Fatalf("after login the user owns %d workspaces, want exactly 1", workspaces)
	}

	// His forms came with it: this is the half that a refusal (the behaviour this
	// card replaces) failed while minting nothing.
	var unscoped int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM forms WHERE account_id=$1 AND workspace_id IS NULL`, account).
		Scan(&unscoped); err != nil {
		t.Fatalf("count unscoped forms: %v", err)
	}
	if unscoped != 0 {
		t.Fatalf("%d form(s) kept workspace_id IS NULL after the login provision for %q — his "+
			"dashboard would show nothing", unscoped, email)
	}
}
