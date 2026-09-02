// Package dbstore: Postgres stores for JomForm entities.
package dbstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrEmailTaken is returned on duplicate user email.
var ErrEmailTaken = errors.New("email already registered")

// User models a row from users table.
type User struct {
	ID           string
	Email        string
	PasswordHash string
	TOTPSecret   string // empty = 2FA not configured
	TOTPEnabled  bool
	Name         string
	AvatarURL    string
	Verified     bool // email confirmed; new accounts require this before login (issue #42)
	CreatedAt    time.Time
	UpdatedAt    time.Time
	// PwResetAt is set when the user resets their password via the
	// self-service flow (issue #62). Sessions issued before this timestamp are
	// rejected by session resolution, killing every prior session. NULL until
	// the first reset (issue #63 also keys trust-cookie invalidation on it).
	PwResetAt *time.Time
}

// UserStore wraps pgx for user operations.
type UserStore struct {
	pool *pgxpool.Pool
}

// NewUserStore builds a UserStore over the given pool.
func NewUserStore(pool *pgxpool.Pool) *UserStore { return &UserStore{pool: pool} }

// Create inserts a new user. Returns ErrEmailTaken on duplicate email.
// New accounts are created unverified (verified=false) so they must confirm
// their address before their first password login (issue #42).
func (s *UserStore) Create(ctx context.Context, email, passwordHash string) (*User, error) {
	u := &User{}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, verified) VALUES ($1, $2, false)
		 RETURNING id, email, password_hash, COALESCE(totp_secret,''), totp_enabled, COALESCE(name,''), COALESCE(avatar_url,''), verified, created_at, updated_at, pw_reset_at`,
		email, passwordHash,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.TOTPSecret, &u.TOTPEnabled, &u.Name, &u.AvatarURL, &u.Verified, &u.CreatedAt, &u.UpdatedAt, &u.PwResetAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	return u, nil
}

// ByEmail fetches a user by email (case-insensitive column).
func (s *UserStore) ByEmail(ctx context.Context, email string) (*User, error) {
	return s.scanOne(s.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, COALESCE(totp_secret,''), totp_enabled, COALESCE(name,''), COALESCE(avatar_url,''), verified, created_at, updated_at, pw_reset_at
		 FROM users WHERE email = $1`, email))
}

// ByID fetches a user by UUID string.
func (s *UserStore) ByID(ctx context.Context, id string) (*User, error) {
	return s.scanOne(s.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, COALESCE(totp_secret,''), totp_enabled, COALESCE(name,''), COALESCE(avatar_url,''), verified, created_at, updated_at, pw_reset_at
		 FROM users WHERE id = $1`, id))
}

// SetTOTPSecret stores a generated secret. Kept for explicit callers; note that
// the 2FA setup flow stores pending secrets in Valkey instead, only promoting a
// secret to this column at enable time.
func (s *UserStore) SetTOTPSecret(ctx context.Context, userID, secret string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET totp_secret = $2, updated_at = now() WHERE id = $1`, userID, secret)
	return err
}

// TOTPSecret returns the stored secret ("" when none).
func (s *UserStore) TOTPSecret(ctx context.Context, userID string) (string, error) {
	var secret string
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(totp_secret,'') FROM users WHERE id = $1`, userID).Scan(&secret)
	return secret, err
}

// EnableTOTP atomically activates 2FA: writes the confirmed secret (which was
// pending in Valkey) and sets totp_enabled=true in a single statement so the
// two columns never disagree.
func (s *UserStore) EnableTOTP(ctx context.Context, userID, secret string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET totp_secret = $2, totp_enabled = true, updated_at = now() WHERE id = $1`, userID, secret)
	return err
}

// DisableTOTP clears the secret entirely.
func (s *UserStore) DisableTOTP(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET totp_enabled = false, totp_secret = NULL, updated_at = now() WHERE id = $1`, userID)
	return err
}

func (s *UserStore) scanOne(row pgx.Row) (*User, error) {
	u := &User{}
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.TOTPSecret, &u.TOTPEnabled, &u.Name, &u.AvatarURL, &u.Verified, &u.CreatedAt, &u.UpdatedAt, &u.PwResetAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// SetVerified marks a user's email as confirmed (issue #42 verify link).
func (s *UserStore) SetVerified(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET verified = true, updated_at = now() WHERE id = $1`, userID)
	return err
}

// MarkUnverified flags a user row unverified (issue #56): when an address
// hard-bounces or complains via SNS we flip the owning account (if any) back
// to unverified so it cannot log in. Idempotent — a row with no matching user
// (e.g. a fake address never validated) is not an error.
func (s *UserStore) MarkUnverified(ctx context.Context, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET verified = false, updated_at = now() WHERE email = $1`, email)
	return err
}

// SocialIdentity mirrors user_identities rows.
type SocialIdentity struct {
	Provider    string    `json:"provider"`
	ProviderUID string    `json:"-"`
	Email       string    `json:"email"`
	DisplayName string    `json:"name"`
	Picture     string    `json:"picture"`
	CreatedAt   time.Time `json:"linked_at"`
}

// FindIdentity resolves a user id from a linked social identity.
func (s *UserStore) FindIdentity(ctx context.Context, provider, providerUID string) (string, error) {
	var userID string
	err := s.pool.QueryRow(ctx,
		`SELECT user_id FROM user_identities WHERE provider=$1 AND provider_uid=$2`,
		provider, providerUID).Scan(&userID)
	if err != nil {
		return "", err
	}
	return userID, err
}

// LinkIdentity attaches a social identity to a user (upsert).
func (s *UserStore) LinkIdentity(ctx context.Context, userID, provider, providerUID, email, name, picture string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_identities (user_id, provider, provider_uid, email, display_name, picture)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (provider, provider_uid) DO UPDATE SET user_id=EXCLUDED.user_id, email=EXCLUDED.email`,
		userID, provider, providerUID, email, name, picture)
	return err
}

// UnlinkIdentity removes a provider link (password login still required).
func (s *UserStore) UnlinkIdentity(ctx context.Context, userID, provider string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM user_identities WHERE user_id=$1 AND provider=$2`, userID, provider)
	return err
}

// ListIdentities shows linked providers for a user.
func (s *UserStore) ListIdentities(ctx context.Context, userID string) ([]*SocialIdentity, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT provider, provider_uid, COALESCE(email,''), COALESCE(display_name,''), COALESCE(picture,''), created_at
		 FROM user_identities WHERE user_id=$1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SocialIdentity
	for rows.Next() {
		si := &SocialIdentity{}
		if err := rows.Scan(&si.Provider, &si.ProviderUID, &si.Email, &si.DisplayName, &si.Picture, &si.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, si)
	}
	return out, rows.Err()
}

// CreateFromSocial creates a user from a social profile (no password).
func (s *UserStore) CreateFromSocial(ctx context.Context, email, name, provider, providerUID, picture string) (string, error) {
	var id string
	email = strings.ToLower(strings.TrimSpace(email))
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name, avatar_url) VALUES ($1,'',NULLIF($2,''),NULLIF($3,'')) RETURNING id`, email, name, picture).Scan(&id)
	if err != nil {
		// Existing user (unique email)? return its id instead.
		var existing string
		if qerr := s.pool.QueryRow(ctx,
			`SELECT id FROM users WHERE email=$1`, email).Scan(&existing); qerr == nil {
			return existing, nil
		}
		return "", err
	}
	_ = s.LinkIdentity(ctx, id, provider, providerUID, email, name, picture)
	return id, nil
}

// UpdateName sets the display name.
func (s *UserStore) UpdateName(ctx context.Context, userID, name string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET name=$2 WHERE id=$1`, userID, name)
	return err
}

// UpdateEmail changes the login email (caller verifies uniqueness).
func (s *UserStore) UpdateEmail(ctx context.Context, userID, email string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET email=$2 WHERE id=$1`, userID, strings.ToLower(email))
	return err
}
