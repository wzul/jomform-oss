package mcpsrv

// Card t_7fb80479 — list_templates derives a template's description, and it
// must agree with the template gallery's rule after `text` was settled as an
// input block.
//
// The gallery card and the MCP tool used to read "the first `text` block's
// value"; with `text` as a single-line INPUT that value is a field LABEL, so a
// name-collecting form described itself as "Cuba. Nama". Both now read the
// form's intro line through formfields.Description — the first `section`/`html`
// block, falling back to the first `text` block for a row written under the old
// content model (the seeded library, until card t_d46c21e0 migrates it).
//
// Driven through the real handler (s.listTemplatesMCP) with the real
// dbstore; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"os"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestTemplateListMCPDescriptionReadsTheIntroNotAFieldLabel(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping MCP list_templates description test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('mcp-txt-' || md5(random()::text) || '@example.test', 'it') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'mcp-txt-' || md5(random()::text), 'mcp-txt-' || md5(random()::text) RETURNING id`,
		userID,
	).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	const (
		introSection = "Tell us about your enquiry."
		introLegacy  = "Apply for a role with us."
	)
	fs := dbstore.NewFormStore(pool)
	rows := []*dbstore.Form{
		{
			AccountID: accountID, Slug: "mcp-txt-section-" + accountID[:8],
			Title: "Intro in a section", Status: dbstore.FormStatusPublished, IsTemplate: true,
			Config: map[string]any{"mode": "response", "category": "Contact", "blocks": []any{
				map[string]any{"id": "nm", "type": "text", "value": "Nama penuh"},
				map[string]any{"id": "in1", "type": "section", "value": introSection},
			}},
			Theme: map[string]any{"primary": "#5e6ad2"},
		},
		{
			AccountID: accountID, Slug: "mcp-txt-legacy-" + accountID[:8],
			Title: "Intro in a text block", Status: dbstore.FormStatusPublished, IsTemplate: true,
			Config: map[string]any{"mode": "response", "category": "HR", "blocks": []any{
				map[string]any{"id": "b1", "type": "text", "value": introLegacy},
				map[string]any{"id": "nm", "type": "name", "value": "Full name"},
			}},
			Theme: map[string]any{"primary": "#64748b"},
		},
	}
	for _, f := range rows {
		if err := fs.Create(ctx, f); err != nil {
			t.Fatalf("create template %s: %v", f.Slug, err)
		}
		f := f
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM forms WHERE id=$1`, f.ID) })
	}

	srv := &Server{deps: &Deps{Forms: fs}}
	_, out, err := srv.listTemplatesMCP(ctx, nil, emptyIn{})
	if err != nil {
		t.Fatalf("list_templates: %v", err)
	}
	if out == nil {
		t.Fatal("list_templates returned no output")
	}
	bySlug := map[string]templateOut{}
	for _, o := range out.Items {
		bySlug[o.Slug] = o
	}
	got, ok := bySlug[rows[0].Slug]
	if !ok {
		t.Fatalf("list_templates dropped the row %q", rows[0].Slug)
	}
	if got.Description != introSection {
		t.Errorf("description = %q, want the section's intro %q — a `text` block is an input now, so its value is a LABEL",
			got.Description, introSection)
	}
	if got.Category != "Contact" || got.Mode != "response" {
		t.Errorf("category/mode = %q/%q, want Contact/response", got.Category, got.Mode)
	}
	gotLegacy, ok := bySlug[rows[1].Slug]
	if !ok {
		t.Fatalf("list_templates dropped the row %q", rows[1].Slug)
	}
	if gotLegacy.Description != introLegacy {
		t.Errorf("description = %q, want %q — a row written before `text` settled keeps its description",
			gotLegacy.Description, introLegacy)
	}
}
