// The courier WIRING BUNDLE: everything a surface (the dashboard's REST edge, the
// public submit/callback edges, the MCP tools) needs to quote, connect and report.
//
// WHY IT LIVES HERE. It is defined in internal/shipping and not in httpapi because
// mcpsrv must not import httpapi (httpapi imports mcpsrv for the error-code
// catalogue), while the two surfaces must use the SAME rules. One type, one
// registry, one store, one credential set — a tool cannot report a provider as
// ready while the dashboard reports it as missing a grant.
//
// IT IS NIL-SAFE BY DESIGN. A deployment that wires nothing (a bare-mode test, a
// build with no courier credentials) gets a bundle whose every accessor answers
// "nothing here": the submit path falls back to the workspace's zone table, the
// connect surface reports the providers as not configured, and nothing panics.
package shipping

import (
	"context"
	"strings"
)

// Couriers bundles the phase-2 courier wiring.
type Couriers struct {
	// Registry holds the provider adapters this deployment could build. A
	// deployment with no credentials still has a registry whose providers report
	// Available() == false, which is the "not configured yet" state.
	Registry *Registry
	// Store reads a workspace's connection (pickup point + status) and persists
	// the OAuth grant. Nil means every courier block falls back to its zone rate.
	Store CourierStore
	// Creds is the runtime credential set (never rendered; see credentials.go).
	Creds CourierCredentials
	// Env is the deployment environment, which picks the registered EasyParcel
	// redirect URI ("prod" uses the production one).
	Env string
	// Quotes reuses a live quotation for an identical submission (the
	// double-submission guard). Nil disables the reuse.
	Quotes *QuoteCache
}

// Adapter returns the provider registry (nil when unwired).
func (c *Couriers) Adapter() *Registry {
	if c == nil {
		return nil
	}
	return c.Registry
}

// Quotes returns the quotation cache (nil when unwired — every access is a no-op).
func (c *Couriers) QuoteCache() *QuoteCache {
	if c == nil {
		return nil
	}
	return c.Quotes
}

// Policy returns the shared connect policy for this deployment.
func (c *Couriers) Policy() *Connector {
	if c == nil {
		return &Connector{}
	}
	return &Connector{Store: c.Store, Creds: c.Creds, Registry: c.Registry, Env: c.Env}
}

// Connections reads a workspace's connections (nil store → none).
func (c *Couriers) Connections(ctx context.Context, accountID string) []Connection {
	if c == nil || c.Store == nil || strings.TrimSpace(accountID) == "" {
		return nil
	}
	list, err := c.Store.Connections(ctx, accountID)
	if err != nil {
		return nil
	}
	return list
}

// Connected reports whether a provider may be quoted for this workspace, and
// returns the connection that says where the parcel ships from.
//
// A missing store reads as "not connected" — the FALLBACK state — rather than an
// error: a form must keep taking orders when the dashboard's courier
// configuration is unavailable, and the zone table charges the fee.
func (c *Couriers) Connected(ctx context.Context, accountID, provider string) (Connection, bool) {
	if c == nil || c.Store == nil || strings.TrimSpace(provider) == "" {
		return Connection{}, false
	}
	conn, found, err := c.Store.Connection(ctx, accountID, provider)
	if err != nil || !found || !conn.Usable() {
		return Connection{}, false
	}
	// An OAuth provider with no grant yet is connected in name only: the adapter
	// would report it unavailable, and checking here keeps the recorded fallback
	// reason accurate ("not connected" rather than "provider fault").
	if conn.Provider == ProviderEasyParcel && !conn.HasGrant {
		return Connection{}, false
	}
	return conn, true
}

// States reports every provider this build knows for one workspace, using the
// SHARED readiness rules.
func (c *Couriers) States(ctx context.Context, accountID string) []ProviderState {
	creds := CourierCredentials{}
	reg := (*Registry)(nil)
	if c != nil {
		creds = c.Creds
		reg = c.Registry
	}
	return CourierStates(reg, c.Connections(ctx, accountID), creds)
}
