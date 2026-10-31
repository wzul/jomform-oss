package dbstore

// Issue #594 — the write-path half of the gallery-metadata guard.
//
// The gallery endpoint and the New-form picker render a template card from
// config.category and the FIRST text block. Three hand-inserted templates had
// neither, so their cards showed a bare title. Migration 000060 repairs those
// three rows; this file covers the other half of "a new template cannot enter
// without both": the store refuses to publish a template that is missing
// either field, and a bulk publish skips such a template instead of dropping
// the whole batch.
//
// The refusal has to be distinguishable from "no such form" — a caller that is
// told "not found" would go looking for the wrong bug — so these tests assert
// the two are different errors, and that a form that is NOT a template is never
// affected by the rule.
//
// Skipped unless JOMFORM_TEST_DATABASE_URL is set (integration convention).

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/formfields"
)

// templateWith creates a template-shaped form with the given config.
func templateWith(t *testing.T, ctx context.Context, p *pgxpool.Pool, acc, slug, status string, config map[string]any) *Form {
	t.Helper()
	f := &Form{
		AccountID: acc, Slug: slug, Title: "Issue 594 " + slug,
		Status: status, IsTemplate: true, Config: config,
	}
	if err := NewFormStore(p).Create(ctx, f); err != nil {
		t.Fatalf("create %s: %v", slug, err)
	}
	t.Cleanup(func() {
		_, _ = p.Exec(context.Background(), `DELETE FROM forms WHERE id=$1`, f.ID)
	})
	return f
}

// completeTemplateConfig is the shape every gallery template must have: a
// category plus a text block whose value is the description line.
func completeTemplateConfig() map[string]any {
	return map[string]any{
		"mode":     "payment",
		"category": "Contact",
		"blocks": []any{
			map[string]any{"id": "b1", "type": "text", "value": "Get in touch."},
		},
	}
}

func TestPublishRefusesTemplateWithoutGalleryMetadata(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	fs := NewFormStore(p)
	acc := uniqueAccount(t, ctx, p)

	cases := []struct {
		name   string
		config map[string]any
	}{
		{"no category and no text block", map[string]any{"mode": "payment"}},
		{"category only", map[string]any{
			"category": "Contact",
			"blocks":   []any{map[string]any{"type": "heading", "value": "Hi"}},
		}},
		{"text block only, category empty", map[string]any{
			"category": "",
			"blocks":   []any{map[string]any{"type": "text", "value": "Get in touch."}},
		}},
		{"text block with an empty value", map[string]any{
			"category": "Contact",
			"blocks":   []any{map[string]any{"type": "text", "value": ""}},
		}},
		// t_6a23c70d widened the rule to the block types the read side treats
		// as prose, so the refusal has to be pinned for those too: an empty
		// Section (or HTML) block is still no description.
		{"section block with an empty value", map[string]any{
			"category": "Contact",
			"blocks":   []any{map[string]any{"type": "section", "value": ""}},
		}},
		{"html block with an empty value", map[string]any{
			"category": "Contact",
			"blocks":   []any{map[string]any{"type": "html", "value": ""}},
		}},
		// t_cda6afd5: whitespace is not a description. formfields.Description
		// trims a block's value before it decides the block is empty, so a
		// spaces-only Section published a template whose gallery card had a
		// category and NO subtitle — the #594 outcome the predicate exists to
		// stop. The gate now trims the same way (formProseValueSQL), so these
		// three are refused exactly like their "" siblings above.
		{"section block with a spaces-only value", map[string]any{
			"category": "Contact",
			"blocks":   []any{map[string]any{"type": "section", "value": "   "}},
		}},
		{"html block with a tab/newline value", map[string]any{
			"category": "Contact",
			"blocks":   []any{map[string]any{"type": "html", "value": "\t\n  \r\n"}},
		}},
		{"text block with a spaces-only value", map[string]any{
			"category": "Contact",
			"blocks":   []any{map[string]any{"type": "text", "value": "   "}},
		}},
		// t_b58fd4fc: the same rule, one field over. The category arm of the
		// predicate had no trim at all, so a whitespace-only category
		// published and the picker painted a chip holding only spaces ("   "
		// is truthy in JS) with a generic icon — a gallery card whose only
		// label is blank. A real intro is present in each case below, so the
		// ONLY thing wrong with the template is the category.
		{"spaces-only category", map[string]any{
			"category": "   ",
			"blocks":   []any{map[string]any{"type": "section", "value": "Get in touch."}},
		}},
		{"category of tabs and newlines", map[string]any{
			"category": "	\n  \r\n",
			"blocks":   []any{map[string]any{"type": "section", "value": "Get in touch."}},
		}},
		{"category of a non-breaking space", map[string]any{
			"category": "\u00a0",
			"blocks":   []any{map[string]any{"type": "section", "value": "Get in touch."}},
		}},
		{"category that is not a string", map[string]any{
			"category": 5,
			"blocks":   []any{map[string]any{"type": "section", "value": "Get in touch."}},
		}},
	}
	for i, tc := range cases {
		tc := tc
		slug := "tpl-594-blocked-" + string(rune('a'+i))
		f := templateWith(t, ctx, p, acc, slug, FormStatusDraft, tc.config)

		// SetStatus (the publish route's path) must refuse...
		err := fs.SetStatus(ctx, acc, f.ID, FormStatusPublished)
		if !errors.Is(err, ErrTemplateGalleryMetadata) {
			t.Errorf("%s: SetStatus error = %v, want ErrTemplateGalleryMetadata", tc.name, err)
		}
		// ...and so must SetStatusWithPassword (the status route + MCP path).
		err = fs.SetStatusWithPassword(ctx, acc, f.ID, FormStatusPublished, "")
		if !errors.Is(err, ErrTemplateGalleryMetadata) {
			t.Errorf("%s: SetStatusWithPassword error = %v, want ErrTemplateGalleryMetadata", tc.name, err)
		}
		// The refusal must not be reported as a missing row.
		if errors.Is(err, ErrFormNotFound) {
			t.Errorf("%s: refusal reported as ErrFormNotFound — the caller would hunt the wrong bug",
				tc.name)
		}
		// Nothing was written: the row is still a draft.
		var status string
		if err := p.QueryRow(ctx, `SELECT status FROM forms WHERE id=$1`, f.ID).Scan(&status); err != nil {
			t.Fatalf("read status: %v", err)
		}
		if status != FormStatusDraft {
			t.Errorf("%s: status = %q after a refused publish, want draft", tc.name, status)
		}
	}
}

func TestPublishAcceptsCompleteTemplateAndLeavesFormsAlone(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	fs := NewFormStore(p)
	acc := uniqueAccount(t, ctx, p)

	// A complete template publishes.
	good := templateWith(t, ctx, p, acc, "tpl-594-good", FormStatusDraft, completeTemplateConfig())
	if err := fs.SetStatus(ctx, acc, good.ID, FormStatusPublished); err != nil {
		t.Fatalf("publishing a complete template failed: %v", err)
	}
	var status string
	if err := p.QueryRow(ctx, `SELECT status FROM forms WHERE id=$1`, good.ID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != FormStatusPublished {
		t.Errorf("status = %q, want published", status)
	}

	// A NON-template form with no category and no text block is untouched by
	// the rule: it never reaches the gallery, so it must publish as before.
	plain := &Form{
		AccountID: acc, Slug: "form-594-plain", Title: "Plain Form",
		Status: FormStatusDraft, Config: map[string]any{"mode": "response"},
	}
	if err := fs.Create(ctx, plain); err != nil {
		t.Fatalf("create plain form: %v", err)
	}
	t.Cleanup(func() {
		_, _ = p.Exec(context.Background(), `DELETE FROM forms WHERE id=$1`, plain.ID)
	})
	if err := fs.SetStatus(ctx, acc, plain.ID, FormStatusPublished); err != nil {
		t.Fatalf("publishing a non-template form was refused: %v — the rule must only apply to "+
			"templates (issue #594)", err)
	}

	// Un-publishing and other status moves on an incomplete template are still
	// allowed: the rule is about entering the gallery, not about the row.
	incomplete := templateWith(t, ctx, p, acc, "tpl-594-incomplete", FormStatusDraft,
		map[string]any{"mode": "payment"})
	if err := fs.SetStatus(ctx, acc, incomplete.ID, FormStatusUnlisted); err != nil {
		t.Fatalf("moving an incomplete template to unlisted was refused: %v", err)
	}
	if err := fs.SetStatus(ctx, acc, incomplete.ID, FormStatusDraft); err != nil {
		t.Fatalf("moving an incomplete template back to draft was refused: %v", err)
	}

	// A missing id is still ErrFormNotFound, not the metadata error.
	err := fs.SetStatus(ctx, acc, good.ID, FormStatusPublished)
	if err != nil {
		t.Fatalf("re-publishing the complete template failed: %v", err)
	}
	missing := "00000000-0000-0000-0000-000000000000"
	if err := fs.SetStatus(ctx, acc, missing, FormStatusPublished); !errors.Is(err, ErrFormNotFound) {
		t.Errorf("SetStatus on a missing form = %v, want ErrFormNotFound", err)
	}
	if err := fs.SetStatusWithPassword(ctx, acc, missing, FormStatusPublished, ""); !errors.Is(err, ErrFormNotFound) {
		t.Errorf("SetStatusWithPassword on a missing form = %v, want ErrFormNotFound", err)
	}
}

func TestBulkPublishSkipsIncompleteTemplatesOnly(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	fs := NewFormStore(p)
	acc := uniqueAccount(t, ctx, p)

	good := templateWith(t, ctx, p, acc, "tpl-594-bulk-good", FormStatusDraft, completeTemplateConfig())
	bad := templateWith(t, ctx, p, acc, "tpl-594-bulk-bad", FormStatusDraft, map[string]any{"mode": "payment"})
	plain := &Form{
		AccountID: acc, Slug: "form-594-bulk-plain", Title: "Plain",
		Status: FormStatusDraft, Config: map[string]any{},
	}
	if err := fs.Create(ctx, plain); err != nil {
		t.Fatalf("create plain form: %v", err)
	}
	t.Cleanup(func() {
		_, _ = p.Exec(context.Background(), `DELETE FROM forms WHERE id=$1`, plain.ID)
	})

	ids := []string{good.ID, bad.ID, plain.ID}
	blocked, err := fs.BlockedGalleryTemplates(ctx, acc, ids)
	if err != nil {
		t.Fatalf("BlockedGalleryTemplates: %v", err)
	}
	if len(blocked) != 1 || blocked[0] != bad.ID {
		t.Errorf("blocked = %v, want exactly the incomplete template %s", blocked, bad.ID)
	}

	// The batch still publishes the two rows that may be published: one
	// incomplete template must not veto the merchant's other selections.
	n, err := fs.SetStatusMany(ctx, acc, ids, FormStatusPublished)
	if err != nil {
		t.Fatalf("SetStatusMany: %v", err)
	}
	if n != 2 {
		t.Errorf("SetStatusMany published %d rows, want 2 (the complete template + the plain form)", n)
	}
	var badStatus string
	if err := p.QueryRow(ctx, `SELECT status FROM forms WHERE id=$1`, bad.ID).Scan(&badStatus); err != nil {
		t.Fatalf("read skipped template: %v", err)
	}
	if badStatus != FormStatusDraft {
		t.Errorf("the incomplete template is %q after a bulk publish, want draft", badStatus)
	}

	// A bulk DRAFT action is unaffected: nothing enters the gallery.
	if _, err := fs.SetStatusMany(ctx, acc, ids, FormStatusDraft); err != nil {
		t.Fatalf("bulk draft: %v", err)
	}
	var allDraft int
	if err := p.QueryRow(ctx,
		`SELECT count(*) FROM forms WHERE id = ANY($1) AND status = 'draft'`, ids).Scan(&allDraft); err != nil {
		t.Fatalf("count drafts: %v", err)
	}
	if allDraft != 3 {
		t.Errorf("%d of 3 rows are draft after a bulk draft action, want 3", allDraft)
	}
}

// TestPublishAcceptsASectionOrHTMLDescription is the OTHER direction of the
// #594 rule (card t_6a23c70d).
//
// The predicate demanded a non-empty `text` block. Since t_7fb80479 `text` is a
// single-line text INPUT whose value is the field's LABEL, and a template's
// description is the first non-empty `section`/`html` block — which is what
// formfields.Description reads and what the seeded library now carries
// (000076). So the gate refused the shape the product itself writes and
// accepted "Nama anda" in its place, and publishing a template from the gallery
// was impossible.
//
// Each case below is a shape the OLD predicate rejected. The first assertion is
// the coupling that makes the rule one rule: the config must be one the read
// side can describe, and the gate must accept exactly those. The three publish
// paths are all driven because all three embed the same predicate.
func TestPublishAcceptsASectionOrHTMLDescription(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	fs := NewFormStore(p)
	acc := uniqueAccount(t, ctx, p)

	const sentence = "Get in touch — we usually reply within a day."
	section := func(blockType string, extra ...map[string]any) map[string]any {
		blocks := []any{map[string]any{"id": "in1", "type": blockType, "value": sentence}}
		for _, b := range extra {
			blocks = append(blocks, b)
		}
		return map[string]any{"mode": "response", "category": "Contact", "blocks": blocks}
	}

	cases := []struct {
		name   string
		config map[string]any
	}{
		{"a Section intro (the shape the builder and the seeded library write)", section("section")},
		{"an HTML intro", section("html")},
		{"a Section intro above a labelled text field", section("section",
			map[string]any{"id": "n1", "type": "text", "value": "Nama anda", "required": true})},
	}

	for i, tc := range cases {
		// The read side is the authority: if Description cannot describe this
		// config, the case is not testing what it claims to.
		if got := formfields.Description(tc.config); got != sentence {
			t.Fatalf("%s: formfields.Description = %q, want the intro sentence — this case "+
				"does not model what the gallery card reads", tc.name, got)
		}

		slug := fmt.Sprintf("tpl-6a23-ok-%d", i)

		// SetStatus — the single-form publish route.
		single := templateWith(t, ctx, p, acc, slug, FormStatusDraft, tc.config)
		if err := fs.SetStatus(ctx, acc, single.ID, FormStatusPublished); err != nil {
			t.Errorf("%s: SetStatus refused a template the gallery card CAN describe: %v",
				tc.name, err)
		}
		var status string
		if err := p.QueryRow(ctx, `SELECT status FROM forms WHERE id=$1`, single.ID).Scan(&status); err != nil {
			t.Fatalf("read status: %v", err)
		}
		if status != FormStatusPublished {
			t.Errorf("%s: status = %q after a publish, want published", tc.name, status)
		}

		// SetStatusWithPassword — the status route and the MCP update path.
		withPwd := templateWith(t, ctx, p, acc, slug+"-pwd", FormStatusDraft, tc.config)
		if err := fs.SetStatusWithPassword(ctx, acc, withPwd.ID, FormStatusPublished, ""); err != nil {
			t.Errorf("%s: SetStatusWithPassword refused a template the gallery card CAN "+
				"describe: %v", tc.name, err)
		}

		// The bulk path: not reported as a skip, and the batch publishes it.
		bulk := templateWith(t, ctx, p, acc, slug+"-bulk", FormStatusDraft, tc.config)
		blocked, err := fs.BlockedGalleryTemplates(ctx, acc, []string{bulk.ID})
		if err != nil {
			t.Fatalf("%s: BlockedGalleryTemplates: %v", tc.name, err)
		}
		if len(blocked) != 0 {
			t.Errorf("%s: BlockedGalleryTemplates = %v, want none — a bulk publish would "+
				"report this template as skipped", tc.name, blocked)
		}
		n, err := fs.SetStatusMany(ctx, acc, []string{bulk.ID}, FormStatusPublished)
		if err != nil {
			t.Fatalf("%s: SetStatusMany: %v", tc.name, err)
		}
		if n != 1 {
			t.Errorf("%s: SetStatusMany published %d rows, want 1", tc.name, n)
		}
	}
}

// TestGalleryGateTrimsExactlyWhatDescriptionTrims pins the ONE rule the
// predicate is stated in terms of (card t_cda6afd5): a template may publish iff
// formfields.Description can describe it.
//
// The divergence it closes was whitespace. The gate asked
// for a non-empty value with no trim at all, while Description asks
// `strings.TrimSpace(value) != ""`, so a Section whose value was "   " — or
// "	\n", which a JSON caller can send even though a merchant's keyboard cannot
// — counted as a description to the gate and as nothing to the card: the
// template published, the gallery row carried `"description": null`, and the
// merchant's card had a category and no subtitle. That is the #594 outcome the
// gate exists to prevent, reachable through `section`/`html` because those are
// the types the read side prefers.
//
// The whitespace set is DERIVED from unicode.IsSpace here, not hand-copied from
// formProseValueSQL, so this test does not restate the implementation: it asks
// Go what blank means, asserts the read side cannot describe such a block, and
// only then asks the gate. A code point the SQL trim forgets reds here naming
// the code point — the two rules stay in step by being one set.
func TestGalleryGateTrimsExactlyWhatDescriptionTrims(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	fs := NewFormStore(p)
	acc := uniqueAccount(t, ctx, p)

	cfg := func(blocks ...map[string]any) map[string]any {
		list := make([]any, 0, len(blocks))
		for _, b := range blocks {
			list = append(list, b)
		}
		return map[string]any{"mode": "response", "category": "Contact", "blocks": list}
	}
	prose := func(blockType, value string) map[string]any {
		return map[string]any{"id": "in1", "type": blockType, "value": value}
	}

	// The whitespace Go calls whitespace. Every code point, not a sample.
	var blanks []rune
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if unicode.IsSpace(r) {
			blanks = append(blanks, r)
		}
	}
	if len(blanks) < 20 {
		t.Fatalf("derived only %d unicode.IsSpace code points — the derivation is broken, not the "+
			"gate, so no verdict below would mean anything", len(blanks))
	}

	for _, r := range blanks {
		value := string(r)
		// PREMISE, in Go: this really is a value Go calls blank.
		if strings.TrimSpace(value) != "" {
			t.Fatalf("U+%04X: strings.TrimSpace(%q) is not empty — not a whitespace code point", r, value)
		}
		c := cfg(prose("section", value))
		// The read side is the authority: it cannot describe this block.
		if got := formfields.Description(c); got != "" {
			t.Fatalf("U+%04X: formfields.Description = %q, want \"\" — the premise of this case", r, got)
		}

		f := templateWith(t, ctx, p, acc, fmt.Sprintf("tpl-6a23-ws-%04x", r), FormStatusDraft, c)

		// The bulk path's skip listing is the predicate on its own.
		blocked, err := fs.BlockedGalleryTemplates(ctx, acc, []string{f.ID})
		if err != nil {
			t.Fatalf("U+%04X: BlockedGalleryTemplates: %v", r, err)
		}
		if len(blocked) != 1 {
			t.Errorf("U+%04X (%q): BlockedGalleryTemplates = %v, want the id — a bulk publish would "+
				"count this template as published while its gallery card has no subtitle", r, value, blocked)
		}

		// ...and the publish paths refuse it.
		if err := fs.SetStatus(ctx, acc, f.ID, FormStatusPublished); !errors.Is(err, ErrTemplateGalleryMetadata) {
			t.Errorf("U+%04X (%q): SetStatus error = %v, want ErrTemplateGalleryMetadata — the gate "+
				"accepted a value formfields.Description trims to nothing", r, value, err)
		}
		if err := fs.SetStatusWithPassword(ctx, acc, f.ID, FormStatusPublished, ""); !errors.Is(err, ErrTemplateGalleryMetadata) {
			t.Errorf("U+%04X (%q): SetStatusWithPassword error = %v, want ErrTemplateGalleryMetadata",
				r, value, err)
		}
		var status string
		if err := p.QueryRow(ctx, `SELECT status FROM forms WHERE id=$1`, f.ID).Scan(&status); err != nil {
			t.Fatalf("read status: %v", err)
		}
		if status != FormStatusDraft {
			t.Errorf("U+%04X (%q): status = %q after a refused publish, want draft", r, value, status)
		}
	}

	// THE OTHER DIRECTION — the trim must not eat a real intro. Surrounding
	// whitespace is not content, but the sentence inside it is, and the card
	// renders the trimmed sentence.
	const sentence = "Get in touch — we usually reply within a day."
	padded := cfg(prose("section", "\t  "+sentence+" \n "))
	if got := formfields.Description(padded); got != sentence {
		t.Fatalf("formfields.Description(padded) = %q, want %q", got, sentence)
	}
	ok := templateWith(t, ctx, p, acc, "tpl-6a23-ws-padded", FormStatusDraft, padded)
	if err := fs.SetStatus(ctx, acc, ok.ID, FormStatusPublished); err != nil {
		t.Errorf("SetStatus refused a Section whose intro is real text with whitespace around it: %v", err)
	}

	// And a blank Section next to a real one is still a description — the gate
	// asks whether ANY prose block carries text, exactly as Description walks
	// the blocks until it finds one.
	mixed := cfg(prose("section", "   "), prose("section", sentence))
	if got := formfields.Description(mixed); got != sentence {
		t.Fatalf("formfields.Description(mixed) = %q, want %q", got, sentence)
	}
	mixedTpl := templateWith(t, ctx, p, acc, "tpl-6a23-ws-mixed", FormStatusDraft, mixed)
	if err := fs.SetStatus(ctx, acc, mixedTpl.ID, FormStatusPublished); err != nil {
		t.Errorf("SetStatus refused a template that has a blank Section AND a described one: %v", err)
	}
}

// TestGalleryGateTrimsTemplateCategoryExactlyAsTheCardDoes pins the CATEGORY
// half of the same one rule (card t_b58fd4fc): a template may enter the gallery
// iff the card can render a category for it.
//
// The category arm of galleryMetadataMissingSQL used to compare the raw JSON
// value against the empty string, with no trim, while the description arm next
// to it trimmed. So
// `"category": "   "` published, and TemplatePicker paints
// `CATEGORY_ICON[tpl.category] || "📄"` plus `{tpl.category && <span>…}` — a
// non-empty JS string — which is a chip holding only spaces and a generic icon:
// the same bare-card outcome #594 is about, one field over from the one card
// t_cda6afd5 fixed.
//
// The whitespace set is DERIVED from unicode.IsSpace, not copied from
// formProseValueSQL, so this test does not restate the implementation: it asks
// Go what blank means, asserts the read side (formfields.Category) reports
// nothing for it, and only then asks the gate — on all three publish paths
// (single, status+password, bulk skip listing).
func TestGalleryGateTrimsTemplateCategoryExactlyAsTheCardDoes(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	fs := NewFormStore(p)
	acc := uniqueAccount(t, ctx, p)

	const sentence = "Get in touch — we usually reply within a day."
	cfg := func(category string) map[string]any {
		return map[string]any{"mode": "response", "category": category,
			"blocks": []any{map[string]any{"id": "in1", "type": "section", "value": sentence}}}
	}

	// The whitespace Go calls whitespace. Every code point, not a sample.
	var blanks []rune
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if unicode.IsSpace(r) {
			blanks = append(blanks, r)
		}
	}
	if len(blanks) < 20 {
		t.Fatalf("derived only %d unicode.IsSpace code points — the derivation is broken, not the "+
			"gate, so no verdict below would mean anything", len(blanks))
	}

	for _, r := range blanks {
		value := string(r)
		c := cfg(value)
		// PREMISE, in Go: this really is a category Go calls blank.
		if got := formfields.Category(c); got != "" {
			t.Fatalf("U+%04X: formfields.Category(%q) = %q, want \"\" — not a whitespace code point",
				r, value, got)
		}
		// ...and the case models a card that would otherwise have something to
		// show, so the category is the only reason it may not publish.
		if got := formfields.Description(c); got != sentence {
			t.Fatalf("U+%04X: formfields.Description = %q, want the intro — the category is supposed "+
				"to be the only defect in this case", r, got)
		}

		f := templateWith(t, ctx, p, acc, fmt.Sprintf("tpl-b58f-cat-%04x", r), FormStatusDraft, c)

		// The bulk path's skip listing is the predicate on its own.
		blocked, err := fs.BlockedGalleryTemplates(ctx, acc, []string{f.ID})
		if err != nil {
			t.Fatalf("U+%04X: BlockedGalleryTemplates: %v", r, err)
		}
		if len(blocked) != 1 {
			t.Errorf("U+%04X (%q): BlockedGalleryTemplates = %v, want the id — a bulk publish would "+
				"leave this row published with a category chip holding only whitespace", r, value, blocked)
		}

		// ...and the publish paths refuse it.
		if err := fs.SetStatus(ctx, acc, f.ID, FormStatusPublished); !errors.Is(err, ErrTemplateGalleryMetadata) {
			t.Errorf("U+%04X (%q): SetStatus error = %v, want ErrTemplateGalleryMetadata — the gate "+
				"accepted a category formfields.Category trims to nothing", r, value, err)
		}
		if err := fs.SetStatusWithPassword(ctx, acc, f.ID, FormStatusPublished, ""); !errors.Is(err, ErrTemplateGalleryMetadata) {
			t.Errorf("U+%04X (%q): SetStatusWithPassword error = %v, want ErrTemplateGalleryMetadata",
				r, value, err)
		}
		var status string
		if err := p.QueryRow(ctx, `SELECT status FROM forms WHERE id=$1`, f.ID).Scan(&status); err != nil {
			t.Fatalf("read status: %v", err)
		}
		if status != FormStatusDraft {
			t.Errorf("U+%04X (%q): status = %q after a refused publish, want draft", r, value, status)
		}
	}

	// THE OTHER DIRECTION — a real category with whitespace around it still
	// publishes, and the read side reports the trimmed name the picker's icon
	// map is keyed by.
	padded := cfg("	  Contact \n ")
	if got := formfields.Category(padded); got != "Contact" {
		t.Fatalf("formfields.Category(padded) = %q, want \"Contact\"", got)
	}
	ok := templateWith(t, ctx, p, acc, "tpl-b58f-cat-padded", FormStatusDraft, padded)
	if err := fs.SetStatus(ctx, acc, ok.ID, FormStatusPublished); err != nil {
		t.Errorf("SetStatus refused a template whose category is a real name with whitespace around "+
			"it: %v", err)
	}
	blocked, err := fs.BlockedGalleryTemplates(ctx, acc, []string{ok.ID})
	if err != nil {
		t.Fatalf("BlockedGalleryTemplates: %v", err)
	}
	if len(blocked) != 0 {
		t.Errorf("BlockedGalleryTemplates = %v for a padded real category, want none", blocked)
	}
}
