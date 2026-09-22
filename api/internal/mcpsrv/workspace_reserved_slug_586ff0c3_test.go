// t_586ff0c3 — the MCP claim path refuses a reserved workspace subdomain.
//
// WHY THIS FILE EXISTS
//
// The card's rule is that ONE list serves BOTH validators, so the MCP surface
// must be measured independently of REST: the failure mode is a name refused at
// POST /v1/workspaces and claimable through create_workspace (an agent is a
// first-class caller here, and an agent that can claim `billing` is the same
// hole). The handler is driven DIRECTLY with the identity in the context — the
// same shape the repo's other tool tests use (see lang_placement_mcp_test.go):
// the in-memory MCP transport does not carry a per-call Go context through to
// the server-side handler, so a protocol round trip cannot assert
// identity-scoped behaviour.
//
// The refusal half is paired with an ACCEPT half for the same reason as the REST
// test: a validator that rejects everything would satisfy the refusals alone.

package mcpsrv

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
	"github.com/wzul/jomform/api/internal/urlguard"
)

// reservedSlugState is the call context for the two fixture-driven tests: the
// server, the identity-carrying context, and the scratch pool the rows are read
// back from. The claim helper exists so both tests drive the SAME call shape.
type reservedSlugState struct {
	srv  *Server
	ctx  context.Context
	pool *pgxpool.Pool
	sp   *dbstore.WorkspaceStore
}

// claim calls the real tool handler and returns its error.
func (f reservedSlugState) claim(t *testing.T, name, slug string) error {
	t.Helper()
	_, _, err := f.srv.createWorkspace(f.ctx, nil, createWorkspaceIn{Name: name, Slug: slug})
	return err
}

// rowCount reads the workspaces table directly. It deliberately does NOT go
// through dbstore.WorkspaceStore.BySlug: reading the count keeps this guard's
// verdict independent of the store's not-found shape, so a regression there
// cannot make this test pass or fail for the wrong reason.
func (f reservedSlugState) rowCount(t *testing.T, slug string) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspaces WHERE slug = $1`, slug).Scan(&n); err != nil {
		t.Fatalf("count workspaces for %q: %v", slug, err)
	}
	return n
}

// newReservedSlugState seeds a user and returns a server whose create_workspace
// call carries a WRITABLE identity. Without the scope the tool would be refused
// by the write gate (issue #645) for a reason unrelated to slugs — a
// green-looking test that proved nothing about the reserved list.
func newReservedSlugState(t *testing.T) reservedSlugState {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('rsv-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	sp := dbstore.NewWorkspaceStore(pool)
	srv := &Server{deps: &Deps{Workspaces: sp}}
	return reservedSlugState{
		srv:  srv,
		ctx:  WithAuthIdentity(ctx, &AuthIdentity{UserID: userID, Scopes: []string{"readwrite"}}),
		pool: pool,
		sp:   sp,
	}
}

// TestMCPCreateWorkspaceRefusesReservedSlugs drives create_workspace for a name
// from each category and asserts both the error and the ABSENCE of a row (a tool
// that reports a refusal over a workspace it created is worse than no check).
func TestMCPCreateWorkspaceRefusesReservedSlugs(t *testing.T) {
	st := newReservedSlugState(t)

	for _, slug := range []string{
		"www", "api", "app", "mail", "s3",
		"docs", "mcp", "templates", "custom",
		"support", "help", "status", "billing",
		"admin", "account", "accounts", "login", "signup", "security", "blog",
		"payments", "pay", "checkout", "cdn", "assets",
	} {
		err := st.claim(t, "Claimed "+slug, slug)
		if err == nil {
			t.Errorf("create_workspace slug=%q returned no error — an agent could claim "+
				"%s.jomform.com over MCP even though REST refuses it", slug, slug)
			continue
		}
		if !strings.Contains(strings.ToLower(err.Error()), "reserved") {
			t.Errorf("slug=%q error %q does not say the name is reserved for JomForm", slug, err)
		}
		// The row must NOT exist. The count is read straight from the table
		// rather than through BySlug, so this assertion cannot be satisfied by
		// anything other than a genuine absence of the row.
		if n := st.rowCount(t, slug); n != 0 {
			t.Errorf("slug=%q was refused but %d workspaces row(s) exist — the "+
				"check runs after the insert", slug, n)
		}
	}

	// Sanity: assert the two sharpest names against the shared rule explicitly, so
	// this test cannot pass by iterating a list that lost its entries.
	for _, must := range []string{"docs", "mcp", "billing"} {
		if !urlguard.ReservedSubdomain(must) {
			t.Errorf("%q is no longer reserved — the loop above would pass by "+
				"checking nothing", must)
		}
	}
}

// TestMCPCreateWorkspaceAcceptsALegitimateSlug proves the rule did not become a
// blanket refusal, and that the row really lands.
func TestMCPCreateWorkspaceAcceptsALegitimateSlug(t *testing.T) {
	st := newReservedSlugState(t)

	// A fixed legitimate slug; the scratch schema is re-created per test-binary
	// run (testdb.Open), so this is fresh. Asserted below rather than assumed.
	// row must NOT exist — read straight from the table, see rowCount's comment.
	slug := "acme-store-1"
	if n := st.rowCount(t, slug); n != 0 {
		t.Fatalf("fixture assumption broken: %q already exists in the scratch schema", slug)
	}
	if err := st.claim(t, "Acme Store", slug); err != nil {
		t.Fatalf("create_workspace slug=%q was refused: %v — the reserved list is "+
			"over-reaching and blocking a legitimate address", slug, err)
	}
	// Read back by count AND by the store's own accessor: the count proves the row
	// landed, the accessor proves the tool's result is reachable the way the rest
	// of the code reaches it (and would surface a scan regression here).
	if n := st.rowCount(t, slug); n != 1 {
		t.Fatalf("create_workspace reported success for %q but the table holds %d "+
			"row(s) — a phantom create", slug, n)
	}
	row, err := st.sp.BySlug(context.Background(), slug)
	if err != nil {
		t.Fatalf("BySlug(%q) after a successful create: %v", slug, err)
	}
	if row.Slug != slug {
		t.Errorf("stored slug = %q, want %q", row.Slug, slug)
	}

	// The pre-existing rules are still in force (the card adds a rule; it does
	// not replace one).
	if err := st.claim(t, "Bad", "acme-jomform"); err == nil {
		t.Error("slug=acme-jomform accepted — the HasSuffix rule was lost on MCP")
	}
	if err := st.claim(t, "Bad", "ab"); err == nil {
		t.Error("slug=ab accepted — the length rule was lost on MCP")
	}
	if err := st.claim(t, "Bad", "acme_store"); err == nil {
		t.Error("slug=acme_store accepted — the charset rule was lost on MCP")
	}
}

// TestMCPAndRESTShareOneReservedList is the guard on the card's FIRST
// requirement, measured from the MCP side: the MCP validator must not carry its
// own copy of the list. urlguard's subdomains_test.go reads both sources for the
// call; this asserts the behavioural consequence — the SAME set of names is
// refused here as the shared rule reports, with the shared wording.
func TestMCPAndRESTShareOneReservedList(t *testing.T) {
	for _, label := range []string{"support", "status", "billing", "docs", "mcp", "api", "cdn"} {
		if !urlguard.ReservedSubdomain(label) {
			t.Errorf("urlguard.ReservedSubdomain(%q) = false — MCP and REST would both "+
				"accept it", label)
		}
		got := validateWorkspaceSlug(label)
		if got == nil {
			t.Errorf("validateWorkspaceSlug(%q) = nil, want a refusal — the MCP "+
				"validator is not consuming the shared rule", label)
			continue
		}
		if !strings.Contains(got.Error(), urlguard.ReservedSubdomainMessage(label)) {
			t.Errorf("validateWorkspaceSlug(%q) error %q is not the shared "+
				"urlguard message — a second wording is a second rule", label, got)
		}
	}
	if err := validateWorkspaceSlug("acme"); err != nil {
		t.Errorf("validateWorkspaceSlug(acme) = %v, want nil", err)
	}
}
