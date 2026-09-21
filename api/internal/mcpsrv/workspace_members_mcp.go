// MCP parity tools: workspace membership + invites (issue #161, part A of #160).
// Admin-scoped — the caller must be the workspace owner or an admin member.
package mcpsrv

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// randomToken returns a 32-byte hex token for invite redemption links.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// workspaceMembersStore returns the workspace-member store, erroring when not wired.
func (s *Server) workspaceMembersStore() (*dbstore.WorkspaceMemberStore, error) {
	if s.deps.WorkspaceMembers == nil {
		return nil, errors.New("workspace members tidak tersedia")
	}
	return s.deps.WorkspaceMembers, nil
}

// requireWorkspaceAdmin resolves the caller's user id and verifies they are the
// owner or an admin member of the workspace. Returns the workspace id on success.
//
// It checks the ROLE only. Mutating member/invite tools must use
// requireWorkspaceAdminWrite, which adds the API-key write scope (issue #645:
// the two axes are independent and a write must satisfy both).
func (s *Server) requireWorkspaceAdmin(ctx context.Context, workspaceID string) (string, error) {
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return "", err
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return "", err
	}
	workspace, err := sp.ByID(ctx, workspaceID)
	if err != nil || workspace == nil {
		return "", errors.New("workspace tidak dijumpai")
	}
	if workspace.Owner == owner {
		return workspaceID, nil
	}
	sm, err := s.workspaceMembersStore()
	if err != nil {
		return "", err
	}
	admin, aerr := sm.IsAdmin(ctx, workspaceID, owner)
	if aerr == nil && admin {
		return workspaceID, nil
	}
	return "", errors.New("perlu admin workspace")
}

// requireWorkspaceAdminWrite is requireWorkspaceAdmin plus the API-key write
// scope, for the MUTATING member/invite tools (invite, update role, remove,
// cancel invite). The two list tools use the plain admin gate.
func (s *Server) requireWorkspaceAdminWrite(ctx context.Context, workspaceID string) (string, error) {
	if err := s.requireWriteScope(ctx); err != nil {
		return "", err
	}
	return s.requireWorkspaceAdmin(ctx, workspaceID)
}

// ── invite_workspace_member ─────────────────────────────────────────────────────

type inviteWorkspaceMemberIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces)"`
	Email       string `json:"email" jsonschema:"Emel ahli yang dijemput"`
	Role        string `json:"role,omitempty" jsonschema:"Peranan: read_only|manage|admin (lalai read_only)"`
}

func (s *Server) inviteWorkspaceMember(
	ctx context.Context, _ *mcp.CallToolRequest, in inviteWorkspaceMemberIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: mutating admin action — write scope AND admin role.
	if _, err := s.requireWorkspaceAdminWrite(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	role := in.Role
	if role == "" {
		role = dbstore.RoleReadOnly
	}
	if !dbstore.ValidWorkspaceRole(role) {
		return nil, nil, errors.New("role mesti read_only|manage|admin")
	}
	if in.Email == "" {
		return nil, nil, errors.New("emel wajib")
	}
	sm, err := s.workspaceMembersStore()
	if err != nil {
		return nil, nil, err
	}
	// Existing user → add as a member directly.
	if u, uerr := s.deps.Users.ByEmail(ctx, in.Email); uerr == nil && u != nil {
		if err := sm.AddMember(ctx, in.WorkspaceID, u.ID, role); err != nil {
			return nil, nil, fmt.Errorf("db: %w", err)
		}
		return nil, map[string]string{"status": "added", "email": in.Email, "role": role}, nil
	}
	// New user → pending invite (email is sent by the REST flow; MCP records
	// the invite so the admin can manage it).
	token, terr := randomToken()
	if terr != nil {
		return nil, nil, errors.New("internal error")
	}
	inv, err := sm.CreateInvite(ctx, in.WorkspaceID, in.Email, role, token)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "invited", "id": inv.ID, "email": in.Email, "role": role}, nil
}

// ── list_workspace_members ──────────────────────────────────────────────────────

type listWorkspaceMembersIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces)"`
}

type workspaceMemberOut struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Role   string `json:"role"`
}

func (s *Server) listWorkspaceMembers(
	ctx context.Context, _ *mcp.CallToolRequest, in listWorkspaceMembersIn,
) (*mcp.CallToolResult, *listOutput[workspaceMemberOut], error) {
	if _, err := s.requireWorkspaceAdmin(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	sm, err := s.workspaceMembersStore()
	if err != nil {
		return nil, nil, err
	}
	members, err := sm.ListMembers(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]workspaceMemberOut, 0, len(members))
	for _, m := range members {
		out = append(out, workspaceMemberOut{UserID: m.UserID, Email: m.Email, Name: m.Name, Role: m.Role})
	}
	return nil, newListOut(out), nil
}

// ── update_workspace_member_role ────────────────────────────────────────────────

type updateWorkspaceMemberRoleIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces)"`
	UserID      string `json:"user_id" jsonschema:"ID ahli (dari list_workspace_members)"`
	Role        string `json:"role" jsonschema:"Peranan baharu: read_only|manage|admin"`
}

func (s *Server) updateWorkspaceMemberRole(
	ctx context.Context, _ *mcp.CallToolRequest, in updateWorkspaceMemberRoleIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: mutating admin action — write scope AND admin role.
	if _, err := s.requireWorkspaceAdminWrite(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	if !dbstore.ValidWorkspaceRole(in.Role) {
		return nil, nil, errors.New("role mesti read_only|manage|admin")
	}
	sm, err := s.workspaceMembersStore()
	if err != nil {
		return nil, nil, err
	}
	if err := sm.UpdateRole(ctx, in.WorkspaceID, in.UserID, in.Role); err != nil {
		if errors.Is(err, dbstore.ErrNotWorkspaceMember) {
			return nil, nil, errors.New("ahli tidak dijumpai")
		}
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "updated", "user_id": in.UserID, "role": in.Role}, nil
}

// ── cancel_workspace_invite ─────────────────────────────────────────────────────

type cancelWorkspaceInviteIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces)"`
	InviteID    string `json:"invite_id" jsonschema:"ID jemputan (dari list_workspace_invites)"`
}

func (s *Server) cancelWorkspaceInvite(
	ctx context.Context, _ *mcp.CallToolRequest, in cancelWorkspaceInviteIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: mutating admin action — write scope AND admin role.
	if _, err := s.requireWorkspaceAdminWrite(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	sm, err := s.workspaceMembersStore()
	if err != nil {
		return nil, nil, err
	}
	if err := sm.DeleteInvite(ctx, in.WorkspaceID, in.InviteID); err != nil {
		if errors.Is(err, dbstore.ErrNotWorkspaceMember) {
			return nil, nil, errors.New("jemputan tidak dijumpai")
		}
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "deleted", "invite_id": in.InviteID}, nil
}

// ── list_workspace_invites ──────────────────────────────────────────────────────

type listWorkspaceInvitesIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces)"`
}

type workspaceInviteOut struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

func (s *Server) listWorkspaceInvites(
	ctx context.Context, _ *mcp.CallToolRequest, in listWorkspaceInvitesIn,
) (*mcp.CallToolResult, *listOutput[workspaceInviteOut], error) {
	if _, err := s.requireWorkspaceAdmin(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	sm, err := s.workspaceMembersStore()
	if err != nil {
		return nil, nil, err
	}
	invites, err := sm.ListInvites(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]workspaceInviteOut, 0, len(invites))
	for _, inv := range invites {
		out = append(out, workspaceInviteOut{ID: inv.ID, Email: inv.Email, Role: inv.Role, CreatedAt: inv.CreatedAt})
	}
	return nil, newListOut(out), nil
}

// ── remove_workspace_member ─────────────────────────────────────────────────────

type removeWorkspaceMemberIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces)"`
	UserID      string `json:"user_id" jsonschema:"ID ahli (dari list_workspace_members)"`
}

func (s *Server) removeWorkspaceMember(
	ctx context.Context, _ *mcp.CallToolRequest, in removeWorkspaceMemberIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: mutating admin action — write scope AND admin role.
	if _, err := s.requireWorkspaceAdminWrite(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	// Prevent removing the owner.
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	workspace, err := sp.ByID(ctx, in.WorkspaceID)
	if err == nil && workspace != nil && workspace.Owner == in.UserID {
		return nil, nil, errors.New("pemilik workspace tidak boleh dibuang")
	}
	sm, err := s.workspaceMembersStore()
	if err != nil {
		return nil, nil, err
	}
	if err := sm.RemoveMember(ctx, in.WorkspaceID, in.UserID); err != nil {
		if errors.Is(err, dbstore.ErrNotWorkspaceMember) {
			return nil, nil, errors.New("ahli tidak dijumpai")
		}
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "removed", "user_id": in.UserID}, nil
}
