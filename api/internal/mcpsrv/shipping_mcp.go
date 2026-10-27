// MCP parity tools for managed shipping zones + rates.
//
// The dashboard's Shipping tab (form settings) reads and writes one workspace's
// zone sets through /v1/rest/shipping; these two tools are the same surface over
// MCP, so a connected agent can configure a merchant's delivery rates without a
// browser. Parity is a rule in AGENTS.md (every dashboard feature needs an MCP
// tool) and the checklist parsed by mcp_parity_test.go names both of them.
//
// Both tools resolve the SAME account the dashboard would for the same call: the
// per-call `workspace_id` (or `account_id`) argument is honoured by accountAndScope
// (issue #307), and with no argument the caller's own account is used — the same
// resolution the REST edge performs for ?workspace= (issue #158).
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
var errShippingUnavailable = errors.New("kadar penghantaran tidak tersedia")

// shippingRateIn is one rate row of a zone: a price, an optional weight band and
// an optional free-shipping threshold. Money is integer SEN.
type shippingRateIn struct {
	Label string `json:"label,omitempty" jsonschema:"Nama kadar yang dilihat pembeli (cth: Standard, Express). Kosong = tanpa nama"`
	// PriceSen is the fee in sen (RM1.00 = 100). Never a decimal string.
	PriceSen int64 `json:"price_sen" jsonschema:"Harga kadar dalam SEN (RM8.00 = 800)"`
	// MinWeightG / MaxWeightG are the weight band in grams; 0 = no bound.
	MinWeightG  int   `json:"min_weight_g,omitempty" jsonschema:"Berat minimum (gram). 0 = tiada had bawah"`
	MaxWeightG  int   `json:"max_weight_g,omitempty" jsonschema:"Berat maksimum (gram). 0 = tiada had atas"`
	FreeOverSen int64 `json:"free_over_sen,omitempty" jsonschema:"Penghantaran percuma bila subtotal barang >= nilai ini (SEN). 0 = tiada ambang"`
	Sort        int   `json:"sort,omitempty" jsonschema:"Susunan kadar (kecil dahulu). Kadar pertama yang dipilih"`
}

// shippingZoneIn is one zone: its match rules (any ONE match is enough) and its
// rates.
type shippingZoneIn struct {
	ID           string           `json:"id,omitempty" jsonschema:"ID zon (kosong = baharu; ID lama diganti pada setiap simpan)"`
	Name         string           `json:"name" jsonschema:"Nama zon (cth: Semenanjung, Sabah & Sarawak)"`
	States       []string         `json:"states,omitempty" jsonschema:"Negeri yang diliputi. Nama negeri Malaysia (Johor, Penang/Pulau Pinang, KL...) atau token wilayah: peninsular, east_malaysia"`
	Districts    []string         `json:"districts,omitempty" jsonschema:"Daerah/bandar yang diliputi (padanan tepat, tidak sensitif huruf besar)"`
	PostcodeFrom string           `json:"postcode_from,omitempty" jsonschema:"Poskod mula (inklusif). Boleh dipakai tanpa negeri"`
	PostcodeTo   string           `json:"postcode_to,omitempty" jsonschema:"Poskod akhir (inklusif). Kosong = satu nilai sahaja"`
	Priority     int              `json:"priority,omitempty" jsonschema:"Keutamaan penilaian (kecil dahulu); zon pengecualian di atas zon negeri"`
	Fallback     bool             `json:"is_fallback,omitempty" jsonschema:"Zon lalai: dipakai HANYA bila tiada zon lain padan (bila on_no_match=default pada blok penghantaran)"`
	Rates        []shippingRateIn `json:"rates" jsonschema:"Kadar untuk zon ini. Sekurang-kurangnya satu diperlukan"`
}

// shippingSetIn is one named set of zones.
type shippingSetIn struct {
	ID        string           `json:"id,omitempty" jsonschema:"ID set zon (kosong = baharu). ID ini yang dirujuk oleh blok penghantaran borang"`
	Name      string           `json:"name" jsonschema:"Nama set zon"`
	IsDefault bool             `json:"is_default,omitempty" jsonschema:"Set lalai untuk workspace ini"`
	Zones     []shippingZoneIn `json:"zones" jsonschema:"Zon dalam set ini"`
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
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	States       []string          `json:"states"`
	Districts    []string          `json:"districts"`
	PostcodeFrom string            `json:"postcode_from,omitempty"`
	PostcodeTo   string            `json:"postcode_to,omitempty"`
	Priority     int               `json:"priority"`
	Fallback     bool              `json:"is_fallback"`
	Rates        []shippingRateOut `json:"rates"`
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
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"ID workspace (dari list_workspaces). Kosong = akaun anda sendiri"`
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
	WorkspaceID string          `json:"workspace_id,omitempty" jsonschema:"ID workspace (dari list_workspaces). Kosong = akaun anda sendiri"`
	Sets        []shippingSetIn `json:"sets" jsonschema:"SEMUA set zon yang dikehendaki. Set yang tiada di sini akan DIPADAM (ditolak jika ada borang yang masih merujuknya)"`
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
			Priority: z.Priority, Fallback: z.Fallback, Rates: []shipping.Rate{},
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
				Priority: z.Priority, Fallback: z.Fallback, Rates: []shippingRateOut{},
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
