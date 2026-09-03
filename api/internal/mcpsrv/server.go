// Package mcpsrv exposes JomForm operations as MCP tools so merchants can
// run their store entirely through their own AI agent.
package mcpsrv

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/currency"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/llm"
	"github.com/wzul/jomform/api/internal/payments"
)

// Deps bundles the stores an MCP server acts on.
type Deps struct {
	Accounts *dbstore.AccountStore
	Products *dbstore.ProductStore
	// Issue #151: product variations (per-variation image/price/stock).
	ProductVariations *dbstore.ProductVariationStore
	Sales    *dbstore.SaleStore
	Subs     *dbstore.SubscriptionStore
	Keys     *dbstore.APIKeyStore
	Forms    *dbstore.FormStore
	Webhooks *dbstore.WebhookStore
	Spaces   *dbstore.SpaceStore
	// Issue #161: space membership + invites (MCP parity tools).
	SpaceMembers *dbstore.SpaceMemberStore
	// Issue #161: user lookup for invite-by-email (existing user → add directly).
	// Issue #164: Users enables the set_default_space tool (user-level default
	// space for the multi-space login picker). Nil → the tool errors.
	Users *dbstore.UserStore
	// Issue #92: audit/behaviour log (list_events tool). Nil → tool returns
	// an error rather than panicking.
	Events *dbstore.EventStore
	// Issue #106: SES delivery-event log, for the per-sale email status tool.
	// Nil → the tool returns an empty status.
	EmailDeliveryEvents *dbstore.EmailDeliveryEventStore
	// Issue #171: response-only form submissions (list_form_submissions tool).
	// Nil → the tool errors.
	Submissions *dbstore.SubmissionStore
	// Issue #180: media library (list_media tool). Nil → the tool errors.
	Media *dbstore.MediaStore
	// Issue #121: OAuth 2.1 store. When wired, /mcp accepts either a Bearer
	// API key OR an OAuth access token (both resolve to an account). Nil →
	// API-key auth only.
	OAuth *dbstore.OAuthStore
	// Issue #185: BNM exchange-rate cache (Valkey). Nil → the
	// get_currency_rates tool returns an empty list.
	Currency *currency.Store
	// DecryptKey (ENCRYPTION_KEY hex bytes) enables per-form CHIP override tools.
	DecryptKey []byte
	// Issue #184: LLM client for auto-generating legal pages. Nil → the
	// generate_legal_page tool errors.
	LLM interface {
		GenerateLegalPage(ctx context.Context, pageType string, d llm.BusinessDetails) (string, error)
	}
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

// AuthIdentity is the authenticated principal carried on the request context.
// It backs BOTH authentication paths (issue #121): a Bearer API key
// (api_keys table) or an OAuth access token (oauth_tokens table). Either way
// the account is resolved; scopes gate write tools.
type AuthIdentity struct {
	AccountID string
	Scopes    []string
	// APIKey is non-nil only when authenticated via a Bearer API key. It is
	// needed by the revoke_api_key tool to refuse revoking the live key.
	APIKey *dbstore.APIKey
}

// WithAPIKey stores an authenticated API key's identity in ctx.
func WithAPIKey(ctx context.Context, key *dbstore.APIKey) context.Context {
	return WithAuthIdentity(ctx, &AuthIdentity{
		AccountID: key.AccountID,
		Scopes:    key.Scopes,
		APIKey:    key,
	})
}

// WithOAuthToken stores an OAuth access token's identity in ctx. OAuth tokens
// grant read+write (readwrite) scope, mirroring an admin API key.
func WithOAuthToken(ctx context.Context, accountID string) context.Context {
	return WithAuthIdentity(ctx, &AuthIdentity{
		AccountID: accountID,
		Scopes:    []string{"readwrite"},
	})
}

// WithAuthIdentity stores the resolved principal in ctx.
func WithAuthIdentity(ctx context.Context, id *AuthIdentity) context.Context {
	return context.WithValue(ctx, apiKeyCtxKey, id)
}

// IdentityFromContext returns the authenticated principal (may be nil).
func IdentityFromContext(ctx context.Context) *AuthIdentity {
	if v, ok := ctx.Value(apiKeyCtxKey).(*AuthIdentity); ok {
		return v
	}
	return nil
}

// KeyFromContext returns the authenticated API key, or nil when the request
// was authenticated via an OAuth access token (or not at all).
func KeyFromContext(ctx context.Context) *dbstore.APIKey {
	if id := IdentityFromContext(ctx); id != nil {
		return id.APIKey
	}
	return nil
}
