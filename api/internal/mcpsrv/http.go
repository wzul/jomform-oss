// MCP HTTP transport wiring: streamable HTTP + API-key auth.
package mcpsrv

import (
	"context"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// HTTPHandler returns an http.Handler serving MCP over Streamable HTTP at
// any mount point (typically /mcp). Auth: Authorization: Bearer <api key>;
// the per-request server instance carries the authenticated key's identity.
func (s *Server) HTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, ok := s.authenticate(r)
		if !ok {
			http.Error(w, `{"error":"api key tidak sah"}`, http.StatusUnauthorized)
			return
		}
		// Per-request server bound to the authenticated account via context.
		handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
			return s.mcp
		}, &mcp.StreamableHTTPOptions{Stateless: true})

		ctx := WithAPIKey(r.Context(), key)
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

// authenticate extracts and validates the Bearer API key.
func (s *Server) authenticate(r *http.Request) (*dbstore.APIKey, bool) {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return nil, false
	}
	raw := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if raw == "" {
		return nil, false
	}
	key, err := s.deps.Keys.LookupByKeyHash(r.Context(), auth.HashAPIKey(raw))
	if err != nil {
		return nil, false
	}
	// Fire-and-forget usage stamp.
	go s.deps.Keys.Touch(context.Background(), key.ID)
	return key, true
}
