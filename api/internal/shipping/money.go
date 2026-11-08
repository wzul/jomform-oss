// Decimal money coming IN from a courier, converted to integer sen exactly once.
//
// WHY THIS FILE EXISTS. Every vendor answers with a decimal: Lalamove's
// `priceBreakdown.total` is the string "8.2", EasyParcel's `total_amount` is the
// JSON number 12.31. Letting either through as a float64 and multiplying by 100
// introduces the usual 0.1 artefacts (12.31 * 100 = 1230.9999…) and, sooner or
// later, a one-sen disagreement between what the courier charged and what the
// sale recorded. So the decimal is parsed from its DIGITS: integer part, two
// fraction digits, and a half-up round on the third — all in integer arithmetic.
//
// Money is integer sen everywhere else in this repo; this is the boundary where
// that rule is enforced for courier input.
package shipping

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// ErrAmount: the provider sent something that is not a decimal amount.
var ErrAmount = errors.New("the amount from the provider is not a valid decimal")

// ParseAmountSen converts a provider amount into integer sen.
//
// Accepted: a string ("8.2", "RM8.20", " 1,234.5 "), a json.Number, a float64
// (encoding/json's default for `any`), or an integer type. A currency prefix and
// thousands separators are tolerated because vendors are inconsistent about both.
//
// Rounding is HALF-UP on the third fraction digit (12.345 -> 1235 sen), the rule
// a merchant would apply by hand. Anything non-numeric is ErrAmount: a provider
// that answers "N/A" must not become a 0 fee.
func ParseAmountSen(v any) (int64, error) {
	var s string
	switch n := v.(type) {
	case nil:
		return 0, ErrAmount
	case string:
		s = strings.TrimSpace(n)
	case json.Number:
		s = n.String()
	case float64:
		// FormatFloat with -1 gives the shortest exact representation, so the
		// digits parsed below are the digits the vendor sent (12.31 -> "12.31").
		s = strconv.FormatFloat(n, 'f', -1, 64)
	case float32:
		s = strconv.FormatFloat(float64(n), 'f', -1, 32)
	case int:
		return int64(n) * 100, nil
	case int64:
		return n * 100, nil
	case int32:
		return int64(n) * 100, nil
	case uint64:
		return int64(n) * 100, nil
	default:
		return 0, ErrAmount
	}
	s = strings.TrimSpace(s)
	neg := false
	if strings.HasPrefix(s, "-") {
		neg, s = true, strings.TrimSpace(s[1:])
	}
	s = strings.TrimPrefix(s, "+")
	// A currency prefix and thousands separators are noise, not value.
	s = strings.TrimSpace(s)
	for _, pfx := range []string{"RM", "MYR", "$"} {
		if strings.HasPrefix(strings.ToUpper(s), pfx) {
			s = strings.TrimSpace(s[len(pfx):])
			break
		}
	}
	s = strings.ReplaceAll(s, ",", "")
	if s == "" {
		return 0, ErrAmount
	}

	intPart, fracPart := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, fracPart = s[:i], s[i+1:]
	}
	if intPart == "" {
		intPart = "0"
	}
	var sen int64
	for _, r := range intPart {
		if r < '0' || r > '9' {
			return 0, ErrAmount
		}
		sen = sen*10 + int64(r-'0')
		if sen > (1<<62)/10 {
			return 0, ErrAmount
		}
	}
	sen *= 100
	for _, r := range fracPart {
		if r < '0' || r > '9' {
			return 0, ErrAmount
		}
	}
	switch {
	case len(fracPart) >= 1:
		sen += int64(fracPart[0]-'0') * 10
	}
	if len(fracPart) >= 2 {
		sen += int64(fracPart[1] - '0')
	}
	if len(fracPart) >= 3 && fracPart[2] >= '5' {
		sen++
	}
	if neg {
		sen = -sen
	}
	return sen, nil
}

// FormatSen renders integer sen as a plain decimal string without a currency
// prefix ("8.20"), which is the shape a vendor expects us to echo back. Display
// formatting (with "RM") is the web layer's job.
func FormatSen(sen int64) string {
	neg := sen < 0
	if neg {
		sen = -sen
	}
	out := strconv.FormatInt(sen/100, 10) + "." + twoDigits(sen%100)
	if neg {
		return "-" + out
	}
	return out
}

// twoDigits renders 0..99 as exactly two digits.
func twoDigits(v int64) string {
	if v < 10 {
		return "0" + strconv.FormatInt(v, 10)
	}
	return strconv.FormatInt(v, 10)
}
