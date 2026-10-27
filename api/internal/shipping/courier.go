// Courier provider layer: the phase-2 rate source behind the SAME seam phase 1
// opened (`shipping` block `source = "courier"`).
//
// WHAT THIS PACKAGE ADDS. Phase 1 resolves a delivery fee from a workspace's own
// zone table. That is a policy the merchant types; it is not what a courier will
// actually charge. This file is the provider layer that asks a real courier for a
// price, and the ZONE TABLE STAYS AS THE FALLBACK: if the quote fails, times out,
// or the provider cannot cover the destination, the order is still charged the
// zone rate for the same address rather than refusing (or, worse, charging 0).
// WHICH ONE WON IS ALWAYS RECORDED — CourierQuote.Source is "courier" or "zones"
// and the reason a fallback applied is a CLASS ("unavailable", "error",
// "timeout"), never a vendor payload.
//
// IT IS STILL PURE. No database, no HTTP client of its own: an adapter is handed
// a Provider (a value built by cmd/jomform-api from the runtime credentials) and
// this file only decides WHICH provider is asked and WHAT the answer means. That
// is what makes the fallback rule testable without a network.
//
// MONEY IS INTEGER SEN. A provider's price arrives as a decimal (Lalamove:
// "8.2"; EasyParcel: 12.31) and is converted ONCE, by ParseAmountSen, with no
// float arithmetic — see money.go.
package shipping

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Courier providers this build knows how to talk to. The string is what a
// shipping block (and the connect surface) names, so it is part of the config
// contract: never rename one.
const (
	ProviderLalamove   = "lalamove"
	ProviderEasyParcel = "easyparcel"
)

// Fee sources recorded on a sale's shipping_detail. "courier" means a provider
// quoted this fee; "zones" means the workspace zone table did (either because
// the block asked for zones, or because the courier quote fell back to it).
const (
	FeeSourceCourier = "courier"
	FeeSourceZones   = "zones"
)

// Why a courier block fell back to the zone table. These are CLASSES: they are
// safe to store and show, and they never carry a vendor message, an HTTP body or
// any part of a credential.
const (
	// FallbackUnavailable: no credentials, not connected, or the provider does
	// not cover the destination (e.g. no coordinates for the stop, or an
	// intra-city courier asked for a nationwide parcel).
	FallbackUnavailable = "unavailable"
	// FallbackError: the provider answered with an error.
	FallbackError = "error"
	// FallbackTimeout: the provider did not answer inside the deadline.
	FallbackTimeout = "timeout"
	// FallbackNotConfigured: there was no zone table to fall back to. The order
	// is refused rather than charged 0 (see ErrCourierNoFallback).
	FallbackNotConfigured = "not_configured"
)

// Errors. The two refusals are buyer-facing (Malay, like every other public
// submit message here); the provider faults are internal and only ever surface
// as a fallback reason on the sale.
var (
	// ErrProviderUnknown: a block named a provider this build does not have.
	ErrProviderUnknown = errors.New("pembekal penghantaran ini tidak dikenali")
	// ErrProviderUnavailable: the provider has no usable credentials (a missing
	// env file, a production key Wan has not obtained yet) or is not connected
	// for this workspace. NEVER an error on the merchant's form — it is the
	// signal to fall back.
	ErrProviderUnavailable = errors.New("pembekal penghantaran ini belum disambung")
	// ErrProviderNoCoverage: the provider cannot quote this destination (a
	// Lalamove stop with no coordinates, an EasyParcel address outside MY).
	ErrProviderNoCoverage = errors.New("pembekal ini tidak meliputi alamat ini")
	// ErrProviderFault: the provider answered with an error.
	ErrProviderFault = errors.New("pembekal penghantaran gagal memberi sebut harga")
	// ErrCourierNoFallback: the quote failed AND the block declares no zone set
	// to fall back to. A refusal, never a silent 0.
	ErrCourierNoFallback = errors.New("sebut harga penghantaran gagal dan tiada kadar zon lalai")
	// ErrCourierAddress: the courier block could not be given a usable address.
	ErrCourierAddress = errors.New("alamat penghantaran tidak lengkap untuk sebut harga")
)

// Dimensions is a parcel's size in centimetres (0 = unknown). Only EasyParcel
// uses it today; Lalamove prices by the route, not the parcel.
type Dimensions struct {
	WidthCM  float64
	LengthCM float64
	HeightCM float64
}

// QuoteRequest is everything a provider needs to price one delivery. It is built
// from the buyer's submitted address plus the workspace's courier configuration
// — never from anything the buyer can set directly (a buyer must not be able to
// choose the courier they are charged for).
type QuoteRequest struct {
	// AccountID is the workspace the quote is made for; adapters use it to find
	// their own connection/token.
	AccountID string
	// Origin is WHERE THE PARCEL SHIPS FROM: the merchant's pickup address from
	// the courier connection (postcode + state) and, for a point-based provider,
	// its coordinates.
	Origin Address
	// Destination is the buyer's submitted address.
	Destination Address
	// OriginLat / OriginLng and DestinationLat / DestinationLng are the points a
	// point-based provider (Lalamove) needs. Empty means unknown, which for such
	// a provider is ErrProviderNoCoverage (fall back), never a guessed point.
	OriginLat      string
	OriginLng      string
	DestinationLat string
	DestinationLng string
	// WeightG is the parcel weight in GRAMS (0 = unknown). Providers that price
	// by weight are given it in their own unit by the adapter.
	WeightG int64
	// ParcelValueSen is the declared value of the goods, in sen.
	ParcelValueSen int64
	// ServiceType is the provider's own service code (Lalamove: MOTORCYCLE,
	// CAR, VAN). Empty lets the adapter pick its default.
	ServiceType string
	// Dimensions is the parcel size (optional).
	Dimensions Dimensions
	// Country is the ISO-3166 alpha-2 country of BOTH stops today ("MY"). A
	// provider that does not serve it answers ErrProviderNoCoverage.
	Country string
}

// ProviderQuote is one provider's answer: the price, the identifier that ties it
// to the provider's own record, and when it stops being usable.
type ProviderQuote struct {
	Provider string
	// Environment is "sandbox" or "prod" as far as the adapter can tell from
	// its own credentials. It is recorded on the sale so a simulated fee is
	// never mistaken for a real one.
	Environment string
	// PriceSen is the fee in integer sen.
	PriceSen int64
	// Currency is the provider's currency code (MYR).
	Currency string
	// QuoteID is the provider's quotation id. Lalamove's is what an order is
	// placed with, so it is stored: a quotation has no idempotency key, and two
	// orders must never be placed from one buyer intent.
	QuoteID string
	// ExpiresAt is when the provider says the quotation stops being usable.
	// Lalamove: 5 minutes per its docs (measured 30 seconds on sandbox). Zero
	// means the provider did not say.
	ExpiresAt time.Time
	// ServiceName / CourierName are the human labels to record ("Aramex",
	// "MOTORCYCLE").
	ServiceName string
	CourierName string
	// Detail holds the provider's own price components, already normalized to
	// integer sen where they are money (keys ending in "_sen" are int64). It is
	// what makes a disputed fee explainable.
	Detail map[string]any
}

// Provider is one courier integration. Implementations must be safe for
// concurrent use and must NEVER log or return a credential value.
type Provider interface {
	// Name is the registry key (ProviderLalamove / ProviderEasyParcel).
	Name() string
	// Environment reports "sandbox", "prod" or "" as far as the adapter can tell.
	Environment() string
	// Available is false when the adapter has no usable credentials or token.
	// It is a QUESTION, not an error: the caller falls back.
	Available() bool
	// Quote prices one delivery. It returns ErrProviderUnavailable,
	// ErrProviderNoCoverage, ErrProviderFault or a context error — never a
	// credential and never a raw vendor body.
	Quote(ctx context.Context, req QuoteRequest) (ProviderQuote, error)
}

// Registry is the provider lookup. It is built once at startup from whatever
// credentials exist, so a deployment with none still has a working form: every
// provider simply reports Available() == false and the zone table is used.
type Registry struct {
	providers map[string]Provider
	order     []string
}

// NewRegistry builds a registry from the given providers (nil entries ignored).
func NewRegistry(providers ...Provider) *Registry {
	r := &Registry{providers: map[string]Provider{}}
	for _, p := range providers {
		if p == nil {
			continue
		}
		if _, dup := r.providers[p.Name()]; dup {
			continue
		}
		r.providers[p.Name()] = p
		r.order = append(r.order, p.Name())
	}
	return r
}

// Get returns a registered provider by name.
func (r *Registry) Get(name string) (Provider, bool) {
	if r == nil {
		return nil, false
	}
	p, ok := r.providers[Normalize(name)]
	return p, ok
}

// Names returns the registered provider names in a stable order.
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.order...)
}

// Each calls fn for every provider (used by the connect surface to report
// status). A nil registry has no providers.
func (r *Registry) Each(fn func(Provider)) {
	if r == nil {
		return
	}
	for _, name := range r.order {
		fn(r.providers[name])
	}
}

// CourierOptions is the per-block input to ResolveCourier.
type CourierOptions struct {
	// Provider names the courier the block asks for (ProviderLalamove,
	// ProviderEasyParcel).
	Provider string
	// Origin is the merchant's pickup address; Destination the buyer's address.
	Origin      Address
	Destination Address
	// OriginLat/OriginLng come from the courier connection (the merchant's
	// pickup point). DestinationLat/DestinationLng override the drop-off point;
	// LEFT EMPTY they are taken from the zone the address matched (Zone.Lat/Lng),
	// which is the point a merchant mapped their own zone to. Neither is ever
	// invented: a point-based provider with no point refuses and the zone rate
	// applies.
	OriginLat      string
	OriginLng      string
	DestinationLat string
	DestinationLng string
	// Fallback is the block's zone set (nil when the block names none). It is
	// the ESTIMATE the buyer's form showed AND the fee that applies when the
	// quote fails.
	Fallback *ZoneSet
	// OnNoMatch / ChosenZoneID are the phase-1 zone resolution options, used for
	// the fallback and the estimate.
	OnNoMatch    string
	ChosenZoneID string
	// WeightG is the declared parcel weight in grams (0 = unknown).
	WeightG int64
	// ParcelValueSen is the declared value of the goods, in sen.
	ParcelValueSen int64
	// SubtotalSen is the order's items subtotal, which the zone free-shipping
	// threshold compares against.
	SubtotalSen int64
	// ServiceType is the provider's own service code.
	ServiceType string
	// Country is the ISO alpha-2 country of the stops (defaults to "MY").
	Country string
	// Timeout bounds the provider call. A quote holds up a checkout, so it is
	// short (see DefaultCourierTimeout) and a timeout falls back rather than
	// failing the order.
	Timeout time.Duration
	// AccountID is the workspace the quote belongs to.
	AccountID string
	// Cache, when set, reuses a still-valid quotation for an IDENTICAL request so
	// a double-submitted checkout does not create two quotations for one buyer
	// intent (neither provider has an idempotency key — see quotecache.go).
	// Nil means "always ask".
	Cache *QuoteCache
}

// DefaultCourierTimeout bounds a provider quote. A live quote sits in the
// buyer's checkout path, so it must never be able to hang the submit; anything
// slower than this falls back to the zone rate and the sale records why.
const DefaultCourierTimeout = 6 * time.Second

// CourierQuote is the resolved courier fee plus everything the sale records about
// how it was reached: which SOURCE won, which provider answered, the provider's
// own quotation id and expiry, the zone estimate the buyer was shown, and — when
// the zone table won — the reason the provider lost.
type CourierQuote struct {
	// Source is FeeSourceCourier or FeeSourceZones. It is the answer to "who
	// produced shipping_sen".
	Source string `json:"source"`
	// Provider is the courier that was ASKED (even when it lost, so a fallback
	// is explainable).
	Provider string `json:"provider,omitempty"`
	// Environment is the provider's environment as far as its credentials say
	// ("sandbox" / "prod"), so a simulated quote is never mistaken for a real one.
	Environment string `json:"environment,omitempty"`
	// PriceSen is the fee actually charged.
	PriceSen int64 `json:"price_sen"`
	// EstimatePriceSen is the zone rate the buyer's form showed for this block
	// (0 when the block has no zone table). A courier block is quoted at
	// checkout, so this is what the buyer saw and the quote is what they pay.
	EstimatePriceSen int64 `json:"estimate_price_sen,omitempty"`
	// ProviderQuoteID is the quotation the fee came from.
	ProviderQuoteID string `json:"provider_quote_id,omitempty"`
	// ExpiresAt is when that quotation stops being usable.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	// ServiceName / CourierName are the provider's labels.
	ServiceName string `json:"service_name,omitempty"`
	CourierName string `json:"courier_name,omitempty"`
	Currency    string `json:"currency,omitempty"`
	// FallbackReason is set only when Source is FeeSourceZones and the block
	// asked for a courier: FallbackUnavailable, FallbackError or FallbackTimeout.
	FallbackReason string `json:"fallback_reason,omitempty"`
	// Zone is the zone-table quote that produced the fee (or the estimate) —
	// embedded so the sale keeps the same shape phase 1 wrote.
	Zone *Quote `json:"zone,omitempty"`
	// Detail is the provider's price breakdown plus the quote bookkeeping.
	Detail map[string]any `json:"detail,omitempty"`
}

// ResolveCourier answers the fee for one courier-sourced shipping block.
//
// THE RULE, in order, and every branch is observable on the returned quote:
//
//  1. The block's zone set (when it names one) is resolved FIRST, for two
//     reasons: it is the estimate the buyer's form showed, and it is the fee
//     that applies if the courier cannot be used. A zone set that resolves to an
//     error is not fatal here (the courier may still answer) — the error is only
//     returned if the courier ALSO fails, which is when there is nothing left to
//     charge.
//  2. The provider is looked up. An unknown/unavailable provider is a FALLBACK
//     (reason "unavailable"), not an error: a missing credential must never stop
//     a merchant's form from taking orders.
//  3. The provider is asked, under opt.Timeout. A context deadline is "timeout",
//     anything else is "error". Both fall back.
//  4. A provider answer WINS: PriceSen is the quoted fee, Source is "courier",
//     and the quotation id + expiry are recorded.
//  5. When the courier lost and the block names no zone set, the order is
//     REFUSED with ErrCourierNoFallback — never charged 0.
//
// It returns ErrCourierAddress for a destination the buyer did not supply at all
// (no postcode AND no state), because a courier cannot be asked to price "anywhere".
func ResolveCourier(ctx context.Context, reg *Registry, opts CourierOptions) (CourierQuote, error) {
	if opts.Country == "" {
		opts.Country = "MY"
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultCourierTimeout
	}
	out := CourierQuote{Provider: Normalize(opts.Provider), Currency: "MYR"}

	// (1) The zone table: estimate + fallback, both best-effort.
	var zoneQuote Quote
	var zoneErr error
	haveZone := opts.Fallback != nil && len(opts.Fallback.Zones) > 0
	if haveZone {
		zoneQuote, zoneErr = Resolve(*opts.Fallback, opts.Destination, opts.SubtotalSen, Options{
			OnNoMatch:    opts.OnNoMatch,
			ChosenZoneID: opts.ChosenZoneID,
			WeightG:      opts.WeightG,
		})
		if zoneErr == nil {
			out.EstimatePriceSen = zoneQuote.PriceSen
			zq := zoneQuote
			out.Zone = &zq
			// The drop-off point of a point-based provider is the matched zone's
			// own point. It is read HERE, from the same match the fee came from,
			// so the point a courier is quoted to is always the zone the buyer's
			// address actually landed in.
			if opts.DestinationLat == "" && opts.DestinationLng == "" {
				if z, ok := ZoneByID(*opts.Fallback, zoneQuote.ZoneID); ok {
					opts.DestinationLat, opts.DestinationLng = z.Lat, z.Lng
				}
			}
		}
	}
	// fallbackToZones charges the zone rate when the courier lost, or refuses
	// when there is nothing to fall back to.
	fallbackToZones := func(reason string) (CourierQuote, error) {
		if zoneErr != nil {
			return CourierQuote{}, zoneErr
		}
		if !haveZone {
			return CourierQuote{}, ErrCourierNoFallback
		}
		out.Source = FeeSourceZones
		out.PriceSen = zoneQuote.PriceSen
		out.FallbackReason = reason
		out.Currency = ""
		return out, nil
	}
	// A destination that says nothing at all cannot be quoted and cannot be
	// zoned either: refuse before asking anyone (a courier asked for "anywhere"
	// would price a point we invented).
	if opts.Destination.Empty() {
		return CourierQuote{}, ErrCourierAddress
	}

	// (2) Which provider.
	provider, ok := reg.Get(opts.Provider)
	if !ok || provider == nil {
		if opts.Provider == "" {
			return fallbackToZones(FallbackUnavailable)
		}
		if haveZone {
			return fallbackToZones(FallbackUnavailable)
		}
		return CourierQuote{}, ErrProviderUnknown
	}
	out.Environment = provider.Environment()
	if !provider.Available() {
		return fallbackToZones(FallbackUnavailable)
	}

	// (3) Ask, under a deadline that cannot outlive the checkout.
	qctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	request := QuoteRequest{
		AccountID:      opts.AccountID,
		Origin:         opts.Origin,
		Destination:    opts.Destination,
		OriginLat:      opts.OriginLat,
		OriginLng:      opts.OriginLng,
		DestinationLat: opts.DestinationLat,
		DestinationLng: opts.DestinationLng,
		WeightG:        opts.WeightG,
		ParcelValueSen: opts.ParcelValueSen,
		ServiceType:    opts.ServiceType,
		Country:        opts.Country,
	}
	// A quotation is reused for an identical request while it is still valid, so
	// a double-submitted checkout cannot create two quotations (and, later, two
	// bookings) for one buyer intent.
	var pq ProviderQuote
	if cached, ok := opts.Cache.Get(request); ok {
		pq = cached
	} else {
		var qerr error
		pq, qerr = provider.Quote(qctx, request)
		if qerr != nil {
			if errors.Is(qerr, context.DeadlineExceeded) || errors.Is(qerr, context.Canceled) {
				return fallbackToZones(FallbackTimeout)
			}
			return fallbackToZones(FallbackError)
		}
		opts.Cache.Put(request, pq)
	}

	// (4) The courier wins.
	out.Source = FeeSourceCourier
	out.PriceSen = pq.PriceSen
	out.Provider = firstNonBlank(pq.Provider, out.Provider)
	out.Environment = firstNonBlank(pq.Environment, out.Environment)
	out.ProviderQuoteID = pq.QuoteID
	out.ServiceName = pq.ServiceName
	out.CourierName = pq.CourierName
	out.Currency = firstNonBlank(pq.Currency, "MYR")
	if !pq.ExpiresAt.IsZero() {
		exp := pq.ExpiresAt.UTC()
		out.ExpiresAt = &exp
	}
	return out, nil
}

// firstNonBlank returns a, or b when a is blank.
func firstNonBlank(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ProviderStatus is what the connect surface reports about one provider. It
// carries NO credential: only whether the deployment has usable credentials and
// where they came from (a path is configuration, not a secret).
type ProviderStatus struct {
	Name        string `json:"name"`
	Environment string `json:"environment,omitempty"`
	// CredentialsPresent is true when the runtime credential source for this
	// provider is configured. It says nothing about the value.
	CredentialsPresent bool `json:"credentials_present"`
	// CredentialsHint names WHERE the credentials are read from (the env file
	// path or the env var names), so an operator can fix a missing one.
	CredentialsHint string `json:"credentials_hint,omitempty"`
	// NeedsBrowser is true for a provider whose connect step is an OAuth
	// consent a HUMAN must complete (EasyParcel). Such a provider is connected
	// by handing the merchant a link, never by a server-side call.
	NeedsBrowser bool `json:"needs_browser"`
	// PointBased is true for a provider that needs coordinates for both stops.
	PointBased bool `json:"point_based"`
	// DocURL is where the merchant reads about this provider.
	DocURL string `json:"doc_url,omitempty"`
}

// ProviderStatuses reports every registered provider, plus any provider this
// build knows about but could not build (e.g. EasyParcel with no client id) so
// the dashboard can say "not configured yet" rather than hiding it.
func ProviderStatuses(reg *Registry) []ProviderStatus {
	known := map[string]ProviderStatus{
		ProviderLalamove: {
			Name: ProviderLalamove, NeedsBrowser: false, PointBased: true,
			CredentialsHint: "LALAMOVE_SANDBOX_API_KEY / LALAMOVE_SANDBOX_API_SECRET (env file)",
			DocURL:          "https://developers.lalamove.com/",
		},
		ProviderEasyParcel: {
			Name: ProviderEasyParcel, NeedsBrowser: true, PointBased: false,
			CredentialsHint: "EASYPARCEL_CLIENT_ID / EASYPARCEL_CLIENT_SECRET (env file)",
			DocURL:          "https://developer.easyparcel.com/",
		},
	}
	out := make([]ProviderStatus, 0, len(known))
	names := []string{ProviderLalamove, ProviderEasyParcel}
	for _, name := range names {
		st := known[name]
		if p, ok := reg.Get(name); ok && p != nil {
			st.Environment = p.Environment()
			st.CredentialsPresent = p.Available()
		}
		out = append(out, st)
	}
	return out
}

// describeProviderError is a small helper used by tests and logs: it renders a
// provider error as one of the fallback classes without ever including a
// credential or a raw vendor payload.
func describeProviderError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrProviderUnavailable):
		return FallbackUnavailable
	case errors.Is(err, ErrProviderNoCoverage), errors.Is(err, ErrProviderUnknown):
		return FallbackUnavailable
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return FallbackTimeout
	default:
		return FallbackError
	}
}

// String renders a quote for logs and error messages. It deliberately contains
// no credential material.
func (q CourierQuote) String() string {
	via := q.Source
	if q.Provider != "" {
		via = fmt.Sprintf("%s/%s", q.Provider, q.Source)
	}
	if q.FallbackReason != "" {
		via += " (" + q.FallbackReason + ")"
	}
	return fmt.Sprintf("fee %d sen via %s", q.PriceSen, via)
}
