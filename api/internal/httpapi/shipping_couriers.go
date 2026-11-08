// The submit path's courier source (phase 2).
//
// WHAT THIS FILE IS. Phase 1 taught a form's shipping block to charge a rate from
// the workspace's zone table. This file makes `source = "courier"` work: the
// block names a provider, the provider is asked for a real quotation at submit
// time, and the quoted fee is what the order charges. THE ZONE TABLE STAYS AS THE
// FALLBACK — a provider that is not connected, has no credentials, cannot cover
// the destination, fails or times out leaves the order chargeable at the zone
// rate, and the sale records WHICH SOURCE WON (see shippingDetailBlock).
//
// WHY THE QUOTE HAPPENS HERE AND NOT AT RENDER. A Lalamove quotation expires in
// five minutes (30 seconds, measured, on its sandbox) and neither provider has an
// idempotency key, so a quotation fetched when the page was painted would be dead
// by the time most buyers submit. The form therefore shows the ZONE rate as an
// ESTIMATE (and says so, see the render path), and the fee is quoted once, at
// submit, and reused for a duplicate of the same submission via shipping.QuoteCache.
//
// NOTHING HERE CAN BOOK. A quote is a price. No adapter in this phase submits an
// order, and no code path in this file calls anything but Quote.
package httpapi

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/wzul/jomform/api/internal/buyertext"
	"github.com/wzul/jomform/api/internal/shipping"
)

// Couriers is the shared courier wiring bundle, aliased from internal/shipping so
// the HTTP handlers read naturally. It is deliberately NOT defined in this package:
// mcpsrv must not import httpapi, and the MCP tools use the SAME type — one
// registry, one store, one credential set, one readiness rule.
type Couriers = shipping.Couriers

// resolveCourierBlocks resolves every courier-sourced block: it asks the provider,
// and falls back to the block's zone set when the courier cannot be used.
//
// A courier block with NO zone set and a failed quote is a REFUSAL
// (shipping.ErrCourierNoFallback) — never a silent 0, which is the defect the
// whole fallback design exists to prevent.
func (h *PublicHandlers) resolveCourierBlocks(ctx context.Context, tgt orderTarget, specs []shippingBlockSpec, in *submitInput, subtotalSen int64) (int64, []shippingDetailBlock, error) {
	if len(specs) == 0 {
		return 0, nil, nil
	}
	if tgt.Form == nil {
		return 0, nil, buyertext.New(buyertext.CodeShipFormNotFound, nil)
	}
	var total int64
	blocks := []shippingDetailBlock{}
	for _, spec := range specs {
		conn, ok := h.Couriers.Connected(ctx, tgt.AccountID, spec.Provider)
		// The zone set is read whether or not the courier is usable: it is the
		// estimate the buyer's form showed AND the fee that applies when the
		// courier loses.
		set, haveSet := h.courierFallbackSet(ctx, tgt.AccountID, spec)
		var fallback *shipping.ZoneSet
		if haveSet {
			fallback = &set
		}
		if !ok {
			// Not connected: the zone table charges the fee. Refusing instead
			// would break every form the moment a courier is unavailable, which
			// is exactly what "the zone table is the fallback" rules out.
			if fallback == nil {
				return 0, nil, shipping.ErrCourierNoFallback
			}
			quote, err := shipping.Resolve(set, addressFor(spec, in), subtotalSen, shipping.Options{
				OnNoMatch:    spec.OnNoMatch,
				ChosenZoneID: firstNonEmpty(in.Fields[spec.ID], in.Fields[spec.ItemID]),
				WeightG:      spec.WeightG,
			})
			if err != nil {
				return 0, nil, err
			}
			total += quote.PriceSen
			blocks = append(blocks, shippingDetailBlock{
				BlockID:          spec.ID,
				Source:           shipping.FeeSourceZones,
				Quote:            quote,
				Provider:         spec.Provider,
				EstimatePriceSen: quote.PriceSen,
				FallbackReason:   shipping.FallbackUnavailable,
			})
			continue
		}

		quote, err := shipping.ResolveCourier(ctx, h.courierRegistry(), shipping.CourierOptions{
			AccountID:      tgt.AccountID,
			Provider:       spec.Provider,
			Origin:         conn.Origin,
			OriginLat:      conn.OriginLat,
			OriginLng:      conn.OriginLng,
			Destination:    addressFor(spec, in),
			Fallback:       fallback,
			OnNoMatch:      spec.OnNoMatch,
			ChosenZoneID:   firstNonEmpty(in.Fields[spec.ID], in.Fields[spec.ItemID]),
			WeightG:        spec.WeightG,
			ParcelValueSen: spec.ParcelValueSen,
			SubtotalSen:    subtotalSen,
			ServiceType:    firstNonEmpty(spec.ServiceType, conn.ServiceType),
			Country:        firstNonEmpty(conn.OriginCountry, "MY"),
			Cache:          h.courierQuotes(),
		})
		if err != nil {
			// A refusal the buyer must see verbatim: no courier AND no zone table
			// to fall back to, or an address a courier cannot be asked about.
			return 0, nil, err
		}
		total += quote.PriceSen
		blocks = append(blocks, courierDetailBlock(spec.ID, spec.Provider, quote))
	}
	return total, blocks, nil
}

// courierFallbackSet reads the block's zone set (its fallback and the point a
// point-based provider is quoted to). A missing set, an unconfigured store or a
// read error yields haveSet=false: the caller then either refuses (no fallback at
// all) or quotes without an estimate.
func (h *PublicHandlers) courierFallbackSet(ctx context.Context, accountID string, spec shippingBlockSpec) (shipping.ZoneSet, bool) {
	setID := strings.TrimSpace(spec.ZoneSetID)
	if h.Shipping == nil || accountID == "" || setID == "" {
		return shipping.ZoneSet{}, false
	}
	set, found, err := h.Shipping.SetByID(ctx, []string{accountID}, setID)
	if err != nil || !found {
		return shipping.ZoneSet{}, false
	}
	return set, true
}

// courierRegistry / courierQuotes are nil-safe accessors: a deployment without the
// wiring behaves exactly like one whose credentials are missing.
func (h *PublicHandlers) courierRegistry() *shipping.Registry {
	return h.Couriers.Adapter()
}

func (h *PublicHandlers) courierQuotes() *shipping.QuoteCache {
	return h.Couriers.QuoteCache()
}

// courierDetailBlock renders one resolved courier block for the sale.
//
// price_sen keeps its phase-1 meaning — the fee this block CHARGED — and the
// embedded zone quote is the zone the estimate/fallback came from. When the
// provider won, that zone quote is the ESTIMATE the buyer saw; when it lost, it is
// the fee that applied and FallbackReason says why.
func courierDetailBlock(blockID, provider string, quote shipping.CourierQuote) shippingDetailBlock {
	block := shippingDetailBlock{
		BlockID:          blockID,
		Source:           quote.Source,
		Provider:         firstNonEmpty(quote.Provider, provider),
		Environment:      quote.Environment,
		ProviderQuoteID:  quote.ProviderQuoteID,
		ExpiresAt:        quote.ExpiresAt,
		ServiceName:      quote.ServiceName,
		CourierName:      quote.CourierName,
		EstimatePriceSen: quote.EstimatePriceSen,
		FallbackReason:   quote.FallbackReason,
		ProviderDetail:   quote.Detail,
	}
	if quote.Zone != nil {
		zone := *quote.Zone
		// The CHARGED fee is this block's price_sen, whichever source produced
		// it; the zone quote keeps everything else (zone name, rate, matched_by,
		// free-shipping reasoning).
		zone.PriceSen = quote.PriceSen
		block.Quote = zone
	} else {
		// No zone table (a courier-only block): the block still needs a price and
		// a match note so a consumer reading blocks[].price_sen is never misled by
		// a zero.
		block.Quote = shipping.Quote{
			PriceSen:     quote.PriceSen,
			ListPriceSen: quote.PriceSen,
		}
		block.ProviderCurrency = quote.Currency
	}
	return block
}

// resolveShipping resolves EVERY shipping block of an order — zone-sourced and
// courier-sourced — and returns the total fee plus the ONE shipping_detail JSON
// the sale records. The two sources share the detail shape so a sale always says
// where its money came from.
func (h *PublicHandlers) resolveShipping(ctx context.Context, tgt orderTarget, zoneSpecs, courierSpecs []shippingBlockSpec, in *submitInput, subtotalSen int64) (int64, json.RawMessage, error) {
	zoneTotal, zoneBlocks, err := h.resolveZoneBlocks(ctx, tgt, zoneSpecs, in, subtotalSen)
	if err != nil {
		return 0, nil, err
	}
	courierTotal, courierBlocks, err := h.resolveCourierBlocks(ctx, tgt, courierSpecs, in, subtotalSen)
	if err != nil {
		return 0, nil, err
	}
	blocks := append(zoneBlocks, courierBlocks...)
	return zoneTotal + courierTotal, marshalShippingDetail(blocks), nil
}

// shippingFeeItemIDs returns the line-item product ids a shipping fee must be
// dropped from the cart for: EVERY block whose rate source resolves the fee on the
// server (zones AND courier). A static block is untouched — its price stays a line
// item (issue #173/#461).
func shippingFeeItemIDs(zoneSpecs, courierSpecs []shippingBlockSpec) map[string]bool {
	out := map[string]bool{}
	for _, spec := range append(append([]shippingBlockSpec{}, zoneSpecs...), courierSpecs...) {
		out[spec.ItemID] = true
	}
	return out
}
