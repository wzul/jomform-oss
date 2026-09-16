package chip

// Issue #557 (second cause): CHIP's GET /public_key/ returns the PEM as a BARE
// JSON STRING, not as an object.
//
//	"-----BEGIN PUBLIC KEY-----\nMIIB...\n-----END PUBLIC KEY-----\n"
//
// The client decoded into a struct `{public_key: string}`, so the unmarshal
// failed ("cannot unmarshal string into Go value of type struct"). PublicKey
// returned an error, the payments service propagated it, the callback handler
// never obtained a key, and the fail-closed guard (#365) rejected EVERY signed
// callback with 503 — while CHIP had already charged the customer and saved the
// card token. Every sale stayed pending forever.
//
// The wiring fix alone (#558) was not enough: it made the lookup reachable, and
// this decode bug then failed it one step later. Both had to be fixed.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
)

// pemFor returns a real PKIX PEM public key so the parse step is exercised too.
func pemFor(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

// servePublicKey returns a CHIP client whose /public_key/ responds with exactly
// the bytes given.
func servePublicKey(t *testing.T, body string) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /public_key/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return New(srv.URL)
}

// TestIssue557PublicKeyAcceptsBareJSONString is the regression. The body is the
// shape CHIP actually returns.
func TestIssue557PublicKeyAcceptsBareJSONString(t *testing.T) {
	pemStr := pemFor(t)
	body, err := json.Marshal(pemStr) // bare JSON string, as the live API sends
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	c := servePublicKey(t, string(body))
	got, err := c.PublicKey(context.Background(), "sk_test_557")
	if err != nil {
		t.Fatalf("issue #557: PublicKey rejected CHIP's actual bare-string response: %v\n"+
			"this is the decode that made every callback 503", err)
	}
	if got != pemStr {
		t.Errorf("PublicKey returned %d bytes, want the %d-byte PEM", len(got), len(pemStr))
	}
	// The whole point: the value must be usable as a verification key.
	if _, err := ParsePublicKey(got); err != nil {
		t.Errorf("ParsePublicKey on the returned value: %v", err)
	}
}

// TestIssue557PublicKeyStillAcceptsObjectForm keeps the object shape working, so
// a change on either side does not silently break verification again.
func TestIssue557PublicKeyStillAcceptsObjectForm(t *testing.T) {
	pemStr := pemFor(t)
	body, _ := json.Marshal(map[string]string{"public_key": pemStr})

	c := servePublicKey(t, string(body))
	got, err := c.PublicKey(context.Background(), "sk_test_557")
	if err != nil {
		t.Fatalf("PublicKey rejected the object form: %v", err)
	}
	if got != pemStr {
		t.Errorf("PublicKey returned %d bytes, want the %d-byte PEM", len(got), len(pemStr))
	}
}

// TestIssue557PublicKeyRejectsGarbage keeps the failure loud rather than
// returning an empty string that would silently disarm verification.
func TestIssue557PublicKeyRejectsGarbage(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `""`, `[]`, `{"public_key":""}`} {
		c := servePublicKey(t, body)
		got, err := c.PublicKey(context.Background(), "sk_test_557")
		if err == nil {
			t.Errorf("PublicKey(%s) = %q, want an error — an unrecognised response must fail loudly, "+
				"not return a value that leaves the callback unverifiable", body, got)
		}
	}
}
