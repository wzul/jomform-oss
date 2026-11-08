package cryptoutil

import "strings"

// MaskSecret renders a stored secret for display: at most a mode hint plus the
// last four characters, never the body of the secret.
//
// This is the ONE mask rule for every surface that shows a stored secret (the
// settings page through GET /v1/rest/settings/chip and the dashboard bundle,
// and the get_chip_settings MCP tool). Do not hand-roll another copy: the
// previous per-package copies in httpapi and mcpsrv both returned everything
// BUT the last four characters, so a merchant's 88-character CHIP secret was
// published 84 characters at a time (card t_56a2bd7f).
//
// The contract:
//
//   - the output never contains a character from the middle of the secret;
//
//   - the output does not grow with the secret's length — its budget is a
//     recognised mode prefix plus the bullet run plus four characters;
//
//   - a recognised `sk_live_`/`sk_test_` prefix is kept so the merchant can
//     still tell live from test; the prefix is public (it is what the merchant
//     types), the rest is not;
//
//   - the last four characters are kept so the merchant can recognise which
//     key is stored (the same tail WooCommerce shows);
//
//   - a secret with no more characters than the tail is masked whole, because
//     showing its only characters would defeat the mask.
//
//     sk_live_abcdefghijKw==  -> sk_live_••••Kw==
//     abcdefghijKw==          -> ••••Kw==
//     abc                     -> ••••
func MaskSecret(secret string) string {
	s := strings.TrimSpace(secret)
	if s == "" {
		return ""
	}
	prefix := modePrefix(s)
	body := strings.TrimPrefix(s, prefix)
	runes := []rune(body)
	if len(runes) <= secretTailLen {
		return prefix + secretMask
	}
	return prefix + secretMask + string(runes[len(runes)-secretTailLen:])
}

const (
	// secretMask is U+2022 (bullet) repeated — the same glyph the settings UI
	// shows in its placeholder, so the masked line looks like a mask.
	secretMask    = "••••"
	secretTailLen = 4
)

// modePrefix returns the recognised public mode prefix of a secret, or "".
// Only these two are kept; anything else is part of the secret.
func modePrefix(s string) string {
	for _, p := range [...]string{"sk_live_", "sk_test_"} {
		if strings.HasPrefix(s, p) {
			return p
		}
	}
	return ""
}
