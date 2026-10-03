package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestPostClassifiesModelUnavailableStatus pins WHICH sentinel each provider status
// produces for a bad-or-retired MODEL.
//
// Before this, the package had coverage for 401/403 only (TestPostTypedCredentialRejection),
// so every other status fell to the generic branch. Measured on prod: a retired model
// answered 410, the generic error reached respondAIErr, and every AI surface returned a
// bare 500 {"code":"internal"} while the assistant was still offered for ~13 h
// (card t_3a112596).
//
// The two sentinels are deliberately DIFFERENT. A 404/410 is NOT a rejected key, so
// classifying it as ErrCredential would log `llm_credential_rejected` for a fault the key
// has nothing to do with, sending an operator to rotate a key that works. The OTHER
// failures stay generic in the same table for the same reason in reverse: if they became
// sentinels, a genuine outage would masquerade as a configuration problem.
func TestPostClassifiesModelUnavailableStatus(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		wantCred bool
		wantGone bool
	}{
		{"401 unauthorized", 401, `{"error":{"message":"invalid api key"}}`, true, false},
		{"403 forbidden", 403, `{"error":{"message":"key revoked"}}`, true, false},
		{"404 model not found", 404, `{"error":{"message":"model gpt-9 does not exist"}}`, false, true},
		{"410 model retired", 410, `{"error":{"message":"deepseek-v3.2 was retired at 2026-09-25"}}`, false, true},
		{"500 provider failure", 500, `{"error":{"message":"boom"}}`, false, false},
		{"429 rate limited", 429, `{"error":{"message":"slow down"}}`, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := captureServer(t, tc.status, "application/json", tc.body)
			_, err := client.ChatWithTools(context.Background(), nil, nil)
			if err == nil {
				t.Fatalf("status %d was treated as success", tc.status)
			}
			if got := errors.Is(err, ErrCredential); got != tc.wantCred {
				t.Fatalf("errors.Is(err, ErrCredential) = %v, want %v — err = %v\n"+
					"A rejected key is a deployment state; a model that is missing or retired is a "+
					"DIFFERENT one, and every other failure must stay an internal error.",
					got, tc.wantCred, err)
			}
			if got := errors.Is(err, ErrModelUnavailable); got != tc.wantGone {
				t.Fatalf("errors.Is(err, ErrModelUnavailable) = %v, want %v — err = %v\n"+
					"A 404/410 must be reportable as an unavailable MODEL (not a rejected key, not a "+
					"generic internal error): that is the prod outage card t_3a112596 is about.",
					got, tc.wantGone, err)
			}
			if errors.Is(err, ErrCredential) && errors.Is(err, ErrModelUnavailable) {
				t.Fatalf("status %d wraps BOTH sentinels; they name different faults", tc.status)
			}
			// The provider's own message survives: it is what names the model to change.
			if tc.wantGone {
				if want := providerMessage(tc.body); want != "" && !errContains(err, want) {
					t.Errorf("err = %v, want it to carry the provider's message %q", err, want)
				}
			}
		})
	}
}

func errContains(err error, sub string) bool {
	return err != nil && strings.Contains(err.Error(), sub)
}
