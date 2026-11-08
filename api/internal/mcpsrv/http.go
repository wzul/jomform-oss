// MCP HTTP transport wiring: streamable HTTP + API-key / OAuth-token auth.
package mcpsrv

import (
	"context"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/oauthmeta"
)

// HTTPHandler returns an http.Handler serving MCP over Streamable HTTP at
// any mount point (typically /mcp). Auth (issue #121): Authorization:
// Bearer <value> is accepted as EITHER an API key (looked up in api_keys,
// existing behaviour) OR an OAuth access token (looked up in oauth_tokens,
// opaque + revocable), and both resolve to the same account. The per-request
// server instance carries the authenticated account's identity.
func (s *Server) HTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accountID, userID, scopes, apikey, agent, ok := s.authenticate(r)
		if !ok {
			// Issue #509: an unauthenticated /mcp request MUST carry an RFC 6750
			// Bearer challenge pointing at the RFC 9728 protected-resource
			// metadata, otherwise a native MCP client has to guess the metadata
			// URL (the documented flow is 401 → read the challenge → fetch the
			// resource metadata → fetch AS metadata → OAuth → retry).
			//
			// error="invalid_token" is emitted only when the request actually
			// carried a Bearer credential that did not resolve (RFC 6750 §3.1);
			// a request with no Authorization header gets the bare challenge
			// telling the client where to authenticate.
			w.Header().Set("WWW-Authenticate", oauthmeta.Challenge(
				oauthmeta.HostOrDefault(r), strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")))
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}
		// Per-request server bound to the authenticated account via context.
		handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
			return s.mcp
		}, &mcp.StreamableHTTPOptions{Stateless: true})

		ctx := WithAuthIdentity(r.Context(), &AuthIdentity{
			AccountID: accountID,
			UserID:    userID,
			Scopes:    scopes,
			APIKey:    apikey,
			Agent:     agent,
		})
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

// authenticate extracts and validates the Bearer credential. It returns the
// resolved account identity. `apikey` is non-nil only for API-key auth (used
// by the revoke_api_key tool); for OAuth-token auth it is nil and scopes are
// the token's GRANTED scopes read off the oauth_tokens row (t.Scope, the OAuth
// vocabulary resolved at authorize time by oauthmeta.GrantedScopes: "read" or
// "readwrite") — NOT a hardcoded readwrite. Only an API-key credential carries
// the API-key scope vocabulary (key.Scopes: readonly/readwrite/admin).
//
// `agent` is the connected MCP client's display name (issue #195): the API
// key's name, or the OAuth client's registered client_name. ok=false on any
// failure. Issue #307: OAuth tokens are user-bound, so userID is returned for
// the OAuth path (API keys have no user — the caller resolves the account
// owner when needed).
func (s *Server) authenticate(r *http.Request) (accountID, userID string, scopes []string, apikey *dbstore.APIKey, agent string, ok bool) {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", "", nil, nil, "", false
	}
	raw := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if raw == "" {
		return "", "", nil, nil, "", false
	}

	// 1. OAuth access token (issue #121) — checked first so an opaque access
	//    token (which is not a key hash) never collides with the API-key table.
	//    OAuth tokens are opaque, stored hashed, and revocable. Issue #307:
	//    the token is bound to the USER, so the identity carries UserID.
	if s.deps.OAuth != nil {
		if t, err := s.deps.OAuth.AccessTokenByHash(r.Context(), auth.HashAPIKey(raw)); err == nil {
			// Fire-and-forget usage stamp.
			go s.deps.OAuth.Touch(context.Background(), auth.HashAPIKey(raw))
			// Resolve the registered client name so mcp.tool_call events can
			// attribute the call to the connected agent (issue #195).
			agent := ""
			if c, cerr := s.deps.OAuth.ClientByID(r.Context(), t.ClientID); cerr == nil {
				agent = c.Name
			}
			return t.AccountID, t.UserID, t.Scope, nil, agent, true
		}
	}

	// 2. API key (existing behaviour, unchanged).
	key, err := s.deps.Keys.LookupByKeyHash(r.Context(), auth.HashAPIKey(raw))
	if err != nil {
		return "", "", nil, nil, "", false
	}
	// Fire-and-forget usage stamp.
	go s.deps.Keys.Touch(context.Background(), key.ID)
	return key.AccountID, "", key.Scopes, key, key.Name, true
}
