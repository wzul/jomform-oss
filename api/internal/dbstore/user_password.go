package dbstore

import "context"

// SetPasswordHash replaces a user's password hash.
func (s *UserStore) SetPasswordHash(ctx context.Context, userID, hash string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET password_hash=$1, updated_at=now() WHERE id=$2`, hash, userID)
	return err
}

// ResetPassword sets a new password hash AND stamps pw_reset_at=now() in a
// single statement (issue #62). pw_reset_at is consulted by session resolution
// to kill every session issued before the reset, and by the trusted-device
// store to invalidate all jf_trust cookies (issue #63). Existing sessions for
// the user become invalid immediately after this returns.
func (s *UserStore) ResetPassword(ctx context.Context, userID, hash string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET password_hash=$1, pw_reset_at=now(), updated_at=now() WHERE id=$2`, hash, userID)
	return err
}
