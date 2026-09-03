// Space membership + invites (issue #161, part A of #160).
//
// space_members records who belongs to a space and with what role
// (read_only / manage / admin). The owner is stored as a member row with
// role='admin' for uniformity, so every space has at least one admin.
// space_invites holds pending invitations for people who don't yet have a
// JomForm account; when they sign up via the emailed link the invite is
// redeemed and they are added as a member with the invited role.
package dbstore

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Space roles (semantics defined in issue #160).
const (
	RoleReadOnly = "read_only"
	RoleManage   = "manage"
	RoleAdmin    = "admin"
)

// ValidSpaceRole reports whether role is one of the supported member roles.
func ValidSpaceRole(role string) bool {
	switch role {
	case RoleReadOnly, RoleManage, RoleAdmin:
		return true
	}
	return false
}

// ErrNotSpaceMember is returned when a user is not a member of a space.
var ErrNotSpaceMember = errors.New("bukan ahli space")

// SpaceMember models a space_members row.
type SpaceMember struct {
	ID        string
	SpaceID   string
	UserID    string
	Role      string
	Email     string // joined from users, for listing
	Name      string // joined from users, for listing
	CreatedAt string
}

// SpaceInvite models a space_invites row.
type SpaceInvite struct {
	ID        string
	SpaceID   string
	Email     string
	Role      string
	Token     string
	CreatedAt string
}

// SpaceMemberStore wraps space_members + space_invites.
type SpaceMemberStore struct{ pool *pgxpool.Pool }

// NewSpaceMemberStore builds a SpaceMemberStore over the given pool.
func NewSpaceMemberStore(pool *pgxpool.Pool) *SpaceMemberStore {
	return &SpaceMemberStore{pool: pool}
}

// AddMember adds a user to a space with the given role. Idempotent: if the
// user is already a member the role is left unchanged (no error).
func (s *SpaceMemberStore) AddMember(ctx context.Context, spaceID, userID, role string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO space_members (space_id, user_id, role) VALUES ($1,$2,$3)
		 ON CONFLICT (space_id, user_id) DO NOTHING`,
		spaceID, userID, role)
	return err
}

// Role returns the role a user holds in a space ("" when not a member).
func (s *SpaceMemberStore) Role(ctx context.Context, spaceID, userID string) (string, error) {
	var role string
	err := s.pool.QueryRow(ctx,
		`SELECT role FROM space_members WHERE space_id=$1 AND user_id=$2`,
		spaceID, userID).Scan(&role)
	if err != nil {
		return "", err
	}
	return role, nil
}

// IsAdmin reports whether the user is an admin (or owner) of the space.
func (s *SpaceMemberStore) IsAdmin(ctx context.Context, spaceID, userID string) (bool, error) {
	role, err := s.Role(ctx, spaceID, userID)
	if err != nil {
		return false, err
	}
	return role == RoleAdmin, nil
}

// ListMembers lists a space's members (with email + name) ordered by role
// (admin first) then created_at.
func (s *SpaceMemberStore) ListMembers(ctx context.Context, spaceID string) ([]*SpaceMember, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT m.id, m.space_id, m.user_id, m.role, COALESCE(u.email,''), COALESCE(u.name,''), m.created_at::text
		   FROM space_members m
		   JOIN users u ON u.id = m.user_id
		  WHERE m.space_id = $1
		  ORDER BY (m.role = 'admin') DESC, m.created_at`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SpaceMember
	for rows.Next() {
		m := &SpaceMember{}
		if err := rows.Scan(&m.ID, &m.SpaceID, &m.UserID, &m.Role, &m.Email, &m.Name, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UpdateRole changes a member's role. Returns ErrNotSpaceMember when the user
// is not a member of the space.
func (s *SpaceMemberStore) UpdateRole(ctx context.Context, spaceID, userID, role string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE space_members SET role=$3 WHERE space_id=$1 AND user_id=$2`,
		spaceID, userID, role)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotSpaceMember
	}
	return nil
}

// RemoveMember removes a user from a space. Returns ErrNotSpaceMember when
// the user is not a member.
func (s *SpaceMemberStore) RemoveMember(ctx context.Context, spaceID, userID string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM space_members WHERE space_id=$1 AND user_id=$2`,
		spaceID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotSpaceMember
	}
	return nil
}

// CreateInvite records a pending invite for an email with the given role and
// returns the invite (with its redemption token). Idempotent on (space,email):
// a duplicate invite returns the existing row.
func (s *SpaceMemberStore) CreateInvite(ctx context.Context, spaceID, email, role, token string) (*SpaceInvite, error) {
	inv := &SpaceInvite{SpaceID: spaceID, Email: email, Role: role, Token: token}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO space_invites (space_id, email, role, token) VALUES ($1,$2,$3,$4)
		 ON CONFLICT (space_id, email) DO UPDATE SET role=EXCLUDED.role, token=EXCLUDED.token
		 RETURNING id, space_id, email, role, token, created_at::text`,
		spaceID, email, role, token).Scan(&inv.ID, &inv.SpaceID, &inv.Email, &inv.Role, &inv.Token, &inv.CreatedAt)
	if err != nil {
		return nil, err
	}
	return inv, nil
}

// ListInvites lists a space's pending invites.
func (s *SpaceMemberStore) ListInvites(ctx context.Context, spaceID string) ([]*SpaceInvite, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, space_id, email, role, token, created_at::text
		   FROM space_invites WHERE space_id=$1 ORDER BY created_at`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SpaceInvite
	for rows.Next() {
		inv := &SpaceInvite{}
		if err := rows.Scan(&inv.ID, &inv.SpaceID, &inv.Email, &inv.Role, &inv.Token, &inv.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// DeleteInvite removes a pending invite by id. Returns ErrNotSpaceMember when
// the invite does not exist (kept simple: callers treat it as not-found).
func (s *SpaceMemberStore) DeleteInvite(ctx context.Context, spaceID, inviteID string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM space_invites WHERE id=$1 AND space_id=$2`, inviteID, spaceID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotSpaceMember
	}
	return nil
}

// InviteByToken resolves a pending invite by its redemption token.
func (s *SpaceMemberStore) InviteByToken(ctx context.Context, token string) (*SpaceInvite, error) {
	inv := &SpaceInvite{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, space_id, email, role, token, created_at::text
		   FROM space_invites WHERE token=$1`, token).
		Scan(&inv.ID, &inv.SpaceID, &inv.Email, &inv.Role, &inv.Token, &inv.CreatedAt)
	if err != nil {
		return nil, err
	}
	return inv, nil
}

// RedeemInvite atomically adds the user as a member with the invite's role
// and deletes the invite. Returns ErrNotSpaceMember when the invite is gone
// (already redeemed or deleted).
func (s *SpaceMemberStore) RedeemInvite(ctx context.Context, token, userID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var spaceID, role string
	if err := tx.QueryRow(ctx,
		`SELECT space_id, role FROM space_invites WHERE token=$1`, token).
		Scan(&spaceID, &role); err != nil {
		return ErrNotSpaceMember
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO space_members (space_id, user_id, role) VALUES ($1,$2,$3)
		 ON CONFLICT (space_id, user_id) DO NOTHING`,
		spaceID, userID, role); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM space_invites WHERE token=$1`, token); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
