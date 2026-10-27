package shipping

import (
	"errors"
	"testing"
)

// peninsularAndEast is the configuration the card's acceptance describes: one
// zone for Peninsular Malaysia (RM8.00, free over RM150) and one for East
// Malaysia (RM15.00), plus a fallback zone for addresses neither covers.
func peninsularAndEast() ZoneSet {
	return ZoneSet{
		ID: "set1", Name: "Malaysia", Default: true,
		Zones: []Zone{
			{
				ID: "z_west", Name: "West Malaysia", States: []string{"peninsular"}, Priority: 0,
				Rates: []Rate{{ID: "r_west", Label: "Standard", PriceSen: 800, FreeOverSen: 15000}},
			},
			{
				ID: "z_east", Name: "East Malaysia", States: []string{"east_malaysia"}, Priority: 1,
				Rates: []Rate{{ID: "r_east", Label: "Standard", PriceSen: 1500}},
			},
			{
				ID: "z_rest", Name: "Rest of world", Fallback: true,
				Rates: []Rate{{ID: "r_rest", Label: "Standard", PriceSen: 5000}},
			},
		},
	}
}

// TestResolvePeninsularAndEastMalaysiaDiffer is the card's headline acceptance:
// two submissions of the same cart, from a Peninsular and an East Malaysian
// address, are charged DIFFERENT fees — and each fee is the rate row of the zone
// the ADDRESS resolved to, not the one the buyer picked.
func TestResolvePeninsularAndEastMalaysiaDiffer(t *testing.T) {
	set := peninsularAndEast()

	west, err := Resolve(set, Address{State: "Selangor", Postcode: "40150"}, 5000, Options{})
	if err != nil {
		t.Fatalf("west: %v", err)
	}
	east, err := Resolve(set, Address{State: "Sabah", Postcode: "88000"}, 5000, Options{})
	if err != nil {
		t.Fatalf("east: %v", err)
	}
	if west.PriceSen == east.PriceSen {
		t.Fatalf("peninsular and east Malaysia were charged the same fee (%d)", west.PriceSen)
	}
	if west.PriceSen != 800 || west.ZoneID != "z_west" || west.RateID != "r_west" {
		t.Errorf("peninsular quote = %+v, want zone z_west rate r_west 800", west)
	}
	if east.PriceSen != 1500 || east.ZoneID != "z_east" {
		t.Errorf("east quote = %+v, want zone z_east 1500", east)
	}
	if west.MatchedBy != "state" || east.MatchedBy != "state" {
		t.Errorf("matched_by = %q / %q, want state / state (the rule that fired is recorded)",
			west.MatchedBy, east.MatchedBy)
	}
}

// TestStateSpellingsAndRegionsMatch: a zone rule written "peninsular" must match
// every Peninsular state in any spelling a buyer might submit, and a rule naming
// one state must match that state alone.
func TestStateSpellingsAndRegionsMatch(t *testing.T) {
	cases := []struct {
		rule string
		addr string
		want bool
	}{
		{"peninsular", "Selangor", true},
		{"peninsular", "penang", true},
		{"Peninsular Malaysia", "Pulau Pinang", true},
		{"west malaysia", "Wilayah Kuala Lumpur", true},
		{"west malaysia", "KL", true},
		{"east_malaysia", "Sarawak", true},
		{"east_malaysia", "Labuan", true},
		{"east malaysia", "sabah", true},
		{"east_malaysia", "Selangor", false},
		{"peninsular", "Sabah", false},
		{"Johor", "johor", true},
		{"Malacca", "Melaka", true},
		{"Melaka", "malacca", true},
		{"Kuala Lumpur", "KL", true},
		// An unknown token still matches itself exactly (a merchant's own
		// region label), so a custom zone is possible without a code change.
		{"Northern Region", "Northern Region", true},
		{"Northern Region", "Southern Region", false},
	}
	for _, c := range cases {
		set := ZoneSet{Zones: []Zone{{ID: "z", Name: "Z", States: []string{c.rule},
			Rates: []Rate{{ID: "r", PriceSen: 100}}}}}
		_, _, got := MatchZone(set, Address{State: c.addr})
		if got != c.want {
			t.Errorf("rule %q vs address %q: match=%v, want %v", c.rule, c.addr, got, c.want)
		}
	}
}

// TestMatchByPostcodeRangeAndDistrictAndPriority: a postcode exception zone
// (priority 0) must win over the broad state zone it sits inside, and a numeric
// range must not swallow a non-numeric postcode.
func TestMatchByPostcodeRangeAndDistrictAndPriority(t *testing.T) {
	set := ZoneSet{Zones: []Zone{
		{ID: "z_broad", Name: "Selangor", States: []string{"selangor"}, Priority: 1,
			Rates: []Rate{{ID: "rb", PriceSen: 900}}},
		{ID: "z_island", Name: "Pulau Ketam", PostcodeFrom: "42940", PostcodeTo: "42960", Priority: 0,
			Rates: []Rate{{ID: "ri", PriceSen: 2500}}},
		{ID: "z_district", Name: "Putrajaya", Districts: []string{"Putrajaya"}, Priority: 2,
			Rates: []Rate{{ID: "rd", PriceSen: 700}}},
	}}

	z, by, ok := MatchZone(set, Address{State: "Selangor", Postcode: "42950"})
	if !ok || z.ID != "z_island" || by != "postcode" {
		t.Fatalf("postcode exception: got zone %q by %q ok=%v, want z_island/postcode", z.ID, by, ok)
	}
	z, by, ok = MatchZone(set, Address{State: "Selangor", Postcode: "40150"})
	if !ok || z.ID != "z_broad" || by != "state" {
		t.Fatalf("broad state zone: got zone %q by %q ok=%v, want z_broad/state", z.ID, by, ok)
	}
	z, by, ok = MatchZone(set, Address{District: "putrajaya"})
	if !ok || z.ID != "z_district" || by != "district" {
		t.Fatalf("district match: got zone %q by %q ok=%v, want z_district/district", z.ID, by, ok)
	}
	// A non-numeric postcode must NOT fall inside a numeric range.
	if _, _, ok := MatchZone(set, Address{Postcode: "SW1A"}); ok {
		t.Fatal("a non-numeric postcode matched a numeric range")
	}
	// Single-value range (no `to`).
	single := ZoneSet{Zones: []Zone{{ID: "z_one", Name: "One", PostcodeFrom: "50000",
		Rates: []Rate{{ID: "r", PriceSen: 100}}}}}
	if _, _, ok := MatchZone(single, Address{Postcode: "50000"}); !ok {
		t.Error("single-value postcode rule did not match its own value")
	}
	if _, _, ok := MatchZone(single, Address{Postcode: "50001"}); ok {
		t.Error("single-value postcode rule matched a different postcode")
	}
}

// TestNoZoneMatchIsExplicitNeverZero is the "never a silent 0" rule: with the
// default policy the order is REFUSED, with on_no_match=default it is charged
// the fallback zone's rate, and a fallback policy with no fallback zone is a
// refusal too.
func TestNoZoneMatchIsExplicitNeverZero(t *testing.T) {
	set := peninsularAndEast()

	if _, err := Resolve(set, Address{State: "Antarctica"}, 5000, Options{}); !errors.Is(err, ErrNoZone) {
		t.Fatalf("unmatched address with the default policy: err=%v, want ErrNoZone", err)
	}
	if _, err := Resolve(set, Address{State: "Antarctica"}, 5000,
		Options{OnNoMatch: OnNoMatchReject}); !errors.Is(err, ErrNoZone) {
		t.Fatalf("explicit reject: err=%v, want ErrNoZone", err)
	}
	q, err := Resolve(set, Address{State: "Antarctica"}, 5000, Options{OnNoMatch: OnNoMatchDefault})
	if err != nil {
		t.Fatalf("fallback policy: %v", err)
	}
	if q.ZoneID != "z_rest" || q.PriceSen != 5000 || q.MatchedBy != "fallback" {
		t.Errorf("fallback quote = %+v, want z_rest 5000 matched_by=fallback", q)
	}
	// A fallback zone is NEVER a direct match, even when its rules would match:
	// the policy decides, not the address.
	fbDirect := ZoneSet{Zones: []Zone{
		{ID: "z_fb", Name: "Fallback", States: []string{"selangor"}, Fallback: true,
			Rates: []Rate{{ID: "r", PriceSen: 100}}},
	}}
	if _, err := Resolve(fbDirect, Address{State: "Selangor"}, 0, Options{}); !errors.Is(err, ErrNoZone) {
		t.Fatalf("a fallback zone matched directly: err=%v, want ErrNoZone", err)
	}

	noFallback := ZoneSet{Zones: []Zone{{ID: "z_west", Name: "West", States: []string{"peninsular"},
		Rates: []Rate{{ID: "r", PriceSen: 800}}}}}
	if _, err := Resolve(noFallback, Address{State: "Sabah"}, 0,
		Options{OnNoMatch: OnNoMatchDefault}); !errors.Is(err, ErrNoFallback) {
		t.Fatalf("default policy without a fallback zone: err=%v, want ErrNoFallback", err)
	}
	// A TYPO in the policy must behave like reject, never like "free".
	if _, err := Resolve(set, Address{State: "Antarctica"}, 0,
		Options{OnNoMatch: "defualt"}); !errors.Is(err, ErrNoZone) {
		t.Fatalf("mistyped policy: err=%v, want ErrNoZone", err)
	}
}

// TestFreeShippingThresholdIsRecorded: crossing the threshold charges 0 AND
// records why; below it the full rate applies.
func TestFreeShippingThresholdIsRecorded(t *testing.T) {
	set := peninsularAndEast()
	addr := Address{State: "Selangor"}

	under, err := Resolve(set, addr, 14999, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if under.PriceSen != 800 || under.Free || under.FreeOverSen != 0 {
		t.Errorf("subtotal below the threshold = %+v, want 800 / not free / no threshold", under)
	}
	at, err := Resolve(set, addr, 15000, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if at.PriceSen != 0 || !at.Free || at.FreeOverSen != 15000 {
		t.Errorf("subtotal AT the threshold = %+v, want 0 / free / 15000 recorded", at)
	}
	if at.ListPriceSen != 800 {
		t.Errorf("free quote lost the list price (%d) — the merchant must see what was waived", at.ListPriceSen)
	}
	over, err := Resolve(set, addr, 90000, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if over.PriceSen != 0 || !over.Free {
		t.Errorf("above the threshold = %+v, want free", over)
	}
	// The EAST zone's rate has NO threshold: the same big cart still pays 1500.
	east, err := Resolve(set, Address{State: "Sabah"}, 90000, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if east.PriceSen != 1500 || east.Free {
		t.Errorf("east with no threshold = %+v, want 1500 not free", east)
	}
}

// TestChosenZoneMustAgreeWithTheAddress: the address is the price authority. A
// zone choice that disagrees is REFUSED (the buyer's screen showed the other
// price); with no address at all the choice is the only input and is honoured.
func TestChosenZoneMustAgreeWithTheAddress(t *testing.T) {
	set := peninsularAndEast()

	agree, err := Resolve(set, Address{State: "Selangor"}, 0, Options{ChosenZoneID: "z_west"})
	if err != nil || agree.ZoneID != "z_west" {
		t.Fatalf("agreeing choice: %+v err=%v", agree, err)
	}
	if _, err := Resolve(set, Address{State: "Selangor"}, 0, Options{ChosenZoneID: "z_east"}); !errors.Is(err, ErrZoneMismatch) {
		t.Fatalf("disagreeing choice: err=%v, want ErrZoneMismatch", err)
	}
	if _, err := Resolve(set, Address{State: "Selangor"}, 0, Options{ChosenZoneID: "nope"}); !errors.Is(err, ErrZoneUnknown) {
		t.Fatalf("unknown zone id: err=%v, want ErrZoneUnknown", err)
	}

	choice, err := Resolve(set, Address{}, 0, Options{ChosenZoneID: "z_east"})
	if err != nil {
		t.Fatalf("choice with no address: %v", err)
	}
	if choice.ZoneID != "z_east" || choice.MatchedBy != "choice" {
		t.Errorf("choice with no address = %+v, want z_east matched_by=choice", choice)
	}
	// No address and no choice is a refusal, not a free order.
	if _, err := Resolve(set, Address{}, 0, Options{}); !errors.Is(err, ErrNoZone) {
		t.Fatalf("no address and no choice: err=%v, want ErrNoZone", err)
	}
}

// TestWeightBandsPickTheCoveringRate: a declared parcel weight selects the rate
// whose band contains it; a weight outside every band is a refusal, and a zone
// that does not band at all keeps its first rate whatever the weight.
func TestWeightBandsPickTheCoveringRate(t *testing.T) {
	set := ZoneSet{Zones: []Zone{{
		ID: "z", Name: "West", States: []string{"peninsular"},
		Rates: []Rate{
			{ID: "r_small", Label: "0-2kg", PriceSen: 800, MinWeightG: 1, MaxWeightG: 2000, Sort: 0},
			{ID: "r_big", Label: "2-10kg", PriceSen: 1500, MinWeightG: 2001, MaxWeightG: 10000, Sort: 1},
		},
	}}}
	addr := Address{State: "Selangor"}

	q, err := Resolve(set, addr, 1500, Options{WeightG: 1500})
	if err != nil || q.RateID != "r_small" || q.PriceSen != 800 {
		t.Fatalf("1.5kg: %+v err=%v, want r_small 800", q, err)
	}
	q, err = Resolve(set, addr, 1500, Options{WeightG: 6000})
	if err != nil || q.RateID != "r_big" || q.PriceSen != 1500 {
		t.Fatalf("6kg: %+v err=%v, want r_big 1500", q, err)
	}
	if _, err := Resolve(set, addr, 1500, Options{WeightG: 20000}); !errors.Is(err, ErrNoRateForWeight) {
		t.Fatalf("20kg with no covering band: err=%v, want ErrNoRateForWeight", err)
	}
	// Unknown weight: bands are ignored, first rate by sort wins.
	q, err = Resolve(set, addr, 1500, Options{})
	if err != nil || q.RateID != "r_small" {
		t.Fatalf("unknown weight: %+v err=%v, want r_small (bands ignored)", q, err)
	}

	// A zone without bands keeps its rate for ANY declared weight.
	flat := ZoneSet{Zones: []Zone{{
		ID: "z", Name: "West", States: []string{"peninsular"},
		Rates: []Rate{{ID: "r_flat", PriceSen: 900}},
	}}}
	q, err = Resolve(flat, addr, 1500, Options{WeightG: 25000})
	if err != nil || q.RateID != "r_flat" {
		t.Fatalf("unbanded zone with a weight: %+v err=%v, want r_flat", q, err)
	}

	// A zone with no rate at all is a refusal (never a free order) — including
	// when a rate exists but its band cannot match and others are banded.
	empty := ZoneSet{Zones: []Zone{{ID: "z", Name: "West", States: []string{"peninsular"}}}}
	if _, err := Resolve(empty, addr, 0, Options{}); !errors.Is(err, ErrNoRate) {
		t.Fatalf("zone with no rates: err=%v, want ErrNoRate", err)
	}
}

// TestPriorityOrdersEvaluationWithinOneZone: a lower priority number is
// evaluated first, and the FIRST matching zone wins even when a later one would
// also match.
func TestPriorityOrdersEvaluationWithinOneZone(t *testing.T) {
	set := ZoneSet{Zones: []Zone{
		{ID: "z_late", Name: "Late", States: []string{"peninsular"}, Priority: 5,
			Rates: []Rate{{ID: "r", PriceSen: 100}}},
		{ID: "z_early", Name: "Early", States: []string{"peninsular"}, Priority: 1,
			Rates: []Rate{{ID: "r", PriceSen: 200}}},
	}}
	q, err := Resolve(set, Address{State: "Perak"}, 0, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if q.ZoneID != "z_early" {
		t.Fatalf("zone %q won; priority 1 must be evaluated before priority 5", q.ZoneID)
	}
}

// TestPickRateIsDeterministicWithoutBands: same sort → cheapest first, then
// label, so two saves of "Standard/Express" pick the same rate for every buyer.
func TestPickRateIsDeterministicWithoutBands(t *testing.T) {
	rates := []Rate{
		{ID: "b", Label: "Express", PriceSen: 1500, Sort: 1},
		{ID: "a", Label: "Standard", PriceSen: 800, Sort: 0},
	}
	r, err := PickRate(rates, 0)
	if err != nil || r.ID != "a" {
		t.Fatalf("PickRate = %+v err=%v, want a (lowest sort)", r, err)
	}
	sameSort := []Rate{
		{ID: "expensive", Label: "Express", PriceSen: 1500},
		{ID: "cheap", Label: "Standard", PriceSen: 800},
	}
	r, err = PickRate(sameSort, 0)
	if err != nil || r.ID != "cheap" {
		t.Fatalf("PickRate with equal sort = %+v err=%v, want cheap", r, err)
	}
}
