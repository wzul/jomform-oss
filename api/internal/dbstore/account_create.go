package dbstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// Account models the accounts table (defined in account.go).
// Create inserts a new account. Slug must already be unique-verified by the
// caller (or retried externally); the DB enforces uniqueness.
func (s *AccountStore) Create(ctx context.Context, a *Account) error {
	if a.ID == "" {
		a.ID = newUUIDString()
	}
	return s.pool.QueryRow(ctx,
		`INSERT INTO accounts (id, owner_user_id, name, slug, timezone, currency)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 RETURNING created_at, updated_at`,
		a.ID, a.OwnerUserID, a.Name, a.Slug, orDefault(a.Timezone, "Asia/Kuala_Lumpur"),
		orDefault(a.Currency, "MYR"),
	).Scan(&a.CreatedAt, &a.UpdatedAt)
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// newUUIDString generates a random UUID v4 string.
func newUUIDString() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	u := hex.EncodeToString(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", u[0:8], u[8:12], u[12:16], u[16:20], u[20:32])
}

// AccountExists checks slug availability.
func (s *AccountStore) SlugTaken(ctx context.Context, slug string) (bool, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE slug=$1`, slug).Scan(&n)
	return n > 0, err
}

var _ = time.Now
