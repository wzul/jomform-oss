// Package dbstore: Postgres stores for JomForm entities.
package dbstore

import (
	"context"
	"errors"
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
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// UserStore wraps pgx for user operations.
type UserStore struct {
	pool *pgxpool.Pool
}

// NewUserStore builds a UserStore over the given pool.
func NewUserStore(pool *pgxpool.Pool) *UserStore { return &UserStore{pool: pool} }

// Create inserts a new user. Returns ErrEmailTaken on duplicate email.
func (s *UserStore) Create(ctx context.Context, email, passwordHash string) (*User, error) {
	u := &User{}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2)
		 RETURNING id, email, password_hash, COALESCE(totp_secret,''), totp_enabled, created_at, updated_at`,
		email, passwordHash,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.TOTPSecret, &u.TOTPEnabled, &u.CreatedAt, &u.UpdatedAt)
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
		`SELECT id, email, password_hash, COALESCE(totp_secret,''), totp_enabled, created_at, updated_at
		 FROM users WHERE email = $1`, email))
}

// ByID fetches a user by UUID string.
func (s *UserStore) ByID(ctx context.Context, id string) (*User, error) {
	return s.scanOne(s.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, COALESCE(totp_secret,''), totp_enabled, created_at, updated_at
		 FROM users WHERE id = $1`, id))
}

// SetTOTPSecret stores a generated (not yet enabled) secret.
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

// EnableTOTP marks 2FA active.
func (s *UserStore) EnableTOTP(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET totp_enabled = true, updated_at = now() WHERE id = $1`, userID)
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
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.TOTPSecret, &u.TOTPEnabled, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}
