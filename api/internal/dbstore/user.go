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
	// Phone is the user's optional phone number (issue #181). Empty = not set.
	Phone string
	// Timezone is the user's preferred IANA timezone (issue #191). Empty =
	// Auto — dashboard timestamps follow the browser/PC default.
	Timezone  string
	Verified  bool // email confirmed; new accounts require this before login (issue #42)
	CreatedAt time.Time
	UpdatedAt time.Time
	// PwResetAt is set when the user resets their password via the
	// self-service flow (issue #62). Sessions issued before this timestamp are
	// rejected by session resolution, killing every prior session. NULL until
	// the first reset (issue #63 also keys trust-cookie invalidation on it).
	PwResetAt *time.Time
	// TOTPLastStep is the last TOTP time-step whose code was accepted
	// (issue #528). TOTP was stateless, so the same 6-digit code verified
	// repeatedly for its whole drift window; recording the consumed step makes
	// each code single-use. 0 = nothing consumed yet.
	TOTPLastStep int64
	// DefaultWorkspaceID is the user's chosen default workspace (issue #164). NULL =
	// no default → multi-workspace users are prompted to pick a workspace after login.
	// Read via the dedicated accessor (DefaultWorkspace) so it is NOT part of the
	// main user scan; the column is intentionally unselected.
	DefaultWorkspaceID *string
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
		 RETURNING id, email, password_hash, COALESCE(totp_secret,''), totp_enabled, COALESCE(name,''), COALESCE(avatar_url,''), COALESCE(phone,''), COALESCE(timezone,''), verified, created_at, updated_at, pw_reset_at, totp_last_step`,
		email, passwordHash,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.TOTPSecret, &u.TOTPEnabled, &u.Name, &u.AvatarURL, &u.Phone, &u.Timezone, &u.Verified, &u.CreatedAt, &u.UpdatedAt, &u.PwResetAt, &u.TOTPLastStep)
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
		`SELECT id, email, password_hash, COALESCE(totp_secret,''), totp_enabled, COALESCE(name,''), COALESCE(avatar_url,''), COALESCE(phone,''), COALESCE(timezone,''), verified, created_at, updated_at, pw_reset_at, totp_last_step
		 FROM users WHERE email = $1`, email))
}

// ByID fetches a user by UUID string.
func (s *UserStore) ByID(ctx context.Context, id string) (*User, error) {
	return s.scanOne(s.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, COALESCE(totp_secret,''), totp_enabled, COALESCE(name,''), COALESCE(avatar_url,''), COALESCE(phone,''), COALESCE(timezone,''), verified, created_at, updated_at, pw_reset_at, totp_last_step
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

// ConsumeTOTPStep records that the TOTP time-step `step` has been used
// (issue #528), returning true only when this call actually advanced the stored
// step. The predicate is part of the UPDATE, so two concurrent submissions of
// the SAME code cannot both succeed: the first advances totp_last_step, the
// second matches no row and gets ok=false. That is what makes a TOTP code
// single-use rather than single-use-per-window.
//
// A false return must be treated as a replayed code (reject it), not as an
// error — err is only for a genuine database failure.
func (s *UserStore) ConsumeTOTPStep(ctx context.Context, userID string, step int64) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE users SET totp_last_step = $2, updated_at = now()
		  WHERE id = $1 AND totp_last_step < $2`, userID, step)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *UserStore) scanOne(row pgx.Row) (*User, error) {
	u := &User{}
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.TOTPSecret, &u.TOTPEnabled, &u.Name, &u.AvatarURL, &u.Phone, &u.Timezone, &u.Verified, &u.CreatedAt, &u.UpdatedAt, &u.PwResetAt, &u.TOTPLastStep)
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
//
// Issue #539: `verified` is now written EXPLICITLY from the caller's assessment
// of the provider's email_verified claim. It used to be omitted, so the row
// inherited the schema default — and 000017 sets `verified DEFAULT true`. Every
// social signup was therefore born verified without the provider ever asserting
// the address, bypassing the email-verification gate the password path enforces
// and exempting the row from the unverified purge sweep.
//
// emailVerified must come from the provider, never be assumed. Callers that
// cannot establish it (a provider that omits the claim, or asserts false) must
// pass false so the normal verification flow applies.
func (s *UserStore) CreateFromSocial(ctx context.Context, email, name, provider, providerUID, picture string, emailVerified bool) (string, error) {
	var id string
	email = strings.ToLower(strings.TrimSpace(email))
	// users.name is NOT NULL — never let an empty provider name become NULL.
	// Fall back to the email's local part, then a generic label.
	if strings.TrimSpace(name) == "" {
		name = emailLocalPart(email)
	}
	if name == "" {
		name = "User"
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name, avatar_url, verified)
		 VALUES ($1,'',NULLIF($2,''),NULLIF($3,''),$4) RETURNING id`,
		email, name, picture, emailVerified).Scan(&id)
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

// emailLocalPart returns the portion of an email before the '@' (trimmed),
// or "" when the address has no local part.
func emailLocalPart(email string) string {
	if i := strings.IndexByte(email, '@'); i > 0 {
		return strings.TrimSpace(email[:i])
	}
	return ""
}

// UpdateAvatar sets the user's profile picture URL (issue #133). Empty string
// clears it (falls back to the initial-letter avatar). The caller validates
// the URL is JomForm-hosted media (our upload bucket).
func (s *UserStore) UpdateAvatar(ctx context.Context, userID, avatarURL string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET avatar_url=$2, updated_at=now() WHERE id=$1`, userID, avatarURL)
	return err
}

// UpdateName sets the display name.
func (s *UserStore) UpdateName(ctx context.Context, userID, name string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET name=$2 WHERE id=$1`, userID, name)
	return err
}

// UpdatePhone sets the user's optional phone number (issue #181). Empty string
// clears it (blank = not set). The caller validates the format loosely.
func (s *UserStore) UpdatePhone(ctx context.Context, userID, phone string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET phone=NULLIF($2,''), updated_at=now() WHERE id=$1`, userID, phone)
	return err
}

// UpdateTimezone sets the user's preferred IANA timezone (issue #191). Empty
// string clears it (blank = Auto — follow the browser/PC). The caller
// validates the value is a known IANA timezone name.
func (s *UserStore) UpdateTimezone(ctx context.Context, userID, timezone string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET timezone=NULLIF($2,''), updated_at=now() WHERE id=$1`, userID, timezone)
	return err
}

// UpdateEmail changes the login email (caller verifies uniqueness).
func (s *UserStore) UpdateEmail(ctx context.Context, userID, email string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET email=$2 WHERE id=$1`, userID, strings.ToLower(email))
	return err
}

// DefaultWorkspace returns the user's default workspace id ("" when none). Read via a
// dedicated accessor so the column stays out of the main user scan (issue #164).
func (s *UserStore) DefaultWorkspace(ctx context.Context, userID string) (string, error) {
	var id *string
	err := s.pool.QueryRow(ctx, `SELECT default_workspace_id FROM users WHERE id=$1`, userID).Scan(&id)
	if err != nil {
		return "", err
	}
	if id == nil {
		return "", nil
	}
	return *id, nil
}

// SetDefaultWorkspace records the user's chosen default workspace (issue #164). The
// caller validates the workspace belongs to the user (owner or member).
func (s *UserStore) SetDefaultWorkspace(ctx context.Context, userID, workspaceID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET default_workspace_id=$2, updated_at=now() WHERE id=$1`, userID, workspaceID)
	return err
}

// ClearDefaultWorkspace removes the user's default workspace (issue #164), so the
// workspace-picker prompt returns on the next login.
func (s *UserStore) ClearDefaultWorkspace(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET default_workspace_id=NULL, updated_at=now() WHERE id=$1`, userID)
	return err
}

// CountUnverifiedOlderThan returns how many users are still unverified
// (verified=false) and were created before cutoff (issue #190). Used by the
// MCP parity tool to surface the purge backlog without deleting anything.
func (s *UserStore) CountUnverifiedOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE verified = false AND created_at < $1`, cutoff).Scan(&n)
	return n, err
}

// DeleteUnverifiedOlderThan deletes users who never confirmed their email and
// were created before cutoff (issue #190). It also cleans up their
// auto-provisioned accounts (and any owned workspaces) so no orphan rows remain.
//
// Safety: only rows with verified=false are ever deleted — the final DELETE
// carries a verified=false guard as a hard backstop, so a verified account is
// never removed even if it slipped into the scan. Accounts with sales or
// subscriptions (RESTRICT FKs) are left untouched, and the user is only
// deleted once their deletable accounts/workspaces are gone.
//
// Returns the number of users actually deleted.
func (s *UserStore) DeleteUnverifiedOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx,
		`SELECT id FROM users WHERE verified = false AND created_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	var deleted int64
	for _, id := range ids {
		// Delete workspaces owned by the user that have no linked accounts, so the
		// user delete's CASCADE on workspaces can't be blocked by accounts.workspace_id.
		if _, err := tx.Exec(ctx,
			`DELETE FROM workspaces
			 WHERE owner_user_id = $1
			   AND id NOT IN (SELECT workspace_id FROM accounts WHERE workspace_id IS NOT NULL)`,
			id); err != nil {
			return 0, err
		}
		// Delete the user's accounts (incl. the auto-provisioned one from
		// handleRegister) that have no sales/subscriptions (RESTRICT guards).
		if _, err := tx.Exec(ctx,
			`DELETE FROM accounts
			 WHERE owner_user_id = $1
			   AND id NOT IN (SELECT account_id FROM sales)
			   AND id NOT IN (SELECT account_id FROM subscriptions)`,
			id); err != nil {
			return 0, err
		}
		// Delete the user. The verified=false guard is a hard safety net.
		tag, err := tx.Exec(ctx,
			`DELETE FROM users WHERE id = $1 AND verified = false`, id)
		if err != nil {
			return 0, err
		}
		deleted += tag.RowsAffected()
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return deleted, nil
}
