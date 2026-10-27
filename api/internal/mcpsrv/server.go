// Package mcpsrv exposes JomForm operations as MCP tools so merchants can
// run their store entirely through their own AI agent.
package mcpsrv

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/chip"
	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/currency"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/llm"
	"github.com/wzul/jomform/api/internal/objstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// Deps bundles the stores an MCP server acts on.
type Deps struct {
	Accounts *dbstore.AccountStore
	Products *dbstore.ProductStore
	// Issue #151: product variations (per-variation image/price/stock).
	ProductVariations *dbstore.ProductVariationStore
	Sales             *dbstore.SaleStore
	Subs              *dbstore.SubscriptionStore
	Keys              *dbstore.APIKeyStore
	Forms             *dbstore.FormStore
	Webhooks          *dbstore.WebhookStore
	Workspaces        *dbstore.WorkspaceStore
	// Issue #161: workspace membership + invites (MCP parity tools).
	WorkspaceMembers *dbstore.WorkspaceMemberStore
	// Issue #444: affiliate marketing.
	Affiliates *dbstore.AffiliateStore
	// Issue #161: user lookup for invite-by-email (existing user → add directly).
	// Issue #164: Users enables the set_default_workspace tool (user-level default
	// workspace for the multi-workspace login picker). Nil → the tool errors.
	Users *dbstore.UserStore
	// Issue #92: audit/behaviour log (list_events tool). Nil → tool returns
	// an error rather than panicking.
	Events *dbstore.EventStore
	// Issue #106: SES delivery-event log, for the per-sale email status tool.
	// Nil → the tool returns an empty status.
	EmailDeliveryEvents *dbstore.EmailDeliveryEventStore
	// Issue #574: inbound callback events parked as dead letters. Nil → the
	// admin dead-letter tools error.
	WebhookEvents *dbstore.WebhookEventStore
	// Suppressions is the SES suppression list (issue #56). Nil → the
	// super-admin suppression tools (issue #601) error. Backs the /admin
	// suppression section through the matching REST endpoints.
	Suppressions *dbstore.SuppressionStore
	// Issue #171: response-only form submissions (list_form_submissions tool).
	// Nil → the tool errors.
	Submissions *dbstore.SubmissionStore
	// Issue #193: per-form notification rules (get/set_form_notifications).
	// Nil → the tools error.
	Notifications *dbstore.FormNotificationStore
	// Issue #180: media library (list_media tool). Nil → the tool errors.
	Media *dbstore.MediaStore
	// Shipping zones + rates (get/set_shipping_rates). Nil → the tools error
	// rather than reporting an empty configuration (an empty set list would
	// read as "free shipping everywhere" to a caller).
	Shipping *dbstore.ShippingStore
	// Issue #361/#363: the S3/MinIO object store backing the media bucket,
	// shared with the dashboard (internal/objstore). Wire it so upload_media
	// can push image + code assets tenant-scoped. Nil → the tool errors.
	Store *objstore.ObjectStore
	// Issue #364/#363: background asset-scan queue (same Valkey queue as email
	// #199 / spam judge #245). Code-asset uploads enqueue an asset_scan job
	// here; a nil queue (dev, no Valkey) keeps the asset 'pending'/unserved
	// (fail-closed).
	AssetQueue *emailqueue.Queue
	// Issue #121: OAuth 2.1 store. When wired, /mcp accepts either a Bearer
	// API key OR an OAuth access token (both resolve to an account). Nil →
	// API-key auth only.
	OAuth *dbstore.OAuthStore
	// Issue #185: BNM exchange-rate cache (Valkey). Nil → the
	// get_currency_rates tool returns an empty list.
	Currency *currency.Store
	// Issue #396: custom-domain store (list/add/verify/remove/auto-CNAME).
	// Nil → the domain tools error.
	Domains *dbstore.DomainStore
	// Issue #396: platform system settings (get/set). Nil → the tools error.
	SystemSettings *dbstore.SystemSettingStore
	// Issue #396: passkey credentials (list/delete). Nil → the tools error.
	Passkeys *dbstore.PasskeyStore
	// Issue #396: session store backing the 2FA pending-TOTP flow. Nil → the
	// 2FA setup/enable tools error.
	Sessions *auth.Sessions
	// DecryptKey (ENCRYPTION_KEY hex bytes) enables per-form CHIP override tools.
	DecryptKey []byte
	// Issue #184: LLM client for auto-generating legal pages. Nil → the
	// generate_legal_page tool errors.
	LLM interface {
		GenerateLegalPage(ctx context.Context, pageType string, d llm.BusinessDetails) (string, error)
		// Issue #256: AI form builder — draft a form config from a prompt.
		GenerateFormConfig(ctx context.Context, prompt string) (string, error)
	}
	// SuperAdmin (issue #385): reports whether an email is on the site
	// super-admin allowlist (SUPER_ADMIN_EMAILS). Nil → no super-admins.
	// Super-admin unlocks platform-wide monitoring ONLY on explicitly gated
	// admin tools/pages (#386/#387), never implicitly on normal tools.
	SuperAdmin func(email string) bool
	// Feedback is the in-app feedback thread store. Nil → the feedback tools
	// error. It is user-scoped, NOT account-scoped: a thread belongs to the
	// person who filed it and carries no account_id (migration 000063), so
	// these tools never resolve a workspace scope.
	Feedback *dbstore.FeedbackThreadStore
	// NotifyFeedback is called (best-effort) after a feedback thread is filed
	// through an MCP tool, so the platform team is notified exactly as it is
	// when the same thread is filed on the /feedback page. Nil → no
	// notification (the thread is still filed and readable).
	NotifyFeedback func(th *dbstore.FeedbackThread, body string)
	// RetryFeedbackNotify re-attempts the notification that last failed for a
	// thread and reports the outcome (queued|sent|failed|none|skipped). It is
	// the REST handler's own release path — wired here so the MCP retry tool
	// calls the same logic instead of re-implementing what to re-send and to
	// whom.
	// Nil → the retry_feedback_notify tool errors.
	RetryFeedbackNotify func(ctx context.Context, threadID string) (string, error)
	// CFOAuthConfigured reports whether THIS DEPLOYMENT can offer the
	// Cloudflare OAuth (auto-CNAME) flow — the same fact GET /v1/rest/cf/status
	// publishes as `configured`, because JF_CF_OAUTH_CLIENT_ID is set
	// (httpapi/cf_oauth.go: cfOAuthConfigured). The get_cloudflare_status tool
	// publishes that same word, so it must be answered by that same rule:
	// wired from httpapi.CFOAuthConfigured (an mcpsrv → httpapi import would be
	// a cycle), never by reading the env a second time here.
	// Nil (bare-mode tests, nothing wired) → configured:false.
	CFOAuthConfigured func() bool
}

// Server wraps the MCP *Server with JomForm deps.
type Server struct {
	mcp    *mcp.Server
	decKey []byte // ENCRYPTION_KEY bytes
	pay    *payments.Service
	deps   *Deps
	chip   *chip.Client
	// startedAt records when the server was built, for the admin_system_health
	// uptime metric (issue #386).
	startedAt time.Time
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
	// UserID is the authenticated user's id (issue #307). It is set for OAuth
	// access tokens (the token is bound to the user) and empty for API-key
	// auth, where the principal is the account and its owner is resolved from
	// the account row when needed.
	UserID string
	Scopes []string
	// APIKey is non-nil only when authenticated via a Bearer API key. It is
	// needed by the revoke_api_key tool to refuse revoking the live key.
	APIKey *dbstore.APIKey
	// Agent is the human-readable name of the connected MCP client (issue
	// #195). For OAuth clients it is the registered client_name; for API-key
	// auth it is the key's name. Empty when unknown. Recorded on every
	// mcp.tool_call event so the dashboard can show which agent made a call.
	Agent string
}

// WithAPIKey stores an authenticated API key's identity in ctx.
func WithAPIKey(ctx context.Context, key *dbstore.APIKey) context.Context {
	return WithAuthIdentity(ctx, &AuthIdentity{
		AccountID: key.AccountID,
		Scopes:    key.Scopes,
		APIKey:    key,
		Agent:     key.Name,
	})
}

// WithOAuthToken stores an OAuth access token's identity in ctx, carrying the
// token's GRANTED scopes — never a hardcoded readwrite.
//
// Until this change the helper took no scope and hardcoded ["readwrite"], which
// mirrored the old single-scope flow. Now that an access token can hold `read`
// alone, a helper that silently widened the grant would be the exact "shown but
// not enforced" trap the scope work exists to close. Callers pass t.Scope, the
// value authenticate() read from the token row.
func WithOAuthToken(ctx context.Context, accountID, agent string, scopes []string) context.Context {
	return WithAuthIdentity(ctx, &AuthIdentity{
		AccountID: accountID,
		Scopes:    scopes,
		Agent:     agent,
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
