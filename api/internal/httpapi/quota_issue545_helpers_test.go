package httpapi

// Helpers for the storage-quota tests (issue #545).

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// seedQuotaUsage forces an account's storage-quota counter to an exact value, so a
// test can place it at a precise point relative to the cap. The counter
// (accounts.media_used_bytes) is the figure the upload handlers enforce, so this
// is what a real near-cap account looks like.
func seedQuotaUsage(ctx context.Context, t *testing.T, accountID string, n int64) error {
	t.Helper()
	return setQuotaUsage(ctx, bundleTestPool(t), accountID, n)
}

func setQuotaUsage(ctx context.Context, p *pgxpool.Pool, accountID string, n int64) error {
	_, err := p.Exec(ctx, `UPDATE accounts SET media_used_bytes = $2 WHERE id = $1`, accountID, n)
	return err
}
