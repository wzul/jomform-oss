// MCP parity tools: space membership + invites (issue #161, part A of #160).
// Admin-scoped — the caller must be the space owner or an admin member.
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

// spaceMembersStore returns the space-member store, erroring when not wired.
func (s *Server) spaceMembersStore() (*dbstore.SpaceMemberStore, error) {
	if s.deps.SpaceMembers == nil {
		return nil, errors.New("space members tidak tersedia")
	}
	return s.deps.SpaceMembers, nil
}

// requireSpaceAdmin resolves the caller's user id and verifies they are the
// owner or an admin member of the space. Returns the space id on success.
func (s *Server) requireSpaceAdmin(ctx context.Context, spaceID string) (string, error) {
	owner, err := s.spaceOwner(ctx)
	if err != nil {
		return "", err
	}
	sp, err := s.spacesStore()
	if err != nil {
		return "", err
	}
	space, err := sp.ByID(ctx, spaceID)
	if err != nil || space == nil {
		return "", errors.New("space tidak dijumpai")
	}
	if space.Owner == owner {
		return spaceID, nil
	}
	sm, err := s.spaceMembersStore()
	if err != nil {
		return "", err
	}
	admin, aerr := sm.IsAdmin(ctx, spaceID, owner)
	if aerr == nil && admin {
		return spaceID, nil
	}
	return "", errors.New("perlu admin space")
}

// ── invite_space_member ─────────────────────────────────────────────────────

type inviteSpaceMemberIn struct {
	SpaceID string `json:"space_id" jsonschema:"ID space (dari list_spaces)"`
	Email   string `json:"email" jsonschema:"Emel ahli yang dijemput"`
	Role    string `json:"role,omitempty" jsonschema:"Peranan: read_only|manage|admin (lalai read_only)"`
}

func (s *Server) inviteSpaceMember(
	ctx context.Context, _ *mcp.CallToolRequest, in inviteSpaceMemberIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if _, err := s.requireSpaceAdmin(ctx, in.SpaceID); err != nil {
		return nil, nil, err
	}
	role := in.Role
	if role == "" {
		role = dbstore.RoleReadOnly
	}
	if !dbstore.ValidSpaceRole(role) {
		return nil, nil, errors.New("role mesti read_only|manage|admin")
	}
	if in.Email == "" {
		return nil, nil, errors.New("emel wajib")
	}
	sm, err := s.spaceMembersStore()
	if err != nil {
		return nil, nil, err
	}
	// Existing user → add as a member directly.
	if u, uerr := s.deps.Users.ByEmail(ctx, in.Email); uerr == nil && u != nil {
		if err := sm.AddMember(ctx, in.SpaceID, u.ID, role); err != nil {
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
	inv, err := sm.CreateInvite(ctx, in.SpaceID, in.Email, role, token)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "invited", "id": inv.ID, "email": in.Email, "role": role}, nil
}

// ── list_space_members ──────────────────────────────────────────────────────

type listSpaceMembersIn struct {
	SpaceID string `json:"space_id" jsonschema:"ID space (dari list_spaces)"`
}

type spaceMemberOut struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Role   string `json:"role"`
}

func (s *Server) listSpaceMembers(
	ctx context.Context, _ *mcp.CallToolRequest, in listSpaceMembersIn,
) (*mcp.CallToolResult, []spaceMemberOut, error) {
	if _, err := s.requireSpaceAdmin(ctx, in.SpaceID); err != nil {
		return nil, nil, err
	}
	sm, err := s.spaceMembersStore()
	if err != nil {
		return nil, nil, err
	}
	members, err := sm.ListMembers(ctx, in.SpaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]spaceMemberOut, 0, len(members))
	for _, m := range members {
		out = append(out, spaceMemberOut{UserID: m.UserID, Email: m.Email, Name: m.Name, Role: m.Role})
	}
	return nil, out, nil
}

// ── update_space_member_role ────────────────────────────────────────────────

type updateSpaceMemberRoleIn struct {
	SpaceID string `json:"space_id" jsonschema:"ID space (dari list_spaces)"`
	UserID  string `json:"user_id" jsonschema:"ID ahli (dari list_space_members)"`
	Role    string `json:"role" jsonschema:"Peranan baharu: read_only|manage|admin"`
}

func (s *Server) updateSpaceMemberRole(
	ctx context.Context, _ *mcp.CallToolRequest, in updateSpaceMemberRoleIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if _, err := s.requireSpaceAdmin(ctx, in.SpaceID); err != nil {
		return nil, nil, err
	}
	if !dbstore.ValidSpaceRole(in.Role) {
		return nil, nil, errors.New("role mesti read_only|manage|admin")
	}
	sm, err := s.spaceMembersStore()
	if err != nil {
		return nil, nil, err
	}
	if err := sm.UpdateRole(ctx, in.SpaceID, in.UserID, in.Role); err != nil {
		if errors.Is(err, dbstore.ErrNotSpaceMember) {
			return nil, nil, errors.New("ahli tidak dijumpai")
		}
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "updated", "user_id": in.UserID, "role": in.Role}, nil
}

// ── remove_space_member ─────────────────────────────────────────────────────

type removeSpaceMemberIn struct {
	SpaceID string `json:"space_id" jsonschema:"ID space (dari list_spaces)"`
	UserID  string `json:"user_id" jsonschema:"ID ahli (dari list_space_members)"`
}

func (s *Server) removeSpaceMember(
	ctx context.Context, _ *mcp.CallToolRequest, in removeSpaceMemberIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if _, err := s.requireSpaceAdmin(ctx, in.SpaceID); err != nil {
		return nil, nil, err
	}
	// Prevent removing the owner.
	sp, err := s.spacesStore()
	if err != nil {
		return nil, nil, err
	}
	space, err := sp.ByID(ctx, in.SpaceID)
	if err == nil && space != nil && space.Owner == in.UserID {
		return nil, nil, errors.New("pemilik space tidak boleh dibuang")
	}
	sm, err := s.spaceMembersStore()
	if err != nil {
		return nil, nil, err
	}
	if err := sm.RemoveMember(ctx, in.SpaceID, in.UserID); err != nil {
		if errors.Is(err, dbstore.ErrNotSpaceMember) {
			return nil, nil, errors.New("ahli tidak dijumpai")
		}
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "removed", "user_id": in.UserID}, nil
}
