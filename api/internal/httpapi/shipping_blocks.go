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
	"time"

	"github.com/wzul/jomform/api/internal/buyertext"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/formfields"
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
	// Provider is the courier a `source = "courier"` block asks for
	// (shipping.ProviderLalamove / ProviderEasyParcel). Empty makes the block
	// fall back to the zone table, because there is no provider to ask.
	Provider string
	// ServiceType is the provider's own service code (Lalamove: MOTORCYCLE /
	// CAR / VAN). Empty lets the adapter pick its default.
	ServiceType string
	// ParcelValueSen is the declared value of the goods, in sen (0 = unknown).
	// A courier needs it for insurance/COD pricing.
	ParcelValueSen int64
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
	// Card t_65a88197: a block's submitted key is formfields.SubmitKeys over
	// EVERY block — its own id, or the positional "_block_<i>" of a legacy block
	// that carries no id — which is the key both public renderers paint and the
	// order route charges. The derivation is over the whole array because the
	// positional key depends on the block's index, so this walk cannot skip the
	// non-shipping blocks.
	ids := make([]string, len(blocks))
	for i, raw := range blocks {
		if b, ok := raw.(map[string]any); ok {
			ids[i] = cfgString(b["id"])
		}
	}
	keys := formfields.SubmitKeys(ids)
	var out []shippingBlockSpec
	for i, raw := range blocks {
		b, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(cfgString(b["type"])) != "shipping" {
			continue
		}
		src := shipping.SourceStatic
		if s := strings.TrimSpace(cfgString(b["source"])); s != "" {
			src = s
		}
		if !want(src) {
			continue
		}
		// ID is the key the block's submitted value lands under. An id-less
		// block used to be skipped outright (`id == ""` → continue), which left
		// a zone/courier-sourced block with no identity at all: the page posted
		// its positional key as an item, nothing dropped it, and the order
		// answered 400 "produk tidak sah: _block_<i>" instead of resolving the
		// fee. keys[i] gives it the identity the renderer and the order route
		// already agree on.
		id := keys[i]
		itemID := formfields.PricingKey(cfgString(b["product_id"]), id)
		out = append(out, shippingBlockSpec{
			ID:             id,
			ItemID:         itemID,
			Source:         src,
			ZoneSetID:      strings.TrimSpace(cfgString(b["zone_set_id"])),
			StateField:     strings.TrimSpace(cfgString(b["zone_state_field"])),
			DistrictField:  strings.TrimSpace(cfgString(b["zone_district_field"])),
			PostcodeField:  strings.TrimSpace(cfgString(b["zone_postcode_field"])),
			OnNoMatch:      strings.TrimSpace(cfgString(b["on_no_match"])),
			WeightG:        cfgSen(b["weight_g"]),
			Provider:       shipping.Normalize(cfgString(b["courier_provider"])),
			ServiceType:    strings.TrimSpace(cfgString(b["courier_service_type"])),
			ParcelValueSen: cfgSen(b["parcel_value_sen"]),
			Required:       b["required"] == true,
			Label:          strings.TrimSpace(cfgString(b["value"])),
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
// can SEE which zone and rate applied instead of inferring it from a number, and
// (phase 2) WHICH SOURCE produced it — a courier quote or the workspace's own
// zone table.
type shippingDetail struct {
	Blocks []shippingDetailBlock `json:"blocks"`
	// Source is what produced the block fees as a whole: "courier" when every
	// block's fee came from a live quotation, "zones" when the zone table did
	// (a zone-sourced block, or a courier block whose quote fell back), and
	// "mixed" when both happened in one order. It answers "where did
	// sales.shipping_sen come from" without walking the blocks.
	Source string `json:"source,omitempty"`
	// Fallbacks counts the blocks whose courier quote fell back to the zone
	// table, so a fee that is NOT a live rate is visible at a glance.
	Fallbacks int `json:"fallbacks,omitempty"`
}

type shippingDetailBlock struct {
	BlockID string `json:"block_id"`
	// Source is "zones" or "courier" for THIS block; phase 1 wrote "zones" and
	// the shape is unchanged for it.
	Source string `json:"source"`
	// The zone-table answer, FLAT (the phase-1 shape): zone_id, zone_name,
	// rate_id, rate_label, price_sen, list_price_sen, free, matched_by, ...
	//
	// price_sen keeps ONE meaning across both sources — the fee this block
	// CHARGED. For a courier block the zone fields describe the zone the
	// estimate/fallback came from while price_sen is the amount actually
	// charged (the quoted fee when the courier won).
	shipping.Quote
	// Courier-only keys. They are omitted for a zones block, so phase 1's JSON
	// is unchanged.
	//
	// Provider is the courier that was ASKED (recorded even when it lost, which
	// is what makes a fallback explainable).
	Provider string `json:"provider,omitempty"`
	// Environment is the provider's environment as far as its credentials say
	// ("sandbox" / "prod" / "account"), so a SIMULATED fee is never mistaken for
	// a real one on a merchant's sale.
	Environment string `json:"environment,omitempty"`
	// ProviderQuoteID is the provider's own quotation id, and ExpiresAt when it
	// stops being usable. A quotation has no idempotency key, so this is the id
	// an operator needs to reconcile or dispute a fee.
	ProviderQuoteID string     `json:"provider_quote_id,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	ServiceName     string     `json:"service_name,omitempty"`
	CourierName     string     `json:"courier_name,omitempty"`
	// ProviderCurrency is the provider's own currency code when the block has no
	// zone table at all (so the fee's unit is still recorded).
	ProviderCurrency string `json:"provider_currency,omitempty"`
	// EstimatePriceSen is the zone rate the buyer's FORM showed for this block.
	// A courier block is quoted at checkout, so this is what the buyer saw and
	// price_sen is what they were charged; recording both makes the difference
	// explainable instead of surprising.
	EstimatePriceSen int64 `json:"estimate_price_sen,omitempty"`
	// FallbackReason is set only when a courier block's fee came from the zone
	// table: unavailable | error | timeout (a CLASS, never a vendor payload).
	FallbackReason string `json:"fallback_reason,omitempty"`
	// ProviderDetail is the provider's own price breakdown (integer sen inside).
	ProviderDetail map[string]any `json:"provider_detail,omitempty"`
}

// resolveZoneShipping resolves EVERY zone-sourced shipping block of the order
// target and returns the total fee in sen plus the JSON recorded on the sale.
//
// A form target is required: the blocks live in the form's config. subtotalSen
// is the ITEMS subtotal (line items only) — the free-shipping threshold compares
// against it, so the fee itself never counts towards the threshold.
//
// Every failure is a refusal (a *shippingError the caller turns into a 400), and
// never a silent 0: an unknown zone set, an address no zone covers with
// on_no_match=reject, a zone with no rate.
func (h *PublicHandlers) resolveZoneShipping(ctx context.Context, tgt orderTarget, specs []shippingBlockSpec, in *submitInput, subtotalSen int64) (int64, json.RawMessage, error) {
	total, blocks, err := h.resolveZoneBlocks(ctx, tgt, specs, in, subtotalSen)
	if err != nil {
		return 0, nil, err
	}
	return total, marshalShippingDetail(blocks), nil
}

// resolveZoneBlocks is the body of the zone resolution, in the form the combined
// (zone + courier) resolver needs: the per-block records, so ONE shipping_detail
// can carry both sources.
func (h *PublicHandlers) resolveZoneBlocks(ctx context.Context, tgt orderTarget, specs []shippingBlockSpec, in *submitInput, subtotalSen int64) (int64, []shippingDetailBlock, error) {
	if len(specs) == 0 {
		return 0, nil, nil
	}
	if tgt.Form == nil {
		return 0, nil, buyertext.New(buyertext.CodeShipFormNotFound, nil)
	}
	if h.Shipping == nil {
		return 0, nil, buyertext.New(buyertext.CodeShipZonesNotConfigured, nil)
	}
	accounts := []string{tgt.AccountID}

	// Sets are resolved once per distinct set id, so two blocks pointing at the
	// same set do not read it twice.
	cache := map[string]shipping.ZoneSet{}
	var total int64
	blocks := []shippingDetailBlock{}
	for _, spec := range specs {
		setID := strings.TrimSpace(spec.ZoneSetID)
		if setID == "" {
			return 0, nil, buyertext.New(buyertext.CodeShipBlockNotLinked, nil)
		}
		set, ok := cache[setID]
		if !ok {
			loaded, found, err := h.Shipping.SetByID(ctx, accounts, setID)
			if err != nil {
				return 0, nil, err
			}
			if !found {
				return 0, nil, buyertext.New(buyertext.CodeShipZoneSetNotFound, nil)
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
			return 0, nil, err
		}
		total += quote.PriceSen
		blocks = append(blocks, shippingDetailBlock{BlockID: spec.ID, Source: shipping.FeeSourceZones, Quote: quote})
	}
	return total, blocks, nil
}

// marshalShippingDetail renders the per-block records (and the summary of which
// SOURCE produced them) into the sale's shipping_detail JSON.
func marshalShippingDetail(blocks []shippingDetailBlock) json.RawMessage {
	if len(blocks) == 0 {
		return nil
	}
	detail := shippingDetail{Blocks: blocks}
	couriers, zones := 0, 0
	for _, b := range blocks {
		if b.Source == shipping.FeeSourceCourier {
			couriers++
		} else {
			zones++
		}
		if b.FallbackReason != "" {
			detail.Fallbacks++
		}
	}
	switch {
	case couriers > 0 && zones == 0:
		detail.Source = shipping.FeeSourceCourier
	case zones > 0 && couriers == 0:
		detail.Source = shipping.FeeSourceZones
	default:
		// Both sources charged a block of the same order (a courier block that
		// fell back beside a live quote, or a zone block beside a quoted one).
		detail.Source = "mixed"
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return nil
	}
	return raw
}

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
