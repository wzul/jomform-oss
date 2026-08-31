// Package mcpsrv exposes JomForm operations as MCP tools so merchants can
// run their store entirely through their own AI agent.
package mcpsrv

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// Deps bundles the stores an MCP server acts on.
type Deps struct {
	Accounts *dbstore.AccountStore
	Products *dbstore.ProductStore
	Sales    *dbstore.SaleStore
	Subs     *dbstore.SubscriptionStore
	Keys     *dbstore.APIKeyStore
	Forms    *dbstore.FormStore
	// DecryptKey (ENCRYPTION_KEY hex bytes) enables per-form CHIP override tools.
	DecryptKey []byte
}

// Server wraps the MCP *Server with JomForm deps.
type Server struct {
	mcp    *mcp.Server
	decKey []byte // ENCRYPTION_KEY bytes
	pay    *payments.Service
	deps   *Deps
	chip   *chip.Client
}

// decrypt opens an account's encrypted CHIP secret key.
func (s *Server) decrypt(sealedHex string) (string, error) {
	b, err := cryptoutil.Decrypt(s.decKey, sealedHex)
	return string(b), err
}

// accountFromKey resolves the account an API key belongs to. The transport
// layer has already authenticated the key; context carries its hash.
type ctxKey string

const apiKeyCtxKey ctxKey = "jomform_api_key"

// WithAPIKey stores the authenticated API key in ctx.
func WithAPIKey(ctx context.Context, key *dbstore.APIKey) context.Context {
	return context.WithValue(ctx, apiKeyCtxKey, key)
}

// KeyFromContext returns the authenticated API key (may be nil).
func KeyFromContext(ctx context.Context) *dbstore.APIKey {
	if v, ok := ctx.Value(apiKeyCtxKey).(*dbstore.APIKey); ok {
		return v
	}
	return nil
}
