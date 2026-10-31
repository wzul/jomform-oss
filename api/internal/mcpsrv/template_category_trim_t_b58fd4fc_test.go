package mcpsrv

// Card t_b58fd4fc — MCP parity for the gallery's category chip.
//
// list_templates maps a template's card fields from the same config keys the
// REST gallery does (AGENTS.md "MCP FEATURE PARITY"), so the category it returns
// must be the same trimmed value the card paints: "   " is truthy in JS and the
// picker would draw a chip holding only spaces, and "  Contact  " would miss the
// icon map. The publish gate refuses a blank category, but a row published
// before that rule (or inserted by hand) still sits in the gallery, so the read
// side trims.
//
// Driven through the real handler (s.listTemplatesMCP) with the real dbstore;
// skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"os"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestTemplateListMCPTrimsTheCategoryItServes(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping MCP list_templates category test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('mcp-cat-' || md5(random()::text) || '@example.test', 'it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'mcp-cat-' || md5(random()::text), 'mcp-cat-' || md5(random()::text) RETURNING id`,
		userID,
	).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	const intro = "Tell us what you need and we will reply."
	rows := []struct {
		slug     string
		category any
		want     string
	}{
		{"mcp-cat-padded", "\t  Contact \n ", "Contact"},
		{"mcp-cat-blank", "   ", ""},
		{"mcp-cat-number", 5, ""},
	}
	fs := dbstore.NewFormStore(pool)
	for _, r := range rows {
		f := &dbstore.Form{
			AccountID: accountID, Slug: r.slug + "-" + accountID[:8], Title: r.slug,
			Status: dbstore.FormStatusPublished, IsTemplate: true,
			Config: map[string]any{
				"mode": "response", "category": r.category,
				"blocks": []any{map[string]any{"id": "in1", "type": "section", "value": intro}},
			},
		}
		if err := fs.Create(ctx, f); err != nil {
			t.Fatalf("create template %s: %v", f.Slug, err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM forms WHERE id=$1`, f.ID)
		})
	}

	srv := &Server{deps: &Deps{Forms: fs}}
	_, out, err := srv.listTemplatesMCP(ctx, nil, emptyIn{})
	if err != nil {
		t.Fatalf("list_templates: %v", err)
	}
	if out == nil {
		t.Fatal("list_templates returned no output")
	}
	served := map[string]templateOut{}
	for _, o := range out.Items {
		served[o.Slug] = o
	}
	for _, r := range rows {
		want := r.slug + "-" + accountID[:8]
		got, ok := served[want]
		if !ok {
			t.Fatalf("list_templates dropped the row %q", want)
		}
		if got.Category != r.want {
			t.Errorf("%s: category = %q, want %q — an agent must see the same category the "+
				"dashboard card paints", want, got.Category, r.want)
		}
		if got.Description != intro {
			t.Errorf("%s: description = %q, want the intro %q", want, got.Description, intro)
		}
	}
}
