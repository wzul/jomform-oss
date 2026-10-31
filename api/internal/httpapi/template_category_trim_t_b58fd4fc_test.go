package httpapi

// Card t_b58fd4fc — the template picker paints a gallery card's category chip
// from `category` as served by GET /v1/templates, and keys its icon off that
// exact string (CATEGORY_ICON in web/src/components/TemplatePicker.tsx). A
// category of "   " is TRUTHY in JS, so the card painted a chip holding only
// spaces and a generic icon.
//
// The publish gate now refuses such a template, but the gate only runs on a
// publish transition: rows published before that rule (or inserted by hand —
// #594 itself was three hand-inserted rows) still sit in the gallery. The read
// side therefore returns the category TRIMMED, so a blank category reaches the
// picker as "" and `{tpl.category && …}` draws no chip at all, and a padded real
// category ("  Contact  ") reaches it as the exact string the icon map knows.
// This test drives the real route over the real store, the same call the picker
// makes.
//
// Skipped unless JOMFORM_TEST_DATABASE_URL is set (integration convention).

import (
	"context"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestGalleryServesAServedCategoryTheCardCanPaint(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	acc, _ := bundleFixture(t, ctx, pool)
	fs := dbstore.NewFormStore(pool)

	const intro = "Tell us what you need and we will reply."
	rows := []struct {
		slug     string
		category any
		want     string
	}{
		// Published straight through the store, i.e. the shape a row has when
		// it entered the gallery before the gate trimmed the category.
		{"tpl-b58f-cat-padded", "\t  Contact \n ", "Contact"},
		{"tpl-b58f-cat-blank", "   ", ""},
		{"tpl-b58f-cat-number", 5, ""},
		{"tpl-b58f-cat-clean", "Events", "Events"},
	}
	for _, r := range rows {
		f := &dbstore.Form{
			AccountID: acc, Slug: r.slug, Title: "b58f " + r.slug,
			Status: dbstore.FormStatusPublished, IsTemplate: true,
			Config: map[string]any{
				"mode": "response", "category": r.category,
				"blocks": []any{map[string]any{"id": "in1", "type": "section", "value": intro}},
			},
		}
		if err := fs.Create(ctx, f); err != nil {
			t.Fatalf("create %s: %v", r.slug, err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM forms WHERE id=$1`, f.ID)
		})
	}

	served := map[string]galleryCard{}
	for _, c := range galleryCards(t, pool) {
		served[c.Slug] = c
	}
	for _, r := range rows {
		card, ok := served[r.slug]
		if !ok {
			t.Fatalf("GET /v1/templates dropped %s", r.slug)
		}
		if card.Category != r.want {
			t.Errorf("%s: category = %q, want %q — the picker paints this value as the card's "+
				"category chip and keys its icon off the exact string", r.slug, card.Category, r.want)
		}
		// The description is the control: these rows are complete templates, so
		// a category verdict cannot be explained by a dropped card.
		if card.Description != intro {
			t.Errorf("%s: description = %q, want the intro %q", r.slug, card.Description, intro)
		}
	}
}
