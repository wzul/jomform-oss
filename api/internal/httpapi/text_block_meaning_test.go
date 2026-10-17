package httpapi

// Card t_7fb80479 — the `text` block has ONE meaning, and this drives the two
// surfaces that still read it as prose.
//
// The block's semantics were settled as "a single-line text INPUT" by three
// merged cards (t_99c3a08b for the response renderer, t_23289ce3 for the
// payment renderer, t_4086b8f0 for the complete palette list) — the palette
// offers it in the Standard group beside textarea/number/select with
// placeholder / max length / default value / pattern, and both public
// renderers paint `<input type="text">` whose label is the block's value.
//
// What had NOT moved with them is the form's DESCRIPTION, derived in three
// places from "the first `text` block's value" (this package's template
// gallery, mcpsrv's list_templates, and the builder's save). That rule was
// written when `text` meant prose; with `text` as an input its value is a
// LABEL, so a merchant's name-collecting form described itself as
// "Cuba. Nama" instead of its intro line.
//
// The rule now reads the form's PROSE — the first `section`/`html` block —
// and falls back to the first `text` block only for a row written under the
// old content model (the seeded library's intro still lives in a `text` block
// until card t_d46c21e0 migrates those rows, and migration 000060's prepended
// `b0-intro` block is the same shape).
//
// Driven through the real route (GET /v1/templates, the call TemplatePicker.tsx
// makes) against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// textBlockMeaningCards fetches the public gallery over the real route and
// indexes the cards by slug, so a row the endpoint dropped cannot pass.
func textBlockMeaningCards(t *testing.T, pool *pgxpool.Pool) map[string]galleryCard {
	t.Helper()
	h := &RESTHandlers{Forms: dbstore.NewFormStore(pool)}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/templates", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/templates = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var cards []galleryCard
	if err := json.Unmarshal(rec.Body.Bytes(), &cards); err != nil {
		t.Fatalf("decode gallery: %v (%s)", err, rec.Body.String())
	}
	out := map[string]galleryCard{}
	for _, c := range cards {
		out[c.Slug] = c
	}
	return out
}

func TestTemplateGalleryDescriptionReadsTheIntroNotAFieldLabel(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping template-gallery description test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)
	fs := dbstore.NewFormStore(pool)

	const (
		introSection = "Get in touch — we usually reply within a day."
		introLegacy  = "Order from our menu — pay online when you submit."
	)
	// `section` intro with a text FIELD in front of it: the field's label must
	// never become the card's description.
	migrated := &dbstore.Form{
		AccountID: accountID, Slug: "it-intro-section-" + accountID[:8],
		Title: "Intro in a section", Status: dbstore.FormStatusPublished, IsTemplate: true,
		Config: map[string]any{"mode": "response", "category": "Contact", "blocks": []any{
			map[string]any{"id": "nm", "type": "text", "value": "Cuba. Nama"},
			map[string]any{"id": "in1", "type": "section", "value": introSection},
		}},
		Theme: map[string]any{"primary": "#5e6ad2"},
	}
	// The old content model: no display block at all, the sentence is a `text`
	// block's value (the seeded library's shape, until t_d46c21e0).
	legacy := &dbstore.Form{
		AccountID: accountID, Slug: "it-intro-legacy-" + accountID[:8],
		Title: "Intro in a text block", Status: dbstore.FormStatusPublished, IsTemplate: true,
		Config: map[string]any{"mode": "payment", "category": "Food & Beverage", "blocks": []any{
			map[string]any{"id": "b1", "type": "text", "value": introLegacy},
			map[string]any{"id": "nm", "type": "name", "value": "Full name"},
		}},
		Theme: map[string]any{"primary": "#5e6ad2"},
	}
	for _, f := range []*dbstore.Form{migrated, legacy} {
		if err := fs.Create(ctx, f); err != nil {
			t.Fatalf("create template %s: %v", f.Slug, err)
		}
		f := f
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM forms WHERE id=$1`, f.ID) })
	}

	cards := textBlockMeaningCards(t, pool)
	got, ok := cards[migrated.Slug]
	if !ok {
		t.Fatalf("the gallery dropped the row %q", migrated.Slug)
	}
	if got.Description != introSection {
		t.Errorf("description = %q, want the section's intro %q — a `text` block is an input now, so its value is a LABEL",
			got.Description, introSection)
	}
	gotLegacy, ok := cards[legacy.Slug]
	if !ok {
		t.Fatalf("the gallery dropped the row %q", legacy.Slug)
	}
	if gotLegacy.Description != introLegacy {
		t.Errorf("description = %q, want %q — a row written before `text` settled keeps its description",
			gotLegacy.Description, introLegacy)
	}
}
