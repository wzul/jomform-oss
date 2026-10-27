// MCP parity tools for the phase-2 courier layer.
//
// The dashboard's Form Settings -> Shipping tab (courier section) lists each
// provider, connects one, and disconnects it through
// /v1/rest/shipping/couriers/...; these three tools are the same surface over MCP,
// so a connected agent can set a merchant up without a browser. Parity is a rule in
// AGENTS.md and the checklist parsed by mcp_parity_test.go names all three.
//
// THEY SHARE THE RULES WITH THE DASHBOARD. Connect/disconnect go through
// shipping.Connector (internal/shipping/connect.go) and readiness through
// shipping.CourierStates, so an agent cannot connect something the dashboard would
// have refused, and a provider cannot be "ready" in one surface and not the other.
//
// ONE THING AN AGENT MUST NOT DO. EasyParcel's connect needs a HUMAN: the tool
// hands back the authorize LINK for the merchant to open on his own device, and it
// never sees, asks for or types a password. The link expires; asking for a fresh
// one is normal.
package mcpsrv

import (
	"context"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/shipping"
)

// errCouriersUnavailable: the courier wiring is absent (a bare-mode deployment).
// Refusing loudly is deliberate — a tool that answered "no providers configured"
// would read as "this deployment has no couriers", which is a different fact.
var errCouriersUnavailable = errCourierErr("sambungan pembekal penghantaran tidak tersedia")

// errCourierErr builds a plain error (small helper so the var block above reads
// as a sentence).
func errCourierErr(msg string) error { return &courierErr{msg: msg} }

type courierErr struct{ msg string }

func (e *courierErr) Error() string { return e.msg }

// courierProviderOut is one provider's status for the workspace.
type courierProviderOut struct {
	Provider string `json:"provider"`
	// CredentialsPresent is whether the DEPLOYMENT holds a usable credential for
	// this provider (never the value).
	CredentialsPresent bool   `json:"credentials_present"`
	CredentialsSource  string `json:"credentials_source,omitempty"`
	CredentialsHint    string `json:"credentials_hint,omitempty"`
	// NeedsBrowser is true when connecting requires the MERCHANT to open a link
	// and consent (EasyParcel). An agent can issue the link but must not complete
	// it.
	NeedsBrowser bool `json:"needs_browser"`
	// PointBased is true when the provider needs coordinates (Lalamove): a pickup
	// point for the workspace and a point on each zone.
	PointBased bool   `json:"point_based"`
	DocURL     string `json:"doc_url,omitempty"`
	// Environment is sandbox/live/unknown. A sandbox rate is SIMULATED.
	Environment string `json:"environment,omitempty"`
	Connected   bool   `json:"connected"`
	Status      string `json:"status,omitempty"`
	// Ready is whether this workspace can be QUOTED by this provider now.
	Ready bool `json:"ready"`
	// Missing names what is still needed: credentials | connect | grant | origin.
	Missing []string `json:"missing,omitempty"`
	// Origin is where the parcel ships from (merchant configuration).
	OriginPostcode string `json:"origin_postcode,omitempty"`
	OriginState    string `json:"origin_state,omitempty"`
	OriginCountry  string `json:"origin_country,omitempty"`
	OriginLat      string `json:"origin_lat,omitempty"`
	OriginLng      string `json:"origin_lng,omitempty"`
	ServiceType    string `json:"service_type,omitempty"`
	AccountLabel   string `json:"account_label,omitempty"`
	HasGrant       bool   `json:"has_grant,omitempty"`
	LastError      string `json:"last_error,omitempty"`
	ConnectedAt    string `json:"connected_at,omitempty"`
}

// shippingCouriersOut is the read shape. The SDK derives the tool's outputSchema
// from the handler's Out type, which must be an OBJECT (output_schema_test.go
// enforces that).
type shippingCouriersOut struct {
	AccountID string               `json:"account_id"`
	Providers []courierProviderOut `json:"providers"`
	// RedirectURI is the callback URL this deployment must have registered with
	// EasyParcel for a connect to come back ("" when none is configured).
	RedirectURI string `json:"redirect_uri,omitempty"`
	// CallbackPath is the public path merchants register (the web app proxies
	// /api/* to the API's /v1/*).
	CallbackPath string `json:"callback_path"`
}

// getShippingCouriersIn is the read input: the per-call scope is carried by the
// reserved arguments (workspace_id/account_id), handled by the call-scope plumbing.
type getShippingCouriersIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"ID workspace (dari list_workspaces). Kosong = akaun anda sendiri"`
}

// getShippingCouriers reads every provider's status for one workspace.
func (s *Server) getShippingCouriers(
	ctx context.Context, _ *mcp.CallToolRequest, in getShippingCouriersIn,
) (*mcp.CallToolResult, *shippingCouriersOut, error) {
	if s.deps.Couriers == nil {
		return nil, nil, errCouriersUnavailable
	}
	accountID, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	return nil, couriersOut(accountID, s.deps.Couriers.States(ctx, accountID),
		s.deps.Couriers.Creds.EasyParcel.RedirectURI(s.deps.Couriers.Env)), nil
}

// connectShippingCourierIn is the connect input. It carries NO credential: an
// HMAC key pair lives in the deployment's runtime env file, and an OAuth grant
// comes from the MERCHANT's own consent.
type connectShippingCourierIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"ID workspace (dari list_workspaces). Kosong = akaun anda sendiri"`
	Provider    string `json:"provider" jsonschema:"Pembekal: lalamove atau easyparcel"`
	// OriginPostcode / OriginState are required by EasyParcel, which needs the
	// ISO 3166-2 subdivision code derived from the state name.
	OriginPostcode string `json:"origin_postcode,omitempty" jsonschema:"Poskod tempat barang diambil (wajib untuk easyparcel)"`
	OriginState    string `json:"origin_state,omitempty" jsonschema:"Negeri tempat barang diambil, cth Selangor / Penang (wajib untuk easyparcel)"`
	OriginCountry  string `json:"origin_country,omitempty" jsonschema:"Kod negara 2 huruf (lalai MY)"`
	// OriginLat / OriginLng are required by a point-based provider (Lalamove).
	OriginLat string `json:"origin_lat,omitempty" jsonschema:"Latitud tempat ambil (wajib untuk lalamove)"`
	OriginLng string `json:"origin_lng,omitempty" jsonschema:"Longitud tempat ambil (wajib untuk lalamove)"`
	// ServiceType is the provider's own service code (Lalamove: MOTORCYCLE/CAR/VAN).
	ServiceType string `json:"service_type,omitempty" jsonschema:"Kod servis pembekal (lalamove: MOTORCYCLE, CAR, VAN). Kosong = lalai pembekal"`
	// Environment is what the merchant IS authorizing: a DEMO account (sandbox) or
	// a live one. Say sandbox when demoing, because a sandbox rate is simulated.
	Environment string `json:"environment,omitempty" jsonschema:"sandbox (akaun demo) atau live. Untuk easyparcel, ini bergantung pada akaun yang dibenarkan"`
}

// shippingCourierConnectOut is the connect answer.
type shippingCourierConnectOut struct {
	Provider  string `json:"provider"`
	Connected bool   `json:"connected"`
	// AuthorizeURL is the link the MERCHANT must open when the provider needs his
	// consent (EasyParcel). An agent may hand it over; it must NEVER open it,
	// type a password or accept one.
	AuthorizeURL string `json:"authorize_url,omitempty"`
	// RedirectURI is the exact value that must be registered with the provider.
	RedirectURI string `json:"redirect_uri,omitempty"`
	// StateExpiresAt is when that link stops working (a fresh one is issued by
	// calling this tool again — an expired link is not an error).
	StateExpiresAt string `json:"state_expires_at,omitempty"`
	// HumanStep says, in one sentence, what the MERCHANT has to do. Empty when
	// nothing is needed from him.
	HumanStep string             `json:"human_step,omitempty"`
	Status    courierProviderOut `json:"status"`
}

// connectShippingCourier connects one provider for a workspace, or returns the
// authorize link a human must open.
func (s *Server) connectShippingCourier(
	ctx context.Context, _ *mcp.CallToolRequest, in connectShippingCourierIn,
) (*mcp.CallToolResult, *shippingCourierConnectOut, error) {
	if s.deps.Couriers == nil {
		return nil, nil, errCouriersUnavailable
	}
	// Issue #645: write gate = API-key write scope AND manage-or-admin role.
	if _, err := s.requireWorkspaceWriteRole(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	accountID, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	outcome, err := s.deps.Couriers.Policy().Connect(ctx, accountID, in.Provider, shipping.ConnectRequest{
		OriginPostcode: in.OriginPostcode,
		OriginState:    in.OriginState,
		OriginCountry:  in.OriginCountry,
		OriginLat:      in.OriginLat,
		OriginLng:      in.OriginLng,
		ServiceType:    in.ServiceType,
		Environment:    in.Environment,
	})
	if err != nil {
		return nil, nil, err
	}
	out := &shippingCourierConnectOut{
		Provider:     outcome.Provider,
		Connected:    outcome.Connected,
		AuthorizeURL: outcome.AuthorizeURL,
		RedirectURI:  outcome.RedirectURI,
	}
	if !outcome.StateExpiresAt.IsZero() {
		out.StateExpiresAt = outcome.StateExpiresAt.UTC().Format(time.RFC3339)
	}
	if outcome.AuthorizeURL != "" {
		// The consent handoff contract, in the tool's own words: the link is
		// HIS to open, on any device, at a time of his choosing, and the lane
		// never handles his password.
		out.HumanStep = "Hantar pautan ini kepada pemilik akaun untuk dibuka sendiri (mana-mana peranti). " +
			"Jangan buka, jangan taip kata laluan, jangan minta kata laluan. Pautan ini luput — panggil tool ini semula untuk pautan baharu."
	}
	for _, st := range s.deps.Couriers.States(ctx, accountID) {
		if st.Info.Name == outcome.Provider {
			out.Status = courierProviderOutFrom(st)
			break
		}
	}
	return nil, out, nil
}

// disconnectShippingCourierIn is the disconnect input.
type disconnectShippingCourierIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"ID workspace (dari list_workspaces). Kosong = akaun anda sendiri"`
	Provider    string `json:"provider" jsonschema:"Pembekal: lalamove atau easyparcel"`
}

// disconnectShippingCourier drops the workspace's connection and its grant, keeping
// the pickup address so reconnecting is one call.
func (s *Server) disconnectShippingCourier(
	ctx context.Context, _ *mcp.CallToolRequest, in disconnectShippingCourierIn,
) (*mcp.CallToolResult, *shippingCouriersOut, error) {
	if s.deps.Couriers == nil {
		return nil, nil, errCouriersUnavailable
	}
	if _, err := s.requireWorkspaceWriteRole(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	accountID, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	if err := s.deps.Couriers.Policy().Disconnect(ctx, accountID, in.Provider); err != nil {
		return nil, nil, err
	}
	return nil, couriersOut(accountID, s.deps.Couriers.States(ctx, accountID),
		s.deps.Couriers.Creds.EasyParcel.RedirectURI(s.deps.Couriers.Env)), nil
}

// couriersOut maps the shared state model onto the tool's response shape.
func couriersOut(accountID string, states []shipping.ProviderState, redirectURI string) *shippingCouriersOut {
	out := &shippingCouriersOut{
		AccountID:    accountID,
		Providers:    make([]courierProviderOut, 0, len(states)),
		RedirectURI:  strings.TrimSpace(redirectURI),
		CallbackPath: "/api/shipping/easyparcel/callback",
	}
	for _, st := range states {
		out.Providers = append(out.Providers, courierProviderOutFrom(st))
	}
	return out
}

// courierProviderOutFrom maps ONE shared state onto the tool's row.
func courierProviderOutFrom(st shipping.ProviderState) courierProviderOut {
	row := courierProviderOut{
		Provider:           st.Info.Name,
		CredentialsPresent: st.Info.CredentialsPresent,
		CredentialsSource:  st.CredentialsSource,
		CredentialsHint:    st.Info.CredentialsHint,
		NeedsBrowser:       st.Info.NeedsBrowser,
		PointBased:         st.Info.PointBased,
		DocURL:             st.Info.DocURL,
		Environment:        st.Info.Environment,
		Ready:              st.Ready,
		Missing:            st.Missing,
	}
	if c := st.Connection; c != nil {
		row.Status = c.Status
		if c.Environment != "" {
			row.Environment = c.Environment
		}
		row.Connected = c.Usable()
		row.AccountLabel = c.AccountLabel
		row.OriginPostcode = c.Origin.Postcode
		row.OriginState = c.Origin.State
		row.OriginCountry = c.OriginCountry
		row.OriginLat = c.OriginLat
		row.OriginLng = c.OriginLng
		row.ServiceType = c.ServiceType
		row.HasGrant = c.HasGrant
		row.LastError = c.LastError
		if !c.ConnectedAt.IsZero() {
			row.ConnectedAt = c.ConnectedAt.UTC().Format(time.RFC3339)
		}
	}
	return row
}
