package httpapi

// Issue #594 — gallery-template metadata guard.
//
// THE BUG. The New-form template picker draws each gallery card from
// GET /v1/templates, whose fields the picker maps as:
//
//     title       <- forms.title
//     category    <- config.category
//     description <- the value of the FIRST block whose type is 'text'
//
// (listTemplates in rest_templates.go; the list_templates MCP tool maps the
// same three fields from the same config keys.) Three templates — tpl-fnb,
// tpl-booking and tpl-tuition — were inserted by hand before the template
// library existed, and migration 000027's NOT EXISTS guard skipped them, so
// they carried no config.category and no text block at all. Their cards showed
// a title with no category and no description while the other 11 showed both.
//
// WHAT THESE TESTS PIN. Not the three known-bad rows: the class. Every
// template the gallery serves must carry a non-empty category and a non-empty
// description, and the gallery's set is proven equal to the set of published
// templates in the database, so a row the endpoint dropped cannot hide from
// the check. A new gallery template that ships without both fails the build.
//
// WHY IT BITES. The guard reads the endpoint, but a freshly migrated database
// already has complete seeded templates — so the test would pass even with the
// repair removed. Both tests therefore RECREATE the pre-#174 shape first
// (strip config.category and the intro block from the three rows), assert the
// recreated shape is detected as incomplete, and only then apply migration
// 000060's own SQL, extracted from the embedded migration set, and assert the
// gap is gone. Deleting or neutering that SQL leaves the recreated rows
// incomplete and both tests fail.
//
// Skipped unless JOMFORM_TEST_DATABASE_URL is set (integration convention).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/db"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// migrationName594 is the migration that gives the three legacy gallery
// templates their category and intro line.
const migrationName594 = "000060_template_gallery_metadata.up.sql"

// galleryFloor is the anti-vacuity floor: the shipped library has 14
// templates, and a floor well below that catches an empty or truncated
// gallery (a mapper regression, a broken seed) without pinning the exact size.
const galleryFloor = 10

// legacyGalleryMetadata are the three pre-#174 templates and the category
// migration 000060 gives them. The strings are the keys of CATEGORY_ICON in
// web/src/components/TemplatePicker.tsx — the picker falls back to a generic
// icon for an unknown category — and the values 000027_template_library
// already uses for these three slugs.
var legacyGalleryMetadata = map[string]string{
	"tpl-fnb":     "Food & Beverage",
	"tpl-booking": "Booking",
	"tpl-tuition": "Education",
}

// legacyFnbConfig is tpl-fnb's config exactly as production held it before this
// fix (read 2026-09-18): three own blocks, a products snapshot, no category and
// no text block. It is the fixture the repair is exercised against, because the
// rows a FRESH database seeds are complete and so cannot show the defect.
const legacyFnbConfig = `{
  "blocks": [
    {"type": "heading", "value": "Tempahan"},
    {"type": "name", "value": "Nama"},
    {"type": "product", "value": "Nasi Lemak", "product_id": "p1"}
  ],
  "products": [{"id": "p1", "name": "Nasi Lemak", "price_sen": 1000}]
}`

type galleryCard struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"`
}

// galleryCards fetches the public gallery over the real route, the same call
// TemplatePicker.tsx makes (GET /v1/templates, no auth).
func galleryCards(t *testing.T, pool *pgxpool.Pool) []galleryCard {
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
	var out []galleryCard
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode gallery: %v (%s)", err, rec.Body.String())
	}
	return out
}

// migrationSQL594 pulls the repair statement out of the embedded migration set
// so these tests exercise the real SQL rather than a copy of it (the same
// approach template_gallery_issue589_test.go takes for 000059).
func migrationSQL594(t *testing.T) string {
	t.Helper()
	migrations, err := db.MigrationStatements()
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	for _, m := range migrations {
		if m.Name == migrationName594 {
			return m.SQL
		}
	}
	t.Fatalf("migration %s not found in the embedded set", migrationName594)
	return ""
}

// stripLegacyMetadata rewinds the three gallery templates to the shape they
// had before migration 000060 — no config.category, no intro block at all — and
// registers the restore, so the rest of the package still sees the migrated
// rows. This is the pre-fix production state, reproduced on purpose.
//
// "No intro block" means no PROSE block (section/html) either, not just no
// `text` block: 000077_template_intro_section retypes these rows' intro to
// `section`, and the description derivation (formfields.Description) reads a
// section first, so a strip that only removed `text` blocks would leave the
// description behind and this reproduction would be partial.
//
// The expansion goes through the same `jsonb_typeof(...) = 'array'` CASE the
// production predicate uses (dbstore/form.go's formBlocksArray), not COALESCE:
// COALESCE only replaces SQL NULL, so a non-array value would be handed straight
// to jsonb_array_elements and raise SQLSTATE 22023 on this fixture too.
func stripLegacyMetadata(t *testing.T, ctx context.Context, p *pgxpool.Pool) {
	t.Helper()
	saved := map[string]string{}
	rows, err := p.Query(ctx,
		`SELECT slug, config::text FROM forms
		  WHERE slug = ANY($1) AND is_template AND deleted_at IS NULL`,
		legacyGallerySlugs())
	if err != nil {
		t.Fatalf("snapshot legacy configs: %v", err)
	}
	for rows.Next() {
		var slug, cfg string
		if err := rows.Scan(&slug, &cfg); err != nil {
			t.Fatalf("scan snapshot: %v", err)
		}
		saved[slug] = cfg
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate snapshot: %v", err)
	}
	if len(saved) != 3 {
		t.Fatalf("snapshot captured %d of the 3 legacy templates — the reproduction would be "+
			"partial", len(saved))
	}
	// Cleanup first so a failure below still restores the rows.
	t.Cleanup(func() {
		for slug, cfg := range saved {
			if _, err := p.Exec(context.Background(),
				`UPDATE forms SET config = $2::jsonb WHERE slug = $1 AND is_template`, slug, cfg); err != nil {
				t.Errorf("restore %s config: %v", slug, err)
			}
		}
	})

	// The block predicate drops the row's INTRO whatever shape it has: the
	// legacy `text` block (the pre-000077 content model) and the `section` this
	// package's t_d46c21e0 migration writes. Keeping the row's own field blocks.
	if _, err := p.Exec(ctx,
		`UPDATE forms
		    SET config = jsonb_set(
		          config - 'category',
		          '{blocks}',
		          COALESCE(
		            (SELECT jsonb_agg(b)
		               FROM jsonb_array_elements(
		                      CASE WHEN jsonb_typeof(config->'blocks') = 'array'
		                           THEN config->'blocks' ELSE '[]'::jsonb END
		                    ) b
		              WHERE NOT (b->>'type' IN ('text', 'section', 'html')
		                         AND COALESCE(b->>'value','') <> '')),
		            '[]'::jsonb
		          )
		        )
		  WHERE slug = ANY($1) AND is_template AND deleted_at IS NULL`,
		legacyGallerySlugs()); err != nil {
		t.Fatalf("recreate the pre-#174 shape: %v", err)
	}
}

func legacyGallerySlugs() []string {
	return []string{"tpl-fnb", "tpl-booking", "tpl-tuition"}
}

// metadataGaps names every card the picker would draw without a category or
// without a description — the exact user-visible defect.
func metadataGaps(cards []galleryCard) []string {
	var gaps []string
	for _, c := range cards {
		if c.Category == "" {
			gaps = append(gaps, fmt.Sprintf("%s (no category)", c.Slug))
		}
		if c.Description == "" {
			gaps = append(gaps, fmt.Sprintf("%s (no description)", c.Slug))
		}
	}
	return gaps
}

// TestGalleryTemplatesAllCarryCategoryAndDescription is the whole-class guard.
func TestGalleryTemplatesAllCarryCategoryAndDescription(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	// Rewind the three legacy rows to their pre-fix state, then prove the
	// recreated shape really is detected — without this step a green run could
	// mean nothing more than "the seed data was already complete".
	stripLegacyMetadata(t, ctx, pool)
	if gaps := metadataGaps(galleryCards(t, pool)); len(gaps) != 3*2 {
		t.Fatalf("after recreating the pre-#174 shape, the gallery reports %d gaps (%v); want "+
			"both fields missing on all three legacy templates — the reproduction is not faithful "+
			"and this guard would pass vacuously", len(gaps), gaps)
	}

	// Apply the fix, exactly as production does.
	if _, err := pool.Exec(ctx, migrationSQL594(t)); err != nil {
		t.Fatalf("apply %s: %v", migrationName594, err)
	}

	cards := galleryCards(t, pool)
	if len(cards) < galleryFloor {
		t.Fatalf("gallery returned %d templates, want at least %d — an empty or truncated "+
			"gallery would make the per-template checks below pass vacuously",
			len(cards), galleryFloor)
	}

	// Prove the checked set is the WHOLE class: a gallery row the endpoint
	// silently dropped would otherwise never be inspected.
	var published int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM forms
		  WHERE is_template AND status = 'published' AND deleted_at IS NULL`).Scan(&published); err != nil {
		t.Fatalf("count published templates: %v", err)
	}
	if published != len(cards) {
		t.Errorf("gallery served %d templates but the database holds %d published templates — "+
			"every one of them must carry both fields (issue #594)", len(cards), published)
	}

	if gaps := metadataGaps(cards); len(gaps) > 0 {
		t.Errorf("the picker card for %v would render a title with no category or no description; "+
			"every gallery template needs config.category and a text block (issue #594)", gaps)
	}
	for _, c := range cards {
		if c.Title == "" {
			t.Errorf("template %s (%s) has no title", c.Slug, c.ID)
		}
	}

	// The three reported templates: present, and carrying a category the
	// picker's icon map recognises.
	for slug, wantCat := range legacyGalleryMetadata {
		found := false
		for _, c := range cards {
			if c.Slug != slug {
				continue
			}
			found = true
			if c.Category != wantCat {
				t.Errorf("template %s category = %q, want %q (the CATEGORY_ICON key in "+
					"TemplatePicker.tsx and the value 000027 uses for this slug)",
					slug, c.Category, wantCat)
			}
		}
		if !found {
			t.Errorf("template %s is missing from the gallery", slug)
		}
	}
}

// TestGalleryMetadataMigrationIsTargetedAndIdempotent covers what the migration
// must NOT do: rewrite a complete template, reorder or drop a repaired row's own
// content, or change anything on a second run. The whole test runs in a
// transaction that is rolled back, so it leaves no state behind.
func TestGalleryMetadataMigrationIsTargetedAndIdempotent(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Fixture: tpl-fnb as production held it before the fix. The row a fresh
	// database seeds is already complete, so the real pre-fix shape has to be
	// put back for the repair to have anything to repair.
	if _, err := tx.Exec(ctx,
		`UPDATE forms SET config = $1::jsonb WHERE slug = 'tpl-fnb' AND is_template`,
		legacyFnbConfig); err != nil {
		t.Fatalf("install the pre-#174 tpl-fnb fixture: %v", err)
	}

	// A COMPLETE template must come out byte-identical: a WHERE that is too
	// wide would silently rewrite the rest of the library.
	var eventBefore, eventAfter string
	if err := tx.QueryRow(ctx,
		`SELECT config::text FROM forms WHERE slug = 'tpl-event' AND is_template`).Scan(&eventBefore); err != nil {
		t.Fatalf("read tpl-event before: %v", err)
	}

	up := migrationSQL594(t)
	if _, err := tx.Exec(ctx, up); err != nil {
		t.Fatalf("apply %s: %v", migrationName594, err)
	}

	// The repaired row: both fields present, and everything it already had is
	// intact — title, status, theme, products snapshot, and its own blocks in
	// their original order behind the prepended intro (the seeded library's
	// shape, and what the picker's "first text block" rule expects).
	var blocks, products []map[string]any
	var theme map[string]any
	var category, title, status string
	if err := tx.QueryRow(ctx,
		`SELECT config->'blocks', theme, config->'products', config->>'category', status, title
		   FROM forms WHERE slug = 'tpl-fnb' AND is_template`).
		Scan(&blocks, &theme, &products, &category, &status, &title); err != nil {
		t.Fatalf("read tpl-fnb after the repair: %v", err)
	}
	if category != legacyGalleryMetadata["tpl-fnb"] {
		t.Errorf("tpl-fnb category = %q, want %q", category, legacyGalleryMetadata["tpl-fnb"])
	}
	if title != "F&B Order + Payment" || status != "published" {
		t.Errorf("tpl-fnb title/status = %q/%q, want the untouched original", title, status)
	}
	if theme["primary"] == nil {
		t.Errorf("tpl-fnb theme lost its primary colour: %v", theme)
	}
	if len(products) == 0 {
		t.Errorf("tpl-fnb lost its products snapshot: %v", products)
	}
	if len(blocks) != 4 {
		t.Fatalf("tpl-fnb has %d blocks after the repair, want 4 (the prepended intro plus the "+
			"template's own 3) — the repair must PREPEND, not replace (issue #594)", len(blocks))
	}
	if blocks[0]["type"] != "text" || blocks[0]["value"] == "" {
		t.Errorf("tpl-fnb block[0] = %v, want the intro text block first (the picker reads the "+
			"FIRST text block as the description)", blocks[0])
	}
	for i, want := range []string{"heading", "name", "product"} {
		if got, _ := blocks[i+1]["type"].(string); got != want {
			t.Errorf("tpl-fnb block[%d].type = %q, want %q — the repair must not reorder or drop "+
				"the template's own blocks", i+1, got, want)
		}
	}

	// Idempotent: a second run (restart, restore, replayed migration) changes
	// nothing — no duplicated intro, no rewritten category.
	var fnbAfterFirst string
	if err := tx.QueryRow(ctx,
		`SELECT config::text FROM forms WHERE slug = 'tpl-fnb' AND is_template`).Scan(&fnbAfterFirst); err != nil {
		t.Fatalf("read tpl-fnb before the second run: %v", err)
	}
	if _, err := tx.Exec(ctx, up); err != nil {
		t.Fatalf("re-apply %s: %v", migrationName594, err)
	}
	var fnbAfterSecond string
	if err := tx.QueryRow(ctx,
		`SELECT config::text FROM forms WHERE slug = 'tpl-fnb' AND is_template`).Scan(&fnbAfterSecond); err != nil {
		t.Fatalf("read tpl-fnb after the second run: %v", err)
	}
	if fnbAfterFirst != fnbAfterSecond {
		t.Errorf("re-running the migration changed the repaired row:\n first=%s\n second=%s",
			fnbAfterFirst, fnbAfterSecond)
	}
	if err := tx.QueryRow(ctx,
		`SELECT config::text FROM forms WHERE slug = 'tpl-event' AND is_template`).Scan(&eventAfter); err != nil {
		t.Fatalf("read tpl-event after: %v", err)
	}
	if eventBefore != eventAfter {
		t.Errorf("the migration changed an already-complete template's config:\n before=%s\n after=%s",
			eventBefore, eventAfter)
	}

	// The rollback must undo exactly what the repair added and nothing else:
	// category gone, intro gone, the template's own blocks still there.
	if _, err := tx.Exec(ctx, migrationDown594(t)); err != nil {
		t.Fatalf("apply the down migration: %v", err)
	}
	var downBlocks []map[string]any
	var downCat *string
	if err := tx.QueryRow(ctx,
		`SELECT config->'blocks', config->>'category' FROM forms
		  WHERE slug = 'tpl-fnb' AND is_template`).Scan(&downBlocks, &downCat); err != nil {
		t.Fatalf("read tpl-fnb after the down migration: %v", err)
	}
	if downCat != nil && *downCat != "" {
		t.Errorf("down migration left category = %q", *downCat)
	}
	if len(downBlocks) != 3 {
		t.Errorf("down migration left %d blocks, want the template's original 3 (heading/name/product)",
			len(downBlocks))
	}
	for i, want := range []string{"heading", "name", "product"} {
		if i >= len(downBlocks) {
			break
		}
		if got, _ := downBlocks[i]["type"].(string); got != want {
			t.Errorf("down migration block[%d].type = %q, want %q", i, got, want)
		}
	}
}

// migrationDown594 reads the rollback file. Only *.up.sql files are embedded
// and applied at boot (db.migrationFS), so the down file is read from the
// checkout — the shared reader lives in template_intro_prose_test.go.
func migrationDown594(t *testing.T) string {
	t.Helper()
	return migrationDownSQL(t, strings.TrimSuffix(migrationName594, ".up.sql")+".down.sql")
}
