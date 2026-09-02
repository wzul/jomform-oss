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
	// products.images (JSONB array of strings)
	if n, err := migrateJSONBArray(ctx, pool, "products", "images", oldPrefix, newBase); err != nil {
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

// migrateJSONBArray rewrites each string element of a JSONB array column.
func migrateJSONBArray(ctx context.Context, pool *pgxpool.Pool, table, column, oldPrefix, newBase string) (int, error) {
	tag, err := pool.Exec(ctx,
		fmt.Sprintf(`UPDATE %s SET %s = (
			SELECT jsonb_agg(replace(elem, $1, $2))
			FROM jsonb_array_elements_text(%s) AS t(elem)
		) WHERE %s::text LIKE '%%' || $1 || '%%'`,
			table, column, column, column),
		oldPrefix, newBase)
	if err != nil {
		return 0, fmt.Errorf("migrate %s.%s: %w", table, column, err)
	}
	return int(tag.RowsAffected()), nil
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
