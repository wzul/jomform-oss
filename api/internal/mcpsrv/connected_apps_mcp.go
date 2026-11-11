// MCP parity tools for the "Connected apps" dashboard surface (card
// t_9a6cc85f).
//
// The dashboard page lets a person see every live OAuth credential an AI agent
// holds, revoke one, and change its scope. Parity says the same capabilities
// must exist over MCP, so they do — with the SAME scope-ladder rule: an agent
// may LOWER its own grant through the tool, and may never RAISE it. A widening
// decision belongs to a human on a consent screen (the dashboard or the
// device-authorisation email), not to the credential that stands to gain.
package mcpsrv

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/oauthmeta"
)

// connectedAppRow is one live OAuth token pair.
type connectedAppRow struct {
	ID         string   `json:"id"`
	ClientID   string   `json:"client_id"`
	Name       string   `json:"name"`
	Scopes     []string `json:"scopes"`
	CreatedAt  string   `json:"created_at,omitempty"`
	LastUsedAt string   `json:"last_used_at,omitempty"`
}

// currentUserIDForGrants returns the authenticated USER, which is the principal
// an OAuth token is bound to (issue #307). An API key has no user, so these
// tools refuse it rather than guessing an owner.
func currentUserIDForGrants(ctx context.Context) (string, error) {
	id := IdentityFromContext(ctx)
	if id == nil || id.UserID == "" {
		return "", errors.New("this needs an OAuth connection (a connected agent); an API key is bound to a workspace, not a user")
	}
	return id.UserID, nil
}

// ── list_connected_apps ──────────────────────────────────────────────────────

func (s *Server) listConnectedApps(
	ctx context.Context, _ *mcp.CallToolRequest, _ struct{},
) (*mcp.CallToolResult, *listOutput[connectedAppRow], error) {
	userID, err := currentUserIDForGrants(ctx)
	if err != nil {
		return nil, nil, err
	}
	if s.deps.OAuth == nil {
		return nil, nil, errors.New("connected apps are not configured on this deployment")
	}
	apps, err := s.deps.OAuth.ConnectedAppsByUser(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	out := make([]connectedAppRow, 0, len(apps))
	for _, a := range apps {
		row := connectedAppRow{
			ID: a.ID, ClientID: a.ClientID, Name: a.ClientName, Scopes: a.Scope,
			CreatedAt: a.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
		}
		if a.LastUsedAt != nil && !a.LastUsedAt.IsZero() {
			row.LastUsedAt = a.LastUsedAt.UTC().Format("2006-01-02 15:04:05")
		}
		out = append(out, row)
	}
	return nil, newListOut(out), nil
}

// ── revoke_connected_app ─────────────────────────────────────────────────────

type revokeConnectedAppIn struct {
	ID string `json:"id" jsonschema:"Connection id (from list_connected_apps)"`
}

// revokeConnectedApp kills one token pair — access and refresh together.
func (s *Server) revokeConnectedApp(
	ctx context.Context, _ *mcp.CallToolRequest, in revokeConnectedAppIn,
) (*mcp.CallToolResult, map[string]string, error) {
	userID, err := currentUserIDForGrants(ctx)
	if err != nil {
		return nil, nil, err
	}
	// Revoking a credential is a write: the API-key scope gate applies to the
	// call (scope_gate_guard_test.go requires it), and the role check keeps a
	// read-only member of a shared workspace from cutting the owner's agent.
	if _, err := s.requireWriteRole(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.OAuth == nil {
		return nil, nil, errors.New("connected apps are not configured on this deployment")
	}
	if in.ID == "" {
		return nil, nil, errors.New("id is required (see list_connected_apps)")
	}
	// Revoking the pair this very call is riding on ends the session mid-call;
	// the row is gone either way, so the refusal below is not a safety check but
	// a courtesy for the common "agent revokes itself" mistake.
	if err := s.deps.OAuth.RevokeTokenByIDForUser(ctx, userID, in.ID); err != nil {
		if errors.Is(err, dbstore.ErrOAuthTokenMissing) {
			return nil, nil, errors.New("this connection is not active")
		}
		return nil, nil, err
	}
	return nil, map[string]string{"status": "revoked"}, nil
}

// ── set_connected_app_scope ──────────────────────────────────────────────────

type setConnectedAppScopeIn struct {
	ID    string `json:"id" jsonschema:"Connection id (from list_connected_apps)"`
	Scope string `json:"scope" jsonschema:"read (narrower) — widening to readwrite is refused here and needs a human consent"`
}

// setConnectedAppScope LOWERs a connection's scope by rotating its pair.
//
// It deliberately cannot widen: an agent that could raise its own grant would
// make the whole ladder theatre — one failed write, one self-issued upgrade, and
// the consent screen is bypassed. Widening goes through the dashboard
// (one confirm) or a new device authorisation (the email is the consent screen).
func (s *Server) setConnectedAppScope(
	ctx context.Context, _ *mcp.CallToolRequest, in setConnectedAppScopeIn,
) (*mcp.CallToolResult, map[string]any, error) {
	userID, err := currentUserIDForGrants(ctx)
	if err != nil {
		return nil, nil, err
	}
	if _, err := s.requireWriteRole(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.OAuth == nil {
		return nil, nil, errors.New("connected apps are not configured on this deployment")
	}
	if in.ID == "" {
		return nil, nil, errors.New("id is required (see list_connected_apps)")
	}
	if strings.TrimSpace(in.Scope) != oauthmeta.ScopeRead {
		return nil, nil, errors.New("this tool can only LOWER a connection to 'read'. Widening to readwrite requires a human consent: use the Connected apps page on jomform.com or a new device authorisation")
	}
	apps, err := s.deps.OAuth.ConnectedAppsByUser(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	var current *dbstore.ConnectedApp
	for i := range apps {
		if apps[i].ID == in.ID {
			current = &apps[i]
			break
		}
	}
	if current == nil {
		return nil, nil, errors.New("this connection is not active")
	}
	if !scopeHasValue(current.Scope, oauthmeta.ScopeReadWrite) {
		return nil, map[string]any{"status": "unchanged", "scope": "read"}, nil
	}
	now := time.Now().UTC()
	access, refresh := randHex(32), randHex(32)
	nt, oldScope, err := s.deps.OAuth.RotateTokenScope(ctx, userID, in.ID, []string{oauthmeta.ScopeRead},
		auth.HashAPIKey(access), auth.HashAPIKey(refresh), now.Add(time.Hour), now.Add(30*24*time.Hour))
	if err != nil {
		return nil, nil, err
	}
	return nil, map[string]any{
		"status": "rotated", "id": nt.ID, "scope": "read",
		"previous_scope": strings.Join(oldScope, " "),
		"access_token":   access, "refresh_token": refresh,
		"note": "The previous credential is revoked. Copy these now — they are shown once.",
	}, nil
}

func scopeHasValue(scope []string, want string) bool {
	for _, s := range scope {
		if s == want {
			return true
		}
	}
	return false
}
