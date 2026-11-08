package payments

// Card t_06851d88: the pay-what-you-want limit rules. They live in payments so
// the REST handlers, the MCP tools and the checkout all judge a product by the
// same rule; these tests pin the rule itself.

import "testing"

func TestValidateCustomAmountLimits(t *testing.T) {
	i := func(v int64) *int64 { return &v }
	base := int64(100) // the platform floor

	cases := []struct {
		name      string
		min       int64
		max       *int64
		suggested *int64
		wantErr   bool
	}{
		{"floor only", base, nil, nil, false},
		{"floor with cap", base, i(5000), nil, false},
		{"floor with suggestion", base, nil, i(500), false},
		{"suggestion equals cap", base, i(5000), i(5000), false},
		{"below the platform floor", 99, nil, nil, true},
		{"zero", 0, nil, nil, true},
		{"negative", -500, nil, nil, true},
		{"cap below min", 1000, i(999), nil, true},
		{"suggestion below min", 1000, nil, i(999), true},
		{"suggestion above cap", 1000, i(5000), i(5001), true},
		{"cap above the platform ceiling", 1000, i(MaxAmountSen + 1), nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := ValidateCustomAmountLimits(tc.min, tc.max, tc.suggested)
			if tc.wantErr && msg == "" {
				t.Fatalf("ValidateCustomAmountLimits(%d,%v,%v) accepted an unusable triple", tc.min, tc.max, tc.suggested)
			}
			if !tc.wantErr && msg != "" {
				t.Fatalf("ValidateCustomAmountLimits(%d,%v,%v) refused a valid triple: %s", tc.min, tc.max, tc.suggested, msg)
			}
		})
	}
}

// TestCustomAmountFloorIsAboveOneSen states the reason for the floor in the
// same place it is enforced: with the acquirer's FIXED per-transaction fee, a
// sub-RM1.00 gift nets the merchant less than nothing.
func TestCustomAmountFloorIsAboveOneSen(t *testing.T) {
	if MinCustomAmountSen < 100 {
		t.Fatalf("MinCustomAmountSen = %d, want at least 100 (RM1.00)", MinCustomAmountSen)
	}
	// The floor is also the smallest amount at which the gross-up still leaves
	// the merchant whole: for anything the floor allows, net − asking ∈ {0,1}.
	for _, fixed := range feeGridFixed {
		for _, bp := range feeGridRates {
			net := func(asking int64) int64 {
				total, fee := GrossUp(asking, fixed, bp)
				return total - fee
			}(MinCustomAmountSen)
			if d := net - MinCustomAmountSen; d < 0 || d > 1 {
				t.Errorf("floor amount %d at fixed=%d bp=%d nets %+d sen, want 0..1",
					MinCustomAmountSen, fixed, bp, d)
			}
		}
	}
}

func TestCustomAmountDisplayPriceFollowsTheSuggestion(t *testing.T) {
	i := func(v int64) *int64 { return &v }
	if got := CustomAmountDisplayPrice(500, nil); got != 500 {
		t.Errorf("no suggestion: got %d, want the floor 500", got)
	}
	if got := CustomAmountDisplayPrice(500, i(2000)); got != 2000 {
		t.Errorf("with suggestion: got %d, want 2000", got)
	}
	// A zero suggestion is not a real pre-fill; fall back to the floor rather
	// than displaying RM0.00 for a product that cannot be bought for zero.
	if got := CustomAmountDisplayPrice(500, i(0)); got != 500 {
		t.Errorf("zero suggestion: got %d, want the floor 500", got)
	}
}
