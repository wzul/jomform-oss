// What the courier surfaces need to PERSIST: one workspace's connection to a
// provider, the OAuth handshake around it, and the sealed grant itself.
//
// WHY THE INTERFACES LIVE IN THIS PACKAGE. The adapters (lalamove.go,
// easyparcel.go) must refresh a token and read a pickup address without knowing
// whether the bytes are in Postgres, in memory (tests) or anywhere else — and
// internal/dbstore already imports internal/shipping, so the implementation
// cannot live here without a cycle. Declaring the interface on the consumer side
// is what keeps the dependency pointing one way.
//
// NOTHING HERE HOLDS A PLAINTEXT CREDENTIAL. A Connection carries only
// configuration (label, origin, environment, status) and the DATES of a grant,
// never the grant; the sealed blob is read through TokenStore, which is
// implemented by the store and never rendered into a response.
package shipping

import (
	"context"
	"strings"
	"time"
)

// Connection statuses.
const (
	StatusConnected    = "connected"
	StatusDisconnected = "disconnected"
	StatusError        = "error"
)

// Environments a connection can report. EasyParcel's is decided by the account
// the merchant authorized, so it is recorded per connection; Lalamove's comes
// from its key prefix.
const (
	EnvSandbox = "sandbox"
	EnvLive    = "live"
	EnvUnknown = ""
)

// Connection is one workspace's link to one provider.
type Connection struct {
	Provider string
	Status   string
	// Environment is sandbox/live/unknown (see the constants above). It is shown
	// to the merchant so a simulated rate is never mistaken for a real one.
	Environment string
	// AccountLabel is a human label for the connected account (never a token).
	AccountLabel string
	// Origin is the PICKUP address a courier collects from — merchant
	// configuration, not buyer data.
	Origin Address
	// OriginCountry is the ISO alpha-2 country of the pickup address ("MY").
	OriginCountry string
	OriginLat     string
	OriginLng     string
	ServiceType   string
	// HasGrant is true when a sealed grant exists (an OAuth provider that has
	// been authorized). A provider that authenticates per request never has one.
	HasGrant bool
	// GrantExpiresAt / RefreshExpiresAt are the grant's own lifetimes.
	GrantExpiresAt   time.Time
	RefreshExpiresAt time.Time
	// LastError is a CLASS ("expired", "rejected", "refresh_failed"), never a
	// vendor payload.
	LastError      string
	ConnectedAt    time.Time
	DisconnectedAt time.Time
}

// Usable reports whether a courier quote may be attempted for this connection:
// it must be connected, and an OAuth provider must still hold a grant.
func (c Connection) Usable() bool {
	if strings.TrimSpace(c.Provider) == "" || c.Status != StatusConnected {
		return false
	}
	return true
}

// ConnectionStore reads a workspace's courier connections.
type ConnectionStore interface {
	// Connection returns ONE provider's connection; found=false when the
	// workspace has never configured it.
	Connection(ctx context.Context, accountID, provider string) (Connection, bool, error)
	// Connections returns every connection of the workspace (for the dashboard).
	Connections(ctx context.Context, accountID string) ([]Connection, error)
}

// StateRef is a consumed OAuth `state`: the attempt it belonged to.
type StateRef struct {
	AccountID   string
	Provider    string
	RedirectURI string
	CreatedBy   string
}

// CourierStore is everything the connect surface and the adapters persist. One
// implementation satisfies the whole set (dbstore.CourierStore), so a caller
// cannot wire the read half without the write half.
type CourierStore interface {
	ConnectionStore
	// SaveConnection inserts or updates a workspace's connection. It never
	// stores a plaintext grant (that goes through TokenStore, which seals it).
	SaveConnection(ctx context.Context, accountID string, c Connection) error
	// Disconnect marks the connection disconnected and DROPS its grant. The row
	// (and the merchant's pickup address) is kept, so reconnecting is one click.
	Disconnect(ctx context.Context, accountID, provider string) error
	// IssueState creates a single-use OAuth state for a connect attempt and
	// returns it. `ttl` bounds how long it stays valid (an expired link is
	// re-issued, not replayed).
	IssueState(ctx context.Context, accountID, provider, redirectURI, createdBy string) (string, error)
	// ConsumeState claims a state exactly once, within ttl. A second callback
	// carrying the same state gets found=false.
	ConsumeState(ctx context.Context, state string, ttl time.Duration) (StateRef, bool, error)
	// TokenStore is the sealed-grant half.
	TokenStore
}

// TokenStore persists one workspace's OAuth grant for ONE provider. The
// implementation seals the value at rest (dbstore seals it with the deployment's
// encryption key); this interface exists so the adapter can refresh a token
// without knowing how it is stored, and so tests can run with an in-memory store.
type TokenStore interface {
	// Load returns the stored grant; found=false when the workspace has never
	// connected (or has disconnected).
	Load(ctx context.Context, accountID, provider string) (TokenSet, bool, error)
	// Save stores (sealed) the grant, including a refreshed one.
	Save(ctx context.Context, accountID, provider string, t TokenSet) error
	// MarkError records a CLASS on the connection ("expired", "rejected") —
	// never a vendor payload and never a token — so the dashboard can say what
	// happened instead of showing a rate that silently is not live.
	MarkError(ctx context.Context, accountID, provider, class string) error
}
