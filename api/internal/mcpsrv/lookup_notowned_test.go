package mcpsrv

// Regression: an MCP tool call naming a form or product the caller does not
// own must answer "entiti tidak dijumpai" — the not-owned sentinel that lets
// the call-scope walk move on to the caller's next account — not a raw driver
// error.
//
// resolveEntityAccount tries each account the call may act on and continues
// only when the lookup answers a store "absent in this account" sentinel
// (isStoredNotFound). Both lookups it drives — FormStore.GetByID and
// ProductStore.Get — used to hand back the raw pgx error for an unknown,
// foreign, or malformed id, which the walk read as a real failure and returned
// verbatim: the tool call surfaced a SQL driver message instead of the
// not-found answer every other path gives.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL) for the
// per-form ownership resolution; skipped when unset.

import (
	"context"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestMCPNotOwnedLookupsAnswerNotOwned(t *testing.T) {
	st := issue501MCPFixture(t)
	ctx := context.Background()

	ownForm := st.seedForm(t, "notowned-own-form")
	ownProduct := st.seedProduct(t, "NotOwned Own Product")

	// A second account, unreachable from this call's scope, owns the rows the
	// caller will name.
	var otherUser, otherWorkspace, otherAccount string
	if err := st.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('notowned-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&otherUser); err != nil {
		t.Fatalf("create other user: %v", err)
	}
	if err := st.pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'notowned-ws-' || md5(random()::text), 'notowned-' || md5(random()::text))
		 RETURNING id`, otherUser).Scan(&otherWorkspace); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	if err := st.pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'notowned-acc-' || md5(random()::text), 'notowned-' || md5(random()::text), $2)
		 RETURNING id`, otherUser, otherWorkspace).Scan(&otherAccount); err != nil {
		t.Fatalf("create other account: %v", err)
	}

	foreignForm := &dbstore.Form{
		AccountID: otherAccount, Slug: "notowned-foreign-form", Title: "Foreign",
		Config: map[string]any{}, Theme: map[string]any{}, Status: dbstore.FormStatusDraft,
	}
	if err := st.srv.deps.Forms.Create(ctx, foreignForm); err != nil {
		t.Fatalf("create foreign form: %v", err)
	}
	foreignProduct := &dbstore.Product{
		AccountID: otherAccount, Name: "NotOwned Foreign Product",
		PriceSen: 1000, Type: dbstore.SaleTypeOneTime, Active: true,
	}
	if err := st.srv.deps.Products.Create(ctx, foreignProduct); err != nil {
		t.Fatalf("create foreign product: %v", err)
	}

	// Control: the caller's own rows still resolve (no over-correction).
	if _, err := st.srv.resolveFormAccount(st.ctx, ownForm); err != nil {
		t.Fatalf("resolveFormAccount(own form) = %v, want nil", err)
	}
	if _, err := st.srv.resolveProductAccount(st.ctx, ownProduct); err != nil {
		t.Fatalf("resolveProductAccount(own product) = %v, want nil", err)
	}

	for _, tc := range []struct {
		name   string
		lookup func() (string, error)
	}{
		{"form: nonexistent", func() (string, error) {
			return st.srv.resolveFormAccount(st.ctx, "11111111-2222-3333-4444-555555555555")
		}},
		{"form: other account's", func() (string, error) {
			return st.srv.resolveFormAccount(st.ctx, foreignForm.ID)
		}},
		{"form: non-uuid", func() (string, error) {
			return st.srv.resolveFormAccount(st.ctx, "not-a-uuid")
		}},
		{"product: nonexistent", func() (string, error) {
			return st.srv.resolveProductAccount(st.ctx, "11111111-2222-3333-4444-555555555555")
		}},
		{"product: other account's", func() (string, error) {
			return st.srv.resolveProductAccount(st.ctx, foreignProduct.ID)
		}},
		{"product: non-uuid", func() (string, error) {
			return st.srv.resolveProductAccount(st.ctx, "not-a-uuid")
		}},
	} {
		_, err := tc.lookup()
		if err == nil {
			t.Errorf("%s: resolved, want %v", tc.name, errNotOwned)
			continue
		}
		if !strings.Contains(err.Error(), errNotOwned.Error()) {
			t.Errorf("%s: err = %v, want the not-owned sentinel %q", tc.name, err, errNotOwned)
		}
		// A raw driver error would leak the SQL layer to the tool caller.
		for _, leaked := range []string{"SQLSTATE", "no rows in result set", "22P02"} {
			if strings.Contains(err.Error(), leaked) {
				t.Errorf("%s: err leaks the driver detail %q: %v", tc.name, leaked, err)
			}
		}
	}
}
