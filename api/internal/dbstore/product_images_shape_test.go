package dbstore

// The `products.images` JSONB shape class — the THIRD `jsonb_array_elements`
// site a caller can reach (after `sales.items`, PR 820 / card t_d8764334, and
// `config->'blocks'`, card t_8fcb546d). Card t_c5832f47.
//
// `products.images` is `JSONB NOT NULL DEFAULT '[]'` (000001_init.up.sql) with
// no CHECK constraint, and the startup media migration expands it with
// `jsonb_array_elements_text` (media_migrate.go). Postgres raises SQLSTATE 22023
// the moment that function is handed anything that is not an array — "cannot
// extract elements from an object" for an object, "…from a scalar" for a string
// or a number.
//
// Why ONE odd row is worse here than in the gallery: MigrateMediaURLs is called
// from the API's OWN startup (cmd/jomform-api/main.go), where a non-nil error is
// `osExit(1)`. So a single product row whose `images` is not an array AND whose
// text carries the old internal prefix ({S3_ENDPOINT}/{bucket}/) does not fail
// one request — it stops the process from booting at all.
//
// Reachability of the bad column value: `products.images` is typed `[]string` on
// every REST/MCP write path, so a non-array cannot be produced by the app — it
// requires a direct/external write (an import, an external tool, a manual fix),
// the same premise the `sales.items` and `config.blocks` fixes rest on.
//
// These tests seed exactly that row on this package's scratch schema, call the
// SAME function main.go calls, and assert the migration still rewrites the
// well-formed rows. "Does not error" is not the whole assertion: the good row
// must really move, the odd row must NOT be silently destroyed (a value the
// migration cannot interpret is left alone, not replaced by `[]`), and a second
// run must still be the idempotent no-op the migration promises.
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The two values MigrateMediaURLs rewrites between: the internal S3 prefix the
// startup migration looks for, and the browser-reachable base it rewrites it to.
const (
	mediaMigrateOldPrefix = "http://minio:9000/jomform-media/"
	mediaMigrateNewBase   = "https://jomform.test/media/"
)

// nonArrayImagesShapes is the whole class, not one fixture. The object and the
// string carry the old prefix in their TEXT, which is what makes the row match
// the statement's `images::text LIKE '%prefix%'` predicate — the trigger. The
// number and the JSON null never match it, and are kept as controls: they must
// stay untouched and must not be an error either.
var nonArrayImagesShapes = []struct {
	name   string
	images string
}{
	{"object carrying the old prefix", `{"u":"http://minio:9000/jomform-media/u/odd.jpg"}`},
	{"string carrying the old prefix", `"http://minio:9000/jomform-media/u/odd.jpg"`},
	{"number", `42`},
	{"json null", `null`},
}

// seedProductWithRawImages writes a product whose `images` is set from JSON
// TEXT, so a test can put a shape on the column that no app write path produces
// (every app path serialises a Go slice, i.e. an array).
func seedProductWithRawImages(t *testing.T, ctx context.Context, p *pgxpool.Pool, acc, imagesJSON string) string {
	t.Helper()
	var id string
	if err := p.QueryRow(ctx,
		`INSERT INTO products (account_id, name, price_sen, currency, type, images)
		 VALUES ($1, 'IT images ' || md5(random()::text), 1200, 'MYR', 'one_time', $2::jsonb)
		 RETURNING id`,
		acc, imagesJSON).Scan(&id); err != nil {
		t.Fatalf("seed product with images=%s: %v", imagesJSON, err)
	}
	t.Cleanup(func() {
		_, _ = p.Exec(context.Background(), `DELETE FROM products WHERE id=$1`, id)
	})
	return id
}

// productImagesEqualJSON reports whether a product's `images` is still exactly
// the JSON value given. It compares as jsonb, not as text: Postgres re-renders a
// stored object with its own spacing (`{"u":"x"}` reads back as `{"u": "x"}`),
// so a text comparison would call a value that is unchanged "changed".
func productImagesEqualJSON(t *testing.T, ctx context.Context, p *pgxpool.Pool, id, wantJSON string) bool {
	t.Helper()
	var same bool
	if err := p.QueryRow(ctx, `SELECT images = $2::jsonb FROM products WHERE id=$1`, id, wantJSON).Scan(&same); err != nil {
		t.Fatalf("compare images of product %s against %s: %v", id, wantJSON, err)
	}
	return same
}

func productImagesText(t *testing.T, ctx context.Context, p *pgxpool.Pool, id string) string {
	t.Helper()
	var images string
	if err := p.QueryRow(ctx, `SELECT images::text FROM products WHERE id=$1`, id).Scan(&images); err != nil {
		t.Fatalf("read images of product %s: %v", id, err)
	}
	return images
}

// assertNoImagesExtractError fails with the measured error when the migration
// raised SQLSTATE 22023, so a regression names the defect (and the osExit(1) it
// causes) instead of only its symptom. (Same shape as
// sales_items_shape_test.go's assertNoExtractError, worded for this column.)
func assertNoImagesExtractError(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), "cannot extract elements") ||
		strings.Contains(err.Error(), "22023") {
		t.Fatalf("%s raised the non-array JSONB error: %v — ONE odd `products.images` value makes the "+
			"STARTUP media rewrite return an error, and cmd/jomform-api/main.go answers that with "+
			"osExit(1): the API does not boot at all", what, err)
	}
	t.Fatalf("%s: %v", what, err)
}

// TestStartupMediaMigrationSurvivesANonArrayProductImages is the card's case: one
// product whose `images` is not an array must not stop the migration that the API
// runs before it serves anything.
func TestStartupMediaMigrationSurvivesANonArrayProductImages(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)

	for _, shape := range nonArrayImagesShapes {
		t.Run(shape.name, func(t *testing.T) {
			// A fresh account per shape, and a fresh copy of the well-formed
			// row: MigrateMediaURLs is table-wide, so a row migrated by an
			// earlier subtest would make this one's count meaningless.
			acc := uniqueAccount(t, ctx, p)
			// The row the migration MUST still rewrite: a well-formed array
			// whose first element carries the old internal prefix. Its second
			// element is already public and must survive the same statement
			// untouched.
			good := seedProductWithRawImages(t, ctx, p, acc,
				`["http://minio:9000/jomform-media/u/good.jpg","https://cdn.example.test/keep.png"]`)
			bad := seedProductWithRawImages(t, ctx, p, acc, shape.images)

			n, err := MigrateMediaURLs(ctx, p, mediaMigrateOldPrefix, mediaMigrateNewBase)
			assertNoImagesExtractError(t, "MigrateMediaURLs (the startup media rewrite; a non-nil error here is osExit(1) in main.go)", err)

			// 1. The well-formed row really moved — "no error" alone would also
			//    hold for a migration that quietly skipped everything.
			goodImages := productImagesText(t, ctx, p, good)
			if !strings.Contains(goodImages, mediaMigrateNewBase+"u/good.jpg") {
				t.Errorf("the well-formed product's images = %s, want the first element rewritten to %s — "+
					"a non-array row in another product must not stop the rewrite",
					goodImages, mediaMigrateNewBase+"u/good.jpg")
			}
			if !strings.Contains(goodImages, "https://cdn.example.test/keep.png") {
				t.Errorf("the well-formed product's already-public element was lost: images = %s", goodImages)
			}
			if n < 1 {
				t.Errorf("MigrateMediaURLs reported %d rows updated, want at least 1 (the well-formed product)", n)
			}

			// 2. The odd row was NOT silently destroyed. A migration that treats
			//    a non-array as `[]` boots, but it also throws away a value it
			//    could not interpret; the tolerant reading here is to leave it
			//    exactly as it was found and let a human repair it.
			if !productImagesEqualJSON(t, ctx, p, bad, shape.images) {
				t.Errorf("the odd product's images changed from %s to %s — the migration must SKIP a value it "+
					"cannot interpret, never replace it", shape.images, productImagesText(t, ctx, p, bad))
			}

			// 3. Idempotence — the property the migration's own doc comment
			//    promises. With the odd row left in place, the second run must
			//    still find nothing to do AND must not raise on the row it just
			//    skipped.
			again, err := MigrateMediaURLs(ctx, p, mediaMigrateOldPrefix, mediaMigrateNewBase)
			assertNoImagesExtractError(t, "the second MigrateMediaURLs run (idempotence)", err)
			if again != 0 {
				t.Errorf("the second MigrateMediaURLs run updated %d rows, want 0 — the migration is "+
					"supposed to be a no-op once no stored URL has the old prefix", again)
			}
		})
	}
}
