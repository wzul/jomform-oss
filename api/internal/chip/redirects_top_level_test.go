package chip

import (
	"encoding/json"
	"strings"
	"testing"
)

// CHIP only honors redirect/callback/top-level reference at the request ROOT.
// Verified against portal.purchases: nested-in-purchase values are silently discarded.
func TestRedirectsAtTopLevel(t *testing.T) {
	req := PurchaseRequest{
		BrandID:         "b",
		SuccessRedirect: "https://x/t/REF",
		SuccessCallback: "https://x/cb",
		Reference:       "REF",
		Purchase:        PurchaseDetails{Currency: "MYR"},
	}
	b, _ := json.Marshal(req)
	s := string(b)
	if !strings.Contains(s, `"success_redirect":"https://x/t/REF"`) ||
		!strings.Contains(s, `"reference":"REF"`) {
		t.Fatalf("redirects must marshal at top level: %s", s)
	}
}
