// t_586ff0c3 — the REST claim path refuses a reserved workspace subdomain.
//
// WHY THIS FILE EXISTS (and why the urlguard unit tests are not enough)
//
// internal/urlguard proves the LIST is right. It cannot prove the list is
// WIRED: a validator that never calls it, or that calls it after the row is
// already inserted, passes every unit test in that package. This file drives the
// real handler over the real route (POST /v1/workspaces, session-authed) and
// reads the effect back out of Postgres, so "the rule bites on the REST surface"
// is measured, not asserted.
//
// The assertions are deliberately on BOTH halves:
//   - a refusal for a name from each category (the hole), including the 200
//     measured on prod before this change (support, billing, status, admin,
//     docs, mcp, templates); and
//   - an ACCEPT for a legitimate name, plus a row that is really there — a
//     validator that refused everything would also pass the refusal half.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL) and Valkey
// (JOMFORM_TEST_VALKEY_URL) for the session, exactly like the neighbouring
// tenancy test. It does NOT skip when they are missing in the preflight gate —
// preflight requires both env vars and fails loudly if they are unset.

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/urlguard"
)

// reservedSlugFixture seeds one user with a live session cookie. Unlike
// tenancyFixture it creates NO workspace, so every slug this test claims is
// genuinely new and a 409 can never be mistaken for a reserved-name refusal.
func reservedSlugFixture(t *testing.T) (*RESTHandlers, string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)
	sess := authTestValkey(t)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('rsv-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	// A session-authed caller must already have an account: authed()'s
	// resolveSessionAccount falls back to the first OWNED account, and returns
	// 401 when there is none. Registration always auto-provisions one
	// (httpapi/auth.go: "Auto-provision the default account so new users can
	// build immediately"), so seeding it here reproduces a real registered
	// merchant rather than a state production never produces. Without it every
	// claim below answers 401 and the test would prove nothing about slugs.
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'rsv-' || md5(random()::text), 'rsv-' || md5(random()::text)
		 RETURNING id`, userID).Scan(new(string)); err != nil {
		t.Fatalf("create default account: %v", err)
	}

	token, err := sess.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	h := newBundleHandlers(pool)
	h.Sessions = sess
	return h, token
}

// claimSlug POSTs a workspace claim through the registered route and returns the
// status and the decoded body.
func claimSlug(t *testing.T, h *RESTHandlers, token, name, slug string) (int, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	body, err := json.Marshal(map[string]string{"name": name, "slug": slug})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/workspaces", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var out map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec.Code, out
}

// workspaceRowExists reads the claim back out of the database. A 201 with no row
// would be a phantom success; a refusal check without this could pass over a
// handler that silently created nothing at all.
func workspaceRowExists(t *testing.T, slug string) bool {
	t.Helper()
	pool := bundleTestPool(t)
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspaces WHERE slug = $1`, slug).Scan(&n); err != nil {
		t.Fatalf("count workspaces for %q: %v", slug, err)
	}
	return n > 0
}

// TestRESTCreateWorkspaceRefusesReservedSlugs is the claim-side guard the card
// asks for: every category the reserved list carries must be REFUSED over the
// real REST surface, with a message that says the name is reserved for JomForm,
// and with no workspace row created.
func TestRESTCreateWorkspaceRefusesReservedSlugs(t *testing.T) {
	h, token := reservedSlugFixture(t)

	// One name per category, plus the sharpest prod cases (the ones that answered
	// 200 from the tenant wildcard before this change).
	for _, slug := range []string{
		"www", "api", "app", "mail", "s3", // infrastructure
		"docs", "mcp", "templates", "custom", // routed / JomForm-owned
		"support", "help", "status", "billing", // sensitive / impersonation
		"admin", "account", "accounts", "login", "signup", "security", "blog",
		"payments", "pay", "checkout", "cdn", "assets",
	} {
		code, body := claimSlug(t, h, token, "Claimed "+slug, slug)
		if code != http.StatusBadRequest {
			t.Errorf("POST /v1/workspaces slug=%q -> %d, want 400 (a merchant could "+
				"claim %s.jomform.com)", slug, code, slug)
			continue
		}
		msg, _ := body["error"].(string)
		if !strings.Contains(strings.ToLower(msg), "reserved") {
			t.Errorf("slug=%q refusal message %q does not say the name is reserved — "+
				"the merchant reads it as their own input being malformed", slug, msg)
		}
		if got, _ := body["code"].(string); got != urlguard.ReservedSubdomainCode {
			t.Errorf("slug=%q body code = %q, want %q — the dashboard matches on this "+
				"token to render its own localised sentence (#694)",
				slug, got, urlguard.ReservedSubdomainCode)
		}
		// The refusal must be a REFUSAL, not a message over a created row.
		if workspaceRowExists(t, slug) {
			t.Errorf("slug=%q was refused with 400 but a workspaces row exists — "+
				"the check runs after the insert", slug)
		}
	}
}

// TestRESTCreateWorkspaceAcceptsALegitimateSlug is the other half: the new rule
// must not turn workspace creation into a blanket refusal.
func TestRESTCreateWorkspaceAcceptsALegitimateSlug(t *testing.T) {
	h, token := reservedSlugFixture(t)

	// The unique suffix keeps this run independent of other rows in the scratch
	// schema. randSuffix() is the package's existing helper.
	slug := "acme-" + randSuffix()
	code, body := claimSlug(t, h, token, "Acme Store", slug)
	if code != http.StatusCreated {
		t.Fatalf("POST /v1/workspaces slug=%q -> %d body=%v, want 201 — the reserved "+
			"list is over-reaching and refusing a legitimate merchant address",
			slug, code, body)
	}
	if got, _ := body["slug"].(string); got != slug {
		t.Errorf("created slug = %q, want %q", got, slug)
	}
	if !workspaceRowExists(t, slug) {
		t.Errorf("201 for slug=%q but no workspaces row — the handler reported a create "+
			"that did not happen", slug)
	}

	// And the pre-existing rules are untouched by this change: the charset and
	// the *jomform suffix refusal still stand (the card requires the rule to be
	// ADDED, not replaced).
	if code, _ := claimSlug(t, h, token, "Bad", "acme-jomform"); code != http.StatusBadRequest {
		t.Errorf("slug=acme-jomform -> %d, want 400 (the HasSuffix rule was lost)", code)
	}
	if code, _ := claimSlug(t, h, token, "Bad", "ab"); code != http.StatusBadRequest {
		t.Errorf("slug=ab -> %d, want 400 (the length rule was lost)", code)
	}
	if code, _ := claimSlug(t, h, token, "Bad", "acme_store"); code != http.StatusBadRequest {
		t.Errorf("slug=acme_store -> %d, want 400 (the charset rule was lost)", code)
	}
}

// TestRESTCreateWorkspaceRefusalIsNotTheServingList documents the distinction the
// card calls out, in code rather than in prose: accountSubdomain's serving-side
// list does NOT carry the sensitive names, so it could never have been the
// guard. If someone "simplifies" the claim check away by pointing at the serving
// list, this fails.
func TestRESTCreateWorkspaceRefusalIsNotTheServingList(t *testing.T) {
	for _, name := range []string{"support", "billing", "status", "docs", "mcp"} {
		if accountSubdomain(name+".jomform.com") == "" {
			t.Errorf("accountSubdomain(%q) now refuses to resolve this host — the "+
				"serving list has grown the claim list's job; the two rules must stay "+
				"separate (serving vs claim)", name+".jomform.com")
		}
		if !urlguard.ReservedSubdomain(name) {
			t.Errorf("%q is not reserved, so nothing stops the claim", name)
		}
	}
}
