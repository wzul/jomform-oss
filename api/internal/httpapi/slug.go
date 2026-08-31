package httpapi

import (
	"crypto/rand"
	"encoding/hex"

	"regexp"
	"strings"
)

var slugUnsafe = regexp.MustCompile(`[^a-z0-9-]+`)

// slugify builds a URL-safe slug from an email local-part, guaranteed
// non-empty by falling back to the user ID prefix.
func slugify(localPart, userID string) string {
	base := strings.ToLower(localPart)
	base = strings.ReplaceAll(base, ".", "-")
	base = slugUnsafe.ReplaceAllString(base, "")
	base = strings.Trim(base, "-")
	if base == "" {
		base = userID[:8]
	}
	if len(base) > 30 {
		base = base[:30]
	}
	return base
}

// shortRand returns 4 hex chars for collision retries.
func shortRand() string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
