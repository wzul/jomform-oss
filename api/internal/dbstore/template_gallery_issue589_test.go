package dbstore

// Issue #589 regression test — the New-form picker carded a template twice.
//
// The picker merges two lists: the public gallery (ListTemplates: every
// published template, no account filter) and the merchant's own templates
// (ListTemplatesByAccount: is_template rows owned by the caller). Those two
// predicates used to overlap. The three pre-#174 gallery templates were inserted
// by hand into a merchant account, so each satisfied BOTH queries: the same
// forms row came back twice and was drawn twice (17 cards for 14 templates,
// "Tuition Class Registration" among them).
//
// Three guards, covering both halves of the fix:
//   1. ListTemplatesByAccount never returns a published gallery template, so the
//      two lists are disjoint and merging them cannot duplicate a row;
//   2. migration 000059's data realignment actually moves a merchant-owned
//      published template to the system templates account — executed here, not
//      just asserted against seed data, so neutering the UPDATE fails the test;
//   3. the migration leaves a merchant's OWN template (a draft) alone.
//
// Skipped unless JOMFORM_TEST_DATABASE_URL is set; migrations are applied first,
// mirroring the production boot path.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/db"
)

// migrationName589 is the migration that realigns gallery template ownership.
const migrationName589 = "000059_template_gallery_ownership.up.sql"

// legacyGallerySlugs are the three templates inserted by hand before the gallery
// had a system account (see the 000027 header comment).
var legacyGallerySlugs = []string{"tpl-fnb", "tpl-booking", "tpl-tuition"}

// systemTemplatesAccountID resolves the account migration 000027 created.
func systemTemplatesAccountID(t *testing.T, ctx context.Context, p *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := p.QueryRow(ctx,
		`SELECT id FROM accounts WHERE slug = 'jomform-templates'`).Scan(&id); err != nil {
		t.Fatalf("system templates account missing: %v", err)
	}
	return id
}

// publishedTemplateAccountIDs returns slug -> account_id for published
// templates in ids.
func publishedTemplateAccountIDs(t *testing.T, ctx context.Context, p *pgxpool.Pool, ids []string) map[string]string {
	t.Helper()
	rows, err := p.Query(ctx,
		`SELECT slug, account_id FROM forms
		  WHERE is_template AND status = 'published' AND deleted_at IS NULL
		    AND slug = ANY($1)`, ids)
	if err != nil {
		t.Fatalf("query published templates: %v", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var slug, accountID string
		if err := rows.Scan(&slug, &accountID); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[slug] = accountID
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate: %v", err)
	}
	return out
}

// assertFormFollowsAccountWorkspace asserts a form's workspace_id matches the
// workspace of the account that owns it. forms_sync_workspace_id recomputes
// workspace_id from account_id on INSERT and on UPDATE OF account_id, so a row
// that still points at its old workspace would keep showing up on that
// workspace's public storefront and sitemap — both of which select published
// forms by workspace and do not filter is_template.
func assertFormFollowsAccountWorkspace(t *testing.T, ctx context.Context, p *pgxpool.Pool, formID, what string) {
	t.Helper()
	var formWS, accountWS *string
	if err := p.QueryRow(ctx, `
		SELECT f.workspace_id::text, a.workspace_id::text
		  FROM forms f JOIN accounts a ON a.id = f.account_id
		 WHERE f.id = $1`, formID).Scan(&formWS, &accountWS); err != nil {
		t.Fatalf("read workspace for %s: %v", what, err)
	}
	if !samePtr(formWS, accountWS) {
		t.Errorf("%s: workspace_id = %s but its account's workspace_id = %s — "+
			"forms_sync_workspace_id did not follow the new account, so the row would keep "+
			"appearing on the previous workspace's public storefront/sitemap (issue #589)",
			what, ptrStr(formWS), ptrStr(accountWS))
	}
}

func ptrStr(p *string) string {
	if p == nil {
		return "<null>"
	}
	return *p
}

func samePtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// TestOwnTemplatesExcludePublishedGalleryTemplates covers guard 1: a published
// template owned by a merchant account must not appear in that merchant's own
// template list, because the picker merges the two lists.
func TestOwnTemplatesExcludePublishedGalleryTemplates(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	fs := NewFormStore(p)

	merchant := uniqueAccount(t, ctx, p)

	// Reproduce the production shape: a PUBLISHED template owned by a merchant
	// account. create_template forces status='draft', so this can only arise from
	// data that predates that constraint — exactly the three legacy rows.
	published := &Form{
		AccountID: merchant, Slug: "tpl-published-" + merchant[:8],
		Title: "Tuition Class Registration", Status: FormStatusPublished,
		IsTemplate: true, Config: map[string]any{"mode": "payment"},
	}
	if err := fs.Create(ctx, published); err != nil {
		t.Fatalf("create published template in a merchant account: %v", err)
	}
	// Plus a genuine merchant template (a draft, as create_template produces).
	own := &Form{
		AccountID: merchant, Slug: "tpl-own-" + merchant[:8],
		Title: "My Invoice Form", Status: FormStatusDraft,
		IsTemplate: true, Config: map[string]any{"mode": "response"},
	}
	if err := fs.Create(ctx, own); err != nil {
		t.Fatalf("create own template: %v", err)
	}
	// This package shares one scratch schema across its tests, so leave no rows
	// behind: a stray published template in a merchant account is exactly the
	// state the other test in this file asserts is impossible.
	for _, id := range []string{published.ID, own.ID} {
		id := id
		t.Cleanup(func() { _, _ = p.Exec(context.Background(), `DELETE FROM forms WHERE id=$1`, id) })
	}

	gallery, err := fs.ListTemplates(ctx)
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	mine, err := fs.ListTemplatesByAccount(ctx, merchant)
	if err != nil {
		t.Fatalf("ListTemplatesByAccount: %v", err)
	}

	// The regression: the published gallery row must NOT also be listed as the
	// merchant's own template, or the picker draws it twice.
	for _, f := range mine {
		if f.ID == published.ID {
			t.Errorf("published gallery template %s (%s) is also listed as an own template — "+
				"the picker merges both lists and would card it twice (issue #589)",
				f.Slug, f.ID)
		}
	}
	// The merchant's real template still shows up.
	if len(mine) != 1 || mine[0].ID != own.ID {
		t.Fatalf("own templates = %d entries, want exactly the draft template %s",
			len(mine), own.Slug)
	}
	// The published row is still served by the gallery, so it is offered once.
	inGallery := false
	for _, f := range gallery {
		if f.ID == published.ID {
			inGallery = true
		}
	}
	if !inGallery {
		t.Errorf("published template %s is in neither list — it must be in the gallery",
			published.Slug)
	}

	// The two lists share no row id, so a picker merge cannot duplicate anything.
	galleryIDs := make(map[string]bool, len(gallery))
	for _, f := range gallery {
		galleryIDs[f.ID] = true
	}
	for _, f := range mine {
		if galleryIDs[f.ID] {
			t.Errorf("template %s (%s) appears in BOTH template lists — merging them duplicates it",
				f.Slug, f.ID)
		}
	}
}

// TestTemplateOwnershipMigrationRealignsLegacyRows covers guards 2 and 3 by
// executing migration 000059's SQL against a merchant-owned published template.
func TestTemplateOwnershipMigrationRealignsLegacyRows(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	fs := NewFormStore(p)

	systemID := systemTemplatesAccountID(t, ctx, p)

	// The migration already ran at migrate time; pull its SQL back out so this
	// test exercises the real statement rather than a copy of it.
	var sql589 string
	migrations, err := db.MigrationStatements()
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	for _, m := range migrations {
		if m.Name == migrationName589 {
			sql589 = m.SQL
		}
	}
	if sql589 == "" {
		t.Fatalf("migration %s not found in the embedded set", migrationName589)
	}

	// Seed data check: the three legacy slugs must sit in the system account.
	// Before the migration they were in a merchant account, which is the bug.
	got := publishedTemplateAccountIDs(t, ctx, p, legacyGallerySlugs)
	for _, slug := range legacyGallerySlugs {
		accountID, ok := got[slug]
		if !ok {
			t.Errorf("gallery template %s is missing", slug)
			continue
		}
		if accountID != systemID {
			t.Errorf("gallery template %s is owned by account %s, want the system templates account %s "+
				"(issue #589: a merchant-owned gallery row is returned by both template lists)",
				slug, accountID, systemID)
		}
	}

	// Guard 2: recreate the legacy shape AFTER migrations ran, then run the
	// migration's SQL. It must move the row to the system account.
	merchant := uniqueAccount(t, ctx, p)
	legacy := &Form{
		AccountID: merchant, Slug: "tpl-legacy-" + merchant[:8],
		Title: "Legacy Hand-Inserted Template", Status: FormStatusPublished,
		IsTemplate: true, Config: map[string]any{"mode": "payment"},
		Theme: map[string]any{"primary": "#8b5cf6"},
	}
	if err := fs.Create(ctx, legacy); err != nil {
		t.Fatalf("create legacy-shaped published template: %v", err)
	}

	// Positive control: a merchant's own template (a draft) must be left alone.
	ownDraft := &Form{
		AccountID: merchant, Slug: "tpl-mine-" + merchant[:8],
		Title: "Merchant Own Template", Status: FormStatusDraft,
		IsTemplate: true, Config: map[string]any{"mode": "response"},
	}
	if err := fs.Create(ctx, ownDraft); err != nil {
		t.Fatalf("create merchant draft template: %v", err)
	}
	// This test moves a published template into the system account and then
	// asserts NO published template is left in a merchant account. Leave no rows
	// behind so the assertion reflects this test's own state and not a sibling
	// test's leftovers.
	t.Cleanup(func() {
		_, _ = p.Exec(context.Background(), `DELETE FROM forms WHERE id=ANY($1)`,
			[]string{legacy.ID, ownDraft.ID})
	})

	if _, err := p.Exec(ctx, sql589); err != nil {
		t.Fatalf("apply %s: %v", migrationName589, err)
	}

	// The legacy-shaped row moved...
	var gotAccount string
	if err := p.QueryRow(ctx, `SELECT account_id FROM forms WHERE id=$1`, legacy.ID).Scan(&gotAccount); err != nil {
		t.Fatalf("read migrated row: %v", err)
	}
	if gotAccount != systemID {
		t.Errorf("after %s, template %s is owned by %s, want the system templates account %s — "+
			"a merchant-owned published template is returned by both template lists (issue #589)",
			migrationName589, legacy.Slug, gotAccount, systemID)
	}
	// ...without losing any of its data: ownership only, ids and content intact.
	var title, status string
	var isTemplate bool
	var config, theme map[string]any
	if err := p.QueryRow(ctx,
		`SELECT title, status, is_template, config, theme FROM forms WHERE id=$1`,
		legacy.ID).Scan(&title, &status, &isTemplate, &config, &theme); err != nil {
		t.Fatalf("read migrated row content: %v", err)
	}
	if title != legacy.Title || status != FormStatusPublished || !isTemplate {
		t.Errorf("migration changed more than ownership: title=%q status=%q is_template=%v",
			title, status, isTemplate)
	}

	// workspace_id is recomputed from account_id by the forms_sync_workspace_id
	// trigger, so the row also leaves the merchant's public storefront/sitemap
	// listing (both select published forms by workspace). Assert the form now
	// follows its NEW account rather than trusting the trigger fired.
	assertFormFollowsAccountWorkspace(t, ctx, p, legacy.ID, "migrated template "+legacy.Slug)
	assertFormFollowsAccountWorkspace(t, ctx, p, ownDraft.ID, "merchant draft template "+ownDraft.Slug)
	if config["mode"] != "payment" || theme["primary"] != "#8b5cf6" {
		t.Errorf("migration changed config/theme: config=%v theme=%v", config, theme)
	}

	// ...and the merchant's own draft template was NOT taken away.
	var draftAccount string
	if err := p.QueryRow(ctx, `SELECT account_id FROM forms WHERE id=$1`, ownDraft.ID).Scan(&draftAccount); err != nil {
		t.Fatalf("read draft template: %v", err)
	}
	if draftAccount != merchant {
		t.Errorf("migration moved the merchant's own draft template %s to account %s — "+
			"a merchant template must never be reassigned", ownDraft.Slug, draftAccount)
	}

	// Idempotent: a second run (e.g. after a restore) changes nothing.
	if _, err := p.Exec(ctx, sql589); err != nil {
		t.Fatalf("re-apply %s: %v", migrationName589, err)
	}
	if err := p.QueryRow(ctx, `SELECT account_id FROM forms WHERE id=$1`, legacy.ID).Scan(&gotAccount); err != nil {
		t.Fatalf("read row after re-apply: %v", err)
	}
	if gotAccount != systemID {
		t.Errorf("re-applying %s moved the row to %s", migrationName589, gotAccount)
	}

	// No published template may remain in a non-system account: that is what
	// keeps the own-templates list disjoint from the gallery.
	var strays int
	if err := p.QueryRow(ctx,
		`SELECT count(*) FROM forms
		  WHERE is_template AND status = 'published' AND deleted_at IS NULL
		    AND account_id <> $1`, systemID).Scan(&strays); err != nil {
		t.Fatalf("count stray published templates: %v", err)
	}
	if strays != 0 {
		t.Errorf("%d published template(s) are owned by a non-system account — "+
			"they are returned by both template lists (issue #589)", strays)
	}

	// No two gallery rows may share a slug: the picker would offer the same
	// template twice under two ids.
	var dupSlugs int
	if err := p.QueryRow(ctx,
		`SELECT count(*) FROM (
		    SELECT slug FROM forms
		     WHERE is_template AND status = 'published' AND deleted_at IS NULL
		     GROUP BY slug HAVING count(*) > 1
		 ) d`).Scan(&dupSlugs); err != nil {
		t.Fatalf("count duplicate gallery slugs: %v", err)
	}
	if dupSlugs != 0 {
		t.Errorf("%d gallery slug(s) are duplicated across published templates", dupSlugs)
	}
}
