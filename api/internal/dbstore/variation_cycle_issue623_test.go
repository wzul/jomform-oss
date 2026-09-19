package dbstore

// Issue #623: a variation may carry its own subscription cycle, and a field it
// does not set inherits the product's. Wan's decision: "inherit from the
// product", chosen because it is the only option with ZERO behaviour change for
// every subscription product that exists today — every existing variation is
// already in the "no cycle of its own" state.
//
// These are pure unit tests (no DB): the rule itself, which both the checkout
// and the recurring engine call.

import "testing"

func intp(n int) *int { return &n }

func TestEffectiveCycleInheritsWhenVariationSetsNothing(t *testing.T) {
	// The migration-safety case, and the reason this design was chosen: a
	// product whose variations set no cycle must bill EXACTLY as it does today.
	prod := &Product{BillingPeriod: "month", TrialDays: 14, SubscriptionCharges: intp(3)}
	variation := &ProductVariation{Name: "Size M"} // no cycle of its own

	got := EffectiveCycle(prod, variation)
	if got.Period != "month" {
		t.Errorf("period = %q, want the product's %q", got.Period, "month")
	}
	if got.TrialDays != 14 {
		t.Errorf("trial_days = %d, want the product's 14", got.TrialDays)
	}
	if got.Charges == nil || *got.Charges != 3 {
		t.Errorf("charges = %v, want the product's 3", got.Charges)
	}
}

func TestEffectiveCycleVariationOverridesProduct(t *testing.T) {
	// The request: "Size M monthly RM30" and "Size L yearly RM300" from ONE
	// product. The variation's own cycle must win.
	prod := &Product{BillingPeriod: "month", TrialDays: 0, SubscriptionCharges: intp(12)}
	yearly := &ProductVariation{Name: "Size L", BillingPeriod: "year", Charges: intp(1)}

	got := EffectiveCycle(prod, yearly)
	if got.Period != "year" {
		t.Errorf("period = %q, want the variation's %q — otherwise a yearly variation is billed monthly", got.Period, "year")
	}
	if got.Charges == nil || *got.Charges != 1 {
		t.Errorf("charges = %v, want the variation's 1", got.Charges)
	}
	if got.TrialDays != 0 {
		t.Errorf("trial_days = %d, want the product's 0 (the variation did not set one)", got.TrialDays)
	}
}

func TestEffectiveCycleInheritIsPerField(t *testing.T) {
	// Each column is nullable, so inheritance is per field rather than
	// all-or-nothing: a variation can set only the period and keep inheriting
	// the product's trial and charge count.
	prod := &Product{BillingPeriod: "month", TrialDays: 7, SubscriptionCharges: intp(6)}
	v := &ProductVariation{Name: "Size L", BillingPeriod: "year"}

	got := EffectiveCycle(prod, v)
	if got.Period != "year" {
		t.Errorf("period = %q, want the variation's year", got.Period)
	}
	if got.TrialDays != 7 {
		t.Errorf("trial_days = %d, want the product's 7", got.TrialDays)
	}
	if got.Charges == nil || *got.Charges != 6 {
		t.Errorf("charges = %v, want the product's 6", got.Charges)
	}
}

func TestEffectiveCycleSetToTheSameValueIsStillAnOverride(t *testing.T) {
	// "not set" and "explicitly set to the product's value" are DIFFERENT
	// states and must stay distinguishable. If the UI pre-filled the product's
	// cycle into every variation, the product-level setting would silently
	// become inert for those rows. The only way to tell them apart is the
	// pointer/empty-string, so this pins that the store keeps them apart.
	prod := &Product{BillingPeriod: "month", TrialDays: 3, SubscriptionCharges: intp(2)}
	inherits := &ProductVariation{Name: "Inherits"}
	explicit := &ProductVariation{Name: "Explicit", BillingPeriod: "month", TrialDays: intp(3), Charges: intp(2)}

	// The RESOLVED cycle is equal (same billing), so compare field by field —
	// the struct holds a *int, so a bare `==` would compare pointers.
	a, b := EffectiveCycle(prod, inherits), EffectiveCycle(prod, explicit)
	if a.Period != b.Period || a.TrialDays != b.TrialDays ||
		a.Charges == nil || b.Charges == nil || *a.Charges != *b.Charges {
		t.Errorf("a variation with no cycle and one set to the same values must resolve the same way: %+v vs %+v", a, b)
	}
	if inherits.BillingPeriod != "" || inherits.TrialDays != nil || inherits.Charges != nil {
		t.Errorf("the inheriting variation's fields must stay unset: %+v", inherits)
	}
	if explicit.BillingPeriod != "month" || explicit.TrialDays == nil || explicit.Charges == nil {
		t.Errorf("the explicitly-set variation's fields must stay set: %+v", explicit)
	}
	// Setting the variation back to unset must restore inheritance — the state
	// transition that proves the two states are still distinct.
	cleared := &ProductVariation{Name: "Cleared"}
	if got := EffectiveCycle(prod, cleared); got.Period != "month" || got.TrialDays != 3 ||
		got.Charges == nil || *got.Charges != 2 {
		t.Errorf("after clearing, the variation must inherit the product again; got %+v", got)
	}
}

func TestEffectiveCycleNilInputsAreSafe(t *testing.T) {
	// A simple product (no variation) and a bare mode with no product must not
	// panic, and must not invent a cycle.
	if got := EffectiveCycle(&Product{BillingPeriod: "year"}, nil); got.Period != "year" {
		t.Errorf("no variation: period = %q, want the product's year", got.Period)
	}
	if got := EffectiveCycle(nil, nil); got.Period != "" || got.TrialDays != 0 || got.Charges != nil {
		t.Errorf("no product and no variation must yield an empty cycle, got %+v", got)
	}
	v := &ProductVariation{BillingPeriod: "month"}
	if got := EffectiveCycle(nil, v); got.Period != "month" {
		t.Errorf("variation only: period = %q, want month", got.Period)
	}
}
