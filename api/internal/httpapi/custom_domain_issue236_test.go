package httpapi

// Issue #236 regression integration test: a verified custom domain stored in
// the account_domains table (multi-domain, migration 000007) must resolve to
// its account slug so /f/{slug} serves the form on that host. Also covers the
// legacy accounts.custom_domain fallback.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when unset.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// customDomainFixture seeds a user + space + account + published form and
// returns the handlers plus the account slug and form slug.
func customDomainFixture(t *testing.T) (*PublicHandlers, string, string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID, spaceSlug, accountID, accountSlug string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO spaces (owner_user_id, name, slug)
		 VALUES ($1, 'it-space-' || md5(random()::text), 'it-' || md5(random()::text))
		 RETURNING slug`, userID).Scan(&spaceSlug); err != nil {
		t.Fatalf("create space: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, space_id)
		 SELECT $1, 'it-acc-' || md5(random()::text), 'it-' || md5(random()::text), id
		 FROM spaces WHERE slug = $2
		 RETURNING id, slug`, userID, spaceSlug).Scan(&accountID, &accountSlug); err != nil {
		t.Fatalf("create account: %v", err)
	}

	fs := dbstore.NewFormStore(pool)
	formSlug := "it-form-" + accountID[:8]
	f := &dbstore.Form{
		AccountID: accountID, Slug: formSlug, Title: "IT form", Status: "published",
		Config: map[string]any{"blocks": []any{map[string]any{"type": "text", "value": "Hello"}}},
		Theme:  map[string]any{"primary": "#5e6ad2"},
	}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("create form: %v", err)
	}

	h := &PublicHandlers{
		Forms:    fs,
		Accounts: dbstore.NewAccountStore(pool),
		Domains:  dbstore.NewDomainStore(pool),
		Spaces:   dbstore.NewSpaceStore(pool),
		BaseURL:  "http://localhost:8080",
	}
	return h, accountSlug, formSlug
}

// TestCustomDomainOwnerViaAccountDomains: a verified domain in account_domains
// resolves to the account slug (issue #236 — previously only the legacy
// accounts.custom_domain column was consulted, so multi-domain claims 404'd).
func TestCustomDomainOwnerViaAccountDomains(t *testing.T) {
	h, accountSlug, formSlug := customDomainFixture(t)
	ctx := context.Background()
	pool := bundleTestPool(t)

	// Claim + verify a domain in account_domains.
	domain := "it-" + accountSlug + ".example.test"
	// Resolve the account id from the slug.
	var accountID string
	if err := pool.QueryRow(ctx,
		`SELECT id FROM accounts WHERE slug = $1`, accountSlug).Scan(&accountID); err != nil {
		t.Fatalf("lookup account id: %v", err)
	}
	if _, err := h.Domains.AddDomain(ctx, accountID, domain, "cname"); err != nil {
		t.Fatalf("add domain: %v", err)
	}
	if err := h.Domains.MarkVerified(ctx, accountID, domain); err != nil {
		t.Fatalf("mark verified: %v", err)
	}

	// customDomainOwner must resolve the verified multi-domain claim.
	if got := customDomainOwner(ctx, h.Accounts, h.Domains, domain); got != accountSlug {
		t.Fatalf("customDomainOwner(%q) = %q, want %q", domain, got, accountSlug)
	}

	// And the full HTTP path must serve the form on that host.
	rec := publicServe(t, h, domain, "/f/"+formSlug)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /f/%s on custom domain status = %d, want 200 (body %s)", formSlug, rec.Code, rec.Body.String())
	}
}

// TestCustomDomainOwnerLegacyFallback: an account that claimed a single domain
// via the legacy accounts.custom_domain column still resolves (backward compat).
func TestCustomDomainOwnerLegacyFallback(t *testing.T) {
	h, accountSlug, formSlug := customDomainFixture(t)
	ctx := context.Background()
	pool := bundleTestPool(t)

	domain := "legacy-" + accountSlug + ".example.test"
	if _, err := pool.Exec(ctx,
		`UPDATE accounts SET custom_domain = $1, custom_domain_verified = true WHERE slug = $2`,
		domain, accountSlug); err != nil {
		t.Fatalf("set legacy custom_domain: %v", err)
	}

	if got := customDomainOwner(ctx, h.Accounts, h.Domains, domain); got != accountSlug {
		t.Fatalf("customDomainOwner(%q) = %q, want %q", domain, got, accountSlug)
	}

	rec := publicServe(t, h, domain, "/f/"+formSlug)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /f/%s on legacy custom domain status = %d, want 200 (body %s)", formSlug, rec.Code, rec.Body.String())
	}
}

// TestCustomDomainOwnerUnknownHost: an unregistered host resolves to "".
func TestCustomDomainOwnerUnknownHost(t *testing.T) {
	h, _, _ := customDomainFixture(t)
	ctx := context.Background()
	if got := customDomainOwner(ctx, h.Accounts, h.Domains, "nope.example.test"); got != "" {
		t.Fatalf("customDomainOwner(unknown) = %q, want \"\"", got)
	}
}

// TestCustomDomainRootServesStorefront: a verified custom domain's root "/"
// must serve the space storefront (list of published forms) instead of 404
// (issue #286). This mirrors how a space subdomain serves its root.
func TestCustomDomainRootServesStorefront(t *testing.T) {
	h, accountSlug, formSlug := customDomainFixture(t)
	ctx := context.Background()
	pool := bundleTestPool(t)

	var accountID string
	if err := pool.QueryRow(ctx,
		`SELECT id FROM accounts WHERE slug = $1`, accountSlug).Scan(&accountID); err != nil {
		t.Fatalf("lookup account id: %v", err)
	}
	var spaceSlug string
	if err := pool.QueryRow(ctx,
		`SELECT sp.slug FROM spaces sp JOIN accounts a ON a.space_id = sp.id WHERE a.id = $1`,
		accountID).Scan(&spaceSlug); err != nil {
		t.Fatalf("lookup space slug: %v", err)
	}

	domain := "root-" + accountSlug + ".example.test"
	if _, err := h.Domains.AddDomain(ctx, accountID, domain, "cname"); err != nil {
		t.Fatalf("add domain: %v", err)
	}
	if err := h.Domains.MarkVerified(ctx, accountID, domain); err != nil {
		t.Fatalf("mark verified: %v", err)
	}

	// Custom-domain root serves the storefront (200 + form slug in body).
	rec := publicServe(t, h, domain, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / on custom domain status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), formSlug) {
		t.Fatalf("GET / on custom domain body missing form slug %q (body %s)", formSlug, rec.Body.String())
	}

	// Space subdomain root still serves its storefront (unchanged).
	rec = publicServe(t, h, spaceSlug+".jomform.com", "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / on space subdomain status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), formSlug) {
		t.Fatalf("GET / on space subdomain body missing form slug %q (body %s)", formSlug, rec.Body.String())
	}

	// Bare apex (no subdomain, no custom domain) still 404s (issue #77).
	rec = publicServe(t, h, "jomform.com", "/")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET / on bare apex status = %d, want 404", rec.Code)
	}
}
