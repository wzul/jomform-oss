package dbstore

// Helpers for the issue #545 quota tests.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// setUsedBytes forces the quota counter to an exact value, so a test can place an
// account at a precise point relative to the cap.
func setUsedBytes(ctx context.Context, t *testing.T, p *pgxpool.Pool, accountID string, n int64) error {
	t.Helper()
	// Drop any constraint friction by writing through the same column the
	// production guard uses.
	_, err := p.Exec(ctx, `UPDATE accounts SET media_used_bytes = $2 WHERE id = $1`, accountID, n)
	return err
}

// objstoreMaxWorkspaceBytesForTest is the per-workspace cap as declared in
// objstore.MaxWorkspaceBytes. It is duplicated here as a literal because the
// dbstore package cannot import objstore without a cycle; the parity test exists
// precisely to catch the two drifting apart (issue #545).
const objstoreMaxWorkspaceBytesForTest = 1 << 30 // 1GB
