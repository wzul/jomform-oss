// Shipping blocks: reading a form's shipping-block configuration and resolving
// the managed (zone) rate source at submit time.
//
// ONE READER, TWO USES. The renderer needs the same per-block settings the
// submit path does — which source, which zone set, which submitted blocks carry
// the buyer's state/postcode/district, what to do when nothing matches and what
// parcel weight the block declares. Both read them through this file, so a
// block cannot render one thing and be charged another.
//
// WHAT IS DELIBERATELY UNCHANGED. A shipping block with no `source` (or
// source=static) is the pre-existing priced choice list: it is NOT in
// zoneShippingBlocks, the submit path never touches it, and its line item is
// summed exactly as before (issue #173/#461). That is the card's regression
// rule, and it is why the new keys are read only when source says "zones".
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/shipping"
)

// shippingBlockSpec is one builder shipping block, as the renderer and the
// submit path both need it.
type shippingBlockSpec struct {
	// ID is the block's own id (the key its submitted value lands under).
	ID string
	// ItemID is what the block submits as a line item's product_id. The builder
	// sets it to the block's own id (a pricing block is not a product), and the
	// submit path uses it to drop that item for a zone-sourced block so the fee
	// is not counted twice.
	ItemID string
	// Source is shipping.SourceStatic | SourceZones | SourceCourier.
	Source string
	// ZoneSetID is the managed zone set the block points at (zones source).
	ZoneSetID string
	// StateField / DistrictField / PostcodeField name the form's own blocks
	// whose submitted values are the buyer's address. Empty = use the client
	// envelope (client.state / client.city / client.zip_code).
	StateField    string
	DistrictField string
	PostcodeField string
	// OnNoMatch is shipping.OnNoMatchReject | OnNoMatchDefault.
	OnNoMatch string
	// WeightG is the parcel weight the block declares, in grams (0 = unknown).
	WeightG int64
	// Required mirrors the block's required flag.
	Required bool
	// Label is the block's label text (rendered as the field label).
	Label string
}

// shippingBlocksIn reads EVERY shipping block of a form config (any source).
func shippingBlocksIn(config map[string]any) []shippingBlockSpec {
	return shippingBlocksWhere(config, func(string) bool { return true })
}

// zoneShippingBlocks reads only the blocks whose rate source is the managed zone
// set — the ones the submit path resolves and the renderer renders from rates.
func zoneShippingBlocks(config map[string]any) []shippingBlockSpec {
	return shippingBlocksWhere(config, func(src string) bool { return src == shipping.SourceZones })
}

// courierShippingBlocks reads the blocks naming the phase-2 courier source.
// They are refused loudly at submit time rather than charged 0.
func courierShippingBlocks(config map[string]any) []shippingBlockSpec {
	return shippingBlocksWhere(config, func(src string) bool { return src == shipping.SourceCourier })
}

// cfgString trims a JSON-decoded string value, or "" for any other type.
func cfgString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

// cfgSen reads a JSON number as integer sen. encoding/json decodes every number
// into float64; a string form ("500") is accepted too because an agent-authored
// config can carry either. Anything else is 0. Money is NEVER a float here: the
// value is truncated to an int64 sen at this boundary, once.
func cfgSen(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	case string:
		var out int64
		for _, r := range strings.TrimSpace(n) {
			if r < '0' || r > '9' {
				return 0
			}
			out = out*10 + int64(r-'0')
		}
		return out
	default:
		return 0
	}
}

// shippingBlocksWhere walks config.blocks for shipping blocks the source filter
// accepts.
func shippingBlocksWhere(config map[string]any, want func(source string) bool) []shippingBlockSpec {
	if config == nil {
		return nil
	}
	blocks, _ := config["blocks"].([]any)
	var out []shippingBlockSpec
	for _, raw := range blocks {
		b, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(cfgString(b["type"])) != "shipping" {
			continue
		}
		id := strings.TrimSpace(cfgString(b["id"]))
		if id == "" {
			continue
		}
		src := shipping.SourceStatic
		if s := strings.TrimSpace(cfgString(b["source"])); s != "" {
			src = s
		}
		if !want(src) {
			continue
		}
		itemID := strings.TrimSpace(cfgString(b["product_id"]))
		if itemID == "" {
			// The builder always sets product_id = the block id for a pricing
			// block; an agent-authored config may omit it. Either way the
			// submitted item's product_id is what must be dropped.
			itemID = id
		}
		out = append(out, shippingBlockSpec{
			ID:            id,
			ItemID:        itemID,
			Source:        src,
			ZoneSetID:     strings.TrimSpace(cfgString(b["zone_set_id"])),
			StateField:    strings.TrimSpace(cfgString(b["zone_state_field"])),
			DistrictField: strings.TrimSpace(cfgString(b["zone_district_field"])),
			PostcodeField: strings.TrimSpace(cfgString(b["zone_postcode_field"])),
			OnNoMatch:     strings.TrimSpace(cfgString(b["on_no_match"])),
			WeightG:       cfgSen(b["weight_g"]),
			Required:      b["required"] == true,
			Label:         strings.TrimSpace(cfgString(b["value"])),
		})
	}
	return out
}

// addressFor builds the buyer's Address for one block: the form's mapped blocks
// win, the client envelope is the fallback (so a bank-direct or API submission
// that carries client.state still resolves).
func addressFor(spec shippingBlockSpec, in *submitInput) shipping.Address {
	pick := func(field, fallback string) string {
		if field != "" {
			if v := strings.TrimSpace(in.Fields[field]); v != "" {
				return v
			}
		}
		return strings.TrimSpace(fallback)
	}
	return shipping.Address{
		State:    pick(spec.StateField, in.Client.State),
		District: pick(spec.DistrictField, in.Client.City),
		Postcode: pick(spec.PostcodeField, in.Client.ZipCode),
	}
}

// shippingDetail is what the sale records about a resolved fee, so the merchant
// can SEE which zone and rate applied instead of inferring it from a number.
type shippingDetail struct {
	Blocks []shippingDetailBlock `json:"blocks"`
}

type shippingDetailBlock struct {
	BlockID string `json:"block_id"`
	// Source is always "zones" today; recorded so phase 2 can add "courier"
	// without a shape change.
	Source string `json:"source"`
	shipping.Quote
}

// resolveZoneShipping resolves EVERY zone-sourced shipping block of the order
// target and returns the total fee in sen plus the JSON recorded on the sale.
//
// A form target is required: the blocks live in the form's config. subtotalSen
// is the ITEMS subtotal (line items only) — the free-shipping threshold compares
// against it, so the fee itself never counts towards the threshold.
//
// Every failure is a refusal (a *shippingError the caller turns into a 400), and
// never a silent 0: an unknown zone set, a courier source, an address no zone
// covers with on_no_match=reject, a zone with no rate.
func (h *PublicHandlers) resolveZoneShipping(ctx context.Context, tgt orderTarget, specs []shippingBlockSpec, in *submitInput, subtotalSen int64) (int64, json.RawMessage, error) {
	if len(specs) == 0 {
		return 0, nil, nil
	}
	if tgt.Form == nil {
		return 0, nil, &shippingError{"borang tidak dijumpai untuk kadar penghantaran"}
	}
	if h.Shipping == nil {
		return 0, nil, &shippingError{"kadar penghantaran mengikut zon tidak dikonfigurasi"}
	}
	accounts := []string{tgt.AccountID}

	// Sets are resolved once per distinct set id, so two blocks pointing at the
	// same set do not read it twice.
	cache := map[string]shipping.ZoneSet{}
	var total int64
	detail := shippingDetail{Blocks: []shippingDetailBlock{}}
	for _, spec := range specs {
		setID := strings.TrimSpace(spec.ZoneSetID)
		if setID == "" {
			return 0, nil, &shippingError{"blok penghantaran ini belum dipautkan ke set zon"}
		}
		set, ok := cache[setID]
		if !ok {
			loaded, found, err := h.Shipping.SetByID(ctx, accounts, setID)
			if err != nil {
				return 0, nil, err
			}
			if !found {
				return 0, nil, &shippingError{"set zon penghantaran tidak dijumpai"}
			}
			set = loaded
			cache[setID] = set
		}
		quote, err := shipping.Resolve(set, addressFor(spec, in), subtotalSen, shipping.Options{
			OnNoMatch: spec.OnNoMatch,
			// The buyer's own zone choice. The select's element carries the
			// block's product_id as its data-id, which the builder sets to the
			// block's own id; an agent-authored config may differ, so both keys
			// are read and the block id wins.
			ChosenZoneID: firstNonEmpty(in.Fields[spec.ID], in.Fields[spec.ItemID]),
			WeightG:      spec.WeightG,
		})
		if err != nil {
			return 0, nil, &shippingError{err.Error()}
		}
		total += quote.PriceSen
		detail.Blocks = append(detail.Blocks, shippingDetailBlock{BlockID: spec.ID, Source: shipping.SourceZones, Quote: quote})
	}
	if total == 0 && len(detail.Blocks) == 0 {
		return 0, nil, nil
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return 0, nil, err
	}
	return total, raw, nil
}

// shippingError is a buyer-facing refusal on the shipping path: a 400 whose
// message is the resolver's own sentence (never an internal error).
type shippingError struct{ msg string }

func (e *shippingError) Error() string { return e.msg }

// zoneOptionView is one <option> of a zone-sourced shipping select.
type zoneOptionView struct {
	Value       string
	Label       string
	PriceSen    int64
	FreeOverSen int64
	Note        string
}

// zoneOptionsFor builds the select options for one block: every zone a buyer can
// legitimately pick. A FALLBACK zone is offered only when the block's
// on_no_match is "default" — that is the case where a buyer outside every zone
// would otherwise have nothing to choose (and the server would charge the
// fallback rate anyway, which is exactly what that policy means).
//
// Each option's price comes from the SAME resolver the checkout uses (for the
// block's declared weight), and a rate with a free-shipping threshold carries
// the note the buyer needs to understand a total that drops: the client script
// zeroes the option's price once the items subtotal crosses it.
func zoneOptionsFor(set shipping.ZoneSet, spec shippingBlockSpec, ls langStrings) []zoneOptionView {
	out := []zoneOptionView{}
	fallback := shipping.Normalize(spec.OnNoMatch) == shipping.OnNoMatchDefault
	for _, z := range set.Zones {
		if z.Fallback && !fallback {
			continue
		}
		rate, err := shipping.PickRate(z.Rates, spec.WeightG)
		if err != nil {
			// A zone with no usable rate cannot be offered: picking it would
			// only fail at submit. Publish readiness refuses this shape for a
			// live form.
			continue
		}
		label := z.Name
		if z.Fallback {
			label = fmt.Sprintf("%s (%s)", z.Name, ls.ShipFallback)
		}
		if rate.Label != "" {
			label = fmt.Sprintf("%s · %s", label, rate.Label)
		}
		ov := zoneOptionView{
			Value:       z.ID,
			Label:       label,
			PriceSen:    rate.PriceSen,
			FreeOverSen: rate.FreeOverSen,
		}
		if rate.FreeOverSen > 0 {
			ov.Note = strings.ReplaceAll(ls.ShipFreeOver, "{amount}", senLabel(rate.FreeOverSen))
		}
		out = append(out, ov)
	}
	return out
}

// senLabel renders integer sen for display ("RM150.00"). Display only — every
// stored and charged amount stays an integer sen.
func senLabel(sen int64) string {
	return fmt.Sprintf("RM%.2f", float64(sen)/100)
}

// loadZoneSet reads ONE zone set of a form's account for the renderer (and for
// the resolution preview). A nil store, an unknown set or a read error yields the
// zero ZoneSet, so the renderer falls back to a labelled block with no options
// rather than failing the buyer's page — the SUBMIT path is where a missing set
// is a refusal.
func (h *PublicHandlers) loadZoneSet(ctx context.Context, accountID, setID string) shipping.ZoneSet {
	if h.Shipping == nil || accountID == "" || strings.TrimSpace(setID) == "" {
		return shipping.ZoneSet{}
	}
	set, found, err := h.Shipping.SetByID(ctx, []string{accountID}, strings.TrimSpace(setID))
	if err != nil || !found {
		return shipping.ZoneSet{}
	}
	return set
}

// firstNonEmpty returns the first non-blank value (trimmed), or "".
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// shippingFormRefs is the guard the save path uses to refuse deleting a zone set
// a form still points at (dbstore does the same reading inside its transaction).
func (h *RESTHandlers) shippingFormRefs(ctx context.Context, accounts []string, setID string) []dbstore.ShippingFormRef {
	if h.Shipping == nil {
		return nil
	}
	refs, err := h.Shipping.FormsUsingSet(ctx, accounts, setID)
	if err != nil {
		return nil
	}
	return refs
}
