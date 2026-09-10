// Workspace membership + invites (issue #161, part A of #160).
//
// workspace_members records who belongs to a workspace and with what role
// (read_only / manage / admin). The owner is stored as a member row with
// role='admin' for uniformity, so every workspace has at least one admin.
// workspace_invites holds pending invitations for people who don't yet have a
// JomForm account; when they sign up via the emailed link the invite is
// redeemed and they are added as a member with the invited role.
package dbstore

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Workspace roles (semantics defined in issue #160).
const (
	RoleReadOnly = "read_only"
	RoleManage   = "manage"
	RoleAdmin    = "admin"
)

// ValidWorkspaceRole reports whether role is one of the supported member roles.
func ValidWorkspaceRole(role string) bool {
	switch role {
	case RoleReadOnly, RoleManage, RoleAdmin:
		return true
	}
	return false
}

// ErrNotWorkspaceMember is returned when a user is not a member of a workspace.
var ErrNotWorkspaceMember = errors.New("bukan ahli workspace")

// WorkspaceMember models a workspace_members row.
type WorkspaceMember struct {
	ID        string
	WorkspaceID   string
	UserID    string
	Role      string
	Email     string // joined from users, for listing
	Name      string // joined from users, for listing
	CreatedAt string
}

// WorkspaceInvite models a workspace_invites row.
type WorkspaceInvite struct {
	ID        string
	WorkspaceID   string
	Email     string
	Role      string
	Token     string
	CreatedAt string
}

// WorkspaceMemberStore wraps workspace_members + workspace_invites.
type WorkspaceMemberStore struct{ pool *pgxpool.Pool }

// NewWorkspaceMemberStore builds a WorkspaceMemberStore over the given pool.
func NewWorkspaceMemberStore(pool *pgxpool.Pool) *WorkspaceMemberStore {
	return &WorkspaceMemberStore{pool: pool}
}

// AddMember adds a user to a workspace with the given role. Idempotent: if the
// user is already a member the role is left unchanged (no error).
func (s *WorkspaceMemberStore) AddMember(ctx context.Context, workspaceID, userID, role string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1,$2,$3)
		 ON CONFLICT (workspace_id, user_id) DO NOTHING`,
		workspaceID, userID, role)
	return err
}

// Role returns the role a user holds in a workspace ("" when not a member).
func (s *WorkspaceMemberStore) Role(ctx context.Context, workspaceID, userID string) (string, error) {
	var role string
	err := s.pool.QueryRow(ctx,
		`SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`,
		workspaceID, userID).Scan(&role)
	if err != nil {
		return "", err
	}
	return role, nil
}

// IsAdmin reports whether the user is an admin (or owner) of the workspace.
func (s *WorkspaceMemberStore) IsAdmin(ctx context.Context, workspaceID, userID string) (bool, error) {
	role, err := s.Role(ctx, workspaceID, userID)
	if err != nil {
		return false, err
	}
	return role == RoleAdmin, nil
}

// ListMembers lists a workspace's members (with email + name) ordered by role
// (admin first) then created_at.
func (s *WorkspaceMemberStore) ListMembers(ctx context.Context, workspaceID string) ([]*WorkspaceMember, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT m.id, m.workspace_id, m.user_id, m.role, COALESCE(u.email,''), COALESCE(u.name,''), m.created_at::text
		   FROM workspace_members m
		   JOIN users u ON u.id = m.user_id
		  WHERE m.workspace_id = $1
		  ORDER BY (m.role = 'admin') DESC, m.created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*WorkspaceMember
	for rows.Next() {
		m := &WorkspaceMember{}
		if err := rows.Scan(&m.ID, &m.WorkspaceID, &m.UserID, &m.Role, &m.Email, &m.Name, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UpdateRole changes a member's role. Returns ErrNotWorkspaceMember when the user
// is not a member of the workspace.
func (s *WorkspaceMemberStore) UpdateRole(ctx context.Context, workspaceID, userID, role string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE workspace_members SET role=$3 WHERE workspace_id=$1 AND user_id=$2`,
		workspaceID, userID, role)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotWorkspaceMember
	}
	return nil
}

// RemoveMember removes a user from a workspace. Returns ErrNotWorkspaceMember when
// the user is not a member.
func (s *WorkspaceMemberStore) RemoveMember(ctx context.Context, workspaceID, userID string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`,
		workspaceID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotWorkspaceMember
	}
	return nil
}

// CreateInvite records a pending invite for an email with the given role and
// returns the invite (with its redemption token). Idempotent on (workspace,email):
// a duplicate invite returns the existing row.
func (s *WorkspaceMemberStore) CreateInvite(ctx context.Context, workspaceID, email, role, token string) (*WorkspaceInvite, error) {
	inv := &WorkspaceInvite{WorkspaceID: workspaceID, Email: email, Role: role, Token: token}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO workspace_invites (workspace_id, email, role, token) VALUES ($1,$2,$3,$4)
		 ON CONFLICT (workspace_id, email) DO UPDATE SET role=EXCLUDED.role, token=EXCLUDED.token
		 RETURNING id, workspace_id, email, role, token, created_at::text`,
		workspaceID, email, role, token).Scan(&inv.ID, &inv.WorkspaceID, &inv.Email, &inv.Role, &inv.Token, &inv.CreatedAt)
	if err != nil {
		return nil, err
	}
	return inv, nil
}

// ListInvites lists a workspace's pending invites.
func (s *WorkspaceMemberStore) ListInvites(ctx context.Context, workspaceID string) ([]*WorkspaceInvite, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, workspace_id, email, role, token, created_at::text
		   FROM workspace_invites WHERE workspace_id=$1 ORDER BY created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*WorkspaceInvite
	for rows.Next() {
		inv := &WorkspaceInvite{}
		if err := rows.Scan(&inv.ID, &inv.WorkspaceID, &inv.Email, &inv.Role, &inv.Token, &inv.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// DeleteInvite removes a pending invite by id. Returns ErrNotWorkspaceMember when
// the invite does not exist (kept simple: callers treat it as not-found).
func (s *WorkspaceMemberStore) DeleteInvite(ctx context.Context, workspaceID, inviteID string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM workspace_invites WHERE id=$1 AND workspace_id=$2`, inviteID, workspaceID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotWorkspaceMember
	}
	return nil
}

// InviteByToken resolves a pending invite by its redemption token.
func (s *WorkspaceMemberStore) InviteByToken(ctx context.Context, token string) (*WorkspaceInvite, error) {
	inv := &WorkspaceInvite{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, workspace_id, email, role, token, created_at::text
		   FROM workspace_invites WHERE token=$1`, token).
		Scan(&inv.ID, &inv.WorkspaceID, &inv.Email, &inv.Role, &inv.Token, &inv.CreatedAt)
	if err != nil {
		return nil, err
	}
	return inv, nil
}

// RedeemInvite atomically adds the user as a member with the invite's role
// and deletes the invite. Returns ErrNotWorkspaceMember when the invite is gone
// (already redeemed or deleted).
func (s *WorkspaceMemberStore) RedeemInvite(ctx context.Context, token, userID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var workspaceID, role string
	if err := tx.QueryRow(ctx,
		`SELECT workspace_id, role FROM workspace_invites WHERE token=$1`, token).
		Scan(&workspaceID, &role); err != nil {
		return ErrNotWorkspaceMember
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1,$2,$3)
		 ON CONFLICT (workspace_id, user_id) DO NOTHING`,
		workspaceID, userID, role); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM workspace_invites WHERE token=$1`, token); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
