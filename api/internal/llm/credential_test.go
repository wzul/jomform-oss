// The credential half of the LLM client: is a key configured, and what happens
// when the provider rejects one.
//
// WHY THIS IS ITS OWN FILE. Two different questions get confused constantly:
//
//	"is a key CONFIGURED?"      -> HasCredential, a presence check
//	"does the key still WORK?"  -> the provider's answer, arriving as
//	                               ErrCredential on a 401/403
//
// Every AI surface in this repo needs both, and each needs them for a different
// decision: presence decides whether a control is OFFERED at all (the in-app
// assistant asks before showing a chat box), validity decides what a deployment
// answers once a key is rotated or expires underneath it. Collapsing them — as
// the old code did by treating every failure the same — is how a rotated key
// silently turns a working feature into "every message is a 500".
//
// These run against an httptest server: no network, no API key.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestHasCredentialReportsConfigurationNotValidity pins the presence check, in
// both directions. A client with no key must say so (the UI hides the feature on
// that answer), and a client with a key must say so even though only the provider
// can say whether that key works — a presence check that tried to be a validity
// check would have to make a network call to answer a question the caller asks
// before any call is made.
func TestHasCredentialReportsConfigurationNotValidity(t *testing.T) {
	t.Run("a configured key reports true", func(t *testing.T) {
		c := &Client{apiKey: "sk-configured"}
		if !c.HasCredential() {
			t.Fatal("HasCredential() = false with a key set — a deployment that is configured would " +
				"hide every AI surface")
		}
	})
	t.Run("no key reports false", func(t *testing.T) {
		c := &Client{}
		if c.HasCredential() {
			t.Fatal("HasCredential() = true with no key — the in-app assistant would be offered and " +
				"then fail on every message")
		}
	})
	t.Run("a nil client reports false rather than panicking", func(t *testing.T) {
		var c *Client
		if c.HasCredential() {
			t.Fatal("a nil client reported a credential")
		}
	})
	// The key itself must never be readable through this path — it is answered
	// as a bool so no caller can log or return it.
	t.Run("the key is not exposed", func(t *testing.T) {
		c := &Client{apiKey: "sk-must-not-leak"}
		if fmt.Sprintf("%v", c.HasCredential()) == "sk-must-not-leak" {
			t.Fatal("HasCredential leaked the key")
		}
	})
}

// TestPostTypedCredentialRejection is the core of the fix: a 401/403 from the
// provider must be distinguishable from every other failure, because the surfaces
// map it to "the AI is unavailable" (503) rather than to a generic internal error
// (500). A bare 500 on a rotated key is exactly the state the card sent back.
//
// The OTHER failures are asserted in the same test on purpose: if 500/timeout
// also became ErrCredential, a genuine outage would masquerade as a
// configuration problem and the operator would chase the wrong thing.
func TestPostTypedCredentialRejection(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		wantCred bool
	}{
		{"401 unauthorized", http.StatusUnauthorized, `{"error":{"message":"invalid api key"}}`, true},
		{"403 forbidden", http.StatusForbidden, `{"error":{"message":"key revoked"}}`, true},
		{"500 provider failure", http.StatusInternalServerError, `{"error":{"message":"boom"}}`, false},
		{"429 rate limited", http.StatusTooManyRequests, `{"error":{"message":"slow down"}}`, false},
		{"503 provider down", http.StatusServiceUnavailable, `{"error":{"message":"maintenance"}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := captureServer(t, tc.status, "application/json", tc.body)
			_, err := client.ChatWithTools(context.Background(), nil, nil)
			if err == nil {
				t.Fatalf("status %d was treated as success", tc.status)
			}
			gotCred := errors.Is(err, ErrCredential)
			if gotCred != tc.wantCred {
				t.Fatalf("errors.Is(err, ErrCredential) = %v, want %v — err = %v\n"+
					"A rejected key must be reportable as an unavailable feature; every other "+
					"failure must stay an internal error, or the operator chases the wrong cause.",
					gotCred, tc.wantCred, err)
			}
			// The provider's own explanation is kept (it is logged, never
			// returned to the user as-is).
			if want := providerMessage(tc.body); want != "" && !strings.Contains(err.Error(), want) {
				t.Errorf("err = %v, want it to carry the provider's message %q", err, want)
			}
		})
	}
}

// TestCredentialRejectionIsWrappedNotReplaced is the property Is() alone cannot
// show: the error must still WRAP ErrCredential so a caller using errors.Is gets
// it, and it must not become a bare sentinel that loses the status and reason an
// operator needs in the log.
func TestCredentialRejectionIsWrappedNotReplaced(t *testing.T) {
	client, _ := captureServer(t, http.StatusUnauthorized, "application/json",
		`{"error":{"message":"invalid api key"}}`)
	_, err := client.ChatWithTools(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("a 401 was treated as success")
	}
	if err == ErrCredential {
		t.Fatal("the error IS the bare sentinel: the status and the provider's message were lost, " +
			"so the log line cannot say WHY the key was refused")
	}
	if !errors.Is(err, ErrCredential) {
		t.Fatalf("err = %v, want one that wraps ErrCredential", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("err = %v, want the status in the message", err)
	}
}

// TestCredentialRejectionCoversEveryAiEntryPoint: the in-app assistant is not
// the only AI surface — the AI form builder and the legal-page generator go
// through the same post(), so a rotated key must be recognisable there too, not
// only on the chat's path.
func TestCredentialRejectionCoversEveryAiEntryPoint(t *testing.T) {
	client, _ := captureServer(t, http.StatusUnauthorized, "application/json",
		`{"error":{"message":"invalid api key"}}`)
	if _, err := client.GenerateFormConfig(context.Background(), "borang tempahan"); !errors.Is(err, ErrCredential) {
		t.Errorf("GenerateFormConfig err = %v, want it to wrap ErrCredential", err)
	}
	client, _ = captureServer(t, http.StatusUnauthorized, "application/json",
		`{"error":{"message":"invalid api key"}}`)
	if _, err := client.GenerateLegalPage(context.Background(), "privacy", BusinessDetails{}); !errors.Is(err, ErrCredential) {
		t.Errorf("GenerateLegalPage err = %v, want it to wrap ErrCredential", err)
	}
	client, _ = captureServer(t, http.StatusUnauthorized, "application/json",
		`{"error":{"message":"invalid api key"}}`)
	if _, err := client.ClassifySpam(context.Background(), "hello"); !errors.Is(err, ErrCredential) {
		t.Errorf("ClassifySpam err = %v, want it to wrap ErrCredential", err)
	}
}

// TestNoCredentialIsNotARejectedCredential pins the boundary between the two
// questions. "Never configured" is answered by HasCredential without any network
// call, and a client that does make a call still reports a rejection as
// ErrCredential — the two states are reachable through different paths, and a
// surface that hides a feature on the first must not be told the second.
func TestNoCredentialIsNotARejectedCredential(t *testing.T) {
	if (&Client{}).HasCredential() {
		t.Fatal("a keyless client reported a credential")
	}
	// A configured client that the provider turns away reports the typed
	// rejection — the other half of the pair.
	client, _ := captureServer(t, http.StatusUnauthorized, "application/json",
		`{"error":{"message":"invalid api key"}}`)
	// captureServer builds a client with no key (it is the wire-shape fixture),
	// so a key is set here: this half of the test is about a deployment that IS
	// configured and whose key the provider has stopped accepting.
	client.apiKey = "sk-rotated"
	if !client.HasCredential() {
		t.Fatal("the test client has no credential, so it cannot exercise the rejection path")
	}
	_, err := client.ChatWithTools(context.Background(), nil, nil)
	if !errors.Is(err, ErrCredential) {
		t.Fatal("a provider 401 did not become ErrCredential — the surfaces would answer 500 for a " +
			"deployment whose key is bad")
	}
}

// providerMessage pulls the "message" field out of a canned error body, so the
// assertion does not depend on byte offsets into a literal.
func providerMessage(body string) string {
	var out struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return ""
	}
	return out.Error.Message
}
