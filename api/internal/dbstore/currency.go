package dbstore

// Supported currencies for per-space settlement (issue #185). The set mirrors
// the CHECK constraint in migration 000029_space_currency and the BNM
// exchange-rate coverage. MYR is the default for existing spaces.
var supportedCurrencies = map[string]bool{
	"MYR": true, "USD": true, "SGD": true, "EUR": true, "GBP": true,
	"AUD": true, "CNY": true, "JPY": true, "THB": true, "IDR": true,
	"PHP": true, "HKD": true, "KRW": true, "INR": true, "AED": true,
	"SAR": true, "NZD": true, "CAD": true, "CHF": true, "VND": true,
	"BND": true, "TWD": true, "MMK": true, "KHR": true, "NPR": true,
	"PKR": true, "EGP": true,
}

// IsSupportedCurrency reports whether code is a supported settlement
// currency. Used to validate per-space currency input and to fall back to
// MYR for legacy/unset rows.
func IsSupportedCurrency(code string) bool {
	return supportedCurrencies[code]
}
