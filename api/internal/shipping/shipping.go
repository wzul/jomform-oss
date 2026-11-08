// Package shipping resolves a delivery FEE for a form's shipping block from a
// workspace-managed set of zones and rates.
//
// WHY THIS EXISTS. A builder `shipping` block has always been a priced choice
// list the merchant types by hand, per form: the "zones" are just option labels
// and the prices are literals in that one form's config. Nothing is reused
// across forms, there is no place for a free-shipping threshold, and the server
// cannot derive anything from the buyer's address. This package is the second
// rate SOURCE: the block names a zone set, the buyer's address picks the zone,
// and the matched rate row is the fee.
//
// IT IS PURE. No database, no HTTP, no courier call — the store hands it a set
// and it answers with a Quote. That is what makes the rules testable directly
// (and what a phase-2 courier source can reuse for its weight bands).
//
// THE STATIC PATH IS NOT HERE. A block with no `source` (or source=static) keeps
// resolving from its own `choices` exactly as before; this package is never
// consulted for it. That is the regression rule of the card: an existing
// published form's shipping fee must not move.
//
// MONEY IS INTEGER SEN. There is no float anywhere in this package and no
// rounding step: a fee is a rate row's price_sen, and the only arithmetic is
// `subtotal >= free_over_sen` and the caller's `subtotal + shipping - discount`
// (payments.SumItems). That keeps the fee identical to the way an existing
// priced choice is summed.
package shipping

import (
	"sort"
	"strings"

	"github.com/wzul/jomform/api/internal/buyertext"
)

// Rate sources a shipping block can name in its `source` field.
const (
	// SourceStatic is the pre-existing behaviour: the block's own priced
	// choice list. It is the DEFAULT (an absent/empty source means this one).
	SourceStatic = "static"
	// SourceZones derives the buyer's zone from the submitted address and
	// applies the matching managed rate. This package implements it.
	SourceZones = "zones"
	// SourceCourier is the PHASE 2 seam (J&T / PosLaju / Shopee Express /
	// Lalamove live quoting, which needs merchant accounts Wan must obtain).
	// It is NOT implemented: the submit path refuses it with a clear message
	// instead of falling back to 0, and the builder does not offer it as a
	// working option.
	SourceCourier = "courier"
)

// What happens when the buyer's address matches NO zone. Never a silent 0.
const (
	// OnNoMatchReject refuses the order, naming the address. The default when
	// the block does not say (a zone set that silently charged 0 would be the
	// defect this exists to prevent, and a refused order is the loud one).
	OnNoMatchReject = "reject"
	// OnNoMatchDefault falls back to the zone flagged is_fallback. If the set
	// has no fallback zone the order is refused rather than charged 0.
	OnNoMatchDefault = "default"
)

// Errors the caller turns into a buyer-facing 400. Each carries a stable
// refusal code and its English wire prose; the HTTP layer renders the same
// sentence in the buyer's resolved language (internal/buyertext).
var (
	// ErrNoZone: nothing in the set matches the submitted address and the
	// block rejects unmatched addresses.
	ErrNoZone = buyertext.New(buyertext.CodeShipNoDelivery, nil)
	// ErrNoFallback: on_no_match=default but the set declares no fallback zone.
	ErrNoFallback = buyertext.New(buyertext.CodeShipNoZoneNoDefault, nil)
	// ErrZoneUnknown: the submitted zone choice is not in the block's set.
	ErrZoneUnknown = buyertext.New(buyertext.CodeShipZoneInvalid, nil)
	// ErrZoneMismatch: the submitted zone choice disagrees with the zone the
	// address resolves to. Refused rather than re-priced behind the buyer's
	// back — the client's shown price would be a lie.
	ErrZoneMismatch = buyertext.New(buyertext.CodeShipZoneMismatch, nil)
	// ErrNoRate: the matched zone carries no rate row.
	ErrNoRate = buyertext.New(buyertext.CodeShipZoneNoRate, nil)
	// ErrNoRateForWeight: the block declares a parcel weight and no rate row's
	// band covers it.
	ErrNoRateForWeight = buyertext.New(buyertext.CodeShipNoRateForWeight, nil)
	// ErrNotSupported: a source this phase does not implement (courier).
	ErrNotSupported = buyertext.New(buyertext.CodeShipSourceUnsupported, nil)
)

// Rate is one rate row of a zone: a price, an optional weight band and an
// optional free-shipping threshold.
type Rate struct {
	ID       string
	Label    string
	PriceSen int64
	// Weight band in grams; MaxWeightG == 0 means no upper bound, MinWeightG
	// == 0 means no lower bound.
	MinWeightG int
	MaxWeightG int
	// FreeOverSen: an items subtotal at or above this makes the fee 0 (0
	// disables the threshold). Crossing it is reported on the Quote so the
	// sale records WHY the fee is 0.
	FreeOverSen int64
	// Sort orders the candidates of one zone; lower first.
	Sort int
}

// Zone is one region: a name, the rules that match it and its rate rows.
type Zone struct {
	ID   string
	Name string
	// Match rules. Any ONE match is enough. States takes Malaysian state
	// names in any spelling the alias table knows, plus the region tokens
	// peninsular / east_malaysia (see stateAliases/regionStates).
	States       []string
	Districts    []string
	PostcodeFrom string
	PostcodeTo   string
	// Priority orders zone evaluation (lower first) so a postcode exception
	// can sit above the broad state zone containing it.
	Priority int
	// Fallback marks the zone used ONLY when nothing else matched, and only
	// when the block's on_no_match is "default".
	Fallback bool
	// Lat / Lng are the zone's own point (decimal degrees, optional). A
	// point-based courier (Lalamove) quotes a route between two points, so this
	// is the drop-off point for a buyer whose address matches this zone; the
	// workspace's courier connection supplies the pickup point. EMPTY MEANS
	// UNKNOWN: a courier block then falls back to this zone's rate rather than
	// quoting a coordinate nobody gave (see ResolveCourier).
	Lat   string
	Lng   string
	Rates []Rate
}

// ZoneSet is a named collection of zones a shipping block points at.
type ZoneSet struct {
	ID      string
	Name    string
	Default bool
	Zones   []Zone
}

// Address is what the buyer's submission says about where the parcel goes. The
// submit path fills it from the form's mapped blocks (state/city/postcode) and
// falls back to the client envelope, so a plain API/bank-direct buyer with a
// client.state still resolves.
type Address struct {
	State    string
	District string
	Postcode string
}

// Empty reports whether the submission said nothing about the address at all.
// An empty address cannot be derived from, so a block whose buyer chose a zone
// explicitly uses that choice (see Resolve).
func (a Address) Empty() bool {
	return strings.TrimSpace(a.State) == "" &&
		strings.TrimSpace(a.District) == "" &&
		strings.TrimSpace(a.Postcode) == ""
}

// Options are the per-block settings the resolver needs.
type Options struct {
	// OnNoMatch is OnNoMatchReject (default) or OnNoMatchDefault.
	OnNoMatch string
	// ChosenZoneID is the zone id the buyer's own select submitted, if the
	// form carries the block's select. It never overrides the address: it is
	// used only when there is no address to derive from, and it MUST agree
	// with the derived zone otherwise.
	ChosenZoneID string
	// WeightG is the parcel weight the block declares, in grams (0 = unknown,
	// in which case weight bands are not applied).
	WeightG int64
}

// Quote is the resolved delivery fee plus everything the sale records about how
// it was reached.
type Quote struct {
	ZoneID   string `json:"zone_id"`
	ZoneName string `json:"zone_name"`
	RateID   string `json:"rate_id"`
	// RateLabel is the rate row's own label (may be empty).
	RateLabel string `json:"rate_label,omitempty"`
	// PriceSen is the fee the order is charged: RatePriceSen adjusted for the
	// free-shipping threshold.
	PriceSen int64 `json:"price_sen"`
	// ListPriceSen is the rate row's price before the threshold.
	ListPriceSen int64 `json:"list_price_sen"`
	// Free is true when the items subtotal crossed the rate's free_over_sen.
	Free bool `json:"free"`
	// FreeOverSen is the threshold that applied (0 when none did).
	FreeOverSen int64 `json:"free_over_sen,omitempty"`
	// MatchedBy is which rule resolved the zone: state | postcode | district |
	// fallback | choice.
	MatchedBy string `json:"matched_by"`
	// WeightG echoes the weight the rate was chosen for (0 = unknown).
	WeightG int64 `json:"weight_g,omitempty"`
}

// Normalize reduces a free-text region name to a comparable key: lowercased,
// trimmed, internal whitespace collapsed, punctuation and separators dropped or
// folded to spaces. The underscores matter: a config written by an agent (or a
// token copied out of these docs) spells the region tokens `east_malaysia` /
// `west_malaysia`, and they must resolve to the same key as "East Malaysia".
func Normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer(".", "", ",", "", "-", " ", "_", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// stateAliases maps every accepted spelling of a Malaysian state to ONE
// canonical key, so "Penang", "Pulau Pinang" and "pinang" are one zone rule and
// "KL" matches Kuala Lumpur. Names outside this table (a merchant's own region
// label, a district used as a state) fall back to their normalized text, which
// still matches itself exactly.
var stateAliases = map[string]string{
	"johor":                "johor",
	"johor bahru":          "johor",
	"kedah":                "kedah",
	"kedah darul aman":     "kedah",
	"kelantan":             "kelantan",
	"melaka":               "melaka",
	"malacca":              "melaka",
	"negeri sembilan":      "negeri sembilan",
	"n sembilan":           "negeri sembilan",
	"sembilan":             "negeri sembilan",
	"pahang":               "pahang",
	"perak":                "perak",
	"perlis":               "perlis",
	"pulau pinang":         "penang",
	"penang":               "penang",
	"pinang":               "penang",
	"selangor":             "selangor",
	"terengganu":           "terengganu",
	"kuala lumpur":         "kuala lumpur",
	"wp kuala lumpur":      "kuala lumpur",
	"wilayah kuala lumpur": "kuala lumpur",
	"kl":                   "kuala lumpur",
	"putrajaya":            "putrajaya",
	"wp putrajaya":         "putrajaya",
	"sabah":                "sabah",
	"sarawak":              "sarawak",
	"labuan":               "labuan",
	"wp labuan":            "labuan",
}

// regionStates expands the region tokens a zone rule may use. A merchant who
// types "Peninsular Malaysia" must not have to list thirteen states, and the
// two groups are the only ones that matter for a Malaysian courier fee.
var regionStates = map[string][]string{
	"peninsular":          {"johor", "kedah", "kelantan", "melaka", "negeri sembilan", "pahang", "perak", "perlis", "penang", "selangor", "terengganu", "kuala lumpur", "putrajaya"},
	"peninsular malaysia": {"johor", "kedah", "kelantan", "melaka", "negeri sembilan", "pahang", "perak", "perlis", "penang", "selangor", "terengganu", "kuala lumpur", "putrajaya"},
	"west malaysia":       {"johor", "kedah", "kelantan", "melaka", "negeri sembilan", "pahang", "perak", "perlis", "penang", "selangor", "terengganu", "kuala lumpur", "putrajaya"},
	"semenanjung":         {"johor", "kedah", "kelantan", "melaka", "negeri sembilan", "pahang", "perak", "perlis", "penang", "selangor", "terengganu", "kuala lumpur", "putrajaya"},
	"east malaysia":       {"sabah", "sarawak", "labuan"},
	"malaysia timur":      {"sabah", "sarawak", "labuan"},
	"borneo":              {"sabah", "sarawak", "labuan"},
	"bm":                  {"sabah", "sarawak", "labuan"},
}

// stateKey resolves a state spelling to its canonical key.
func stateKey(s string) string {
	n := Normalize(s)
	if n == "" {
		return ""
	}
	if k, ok := stateAliases[n]; ok {
		return k
	}
	return n
}

// stateTokens expands one zone `states` entry into the set of canonical keys it
// covers: a region token expands to its group, any other entry to itself.
func stateTokens(entry string) []string {
	n := Normalize(entry)
	if n == "" {
		return nil
	}
	if group, ok := regionStates[n]; ok {
		out := make([]string, 0, len(group))
		out = append(out, group...)
		return out
	}
	return []string{stateKey(n)}
}

// MatchZone finds the zone the address resolves to WITHOUT consulting fallback
// zones: a fallback is a policy (on_no_match), not a match, and a reject block
// must still reject an address that only a fallback zone would cover.
//
// Zones are evaluated by priority (lower first), then in set order, and the
// first matching zone wins. Within a zone the rules are tried states ->
// postcode -> districts; the rule that fired is returned so it can be recorded.
func MatchZone(set ZoneSet, addr Address) (Zone, string, bool) {
	ordered := make([]Zone, 0, len(set.Zones))
	for _, z := range set.Zones {
		if z.Fallback {
			continue
		}
		ordered = append(ordered, z)
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Priority < ordered[j].Priority })

	wantState := stateKey(addr.State)
	wantDistrict := Normalize(addr.District)
	wantPostcode := Normalize(addr.Postcode)

	for _, z := range ordered {
		if wantState != "" {
			for _, entry := range z.States {
				for _, tok := range stateTokens(entry) {
					if tok != "" && tok == wantState {
						return z, "state", true
					}
				}
			}
		}
		if wantPostcode != "" && PostcodeInRange(z.PostcodeFrom, z.PostcodeTo, wantPostcode) {
			return z, "postcode", true
		}
		if wantDistrict != "" {
			for _, d := range z.Districts {
				if n := Normalize(d); n != "" && n == wantDistrict {
					return z, "district", true
				}
			}
		}
	}
	return Zone{}, "", false
}

// FallbackZone returns the set's fallback zone, if it declares one.
func FallbackZone(set ZoneSet) (Zone, bool) {
	for _, z := range set.Zones {
		if z.Fallback {
			return z, true
		}
	}
	return Zone{}, false
}

// ZoneByID returns a zone of the set by id.
func ZoneByID(set ZoneSet, id string) (Zone, bool) {
	for _, z := range set.Zones {
		if z.ID == id {
			return z, true
		}
	}
	return Zone{}, false
}

// PostcodeInRange reports whether postcode (a normalized string) falls inside the
// inclusive range from..to. Both bounds must be numeric; an empty `from` cannot
// match, an empty `to` means a single value. A non-numeric postcode (some
// countries) never matches a numeric range rather than being coerced.
func PostcodeInRange(from, to, postcode string) bool {
	lo := digits(from)
	p := digits(postcode)
	if lo < 0 || p < 0 {
		return false
	}
	if p < lo {
		return false
	}
	if strings.TrimSpace(to) == "" {
		return p == lo
	}
	hi := digits(to)
	if hi < 0 {
		return false
	}
	return p <= hi
}

// digits parses an all-digit string (leading zeros allowed, 5 digits typical)
// into an int64, or -1 when it is not all digits.
func digits(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return -1
	}
	var out int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return -1
		}
		out = out*10 + int64(r-'0')
	}
	return out
}

// PickRate chooses the rate row that applies for weightG:
//
//   - weightG <= 0 (unknown): the first row by (sort, price, label). Bands are
//     ignored, because a block that declares no weight must not be refused.
//   - weightG > 0: only rows whose band contains the weight are candidates;
//     a declared weight with no covering band is ErrNoRateForWeight rather than
//     a silent fallback to an unrelated price.
func PickRate(rates []Rate, weightG int64) (Rate, error) {
	if len(rates) == 0 {
		return Rate{}, ErrNoRate
	}
	ordered := append([]Rate(nil), rates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Sort != ordered[j].Sort {
			return ordered[i].Sort < ordered[j].Sort
		}
		if ordered[i].PriceSen != ordered[j].PriceSen {
			return ordered[i].PriceSen < ordered[j].PriceSen
		}
		return ordered[i].Label < ordered[j].Label
	})
	if weightG <= 0 {
		return ordered[0], nil
	}
	for _, r := range ordered {
		banded := r.MinWeightG > 0 || r.MaxWeightG > 0
		if !banded {
			continue
		}
		if int64(r.MinWeightG) <= weightG && (r.MaxWeightG == 0 || weightG <= int64(r.MaxWeightG)) {
			return r, nil
		}
	}
	// No band covered the weight. A single unbanded row is still the answer for
	// a zone that does not do weight bands at all (the merchant declared a
	// weight but priced by zone only) — but only when EVERY row is unbanded;
	// a mixed set means the bands are real and the weight is outside them.
	allUnbanded := true
	for _, r := range rates {
		if r.MinWeightG > 0 || r.MaxWeightG > 0 {
			allUnbanded = false
			break
		}
	}
	if allUnbanded {
		return ordered[0], nil
	}
	return Rate{}, ErrNoRateForWeight
}

// Resolve answers the delivery fee for one shipping block. subtotalSen is the
// order's ITEMS subtotal (line items only — the shipping fee itself is not part
// of it), which is what the free-shipping threshold compares against.
//
// The rules, in order, and each one is observable on the Quote:
//
//  1. The address is derived through MatchZone.
//  2. A submitted zone choice must AGREE with the derived zone (ErrZoneMismatch).
//     The address is the source of truth; a disagreement is refused rather than
//     re-priced, because the buyer's own screen showed the other price.
//  3. No match -> on_no_match. "reject" (the default) is ErrNoZone; "default"
//     uses the fallback zone, and ErrNoFallback when the set has none. Neither
//     path can produce a silent 0.
//  4. An address that says NOTHING and a submitted zone choice is the one case
//     the choice decides (there is nothing to derive from) — recorded as
//     matched_by "choice".
//  5. The rate row is chosen by PickRate (weight bands when a weight is given).
//  6. The free-shipping threshold turns the fee into 0 and sets Free +
//     FreeOverSen, so the sale records why.
func Resolve(set ZoneSet, addr Address, subtotalSen int64, opts Options) (Quote, error) {
	zone, by, found := MatchZone(set, addr)

	if found && opts.ChosenZoneID != "" {
		chosen, ok := ZoneByID(set, opts.ChosenZoneID)
		if !ok {
			return Quote{}, ErrZoneUnknown
		}
		if chosen.ID != zone.ID {
			return Quote{}, ErrZoneMismatch
		}
	}

	if !found {
		switch normalizeOnNoMatch(opts.OnNoMatch) {
		case OnNoMatchDefault:
			fb, ok := FallbackZone(set)
			if !ok {
				return Quote{}, ErrNoFallback
			}
			zone, by = fb, "fallback"
		default:
			// Nothing matched. The buyer's own choice is honoured ONLY when the
			// submission carried no address at all (nothing to derive from) —
			// never as a quiet override of an address that really named another
			// zone.
			if addr.Empty() && opts.ChosenZoneID != "" {
				chosen, ok := ZoneByID(set, opts.ChosenZoneID)
				if !ok {
					return Quote{}, ErrZoneUnknown
				}
				zone, by = chosen, "choice"
				break
			}
			return Quote{}, ErrNoZone
		}
	}

	rate, err := PickRate(zone.Rates, opts.WeightG)
	if err != nil {
		return Quote{}, err
	}
	q := Quote{
		ZoneID:       zone.ID,
		ZoneName:     zone.Name,
		RateID:       rate.ID,
		RateLabel:    rate.Label,
		PriceSen:     rate.PriceSen,
		ListPriceSen: rate.PriceSen,
		MatchedBy:    by,
		WeightG:      opts.WeightG,
	}
	if rate.FreeOverSen > 0 && subtotalSen >= rate.FreeOverSen {
		q.PriceSen = 0
		q.Free = true
		q.FreeOverSen = rate.FreeOverSen
	}
	return q, nil
}

// normalizeOnNoMatch maps anything that is not explicitly "default" to the
// reject rule, so a typo in a block config refuses loudly rather than charging 0.
func normalizeOnNoMatch(v string) string {
	if Normalize(v) == OnNoMatchDefault {
		return OnNoMatchDefault
	}
	return OnNoMatchReject
}
