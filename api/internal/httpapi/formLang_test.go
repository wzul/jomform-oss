package httpapi

import (
	"context"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// Issue #57: effective buyer-facing language = form.config.lang ?? space
// defaultLang ?? 'en'. These run without a DB: a nil Spaces store falls back
// to 'en' for the space default, while a valid per-form override must still
// win.
func TestEffectiveLangStringsResolution(t *testing.T) {
	h := &PublicHandlers{} // Spaces == nil → space default resolves to "en"

	ctx := context.Background()

	cases := []struct {
		name     string
		config   map[string]any
		wantLang string
		wantSub  string // English submit, so "en" default is observable
	}{
		{
			name:     "form override wins",
			config:   map[string]any{"lang": "ms"},
			wantLang: "ms",
			wantSub:  "Teruskan ke Pembayaran",
		},
		{
			name:     "form override zh",
			config:   map[string]any{"lang": "zh"},
			wantLang: "zh",
			wantSub:  "前往付款",
		},
		{
			name:     "no config -> en fallback",
			config:   map[string]any{},
			wantLang: "en",
			wantSub:  "Proceed to Payment",
		},
		{
			name:     "nil config -> en fallback",
			config:   nil,
			wantLang: "en",
			wantSub:  "Proceed to Payment",
		},
		{
			name:     "invalid lang ignored -> en fallback",
			config:   map[string]any{"lang": "fr"},
			wantLang: "en",
			wantSub:  "Proceed to Payment",
		},
		{
			name:     "ar override",
			config:   map[string]any{"lang": "ar"},
			wantLang: "ar",
			wantSub:  "الانتقال إلى الدفع",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &dbstore.Form{Config: tc.config}
			ls := h.effectiveLangStrings(ctx, f)
			if ls.Lang != tc.wantLang {
				t.Fatalf("lang = %q, want %q", ls.Lang, tc.wantLang)
			}
			if ls.Submit != tc.wantSub {
				t.Errorf("submit = %q, want %q", ls.Submit, tc.wantSub)
			}
		})
	}
}

// validFormLang accepts exactly the five supported codes.
func TestValidFormLang(t *testing.T) {
	ok := map[string]bool{
		"en": true, "ms": true, "zh": true, "ta": true, "ar": true,
		"fr": false, "": false, "EN": false, "de": false,
	}
	for code, want := range ok {
		if got := validFormLang(code); got != want {
			t.Errorf("validFormLang(%q) = %v, want %v", code, got, want)
		}
	}
}

// Every dictionary carries all system strings (no empty translations that
// could leak a blank label onto a buyer-facing form).
func TestFormLangsCompleteness(t *testing.T) {
	fields := []string{"EmailLabel", "CouponLabel", "Total", "Submit", "ErrorAlert", "NoProductAlert", "SelectVariation", "SelectVariationAlert", "InStock"}
	if len(formLangs) != 5 {
		t.Fatalf("formLangs has %d entries, want 5", len(formLangs))
	}
	for code, ls := range formLangs {
		if !validFormLang(code) {
			t.Errorf("dictionary key %q is not a supported language", code)
		}
		if ls.Lang != code {
			t.Errorf("lang %q has Lang=%q mismatch", code, ls.Lang)
		}
		for _, f := range fields {
			switch f {
			case "EmailLabel":
				if ls.EmailLabel == "" {
					t.Errorf("lang %q has empty EmailLabel", code)
				}
			case "CouponLabel":
				if ls.CouponLabel == "" {
					t.Errorf("lang %q has empty CouponLabel", code)
				}
			case "Total":
				if ls.Total == "" {
					t.Errorf("lang %q has empty Total", code)
				}
			case "Submit":
				if ls.Submit == "" {
					t.Errorf("lang %q has empty Submit", code)
				}
			case "ErrorAlert":
				if ls.ErrorAlert == "" {
					t.Errorf("lang %q has empty ErrorAlert", code)
				}
			case "NoProductAlert":
				if ls.NoProductAlert == "" {
					t.Errorf("lang %q has empty NoProductAlert", code)
				}
			case "SelectVariation":
				if ls.SelectVariation == "" {
					t.Errorf("lang %q has empty SelectVariation", code)
				}
			case "SelectVariationAlert":
				if ls.SelectVariationAlert == "" {
					t.Errorf("lang %q has empty SelectVariationAlert", code)
				}
			case "InStock":
				if ls.InStock == "" {
					t.Errorf("lang %q has empty InStock", code)
				}
			}
		}
	}
}
