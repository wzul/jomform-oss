package dbstore

import "context"

// SetPasswordHash replaces a user's password hash.
func (s *UserStore) SetPasswordHash(ctx context.Context, userID, hash string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET password_hash=$1, updated_at=now() WHERE id=$2`, hash, userID)
	return err
}
