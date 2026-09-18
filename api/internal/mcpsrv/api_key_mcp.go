// MCP parity tools: API-key self-service (list/create/revoke).
// Mirrors the REST /v1/rest/keys handlers exactly:
//   - list  → GET    /v1/rest/keys        (account-scoped, active keys only)
//   - create→ POST   /v1/rest/keys        (hash-only storage, raw key shown once)
//   - revoke→ DELETE /v1/rest/keys/{id}   (account-scoped, permanent)
package mcpsrv

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/auth"
)

// ── list_api_keys ────────────────────────────────────────────────────────────

type apiKeyListOut struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	CreatedAt string   `json:"created_at"`
	LastUsed  string   `json:"last_used"`
}

// listAPIKeys lists the authenticated account's active API keys. The key
// material (hash) is never returned — only id/name/scopes/created_at/last_used.
func (s *Server) listAPIKeys(
	ctx context.Context, _ *mcp.CallToolRequest, _ struct{},
) (*mcp.CallToolResult, *listOutput[apiKeyListOut], error) {
	acc, err := s.accountAndScope(ctx, false) // read scope enough
	if err != nil {
		return nil, nil, err
	}
	keys, err := s.deps.Keys.ListByAccount(ctx, acc)
	if err != nil {
		return nil, nil, err
	}
	out := []apiKeyListOut{}
	for _, k := range keys {
		row := apiKeyListOut{
			ID: k.ID, Name: k.Name, Scopes: k.Scopes,
			CreatedAt: k.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
		}
		if k.LastUsedAt != nil {
			row.LastUsed = k.LastUsedAt.UTC().Format("2006-01-02 15:04:05")
		}
		out = append(out, row)
	}
	return nil, newListOut(out), nil
}

// ── create_api_key ───────────────────────────────────────────────────────────

type createAPIKeyIn struct {
	Name   string   `json:"name" jsonschema:"Nama untuk key ini"`
	Scopes []string `json:"scopes,omitempty" jsonschema:"readonly|readwrite (default: readonly)"`
}

// createAPIKey creates a new API key for the authenticated account. Only the
// hash is stored; the full raw key is returned exactly once here.
func (s *Server) createAPIKey(
	ctx context.Context, _ *mcp.CallToolRequest, in createAPIKeyIn,
) (*mcp.CallToolResult, map[string]any, error) {
	acc, err := s.requireWriteRole(ctx) // creating a key is a write
	if err != nil {
		return nil, nil, err
	}
	if in.Name == "" {
		return nil, nil, errors.New("nama key wajib")
	}
	scopes := in.Scopes
	if len(scopes) == 0 {
		scopes = []string{"readonly"}
	}
	raw, hash, err := auth.GenerateAPIKey()
	if err != nil {
		return nil, nil, err
	}
	k, err := s.deps.Keys.Create(ctx, acc, hash, in.Name, scopes)
	if err != nil {
		return nil, nil, err
	}
	// The raw key is shown exactly once — store keeps only the hash.
	return nil, map[string]any{
		"id":     k.ID,
		"name":   k.Name,
		"scopes": k.Scopes,
		"key":    raw,
		"note":   "Simpan key ini sekarang — ia tidak akan dipapar lagi.",
	}, nil
}

// ── revoke_api_key ───────────────────────────────────────────────────────────

type revokeAPIKeyIn struct {
	ID      string `json:"id" jsonschema:"ID key (dari list_api_keys)"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"Set true untuk sahkan revoke key yang sedang digunakan sekarang"`
}

// revokeAPIKey permanently revokes an account API key. Ownership is checked
// in the store (account-scoped UPDATE, mirroring REST). Revoke is permanent —
// create a new key instead. If the id targets the key currently authenticating
// this request, Confirm must be true (otherwise the call is refused), since
// revoking it would kill the current session.
func (s *Server) revokeAPIKey(
	ctx context.Context, _ *mcp.CallToolRequest, in revokeAPIKeyIn,
) (*mcp.CallToolResult, map[string]string, error) {
	acc, err := s.requireWriteRole(ctx) // revoking a key is a write
	if err != nil {
		return nil, nil, err
	}
	if in.ID == "" {
		return nil, nil, errors.New("id key wajib")
	}
	// Refuse to silently revoke the key authenticating this session unless
	// the caller explicitly confirms it in the same call.
	if cur := KeyFromContext(ctx); cur != nil && cur.ID == in.ID && !in.Confirm {
		return nil, nil, errors.New("ini key yang sedang digunakan untuk sesi ini — set confirm=true untuk sahkan revoke (ia akan memutuskan akses sendiri); atau cipta key baru")
	}
	if err := s.deps.Keys.Revoke(ctx, acc, in.ID); err != nil {
		return nil, nil, err
	}
	// Return the revoked key id so the caller can track what was revoked.
	return nil, map[string]string{"id": in.ID, "status": "revoked"}, nil
}
