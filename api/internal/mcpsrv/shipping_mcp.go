// MCP parity tools for managed shipping zones + rates.
//
// The dashboard's Shipping tab (form settings) reads and writes one workspace's
// zone sets through /v1/rest/shipping; these two tools are the same surface over
// MCP, so a connected agent can configure a merchant's delivery rates without a
// browser. Parity is a rule in AGENTS.md (every dashboard feature needs an MCP
// tool) and the checklist parsed by mcp_parity_test.go names both of them.
//
// THE TWO HALVES DO NOT RESOLVE THE SAME WAY, and each schema must state the half
// it actually implements. The READ falls back to the caller's own account when
// `workspace_id` is empty: accountAndScope does that (issue #307), the same
// resolution the REST edge performs for ?workspace= (issue #158).
// The WRITE does not — it gates through requireWorkspaceWriteRole, which needs an
// EXPLICIT workspace because the manage-or-admin role it checks is a role IN a
// workspace, so an empty id is refused ("workspace not found") before
// accountAndScope is ever reached. Both input schemas used to promise "Kosong =
// akaun anda sendiri"; that was true only of the read, so the promise now lives
// only on the read. A write tool here must not advertise a fallback it does not
// implement — TestNoWriteToolAdvertisesTheOwnAccountFallback
// (shipping_workspace_id_schema_test.go) enforces the pairing.
package mcpsrv

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/shipping"
)

// errShippingUnavailable is returned when the shipping store is not wired (a
// bare-mode deployment). Refusing loudly is deliberate: a missing store must
// never look like "no rates", which on the submit path would be free shipping.
var errShippingUnavailable = errors.New("shipping rates not available")

// shippingRateIn is one rate row of a zone: a price, an optional weight band and
// an optional free-shipping threshold. Money is integer SEN.
type shippingRateIn struct {
	Label string `json:"label,omitempty" jsonschema:"Rate name shown to the buyer (e.g.: Standard, Express). Empty = no name"`
	// PriceSen is the fee in sen (RM1.00 = 100). Never a decimal string.
	PriceSen int64 `json:"price_sen" jsonschema:"Rate price in SEN (RM8.00 = 800)"`
	// MinWeightG / MaxWeightG are the weight band in grams; 0 = no bound.
	MinWeightG  int   `json:"min_weight_g,omitempty" jsonschema:"Minimum weight (grams). 0 = no lower limit"`
	MaxWeightG  int   `json:"max_weight_g,omitempty" jsonschema:"Maximum weight (grams). 0 = no upper limit"`
	FreeOverSen int64 `json:"free_over_sen,omitempty" jsonschema:"Free shipping when the item subtotal >= this value (SEN). 0 = no threshold"`
	Sort        int   `json:"sort,omitempty" jsonschema:"Rate ordering (smallest first). The first matching rate is chosen"`
}

// shippingZoneIn is one zone: its match rules (any ONE match is enough) and its
// rates.
type shippingZoneIn struct {
	ID           string   `json:"id,omitempty" jsonschema:"Zone ID (empty = new; the old ID is replaced on every save)"`
	Name         string   `json:"name" jsonschema:"Zone name (e.g.: Peninsular, Sabah & Sarawak)"`
	States       []string `json:"states,omitempty" jsonschema:"States covered. Malaysian state names (Johor, Penang/Pulau Pinang, KL...) or region tokens: peninsular, east_malaysia"`
	Districts    []string `json:"districts,omitempty" jsonschema:"District/city covered (exact match, case-insensitive)"`
	PostcodeFrom string   `json:"postcode_from,omitempty" jsonschema:"Start postcode (inclusive). Can be used without a state"`
	PostcodeTo   string   `json:"postcode_to,omitempty" jsonschema:"End postcode (inclusive). Empty = a single value only"`
	Priority     int      `json:"priority,omitempty" jsonschema:"Evaluation priority (smallest first); exclusion zones rank above state zones"`
	Fallback     bool     `json:"is_fallback,omitempty" jsonschema:"Default zone: applied ONLY when no other zone matches (when on_no_match=default on the shipping block)"`
	// Lat / Lng (phase 2): the zone's own point, which is where a POINT-BASED
	// courier (lalamove) is quoted TO. Optional — a zone without a point simply
	// cannot be courier-quoted and its own rate applies instead. Give BOTH or
	// neither.
	Lat   string           `json:"lat,omitempty" jsonschema:"Zone point latitude (decimal). For point-based shipping providers (lalamove). Provide together with lng"`
	Lng   string           `json:"lng,omitempty" jsonschema:"Zone point longitude (decimal). Supply together with lat"`
	Rates []shippingRateIn `json:"rates" jsonschema:"Rates for this zone. At least one is required"`
}

// shippingSetIn is one named set of zones.
type shippingSetIn struct {
	ID        string           `json:"id,omitempty" jsonschema:"Zone set ID (empty = new). This is the ID referenced by the form's shipping block"`
	Name      string           `json:"name" jsonschema:"Zone set name"`
	IsDefault bool             `json:"is_default,omitempty" jsonschema:"Default set for this workspace"`
	Zones     []shippingZoneIn `json:"zones" jsonschema:"Zones in this set"`
}

// shippingRateOut / shippingZoneOut / shippingSetOut / shippingRatesOut are the
// read shapes (the SDK derives the tool's outputSchema from the handler's Out
// type, which must be an OBJECT — output_schema_test.go enforces that).
type shippingRateOut struct {
	ID          string `json:"id"`
	Label       string `json:"label,omitempty"`
	PriceSen    int64  `json:"price_sen"`
	MinWeightG  int    `json:"min_weight_g"`
	MaxWeightG  int    `json:"max_weight_g"`
	FreeOverSen int64  `json:"free_over_sen"`
	Sort        int    `json:"sort"`
}

type shippingZoneOut struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	States       []string `json:"states"`
	Districts    []string `json:"districts"`
	PostcodeFrom string   `json:"postcode_from,omitempty"`
	PostcodeTo   string   `json:"postcode_to,omitempty"`
	Priority     int      `json:"priority"`
	Fallback     bool     `json:"is_fallback"`
	// Lat / Lng are the zone's own point (phase 2): where a point-based courier
	// is quoted to. Empty = not set.
	Lat   string            `json:"lat,omitempty"`
	Lng   string            `json:"lng,omitempty"`
	Rates []shippingRateOut `json:"rates"`
}

type shippingSetOut struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	IsDefault bool              `json:"is_default"`
	Zones     []shippingZoneOut `json:"zones"`
}

type shippingRatesOut struct {
	AccountID string           `json:"account_id"`
	Sets      []shippingSetOut `json:"sets"`
}

// getShippingRatesIn is the read input: the per-call scope is carried by the
// optional reserved arguments (workspace_id/account_id), handled by the call
// scope plumbing rather than by a field here.
type getShippingRatesIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"Workspace ID (from list_workspaces). Empty = your own account"`
}

// getShippingRates reads the managed zone sets of one account.
func (s *Server) getShippingRates(
	ctx context.Context, _ *mcp.CallToolRequest, _ getShippingRatesIn,
) (*mcp.CallToolResult, *shippingRatesOut, error) {
	if s.deps.Shipping == nil {
		return nil, nil, errShippingUnavailable
	}
	accountID, err := s.accountAndScope(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	sets, err := s.deps.Shipping.Sets(ctx, []string{accountID})
	if err != nil {
		return nil, nil, err
	}
	return nil, &shippingRatesOut{AccountID: accountID, Sets: shippingSetsOut(sets)}, nil
}

// setShippingRatesIn replaces the WHOLE configuration: a set omitted from `sets`
// is DELETED (refused when a form still points at it).
type setShippingRatesIn struct {
	WorkspaceID string          `json:"workspace_id,omitempty" jsonschema:"Workspace ID (from list_workspaces). Required: this write tool rejects an empty workspace_id — call list_workspaces first"`
	Sets        []shippingSetIn `json:"sets" jsonschema:"ALL zone sets that are wanted. Sets absent here will be DELETED (rejected if any form still references them)"`
}

// setShippingRates replaces the account's zone sets, rates and match rules.
func (s *Server) setShippingRates(
	ctx context.Context, _ *mcp.CallToolRequest, in setShippingRatesIn,
) (*mcp.CallToolResult, *shippingRatesOut, error) {
	if s.deps.Shipping == nil {
		return nil, nil, errShippingUnavailable
	}
	// Issue #645: write gate = API-key write scope AND manage-or-admin role.
	if _, err := s.requireWorkspaceWriteRole(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	accountID, err := s.accountAndScope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	sets := make([]shipping.ZoneSet, 0, len(in.Sets))
	for _, set := range in.Sets {
		sets = append(sets, set.toZoneSet())
	}
	// The SAME validation the dashboard's PUT runs, before the transaction: a
	// zone with no rate, an unnamed zone or an inverted weight band is refused
	// here rather than becoming a buyer's 400 at checkout.
	if err := dbstore.ValidateShippingSets(sets); err != nil {
		return nil, nil, err
	}
	if err := s.deps.Shipping.Replace(ctx, accountID, sets); err != nil {
		return nil, nil, err
	}
	saved, err := s.deps.Shipping.Sets(ctx, []string{accountID})
	if err != nil {
		return nil, nil, err
	}
	return nil, &shippingRatesOut{AccountID: accountID, Sets: shippingSetsOut(saved)}, nil
}

func (in shippingSetIn) toZoneSet() shipping.ZoneSet {
	out := shipping.ZoneSet{ID: in.ID, Name: in.Name, Default: in.IsDefault, Zones: []shipping.Zone{}}
	for _, z := range in.Zones {
		zone := shipping.Zone{
			ID: z.ID, Name: z.Name, States: z.States, Districts: z.Districts,
			PostcodeFrom: z.PostcodeFrom, PostcodeTo: z.PostcodeTo,
			Priority: z.Priority, Fallback: z.Fallback,
			Lat: z.Lat, Lng: z.Lng,
			Rates: []shipping.Rate{},
		}
		for _, r := range z.Rates {
			zone.Rates = append(zone.Rates, shipping.Rate{
				Label: r.Label, PriceSen: r.PriceSen,
				MinWeightG: r.MinWeightG, MaxWeightG: r.MaxWeightG,
				FreeOverSen: r.FreeOverSen, Sort: r.Sort,
			})
		}
		out.Zones = append(out.Zones, zone)
	}
	return out
}

// shippingSetsOut maps store rows onto the tool's response shape.
func shippingSetsOut(sets []shipping.ZoneSet) []shippingSetOut {
	out := make([]shippingSetOut, 0, len(sets))
	for _, s := range sets {
		so := shippingSetOut{ID: s.ID, Name: s.Name, IsDefault: s.Default, Zones: []shippingZoneOut{}}
		for _, z := range s.Zones {
			zo := shippingZoneOut{
				ID: z.ID, Name: z.Name, States: z.States, Districts: z.Districts,
				PostcodeFrom: z.PostcodeFrom, PostcodeTo: z.PostcodeTo,
				Priority: z.Priority, Fallback: z.Fallback, Lat: z.Lat, Lng: z.Lng,
				Rates: []shippingRateOut{},
			}
			for _, r := range z.Rates {
				zo.Rates = append(zo.Rates, shippingRateOut{
					ID: r.ID, Label: r.Label, PriceSen: r.PriceSen,
					MinWeightG: r.MinWeightG, MaxWeightG: r.MaxWeightG,
					FreeOverSen: r.FreeOverSen, Sort: r.Sort,
				})
			}
			so.Zones = append(so.Zones, zo)
		}
		out = append(out, so)
	}
	return out
}
