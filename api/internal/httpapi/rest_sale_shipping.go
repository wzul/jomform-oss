package httpapi

import (
	"encoding/json"

	"github.com/wzul/jomform/api/internal/shipping"
)

// shippingSourceSummary answers, from a sale's shipping_detail record, WHERE its
// delivery fee came from: which source WON, what to name it (the courier provider
// or the zone the address matched), and — when a courier was asked and did not
// price the sale — which KIND of failure sent it to the zone rate.
//
// It exists so the dashboard can state the provenance of a fee on the sale
// itself. Before this, the record was in the database and the drawer showed only
// an amount, which is not enough to answer "why was I charged this?".
//
// The names are the record's own vocabulary (shipping.FeeSourceCourier /
// FeeSourceZones, the provider's name, the zone's name, the fallback class), not
// a sentence: the dashboard translates them.
//
// A record this process cannot read yields empty strings rather than an error.
// The fee is already charged and stored; refusing to list the sale because its
// annotation is unreadable would turn an audit detail into an outage, and the
// drawer is written to say nothing rather than to invent a source.
func shippingSourceSummary(raw json.RawMessage) (source, sourceDetail, fallbackReason string) {
	if len(raw) == 0 {
		return "", "", ""
	}
	var doc struct {
		Source string `json:"source"`
		Blocks []struct {
			Source         string `json:"source"`
			Provider       string `json:"provider"`
			ZoneName       string `json:"zone_name"`
			FallbackReason string `json:"fallback_reason"`
		} `json:"blocks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", "", ""
	}
	source = doc.Source
	for _, b := range doc.Blocks {
		// The first block decides the label: every block in a sale belongs to the
		// same workspace and the same provider, and a sale whose blocks disagreed
		// would be a bug the record itself shows.
		//
		// Which NAME to use follows the block's OWN source, not whichever field is
		// populated: a fallen-back block still carries the provider it asked (that
		// is how the failure is explainable), so preferring `provider` whenever it
		// is set would label a zone-charged fee with the courier that failed.
		if sourceDetail == "" {
			blockSource := b.Source
			if blockSource == "" {
				blockSource = doc.Source
			}
			switch blockSource {
			case shipping.FeeSourceCourier:
				sourceDetail = b.Provider
			case shipping.FeeSourceZones:
				sourceDetail = b.ZoneName
			default:
				// A record from a shape this code does not know: name whatever it
				// does carry rather than nothing.
				if b.ZoneName != "" {
					sourceDetail = b.ZoneName
				} else {
					sourceDetail = b.Provider
				}
			}
		}
		// The first block that FELL BACK is the one worth naming: the reason is a
		// class ("unavailable" | "error" | "timeout"), never a vendor payload.
		if fallbackReason == "" && b.FallbackReason != "" {
			fallbackReason = b.FallbackReason
		}
	}
	return source, sourceDetail, fallbackReason
}
