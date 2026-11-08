// Connect/disconnect RULES for the courier providers, shared by every surface.
//
// WHY THIS IS ITS OWN FILE. Two surfaces drive a connect — the dashboard's REST
// edge (/v1/rest/shipping/couriers/.../connect) and the MCP tools
// (connect_shipping_courier) — and mcpsrv must not import httpapi (httpapi
// imports mcpsrv for the error-code catalogue). Putting the RULES here means both
// ask the same questions, so a merchant who connects through an agent gets
// exactly what the dashboard would have done: the same origin validation, the
// same refusal when a credential is missing, and the same single-use OAuth link.
//
// THE TWO SHAPES, AGAIN, BECAUSE THEY ARE EASY TO CONFLATE:
//
//   - a SERVER-TO-SERVER provider (Lalamove) is connected by naming WHERE THE
//     PARCEL SHIPS FROM; its HMAC key pair lives in a runtime env file and never
//     travels through the dashboard, an API call or a tool argument.
//   - an OAUTH provider (EasyParcel) is connected by the MERCHANT completing a
//     consent in a browser. This code issues a LINK (with a single-use state) and
//     nothing else: it never opens a browser, never asks for a password and never
//     sees one. An expired link is re-issued, not replayed.
package shipping

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Errors a connect can refuse with. Each is a distinct remedy, so the caller can
// say what to DO rather than "connect failed".
var (
	// ErrConnectNoCredentials: the deployment has no usable credential for this
	// provider (production Lalamove keys are Wan's to obtain; EasyParcel's OAuth
	// app may be unconfigured).
	ErrConnectNoCredentials = errors.New("this provider's credential is not configured on the server")
	// ErrConnectOriginRequired: a point-based provider (Lalamove) needs the
	// PICKUP point; without it every quote would fall back, so connecting without
	// one would create a connection that can never be used.
	ErrConnectOriginRequired = errors.New("this provider needs the pickup location's latitude/longitude")
	// ErrConnectNoRedirect: an OAuth provider has no registered callback URL for
	// this deployment (the redirect_uri must match the Developer Hub exactly).
	ErrConnectNoRedirect = errors.New("this provider's redirect_uri is not registered")
	// ErrConnectUnavailable: the store cannot persist the connection.
	ErrConnectUnavailable = errors.New("the shipping provider connection store is unavailable")
)

// ConnectRequest is what a connect supplies: WHERE THE PARCEL SHIPS FROM plus the
// provider's own service choice. No credential is ever part of it.
type ConnectRequest struct {
	OriginPostcode string
	OriginState    string
	OriginCountry  string
	OriginLat      string
	OriginLng      string
	ServiceType    string
	// Environment is what the merchant IS authorizing ("sandbox" for a demo
	// account, "live" for a real one). An OAuth provider decides the environment
	// by the authorized account, so recording the intent is what makes a
	// simulated rate visible as simulated.
	Environment string
}

// ConnectOutcome is the result: either the provider is quotable now, or a HUMAN
// must open AuthorizeURL (the only case a browser is involved).
type ConnectOutcome struct {
	Provider  string
	Connected bool
	// AuthorizeURL is set only for a provider whose connect needs the merchant's
	// own consent.
	AuthorizeURL string
	// RedirectURI is the exact value that must be registered with the provider
	// for that link to come back.
	RedirectURI string
	// StateExpiresAt is when the link stops being valid.
	StateExpiresAt time.Time
}

// Environment returns the stored environment vocabulary for a merchant's words.
func ConnectEnvironment(v string) string {
	switch Normalize(v) {
	case "live", "prod", "production", "real":
		return EnvLive
	case "sandbox", "demo", "test", "testing":
		return EnvSandbox
	default:
		return EnvUnknown
	}
}

// OriginFromRequest builds the Connection's origin fields from a connect request.
// The service code is upper-cased HERE (not in a store) so every persistence
// implementation records the same value.
func OriginFromRequest(in ConnectRequest) (Address, string, string, string, string) {
	return Address{Postcode: strings.TrimSpace(in.OriginPostcode), State: strings.TrimSpace(in.OriginState)},
		strings.TrimSpace(in.OriginCountry), strings.TrimSpace(in.OriginLat),
		strings.TrimSpace(in.OriginLng), strings.ToUpper(strings.TrimSpace(in.ServiceType))
}

// Connector performs a connect against the store + credentials. It is the whole
// per-provider policy in one place.
type Connector struct {
	// Store persists the connection (and, later, the OAuth grant).
	Store CourierStore
	// Creds is the runtime credential set. It is read for PRESENCE only: no value
	// is ever copied into a response or a log.
	Creds CourierCredentials
	// Registry is used to refuse an unknown provider name before anything is
	// written.
	Registry *Registry
	// Env is the deployment environment ("prod" picks the production redirect
	// URI).
	Env string
	// Now is injectable for tests; nil uses time.Now.
	Now func() time.Time
	// StateTTL bounds a consent link's life. Zero uses DefaultStateTTL.
	StateTTL time.Duration
}

// DefaultStateTTL is how long an issued consent link stays valid. The merchant
// opens it at a time of his choosing (the consent handoff is his action), and an
// expired link is simply re-issued.
const DefaultStateTTL = 30 * time.Minute

func (c *Connector) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// stateTTL returns the configured TTL or the default.
func (c *Connector) stateTTL() time.Duration {
	if c.StateTTL > 0 {
		return c.StateTTL
	}
	return DefaultStateTTL
}

// Connect connects ONE provider for a workspace.
func (c *Connector) Connect(ctx context.Context, accountID, provider string, in ConnectRequest) (ConnectOutcome, error) {
	if c == nil || c.Store == nil {
		return ConnectOutcome{}, ErrConnectUnavailable
	}
	name := Normalize(provider)
	if c.Registry != nil {
		if _, known := c.Registry.Get(name); !known {
			return ConnectOutcome{}, ErrProviderUnknown
		}
	}
	origin, country, lat, lng, service := OriginFromRequest(in)
	conn := Connection{
		Provider:      name,
		Status:        StatusDisconnected,
		Environment:   ConnectEnvironment(in.Environment),
		Origin:        origin,
		OriginCountry: firstNonBlank(country, "MY"),
		OriginLat:     lat,
		OriginLng:     lng,
		ServiceType:   service,
	}
	switch name {
	case ProviderLalamove:
		if !c.Creds.Lalamove.Configured() {
			return ConnectOutcome{}, ErrConnectNoCredentials
		}
		if lat == "" || lng == "" {
			return ConnectOutcome{}, ErrConnectOriginRequired
		}
		// The environment of a key pair is its prefix (pk_test_/pk_prod_), which
		// the adapter derives; the merchant cannot choose it.
		conn.Environment = c.Creds.Lalamove.Environment()
		conn.AccountLabel = strings.ToUpper(c.Creds.Lalamove.Market)
		conn.Status = StatusConnected
		if err := c.Store.SaveConnection(ctx, accountID, conn); err != nil {
			return ConnectOutcome{}, err
		}
		return ConnectOutcome{Provider: name, Connected: true}, nil
	case ProviderEasyParcel:
		if !c.Creds.EasyParcel.Configured() {
			return ConnectOutcome{}, ErrConnectNoCredentials
		}
		redirectURI := c.Creds.EasyParcel.RedirectURI(c.Env)
		if redirectURI == "" {
			return ConnectOutcome{}, ErrConnectNoRedirect
		}
		state, err := c.Store.IssueState(ctx, accountID, name, redirectURI, "")
		if err != nil {
			return ConnectOutcome{}, err
		}
		link, err := EasyParcelAuthorizeURL(c.Creds.EasyParcel, state, redirectURI)
		if err != nil {
			return ConnectOutcome{}, err
		}
		// The pickup address is saved BEFORE consent: it is configuration the
		// merchant just gave, and the callback then only attaches the grant.
		if err := c.Store.SaveConnection(ctx, accountID, conn); err != nil {
			return ConnectOutcome{}, err
		}
		return ConnectOutcome{
			Provider:       name,
			AuthorizeURL:   link,
			RedirectURI:    redirectURI,
			StateExpiresAt: c.now().UTC().Add(c.stateTTL()),
		}, nil
	default:
		return ConnectOutcome{}, ErrProviderUnknown
	}
}

// Disconnect drops a workspace's connection (and its sealed grant), keeping the
// pickup address so reconnecting is one click.
func (c *Connector) Disconnect(ctx context.Context, accountID, provider string) error {
	if c == nil || c.Store == nil {
		return ErrConnectUnavailable
	}
	name := Normalize(provider)
	if c.Registry != nil {
		if _, known := c.Registry.Get(name); !known {
			return ErrProviderUnknown
		}
	}
	return c.Store.Disconnect(ctx, accountID, name)
}

// ProviderState is one provider's status for one workspace: what the deployment
// can do, what the workspace has configured, and — the actionable bit — what is
// still MISSING for a real quote to happen.
type ProviderState struct {
	// Info is the build's knowledge of the provider (credentials present, whether
	// a browser is needed, whether it is point-based).
	Info ProviderStatus
	// Connection is the workspace's row, when it has one.
	Connection *Connection
	// CredentialsSource is "file" / "env" / "file+env" / "none" — where the value
	// was read from (never the value).
	CredentialsSource string
	// Ready is true when this provider can quote for this workspace right now.
	Ready bool
	// Missing names what is still needed, in the order the merchant should fix
	// it: "credentials", "connect", "grant", "origin".
	Missing []string
}

// CourierStates reports every provider this build knows for one workspace.
// conns may be nil (nothing configured). It is the single source of truth for
// both the dashboard's status rows and the MCP tool, so the two cannot disagree
// about whether a provider is ready.
func CourierStates(reg *Registry, conns []Connection, creds CourierCredentials) []ProviderState {
	byProvider := map[string]Connection{}
	for _, c := range conns {
		byProvider[c.Provider] = c
	}
	infos := ProviderStatuses(reg)
	out := make([]ProviderState, 0, len(infos))
	for _, info := range infos {
		st := ProviderState{Info: info}
		switch info.Name {
		case ProviderLalamove:
			st.CredentialsSource = creds.LalamoveSource
		case ProviderEasyParcel:
			st.CredentialsSource = creds.EasyParcelSource
		}
		if conn, ok := byProvider[info.Name]; ok {
			c := conn
			st.Connection = &c
		}
		st.Missing = courierMissingFor(st)
		st.Ready = len(st.Missing) == 0
		out = append(out, st)
	}
	return out
}

// courierMissingFor names what is still needed for one provider to be quotable.
// The ORDER is the order a merchant should fix things in, and the words are the
// keys the dashboard translates.
func courierMissingFor(st ProviderState) []string {
	missing := []string{}
	if !st.Info.CredentialsPresent {
		missing = append(missing, "credentials")
	}
	conn := st.Connection
	if conn == nil || !conn.Usable() {
		missing = append(missing, "connect")
		return missing
	}
	if st.Info.NeedsBrowser && !conn.HasGrant {
		// An OAuth provider is quoted with the merchant's OWN grant: a connection
		// without one can price nothing.
		missing = append(missing, "grant")
	}
	if st.Info.PointBased && (strings.TrimSpace(conn.OriginLat) == "" || strings.TrimSpace(conn.OriginLng) == "") {
		// A point-based courier needs the pickup point; without it every quote
		// would be refused by the adapter.
		missing = append(missing, "origin")
	}
	return missing
}

// DescribeProviderState renders a state as one bounded sentence (used by the MCP
// tool and by logs). It contains no credential and no vendor payload.
func DescribeProviderState(st ProviderState) string {
	ready := "ready"
	if !st.Ready {
		ready = "not ready: " + strings.Join(st.Missing, ", ")
	}
	env := st.Info.Environment
	if env == "" {
		env = "unknown environment"
	}
	return fmt.Sprintf("%s (%s, %s)", st.Info.Name, env, ready)
}
