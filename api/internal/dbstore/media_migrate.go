// Issue #145: one-time data migration that rewrites already-stored internal
// media URLs (e.g. http://minio:9000/jomform-media/u/...) to the new
// browser-reachable public base (e.g. https://jomform.com/media/u/...).
//
// The schema migrations are static SQL files and cannot know the runtime
// S3_PUBLIC_BASE, so this runs as a Go data migration at startup, right after
// db.Migrate. It is idempotent: after the first run no stored URL still starts
// with the old internal prefix, so subsequent runs are no-ops.
package dbstore

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MigrateMediaURLs rewrites any stored media URL that begins with oldPrefix to
// begin with newBase. It covers the three places uploaded media URLs are
// persisted: users.avatar_url, forms.logo_url, and products.images (a JSONB
// array of strings). Returns the total number of rows updated.
func MigrateMediaURLs(ctx context.Context, pool *pgxpool.Pool, oldPrefix, newBase string) (int, error) {
	if oldPrefix == "" || newBase == "" {
		return 0, nil
	}
	// Normalise: oldPrefix is the internal {S3_ENDPOINT}/{bucket}/ prefix;
	// newBase is the browser-reachable base (no trailing slash).
	oldPrefix = trimSlash(oldPrefix) + "/"
	newBase = trimSlash(newBase) + "/"

	total := 0
	// users.avatar_url
	if n, err := migrateTextColumn(ctx, pool, "users", "avatar_url", oldPrefix, newBase); err != nil {
		return total, err
	} else {
		total += n
	}
	// forms.logo_url
	if n, err := migrateTextColumn(ctx, pool, "forms", "logo_url", oldPrefix, newBase); err != nil {
		return total, err
	} else {
		total += n
	}
	// products.images (JSONB array of strings). A non-array value in that
	// column is skipped, never rewritten — see mediaImagesUpdateSQL: one odd
	// row must not turn the API's own startup into osExit(1).
	if n, err := migrateProductImages(ctx, pool, oldPrefix, newBase); err != nil {
		return total, err
	} else {
		total += n
	}
	if total > 0 {
		slog.Info("media_url_migrated", "rows", total, "old_prefix", oldPrefix, "new_base", newBase)
	}
	return total, nil
}

// migrateTextColumn rewrites a TEXT column's stored URLs.
func migrateTextColumn(ctx context.Context, pool *pgxpool.Pool, table, column, oldPrefix, newBase string) (int, error) {
	tag, err := pool.Exec(ctx,
		fmt.Sprintf(`UPDATE %s SET %s = replace(%s, $1, $2) WHERE %s LIKE '%%' || $1 || '%%'`,
			table, column, column, column),
		oldPrefix, newBase)
	if err != nil {
		return 0, fmt.Errorf("migrate %s.%s: %w", table, column, err)
	}
	return int(tag.RowsAffected()), nil
}

// productImagesArray renders `products.images` (the column named by col) as a
// JSON array, whatever the column actually holds.
//
// Why this exists: `products.images` is `JSONB NOT NULL DEFAULT '[]'`
// (000001_init.up.sql) with NO CHECK constraint, and `jsonb_array_elements_text`
// raises SQLSTATE 22023 ("cannot extract elements from an object" / "…from a
// scalar") on anything that is not an array. Every application write path types
// the field `[]string`, so a non-array can only come from a direct/external
// write (an import, an external tool, a manual fix) — but ONE such row is enough
// to matter here, because the caller is the API's OWN startup
// (cmd/jomform-api/main.go): a non-nil error from MigrateMediaURLs is osExit(1),
// so the process does not boot at all rather than failing one request.
//
// Treating a non-array as empty is the tolerant reading, exactly as
// saleItemsArray and formBlocksArray do. The WHERE clause of the statement that
// uses this helper goes one step further and leaves such a row untouched — see
// mediaImagesUpdateSQL.
//
// The test is `jsonb_typeof(col) = 'array'`, NOT `COALESCE(col, '[]'::jsonb)`:
// COALESCE only replaces SQL NULL, and it is precisely a NON-NULL non-array
// value (an object, a string, a number) that makes the extraction raise. The
// guard has to ask what the value IS, not whether it is present.
//
// Keep EVERY expansion of `products.images` going through this helper — a bare
// `jsonb_array_elements_text(<col>)` on that column reintroduces the
// startup-time failure it exists to prevent.
func productImagesArray(col string) string {
	return "CASE WHEN jsonb_typeof(" + col + ") = 'array' THEN " + col + " ELSE '[]'::jsonb END"
}

// mediaImagesUpdateSQL is the statement migrateProductImages runs. It rewrites
// the internal prefix inside every string element of `products.images`.
//
// TWO things keep a non-array value from mattering, and both are here on
// purpose:
//
//   - the expansion goes through productImagesArray, so the extraction can never
//     be handed a non-array and raise SQLSTATE 22023;
//   - the WHERE requires `jsonb_typeof(images) = 'array'`, so a row whose value
//     is not an array is not UPDATED at all. The CASE alone would have rewritten
//     such a row to `[]` — it would boot, but it would silently destroy a value
//     the migration cannot interpret. Skipping it keeps the row exactly as it
//     was found, for a human to repair.
//
// Only rows whose stored text carries the old prefix are touched, and once
// rewritten no stored URL carries it any more — which is what makes
// MigrateMediaURLs idempotent.
//
// var, not const, only because the expansion is assembled from
// productImagesArray(); the statement is fixed at init and never mutated.
var mediaImagesUpdateSQL = `UPDATE products SET images = (
			SELECT jsonb_agg(replace(elem, $1, $2))
			FROM jsonb_array_elements_text(` + productImagesArray("images") + `) AS t(elem)
		) WHERE images::text LIKE '%' || $1 || '%'
		  AND jsonb_typeof(images) = 'array'`

// migrateProductImages rewrites each string element of products.images.
func migrateProductImages(ctx context.Context, pool *pgxpool.Pool, oldPrefix, newBase string) (int, error) {
	tag, err := pool.Exec(ctx, mediaImagesUpdateSQL, oldPrefix, newBase)
	if err != nil {
		return 0, fmt.Errorf("migrate products.images: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
