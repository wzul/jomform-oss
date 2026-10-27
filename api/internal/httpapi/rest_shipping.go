// REST surface for the managed shipping zones + rates (the dashboard's Shipping
// tab in form settings).
//
// TWO ROUTES, NOT EIGHT. The zone/rate configuration is edited as ONE document
// in the dashboard (the merchant adds zones, rates and thresholds and saves
// once), so the read is a single GET and the write a single PUT that replaces
// the configuration in one transaction. That keeps the store's guarantees
// reachable from the UI (set ids are preserved across a save, so a form's
// reference never breaks, and a set a form still points at cannot be deleted)
// without a partial-save window in which a live form's block resolves to
// nothing.
//
// SCOPE. Both routes act on the account the REQUEST resolved to —
// ?workspace=<slug> resolves that workspace's own account (issue #158), which is
// exactly the account the form being edited belongs to. A session caller needs
// the manage-or-admin role in that workspace; an API key needs the write scope
// (requireWriteRole checks BOTH axes).
package httpapi

import (
	"net/http"
	"strings"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/shipping"
)

// shippingRateJSON is one rate row on the wire. Money is integer SEN.
type shippingRateJSON struct {
	Label       string `json:"label,omitempty"`
	PriceSen    int64  `json:"price_sen"`
	MinWeightG  int    `json:"min_weight_g"`
	MaxWeightG  int    `json:"max_weight_g"`
	FreeOverSen int64  `json:"free_over_sen"`
	Sort        int    `json:"sort"`
}

// shippingZoneJSON is one zone: its match rules and its rates.
type shippingZoneJSON struct {
	Name         string             `json:"name"`
	States       []string           `json:"states,omitempty"`
	Districts    []string           `json:"districts,omitempty"`
	PostcodeFrom string             `json:"postcode_from,omitempty"`
	PostcodeTo   string             `json:"postcode_to,omitempty"`
	Priority     int                `json:"priority"`
	IsFallback   bool               `json:"is_fallback,omitempty"`
	Rates        []shippingRateJSON `json:"rates"`
}

// shippingSetJSON is a named set of zones. `id` is sent back on a save so an
// existing set keeps its identity (and every form pointing at it keeps working);
// it is omitted for a new set.
type shippingSetJSON struct {
	ID        string             `json:"id,omitempty"`
	Name      string             `json:"name"`
	IsDefault bool               `json:"is_default,omitempty"`
	Zones     []shippingZoneJSON `json:"zones"`
}

// shippingListResp is the GET/PUT response.
type shippingListResp struct {
	AccountID string            `json:"account_id"`
	Sets      []shippingSetJSON `json:"sets"`
	// BlocksUsingZones counts the forms of this account whose shipping block
	// names a zone set. It is what tells the dashboard whether the managed
	// source is actually in use (and therefore whether deleting a set matters).
	FormsUsingZones int `json:"forms_using_zones"`
}

// shippingSetsJSON maps store rows onto the wire shape.
func shippingSetsJSON(sets []shipping.ZoneSet) []shippingSetJSON {
	out := make([]shippingSetJSON, 0, len(sets))
	for _, s := range sets {
		sj := shippingSetJSON{ID: s.ID, Name: s.Name, IsDefault: s.Default, Zones: []shippingZoneJSON{}}
		for _, z := range s.Zones {
			zj := shippingZoneJSON{
				Name: z.Name, States: z.States, Districts: z.Districts,
				PostcodeFrom: z.PostcodeFrom, PostcodeTo: z.PostcodeTo,
				Priority: z.Priority, IsFallback: z.Fallback, Rates: []shippingRateJSON{},
			}
			for _, r := range z.Rates {
				zj.Rates = append(zj.Rates, shippingRateJSON{
					Label: r.Label, PriceSen: r.PriceSen,
					MinWeightG: r.MinWeightG, MaxWeightG: r.MaxWeightG,
					FreeOverSen: r.FreeOverSen, Sort: r.Sort,
				})
			}
			sj.Zones = append(sj.Zones, zj)
		}
		out = append(out, sj)
	}
	return out
}

// shippingZoneSetsFromJSON parses the request body into resolver types.
func shippingZoneSetsFromJSON(in []shippingSetJSON) []shipping.ZoneSet {
	out := make([]shipping.ZoneSet, 0, len(in))
	for _, s := range in {
		set := shipping.ZoneSet{ID: strings.TrimSpace(s.ID), Name: s.Name, Default: s.IsDefault, Zones: []shipping.Zone{}}
		for _, z := range s.Zones {
			zone := shipping.Zone{
				Name: z.Name, States: z.States, Districts: z.Districts,
				PostcodeFrom: z.PostcodeFrom, PostcodeTo: z.PostcodeTo,
				Priority: z.Priority, Fallback: z.IsFallback, Rates: []shipping.Rate{},
			}
			for _, r := range z.Rates {
				zone.Rates = append(zone.Rates, shipping.Rate{
					Label: r.Label, PriceSen: r.PriceSen,
					MinWeightG: r.MinWeightG, MaxWeightG: r.MaxWeightG,
					FreeOverSen: r.FreeOverSen, Sort: r.Sort,
				})
			}
			set.Zones = append(set.Zones, zone)
		}
		out = append(out, set)
	}
	return out
}

// getShipping reads the account's managed shipping configuration.
// Route: GET /v1/rest/shipping
func (h *RESTHandlers) getShipping(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if h.Shipping == nil {
		writeErr(w, http.StatusServiceUnavailable, "kadar penghantaran tidak tersedia")
		return
	}
	sets, err := h.Shipping.Sets(r.Context(), []string{actx.Account.ID})
	if err != nil {
		respondErr(w, r, err)
		return
	}
	resp := shippingListResp{AccountID: actx.Account.ID, Sets: shippingSetsJSON(sets)}
	if n, cerr := h.Shipping.FormsUsingZones(r.Context(), []string{actx.Account.ID}); cerr == nil {
		resp.FormsUsingZones = n
	}
	respond(w, http.StatusOK, resp)
}

// putShipping replaces the account's managed shipping configuration in ONE
// transaction: sets named in the payload keep their ids (and every form's
// reference to them), new sets are inserted, and a set the payload omits is
// DELETED — refused with a 409 naming the forms when one still points at it.
// Route: PUT /v1/rest/shipping
func (h *RESTHandlers) putShipping(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if h.Shipping == nil {
		writeErr(w, http.StatusServiceUnavailable, "kadar penghantaran tidak tersedia")
		return
	}
	// Issue #645: a write needs the API-key write scope AND the manage-or-admin
	// role in the ?workspace= scope.
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	var in struct {
		Sets []shippingSetJSON `json:"sets"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "butiran tidak lengkap")
		return
	}
	sets := shippingZoneSetsFromJSON(in.Sets)
	if err := dbstore.ValidateShippingSets(sets); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Shipping.Replace(r.Context(), actx.Account.ID, sets); err != nil {
		if inUse, bad := err.(*dbstore.ShippingSetInUseError); bad {
			writeErr(w, http.StatusConflict, inUse.Error())
			return
		}
		if err == dbstore.ErrShippingSetNotFound {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		respondErr(w, r, err)
		return
	}
	saved, err := h.Shipping.Sets(r.Context(), []string{actx.Account.ID})
	if err != nil {
		respondErr(w, r, err)
		return
	}
	respond(w, http.StatusOK, shippingListResp{AccountID: actx.Account.ID, Sets: shippingSetsJSON(saved)})
}
