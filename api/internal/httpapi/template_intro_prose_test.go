package httpapi

// Card t_d46c21e0 — the seeded template library's intro line is PROSE, and the
// rows it shipped are migrated to say so.
//
// THE DEFECT. All 14 gallery templates seeded by migration 000027 (plus the
// intro migration 000060 prepends to the three pre-#174 rows) keep their intro
// SENTENCE in a block of type `text`. `text` is the palette's only single-line
// text INPUT and its value is that field's LABEL — the response renderer has
// painted it as <input type="text"> since t_99c3a08b, the payment renderer since
// t_23289ce3, and t_7fb80479 settled the meaning everywhere else. Every one of
// these rows therefore served the sentence as a field label over an empty input
// (and, because `text` is collectable, as an extra field in a menu checkout).
//
// WHAT THESE TESTS PIN. Not one template: the class, through the REAL call
// sites. They rewind the library to the pre-fix shape with migration 000077's
// own down migration, assert the defect is really there (label + the input the
// card reproduces), apply the up migration's SQL as extracted from the embedded
// set, and then assert the intro reaches the page as prose and that nothing
// collects it. Deleting or neutering that SQL leaves the rewound rows as `text`
// blocks and every assertion below fails — a green run cannot mean "the seeded
// data happened to be fine".
//
// The last case drives the real clone route (POST /v1/templates/use), because
// that is how a merchant's form gets this content.
//
// Skipped unless JOMFORM_TEST_DATABASE_URL is set (integration convention).

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/db"
	"github.com/wzul/jomform/api/internal/formfields"
)

// introMigrationUp is the migration that retypes the library's intro to
// `section`; introMigrationDown is its rollback (also the rewind the tests use
// to put the pre-fix content model back).
const (
	introMigrationUp   = "000077_template_intro_section.up.sql"
	introMigrationDown = "000077_template_intro_section.down.sql"
)

// seededLibraryIntros is the whole shipped gallery: slug -> the mode whose
// renderer serves it. Every one of these rows opens with an intro block; the
// migration's allowlist names the same 14 slugs.
var seededLibraryIntros = []struct{ slug, mode string }{
	{"tpl-fnb", "payment"},
	{"tpl-booking", "payment"},
	{"tpl-tuition", "payment"},
	{"tpl-event", "payment"},
	{"tpl-donation", "payment"},
	{"tpl-ecommerce", "payment"},
	{"tpl-contact-simple", "response"},
	{"tpl-contact-advanced", "response"},
	{"tpl-newsletter", "response"},
	{"tpl-survey", "response"},
	{"tpl-quote", "response"},
	{"tpl-employment", "response"},
	{"tpl-gift", "response"},
	{"tpl-contest", "response"},
}

// migrationSQL reads one embedded *.up.sql migration. The embedded set is what
// db.Migrate applies, so the tests run the migration production runs rather
// than a copy of it.
func migrationSQL(t *testing.T, name string) string {
	t.Helper()
	migrations, err := db.MigrationStatements()
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	for _, m := range migrations {
		if m.Name == name {
			return m.SQL
		}
	}
	t.Fatalf("migration %s not found in the embedded set", name)
	return ""
}

// migrationDownSQL reads a rollback file from the checkout: only *.up.sql files
// are embedded and applied at boot (db.migrationFS), so the down file has to
// come from the same relative-to-the-repo layout a Go test always runs with.
func migrationDownSQL(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "db", "migrations", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// testBlock is the subset of a builder block these tests read.
type testBlock struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

// testBlocks decodes a config's builder blocks in their stored order.
func testBlocks(t *testing.T, cfg map[string]any) []testBlock {
	t.Helper()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	var doc struct {
		Blocks []testBlock `json:"blocks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode blocks: %v", err)
	}
	return doc.Blocks
}

// rowIntro reports the block that introduces a template: the first block
// carrying a value that is either PROSE (section/html — the shipped content
// model) or the `text` block that used to stand in for it. It is the block
// formfields.Description reads and the block both renderers paint, so a row is
// describable under either model.
func rowIntro(t *testing.T, cfg map[string]any) (id, typ, value string) {
	t.Helper()
	for _, b := range testBlocks(t, cfg) {
		v := strings.TrimSpace(b.Value)
		if v == "" {
			continue
		}
		switch b.Type {
		case "section", "html", "text":
			return b.ID, b.Type, v
		}
	}
	return "", "", ""
}

// carriesProse reports whether a row already has the shape the migration
// produces: a non-empty section/html block, which is where this product keeps a
// form's intro line (formfields.proseBlockTypes).
func carriesProse(t *testing.T, cfg map[string]any) bool {
	t.Helper()
	for _, b := range testBlocks(t, cfg) {
		if (b.Type == "section" || b.Type == "html") && strings.TrimSpace(b.Value) != "" {
			return true
		}
	}
	return false
}

// libraryConfig reads one seeded library row's config as the database holds it.
func libraryConfig(t *testing.T, ctx context.Context, p *pgxpool.Pool, slug string) map[string]any {
	t.Helper()
	var cfg map[string]any
	if err := p.QueryRow(ctx,
		`SELECT config FROM forms WHERE slug = $1 AND is_template AND deleted_at IS NULL`,
		slug).Scan(&cfg); err != nil {
		t.Fatalf("read library template %s: %v", slug, err)
	}
	return cfg
}

// libraryConfigs snapshots every seeded library row as canonical JSON text, so a
// migration that changed more than it claims shows up as a byte difference.
func libraryConfigs(t *testing.T, ctx context.Context, p *pgxpool.Pool) map[string]string {
	t.Helper()
	slugs := make([]string, 0, len(seededLibraryIntros))
	for _, r := range seededLibraryIntros {
		slugs = append(slugs, r.slug)
	}
	rows, err := p.Query(ctx,
		`SELECT slug, config::text FROM forms
		  WHERE slug = ANY($1) AND is_template AND deleted_at IS NULL`, slugs)
	if err != nil {
		t.Fatalf("snapshot library configs: %v", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var slug, cfg string
		if err := rows.Scan(&slug, &cfg); err != nil {
			t.Fatalf("scan snapshot: %v", err)
		}
		out[slug] = cfg
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate snapshot: %v", err)
	}
	if len(out) != len(slugs) {
		t.Fatalf("snapshot captured %d of the %d seeded library templates — the shipped gallery "+
			"is not what these tests assume", len(out), len(slugs))
	}
	return out
}

// renderSeededForm serves a config through the renderer the row's mode uses —
// the real call site, not the template by hand.
func renderSeededForm(t *testing.T, slug, mode string, cfg map[string]any) string {
	t.Helper()
	if mode == "response" {
		return renderResponsePage(t, slug, cfg)
	}
	return renderPaymentPage(t, slug, cfg)
}

// introProseMarkup is the element both renderers paint for a `section` block:
// the intro sentence as prose, with no control and no field name.
func introProseMarkup(sentence string) string {
	return `<div class="section">` + html.EscapeString(sentence) + `</div>`
}

// TestSeededTemplateIntroRendersAsProse is the card's own acceptance check: the
// intro sentence of one seeded template per mode reaches the page as prose (a
// heading), and no input is named after the block.
func TestSeededTemplateIntroRendersAsProse(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping seeded-template intro test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)

	migrated := libraryConfigs(t, ctx, pool)

	// Put the pre-fix content model back, with the migration's own rollback, so
	// the defect below is the shipped one and not a guess about it.
	if _, err := pool.Exec(ctx, migrationDownSQL(t, introMigrationDown)); err != nil {
		t.Fatalf("rewind the library with %s: %v", introMigrationDown, err)
	}

	// A payment row and a response row: both renderers paint the block, and
	// both used to paint a labelled input.
	probes := []struct{ slug, mode string }{
		{"tpl-fnb", "payment"},
		{"tpl-contact-simple", "response"},
	}
	type probe struct {
		slug, mode, id, sentence, body string
	}
	var rewound []probe
	for _, p := range probes {
		cfg := libraryConfig(t, ctx, pool, p.slug)
		id, typ, sentence := rowIntro(t, cfg)
		if typ != "text" || id == "" {
			t.Fatalf("after the rewind, %s's intro block = id %q type %q — the pre-fix shape was "+
				"not recreated and this test would pass vacuously", p.slug, id, typ)
		}
		body := renderSeededForm(t, p.slug, p.mode, cfg)
		if !strings.Contains(body, `<label class="lbl">`+html.EscapeString(sentence)+`</label>`) {
			t.Errorf("%s: the intro sentence is not the field's label in the pre-fix page — this "+
				"test does not reproduce the reported defect\n%.600s", p.slug, body)
		}
		if in := firstInputNamed(body, id); in == "" {
			t.Errorf("%s: the pre-fix page has no input named %q, so the rewind did not recreate "+
				"the defect", p.slug, id)
		}
		rewound = append(rewound, probe{slug: p.slug, mode: p.mode, id: id, sentence: sentence, body: body})
	}

	// Apply the fix, exactly as production applies it.
	if _, err := pool.Exec(ctx, migrationSQL(t, introMigrationUp)); err != nil {
		t.Fatalf("apply %s: %v", introMigrationUp, err)
	}

	for _, p := range rewound {
		cfg := libraryConfig(t, ctx, pool, p.slug)
		id, typ, sentence := rowIntro(t, cfg)
		if typ != "section" {
			t.Fatalf("%s's intro block is %q after the migration, want `section`", p.slug, typ)
		}
		if id == "" || sentence == "" {
			t.Fatalf("%s's intro block lost its id (%q) or value (%q) — only the TYPE may change",
				p.slug, id, sentence)
		}
		body := renderSeededForm(t, p.slug, p.mode, cfg)
		if !strings.Contains(body, introProseMarkup(sentence)) {
			t.Errorf("%s: the intro sentence does not reach the page as prose.\nwant: %s\n%.600s",
				p.slug, introProseMarkup(sentence), body)
		}
		if in := firstInputNamed(body, id); in != "" {
			t.Errorf("%s: the intro still renders an input: %s", p.slug, in)
		}
		if strings.Contains(body, `name="`+id+`"`) {
			t.Errorf("%s: the page still names a control %q — the sentence is still collectable",
				p.slug, id)
		}
		// The other half of the defect: `text` is collectable, so the intro was
		// part of the form's derived field contract (an extra menu-checkout
		// field). Prose is not.
		for _, f := range formfields.Contract(cfg) {
			if f.FieldID == id {
				t.Errorf("%s: the intro block %q is still a registered field (%+v)", p.slug, id, f)
			}
		}
	}

	// The pair is exactly invertible on a migrated database: rewriting the intro
	// back to `text` and forward to `section` must reproduce the row byte for
	// byte, which is what proves the migration touches nothing else in it.
	after := libraryConfigs(t, ctx, pool)
	for slug, before := range migrated {
		if after[slug] != before {
			t.Errorf("%s: down+up did not restore the row — the migration changes more than the "+
				"intro block's type.\nbefore: %s\nafter:  %s", slug, before, after[slug])
		}
	}
}

// TestEveryGalleryTemplateDescribesItselfWithProse is the whole-class guard: no
// shipped gallery template may keep its intro in a `text` block, because that
// value is a field LABEL and the card it renders is derived from it.
func TestEveryGalleryTemplateDescribesItselfWithProse(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping gallery-template intro guard")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)

	// Rewind first: without it a green run could mean nothing more than "the
	// seeded data was already fine". The rewind has to be detected on EVERY
	// row, or the guard is only checking the rows its author happened to look
	// at.
	if _, err := pool.Exec(ctx, migrationDownSQL(t, introMigrationDown)); err != nil {
		t.Fatalf("rewind the library with %s: %v", introMigrationDown, err)
	}
	if gaps := templatesWhoseIntroIsAFieldLabel(t, ctx, pool); len(gaps) != len(seededLibraryIntros) {
		t.Fatalf("after the rewind %d of %d library templates are reported; all of them keep their "+
			"intro in a `text` block, so the reproduction is partial: %v",
			len(gaps), len(seededLibraryIntros), gaps)
	}

	// The fix, then the class assertion.
	if _, err := pool.Exec(ctx, migrationSQL(t, introMigrationUp)); err != nil {
		t.Fatalf("apply %s: %v", introMigrationUp, err)
	}
	if gaps := templatesWhoseIntroIsAFieldLabel(t, ctx, pool); len(gaps) > 0 {
		t.Errorf("these gallery templates still keep their intro in a `text` block, so their card "+
			"description is derived from a field label and the sentence renders as one: %v", gaps)
	}

	// Every one of them still describes itself, which is what the 594 guard
	// requires of every gallery row.
	for _, r := range seededLibraryIntros {
		cfg := libraryConfig(t, ctx, pool, r.slug)
		if formfields.Description(cfg) == "" {
			t.Errorf("%s has no description after the migration — the gallery card would be a bare "+
				"title (issue #594)", r.slug)
		}
		if !carriesProse(t, cfg) {
			t.Errorf("%s carries no non-empty section/html block, so its description is still derived "+
				"from the legacy `text` fallback — the sentence renders as a field label", r.slug)
		}
		// And no shipped template collects its intro.
		id, typ, sentence := rowIntro(t, cfg)
		if typ != "section" {
			t.Errorf("%s: intro block type = %q, want `section`", r.slug, typ)
			continue
		}
		body := renderSeededForm(t, r.slug, r.mode, cfg)
		if in := firstInputNamed(body, id); in != "" {
			t.Errorf("%s: the intro renders an input: %s", r.slug, in)
		}
		if !strings.Contains(body, introProseMarkup(sentence)) {
			t.Errorf("%s: the intro sentence is not prose on the page it serves\n%.400s", r.slug, body)
		}
	}
}

// templatesWhoseIntroIsAFieldLabel names every library row whose intro is still
// a non-empty `text` block — the exact defect, one row at a time.
func templatesWhoseIntroIsAFieldLabel(t *testing.T, ctx context.Context, p *pgxpool.Pool) []string {
	t.Helper()
	var gaps []string
	for _, r := range seededLibraryIntros {
		cfg := libraryConfig(t, ctx, p, r.slug)
		id, typ, sentence := rowIntro(t, cfg)
		if typ == "text" && sentence != "" {
			gaps = append(gaps, fmt.Sprintf("%s (intro %q is a text block: %q)", r.slug, id, sentence))
		}
	}
	return gaps
}

// TestFormCreatedFromATemplateCarriesTheIntroAsProse drives the real clone route
// (POST /v1/templates/use, the call the New-form picker makes) and serves the
// clone's stored config: a form created from the library must not inherit the
// wart, on a database where the migration has already run.
func TestFormCreatedFromATemplateCarriesTheIntroAsProse(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping template-clone intro test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)
	// Card t_d07c8fbb: POST /v1/templates/use mints a form, so an account with no
	// workspace is refused 409 (workspace.required). This test is about the
	// clone's CONFIG, so the account must be able to complete the clone.
	bundleLinkWorkspace(t, ctx, pool, accountID)
	key := bundleAPIKey(t, ctx, pool, accountID)
	h := newBundleHandlers(pool)

	for _, tc := range []struct{ slug, mode string }{
		{"tpl-fnb", "payment"},
		{"tpl-contact-simple", "response"},
	} {
		code, raw := tplReq(t, h, key, http.MethodPost, "/v1/templates/use", `{"slug":"`+tc.slug+`"}`)
		if code != http.StatusCreated {
			t.Fatalf("POST /v1/templates/use %s = %d (body=%s)", tc.slug, code, raw)
		}
		var out struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("decode clone response: %v (%s)", err, raw)
		}
		cleanup := func(id string) {
			t.Cleanup(func() {
				_, _ = pool.Exec(context.Background(), `DELETE FROM forms WHERE id=$1`, id)
			})
		}
		cleanup(out.ID)

		var owner string
		var cfg map[string]any
		if err := pool.QueryRow(ctx,
			`SELECT account_id, config FROM forms WHERE id=$1`, out.ID).Scan(&owner, &cfg); err != nil {
			t.Fatalf("read the cloned form: %v", err)
		}
		if owner != accountID {
			t.Fatalf("the clone belongs to %s, want the calling account %s", owner, accountID)
		}
		id, typ, sentence := rowIntro(t, cfg)
		if typ != "section" || sentence == "" {
			t.Fatalf("the form cloned from %s carries intro id %q type %q — a form created from the "+
				"template still holds the sentence in a field block", tc.slug, id, typ)
		}
		body := renderSeededForm(t, out.Slug, tc.mode, cfg)
		if !strings.Contains(body, introProseMarkup(sentence)) {
			t.Errorf("the clone of %s does not render its intro as prose\n%.600s", tc.slug, body)
		}
		if in := firstInputNamed(body, id); in != "" {
			t.Errorf("the clone of %s renders an input named %q: %s", tc.slug, id, in)
		}
	}
}

// TestIntroMigrationTargetsOnlyTheIntroBlock covers what the migration must NOT
// do, on the shape PRODUCTION actually holds for the three pre-#174 rows: an
// intro block 000060 prepended as 'b0-intro' in front of the row's own blocks
// (which carry no ids at all). Everything runs in a transaction that is rolled
// back, so it leaves no state behind.
func TestIntroMigrationTargetsOnlyTheIntroBlock(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping intro-migration targeting test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// tpl-fnb as production held it before this fix: migration 000060's prepended
	// intro (id 'b0-intro') in front of three id-less blocks.
	const prodFnb = `{
	  "mode": "payment",
	  "category": "Food & Beverage",
	  "blocks": [
	    {"id": "b0-intro", "type": "text", "value": "Order from our menu — pay online when you submit."},
	    {"type": "heading", "value": "Tempahan"},
	    {"type": "name", "value": "Nama"},
	    {"type": "product", "value": "Nasi Lemak", "product_id": "p1"}
	  ],
	  "products": [{"id": "p1", "name": "Nasi Lemak", "price_sen": 1000}]
	}`
	if _, err := tx.Exec(ctx,
		`UPDATE forms SET config = $1::jsonb WHERE slug = 'tpl-fnb' AND is_template`, prodFnb); err != nil {
		t.Fatalf("install the production tpl-fnb shape: %v", err)
	}

	// A row that is already complete must come out byte-identical: a WHERE that
	// is too wide would silently rewrite the rest of the library.
	var eventBefore string
	if err := tx.QueryRow(ctx,
		`SELECT config::text FROM forms WHERE slug = 'tpl-event' AND is_template`).Scan(&eventBefore); err != nil {
		t.Fatalf("read tpl-event before: %v", err)
	}

	up := migrationSQL(t, introMigrationUp)
	if _, err := tx.Exec(ctx, up); err != nil {
		t.Fatalf("apply %s: %v", introMigrationUp, err)
	}

	var blocks []testBlock
	var products []map[string]any
	var category string
	if err := tx.QueryRow(ctx,
		`SELECT config->'blocks', config->'products', config->>'category' FROM forms
		  WHERE slug = 'tpl-fnb' AND is_template`).Scan(&blocks, &products, &category); err != nil {
		t.Fatalf("read tpl-fnb after the migration: %v", err)
	}
	if len(blocks) != 4 {
		t.Fatalf("tpl-fnb has %d blocks after the migration, want 4 — the statement must retype the "+
			"intro, never add, drop or reorder a block", len(blocks))
	}
	if blocks[0].ID != "b0-intro" || blocks[0].Type != "section" ||
		blocks[0].Value != "Order from our menu — pay online when you submit." {
		t.Errorf("tpl-fnb block[0] = %+v, want the prepended intro retyped to `section` with its id "+
			"and value intact", blocks[0])
	}
	for i, want := range []string{"heading", "name", "product"} {
		if blocks[i+1].Type != want {
			t.Errorf("tpl-fnb block[%d].type = %q, want %q — the migration must not reorder or drop "+
				"the template's own blocks", i+1, blocks[i+1].Type, want)
		}
	}
	if category != "Food & Beverage" || len(products) == 0 {
		t.Errorf("the migration changed more than the intro block: category=%q products=%v",
			category, products)
	}

	// Idempotent: a second run (restart, restore, replayed migration) changes
	// nothing — the row already carries prose.
	var afterFirst string
	if err := tx.QueryRow(ctx,
		`SELECT config::text FROM forms WHERE slug = 'tpl-fnb' AND is_template`).Scan(&afterFirst); err != nil {
		t.Fatalf("read tpl-fnb before the second run: %v", err)
	}
	if _, err := tx.Exec(ctx, up); err != nil {
		t.Fatalf("re-apply %s: %v", introMigrationUp, err)
	}
	var afterSecond string
	if err := tx.QueryRow(ctx,
		`SELECT config::text FROM forms WHERE slug = 'tpl-fnb' AND is_template`).Scan(&afterSecond); err != nil {
		t.Fatalf("read tpl-fnb after the second run: %v", err)
	}
	if afterFirst != afterSecond {
		t.Errorf("re-running the migration changed the migrated row:\n first=%s\n second=%s",
			afterFirst, afterSecond)
	}
	var eventAfter string
	if err := tx.QueryRow(ctx,
		`SELECT config::text FROM forms WHERE slug = 'tpl-event' AND is_template`).Scan(&eventAfter); err != nil {
		t.Fatalf("read tpl-event after: %v", err)
	}
	if eventBefore != eventAfter {
		t.Errorf("the migration changed an already-complete template's config:\n before=%s\n after=%s",
			eventBefore, eventAfter)
	}

	// The rollback restores exactly what the up migration retyped — and leaves
	// the row's own id-less blocks in their original order.
	if _, err := tx.Exec(ctx, migrationDownSQL(t, introMigrationDown)); err != nil {
		t.Fatalf("apply %s: %v", introMigrationDown, err)
	}
	var downBlocks []testBlock
	if err := tx.QueryRow(ctx,
		`SELECT config->'blocks' FROM forms WHERE slug = 'tpl-fnb' AND is_template`).Scan(&downBlocks); err != nil {
		t.Fatalf("read tpl-fnb after the down migration: %v", err)
	}
	if len(downBlocks) != 4 || downBlocks[0].ID != "b0-intro" || downBlocks[0].Type != "text" {
		t.Errorf("the down migration left %+v, want the prepended intro back as a `text` block",
			downBlocks)
	}
	for i, want := range []string{"heading", "name", "product"} {
		if i+1 >= len(downBlocks) {
			break
		}
		if downBlocks[i+1].Type != want || downBlocks[i+1].ID != "" {
			t.Errorf("down migration block[%d] = %+v, want the template's own id-less %q block",
				i+1, downBlocks[i+1], want)
		}
	}
}
