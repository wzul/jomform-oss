package chip

import (
	"encoding/json"
	"testing"
)

func TestDebugMarshalRedirects(t *testing.T) {
	req := PurchaseRequest{
		CreatorAgent: "JomForm",
		BrandID:      "brand",
		Client:       &ClientDetails{Email: "a@b.c", FullName: "Wan"},
		Purchase: PurchaseDetails{
			SuccessRedirect: "https://jomform.com/t/REF",
			FailureRedirect: "https://jomform.com/t/REF?status=fail",
			SuccessCallback: "https://jomform.com/v1/payments/chip/callback",
		},
	}
	b, _ := json.Marshal(req)
	t.Logf("NESTED-STYLE SENT: %s", b)
}
