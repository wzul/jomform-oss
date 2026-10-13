package dbstore

// The `config.blocks` JSONB shape class — the SECOND `jsonb_array_elements`
// site a caller can reach, found while verifying PR 820 (which fixed the same
// class on `sales.items`, card t_d8764334).
//
// `forms.config` is `JSONB NOT NULL DEFAULT '{}'` (000001_init.up.sql) with no
// CHECK constraint, and the gallery-metadata predicate expands
// `config->'blocks'` with `jsonb_array_elements` (form.go, galleryMetadataMissingSQL).
// Postgres raises SQLSTATE 22023 the moment that function is handed anything
// that is not an array, and the guard the predicate used — `COALESCE(col,
// '[]'::jsonb)` — only replaces SQL NULL: a NON-NULL non-array (an object, a
// string, a number) and even a JSON `null` pass straight through to the
// extraction and raise.
//
// Why that is worse than one bad row: `BlockedGalleryTemplates` and
// `SetStatusMany` both expand the column for a WHOLE SET of ids in one
// statement (`id = ANY($2)`), so ONE malformed row makes a bulk publish fail
// for every id in the batch — a well-formed form in the same action stayed
// `draft` while the caller saw 500 {"code":"internal"} (measured by the QA card,
// reproduced identically on pristine origin/main 376a50c9).
//
// These tests seed exactly that row and assert the rest of the account still
// publishes. "Does not panic" is not the assertion: the OTHER rows in the batch
// must really move, and the malformed row itself must be refused for the RIGHT
// reason (missing gallery metadata, the #594 rule), not with the extraction
// error.
//
// The row is seeded with raw SQL because a non-array shape is out-of-band data:
// the app write paths are refused at the HTTP/MCP layer (urlguard.CheckConfigShape)
// and never produce one — but the column has no constraint, so such a row has
// to be assumed to exist.
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// nonArrayBlocksShapes is the whole class, not one fixture: an object, a string,
// a number and a JSON null all raise under the old `COALESCE` guard. The JSON
// null is the one that ALSO survives a `jsonb_typeof(col) IS NULL` style check,
// so it is kept as a case of its own.
var nonArrayBlocksShapes = []struct {
	name   string
	blocks string
}{
	{"object", `{"oops":true}`},
	{"string", `"oops"`},
	{"number", `42`},
	{"json null", `null`},
}

// seedTemplateWithRawBlocks writes a template whose config.blocks is set from
// JSON TEXT, so a test can put a shape on the column that no app write path
// produces.
func seedTemplateWithRawBlocks(t *testing.T, ctx context.Context, p *pgxpool.Pool, acc, blocksJSON string) string {
	t.Helper()
	config := `{"mode":"response","category":"qa","blocks":` + blocksJSON + `}`
	var id string
	if err := p.QueryRow(ctx,
		`INSERT INTO forms (account_id, slug, title, config, status, is_template)
		 VALUES ($1, 'tpl-blocks-' || md5(random()::text), 'Blocks shape', $2::jsonb, 'draft', true)
		 RETURNING id`,
		acc, config).Scan(&id); err != nil {
		t.Fatalf("seed template with blocks=%s: %v", blocksJSON, err)
	}
	t.Cleanup(func() {
		_, _ = p.Exec(context.Background(), `DELETE FROM forms WHERE id=$1`, id)
	})
	return id
}

// assertNoBlocksExtractError fails with the measured error when a read raised
// SQLSTATE 22023, so a regression names the defect instead of only its symptom.
// (Same shape as sales_items_shape_test.go's assertNoExtractError, worded for
// this column.)
func assertNoBlocksExtractError(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), "cannot extract elements") ||
		strings.Contains(err.Error(), "22023") {
		t.Fatalf("%s raised the non-array JSONB error: %v — ONE odd `config.blocks` value takes "+
			"out the whole statement, and the statement covers every id in the batch", what, err)
	}
	t.Fatalf("%s: %v", what, err)
}

// TestBulkPublishSurvivesANonArrayConfigBlocks is the card's case: one template
// whose `config.blocks` is not an array must not veto the merchant's other
// selections, and the listing that explains the skip must still answer.
func TestBulkPublishSurvivesANonArrayConfigBlocks(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	fs := NewFormStore(p)
	acc := uniqueAccount(t, ctx, p)

	for _, shape := range nonArrayBlocksShapes {
		t.Run(shape.name, func(t *testing.T) {
			// A well-formed template in the SAME account, and a plain form: both
			// must still publish while the malformed row sits beside them.
			good := templateWith(t, ctx, p, acc, "tpl-blocks-good-"+strings.ReplaceAll(shape.name, " ", "-"),
				FormStatusDraft, completeTemplateConfig())
			plain := &Form{
				AccountID: acc, Slug: "form-blocks-plain-" + strings.ReplaceAll(shape.name, " ", "-"),
				Title: "Plain", Status: FormStatusDraft, Config: map[string]any{"mode": "response"},
			}
			if err := fs.Create(ctx, plain); err != nil {
				t.Fatalf("create plain form: %v", err)
			}
			t.Cleanup(func() {
				_, _ = p.Exec(context.Background(), `DELETE FROM forms WHERE id=$1`, plain.ID)
			})

			bad := seedTemplateWithRawBlocks(t, ctx, p, acc, shape.blocks)

			// 1. The bulk listing the handler uses to explain a skip expands the
			//    column for the WHOLE id set.
			ids := []string{good.ID, bad, plain.ID}
			blocked, err := fs.BlockedGalleryTemplates(ctx, acc, ids)
			assertNoBlocksExtractError(t, "BlockedGalleryTemplates (the bulk skip listing)", err)
			if len(blocked) != 1 || blocked[0] != bad {
				t.Errorf("blocked = %v, want exactly the malformed template %s", blocked, bad)
			}

			// 2. The bulk publish itself: the two publishable rows must move.
			n, err := fs.SetStatusMany(ctx, acc, ids, FormStatusPublished)
			assertNoBlocksExtractError(t, "SetStatusMany (bulk publish)", err)
			if n != 2 {
				t.Errorf("SetStatusMany published %d rows, want 2 (the complete template + the plain form) — "+
					"ONE malformed row must not veto the batch", n)
			}
			var goodStatus, plainStatus, badStatus string
			if err := p.QueryRow(ctx,
				`SELECT (SELECT status FROM forms WHERE id=$1),
				        (SELECT status FROM forms WHERE id=$2),
				        (SELECT status FROM forms WHERE id=$3)`,
				good.ID, plain.ID, bad).Scan(&goodStatus, &plainStatus, &badStatus); err != nil {
				t.Fatalf("read statuses: %v", err)
			}
			if goodStatus != FormStatusPublished || plainStatus != FormStatusPublished {
				t.Errorf("after the bulk publish: well-formed template=%q plain form=%q, want both published",
					goodStatus, plainStatus)
			}
			if badStatus != FormStatusDraft {
				t.Errorf("the malformed template is %q after a bulk publish, want draft (skipped)", badStatus)
			}

			// 3. The malformed row's OWN publish must be refused for the right
			//    reason — the #594 gallery-metadata rule, not the extraction error.
			if err := fs.SetStatus(ctx, acc, bad, FormStatusPublished); !errors.Is(err, ErrTemplateGalleryMetadata) {
				t.Fatalf("SetStatus on the malformed template = %v, want ErrTemplateGalleryMetadata "+
					"(a non-array blocks reads as NO blocks, so the row has no description)", err)
			}
			if err := fs.SetStatusWithPassword(ctx, acc, bad, FormStatusPublished, ""); !errors.Is(err, ErrTemplateGalleryMetadata) {
				t.Fatalf("SetStatusWithPassword on the malformed template = %v, want ErrTemplateGalleryMetadata", err)
			}
			var stillDraft string
			if err := p.QueryRow(ctx, `SELECT status FROM forms WHERE id=$1`, bad).Scan(&stillDraft); err != nil {
				t.Fatalf("read malformed row: %v", err)
			}
			if stillDraft != FormStatusDraft {
				t.Errorf("the malformed template is %q after a refused publish, want draft", stillDraft)
			}

			// 4. A NON-template form carrying the same bad shape is never read by
			//    the predicate at all — it must publish as before. This is the
			//    control that the trigger is the template branch, not the shape
			//    alone.
			ordinary := seedOrdinaryFormWithRawBlocks(t, ctx, p, acc, shape.blocks)
			if err := fs.SetStatus(ctx, acc, ordinary, FormStatusPublished); err != nil {
				t.Fatalf("publishing a NON-template form with a non-array blocks = %v, want nil "+
					"(the predicate only reads templates)", err)
			}
		})
	}
}

// seedOrdinaryFormWithRawBlocks is the control row: a non-template form with the
// same malformed block shape, which the gallery predicate never sees.
func seedOrdinaryFormWithRawBlocks(t *testing.T, ctx context.Context, p *pgxpool.Pool, acc, blocksJSON string) string {
	t.Helper()
	var id string
	if err := p.QueryRow(ctx,
		`INSERT INTO forms (account_id, slug, title, config, status, is_template)
		 VALUES ($1, 'form-blocks-' || md5(random()::text), 'Ordinary blocks shape',
		         ('{"mode":"response","blocks":' || $2 || '}')::jsonb, 'draft', false)
		 RETURNING id`,
		acc, blocksJSON).Scan(&id); err != nil {
		t.Fatalf("seed ordinary form with blocks=%s: %v", blocksJSON, err)
	}
	t.Cleanup(func() {
		_, _ = p.Exec(context.Background(), `DELETE FROM forms WHERE id=$1`, id)
	})
	return id
}
