// System-wide settings (key/value) — admin-configurable.
package dbstore

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SystemSettingStore struct{ pool *pgxpool.Pool }

func NewSystemSettingStore(pool *pgxpool.Pool) *SystemSettingStore {
	return &SystemSettingStore{pool: pool}
}

// Get returns the value or "" when unset.
func (s *SystemSettingStore) Get(ctx context.Context, key string) (string, error) {
	var v string
	err := s.pool.QueryRow(ctx, `SELECT value FROM system_settings WHERE key=$1`, key).Scan(&v)
	if err != nil {
		return "", err
	}
	return v, nil
}

// Set upserts a setting.
func (s *SystemSettingStore) Set(ctx context.Context, key, value string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO system_settings (key, value) VALUES ($1,$2)
		 ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, key, value)
	return err
}

// All returns every setting (admin UI).
func (s *SystemSettingStore) All(ctx context.Context) ([]map[string]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT key, value FROM system_settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]string
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out = append(out, map[string]string{"key": k, "value": v})
	}
	return out, rows.Err()
}
